package subtitles_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/subtitles"
)

// writeFiles creates files (path -> content) under root.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestImportSidecarsPlacement(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string // in the download folder
		want  []string          // file names next to the video afterwards (besides the video)
	}{
		{
			name: "languages from names, codes and folders",
			files: map[string]string{
				"Movie.2020.1080p-GRP.mkv":     "video",
				"Subs/2_English.srt":           "en text",
				"Subs/French.srt":              "fr text",
				"Subs/Deutsch/track.srt":       "de text",
				"Movie.2020.1080p-GRP.ita.srt": "it text",
			},
			want: []string{"Movie (2020).de.srt", "Movie (2020).en.srt", "Movie (2020).fr.srt", "Movie (2020).it.srt"},
		},
		{
			name: "forced and sdh keep their flag in the name",
			files: map[string]string{
				"Subs/movie.en.forced.srt": "forced",
				"Subs/movie.en.srt":        "full",
				"Subs/movie.en.sdh.srt":    "sdh",
			},
			want: []string{"Movie (2020).en.forced.srt", "Movie (2020).en.sdh.srt", "Movie (2020).en.srt"},
		},
		{
			name: "other formats and a vobsub pair",
			files: map[string]string{
				"Subs/English.ass": "ass",
				"Subs/French.idx":  "idx",
				"Subs/French.sub":  "sub",
				"Subs/Dutch.sup":   "sup",
			},
			want: []string{"Movie (2020).en.ass", "Movie (2020).fr.idx", "Movie (2020).fr.sub", "Movie (2020).nl.sup"},
		},
		{
			name: "samples, unknown languages, unrelated files and empty files are skipped",
			files: map[string]string{
				"Sample/English.srt":        "sample dir",
				"movie-sample.en.srt":       "sample name",
				"Subs/track.srt":            "no language",
				"Movie.2020.FRENCH-GRP.srt": "language only mentioned in the release name",
				"Subs/Spanish.srt":          "es text",
				"Subs/Italian.srt":          "",
				"readme.txt":                "not a subtitle",
				"Subs/Portuguese.nfo":       "not a subtitle",
			},
			want: []string{"Movie (2020).es.srt"},
		},
		{
			name: "two subtitles for the same slot keep the fuller one",
			files: map[string]string{
				"Subs/English.srt":   "short",
				"Subs/3_English.srt": "a much longer subtitle file",
			},
			want: []string{"Movie (2020).en.srt"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dl := t.TempDir()
			writeFiles(t, dl, tc.files)
			before := snapshot(t, dl)

			lib := t.TempDir()
			video := filepath.Join(lib, "Movie (2020).mkv")
			writeFiles(t, lib, map[string]string{"Movie (2020).mkv": "video"})

			scs, err := subtitles.FindSidecars(dl)
			if err != nil {
				t.Fatalf("FindSidecars: %v", err)
			}
			if _, err := subtitles.ImportSidecars(video, scs); err != nil {
				t.Fatalf("ImportSidecars: %v", err)
			}

			got := listDir(t, lib)
			want := append([]string{"Movie (2020).mkv"}, tc.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("files next to the video = %v, want %v", got, want)
			}
			if tc.name == "two subtitles for the same slot keep the fuller one" {
				b, _ := os.ReadFile(filepath.Join(lib, "Movie (2020).en.srt"))
				if string(b) != "a much longer subtitle file" {
					t.Fatalf("kept %q, want the fuller subtitle", b)
				}
			}
			if after := snapshot(t, dl); !reflect.DeepEqual(before, after) {
				t.Fatalf("the download folder was changed: before %v after %v", before, after)
			}
		})
	}
}

