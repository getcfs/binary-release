package main

import (
	"errors"
	"hash"
	"hash/crc32"
	"log"
	"os"
	"sort"
	"time"

	"bazil.org/fuse"

	pb "github.com/creiht/formic/proto"
	"github.com/gholt/brimtime"
	"github.com/gholt/store"
	"github.com/gogo/protobuf/proto"
	"github.com/spaolacci/murmur3"
	"golang.org/x/net/context"
)

const (
	InodeEntryVersion = 1
	DirEntryVersion   = 1
	FileBlockVersion  = 1
)

type FileService interface {
	GetAttr(ctx context.Context, id []byte) (*pb.Attr, error)
	SetAttr(ctx context.Context, id []byte, attr *pb.Attr, valid uint32) (*pb.Attr, error)
	Create(ctx context.Context, parent, id []byte, inode uint64, name string, attr *pb.Attr, isdir bool) (string, *pb.Attr, error)
	Update(ctx context.Context, id []byte, block, size, blocksize uint64, mtime int64) error
	Lookup(ctx context.Context, parent []byte, name string) (string, *pb.Attr, error)
	ReadDirAll(ctx context.Context, id []byte) (*pb.ReadDirAllResponse, error)
	Remove(ctx context.Context, parent []byte, name string) (int32, error)
	Symlink(ctx context.Context, parent, id []byte, name string, target string, attr *pb.Attr, inode uint64) (*pb.SymlinkResponse, error)
	Readlink(ctx context.Context, id []byte) (*pb.ReadlinkResponse, error)
	Getxattr(ctx context.Context, id []byte, name string) (*pb.GetxattrResponse, error)
	Setxattr(ctx context.Context, id []byte, name string, value []byte) (*pb.SetxattrResponse, error)
	Listxattr(ctx context.Context, id []byte) (*pb.ListxattrResponse, error)
	Removexattr(ctx context.Context, id []byte, name string) (*pb.RemovexattrResponse, error)
	Rename(ctx context.Context, oldParent, newParent []byte, oldName, newName string) (*pb.RenameResponse, error)
	GetChunk(ctx context.Context, id []byte) ([]byte, error)
	WriteChunk(ctx context.Context, id, data []byte) error
	DeleteChunk(ctx context.Context, id []byte, tsm int64) error
	DeleteListing(ctx context.Context, parent []byte, name string, tsm int64) error
	GetInode(ctx context.Context, id []byte) (*pb.InodeEntry, error)
	GetDirent(ctx context.Context, parent []byte, name string) (*pb.DirEntry, error)
}

var ErrStoreHasNewerValue = errors.New("Error store already has newer value")

type StoreComms struct {
	vstore store.ValueStore
	gstore store.GroupStore
}

func NewStoreComms(vstore store.ValueStore, gstore store.GroupStore) (*StoreComms, error) {
	return &StoreComms{
		vstore: vstore,
		gstore: gstore,
	}, nil
}

// Helper methods to get data from value and group store
func (o *StoreComms) ReadValue(ctx context.Context, id []byte) ([]byte, error) {
	// TODO: You might want to make this whole area pass in reusable []byte to
	// lessen gc pressure.
	keyA, keyB := murmur3.Sum128(id)
	_, v, err := o.vstore.Read(ctx, keyA, keyB, nil)
	return v, err
}

func (o *StoreComms) WriteValue(ctx context.Context, id, data []byte) error {
	keyA, keyB := murmur3.Sum128(id)
	timestampMicro := brimtime.TimeToUnixMicro(time.Now())
	oldTimestampMicro, err := o.vstore.Write(ctx, keyA, keyB, timestampMicro, data)
	if err != nil {
		return err
	}
	if oldTimestampMicro >= timestampMicro {
		return ErrStoreHasNewerValue
	}
	return nil
}

func (o *StoreComms) DeleteValue(ctx context.Context, id []byte) error {
	timestampMicro := brimtime.TimeToUnixMicro(time.Now())
	return o.DeleteValueTS(ctx, id, timestampMicro)
}

func (o *StoreComms) DeleteValueTS(ctx context.Context, id []byte, tsm int64) error {
	keyA, keyB := murmur3.Sum128(id)
	oldTimestampMicro, err := o.vstore.Delete(ctx, keyA, keyB, tsm)
	if oldTimestampMicro >= tsm {
		return ErrStoreHasNewerValue
	}
	return err
}

