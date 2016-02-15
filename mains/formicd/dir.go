package main

import (
	"crypto/tls"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"bazil.org/fuse"

	pb "github.com/creiht/formic/proto"
	gp "github.com/pandemicsyn/oort/api/groupproto"
	"github.com/spaolacci/murmur3"
)

type DirService interface {
	GetAttr(id []byte) (*pb.Attr, error)
	SetAttr(id []byte, attr *pb.Attr, valid uint32) (*pb.Attr, error)
	Create(parent, id []byte, inode uint64, name string, attr *pb.Attr, isdir bool) (string, *pb.Attr, error)
	Update(id []byte, block, size, blocksize uint64, mtime int64)
	Lookup(parent []byte, name string) (string, *pb.Attr, error)
	ReadDirAll(id []byte) (*pb.ReadDirAllResponse, error)
	Remove(parent []byte, name string) (int32, error)
	Symlink(parent, id []byte, name string, target string, attr *pb.Attr, inode uint64) (*pb.SymlinkResponse, error)
	Readlink(id []byte) (*pb.ReadlinkResponse, error)
	Getxattr(id []byte, name string) (*pb.GetxattrResponse, error)
	Setxattr(id []byte, name string, value []byte) (*pb.SetxattrResponse, error)
	Listxattr(id []byte) (*pb.ListxattrResponse, error)
	Removexattr(id []byte, name string) (*pb.RemovexattrResponse, error)
	Rename(oldParent, newParent []byte, oldName, newName string) (*pb.RenameResponse, error)
}

// Entry describes each node in our fs.
// it also contains a list of all other entries "in this node".
// i.e. all files/directory in this directory.
type Entry struct {
	path  string // string path/name for this entry
	isdir bool
	sync.RWMutex
	attr      *pb.Attr
	parent    uint64            // inode of the parent
	inode     uint64            //the original/actual inode incase fuse stomps on the one in attr
	entries   map[string]uint64 // subdir/files by name
	ientries  map[uint64]string // subdir/files by inode
	nodeCount uint64            // uint64
	islink    bool
	target    string
	xattrs    map[string][]byte
	blocks    uint64 // For files, the number of blocks this file represents
	blocksize uint64 // The size of each block
	lastblock uint64 // The size of the last block
}

// Oort Group Store DS
type OortDS struct {
	addr               string
	gopts              []grpc.DialOption
	gcreds             credentials.TransportAuthenticator
	insecureSkipVerify bool
	conn               *grpc.ClientConn
	client             gp.GroupStoreClient
	sync.RWMutex
}

func NewOortDS(addr string, insecureSkipVerify bool, grpcOpts ...grpc.DialOption) (*OortDS, error) {
	var err error
	o := &OortDS{
		addr:  addr,
		gopts: grpcOpts,
		gcreds: credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: insecureSkipVerify,
		}),
		insecureSkipVerify: insecureSkipVerify,
	}
	o.gopts = append(o.gopts, grpc.WithTransportCredentials(o.gcreds))
	o.conn, err = grpc.Dial(o.addr, o.gopts...)
	if err != nil {
		return &OortDS{}, err
	}
	o.client = gp.NewGroupStoreClient(o.conn)
	return o, nil
}

func (o *OortDS) ConnClose() error {
	o.Lock()
	defer o.Unlock()
	return o.conn.Close()
}

func (o *OortDS) ConnState() (grpc.ConnectivityState, error) {
	o.RLock()
	defer o.RUnlock()
	return o.conn.State()
}

func (o *OortDS) GetReadStream(ctx context.Context, opts ...grpc.CallOption) (gp.GroupStore_StreamReadClient, error) {
	o.RLock()
	defer o.RUnlock()
	return o.client.StreamRead(ctx)
}

func (o *OortDS) GetWriteStream(ctx context.Context, opts ...grpc.CallOption) (gp.GroupStore_StreamWriteClient, error) {
	o.RLock()
	defer o.RUnlock()
	return o.client.StreamWrite(ctx)
}

// In memory implementation of DirService
// NOTE: This is only for testing and POC
type InMemDS struct {
	sync.RWMutex
	nodes map[uint64]*Entry
}

