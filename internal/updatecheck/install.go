package updatecheck

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

// The files a release carries (see .github/workflows/release.yml and
// docs/RELEASING.md). sha256sums.txt lists the SHA-256 of every archive and is
// signed with the maintainer's private key; the signature is the only thing
// Mediarium trusts, so a changed file on GitHub or a changed download cannot
// pass for a real release.
const (
	SumsName = "sha256sums.txt"
	SigName  = "sha256sums.txt.sig"
)

// The sizes we are willing to read. The program is about 45 MB.
const (
	maxSums    = 1 << 20   // sha256sums.txt
	maxSig     = 4 << 10   // its signature
	MaxArchive = 300 << 20 // the archive as downloaded
	// MaxBinary is the biggest program file we unpack from an archive.
	MaxBinary = 200 << 20
)

// Problems the caller turns into plain sentences.
var (
	ErrNoSignature    = errors.New("the release has no signature file")
	ErrBadSignature   = errors.New("the signature does not match")
	ErrBadChecksum    = errors.New("the downloaded file does not match the signed checksum")
	ErrNoArchive      = errors.New("the release has no program file for this system")
	ErrHostNotAllowed = errors.New("the download was sent to a host that is not allowed")
	ErrNoProgram      = errors.New("the archive does not contain the program")
	ErrKeyMissing     = errors.New("no signing key is built into this version")
)

// ParsePublicKey reads the base64 text of a raw 32-byte ed25519 public key.
func ParsePublicKey(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("%w: not base64", ErrKeyMissing)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: wrong length", ErrKeyMissing)
	}
	return ed25519.PublicKey(raw), nil
}

// ArchiveName is the file the release pipeline makes for a system:
// mediarium_1.2.0_linux_amd64.tar.gz (a .zip on Windows).
func ArchiveName(version, goos, goarch string) string {
	name := fmt.Sprintf("mediarium_%s_%s_%s", version, goos, goarch)
	if goos == "windows" {
		return name + ".zip"
	}
	return name + ".tar.gz"
}

// Installable says whether a release can be installed by the app on this
// system: it must have the signature file, the list of checksums and the
// program file for goos/goarch. reason is a plain sentence when it cannot.
// Only Linux can install itself, because only the container images know how to
// start an installed update (see docs/INSTALL.md).
func Installable(rel Release, goos, goarch string) (ok bool, reason string) {
	kind, why := installProblem(rel, goos, goarch)
	return kind == nil, why
}

// installProblem is Installable with the kind of problem as an error value, so
// callers never have to match on the wording of the sentence.
func installProblem(rel Release, goos, goarch string) (kind error, reason string) {
	if goos != "linux" {
		return ErrNoArchive, "Automatic updates only work in the Docker image. Download the new version from the release page."
	}
	has := map[string]bool{}
	for _, a := range rel.Assets {
		has[a.Name] = true
	}
	if !has[SumsName] || !has[SigName] {
		return ErrNoSignature, "This release isn't signed, so it can't be installed automatically. Update the way you installed it."
	}
	if !has[ArchiveName(rel.Version, goos, goarch)] {
		return ErrNoArchive, fmt.Sprintf("This release has no program file for %s/%s.", goos, goarch)
	}
	return nil, ""
}

// Fetcher downloads a release and checks it. Only the official repository's
// release files are ever fetched, from GitHub's own hosts.
type Fetcher struct {
	Repo    string       // owner/name
	Base    string       // "https://github.com" unless a test says otherwise
	Version string       // running version, for the User-Agent
	Client  *http.Client // built by NewFetcher

	allowedHosts map[string]bool
	allowHTTP    bool
}

// releaseHosts are the hosts a release download may be on. GitHub answers a
// download with a redirect to its file storage, and has used both of the
// storage names.
var releaseHosts = []string{
	"github.com",
	"objects.githubusercontent.com",
	"release-assets.githubusercontent.com",
}

// NewFetcher returns a Fetcher for the official repository.
func NewFetcher(runningVersion string) *Fetcher {
	f := &Fetcher{Repo: DefaultRepo, Base: "https://github.com", Version: runningVersion}
	f.setHosts(releaseHosts, false)
	return f
}

// setHosts fixes the allowed hosts and installs the redirect check on a client
// that also refuses link-local and metadata addresses (netguard).
func (f *Fetcher) setHosts(hosts []string, allowHTTP bool) {
	f.allowedHosts = map[string]bool{}
	for _, h := range hosts {
		f.allowedHosts[strings.ToLower(h)] = true
	}
	f.allowHTTP = allowHTTP
	c := netguard.Client(0)
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return f.checkURL(req.URL)
	}
	f.Client = c
}

func (f *Fetcher) checkURL(u *url.URL) error {
	if u.Scheme != "https" && !(f.allowHTTP && u.Scheme == "http") {
		return fmt.Errorf("%w: %s is not a secure address", ErrHostNotAllowed, u.Hostname())
	}
	if !f.allowedHosts[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, u.Hostname())
	}
	return nil
}

// assetURL is the address of a file attached to a release, built from the
// repository and tag rather than taken from GitHub's answer.
func (f *Fetcher) assetURL(tag, name string) string {
	return strings.TrimRight(f.Base, "/") + "/" + f.Repo + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
}

