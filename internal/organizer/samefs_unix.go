//go:build !windows

package organizer

import (
	"fmt"
	"syscall"
)

// SameFilesystem reports whether a and b live on the same filesystem/
// device — the actual precondition for os.Link to succeed (PRD.md §4.8/
// §5.2: "Wizard auto-detects whether downloads and library paths share a
// filesystem and warns if hardlinking won't work"). Compares the device
// ID from stat(2), which is how the kernel itself decides whether a link
// can be hardlinked (cross-device links fail with EXDEV — see
// internal/organizer/import.go's copy fallback, which is the runtime
// consequence of what this function predicts ahead of time).
func SameFilesystem(a, b string) (same bool, supported bool, err error) {
	var statA, statB syscall.Stat_t
	if err := syscall.Stat(a, &statA); err != nil {
		return false, true, fmt.Errorf("stat %s: %w", a, err)
	}
	if err := syscall.Stat(b, &statB); err != nil {
		return false, true, fmt.Errorf("stat %s: %w", b, err)
	}
	return statA.Dev == statB.Dev, true, nil
}