func NewInMemDS() *InMemDS {
	ds := &InMemDS{
		nodes: make(map[uint64]*Entry),
	}
	n := &Entry{
		path:     "/",
		inode:    1,
		isdir:    true,
		entries:  make(map[string]uint64),
		ientries: make(map[uint64]string),
	}
	ts := time.Now().Unix()
	n.attr = &pb.Attr{
		Inode:  n.inode,
		Atime:  ts,
		Mtime:  ts,
		Ctime:  ts,
		Crtime: ts,
		Mode:   uint32(os.ModeDir | 0775),
		Uid:    1001, // TODO: need to config default user/group id
		Gid:    1001,
	}
	ds.nodes[murmur3.Sum64(GetID(1, 1, n.attr.Inode, 0))] = n
	return ds
}

func (ds *InMemDS) GetAttr(id []byte) (*pb.Attr, error) {
	ds.RLock()
	defer ds.RUnlock()
	if entry, ok := ds.nodes[murmur3.Sum64(id)]; ok {
		return entry.attr, nil
	}
	return &pb.Attr{}, nil
}

func (ds *InMemDS) SetAttr(id []byte, attr *pb.Attr, v uint32) (*pb.Attr, error) {
	ds.Lock()
	defer ds.Unlock()
	valid := fuse.SetattrValid(v)
	if entry, ok := ds.nodes[murmur3.Sum64(id)]; ok {
		if valid.Mode() {
			entry.attr.Mode = attr.Mode
		}
		if valid.Size() {
			if attr.Size == 0 {
				entry.blocks = 0
				entry.lastblock = 0
			}
			entry.attr.Size = attr.Size
		}
		if valid.Mtime() {
			entry.attr.Mtime = attr.Mtime
		}
		if valid.Atime() {
			entry.attr.Atime = attr.Atime
		}
		if valid.Uid() {
			entry.attr.Uid = attr.Uid
		}
		if valid.Gid() {
			entry.attr.Gid = attr.Gid
		}
		return entry.attr, nil
	}
	return &pb.Attr{}, nil
}

func (ds *InMemDS) Create(parent, id []byte, inode uint64, name string, attr *pb.Attr, isdir bool) (string, *pb.Attr, error) {
	ds.Lock()
	defer ds.Unlock()
	p := murmur3.Sum64(parent)
	if _, exists := ds.nodes[p].entries[name]; exists {
		return "", &pb.Attr{}, nil
	}
	entry := &Entry{
		path:   name,
		inode:  inode,
		isdir:  isdir,
		attr:   attr,
		xattrs: make(map[string][]byte),
		blocks: 0,
	}
	if isdir {
		entry.entries = make(map[string]uint64)
		entry.ientries = make(map[uint64]string)
	}
	i := murmur3.Sum64(id)
	ds.nodes[i] = entry
	ds.nodes[p].entries[name] = i
	ds.nodes[p].ientries[i] = name
	atomic.AddUint64(&ds.nodes[p].nodeCount, 1)
	return name, attr, nil
}

func (ds *InMemDS) Lookup(parent []byte, name string) (string, *pb.Attr, error) {
	ds.RLock()
	defer ds.RUnlock()
	id, ok := ds.nodes[murmur3.Sum64(parent)].entries[name]
	if !ok {
		return "", &pb.Attr{}, nil
	}
	entry := ds.nodes[id]
	return entry.path, entry.attr, nil
}

// Needed to be able to sort the dirents
/*
type ByDirent []*pb.DirEnt

func (d ByDirent) Len() int {
	return len(d)
}

func (d ByDirent) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}

func (d ByDirent) Less(i, j int) bool {
	return d[i].Name < d[j].Name
}
*/

func (ds *InMemDS) ReadDirAll(id []byte) (*pb.ReadDirAllResponse, error) {
	ds.RLock()
	defer ds.RUnlock()
	e := &pb.ReadDirAllResponse{}
	for i, _ := range ds.nodes[murmur3.Sum64(id)].ientries {
		entry := ds.nodes[i]
		if entry.isdir {
			e.DirEntries = append(e.DirEntries, &pb.DirEnt{Name: entry.path, Attr: entry.attr})
		} else {
			e.FileEntries = append(e.FileEntries, &pb.DirEnt{Name: entry.path, Attr: entry.attr})
		}
	}
	sort.Sort(ByDirent(e.DirEntries))
	sort.Sort(ByDirent(e.FileEntries))
	return e, nil
}

func (ds *InMemDS) Remove(parent []byte, name string) (int32, error) {
	ds.Lock()
	defer ds.Unlock()
	p := murmur3.Sum64(parent)
	id, ok := ds.nodes[p].entries[name]
	if !ok {
		return 1, nil
	}
	delete(ds.nodes, id)
	delete(ds.nodes[p].entries, name)
	delete(ds.nodes[p].ientries, id)
	atomic.AddUint64(&ds.nodes[p].nodeCount, ^uint64(0)) // -1
	return 0, nil
}

