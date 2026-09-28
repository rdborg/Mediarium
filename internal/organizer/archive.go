package organizer

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nwaples/rardecode/v2"

	"github.com/ryanborg/mediarium/internal/fsinfo"
)

// Sentinel errors returned (wrapped) by Extract so callers can tell a bad
// release apart from a problem with the local setup.
var (
	// ErrPasswordProtected: the archive (or a file inside it) is encrypted.
	// Mediarium never has a password, so the release is unusable.
	ErrPasswordProtected = errors.New("archive is password protected")
	// ErrUnsafeArchive: an entry would land outside the destination folder
	// (path traversal, absolute path, or a link pointing outside).
	ErrUnsafeArchive = errors.New("archive contains an unsafe path or link")
	// ErrArchiveTooLarge: the archive would unpack to more than the safety
	// limit (zip-bomb style protection).
	ErrArchiveTooLarge = errors.New("archive unpacks to more data than the safety limit allows")
	// ErrInsufficientSpace: the destination disk does not have room. This is
	// a local problem, not the release's fault.
	ErrInsufficientSpace = errors.New("not enough free disk space to unpack the archive")
	// ErrSevenZipMissing: a .7z archive needs the external 7z tool, which is
	// not installed. Local problem, not the release's fault.
	ErrSevenZipMissing = errors.New("the 7z tool is not installed, so .7z archives cannot be unpacked")
	// ErrUnsupportedArchive: not a format Extract knows how to unpack.
	ErrUnsupportedArchive = errors.New("unsupported archive type")
)

const (
	// DefaultMaxExtractBytes is the most one archive may unpack to. 200 GB is
	// far above any real movie/season pack (a UHD remux is ~80 GB) but stops
	// a maliciously tiny archive that expands to terabytes. Declared sizes
	// in archive headers can lie, so the limit is also enforced on the
	// bytes actually written.
	DefaultMaxExtractBytes int64 = 200 << 30

	// maxArchiveEntries bounds the number of entries in one archive so a
	// million-tiny-files archive cannot exhaust inodes.
	maxArchiveEntries = 100_000

	// spaceReserveBytes is kept free on the destination disk on top of the
	// size of each file about to be written, so unpacking never takes the
	// disk to exactly zero.
	spaceReserveBytes uint64 = 100 << 20
)

// Extractor unpacks archives found in a completed download (PRD §4.4 —
// post-processor unpack step).
//
// RAR (single, .partNN.rar and .rar + .r00/.r01 volume sets) and ZIP are
// unpacked natively in pure Go (rardecode, archive/zip): the Alpine 7z build
// in the Docker image has no RAR codec, and RAR is what most Usenet posts
// use. Only .7z still shells out to the external 7z CLI (the same "bundle a
// system binary" pattern approved for PAR2 repair).
type Extractor struct {
	BinaryPath string // 7z binary, defaults to "7z" (resolved via PATH) if empty

	// MaxTotalBytes caps the unpacked size of one archive. Zero means
	// DefaultMaxExtractBytes.
	MaxTotalBytes int64

	// FreeSpace reports the free bytes on the disk holding dir. ok=false
	// means unknown (the check is then skipped). Nil uses the real disk.
	// Exposed so tests can simulate a full disk.
	FreeSpace func(dir string) (free uint64, ok bool)
}

func NewExtractor() *Extractor { return &Extractor{BinaryPath: "7z"} }

func (e *Extractor) binary() string {
	if e.BinaryPath != "" {
		return e.BinaryPath
	}
	return "7z"
}

// Available reports whether the external 7z binary can be run. It is only
// needed for .7z archives: RAR and ZIP are unpacked natively.
func (e *Extractor) Available() bool {
	_, err := exec.LookPath(e.binary())
	return err == nil
}

func (e *Extractor) maxTotal() int64 {
	if e.MaxTotalBytes > 0 {
		return e.MaxTotalBytes
	}
	return DefaultMaxExtractBytes
}

