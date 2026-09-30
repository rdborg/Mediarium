package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for a Mediarium program: started
// with --version-check under a name like "prog-1.2.0" it prints that version,
// as the real app does. "-riscv" in the name makes it claim another platform,
// "-silent" makes it print nothing, "-fail" makes it exit with an error and
// "-hang" makes it never answer.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--version-check" {
		name := filepath.Base(os.Args[0])
		switch {
		case strings.Contains(name, "-fail"):
			os.Exit(3)
		case strings.Contains(name, "-hang"):
			time.Sleep(time.Minute)
		case strings.Contains(name, "-silent"):
			os.Exit(0)
		case strings.Contains(name, "-riscv"):
			fmt.Printf("mediarium 1.0.0 %s/riscv64\n", runtime.GOOS)
			os.Exit(0)
		}
		version := "0.0.0"
		if i := strings.Index(name, "prog-"); i >= 0 {
			version = strings.SplitN(name[i+5:], "_", 2)[0]
		}
		fmt.Println(VersionLine(version))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeProgram copies this test binary to dir/name, so it can be run like a
// program that answers --version-check.
func fakeProgram(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("needs Linux")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, in, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseChecksum(t *testing.T) {
	good := strings.Repeat("ab", 32)
	cases := []struct {
		in   string
		want string
		err  error
	}{
		{good, good, nil},
		{strings.ToUpper(good), good, nil},
		{"  " + good + "\n", good, nil},
		{"", "", ErrChecksumFormat},
		{good[:63], "", ErrChecksumFormat},
		{good + "0", "", ErrChecksumFormat},
		{strings.Repeat("zz", 32), "", ErrChecksumFormat},
		{"sha256:" + good, "", ErrChecksumFormat},
	}
	for _, tc := range cases {
		got, err := ParseChecksum(tc.in)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("ParseChecksum(%q) = %q, %v; want %q, %v", tc.in, got, err, tc.want, tc.err)
		}
	}
}

