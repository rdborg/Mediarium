package organizer

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/nwaples/rardecode/v2"

	"github.com/rdborg/mediarium/internal/fsinfo"
)

// Sentinel errors returned (wrapped) by Extract so callers can tell a bad
// release apart from a problem with the local setup.
var (
	// ErrPasswordProtected: the archive (or a file inside it) is encrypted
	// and there is no password for it, so the release is unusable.
	ErrPasswordProtected = errors.New("archive is password protected")
	// ErrWrongPassword: the archive is encrypted and the password that came
	// with the release does not open it. It wraps ErrPasswordProtected, so
	// callers that only check for that still treat it as a bad release.
	ErrWrongPassword = fmt.Errorf("%w, and the password that came with it is wrong", ErrPasswordProtected)
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

	// max7zListing is the most of `7z l -slt` that is kept. An entry takes
	// about 250 bytes plus its name, so maxArchiveEntries entries with long
	// names fit; a longer listing is refused, never cut short.
	max7zListing = 64 << 20

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

	// Password opens encrypted RAR, 7z and ZIP archives. Empty means none.
	Password string

	// SevenZipTimeout stops the external 7z tool if it runs longer than this
	// for one archive. Zero means six hours.
	SevenZipTimeout time.Duration

	// Context, when set, stops an unpack that is still going once it ends (a
	// download that is paused or stopped). Nil means it can't be stopped.
	Context context.Context

	// Progress, when set, is called every couple of seconds while an archive is
	// being unpacked: how much has been done, out of how much (total is 0 when
	// that is not known). It is called from another goroutine, and keeps being
	// called with the same figures while a write is stuck. The unit depends on
	// the format, but done/total is always the fraction that is finished.
	Progress func(done, total int64)
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
	// sevenZipSplitRe matches a 7z archive cut into numbered pieces
	// ("name.7z.001", "name.7z.002", ...), which is how Usenet posts them.
	sevenZipSplitRe = regexp.MustCompile(`(?i)\.7z\.0*(\d+)$`)
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
	return primariesIn(dir, entries), nil
}

// FindPrimaryArchivesDeep is FindPrimaryArchives for dir and every folder
// inside it: a torrent saves its files in a folder named after it, and a
// debrid service returns folders too. Each archive is returned with its full
// path, so it can be unpacked next to where it was found. Folders of the
// app's own (hidden ones starting with a dot) and Sample folders are skipped.
func FindPrimaryArchivesDeep(dir string) ([]string, error) {
	var primaries []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return fmt.Errorf("read dir %s: %w", dir, err)
			}
			return nil // an unreadable sub-folder is skipped, not fatal
		}
		if !d.IsDir() {
			return nil
		}
		if path != dir {
			name := strings.ToLower(d.Name())
			if strings.HasPrefix(name, ".") || name == "sample" || name == "samples" {
				return filepath.SkipDir
			}
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil
		}
		primaries = append(primaries, primariesIn(path, entries)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(primaries)
	return primaries, nil
}

// primariesIn picks the first volume of each archive set out of one folder's
// entries.
func primariesIn(dir string, entries []os.DirEntry) []string {
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
		case sevenZipSplitRe.MatchString(lower):
			if m := sevenZipSplitRe.FindStringSubmatch(lower); m[1] != "1" {
				continue // only .7z.001 is the entry point; 7z finds the rest
			}
			primaries = append(primaries, filepath.Join(dir, name))
		case strings.HasSuffix(lower, ".rar"), strings.HasSuffix(lower, ".zip"), strings.HasSuffix(lower, ".7z"):
			primaries = append(primaries, filepath.Join(dir, name))
		}
	}
	sort.Strings(primaries)
	return primaries
}

// Format is an archive format Extract knows.
type Format string

const (
	FormatRAR Format = "rar"
	FormatZip Format = "zip"
	Format7z  Format = "7z"
)

