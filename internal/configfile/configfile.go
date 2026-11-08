// Package configfile keeps a Mediarium's settings in one YAML file, for people
// who want them in git or in a deploy script. It does not touch the database:
// it reads and writes through the same API the Settings pages use, so every
// value is checked exactly as if it had been typed in the page.
//
// The file has one block per settings page that holds a single set of values:
//
//	settings:        # Settings > General, Library, Downloading, Info, ...
//	downloadClient:  # Settings > Downloading > Torrents > Use your own torrent client
//	debrid:          # Settings > Downloading > Torrents > Debrid service
//	proxy:           # Settings > Downloading > Proxy
//	systemOptions:   # Settings > System > Updates
//
// Things you add several of are lists, matched by their name:
//
//	indexers:        # Settings > Indexers
//	usenetServers:   # Settings > Downloading > Usenet servers
//	mediaServers:    # Settings > Media servers
//	qualityProfiles: # Settings > Profiles
//
// A list item that Mediarium doesn't have yet is added, one it has is changed
// to match, and one that is not in the file is left alone.
//
// Anything not in the file is left as it is. Secrets are written as
// ${NAME}, which is replaced with the environment variable NAME when the file
// is applied, so they never have to be in the file.
package configfile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FormatVersion is the version of the file format this Mediarium writes and
// reads. A file made by a newer Mediarium has a higher number and is refused.
const FormatVersion = 1

// SecretPlaceholder stands in for a password or key in an export. Read back,
// it means "keep the one Mediarium has"; for something the file adds it means
// the password still has to be entered.
const SecretPlaceholder = "<secret>"

// NewerFormatError is a file made by a newer Mediarium.
type NewerFormatError struct{ Version int }

func (e *NewerFormatError) Error() string {
	return fmt.Sprintf("This file was made by a newer version of Mediarium (file format %d; this one reads up to format %d). Update Mediarium, then check the file again.", e.Version, FormatVersion)
}

// Section is one block of the file and the API address that holds it.
type Section struct {
	Name string
	Path string
}

// Sections are the blocks a file may have, in the order they are applied.
var Sections = []Section{
	{"settings", "/api/settings"},
	{"downloadClient", "/api/download-client"},
	{"debrid", "/api/debrid"},
	{"proxy", "/api/proxy"},
	{"systemOptions", "/api/system/options"},
}

func sectionNames() string {
	var names []string
	for _, s := range Sections {
		names = append(names, s.Name)
	}
	for _, l := range Lists {
		names = append(names, l.Name)
	}
	return strings.Join(names, ", ")
}

// Client talks to a running Mediarium as an administrator.
type Client struct {
	BaseURL string // for example http://127.0.0.1:8264
	Key     string // an administrator's API key (Profile page); empty when HTTP already carries a session
	HTTP    *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) do(ctx context.Context, method, path string, body any) (map[string]any, error) {
	data, err := c.doRaw(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("Mediarium's answer for %s wasn't what was expected", path)
		}
	}
	return out, nil
}

