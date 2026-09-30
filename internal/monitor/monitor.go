// Package monitor keeps an eye on the things Mediarium depends on (Usenet
// servers, indexers) by checking each one on a schedule, remembering the last
// result, and raising a notification only when something changes state, so a
// server that stays down is reported once, not every cycle.
//
// The package knows nothing about how a check is made: callers hand it a
// function that lists the current checks (internal/api wires in the real
// Usenet login and indexer probes), a clock and an interval, all injectable so
// the logic is testable without waiting.
package monitor

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind is what sort of thing a check covers.
type Kind string

const (
	KindUsenet  Kind = "usenet"
	KindIndexer Kind = "indexer"
)

// Check is one thing to test. Run returns nil when it is healthy.
type Check struct {
	Kind Kind
	ID   int64
	Name string
	Run  func(ctx context.Context) error
}

// Status is the last known result for one check.
type Status struct {
	Kind      Kind      `json:"kind"`
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	OK        bool      `json:"ok"`
	CheckedAt time.Time `json:"checkedAt"`
	Error     string    `json:"error"`
	Hint      string    `json:"hint"`
}

// Event is what the monitor asks to have announced: a check that started
// failing, or one that recovered.
type Event struct {
	Title   string
	Message string
	Failing bool // false means "recovered"

	Kind  Kind // what sort of thing it is, and its name
	Name  string
	Error string // what the failed check said, with credentials masked (empty when recovered)
}

// Monitor runs checks and tracks their state. Fill in the function fields, then
// call Run (background loop) or RunOnce (a single pass).
type Monitor struct {
	// Checks lists what to test right now (the enabled servers and indexers).
	Checks func(ctx context.Context) ([]Check, error)
	// Interval is read before every wait, so a settings change takes effect on
	// the next cycle. Zero or less means the monitor is off.
	Interval func() time.Duration
	// Notify announces a state change. May be nil.
	Notify func(Event)

	// Now is the clock (default time.Now).
	Now func() time.Time
	// After returns a channel that fires after d (default time.After); tests
	// replace it to drive the loop without sleeping.
	After func(d time.Duration) <-chan time.Time
	// InitialDelay is how long the loop waits before its first pass, giving the
	// network time to come up after a restart. Default one minute.
	InitialDelay time.Duration
	// OffPoll is how often an "off" monitor re-reads Interval to notice it was
	// switched on. Default one minute.
	OffPoll time.Duration

	runMu sync.Mutex // one pass at a time

	mu       sync.Mutex
	statuses map[string]*Status
}

func key(k Kind, id int64) string { return fmt.Sprintf("%s:%d", k, id) }

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Monitor) after(d time.Duration) <-chan time.Time {
	if m.After != nil {
		return m.After(d)
	}
	return time.After(d)
}

// Run loops until ctx is done: wait, run a pass, repeat. With the interval at
// zero it runs nothing, just keeps watching for the setting to change.
func (m *Monitor) Run(ctx context.Context) {
	wait := m.InitialDelay
	if wait <= 0 {
		wait = time.Minute
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.after(wait):
		}
		iv := time.Duration(0)
		if m.Interval != nil {
			iv = m.Interval()
		}
		if iv <= 0 {
			wait = m.OffPoll
			if wait <= 0 {
				wait = time.Minute
			}
			continue
		}
		m.RunOnce(ctx)
		wait = iv
	}
}

