// Package logbuf keeps the most recent lines the app logged, in memory, with
// secrets taken out, so the Settings page can hand them to a person for a
// support request without a trip to the NAS log viewer.
package logbuf

import (
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/rdborg/mediarium/internal/netguard"
)

// Lines is how many log lines Default keeps.
const Lines = 500

// maxLine cuts an absurdly long line, so a runaway message cannot fill memory.
const maxLine = 2000

// Default is the buffer the app's log output is copied into.
var Default = New(Lines)

// Buffer is a ring of the last n log lines. It is an io.Writer, so it can sit
// next to stderr in log.SetOutput. Every line is scrubbed of secrets as it
// comes in, so nothing sensitive is ever held here.
type Buffer struct {
	mu      sync.Mutex
	lines   []string
	next    int
	full    bool
	partial string // the start of a line whose end has not arrived yet
}

// New returns a buffer that keeps the last n lines.
func New(n int) *Buffer {
	if n < 1 {
		n = 1
	}
	return &Buffer{lines: make([]string, n)}
}

// Write splits p into lines and keeps them. It never fails.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := b.partial + string(p)
	b.partial = ""
	for {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			break
		}
		b.add(text[:i])
		text = text[i+1:]
	}
	if len(text) > maxLine {
		b.add(text)
		text = ""
	}
	b.partial = text
	return len(p), nil
}

func (b *Buffer) add(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	if len(line) > maxLine {
		line = line[:maxLine] + "..."
	}
	b.lines[b.next] = Scrub(line)
	b.next++
	if b.next == len(b.lines) {
		b.next, b.full = 0, true
	}
}

// Lines returns the kept lines, oldest first.
func (b *Buffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.full {
		return append([]string(nil), b.lines[:b.next]...)
	}
	out := make([]string, 0, len(b.lines))
	out = append(out, b.lines[b.next:]...)
	return append(out, b.lines[:b.next]...)
}

var (
	urlRe = regexp.MustCompile(`https?://[^\s"'<>]+`)
	// scheme://anything@ : the login part of an address of any kind (a password
	// may itself contain a slash, which no URL parser accepts, so this is text).
	userinfoRe = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s@"'<>]*@`)
	// magnet:?xt=...&tr=... : trackers in a magnet link often carry a passkey.
	magnetRe = regexp.MustCompile(`(?i)magnet:\?[^\s"'<>]+`)
	// name=value, name: value and "name":"value" for names that hold secrets.
	// The name may be the end of a longer one (tmdb_api_key, x-plex-token,
	// newznab_apikey, PrivateKey). A quoted value is taken whole, spaces
	// included.
	pairRe = regexp.MustCompile(`(?i)([A-Za-z0-9_.-]*(?:api[_-]?key|token|password|passwd|pwd|secret|passkey|rsskey|authkey|torrent[_-]?pass|(?:private|preshared)[_-]?key)["']?\s*[:=]\s*)` +
		`("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|(?:(?:bearer|basic|digest)\s+)?[^\s"'&,;]+)`)
	// Whole header lines: the value of these can hold several parts.
	headerRe = regexp.MustCompile(`(?i)((?:proxy-)?authorization|set-cookie|cookie)(["']?\s*[:=]\s*)[^\r\n]+`)
	// "Bearer abc..." and "Basic abc..." headers.
	schemeRe = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`)
	// Long hexadecimal strings: keys, hashes and session tokens.
	hexRe = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	// Addresses that are the whole credential: a Telegram bot token, a Discord
	// or Slack webhook, a generic webhook id.
	telegramRe = regexp.MustCompile(`/bot\d+:[A-Za-z0-9_-]+`)
	webhookRe  = regexp.MustCompile(`(?i)(/api/webhooks/|hooks\.slack\.com/services/|/webhooks?/)[^\s/?"'<>]+(?:/[^\s?"'<>]+)?`)
	// The first part of the value of a "xt" magnet parameter is all worth keeping.
	xtRe = regexp.MustCompile(`(?i)xt=urn:btih:[0-9a-z]+`)
)

const redacted = "REDACTED"

// Scrub takes secrets out of one log line: passwords, API keys and tokens
// given as name=value, the query and login parts of URLs, authorization
// headers and long hexadecimal strings. It is deliberately greedy: a line that
// loses a harmless word is better than one that leaks a key.
func Scrub(line string) string {
	line = magnetRe.ReplaceAllStringFunc(line, func(m string) string {
		if xt := xtRe.FindString(m); xt != "" {
			return "magnet:?" + xt + "&REDACTED"
		}
		return "magnet:?" + redacted
	})
	line = userinfoRe.ReplaceAllString(line, "${1}"+redacted+"@")
	line = urlRe.ReplaceAllStringFunc(line, func(u string) string {
		// A trailing full stop or bracket belongs to the sentence, not the URL.
		trail := ""
		for len(u) > 0 && strings.ContainsRune(".,;:)]}", rune(u[len(u)-1])) {
			trail = string(u[len(u)-1]) + trail
			u = u[:len(u)-1]
		}
		return netguard.RedactURL(u) + trail
	})
	line = telegramRe.ReplaceAllString(line, "/bot"+redacted)
	line = webhookRe.ReplaceAllString(line, "${1}"+redacted)
	line = headerRe.ReplaceAllString(line, "${1}${2}"+redacted)
	line = pairRe.ReplaceAllString(line, "${1}"+redacted)
	line = schemeRe.ReplaceAllString(line, "${1} "+redacted)
	line = hexRe.ReplaceAllString(line, redacted)
	return line
}

// ScrubWriter passes what it is given on to W with secrets taken out (see
// Scrub). Wrap the process's stderr in it so what `docker logs` shows, which
// people paste into bug reports, is as clean as the in-memory copy. Each Write
// is taken to be whole log lines, which is how the log packages write.
type ScrubWriter struct{ W io.Writer }

// Write scrubs p line by line and writes the result. It reports len(p) written
// when the underlying writer took the scrubbed text, whatever its length.
func (s ScrubWriter) Write(p []byte) (int, error) {
	text := string(p)
	var b strings.Builder
	for len(text) > 0 {
		line, rest, hasNL := strings.Cut(text, "\n")
		b.WriteString(Scrub(line))
		if hasNL {
			b.WriteByte('\n')
		}
		text = rest
	}
	if _, err := io.WriteString(s.W, b.String()); err != nil {
		return 0, err
	}
	return len(p), nil
}
