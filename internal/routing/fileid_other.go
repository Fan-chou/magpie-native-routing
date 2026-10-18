//go:build !unix

package routing

import "os"

func fileIdentity(os.FileInfo) uint64               { return 0 }
func sameFileIdentity(_ uint64, _ os.FileInfo) bool { return false }
