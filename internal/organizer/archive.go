package organizer

import (
	"archive/zip"
	"bytes"
	"context"
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
	"strconv"
	"strings"
	"time"

	"github.com/nwaples/rardecode/v2"

	"github.com/rdborg/mediarium/internal/fsinfo"
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
	ErrSevenZipMissing = errors.New("the 7z tool is not installed, so .7z archives can't be unpacked")
	// ErrUnsupportedArchive: not a format Extract knows how to unpack.
	ErrUnsupportedArchive = errors.New("this type of archive is not supported")
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

	// maxRARDictionary is the biggest decoder dictionary a RAR file may ask
	// for. Real releases use 32 to 128 MB; the library's own default (4 GB)
	// would let a hostile file make a small server run out of memory.
	maxRARDictionary int64 = 512 << 20

	// defaultSevenZipTimeout is how long the external 7z tool may run for one
	// archive before it is stopped.
	defaultSevenZipTimeout = 6 * time.Hour

	// spaceReserveBytes is kept free on the destination disk on top of the
	// size of each file about to be written, so unpacking never takes the
	// disk to exactly zero.
	spaceReserveBytes uint64 = 100 << 20
)

// Extractor unpacks archives found in a completed download
// (post-processor unpack step).
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

	// SevenZipTimeout stops the external 7z tool if it runs longer than this
	// for one archive. Zero means six hours.
	SevenZipTimeout time.Duration
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
	// rarSetRe splits "name.part07.rar" into the set name and the digits.
	rarSetRe = regexp.MustCompile(`(?i)^(.+)\.part(\d+)\.rar$`)
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
	archivePath, err := normalizeRARVolumeNames(archivePath)
	if err != nil {
		return fmt.Errorf("extract %s: %w", filepath.Base(archivePath), err)
	}
	name := filepath.Base(archivePath)
	rc, err := rardecode.OpenReader(archivePath, rardecode.MaxDictionarySize(maxRARDictionary))
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

