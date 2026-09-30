// Package updatecheck asks GitHub which release of Mediarium is the newest, so
// the app can tell an administrator that an update is out.
//
// It only reads. The request goes to the public GitHub Releases API of the
// official repository and carries no identifying data: no account, no install
// id, no settings, only a User-Agent of the form "Mediarium/<version>". Nothing
// is downloaded or installed by this package; installing is up to the person.
package updatecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/semver"
)

// DefaultRepo is the official repository, as owner/name. A build for another
// fork can change it with
//
//	-ldflags "-X github.com/rdborg/mediarium/internal/updatecheck.DefaultRepo=owner/name"
var DefaultRepo = "rdborg/Mediarium"

// DefaultBaseURL is the GitHub API.
const DefaultBaseURL = "https://api.github.com"

const (
	requestTimeout = 15 * time.Second
	maxResponse    = 4 << 20 // bytes of the release list we are willing to read
	perPage        = 30      // how many recent releases to look through
	// NotesLines is how many lines of the release notes are kept.
	NotesLines   = 10
	maxLineRunes = 300
)

// Errors a check can end in. Callers show the same plain "couldn't check just
// now" for all of them; the difference is only for the log.
var (
	ErrRateLimited = errors.New("GitHub is limiting requests right now")
	ErrUnavailable = errors.New("GitHub did not give a usable answer")
)

// Release is the newest release that applies to this install.
type Release struct {
	Version     string    `json:"version"`             // 1.2.0, without a leading v
	Name        string    `json:"name,omitempty"`      // the title of the release
	Notes       string    `json:"notes,omitempty"`     // the first lines of the notes, plain text
	MoreNotes   bool      `json:"moreNotes,omitempty"` // the notes go on past what is shown
	URL         string    `json:"url"`                 // the release page on GitHub
	Prerelease  bool      `json:"prerelease,omitempty"`
	PublishedAt time.Time `json:"publishedAt,omitempty"`
	Tag         string    `json:"tag,omitempty"`    // the git tag, used to build download addresses
	Assets      []Asset   `json:"assets,omitempty"` // the files attached to the release
}

// githubRelease is the part of a GitHub release we read.
type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// Checker fetches the release list. It remembers the last answer and its
// ETag, so asking again costs nothing against GitHub's rate limit while
// nothing has changed.
type Checker struct {
	Repo    string       // owner/name; DefaultRepo when empty
	Version string       // the running version, for the User-Agent
	BaseURL string       // DefaultBaseURL when empty (tests point it at a fake server)
	Client  *http.Client // netguard client with a timeout when nil

	mu   sync.Mutex
	etag string
	body []byte
}

// New returns a Checker for the official repository.
func New(runningVersion string) *Checker {
	return &Checker{Repo: DefaultRepo, Version: runningVersion}
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidRepo reports whether s looks like owner/name.
func ValidRepo(s string) bool { return repoPattern.MatchString(s) }

func (c *Checker) repo() string {
	if c.Repo != "" {
		return c.Repo
	}
	return DefaultRepo
}

func (c *Checker) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return netguard.Client(requestTimeout)
}

// userAgent is the only thing that identifies the request: the program and
// its version, as every HTTP client does.
func (c *Checker) userAgent() string {
	v := c.Version
	if v == "" {
		v = "dev"
	}
	// The version comes from the build, but keep it to safe characters anyway.
	v = strings.Map(func(r rune) rune {
		if r < 0x21 || r > 0x7e {
			return -1
		}
		return r
	}, v)
	return "Mediarium/" + v
}

// Latest returns the newest release that applies to a running version:
// a stable release when running is a normal release, and the newest of either
// kind when running is itself a pre-release. It does not say whether that
// release is newer than the running one (see Newer). ok is false when the
// repository has no usable release yet.
func (c *Checker) Latest(ctx context.Context, running string) (rel Release, ok bool, err error) {
	list, err := c.fetch(ctx)
	if err != nil {
		return Release{}, false, err
	}
	rel, ok = pick(c.repo(), running, list)
	return rel, ok, nil
}

func (c *Checker) fetch(ctx context.Context) ([]githubRelease, error) {
	repo := c.repo()
	if !ValidRepo(repo) {
		return nil, fmt.Errorf("%w: the repository name %q is not valid", ErrUnavailable, repo)
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	endpoint := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", base, repo, perPage)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent())

	c.mu.Lock()
	if c.etag != "" && len(c.body) > 0 {
		req.Header.Set("If-None-Match", c.etag)
	}
	c.mu.Unlock()

	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, netguard.CleanError(err))
	}
	defer resp.Body.Close()

	var raw []byte
	switch {
	case resp.StatusCode == http.StatusNotModified:
		c.mu.Lock()
		raw = c.body
		c.mu.Unlock()
		if len(raw) == 0 {
			return nil, fmt.Errorf("%w: unexpected 304", ErrUnavailable)
		}
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && rateLimited(resp):
		return nil, ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	default:
		raw, err = io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
		if err != nil {
			return nil, fmt.Errorf("%w: read answer: %w", ErrUnavailable, err)
		}
		if len(raw) > maxResponse {
			return nil, fmt.Errorf("%w: answer too large", ErrUnavailable)
		}
	}

	var list []githubRelease
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&list); err != nil {
		return nil, fmt.Errorf("%w: answer is not a release list", ErrUnavailable)
	}
	if resp.StatusCode == http.StatusOK {
		c.mu.Lock()
		c.etag, c.body = resp.Header.Get("ETag"), raw
		c.mu.Unlock()
	}
	return list, nil
}

