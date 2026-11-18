package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/speed"
)

// Two simple guards for downloads: a speed limit (optionally only for some
// hours of the day), and a free-space floor below which nothing new starts.

const (
	defaultMinFreeGB = 5
	// speedCheck is how often the speed schedule is looked at. The rules
	// change on the hour, so a check every minute applies them within a
	// minute; the check itself is cheap.
	speedCheck = time.Minute
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

// speedRule is one line of the speed schedule: on these days (0 is Sunday;
// none listed means every day), from this hour to that one (the same hour twice
// is the whole day), downloads are limited to LimitMB MB/s (0 = no limit).
type speedRule struct {
	Days    []int `json:"days"`
	From    int   `json:"from"`
	To      int   `json:"to"`
	LimitMB int   `json:"limitMB"`
}

const maxSpeedRules = 14

// active says whether the rule covers the moment wd (weekday) and h (hour). A
// rule that runs past midnight (22 to 6) belongs to the day it starts on, so
// "Friday 22-6" still covers early Saturday.
func (r speedRule) active(wd, h int) bool {
	onDay := func(d int) bool {
		if len(r.Days) == 0 {
			return true
		}
		for _, x := range r.Days {
			if x == d {
				return true
			}
		}
		return false
	}
	switch {
	case r.From == r.To:
		return onDay(wd)
	case r.From < r.To:
		return onDay(wd) && h >= r.From && h < r.To
	default:
		return (onDay(wd) && h >= r.From) || (onDay((wd+6)%7) && h < r.To)
	}
}

func (r speedRule) valid() bool {
	if r.From < 0 || r.From > 23 || r.To < 0 || r.To > 23 || r.LimitMB < 0 || r.LimitMB > 10000 {
		return false
	}
	for _, d := range r.Days {
		if d < 0 || d > 6 {
			return false
		}
	}
	return true
}

// speedSchedule is the saved list of speed rules.
func (s *Server) speedSchedule() []speedRule {
	raw, _ := s.Settings.Get(settings.KeySpeedSchedule)
	var rules []speedRule
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &rules)
	}
	out := rules[:0]
	for _, r := range rules {
		if r.valid() {
			out = append(out, r)
		}
	}
	return out
}

// scheduledLimit returns the limit the first matching rule sets.
func scheduledLimit(rules []speedRule, now time.Time) (mb int, ok bool) {
	for _, r := range rules {
		if r.active(int(now.Weekday()), now.Hour()) {
			return r.LimitMB, true
		}
	}
	return 0, false
}

// applySpeedLimit sets the download limit (Usenet and torrents) and the torrent
// upload limit for this moment; both follow the same hours. A line of the speed
// schedule that fits the moment sets the download limit instead.
func (s *Server) applySpeedLimit(now time.Time) {
	mb := s.speedLimitMB()
	upKB := s.torrentUploadLimitKB()
	if from, to, ok := s.speedLimitHours(); ok && !inHours(now.Hour(), from, to) {
		mb, upKB = 0, noUploadLimit
	}
	if scheduled, ok := scheduledLimit(s.speedSchedule(), now); ok {
		mb = scheduled
	}
	speed.Set(int64(mb) << 20)
	// 0 is not a limit: nothing is uploaded at all.
	speed.SetUploadOff(upKB == 0)
	speed.SetUpload(int64(max(upKB, 0)) << 10)
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

// releaseSpaceReserve is kept free on top of what a release needs.
const releaseSpaceReserve = 256 << 20

// checkRoomForRelease refuses a release that cannot fit in dir: what it still
// has to fetch (need, less what an earlier try already saved in dir) plus a
// small reserve against the disk's free space. When the free space cannot be
// read nothing is refused, as a guess must never stop a download.
func checkRoomForRelease(dir string, need int64) error {
	if need <= 0 {
		return nil
	}
	u, err := fsinfo.DiskUsage(dir)
	if err != nil || u.TotalBytes == 0 {
		return nil
	}
	left := need - cleanup.Size(dir)
	if left <= 0 || uint64(left)+releaseSpaceReserve <= u.FreeBytes {
		return nil
	}
	return fmt.Errorf("there is not enough free disk space in the downloads folder for this release: it needs %s and %s is free", humanBytes(left), humanBytes(int64(u.FreeBytes)))
}

// diskTooFull reports whether the downloads folder has less free space than
// the floor, in which case the download line starts nothing new. It records
// the reason in Logs and errors (repeats fold into one line).
func (s *Server) diskTooFull() bool {
	dir, message, short := s.diskSpaceShort()
	if !short {
		return false
	}
	problems.Record(problems.Problem{
		Code:    problems.CodeDiskLow,
		Subject: dir,
		Message: message,
		Quiet:   true,
	})
	return true
}

// diskSpaceShort reports whether the downloads folder has less free space than
// the floor, with the folder and the sentence that says so. It records
// nothing, so the Activity page can ask it as often as it likes.
func (s *Server) diskSpaceShort() (dir, message string, short bool) {
	floor := s.minFreeGB()
	if floor == 0 {
		return "", "", false
	}
	// New downloads are worked on in the temporary download folder when there is
	// one, so that is the disk that fills up.
	dir, where := s.downloadsRoot(), false
	if b := s.tempBase(); b != "" {
		dir, where = b, true
	}
	if dir == "" {
		return "", "", false
	}
	u, err := fsinfo.DiskUsage(dir)
	if err != nil || u.TotalBytes == 0 {
		return "", "", false // cannot tell: never block on a guess
	}
	if u.FreeBytes >= uint64(floor)<<30 {
		return "", "", false
	}
	if where {
		return dir, fmt.Sprintf("Downloads are waiting: only %s free in the temporary download folder, less than the %d GB you asked to keep.", humanBytes(int64(u.FreeBytes)), floor), true
	}
	return dir, fmt.Sprintf("Downloads are waiting: only %s free in the downloads folder, less than the %d GB you asked to keep.", humanBytes(int64(u.FreeBytes)), floor), true
}
