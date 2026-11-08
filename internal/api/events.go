package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
)

// The live event stream (GET /api/events). A browser keeps one of these open
// per browser, not per tab (web/src/liveEvents.ts shares it between tabs),
// because a browser talks to one address over at most six connections at a
// time on HTTP/1.1, and every open stream uses one of them for as long as the
// page is open.
//
// The server makes sure a stream cannot pile up either: a person has a few
// slots, the rest are turned away with 429 and Retry-After, a slot is given
// back the moment its connection ends, and a comment goes down every so often
// so that a connection that went away without saying so is noticed.

const (
	// eventsPerUser is how many streams one account may keep open.
	eventsPerUser = 8
	// eventsPerIP is how many streams one address may keep open, counting every
	// account behind it (a family behind one router).
	eventsPerIP = 24
	// eventsKeepAlive is how often a comment is sent when nothing else is.
	eventsKeepAlive = 20 * time.Second
	// eventsWriteTime is how long one write to a client may take.
	eventsWriteTime = 10 * time.Second
	// eventsBuffer is how many events wait for one slow client before it is
	// told to load everything again.
	eventsBuffer = 32
	// eventsRetryAfter is what the answer to a turned-away stream tells the
	// client to wait, in seconds.
	eventsRetryAfter = 15
)

// eventLimits are the caps and timings, as variables so a test can shrink them.
var eventLimits = struct {
	perUser, perIP int
	keepAlive      time.Duration
}{eventsPerUser, eventsPerIP, eventsKeepAlive}

// liveEvent is one thing sent to the open streams.
type liveEvent struct {
	Name string // the SSE event name: "activity", "resync"
	Data string // the data line, JSON
}

// eventSub is one open stream.
type eventSub struct {
	ch      chan liveEvent
	user    string
	ip      string
	dropped bool // an event did not fit: the client is told to reload
}

// eventHub holds the open streams.
type eventHub struct {
	mu     sync.Mutex
	subs   map[*eventSub]struct{}
	byUser map[string]int
	byIP   map[string]int
	closed bool
}

// join gives a stream a slot, or reports that there is none left.
func (h *eventHub) join(user, ip string) (*eventSub, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.byUser[user] >= eventLimits.perUser || h.byIP[ip] >= eventLimits.perIP {
		return nil, false
	}
	if h.subs == nil {
		h.subs, h.byUser, h.byIP = map[*eventSub]struct{}{}, map[string]int{}, map[string]int{}
	}
	sub := &eventSub{ch: make(chan liveEvent, eventsBuffer), user: user, ip: ip}
	h.subs[sub] = struct{}{}
	h.byUser[user]++
	h.byIP[ip]++
	return sub, true
}

// leave gives the slot back. It is safe to call twice.
func (h *eventHub) leave(sub *eventSub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[sub]; !ok {
		return
	}
	delete(h.subs, sub)
	if h.byUser[sub.user]--; h.byUser[sub.user] <= 0 {
		delete(h.byUser, sub.user)
	}
	if h.byIP[sub.ip]--; h.byIP[sub.ip] <= 0 {
		delete(h.byIP, sub.ip)
	}
}

// open is how many streams are open right now.
func (h *eventHub) open() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// publish sends an event to every open stream without waiting for any of
// them: a stream that cannot keep up is marked and told to load again.
func (h *eventHub) publish(name, data string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		select {
		case sub.ch <- liveEvent{name, data}:
		default:
			sub.dropped = true
		}
	}
}

// takeDropped reports, once, that events were lost for sub.
func (h *eventHub) takeDropped(sub *eventSub) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := sub.dropped
	sub.dropped = false
	return d
}

// shutdown refuses new streams and ends the open ones (the handlers see
// their channel closed), so stopping the app does not wait for them.
func (h *eventHub) shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for sub := range h.subs {
		close(sub.ch)
		delete(h.subs, sub)
	}
	h.byUser, h.byIP = map[string]int{}, map[string]int{}
}

// CloseEventStreams ends every open live stream. The server calls it when it
// starts to stop.
func (s *Server) CloseEventStreams() { s.events.shutdown() }

// publishEvent tells the open browsers that something changed. The data is
// only ever a short kind of change, never a title, a path or a key.
func (s *Server) publishEvent(name string, fields map[string]string) {
	data := "{}"
	if len(fields) > 0 {
		if b, err := json.Marshal(fields); err == nil {
			data = string(b)
		}
	}
	s.events.publish(name, data)
}

// handleEvents serves the live stream: "hello" first, then an event for each
// change, and a comment line every few seconds while it is quiet.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	user := "anon"
	if u := auth.UserFromContext(r.Context()); u != nil {
		user = strconv.FormatInt(u.ID, 10)
	}
	ip := s.clientIP(r)
	sub, ok := s.events.join(user, ip)
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(eventsRetryAfter))
		writeError(w, http.StatusTooManyRequests, "Too many live connections are open. Close other Mediarium tabs or windows, or wait a moment and try again.")
		return
	}
	defer s.events.leave(sub)

	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no") // a reverse proxy must not hold the events back
	w.WriteHeader(http.StatusOK)

	send := func(format string, args ...any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(eventsWriteTime))
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send("retry: 5000\nevent: hello\ndata: {}\n\n") {
		return
	}
	tick := time.NewTicker(eventLimits.keepAlive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-sub.ch:
			if !open {
				return
			}
			if s.events.takeDropped(sub) && !send("event: resync\ndata: {}\n\n") {
				return
			}
			if !send("event: %s\ndata: %s\n\n", ev.Name, ev.Data) {
				return
			}
		case <-tick.C:
			if s.events.takeDropped(sub) {
				if !send("event: resync\ndata: {}\n\n") {
					return
				}
			} else if !send(": keep-alive\n\n") {
				slog.Debug("live stream: the client is gone", "user", user)
				return
			}
		}
	}
}
