package organizer

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A network share, some NAS boxes and NTFS or exFAT mounts refuse chmod. The
// unpack must still leave the file in place (GitHub #189).
func TestExtractZipCarriesOnWhenChmodIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"EPERM", &os.PathError{Op: "chmod", Path: "/data/.unpack-1", Err: syscall.EPERM}},
		{"ENOTSUP", &os.PathError{Op: "chmod", Path: "/data/.unpack-1", Err: syscall.ENOTSUP}},
		{"EINVAL", &os.PathError{Op: "chmod", Path: "/data/.unpack-1", Err: syscall.EINVAL}},
		{"something else", errors.New("odd share")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := chmodFile
			chmodFile = func(*os.File, os.FileMode) error { return tc.err }
			t.Cleanup(func() { chmodFile = old })

			root := t.TempDir()
			archive := filepath.Join(root, "release.zip")
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			w, err := zw.Create("Movie.mkv")
			if err != nil {
				t.Fatal(err)
			}
			w.Write([]byte("movie bytes"))
			zw.Close()
			if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(root, "dest")
			if err := NewExtractor().Extract(archive, dest); err != nil {
				t.Fatalf("extract: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(dest, "Movie.mkv"))
			if err != nil || string(got) != "movie bytes" {
				t.Fatalf("file = %q, %v", got, err)
			}
			entries, _ := os.ReadDir(dest)
			if len(entries) != 1 {
				t.Errorf("leftovers in dest: %v", entries)
			}
		})
	}
}

func TestChmodRefused(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{syscall.EPERM, true}, {syscall.ENOTSUP, true}, {syscall.EINVAL, true},
		{os.ErrPermission, true}, {errors.New("disk full"), false}, {syscall.ENOSPC, false},
	} {
		if got := chmodRefused(tc.err); got != tc.want {
			t.Errorf("chmodRefused(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