// RunOnce checks everything now, updates the stored results and announces any
// state changes. Concurrent calls queue up rather than overlap.
func (m *Monitor) RunOnce(ctx context.Context) {
	m.runMu.Lock()
	defer m.runMu.Unlock()

	checks, err := m.Checks(ctx)
	if err != nil {
		log.Printf("monitor: list checks: %v", err)
		return
	}

	// Check in parallel: a dead server can take its whole timeout to answer and
	// must not hold up the others.
	results := make([]error, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					results[i] = fmt.Errorf("check crashed: %v", r)
				}
			}()
			results[i] = c.Run(ctx)
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return // shutting down: the results are not real
	}

	checkedAt := m.now()
	var events []Event
	m.mu.Lock()
	seen := make(map[string]bool, len(checks))
	if m.statuses == nil {
		m.statuses = map[string]*Status{}
	}
	for i, c := range checks {
		k := key(c.Kind, c.ID)
		seen[k] = true
		prev, known := m.statuses[k]
		st := &Status{Kind: c.Kind, ID: c.ID, Name: c.Name, OK: results[i] == nil, CheckedAt: checkedAt}
		if results[i] != nil {
			st.Error = Redact(results[i].Error())
			st.Hint = Hint(results[i])
		}
		m.statuses[k] = st

		label := describe(c)
		switch {
		case !st.OK && (!known || prev.OK):
			// ok -> failing. A first-ever result that is a failure counts too:
			// after a restart there is no earlier "ok" to compare with, and a
			// server that is down right now is worth hearing about once.
			events = append(events, Event{
				Title:   label + " is not working",
				Message: failureMessage(st),
				Failing: true,
				Kind:    c.Kind, Name: c.Name, Error: st.Error,
			})
		case st.OK && known && !prev.OK:
			events = append(events, Event{
				Title:   label + " is working again",
				Message: "The last check passed.",
				Kind:    c.Kind, Name: c.Name,
			})
		}
	}
	for k := range m.statuses {
		if !seen[k] { // deleted or disabled since last time
			delete(m.statuses, k)
		}
	}
	m.mu.Unlock()

	if m.Notify != nil {
		for _, ev := range events {
			m.Notify(ev)
		}
	}
}

func describe(c Check) string {
	noun := "Usenet server"
	if c.Kind == KindIndexer {
		noun = "Indexer"
	}
	return fmt.Sprintf("%s %q", noun, c.Name)
}

func failureMessage(st *Status) string {
	msg := st.Hint
	if st.Error != "" {
		msg += "\nDetails: " + st.Error
	}
	return msg
}

// Statuses returns the last result of every check, ordered by kind, then name.
func (m *Monitor) Statuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.statuses))
	for _, s := range m.statuses {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

const (
	hintAuth    = "The login was rejected. Your subscription may have expired or the password may have changed."
	hintQuota   = "A limit has been reached. Your quota may be used up or your subscription may have expired."
	hintConns   = "This login has too many connections open. Stop other programs using the account, or lower the connections here."
	hintNetwork = "Couldn't reach the server. Check your internet connection."
	hintServer  = "The provider's server returned an error. Try again later."
	hintOther   = "Something went wrong. Press the Test button in Settings for details."
)

// Hint turns a failure into a sentence a person can act on.
func Hint(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(msg, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("too many connections", "max connections", "connection limit"):
		return hintConns
	case has("quota", "limit reached", "limit exceeded", "bandwidth"):
		return hintQuota
	case has("401", "403", "unauthorized", "forbidden", "invalid login", "auth failed", "authentication failed",
		"authentication required", "login failed", "login rejected", "incorrect user credentials", "invalid api key",
		"invalid apikey", "wrong api key", "access denied", "not authorized", "nntp 481", "nntp 482", "nntp 502"):
		return hintAuth
	case has("timeout", "timed out", "deadline exceeded", "no such host", "connection refused", "connection reset",
		"unreachable", "network is down", "unexpected eof", ": eof", "dial ", "tls:", "certificate"):
		return hintNetwork
	case has(" 500", " 502", " 503", " 504", "status 5", "bad gateway", "service unavailable", "internal server error"):
		return hintServer
	}
	return hintOther
}

var secretParam = regexp.MustCompile(`(?i)\b(apikey|api_key|token|passwd|password)=[^&\s"']+`)

// Redact masks credential-looking query parameters in an error message.
func Redact(msg string) string {
	return secretParam.ReplaceAllString(msg, "$1=***")
}
