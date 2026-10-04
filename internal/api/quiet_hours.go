package api

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/settings"
)

// Quiet hours: between two hours (say 23:00 and 7:00) everyday messages
// ("downloading", "ready to watch", subtitles, a new version) wait and go out
// together when the quiet hours end. Problems (a failed download, a broken
// connection, a decision to make) are sent straight away. Waiting messages
// are kept in memory: a restart during quiet hours drops them.

const maxHeldNotices = 200

type heldNotices struct {
	mu     sync.Mutex
	events []notify.Event
}

// quietHours returns the quiet window, ok false when there is none.
func (s *Server) quietHours() (from, to int, ok bool) {
	v, _ := s.Settings.Get(settings.KeyNotifyQuietHours)
	return parseHours(v)
}

// urgentEvent reports whether a message is about a problem, which quiet
// hours never hold back.
func urgentEvent(ev notify.Event) bool {
	switch notify.EventKind(ev.Type) {
	case notify.EventFailed, notify.EventHealth, notify.EventConflict:
		return true
	}
	return ev.Type == "test"
}

// holdForQuietHours keeps ev back when it is quiet time now, and reports
// whether it did.
func (s *Server) holdForQuietHours(ev notify.Event, now time.Time) bool {
	from, to, ok := s.quietHours()
	if !ok || urgentEvent(ev) || !inHours(now.Hour(), from, to) {
		return false
	}
	s.held.mu.Lock()
	defer s.held.mu.Unlock()
	if len(s.held.events) < maxHeldNotices {
		s.held.events = append(s.held.events, ev)
	}
	return true
}

// quietHoursJob sends what waited, once the quiet hours are over.
func (s *Server) quietHoursJob(context.Context) {
	if from, to, ok := s.quietHours(); ok && inHours(time.Now().Hour(), from, to) {
		return
	}
	s.held.mu.Lock()
	events := s.held.events
	s.held.events = nil
	s.held.mu.Unlock()
	for _, ev := range events {
		s.deliverNotification(ev)
	}
}

// heldCount is how many messages are waiting for the quiet hours to end.
func (s *Server) heldCount() int {
	s.held.mu.Lock()
	defer s.held.mu.Unlock()
	return len(s.held.events)
}

func quietHoursText(from, to int) string {
	return strconv.Itoa(from) + "-" + strconv.Itoa(to)
}