func (e *Extractor) freeSpace(dir string) (uint64, bool) {
	if e.FreeSpace != nil {
		return e.FreeSpace(dir)
	}
	u, err := fsinfo.DiskUsage(dir)
	if err != nil || u.TotalBytes == 0 {
		return 0, false
	}
	return u.FreeBytes, true
}

var (
	rarVolumeRe = regexp.MustCompile(`(?i)\.part0*(\d+)\.rar$`)
	rarOldStyle = regexp.MustCompile(`(?i)\.r\d{2,3}$`)
)

// FindPrimaryArchives scans dir for the "first volume" of each archive set
// — e.g. "movie.rar" or "movie.part1.rar", not "movie.part2.rar" or
// "movie.r01" — since extracting just the first volume pulls in the rest
// of a same-set multi-volume archive automatically.
func FindPrimaryArchives(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var primaries []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)

		switch {
		case rarOldStyle.MatchString(lower):
			continue // .r00, .r01, ... are continuation volumes
		case rarVolumeRe.MatchString(lower):
			if m := rarVolumeRe.FindStringSubmatch(lower); m[1] != "1" && m[1] != "01" {
				continue // only part1/part01 is the entry point
			}
			primaries = append(primaries, filepath.Join(dir, name))
		case strings.HasSuffix(lower, ".rar"), strings.HasSuffix(lower, ".zip"), strings.HasSuffix(lower, ".7z"):
			primaries = append(primaries, filepath.Join(dir, name))
		}
	}
	sort.Strings(primaries)
	return primaries, nil
}

// Extract unpacks archivePath into destDir. The format is chosen from the
// file name: .rar (any volume naming) and .zip natively, .7z via the
// external 7z tool.
func (e *Extractor) Extract(archivePath, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create extract dest dir: %w", err)
	}
	switch lower := strings.ToLower(archivePath); {
	case strings.HasSuffix(lower, ".rar"), rarOldStyle.MatchString(lower):
		return e.extractRAR(archivePath, destDir)
	case strings.HasSuffix(lower, ".zip"):
		return e.extractZip(archivePath, destDir)
	case strings.HasSuffix(lower, ".7z"):
		return e.extract7z(archivePath, destDir)
	default:
		return fmt.Errorf("extract %s: %w", filepath.Base(archivePath), ErrUnsupportedArchive)
	}
}

// extractRAR unpacks a RAR set natively. OpenReader on the first volume
// follows the remaining volumes (.part2.rar / .r00 ...) automatically.
func (e *Extractor) extractRAR(archivePath, destDir string) error {
	name := filepath.Base(archivePath)
	rc, err := rardecode.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("extract %s: %w", name, classifyRARError(err))
	}
	defer rc.Close()

	x := e.newExtraction(destDir)
	for {
		h, err := rc.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("extract %s: %w", name, classifyRARError(err))
		}
		if h.Encrypted || h.HeaderEncrypted {
			return fmt.Errorf("extract %s: %w", name, ErrPasswordProtected)
		}
		if err := x.countEntry(); err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}

		switch {
		case h.LinkType != rardecode.LinkTypeNone:
			// Links are validated but never created: media releases do not
			// need them, and not creating any removes the classic "write
			// through an earlier symlink" escape entirely.
			if err := checkLink(destDir, h.Name, h.LinkTarget); err != nil {
				return fmt.Errorf("extract %s: %w", name, err)
			}
		case h.Mode()&fs.ModeSymlink != 0:
			// RAR4 stores a symlink as a file whose content is the target.
			target, err := io.ReadAll(io.LimitReader(rc, 4096))
			if err != nil {
				return fmt.Errorf("extract %s: %w", name, classifyRARError(err))
			}
			if err := checkLink(destDir, h.Name, string(target)); err != nil {
				return fmt.Errorf("extract %s: %w", name, err)
			}
		case h.IsDir:
			if _, err := x.makeDir(h.Name); err != nil {
				return fmt.Errorf("extract %s: %w", name, err)
			}
		default:
			declared := h.UnPackedSize
			if h.UnKnownSize {
				declared = -1
			}
			if err := x.writeFile(h.Name, declared, rc); err != nil {
				return fmt.Errorf("extract %s: %w", name, classifyRARError(err))
			}
		}
	}
}

