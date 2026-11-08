package api

import "time"

// SetEventLimitsForTest shrinks the number of live streams one account and one
// address may keep open and the time between keep-alive comments, and returns
// the function that puts the real ones back.
func SetEventLimitsForTest(perUser, perIP int, keepAlive time.Duration) (restore func()) {
	eventLimitsMu.Lock()
	old := eventLimits
	eventLimits.perUser, eventLimits.perIP, eventLimits.keepAlive = perUser, perIP, keepAlive
	eventLimitsMu.Unlock()
	return func() {
		eventLimitsMu.Lock()
		eventLimits = old
		eventLimitsMu.Unlock()
	}
}

// SetReadDeadlineForTest shortens the time a page may take to load, and
// returns the function that puts the real one back.
func SetReadDeadlineForTest(d time.Duration) (restore func()) {
	old := readDeadline
	readDeadline = d
	return func() { readDeadline = old }
}
