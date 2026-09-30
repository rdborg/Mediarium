package notify

import (
	"encoding/json"
	"net/mail"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// FuzzRenderTemplate: whatever an event says, it stays inside the string it
// was put in, so it can never add fields to a webhook body.
func FuzzRenderTemplate(f *testing.F) {
	f.Add(`{"t":"{{title}}","m":"{{message}}"}`, "Grabbed", "a \"quoted\" line\nand more")
	f.Add(`{"t":"{{title}}"}`, `","admin":true,"x":"`, `\`)
	f.Add(`{"t":"{{title}}"}`, "\u2028\u2029</script>", "\x00\x1f")
	f.Add(`{{title}}`, "x", "y")
	f.Add(`{"a":`, "x", "y")
	f.Fuzz(func(t *testing.T, tmpl, title, message string) {
		ev := Event{Type: "grabbed", Title: title, Message: message, Timestamp: time.Unix(1700000000, 0)}
		out, err := renderTemplate(tmpl, ev)
		if err != nil {
			return
		}
		if !json.Valid([]byte(out)) {
			t.Fatalf("invalid JSON produced: %q", out)
		}
		// the fixed shape: the title is exactly the one value it was placed in
		if utf8.ValidString(title) && !strings.Contains(tmpl, "{{") {
			return
		}
		shape := `{"t":"{{title}}"}`
		out, err = renderTemplate(shape, ev)
		if err != nil {
			t.Fatalf("fixed shape refused: %v", err)
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 1 {
			t.Fatalf("title %q broke out of its string: %q (%v)", title, out, err)
		}
		if utf8.ValidString(title) && got["t"] != title {
			t.Fatalf("title changed: %q -> %q", title, got["t"])
		}
	})
}

// FuzzBuildMessage: text from an event or a setting can never add a header
// line to an e-mail.
func FuzzBuildMessage(f *testing.F) {
	f.Add("Mediarium <me@example.com>", "you@example.com", "Grabbed\r\nBcc: evil@example.com", "body\r\n.\r\nQUIT")
	f.Add("a@b", "c@d, e@f", "\u2028 x", "")
	f.Add("\r\nX: y", "z", "t", "m")
	f.Fuzz(func(t *testing.T, from, to, title, message string) {
		raw := buildMessage(from, strings.Split(to, ","), Event{Title: title, Message: message, Timestamp: time.Unix(1700000000, 0)})
		text := string(raw)
		head, _, ok := strings.Cut(text, "\r\n\r\n")
		if !ok {
			t.Fatalf("no end of headers in %q", text)
		}
		lines := strings.Split(head, "\r\n")
		want := []string{"From", "To", "Subject", "Date", "MIME-Version", "Content-Type", "Content-Transfer-Encoding"}
		if len(lines) != len(want) {
			t.Fatalf("%d header lines, want %d: %q", len(lines), len(want), head)
		}
		for i, l := range lines {
			if !strings.HasPrefix(l, want[i]+": ") {
				t.Fatalf("header %d is %q, want %s", i, l, want[i])
			}
		}
		if _, err := mail.ReadMessage(strings.NewReader(text)); err != nil {
			t.Fatalf("not a readable message: %v\n%q", err, text)
		}
	})
}
