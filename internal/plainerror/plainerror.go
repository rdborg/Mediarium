// Package plainerror turns the raw text of network and file errors into a
// sentence a person can read. A download that fails on a DNS lookup should
// say "The address could not be found", not
// `Get "https://x/y.nzb": dial tcp: lookup x on 10.0.0.1:53: no such host`.
//
// Only the raw part of an error is rewritten. The app's own phrases in front
// of it ("couldn't get the NZB file") are kept, and an error that holds no raw
// part at all is returned exactly as it is. The raw text belongs in a log or
// in the technical detail of Settings > System > Logs and errors, never on a
// button, a toast or a table the person reads first.
package plainerror

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"syscall"
)

// Message describes err in plain words. A nil error gives "".
func Message(err error) string {
	if err == nil {
		return ""
	}
	return rewrite(err.Error(), err)
}

// Text does the same for an error that was stored as text. Running it over a
// message that is already plain leaves the message unchanged.
func Text(msg string) string {
	return rewrite(msg, nil)
}

// rawStart matches the first words of the errors that Go's network and file
// packages produce. Everything from the first segment that matches is "raw".
var rawStart = regexp.MustCompile(`(?i)^(` +
	`(get|post|put|head|delete|patch) "?https?://` + // net/http: Get "https://…"
	`|dial (tcp|udp|unix)` +
	`|lookup [^ ]+` +
	`|(read|write) (tcp|udp|unix)` +
	`|(open|mkdir|mkdirall|stat|lstat|rename|remove|readdir|readdirent|chmod|chown|symlink|link|truncate|copy|create|write|read) [^ ]*[/\\]` +
	`|context (canceled|deadline exceeded)` +
	`|tls:|x509:|net/http:|http2:` +
	`|i/o timeout|no such host|connection (refused|reset)|unexpected eof|eof$` +
	`|the server answered with status \d{3}` +
	`|status \d{3}` +
	`|http (error )?\d{3}` +
	`)`)

var statusCode = regexp.MustCompile(`(?i)(?:status|http(?: error)?|error|returned|answered with status|code)[ :]*(\d{3})\b`)

// rewrite keeps the app's own leading phrases and swaps the raw rest for one
// plain sentence.
func rewrite(msg string, err error) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return msg
	}
	parts := strings.Split(msg, ": ")
	raw := -1
	for i, p := range parts {
		if rawStart.MatchString(strings.TrimSpace(p)) {
			raw = i
			break
		}
	}
	if raw < 0 {
		return msg
	}
	reason := reasonFor(strings.Join(parts[raw:], ": "), err)
	if reason == "" {
		return msg
	}
	lead := leadIn(parts[:raw])
	if lead == "" {
		return reason
	}
	return lead + ". " + reason
}

// leadIn joins the app's own phrases, dropping the generic "the download
// failed" when something more specific follows it.
func leadIn(parts []string) string {
	var keep []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		keep = append(keep, p)
	}
	if len(keep) > 1 {
		var specific []string
		for _, p := range keep {
			if !strings.EqualFold(p, "the download failed") {
				specific = append(specific, p)
			}
		}
		keep = specific
	}
	if len(keep) == 0 {
		return ""
	}
	out := strings.Join(keep, ": ")
	out = strings.TrimRight(out, ". ")
	return strings.ToUpper(out[:1]) + out[1:]
}

// reasonFor picks the sentence for the raw text (and, when it is still
// wrapped, for the error's own type).
func reasonFor(raw string, err error) string {
	low := strings.ToLower(raw)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(low, w) {
				return true
			}
		}
		return false
	}
	is := func(target error) bool { return err != nil && errors.Is(err, target) }
	switch {
	case is(context.Canceled) || has("context canceled"):
		return "It was stopped before it finished."
	case is(syscall.ENOSPC) || has("no space left", "disk full", "not enough space", "disk quota exceeded"):
		return "The disk is full. Free up some space and try again."
	case is(fs.ErrPermission) || has("permission denied", "access is denied", "operation not permitted", "read-only file system"):
		return "Mediarium is not allowed to use that file or folder. Check its permissions."
	case has("no such host", "server misbehaving", "temporary failure in name resolution", "name or service not known", "no address associated"):
		return "The address could not be found. Check the address and your internet connection."
	case has("connection refused"):
		return "The server refused the connection. Check it's running and the address and port are right."
	case is(context.DeadlineExceeded) || has("i/o timeout", "timed out", "deadline exceeded", "timeout"):
		return "The server took too long to answer. Try again in a few minutes."
	case has("x509:", "tls:", "certificate"):
		return "The secure connection could not be set up. The server's certificate may be wrong or expired."
	case has("network is unreachable", "no route to host", "network is down"):
		return "The network could not be reached. Check your internet connection."
	case has("connection reset", "broken pipe", "unexpected eof") || low == "eof" || strings.HasSuffix(low, ": eof"):
		return "The connection was dropped. Try again in a few minutes."
	}
	if m := statusCode.FindStringSubmatch(raw); m != nil {
		switch code := m[1]; {
		case code == "401" || code == "403":
			return "The site refused the login or key (error " + code + ")."
		case code == "404" || code == "410":
			return "The site no longer has that file (error " + code + ")."
		case code == "429":
			return "The site says there were too many requests. Wait a while and try again."
		case strings.HasPrefix(code, "5"):
			return "The site had a problem on its side (error " + code + "). Try again later."
		default:
			return "The site answered with an error (" + code + ")."
		}
	}
	if is(fs.ErrNotExist) || has("no such file or directory", "cannot find the path", "cannot find the file", "the system cannot find", "file does not exist") {
		return "A file or folder could not be found."
	}
	return ""
}

// internalMarkers are words that only appear in errors of the app's own
// plumbing (the database, JSON, a crash), never in a sentence written for a
// person.
var internalMarkers = []string{
	"sql:", "sqlite", "constraint failed", "database is locked", "no such table", "no such column",
	"json:", "invalid character", "unexpected end of json", "cannot unmarshal", "runtime error", "nil pointer",
	"panic:", "goroutine ",
}

// Generic is what a person sees when the real error is only useful in the
// log. It is a full sentence, so callers can show it as it is.
const Generic = "Something went wrong inside Mediarium. Try again. If it keeps happening, an administrator can see why in Settings > System > Logs and errors."

// ForResponse makes the text of a server error fit to show in the page. Network
// and file errors become plain sentences; the app's plumbing errors become
// Generic. Text that is already a sentence for a person is returned as it is.
// The second result says whether the text was replaced, so the caller can log
// the original.
func ForResponse(msg string) (string, bool) {
	out := rewrite(msg, nil)
	if out != msg {
		return out, true
	}
	low := strings.ToLower(msg)
	for _, m := range internalMarkers {
		if strings.Contains(low, m) {
			return Generic, true
		}
	}
	return msg, false
}
