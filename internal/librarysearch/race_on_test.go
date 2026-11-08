//go:build race

package librarysearch_test

import "time"

// The race detector slows everything down many times over, and CI runners are
// shared, so the speed check only catches a search that is really broken.
const slowSearch = 2 * time.Second
