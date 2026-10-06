// Package selfupdate installs a new Mediarium program file next to the
// container image's own, without Docker access and without extra privileges.
//
// The container image starts /app/app. When a newer program file sits in
// <config>/update/app, the image's entrypoint (docker/entrypoint.sh in the repository) starts
// that one instead, and falls back to the image's own program if the new one
// keeps failing. This package is the app's half: it receives a file (from an
// administrator's upload, or from a signed GitHub release), checks it, puts it
// in place, and keeps the previous one so the change can be undone. Files it
// writes:
//
//	app            the installed program (mode 0755)
//	app.sha256     its SHA-256, checked by the entrypoint before every start
//	VERSION        its version number
//	app.previous   the program that was installed before (and VERSION.previous)
//	app.failed     a program the entrypoint gave up on (and VERSION.failed)
//	boot-count     starts since the program last proved itself healthy
package selfupdate

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/rdborg/mediarium/internal/semver"
)

// File names inside the update folder. The entrypoint script uses the same
// names; keep the two in step.
const (
	DirName        = "update"
	FileApp        = "app"
	FileSum        = "app.sha256"
	FileVersion    = "VERSION"
	FilePrevious   = "app.previous"
	FilePrevVer    = "VERSION.previous"
	FileFailed     = "app.failed"
	FileFailedVer  = "VERSION.failed"
	FileBootCount  = "boot-count"
	tempUploadGlob = ".upload-*"
	tempReleaseGlb = ".release-*"
)

// Limits and timings.
const (
	// MaxBinaryBytes is the biggest program file accepted (the real one is
	// about 45 MB).
	MaxBinaryBytes = 200 << 20
	// HealthyAfter is how long an installed program must keep running before
	// it counts as good and the start counter is cleared. A program that dies
	// sooner, MaxBootAttempts times in a row, is put aside by the entrypoint.
	HealthyAfter    = 20 * time.Second
	MaxBootAttempts = 3
)

// Errors the callers turn into plain sentences.
var (
	ErrTooLarge         = errors.New("the file is too large")
	ErrEmpty            = errors.New("the file is empty")
	ErrChecksumFormat   = errors.New("the checksum is not a 64-character hex SHA-256")
	ErrChecksumMismatch = errors.New("the file does not match the checksum")
	ErrNotMediarium     = errors.New("the file is not a Mediarium program")
	ErrWrongPlatform    = errors.New("the file is for another system")
	ErrBadVersion       = errors.New("the version number cannot be compared")
	ErrNotNewer         = errors.New("the version is not newer than the running one")
	ErrOlderThanImage   = errors.New("the version is older than the one in the container image")
)

// Dir is the update folder inside the config folder.
func Dir(configDir string) string { return filepath.Join(configDir, DirName) }

// Pushed describes the program installed in the update folder.
type Pushed struct {
	Version         string    `json:"version"`
	SHA256          string    `json:"sha256,omitempty"`
	InstalledAt     time.Time `json:"installedAt"`
	HasPrevious     bool      `json:"hasPrevious"`
	PreviousVersion string    `json:"previousVersion,omitempty"`
	Running         bool      `json:"running"` // this very process is that program
}

// Failed describes a program the entrypoint put aside because it kept
// stopping right after it started.
type Failed struct {
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

// State reads what is installed. Problems reading a file just mean "nothing
// there".
func State(dir string) (pushed *Pushed, failed *Failed) {
	if fi, err := os.Stat(filepath.Join(dir, FileApp)); err == nil && fi.Mode().IsRegular() {
		p := &Pushed{
			Version:     readLine(filepath.Join(dir, FileVersion)),
			SHA256:      strings.Fields(readLine(filepath.Join(dir, FileSum)) + " ")[0],
			InstalledAt: fi.ModTime().UTC(),
			Running:     RunningPushed(dir),
		}
		if _, err := os.Stat(filepath.Join(dir, FilePrevious)); err == nil {
			p.HasPrevious = true
			p.PreviousVersion = readLine(filepath.Join(dir, FilePrevVer))
		}
		pushed = p
	}
	if fi, err := os.Stat(filepath.Join(dir, FileFailed)); err == nil && fi.Mode().IsRegular() {
		failed = &Failed{Version: readLine(filepath.Join(dir, FileFailedVer)), At: fi.ModTime().UTC()}
	}
	return pushed, failed
}

func readLine(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(b) > 256 {
		b = b[:256]
	}
	return strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
}

// RunningPushed reports whether this process is the installed program.
func RunningPushed(dir string) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return false
	}
	pushed, err := filepath.EvalSymlinks(filepath.Join(dir, FileApp))
	return err == nil && self == pushed
}

