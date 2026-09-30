package organizer_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/parser"
)

func FuzzSanitize(f *testing.F) {
	for _, s := range []string{
		"", ".", "..", "...", " ", "a/b", `a\b`, "a:b", "CON", "nul.txt", "COM1", "a\x00b", "\xff", "x.", "x ",
		"Movie: The <Return> / \"Sequel\"?", strings.Repeat("a", 300), strings.Repeat("日", 300), "🎬🎬🎬", "\u202e",
		"../../etc/passwd", `C:\Windows`, "/abs",
	} {
		f.Add(s, false, "")
		f.Add(s, true, "-")
		f.Add(s, true, "$1")
		f.Add(s, true, "/")
	}
	f.Fuzz(func(t *testing.T, name string, replace bool, repl string) {
		mode := organizer.SanitizeStrip
		if replace {
			mode = organizer.SanitizeReplace
		}
		got := organizer.Sanitize(name, mode, repl)
		if problem := checkComponent(got, ".mkv"); problem != "" {
			t.Fatalf("Sanitize(%q, %v, %q) = %q %s", name, mode, repl, got, problem)
		}
		if again := organizer.Sanitize(name, mode, repl); again != got {
			t.Fatalf("Sanitize(%q) is not deterministic", name)
		}
		if got == "" && name != "" && strings.Trim(name, " .") != "" && !onlyIllegal(name) {
			t.Fatalf("Sanitize(%q) = %q lost a whole name", name, got)
		}
		// A name that is already clean must not change.
		if organizer.Sanitize(got, mode, repl) != got && replace == false {
			t.Fatalf("Sanitize(%q) = %q is not stable", name, got)
		}
	})
}

// onlyIllegal reports whether name holds nothing but characters Sanitize removes.
func onlyIllegal(name string) bool {
	for _, r := range strings.ToValidUTF8(name, "") {
		if !strings.ContainsRune(`<>:"/\|?*`, r) && r >= 0x20 {
			return false
		}
	}
	return true
}

func FuzzRenderLibraryPath(f *testing.F) {
	f.Add("{Movie Title} ({Year})", "The Matrix", "", "", "GRP", 1999, 0, 0, false, "-")
	f.Add("{Series Title} - S{Season:00}E{Episode:00} - {Episode Title}", "", "Show", "Ep: One/Two", "", 2011, 1, 2, true, "_")
	f.Add("{Movie Title}/../../{Year}", "..", "..", "..", "..", 0, 0, 0, false, "")
	f.Add("{Season:"+strings.Repeat("0", 100)+"}", "x", "x", "x", "x", 1, 1, 1, false, "")
	f.Add("{{}}{}{:}{Year:}", "CON", "NUL", "AUX", "PRN", -1, -1, -1, true, "$")
	f.Add("/{Movie Title}", "/etc/passwd", "C:\\x", "\x00", "\xff", 2020, 1, 1, false, "")
	f.Add(strings.Repeat("{Movie Title}", 100), strings.Repeat("日", 200), "", "", "", 0, 0, 0, false, "")
	f.Fuzz(func(t *testing.T, format, movie, series, episode, group string, year, season, ep int, replace bool, repl string) {
		mode := organizer.SanitizeStrip
		if replace {
			mode = organizer.SanitizeReplace
		}
		ctx := organizer.NamingContext{
			MovieTitle: movie, SeriesTitle: series, EpisodeTitle: episode, ReleaseGroup: group,
			Year: year, Season: season, Episode: ep, Quality: "1080p", Source: "WEB-DL",
		}
		root := filepath.Join(string(filepath.Separator), "library")
		start := time.Now()
		folder, file, full := libraryPath(root, "{Series Title} ({Year})/Season {Season:00}", format, ".mkv", ctx, mode, repl)
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("naming took %s", d)
		}
		for _, c := range []string{folder, file} {
			if problem := checkComponent(c, ".mkv"); problem != "" {
				t.Fatalf("format %q gave component %q which %s", format, c, problem)
			}
		}
		assertInside(t, root, full)
		if !utf8.ValidString(full) {
			t.Fatalf("path %q is not valid UTF-8", full)
		}
	})
}

// FuzzReleaseNameToPath takes a release name apart with the parser and puts
// the pieces into a file name, as an import does. Whatever the release name
// holds (separators, dots, NUL), the file lands inside the library.
func FuzzReleaseNameToPath(f *testing.F) {
	for _, s := range []string{
		"The.Matrix.1999.1080p.WEB-DL.x264-GROUP", "../../etc/passwd.2020.1080p-GRP", "Movie.2020.Director's.Cut.1080p-../x",
		`a/b\c:d.2020`, "\x00", "CON.2020.720p", "Show.S01E01.1080p.WEB-DL.H.265-NUL", strings.Repeat("Word.", 500) + "2020",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, release string) {
		r := parser.Parse(release)
		ctx := organizer.NamingContext{
			MovieTitle: r.Title, SeriesTitle: r.Title, EpisodeTitle: r.Title, Year: r.Year,
			Season: r.Season, Episode: r.Episode, Quality: r.Resolution, Source: r.Source,
			Codec: r.Codec, Edition: r.Edition, ReleaseGroup: r.Group,
		}
		root := filepath.Join(string(filepath.Separator), "library")
		for _, format := range []string{
			"{Movie Title} ({Year}) [{Quality} {Source} {Codec}] {Custom Formats}-{Release Group}",
			"{Series Title} - S{Season:00}E{Episode:00} - {Episode Title} [{Quality}]",
		} {
			folder, file, full := libraryPath(root, organizer.Presets["plex"], format, ".mkv", ctx, organizer.SanitizeStrip, "")
			for _, c := range []string{folder, file} {
				if problem := checkComponent(c, ".mkv"); problem != "" {
					t.Fatalf("release %q gave component %q which %s", release, c, problem)
				}
			}
			assertInside(t, root, full)
		}
	})
}
