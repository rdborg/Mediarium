package notify_test

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/notify"
)

var composeAt = time.Date(2026, 9, 30, 14, 5, 0, 0, time.UTC)

var titanic = notify.Item{
	Media: "movie", Title: "Titanic", Year: 1997, Quality: "Bluray-1080p", SizeBytes: 8_800_000_000,
	Path: "/data/Movies/Titanic (1997)/Titanic (1997).mkv", Release: "Titanic.1997.1080p.BluRay.x264-GRP",
	PosterURL: "https://image.tmdb.org/t/p/w185/abc.jpg", LinkPath: "/title/597",
}

func TestComposeSubjects(t *testing.T) {
	links := notify.Links{Base: "https://mediarium.example.com/"}
	failed := titanic
	failed.Reason = "The disk is full."
	episode := notify.Item{Media: "episode", Title: "Severance", Year: 2022, Episode: "S02E03", Quality: "WEBDL-1080p", LinkPath: "/series/4"}
	pack := notify.Item{Media: "show", Title: "Severance", Year: 2022, Episode: "Season 2", LinkPath: "/series/4"}
	album := notify.Item{Media: "album", Title: "Radiohead - OK Computer", Year: 1997, LinkPath: "/music/artist/2"}
	sub := notify.Item{Media: "movie", Title: "Titanic", Year: 1997, Language: "English", Path: "/data/Movies/Titanic (1997)/Titanic (1997).en.srt"}

	tests := []struct {
		name      string
		event     string
		item      notify.Item
		subject   string
		wantLead  string
		wantLabel []string
	}{
		{"movie imported", "imported", titanic, "Titanic (1997) is ready to watch", "was downloaded and added to your library", []string{"Title", "Year", "Quality", "Size", "Saved to", "Time"}},
		{"episode imported", "imported", episode, "Severance S02E03 is ready to watch", "Severance S02E03", []string{"Episode"}},
		{"season pack imported", "imported", pack, "Severance (2022), season 2 is ready to watch", "", []string{"Episode"}},
		{"album imported", "imported", album, "Radiohead - OK Computer (1997) is ready to listen to", "", nil},
		{"grabbed", "grabbed", titanic, "Downloading Titanic (1997)", "added it to the downloads", []string{"Release"}},
		{"failed", "failed", failed, "Download failed: Titanic (1997)", "did not work", []string{"Reason", "Release"}},
		{"conflict", "conflict", titanic, "Needs your decision: Titanic (1997)", "Choose what to do in Activity", []string{"Existing file"}},
		{"subtitle", "subtitle", sub, "Subtitles added: Titanic (1997)", "Added English subtitles for Titanic (1997)", []string{"Language"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := notify.Compose(tc.event, tc.item, links, composeAt)
			if ev.Title != tc.subject {
				t.Errorf("subject %q, want %q", ev.Title, tc.subject)
			}
			if !strings.Contains(ev.Lead, tc.wantLead) {
				t.Errorf("lead %q does not contain %q", ev.Lead, tc.wantLead)
			}
			have := map[string]string{}
			for _, d := range ev.Details {
				if d.Value == "" {
					t.Errorf("empty row %q was kept", d.Label)
				}
				have[d.Label] = d.Value
			}
			for _, l := range tc.wantLabel {
				if have[l] == "" {
					t.Errorf("no %q row in %v", l, ev.Details)
				}
			}
			if !strings.HasPrefix(ev.Message, ev.Lead) {
				t.Errorf("plain message %q does not start with the lead", ev.Message)
			}
			if ev.LinkURL != "https://mediarium.example.com"+tc.item.LinkPath && tc.item.LinkPath != "" {
				t.Errorf("link %q", ev.LinkURL)
			}
		})
	}
}

