package download

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
)

// A news server login may only have so many connections open at once. Each
// download used to open its own set, so two downloads (or a download and a
// retry) added up to twice the plan, and the provider answered "502 Too many
// connections". A gate is one shared counter per login: every connection to
// it, from every download, takes a place first and gives it back when the
// connection closes.
//
// The gate also learns. When the provider still says "too many" (another
// program, like SABnzbd, may be using the same login), it halves the number
// of places for the rest of the session, down to one, and the workers wait a
// little and try again instead of failing the download. It only gives up after
// several minutes of that, with a message the person can act on.

// Timing of the wait after a "too many connections" answer. Variables so the
// tests can shorten them.
var (
	tooManyBackoffBase = time.Second
	tooManyBackoffMax  = 30 * time.Second
	tooManyGiveUp      = 5 * time.Minute
	// tooManyQuiet is how long without a "too many" answer ends a spell of
	// trouble: a download started later begins with a clean slate.
	tooManyQuiet = time.Minute
	// tooManySettle is the shortest time between two halvings, so a burst of
	// refusals to connections opened at the same moment counts as one.
	tooManySettle = 3 * time.Second
)

// errServerRemoved is what a worker gets when its server was edited (to a
// different address or login), switched off or removed while it was downloading.
var errServerRemoved = errors.New("the Usenet server was changed or removed while this was downloading. Try the download again")

type gateKey struct {
	host string
	port int
	ssl  bool
	user string
}

func keyFor(cfg ClientConfig) gateKey {
	return gateKey{host: strings.ToLower(cfg.Host), port: cfg.Port, ssl: cfg.UseSSL, user: cfg.Username}
}

// serverGate is the shared counter for one login on one server.
type serverGate struct {
	key  gateKey
	mu   sync.Mutex
	cond *sync.Cond

	configured int // connections the person set
	learned    int // connections that worked this session, at most configured
	inUse      int
	retired    bool
	conns      map[*NNTPConn]struct{}

	softSince  time.Time // first "too many" answer of this spell of trouble
	softLast   time.Time // latest one
	softCount  int       // how many answers in this spell
	lastHalved time.Time
	lastClosed time.Time // when a working connection last closed
}

var (
	gatesMu sync.Mutex
	gates   = map[gateKey]*serverGate{}
)

// gateFor returns the shared gate for cfg's login, creating it on first use.
// A different connection count than before (the person edited the server)
// resets what the gate learned.
func gateFor(cfg ClientConfig) *serverGate {
	n := max(cfg.Connections, 1)
	gatesMu.Lock()
	defer gatesMu.Unlock()
	k := keyFor(cfg)
	g := gates[k]
	if g == nil {
		g = &serverGate{key: k, configured: n, learned: n, conns: map[*NNTPConn]struct{}{}}
		g.cond = sync.NewCond(&g.mu)
		gates[k] = g
		return g
	}
	g.setConfigured(n)
	return g
}

func (g *serverGate) setConfigured(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if n != g.configured {
		g.configured, g.learned = n, n
		g.softSince, g.softLast, g.softCount = time.Time{}, time.Time{}, 0
		g.cond.Broadcast()
	}
}

func (g *serverGate) limitLocked() int { return max(min(g.configured, g.learned), 1) }

// troubledLocked is the "give up" error once the provider has been refusing
// connections for tooManyGiveUp with no success in between.
func (g *serverGate) troubledLocked() error {
	if g.softSince.IsZero() || time.Since(g.softLast) > tooManyQuiet {
		return nil
	}
	if g.softLast.Sub(g.softSince) >= tooManyGiveUp {
		return errors.New(TooManyConnectionsMessage)
	}
	return nil
}

// acquire waits for a place. The returned function gives it back (once).
func (g *serverGate) acquire(ctx context.Context) (release func(), err error) {
	stop := context.AfterFunc(ctx, func() {
		g.mu.Lock()
		g.cond.Broadcast()
		g.mu.Unlock()
	})
	defer stop()
	g.mu.Lock()
	defer g.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if g.retired {
			return nil, errServerRemoved
		}
		if err := g.troubledLocked(); err != nil {
			return nil, err
		}
		if g.inUse < g.limitLocked() {
			g.inUse++
			var once sync.Once
			return func() {
				once.Do(func() {
					g.mu.Lock()
					g.inUse--
					g.cond.Broadcast()
					g.mu.Unlock()
				})
			}, nil
		}
		g.cond.Wait()
	}
}

// shedOne takes back the place of connection c when more connections are open
// than the gate now allows (the person lowered the number, or the provider
// said too many), and reports whether it did. The caller then closes c. Only as
// many callers as there are extra connections get a yes, however many ask at
// once. Attempts still being made are not counted: they may yet be refused.
func (g *serverGate) shedOne(c *NNTPConn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.conns) > g.limitLocked() {
		delete(g.conns, c)
		g.inUse--
		g.lastClosed = time.Now()
		g.cond.Broadcast()
		return true
	}
	return false
}

