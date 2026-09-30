//go:build linux

package fsinfo

import (
	"strconv"
	"syscall"
)

// statfsID is the filesystem id the kernel reports (f_fsid). Paths on one
// volume share it even when they sit on different mounts or subvolumes. ok is
// false when the kernel leaves it empty.
func statfsID(path string) (id string, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "", false
	}
	a, b := uint32(st.Fsid.X__val[0]), uint32(st.Fsid.X__val[1])
	if a == 0 && b == 0 {
		return "", false
	}
	return "fsid:" + strconv.FormatUint(uint64(a), 16) + "-" + strconv.FormatUint(uint64(b), 16), true
}