func (o *StoreComms) WriteGroup(ctx context.Context, key, childKey, value []byte) error {
	timestampMicro := brimtime.TimeToUnixMicro(time.Now())
	return o.WriteGroupTS(ctx, key, childKey, value, timestampMicro)
}

func (o *StoreComms) WriteGroupTS(ctx context.Context, key, childKey, value []byte, tsm int64) error {
	keyA, keyB := murmur3.Sum128(key)
	childKeyA, childKeyB := murmur3.Sum128(childKey)
	oldTimestampMicro, err := o.gstore.Write(ctx, keyA, keyB, childKeyA, childKeyB, tsm, value)
	if err != nil {
		return nil
	}
	if oldTimestampMicro >= tsm {
		return ErrStoreHasNewerValue
	}
	return nil
}

func (o *StoreComms) ReadGroupItem(ctx context.Context, key, childKey []byte) ([]byte, error) {
	childKeyA, childKeyB := murmur3.Sum128(childKey)
	return o.ReadGroupItemByKey(ctx, key, childKeyA, childKeyB)
}

func (o *StoreComms) ReadGroupItemByKey(ctx context.Context, key []byte, childKeyA, childKeyB uint64) ([]byte, error) {
	keyA, keyB := murmur3.Sum128(key)
	_, v, err := o.gstore.Read(ctx, keyA, keyB, childKeyA, childKeyB, nil)
	return v, err
}

func (o *StoreComms) DeleteGroupItem(ctx context.Context, key, childKey []byte) error {
	timestampMicro := brimtime.TimeToUnixMicro(time.Now())
	return o.DeleteGroupItemTS(ctx, key, childKey, timestampMicro)
}

func (o *StoreComms) DeleteGroupItemTS(ctx context.Context, key, childKey []byte, tsm int64) error {
	keyA, keyB := murmur3.Sum128(key)
	childKeyA, childKeyB := murmur3.Sum128(childKey)
	oldTimestampMicro, err := o.gstore.Delete(ctx, keyA, keyB, childKeyA, childKeyB, tsm)
	if err != nil {
		return err
	}
	if oldTimestampMicro >= tsm {
		return ErrStoreHasNewerValue
	}
	return nil
}

func (o *StoreComms) LookupGroup(ctx context.Context, key []byte) ([]store.LookupGroupItem, error) {
	keyA, keyB := murmur3.Sum128(key)
	items, err := o.gstore.LookupGroup(ctx, keyA, keyB)
	if err != nil {
		return nil, err
	}
	return items, nil
}

type OortFS struct {
	hasher     func() hash.Hash32
	comms      *StoreComms
	deleteChan chan *DeleteItem
}

func NewOortFS(vstore store.ValueStore, gstore store.GroupStore) (*OortFS, error) {
	// TODO: This all eventually needs to replaced with value and group rings
	comms, err := NewStoreComms(vstore, gstore)
	if err != nil {
		return &OortFS{}, err
	}
	o := &OortFS{
		hasher: crc32.NewIEEE,
		comms:  comms,
	}
	// TODO: This should be setup out of band when an FS is first created
	// NOTE: This also means that it is only single user until we init filesystems out of band
	// Init the root node
	id := GetID([]byte("1"), 1, 0)
	// TODO: The context.Background() calls likely need to be replaced with
	// actual contexts having a timeouts.
	n, err := o.GetChunk(context.Background(), id)
	if len(n) == 0 {
		log.Println("Root node not found, creating new root")
		// Need to create the root node
		r := &pb.InodeEntry{
			Version: InodeEntryVersion,
			Inode:   1,
			IsDir:   true,
			FsId:    []byte("1"),
		}
		ts := time.Now().Unix()
		r.Attr = &pb.Attr{
			Inode:  1,
			Atime:  ts,
			Mtime:  ts,
			Ctime:  ts,
			Crtime: ts,
			Mode:   uint32(os.ModeDir | 0775),
			Uid:    1001, // TODO: need to config default user/group id
			Gid:    1001,
		}
		b, err := proto.Marshal(r)
		if err != nil {
			return &OortFS{}, err
		}
		err = o.WriteChunk(context.Background(), id, b)
		if err != nil {
			return &OortFS{}, err
		}
	}
	// TODO: How big should the chan be, or should we have another in memory queue that feeds the chan?
	o.deleteChan = make(chan *DeleteItem, 1000)
	deletes := newDeletinator(o.deleteChan, o)
	go deletes.run()
	return o, nil
}

