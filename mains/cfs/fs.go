package main

import (
	"log"
	"os"
	"sync"
	"time"

	"golang.org/x/net/context"

	pb "github.com/creiht/formic/proto"

	"bazil.org/fuse"
	"bazil.org/fuse/fuseutil"
)

type fs struct {
	conn    *fuse.Conn
	rpc     *rpc
	handles *fileHandles
}

func newfs(c *fuse.Conn, r *rpc) *fs {
	fs := &fs{
		conn:    c,
		rpc:     r,
		handles: newFileHandles(),
	}
	return fs
}

// Handle fuse request
func (f *fs) handle(r fuse.Request) {
	switch r := r.(type) {
	default:
		log.Printf("Unhandled request: %v", r)
		r.RespondError(fuse.ENOSYS)

	case *fuse.GetattrRequest:
		f.handleGetattr(r)

	case *fuse.LookupRequest:
		f.handleLookup(r)

	case *fuse.MkdirRequest:
		f.handleMkdir(r)

	case *fuse.OpenRequest:
		f.handleOpen(r)

	case *fuse.ReadRequest:
		f.handleRead(r)

	case *fuse.WriteRequest:
		f.handleWrite(r)

	case *fuse.CreateRequest:
		f.handleCreate(r)

	case *fuse.SetattrRequest:
		f.handleSetattr(r)

	case *fuse.ReleaseRequest:
		f.handleRelease(r)

	case *fuse.FlushRequest:
		f.handleFlush(r)

	case *fuse.InterruptRequest:
		f.handleInterrupt(r)

	case *fuse.ForgetRequest:
		f.handleForget(r)

	case *fuse.RemoveRequest:
		f.handleRemove(r)

	case *fuse.AccessRequest:
		f.handleAccess(r)

	case *fuse.SymlinkRequest:
		f.handleSymlink(r)

	case *fuse.ReadlinkRequest:
		f.handleReadlink(r)

	case *fuse.GetxattrRequest:
		f.handleGetxattr(r)

	case *fuse.ListxattrRequest:
		f.handleListxattr(r)

	case *fuse.SetxattrRequest:
		f.handleSetxattr(r)

	case *fuse.RemovexattrRequest:
		f.handleRemovexattr(r)

	case *fuse.RenameRequest:
		f.handleRename(r)

	case *fuse.StatfsRequest:
		f.handleStatfs(r)

		/*
			case *fuse.MknodRequest:
				f.handleMknod(r)

			case *fuse.InitRequest:
				f.handleInit(r)

			case *fuse.LinkRequest:
				f.handleLink(r)

			case *fuse.DestroyRequest:
				f.handleDestroy(r)

			case *fuse.FsyncRequest:
				f.handleFsync(r)
		*/
	}
}

type fileHandle struct {
	inode     fuse.NodeID
	readCache []byte
}

type fileHandles struct {
	cur     fuse.HandleID
	handles map[fuse.HandleID]*fileHandle
	sync.RWMutex
}

func newFileHandles() *fileHandles {
	return &fileHandles{
		cur:     0,
		handles: make(map[fuse.HandleID]*fileHandle),
	}
}

func (f *fileHandles) newFileHandle(inode fuse.NodeID) fuse.HandleID {
	f.Lock()
	defer f.Unlock()
	// TODO: not likely that you would use all uint64 handles, but should be better than this
	f.handles[f.cur] = &fileHandle{inode: inode}
	f.cur += 1
	return f.cur - 1
}

func (f *fileHandles) removeFileHandle(h fuse.HandleID) {
	f.Lock()
	defer f.Unlock()
	// TODO: Need to add error handling
	delete(f.handles, h)
}

func (f *fileHandles) cacheRead(h fuse.HandleID, data []byte) {
	f.Lock()
	defer f.Unlock()
	// TODO: Need to add error handling
	f.handles[h].readCache = data
}

func (f *fileHandles) getReadCache(h fuse.HandleID) []byte {
	f.RLock()
	defer f.RUnlock()
	// TODO: Need to add error handling
	return f.handles[h].readCache
}

func copyAttr(dst *fuse.Attr, src *pb.Attr) {
	dst.Inode = src.Inode
	dst.Mode = os.FileMode(src.Mode)
	dst.Size = src.Size
	dst.Mtime = time.Unix(src.Mtime, 0)
	dst.Atime = time.Unix(src.Atime, 0)
	dst.Ctime = time.Unix(src.Ctime, 0)
	dst.Crtime = time.Unix(src.Crtime, 0)
	dst.Uid = src.Uid
	dst.Gid = src.Gid
}

