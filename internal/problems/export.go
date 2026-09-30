package problems

import (
	"fmt"
	"strings"
	"time"
)

// Ago says how long before now t was, in a few plain words: "just now",
// "2 minutes ago", "3 hours ago", "5 days ago".
func Ago(now, t time.Time) string {
	d := now.Sub(t)
	unit := func(n int, word string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s ago", word)
		}
		return fmt.Sprintf("%d %ss ago", n, word)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return unit(int(d/time.Minute), "minute")
	case d < 48*time.Hour:
		return unit(int(d/time.Hour), "hour")
	default:
		return unit(int(d/(24*time.Hour)), "day")
	}
}

// Times says how often something happened: "once", "3 times".
func Times(n int) string {
	if n <= 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}

// Text writes the problems as a plain text file a person can attach to a
// support request. entries are written in the order given.
func Text(entries []Entry, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mediarium problem log\nWritten: %s\nProblems: %d\n", now.Format("2006-01-02 15:04:05 MST"), len(entries))
	if len(entries) == 0 {
		b.WriteString("\nNo problems.\n")
		return b.String()
	}
	for _, e := range entries {
		title := e.Message
		if h, ok := Lookup(e.Code); ok {
			title = h.Title
		}
		fmt.Fprintf(&b, "\n[%s] %s  %s  %s\n", e.LastAt.In(now.Location()).Format("2006-01-02 15:04:05"), strings.ToUpper(string(e.Level)), AreaLabel(e.Area), e.Code)
		fmt.Fprintf(&b, "  %s\n", title)
		if e.Message != "" && e.Message != title {
			fmt.Fprintf(&b, "  %s\n", e.Message)
		}
		if e.Count > 1 {
			fmt.Fprintf(&b, "  Happened %s, first at %s\n", Times(e.Count), e.FirstAt.In(now.Location()).Format("2006-01-02 15:04:05"))
		}
		if e.Title != "" {
			fmt.Fprintf(&b, "  For: %s\n", e.Title)
		}
		if e.DownloadID > 0 {
			fmt.Fprintf(&b, "  Download: %d\n", e.DownloadID)
		}
		if e.Detail != "" {
			b.WriteString("  Detail:\n")
			for _, ln := range strings.Split(e.Detail, "\n") {
				fmt.Fprintf(&b, "    %s\n", ln)
			}
		}
	}
	return b.String()
}
