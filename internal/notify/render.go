package notify

import (
	"bytes"
	"fmt"
	"html"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"
)

// The email is built from one template. Everything that comes from outside
// (titles from indexers, paths, reasons) goes through html.EscapeString, and
// the page loads nothing but the poster picture the person's metadata service
// hosts: no tracking pixels, no scripts, no web fonts.

const (
	brandTeal   = "#34d1bf" // the logo colour
	brandDark   = "#06201d" // text on the teal
	mailInk     = "#1c2b2a"
	mailMuted   = "#5b6b69"
	mailBorder  = "#e2e8e7"
	mailPaper   = "#f3f6f6"
	mailWarning = "#c2410c"
)

// kindAccent is the colour of the thin stripe under the header and of the
// button: the brand teal, or a warm colour for the messages about problems.
func kindAccent(ev Event) string {
	switch EventKind(ev.Type) {
	case EventFailed, EventHealth, EventConflict:
		return mailWarning
	}
	return brandTeal
}

// kindLabel names the event the way Settings does, for the footer.
func kindLabel(ev Event) string {
	if ev.Type == "test" {
		return "test message"
	}
	for _, k := range EventKinds() {
		if k.ID == EventKind(ev.Type) {
			return k.Label
		}
	}
	return ev.Type
}

// httpsURL returns u when it is a web address safe to put in a page, and ""
// otherwise (no javascript: or data: addresses).
func httpsURL(u string) string {
	u = strings.TrimSpace(u)
	low := strings.ToLower(u)
	if strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "http://") {
		if strings.ContainsAny(u, " \t\r\n\"'<>") {
			return ""
		}
		return u
	}
	return ""
}

// RenderEmailHTML draws the message as an HTML page for email clients: a
// table layout with inline styles, which is all they agree on.
func RenderEmailHTML(ev Event) string {
	e := html.EscapeString
	accent := kindAccent(ev)
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light"><title>`)
	b.WriteString(e(ev.Title))
	b.WriteString(`</title></head><body style="margin:0;padding:0;background:` + mailPaper + `;">`)
	// The preview line some clients show next to the subject.
	b.WriteString(`<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent;">` + e(ev.Lead) + `</div>`)
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:` + mailPaper + `;"><tr><td align="center" style="padding:24px 12px;">`)
	b.WriteString(`<table role="presentation" width="560" cellpadding="0" cellspacing="0" style="width:100%;max-width:560px;background:#ffffff;border:1px solid ` + mailBorder + `;border-radius:10px;overflow:hidden;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:` + mailInk + `;">`)

	// Header: the wordmark on the brand teal.
	b.WriteString(`<tr><td style="background:` + brandTeal + `;padding:16px 24px;font-size:20px;font-weight:700;letter-spacing:.2px;color:` + brandDark + `;">Mediarium</td></tr>`)
	b.WriteString(`<tr><td style="height:3px;line-height:3px;font-size:0;background:` + accent + `;">&nbsp;</td></tr>`)

	// Subject, poster and sentence.
	b.WriteString(`<tr><td style="padding:24px 24px 8px 24px;"><table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>`)
	if poster := httpsURL(ev.PosterURL); poster != "" {
		b.WriteString(`<td width="96" valign="top" style="padding-right:16px;width:96px;"><img src="` + e(poster) + `" width="96" alt="" style="display:block;width:96px;height:auto;border-radius:6px;border:0;"></td>`)
	}
	b.WriteString(`<td valign="top"><h1 style="margin:0 0 8px 0;font-size:20px;line-height:1.3;font-weight:700;color:` + mailInk + `;">` + e(ev.Title) + `</h1>`)
	b.WriteString(`<p style="margin:0;font-size:15px;line-height:1.5;color:` + mailInk + `;">` + e(ev.Lead) + `</p></td>`)
	b.WriteString(`</tr></table></td></tr>`)

	// The details table.
	if len(ev.Details) > 0 {
		b.WriteString(`<tr><td style="padding:12px 24px 8px 24px;"><table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-top:1px solid ` + mailBorder + `;">`)
		for _, d := range ev.Details {
			b.WriteString(`<tr><td valign="top" style="padding:8px 12px 8px 0;width:110px;font-size:13px;color:` + mailMuted + `;border-bottom:1px solid ` + mailBorder + `;">` + e(d.Label) + `</td>`)
			b.WriteString(`<td valign="top" style="padding:8px 0;font-size:14px;color:` + mailInk + `;border-bottom:1px solid ` + mailBorder + `;word-break:break-word;">` + e(d.Value) + `</td></tr>`)
		}
		b.WriteString(`</table></td></tr>`)
	}

	// The button.
	if link := httpsURL(ev.LinkURL); link != "" {
		b.WriteString(`<tr><td style="padding:16px 24px 8px 24px;"><a href="` + e(link) + `" style="display:inline-block;padding:11px 20px;background:` + accent + `;color:` + buttonInk(accent) + `;text-decoration:none;font-size:14px;font-weight:600;border-radius:6px;">Open in Mediarium</a></td></tr>`)
	}

	// The footer.
	foot := "This message is about: " + kindLabel(ev) + "."
	b.WriteString(`<tr><td style="padding:20px 24px 24px 24px;font-size:12px;line-height:1.5;color:` + mailMuted + `;">` + e(foot) + ` To change which messages you get, open Settings &gt; Connections &gt; Notifications in Mediarium`)
	if s := httpsURL(ev.SettingsURL); s != "" {
		b.WriteString(` (<a href="` + e(s) + `" style="color:` + mailMuted + `;">open it</a>)`)
	}
	b.WriteString(`.</td></tr>`)

	b.WriteString(`</table></td></tr></table></body></html>`)
	return b.String()
}