func TestComposeDetailsFormatting(t *testing.T) {
	ev := notify.Compose("imported", titanic, notify.Links{}, composeAt)
	got := map[string]string{}
	for _, d := range ev.Details {
		got[d.Label] = d.Value
	}
	if got["Size"] != "8.2 GB" || got["Year"] != "1997" || got["Time"] != "30 Sep 2026, 14:05" || got["Saved to"] != titanic.Path {
		t.Fatalf("rows %v", got)
	}
	if ev.LinkURL != "" || ev.SettingsURL != "" {
		t.Fatalf("no address was set, yet links %q %q", ev.LinkURL, ev.SettingsURL)
	}
}

func TestComposeTextSubjects(t *testing.T) {
	links := notify.Links{Base: "https://m.example.com"}
	tests := []struct {
		name, event, title, message string
		subject                     string
		link                        string
	}{
		{"test", "test", "anything", "", "Mediarium test message", ""},
		{"login refused", "health", "Your Usenet login was refused", "Check the password.", "Mediarium needs attention: your Usenet login was refused", "https://m.example.com/settings/logs"},
		{"named provider keeps its capital", "health", "Eweka is not working", "Timed out.", "Mediarium needs attention: Eweka is not working", "https://m.example.com/settings/logs"},
		{"recovered", "health", "Eweka is working again", "The last check passed.", "Mediarium: Eweka is working again", ""},
		{"update", "update", "Mediarium 1.3.0 is available", "You are running 1.2.0.", "Mediarium 1.3.0 is available", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := notify.ComposeText(tc.event, tc.title, tc.message, links, composeAt)
			if ev.Title != tc.subject || ev.LinkURL != tc.link {
				t.Fatalf("got subject %q link %q, want %q %q", ev.Title, ev.LinkURL, tc.subject, tc.link)
			}
			if ev.Lead == "" || ev.Message == "" {
				t.Fatalf("no body: %+v", ev)
			}
		})
	}
}

