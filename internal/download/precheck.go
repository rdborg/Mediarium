package download

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// A check before downloading: ask the news servers (NNTP STAT, which says
// whether an article exists without sending it) about a sample of the articles
// in an NZB. If so many are missing that the PAR2 files in the release could
// never rebuild them, the release is given up on before a single article is
// downloaded, instead of after an hour of downloading.
//
// SABnzbd's "Check before download" does the same with every article, which is
// slow. This one asks about a small spread-out sample and estimates from it.
// It costs a few requests, which some providers dislike in large numbers, and
// it is only an estimate, so it is off unless the person switches it on. It
// never fails a download because of a problem of its own: when the servers
// can't be asked (no STAT, a login problem, too slow) it says so and the
// download starts as usual.

// PrecheckOptions tune the check. The zero value gives the defaults.
type PrecheckOptions struct {
	Fraction    float64       // share of the articles asked about; default 0.03
	MinSample   int           // at least this many (or all, in a small NZB); default 10
	MaxSample   int           // at most this many; default 100
	Connections int           // per server, at most; default 2
	Timeout     time.Duration // for the whole check; default 30 seconds
}

const (
	defaultPrecheckFraction = 0.03
	defaultPrecheckMin      = 10
	defaultPrecheckMax      = 100
	defaultPrecheckConns    = 2
	defaultPrecheckTimeout  = 30 * time.Second
	// largeFileSegments is how many articles make a file "large": the first and
	// last article of such a file are always asked about (a post that is cut off
	// usually loses the end of a file).
	largeFileSegments = 10
	// extraConnectWait is how long a second connection waits for a free place
	// from the shared connection limit before it is given up on: the check
	// works fine with one.
	extraConnectWait = 1500 * time.Millisecond
)

func (o PrecheckOptions) withDefaults() PrecheckOptions {
	if o.Fraction <= 0 {
		o.Fraction = defaultPrecheckFraction
	}
	if o.MinSample <= 0 {
		o.MinSample = defaultPrecheckMin
	}
	if o.MaxSample <= 0 {
		o.MaxSample = defaultPrecheckMax
	}
	if o.MaxSample < o.MinSample {
		o.MaxSample = o.MinSample
	}
	if o.Connections <= 0 {
		o.Connections = defaultPrecheckConns
	}
	o.Connections = min(o.Connections, defaultPrecheckConns)
	if o.Timeout <= 0 {
		o.Timeout = defaultPrecheckTimeout
	}
	return o
}

// PrecheckResult is what the check found out.
type PrecheckResult struct {
	// Skipped is true when the servers could not be asked; SkipReason says why,
	// in plain words. Nothing else is meaningful then.
	Skipped    bool
	SkipReason string

	Total   int // articles in the NZB
	Checked int // articles asked about
	Missing int // of those, how many no server has

	// EstimatedMissingBytes is the share missing in the sample applied to the
	// whole release; RepairableBytes is what its PAR2 files could rebuild.
	EstimatedMissingBytes int64
	RepairableBytes       int64
}

// Hopeless reports whether the release is probably beyond repair: the
// estimated loss is more than its PAR2 files could rebuild.
func (r PrecheckResult) Hopeless() bool {
	return !r.Skipped && r.Missing > 0 && r.EstimatedMissingBytes > r.RepairableBytes
}

// Err is the error that ends the download when the release is hopeless, nil
// otherwise. It is a ReleaseError, so the release goes on the blocklist.
func (r PrecheckResult) Err() error {
	if !r.Hopeless() {
		return nil
	}
	return &ReleaseError{fmt.Errorf("Checked %d articles; %d of them are not on your Usenet servers", r.Checked, r.Missing)}
}

// Summary is the line for the item's activity.
func (r PrecheckResult) Summary() string {
	switch {
	case r.Skipped:
		return "Article check skipped: " + r.SkipReason
	case r.Missing == 0:
		return fmt.Sprintf("Checked %d of %d articles: all found", r.Checked, r.Total)
	case r.Hopeless() && r.RepairableBytes == 0:
		return fmt.Sprintf("Checked %d of %d articles: %d not found, and the release has no PAR2 files to repair it", r.Checked, r.Total, r.Missing)
	case r.Hopeless():
		return fmt.Sprintf("Checked %d of %d articles: %d not found, more than the PAR2 files can repair", r.Checked, r.Total, r.Missing)
	}
	return fmt.Sprintf("Checked %d of %d articles: %d not found, the PAR2 files can repair that", r.Checked, r.Total, r.Missing)
}

func skippedPrecheck(total int, reason string) PrecheckResult {
	return PrecheckResult{Skipped: true, SkipReason: reason, Total: total}
}

