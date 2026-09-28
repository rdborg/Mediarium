package api

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanborg/mediarium/internal/organizer"
)

// A full disk or a missing 7z is our setup's fault and must never blocklist
// a good release; corrupt, unsafe or password-protected archives are the
// release's fault.
func TestUnpackFailureBlamesTheReleaseOnlyWhenItIsAtFault(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantBad bool
	}{
		{"password protected", fmt.Errorf("extract x.rar: %w", organizer.ErrPasswordProtected), true},
		{"unsafe paths", fmt.Errorf("extract x.zip: %w", organizer.ErrUnsafeArchive), true},
		{"zip bomb", fmt.Errorf("extract x.zip: %w", organizer.ErrArchiveTooLarge), true},
		{"corrupt", errors.New("rardecode: bad header crc"), true},
		{"disk full", fmt.Errorf("extract x.rar: %w", organizer.ErrInsufficientSpace), false},
		{"7z missing", fmt.Errorf("extract x.7z: %w", organizer.ErrSevenZipMissing), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unpackFailure(tt.err)
			if isBadRelease(got) != tt.wantBad {
				t.Errorf("isBadRelease = %v, want %v", isBadRelease(got), tt.wantBad)
			}
			if !errors.Is(got, tt.err) {
				t.Error("the original error must stay reachable with errors.Is")
			}
		})
	}
}

// unpackArchives needs no external tool for ZIP any more.
func TestUnpackArchivesHandlesZipWithoutSevenZip(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "release.zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("Movie.2001.mkv")
	w.Write([]byte("video"))
	zw.Close()
	f.Close()

	if err := unpackArchives(dir); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "Movie.2001.mkv")); err != nil || string(got) != "video" {
		t.Fatalf("extracted file: %q, %v", got, err)
	}
}