func (c *Client) doRaw(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return nil, fmt.Errorf("bad Mediarium address %q: %w", c.BaseURL, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Key != "" {
		req.Header.Set("X-API-Key", c.Key)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach Mediarium at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		msg := ""
		if json.Unmarshal(data, &e) == nil {
			msg = e.Error
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			if msg == "" {
				msg = "Mediarium refused the request"
			}
			return nil, fmt.Errorf("%s (use an administrator's API key, made on the Profile page)", strings.TrimRight(msg, ". "))
		}
		if msg != "" {
			return nil, errors.New(msg)
		}
		return nil, fmt.Errorf("Mediarium answered HTTP %d", resp.StatusCode)
	}
	return data, nil
}

// derived are values a page shows but that are worked out, not chosen: they
// are left out of an export (and ignored by Mediarium if a file has them).
var derived = regexp.MustCompile(`^(has[A-Z].*|.*BuiltIn|.*UsingOwnKey|canCheck|legalAcknowledgedAt|onboardingDone|containerFolders)$`)

// Export reads every block from the running Mediarium and writes the file.
func Export(ctx context.Context, c *Client) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# Mediarium settings. Apply with: mediarium config apply <this file>, or on Settings > System.\n")
	buf.WriteString("# Keep only the lines you want to control: anything not here is left alone.\n")
	buf.WriteString("# Passwords and keys are never exported: <secret> means one is saved. Write the real one, or ${NAME} to read it from the environment.\n\n")
	fmt.Fprintf(&buf, "formatVersion: %d\n\n", FormatVersion)
	for _, s := range Sections {
		cur, err := c.do(ctx, http.MethodGet, s.Path, nil)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", s.Name, err)
		}
		for k := range writeOnly[s.Name] {
			if on, _ := cur[secretFlag(s.Name, k)].(bool); on {
				cur[k] = SecretPlaceholder
			}
		}
		for k := range cur {
			if derived.MatchString(k) {
				delete(cur, k)
			}
		}
		var node yaml.Node
		if err := node.Encode(map[string]any{s.Name: cur}); err != nil {
			return nil, err
		}
		out, err := yaml.Marshal(&node)
		if err != nil {
			return nil, err
		}
		buf.Write(out)
		buf.WriteString("\n")
	}
	for _, ls := range Lists {
		items, err := currentList(ctx, c, ls)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", ls.Name, err)
		}
		if len(items) == 0 {
			continue
		}
		if ls.Name == "qualityProfiles" {
			// Other profiles and custom formats by name: their numbers mean
			// nothing on another install.
			refs, err := loadProfileRefs(ctx, c, items)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", ls.Name, err)
			}
			for i := range items {
				items[i] = refs.toNames(items[i])
			}
		}
		out := make([]map[string]any, 0, len(items))
		for _, it := range items {
			addListPlaceholders(ls, it)
			for k := range it {
				if listDerived.MatchString(k) {
					delete(it, k)
				}
			}
			out = append(out, it)
		}
		sort.SliceStable(out, func(i, j int) bool {
			a, _ := out[i]["name"].(string)
			b, _ := out[j]["name"].(string)
			return strings.ToLower(a) < strings.ToLower(b)
		})
		var node yaml.Node
		if err := node.Encode(map[string]any{ls.Name: out}); err != nil {
			return nil, err
		}
		b, err := yaml.Marshal(&node)
		if err != nil {
			return nil, err
		}
		buf.Write(b)
		buf.WriteString("\n")
	}
	return buf.Bytes(), nil
}

// secretFlag is the name of the value a page shows to say a secret is saved.
func secretFlag(section, key string) string {
	if section == "debrid" && key == "apiKey" {
		return "hasKey"
	}
	return "has" + strings.ToUpper(key[:1]) + key[1:]
}

// addListPlaceholders writes <secret> for each password or key an item has
// saved, and drops what a file doesn't take back.
func addListPlaceholders(ls ListSection, it map[string]any) {
	for _, k := range ls.Secrets {
		if on, _ := it[secretFlag(ls.Name, k)].(bool); on {
			it[k] = SecretPlaceholder
		}
	}
	if ls.Name != "notifications" {
		return
	}
	cfg, _ := it["config"].(map[string]any)
	if cfg == nil {
		cfg = map[string]any{}
	}
	if saved, ok := it["hasSecrets"].(map[string]any); ok {
		for k, v := range saved {
			if on, _ := v.(bool); on {
				cfg[k] = SecretPlaceholder
			}
		}
	}
	it["config"] = cfg
	for k := range it {
		if !ls.has(ls.Fields, k) {
			delete(it, k)
		}
	}
}

// takePlaceholders removes every <secret> from m (and the maps in it) and
// returns where they were, as paths like config.password.
func takePlaceholders(m map[string]any, prefix string) []string {
	var out []string
	for k, v := range m {
		switch t := v.(type) {
		case string:
			if t == SecretPlaceholder {
				delete(m, k)
				out = append(out, prefix+k)
			}
		case map[string]any:
			out = append(out, takePlaceholders(t, prefix+k+".")...)
		}
	}
	sort.Strings(out)
	return out
}

