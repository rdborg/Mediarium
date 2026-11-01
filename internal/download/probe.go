package download

import (
	"context"
	"errors"
	"time"
)

// ProbeResult is what a test of a login's connection limit found.
type ProbeResult struct {
	// Accepted is how many connections the provider let in at once.
	Accepted int
	// Requested is how many were tried (at most).
	Requested int
	// Refused is true when the provider said "too many connections" before
	// Requested was reached, so Accepted is the provider's limit for this login
	// right now (other programs using the login count against it).
	Refused bool
}

// ErrServerBusy is returned by ProbeConnections when a download is using the
// login, which would make the answer meaningless.
var ErrServerBusy = errors.New("a download is using this server right now")

// ActiveConnections is how many connections the downloader has open or is
// opening to a login.
func ActiveConnections(cfg ClientConfig) int {
	gatesMu.Lock()
	g := gates[keyFor(cfg)]
	gatesMu.Unlock()
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inUse
}

// LoginUse says what the downloader is doing with a login right now, so a
// background check or a Test button does not open one more connection into a
// login that is full of Mediarium's own. working is true when a download has a
// signed-in connection open (so the server and the login are known to work).
// busy is true when connections are open or being opened, or one was closed
// in the last half minute (a provider can take that long to count it out).
func LoginUse(cfg ClientConfig) (working, busy bool) {
	gatesMu.Lock()
	g := gates[keyFor(cfg)]
	gatesMu.Unlock()
	if g == nil {
		return false, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.conns) > 0, g.inUse > 0 || time.Since(g.lastClosed) < loginSettle
}

// loginSettle is how long after one of its own connections closed Mediarium
// keeps away from the login with checks of its own.
const loginSettle = 30 * time.Second

// ProbeConnections finds out how many connections a news server lets this
// login open at once, by opening them one after another (each signed in) until
// the provider says there are too many or upTo are open, then closing them
// all. It stops at the first refusal, so it never asks for more than the
// provider's limit by more than one.
func ProbeConnections(ctx context.Context, cfg ClientConfig, upTo int) (ProbeResult, error) {
	upTo = max(min(upTo, 100), 1)
	res := ProbeResult{Requested: upTo}
	if _, busy := LoginUse(cfg); busy {
		return res, ErrServerBusy
	}
	// Every connection of the probe is counted in the login's gate, so a
	// download that starts meanwhile waits instead of being refused.
	gate := gateFor(cfg)
	var open []*NNTPConn
	defer func() {
		for _, c := range open {
			_ = c.Quit()
			_ = c.Close()
			gate.give()
		}
		gate.noteClosed()
	}()
	for len(open) < upTo {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		c, err := DialNNTP(cfg.Host, cfg.Port, cfg.UseSSL, 10*time.Second)
		if err == nil {
			err = c.Authenticate(cfg.Username, cfg.Password)
			if err != nil {
				_ = c.Close()
			}
		}
		if err != nil {
			if IsTooManyConnections(err) {
				res.Refused = true
				break
			}
			return res, err
		}
		gate.take()
		open = append(open, c)
	}
	res.Accepted = len(open)
	return res, nil
}
