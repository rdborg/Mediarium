package download

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsafeExtension(t *testing.T) {
	tests := []struct {
		name string
		want string
		bad  bool
	}{
		{"Movie.2024.1080p.mkv", "", false},
		{"Movie.2024.1080p.mkv.exe", ".exe", true},
		{"setup.EXE", ".exe", true},
		{"Codec Pack.scr", ".scr", true},
		{"watch me.lnk", ".lnk", true},
		{"run.bat", ".bat", true},
		{"run.cmd", ".cmd", true},
		{"install.msi", ".msi", true},
		{"a.vbs", ".vbs", true},
		{"a.js", ".js", true},
		{"a.jar", ".jar", true},
		{"a.ps1", ".ps1", true},
		{"a.pif", ".pif", true},
		{"a.hta", ".hta", true},
		{"a.cpl", ".cpl", true},
		{"a.sh", ".sh", true},
		{"a.arj", ".arj", true},
		{"a.zipx", ".zipx", true},
		// trailing dots and spaces are dropped by the system
		{"setup.exe.", ".exe", true},
		{"setup.exe . ", ".exe", true},
		// folders written either way
		{`sub\dir\setup.exe`, ".exe", true},
		{"sub/dir/setup.exe", ".exe", true},
		// only the last ending counts
		{"film.exe.mkv", "", false},
		// endings that honest releases carry
		{"www.example.com", "", false},
		{"Torrent downloaded from.url", "", false},
		{"movie.nfo", "", false},
		{"movie.srt", "", false},
		{"movie.rar", "", false},
		{"movie.par2", "", false},
		{"noextension", "", false},
		{"", "", false},
		{".exe", ".exe", true},
	}
	for _, tt := range tests {
		got, bad := UnsafeExtension(tt.name)
		if got != tt.want || bad != tt.bad {
			t.Errorf("UnsafeExtension(%q) = %q, %v; want %q, %v", tt.name, got, bad, tt.want, tt.bad)
		}
	}
}

func nzbWith(subjects ...string) *NZB {
	n := &NZB{}
	for i, s := range subjects {
		n.Files = append(n.Files, NZBFile{Subject: s, Segments: []NZBSegment{{Number: 1, Bytes: 10, MessageID: "id" + string(rune('a'+i)) + "@x"}}})
	}
	return n
}

func TestUnsafeInNZB(t *testing.T) {
	tests := []struct {
		name      string
		subjects  []string
		wantErr   bool
		wantInMsg []string
		wantTotal int
	}{
		{"clean release", []string{`[1/3] "Movie.2024.mkv" yEnc (1/50)`, `[2/3] "Movie.2024.par2" yEnc (1/1)`}, false, nil, 0},
		{"one executable", []string{`"Movie.2024.mkv" yEnc (1/50)`, `"Movie Player.exe" yEnc (1/2)`}, true, []string{"Movie Player.exe"}, 1},
		{"hidden behind a double ending", []string{`"Movie.2024.mkv.scr" yEnc (1/50)`}, true, []string{"Movie.2024.mkv.scr"}, 1},
		{"upper case", []string{`"READ ME.BAT" yEnc (1/1)`}, true, []string{"READ ME.BAT"}, 1},
		{"many are counted", []string{`"a.exe" yEnc (1/1)`, `"b.exe" yEnc (1/1)`, `"c.exe" yEnc (1/1)`, `"d.exe" yEnc (1/1)`, `"e.exe" yEnc (1/1)`}, true, []string{"a.exe", "b.exe", "c.exe", "and 2 more"}, 5},
		{"random names show nothing yet", []string{`"wFvDJHhDrbESDBLq" yEnc (1/50)`}, false, nil, 0},
		{"subject without quotes still shows the name", []string{`Movie.2024.exe yEnc (1/50)`}, true, []string{"Movie.2024.exe"}, 1},
		{"subject without a file name has none to judge", []string{`just some words yEnc (1/50)`}, false, nil, 0},
		{"reordering mark cannot hide the ending", []string{"\"evil‮gpj.exe\" yEnc (1/1)"}, true, nil, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UnsafeInNZB(nzbWith(tt.subjects...))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				return
			}
			var ue *UnsafeFilesError
			if !errors.As(err, &ue) {
				t.Fatalf("error is %T, want *UnsafeFilesError", err)
			}
			if ue.Total != tt.wantTotal || ue.After {
				t.Errorf("Total = %d After = %v; want %d false", ue.Total, ue.After, tt.wantTotal)
			}
			if !strings.Contains(err.Error(), "contains programs or scripts") {
				t.Errorf("message %q lacks the fixed phrase", err.Error())
			}
			for _, s := range tt.wantInMsg {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("message %q lacks %q", err.Error(), s)
				}
			}
		})
	}
}

