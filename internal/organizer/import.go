package organizer

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/fileperms"
)

// ConflictPolicy governs what happens when the destination path already
// has a file ("never delete/overwrite ... without an explicit
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
	// ConflictAsk defers to a manual-import review queue —
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

// FindOption changes what the Find functions count.
type FindOption int

// FollowLinks counts a symbolic link as the file it leads to. It is only for
// a download Mediarium laid out itself as links to an external download
// client's files: Import still refuses every link, and ImportClientFile
// decides about each one.
const FollowLinks FindOption = 1

func followsLinks(opts []FindOption) bool {
	for _, o := range opts {
		if o == FollowLinks {
			return true
		}
	}
	return false
}

// fileInfo returns what a walked entry is as a file, or nil when it isn't
// one: a folder, a device, or a link (unless links are followed, when a link
// to a plain file counts as that file; a link to a folder never does).
func fileInfo(path string, info os.FileInfo, links bool) os.FileInfo {
	if info.Mode().IsRegular() {
		return info
	}
	if !links || info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	target, err := os.Stat(path)
	if err != nil || !target.Mode().IsRegular() {
		return nil
	}
	return target
}

// FindLargestVideoFile walks dir (after unpacking/PAR2 repair have already
// run) and returns the largest video file found — the conventional
// heuristic for "which file in this download is actually the movie"
// (samples, extras, and NFO/subtitle files are typically much smaller).
func FindLargestVideoFile(dir string, opts ...FindOption) (string, error) {
	var (
		best     string
		bestSize int64
	)
	links := followsLinks(opts)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Only real files count: a link named like a video would import
		// whatever it points at (see Import).
		if info = fileInfo(path, info, links); info == nil || !IsVideoFile(path) {
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
		return "", fmt.Errorf("no video file found in %s (%s)", dir, describeFolder(dir))
	}
	return best, nil
}

