package commands

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	gopath "path"
	"strings"

	"github.com/ipfs/go-ipfs/Godeps/_workspace/src/github.com/cheggaaa/pb"
	context "github.com/ipfs/go-ipfs/Godeps/_workspace/src/golang.org/x/net/context"

	cmds "github.com/ipfs/go-ipfs/commands"
	core "github.com/ipfs/go-ipfs/core"
	path "github.com/ipfs/go-ipfs/path"
	fsrepo "github.com/ipfs/go-ipfs/repo/fsrepo"
	tar "github.com/ipfs/go-ipfs/thirdparty/tar"
	uio "github.com/ipfs/go-ipfs/unixfs/io"
	utar "github.com/ipfs/go-ipfs/unixfs/tar"
)

var ErrInvalidCompressionLevel = errors.New("Compression level must be between 1 and 9")

var GetCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Download IPFS objects",
		ShortDescription: `
Retrieves the object named by <ipfs-or-ipns-path> and stores the data to disk.

By default, the output will be stored at ./<ipfs-path>, but an alternate path
can be specified with '--output=<path>' or '-o=<path>'.

To output a TAR archive instead of unpacked files, use '--archive' or '-a'.

To compress the output with GZIP compression, use '--compress' or '-C'. You
may also specify the level of compression by specifying '-l=<1-9>'.
`,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("ipfs-path", true, false, "The path to the IPFS object(s) to be outputted").EnableStdin(),
	},
	Options: []cmds.Option{
		cmds.StringOption("output", "o", "The path where output should be stored"),
		cmds.BoolOption("archive", "a", "Output a TAR archive"),
		cmds.BoolOption("compress", "C", "Compress the output with GZIP compression"),
		cmds.IntOption("compression-level", "l", "The level of compression (1-9)"),
	},
	PreRun: func(req cmds.Request) error {
		_, err := getCompressOptions(req)
		return err
	},
	Run: func(req cmds.Request, res cmds.Response) {
		cmplvl, err := getCompressOptions(req)
		if err != nil {
			res.SetError(err, cmds.ErrClient)
			return
		}

		node, err := req.Context().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		p := path.Path(req.Arguments()[0])
		var reader io.Reader
		if archive, _, _ := req.Option("archive").Bool(); !archive && cmplvl != gzip.NoCompression {
			// only use this when the flag is '-C' without '-a'
			reader, err = getZip(req.Context().Context, node, p, cmplvl)
		} else {
			reader, err = get(req.Context().Context, node, p, cmplvl)
		}
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
		res.SetOutput(reader)
	},
	PostRun: func(req cmds.Request, res cmds.Response) {
		if res.Output() == nil {
			return
		}
		outReader := res.Output().(io.Reader)
		res.SetOutput(nil)

		outPath, _, _ := req.Option("output").String()
		if len(outPath) == 0 {
			_, outPath = gopath.Split(req.Arguments()[0])
			outPath = gopath.Clean(outPath)
		}

		cmplvl, err := getCompressOptions(req)
		if err != nil {
			res.SetError(err, cmds.ErrClient)
			return
		}

		if archive, _, _ := req.Option("archive").Bool(); archive || cmplvl != gzip.NoCompression {
			if archive && !strings.HasSuffix(outPath, ".tar") {
				outPath += ".tar"
			}
			if cmplvl != gzip.NoCompression {
				outPath += ".gz"
			}
			fmt.Printf("Saving archive to %s\n", outPath)

			file, err := os.Create(outPath)
			if err != nil {
				res.SetError(err, cmds.ErrNormal)
				return
			}
			defer file.Close()

			bar := pb.New(0).SetUnits(pb.U_BYTES)
			bar.Output = os.Stderr
			pbReader := bar.NewProxyReader(outReader)
			bar.Start()
			defer bar.Finish()

			_, err = io.Copy(file, pbReader)
			if err != nil {
				res.SetError(err, cmds.ErrNormal)
				return
			}

			return
		}

		fmt.Printf("Saving file(s) to %s\n", outPath)

		// TODO: get total length of files
		bar := pb.New(0).SetUnits(pb.U_BYTES)
		bar.Output = os.Stderr

		// wrap the reader with the progress bar proxy reader
		reader := bar.NewProxyReader(outReader)

		bar.Start()
		defer bar.Finish()

		extractor := &tar.Extractor{outPath}
		err = extractor.Extract(reader)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
		}
	},
}

func getCompressOptions(req cmds.Request) (int, error) {
	cmprs, _, _ := req.Option("compress").Bool()
	cmplvl, cmplvlFound, _ := req.Option("compression-level").Int()
	switch {
	case !cmprs:
		return gzip.NoCompression, nil
	case cmprs && !cmplvlFound:
		return gzip.DefaultCompression, nil
	case cmprs && cmplvlFound && (cmplvl < 1 || cmplvl > 9):
		return gzip.NoCompression, ErrInvalidCompressionLevel
	}
	return gzip.NoCompression, nil
}

func get(ctx context.Context, node *core.IpfsNode, p path.Path, compression int) (io.Reader, error) {
	dagnode, err := core.Resolve(ctx, node, p)
	if err != nil {
		return nil, err
	}

	// Write to HEAD log
	fsrepo.Reflog("get " + p.String())

	return utar.NewReader(p, node.DAG, dagnode, compression)
}

// getZip is equivalent to `ipfs getdag $hash | gzip`
func getZip(ctx context.Context, node *core.IpfsNode, p path.Path, compression int) (io.Reader, error) {
	dagnode, err := core.Resolve(ctx, node, p)
	if err != nil {
		return nil, err
	}

	// Write to HEAD log
	fsrepo.Reflog("get " + p.String())

	reader, err := uio.NewDagReader(ctx, dagnode, node.DAG)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	gw, err := gzip.NewWriterLevel(pw, compression)
	if err != nil {
		return nil, err
	}
	bufin := bufio.NewReader(reader)
	go func() {
		_, err := bufin.WriteTo(gw)
		if err != nil {
			log.Error("Fail to compress the stream")
		}
		gw.Close()
		pw.Close()
	}()

	return pr, nil
}
