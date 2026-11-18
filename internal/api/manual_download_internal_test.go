package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/settings"
)

func settingsServer(t *testing.T) *Server {
	t.Helper()
	db, err := OpenTestDB(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Server{Settings: settings.New(db, nil)}
}

func TestCheckSettingsDefaults(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		saved string
		got   func(*Server) any
		want  any
	}{
		{"retention unset is off", settings.KeyRetentionDays, "", func(s *Server) any { return s.retentionDays() }, 0},
		{"retention saved", settings.KeyRetentionDays, "1500", func(s *Server) any { return s.retentionDays() }, 1500},
		{"retention with spaces", settings.KeyRetentionDays, " 800 ", func(s *Server) any { return s.retentionDays() }, 800},
		{"retention nonsense is off", settings.KeyRetentionDays, "lots", func(s *Server) any { return s.retentionDays() }, 0},
		{"retention negative is off", settings.KeyRetentionDays, "-3", func(s *Server) any { return s.retentionDays() }, 0},
		{"retention too big is off", settings.KeyRetentionDays, "999999", func(s *Server) any { return s.retentionDays() }, 0},
		{"programs: unset is on", settings.KeyRejectExecutables, "", func(s *Server) any { return s.rejectExecutables() }, true},
		{"programs: 1 is on", settings.KeyRejectExecutables, "1", func(s *Server) any { return s.rejectExecutables() }, true},
		{"programs: 0 is off", settings.KeyRejectExecutables, "0", func(s *Server) any { return s.rejectExecutables() }, false},
		{"duplicates: unset is off", settings.KeySkipDuplicates, "", func(s *Server) any { return s.skipDuplicates() }, false},
		{"duplicates: 1 is on", settings.KeySkipDuplicates, "1", func(s *Server) any { return s.skipDuplicates() }, true},
		{"duplicates: 0 is off", settings.KeySkipDuplicates, "0", func(s *Server) any { return s.skipDuplicates() }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := settingsServer(t)
			if tt.saved != "" {
				if err := s.Settings.Set(tt.key, tt.saved, false); err != nil {
					t.Fatal(err)
				}
			}
			if got := tt.got(s); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func torrentBytes(name string, files map[string]int) []byte {
	var b strings.Builder
	b.WriteString("d4:infod")
	if len(files) == 0 {
		fmt.Fprintf(&b, "6:lengthi5e")
	} else {
		b.WriteString("5:filesl")
		for n, size := range files {
			fmt.Fprintf(&b, "d6:lengthi%de4:pathl%d:%see", size, len(n), n)
		}
		b.WriteString("e")
	}
	fmt.Fprintf(&b, "4:name%d:%s12:piece lengthi16384e6:pieces20:AAAAAAAAAAAAAAAAAAAAee", len(name), name)
	return []byte(b.String())
}

func TestCheckTorrentFileSafe(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		off     bool
		wantBad bool
	}{
		{"a clean torrent", torrentBytes("Movie", map[string]int{"movie.mkv": 100, "movie.nfo": 1}), false, false},
		{"a program next to the video", torrentBytes("Movie", map[string]int{"movie.mkv": 100, "Player.exe": 1}), false, true},
		{"a program in a folder", torrentBytes("Movie", map[string]int{"movie.mkv": 100, "Extras/readme.scr": 1}), false, true},
		{"a single file that is a program", torrentBytes("setup.exe", nil), false, true},
		{"a single clean file", torrentBytes("movie.mkv", nil), false, false},
		{"the check switched off", torrentBytes("Movie", map[string]int{"Player.exe": 1}), true, false},
		{"a file that is not a torrent is left to the client", []byte("not a torrent"), false, false},
		{"a nesting bomb is left to the client, which refuses it", append([]byte("d1:x"), strings.Repeat("l", 6<<20)...), false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := settingsServer(t)
			if tt.off {
				if err := s.Settings.Set(settings.KeyRejectExecutables, "0", false); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(t.TempDir(), "release.torrent")
			if err := os.WriteFile(path, tt.data, 0o644); err != nil {
				t.Fatal(err)
			}
			err := s.checkTorrentFileSafe(path)
			var unsafe *download.UnsafeFilesError
			got := errors.As(err, &unsafe)
			if got != tt.wantBad {
				t.Errorf("err = %v, want unsafe = %v", err, tt.wantBad)
			}
			if tt.wantBad && !isBadRelease(err) {
				t.Error("a release with a program in it must count as a bad release, so it is blocklisted")
			}
		})
	}
}

// A torrent whose own name says nothing still has files that do.
func TestInspectReleaseReadsTheNameFromInsideATorrent(t *testing.T) {
	info, err := inspectRelease(torrentBytes("m2", map[string]int{"Heat.1995.1080p.BluRay.x264-GRP.mkv": 900, "Heat.1995.1080p.BluRay.x264-GRP.nfo": 10, "Sample/sample.mkv": 50}))
	if err != nil {
		t.Fatal(err)
	}
	if info.name != "m2" || info.inside != "Heat.1995.1080p.BluRay.x264-GRP" {
		t.Errorf("name %q, inside %q", info.name, info.inside)
	}
	if got := chooseReleaseName("", info.name, "m2", info.inside); got != "Heat.1995.1080p.BluRay.x264-GRP" {
		t.Errorf("the download goes by %q", got)
	}
}

func TestInspectRelease(t *testing.T) {
	const nzb = `<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><file subject="[1/1] &quot;Movie.mkv&quot; yEnc (1/1)"><groups><group>alt.binaries.test</group></groups><segments><segment bytes="500" number="1">abc@example</segment></segments></file></nzb>`
	tests := []struct {
		name     string
		data     string
		wantKind releaseKind
		wantSize int64
		wantErr  string
	}{
		{"an NZB", nzb, kindNZB, 500, ""},
		{"an NZB with a byte order mark and spaces first", "\xef\xbb\xbf\n  " + nzb, kindNZB, 500, ""},
		{"a torrent", string(torrentBytes("Movie.mkv", nil)), kindTorrent, 5, ""},
		{"a multi-file torrent", string(torrentBytes("Movie", map[string]int{"a.mkv": 100, "b.srt": 20})), kindTorrent, 120, ""},
		{"nothing", "", "", 0, "empty"},
		{"only spaces", "  \n ", "", 0, "empty"},
		{"a web page", "<html></html>", "", 0, "working NZB"},
		{"an NZB without files", `<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"></nzb>`, "", 0, "doesn't list any files"},
		{"a cut-off torrent", "d4:infod4:name3:ab", "", 0, "working .torrent"},
		// Millions of nested lists end the program if the library decodes them.
		{"a nesting bomb", "d1:x" + strings.Repeat("l", 6<<20), "", 0, "working .torrent"},
		{"a zip", "PK\x03\x04", "", 0, "isn't an NZB or a .torrent"},
		{"text", "hello", "", 0, "isn't an NZB or a .torrent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := inspectRelease([]byte(tt.data))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if info.kind != tt.wantKind || info.size != tt.wantSize {
				t.Errorf("kind %s size %d, want %s %d", info.kind, info.size, tt.wantKind, tt.wantSize)
			}
		})
	}
}

