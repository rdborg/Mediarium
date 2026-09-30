package music

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIdentifyFileFromNames(t *testing.T) {
	cases := []struct {
		path string
		want FileInfo
	}{
		{"Album/01 - Airbag.flac", FileInfo{Track: 1, Title: "Airbag"}},
		{"Album/02. Paranoid Android.mp3", FileInfo{Track: 2, Title: "Paranoid Android"}},
		{"Album/3 Subterranean Homesick Alien.flac", FileInfo{Track: 3, Title: "Subterranean Homesick Alien"}},
		{"Album/2-07 - Lucky.flac", FileInfo{Disc: 2, Track: 7, Title: "Lucky"}},
		{"Album/1.12 The Tourist.flac", FileInfo{Disc: 1, Track: 12, Title: "The Tourist"}},
		{"Album/Radiohead - 04 - Exit Music (For a Film).mp3", FileInfo{Track: 4, Title: "Exit Music (For a Film)"}},
		{"Album/107-radiohead-lucky.mp3", FileInfo{Disc: 1, Track: 7, Title: "radiohead-lucky"}},
		{"Album/05_let_down.mp3", FileInfo{Track: 5, Title: "let down"}},
		{"Album/CD2/03 - Karma Police.flac", FileInfo{Disc: 2, Track: 3, Title: "Karma Police"}},
		{"Album/Disc 1/01 Airbag.flac", FileInfo{Disc: 1, Track: 1, Title: "Airbag"}},
		{"Album/1979.mp3", FileInfo{Title: "1979"}},
		{"Album/No Surprises.flac", FileInfo{Title: "No Surprises"}},
	}
	for _, tc := range cases {
		p := filepath.FromSlash(tc.path)
		got := IdentifyFile(p, nil)
		tc.want.Path = p
		if got != tc.want {
			t.Errorf("IdentifyFile(%q) = %+v, want %+v", tc.path, got, tc.want)
		}
	}
}

type fakeTags map[string]FileInfo

func (f fakeTags) ReadTags(path string) (FileInfo, bool, error) {
	info, ok := f[path]
	return info, ok, nil
}

func TestIdentifyFileTagsWinWhereSet(t *testing.T) {
	tags := fakeTags{"x/track.flac": {Disc: 2, Track: 4, Title: "Tagged Title"}, "x/05 - Named.flac": {Track: 0, Title: ""}}
	if got := IdentifyFile("x/track.flac", tags); got.Disc != 2 || got.Track != 4 || got.Title != "Tagged Title" {
		t.Fatalf("tags should fill in: %+v", got)
	}
	if got := IdentifyFile("x/05 - Named.flac", tags); got.Track != 5 || got.Title != "Named" {
		t.Fatalf("empty tags keep the name's values: %+v", got)
	}
}

func slots(titles ...string) []TrackSlot {
	var out []TrackSlot
	for i, t := range titles {
		out = append(out, TrackSlot{Disc: 1, Position: i + 1, Title: t})
	}
	return out
}

func matched(files []FileInfo, s []TrackSlot) (map[string]string, []string) {
	m, un := MatchTracks(files, s)
	out := map[string]string{}
	for _, x := range m {
		out[filepath.Base(files[x.File].Path)] = s[x.Slot].Title
	}
	var left []string
	for _, i := range un {
		left = append(left, filepath.Base(files[i].Path))
	}
	return out, left
}

func identifyAll(paths ...string) []FileInfo {
	var out []FileInfo
	for _, p := range paths {
		out = append(out, IdentifyFile(p, nil))
	}
	return out
}

