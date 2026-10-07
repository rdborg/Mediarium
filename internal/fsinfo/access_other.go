//go:build !unix

package fsinfo

// canWrite can't be asked without writing on this system; folders are
// taken as writable and a real write still reports a problem.
func canWrite(string) bool { return true }