// Extract unpacks archivePath into destDir. The format is chosen from the
// file name: .rar (any volume naming) and .zip natively, .7z via the
// external 7z tool.
func (e *Extractor) Extract(archivePath, destDir string) error {
	switch lower := strings.ToLower(archivePath); {
	case strings.HasSuffix(lower, ".rar"), rarOldStyle.MatchString(lower):
		return e.ExtractAs(archivePath, destDir, FormatRAR)
	case strings.HasSuffix(lower, ".zip"):
		return e.ExtractAs(archivePath, destDir, FormatZip)
	case strings.HasSuffix(lower, ".7z"), sevenZipSplitRe.MatchString(lower):
		return e.ExtractAs(archivePath, destDir, Format7z)
	default:
		return fmt.Errorf("extract %s: %w", filepath.Base(archivePath), ErrUnsupportedArchive)
	}
}

// ExtractAs unpacks archivePath into destDir as the given format, whatever
// the file is called (a .cbr comic is a RAR, a .cb7 a 7z). A single file is
// expected: volumes of a set are only found by their usual names.
func (e *Extractor) ExtractAs(archivePath, destDir string, f Format) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create extract dest dir: %w", err)
	}
	switch f {
	case FormatRAR:
		return e.extractRAR(archivePath, destDir)
	case FormatZip:
		return e.extractZip(archivePath, destDir)
	case Format7z:
		return e.extract7z(archivePath, destDir)
	default:
		return fmt.Errorf("extract %s: %w", filepath.Base(archivePath), ErrUnsupportedArchive)
	}
}

// DetectFormat reads the first bytes of r and says which archive format it
// is, by its signature rather than its name: plenty of .cbr files are really
// ZIPs, and plenty of .cbz files RARs. ok is false for anything else.
func DetectFormat(r io.Reader) (f Format, ok bool) {
	var head [8]byte
	n, _ := io.ReadFull(r, head[:])
	b := head[:n]
	switch {
	case bytes.HasPrefix(b, []byte("PK\x03\x04")), bytes.HasPrefix(b, []byte("PK\x05\x06")), bytes.HasPrefix(b, []byte("PK\x07\x08")):
		return FormatZip, true
	case bytes.HasPrefix(b, []byte("Rar!\x1a\x07\x00")), bytes.HasPrefix(b, []byte("Rar!\x1a\x07\x01\x00")):
		return FormatRAR, true
	case bytes.HasPrefix(b, []byte("7z\xbc\xaf\x27\x1c")):
		return Format7z, true
	}
	return "", false
}