// snapshot lists every file under dir with its size.
func snapshot(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel+":"+strings.Repeat("x", int(info.Size())))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func TestImportSidecarsNeverOverwrites(t *testing.T) {
	dl := t.TempDir()
	writeFiles(t, dl, map[string]string{"English.srt": "new", "French.srt": "fr new"})
	lib := t.TempDir()
	video := filepath.Join(lib, "Movie.mkv")
	writeFiles(t, lib, map[string]string{"Movie.mkv": "video", "Movie.en.srt": "already here"})

	scs, err := subtitles.FindSidecars(dl)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := subtitles.ImportSidecars(video, scs)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 || imported[0].Lang != "fr" {
		t.Fatalf("expected only the French subtitle to be imported, got %+v", imported)
	}
	if b, _ := os.ReadFile(filepath.Join(lib, "Movie.en.srt")); string(b) != "already here" {
		t.Fatalf("existing subtitle was overwritten: %q", b)
	}
}

func TestSidecarEpisodeMatching(t *testing.T) {
	dl := t.TempDir()
	writeFiles(t, dl, map[string]string{
		"Show.S01E01.1080p.mkv":                "v1",
		"Show.S01E02.1080p.mkv":                "v2",
		"Subs/Show.S01E01.1080p/2_English.srt": "e1 en",
		"Subs/Show.S01E02.1080p/2_English.srt": "e2 en",
		"Subs/Show.S01E02.1080p/3_French.srt":  "e2 fr",
		"Show.S01E01.1080p.German.srt":         "e1 de",
		"Subs/English.srt":                     "unmatchable in a pack",
		"Subs/Show.S02E01.English.srt":         "another season",
	})
	scs, err := subtitles.FindSidecars(dl)
	if err != nil {
		t.Fatal(err)
	}

	forEpisode := func(season int, eps []int, single bool) []string {
		var labels []string
		for _, sc := range scs {
			if sc.MatchesEpisodes(season, eps, single) {
				labels = append(labels, sc.Label()+":"+string(mustRead(t, sc.Files[0])))
			}
		}
		sort.Strings(labels)
		return labels
	}

	if got, want := forEpisode(1, []int{1}, false), []string{"de:e1 de", "en:e1 en"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode 1 of a pack: got %v want %v", got, want)
	}
	if got, want := forEpisode(1, []int{2}, false), []string{"en:e2 en", "fr:e2 fr"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode 2 of a pack: got %v want %v", got, want)
	}
	// With a single video in the download, subtitles that name no episode are
	// its own; those naming another episode or season are not.
	single := forEpisode(1, []int{1}, true)
	if !reflect.DeepEqual(single, []string{"de:e1 de", "en:e1 en", "en:unmatchable in a pack"}) {
		t.Fatalf("single video: got %v", single)
	}
	if got := forEpisode(3, []int{1}, false); len(got) != 0 {
		t.Fatalf("season 3 should match nothing, got %v", got)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFindSidecarsMissingFolder(t *testing.T) {
	if _, err := subtitles.FindSidecars(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a folder that does not exist")
	}
}

// A link named like a subtitle is not a subtitle: copying it would put the
// contents of whatever it points at into the library.
func TestSidecarsIgnoreAndNeverCopyLinks(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret.key")
	if err := os.WriteFile(secret, []byte("do not copy me"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, root, map[string]string{"Movie.mkv": "video", "Subs/Movie.de.srt": "hallo"})
	link := filepath.Join(root, "Movie.en.srt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skip("cannot make a symlink here")
	}

	found, err := subtitles.FindSidecars(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Lang != "de" {
		t.Fatalf("FindSidecars = %+v, want only the German file", found)
	}

	// Even handed to the importer directly, the link is not copied.
	video := filepath.Join(t.TempDir(), "Movie (2020).mkv")
	imported, err := subtitles.ImportSidecars(video, []subtitles.Sidecar{{Lang: "en", Files: []string{link}, Size: 14}})
	if err == nil || len(imported) != 0 {
		t.Fatalf("ImportSidecars = %v, %v; want the link refused", imported, err)
	}
	if _, err := os.Lstat(strings.TrimSuffix(video, ".mkv") + ".en.srt"); !os.IsNotExist(err) {
		t.Fatalf("no subtitle may be created from a link, Lstat: %v", err)
	}
}
