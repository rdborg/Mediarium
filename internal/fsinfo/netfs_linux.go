//go:build linux

package fsinfo

import "syscall"

// Filesystem types (statfs f_type) that are a share on another machine.
// FUSE and 9p are left out: mergerfs and Docker Desktop's shared folders use
// them for local disks, and a warning there would be wrong more often than
// right.
var networkTypes = map[int64]string{
	0x6969:     "NFS",
	0x517B:     "SMB",
	0xFF534D42: "SMB (CIFS)",
	0xFE534D42: "SMB",
	0x00C36400: "Ceph",
	0x5346414F: "AFS",
}

// NetworkFilesystem says whether path is on a network share, and which kind.
func NetworkFilesystem(path string) (kind string, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "", false
	}
	kind, ok = networkTypes[int64(st.Type)&0xFFFFFFFF]
	return kind, ok
}
