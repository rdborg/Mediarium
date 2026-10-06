package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/speed"
)

// Two simple guards for downloads: a speed limit (optionally only for some
// hours of the day), and a free-space floor below which nothing new starts.

const (
	defaultMinFreeGB = 5
	speedCheck       = 5 * time.Minute
)

// speedLimitMB is the limit in MB/s, 0 for none.
func (s *Server) speedLimitMB() int {
	v, _ := s.Settings.Get(settings.KeySpeedLimitMB)
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// speedLimitHours is when the limit applies: from and to are hours (0-23),
// and ok is false for "all day".
func (s *Server) speedLimitHours() (from, to int, ok bool) {
	v, _ := s.Settings.Get(settings.KeySpeedLimitHours)
	return parseHours(v)
}

// parseHours reads "8-23". Anything else means all day.
func parseHours(v string) (from, to int, ok bool) {
	a, b, found := strings.Cut(strings.TrimSpace(v), "-")
	if !found {
		return 0, 0, false
	}
	f, err1 := strconv.Atoi(a)
	t, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || f < 0 || f > 23 || t < 0 || t > 23 || f == t {
		return 0, 0, false
	}
	return f, t, true
}

// inHours reports whether hour h falls in [from, to), going past midnight
// when from is later than to (22-6 is the night).
func inHours(h, from, to int) bool {
	if from < to {
		return h >= from && h < to
	}
	return h >= from || h < to
}

// downloadHours is the window new downloads may start in ("1-7"), if any.
func (s *Server) downloadHours() (from, to int, ok bool) {
	v, _ := s.Settings.Get(settings.KeyDownloadHours)
	return parseHours(v)
}

// outsideDownloadHours reports whether new downloads have to wait for their
// window to open. Downloads already running carry on.
func (s *Server) outsideDownloadHours() bool {
	from, to, ok := s.downloadHours()
	return ok && !inHours(time.Now().Hour(), from, to)
}

// applySpeedLimit sets the shared limit for this moment.
func (s *Server) applySpeedLimit(now time.Time) {
	mb := s.speedLimitMB()
	if from, to, ok := s.speedLimitHours(); ok && !inHours(now.Hour(), from, to) {
		mb = 0
	}
	speed.Set(int64(mb) << 20)
}

// speedJob keeps the limit in step with the clock.
func (s *Server) speedJob(context.Context) { s.applySpeedLimit(time.Now()) }

// minFreeGB is the free-space floor in GB, 0 for none.
func (s *Server) minFreeGB() int {
	v, _ := s.Settings.Get(settings.KeyMinFreeGB)
	if strings.TrimSpace(v) == "" {
		return defaultMinFreeGB
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return defaultMinFreeGB
	}
	return n
}

// diskTooFull reports whether the downloads folder has less free space than
// the floor, in which case the download line starts nothing new. It records
// the reason in Logs and errors (repeats fold into one line).
func (s *Server) diskTooFull() bool {
	floor := s.minFreeGB()
	if floor == 0 {
		return false
	}
	dir := s.downloadsRoot()
	if dir == "" {
		return false
	}
	u, err := fsinfo.DiskUsage(dir)
	if err != nil || u.TotalBytes == 0 {
		return false // cannot tell: never block on a guess
	}
	if u.FreeBytes >= uint64(floor)<<30 {
		return false
	}
	problems.Record(problems.Problem{
		Code:    problems.CodeDiskLow,
		Subject: dir,
		Message: fmt.Sprintf("Downloads are waiting: only %s free in the downloads folder, less than the %d GB you asked to keep.", humanBytes(int64(u.FreeBytes)), floor),
		Quiet:   true,
	})
	return true
}
