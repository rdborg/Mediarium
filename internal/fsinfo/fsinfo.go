// Package fsinfo inspects the folders Mediarium is pointed at (movies, TV,
// downloads): do they exist, can the app write to them, how much room is
// left, and — when running in Docker — are they actually folders mapped in
// from the host rather than part of the container's own disposable layer.
//
// It only ever reads, apart from creating and immediately deleting one
// uniquely named probe file to test writability. It never touches existing
// files.
package fsinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Folder describes one inspected directory.
type Folder struct {
	Path       string
	Exists     bool
	IsDir      bool
	Writable   bool
	FreeBytes  uint64
	TotalBytes uint64
	// Mounted is true when the folder sits on a different filesystem from the
	// container's root, i.e. it was mapped in from outside. MountKnown says
	// whether that could be determined (only inside Docker, on Linux).
	Mounted    bool
	MountKnown bool
	Warnings   []string
}

const (
	lowSpaceBytes    = 5 << 30 // 5 GiB
	lowSpaceFraction = 0.02
)

// InDocker reports whether the process looks like it runs in a container.
func InDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(data)
		return strings.Contains(s, "docker") || strings.Contains(s, "containerd") || strings.Contains(s, "kubepods")
	}
	return false
}

// CheckWritable proves the current user can create files in dir by making
// and removing one uniquely named file. Existing files are never touched.
func CheckWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".mediarium-write-test-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// Inspect examines path and explains anything that will cause trouble.
func Inspect(path string) Folder {
	f := Folder{Path: path}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	switch {
	case err != nil && os.IsNotExist(err):
		f.Warnings = append(f.Warnings, "This folder doesn't exist inside the container. Map a folder from your host to this path in your Docker settings (see the installation guide).")
		return f
	case err != nil:
		f.Warnings = append(f.Warnings, fmt.Sprintf("Can't read this folder: %v", err))
		return f
	case !info.IsDir():
		f.Exists = true
		f.Warnings = append(f.Warnings, "This path is a file, not a folder.")
		return f
	}
	f.Exists, f.IsDir = true, true

	if err := CheckWritable(path); err != nil {
		f.Warnings = append(f.Warnings, "Mediarium can't write to this folder. Check the folder's permissions on the host, or set the container's PUID and PGID to a user that owns it.")
	} else {
		f.Writable = true
	}

	if u, err := DiskUsage(path); err == nil {
		f.FreeBytes, f.TotalBytes = u.FreeBytes, u.TotalBytes
		if u.TotalBytes > 0 && (u.FreeBytes < lowSpaceBytes || float64(u.FreeBytes)/float64(u.TotalBytes) < lowSpaceFraction) {
			f.Warnings = append(f.Warnings, "Low disk space on this drive.")
		}
	}

	if InDocker() {
		if dev, ok, err := deviceID(path); err == nil && ok {
			if rootDev, ok2, err2 := deviceID("/"); err2 == nil && ok2 {
				f.MountKnown = true
				f.Mounted = dev != rootDev
				if !f.Mounted {
					f.Warnings = append(f.Warnings, "This folder is inside the container itself, not a folder you mapped from your host or NAS. Anything saved here is lost when the container is updated or recreated. Map a host folder to this path in your Docker settings.")
				}
			}
		}
	}
	return f
}