// ParseChecksum reads a SHA-256 given as 64 hex digits (any case).
func ParseChecksum(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) != 64 {
		return "", ErrChecksumFormat
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", ErrChecksumFormat
	}
	return s, nil
}

// Receive copies body into a new temporary file in dir, hashing it as it goes,
// and stops at max bytes. It returns the file's path, its SHA-256 and its
// size; the file is removed if anything goes wrong.
func Receive(dir string, body io.Reader, max int64) (path, sum string, n int64, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", 0, fmt.Errorf("create the update folder: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return "", "", 0, fmt.Errorf("create a temporary file: %w", err)
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(body, max+1))
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	switch {
	case copyErr != nil:
		err = copyErr
	case n > max:
		err = ErrTooLarge
	case n == 0:
		err = ErrEmpty
	case syncErr != nil:
		err = fmt.Errorf("write the file: %w", syncErr)
	case closeErr != nil:
		err = fmt.Errorf("write the file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return "", "", 0, err
	}
	return tmp.Name(), hex.EncodeToString(h.Sum(nil)), n, nil
}

// CleanTemp removes upload and download leftovers, for example from a request
// that was cut off.
func CleanTemp(dir string) {
	for _, pattern := range []string{tempUploadGlob, tempReleaseGlb} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, m := range matches {
			os.Remove(m)
		}
	}
}

// Probe is what a program says about itself with --version-check.
type Probe struct {
	Version string
	OS      string
	Arch    string
}

// VersionLine is what `app --version-check` prints. The entrypoint parses the
// same line with the shell.
func VersionLine(version string) string {
	return fmt.Sprintf("mediarium %s %s/%s", version, runtime.GOOS, runtime.GOARCH)
}

// ParseVersionLine reads the output of --version-check.
func ParseVersionLine(out string) (Probe, error) {
	fields := strings.Fields(out)
	if len(fields) != 3 || fields[0] != "mediarium" {
		return Probe{}, ErrNotMediarium
	}
	osArch := strings.Split(fields[2], "/")
	if len(osArch) != 2 || osArch[0] == "" || osArch[1] == "" {
		return Probe{}, ErrNotMediarium
	}
	for _, s := range []string{fields[1], osArch[0], osArch[1]} {
		if len(s) > 64 || strings.ContainsFunc(s, func(r rune) bool {
			return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '.' || r == '-' || r == '+' || r == '_')
		}) {
			return Probe{}, ErrNotMediarium
		}
	}
	return Probe{Version: fields[1], OS: osArch[0], Arch: osArch[1]}, nil
}

// limitedBuffer keeps at most max bytes.
type limitedBuffer struct {
	b   []byte
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - len(l.b); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		l.b = append(l.b, p[:room]...)
	}
	return len(p), nil
}

// Inspect makes sure path is a Mediarium program for this system by looking
// at its header and then running it once with --version-check, which only
// prints the version and platform and exits. It is run with an empty
// environment, from its own folder, and gives up after ten seconds.
func Inspect(ctx context.Context, path string) (Probe, error) {
	if runtime.GOOS == "linux" {
		if err := checkELF(path); err != nil {
			return Probe{}, err
		}
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return Probe{}, fmt.Errorf("make the file runnable: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var out []byte
	var runErr error
	for attempt := 0; attempt < 4; attempt++ {
		buf := &limitedBuffer{max: 512}
		cmd := exec.CommandContext(ctx, path, "--version-check")
		cmd.Env = []string{}
		cmd.Dir = filepath.Dir(path)
		cmd.Stdout = buf
		runErr = cmd.Run()
		out = buf.b
		// A file that was written a moment ago can still be "busy" while
		// another process starts; that passes.
		if errors.Is(runErr, syscall.ETXTBSY) {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		break
	}
	if runErr != nil {
		var exit *exec.ExitError
		if errors.As(runErr, &exit) || errors.Is(runErr, context.DeadlineExceeded) {
			return Probe{}, fmt.Errorf("%w: it did not answer --version-check", ErrNotMediarium)
		}
		return Probe{}, fmt.Errorf("start the file: %w", runErr)
	}
	p, err := ParseVersionLine(string(out))
	if err != nil {
		return Probe{}, err
	}
	if p.OS != runtime.GOOS || p.Arch != runtime.GOARCH {
		return p, fmt.Errorf("%w: it is for %s/%s and this is %s/%s", ErrWrongPlatform, p.OS, p.Arch, runtime.GOOS, runtime.GOARCH)
	}
	return p, nil
}

// checkELF confirms path is a Linux executable for this processor.
func checkELF(path string) error {
	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("%w: it is not a Linux program", ErrNotMediarium)
	}
	defer f.Close()
	var want elf.Machine
	switch runtime.GOARCH {
	case "amd64":
		want = elf.EM_X86_64
	case "arm64":
		want = elf.EM_AARCH64
	default:
		return nil
	}
	if f.Machine != want {
		return fmt.Errorf("%w: it is built for %s", ErrWrongPlatform, f.Machine)
	}
	if f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN {
		return fmt.Errorf("%w: it is not a program", ErrNotMediarium)
	}
	return nil
}

// Decide applies the version rules for installing candidate:
//
//   - it must be a version number;
//   - it may not be older than the version inside the container image, because
//     the entrypoint would ignore it and start the image's own program;
//   - unless force is set it must be newer than the running version (force
//     allows the same or an older one, for going back after a bad update).
//
// running and image may be empty or "dev" when unknown: then only force allows
// the install.
func Decide(running, image, candidate string, force bool) error {
	cv, err := semver.Parse(candidate)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrBadVersion, candidate)
	}
	if iv, err := semver.Parse(image); err == nil && cv.Compare(iv) < 0 {
		return fmt.Errorf("%w (%s, image %s)", ErrOlderThanImage, cv, iv)
	}
	rv, err := semver.Parse(running)
	if err != nil {
		if force {
			return nil
		}
		return fmt.Errorf("%w: the running version is %q", ErrBadVersion, running)
	}
	if !force && cv.Compare(rv) <= 0 {
		return fmt.Errorf("%w (%s, running %s)", ErrNotNewer, cv, rv)
	}
	return nil
}

