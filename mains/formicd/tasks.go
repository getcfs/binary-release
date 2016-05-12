package main

import (
	"log"

	"google.golang.org/grpc/peer"

	"github.com/creiht/formic"
	"github.com/gholt/store"

	"golang.org/x/net/context"
)

type localAddr struct{}

func (l localAddr) String() string {
	return "internal"
}
func (l localAddr) Network() string {
	return "internal"
}

type UpdateItem struct {
	id        []byte
	block     uint64
	blocksize uint64
	size      uint64
	mtime     int64
}

type Updatinator struct {
	in chan *UpdateItem
	fs FileService
}

func newUpdatinator(in chan *UpdateItem, fs FileService) *Updatinator {
	return &Updatinator{
		in: in,
		fs: fs,
	}
}

func (u *Updatinator) run() {
	// TODO: Add fan-out based on the id of the update
	for {
		toupdate := <-u.in
		log.Println("Updating: ", toupdate)
		// TODO: Need better context
		p := &peer.Peer{
			Addr: localAddr{},
		}
		ctx := peer.NewContext(context.Background(), p)
		err := u.fs.Update(ctx, toupdate.id, toupdate.block, toupdate.blocksize, toupdate.size, toupdate.mtime)
		if err != nil {
			log.Println("Delete failed, requeing: ", err)
			u.in <- toupdate
		}
	}
}

type DeleteItem struct {
	parent []byte
	name   string
}

type Deletinator struct {
	in chan *DeleteItem
	fs FileService
}

func newDeletinator(in chan *DeleteItem, fs FileService) *Deletinator {
	return &Deletinator{
		in: in,
		fs: fs,
	}
}

func (d *Deletinator) run() {
	// TODO: Parallelize this thing?
	for {
		todelete := <-d.in
		log.Println("Deleting: ", todelete)
		// TODO: Need better context
		p := &peer.Peer{
			Addr: localAddr{},
		}
		ctx := peer.NewContext(context.Background(), p)
		// Get the dir entry info
		dirent, err := d.fs.GetDirent(ctx, todelete.parent, todelete.name)
		if store.IsNotFound(err) {
			// NOTE: If it isn't found then it is likely deleted.
			//       Do we need to do more to ensure this?
			//       Skip for now
			continue
		}
		if err != nil {
			// TODO Better error handling?
			// re-q the id, to try again later
			log.Print("Delete error getting dirent: ", err)
			d.in <- todelete
			continue
		}
		ts := dirent.Tombstone
		deleted := uint64(0)
		for b := uint64(0); b < ts.Blocks; b++ {
			log.Println("  Deleting block: ", b)
			// Delete each block
			id := formic.GetID(ts.FsId, ts.Inode, b+1)
			err := d.fs.DeleteChunk(ctx, id, ts.Dtime)
			if err != nil && !store.IsNotFound(err) && err != ErrStoreHasNewerValue {
				continue
			}
			deleted++
		}
		if deleted == ts.Blocks {
			// Everything is deleted so delete the entry
			log.Println("  Deleting Inode")
			err := d.fs.DeleteChunk(ctx, formic.GetID(ts.FsId, ts.Inode, 0), ts.Dtime)
			if err != nil && !store.IsNotFound(err) && err != ErrStoreHasNewerValue {
				// Couldn't delete the inode entry so try again later
				d.in <- todelete
				continue
			}
			log.Println("  Deleting Listing")
			err = d.fs.DeleteListing(ctx, todelete.parent, todelete.name, ts.Dtime)
			if err != nil && !store.IsNotFound(err) && err != ErrStoreHasNewerValue {
				log.Println("  Err: ", err)
				// TODO: Better error handling
				// Ignore for now to be picked up later?
			}
		} else {
			// If all artifacts are not deleted requeue for later
			d.in <- todelete
		}
	}
}
