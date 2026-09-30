package monitor_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/monitor"
)

// fakeThing is a check whose health the test flips.
type fakeThing struct {
	kind monitor.Kind
	id   int64
	name string
	err  atomic.Value // error, or nil
}

func (f *fakeThing) set(err error) {
	if err == nil {
		f.err.Store(errors.New(""))
		return
	}
	f.err.Store(err)
}

func (f *fakeThing) check() monitor.Check {
	return monitor.Check{Kind: f.kind, ID: f.id, Name: f.name, Run: func(context.Context) error {
		if e, _ := f.err.Load().(error); e != nil && e.Error() != "" {
			return e
		}
		return nil
	}}
}

type recorder struct {
	mu     sync.Mutex
	events []monitor.Event
}

func (r *recorder) notify(e monitor.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) take() []monitor.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

func newMonitor(things *[]*fakeThing, rec *recorder, now func() time.Time) *monitor.Monitor {
	return &monitor.Monitor{
		Checks: func(context.Context) ([]monitor.Check, error) {
			var out []monitor.Check
			for _, th := range *things {
				out = append(out, th.check())
			}
			return out, nil
		},
		Interval: func() time.Duration { return 30 * time.Minute },
		Notify:   rec.notify,
		Now:      now,
	}
}

func TestNotifiesOnlyOnStateChanges(t *testing.T) {
	usenet := &fakeThing{kind: monitor.KindUsenet, id: 1, name: "Eweka"}
	indexer := &fakeThing{kind: monitor.KindIndexer, id: 7, name: "NZBgeek"}
	usenet.set(nil)
	indexer.set(nil)
	things := []*fakeThing{usenet, indexer}
	rec := &recorder{}
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := newMonitor(&things, rec, func() time.Time { return clock })
	ctx := context.Background()

	m.RunOnce(ctx)
	if ev := rec.take(); len(ev) != 0 {
		t.Fatalf("healthy from the start must be silent, got %+v", ev)
	}

	// ok -> failing: one notification, with the actionable hint.
	usenet.set(errors.New("Connected, but login failed: nntp 481: Authentication failed"))
	clock = clock.Add(30 * time.Minute)
	m.RunOnce(ctx)
	ev := rec.take()
	if len(ev) != 1 || !ev[0].Failing || !strings.Contains(ev[0].Title, "Eweka") ||
		!strings.Contains(ev[0].Message, "The login was rejected. Your subscription may have expired or the password may have changed.") {
		t.Fatalf("expected one failure notification, got %+v", ev)
	}

	// Still failing: no repeat, but the status is refreshed.
	clock = clock.Add(30 * time.Minute)
	m.RunOnce(ctx)
	m.RunOnce(ctx)
	if ev := rec.take(); len(ev) != 0 {
		t.Fatalf("a server that stays down must not re-notify, got %+v", ev)
	}
	var got monitor.Status
	for _, s := range m.Statuses() {
		if s.Kind == monitor.KindUsenet {
			got = s
		}
	}
	if got.OK || got.Error == "" || got.Hint == "" || !got.CheckedAt.Equal(clock) || got.Name != "Eweka" || got.ID != 1 {
		t.Fatalf("unexpected stored status: %+v", got)
	}

	// failing -> ok: one recovery notification.
	usenet.set(nil)
	m.RunOnce(ctx)
	ev = rec.take()
	if len(ev) != 1 || ev[0].Failing || !strings.Contains(ev[0].Title, "working again") {
		t.Fatalf("expected one recovery notification, got %+v", ev)
	}
	m.RunOnce(ctx)
	if ev := rec.take(); len(ev) != 0 {
		t.Fatalf("staying healthy is silent, got %+v", ev)
	}

	// The indexer failing is reported independently.
	indexer.set(errors.New("newznab request to NZBgeek: dial tcp: i/o timeout"))
	m.RunOnce(ctx)
	ev = rec.take()
	if len(ev) != 1 || !strings.Contains(ev[0].Title, "Indexer") || !strings.Contains(ev[0].Message, "Couldn't reach the server. Check your internet connection.") {
		t.Fatalf("unexpected indexer notification: %+v", ev)
	}
}

func TestFirstResultFailingIsReportedOnceAndRemovedItemsDropOut(t *testing.T) {
	broken := &fakeThing{kind: monitor.KindUsenet, id: 1, name: "Down"}
	broken.set(errors.New("dial tcp: connection refused"))
	things := []*fakeThing{broken}
	rec := &recorder{}
	m := newMonitor(&things, rec, time.Now)

	m.RunOnce(context.Background())
	m.RunOnce(context.Background())
	if ev := rec.take(); len(ev) != 1 {
		t.Fatalf("expected exactly one notification for a server that is down at the first look, got %+v", ev)
	}

	things = nil // deleted or disabled
	m.RunOnce(context.Background())
	if len(m.Statuses()) != 0 {
		t.Fatalf("a removed item must drop out of the status list: %+v", m.Statuses())
	}
}

func TestStatusesAreSorted(t *testing.T) {
	a := &fakeThing{kind: monitor.KindUsenet, id: 2, name: "B"}
	b := &fakeThing{kind: monitor.KindIndexer, id: 1, name: "Z"}
	c := &fakeThing{kind: monitor.KindUsenet, id: 3, name: "A"}
	things := []*fakeThing{a, b, c}
	m := newMonitor(&things, &recorder{}, time.Now)
	m.RunOnce(context.Background())
	var order []string
	for _, s := range m.Statuses() {
		order = append(order, string(s.Kind)+":"+s.Name)
	}
	if strings.Join(order, ",") != "indexer:Z,usenet:A,usenet:B" {
		t.Fatalf("unexpected order %v", order)
	}
}

