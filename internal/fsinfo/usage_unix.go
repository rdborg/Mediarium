//go:build !windows

package fsinfo

import "syscall"

// Usage is the free and total space of the filesystem holding a path.
type Usage struct {
	FreeBytes  uint64
	TotalBytes uint64
}

// DiskUsage reports space available to unprivileged users.
func DiskUsage(path string) (Usage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Usage{}, err
	}
	bsize := uint64(st.Bsize)
	return Usage{FreeBytes: uint64(st.Bavail) * bsize, TotalBytes: uint64(st.Blocks) * bsize}, nil
}

// deviceID returns the device number of the filesystem holding path.
func deviceID(path string) (uint64, bool, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, true, err
	}
	return uint64(st.Dev), true, nil
}
