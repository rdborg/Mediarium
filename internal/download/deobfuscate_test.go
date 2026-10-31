package download

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenameFromYencGivesHiddenFilesTheirPostedName(t *testing.T) {
	dir := t.TempDir()
	names := []string{"file_0.bin", "file_1.bin", "file_2.bin", "movie.mkv", "file_4.bin", "file_5.bin"}
	var paths []string
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	yenc := []string{
		"Mind.Field.S01E01.part01.rar", // readable: used
		"x9Qk2mZp4Lw8Rt6Yv1Bn3Cs5.rar", // random but a real extension: used
		"Mind.Field.S01E01.part01.rar", // the same name again: kept apart
		"someone-elses-name.mkv",       // the file already has a real name: kept
		"x9Qk2mZp4Lw8Rt6Yv1Bn3Cs5Dh7",  // random, no extension: nothing to gain
		`..\..\evil.mkv`,               // a path in the name: only the last part counts
	}
	got := renameFromYenc(paths, yenc)
	want := []string{"Mind.Field.S01E01.part01.rar", "x9Qk2mZp4Lw8Rt6Yv1Bn3Cs5.rar", "Mind.Field.S01E01.part01 (2).rar", "movie.mkv", "file_4.bin", "evil.mkv"}
	for i, w := range want {
		if filepath.Base(got[i]) != w || filepath.Dir(got[i]) != dir {
			t.Errorf("file %d is %q, want %q in the download folder", i, got[i], w)
		}
		if _, err := os.Stat(got[i]); err != nil {
			t.Errorf("file %d: %v", i, err)
		}
	}
}

func TestLooksObfuscated(t *testing.T) {
	for name, want := range map[string]bool{
		"file_3.bin":                           true,
		"0123456789abcdef0123456789abcdef.mkv": true,
		"a1b2c3":                               true,
		"Movie.2024.1080p.mkv":                 false,
		"show.s01e01.r00":                      false,
	} {
		if got := looksObfuscated(name); got != want {
			t.Errorf("looksObfuscated(%q) = %v, want %v", name, got, want)
		}
	}
}