// FindVideoFiles returns every video file under dir (season packs hold one
// per episode), skipping "sample" clips — releases routinely ship a short
// sample.mkv alongside the real files, and importing it as an episode would
// be wrong. Results are sorted by path so imports are deterministic.
func FindVideoFiles(dir string, opts ...FindOption) ([]string, error) {
	var files []string
	links := followsLinks(opts)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fileInfo(path, info, links) == nil || !IsVideoFile(path) {
			return nil
		}
		// Judged by the path below dir: a folder called "sample" that dir
		// itself sits in says nothing about the files in it.
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		if isSample(rel) {
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

// sampleWord matches "sample" or "samples" as a word of its own in a file
// name ("show.s01e01.sample.mkv"), not inside another word.
var sampleWord = regexp.MustCompile(`(?i)(?:^|[^a-z])samples?(?:[^a-z]|$)`)

func isSample(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, dir := range parts[:len(parts)-1] {
		if d := strings.ToLower(dir); d == "sample" || d == "samples" {
			return true
		}
	}
	return sampleWord.MatchString(parts[len(parts)-1])
}

// ErrNotRegularFile is returned by Import for a source that is a link, a
// device or anything else but a plain file.
var ErrNotRegularFile = errors.New("not a regular file")

// ImportResult reports what actually happened, since a skip (existing
// file, ConflictSkip/ConflictAsk) is a valid, non-error outcome.
type ImportResult struct {
	DestPath     string
	UsedHardlink bool
	Skipped      bool
}

// stagingSuffix marks the file a copy is written to before it takes its
// final name, so a media server never sees half a movie under the real one.
const stagingSuffix = ".mediarium-tmp"

// Import places src at destPath, hardlinking when possible
// ("hardlink-first move strategy") and falling back to a copy when the two
// paths aren't on the same filesystem (hardlinks can't cross devices).
//
// If destPath is already the same file as src (a hardlink made by an earlier
// attempt), nothing is done and the import counts as done.
//
// isBetter is only consulted when policy is ConflictOverwriteIfBetter and
// a file already exists at destPath — nil is fine for any other policy.
//
// A file mode set in Settings is applied to the file that ends up in the
// library and a folder mode to the folders made for it (see internal/fileperms).
func Import(src, destPath string, policy ConflictPolicy, isBetter func() bool) (ImportResult, error) {
	res, err := importPlace(src, destPath, policy, isBetter, false)
	if err == nil && !res.Skipped {
		_ = fileperms.ApplyFile(res.DestPath)
	}
	return res, err
}

// ImportCopy is Import without the hardlink: the file in the library is always
// a copy of its own, so changing or removing the original never touches it.
func ImportCopy(src, destPath string, policy ConflictPolicy, isBetter func() bool) (ImportResult, error) {
	res, err := importPlace(src, destPath, policy, isBetter, true)
	if err == nil && !res.Skipped {
		_ = fileperms.ApplyFile(res.DestPath)
	}
	return res, err
}

// importPlace does the work of Import; noLink skips the hardlink and copies.
func importPlace(src, destPath string, policy ConflictPolicy, isBetter func() bool, noLink bool) (ImportResult, error) {
	// A download is only ever real files. A link (which a hostile archive could
	// carry, pointing at the database or the encryption key) would be copied
	// with its target's contents, so it is refused outright.
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return ImportResult{}, fmt.Errorf("stat source %s: %w", src, err)
	}
	if !srcInfo.Mode().IsRegular() {
		return ImportResult{}, fmt.Errorf("%s: %w", filepath.Base(src), ErrNotRegularFile)
	}
	replacing := false
	// Lstat: a link at the destination, even one that leads nowhere, is
	// something already there, and goes through the conflict policy.
	if destInfo, err := os.Lstat(destPath); err == nil {
		if os.SameFile(srcInfo, destInfo) {
			return ImportResult{DestPath: destPath, UsedHardlink: true}, nil
		}
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

	if err := fileperms.MkdirAll(filepath.Dir(destPath)); err != nil {
		return ImportResult{}, fmt.Errorf("create destination dir: %w", err)
	}

	// An existing file is never removed first: the new file is staged beside it
	// and renamed over it, so a failure part-way leaves the old file intact.
	// Each import gets a staging name of its own, so two imports to the same
	// place can't remove or overwrite each other's file.
	_ = os.Remove(destPath + stagingSuffix) // left by an older version that crashed; ours by name
	removeStaleStaging(destPath, time.Now())
	staging := stagingPath(destPath)

	target := destPath
	if replacing {
		target = staging
	}
	if !noLink {
		if err := os.Link(src, target); err == nil {
			if replacing {
				return finishReplace(staging, destPath, true)
			}
			return ImportResult{DestPath: destPath, UsedHardlink: true}, nil
		} else if errors.Is(err, fs.ErrExist) {
			return ImportResult{}, fmt.Errorf("hardlink %s -> %s: %w", src, target, err)
		}
	}

	// Any other link failure (another drive, on Windows too; a filesystem
	// that has no hardlinks, such as a network share) falls back to a copy,
	// written under a staging name and renamed once it is complete.
	if err := copyFile(src, staging, srcInfo); err != nil {
		return ImportResult{}, fmt.Errorf("copy %s -> %s: %w", src, staging, err)
	}
	if replacing {
		return finishReplace(staging, destPath, false)
	}
	if err := publishNew(staging, destPath); err != nil {
		_ = os.Remove(staging)
		return ImportResult{}, err
	}
	return ImportResult{DestPath: destPath}, nil
}

// stagingPath is a new name beside destPath for a file on its way there. The
// random part makes it unique to one import; the name is cut so it still fits
// in 255 bytes.
func stagingPath(destPath string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	suffix := "." + hex.EncodeToString(b[:]) + stagingSuffix
	dir, base := filepath.Split(destPath)
	return filepath.Join(dir, truncateName(base, 255-len(suffix))+suffix)
}

// staleStagingAfter is how long a staging file may sit untouched before it is
// taken to be left over from a copy that never finished. A copy that is still
// running keeps changing the file, so only a dead one gets this old.
const staleStagingAfter = time.Hour

// stagingName matches the random part of a staging name (see stagingPath).
var stagingName = regexp.MustCompile(`^\.[0-9a-f]{8}` + regexp.QuoteMeta(stagingSuffix) + `$`)

// removeStaleStaging deletes the staging files that an earlier import of
// destPath left beside it when Mediarium was stopped or the power went in the
// middle of a copy. Each retry stages under a new random name, so without this
// the half-copied files of every attempt would stay in the library for ever
// (the size of a movie each). Only files of this destination that nothing has
// written to for staleStagingAfter are removed, so an import that is still
// running is never touched.
func removeStaleStaging(destPath string, now time.Time) {
	dir, base := filepath.Split(destPath)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := truncateName(base, 255-len("."+strings.Repeat("0", 8)+stagingSuffix))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !e.Type().IsRegular() || !stagingName.MatchString(name[len(prefix):]) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < staleStagingAfter {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// finishReplace renames the staged file over the existing one.
func finishReplace(staging, destPath string, usedHardlink bool) (ImportResult, error) {
	if err := os.Rename(staging, destPath); err != nil {
		_ = os.Remove(staging)
		return ImportResult{}, fmt.Errorf("replace %s: %w", destPath, err)
	}
	return ImportResult{DestPath: destPath, UsedHardlink: usedHardlink}, nil
}

// publishNew gives a finished staged copy its final name without replacing a
// file that appeared there in the meantime.
func publishNew(staging, destPath string) error {
	err := os.Link(staging, destPath)
	if err == nil {
		_ = os.Remove(staging)
		return nil
	}
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("link %s: %w", destPath, err)
	}
	// No hardlinks here (that is why the file was copied): rename, after a
	// last look for a file that was not there before.
	if _, statErr := os.Lstat(destPath); statErr == nil {
		return fmt.Errorf("create %s: %w", destPath, fs.ErrExist)
	}
	if err := os.Rename(staging, destPath); err != nil {
		return fmt.Errorf("move %s into place: %w", destPath, err)
	}
	return nil
}

// sourceOpenWaits is how long openSource waits between tries when a file that
// was just downloaded refuses to open. On Docker Desktop with a Windows drive
// that is usually a virus scanner or another program still holding the file,
// and it lets go within seconds.
var sourceOpenWaits = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}

// openSourceFile is os.Open; tests replace it.
var openSourceFile = os.Open

// openSource opens the file an import reads from. Only a permission error is
// tried again; any other error is returned at once.
func openSource(path string) (*os.File, error) {
	f, err := openSourceFile(path)
	for _, wait := range sourceOpenWaits {
		if err == nil || !errors.Is(err, fs.ErrPermission) {
			break
		}
		time.Sleep(wait)
		f, err = openSourceFile(path)
	}
	return f, err
}

// copyFile copies src to dest, which must not exist. On any failure the
// partly written dest is removed. The copy keeps the modification time.
func copyFile(src, dest string, srcInfo os.FileInfo) (err error) {
	in, err := openSource(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer func() {
		if err != nil {
			out.Close()
			_ = os.Remove(dest)
		}
	}()

	n, err := io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("copy contents: %w", err)
	}
	if n != srcInfo.Size() {
		// A short copy (a full disk that did not say so, a file that changed
		// while it was read) is never published as the finished file.
		err = fmt.Errorf("copy contents: %d of %d bytes were written", n, srcInfo.Size())
		return err
	}
	if err = out.Sync(); err != nil {
		return fmt.Errorf("flush destination: %w", err)
	}
	if err = out.Close(); err != nil {
		return fmt.Errorf("close destination: %w", err)
	}
	_ = os.Chtimes(dest, time.Now(), srcInfo.ModTime())
	return nil
}

// IsSample reports whether path looks like a sample clip rather than the
// real video. Pass the path below the folder that was scanned, so a folder
// the scan started in cannot make every file in it a sample.
func IsSample(path string) bool { return isSample(path) }
