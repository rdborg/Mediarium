//go:build !race

package librarysearch_test

import "time"

// The longest a search over the big library may take in a normal run.
const slowSearch = 150 * time.Millisecond