// The HTML is one template for every event. Whatever comes from outside is
// escaped, and nothing but the poster is loaded from anywhere.
func TestEmailHTMLEscapesAndLoadsNothingElse(t *testing.T) {
	links := notify.Links{Base: "https://m.example.com"}
	evil := titanic
	evil.Title = `<script>alert(1)</script> & "Friends"`
	evil.Path = `/data/<img src=x onerror=alert(2)>/file.mkv`
	evil.Release = "Rel'ease\"><b>"
	ev := notify.Compose("imported", evil, links, composeAt)
	out := notify.RenderEmailHTML(ev)

	for _, bad := range []string{"<script", "<img src=x", "<b>\"", "javascript:", "<iframe", "<link ", "@import", "url("} {
		if strings.Contains(out, bad) {
			t.Errorf("the page contains %q", bad)
		}
	}
	if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt; &amp; &#34;Friends&#34;") {
		t.Errorf("title not escaped in %s", out)
	}
	srcs := regexp.MustCompile(`(?i)\b(?:src|background|href)="([^"]*)"`).FindAllStringSubmatch(out, -1)
	for _, m := range srcs {
		u := m[1]
		if u != titanic.PosterURL && u != "https://m.example.com/title/597" && u != "https://m.example.com/settings/notifications" {
			t.Errorf("unexpected address in the page: %s", u)
		}
	}
	if n := strings.Count(out, "<img"); n != 1 {
		t.Errorf("%d images, want only the poster", n)
	}
	if strings.Contains(out, `width="1"`) || strings.Contains(out, `height="1"`) {
		t.Error("looks like a tracking pixel")
	}
	for _, want := range []string{"Mediarium</td>", "#34d1bf", "Open in Mediarium", "You can choose which emails you get", "<table"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestEmailHTMLLeavesOutWhatIsNotThere(t *testing.T) {
	plain := titanic
	plain.PosterURL, plain.LinkPath = "", ""
	out := notify.RenderEmailHTML(notify.Compose("imported", plain, notify.Links{}, composeAt))
	if strings.Contains(out, "<img") || strings.Contains(out, "Open in Mediarium") || strings.Contains(out, "open it") {
		t.Errorf("poster or links present without an address: %s", out)
	}

	unsafe := titanic
	unsafe.PosterURL = "javascript:alert(1)"
	out = notify.RenderEmailHTML(notify.Compose("imported", unsafe, notify.Links{}, composeAt))
	if strings.Contains(out, "javascript:") || strings.Contains(out, "<img") {
		t.Errorf("an unsafe poster address got in: %s", out)
	}

	// The messages about problems are warm, not teal, under the header.
	failed := notify.RenderEmailHTML(notify.Compose("failed", titanic, notify.Links{}, composeAt))
	if !strings.Contains(failed, "#c2410c") {
		t.Error("a failure should use the warning colour")
	}
}

func TestEmailTextAlternative(t *testing.T) {
	ev := notify.Compose("failed", titanic, notify.Links{Base: "https://m.example.com"}, composeAt)
	out := notify.RenderEmailText(ev)
	for _, want := range []string{"Download failed: Titanic (1997)", "Release:", "Open in Mediarium: https://m.example.com/title/597", "You can choose which emails you get", "https://m.example.com/settings/notifications"} {
		if !strings.Contains(out, want) {
			t.Errorf("text missing %q:\n%s", want, out)
		}
	}
	if strings.ContainsAny(out, "<>") && strings.Contains(out, "<td") {
		t.Errorf("markup in the text version:\n%s", out)
	}
}

func TestEmailIsMultipartWithBothParts(t *testing.T) {
	f := startFakeSMTP(t, &fakeSMTP{})
	ev := notify.Compose("imported", titanic, notify.Links{Base: "https://m.example.com"}, composeAt)
	ev.Title = "Titanic (1997) is ready to watch ☃"
	if err := f.sender("none").Send(context.Background(), ev); err != nil {
		t.Fatalf("send: %v", err)
	}
	f.mu.Lock()
	data := f.data
	f.mu.Unlock()

	head, body, _ := strings.Cut(data, "\r\n\r\n")
	var subject, ctype string
	for _, l := range strings.Split(head, "\r\n") {
		switch {
		case strings.HasPrefix(l, "Subject: "):
			subject, _ = new(mime.WordDecoder).DecodeHeader(strings.TrimPrefix(l, "Subject: "))
		case strings.HasPrefix(l, "Content-Type: "):
			ctype = strings.TrimPrefix(l, "Content-Type: ")
		}
	}
	if subject != ev.Title {
		t.Errorf("subject %q, want %q", subject, ev.Title)
	}
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil || mt != "multipart/alternative" {
		t.Fatalf("content type %q: %v", ctype, err)
	}
	mr := multipart.NewReader(strings.NewReader(body), params["boundary"])
	got := map[string]string{}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(quotedprintable.NewReader(p))
		got[strings.SplitN(p.Header.Get("Content-Type"), ";", 2)[0]] = string(b)
	}
	if !strings.Contains(got["text/plain"], "Saved to:") || strings.Contains(got["text/plain"], "<table") {
		t.Errorf("plain part wrong: %q", got["text/plain"])
	}
	if !strings.Contains(got["text/html"], "<table") || !strings.Contains(got["text/html"], "Titanic (1997) is ready to watch ☃") {
		t.Errorf("html part wrong: %q", got["text/html"])
	}
}

