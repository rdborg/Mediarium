package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Small marker files in the update folder. They carry a message across a
// restart, from the process that is stopping to the one that starts.
const (
	FileSafeOnce = "safe-once"     // start once with automation paused
	FileStuck    = "stuck-restart" // the app restarted itself because it stopped answering
)

// WriteSafeOnce asks for the next start to run in safe mode, once.
func WriteSafeOnce(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create the update folder: %w", err)
	}
	return writeAtomic(filepath.Join(dir, FileSafeOnce), time.Now().UTC().Format(time.RFC3339)+"\n")
}

// TakeSafeOnce reports whether a safe-mode start was asked for, and removes
// the request so it applies to this start only.
func TakeSafeOnce(dir string) bool {
	path := filepath.Join(dir, FileSafeOnce)
	if _, err := os.Stat(path); err != nil {
		return false
	}
	_ = os.Remove(path)
	return true
}

// WriteStuck records that the app is about to restart itself, and why.
func WriteStuck(dir, reason string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create the update folder: %w", err)
	}
	reason = strings.NewReplacer("\n", " ", "\r", " ").Replace(reason)
	return writeAtomic(filepath.Join(dir, FileStuck), time.Now().UTC().Format(time.RFC3339)+" "+reason+"\n")
}

// TakeStuck returns the reason and time of a restart the app made because it
// stopped answering, and removes the mark.
func TakeStuck(dir string) (reason string, at time.Time, ok bool) {
	path := filepath.Join(dir, FileStuck)
	line := readLine(path)
	if line == "" {
		return "", time.Time{}, false
	}
	_ = os.Remove(path)
	first, rest, _ := strings.Cut(line, " ")
	at, _ = time.Parse(time.RFC3339, first)
	return rest, at, true
}
