package indexers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Plain-words explanations for two failures that used to show up only as
// technical text: FlareSolverr not answering, and a site that says its page
// is gone.

// flareContactError is a request to FlareSolverr that got no answer at all
// (nothing listening, a wrong address, the connection dropped).
type flareContactError struct{ err error }

func (e *flareContactError) Error() string { return "contact FlareSolverr: " + e.err.Error() }
func (e *flareContactError) Unwrap() error { return e.err }

// FlareSolverrError is a Cloudflare check that FlareSolverr could not pass
// for a site. Address is where FlareSolverr was asked, without any login or
// query part.
type FlareSolverrError struct {
	Site    string
	Address string
	Err     error
}

func (e *FlareSolverrError) Error() string {
	return fmt.Sprintf("%s: FlareSolverr could not pass the Cloudflare check: %v", e.Site, e.Err)
}
func (e *FlareSolverrError) Unwrap() error { return e.Err }

// SafeAddress is a FlareSolverr address as it may be shown: scheme, host and
// port only, so a login or key in the saved address is never printed.
func SafeAddress(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// FlareSolverrDown reports whether err says FlareSolverr could not be reached
// at all, and the address it was tried at.
func FlareSolverrDown(err error) (address string, down bool) {
	var fe *FlareSolverrError
	var ce *flareContactError
	if errors.As(err, &fe) && errors.As(err, &ce) {
		return fe.Address, true
	}
	return "", false
}

// FlareSolverrDownMessage is what to tell the person when FlareSolverr does
// not answer.
func FlareSolverrDownMessage(address string) string {
	if address == "" {
		return "FlareSolverr isn't running or Mediarium can't reach it. Start FlareSolverr, or set its address under Settings > Finding releases > Indexers & Search."
	}
	return fmt.Sprintf("FlareSolverr isn't running or Mediarium can't reach it at %s. Start FlareSolverr, or set its address under Settings > Finding releases > Indexers & Search.", address)
}

// GoneStatus reports whether err is a site answering "not found" or "gone"
// (HTTP 404 or 410), and which of the two.
func GoneStatus(err error) (status int, gone bool) {
	var se *StatusError
	if errors.As(err, &se) && (se.Status == http.StatusNotFound || se.Status == http.StatusGone) {
		return se.Status, true
	}
	return 0, false
}

// GoneMessage is what to tell the person when a site says its page is gone.
func GoneMessage(status int) string {
	return fmt.Sprintf("This site says the page is gone (HTTP %d). The site may have moved or closed. Check its address, or turn it off.", status)
}

// ProblemDetail is the technical text behind ProblemMessage, for the
// "details" line, when ProblemMessage replaced it with an explanation. It is
// empty for every other failure.
func ProblemDetail(o Outcome) string {
	if o.Err == nil {
		return ""
	}
	if _, ok := FlareSolverrDown(o.Err); ok {
		return o.Err.Error()
	}
	if _, ok := GoneStatus(o.Err); ok {
		return o.Err.Error()
	}
	return ""
}
