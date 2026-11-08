//go:build !linux

package fsinfo

// NetworkFilesystem says whether path is on a network share. Only Linux can
// tell; elsewhere it answers false.
func NetworkFilesystem(string) (kind string, ok bool) { return "", false }
