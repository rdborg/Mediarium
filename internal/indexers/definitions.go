package indexers

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefinitionStore keeps a local copy of the community-maintained Cardigann
// indexer definitions (github.com/Prowlarr/Indexers). Nothing is bundled
// with Mediarium: the catalogue is downloaded, as one archive of the
// repository, only when someone opens the "add a site" list (or adds a
// site whose definition isn't cached yet), and refreshed at most once a
// day after that unless asked.
type DefinitionStore struct {
	// Dir holds the cached <id>.yml files and index.json.
	Dir string
	// SourceURL is the archive (.tar.gz or .zip) of the definitions
	// repository. Injectable for tests.
	SourceURL string
	// HTTPClient downloads the archive; nil uses a client with a timeout.
	HTTPClient *http.Client
	// MaxAge is how old the cache may get before opening the list
	// refreshes it.
	MaxAge time.Duration
	// MaxSchema is the newest definition schema version this engine
	// understands.
	MaxSchema int

	mu          sync.Mutex // guards the cache folder, index and lastAttempt
	refreshMu   sync.Mutex // one download at a time
	index       *DefinitionIndex
	lastAttempt time.Time
}

// DefaultDefinitionsURL is an archive of the default branch of the
// Prowlarr/Indexers repository. One unauthenticated download, not subject
// to the GitHub API rate limit.
const DefaultDefinitionsURL = "https://github.com/Prowlarr/Indexers/archive/refs/heads/master.tar.gz"

// DefinitionsSource and DefinitionsLicence describe where the definitions
// come from, for the UI and docs.
const (
	DefinitionsSource  = "Prowlarr/Indexers (https://github.com/Prowlarr/Indexers), community-maintained Cardigann definitions"
	DefinitionsLicence = "The Prowlarr/Indexers repository publishes no licence file; many definitions are synced from Jackett (GPL-2.0). Mediarium does not ship them: they are downloaded to your own server when you open the site list."
)

// SupportedSchema is the Cardigann definition schema version the engine
// implements.
const SupportedSchema = 11

// NewDefinitionStore returns a store caching under dir.
func NewDefinitionStore(dir string) *DefinitionStore {
	return &DefinitionStore{Dir: dir, SourceURL: DefaultDefinitionsURL, MaxAge: 24 * time.Hour, MaxSchema: SupportedSchema}
}

// DefinitionIndex is the cached catalogue (index.json).
type DefinitionIndex struct {
	UpdatedAt     time.Time           `json:"updatedAt"`
	Source        string              `json:"source"`
	SchemaVersion int                 `json:"schemaVersion"`
	Definitions   []DefinitionSummary `json:"definitions"`
}

// DefinitionSummary is what the "add a site" list shows for a definition.
type DefinitionSummary struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Type        string           `json:"type"`
	Language    string           `json:"language"`
	Protocol    string           `json:"protocol"`
	Links       []string         `json:"links"`
	LegacyLinks []string         `json:"legacyLinks,omitempty"`
	Replaces    []string         `json:"replaces,omitempty"`
	Settings    []SettingSummary `json:"settings"`
	// Supported is false when the definition uses something this engine
	// can't run; Problem says what.
	Supported bool   `json:"supported"`
	Problem   string `json:"problem,omitempty"`
}

// SettingSummary is one setting as the definition declares it, for the UI
// to render the form. Never carries a user's value.
type SettingSummary struct {
	Name    string          `json:"name"`
	Label   string          `json:"label"`
	Type    string          `json:"type"`
	Default any             `json:"default,omitempty"`
	Options []SettingOption `json:"options,omitempty"`
}

// SettingOption is one choice of a select setting.
type SettingOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ErrDefinitionNotFound means no cached definition has that id.
var ErrDefinitionNotFound = errors.New("indexer definition not found")

const (
	indexFile           = "index.json"
	maxArchiveBytes     = 64 << 20
	maxDefinitionBytes  = 2 << 20
	minForcedRefreshGap = time.Minute
	failedRefreshGap    = 15 * time.Minute
)

var definitionFileRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Catalogue returns the definition list, refreshing it first when forced
// or when it is missing or older than MaxAge. If a refresh fails but an
// older copy exists, that copy is returned together with the error.
// Downloading happens outside the lock readers use, so searches and the
// indexer list never wait for it.
func (s *DefinitionStore) Catalogue(ctx context.Context, force bool) (*DefinitionIndex, error) {
	s.mu.Lock()
	idx, _ := s.loadIndexLocked()
	need := s.needsRefreshLocked(idx, force)
	s.mu.Unlock()
	if need {
		s.refreshMu.Lock()
		defer s.refreshMu.Unlock()
		s.mu.Lock() // another caller may have refreshed meanwhile
		idx, _ = s.loadIndexLocked()
		need = s.needsRefreshLocked(idx, force)
		if need {
			s.lastAttempt = time.Now()
		}
		s.mu.Unlock()
	}
	if need {
		fresh, err := s.refresh(ctx)
		if err != nil {
			if idx == nil {
				return nil, err
			}
			log.Printf("indexer definitions: refresh failed, using cached copy: %v", err)
			return idx, err
		}
		idx = fresh
	}
	if idx == nil {
		return nil, fmt.Errorf("indexer definitions are not downloaded yet")
	}
	return idx, nil
}

func (s *DefinitionStore) needsRefreshLocked(idx *DefinitionIndex, force bool) bool {
	stale := idx == nil || time.Since(idx.UpdatedAt) > s.maxAge()
	gap := failedRefreshGap
	if force {
		gap = minForcedRefreshGap
	}
	return (force || stale) && (idx == nil || time.Since(s.lastAttempt) >= gap)
}

// Generation identifies the cached catalogue version, so running indexers
// can notice a refresh.
func (s *DefinitionStore) Generation() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, _ := s.loadIndexLocked()
	if idx == nil {
		return ""
	}
	return idx.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

// Summary returns one definition's catalogue entry, downloading the
// catalogue if it isn't cached yet.
func (s *DefinitionStore) Summary(ctx context.Context, id string) (DefinitionSummary, error) {
	idx, err := s.Catalogue(ctx, false)
	if idx == nil {
		return DefinitionSummary{}, err
	}
	if d, ok := findSummary(idx, id); ok {
		return d, nil
	}
	return DefinitionSummary{}, fmt.Errorf("%w: %q", ErrDefinitionNotFound, id)
}

// CachedSummary looks a definition up in the local cache only, never
// downloading anything.
func (s *DefinitionStore) CachedSummary(id string) (DefinitionSummary, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, _ := s.loadIndexLocked()
	if idx == nil {
		return DefinitionSummary{}, false
	}
	return findSummary(idx, id)
}

func findSummary(idx *DefinitionIndex, id string) (DefinitionSummary, bool) {
	for _, d := range idx.Definitions {
		if d.ID == id {
			return d, true
		}
	}
	for _, d := range idx.Definitions { // renamed upstream
		for _, r := range d.Replaces {
			if r == id {
				return d, true
			}
		}
	}
	return DefinitionSummary{}, false
}

// Load parses and validates one definition from the cache, downloading the
// catalogue first if needed.
func (s *DefinitionStore) Load(ctx context.Context, id string) (*Definition, error) {
	sum, err := s.Summary(ctx, id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	data, err := os.ReadFile(filepath.Join(s.Dir, sum.ID+".yml"))
	s.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("read indexer definition %q: %w", sum.ID, err)
	}
	def, err := ParseDefinition(data)
	if err != nil {
		return nil, fmt.Errorf("indexer definition %q: %w", sum.ID, err)
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return def, nil
}

func (s *DefinitionStore) maxAge() time.Duration {
	if s.MaxAge > 0 {
		return s.MaxAge
	}
	return 24 * time.Hour
}

func (s *DefinitionStore) loadIndexLocked() (*DefinitionIndex, error) {
	if s.index != nil {
		return s.index, nil
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, indexFile))
	if err != nil {
		return nil, err
	}
	var idx DefinitionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("read %s: %w", indexFile, err)
	}
	s.index = &idx
	return s.index, nil
}

