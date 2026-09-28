//go:build windows

package organizer

// SameFilesystem has no Windows implementation — Mediarium's actual
// deployment target is Linux/Docker (PRD.md §3), so this only matters for
// someone running the Go binary natively on Windows for local dev.
// supported=false tells the caller to skip the warning rather than
// guessing (see samefs_unix.go for the real, Linux implementation the
// container build actually uses).
func SameFilesystem(a, b string) (same bool, supported bool, err error) {
	return false, false, nil
}