func TestMagnetInfo(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		link     string
		wantName string
		wantErr  bool
	}{
		{"magnet:?xt=urn:btih:" + hash + "&dn=Some+Movie+2020", "Some Movie 2020", false},
		{"magnet:?xt=urn:btih:" + hash, "", false},
		{"magnet:?dn=Something", "", true},
		{"magnet:?xt=urn:btih:nothex", "", true},
		{"magnet:", "", true},
	}
	for _, tt := range tests {
		name, err := magnetInfo(tt.link)
		if (err != nil) != tt.wantErr || name != tt.wantName {
			t.Errorf("magnetInfo(%q) = %q, %v; want %q, error %v", tt.link, name, err, tt.wantName, tt.wantErr)
		}
	}
}

func TestReleaseNames(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"nzb file", nameFromFile("The.Movie.2020.1080p-GRP.nzb"), "The.Movie.2020.1080p-GRP"},
		{"upper case ending", nameFromFile("The.Movie.NZB"), "The.Movie"},
		{"torrent in a folder", nameFromFile(`C:\Users\me\Downloads\The Show S01E02.torrent`), "The Show S01E02"},
		{"control characters go", cleanReleaseName("a\x00b\tc\n d"), "a b c d"},
		{"long names are cut", cleanReleaseName(strings.Repeat("x", 500)), strings.Repeat("x", maxManualName)},
		{"address with a release name", nameFromAddress("https://indexer.example/get/The.Movie.2020.1080p.nzb?apikey=SECRET"), "The.Movie.2020.1080p"},
		{"address that is an API call", nameFromAddress("https://indexer.example/api?t=get&id=1"), ""},
		{"address ending in an id", nameFromAddress("https://indexer.example/getnzb/0123456789abcdef.nzb"), ""},
		{"typed name wins", chooseReleaseName("  My name ", "from file"), "My name"},
		{"file name is the second choice", chooseReleaseName("", "", "from file"), "from file"},
		{"a placeholder at the end", chooseReleaseName("", "", ""), "Download added by hand"},
		{"a name that says nothing loses to one that does", chooseReleaseName("", "m2", "Heat.1995.1080p.BluRay.x264-GRP"), "Heat.1995.1080p.BluRay.x264-GRP"},
		{"equally poor names keep the first", chooseReleaseName("", "my movie night", "movie final"), "my movie night"},
		{"a file name with the quality stays", chooseReleaseName("", "Heat.1995.1080p.BluRay.x264-GRP", "Heat"), "Heat.1995.1080p.BluRay.x264-GRP"},
		{"the better of two real names wins", chooseReleaseName("", "Heat 1995", "Heat.1995.2160p.WEB-DL.DDP5.1.x265-GRP"), "Heat.1995.2160p.WEB-DL.DDP5.1.x265-GRP"},
		{"an empty name is skipped", chooseReleaseName("", "", "  ", "Show.S01E02.720p.HDTV.x264-GRP"), "Show.S01E02.720p.HDTV.x264-GRP"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestUploadReferences(t *testing.T) {
	s := &Server{}
	s.cfg.ConfigDir = t.TempDir()
	ref, err := s.saveUpload([]byte("<nzb/>"), "nzb")
	if err != nil {
		t.Fatal(err)
	}
	if !isUploadRef(ref) || !strings.HasSuffix(ref, ".nzb") {
		t.Fatalf("ref = %q", ref)
	}
	got, err := s.readUpload(ref)
	if err != nil || string(got) != "<nzb/>" {
		t.Fatalf("readUpload = %q, %v", got, err)
	}
	// Only names this code made are followed: nothing can point out of the folder.
	for _, bad := range []string{
		"upload:../../etc/passwd", "upload:" + strings.Repeat("a", 32) + ".exe", "upload:" + strings.Repeat("A", 32) + ".nzb",
		"upload:" + strings.Repeat("a", 31) + ".nzb", "upload:", "upload:/etc/passwd", `upload:..\x.nzb`, "upload:" + strings.Repeat("a", 32) + ".nzb/../x",
	} {
		if _, err := s.readUpload(bad); err == nil {
			t.Errorf("readUpload(%q) should be refused", bad)
		}
	}
	// A reference to a file that is gone says so in plain words.
	if _, err := s.readUpload("upload:" + strings.Repeat("a", 32) + ".nzb"); err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Errorf("a missing file: err = %v", err)
	}
	s.removeUpload(ref)
	if _, err := s.readUpload(ref); err == nil {
		t.Error("the file should be gone after removeUpload")
	}
}

