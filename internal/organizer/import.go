package organizer

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ConflictPolicy governs what happens when the destination path already
// has a file (PRD §4.8 — "never delete/overwrite ... without an explicit
// user-configured conflict policy").
type ConflictPolicy int

const (
	ConflictSkip ConflictPolicy = iota
	ConflictOverwrite
	// ConflictOverwriteIfBetter only replaces the existing file when the
	// caller's isBetter callback (passed to Import) says the incoming file
	// is actually higher quality — this package has no concept of media
	// quality itself (that's internal/quality's job), so the decision is
	// entirely the caller's; Import just enforces "never overwrite without
	// an explicit yes."
	ConflictOverwriteIfBetter
	// ConflictAsk defers to a manual-import review queue (PRD §4.8) —
	// that queue is UI/API-layer state this package doesn't own, so this
	// package treats ConflictAsk the same as ConflictSkip: it never
	// silently overwrites, and the caller is responsible for surfacing
	// the "needs review" case to the user.
	ConflictAsk
)

var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true, ".wmv": true, ".m4v": true, ".ts": true, ".m2ts": true,
}

// IsVideoFile reports whether path has a common video container extension.
func IsVideoFile(path string) bool {
	return videoExtensions[filepathExtLower(path)]
}

func filepathExtLower(path string) string {
	ext := filepath.Ext(path)
	for i := 0; i < len(ext); i++ {
		if ext[i] >= 'A' && ext[i] <= 'Z' {
			ext = string(rune(ext[i]+32)) + ext[i+1:]
		}
	}
	return ext
}

// FindLargestVideoFile walks dir (after unpacking/PAR2 repair have already
// run) and returns the largest video file found — the conventional
// heuristic for "which file in this download is actually the movie"
// (samples, extras, and NFO/subtitle files are typically much smaller).
func FindLargestVideoFile(dir string) (string, error) {
	var (
		best     string
		bestSize int64
	)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !IsVideoFile(path) {
			return nil
		}
		if info.Size() > bestSize {
			best, bestSize = path, info.Size()
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("scan %s for video files: %w", dir, err)
	}
	if best == "" {
		return "", fmt.Errorf("no video file found under %s", dir)
	}
	return best, nil
}

// FindVideoFiles returns every video file under dir (season packs hold one
// per episode), skipping "sample" clips — releases routinely ship a short
// sample.mkv alongside the real files, and importing it as an episode would
// be wrong. Results are sorted by path so imports are deterministic.
func FindVideoFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !IsVideoFile(path) {
			return nil
		}
		if isSample(path) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s for video files: %w", dir, err)
	}
	sort.Strings(files)
	return files, nil
}

func isSample(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	base := lower[strings.LastIndex(lower, "/")+1:]
	return strings.Contains(base, "sample") || strings.Contains(lower, "/sample/")
}

// ImportResult reports what actually happened, since a skip (existing
// file, ConflictSkip/ConflictAsk) is a valid, non-error outcome.
type ImportResult struct {
	DestPath     string
	UsedHardlink bool
	Skipped      bool
}

// Import places src at destPath, hardlinking when possible (PRD §4.8 —
// "hardlink-first move strategy") and falling back to a copy when the two
// paths aren't on the same filesystem (hardlinks can't cross devices).
//
// isBetter is only consulted when policy is ConflictOverwriteIfBetter and
// a file already exists at destPath — nil is fine for any other policy.
func Import(src, destPath string, policy ConflictPolicy, isBetter func() bool) (ImportResult, error) {
	replacing := false
	if _, err := os.Stat(destPath); err == nil {
		switch policy {
		case ConflictOverwrite:
			replacing = true
		case ConflictOverwriteIfBetter:
			if isBetter == nil || !isBetter() {
				return ImportResult{DestPath: destPath, Skipped: true}, nil
			}
			replacing = true
		default: // ConflictSkip, ConflictAsk
			return ImportResult{DestPath: destPath, Skipped: true}, nil
		}
	} else if !os.IsNotExist(err) {
		return ImportResult{}, fmt.Errorf("stat destination %s: %w", destPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return ImportResult{}, fmt.Errorf("create destination dir: %w", err)
	}

	// An existing file is never removed first: the new file is staged beside it
	// and renamed over it, so a failure part-way leaves the old file intact.
	target := destPath
	if replacing {
		target = destPath + ".mediarium-tmp"
		_ = os.Remove(target) // leftover from an earlier crashed attempt; ours by name
	}

	usedHardlink := false
	if err := os.Link(src, target); err == nil {
		usedHardlink = true
	} else if errors.Is(err, fs.ErrExist) {
		return ImportResult{}, fmt.Errorf("hardlink %s -> %s: %w", src, target, err)
	} else if err := copyFile(src, target); err != nil {
		// Any other link failure (another drive, on Windows too; a filesystem
		// that has no hardlinks, such as a network share) falls back to a copy.
		_ = os.Remove(target)
		return ImportResult{}, fmt.Errorf("copy %s -> %s: %w", src, target, err)
	}

	if replacing {
		if err := os.Rename(target, destPath); err != nil {
			_ = os.Remove(target)
			return ImportResult{}, fmt.Errorf("replace %s: %w", destPath, err)
		}
	}
	return ImportResult{DestPath: destPath, UsedHardlink: usedHardlink}, nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy contents: %w", err)
	}
	return out.Sync()
}

// IsSample reports whether path looks like a sample clip rather than the
// real video.
func IsSample(path string) bool { return isSample(path) }
