//go:build unix

package fsinfo

import "syscall"

// canWrite asks the system whether this user may create files in dir,
// without creating one.
func canWrite(dir string) bool {
	return syscall.Access(dir, 0x2) == nil // W_OK
}