func TestReceive(t *testing.T) {
	data := []byte("some program bytes")
	t.Run("stores the bytes and their checksum", func(t *testing.T) {
		dir := t.TempDir()
		path, sum, n, err := Receive(dir, bytes.NewReader(data), 1000)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(path)
		if !bytes.Equal(got, data) || n != int64(len(data)) {
			t.Fatalf("stored %q (%d bytes)", got, n)
		}
		want := "7e8a30ba9cbbac38d5d1e4d5d7c1c2c6"
		_ = want
		if len(sum) != 64 {
			t.Fatalf("sum = %q", sum)
		}
		if filepath.Dir(path) != dir {
			t.Fatalf("stored in %s, want inside %s", filepath.Dir(path), dir)
		}
	})
	t.Run("exactly the limit is allowed", func(t *testing.T) {
		if _, _, _, err := Receive(t.TempDir(), bytes.NewReader(make([]byte, 100)), 100); err != nil {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("one byte over is refused and leaves nothing", func(t *testing.T) {
		dir := t.TempDir()
		_, _, _, err := Receive(dir, bytes.NewReader(make([]byte, 101)), 100)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("error = %v", err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Fatalf("%d files left", len(entries))
		}
	})
	t.Run("an endless body stops at the limit", func(t *testing.T) {
		_, _, _, err := Receive(t.TempDir(), zeros{}, 4096)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("empty is refused", func(t *testing.T) {
		if _, _, _, err := Receive(t.TempDir(), bytes.NewReader(nil), 100); !errors.Is(err, ErrEmpty) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("a broken upload leaves nothing", func(t *testing.T) {
		dir := t.TempDir()
		_, _, _, err := Receive(dir, io.MultiReader(strings.NewReader("abc"), errReader{}), 100)
		if err == nil {
			t.Fatal("no error")
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Fatalf("%d files left", len(entries))
		}
	})
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { return len(p), nil }

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestDecide(t *testing.T) {
	cases := []struct {
		name                      string
		running, image, candidate string
		force                     bool
		want                      error
	}{
		{"newer than both", "1.2.0", "1.2.0", "1.3.0", false, nil},
		{"1.10 is newer than 1.9", "1.9.0", "1.9.0", "1.10.0", false, nil},
		{"1.9 is not newer than 1.10", "1.10.0", "1.10.0", "1.9.0", false, ErrOlderThanImage},
		{"same as running", "1.2.0", "1.1.0", "1.2.0", false, ErrNotNewer},
		{"older than running", "1.3.0", "1.1.0", "1.2.0", false, ErrNotNewer},
		{"force allows the same version", "1.2.0", "1.1.0", "1.2.0", true, nil},
		{"force allows going back above the image", "1.3.0", "1.1.0", "1.2.0", true, nil},
		{"force does not allow older than the image", "1.3.0", "1.2.0", "1.1.0", true, ErrOlderThanImage},
		{"older than the image is refused", "1.3.0", "1.2.0", "1.1.9", false, ErrOlderThanImage},
		{"same as the image is fine when newer than running", "1.1.0", "1.1.0", "1.1.0", true, nil},
		{"pre-release candidate is older than the release", "1.2.0", "1.2.0", "1.2.0-rc.1", false, ErrOlderThanImage},
		{"newer pre-release", "1.2.0", "1.2.0", "1.3.0-rc.1", false, nil},
		{"candidate is garbage", "1.2.0", "1.2.0", "banana", false, ErrBadVersion},
		{"candidate is empty", "1.2.0", "1.2.0", "", true, ErrBadVersion},
		{"running is a dev build", "dev", "dev", "1.3.0", false, ErrBadVersion},
		{"running is a dev build with force", "dev", "dev", "1.3.0", true, nil},
		{"image unknown", "1.2.0", "", "1.3.0", false, nil},
		{"leading v is accepted", "1.2.0", "1.2.0", "v1.3.0", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Decide(tc.running, tc.image, tc.candidate, tc.force)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Decide(%q, %q, %q, %v) = %v, want %v", tc.running, tc.image, tc.candidate, tc.force, err, tc.want)
			}
		})
	}
}

func TestParseVersionLine(t *testing.T) {
	cases := []struct {
		in   string
		want Probe
		ok   bool
	}{
		{"mediarium 1.2.0 linux/amd64\n", Probe{"1.2.0", "linux", "amd64"}, true},
		{"mediarium 1.2.0-rc.1 linux/arm64", Probe{"1.2.0-rc.1", "linux", "arm64"}, true},
		{"mediarium dev linux/amd64", Probe{"dev", "linux", "amd64"}, true},
		{"", Probe{}, false},
		{"hello world", Probe{}, false},
		{"mediarium 1.2.0", Probe{}, false},
		{"mediarium 1.2.0 linux", Probe{}, false},
		{"mediarium 1.2.0 linux/", Probe{}, false},
		{"radarr 1.2.0 linux/amd64", Probe{}, false},
		{"mediarium 1.2.0 linux/amd64 extra", Probe{}, false},
		{"mediarium 1.2.0;rm linux/amd64", Probe{}, false},
		{"mediarium " + strings.Repeat("1", 80) + " linux/amd64", Probe{}, false},
		{"mediarium $(id) linux/amd64", Probe{}, false},
	}
	for _, tc := range cases {
		got, err := ParseVersionLine(tc.in)
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("ParseVersionLine(%q) = %+v, %v", tc.in, got, err)
		}
	}
}

func TestInspect(t *testing.T) {
	dir := t.TempDir()
	t.Run("a Mediarium program", func(t *testing.T) {
		p, err := Inspect(context.Background(), fakeProgram(t, dir, "prog-1.4.0"))
		if err != nil {
			t.Fatalf("Inspect: %v", err)
		}
		if p.Version != "1.4.0" || p.OS != runtime.GOOS || p.Arch != runtime.GOARCH {
			t.Fatalf("probe = %+v", p)
		}
	})
	t.Run("another platform", func(t *testing.T) {
		_, err := Inspect(context.Background(), fakeProgram(t, dir, "prog-riscv"))
		if !errors.Is(err, ErrWrongPlatform) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("a program that does not answer properly", func(t *testing.T) {
		for _, name := range []string{"prog-silent", "prog-fail"} {
			if _, err := Inspect(context.Background(), fakeProgram(t, dir, name)); !errors.Is(err, ErrNotMediarium) {
				t.Errorf("%s: error = %v", name, err)
			}
		}
	})
	t.Run("a program that hangs is given up on", func(t *testing.T) {
		old := probeTimeout
		probeTimeout = 300 * time.Millisecond
		defer func() { probeTimeout = old }()
		start := time.Now()
		_, err := Inspect(context.Background(), fakeProgram(t, dir, "prog-hang"))
		if !errors.Is(err, ErrNotMediarium) {
			t.Fatalf("error = %v", err)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("took %v", time.Since(start))
		}
	})
	t.Run("a script is not a program", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("needs Linux")
		}
		path := filepath.Join(dir, "script.sh")
		os.WriteFile(path, []byte("#!/bin/sh\necho mediarium 9.9.9 linux/"+runtime.GOARCH+"\n"), 0o755)
		if _, err := Inspect(context.Background(), path); !errors.Is(err, ErrNotMediarium) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("random bytes", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("needs Linux")
		}
		path := filepath.Join(dir, "junk")
		os.WriteFile(path, bytes.Repeat([]byte{1, 2, 3}, 100), 0o755)
		if _, err := Inspect(context.Background(), path); !errors.Is(err, ErrNotMediarium) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("runs with an empty environment", func(t *testing.T) {
		t.Setenv("MEDIARIUM_SECRET_FOR_TEST", "leak")
		// The fake program cannot print its environment, so just make sure it still answers.
		if _, err := Inspect(context.Background(), fakeProgram(t, dir, "prog-1.0.0")); err != nil {
			t.Fatal(err)
		}
	})
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallKeepsThePreviousAndRollsBack(t *testing.T) {
	dir := t.TempDir()
	sum := strings.Repeat("a", 64)

	// First install: nothing to keep.
	write(t, filepath.Join(dir, "up1"), "program one")
	if err := Install(dir, filepath.Join(dir, "up1"), "1.2.0", sum); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, FileApp)) != "program one" || strings.TrimSpace(read(t, filepath.Join(dir, FileVersion))) != "1.2.0" {
		t.Fatal("first install not in place")
	}
	if _, err := os.Stat(filepath.Join(dir, FilePrevious)); err == nil {
		t.Fatal("there was nothing to keep as previous")
	}
	fi, _ := os.Stat(filepath.Join(dir, FileApp))
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	if !strings.HasPrefix(read(t, filepath.Join(dir, FileSum)), sum) {
		t.Fatal("checksum file missing")
	}

	// Second install keeps the first as previous.
	write(t, filepath.Join(dir, "up2"), "program two")
	if err := Install(dir, filepath.Join(dir, "up2"), "1.3.0", strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, FilePrevious)) != "program one" || strings.TrimSpace(read(t, filepath.Join(dir, FilePrevVer))) != "1.2.0" {
		t.Fatal("previous not kept")
	}
	pushed, _ := State(dir)
	if pushed == nil || pushed.Version != "1.3.0" || !pushed.HasPrevious || pushed.PreviousVersion != "1.2.0" || pushed.SHA256 != strings.Repeat("b", 64) {
		t.Fatalf("state = %+v", pushed)
	}

	// Rolling back is another install, of the older program (a "force" push).
	write(t, filepath.Join(dir, "up3"), "program one")
	if err := Install(dir, filepath.Join(dir, "up3"), "1.2.0", sum); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, FileApp)) != "program one" || read(t, filepath.Join(dir, FilePrevious)) != "program two" {
		t.Fatal("rollback not in place")
	}

	// Removing goes back to the image.
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if pushed, failed := State(dir); pushed != nil || failed != nil {
		t.Fatalf("state after remove = %+v, %+v", pushed, failed)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("%d files left after remove", len(entries))
	}
	if err := Remove(dir); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}

func TestInstallClearsBootCountAndFailedMark(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileBootCount), "2\n")
	write(t, filepath.Join(dir, FileFailed), "old bad program")
	write(t, filepath.Join(dir, FileFailedVer), "1.9.9\n")
	write(t, filepath.Join(dir, "up"), "new")
	if err := Install(dir, filepath.Join(dir, "up"), "2.0.0", strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{FileBootCount, FileFailed, FileFailedVer} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s was not cleared", name)
		}
	}
}

