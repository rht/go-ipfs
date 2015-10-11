// package importer implements utilities used to create ipfs DAGs from files
// and readers
package importer

import (
	"fmt"
	"os"

	"github.com/ipfs/go-ipfs/commands/files"
	bal "github.com/ipfs/go-ipfs/importer/balanced"
	"github.com/ipfs/go-ipfs/importer/chunk"
	h "github.com/ipfs/go-ipfs/importer/helpers"
	trickle "github.com/ipfs/go-ipfs/importer/trickle"
	dag "github.com/ipfs/go-ipfs/merkledag"
	logging "github.com/ipfs/go-ipfs/vendor/QmXJkcEXB6C9h6Ytb6rrUTFU56Ro62zxgrbxTT3dgjQGA8/go-log"
)

var log = logging.Logger("importer")

// Builds a DAG from the given file, writing created blocks to disk as they are
// created
func BuildDagFromFile(fpath string, ds dag.DAGService) (*dag.Node, error) {
	stat, err := os.Lstat(fpath)
	if err != nil {
		return nil, err
	}

	if stat.IsDir() {
		return nil, fmt.Errorf("`%s` is a directory", fpath)
	}

	f, err := files.NewSerialFile(fpath, fpath, stat)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return BuildDagFromReader(ds, chunk.NewSizeSplitter(f, chunk.DefaultBlockSize))
}

func BuildDagFromReader(ds dag.DAGService, spl chunk.Splitter) (*dag.Node, error) {
	// Start the splitter
	blkch, errch := chunk.Chan(spl)

	dbp := h.DagBuilderParams{
		Dagserv:  ds,
		Maxlinks: h.DefaultLinksPerBlock,
	}

	return bal.BalancedLayout(dbp.New(blkch, errch))
}

func BuildTrickleDagFromReader(ds dag.DAGService, spl chunk.Splitter) (*dag.Node, error) {
	// Start the splitter
	blkch, errch := chunk.Chan(spl)

	dbp := h.DagBuilderParams{
		Dagserv:  ds,
		Maxlinks: h.DefaultLinksPerBlock,
	}

	return trickle.TrickleLayout(dbp.New(blkch, errch))
}