// secretKey is how File.Secrets finds the <secret> lines of one list item.
func secretKey(list, name string) string {
	return list + "/" + strings.ToLower(strings.TrimSpace(name))
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expand replaces ${NAME} and ${NAME:-default} in every text value.
func expand(v any, env func(string) (string, bool), where string) (any, error) {
	switch t := v.(type) {
	case string:
		var missing string
		out := envRef.ReplaceAllStringFunc(t, func(m string) string {
			parts := envRef.FindStringSubmatch(m)
			if val, ok := env(parts[1]); ok && val != "" {
				return val
			}
			if strings.Contains(m, ":-") {
				return parts[2]
			}
			missing = parts[1]
			return ""
		})
		if missing != "" {
			return nil, fmt.Errorf("%s uses ${%s}, but %s isn't set", where, missing, missing)
		}
		return out, nil
	case map[string]any:
		for k, x := range t {
			nx, err := expand(x, env, where+"."+k)
			if err != nil {
				return nil, err
			}
			t[k] = nx
		}
		return t, nil
	case []any:
		for i, x := range t {
			nx, err := expand(x, env, fmt.Sprintf("%s[%d]", where, i))
			if err != nil {
				return nil, err
			}
			t[i] = nx
		}
		return t, nil
	}
	return v, nil
}

// File is a parsed settings file: the blocks (name -> its values) and the
// lists (name -> its items).
type File struct {
	Version int // the file format it says it has; 0 when it doesn't say
	Blocks  map[string]map[string]any
	Lists   map[string][]map[string]any
	// Secrets holds where the file has <secret>, per list item (see
	// secretKey): a password the file doesn't carry.
	Secrets map[string][]string
}

// Parse reads a file. env looks up ${NAME}; pass os.LookupEnv.
func Parse(data []byte, env func(string) (string, bool)) (File, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return File{}, fmt.Errorf("the file isn't valid YAML: %w", err)
	}
	known := map[string]bool{}
	for _, s := range Sections {
		known[s.Name] = true
	}
	out := File{Blocks: map[string]map[string]any{}, Lists: map[string][]map[string]any{}, Secrets: map[string][]string{}}
	if v, said := raw["formatVersion"]; said {
		delete(raw, "formatVersion")
		n, ok := v.(int)
		if !ok || n < 1 {
			return File{}, fmt.Errorf("formatVersion must be a whole number, 1 or more")
		}
		if n > FormatVersion {
			return File{}, &NewerFormatError{Version: n}
		}
		out.Version = n
	}
	for name, v := range raw {
		if ls, ok := listByName(name); ok {
			if v == nil {
				continue
			}
			items, err := parseList(ls, v, env, out.Secrets)
			if err != nil {
				return File{}, err
			}
			out.Lists[name] = items
			continue
		}
		if !known[name] {
			return File{}, fmt.Errorf("%q isn't a block Mediarium knows. The blocks are: %s", name, sectionNames())
		}
		if v == nil {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			return File{}, fmt.Errorf("%q must be a list of name: value lines", name)
		}
		ex, err := expand(m, env, name)
		if err != nil {
			return File{}, err
		}
		block := ex.(map[string]any)
		takePlaceholders(block, "")
		out.Blocks[name] = block
	}
	return out, nil
}

// ReadFile reads and parses a file from disk, taking ${NAME} from the environment.
func ReadFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	return Parse(data, os.LookupEnv)
}

// Change is one value that differs.
type Change struct {
	Section string `json:"section"`
	Key     string `json:"key,omitempty"`
	Have    string `json:"have,omitempty"` // "" for a value Mediarium doesn't show back (a password, a key)
	Want    string `json:"want,omitempty"`
	Secret  bool   `json:"secret,omitempty"`  // not shown back by Mediarium, so it is always sent
	New     bool   `json:"new,omitempty"`     // a list item Mediarium doesn't have yet
	Removed bool   `json:"removed,omitempty"` // a list item the file doesn't have (replace mode)
}

func (c Change) String() string {
	if c.New {
		return fmt.Sprintf("%s: will be added", c.Section)
	}
	if c.Removed {
		return fmt.Sprintf("%s: will be removed", c.Section)
	}
	if c.Secret {
		return fmt.Sprintf("%s.%s: (secret) will be set", c.Section, c.Key)
	}
	return fmt.Sprintf("%s.%s: %s -> %s", c.Section, c.Key, c.Have, c.Want)
}

// same compares a value from the file with one from Mediarium, ignoring that
// YAML has ints where JSON has floats.
func same(a, b any) bool {
	na, nb := normal(a), normal(b)
	return reflect.DeepEqual(na, nb)
}