// refresh downloads the archive and rebuilds the cache. Callers hold
// refreshMu; s.mu is only taken to swap the finished folder in.
func (s *DefinitionStore) refresh(ctx context.Context) (*DefinitionIndex, error) {
	if s.SourceURL == "" {
		return nil, fmt.Errorf("no indexer definitions source configured")
	}
	archive, err := s.download(ctx)
	if err != nil {
		return nil, err
	}
	files, version, err := extractDefinitions(archive, s.maxSchema())
	if err != nil {
		return nil, err
	}

	idx := &DefinitionIndex{
		UpdatedAt:     time.Now().UTC(),
		Source:        DefinitionsSource + ", schema v" + strconv.Itoa(version),
		SchemaVersion: version,
	}
	tmp := s.Dir + ".new"
	if err := os.RemoveAll(tmp); err != nil {
		return nil, fmt.Errorf("clear %s: %w", tmp, err)
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", tmp, err)
	}
	for name, data := range files {
		def, err := ParseDefinition(data)
		if err != nil {
			// list it, marked unusable, rather than failing the whole list
			idx.Definitions = append(idx.Definitions, DefinitionSummary{
				ID: name, Name: name, Links: []string{}, Settings: []SettingSummary{},
				Protocol: string(ProtocolTorrent), Problem: "the definition file can't be read: " + err.Error(),
			})
			continue
		}
		if !definitionFileRe.MatchString(def.ID) {
			continue
		}
		sum := summarize(def)
		// stored under its id (not the file name) so Load finds it
		if err := os.WriteFile(filepath.Join(tmp, def.ID+".yml"), data, 0o644); err != nil {
			return nil, fmt.Errorf("write definition %s: %w", def.ID, err)
		}
		idx.Definitions = append(idx.Definitions, sum)
	}
	if len(idx.Definitions) == 0 {
		return nil, fmt.Errorf("the definitions archive contained no usable definitions")
	}
	sort.Slice(idx.Definitions, func(i, j int) bool {
		return strings.ToLower(idx.Definitions[i].Name) < strings.ToLower(idx.Definitions[j].Name)
	})
	data, err := json.MarshalIndent(idx, "", " ")
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", indexFile, err)
	}
	if err := os.WriteFile(filepath.Join(tmp, indexFile), data, 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", indexFile, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.RemoveAll(s.Dir); err != nil {
		return nil, fmt.Errorf("replace %s: %w", s.Dir, err)
	}
	if err := os.Rename(tmp, s.Dir); err != nil {
		return nil, fmt.Errorf("replace %s: %w", s.Dir, err)
	}
	s.index = idx
	log.Printf("indexer definitions: refreshed count=%d schema=v%d", len(idx.Definitions), version)
	return idx, nil
}

func (s *DefinitionStore) maxSchema() int {
	if s.MaxSchema > 0 {
		return s.MaxSchema
	}
	return SupportedSchema
}

func (s *DefinitionStore) download(ctx context.Context) ([]byte, error) {
	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.SourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build definitions request: %w", err)
	}
	req.Header.Set("User-Agent", "Mediarium")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("archive request returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	if len(data) > maxArchiveBytes {
		return nil, fmt.Errorf("archive larger than %d bytes", maxArchiveBytes)
	}
	return data, nil
}

var versionDirRe = regexp.MustCompile(`(?:^|/)definitions/v(\d+)/([^/]+)\.ya?ml$`)

// extractDefinitions reads definitions/vN/*.yml out of a .tar.gz or .zip
// archive of the repository and keeps the newest schema version not above
// maxSchema (or, if every version is newer, the oldest one). It returns
// the files by base name.
func extractDefinitions(archive []byte, maxSchema int) (map[string][]byte, int, error) {
	byVersion := map[int]map[string][]byte{}
	add := func(name string, r io.Reader, size int64) error {
		m := versionDirRe.FindStringSubmatch(path.Clean(strings.ReplaceAll(name, "\\", "/")))
		if m == nil || size > maxDefinitionBytes || !definitionFileRe.MatchString(m[2]) {
			return nil
		}
		v, _ := strconv.Atoi(m[1])
		data, err := io.ReadAll(io.LimitReader(r, maxDefinitionBytes))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if byVersion[v] == nil {
			byVersion[v] = map[string][]byte{}
		}
		byVersion[v][m[2]] = data
		return nil
	}

	switch {
	case len(archive) > 2 && archive[0] == 0x1f && archive[1] == 0x8b:
		gz, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, 0, fmt.Errorf("open definitions archive: %w", err)
		}
		tr := tar.NewReader(gz)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, 0, fmt.Errorf("read definitions archive: %w", err)
			}
			if hdr.Typeflag != tar.TypeReg {
				continue
			}
			if err := add(hdr.Name, tr, hdr.Size); err != nil {
				return nil, 0, err
			}
		}
	case len(archive) > 2 && archive[0] == 'P' && archive[1] == 'K':
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, 0, fmt.Errorf("open definitions archive: %w", err)
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, 0, fmt.Errorf("read %s: %w", f.Name, err)
			}
			err = add(f.Name, rc, int64(f.UncompressedSize64))
			rc.Close()
			if err != nil {
				return nil, 0, err
			}
		}
	default:
		return nil, 0, fmt.Errorf("the definitions download is not a .tar.gz or .zip archive")
	}

	best := -1
	for v := range byVersion {
		if v <= maxSchema && v > best {
			best = v
		}
	}
	if best < 0 {
		for v := range byVersion {
			if best < 0 || v < best {
				best = v
			}
		}
	}
	if best < 0 {
		return nil, 0, fmt.Errorf("the definitions archive has no definitions/vN folder")
	}
	return byVersion[best], best, nil
}

