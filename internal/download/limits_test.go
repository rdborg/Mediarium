package download

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// limitedNNTP is a fake news server with a connection limit, like a real
// provider's: past the limit it answers "502 Too many connections." and hangs
// up. It counts what the downloader does to it.
type limitedNNTP struct {
	addr string
	port int

	max       int           // connections allowed at once; 0 means no limit
	refuseNth map[int]bool  // 1-based numbers of accepted connections to refuse once
	message   string        // the refusal, default "502 Too many connections."
	atGreet   bool          // refuse in the greeting instead of at sign-in
	bodyDelay time.Duration // how long each article takes
	hang      bool          // never answer BODY
	refuseAll bool          // refuse every connection

	mu        sync.Mutex
	articles  map[string][]byte
	accepted  int
	refused   int
	active    int
	peak      int
	working   int // connections that were let in
	workPeak  int
	activeNow atomic.Int32
}

func newLimitedNNTP(t *testing.T, s *limitedNNTP, articles map[string][]byte) *limitedNNTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.articles = articles
	if s.message == "" {
		s.message = "502 Too many connections."
	}
	s.addr = "127.0.0.1"
	s.port = ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return s
}

// enter counts a new connection and says whether it may stay.
func (s *limitedNNTP) enter() (ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accepted++
	s.active++
	s.activeNow.Store(int32(s.active))
	if s.active > s.peak {
		s.peak = s.active
	}
	full := s.max > 0 && s.working >= s.max
	if full || s.refuseAll || s.refuseNth[s.accepted] {
		s.refused++
		return false
	}
	s.working++
	if s.working > s.workPeak {
		s.workPeak = s.working
	}
	return true
}

func (s *limitedNNTP) leave(wasAllowed bool) {
	s.mu.Lock()
	s.active--
	if wasAllowed {
		s.working--
	}
	s.activeNow.Store(int32(s.active))
	s.mu.Unlock()
}

func (s *limitedNNTP) stats() (accepted, refused, peak int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepted, s.refused, s.peak
}

func (s *limitedNNTP) workingPeak() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workPeak
}

// waitOrClosed waits d, or less if the client hangs up. It reports whether
// the connection is still there.
func waitOrClosed(conn net.Conn, rw *bufio.ReadWriter, d time.Duration) bool {
	conn.SetReadDeadline(time.Now().Add(d))
	_, err := rw.Reader.Peek(1)
	conn.SetReadDeadline(time.Time{})
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func (s *limitedNNTP) handle(conn net.Conn) {
	defer conn.Close()
	allowed := s.enter()
	defer s.leave(allowed)
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	if !allowed && s.atGreet {
		rw.WriteString(s.message + "\r\n")
		rw.Flush()
		return
	}
	rw.WriteString("200 fake nntp ready\r\n")
	rw.Flush()
	for {
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "AUTHINFO USER"):
			rw.WriteString("381 password required\r\n")
		case strings.HasPrefix(upper, "AUTHINFO PASS"):
			if !allowed {
				rw.WriteString(s.message + "\r\n")
				rw.Flush()
				return
			}
			rw.WriteString("281 ok\r\n")
		case strings.HasPrefix(upper, "GROUP"):
			rw.WriteString("211 0 0 0 group\r\n")
		case strings.HasPrefix(upper, "BODY"):
			if s.hang && !waitOrClosed(conn, rw, time.Hour) {
				return
			}
			if s.bodyDelay > 0 && !waitOrClosed(conn, rw, s.bodyDelay) {
				return
			}
			id := strings.Trim(strings.TrimSpace(line[len("BODY"):]), "<>")
			body, ok := s.articles[id]
			if !ok {
				rw.WriteString("430 no such article\r\n")
				break
			}
			rw.WriteString("222 body follows\r\n")
			writeDotTerminated(rw, body)
		case upper == "QUIT":
			rw.WriteString("205 bye\r\n")
			rw.Flush()
			return
		default:
			rw.WriteString("500 what?\r\n")
		}
		rw.Flush()
	}
}

// waitNoConnections fails the test if the server still has connections open
// after a moment.
func (s *limitedNNTP) waitNoConnections(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.activeNow.Load() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%d connections were left open", s.activeNow.Load())
}

