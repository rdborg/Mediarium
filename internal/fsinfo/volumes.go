package fsinfo

import (
	"runtime"
	"strconv"
)

// SameVolumeKeys turns what is known about the filesystem holding a folder
// into a list of keys. Two folders sharing any key are on the same volume.
//
// idKeys are the identities the system gave (device number, filesystem id, or
// a drive letter). When bySpace is true the folder's space figures are added
// as one more key: paths on one volume get identical total and free space
// from the system, even when they are separate mounts or subvolumes with
// different device numbers (a Synology or Btrfs volume is the usual case).
func SameVolumeKeys(idKeys []string, u Usage, bySpace bool) []string {
	keys := append([]string(nil), idKeys...)
	if bySpace && u.TotalBytes > 0 {
		keys = append(keys, "space:"+strconv.FormatUint(u.TotalBytes, 10)+"/"+strconv.FormatUint(u.FreeBytes, 10))
	}
	return keys
}

// VolumeKeys returns the keys that identify the volume holding path (see
// SameVolumeKeys). On Windows the drive letter alone is used.
func VolumeKeys(path string, u Usage) []string {
	var ids []string
	if id, ok := FilesystemID(path); ok {
		ids = append(ids, id)
	}
	if id, ok := statfsID(path); ok {
		ids = append(ids, id)
	}
	return SameVolumeKeys(ids, u, runtime.GOOS != "windows")
}
