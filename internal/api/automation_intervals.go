package api

import (
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/settings"
)

// How often the two automatic loops run, and the limits Settings enforces.
const (
	defaultHuntHours = 6
	minHuntHours     = 1
	maxHuntHours     = 168 // a week

	defaultReleaseCheckMinutes = 15
	minReleaseCheckMinutes     = 5 // every check asks every indexer
	maxReleaseCheckMinutes     = 1440
)

// intSetting reads a whole-number setting; unset or unreadable gives def and
// the result is kept inside min..max.
func (s *Server) intSetting(key string, def, lo, hi int) int {
	v, _ := s.Settings.Get(key)
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return max(lo, min(n, hi))
}

// huntHours is how often the search for missing items and upgrades runs.
func (s *Server) huntHours() int {
	return s.intSetting(settings.KeyHuntIntervalHours, defaultHuntHours, minHuntHours, maxHuntHours)
}

// releaseCheckMinutes is how often the newest releases of every indexer
// are checked.
func (s *Server) releaseCheckMinutes() int {
	return s.intSetting(settings.KeyReleaseCheckMinutes, defaultReleaseCheckMinutes, minReleaseCheckMinutes, maxReleaseCheckMinutes)
}

func (s *Server) huntInterval() time.Duration {
	return time.Duration(s.huntHours()) * time.Hour
}

func (s *Server) releaseCheckInterval() time.Duration {
	return time.Duration(s.releaseCheckMinutes()) * time.Minute
}