func (f *fs) handleGetattr(r *fuse.GetattrRequest) {
	log.Println("Inside handleGetattr")
	log.Println(r)
	resp := &fuse.GetattrResponse{}

	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	a, err := f.rpc.api.GetAttr(rctx, &pb.GetAttrRequest{Inode: uint64(r.Node)})
	if err != nil {
		log.Fatalf("GetAttr fail: %v", err)
	}
	copyAttr(&resp.Attr, a.Attr)

	log.Println(resp)
	r.Respond(resp)
}

func (f *fs) handleLookup(r *fuse.LookupRequest) {
	log.Println("Inside handleLookup")
	log.Printf("Running Lookup for %s", r.Name)
	log.Println(r)
	resp := &fuse.LookupResponse{}

	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	l, err := f.rpc.api.Lookup(rctx, &pb.LookupRequest{Name: r.Name, Parent: uint64(r.Node)})

	if err != nil {
		log.Fatalf("Lookup failed(%s): %v", r.Name, err)
	}
	// If there is no name then it wasn't found
	if l.Name != r.Name {
		log.Printf("ENOENT Lookup(%s)", r.Name)
		r.RespondError(fuse.ENOENT)
		return
	}
	resp.Node = fuse.NodeID(l.Attr.Inode)
	copyAttr(&resp.Attr, l.Attr)
	resp.EntryValid = 5 * time.Second

	log.Println(resp)
	r.Respond(resp)
}

func (f *fs) handleMkdir(r *fuse.MkdirRequest) {
	log.Println("Inside handleMkdir")
	log.Println(r)
	resp := &fuse.MkdirResponse{}

	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	m, err := f.rpc.api.MkDir(rctx, &pb.MkDirRequest{Name: r.Name, Parent: uint64(r.Node), Attr: &pb.Attr{Uid: r.Uid, Gid: r.Gid, Mode: uint32(r.Mode)}})
	if err != nil {
		log.Fatalf("Mkdir failed(%s): %v", r.Name, err)
	}
	// If the name is empty, then the dir already exists
	if m.Name != r.Name {
		log.Printf("EEXIST Mkdir(%s)", r.Name)
		r.RespondError(fuse.EEXIST)
		return
	}
	resp.Node = fuse.NodeID(m.Attr.Inode)
	copyAttr(&resp.Attr, m.Attr)
	resp.EntryValid = 5 * time.Second

	log.Println(resp)
	r.Respond(resp)
}

func (f *fs) handleOpen(r *fuse.OpenRequest) {
	log.Println("Inside handleOpen")
	log.Println(r)
	resp := &fuse.OpenResponse{}
	// For now use the inode as the file handle
	resp.Handle = f.handles.newFileHandle(r.Node)
	log.Println(resp)
	r.Respond(resp)
}

func (f *fs) handleRead(r *fuse.ReadRequest) {
	log.Println("Inside handleRead")
	log.Println(r)
	resp := &fuse.ReadResponse{Data: make([]byte, r.Size)}
	if r.Dir {
		// handle directory listing
		data := f.handles.getReadCache(r.Handle)
		if data == nil {
			rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)

			d, err := f.rpc.api.ReadDirAll(rctx, &pb.ReadDirAllRequest{Inode: uint64(r.Node)})
			log.Println(d)
			if err != nil {
				log.Fatalf("Read on dir failed: %v", err)
			}
			data = fuse.AppendDirent(data, fuse.Dirent{
				Name:  ".",
				Inode: uint64(r.Node),
				Type:  fuse.DT_Dir,
			})
			data = fuse.AppendDirent(data, fuse.Dirent{
				Name: "..",
				Type: fuse.DT_Dir,
			})
			for _, de := range d.DirEntries {
				log.Println(de)
				data = fuse.AppendDirent(data, fuse.Dirent{
					Name:  de.Name,
					Inode: de.Attr.Inode,
					Type:  fuse.DT_Dir,
				})
			}
			for _, fe := range d.FileEntries {
				log.Println(fe)
				data = fuse.AppendDirent(data, fuse.Dirent{
					Name:  fe.Name,
					Inode: fe.Attr.Inode,
					Type:  fuse.DT_File,
				})
			}
			f.handles.cacheRead(r.Handle, data)
		}
		fuseutil.HandleRead(r, resp, data)
		r.Respond(resp)
		return
	} else {
		// handle file read
		rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
		data, err := f.rpc.api.Read(rctx, &pb.ReadRequest{
			Inode:  uint64(r.Node),
			Offset: int64(r.Offset),
			Size:   int64(r.Size),
		})
		if err != nil {
			log.Fatal("Read on file failed: ", err)
		}
		copy(resp.Data, data.Payload)
		r.Respond(resp)
	}
}