func (o *OortFS) GetAttr(ctx context.Context, id []byte) (*pb.Attr, error) {
	b, err := o.GetChunk(ctx, id)
	if err != nil {
		return &pb.Attr{}, err
	}
	n := &pb.InodeEntry{}
	err = proto.Unmarshal(b, n)
	if err != nil {
		return &pb.Attr{}, err
	}
	return n.Attr, nil
}

func (o *OortFS) SetAttr(ctx context.Context, id []byte, attr *pb.Attr, v uint32) (*pb.Attr, error) {
	valid := fuse.SetattrValid(v)
	b, err := o.GetChunk(ctx, id)
	if err != nil {
		return &pb.Attr{}, err
	}
	n := &pb.InodeEntry{}
	err = proto.Unmarshal(b, n)
	if err != nil {
		return &pb.Attr{}, err
	}
	if valid.Mode() {
		n.Attr.Mode = attr.Mode
	}
	if valid.Size() {
		if n.Attr.Size == 0 {
			n.Blocks = 0
			n.LastBlock = 0
		}
		n.Attr.Size = attr.Size
	}
	if valid.Mtime() {
		n.Attr.Mtime = attr.Mtime
	}
	if valid.Atime() {
		n.Attr.Atime = attr.Atime
	}
	if valid.Uid() {
		n.Attr.Uid = attr.Uid
	}
	if valid.Gid() {
		n.Attr.Gid = attr.Gid
	}
	b, err = proto.Marshal(n)
	if err != nil {
		return &pb.Attr{}, err
	}
	err = o.WriteChunk(ctx, id, b)
	if err != nil {
		return &pb.Attr{}, err
	}

	return n.Attr, nil
}

func (o *OortFS) Create(ctx context.Context, parent, id []byte, inode uint64, name string, attr *pb.Attr, isdir bool) (string, *pb.Attr, error) {
	// Check to see if the name already exists
	b, err := o.comms.ReadGroupItem(ctx, parent, []byte(name))
	if err != nil && !store.IsNotFound(err) {
		// TODO: Needs beter error handling
		return "", &pb.Attr{}, err
	}
	if len(b) > 0 {
		return "", &pb.Attr{}, nil
	}
	p := &pb.DirEntry{}
	err = proto.Unmarshal(b, p)
	if err != nil {
		return "", &pb.Attr{}, err
	}
	// Add the name to the group
	d := &pb.DirEntry{
		Version: DirEntryVersion,
		Name:    name,
		Id:      id,
	}
	b, err = proto.Marshal(d)
	if err != nil {
		return "", &pb.Attr{}, err
	}
	err = o.comms.WriteGroup(ctx, parent, []byte(name), b)
	if err != nil {
		return "", &pb.Attr{}, err
	}
	// Add the inode entry
	n := &pb.InodeEntry{
		Version: InodeEntryVersion,
		Inode:   inode,
		IsDir:   isdir,
		Attr:    attr,
		Blocks:  0,
	}
	b, err = proto.Marshal(n)
	if err != nil {
		return "", &pb.Attr{}, err
	}
	err = o.WriteChunk(ctx, id, b)
	if err != nil {
		return "", &pb.Attr{}, err
	}
	return name, attr, nil
}

func (o *OortFS) Lookup(ctx context.Context, parent []byte, name string) (string, *pb.Attr, error) {
	// Get the id
	b, err := o.comms.ReadGroupItem(ctx, parent, []byte(name))
	if store.IsNotFound(err) {
		return "", &pb.Attr{}, nil
	} else if err != nil {
		return "", &pb.Attr{}, err
	}
	d := &pb.DirEntry{}
	err = proto.Unmarshal(b, d)
	if err != nil {
		return "", &pb.Attr{}, err
	}
	if d.Tombstone != nil {
		return "", &pb.Attr{}, nil
	}
	// Get the Inode entry
	b, err = o.GetChunk(ctx, d.Id)
	if err != nil {
		return "", &pb.Attr{}, err
	}
	n := &pb.InodeEntry{}
	err = proto.Unmarshal(b, n)
	if err != nil {
		return "", &pb.Attr{}, err
	}
	return d.Name, n.Attr, nil
}