// Install puts the checked program at tmp in place as the installed update:
// the current one (if any) becomes app.previous, the new one becomes app, and
// the version and checksum files are written. Every step is a rename inside
// one folder, so a crash leaves either the old program or the new one, never
// half of a file. The start counter and any earlier "failed" mark are cleared
// so the new program gets its full number of tries.
func Install(dir, tmp, version, sum string) error {
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("make the file runnable: %w", err)
	}
	app := filepath.Join(dir, FileApp)
	kept := false // the program that was installed now sits in app.previous
	if _, err := os.Stat(app); err == nil {
		if err := os.Rename(app, filepath.Join(dir, FilePrevious)); err != nil {
			return fmt.Errorf("keep the previous program: %w", err)
		}
		kept = true
		if _, err := os.Stat(filepath.Join(dir, FileVersion)); err == nil {
			_ = os.Rename(filepath.Join(dir, FileVersion), filepath.Join(dir, FilePrevVer))
		} else {
			_ = os.Remove(filepath.Join(dir, FilePrevVer))
		}
	}
	// undo puts the folder back as it was. Without it, a failure after the new
	// program is in place (a full disk while writing the version, say) would
	// leave it next to the old program's checksum, which the entrypoint
	// refuses, so the container would fall back to the image's own program
	// although a working installed update was there.
	undo := func() {
		if kept {
			_ = os.Rename(filepath.Join(dir, FilePrevious), app)
			if _, err := os.Stat(filepath.Join(dir, FilePrevVer)); err == nil {
				_ = os.Rename(filepath.Join(dir, FilePrevVer), filepath.Join(dir, FileVersion))
			} else {
				_ = os.Remove(filepath.Join(dir, FileVersion))
			}
			return
		}
		_ = os.Remove(app)
		_ = os.Remove(filepath.Join(dir, FileVersion))
	}
	if err := os.Rename(tmp, app); err != nil {
		undo()
		return fmt.Errorf("put the program in place: %w", err)
	}
	if err := writeAtomic(filepath.Join(dir, FileVersion), version+"\n"); err != nil {
		undo()
		return err
	}
	if err := writeAtomic(filepath.Join(dir, FileSum), sum+"  app\n"); err != nil {
		undo()
		return err
	}
	for _, name := range []string{FileBootCount, FileFailed, FileFailedVer} {
		_ = os.Remove(filepath.Join(dir, name))
	}
	syncDir(dir)
	return nil
}

// writeAtomic writes a small file by writing a temporary one and renaming it.
func writeAtomic(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}

// Remove deletes everything installed, so the next start uses the image's own
// program. It does not touch the running process.
func Remove(dir string) error {
	var firstErr error
	for _, name := range []string{FileApp, FileSum, FileVersion, FilePrevious, FilePrevVer, FileFailed, FileFailedVer, FileBootCount} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return firstErr
}

// BootCount is how many times the installed program has been started without
// yet proving itself.
func BootCount(dir string) int {
	var n int
	if _, err := fmt.Sscanf(readLine(filepath.Join(dir, FileBootCount)), "%d", &n); err != nil {
		return 0
	}
	return n
}

// MarkHealthy clears the start counter. The app calls it once it has been
// running and answering for HealthyAfter.
func MarkHealthy(dir string) {
	_ = os.Remove(filepath.Join(dir, FileBootCount))
}

// probeTimeout is how long a program gets to answer --version-check.
var probeTimeout = 10 * time.Second