func TestUnsafeInDir(t *testing.T) {
	write := func(t *testing.T, dir string, files ...string) {
		t.Helper()
		for _, f := range files {
			p := filepath.Join(dir, filepath.FromSlash(f))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	tests := []struct {
		name    string
		files   []string
		wantBad []string
	}{
		{"clean", []string{"Movie.mkv", "Movie.nfo", "Subs/en.srt"}, nil},
		{"empty folder", nil, nil},
		{"next to the video", []string{"Movie.mkv", "Install.exe"}, []string{"Install.exe"}},
		{"inside a folder, sorted", []string{"Movie.mkv", "Extras/z.bat", "a.scr"}, []string{"Extras/z.bat", "a.scr"}},
		{"our own bookkeeping is not judged", []string{"Movie.mkv", ".mediarium-resume.exe"}, nil},
		{"upper case", []string{"RUN.CMD"}, []string{"RUN.CMD"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, tt.files...)
			err := UnsafeInDir(dir)
			if len(tt.wantBad) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ue *UnsafeFilesError
			if !errors.As(err, &ue) {
				t.Fatalf("err = %v, want *UnsafeFilesError", err)
			}
			if !ue.After {
				t.Error("After should be set for an unpacked download")
			}
			if ue.Total != len(tt.wantBad) {
				t.Errorf("Total = %d, want %d", ue.Total, len(tt.wantBad))
			}
			for _, w := range tt.wantBad {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("message %q lacks %q", err.Error(), w)
				}
			}
			if !strings.Contains(err.Error(), "contains programs or scripts") {
				t.Errorf("message %q lacks the fixed phrase", err.Error())
			}
		})
	}
}

func TestUnsafeInDirMissingFolder(t *testing.T) {
	if err := UnsafeInDir(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Errorf("a folder that is not there has nothing unsafe in it, got %v", err)
	}
}

func TestNZBFingerprint(t *testing.T) {
	mk := func(ids ...string) *NZB {
		f := NZBFile{Subject: "x"}
		for i, id := range ids {
			f.Segments = append(f.Segments, NZBSegment{Number: i + 1, MessageID: id})
		}
		return &NZB{Files: []NZBFile{f}}
	}
	a := mk("1@x", "2@x", "3@x")
	tests := []struct {
		name string
		b    *NZB
		same bool
	}{
		{"same ids", mk("1@x", "2@x", "3@x"), true},
		{"other order", mk("3@x", "1@x", "2@x"), true},
		{"angle brackets", mk("<1@x>", "2@x", "<3@x>"), true},
		{"one id differs", mk("1@x", "2@x", "4@x"), false},
		{"one id fewer", mk("1@x", "2@x"), false},
	}
	for _, tt := range tests {
		if got := a.Fingerprint() == tt.b.Fingerprint(); got != tt.same {
			t.Errorf("%s: same = %v, want %v", tt.name, got, tt.same)
		}
	}
	// The same ids split over files differently are the same post.
	split := &NZB{Files: []NZBFile{
		{Subject: "a", Segments: []NZBSegment{{MessageID: "1@x"}}},
		{Subject: "b", Segments: []NZBSegment{{MessageID: "2@x"}, {MessageID: "3@x"}}},
	}}
	if split.Fingerprint() != a.Fingerprint() {
		t.Error("the fingerprint should not depend on how the ids are split into files")
	}
	if len(a.Fingerprint()) != 64 {
		t.Errorf("fingerprint length = %d, want 64 hex characters", len(a.Fingerprint()))
	}
}