// Needed to be able to sort the dirents
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

func (o *OortFS) ReadDirAll(ctx context.Context, id []byte) (*pb.ReadDirAllResponse, error) {
	// Get the keys from the group
	items, err := o.comms.LookupGroup(ctx, id)
	log.Println("ITEMS: ", items)
	if err != nil {
		// TODO: Needs beter error handling
		log.Println("Error looking up group: ", err)
		return &pb.ReadDirAllResponse{}, err
	}
	// Iterate over each item, getting the ID then the Inode Entry
	e := &pb.ReadDirAllResponse{}
	dirent := &pb.DirEntry{}
	for _, item := range items {
		// lookup the item in the group to get the id
		b, err := o.comms.ReadGroupItemByKey(ctx, id, item.ChildKeyA, item.ChildKeyB)
		if err != nil {
			// TODO: Needs beter error handling
			log.Println("Error with lookup: ", err)
			continue
		}
		err = proto.Unmarshal(b, dirent)
		if err != nil {
			return &pb.ReadDirAllResponse{}, err
		}
		if dirent.Tombstone != nil {
			// Skip deleted entries
			continue
		}
		// get the inode entry
		b, err = o.GetChunk(ctx, dirent.Id)
		if err != nil {
			continue
		}
		n := &pb.InodeEntry{}
		err = proto.Unmarshal(b, n)
		if err != nil {
			continue
		}
		if n.IsDir {
			e.DirEntries = append(e.DirEntries, &pb.DirEnt{Name: dirent.Name, Attr: n.Attr})
		} else {
			e.FileEntries = append(e.FileEntries, &pb.DirEnt{Name: dirent.Name, Attr: n.Attr})
		}
	}
	sort.Sort(ByDirent(e.DirEntries))
	sort.Sort(ByDirent(e.FileEntries))
	return e, nil
}

func (o *OortFS) Remove(ctx context.Context, parent []byte, name string) (int32, error) {
	// Get the ID from the group list
	b, err := o.comms.ReadGroupItem(ctx, parent, []byte(name))
	if store.IsNotFound(err) {
		return 1, nil
	} else if err != nil {
		return 1, err
	}
	d := &pb.DirEntry{}
	err = proto.Unmarshal(b, d)
	if err != nil {
		return 1, err
	}
	// TODO: More error handling needed
	// TODO: Handle possible race conditions where user writes and deletes the same file over and over
	// Mark the item deleted in the group
	t := &pb.Tombstone{}
	tsm := brimtime.TimeToUnixMicro(time.Now())
	t.Dtime = tsm
	t.Qtime = tsm
	t.FsId = []byte("1") // TODO: Make sure this gets set when we are tracking fsids
	inode, err := o.GetInode(ctx, d.Id)
	if err != nil {
		return 1, err
	}
	t.Blocks = inode.Blocks
	t.Inode = inode.Inode
	d.Tombstone = t
	b, err = proto.Marshal(d)
	if err != nil {
		return 1, err
	}
	// NOTE: The tsm-1 is kind of a hack because the timestamp needs to be updated on this write, but if we choose tsm, once the actual delete comes through, it will not work because it is going to try to delete with a timestamp of tsm.
	err = o.comms.WriteGroupTS(ctx, parent, []byte(name), b, tsm-1)
	if err != nil {
		return 1, err // Not really sure what should be done here to try to recover from err
	}
	o.deleteChan <- &DeleteItem{
		parent: parent,
		name:   name,
	}
	return 0, nil
}

func (o *OortFS) Update(ctx context.Context, id []byte, block, blocksize, size uint64, mtime int64) error {
	b, err := o.GetChunk(ctx, id)
	if err != nil {
		return err
	}
	n := &pb.InodeEntry{}
	err = proto.Unmarshal(b, n)
	if err != nil {
		return err
	}
	blocks := n.Blocks
	if block >= blocks {
		n.Blocks = block + 1
		n.LastBlock = size
		n.BlockSize = blocksize
		n.Attr.Size = blocksize*block + size
	} else if block == (blocks - 1) {
		n.LastBlock = size
		n.Attr.Size = blocksize*block + size
	}

	n.Attr.Mtime = mtime
	b, err = proto.Marshal(n)
	if err != nil {
		return err
	}
	err = o.WriteChunk(ctx, id, b)
	if err != nil {
		return err
	}
	return nil
}