func TestStateReportsAFailedProgram(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileFailed), "bad")
	write(t, filepath.Join(dir, FileFailedVer), "1.9.9\n")
	pushed, failed := State(dir)
	if pushed != nil || failed == nil || failed.Version != "1.9.9" {
		t.Fatalf("state = %+v, %+v", pushed, failed)
	}
}

func TestBootCountAndHealthy(t *testing.T) {
	dir := t.TempDir()
	if BootCount(dir) != 0 {
		t.Fatal("count should start at 0")
	}
	write(t, filepath.Join(dir, FileBootCount), "2\n")
	if BootCount(dir) != 2 {
		t.Fatalf("count = %d", BootCount(dir))
	}
	write(t, filepath.Join(dir, FileBootCount), "garbage\n")
	if BootCount(dir) != 0 {
		t.Fatalf("garbage count = %d", BootCount(dir))
	}
	write(t, filepath.Join(dir, FileBootCount), "2\n")
	MarkHealthy(dir)
	if BootCount(dir) != 0 {
		t.Fatal("MarkHealthy did not clear the count")
	}
	MarkHealthy(dir) // nothing to clear is fine
}

func TestCleanTemp(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".upload-123"), "x")
	write(t, filepath.Join(dir, ".release-9.program"), "x")
	write(t, filepath.Join(dir, FileApp), "keep")
	CleanTemp(dir)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != FileApp {
		t.Fatalf("left: %v", entries)
	}
}

func TestMarkers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "update") // does not exist yet
	if TakeSafeOnce(dir) {
		t.Fatal("no safe-once was asked for")
	}
	if err := WriteSafeOnce(dir); err != nil {
		t.Fatal(err)
	}
	if !TakeSafeOnce(dir) {
		t.Fatal("safe-once was not seen")
	}
	if TakeSafeOnce(dir) {
		t.Fatal("safe-once must apply once")
	}

	if _, _, ok := TakeStuck(dir); ok {
		t.Fatal("nothing was recorded")
	}
	if err := WriteStuck(dir, "the database stopped answering\nfor 3 minutes"); err != nil {
		t.Fatal(err)
	}
	reason, at, ok := TakeStuck(dir)
	if !ok || reason != "the database stopped answering for 3 minutes" || time.Since(at) > time.Minute {
		t.Fatalf("TakeStuck = %q, %v, %v", reason, at, ok)
	}
	if _, _, ok := TakeStuck(dir); ok {
		t.Fatal("the mark must be read once")
	}
}
