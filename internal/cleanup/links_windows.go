//go:build windows

package cleanup

import "io/fs"

// linkCount is how many hard links a file has. Windows does not report it
// through os.Stat, so every file counts as having one.
func linkCount(fs.FileInfo) uint64 { return 1 }