func (o *OortFS) Symlink(ctx context.Context, parent, id []byte, name string, target string, attr *pb.Attr, inode uint64) (*pb.SymlinkResponse, error) {
	// Check to see if the name exists
	val, err := o.comms.ReadGroupItem(ctx, parent, []byte(name))
	if err != nil && !store.IsNotFound(err) {
		// TODO: Needs beter error handling
		return &pb.SymlinkResponse{}, err
	}
	if len(val) > 1 { // Exists already
		return &pb.SymlinkResponse{}, nil
	}
	n := &pb.InodeEntry{
		Version: InodeEntryVersion,
		Inode:   inode,
		IsDir:   false,
		IsLink:  true,
		Target:  target,
		Attr:    attr,
	}
	b, err := proto.Marshal(n)
	if err != nil {
		return &pb.SymlinkResponse{}, err
	}
	err = o.WriteChunk(ctx, id, b)
	if err != nil {
		return &pb.SymlinkResponse{}, err
	}
	// Add the name to the group
	d := &pb.DirEntry{
		Version: DirEntryVersion,
		Name:    name,
		Id:      id,
	}
	b, err = proto.Marshal(d)
	if err != nil {
		return &pb.SymlinkResponse{}, err
	}
	err = o.comms.WriteGroup(ctx, parent, []byte(name), b)
	if err != nil {
		return &pb.SymlinkResponse{}, err
	}
	return &pb.SymlinkResponse{Name: name, Attr: attr}, nil
}

func (o *OortFS) Readlink(ctx context.Context, id []byte) (*pb.ReadlinkResponse, error) {
	b, err := o.GetChunk(ctx, id)
	if err != nil {
		return &pb.ReadlinkResponse{}, err
	}
	n := &pb.InodeEntry{}
	err = proto.Unmarshal(b, n)
	if err != nil {
		return &pb.ReadlinkResponse{}, err
	}
	return &pb.ReadlinkResponse{Target: n.Target}, nil
}

func (o *OortFS) Getxattr(ctx context.Context, id []byte, name string) (*pb.GetxattrResponse, error) {
	b, err := o.GetChunk(ctx, id)
	if err != nil {
		return &pb.GetxattrResponse{}, err
	}
	n := &pb.InodeEntry{}
	err = proto.Unmarshal(b, n)
	if err != nil {
		return &pb.GetxattrResponse{}, err
	}
	if xattr, ok := n.Xattr[name]; ok {
		return &pb.GetxattrResponse{Xattr: xattr}, nil
	}
	return &pb.GetxattrResponse{}, nil
}

func (o *OortFS) Setxattr(ctx context.Context, id []byte, name string, value []byte) (*pb.SetxattrResponse, error) {
	b, err := o.GetChunk(ctx, id)
	if err != nil {
		return &pb.SetxattrResponse{}, err
	}
	n := &pb.InodeEntry{}
	err = proto.Unmarshal(b, n)
	if err != nil {
		return &pb.SetxattrResponse{}, err
	}
	n.Xattr[name] = value
	b, err = proto.Marshal(n)
	if err != nil {
		return &pb.SetxattrResponse{}, err
	}
	err = o.WriteChunk(ctx, id, b)
	if err != nil {
		return &pb.SetxattrResponse{}, err
	}
	return &pb.SetxattrResponse{}, nil
}

func (o *OortFS) Listxattr(ctx context.Context, id []byte) (*pb.ListxattrResponse, error) {
	resp := &pb.ListxattrResponse{}
	b, err := o.GetChunk(ctx, id)
	if err != nil {
		return &pb.ListxattrResponse{}, err
	}
	n := &pb.InodeEntry{}
	err = proto.Unmarshal(b, n)
	if err != nil {
		return &pb.ListxattrResponse{}, err
	}
	names := ""
	for name := range n.Xattr {
		names += name
		names += "\x00"
	}
	resp.Xattr = []byte(names)
	return resp, nil
}

