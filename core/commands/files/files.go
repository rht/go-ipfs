package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	gopath "path"
	"strings"

	context "github.com/ipfs/go-ipfs/Godeps/_workspace/src/golang.org/x/net/context"
	cmds "github.com/ipfs/go-ipfs/commands"
	core "github.com/ipfs/go-ipfs/core"
	dag "github.com/ipfs/go-ipfs/merkledag"
	mfs "github.com/ipfs/go-ipfs/mfs"
	path "github.com/ipfs/go-ipfs/path"
	ft "github.com/ipfs/go-ipfs/unixfs"
	logging "github.com/ipfs/go-ipfs/vendor/QmXJkcEXB6C9h6Ytb6rrUTFU56Ro62zxgrbxTT3dgjQGA8/go-log"
)

var log = logging.Logger("cmds/files")

var FilesCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Manipulate unixfs files",
		ShortDescription: `
Files is an API for manipulating ipfs objects as if they were a unix filesystem.
`,
	},
	Subcommands: map[string]*cmds.Command{
		"read":  FilesReadCmd,
		"write": FilesWriteCmd,
		"mv":    FilesMvCmd,
		"cp":    FilesCpCmd,
		"ls":    FilesLsCmd,
		"mkdir": FilesMkdirCmd,
		"stat":  FilesStatCmd,
		"rm":    FilesRmCmd,
	},
}

var FilesStatCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "display file status",
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, false, "path to node to stat"),
	},
	Run: func(req cmds.Request, res cmds.Response) {
		node, err := req.InvocContext().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		path, err := checkPath(req.Arguments()[0])
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		fsn, err := mfs.Lookup(node.FilesRoot, path)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		o, err := statNode(fsn)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		res.SetOutput(o)
	},
	Marshalers: cmds.MarshalerMap{
		cmds.Text: func(res cmds.Response) (io.Reader, error) {
			out := res.Output().(*Object)
			buf := new(bytes.Buffer)
			fmt.Fprintln(buf, out.Hash)
			fmt.Fprintf(buf, "Size: %d\n", out.Size)
			fmt.Fprintf(buf, "CumulativeSize: %d\n", out.CumulativeSize)
			fmt.Fprintf(buf, "ChildBlocks: %d\n", out.Blocks)
			return buf, nil
		},
	},
	Type: Object{},
}

func statNode(fsn mfs.FSNode) (*Object, error) {
	nd, err := fsn.GetNode()
	if err != nil {
		return nil, err
	}

	k, err := nd.Key()
	if err != nil {
		return nil, err
	}

	d, err := ft.FromBytes(nd.Data)
	if err != nil {
		return nil, err
	}

	cumulsize, err := nd.Size()
	if err != nil {
		return nil, err
	}

	return &Object{
		Hash:           k.B58String(),
		Blocks:         len(nd.Links),
		Size:           d.GetFilesize(),
		CumulativeSize: cumulsize,
	}, nil
}

var FilesCpCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "copy files into mfs",
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("source", true, false, "source object to copy"),
		cmds.StringArg("dest", true, false, "destination to copy object to"),
	},
	Run: func(req cmds.Request, res cmds.Response) {
		node, err := req.InvocContext().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		src, err := checkPath(req.Arguments()[0])
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
		dst, err := checkPath(req.Arguments()[1])
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		nd, err := getNodeFromPath(req.Context(), node, src)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		err = mfs.PutNode(node.FilesRoot, dst, nd)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
	},
}

func getNodeFromPath(ctx context.Context, node *core.IpfsNode, p string) (*dag.Node, error) {
	switch {
	case strings.HasPrefix(p, "/ipfs/"):
		np, err := path.ParsePath(p)
		if err != nil {
			return nil, err
		}

		return core.Resolve(ctx, node, np)
	default:
		fsn, err := mfs.Lookup(node.FilesRoot, p)
		if err != nil {
			return nil, err
		}

		return fsn.GetNode()
	}
}

type Object struct {
	Hash           string
	Size           uint64
	CumulativeSize uint64
	Blocks         int
}

type FilesLsOutput struct {
	Entries []mfs.NodeListing
}

var FilesLsCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "List directories",
		ShortDescription: `
List directories.

Examples:

    $ ipfs files ls /welcome/docs/
    about
    contact
    help
    quick-start
    readme
    security-notes

    $ ipfs files ls /myfiles/a/b/c/d
    foo
    bar
`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, false, "path to show listing for"),
	},
	Options: []cmds.Option{
		cmds.BoolOption("l", "use long listing format"),
	},
	Run: func(req cmds.Request, res cmds.Response) {
		path, err := checkPath(req.Arguments()[0])
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		nd, err := req.InvocContext().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		fsn, err := mfs.Lookup(nd.FilesRoot, path)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		switch fsn := fsn.(type) {
		case *mfs.Directory:
			listing, err := fsn.List()
			if err != nil {
				res.SetError(err, cmds.ErrNormal)
				return
			}
			res.SetOutput(&FilesLsOutput{listing})
			return
		case *mfs.File:
			parts := strings.Split(path, "/")
			name := parts[len(parts)-1]
			out := &FilesLsOutput{[]mfs.NodeListing{mfs.NodeListing{Name: name, Type: 1}}}
			res.SetOutput(out)
			return
		default:
			res.SetError(errors.New("unrecognized type"), cmds.ErrNormal)
		}
	},
	Marshalers: cmds.MarshalerMap{
		cmds.Text: func(res cmds.Response) (io.Reader, error) {
			out := res.Output().(*FilesLsOutput)
			buf := new(bytes.Buffer)
			long, _, _ := res.Request().Option("l").Bool()

			for _, o := range out.Entries {
				if long {
					fmt.Fprintf(buf, "%s\t%s\t%d\n", o.Name, o.Hash, o.Size)
				} else {
					fmt.Fprintf(buf, "%s\n", o.Name)
				}
			}
			return buf, nil
		},
	},
	Type: FilesLsOutput{},
}

var FilesReadCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Read a file in a given mfs",
		ShortDescription: `
Read a specified number of bytes from a file at a given offset. By default, will
read the entire file similar to unix cat.

Examples:

    $ ipfs files read /test/hello
    hello
        `,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, false, "path to file to be read"),
	},
	Options: []cmds.Option{
		cmds.IntOption("o", "offset", "offset to read from"),
		cmds.IntOption("n", "count", "maximum number of bytes to read"),
	},
	Run: func(req cmds.Request, res cmds.Response) {
		n, err := req.InvocContext().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		path, err := checkPath(req.Arguments()[0])
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		fsn, err := mfs.Lookup(n.FilesRoot, path)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		fi, ok := fsn.(*mfs.File)
		if !ok {
			res.SetError(fmt.Errorf("%s was not a file", path), cmds.ErrNormal)
			return
		}

		offset, _, err := req.Option("offset").Int()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
		if offset < 0 {
			res.SetError(fmt.Errorf("cannot specify negative offset"), cmds.ErrNormal)
			return
		}

		filen, err := fi.Size()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		if int64(offset) > filen {
			res.SetError(fmt.Errorf("offset was past end of file (%d > %d)", offset, filen), cmds.ErrNormal)
			return
		}

		_, err = fi.Seek(int64(offset), os.SEEK_SET)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
		var r io.Reader = fi
		count, found, err := req.Option("count").Int()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
		if found {
			if count < 0 {
				res.SetError(fmt.Errorf("cannot specify negative 'count'"), cmds.ErrNormal)
				return
			}
			r = io.LimitReader(fi, int64(count))
		}

		res.SetOutput(r)
	},
}

var FilesMvCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Move files",
		ShortDescription: `
Move files around. Just like traditional unix mv.

Example:

    $ ipfs files mv /myfs/a/b/c /myfs/foo/newc

`,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("source", true, false, "source file to move"),
		cmds.StringArg("dest", true, false, "target path for file to be moved to"),
	},
	Run: func(req cmds.Request, res cmds.Response) {
		n, err := req.InvocContext().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		src, err := checkPath(req.Arguments()[0])
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
		dst, err := checkPath(req.Arguments()[1])
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		err = mfs.Mv(n.FilesRoot, src, dst)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
	},
}

var FilesWriteCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Write to a mutable file in a given filesystem",
		ShortDescription: `
Write data to a file in a given filesystem. This command allows you to specify
a beginning offset to write to. The entire length of the input will be written.

If the '--create' option is specified, the file will be created if it does not
exist. Nonexistant intermediate directories will not be created.

Example:

    echo "hello world" | ipfs files write --create /myfs/a/b/file
    echo "hello world" | ipfs files write --truncate /myfs/a/b/file
`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, false, "path to write to"),
		cmds.FileArg("data", true, false, "data to write").EnableStdin(),
	},
	Options: []cmds.Option{
		cmds.IntOption("o", "offset", "offset to write to"),
		cmds.BoolOption("e", "create", "create the file if it does not exist"),
		cmds.BoolOption("t", "truncate", "truncate the file before writing"),
		cmds.IntOption("n", "count", "maximum number of bytes to read"),
	},
	Run: func(req cmds.Request, res cmds.Response) {
		path, err := checkPath(req.Arguments()[0])
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		create, _, _ := req.Option("create").Bool()
		trunc, _, _ := req.Option("truncate").Bool()

		nd, err := req.InvocContext().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		fi, err := getFileHandle(nd.FilesRoot, path, create)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
		defer fi.Close()

		if trunc {
			if err := fi.Truncate(0); err != nil {
				res.SetError(err, cmds.ErrNormal)
				return
			}
		}

		offset, _, err := req.Option("offset").Int()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
		if offset < 0 {
			res.SetError(fmt.Errorf("cannot have negative write offset"), cmds.ErrNormal)
			return
		}

		count, countfound, err := req.Option("count").Int()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
		if countfound && count < 0 {
			res.SetError(fmt.Errorf("cannot have negative byte count"), cmds.ErrNormal)
			return
		}

		_, err = fi.Seek(int64(offset), os.SEEK_SET)
		if err != nil {
			log.Error("seekfail: ", err)
			res.SetError(err, cmds.ErrNormal)
			return
		}

		input, err := req.Files().NextFile()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		var r io.Reader = input
		if countfound {
			r = io.LimitReader(r, int64(count))
		}

		n, err := io.Copy(fi, input)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		log.Debugf("wrote %d bytes to %s", n, path)
	},
}

var FilesMkdirCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "make directories",
		ShortDescription: `
Create the directory if it does not already exist.

Note: all paths must be absolute.

Examples:

    $ ipfs mfs mkdir /test/newdir
    $ ipfs mfs mkdir -p /test/does/not/exist/yet
`,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, false, "path to dir to make"),
	},
	Options: []cmds.Option{
		cmds.BoolOption("p", "parents", "no error if existing, make parent directories as needed"),
	},
	Run: func(req cmds.Request, res cmds.Response) {
		n, err := req.InvocContext().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		dashp, _, _ := req.Option("parents").Bool()
		dirtomake, err := checkPath(req.Arguments()[0])
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		err = mfs.Mkdir(n.FilesRoot, dirtomake, dashp)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}
	},
}

var FilesRmCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "remove a file",
		ShortDescription: `
remove files or directories

    $ ipfs files rm /foo
    $ ipfs files ls /bar
    cat
    dog
    fish
    $ ipfs files rm -r /bar
`,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, true, "file to remove"),
	},
	Options: []cmds.Option{
		cmds.BoolOption("r", "recursive", "recursively remove directories"),
	},
	Run: func(req cmds.Request, res cmds.Response) {
		nd, err := req.InvocContext().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		path, err := checkPath(req.Arguments()[0])
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		if path == "/" {
			res.SetError(fmt.Errorf("cannot delete root"), cmds.ErrNormal)
			return
		}

		// 'rm a/b/c/' will fail unless we trim the slash at the end
		if path[len(path)-1] == '/' {
			path = path[:len(path)-1]
		}

		dir, name := gopath.Split(path)
		parent, err := mfs.Lookup(nd.FilesRoot, dir)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		pdir, ok := parent.(*mfs.Directory)
		if !ok {
			res.SetError(fmt.Errorf("no such file or directory: %s", path), cmds.ErrNormal)
			return
		}

		childi, err := pdir.Child(name)
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		dashr, _, _ := req.Option("r").Bool()

		switch childi.(type) {
		case *mfs.Directory:
			if dashr {
				err := pdir.Unlink(name)
				if err != nil {
					res.SetError(err, cmds.ErrNormal)
					return
				}
			} else {
				res.SetError(fmt.Errorf("%s is a directory, use -r to remove directories", path), cmds.ErrNormal)
				return
			}
		default:
			err := pdir.Unlink(name)
			if err != nil {
				res.SetError(err, cmds.ErrNormal)
				return
			}
		}
	},
}

func getFileHandle(r *mfs.Root, path string, create bool) (*mfs.File, error) {

	target, err := mfs.Lookup(r, path)
	switch err {
	case nil:
		fi, ok := target.(*mfs.File)
		if !ok {
			return nil, fmt.Errorf("%s was not a file", path)
		}
		return fi, nil

	case os.ErrNotExist:
		if !create {
			return nil, err
		}

		// if create is specified and the file doesnt exist, we create the file
		dirname, fname := gopath.Split(path)
		pdiri, err := mfs.Lookup(r, dirname)
		if err != nil {
			log.Error("lookupfail ", dirname)
			return nil, err
		}
		pdir, ok := pdiri.(*mfs.Directory)
		if !ok {
			return nil, fmt.Errorf("%s was not a directory", dirname)
		}

		nd := &dag.Node{Data: ft.FilePBData(nil, 0)}
		err = pdir.AddChild(fname, nd)
		if err != nil {
			return nil, err
		}

		fsn, err := pdir.Child(fname)
		if err != nil {
			return nil, err
		}

		fi, ok := fsn.(*mfs.File)
		if !ok {
			return nil, errors.New("expected *mfs.File, didnt get it. This is likely a race condition")
		}
		return fi, nil

	default:
		return nil, err
	}
}

func checkPath(p string) (string, error) {
	if len(p) == 0 {
		return "", fmt.Errorf("paths must not be empty")
	}

	if p[0] != '/' {
		return "", fmt.Errorf("paths must start with a leading slash")
	}

	cleaned := gopath.Clean(p)
	if p[len(p)-1] == '/' && p != "/" {
		cleaned += "/"
	}
	return cleaned, nil
}