func TestHintClassification(t *testing.T) {
	const (
		auth    = "The login was rejected. Your subscription may have expired or the password may have changed."
		network = "Couldn't reach the server. Check your internet connection."
	)
	cases := []struct {
		err  string
		want string
	}{
		{"Connected, but login failed: nntp 481: Authentication failed", auth},
		{"nntp 502: Access denied", auth},
		{"newznab request to X: unexpected status 401: Unauthorized", auth},
		{"newznab request to X: unexpected status 403: forbidden", auth},
		{"newznab error from X: Incorrect user credentials", auth},
		{"invalid login", auth},
		{"auth failed", auth},
		{"dial nntp news.example.com:563: dial tcp 1.2.3.4:563: i/o timeout", network},
		{"context deadline exceeded", network},
		{"dial tcp: lookup news.example.com: no such host", network},
		{"read nntp greeting: unexpected EOF", network},
		{"nntp 400: quota exceeded for this account", "quota"},
		{"nntp 502: Too many connections.", "too many connections open"},
		{"Your Usenet provider says there are too many connections on this login. If another program (like SABnzbd) uses the same account, stop it or lower the number of connections here.", "too many connections open"},
		{"Connected, but the login failed. The provider refused this username and password.", "The login was rejected"},
		{"newznab request to X: unexpected status 503: Service Unavailable", "returned an error"},
		{"the cat sat on the mat", "Test button"},
	}
	for _, tc := range cases {
		got := monitor.Hint(errors.New(tc.err))
		if !strings.Contains(got, tc.want) {
			t.Errorf("%q -> %q, want it to contain %q", tc.err, got, tc.want)
		}
	}
	if monitor.Hint(nil) != "" {
		t.Error("no error, no hint")
	}
}

func TestRedactHidesCredentials(t *testing.T) {
	got := monitor.Redact(`Get "https://idx.example/api?apikey=SECRET123&t=search&password=hunter2": boom`)
	if strings.Contains(got, "SECRET123") || strings.Contains(got, "hunter2") || !strings.Contains(got, "t=search") {
		t.Fatalf("unexpected redaction: %s", got)
	}
}

func TestRunLoopHonoursIntervalAndOffSwitch(t *testing.T) {
	thing := &fakeThing{kind: monitor.KindUsenet, id: 1, name: "S"}
	thing.set(nil)
	var passes atomic.Int32
	ran := make(chan struct{}, 10)
	asked := make(chan time.Duration, 10)
	fire := make(chan time.Time)
	var minutes atomic.Int64
	minutes.Store(30)

	m := &monitor.Monitor{
		Checks: func(context.Context) ([]monitor.Check, error) {
			passes.Add(1)
			ran <- struct{}{}
			return []monitor.Check{thing.check()}, nil
		},
		Interval:     func() time.Duration { return time.Duration(minutes.Load()) * time.Minute },
		After:        func(d time.Duration) <-chan time.Time { asked <- d; return fire },
		InitialDelay: 2 * time.Minute,
		OffPoll:      time.Minute,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()

	wait := func() time.Duration {
		t.Helper()
		select {
		case d := <-asked:
			return d
		case <-time.After(5 * time.Second):
			t.Fatal("the loop never asked to wait")
			return 0
		}
	}
	waitRan := func() {
		t.Helper()
		select {
		case <-ran:
		case <-time.After(5 * time.Second):
			t.Fatal("the pass never ran")
		}
	}

	if d := wait(); d != 2*time.Minute {
		t.Fatalf("first wait should be the initial delay, got %v", d)
	}
	fire <- time.Now()
	waitRan()
	if d := wait(); d != 30*time.Minute {
		t.Fatalf("after a pass the loop should wait the configured interval, got %v", d)
	}

	// Switched off (0): the loop still wakes, but runs nothing.
	minutes.Store(0)
	fire <- time.Now()
	if d := wait(); d != time.Minute {
		t.Fatalf("an off monitor should poll its setting every minute, got %v", d)
	}
	if passes.Load() != 1 {
		t.Fatalf("no pass may run while off, got %d", passes.Load())
	}

	// Switched back on, with a new interval picked up on the next cycle.
	minutes.Store(5)
	fire <- time.Now()
	waitRan()
	if d := wait(); d != 5*time.Minute {
		t.Fatalf("expected the new 5 minute interval, got %v", d)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run should return once its context is cancelled")
	}
}

func TestEventsSayWhatFailed(t *testing.T) {
	var events []monitor.Event
	m := &monitor.Monitor{
		Checks: func(context.Context) ([]monitor.Check, error) {
			return []monitor.Check{
				{Kind: monitor.KindIndexer, ID: 1, Name: "NZBgeek", Run: func(context.Context) error { return errors.New("HTTP 429 apikey=abc123") }},
			}, nil
		},
		Interval: func() time.Duration { return time.Minute },
		Notify:   func(ev monitor.Event) { events = append(events, ev) },
	}
	m.RunOnce(context.Background())
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	ev := events[0]
	if !ev.Failing || ev.Kind != monitor.KindIndexer || ev.Name != "NZBgeek" || !strings.Contains(ev.Error, "429") || strings.Contains(ev.Error, "abc123") {
		t.Errorf("event = %+v", ev)
	}
}
