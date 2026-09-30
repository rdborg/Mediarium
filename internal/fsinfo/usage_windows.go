//go:build windows

package fsinfo

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Usage is the free and total space of the filesystem holding a path.
type Usage struct {
	FreeBytes  uint64
	TotalBytes uint64
}

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// DiskUsage reports space available to the current user. Windows is only a
// local-development target; the container build uses the unix version.
func DiskUsage(path string) (Usage, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return Usage{}, err
	}
	var free, total, totalFree uint64
	r, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&free)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return Usage{}, callErr
	}
	return Usage{FreeBytes: free, TotalBytes: total}, nil
}

// deviceID is not available on Windows, so mount detection is skipped there.
func deviceID(path string) (uint64, bool, error) { return 0, false, nil }

// FilesystemID names the volume holding path (its drive letter or network
// share), so two folders on the same one can be recognised. ok is false when
// path cannot be read.
func FilesystemID(path string) (id string, ok bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(abs); err != nil {
		return "", false
	}
	vol := filepath.VolumeName(abs)
	if vol == "" {
		return "", false
	}
	return "vol:" + strings.ToUpper(vol), true
}

// statfsID does not exist on Windows; the drive letter is used instead.
func statfsID(path string) (id string, ok bool) { return "", false }