func TestTVTargetMismatch(t *testing.T) {
	tests := []struct {
		title   string
		target  manualTarget
		wantMsg string
	}{
		{"Show.S01E02.1080p.WEB-DL", manualTarget{seriesID: 1, season: 1, episode: 2}, ""},
		{"Show.S01.1080p.WEB-DL", manualTarget{seriesID: 1, season: 1}, ""},
		{"randomname", manualTarget{seriesID: 1, season: 3, episode: 4}, ""},
		{"Show.S01E02.1080p", manualTarget{seriesID: 1, season: 1, episode: 3}, "says episode 2, but you chose episode 3"},
		{"Show.S02E02.1080p", manualTarget{seriesID: 1, season: 1, episode: 2}, "says season 2, but you chose season 1"},
		{"Show.S01.1080p", manualTarget{seriesID: 1, season: 1, episode: 2}, "whole season"},
		{"Show.S01E02.1080p", manualTarget{seriesID: 1, season: 1}, "an episode"},
		{"Show.S01E01E02.1080p", manualTarget{seriesID: 1, season: 1, episode: 2}, ""},
	}
	for _, tt := range tests {
		got := tvTargetMismatch(tt.title, tt.target)
		if tt.wantMsg == "" && got != "" || tt.wantMsg != "" && !strings.Contains(got, tt.wantMsg) {
			t.Errorf("%q for %+v: got %q, want %q", tt.title, tt.target, got, tt.wantMsg)
		}
	}
}