// extractRAR unpacks a RAR set natively. OpenReader on the first volume
// follows the remaining volumes (.part2.rar / .r00 ...) automatically.
func (e *Extractor) extractRAR(archivePath, destDir string) error {
	archivePath, err := normalizeRARVolumeNames(archivePath)
	if err != nil {
		return fmt.Errorf("extract %s: %w", filepath.Base(archivePath), err)
	}
	name := filepath.Base(archivePath)
	// The set's own size is the amount of work: how much of the volumes has been
	// read is how far the unpack has come, whatever the files inside compress to.
	meter := e.startMeter(totalSize(rarSetFiles(archivePath)), false, nil)
	defer meter.finish()
	opts := []rardecode.Option{
		rardecode.MaxDictionarySize(maxRARDictionary),
		rardecode.BufferSize(unpackBufferBytes),
		rardecode.FileSystem(countingFS{m: meter}),
	}
	if e.Password != "" {
		opts = append(opts, rardecode.Password(e.Password))
	}
	// encrypted is set once an entry turns out to be encrypted. The library
	// marks such entries even when the password given is right, so this alone
	// says nothing about the password; it only helps read a later failure.
	encrypted := false
	fail := func(err error) error {
		return fmt.Errorf("extract %s: %w", name, e.classifyRARError(archivePath, encrypted, err))
	}
	rc, err := rardecode.OpenReader(archivePath, opts...)
	if err != nil {
		return fail(err)
	}
	defer rc.Close()

	x := e.newExtraction(destDir)
	x.m = meter
	for {
		h, err := rc.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fail(err)
		}
		if h.Encrypted || h.HeaderEncrypted {
			if e.Password == "" {
				return fmt.Errorf("extract %s: %w", name, ErrPasswordProtected)
			}
			encrypted = true
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
				return fail(err)
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
				return fail(err)
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

// rarGarbled lists the errors the RAR reader gives when the bytes it decodes
// make no sense. In an encrypted archive opened with a password, that is what
// a wrong password looks like: RAR4 has no password check of its own, so the
// wrong key only shows as bad checksums or headers.
var rarGarbled = []error{
	rardecode.ErrBadFileChecksum, rardecode.ErrBadHeaderCRC,
	rardecode.ErrCorruptBlockHeader, rardecode.ErrCorruptFileHeader,
	rardecode.ErrCorruptDecodeHeader, rardecode.ErrCorruptEncryptData,
	rardecode.ErrHuffDecodeFailed, rardecode.ErrInvalidLengthTable,
	rardecode.ErrCorruptPPM, rardecode.ErrDecoderOutOfData,
	rardecode.ErrUnknownDecoder, rardecode.ErrInvalidFilter,
	rardecode.ErrTooManyFilters, rardecode.ErrUnknownFilter,
	rardecode.ErrInvalidVMInstruction, rardecode.ErrShortFile,
	rardecode.ErrBadBlockHeader, rardecode.ErrMultiVolume,
	// A garbled RAR4 header can claim sizes that run past the end of the file.
	io.ErrUnexpectedEOF,
}

// classifyRARError turns an error from the RAR reader into one of ours.
// encrypted says whether an encrypted entry was already seen.
func (e *Extractor) classifyRARError(archivePath string, encrypted bool, err error) error {
	switch {
	case errors.Is(err, rardecode.ErrBadPassword):
		// RAR5 checks the password itself and says so when it is wrong.
		return fmt.Errorf("%w (%v)", ErrWrongPassword, err)
	case errors.Is(err, rardecode.ErrArchiveEncrypted),
		errors.Is(err, rardecode.ErrArchivedFileEncrypted):
		return fmt.Errorf("%w (%v)", ErrPasswordProtected, err)
	}
	if e.Password == "" {
		return err
	}
	for _, g := range rarGarbled {
		if !errors.Is(err, g) {
			continue
		}
		if encrypted || rarHeadersEncrypted(archivePath) {
			return fmt.Errorf("%w (%v)", ErrWrongPassword, err)
		}
		break
	}
	return err
}

// rarHeadersEncrypted reports whether the archive's file list is encrypted
// (made with rar -hp), by opening it again without a password: the reader
// then refuses at once. With a wrong password such an archive fails before
// any entry is read, so this is the only way to tell that apart from damage.
func rarHeadersEncrypted(archivePath string) bool {
	rc, err := rardecode.OpenReader(archivePath, rardecode.MaxDictionarySize(maxRARDictionary))
	if err == nil {
		_, err = rc.Next()
		rc.Close()
	}
	return errors.Is(err, rardecode.ErrArchiveEncrypted)
}

// extractZip unpacks a ZIP with the standard library.
func (e *Extractor) extractZip(archivePath, destDir string) error {
	name := filepath.Base(archivePath)
	zr, err := zip.OpenReader(archivePath)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return fmt.Errorf("extract %s: %w", name, err)
	}
	defer zr.Close()

	// An encrypted ZIP (the old ZipCrypto or AES) is opened by the external
	// 7z tool, which knows both, when there is a password. The standard
	// library can't read either. The same checks run on its listing.
	for _, f := range zr.File {
		if f.Flags&0x1 != 0 {
			if e.Password == "" {
				return fmt.Errorf("extract %s: %w", name, ErrPasswordProtected)
			}
			zr.Close()
			return e.extract7z(archivePath, destDir)
		}
	}

	var total int64
	for _, f := range zr.File {
		if f.UncompressedSize64 <= uint64(1<<50) {
			total += int64(f.UncompressedSize64)
		}
	}
	x := e.newExtraction(destDir)
	x.m = e.startMeter(total, true, nil)
	defer x.m.finish()
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
	ctx, cancel := context.WithTimeout(e.ctxOrBackground(), timeout)
	defer cancel()

	// Without a password 7z fails instead of prompting (stdin is the null
	// device), which is what we want for encrypted archives.
	// The whole listing is checked: a link or a size hidden after a cut-off
	// point would be unpacked unchecked, so a listing too long to keep is refused.
	listing, complete, err := e.run7z(ctx, max7zListing, append(e.sevenZipArgs("l", "-slt"), "--", archivePath)...)
	if err != nil {
		return e.sevenZipError(ctx, name, listing, err)
	}
	if !complete {
		return fmt.Errorf("extract %s: the list of files is longer than %d bytes: %w", name, max7zListing, ErrArchiveTooLarge)
	}
	total, err := check7zListing(listing, e.maxTotal(), e.Password != "")
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
	// 7z reports nothing we read, so the size of what it has written so far
	// against the size the listing declares is the progress.
	meter := e.startMeter(total, true, func() int64 { return dirSize(stage) })
	defer meter.finish()
	if out, _, err := e.run7z(ctx, 64<<10, append(e.sevenZipArgs("x", "-y", "-o"+stage), "--", archivePath)...); err != nil {
		return e.sevenZipError(ctx, name, out, err)
	}
	if err := moveUnpacked(stage, destDir, e.maxTotal()); err != nil {
		return fmt.Errorf("extract %s: %w", name, err)
	}
	return nil
}

// sevenZipArgs builds a 7z command line: the given arguments, the password
// (when there is one; an empty -p would make 7z prompt).
func (e *Extractor) sevenZipArgs(args ...string) []string {
	if e.Password != "" {
		args = append(args, "-p"+e.Password)
	}
	return args
}

// run7z runs the 7z tool with the given arguments and returns what it printed,
// at most limit bytes of it; complete is false when there was more.
func (e *Extractor) run7z(ctx context.Context, limit int, args ...string) (out []byte, complete bool, err error) {
	cmd := exec.CommandContext(ctx, e.binary(), args...)
	cmd.WaitDelay = 5 * time.Second
	buf := &cappedBuffer{max: limit}
	cmd.Stdout, cmd.Stderr = buf, buf
	err = cmd.Run()
	out = buf.Bytes()
	if err != nil && buf.dropped {
		// The reason a run failed is printed last; keep it with the start.
		out = append(append(append([]byte{}, out...), 0x0a), buf.tail...)
	}
	return out, !buf.dropped, err
}

// sevenZipError turns a failed 7z run into the error the caller reports.
func (e *Extractor) sevenZipError(ctx context.Context, name string, out []byte, err error) error {
	if e.Context != nil && e.Context.Err() != nil {
		return fmt.Errorf("extract %s: stopped: %w", name, e.Context.Err())
	}
	if ctx.Err() != nil {
		return fmt.Errorf("extract %s: 7z took too long and was stopped: %w", name, ctx.Err())
	}
	lower := strings.ToLower(string(out))
	// With the file names encrypted and no password, 7z asks for one (it can't:
	// stdin is the null device) and gives up.
	if strings.Contains(lower, "wrong password") || strings.Contains(lower, "cannot open encrypted archive") || strings.Contains(lower, "enter password") {
		if e.Password != "" {
			return fmt.Errorf("extract %s: %w", name, ErrWrongPassword)
		}
		return fmt.Errorf("extract %s: %w", name, ErrPasswordProtected)
	}
	return fmt.Errorf("extract %s: %w: %s", name, err, sevenZipReason(out))
}

// sevenZipReason picks the lines of a failed 7z run that say why: the ones it
// marks ERROR, or failed to allocate, read or write. 7z prints a banner first,
// so cutting the output at its start would hide them (#189). With no such line
// the end of the output is used.
func sevenZipReason(out []byte) string {
	var reasons []string
	for _, line := range strings.Split(string(out), string(rune(10))) {
		line = strings.TrimSpace(line)
		l := strings.ToLower(line)
		if strings.HasPrefix(l, "error") || strings.Contains(l, "can't allocate") || strings.Contains(l, "crc failed") ||
			strings.Contains(l, "data error") || strings.Contains(l, "unexpected end") || strings.Contains(l, "no space") ||
			strings.Contains(l, "cannot ") || strings.Contains(l, "can not ") || strings.Contains(l, "is not supported") {
			reasons = append(reasons, line)
		}
	}
	if len(reasons) > 0 {
		return truncateOutput([]byte(strings.Join(reasons, "; ")))
	}
	if len(out) > 500 {
		out = out[len(out)-500:]
	}
	return strings.TrimSpace(string(out))
}

// cappedBuffer keeps the first max bytes written and drops the rest, noting
// that it did.
type cappedBuffer struct {
	buf     bytes.Buffer
	max     int
	dropped bool
	tail    []byte // the last few KB written, kept even when buf is full
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	const tailMax = 8 << 10
	c.tail = append(c.tail, p...)
	if len(c.tail) > tailMax {
		c.tail = append([]byte{}, c.tail[len(c.tail)-tailMax:]...)
	}
	room := max(c.max-c.buf.Len(), 0)
	if len(p) > room {
		c.dropped = true
	}
	c.buf.Write(p[:min(len(p), room)])
	return len(p), nil
}

func (c *cappedBuffer) Bytes() []byte { return c.buf.Bytes() }

// sevenZipPerms matches the Unix permission string 7z prints for an entry
// ("-rw-r--r--", "lrwxrwxrwx", "drwxr-xr-x").
var sevenZipPerms = regexp.MustCompile(`^[-dlbcps][-rwxsStT]{9}$`)

// check7zListing reads the output of `7z l -slt` and refuses an archive that
// holds a link or other special file, a path that leaves the destination,
// encrypted files (unless there is a password to open them with), too many
// entries or more data than budget. It returns the total unpacked size the
// listing declares.
//
// A file name can hold a line break, which could make one entry look like two
// and hide the second half's link or size. So an entry whose lines are not all
// "Key = value", a block of fields with no Path, a key given twice in one
// entry and a name with control characters all make the archive unsafe.
func check7zListing(out []byte, budget int64, havePassword bool) (int64, error) {
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
		rawPath, loose := "", false
		for _, line := range strings.Split(block, "\n") {
			if line == "" {
				continue
			}
			k, v, ok := strings.Cut(line, " = ")
			if !ok {
				k, ok = strings.CutSuffix(line, " =") // an empty value, trimmed
			}
			if !ok {
				loose = true
				continue
			}
			k = strings.TrimSpace(k)
			if _, dup := fields[k]; dup {
				return 0, fmt.Errorf("an entry lists %q twice: %w", k, ErrUnsafeArchive)
			}
			if k == "Path" {
				rawPath = v
			}
			fields[k] = strings.TrimSpace(v)
		}
		p, ok := fields["Path"]
		if !ok {
			if len(fields) > 0 {
				return 0, fmt.Errorf("an entry has no name: %w", ErrUnsafeArchive)
			}
			continue // text after the list, such as a warning
		}
		if loose {
			return 0, fmt.Errorf("the name %q runs over more than one line: %w", p, ErrUnsafeArchive)
		}
		if strings.IndexFunc(rawPath, unicode.IsControl) >= 0 {
			return 0, fmt.Errorf("the name %q holds a control character: %w", p, ErrUnsafeArchive)
		}
		entries++
		if entries > maxArchiveEntries {
			return 0, fmt.Errorf("more than %d entries: %w", maxArchiveEntries, ErrArchiveTooLarge)
		}
		if _, err := normalizeEntryName(p); err != nil {
			return 0, err
		}
		if fields["Encrypted"] == "+" && !havePassword {
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
	m       *meter        // progress; may be nil
	wbuf    *bufio.Writer // reused between files
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
	// Written under a new name and renamed over the target, so a file that is
	// already there is replaced, never rewritten: it may be hard-linked into
	// the library by an earlier import, and truncating it in place would
	// damage the library's copy too.
	f, err := os.CreateTemp(filepath.Dir(target), ".unpack-*")
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	// Written in big pieces: see unpackBufferBytes.
	if x.wbuf == nil {
		x.wbuf = bufio.NewWriterSize(f, unpackBufferBytes)
	} else {
		x.wbuf.Reset(f)
	}
	lw := &limitedWriter{w: x.wbuf, left: x.budget, ctx: x.e.ctxOrBackground(), m: x.m}
	_, copyErr := io.Copy(lw, r)
	if copyErr == nil {
		copyErr = x.wbuf.Flush()
	}
	if copyErr == nil {
		// Only a nicety: a network share or NAS mount that refuses chmod
		// still holds the file, so a refusal must not fail the unpack.
		chmodBestEffort(f.Name(), func() error { return chmodFile(f, 0o644) })
	}
	closeErr := f.Close()
	x.budget = lw.left
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil {
		copyErr = os.Rename(f.Name(), target)
	}
	if copyErr != nil {
		os.Remove(f.Name()) // do not leave a partial file for the video finder
		if errors.Is(copyErr, errLimitExceeded) {
			return fmt.Errorf("%q: %w", name, ErrArchiveTooLarge)
		}
		return fmt.Errorf("write %s: %w", name, copyErr)
	}
	return nil
}

var errLimitExceeded = errors.New("size limit exceeded")

// chmodFile sets a mode on an open file. A variable so tests can stand in for a
// file system that refuses chmod.
var chmodFile = func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }

// chmodRefused reports whether err is a file system saying it does not do
// permissions: not permitted, not supported or invalid on this mount (SMB and
// NFS shares, some NAS boxes, NTFS and exFAT mounts).
func chmodRefused(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOSYS) ||
		errors.Is(err, errors.ErrUnsupported)
}