// noteClosed records that a working connection was just closed. The provider
// can take a moment to count it as closed, so a refusal in the next few
// seconds says nothing about the limit.
func (g *serverGate) noteClosed() {
	g.mu.Lock()
	g.lastClosed = time.Now()
	g.mu.Unlock()
}

// track and untrack keep the open connections, so RetireServer can close them.
func (g *serverGate) track(c *NNTPConn) {
	g.mu.Lock()
	g.conns[c] = struct{}{}
	g.mu.Unlock()
}

func (g *serverGate) untrack(c *NNTPConn) {
	g.mu.Lock()
	delete(g.conns, c)
	g.mu.Unlock()
}

// succeeded is called once a connection is open and signed in. It ends a spell
// of trouble (the provider let a connection in), and it corrects the limit
// upwards when more connections are open than it thinks the provider allows:
// they are proof that it allows that many. (Refusals to attempts made at the
// same moment can arrive before the connections that did get in are counted.)
func (g *serverGate) succeeded() {
	g.mu.Lock()
	g.softSince, g.softLast, g.softCount = time.Time{}, time.Time{}, 0
	if n := len(g.conns); n > g.learned {
		g.learned = min(n, g.configured)
	}
	g.mu.Unlock()
}

// tooMany records the provider's "too many connections" and returns how long
// the caller should wait before trying again (with jitter, longer the longer the
// trouble lasts). It lowers the number of places for the rest of the session and
// says so in the log: to the number of connections that are open and working
// or being opened now (the refused attempt has already given its place back),
// which is about what the provider has just shown it allows, or, when there
// are none, to half of what it was, never below one. Attempts still in flight
// when a refusal comes are refused in turn, and each refusal counts again, so
// the number settles on the connections that really got in.
func (g *serverGate) tooMany() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if g.softSince.IsZero() || now.Sub(g.softLast) > tooManyQuiet {
		g.softSince, g.softCount = now, 0
	}
	g.softLast = now
	g.softCount++

	before := g.limitLocked()
	switch used := g.inUse; {
	case now.Sub(g.lastClosed) < tooManySettle:
		// A connection of ours only just closed and the provider may not have
		// counted it out yet: wait and try again, without drawing conclusions.
	case used >= 1 && used < before:
		g.learned = used
	case used == 0 && now.Sub(g.lastHalved) > tooManySettle:
		g.learned = max(before/2, 1)
		g.lastHalved = now
	}
	if after := g.limitLocked(); after != before {
		log.Printf("usenet: %s says there are too many connections on this login; using %d instead of %d for now", g.key.host, after, before)
	}
	g.cond.Broadcast()
	problems.Record(problems.Problem{
		Code: problems.CodeUsenetTooMany, Subject: g.key.host,
		Message: fmt.Sprintf("%s says this login has too many connections. Using %d for now (set to %d).", g.key.host, g.limitLocked(), g.configured),
		Detail:  fmt.Sprintf("server %s port %d, connections set to %d, allowed for now %d, open now %d, refusals in this spell %d", g.key.host, g.key.port, g.configured, g.limitLocked(), g.inUse, g.softCount),
	})

	wait := tooManyBackoffBase << min(g.softCount-1, 6)
	wait = min(wait, tooManyBackoffMax)
	return time.Duration(float64(wait) * (0.5 + rand.Float64()))
}

// retire closes every open connection of the gate and refuses new ones.
func (g *serverGate) retire() {
	g.mu.Lock()
	g.retired = true
	for c := range g.conns {
		c.Close()
	}
	g.cond.Broadcast()
	g.mu.Unlock()
}

// ConfigureServer tells the shared connection limit about a server that was
// just added or edited. A lower connection count takes effect at once: the
// downloads running now close their extra connections after the article
// they are on.
func ConfigureServer(cfg ClientConfig) { gateFor(cfg) }

// RetireServer closes every connection to the server and stops the downloads
// running now from using it. Call it when the server is removed, switched off,
// or moved to another address or login. A download that has another server to
// fall back on carries on with that one; otherwise it fails with a message
// that says why.
func RetireServer(cfg ClientConfig) {
	gatesMu.Lock()
	g := gates[keyFor(cfg)]
	delete(gates, keyFor(cfg))
	gatesMu.Unlock()
	if g != nil {
		g.retire()
	}
}

// ServerState is how many connections a news server login has open, for the
// diagnostics report.
type ServerState struct {
	Host       string
	InUse      int
	Limit      int // what is allowed now
	Configured int // what the person set
}

// ServerStates lists every login the downloader has used since it started.
func ServerStates() []ServerState {
	gatesMu.Lock()
	all := make([]*serverGate, 0, len(gates))
	for _, g := range gates {
		all = append(all, g)
	}
	gatesMu.Unlock()
	out := make([]ServerState, 0, len(all))
	for _, g := range all {
		g.mu.Lock()
		out = append(out, ServerState{Host: g.key.host + ":" + strconv.Itoa(g.key.port), InUse: g.inUse, Limit: g.limitLocked(), Configured: g.configured})
		g.mu.Unlock()
	}
	return out
}

// sleepCtx waits d, or less if ctx ends first (then it returns the error).
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
