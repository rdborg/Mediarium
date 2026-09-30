package notify

import (
	"fmt"
	"strings"
	"time"
)

// Detail is one row of the small table that goes with a message.
type Detail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Item is what a download-related event is about. Compose turns it into the
// subject, the sentence and the detail rows every channel shows.
type Item struct {
	Media     string // "movie" | "show" | "episode" | "album"; anything else reads as a movie
	Title     string // "Titanic", "Severance", "Radiohead - OK Computer"
	Year      int    // 0 when unknown
	Episode   string // "S02E03" or "Season 2"; empty for a movie or a whole show
	Quality   string // "Bluray-1080p"
	SizeBytes int64  // 0 when unknown
	Path      string // where the file was saved
	Release   string // the release name that was grabbed
	Reason    string // why a download failed, in plain words
	Language  string // a subtitle's language
	PosterURL string // a hosted poster image, or empty
	LinkPath  string // where the title lives in Mediarium, like /title/597
}

// Name is how the item reads in a subject: "Titanic (1997)" or
// "Severance S02E03".
func (it Item) Name() string {
	switch {
	case it.Episode != "" && it.Media == "episode":
		return it.Title + " " + it.Episode
	case it.Episode != "":
		return fmt.Sprintf("%s, %s", it.withYear(), strings.ToLower(it.Episode[:1])+it.Episode[1:])
	}
	return it.withYear()
}

func (it Item) withYear() string {
	if it.Year > 0 {
		return fmt.Sprintf("%s (%d)", it.Title, it.Year)
	}
	return it.Title
}

// Links holds the addresses the person set for Mediarium. Empty means no
// links are added to messages.
type Links struct {
	Base string // like https://mediarium.example.com, no trailing slash
}

func (l Links) to(path string) string {
	base := strings.TrimRight(strings.TrimSpace(l.Base), "/")
	if base == "" || path == "" {
		return ""
	}
	return base + path
}

// Compose builds the event for a download-related occurrence.
func Compose(eventType string, it Item, links Links, at time.Time) Event {
	kind := EventKind(eventType)
	name := it.Name()
	watch := "ready to watch"
	if it.Media == "album" {
		watch = "ready to listen to"
	}

	ev := Event{Type: eventType, Timestamp: at, PosterURL: it.PosterURL, LinkURL: links.to(it.LinkPath)}
	rows := []Detail{{"Title", it.Title}}
	if it.Year > 0 {
		rows = append(rows, Detail{"Year", fmt.Sprint(it.Year)})
	}
	if it.Episode != "" {
		rows = append(rows, Detail{"Episode", it.Episode})
	}
	rows = append(rows, Detail{"Quality", it.Quality}, Detail{"Size", formatSize(it.SizeBytes)})

	switch kind {
	case EventImported:
		ev.Title = fmt.Sprintf("%s is %s", name, watch)
		ev.Lead = fmt.Sprintf("%s was downloaded and added to your library.", name)
		rows = append(rows, Detail{"Saved to", it.Path})
	case EventAdded:
		ev.Title = "Downloading " + name
		ev.Lead = fmt.Sprintf("Mediarium found a release for %s and added it to the downloads.", name)
		rows = append(rows, Detail{"Release", it.Release})
	case EventFailed:
		ev.Title = "Download failed: " + name
		ev.Lead = fmt.Sprintf("The download of %s did not work.", name)
		rows = []Detail{{"Title", it.Title}, {"Reason", it.Reason}, {"Release", it.Release}}
		if it.Year > 0 {
			rows = []Detail{{"Title", it.Title}, {"Year", fmt.Sprint(it.Year)}, {"Reason", it.Reason}, {"Release", it.Release}}
		}
	case EventConflict:
		ev.Title = "Needs your decision: " + name
		ev.Lead = fmt.Sprintf("A file for %s already exists. Choose what to do in Activity.", name)
		rows = []Detail{{"Title", it.Title}, {"Existing file", it.Path}, {"Release", it.Release}}
	case EventSubtitle:
		ev.Title = "Subtitles added: " + name
		ev.Lead = fmt.Sprintf("Added %s subtitles for %s.", it.Language, name)
		rows = []Detail{{"Title", it.Title}, {"Language", it.Language}, {"Saved to", it.Path}}
	default:
		ev.Title = name
		ev.Lead = it.Reason
		rows = nil
	}
	rows = append(rows, Detail{"Time", formatWhen(at)})
	ev.Details = keepFilled(rows)
	ev.Message = plainBody(ev)
	ev.SettingsURL = links.to("/settings/notifications")
	return ev
}

// ComposeText builds the event for the ones that carry no title of their own
// (a broken connection, a new version, a test): a subject, a sentence and no
// table.
func ComposeText(eventType, title, message string, links Links, at time.Time) Event {
	kind := EventKind(eventType)
	ev := Event{Type: eventType, Timestamp: at, Lead: strings.TrimSpace(message)}
	title = strings.TrimSpace(title)
	switch {
	case eventType == "test":
		ev.Title = "Mediarium test message"
		ev.Lead = "If you can read this, notifications from Mediarium are working."
	case kind == EventHealth && strings.HasSuffix(strings.ToLower(title), "working again"):
		ev.Title = "Mediarium: " + title
	case kind == EventHealth:
		ev.Title = "Mediarium needs attention: " + lowerLead(title)
		ev.LinkURL = links.to("/settings/logs")
	default:
		ev.Title = title
	}
	if ev.Lead == "" {
		ev.Lead = ev.Title + "."
	}
	if eventType == "test" {
		ev.Details = []Detail{{"Sent", formatWhen(at)}}
	}
	ev.Message = plainBody(ev)
	ev.SettingsURL = links.to("/settings/notifications")
	return ev
}

// lowerLead lowercases the first letter of a title that starts with an
// ordinary word like "Your" or "The", so it reads as part of a sentence.
func lowerLead(s string) string {
	for _, w := range []string{"Your ", "The ", "A ", "An ", "This ", "There "} {
		if strings.HasPrefix(s, w) {
			return strings.ToLower(s[:1]) + s[1:]
		}
	}
	return s
}

// keepFilled drops rows with nothing to show.
func keepFilled(rows []Detail) []Detail {
	var out []Detail
	for _, r := range rows {
		if strings.TrimSpace(r.Value) != "" {
			out = append(out, r)
		}
	}
	return out
}

// plainBody is the message as plain text: the sentence, then one line per row.
// Channels that show rows themselves ignore it; everything else uses it as is.
func plainBody(ev Event) string {
	var b strings.Builder
	b.WriteString(ev.Lead)
	for _, d := range ev.Details {
		b.WriteString("\n" + d.Label + ": " + d.Value)
	}
	return b.String()
}

func formatSize(n int64) string {
	if n <= 0 {
		return ""
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}

func formatWhen(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2 Jan 2006, 15:04")
}
