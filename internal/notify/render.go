package notify

import (
	"bytes"
	"encoding/base64"
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
	b.WriteString(`<table role="presentation" width="680" cellpadding="0" cellspacing="0" style="width:100%;max-width:680px;background:#ffffff;border:1px solid ` + mailBorder + `;border-radius:12px;overflow:hidden;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:` + mailInk + `;">`)

	// Header: the full Mediarium logo (sent inside the email, so nothing is
	// fetched from anywhere) over a stripe in the colour of the message.
	b.WriteString(`<tr><td style="padding:22px 32px 18px 32px;background:#ffffff;"><img src="cid:` + logoCID + `" width="200" alt="Mediarium" style="display:block;width:200px;height:auto;border:0;"></td></tr>`)
	b.WriteString(`<tr><td style="height:4px;line-height:4px;font-size:0;background:` + accent + `;">&nbsp;</td></tr>`)

	// Body: the poster on one half, the words and the facts stacked on the other.
	b.WriteString(`<tr><td style="padding:28px 32px 8px 32px;"><table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>`)
	poster := httpsURL(ev.PosterURL)
	if poster != "" {
		b.WriteString(`<td width="300" valign="top" style="width:300px;padding-right:28px;"><img src="` + e(poster) + `" width="300" alt="" style="display:block;width:100%;max-width:300px;height:auto;border-radius:10px;border:0;"></td>`)
	}
	b.WriteString(`<td valign="top">`)
	b.WriteString(`<h1 style="margin:0 0 10px 0;font-size:24px;line-height:1.25;font-weight:700;color:` + mailInk + `;">` + e(ev.Title) + `</h1>`)
	b.WriteString(`<p style="margin:0 0 18px 0;font-size:16px;line-height:1.5;color:` + mailInk + `;">` + e(ev.Lead) + `</p>`)
	for _, d := range ev.Details {
		b.WriteString(`<div style="padding:10px 0;border-top:1px solid ` + mailBorder + `;"><div style="font-size:12px;font-weight:700;letter-spacing:.3px;color:` + mailMuted + `;margin-bottom:3px;">` + e(d.Label) + `</div>`)
		b.WriteString(`<div style="font-size:15px;line-height:1.45;color:` + mailInk + `;word-break:break-word;">` + e(d.Value) + `</div></div>`)
	}
	b.WriteString(`</td></tr></table></td></tr>`)

	// The button.
	if link := httpsURL(ev.LinkURL); link != "" {
		b.WriteString(`<tr><td style="padding:18px 32px 8px 32px;"><a href="` + e(link) + `" style="display:inline-block;padding:13px 26px;background:` + accent + `;color:` + buttonInk(accent) + `;text-decoration:none;font-size:15px;font-weight:700;border-radius:8px;">Open in Mediarium</a></td></tr>`)
	}

	// The footer: only says where to change what gets sent.
	b.WriteString(`<tr><td style="padding:22px 32px 28px 32px;font-size:12px;line-height:1.5;color:` + mailMuted + `;">`)
	if s := httpsURL(ev.SettingsURL); s != "" {
		b.WriteString(`You can choose which emails you get in <a href="` + e(s) + `" style="color:` + mailMuted + `;">Notification settings</a>.`)
	} else {
		b.WriteString(`You can choose which emails you get in Mediarium, under Settings &gt; Connections &gt; Notifications.`)
	}
	b.WriteString(`</td></tr>`)

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
	b.WriteString("\n-- \nYou can choose which emails you get in Mediarium, under Settings > Connections > Notifications")
	if s := httpsURL(ev.SettingsURL); s != "" {
		b.WriteString(": " + s)
	}
	b.WriteString(".\n")
	return b.String()
}

// buildRichMessage renders the RFC 5322 message for a composed event: plain
// text and HTML alternatives, both quoted-printable, CRLF line endings.
func buildRichMessage(from string, to []string, ev Event) []byte {
	// Boundaries: a quoted-printable body can never hold "=_" at a line start.
	const (
		altBoundary = "=_mediarium_alt_5b1f"
		relBoundary = "=_mediarium_rel_5b1f"
	)
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
	h("Content-Type", `multipart/alternative; boundary="`+altBoundary+`"`)
	b.WriteString("\r\n")
	text := func(contentType, body string) {
		b.WriteString("Content-Type: " + contentType + "; charset=\"utf-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")))
		_ = qp.Close()
		b.WriteString("\r\n")
	}
	b.WriteString("--" + altBoundary + "\r\n")
	text("text/plain", RenderEmailText(ev))
	// The HTML page travels with its logo, so the email loads nothing from the
	// web but the poster.
	b.WriteString("--" + altBoundary + "\r\n")
	b.WriteString(`Content-Type: multipart/related; type="text/html"; boundary="` + relBoundary + `"` + "\r\n\r\n")
	b.WriteString("--" + relBoundary + "\r\n")
	text("text/html", RenderEmailHTML(ev))
	b.WriteString("--" + relBoundary + "\r\n")
	b.WriteString("Content-Type: image/png; name=\"mediarium-logo.png\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("Content-ID: <" + logoCID + ">\r\n")
	b.WriteString("Content-Disposition: inline; filename=\"mediarium-logo.png\"\r\n\r\n")
	enc := base64.StdEncoding.EncodeToString(logoPNG)
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	b.WriteString("--" + relBoundary + "--\r\n")
	b.WriteString("--" + altBoundary + "--\r\n")
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