func normal(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func show(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// Result is what happened to one block.
type Result struct {
	Section string
	Changes []Change
	Err     error
	Applied bool
}

// ProblemsError is a file with names or values Mediarium can't use. Nothing
// is written for it.
type ProblemsError struct {
	Unknown []string // names that aren't settings
	Invalid []string // values that can't be used, each a sentence
}

func (e *ProblemsError) Error() string {
	var parts []string
	if len(e.Unknown) > 0 {
		parts = append(parts, "these aren't settings Mediarium has (check the spelling; nothing was changed): "+strings.Join(e.Unknown, ", "))
	}
	if len(e.Invalid) > 0 {
		parts = append(parts, "these values can't be used (nothing was changed): "+strings.Join(e.Invalid, "; "))
	}
	return strings.Join(parts, ". ")
}

// Problems lists what is wrong, one plain sentence each.
func (e *ProblemsError) Problems() []string {
	var out []string
	for _, u := range e.Unknown {
		out = append(out, u+" isn't something Mediarium has. Check the spelling.")
	}
	return append(out, e.Invalid...)
}

type blockPlan struct {
	Result
	path    string
	cur     map[string]any
	body    map[string]any
	secrets int
}

// plan is the file compared with the running Mediarium: what would change,
// and the calls that would change it.
type plan struct {
	blocks         []*blockPlan
	blockUnchanged []string
	lists          *listPlan
}

// Plan compares the file with the running Mediarium without changing anything.
func Plan(ctx context.Context, c *Client, f File) ([]Result, error) {
	p, err := build(ctx, c, f, Options{})
	if err != nil {
		return nil, err
	}
	return p.results(), nil
}

// Apply makes the running Mediarium match the file. Blocks are applied one by
// one; one that Mediarium refuses is reported and the rest still go through.
// (ApplyAll is the all-or-nothing way, used by the Settings page.)
func Apply(ctx context.Context, c *Client, f File) ([]Result, error) {
	p, err := build(ctx, c, f, Options{})
	if err != nil {
		return nil, err
	}
	for _, b := range p.blocks {
		if b.Err != nil || len(b.Changes) == 0 {
			continue
		}
		if _, err := c.do(ctx, http.MethodPut, b.path, b.body); err != nil {
			b.Err = fmt.Errorf("%s: %w", b.Section, err)
		} else {
			b.Applied = true
		}
	}
	results := p.blockResults()
	for _, lr := range p.lists.results {
		res := lr.Result
		for _, act := range lr.actions {
			if _, err := act(ctx, c); err != nil {
				res.Err = fmt.Errorf("%s: %w", res.Section, err)
				break
			}
		}
		res.Applied = res.Err == nil && len(res.Changes) > 0
		results = append(results, res)
	}
	return results, nil
}

func (p *plan) blockResults() []Result {
	var out []Result
	for _, b := range p.blocks {
		out = append(out, b.Result)
	}
	return out
}

func (p *plan) results() []Result {
	out := p.blockResults()
	for _, lr := range p.lists.results {
		out = append(out, lr.Result)
	}
	for _, lr := range p.lists.removals {
		out = append(out, lr.Result)
	}
	return out
}

// writeOnly are the values a page accepts but never shows back, per block:
// passwords, keys and the like. A name that is neither shown nor in this list
// is a typo, and the file is refused.
var writeOnly = map[string]map[string]bool{
	"settings": {
		"tmdbApiKey": true, "openSubtitlesApiKey": true, "subdlApiKey": true,
		"openSubtitlesUsername": true, "openSubtitlesPassword": true, "traktClientId": true, "simklClientId": true,
	},
	"downloadClient": {"password": true},
	"debrid":         {"apiKey": true},
	"proxy":          {"password": true},
}

// build compares the file with the running Mediarium.
func build(ctx context.Context, c *Client, f File, opts Options) (*plan, error) {
	p := &plan{}
	var unknown, invalid []string
	for _, s := range Sections {
		want, ok := f.Blocks[s.Name]
		if !ok || len(want) == 0 {
			continue
		}
		res := Result{Section: s.Name}
		cur, err := c.do(ctx, http.MethodGet, s.Path, nil)
		if err != nil {
			res.Err = fmt.Errorf("read %s: %w", s.Name, err)
			p.blocks = append(p.blocks, &blockPlan{Result: res})
			continue
		}
		body := map[string]any{}
		for k, v := range cur {
			body[k] = v
		}
		secrets := 0
		for _, k := range sortedKeys(want) {
			have, shown := cur[k]
			if !shown {
				if !writeOnly[s.Name][k] {
					unknown = append(unknown, s.Name+"."+k)
					continue
				}
				res.Changes = append(res.Changes, Change{Section: s.Name, Key: k, Secret: true})
				secrets++
				body[k] = want[k]
				continue
			}
			if probs := valueProblem(s.Name+"."+k, k, have, want[k]); probs != "" {
				invalid = append(invalid, probs)
				continue
			}
			if !same(have, want[k]) {
				res.Changes = append(res.Changes, Change{Section: s.Name, Key: k, Have: show(have), Want: show(want[k])})
				body[k] = want[k]
			}
		}
		p.blocks = append(p.blocks, &blockPlan{Result: res, path: s.Path, cur: cur, body: body, secrets: secrets})
		if len(res.Changes) == 0 {
			p.blockUnchanged = append(p.blockUnchanged, s.Name)
		}
	}
	lp, err := planLists(ctx, c, f, opts)
	if err != nil {
		return nil, err
	}
	unknown = append(unknown, lp.unknown...)
	invalid = append(invalid, lp.invalid...)
	if len(unknown)+len(invalid) > 0 {
		return nil, &ProblemsError{Unknown: unknown, Invalid: invalid}
	}
	p.lists = lp
	return p, nil
}
