package api

import (
	"sync"
	"time"
)

// progressEvery is the most often a download's progress is saved to the
// database. The downloader reports every article (hundreds a second on a fast
// line, from many connections at once); saving each one made every page wait
// behind those writes.
const progressEvery = time.Second

// progressSaver turns a flood of "this much is done" reports into at most one
// database write per interval, and never writes the same percentage twice.
type progressSaver struct {
	save  func(pct float64) error
	every time.Duration
	now   func() time.Time

	mu      sync.Mutex
	last    time.Time
	written float64
	pending float64
	wrote   bool
}

func newProgressSaver(save func(pct float64) error) *progressSaver {
	return &progressSaver{save: save, every: progressEvery, now: time.Now}
}

// Report is the downloader's callback.
func (p *progressSaver) Report(done, total int64) {
	pct := percent(done, total)
	p.mu.Lock()
	p.pending = pct
	due := p.now().Sub(p.last) >= p.every && (!p.wrote || pct != p.written)
	if !due {
		p.mu.Unlock()
		return
	}
	p.last, p.written, p.wrote = p.now(), pct, true
	p.mu.Unlock()
	_ = p.save(pct) // best effort: a missed update is corrected by the next one
}

// Flush saves the latest percentage if it has not been saved yet.
func (p *progressSaver) Flush() {
	p.mu.Lock()
	pct := p.pending
	need := !p.wrote || pct != p.written
	p.last, p.written, p.wrote = p.now(), pct, true
	p.mu.Unlock()
	if need {
		_ = p.save(pct)
	}
}
