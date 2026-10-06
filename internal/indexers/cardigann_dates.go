package indexers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Dates in Cardigann definitions come in many shapes: explicit layouts for
// the dateparse filter (usually .NET custom format strings such as
// "yyyy-MM-dd HH:mm:ss zzz", occasionally Go reference layouts), relative
// times ("3 hours ago") and whatever a site prints ("Today 12:30",
// "yesterday", unix timestamps...).

// dotNetLayoutToGo converts a .NET custom date format into a Go layout.
func dotNetLayoutToGo(f string) string {
	var sb strings.Builder
	i := 0
	run := func(c byte) int {
		n := 0
		for i+n < len(f) && f[i+n] == c {
			n++
		}
		return n
	}
	for i < len(f) {
		c := f[i]
		switch c {
		case '\'', '"':
			j := strings.IndexByte(f[i+1:], c)
			if j < 0 {
				sb.WriteString(f[i+1:])
				return sb.String()
			}
			sb.WriteString(f[i+1 : i+1+j])
			i += j + 2
			continue
		case '\\':
			if i+1 < len(f) {
				sb.WriteByte(f[i+1])
			}
			i += 2
			continue
		}
		n := run(c)
		tok := ""
		switch c {
		case 'y':
			if n <= 2 {
				tok = "06"
			} else {
				tok = "2006"
			}
		case 'M':
			tok = [...]string{"1", "01", "Jan", "January"}[min(n, 4)-1]
		case 'd':
			tok = [...]string{"2", "02", "Mon", "Monday"}[min(n, 4)-1]
		case 'H':
			tok = "15"
		case 'h':
			tok = [...]string{"3", "03"}[min(n, 2)-1]
		case 'm':
			tok = [...]string{"4", "04"}[min(n, 2)-1]
		case 's':
			tok = [...]string{"5", "05"}[min(n, 2)-1]
		case 't':
			tok = "PM"
		case 'f', 'F':
			tok = strings.Repeat("0", n)
			if c == 'F' {
				tok = strings.Repeat("9", n)
			}
		case 'z':
			tok = [...]string{"-07", "-07", "-07:00"}[min(n, 3)-1]
		case 'K':
			tok = "Z07:00"
		default:
			tok = f[i : i+n]
		}
		sb.WriteString(tok)
		i += n
	}
	return sb.String()
}

var goLayoutMarkers = []string{"2006", "Jan", "Mon", "15", "04", "05", "01", "02", "MST", "-0700", "-07:00"}

func isGoLayout(layout string) bool {
	for _, m := range goLayoutMarkers {
		if strings.Contains(layout, m) {
			return true
		}
	}
	return false
}

var ampmRe = regexp.MustCompile(`(?i)(^|[\d\s])([ap])\.?m\.?(\s|$)`)

// parseDateLayout parses value with a dateparse layout, .NET or Go style.
// Values without a time zone are read in the local zone; without a year,
// the current year is assumed (both as .NET does).
func parseDateLayout(value, layout string, now time.Time) (time.Time, error) {
	goLayout := layout
	if !isGoLayout(layout) {
		goLayout = dotNetLayoutToGo(layout)
	}
	value = strings.Join(strings.Fields(value), " ")
	goLayout = strings.Join(strings.Fields(goLayout), " ")
	if strings.Contains(goLayout, "PM") {
		// Go only reads upper-case AM/PM; .NET is case-insensitive.
		value = ampmRe.ReplaceAllStringFunc(value, func(m string) string {
			sub := ampmRe.FindStringSubmatch(m)
			return sub[1] + strings.ToUpper(sub[2]) + "M" + sub[3]
		})
	}
	t, err := time.ParseInLocation(goLayout, value, now.Location())
	if err != nil && strings.Contains(goLayout, "-07:00") {
		// .NET's zzz also reads +0000 and +00
		for _, alt := range []string{"-0700", "-07"} {
			if t2, err2 := time.ParseInLocation(strings.Replace(goLayout, "-07:00", alt, 1), value, now.Location()); err2 == nil {
				t, err = t2, nil
				break
			}
		}
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("date %q does not match layout %q: %w", value, layout, err)
	}
	if !strings.Contains(goLayout, "06") {
		t = time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	}
	return t, nil
}