func (o *OortFS) Removexattr(ctx context.Context, id []byte, name string) (*pb.RemovexattrResponse, error) {
	b, err := o.GetChunk(ctx, id)
	if err != nil {
		return &pb.RemovexattrResponse{}, err
	}
	n := &pb.InodeEntry{}
	err = proto.Unmarshal(b, n)
	if err != nil {
		return &pb.RemovexattrResponse{}, err
	}
	delete(n.Xattr, name)
	b, err = proto.Marshal(n)
	if err != nil {
		return &pb.RemovexattrResponse{}, err
	}
	err = o.WriteChunk(ctx, id, b)
	if err != nil {
		return &pb.RemovexattrResponse{}, err
	}
	return &pb.RemovexattrResponse{}, nil
}

func (o *OortFS) Rename(ctx context.Context, oldParent, newParent []byte, oldName, newName string) (*pb.RenameResponse, error) {
	// Check if the new name already exists
	id, err := o.comms.ReadGroupItem(ctx, newParent, []byte(newName))
	if err != nil && !store.IsNotFound(err) {
		// TODO: Needs beter error handling
		return &pb.RenameResponse{}, err
	}
	if len(id) > 0 { // New name already exists
		return &pb.RenameResponse{}, nil
	}
	// Get the ID from the group list
	b, err := o.comms.ReadGroupItem(ctx, oldParent, []byte(oldName))
	if store.IsNotFound(err) {
		return &pb.RenameResponse{}, nil
	}
	if err != nil {
		return &pb.RenameResponse{}, err
	}
	d := &pb.DirEntry{}
	err = proto.Unmarshal(b, d)
	if err != nil {
		return &pb.RenameResponse{}, err
	}
	// Delete old entry
	err = o.comms.DeleteGroupItem(ctx, oldParent, []byte(oldName))
	if err != nil {
		return &pb.RenameResponse{}, err
	}
	// Create new entry
	d.Name = newName
	b, err = proto.Marshal(d)
	err = o.comms.WriteGroup(ctx, newParent, []byte(newName), b)
	if err != nil {
		return &pb.RenameResponse{}, err
	}
	return &pb.RenameResponse{}, nil
}

func (o *OortFS) GetChunk(ctx context.Context, id []byte) ([]byte, error) {
	b, err := o.comms.ReadValue(ctx, id)
	if store.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fb := &pb.FileBlock{}
	err = proto.Unmarshal(b, fb)
	if err != nil {
		return nil, err
	}
	// TODO: Validate checksum and handle errors
	return fb.Data, nil
}

func (o *OortFS) WriteChunk(ctx context.Context, id, data []byte) error {
	crc := o.hasher()
	crc.Write(data)
	fb := &pb.FileBlock{
		Version:  FileBlockVersion,
		Data:     data,
		Checksum: crc.Sum32(),
	}
	b, err := proto.Marshal(fb)
	if err != nil {
		return err
	}
	return o.comms.WriteValue(ctx, id, b)
}

func (o *OortFS) DeleteChunk(ctx context.Context, id []byte, tsm int64) error {
	return o.comms.DeleteValueTS(ctx, id, tsm)
}

func (o *OortFS) DeleteListing(ctx context.Context, parent []byte, name string, tsm int64) error {
	return o.comms.DeleteGroupItemTS(ctx, parent, []byte(name), tsm)
}

func (o *OortFS) GetInode(ctx context.Context, id []byte) (*pb.InodeEntry, error) {
	// Get the Inode entry
	b, err := o.GetChunk(ctx, id)
	if err != nil {
		return nil, err
	}
	n := &pb.InodeEntry{}
	err = proto.Unmarshal(b, n)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (o *OortFS) GetDirent(ctx context.Context, parent []byte, name string) (*pb.DirEntry, error) {
	// Get the Dir Entry
	b, err := o.comms.ReadGroupItem(ctx, parent, []byte(name))
	if store.IsNotFound(err) {
		return &pb.DirEntry{}, nil
	} else if err != nil {
		return &pb.DirEntry{}, err
	}
	d := &pb.DirEntry{}
	err = proto.Unmarshal(b, d)
	if err != nil {
		return &pb.DirEntry{}, err
	}
	return d, nil
}