func classifyRARError(err error) error {
	switch {
	case errors.Is(err, rardecode.ErrArchiveEncrypted),
		errors.Is(err, rardecode.ErrArchivedFileEncrypted),
		errors.Is(err, rardecode.ErrBadPassword):
		return fmt.Errorf("%w (%v)", ErrPasswordProtected, err)
	}
	return err
}

// extractZip unpacks a ZIP with the standard library.
func (e *Extractor) extractZip(archivePath, destDir string) error {
	name := filepath.Base(archivePath)
	zr, err := zip.OpenReader(archivePath)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return fmt.Errorf("extract %s: %w", name, err)
	}
	defer zr.Close()

	x := e.newExtraction(destDir)
	for _, f := range zr.File {
		if err := x.countEntry(); err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}
		if f.Flags&0x1 != 0 { // traditional or AES encryption
			return fmt.Errorf("extract %s: %w", name, ErrPasswordProtected)
		}
		mode := f.Mode()
		switch {
		case mode&os.ModeSymlink != 0:
			// Same rule as RAR: validate the target, never create the link.
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("extract %s: %w", name, err)
			}
			target, err := io.ReadAll(io.LimitReader(rc, 4096))
			rc.Close()
			if err != nil {
				return fmt.Errorf("extract %s: %w", name, err)
			}
			if err := checkLink(destDir, f.Name, string(target)); err != nil {
				return fmt.Errorf("extract %s: %w", name, err)
			}
		case f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") || strings.HasSuffix(f.Name, `\`):
			if _, err := x.makeDir(f.Name); err != nil {
				return fmt.Errorf("extract %s: %w", name, err)
			}
		default:
			declared := int64(-1)
			if f.UncompressedSize64 <= uint64(1<<62) {
				declared = int64(f.UncompressedSize64)
			}
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("extract %s: %w", name, err)
			}
			err = x.writeFile(f.Name, declared, rc)
			rc.Close()
			if err != nil {
				return fmt.Errorf("extract %s: %w", name, err)
			}
		}
	}
	return nil
}

// extract7z shells out to 7z; only .7z archives need it.
func (e *Extractor) extract7z(archivePath, destDir string) error {
	if !e.Available() {
		return fmt.Errorf("extract %s: %w", filepath.Base(archivePath), ErrSevenZipMissing)
	}
	// No -p: with no password given 7z fails instead of prompting (stdin is
	// the null device), which is what we want for encrypted archives.
	cmd := exec.Command(e.binary(), "x", "-y", "-o"+destDir, archivePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(strings.ToLower(string(out)), "wrong password") {
			return fmt.Errorf("extract %s: %w", filepath.Base(archivePath), ErrPasswordProtected)
		}
		return fmt.Errorf("extract %s: %w: %s", archivePath, err, truncateOutput(out))
	}
	return nil
}

// extraction carries the shared safety state for one archive: destination,
// remaining size budget, and entry count.
type extraction struct {
	e       *Extractor
	dest    string
	budget  int64
	entries int
}

func (e *Extractor) newExtraction(dest string) *extraction {
	return &extraction{e: e, dest: dest, budget: e.maxTotal()}
}

func (x *extraction) countEntry() error {
	x.entries++
	if x.entries > maxArchiveEntries {
		return fmt.Errorf("more than %d entries: %w", maxArchiveEntries, ErrArchiveTooLarge)
	}
	return nil
}

// makeDir creates the directory for an archive entry, refusing anything
// that would escape dest. It returns the resolved path.
func (x *extraction) makeDir(name string) (string, error) {
	target, err := safeJoin(x.dest, name)
	if err != nil {
		return "", err
	}
	if target == filepath.Clean(x.dest) {
		return target, nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}
	return target, nil
}

