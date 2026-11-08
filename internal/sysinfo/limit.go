package sysinfo

import (
	"os"
	"strings"
)

// noLimit is the size above which a cgroup memory limit means "none": cgroup
// v1 reports a huge page-rounded number instead of the word "max".
const noLimit = 1 << 50

// ContainerMemoryLimit is the memory the container may use, read from the
// cgroup files under root ("/" outside of tests). ok is false when no limit
// is set or the files are not there (Windows, macOS, a plain Linux host).
func ContainerMemoryLimit(root string) (uint64, bool) {
	root = strings.TrimSuffix(root, "/")
	for _, p := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		if _, err := os.Stat(root + p); err != nil {
			continue
		}
		if n, ok := readUint(root + p); ok && n > 0 && n < noLimit {
			return n, true
		}
		return 0, false
	}
	return 0, false
}
