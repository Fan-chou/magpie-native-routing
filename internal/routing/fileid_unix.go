//go:build unix

package routing

import (
	"os"
	"syscall"
)

func fileIdentity(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev)<<32 ^ uint64(st.Ino)
	}
	return 0
}

func sameFileIdentity(id uint64, fi os.FileInfo) bool { return id == fileIdentity(fi) }
