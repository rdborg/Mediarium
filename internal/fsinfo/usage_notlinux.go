//go:build !linux && !windows

package fsinfo

// statfsID is only read on Linux; elsewhere the device number and the space
// figures are used to tell volumes apart.
func statfsID(path string) (id string, ok bool) { return "", false }
