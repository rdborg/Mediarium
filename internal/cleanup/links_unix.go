//go:build !windows

package cleanup

import (
	"io/fs"
	"syscall"
)

// linkCount is how many hard links a file has (1 when unknown).
func linkCount(info fs.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}
