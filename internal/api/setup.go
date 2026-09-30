package api

import (
	"crypto/rand"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

// The first-run setup rule.
//
// Until the first administrator exists, whoever reaches the sign-up page
// first becomes the owner. On a home network that is the person who just
// started the container. On a server that is already reachable from the
// internet it could be a stranger, so the sign-up is only open without a
// code to someone on the home network (see httpsec.Proxies.FromHome). Anyone
// else needs the one-time setup code Mediarium writes to its log every time
// it starts with no administrator (docker logs mediarium). The code lives only
// in memory, is new on every start and is worthless once the account exists.

// setupState holds the code for this run.
type setupState struct {
	mu   sync.Mutex
	code string
}

// setupCodeAlphabet has no 0/O or 1/I/L, so a code read off a log is hard to
// mistype.
const setupCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// setupCodeLength is 12 characters: about 59 bits, and sign-up attempts are
// rate limited on top.
const setupCodeLength = 12

// newSetupCode makes a code like "K7QM-XR2P-9DHT".
func newSetupCode() string {
	b := make([]byte, setupCodeLength)
	if _, err := rand.Read(b); err != nil {
		panic("api: no randomness for the setup code: " + err.Error())
	}
	var sb strings.Builder
	for i, x := range b {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		// 256 is not a multiple of 31, so the first characters are a hair more
		// likely than the last: irrelevant next to 59 bits.
		sb.WriteByte(setupCodeAlphabet[int(x)%len(setupCodeAlphabet)])
	}
	return sb.String()
}

// setupCode returns this run's code, making it on first use.
func (s *Server) setupCode() string {
	st := &s.security.setup
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.code == "" {
		st.code = newSetupCode()
	}
	return st.code
}

// AnnounceSetup writes the setup code to the log when no administrator exists
// yet. main calls it once at start.
func (s *Server) AnnounceSetup() {
	needed, err := s.Auth.FirstRunNeeded()
	if err != nil || !needed {
		return
	}
	slog.Warn("first-run setup: no administrator account exists yet. Open Mediarium in a browser to create it. "+
		"From the home network no code is needed. From anywhere else (for example through a reverse proxy) enter the one-time setup code shown here",
		"setup_code", s.setupCode())
}

// setupNeedsCode reports whether the caller must give the setup code to create
// the first administrator.
func (s *Server) setupNeedsCode(r *http.Request) bool {
	return !s.sec().proxies.FromHome(r)
}

// setupCodeMatches compares what was typed with the code, ignoring case,
// spaces and dashes.
func (s *Server) setupCodeMatches(given string) bool {
	norm := func(v string) string {
		v = strings.ToUpper(v)
		return strings.NewReplacer("-", "", " ", "", "\t", "").Replace(v)
	}
	want := norm(s.setupCode())
	got := norm(given)
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// setupCodeMessage is the answer to a missing or wrong code.
const setupCodeMessage = "This Mediarium has no account yet, and you are not on the home network, so it needs its one-time setup code. " +
	"You'll find it in the Mediarium log (for example, run: docker logs mediarium). Look for \"setup code\"."