// bigNZB is one file of n segments of 100 bytes, and the articles that serve it.
func bigNZB(n int) (*NZB, map[string][]byte, []byte) {
	full := bytes.Repeat([]byte("0123456789abcdefghijklmnopqrstuvwxyz"), 100*n/36+1)[:100*n]
	articles := map[string][]byte{}
	f := NZBFile{Subject: `[1/1] "big.mkv" yEnc (1/` + strconv.Itoa(n) + `)`, Groups: []string{"alt.binaries.test"}}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("seg%d@example", i)
		articles[id] = buildMultipartYenc("big.mkv", full, i*100+1, (i+1)*100)
		f.Segments = append(f.Segments, NZBSegment{Number: i + 1, Bytes: 100, MessageID: id})
	}
	return &NZB{Files: []NZBFile{f}}, articles, full
}

// fastGate shortens every wait so the tests run in moments, and forgets what
// earlier tests taught the shared gates.
func fastGate(t *testing.T) {
	t.Helper()
	base, maxWait, giveUp, quiet, settle, io := tooManyBackoffBase, tooManyBackoffMax, tooManyGiveUp, tooManyQuiet, tooManySettle, nntpIOTimeout
	tooManyBackoffBase, tooManyBackoffMax, tooManyGiveUp, tooManyQuiet, tooManySettle = 10*time.Millisecond, 40*time.Millisecond, 400*time.Millisecond, time.Second, 50*time.Millisecond
	t.Cleanup(func() {
		tooManyBackoffBase, tooManyBackoffMax, tooManyGiveUp, tooManyQuiet, tooManySettle, nntpIOTimeout = base, maxWait, giveUp, quiet, settle, io
		gatesMu.Lock()
		gates = map[gateKey]*serverGate{}
		gatesMu.Unlock()
	})
}

func downloadTo(t *testing.T, ctx context.Context, cfgs []ClientConfig, nzb *NZB) (Result, error, string) {
	t.Helper()
	dir := t.TempDir()
	res, err := DownloadFromServers(ctx, cfgs, nzb, dir, nil)
	return res, err, dir
}

func mustHold(t *testing.T, res Result, want []byte) {
	t.Helper()
	if len(res.Paths) != 1 {
		t.Fatalf("paths = %v", res.Paths)
	}
	got, err := os.ReadFile(res.Paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("downloaded file is not the original (%d bytes, want %d)", len(got), len(want))
	}
	if res.MissingSegments != 0 {
		t.Fatalf("%d segments missing", res.MissingSegments)
	}
}

func TestProviderLimitLowersTheConnectionsAndTheDownloadCarriesOn(t *testing.T) {
	fastGate(t)
	for _, atGreet := range []bool{false, true} {
		t.Run(fmt.Sprintf("refused in the greeting=%v", atGreet), func(t *testing.T) {
			nzb, articles, full := bigNZB(60)
			srv := newLimitedNNTP(t, &limitedNNTP{max: 3, atGreet: atGreet, bodyDelay: 5 * time.Millisecond}, articles)
			cfg := ClientConfig{Host: srv.addr, Port: srv.port, Username: fmt.Sprint("limit-", atGreet), Password: "x", Connections: 8}

			res, err, _ := downloadTo(t, context.Background(), []ClientConfig{cfg}, nzb)
			if err != nil {
				t.Fatalf("the download failed although only some connections were refused: %v", err)
			}
			mustHold(t, res, full)
			_, refused, _ := srv.stats()
			if refused == 0 {
				t.Fatal("the fake server never refused anything, so the test proves nothing")
			}
			if peak := srv.workingPeak(); peak > 3 {
				t.Errorf("%d connections were working at once, the provider allows 3", peak)
			}
			states := ServerStates()
			var limit int
			for _, s := range states {
				if s.Host == srv.addr+":"+strconv.Itoa(srv.port) {
					limit = s.Limit
				}
			}
			if limit != 3 {
				t.Errorf("the connection limit was lowered to %d, want the 3 the provider allows", limit)
			}
			srv.waitNoConnections(t)
		})
	}
}

