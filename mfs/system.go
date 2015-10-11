// package mfs implements an in memory model of a mutable ipfs filesystem.
//
// It consists of four main structs:
// 1) The Filesystem
//        The filesystem serves as a container and entry point for various mfs filesystems
// 2) Root
//        Root represents an individual filesystem mounted within the mfs system as a whole
// 3) Directories
// 4) Files
package mfs

import (
	"errors"
	"sync"
	"time"

	key "github.com/ipfs/go-ipfs/blocks/key"
	dag "github.com/ipfs/go-ipfs/merkledag"
	ft "github.com/ipfs/go-ipfs/unixfs"

	context "github.com/ipfs/go-ipfs/Godeps/_workspace/src/golang.org/x/net/context"
	logging "github.com/ipfs/go-ipfs/vendor/QmXJkcEXB6C9h6Ytb6rrUTFU56Ro62zxgrbxTT3dgjQGA8/go-log"
)

var ErrNotExist = errors.New("no such rootfs")

var log = logging.Logger("mfs")

var ErrIsDirectory = errors.New("error: is a directory")

type childCloser interface {
	closeChild(string, *dag.Node) error
}

type NodeType int

const (
	TFile NodeType = iota
	TDir
)

// FSNode represents any node (directory, root, or file) in the mfs filesystem
type FSNode interface {
	GetNode() (*dag.Node, error)
	Type() NodeType
	Lock()
	Unlock()
}

// Root represents the root of a filesystem tree
type Root struct {
	// node is the merkledag root
	node *dag.Node

	// val represents the node. It can either be a File or a Directory
	val FSNode

	repub *Republisher

	dserv dag.DAGService

	Type string
}

type PubFunc func(context.Context, key.Key) error

// newRoot creates a new Root and starts up a republisher routine for it
func NewRoot(parent context.Context, ds dag.DAGService, node *dag.Node, pf PubFunc) (*Root, error) {
	ndk, err := node.Key()
	if err != nil {
		return nil, err
	}

	root := &Root{
		node:  node,
		repub: NewRepublisher(parent, pf, time.Millisecond*300, time.Second*3),
		dserv: ds,
	}

	root.repub.setVal(ndk)
	go root.repub.Run()

	pbn, err := ft.FromBytes(node.Data)
	if err != nil {
		log.Error("IPNS pointer was not unixfs node")
		return nil, err
	}

	switch pbn.GetType() {
	case ft.TDirectory:
		root.val = NewDirectory(parent, ndk.String(), node, root, ds)
	case ft.TFile, ft.TMetadata, ft.TRaw:
		fi, err := NewFile(ndk.String(), node, root, ds)
		if err != nil {
			return nil, err
		}
		root.val = fi
	default:
		panic("unrecognized! (NYI)")
	}
	return root, nil
}

func (kr *Root) GetValue() FSNode {
	return kr.val
}

// closeChild implements the childCloser interface, and signals to the publisher that
// there are changes ready to be published
func (kr *Root) closeChild(name string, nd *dag.Node) error {
	k, err := kr.dserv.Add(nd)
	if err != nil {
		return err
	}

	kr.repub.Update(k)
	return nil
}

func (kr *Root) Close() error {
	return kr.repub.Close()
}

// Republisher manages when to publish a given entry
type Republisher struct {
	TimeoutLong  time.Duration
	TimeoutShort time.Duration
	Publish      chan struct{}
	pubfunc      PubFunc
	pubnowch     chan struct{}

	ctx    context.Context
	cancel func()

	lk      sync.Mutex
	val     key.Key
	lastpub key.Key
}

func (rp *Republisher) getVal() key.Key {
	rp.lk.Lock()
	defer rp.lk.Unlock()
	return rp.val
}

// NewRepublisher creates a new Republisher object to republish the given root
// using the given short and long time intervals
func NewRepublisher(ctx context.Context, pf PubFunc, tshort, tlong time.Duration) *Republisher {
	ctx, cancel := context.WithCancel(ctx)
	return &Republisher{
		TimeoutShort: tshort,
		TimeoutLong:  tlong,
		Publish:      make(chan struct{}, 1),
		pubfunc:      pf,
		pubnowch:     make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
	}
}

func (p *Republisher) setVal(k key.Key) {
	p.lk.Lock()
	defer p.lk.Unlock()
	p.val = k
}

func (p *Republisher) pubNow() {
	select {
	case p.pubnowch <- struct{}{}:
	default:
	}
}

func (p *Republisher) Close() error {
	err := p.publish(p.ctx)
	p.cancel()
	return err
}

// Touch signals that an update has occurred since the last publish.
// Multiple consecutive touches may extend the time period before
// the next Publish occurs in order to more efficiently batch updates
func (np *Republisher) Update(k key.Key) {
	np.setVal(k)
	select {
	case np.Publish <- struct{}{}:
	default:
	}
}

// Run is the main republisher loop
func (np *Republisher) Run() {
	for {
		select {
		case <-np.Publish:
			quick := time.After(np.TimeoutShort)
			longer := time.After(np.TimeoutLong)

		wait:
			select {
			case <-np.ctx.Done():
				return
			case <-np.Publish:
				quick = time.After(np.TimeoutShort)
				goto wait
			case <-quick:
			case <-longer:
			case <-np.pubnowch:
			}

			err := np.publish(np.ctx)
			if err != nil {
				log.Error("republishRoot error: %s", err)
			}

		case <-np.ctx.Done():
			return
		}
	}
}

func (np *Republisher) publish(ctx context.Context) error {
	np.lk.Lock()
	topub := np.val
	np.lk.Unlock()

	log.Info("Publishing Changes!")
	err := np.pubfunc(ctx, topub)
	if err != nil {
		return err
	}
	np.lk.Lock()
	np.lastpub = topub
	np.lk.Unlock()
	return nil
}