// predefinedInfo fills in the text of the predefined info_* setting types,
// which definitions declare without a label or text of their own.
var predefinedInfo = map[string][2]string{
	"info_cookie": {"How to get the cookie",
		"Sign in to the site in your web browser, open the browser's developer tools, reload a page of the site and copy the value of the Cookie request header from the network tab. Paste it into the Cookie field. When the site signs you out, the cookie stops working and has to be copied again."},
	"info_flaresolverr": {"About Cloudflare protection",
		"This site may show a Cloudflare check that only a real browser can pass. To use it, run FlareSolverr alongside Mediarium and set its address in Settings > Indexers."},
	"info_useragent": {"About the User-Agent",
		"Some sites tie your session cookie to the browser it was created in. Copy your browser's User-Agent header together with the cookie."},
	"info_category_8000": {"About categories",
		"Results in categories this site doesn't map are listed as Other."},
}

func summarize(def *Definition) DefinitionSummary {
	sum := DefinitionSummary{
		ID:          def.ID,
		Name:        def.Name,
		Description: def.Description,
		Type:        def.Type,
		Language:    def.Language,
		Protocol:    string(def.DefinitionProtocol()),
		Links:       def.Links,
		LegacyLinks: def.LegacyLinks,
		Replaces:    def.Replaces,
		Settings:    []SettingSummary{},
		Supported:   true,
	}
	if sum.Name == "" {
		sum.Name = def.ID
	}
	if sum.Links == nil {
		sum.Links = []string{}
	}
	for _, st := range def.Settings {
		ss := SettingSummary{Name: st.Name, Label: st.Label, Type: st.Type}
		if info, ok := predefinedInfo[st.Type]; ok {
			if ss.Label == "" {
				ss.Label = info[0]
			}
			if st.Default == "" {
				st.Default = info[1]
			}
		}
		switch st.Type {
		case "checkbox":
			ss.Default = parseBoolSetting(st.Default)
		default:
			if st.Default != "" {
				ss.Default = st.Default
			}
		}
		for _, k := range st.Options.Keys {
			ss.Options = append(ss.Options, SettingOption{Value: k, Label: st.Options.Values[k]})
		}
		if ss.Label == "" {
			ss.Label = st.Name
		}
		sum.Settings = append(sum.Settings, ss)
	}
	if problems := def.Problems(); len(problems) > 0 {
		sum.Supported = false
		sum.Problem = strings.Join(problems, "; ")
	}
	return sum
}

// secretNameParts mark settings that hold credentials even when the
// definition declares them as plain text (cookies, API keys, passkeys).
var secretNameParts = []string{"password", "cookie", "apikey", "api_key", "passkey", "pass_key", "rsskey", "authkey", "token", "secret", "2fa"}

// SecretSetting reports whether a definition setting holds a credential:
// stored encrypted and never sent back to the UI.
func SecretSetting(settingType, name string) bool {
	if settingType == "password" {
		return true
	}
	if strings.HasPrefix(settingType, "info") {
		return false
	}
	n := strings.ToLower(name)
	for _, p := range secretNameParts {
		if strings.Contains(n, p) {
			return true
		}
	}
	return n == "pin" || n == "pid" || n == "key"
}

// Setting returns the named setting of a summary.
func (d DefinitionSummary) Setting(name string) (SettingSummary, bool) {
	for _, s := range d.Settings {
		if s.Name == name {
			return s, true
		}
	}
	return SettingSummary{}, false
}

// IsSecret reports whether the named setting holds a credential.
func (d DefinitionSummary) IsSecret(name string) bool {
	s, ok := d.Setting(name)
	if !ok {
		return SecretSetting("", name)
	}
	return SecretSetting(s.Type, s.Name)
}

// HasLink reports whether u is one of the site's addresses (trailing
// slashes ignored).
func (d DefinitionSummary) HasLink(u string) bool {
	norm := func(s string) string { return strings.TrimRight(strings.ToLower(strings.TrimSpace(s)), "/") }
	for _, l := range append(append([]string{}, d.Links...), d.LegacyLinks...) {
		if norm(l) == norm(u) {
			return true
		}
	}
	return false
}

func parseBoolSetting(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on", checkboxTrue:
		return true
	}
	return false
}
