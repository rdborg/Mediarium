package api

import "time"

// SetReadDeadlineForTest shortens the time a page may take to load, and
// returns the function that puts the real one back.
func SetReadDeadlineForTest(d time.Duration) (restore func()) {
	old := readDeadline
	readDeadline = d
	return func() { readDeadline = old }
}
