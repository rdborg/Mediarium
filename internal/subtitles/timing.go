package subtitles

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Subtitle timing: moving a subtitle earlier or later, and lining it up with
// another subtitle of the same video that is known to be in time. Only the
// timestamps are touched; text, styling and numbering stay as they were.
// SRT ("00:01:02,345") and WebVTT ("00:01:02.345" or "01:02.345") are read.

var (
	// A cue's timing line: start --> end, the rest of the line kept.
	timingLine = regexp.MustCompile(`(?m)^(\s*)((?:\d+:)?\d{1,2}:\d{2}[,.]\d{1,3})(\s*-->\s*)((?:\d+:)?\d{1,2}:\d{2}[,.]\d{1,3})`)
)

// ErrNoCues is returned for a file with no timed cues.
var ErrNoCues = errors.New("no subtitle lines with times were found in that file")

func parseStamp(s string) (int64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	main, frac, _ := strings.Cut(s, ".")
	parts := strings.Split(main, ":")
	var h, m, sec int
	var err error
	switch len(parts) {
	case 3:
		if h, err = strconv.Atoi(parts[0]); err != nil {
			return 0, false
		}
		parts = parts[1:]
		fallthrough
	case 2:
		if m, err = strconv.Atoi(parts[0]); err != nil {
			return 0, false
		}
		if sec, err = strconv.Atoi(parts[1]); err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	for len(frac) < 3 {
		frac += "0"
	}
	ms, err := strconv.Atoi(frac[:3])
	if err != nil {
		return 0, false
	}
	return int64(((h*60+m)*60+sec)*1000 + ms), true
}

// formatStamp writes ms in the style of the original stamp (comma or dot,
// with or without hours).
func formatStamp(ms int64, like string) string {
	if ms < 0 {
		ms = 0
	}
	sep := ","
	if strings.Contains(like, ".") {
		sep = "."
	}
	h := ms / 3600000
	m := ms / 60000 % 60
	s := ms / 1000 % 60
	f := ms % 1000
	if strings.Count(like, ":") == 1 && h == 0 {
		return fmt.Sprintf("%02d:%02d%s%03d", m, s, sep, f)
	}
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", h, m, s, sep, f)
}

// Starts lists the start times (ms) of every cue in a subtitle file.
func Starts(data []byte) []int64 {
	var out []int64
	for _, m := range timingLine.FindAllSubmatch(data, -1) {
		if t, ok := parseStamp(string(m[2])); ok {
			out = append(out, t)
		}
	}
	return out
}

// Retime rewrites every timestamp t of a subtitle file as t*scale + offsetMs.
func Retime(data []byte, scale float64, offsetMs int64) ([]byte, error) {
	n := 0
	out := timingLine.ReplaceAllFunc(data, func(line []byte) []byte {
		m := timingLine.FindSubmatch(line)
		a, ok1 := parseStamp(string(m[2]))
		b, ok2 := parseStamp(string(m[4]))
		if !ok1 || !ok2 {
			return line
		}
		n++
		na := int64(math.Round(float64(a)*scale)) + offsetMs
		nb := int64(math.Round(float64(b)*scale)) + offsetMs
		return []byte(string(m[1]) + formatStamp(na, string(m[2])) + string(m[3]) + formatStamp(nb, string(m[4])))
	})
	if n == 0 {
		return nil, ErrNoCues
	}
	return out, nil
}

// Common frame-rate mix-ups (a subtitle made for 25 fps played at 23.976).
var syncScales = []float64{1, 25 / 23.976, 23.976 / 25, 25.0 / 24, 24.0 / 25, 24 / 23.976, 23.976 / 24}

// Match is how a subtitle was lined up with another.
type Match struct {
	Scale    float64 // 1 = same speed
	OffsetMs int64
	Matched  float64 // the share of cues that now start with one in the other file, 0..1
}

const matchWindowMs = 400

// score counts the target cues that start within the window of a ref cue,
// and how far off those are in total (smaller is a tighter fit).
func score(target, ref []int64, scale float64, offset int64) (int, int64) {
	n := 0
	var errSum int64
	for _, t := range target {
		x := int64(math.Round(float64(t)*scale)) + offset
		i := sort.Search(len(ref), func(i int) bool { return ref[i] >= x })
		best := int64(math.MaxInt64)
		if i < len(ref) {
			best = ref[i] - x
		}
		if i > 0 && x-ref[i-1] < best {
			best = x - ref[i-1]
		}
		if best <= matchWindowMs {
			n++
			errSum += best
		}
	}
	return n, errSum
}

// Align finds the speed and offset that make target's cues start with ref's.
// It answers ErrNoCues when either file has none, and a Match whose Matched
// share is low when the two don't fit together at any offset.
func Align(target, ref []byte) (Match, error) {
	t, r := Starts(target), Starts(ref)
	if len(t) == 0 || len(r) == 0 {
		return Match{}, ErrNoCues
	}
	sort.Slice(t, func(i, j int) bool { return t[i] < t[j] })
	sort.Slice(r, func(i, j int) bool { return r[i] < r[j] })
	best := Match{Scale: 1}
	bestScore := -1
	var bestErr int64
	const window = 300_000 // only pairs less than 5 minutes apart vote
	const bucket = 100
	for _, s := range syncScales {
		votes := map[int64]int{}
		for _, ti := range t {
			x := int64(math.Round(float64(ti) * s))
			lo := sort.Search(len(r), func(i int) bool { return r[i] >= x-window })
			for j := lo; j < len(r) && r[j] <= x+window; j++ {
				votes[(r[j]-x)/bucket]++
			}
		}
		// Refine the three most voted buckets in 10 ms steps.
		type cand struct {
			b int64
			n int
		}
		var cs []cand
		for b, n := range votes {
			cs = append(cs, cand{b, n})
		}
		sort.Slice(cs, func(i, j int) bool { return cs[i].n > cs[j].n || (cs[i].n == cs[j].n && cs[i].b < cs[j].b) })
		for k := 0; k < len(cs) && k < 3; k++ {
			for o := cs[k].b*bucket - bucket; o <= cs[k].b*bucket+bucket; o += 10 {
				sc, e := score(t, r, s, o)
				if sc > bestScore || (sc == bestScore && e < bestErr) {
					bestScore, bestErr = sc, e
					best = Match{Scale: s, OffsetMs: o}
				}
			}
		}
	}
	best.Matched = float64(bestScore) / float64(len(t))
	return best, nil
}