// rateLimited tells GitHub's "you are over the limit" 403 from any other 403.
func rateLimited(resp *http.Response) bool {
	return resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""
}

// pick chooses the newest applicable release from a GitHub list. Drafts and
// tags that are not version numbers are skipped. Pre-releases count only when
// the running version is a pre-release too.
func pick(repo, running string, list []githubRelease) (Release, bool) {
	runningPre := false
	if rv, err := semver.Parse(running); err == nil {
		runningPre = rv.IsPrerelease()
	}
	var (
		best    semver.Version
		bestRel githubRelease
		found   bool
	)
	for _, g := range list {
		if g.Draft {
			continue
		}
		v, err := semver.Parse(g.TagName)
		if err != nil {
			continue
		}
		if (g.Prerelease || v.IsPrerelease()) && !runningPre {
			continue
		}
		if !found || v.Compare(best) > 0 {
			best, bestRel, found = v, g, true
		}
	}
	if !found {
		return Release{}, false
	}
	notes, more := PlainNotes(bestRel.Body, NotesLines)
	return Release{
		Version:     best.String(),
		Name:        cleanLine(bestRel.Name),
		Notes:       notes,
		MoreNotes:   more,
		URL:         releaseURL(repo, bestRel.TagName),
		Prerelease:  bestRel.Prerelease || best.IsPrerelease(),
		PublishedAt: bestRel.PublishedAt,
		Tag:         strings.TrimSpace(bestRel.TagName),
		Assets:      assetsOf(bestRel),
	}, true
}

// releaseURL builds the release page address ourselves instead of trusting
// the one in the answer, so what a person is sent to is always a page of the
// official repository.
func releaseURL(repo, tag string) string {
	return "https://github.com/" + repo + "/releases/tag/" + url.PathEscape(strings.TrimSpace(tag))
}

// Newer reports whether rel is a newer version than running. A running
// version that is not a version number (a "dev" build) is never told to
// update.
func Newer(running string, rel Release) bool {
	c, err := semver.Compare(rel.Version, running)
	return err == nil && c > 0
}

var (
	htmlTag      = regexp.MustCompile(`<[^>]*>`)
	mdImage      = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLink       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdHeading    = regexp.MustCompile(`^\s{0,3}#{1,6}\s*`)
	mdEmphasis   = strings.NewReplacer("**", "", "__", "", "`", "")
	blankRuns    = regexp.MustCompile(`\n{3,}`)
	onlyRulerish = regexp.MustCompile(`^\s*([-*_=]\s*){3,}$`)
)

// PlainNotes turns release notes (Markdown from GitHub) into a few lines of
// plain text: at most maxLines lines, headings and emphasis marks removed,
// links reduced to their words, HTML tags and control characters dropped. The
// result is text only. The page shows it as text, never as HTML. more says
// whether there was more than was kept.
func PlainNotes(body string, maxLines int) (notes string, more bool) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if onlyRulerish.MatchString(line) {
			continue
		}
		line = htmlTag.ReplaceAllString(line, "")
		line = mdImage.ReplaceAllString(line, "")
		line = mdLink.ReplaceAllString(line, "$1")
		line = mdHeading.ReplaceAllString(line, "")
		line = mdEmphasis.Replace(line)
		line = cleanLine(line)
		lines = append(lines, line)
	}
	text := strings.Trim(blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"), "\n")
	if text == "" {
		return "", false
	}
	all := strings.Split(text, "\n")
	if maxLines < 0 {
		maxLines = 0
	}
	if len(all) > maxLines {
		return strings.TrimRight(strings.Join(all[:maxLines], "\n"), "\n "), true
	}
	return text, false
}

// cleanLine removes control characters, trims the ends and shortens a very
// long line.
func cleanLine(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case unicode.IsControl(r), r == utf8.RuneError, unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxLineRunes {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:maxLineRunes])) + "…"
	}
	return s
}

// Asset is one file attached to a release.
type Asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// assetsOf lists the attached files whose names are plain file names (no
// slashes or odd characters), which is all the installer ever asks for.
func assetsOf(g githubRelease) []Asset {
	var out []Asset
	for _, a := range g.Assets {
		if !safeAssetName.MatchString(a.Name) {
			continue
		}
		out = append(out, Asset{Name: a.Name, Size: a.Size})
	}
	return out
}

var safeAssetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,120}$`)