// Precheck asks the servers, in priority order, about a sample of the NZB's
// articles. An article counts as there as soon as one server has it, and the
// servers after that one are not asked about it. Each server gets at most two
// connections (fewer if its login allows fewer), taken from the same shared
// limit as downloads. The whole check stops after opt.Timeout.
func Precheck(ctx context.Context, servers []ClientConfig, nzb *NZB, opt PrecheckOptions) PrecheckResult {
	opt = opt.withDefaults()
	total := 0
	for _, f := range nzb.Files {
		total += len(f.Segments)
	}
	if total == 0 {
		return skippedPrecheck(total, "the NZB lists no articles")
	}
	if len(servers) == 0 {
		return skippedPrecheck(total, "no Usenet server is set up")
	}
	sample := pickSample(nzb, opt)

	ctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()

	type answer struct {
		found  []bool
		failed string // why some server could not be asked, "" if all could
	}
	done := make(chan answer, 1)
	go func() {
		found, failed := askServers(ctx, servers, nzb, sample, opt)
		done <- answer{found, failed}
	}()

	var ans answer
	select {
	case ans = <-done:
	case <-ctx.Done():
		// The workers notice the cancelled context and close their connections
		// by themselves.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return skippedPrecheck(total, fmt.Sprintf("the servers took longer than %d seconds to answer", int(opt.Timeout.Seconds())))
		}
		return skippedPrecheck(total, "it was cancelled")
	}

	missing := 0
	for _, ok := range ans.found {
		if !ok {
			missing++
		}
	}
	if missing > 0 && ans.failed != "" {
		// Some article is on none of the servers that answered, but a server did
		// not answer: it may have it.
		return skippedPrecheck(total, ans.failed)
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return skippedPrecheck(total, fmt.Sprintf("the servers took longer than %d seconds to answer", int(opt.Timeout.Seconds())))
		}
		return skippedPrecheck(total, "it was cancelled")
	}
	res := PrecheckResult{Total: total, Checked: len(sample), Missing: missing, RepairableBytes: par2Bytes(nzb)}
	if missing > 0 {
		res.EstimatedMissingBytes = estimateMissingBytes(nzb, sample, ans.found)
	}
	return res
}

// segRef is one article: file and segment index. forced marks the first and
// last articles of large files, which are asked about whatever the sample size.
type segRef struct {
	file, seg int
	forced    bool
}

// estimateMissingBytes scales what the sample found missing up to the whole
// release. The forced articles were not drawn at random (they sit at the ends of
// files), so each stands for itself only; the spread ones stand for all the
// rest. Without any spread ones the plain share is used.
func estimateMissingBytes(nzb *NZB, sample []segRef, found []bool) int64 {
	var forcedBytes, forcedLost int64
	var spread, spreadLost, lostAll int
	for i, ref := range sample {
		lost := !found[i]
		if lost {
			lostAll++
		}
		if ref.forced {
			b := nzb.Files[ref.file].Segments[ref.seg].Bytes
			forcedBytes += b
			if lost {
				forcedLost += b
			}
			continue
		}
		spread++
		if lost {
			spreadLost++
		}
	}
	total := float64(nzb.TotalBytes())
	if spread == 0 {
		return int64(total * float64(lostAll) / float64(len(sample)))
	}
	return forcedLost + int64((total-float64(forcedBytes))*float64(spreadLost)/float64(spread))
}

// pickSample chooses which articles to ask about: the first and last article of
// every large file, and the rest spread evenly over the whole NZB, up to about
// opt.Fraction of all articles (at least opt.MinSample, at most opt.MaxSample).
// The result is in NZB order, so articles of one file are asked together.
func pickSample(nzb *NZB, opt PrecheckOptions) []segRef {
	var all []segRef
	var required []int // positions in all
	for fi, f := range nzb.Files {
		start := len(all)
		for si := range f.Segments {
			all = append(all, segRef{file: fi, seg: si})
		}
		if n := len(f.Segments); n >= largeFileSegments {
			required = append(required, start, start+n-1)
		}
	}
	total := len(all)
	target := int(math.Ceil(float64(total) * opt.Fraction))
	target = min(max(target, opt.MinSample), opt.MaxSample, total)

	chosen := map[int]bool{}
	forced := map[int]bool{}
	if len(required) <= target {
		for _, p := range required {
			chosen[p] = true
			forced[p] = true
		}
	} else {
		// More must-haves than room: keep an even spread of them, firsts and
		// lasts alternating (required holds a first and a last for each file).
		for i := 0; i < target; i++ {
			j := i * len(required) / target
			if i%2 == 1 {
				j |= 1
			}
			chosen[required[min(j, len(required)-1)]] = true
		}
	}
	// The rest of the room is filled with articles spread evenly over the NZB.
	need := target - len(chosen)
	for k := 0; k < need; k++ {
		p := int((float64(k) + 0.5) * float64(total) / float64(need))
		for chosen[p] {
			p = (p + 1) % total
		}
		chosen[p] = true
	}
	positions := make([]int, 0, len(chosen))
	for p := range chosen {
		positions = append(positions, p)
	}
	sort.Ints(positions)
	out := make([]segRef, len(positions))
	for i, p := range positions {
		out[i] = all[p]
		out[i].forced = forced[p]
	}
	return out
}

