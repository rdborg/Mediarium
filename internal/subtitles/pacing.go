package subtitles

import (
	"net/http"
	"sync"
	"time"
)

// apiRequestInterval keeps Mediarium under OpenSubtitles' free limit of 5
// requests per second per IP address. A sweep over a large library would
// otherwise burst past it and be told to stop.
const apiRequestInterval = 250 * time.Millisecond

// pacedTransport spaces requests at least `every` apart, shared by everything
// that uses the client. It never drops a request, it only makes it wait.
type pacedTransport struct {
	next  http.RoundTripper
	every time.Duration

	mu   sync.Mutex
	last time.Time
}

func newPacedTransport(next http.RoundTripper, every time.Duration) *pacedTransport {
	if next == nil {
		next = http.DefaultTransport
	}
	return &pacedTransport{next: next, every: every}
}

func (p *pacedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	p.mu.Lock()
	now := time.Now()
	slot := p.last.Add(p.every)
	if slot.Before(now) {
		slot = now
	}
	p.last = slot
	p.mu.Unlock()

	if wait := time.Until(slot); wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-req.Context().Done():
			t.Stop()
			return nil, req.Context().Err()
		}
	}
	return p.next.RoundTrip(req)
}