// normalizeRARVolumeNames fixes .partNN.rar sets whose numbers are padded
// inconsistently, which some posters do: "part01" … "part09", then "part010",
// "part011". The RAR reader finds the next volume by counting up from the
// current name ("part09" → "part10"), so such a set stops at part09. Every
// volume of the set is renamed to one width, the width of its highest number
// ("part010" → "part10"). Consistent sets are left alone. Only files next to
// the first volume, in the download's own working folder, are renamed. It
// returns the (possibly renamed) path of the first volume.
func normalizeRARVolumeNames(first string) (string, error) {
	dir, base := filepath.Split(first)
	m := rarSetRe.FindStringSubmatch(base)
	if m == nil {
		return first, nil // not a .partNN.rar set
	}
	prefix := m[1]
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		return first, fmt.Errorf("read dir %s: %w", dir, err)
	}
	type volume struct {
		name   string
		num    int
		digits string
	}
	var vols []volume
	maxNum := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		vm := rarSetRe.FindStringSubmatch(e.Name())
		if vm == nil || !strings.EqualFold(vm[1], prefix) {
			continue
		}
		n, err := strconv.Atoi(vm[2])
		if err != nil {
			continue
		}
		vols = append(vols, volume{name: e.Name(), num: n, digits: vm[2]})
		maxNum = max(maxNum, n)
	}
	width := len(strconv.Itoa(maxNum))
	// A set is fine when counting up from its first volume produces every
	// name: each number has the first volume's padding, or just grows past it
	// ("part9" → "part10", "part09" → "part10").
	minNum, firstWidth := -1, 0
	for _, v := range vols {
		if minNum < 0 || v.num < minNum {
			minNum, firstWidth = v.num, len(v.digits)
		}
	}
	consistent := true
	for _, v := range vols {
		if len(v.digits) != max(firstWidth, len(strconv.Itoa(v.num))) {
			consistent = false
			break
		}
	}
	if consistent {
		return first, nil
	}
	seen := map[int]bool{}
	for _, v := range vols {
		if seen[v.num] {
			return first, fmt.Errorf("two volumes numbered %d in the same set", v.num)
		}
		seen[v.num] = true
	}
	renamed := first
	for _, v := range vols {
		target := fmt.Sprintf("%s.part%0*d.rar", prefix, width, v.num)
		if target == v.name {
			continue
		}
		from, to := filepath.Join(dir, v.name), filepath.Join(dir, target)
		if _, err := os.Lstat(to); err == nil {
			return first, fmt.Errorf("rename %s: %s already exists", v.name, target)
		}
		if err := os.Rename(from, to); err != nil {
			return first, fmt.Errorf("rename %s to %s: %w", v.name, target, err)
		}
		if v.name == base {
			renamed = to
		}
	}
	return renamed, nil
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
//
// The external tool is the one place where Mediarium does not control what is
// written, and 7z can recreate the symbolic links stored in an archive. So the
// archive is first listed and refused if it names a link, a special file, an
// unsafe path or more data than allowed; then it is unpacked into a private
// folder inside destDir; and only regular files and folders found there are
// moved into place. Anything else (a link the listing did not show, a device
// file) makes the whole unpack fail and leaves nothing behind.
func (e *Extractor) extract7z(archivePath, destDir string) error {
	name := filepath.Base(archivePath)
	if !e.Available() {
		return fmt.Errorf("extract %s: %w", name, ErrSevenZipMissing)
	}
	timeout := e.SevenZipTimeout
	if timeout <= 0 {
		timeout = defaultSevenZipTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// No -p: with no password given 7z fails instead of prompting (stdin is
	// the null device), which is what we want for encrypted archives.
	listing, err := e.run7z(ctx, "l", "-slt", "--", archivePath)
	if err != nil {
		return e.sevenZipError(ctx, name, listing, err)
	}
	total, err := check7zListing(listing, e.maxTotal())
	if err != nil {
		return fmt.Errorf("extract %s: %w", name, err)
	}
	if total > 0 {
		if free, ok := e.freeSpace(destDir); ok && free < uint64(total)+spaceReserveBytes {
			return fmt.Errorf("extract %s: needs %d bytes, %d free: %w", name, total, free, ErrInsufficientSpace)
		}
	}

	stage, err := os.MkdirTemp(destDir, ".unpack-7z-")
	if err != nil {
		return fmt.Errorf("extract %s: create a working folder: %w", name, err)
	}
	defer os.RemoveAll(stage)
	if out, err := e.run7z(ctx, "x", "-y", "-o"+stage, "--", archivePath); err != nil {
		return e.sevenZipError(ctx, name, out, err)
	}
	if err := moveUnpacked(stage, destDir, e.maxTotal()); err != nil {
		return fmt.Errorf("extract %s: %w", name, err)
	}
	return nil
}

// run7z runs the 7z tool with the given arguments and returns what it printed
// (at most 64 KB of it).
func (e *Extractor) run7z(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.binary(), args...)
	cmd.WaitDelay = 5 * time.Second
	out := &cappedBuffer{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	return out.Bytes(), err
}

// sevenZipError turns a failed 7z run into the error the caller reports.
func (e *Extractor) sevenZipError(ctx context.Context, name string, out []byte, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("extract %s: 7z took too long and was stopped: %w", name, ctx.Err())
	}
	lower := strings.ToLower(string(out))
	if strings.Contains(lower, "wrong password") || strings.Contains(lower, "cannot open encrypted archive") {
		return fmt.Errorf("extract %s: %w", name, ErrPasswordProtected)
	}
	return fmt.Errorf("extract %s: %w: %s", name, err, truncateOutput(out))
}

// cappedBuffer keeps the first max bytes written and drops the rest.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		c.buf.Write(p[:room])
	}
	return len(p), nil
}

func (c *cappedBuffer) Bytes() []byte { return c.buf.Bytes() }

// sevenZipPerms matches the Unix permission string 7z prints for an entry
// ("-rw-r--r--", "lrwxrwxrwx", "drwxr-xr-x").
var sevenZipPerms = regexp.MustCompile(`^[-dlbcps][-rwxsStT]{9}$`)