// Every other channel gets the same clearer subject and the details.
func TestRichPayloadsPerChannel(t *testing.T) {
	ev := notify.Compose("imported", titanic, notify.Links{Base: "https://m.example.com"}, composeAt)
	ev.Title = "@everyone " + ev.Title // a title from outside must not ping or break markup
	ev2 := ev

	capture := func(t *testing.T, send func(url string) error) map[string]any {
		t.Helper()
		var got map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&got)
		}))
		defer srv.Close()
		if err := send(srv.URL); err != nil {
			t.Fatalf("send: %v", err)
		}
		return got
	}
	ctx := context.Background()

	t.Run("discord embed", func(t *testing.T) {
		got := capture(t, func(u string) error { return notify.NewDiscordSender(u).Send(ctx, ev) })
		embed := got["embeds"].([]any)[0].(map[string]any)
		if embed["title"] != ev.Title || embed["url"] != "https://m.example.com/title/597" {
			t.Errorf("embed %v", embed)
		}
		if embed["thumbnail"].(map[string]any)["url"] != titanic.PosterURL {
			t.Errorf("thumbnail %v", embed["thumbnail"])
		}
		names := map[string]bool{}
		for _, f := range embed["fields"].([]any) {
			names[f.(map[string]any)["name"].(string)] = true
		}
		if !names["Quality"] || !names["Size"] || names["Time"] {
			t.Errorf("fields %v", names)
		}
		if len(got["allowed_mentions"].(map[string]any)["parse"].([]any)) != 0 {
			t.Error("mentions are allowed")
		}
	})
	t.Run("slack blocks", func(t *testing.T) {
		bad := ev2
		bad.Lead = "a <b> & c"
		got := capture(t, func(u string) error { return notify.NewSlackSender(u).Send(ctx, bad) })
		blocks := got["blocks"].([]any)
		if blocks[0].(map[string]any)["type"] != "header" || got["text"] != bad.Title {
			t.Errorf("blocks %v", blocks)
		}
		lead := blocks[1].(map[string]any)["text"].(map[string]any)["text"]
		if lead != "a &lt;b&gt; &amp; c" {
			t.Errorf("lead not escaped for Slack: %q", lead)
		}
		raw, _ := json.Marshal(blocks)
		if !strings.Contains(string(raw), `"url":"https://m.example.com/title/597"`) {
			t.Errorf("button missing: %s", raw)
		}
	})
	t.Run("telegram html", func(t *testing.T) {
		bad := ev2
		bad.Title = "<b>x</b> & y"
		got := capture(t, func(u string) error {
			return notify.NewTelegramSenderWithBaseURL("tok", "1", u).Send(ctx, bad)
		})
		text := got["text"].(string)
		if got["parse_mode"] != "HTML" || !strings.HasPrefix(text, "<b>&lt;b&gt;x&lt;/b&gt; &amp; y</b>") || !strings.Contains(text, "<b>Quality:</b> Bluray-1080p") {
			t.Errorf("telegram body %q", text)
		}
	})
	t.Run("ntfy tags and click", func(t *testing.T) {
		got := capture(t, func(u string) error { return notify.NewNtfySender(u, "topic", "").Send(ctx, ev) })
		tags := got["tags"].([]any)
		if len(tags) != 1 || tags[0] != "white_check_mark" || got["click"] != "https://m.example.com/title/597" || got["icon"] != titanic.PosterURL {
			t.Errorf("ntfy body %v", got)
		}
		if !strings.Contains(got["message"].(string), "Quality: Bluray-1080p") {
			t.Errorf("message %q", got["message"])
		}
	})
	t.Run("gotify markdown", func(t *testing.T) {
		got := capture(t, func(u string) error { return notify.NewGotifySender(u, "tok").Send(ctx, ev) })
		extras := got["extras"].(map[string]any)
		if extras["client::display"].(map[string]any)["contentType"] != "text/markdown" {
			t.Errorf("extras %v", extras)
		}
		if !strings.Contains(got["message"].(string), "**Quality:** Bluray\\-1080p") && !strings.Contains(got["message"].(string), "**Quality:** Bluray-1080p") {
			t.Errorf("message %q", got["message"])
		}
	})
	t.Run("pushover html and link", func(t *testing.T) {
		got := capture(t, func(u string) error { return notify.NewPushoverSenderWithBaseURL("u", "t", u).Send(ctx, ev) })
		if got["html"] != float64(1) || got["url"] != "https://m.example.com/title/597" || !strings.Contains(got["message"].(string), "<b>Size:</b> 8.2 GB") {
			t.Errorf("pushover body %v", got)
		}
	})
	t.Run("webhook keeps its fields and adds the rest", func(t *testing.T) {
		got := capture(t, func(u string) error { return notify.NewWebhookSender(u).Send(ctx, ev) })
		if got["event"] != "imported" || got["title"] != ev.Title || got["link"] != "https://m.example.com/title/597" || len(got["details"].([]any)) == 0 {
			t.Errorf("webhook body %v", got)
		}
	})
}