func TestOneRefusedConnectionIsRetriedNotFatal(t *testing.T) {
	fastGate(t)
	nzb, articles, full := bigNZB(30)
	srv := newLimitedNNTP(t, &limitedNNTP{refuseNth: map[int]bool{2: true, 3: true}, bodyDelay: 2 * time.Millisecond}, articles)
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Username: "nth", Password: "x", Connections: 4}
	res, err, _ := downloadTo(t, context.Background(), []ClientConfig{cfg}, nzb)
	if err != nil {
		t.Fatalf("a refused connection failed the whole download: %v", err)
	}
	mustHold(t, res, full)
	if _, refused, _ := srv.stats(); refused != 2 {
		t.Errorf("refused %d, want the 2 the server was set to refuse", refused)
	}
	srv.waitNoConnections(t)
}

func TestGivesUpAfterAWhileWithAMessageAPersonCanActOn(t *testing.T) {
	fastGate(t)
	nzb, articles, _ := bigNZB(10)
	srv := newLimitedNNTP(t, &limitedNNTP{refuseAll: true}, articles)
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Username: "giveup", Password: "x", Connections: 3}

	start := time.Now()
	_, err, _ := downloadTo(t, context.Background(), []ClientConfig{cfg}, nzb)
	if err == nil {
		t.Fatal("a provider that always says too many connections should end in an error")
	}
	if err.Error() != TooManyConnectionsMessage {
		t.Fatalf("error = %q, want the plain message %q", err.Error(), TooManyConnectionsMessage)
	}
	var rel *ReleaseError
	if errors.As(err, &rel) {
		t.Fatal("too many connections is not the release's fault, so it must not blocklist it")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("gave up after %v", took)
	}
	srv.waitNoConnections(t)
}

func TestConnectionsAreClosedWhenADownloadIsCancelled(t *testing.T) {
	fastGate(t)
	nzb, articles, _ := bigNZB(40)
	srv := newLimitedNNTP(t, &limitedNNTP{bodyDelay: 5 * time.Second}, articles)
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Username: "cancel", Password: "x", Connections: 4}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err, _ := downloadTo(t, ctx, []ClientConfig{cfg}, nzb)
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for srv.activeNow.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.activeNow.Load() < 4 {
		t.Fatalf("only %d connections opened", srv.activeNow.Load())
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want cancelled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the download did not stop after it was cancelled")
	}
	srv.waitNoConnections(t)
}

// The case from a real install: two downloads at once, each allowed the whole
// plan, used to open twice the connections the provider allows.
func TestTwoDownloadsShareOneLoginsConnections(t *testing.T) {
	fastGate(t)
	nzbA, articlesA, fullA := bigNZB(50)
	srv := newLimitedNNTP(t, &limitedNNTP{max: 4, bodyDelay: 3 * time.Millisecond}, articlesA)
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Username: "shared", Password: "x", Connections: 4}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err, _ := downloadTo(t, context.Background(), []ClientConfig{cfg}, nzbA)
			if err != nil {
				t.Errorf("download failed: %v", err)
				return
			}
			mustHold(t, res, fullA)
		}()
	}
	wg.Wait()
	// The provider may briefly still count a connection that was just closed,
	// so one or two refusals are not the downloads going over the limit. What
	// matters: they never had more than 4 working at once, they did not fight
	// for places, and the limit was not lowered by mistake.
	_, refused, _ := srv.stats()
	if refused > 2 {
		t.Errorf("the provider refused %d connections: the downloads went over its limit", refused)
	}
	if peak := srv.workingPeak(); peak > 4 {
		t.Errorf("%d connections were working at once, the login allows 4", peak)
	}
	for _, st := range ServerStates() {
		if st.Host == srv.addr+":"+strconv.Itoa(srv.port) && st.Limit != 4 {
			t.Errorf("the limit was lowered to %d although the provider allows 4", st.Limit)
		}
	}
	srv.waitNoConnections(t)
}

func TestRemovingAServerClosesItsConnectionsAndFallsBackToTheNextOne(t *testing.T) {
	fastGate(t)
	nzb, articles, full := bigNZB(40)
	slow := newLimitedNNTP(t, &limitedNNTP{bodyDelay: 100 * time.Millisecond}, articles)
	backup := newLimitedNNTP(t, &limitedNNTP{}, articles)
	primary := ClientConfig{Host: slow.addr, Port: slow.port, Username: "retire", Password: "x", Connections: 3}
	second := ClientConfig{Host: backup.addr, Port: backup.port, Username: "retire2", Password: "x", Connections: 2}

	done := make(chan Result, 1)
	errc := make(chan error, 1)
	go func() {
		res, err, _ := downloadTo(t, context.Background(), []ClientConfig{primary, second}, nzb)
		errc <- err
		done <- res
	}()
	deadline := time.Now().Add(3 * time.Second)
	for slow.activeNow.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	RetireServer(primary)
	slow.waitNoConnections(t)
	if err := <-errc; err != nil {
		t.Fatalf("the download should have carried on with the other server: %v", err)
	}
	mustHold(t, <-done, full)
}

