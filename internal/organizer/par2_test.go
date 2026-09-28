package organizer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ryanborg/mediarium/internal/organizer"
)

func TestFindMainPar2Files(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"movie.par2", "movie.vol000+01.par2", "movie.vol001+02.par2", "movie.mkv"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	got, err := organizer.FindMainPar2Files(dir)
	if err != nil {
		t.Fatalf("find main par2 files: %v", err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "movie.par2" {
		t.Fatalf("expected only movie.par2, got %v", got)
	}
}

func TestPar2VerifyAndRepairRealBinary(t *testing.T) {
	repairer := organizer.NewRepairer()
	if !repairer.Available() {
		t.Skip("par2 binary not available on this machine — skipping real verify/repair test")
	}

	dir := t.TempDir()
	dataFile := filepath.Join(dir, "movie.mkv")
	original := make([]byte, 200_000)
	for i := range original {
		original[i] = byte(i % 251)
	}
	if err := os.WriteFile(dataFile, original, 0o644); err != nil {
		t.Fatalf("write fixture data file: %v", err)
	}

	par2File := filepath.Join(dir, "movie.par2")
	// 10% redundancy is enough recovery data to fix the corruption this
	// test introduces below.
	createCmd := exec.Command("par2", "create", "-r10", par2File, dataFile)
	if out, err := createCmd.CombinedOutput(); err != nil {
		t.Fatalf("create par2 recovery set: %v: %s", err, out)
	}

	result, err := repairer.Verify(par2File)
	if err != nil {
		t.Fatalf("verify (intact): %v", err)
	}
	if !result.OK {
		t.Fatalf("expected intact file to verify OK, output: %s", result.Output)
	}

	// Corrupt a chunk of the data file in place, then confirm verify now
	// reports damage and repair fixes it.
	corrupted, err := os.OpenFile(dataFile, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open data file for corruption: %v", err)
	}
	if _, err := corrupted.WriteAt(make([]byte, 5000), 1000); err != nil {
		t.Fatalf("corrupt data file: %v", err)
	}
	corrupted.Close()

	result, err = repairer.Verify(par2File)
	if err != nil {
		t.Fatalf("verify (corrupted): %v", err)
	}
	if result.OK {
		t.Fatal("expected corrupted file to fail verification")
	}

	if err := repairer.Repair(par2File); err != nil {
		t.Fatalf("repair: %v", err)
	}

	result, err = repairer.Verify(par2File)
	if err != nil {
		t.Fatalf("verify (post-repair): %v", err)
	}
	if !result.OK {
		t.Fatalf("expected repaired file to verify OK, output: %s", result.Output)
	}
}