func (ds *InMemDS) Update(id []byte, block, blocksize, size uint64, mtime int64) {
	// NOTE: Not sure what this function really should look like yet
	i := murmur3.Sum64(id)
	blocks := ds.nodes[i].blocks
	if block >= blocks {
		ds.nodes[i].blocks = block + 1
		ds.nodes[i].lastblock = size
		ds.nodes[i].blocksize = blocksize
		ds.nodes[i].attr.Size = blocksize*block + size
	} else if block == (blocks - 1) {
		ds.nodes[i].lastblock = size
		ds.nodes[i].attr.Size = blocksize*block + size
	}

	ds.nodes[i].attr.Mtime = mtime
}

func (ds *InMemDS) Symlink(parent, id []byte, name string, target string, attr *pb.Attr, inode uint64) (*pb.SymlinkResponse, error) {
	ds.Lock()
	defer ds.Unlock()
	p := murmur3.Sum64(parent)
	if _, exists := ds.nodes[p].entries[name]; exists {
		return &pb.SymlinkResponse{}, nil
	}
	entry := &Entry{
		path:   name,
		inode:  inode,
		isdir:  false,
		islink: true,
		target: target,
		attr:   attr,
		xattrs: make(map[string][]byte),
	}
	i := murmur3.Sum64(id)
	ds.nodes[i] = entry
	ds.nodes[p].entries[name] = i
	ds.nodes[p].ientries[i] = name
	atomic.AddUint64(&ds.nodes[p].nodeCount, 1)
	return &pb.SymlinkResponse{Name: name, Attr: attr}, nil
}

func (ds *InMemDS) Readlink(id []byte) (*pb.ReadlinkResponse, error) {
	ds.RLock()
	defer ds.RUnlock()
	return &pb.ReadlinkResponse{Target: ds.nodes[murmur3.Sum64(id)].target}, nil
}

func (ds *InMemDS) Getxattr(id []byte, name string) (*pb.GetxattrResponse, error) {
	ds.RLock()
	defer ds.RUnlock()
	if xattr, ok := ds.nodes[murmur3.Sum64(id)].xattrs[name]; ok {
		return &pb.GetxattrResponse{Xattr: xattr}, nil
	}
	return &pb.GetxattrResponse{}, nil
}

func (ds *InMemDS) Setxattr(id []byte, name string, value []byte) (*pb.SetxattrResponse, error) {
	ds.Lock()
	defer ds.Unlock()
	if entry, ok := ds.nodes[murmur3.Sum64(id)]; ok {
		entry.xattrs[name] = value
	}
	return &pb.SetxattrResponse{}, nil
}

func (ds *InMemDS) Listxattr(id []byte) (*pb.ListxattrResponse, error) {
	ds.RLock()
	defer ds.RUnlock()
	resp := &pb.ListxattrResponse{}
	if entry, ok := ds.nodes[murmur3.Sum64(id)]; ok {
		names := ""
		for name := range entry.xattrs {
			names += name
			names += "\x00"
		}
		resp.Xattr = []byte(names)
	}
	return resp, nil
}

func (ds *InMemDS) Removexattr(id []byte, name string) (*pb.RemovexattrResponse, error) {
	ds.Lock()
	defer ds.Unlock()
	if entry, ok := ds.nodes[murmur3.Sum64(id)]; ok {
		delete(entry.xattrs, name)
	}
	return &pb.RemovexattrResponse{}, nil
}

func (ds *InMemDS) Rename(oldParent, newParent []byte, oldName, newName string) (*pb.RenameResponse, error) {
	ds.Lock()
	defer ds.Unlock()
	p := murmur3.Sum64(oldParent)
	if id, ok := ds.nodes[p].entries[oldName]; ok {
		// remove old
		delete(ds.nodes[p].entries, oldName)
		delete(ds.nodes[p].ientries, id)
		atomic.AddUint64(&ds.nodes[p].nodeCount, ^uint64(0)) // -1
		// add new
		ds.nodes[id].path = newName
		np := murmur3.Sum64(newParent)
		ds.nodes[np].entries[newName] = id
		ds.nodes[np].ientries[id] = newName
		atomic.AddUint64(&ds.nodes[np].nodeCount, 1)
	}
	return &pb.RenameResponse{}, nil
}
