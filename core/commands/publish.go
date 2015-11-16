package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	context "github.com/ipfs/go-ipfs/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/ipfs/go-ipfs/namesys"
	kb "github.com/ipfs/go-ipfs/routing/kbucket"

	key "github.com/ipfs/go-ipfs/blocks/key"
	cmds "github.com/ipfs/go-ipfs/commands"
	core "github.com/ipfs/go-ipfs/core"
	crypto "github.com/ipfs/go-ipfs/p2p/crypto"
	path "github.com/ipfs/go-ipfs/path"
	offroute "github.com/ipfs/go-ipfs/routing/offline"
)

var errNotOnline = errors.New("This command must be run in online mode. Try running 'ipfs daemon' first.")

var PublishCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Publish an object to IPNS",
		ShortDescription: `
IPNS is a PKI namespace, where names are the hashes of public keys, and
the private key enables publishing new (signed) values. In publish, the
default value of <name> is your own identity public key.
`,
		LongDescription: `
IPNS is a PKI namespace, where names are the hashes of public keys, and
the private key enables publishing new (signed) values. In publish, the
default value of <name> is your own identity public key.

Examples:

Publish an <ipfs-path> to your identity name:

  > ipfs name publish /ipfs/QmatmE9msSfkKxoffpHwNLNKgwZG8eT9Bud6YoPab52vpy
  Published to QmbCMUZw6JFeZ7Wp9jkzbye3Fzp2GGcPgC3nmeUjfVF87n: /ipfs/QmatmE9msSfkKxoffpHwNLNKgwZG8eT9Bud6YoPab52vpy

Publish an <ipfs-path> to another public key (not implemented):

  > ipfs name publish /ipfs/QmatmE9msSfkKxoffpHwNLNKgwZG8eT9Bud6YoPab52vpy QmbCMUZw6JFeZ7Wp9jkzbye3Fzp2GGcPgC3nmeUjfVF87n
  Published to QmbCMUZw6JFeZ7Wp9jkzbye3Fzp2GGcPgC3nmeUjfVF87n: /ipfs/QmatmE9msSfkKxoffpHwNLNKgwZG8eT9Bud6YoPab52vpy

`,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("ipfs-path", true, false, "IPFS path of the obejct to be published").EnableStdin(),
	},
	Options: []cmds.Option{
		cmds.BoolOption("resolve", "resolve given path before publishing (default=true)"),
		cmds.StringOption("lifetime", "t", "time duration that the record will be valid for (default: 24hrs)"),
		cmds.StringOption("ttl", "time duration this record should be cached for (caution: experimental)"),
	},
	Run: func(req cmds.Request, res cmds.Response) {
		log.Debug("Begin Publish")
		n, err := req.InvocContext().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		local, _, _ := req.Option("local").Bool()
		if !n.OnlineMode() {
			err := n.SetupOfflineRouting()
			if err != nil {
				res.SetError(err, cmds.ErrNormal)
				return
			}
		}

		pstr := req.Arguments()[0]

		if n.Identity == "" {
			res.SetError(errors.New("Identity not loaded!"), cmds.ErrNormal)
			return
		}

		popts := &publishOpts{
			verifyExists: true,
			pubValidTime: time.Hour * 24,
		}

		verif, found, _ := req.Option("resolve").Bool()
		if found {
			popts.verifyExists = verif
		}
		validtime, found, _ := req.Option("lifetime").String()
		if found {
			d, err := time.ParseDuration(validtime)
			if err != nil {
				res.SetError(fmt.Errorf("error parsing lifetime option: %s", err), cmds.ErrNormal)
				return
			}

			popts.pubValidTime = d
		}

		ctx := req.Context()
		if ttl, found, _ := req.Option("ttl").String(); found {
			d, err := time.ParseDuration(ttl)
			if err != nil {
				res.SetError(err, cmds.ErrNormal)
				return
			}

			ctx = context.WithValue(ctx, "ipns-publish-ttl", d)
		}

		output, err := publish(ctx, n, n.PrivateKey, path.Path(pstr), popts, local)
		if err != nil {
			switch err {
			case kb.ErrLookupFailure:
				res.SetError(errors.New("Please use 'ipfs name publish --local' instead"), cmds.ErrClient)
			default:
				res.SetError(err, cmds.ErrNormal)
			}
			return
		}
		res.SetOutput(output)
	},
	Marshalers: cmds.MarshalerMap{
		cmds.Text: func(res cmds.Response) (io.Reader, error) {
			v := res.Output().(*IpnsEntry)
			s := fmt.Sprintf("Published to %s: %s\n", v.Name, v.Value)
			return strings.NewReader(s), nil
		},
	},
	Type: IpnsEntry{},
}

type publishOpts struct {
	verifyExists bool
	pubValidTime time.Duration
}

func publish(ctx context.Context, n *core.IpfsNode, k crypto.PrivKey, ref path.Path, opts *publishOpts, local bool) (*IpnsEntry, error) {

	if opts.verifyExists {
		// verify the path exists
		_, err := core.Resolve(ctx, n, ref)
		if err != nil {
			return nil, err
		}
	}

	var err error
	if n.OnlineMode() && local {
		r := offroute.NewOfflineRouter(n.Repo.Datastore(), n.PrivateKey)
		rp := namesys.NewRoutingPublisher(r, n.Repo.Datastore())

		eol := time.Now().Add(opts.pubValidTime)
		err = rp.PublishWithEOL(ctx, k, ref, eol)
	} else {
		eol := time.Now().Add(opts.pubValidTime)
		err = n.Namesys.PublishWithEOL(ctx, k, ref, eol)
	}

	if err != nil {
		return nil, err
	}

	hash, err := k.GetPublic().Hash()
	if err != nil {
		return nil, err
	}

	return &IpnsEntry{
		Name:  key.Key(hash).String(),
		Value: ref.String(),
	}, nil
}