// check7zListing reads the output of `7z l -slt` and refuses an archive that
// holds a link or other special file, a path that leaves the destination,
// encrypted files, too many entries or more data than budget. It returns the
// total unpacked size the listing declares.
func check7zListing(out []byte, budget int64) (int64, error) {
	text := "\n" + strings.ReplaceAll(string(out), "\r\n", "\n")
	_, body, found := strings.Cut(text, "\n----------\n")
	if !found {
		return 0, nil // an empty archive has no entries
	}
	var (
		total   int64
		entries int
	)
	for _, block := range strings.Split(body, "\n\n") {
		fields := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			if k, v, ok := strings.Cut(line, " = "); ok {
				fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
		p, ok := fields["Path"]
		if !ok {
			continue
		}
		entries++
		if entries > maxArchiveEntries {
			return 0, fmt.Errorf("more than %d entries: %w", maxArchiveEntries, ErrArchiveTooLarge)
		}
		if _, err := normalizeEntryName(p); err != nil {
			return 0, err
		}
		if fields["Encrypted"] == "+" {
			return 0, ErrPasswordProtected
		}
		if _, isLink := fields["Symbolic Link"]; isLink {
			return 0, fmt.Errorf("%q is a link: %w", p, ErrUnsafeArchive)
		}
		if attrs := strings.Fields(fields["Attributes"]); len(attrs) > 0 {
			if perms := attrs[len(attrs)-1]; sevenZipPerms.MatchString(perms) && perms[0] != '-' && perms[0] != 'd' {
				return 0, fmt.Errorf("%q is a link or special file: %w", p, ErrUnsafeArchive)
			}
		}
		if size, err := strconv.ParseInt(fields["Size"], 10, 64); err == nil && size > 0 {
			total += size
			if total > budget || total < 0 {
				return 0, fmt.Errorf("%q: %w", p, ErrArchiveTooLarge)
			}
		}
	}
	return total, nil
}

// moveUnpacked moves what 7z wrote into stage to destDir, after checking that
// it holds nothing but regular files and folders, and no more than budget
// bytes. Existing files are replaced, like the native extractors do.
func moveUnpacked(stage, destDir string, budget int64) error {
	var (
		total   int64
		entries int
		files   []string
	)
	err := filepath.WalkDir(stage, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == stage {
			return nil
		}
		entries++
		if entries > maxArchiveEntries {
			return fmt.Errorf("more than %d entries: %w", maxArchiveEntries, ErrArchiveTooLarge)
		}
		switch {
		case d.IsDir():
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
			if total > budget {
				return ErrArchiveTooLarge
			}
			files = append(files, p)
		default:
			rel, _ := filepath.Rel(stage, p)
			return fmt.Errorf("%q is a link or special file: %w", filepath.ToSlash(rel), ErrUnsafeArchive)
		}
		return nil
	})
	if err != nil {
		return err
	}
	x := &extraction{dest: destDir}
	for _, p := range files {
		rel, err := filepath.Rel(stage, p)
		if err != nil {
			return err
		}
		target, err := safeJoin(destDir, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
		if err := x.checkInside(filepath.Dir(target)); err != nil {
			return err
		}
		if fi, err := os.Lstat(target); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%q would overwrite a link: %w", filepath.ToSlash(rel), ErrUnsafeArchive)
		}
		if err := os.Rename(p, target); err != nil {
			return fmt.Errorf("move %s into place: %w", filepath.ToSlash(rel), err)
		}
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
	if err := x.checkInside(target); err != nil {
		return "", fmt.Errorf("%q: %w", name, err)
	}
	return target, nil
}

// checkInside makes sure dir, once every link on the way is followed, is still
// inside the destination. Names are checked as text by safeJoin, which cannot
// see a link that is already on disk (planted by an earlier archive of the
// same release, say): writing "link/file" through a link that points elsewhere
// would put the file outside the destination.
func (x *extraction) checkInside(dir string) error {
	root, err := filepath.EvalSymlinks(x.dest)
	if err != nil {
		return fmt.Errorf("resolve the destination: %w", err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dir, err)
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("goes through a link to somewhere outside the destination: %w", ErrUnsafeArchive)
	}
	return nil
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
	if err := x.checkInside(filepath.Dir(target)); err != nil {
		return fmt.Errorf("%q: %w", name, err)
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
