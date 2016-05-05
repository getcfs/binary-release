package formic

import (
	"bytes"
	"encoding/binary"

	"github.com/spaolacci/murmur3"
)

func GetID(fsid []byte, inode, block uint64) []byte {
	// TODO: Figure out what arrangement we want to use for the hash
	h := murmur3.New128()
	h.Write(fsid)
	binary.Write(h, binary.BigEndian, inode)
	binary.Write(h, binary.BigEndian, block)
	s1, s2 := h.Sum128()
	b := bytes.NewBuffer([]byte(""))
	binary.Write(b, binary.BigEndian, s1)
	binary.Write(b, binary.BigEndian, s2)
	return b.Bytes()
}