var agoPartRe = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?|an?|one)\s*(seconds?|secs?|s|minutes?|mins?|m|hours?|hrs?|h|days?|d|weeks?|wks?|w|months?|mos?|years?|yrs?|y)\b`)

// fromTimeAgo parses "3 hours ago", "1 day 2 hours", "an hour ago".
func fromTimeAgo(s string, now time.Time) (time.Time, error) {
	lower := strings.ToLower(strings.TrimSpace(s))
	lower = strings.ReplaceAll(lower, ",", " ")
	switch lower {
	case "now", "just now", "right now", "moments ago", "less than a minute ago":
		return now, nil
	case "yesterday":
		return now.AddDate(0, 0, -1), nil
	}
	matches := agoPartRe.FindAllStringSubmatch(lower, -1)
	if len(matches) == 0 {
		return time.Time{}, fmt.Errorf("not a relative time: %q", s)
	}
	t := now
	for _, m := range matches {
		var n float64
		switch m[1] {
		case "a", "an", "one":
			n = 1
		default:
			f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
			if err != nil {
				return time.Time{}, fmt.Errorf("not a relative time: %q", s)
			}
			n = f
		}
		unit := m[2]
		switch {
		case strings.HasPrefix(unit, "s"):
			t = t.Add(-time.Duration(n * float64(time.Second)))
		case strings.HasPrefix(unit, "mo"):
			t = t.AddDate(0, -int(n), 0)
		case strings.HasPrefix(unit, "m"):
			t = t.Add(-time.Duration(n * float64(time.Minute)))
		case strings.HasPrefix(unit, "h"):
			t = t.Add(-time.Duration(n * float64(time.Hour)))
		case strings.HasPrefix(unit, "d"):
			t = t.Add(-time.Duration(n * 24 * float64(time.Hour)))
		case strings.HasPrefix(unit, "w"):
			t = t.Add(-time.Duration(n * 7 * 24 * float64(time.Hour)))
		case strings.HasPrefix(unit, "y"):
			t = t.AddDate(-int(n), 0, 0)
		}
	}
	return t, nil
}

var (
	unixRe     = regexp.MustCompile(`^\d{9,13}$`)
	dayTimeRe  = regexp.MustCompile(`(?i)^(today|yesterday|tomorrow)(?:\s*(?:at|,)?\s*(.*))?$`)
	clockRe    = regexp.MustCompile(`(?i)^(\d{1,2}):(\d{2})(?::(\d{2}))?\s*([ap]\.?m\.?)?$`)
	knownDates = []string{
		time.RFC3339Nano, time.RFC3339, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822,
		time.RFC850, time.ANSIC, time.UnixDate,
		"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
		"2006-01-02T15:04:05", "2006-01-02T15:04:05Z0700", "2006-01-02T15:04",
		"2006-01-02 15:04:05 -0700", "2006-01-02 15:04:05 -07:00", "2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02",
		"2006/01/02 15:04:05", "2006/01/02 15:04", "2006/01/02",
		"01/02/2006 15:04:05", "01/02/2006 15:04", "01/02/2006 3:04:05 PM", "01/02/2006",
		"Jan 2 2006 15:04", "Jan 2, 2006 15:04", "Jan 2, 2006 3:04 PM", "Jan 2, 2006", "Jan 2 2006",
		"January 2, 2006", "January 2 2006", "2 January 2006", "2 Jan 2006", "02 Jan 2006 15:04",
		"02 Jan 2006 15:04:05", "2 Jan 2006 15:04", "02-Jan-2006", "02-Jan-2006 15:04",
	}
)

// fromUnknownDate makes the best of whatever date text a site shows.
// isoWithExtraZone matches an ISO date-time that carries its time zone twice,
// "2022-06-25T20:46:53.000000Z +00:00", as some sites print it.
var isoWithExtraZone = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?)Z\s*[+-]\d{2}:?\d{2}$`)

// fromAnyDate reads a date written in any common way: ISO 8601 (also with
// the zone written twice) or the forms fromUnknownDate knows.
func fromAnyDate(s string, now time.Time) (time.Time, error) {
	str := strings.TrimSpace(s)
	if m := isoWithExtraZone.FindStringSubmatch(str); m != nil {
		str = strings.Replace(m[1], " ", "T", 1) + "Z"
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z07:00", "2006-01-02 15:04:05Z07:00"} {
		if t, err := time.Parse(layout, str); err == nil {
			return t, nil
		}
	}
	return fromUnknownDate(str, now)
}

func fromUnknownDate(s string, now time.Time) (time.Time, error) {
	str := strings.Join(strings.Fields(s), " ")
	if str == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	lower := strings.ToLower(str)
	if unixRe.MatchString(str) {
		n, _ := strconv.ParseInt(str, 10, 64)
		if len(str) >= 12 {
			return time.UnixMilli(n), nil
		}
		return time.Unix(n, 0), nil
	}
	if m := dayTimeRe.FindStringSubmatch(lower); m != nil {
		day := now
		switch m[1] {
		case "yesterday":
			day = now.AddDate(0, 0, -1)
		case "tomorrow":
			day = now.AddDate(0, 0, 1)
		}
		if strings.TrimSpace(m[2]) == "" {
			return day, nil
		}
		if h, mi, sec, ok := parseClock(m[2]); ok {
			return time.Date(day.Year(), day.Month(), day.Day(), h, mi, sec, 0, now.Location()), nil
		}
	}
	if h, mi, sec, ok := parseClock(lower); ok {
		return time.Date(now.Year(), now.Month(), now.Day(), h, mi, sec, 0, now.Location()), nil
	}
	if strings.Contains(lower, "ago") || lower == "now" || lower == "just now" {
		return fromTimeAgo(strings.ReplaceAll(lower, "ago", ""), now)
	}
	for _, layout := range knownDates {
		if t, err := time.ParseInLocation(layout, str, now.Location()); err == nil {
			return t, nil
		}
	}
	if t, err := fromTimeAgo(lower, now); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognised date %q", s)
}

func parseClock(s string) (h, m, sec int, ok bool) {
	c := clockRe.FindStringSubmatch(strings.TrimSpace(s))
	if c == nil {
		return 0, 0, 0, false
	}
	h, _ = strconv.Atoi(c[1])
	m, _ = strconv.Atoi(c[2])
	if c[3] != "" {
		sec, _ = strconv.Atoi(c[3])
	}
	if ap := strings.ToLower(c[4]); ap != "" {
		if h == 12 {
			h = 0
		}
		if ap[0] == 'p' {
			h += 12
		}
	}
	if h > 23 || m > 59 || sec > 59 {
		return 0, 0, 0, false
	}
	return h, m, sec, true
}