// askServers asks each server in turn about the articles no earlier server has.
// found[i] says whether sample[i] was found on some server. failed is why a
// server could not be asked ("" when every server answered every question).
func askServers(ctx context.Context, servers []ClientConfig, nzb *NZB, sample []segRef, opt PrecheckOptions) (found []bool, failed string) {
	found = make([]bool, len(sample))
	for _, cfg := range servers {
		var pending []int
		for i, ok := range found {
			if !ok {
				pending = append(pending, i)
			}
		}
		if len(pending) == 0 || ctx.Err() != nil {
			break
		}
		if reason := askServer(ctx, cfg, nzb, sample, pending, found, opt); reason != "" && failed == "" {
			failed = reason
		}
	}
	return found, failed
}

// askServer sends STAT for the pending articles to one server over a few
// connections, marking the ones it has in found. It returns why the server
// could not be asked, "" when it answered every question.
func askServer(ctx context.Context, cfg ClientConfig, nzb *NZB, sample []segRef, pending []int, found []bool, opt PrecheckOptions) string {
	workers := min(opt.Connections, max(cfg.Connections, 1), len(pending))

	var (
		mu     sync.Mutex
		next   int
		reason string
		wg     sync.WaitGroup
	)
	fail := func(r string) {
		mu.Lock()
		if reason == "" {
			reason = r
		}
		mu.Unlock()
	}
	take := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		if reason != "" || next >= len(pending) {
			return 0, false
		}
		i := pending[next]
		next++
		return i, true
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(primary bool) {
			defer wg.Done()
			wait := extraConnectWait
			if primary {
				wait = 0
			}
			conn, closeConn, err := precheckConnect(ctx, cfg, wait)
			if err != nil {
				// A second connection that cannot get a place is no loss.
				if primary && ctx.Err() == nil {
					fail(precheckReason(cfg, err))
				}
				return
			}
			defer closeConn()
			group := ""
			for {
				if ctx.Err() != nil {
					return
				}
				i, ok := take()
				if !ok {
					return
				}
				ref := sample[i]
				f := nzb.Files[ref.file]
				if len(f.Groups) > 0 && f.Groups[0] != group {
					// As the downloader does: some servers want a GROUP first. A
					// refusal is fine, they often answer by message-id regardless.
					var ne *NNTPError
					if err := conn.SelectGroup(f.Groups[0]); err == nil {
						group = f.Groups[0]
					} else if !errors.As(err, &ne) {
						if ctx.Err() == nil {
							fail(precheckReason(cfg, err))
						}
						return
					}
				}
				has, err := conn.Stat(f.Segments[ref.seg].MessageID)
				if err != nil {
					if ctx.Err() == nil {
						fail(precheckReason(cfg, err))
					}
					return
				}
				if has {
					mu.Lock()
					found[i] = true
					mu.Unlock()
				}
			}
		}(w == 0)
	}
	wg.Wait()
	return reason
}

// precheckConnect opens one connection for the check, after taking a place from
// the server's shared connection limit. wait, when set, is how long to wait for
// that place. The returned function closes the connection and gives the place
// back.
func precheckConnect(ctx context.Context, cfg ClientConfig, wait time.Duration) (*NNTPConn, func(), error) {
	gate := gateFor(cfg)
	actx := ctx
	if wait > 0 {
		var cancel context.CancelFunc
		actx, cancel = context.WithTimeout(ctx, wait)
		defer cancel()
	}
	release, err := gate.acquire(actx)
	if err != nil {
		return nil, nil, err
	}
	timeout := 30 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = min(timeout, max(time.Until(dl), time.Second))
	}
	c, err := DialNNTP(cfg.Host, cfg.Port, cfg.UseSSL, timeout)
	if err == nil {
		if err = c.Authenticate(cfg.Username, cfg.Password); err != nil {
			c.Quit()
		}
	}
	if err == nil && ctx.Err() != nil {
		c.Quit()
		err = ctx.Err()
	}
	if err != nil {
		release()
		return nil, nil, err
	}
	if !gate.track(c) {
		c.Quit()
		release()
		return nil, nil, errServerRemoved
	}
	gate.succeeded()
	// A check that runs out of time must not sit waiting on a slow read.
	stop := context.AfterFunc(ctx, func() { c.Close() })
	return c, func() {
		stop()
		gate.untrack(c)
		gate.noteClosed()
		c.Quit()
		release()
	}, nil
}

// precheckReason says in plain words why a server could not be asked.
func precheckReason(cfg ClientConfig, err error) string {
	var ne *NNTPError
	switch {
	case errors.Is(err, errServerRemoved):
		return "the server was changed or removed"
	case IsTooManyConnections(err):
		return cfg.Host + " says this login has too many connections open"
	case errors.As(err, &ne):
		switch ne.Code {
		case 500, 501, 503:
			return cfg.Host + " does not support checking articles"
		case 480, 481, 482, 502:
			return cfg.Host + " would not accept the login for the check"
		}
		return fmt.Sprintf("%s answered %d to the check", cfg.Host, ne.Code)
	case errors.Is(err, context.DeadlineExceeded):
		return "the connections were busy"
	}
	return "could not reach " + cfg.Host
}
