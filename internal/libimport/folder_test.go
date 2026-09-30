package libimport_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/rdborg/mediarium/internal/libimport"
)

func TestScanMovieFolder(t *testing.T) {
	tests := []struct {
		name    string
		folder  string
		files   map[string]int
		want    map[string]string // file (relative) -> quality
		wantErr bool
	}{
		{
			name:   "quality from the file name",
			folder: "Inception (2010)",
			files:  map[string]int{"Inception (2010) Bluray-1080p.mkv": 10, "Inception (2010)-trailer.mkv": 1, "Extras/Making Of.mkv": 1},
			want:   map[string]string{"Inception (2010) Bluray-1080p.mkv": "Bluray-1080p"},
		},
		{
			name:   "file name without a title still counts, quality from the folder",
			folder: "Heat (1995) 720p BluRay",
			files:  map[string]int{"movie.mkv": 10, "notes.txt": 1},
			want:   map[string]string{"movie.mkv": "Bluray-720p"},
		},
		{
			name:   "nothing known about quality",
			folder: "Heat (1995)",
			files:  map[string]int{"movie.mkv": 10},
			want:   map[string]string{"movie.mkv": "Unknown"},
		},
		{name: "missing folder", folder: "Nope (2001)", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, tt.folder)
			for rel, size := range tt.files {
				touch(t, dir, rel, size)
			}
			got, err := libimport.ScanMovieFolder(dir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error for a missing folder")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d files %+v, want %d", len(got), got, len(tt.want))
			}
			for _, f := range got {
				rel, _ := filepath.Rel(dir, f.Path)
				if q, ok := tt.want[filepath.ToSlash(rel)]; !ok || q != f.Quality {
					t.Fatalf("file %s quality %q, want %q (listed: %v)", rel, f.Quality, q, ok)
				}
			}
		})
	}
}

func TestScanSeriesFolder(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Season 01/S01E01 - Pilot.mkv", 10) // no show name in the file
	touch(t, root, "Season 01/Show.S01E02.720p.HDTV.x264-GRP.mkv", 10)
	touch(t, root, "Season 01/Show.S01E02.1080p.WEB-DL.mkv", 20) // bigger copy of E02 wins
	touch(t, root, "Season 02/Show - S02E01E02 - Double.mkv", 10)
	touch(t, root, "Season 02/Behind the scenes.mkv", 10)
	touch(t, root, "Show.S03E01.mkv", 10) // straight in the show folder

	files, skipped, err := libimport.ScanSeriesFolder(root)
	if err != nil {
		t.Fatal(err)
	}
	type ep struct {
		season, first, count int
		quality              string
	}
	var got []ep
	for _, f := range files {
		got = append(got, ep{f.Season, f.Episodes[0], len(f.Episodes), f.Quality})
	}
	sort.Slice(got, func(i, j int) bool {
		if got[i].season != got[j].season {
			return got[i].season < got[j].season
		}
		return got[i].first < got[j].first
	})
	want := []ep{{1, 1, 1, "Unknown"}, {1, 2, 1, "WEBDL-1080p"}, {2, 1, 2, "Unknown"}, {3, 1, 1, "Unknown"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("episode %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(skipped) != 2 {
		t.Fatalf("expected the duplicate and the unnumbered file to be skipped, got %v", skipped)
	}
}
