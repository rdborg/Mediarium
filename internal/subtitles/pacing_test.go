package subtitles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPacedTransportSpacesRequests(t *testing.T) {
	var mu sync.Mutex
	var stamps []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		stamps = append(stamps, time.Now())
		mu.Unlock()
	}))
	defer srv.Close()

	every := 40 * time.Millisecond
	client := &http.Client{Transport: newPacedTransport(nil, every)}

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Errorf("request: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()

	if len(stamps) != 6 {
		t.Fatalf("every request must still be sent, got %d", len(stamps))
	}
	first, last := stamps[0], stamps[0]
	for _, s := range stamps {
		if s.Before(first) {
			first = s
		}
		if s.After(last) {
			last = s
		}
	}
	// Six requests at 40ms spacing take at least ~5 gaps, allowing some timer slack.
	if got := last.Sub(first); got < 5*every-15*time.Millisecond {
		t.Fatalf("requests were not spaced out: they all landed within %v", got)
	}
}

func TestPacedTransportGivesUpWhenTheContextEnds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	client := &http.Client{Transport: newPacedTransport(nil, 2*time.Second)}
	if resp, err := client.Get(srv.URL); err == nil {
		resp.Body.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if _, err := client.Do(req); err == nil {
		t.Fatal("a request waiting for its slot should stop when its context ends")
	}
}