// chmodBestEffort runs a chmod on a file Mediarium just made. Any failure is
// logged at debug and carried on from: the mode is cosmetic, and the file is
// already written, so even a failure the check above does not know is no reason
// to throw the unpacked data away.
func chmodBestEffort(name string, do func() error) {
	if err := do(); err != nil {
		slog.Debug("couldn't set the permissions on an unpacked file, carrying on",
			"path", name, "refused", chmodRefused(err), "err", err)
	}
}

// limitedWriter fails once more than left bytes have been written.
type limitedWriter struct {
	w    io.Writer
	left int64
	ctx  context.Context // an unpack that is stopped fails the next write
	m    *meter          // counts what is written; may be nil
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.ctx != nil {
		if err := l.ctx.Err(); err != nil {
			return 0, err
		}
	}
	if int64(len(p)) > l.left {
		n, _ := l.w.Write(p[:l.left])
		l.left -= int64(n)
		l.m.addWritten(n)
		return n, errLimitExceeded
	}
	n, err := l.w.Write(p)
	l.left -= int64(n)
	l.m.addWritten(n)
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
		// The downloader keeps its own notes in files with these names; a
		// release must not be able to write one (one of them says which torrent
		// to remove from an external client after the import).
		if lower := strings.ToLower(part); strings.HasPrefix(lower, ".mediarium-") || lower == ".debrid-id" {
			return "", fmt.Errorf("entry %q uses a name the app keeps for itself: %w", name, ErrUnsafeArchive)
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