func (f *fs) handleWrite(r *fuse.WriteRequest) {
	log.Println("Inside handleWrite")
	log.Printf("Writing %d bytes at offset %d", len(r.Data), r.Offset)
	log.Println(r)
	// TODO: Implement write
	// Currently this is stupid simple and doesn't handle all the possibilities
	resp := &fuse.WriteResponse{}
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	w, err := f.rpc.api.Write(rctx, &pb.WriteRequest{Inode: uint64(r.Node), Offset: r.Offset, Payload: r.Data})
	if err != nil {
		log.Fatalf("Write to file failed: %v", err)
	}
	if w.Status != 0 {
		log.Printf("Write status non zero(%d)\n", w.Status)
	}
	resp.Size = len(r.Data)
	r.Respond(resp)
}

func (f *fs) handleCreate(r *fuse.CreateRequest) {
	log.Println("Inside handleCreate")
	log.Println(r)
	resp := &fuse.CreateResponse{}
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	c, err := f.rpc.api.Create(rctx, &pb.CreateRequest{Parent: uint64(r.Node), Name: r.Name, Attr: &pb.Attr{Uid: r.Uid, Gid: r.Gid, Mode: uint32(r.Mode)}})
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	resp.Node = fuse.NodeID(c.Attr.Inode)
	copyAttr(&resp.Attr, c.Attr)
	resp.EntryValid = 5 * time.Second
	copyAttr(&resp.LookupResponse.Attr, c.Attr)
	resp.LookupResponse.EntryValid = 5 * time.Second
	r.Respond(resp)
}

func (f *fs) handleSetattr(r *fuse.SetattrRequest) {
	log.Println("Inside handleSetattr")
	log.Println(r)
	resp := &fuse.SetattrResponse{}
	resp.Attr.Inode = uint64(r.Node)
	a := &pb.Attr{
		Inode: uint64(r.Node),
	}
	if r.Valid.Size() {
		a.Size = r.Size
	}
	if r.Valid.Mode() {
		a.Mode = uint32(r.Mode)
	}
	if r.Valid.Atime() {
		a.Atime = r.Atime.Unix()
	}
	if r.Valid.AtimeNow() {
		a.Atime = time.Now().Unix()
	}
	if r.Valid.Mtime() {
		a.Mtime = r.Mtime.Unix()
	}
	if r.Valid.Uid() {
		a.Uid = r.Uid
	}
	if r.Valid.Gid() {
		a.Gid = r.Gid
	}
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	setAttrResp, err := f.rpc.api.SetAttr(rctx, &pb.SetAttrRequest{Attr: a, Valid: uint32(r.Valid)})
	if err != nil {
		log.Fatalf("Setattr failed: %v", err)
	}
	copyAttr(&resp.Attr, setAttrResp.Attr)
	log.Println(resp)
	r.Respond(resp)
}

func (f *fs) handleFlush(r *fuse.FlushRequest) {
	log.Println("Inside handleFlush")
	r.Respond()
}

func (f *fs) handleRelease(r *fuse.ReleaseRequest) {
	log.Println("Inside handleRelease")
	f.handles.removeFileHandle(r.Handle)
	r.Respond()
}

func (f *fs) handleInterrupt(r *fuse.InterruptRequest) {
	log.Println("Inside handleInterrupt")
	// TODO: Just passing on this for now.  Need to figure out what really needs to be done here
	r.Respond()
}

func (f *fs) handleForget(r *fuse.ForgetRequest) {
	log.Println("Inside handleForget")
	// TODO: Just passing on this for now.  Need to figure out what really needs to be done here
	r.Respond()
}

func (f *fs) handleRemove(r *fuse.RemoveRequest) {
	// TODO: Handle dir deletions correctly
	log.Println("Inside handleRemove")
	log.Println(r)
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	_, err := f.rpc.api.Remove(rctx, &pb.RemoveRequest{Parent: uint64(r.Node), Name: r.Name})
	if err != nil {
		log.Fatalf("Failed to delete file: %v", err)
	}
	r.Respond()
}

func (f *fs) handleAccess(r *fuse.AccessRequest) {
	log.Println("Inside handleAccess")
	// TODO: Add real access support, for now allows everything
	r.Respond()
}

// TODO: Implement the following functions (and make sure to comment out the case)
// Note: All handle functions should call r.Respond or r.Respond error before returning

func (f *fs) handleMknod(r *fuse.MknodRequest) {
	log.Println("Inside handleMknod")
	// NOTE: We probably will not need this since we implement Create
	r.RespondError(fuse.EIO)
}

func (f *fs) handleInit(r *fuse.InitRequest) {
	log.Println("Inside handleInit")
	r.RespondError(fuse.ENOSYS)
}

