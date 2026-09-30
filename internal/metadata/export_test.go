package metadata

import "time"

// SetClock fixes the date "upcoming" lists count from, for tests.
func (c *Client) SetClock(now func() time.Time) { c.now = now }