func TestMatchTracks(t *testing.T) {
	album := slots("Airbag", "Paranoid Android", "Subterranean Homesick Alien")

	t.Run("by number and title", func(t *testing.T) {
		got, left := matched(identifyAll("01 - Airbag.flac", "02 - Paranoid Android.flac", "03 - Subterranean Homesick Alien.flac"), album)
		if len(got) != 3 || len(left) != 0 {
			t.Fatalf("got %v, left %v", got, left)
		}
	})

	t.Run("numbering off by a bonus track: titles win", func(t *testing.T) {
		got, left := matched(identifyAll("01 - Intro (Bonus).flac", "02 - Airbag.flac", "03 - Paranoid Android.flac", "04 - Subterranean Homesick Alien.flac"), album)
		want := map[string]string{"02 - Airbag.flac": "Airbag", "03 - Paranoid Android.flac": "Paranoid Android", "04 - Subterranean Homesick Alien.flac": "Subterranean Homesick Alien"}
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(left, []string{"01 - Intro (Bonus).flac"}) {
			t.Fatalf("got %v, left %v", got, left)
		}
	})

	t.Run("scene names carry the artist", func(t *testing.T) {
		got, _ := matched(identifyAll("01-radiohead-airbag.mp3", "02-radiohead-paranoid_android.mp3"), album)
		if got["01-radiohead-airbag.mp3"] != "Airbag" || got["02-radiohead-paranoid_android.mp3"] != "Paranoid Android" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("titles only", func(t *testing.T) {
		got, _ := matched(identifyAll("Paranoid Android.flac", "Airbag.flac"), album)
		if got["Airbag.flac"] != "Airbag" || got["Paranoid Android.flac"] != "Paranoid Android" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("numbers only, titles in another language", func(t *testing.T) {
		got, _ := matched(identifyAll("01 - Luftsack.flac", "02 - Paranoider Android.flac"), album)
		if got["01 - Luftsack.flac"] != "Airbag" || got["02 - Paranoider Android.flac"] != "Paranoid Android" {
			t.Fatalf("number fallback: %v", got)
		}
	})

	t.Run("two discs", func(t *testing.T) {
		two := []TrackSlot{{1, 1, "One"}, {1, 2, "Two"}, {2, 1, "Three"}, {2, 2, "Four"}}
		got, _ := matched(identifyAll(filepath.Join("CD1", "01 - One.flac"), filepath.Join("CD1", "02 - Two.flac"), filepath.Join("CD2", "01 - Three.flac"), "2-02 - Four.flac"), two)
		if len(got) != 4 || got["01 - Three.flac"] != "Three" || got["2-02 - Four.flac"] != "Four" {
			t.Fatalf("got %v", got)
		}
		// Numbered straight through, no disc numbers anywhere.
		got, _ = matched(identifyAll("03 - Drei.flac", "04 - Vier.flac"), two)
		if got["03 - Drei.flac"] != "Three" || got["04 - Vier.flac"] != "Four" {
			t.Fatalf("counted through the discs: %v", got)
		}
	})

	t.Run("a slot is filled once", func(t *testing.T) {
		got, left := matched(identifyAll("01 - Airbag.flac", "01 - Airbag.mp3"), album)
		if len(got) != 1 || len(left) != 1 {
			t.Fatalf("got %v, left %v", got, left)
		}
	})
}

func TestFindAudioFilesAndArtwork(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"CD2/01 - c.flac", "CD1/01 - a.flac", "CD1/02 - b.FLAC", "cover.jpg", "CD1/folder.jpg", "CD1/Cover.JPG",
		"release.nfo", ".hidden/x.flac", "__MACOSX/._y.flac", "._z.flac", "notes.txt",
	} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := FindAudioFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	var rel []string
	for _, f := range files {
		r, _ := filepath.Rel(dir, f)
		rel = append(rel, filepath.ToSlash(r))
	}
	if strings.Join(rel, ",") != "CD1/01 - a.flac,CD1/02 - b.FLAC,CD2/01 - c.flac" {
		t.Fatalf("audio files: %v", rel)
	}
	art, err := FindArtwork(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range art {
		r, _ := filepath.Rel(dir, a)
		names = append(names, filepath.ToSlash(r))
	}
	if strings.Join(names, ",") != "cover.jpg,CD1/folder.jpg" {
		t.Fatalf("artwork (the shallowest of each name): %v", names)
	}
}
