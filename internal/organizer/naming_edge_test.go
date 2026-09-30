package organizer_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/organizer"
)

// checkComponent reports what is wrong with one path component built by
// Sanitize plus a file extension, or "" when it is safe to create.
func checkComponent(c, ext string) string {
	switch {
	case c == "":
		return "" // an unnamed title; the caller decides
	case c == "." || c == "..":
		return "is a dot name"
	case strings.ContainsAny(c, `/\`):
		return "holds a path separator"
	case !utf8.ValidString(c):
		return "is not valid UTF-8"
	case strings.ContainsRune(c, 0):
		return "holds a NUL byte"
	case len(c+ext+".mediarium-tmp") > 255:
		return "is too long once the extension and temporary suffix are added"
	case strings.HasSuffix(c, ".") || strings.HasSuffix(c, " "):
		return "ends in a dot or a space"
	}
	stem := c
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	switch strings.ToUpper(strings.TrimRight(stem, " ")) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "is a Windows device name"
	}
	for _, r := range c {
		if r < 0x20 {
			return "holds a control character"
		}
	}
	return ""
}

func TestSanitizeEdgeCases(t *testing.T) {
	long255 := strings.Repeat("a", 255)
	longCJK := strings.Repeat("日", 200) // 600 bytes
	cases := []struct {
		name        string
		in          string
		mode        organizer.SanitizeMode
		replacement string
		want        string
	}{
		{"empty stays empty", "", organizer.SanitizeStrip, "", ""},
		{"only illegal characters", `???`, organizer.SanitizeStrip, "", "_"},
		{"only dots", "...", organizer.SanitizeStrip, "", "_"},
		{"only spaces", "   ", organizer.SanitizeStrip, "", "_"},
		{"trailing dot", "Mr.", organizer.SanitizeStrip, "", "Mr"},
		{"trailing dots and spaces", "Title . .", organizer.SanitizeStrip, "", "Title"},
		{"trailing dot after a stripped character", "Title.?", organizer.SanitizeStrip, "", "Title"},
		{"ellipsis title", "Wait for it...", organizer.SanitizeStrip, "", "Wait for it"},
		{"windows characters replaced", `a<b>c:d"e/f\g|h?i*j`, organizer.SanitizeReplace, "_", "a_b_c_d_e_f_g_h_i_j"},
		{"windows characters stripped", `a<b>c:d"e/f\g|h?i*j`, organizer.SanitizeStrip, "", "abcdefghij"},
		{"reserved name", "CON", organizer.SanitizeStrip, "", "CON_"},
		{"reserved name lowercase", "nul", organizer.SanitizeStrip, "", "nul_"},
		{"reserved name with extension part", "aux.2019", organizer.SanitizeStrip, "", "aux_.2019"},
		{"reserved name with numbers", "COM1", organizer.SanitizeStrip, "", "COM1_"},
		{"not reserved", "Console", organizer.SanitizeStrip, "", "Console"},
		{"not reserved with digit", "COM10", organizer.SanitizeStrip, "", "COM10"},
		{"parent folder", "..", organizer.SanitizeStrip, "", "_"},
		{"parent folder with separators", "../../etc", organizer.SanitizeStrip, "", "....etc"},
		{"windows drive", `C:\Windows`, organizer.SanitizeStrip, "", "CWindows"},
		{"absolute path", "/etc/passwd", organizer.SanitizeStrip, "", "etcpasswd"},
		{"NUL byte", "a\x00b", organizer.SanitizeStrip, "", "ab"},
		{"control characters", "a\tb\nc\x1fd", organizer.SanitizeStrip, "", "abcd"},
		{"invalid utf-8", "a\xffb\xc3", organizer.SanitizeStrip, "", "ab"},
		{"replacement is literal", "a:b", organizer.SanitizeReplace, "$1", "a$1b"},
		{"replacement with a group name", "a:b", organizer.SanitizeReplace, "${x}", "a${x}b"},
		{"replacement with a separator", "a:b", organizer.SanitizeReplace, "/", "ab"},
		{"replacement with an escape", "a:b", organizer.SanitizeReplace, `\`, "ab"},
		{"replacement of dots", "a:b?", organizer.SanitizeReplace, ".", "a.b"},
		{"right to left", "שלום עולם", organizer.SanitizeStrip, "", "שלום עולם"},
		{"emoji", "Movie 🎬", organizer.SanitizeStrip, "", "Movie 🎬"},
		{"255 bytes is cut", long255, organizer.SanitizeStrip, "", strings.Repeat("a", organizer.MaxNameBytes)},
		{"multi-byte cut on a character", longCJK, organizer.SanitizeStrip, "", strings.Repeat("日", organizer.MaxNameBytes/3)},
		{"leading dots stay", "...And Justice for All (1979)", organizer.SanitizeStrip, "", "...And Justice for All (1979)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := organizer.Sanitize(tc.in, tc.mode, tc.replacement)
			if got != tc.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if problem := checkComponent(got, ".mkv"); problem != "" {
				t.Errorf("Sanitize(%q) = %q %s", tc.in, got, problem)
			}
		})
	}
}

func TestRenderPadIsBounded(t *testing.T) {
	ctx := organizer.NamingContext{Season: 2}
	start := time.Now()
	got := organizer.Render("Season {Season:"+strings.Repeat("0", 1_000_000)+"}", ctx)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("a huge pad took %s", d)
	}
	if len(got) > 100 {
		t.Fatalf("a huge pad made a %d byte name", len(got))
	}
	if !strings.HasSuffix(got, "2") {
		t.Fatalf("got %q", got)
	}
}

func TestRenderEdgeCases(t *testing.T) {
	cases := []struct {
		name   string
		format string
		ctx    organizer.NamingContext
		want   string
	}{
		{"empty title and year", "{Movie Title} ({Year})", organizer.NamingContext{}, ""},
		{"only an empty pair of brackets", "[{Quality}]", organizer.NamingContext{}, ""},
		{"unknown token", "{Nope} {Movie Title}", organizer.NamingContext{MovieTitle: "A"}, "A"},
		{"unclosed brace", "{Movie Title", organizer.NamingContext{MovieTitle: "A"}, "{Movie Title"},
		{"nested braces", "{{Movie Title}}", organizer.NamingContext{MovieTitle: "A"}, "{A}"},
		{"token value holding a token", "{Movie Title}", organizer.NamingContext{MovieTitle: "{Year}", Year: 2000}, "{Year}"},
		{"pad on a title", "{Movie Title:000}", organizer.NamingContext{MovieTitle: "A"}, "00A"},
		{"pad on a large season", "S{Season:00}", organizer.NamingContext{Season: 123}, "S123"},
		{"negative season", "S{Season:00}", organizer.NamingContext{Season: -1}, "S-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := organizer.Render(tc.format, tc.ctx); got != tc.want {
				t.Errorf("Render(%q) = %q, want %q", tc.format, got, tc.want)
			}
		})
	}
}

// libraryPath builds a final path the way the importer does: a folder and a
// file name, each cleaned on its own, under the library root.
func libraryPath(root, folderFormat, fileFormat, ext string, ctx organizer.NamingContext, mode organizer.SanitizeMode, repl string) (folder, file, full string) {
	folder = organizer.Sanitize(organizer.Render(folderFormat, ctx), mode, repl)
	file = organizer.Sanitize(organizer.Render(fileFormat, ctx), mode, repl)
	return folder, file, filepath.Join(root, folder, file+ext)
}

func TestLibraryPathStaysInsideTheRoot(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "library", "movies")
	titles := []string{
		"", "..", ".", "../../../etc/passwd", "/abs/olute", `C:\Windows\System32`, "CON", "a/b/../../..", "...", "  ",
		strings.Repeat("x", 10000), strings.Repeat("日本", 3000), "\x00", "\u202egnp.exe", "Ⅻ", "a\xffb",
	}
	for _, title := range titles {
		ctx := organizer.NamingContext{MovieTitle: title, SeriesTitle: title, EpisodeTitle: title, Year: 2020, Season: 1, Episode: 1, ReleaseGroup: title}
		for _, mode := range []organizer.SanitizeMode{organizer.SanitizeStrip, organizer.SanitizeReplace} {
			for _, preset := range organizer.Presets {
				folder, file, full := libraryPath(root, organizer.Presets["plex"], preset, ".mkv", ctx, mode, "-")
				assertInside(t, root, full)
				for _, c := range []string{folder, file} {
					if problem := checkComponent(c, ".mkv"); problem != "" {
						t.Errorf("title %q: component %q %s", title, c, problem)
					}
				}
			}
			for _, preset := range organizer.TVPresets {
				_, _, full := libraryPath(root, organizer.TVSeriesFolder+"/"+organizer.TVSeasonFolder, preset, ".mkv", ctx, mode, "-")
				assertInside(t, root, full)
			}
		}
	}
}

func assertInside(t *testing.T, root, full string) {
	t.Helper()
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Errorf("%q is not inside %q (rel %q, err %v)", full, root, rel, err)
	}
	if len(full) > 4096 {
		t.Errorf("path is %d bytes", len(full))
	}
}