// writeFile streams r into dest/name. declared is the size the archive
// header claims (-1 if unknown); it is checked up front against the size
// limit and free disk space, and the actual bytes written are limited too,
// because headers can lie.
func (x *extraction) writeFile(name string, declared int64, r io.Reader) error {
	target, err := safeJoin(x.dest, name)
	if err != nil {
		return err
	}
	if target == filepath.Clean(x.dest) {
		return fmt.Errorf("entry %q has no file name: %w", name, ErrUnsafeArchive)
	}
	if declared > x.budget {
		return fmt.Errorf("%q: %w", name, ErrArchiveTooLarge)
	}
	if declared > 0 {
		if free, ok := x.e.freeSpace(x.dest); ok && free < uint64(declared)+spaceReserveBytes {
			return fmt.Errorf("%q needs %d bytes, %d free: %w", name, declared, free, ErrInsufficientSpace)
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	// Never write through a pre-existing link at the destination.
	if fi, err := os.Lstat(target); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%q would overwrite a link: %w", name, ErrUnsafeArchive)
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	lw := &limitedWriter{w: f, left: x.budget}
	_, copyErr := io.Copy(lw, r)
	closeErr := f.Close()
	x.budget = lw.left
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		os.Remove(target) // do not leave a truncated file for the video finder
		if errors.Is(copyErr, errLimitExceeded) {
			return fmt.Errorf("%q: %w", name, ErrArchiveTooLarge)
		}
		return fmt.Errorf("write %s: %w", name, copyErr)
	}
	return nil
}

var errLimitExceeded = errors.New("size limit exceeded")

// limitedWriter fails once more than left bytes have been written.
type limitedWriter struct {
	w    io.Writer
	left int64
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		n, _ := l.w.Write(p[:l.left])
		l.left -= int64(n)
		return n, errLimitExceeded
	}
	n, err := l.w.Write(p)
	l.left -= int64(n)
	return n, err
}

// normalizeEntryName converts an archive entry name to a clean,
// slash-separated relative path, or reports why it is unsafe. Backslashes
// are treated as separators (RAR4 and Windows-made zips use them).
func normalizeEntryName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("entry name %q: %w", name, ErrUnsafeArchive)
	}
	n := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(n, "/") || (len(n) >= 2 && n[1] == ':') {
		return "", fmt.Errorf("absolute path %q: %w", name, ErrUnsafeArchive)
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", fmt.Errorf("path %q escapes the destination: %w", name, ErrUnsafeArchive)
		}
	}
	return path.Clean(n), nil
}

// safeJoin resolves an archive entry name under dest, refusing anything
// that would land outside it.
func safeJoin(dest, name string) (string, error) {
	clean, err := normalizeEntryName(name)
	if err != nil {
		return "", err
	}
	root := filepath.Clean(dest)
	if clean == "." {
		return root, nil
	}
	target := filepath.Join(root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q escapes the destination: %w", name, ErrUnsafeArchive)
	}
	return target, nil
}

// checkLink reports ErrUnsafeArchive if a link entry's target is absolute or
// resolves (relative to the link's own folder) outside the destination.
func checkLink(dest, linkName, target string) error {
	if target == "" {
		return nil
	}
	t := strings.ReplaceAll(target, `\`, "/")
	if strings.HasPrefix(t, "/") || (len(t) >= 2 && t[1] == ':') {
		return fmt.Errorf("link %q points to absolute path %q: %w", linkName, target, ErrUnsafeArchive)
	}
	base, err := normalizeEntryName(linkName)
	if err != nil {
		return err
	}
	resolved := path.Join(path.Dir(base), t)
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Errorf("link %q points outside the destination (%q): %w", linkName, target, ErrUnsafeArchive)
	}
	return nil
}

func truncateOutput(b []byte) string {
	s := string(b)
	const max = 500
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