func (f *fs) handleStatfs(r *fuse.StatfsRequest) {
	log.Println("Inside handleStatfs")
	log.Println(r)
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	resp, err := f.rpc.api.Statfs(rctx, &pb.StatfsRequest{})
	if err != nil {
		log.Fatalf("Failed to Statfs : %v", err)
	}
	fuse_resp := &fuse.StatfsResponse{
		Blocks:  resp.Blocks,
		Bfree:   resp.Bfree,
		Bavail:  resp.Bavail,
		Files:   resp.Files,
		Ffree:   resp.Ffree,
		Bsize:   resp.Bsize,
		Namelen: resp.Namelen,
		Frsize:  resp.Frsize,
	}
	r.Respond(fuse_resp)
}

func (f *fs) handleSymlink(r *fuse.SymlinkRequest) {
	log.Println("Inside handleSymlink")
	log.Println(r)
	resp := &fuse.SymlinkResponse{}
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	symlink, err := f.rpc.api.Symlink(rctx, &pb.SymlinkRequest{Parent: uint64(r.Node), Name: r.NewName, Target: r.Target, Uid: r.Uid, Gid: r.Gid})
	if err != nil {
		log.Fatalf("Symlink failed: %v", err)
	}
	resp.Node = fuse.NodeID(symlink.Attr.Inode)
	copyAttr(&resp.Attr, symlink.Attr)
	resp.EntryValid = 5 * time.Second
	log.Println(resp)
	r.Respond(resp)
}

func (f *fs) handleReadlink(r *fuse.ReadlinkRequest) {
	log.Println("Inside handleReadlink")
	log.Println(r)
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	resp, err := f.rpc.api.Readlink(rctx, &pb.ReadlinkRequest{Inode: uint64(r.Node)})
	if err != nil {
		log.Fatalf("Readlink failed: %v", err)
	}
	log.Println(resp)
	r.Respond(resp.Target)
}

func (f *fs) handleLink(r *fuse.LinkRequest) {
	log.Println("Inside handleLink")
	r.RespondError(fuse.ENOSYS)
}

func (f *fs) handleGetxattr(r *fuse.GetxattrRequest) {
	log.Println("Inside handleGetxattr")
	log.Println(r)
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	req := &pb.GetxattrRequest{
		Inode:    uint64(r.Node),
		Name:     r.Name,
		Size:     r.Size,
		Position: r.Position,
	}
	resp, err := f.rpc.api.Getxattr(rctx, req)
	if err != nil {
		log.Fatalf("Getxattr failed: %v", err)
	}
	fuse_resp := &fuse.GetxattrResponse{Xattr: resp.Xattr}
	log.Println(fuse_resp)
	r.Respond(fuse_resp)
}

func (f *fs) handleListxattr(r *fuse.ListxattrRequest) {
	log.Println("Inside handleListxattr")
	log.Println(r)
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	req := &pb.ListxattrRequest{
		Inode:    uint64(r.Node),
		Size:     r.Size,
		Position: r.Position,
	}
	resp, err := f.rpc.api.Listxattr(rctx, req)
	if err != nil {
		log.Fatalf("Listxattr failed: %v", err)
	}
	fuse_resp := &fuse.ListxattrResponse{Xattr: resp.Xattr}
	log.Println(fuse_resp)
	r.Respond(fuse_resp)
}

func (f *fs) handleSetxattr(r *fuse.SetxattrRequest) {
	log.Println("Inside handleSetxattr")
	log.Println(r)
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	req := &pb.SetxattrRequest{
		Inode:    uint64(r.Node),
		Name:     r.Name,
		Value:    r.Xattr,
		Position: r.Position,
		Flags:    r.Flags,
	}
	_, err := f.rpc.api.Setxattr(rctx, req)
	if err != nil {
		log.Fatalf("Setxattr failed: %v", err)
	}
	r.Respond()
}

func (f *fs) handleRemovexattr(r *fuse.RemovexattrRequest) {
	log.Println("Inside handleRemovexattr")
	log.Println(r)
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	req := &pb.RemovexattrRequest{
		Inode: uint64(r.Node),
		Name:  r.Name,
	}
	_, err := f.rpc.api.Removexattr(rctx, req)
	if err != nil {
		log.Fatalf("Removexattr failed: %v", err)
	}
	r.Respond()
}

func (f *fs) handleDestroy(r *fuse.DestroyRequest) {
	log.Println("Inside handleDestroy")
	r.RespondError(fuse.ENOSYS)
}

func (f *fs) handleRename(r *fuse.RenameRequest) {
	log.Println("Inside handleRename")
	log.Println(r)
	rctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	_, err := f.rpc.api.Rename(rctx, &pb.RenameRequest{OldParent: uint64(r.Node), NewParent: uint64(r.NewDir), OldName: r.OldName, NewName: r.NewName})
	if err != nil {
		log.Fatalf("Rename failed: %v", err)
	}
	r.Respond()
}

func (f *fs) handleFsync(r *fuse.FsyncRequest) {
	log.Println("Inside handleFsync")
	r.RespondError(fuse.ENOSYS)
}
