package api_test

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

// openStream opens the live stream and, when it is let in, waits for its
// first event.
func openStream(t *testing.T, client *http.Client, base string) (resp *http.Response, br *bufio.Reader, cancel context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	c := *client
	c.Timeout = 0
	resp, err := c.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	br = bufio.NewReader(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return resp, br, cancel
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			cancel()
			t.Fatalf("reading the first event: %v", err)
		}
		if strings.HasPrefix(line, "event: hello") {
			return resp, br, cancel
		}
	}
}

func TestEventStreamCapAnswers429AndFreesSlots(t *testing.T) {
	defer api.SetEventLimitsForTest(2, 10, time.Minute)()
	_, base, client := newServerWithDB(t)

	r1, _, cancel1 := openStream(t, client, base)
	defer cancel1()
	if ct := r1.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content type %q", ct)
	}
	_, _, cancel2 := openStream(t, client, base)
	defer cancel2()

	r3, _, cancel3 := openStream(t, client, base)
	defer cancel3()
	if r3.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("the third stream got %d, want 429", r3.StatusCode)
	}
	if r3.Header.Get("Retry-After") == "" {
		t.Error("the 429 has no Retry-After")
	}
	r3.Body.Close()

	// Closing a stream gives its slot back.
	cancel1()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r4, _, cancel4 := openStream(t, client, base)
		if r4.StatusCode == http.StatusOK {
			cancel4()
			break
		}
		r4.Body.Close()
		cancel4()
		if time.Now().After(deadline) {
			t.Fatal("the slot of a closed stream was never freed")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestEventStreamSendsKeepAlive(t *testing.T) {
	defer api.SetEventLimitsForTest(2, 10, 30*time.Millisecond)()
	_, base, client := newServerWithDB(t)
	_, br, cancel := openStream(t, client, base)
	defer cancel()
	done := make(chan string, 1)
	go func() {
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				done <- "read failed: " + err.Error()
				return
			}
			if strings.HasPrefix(line, ": keep-alive") {
				done <- "ok"
				return
			}
		}
	}()
	select {
	case got := <-done:
		if got != "ok" {
			t.Fatal(got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no keep-alive comment arrived")
	}
}

func TestLibrarySuggestRoute(t *testing.T) {
	db, base, client := newServerWithDB(t)
	if _, err := db.Exec(`INSERT INTO movies (tmdb_id, title, year) VALUES (1, 'Amélie', 2001), (2, 'Star Wars', 1977)`); err != nil {
		t.Fatal(err)
	}
	type hit struct {
		Kind  string `json:"kind"`
		Title string `json:"title"`
	}
	for _, c := range []struct {
		q    string
		want int
	}{{"amel", 1}, {"star+w", 1}, {"zzz", 0}, {"", 0}, {"%22", 0}} {
		got := getJSON[[]hit](t, client, base+"/api/library/suggest?q="+c.q)
		if len(got) != c.want {
			t.Errorf("q=%q: %d hits, want %d (%v)", c.q, len(got), c.want, got)
		}
	}
}