// get downloads one file, refusing anything that is not from an allowed host,
// and returns at most max bytes.
func (f *Fetcher) get(ctx context.Context, address string, dst io.Writer, max int64) (int64, error) {
	u, err := url.Parse(address)
	if err != nil {
		return 0, fmt.Errorf("bad address: %w", err)
	}
	if err := f.checkURL(u); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	ua := (&Checker{Version: f.Version}).userAgent()
	req.Header.Set("User-Agent", ua)
	resp, err := f.Client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download: status %d", resp.StatusCode)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, max+1))
	if err != nil {
		return n, fmt.Errorf("download: %w", err)
	}
	if n > max {
		return n, fmt.Errorf("download: the file is larger than the %d MB allowed", max>>20)
	}
	return n, nil
}

func (f *Fetcher) getBytes(ctx context.Context, tag, name string, max int64) ([]byte, error) {
	var b strings.Builder
	if _, err := f.get(ctx, f.assetURL(tag, name), &b, max); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// Download is what Fetch returns.
type Download struct {
	Path   string // the unpacked program, in the folder that was given
	SHA256 string // hex SHA-256 of that program file
	Bytes  int64
}

// Fetch downloads the release's checksum list and signature, checks the
// signature against pub, downloads the archive for goos/goarch, checks its
// SHA-256 against the signed list, and unpacks the program file into dir. It
// removes everything it made if it fails. Nothing is installed here.
func (f *Fetcher) Fetch(ctx context.Context, rel Release, goos, goarch string, pub ed25519.PublicKey, dir string) (*Download, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, ErrKeyMissing
	}
	if kind, _ := installProblem(rel, goos, goarch); kind != nil {
		return nil, kind
	}
	tag := rel.Tag
	if tag == "" {
		tag = "v" + rel.Version
	}

	sums, err := f.getBytes(ctx, tag, SumsName, maxSums)
	if err != nil {
		return nil, fmt.Errorf("get the checksum list: %w", err)
	}
	sigText, err := f.getBytes(ctx, tag, SigName, maxSig)
	if err != nil {
		return nil, fmt.Errorf("get the signature: %w", err)
	}
	if err := VerifySignature(pub, sums, string(sigText)); err != nil {
		return nil, err
	}
	archive := ArchiveName(rel.Version, goos, goarch)
	want, err := SumFor(sums, archive)
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp(dir, ".release-*.download")
	if err != nil {
		return nil, fmt.Errorf("create download file: %w", err)
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	_, err = f.get(ctx, f.assetURL(tag, archive), io.MultiWriter(tmp, h), MaxArchive)
	if cerr := tmp.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("write download file: %w", cerr)
	}
	if err != nil {
		return nil, err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return nil, ErrBadChecksum
	}
	return ExtractProgram(tmp.Name(), dir)
}

// VerifySignature checks a base64 ed25519 signature (as written by
// tools/signsums) over data.
func VerifySignature(pub ed25519.PublicKey, data []byte, sigBase64 string) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigBase64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	if !ed25519.Verify(pub, data, sig) {
		return ErrBadSignature
	}
	return nil
}

// SumFor finds the SHA-256 of file name in a sha256sum-style list
// ("<hex>  <name>", the name optionally starting with * or ./).
func SumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		file := strings.TrimPrefix(strings.TrimPrefix(fields[1], "*"), "./")
		if file != name {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != 64 {
			return "", fmt.Errorf("%w: the list has a bad checksum for %s", ErrBadChecksum, name)
		}
		if _, err := hex.DecodeString(sum); err != nil {
			return "", fmt.Errorf("%w: the list has a bad checksum for %s", ErrBadChecksum, name)
		}
		return sum, nil
	}
	return "", fmt.Errorf("%w: %s is not in the signed list", ErrNoArchive, name)
}

// ExtractProgram unpacks the "mediarium" program file from a release .tar.gz
// into dir and returns where it is. Only a regular file named mediarium, at
// most one folder deep, is taken; every other entry is ignored, so an archive
// cannot write anywhere else.
func ExtractProgram(archive, dir string) (*Download, error) {
	in, err := os.Open(archive)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return nil, fmt.Errorf("%w: not a gzip file", ErrNoProgram)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for i := 0; i < 200; i++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNoProgram, err)
		}
		clean := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if hdr.Typeflag != tar.TypeReg || path.Base(clean) != "mediarium" || strings.Count(clean, "/") > 1 || strings.HasPrefix(clean, "..") || path.IsAbs(clean) {
			continue
		}
		if hdr.Size <= 0 || hdr.Size > MaxBinary {
			return nil, fmt.Errorf("%w: it is the wrong size", ErrNoProgram)
		}
		out, err := os.CreateTemp(dir, ".release-*.program")
		if err != nil {
			return nil, fmt.Errorf("create program file: %w", err)
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(tr, MaxBinary+1))
		if cerr := out.Close(); err == nil && cerr != nil {
			err = cerr
		}
		if err != nil || n > MaxBinary {
			os.Remove(out.Name())
			return nil, fmt.Errorf("%w: cannot unpack it", ErrNoProgram)
		}
		if err := os.Chmod(out.Name(), 0o755); err != nil {
			os.Remove(out.Name())
			return nil, fmt.Errorf("make the program runnable: %w", err)
		}
		return &Download{Path: filepath.Clean(out.Name()), SHA256: hex.EncodeToString(h.Sum(nil)), Bytes: n}, nil
	}
	return nil, ErrNoProgram
}

// Timeout for a whole release download (a slow line is fine, a hung one is not).
const DownloadTimeout = 15 * time.Minute