func TestRemovingTheOnlyServerEndsTheDownloadWithAPlainMessage(t *testing.T) {
	fastGate(t)
	nzb, articles, _ := bigNZB(40)
	srv := newLimitedNNTP(t, &limitedNNTP{bodyDelay: 100 * time.Millisecond}, articles)
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Username: "retire-only", Password: "x", Connections: 3}

	errc := make(chan error, 1)
	go func() {
		_, err, _ := downloadTo(t, context.Background(), []ClientConfig{cfg}, nzb)
		errc <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for srv.activeNow.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	RetireServer(cfg)
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "changed or removed") {
			t.Fatalf("error = %v, want one that says the server was changed or removed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the download did not end after its only server was removed")
	}
	srv.waitNoConnections(t)
}

func TestLoweringTheConnectionCountTakesEffectDuringADownload(t *testing.T) {
	fastGate(t)
	nzb, articles, full := bigNZB(80)
	srv := newLimitedNNTP(t, &limitedNNTP{bodyDelay: 20 * time.Millisecond}, articles)
	cfg := ClientConfig{Host: srv.addr, Port: srv.port, Username: "lower", Password: "x", Connections: 6}

	done := make(chan error, 1)
	var res Result
	go func() {
		var err error
		res, err, _ = downloadTo(t, context.Background(), []ClientConfig{cfg}, nzb)
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for srv.activeNow.Load() < 6 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	edited := cfg
	edited.Connections = 2
	ConfigureServer(edited)

	deadline = time.Now().Add(3 * time.Second)
	for srv.activeNow.Load() > 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := srv.activeNow.Load(); n > 2 {
		t.Fatalf("%d connections still open after lowering the count to 2", n)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mustHold(t, res, full)
}

func TestASilentServerIsGivenUpOn(t *testing.T) {
	fastGate(t)
	nntpIOTimeout = 200 * time.Millisecond
	srv := newLimitedNNTP(t, &limitedNNTP{hang: true}, map[string][]byte{})
	c, err := DialNNTP(srv.addr, srv.port, false, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Authenticate("u", "p"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := c.FetchBody("<x@example>"); err == nil {
		t.Fatal("a server that never answers should end in an error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("waited %v for a silent server", time.Since(start))
	}
	c.Quit()
	srv.waitNoConnections(t)
}

func TestTooManyConnectionsIsRecognised(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"502 with the usual words", &NNTPError{Code: 502, Message: "Too many connections."}, true},
		{"502 with no explanation", &NNTPError{Code: 502, Message: ""}, true},
		{"the words on another code", &NNTPError{Code: 400, Message: "too many connections for this account"}, true},
		{"a connection limit", &NNTPError{Code: 481, Message: "Connection limit exceeded"}, true},
		{"502 about the login", &NNTPError{Code: 502, Message: "Authentication failed"}, false},
		{"502 access denied", &NNTPError{Code: 502, Message: "Access denied"}, false},
		{"wrong password", &NNTPError{Code: 481, Message: "Authentication failed"}, false},
		{"not an NNTP error", errors.New("connection reset"), false},
		{"wrapped", fmt.Errorf("nntp authenticate on news.example.com: %w", &NNTPError{Code: 502, Message: "Too many connections."}), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTooManyConnections(tc.err); got != tc.want {
				t.Fatalf("IsTooManyConnections(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestFriendlyErrorsReadLikeAPersonWroteThem(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{&NNTPError{Code: 502, Message: "Too many connections."}, TooManyConnectionsMessage},
		{&NNTPError{Code: 481, Message: "Authentication failed"}, "The provider refused this username and password."},
		{&NNTPError{Code: 502, Message: "Access denied"}, "The provider refused this username and password."},
	}
	for _, tc := range tests {
		got, ok := FriendlyError(tc.err)
		if !ok || got != tc.want {
			t.Errorf("FriendlyError(%v) = %q, %v; want %q", tc.err, got, ok, tc.want)
		}
	}
	if _, ok := FriendlyError(errors.New("connection reset by peer")); ok {
		t.Error("an error with nothing better to say should be passed through")
	}
}

func TestGateHandsOutNoMorePlacesThanItsLimit(t *testing.T) {
	fastGate(t)
	g := gateFor(ClientConfig{Host: "gate.example", Port: 119, Username: "a", Connections: 2})
	r1, err := g.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := g.acquire(context.Background())

	got := make(chan struct{})
	go func() {
		rel, err := g.acquire(context.Background())
		if err == nil {
			rel()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("a third place was handed out with a limit of 2")
	case <-time.After(100 * time.Millisecond):
	}
	r1()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("the waiting caller did not get the place that was given back")
	}
	r2()
	r2() // giving back twice must not free a place that is not free
	if g.inUse != 0 {
		t.Fatalf("inUse = %d after everything was given back", g.inUse)
	}
}

func TestGateWaitEndsWhenTheDownloadIsCancelled(t *testing.T) {
	fastGate(t)
	g := gateFor(ClientConfig{Host: "cancel.example", Port: 119, Username: "a", Connections: 1})
	rel, _ := g.acquire(context.Background())
	defer rel()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := g.acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the wait to end with the context", err)
	}
}

func TestGateLearnsTheLimitFromRefusals(t *testing.T) {
	tests := []struct {
		name         string
		configured   int
		open         int // places in use when the refusal came
		refusals     int
		wantLimit    int
		sleepBetween time.Duration
	}{
		{"some are working: that many are allowed", 10, 4, 1, 4, 0},
		{"none working: halve", 10, 0, 1, 5, 0},
		{"none working, a burst of refusals counts once", 10, 0, 6, 5, 0},
		{"none working, refusals spread out keep halving to one", 10, 0, 4, 1, 60 * time.Millisecond},
		{"never below one", 1, 0, 3, 1, 60 * time.Millisecond},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fastGate(t)
			cfg := ClientConfig{Host: fmt.Sprint("learn", i, ".example"), Port: 119, Username: "a", Connections: tc.configured}
			g := gateFor(cfg)
			g.inUse = tc.open
			for j := 0; j < tc.refusals; j++ {
				if wait := g.tooMany(); wait <= 0 || wait > 2*tooManyBackoffMax {
					t.Fatalf("wait of %v is not a sensible backoff", wait)
				}
				time.Sleep(tc.sleepBetween)
			}
			g.mu.Lock()
			got := g.limitLocked()
			g.mu.Unlock()
			if got != tc.wantLimit {
				t.Fatalf("limit = %d, want %d", got, tc.wantLimit)
			}
		})
	}
}

func TestEditingTheConnectionCountStartsFresh(t *testing.T) {
	fastGate(t)
	cfg := ClientConfig{Host: "edit.example", Port: 119, Username: "a", Connections: 10}
	g := gateFor(cfg)
	g.tooMany()
	g.mu.Lock()
	lowered := g.limitLocked()
	g.mu.Unlock()
	if lowered >= 10 {
		t.Fatalf("limit = %d, the refusal should have lowered it", lowered)
	}
	cfg.Connections = 12
	ConfigureServer(cfg)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.limitLocked() != 12 {
		t.Fatalf("limit = %d after the person set 12, want 12", g.limitLocked())
	}
}

func TestBackoffGrowsAndStaysWithinItsBounds(t *testing.T) {
	fastGate(t)
	tooManyBackoffBase, tooManyBackoffMax = 100*time.Millisecond, 800*time.Millisecond
	g := gateFor(ClientConfig{Host: "backoff.example", Port: 119, Username: "a", Connections: 4})
	var waits []time.Duration
	for i := 0; i < 8; i++ {
		waits = append(waits, g.tooMany())
	}
	for i, w := range waits {
		exp := 100 * time.Millisecond << min(i, 6)
		exp = min(exp, 800*time.Millisecond)
		if w < exp/2 || w > exp*3/2 {
			t.Errorf("wait %d = %v, want within half and one and a half of %v", i, w, exp)
		}
	}
}