// buttonInk is the text colour that reads on the button.
func buttonInk(bg string) string {
	if bg == brandTeal {
		return brandDark
	}
	return "#ffffff"
}

// RenderEmailText is the plain-text alternative of the same message.
func RenderEmailText(ev Event) string {
	var b strings.Builder
	b.WriteString(ev.Title + "\n\n" + ev.Lead + "\n")
	if len(ev.Details) > 0 {
		b.WriteString("\n")
		width := 0
		for _, d := range ev.Details {
			width = max(width, len(d.Label))
		}
		for _, d := range ev.Details {
			b.WriteString(fmt.Sprintf("%-*s  %s\n", width+1, d.Label+":", oneLine(d.Value)))
		}
	}
	if link := httpsURL(ev.LinkURL); link != "" {
		b.WriteString("\nOpen in Mediarium: " + link + "\n")
	}
	b.WriteString("\n-- \nThis message is about: " + kindLabel(ev) + ".\nTo change which messages you get, open Settings > Connections > Notifications in Mediarium")
	if s := httpsURL(ev.SettingsURL); s != "" {
		b.WriteString(": " + s)
	}
	b.WriteString(".\n")
	return b.String()
}

// buildRichMessage renders the RFC 5322 message for a composed event: plain
// text and HTML alternatives, both quoted-printable, CRLF line endings.
func buildRichMessage(from string, to []string, ev Event) []byte {
	const boundary = "=_mediarium_alt_5b1f" // a quoted-printable body can never hold "=_" at a line start
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", oneLine(from))
	h("To", oneLine(strings.Join(to, ", ")))
	h("Subject", mime.QEncoding.Encode("utf-8", oneLine(ev.Title)))
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	h("Date", ts.Format(time.RFC1123Z))
	h("MIME-Version", "1.0")
	h("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	part := func(contentType, body string) {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + contentType + "; charset=\"utf-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")))
		_ = qp.Close()
		b.WriteString("\r\n")
	}
	part("text/plain", RenderEmailText(ev))
	part("text/html", RenderEmailHTML(ev))
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes()
}

// mdEscape keeps a value from being read as Markdown.
func mdEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "#", `\#`, "<", `\<`, ">", `\>`).Replace(oneLine(s))
}

// slackEscape escapes the three characters Slack reads as markup.
func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// eventEmoji names the ntfy tag (an emoji short code) for an event.
func eventTag(ev Event) string {
	switch EventKind(ev.Type) {
	case EventImported:
		return "white_check_mark"
	case EventAdded:
		return "arrow_down"
	case EventFailed:
		return "x"
	case EventConflict:
		return "warning"
	case EventSubtitle:
		return "speech_balloon"
	case EventHealth:
		return "rotating_light"
	case EventUpdate:
		return "package"
	}
	if ev.Type == "test" {
		return "bell"
	}
	return "bell"
}

// eventColor is the side stripe of a Discord embed.
func eventColor(ev Event) int {
	switch EventKind(ev.Type) {
	case EventFailed, EventHealth:
		return 0xc2410c
	case EventConflict:
		return 0xd97706
	}
	return 0x34d1bf
}
