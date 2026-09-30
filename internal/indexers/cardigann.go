package indexers

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Definition is a Cardigann YAML indexer definition, as maintained by the
// community in the Prowlarr/Indexers repository (schema v11). The engine in
// cardigann_*.go runs it: logging in, searching, scraping the result rows
// and resolving download links.
type Definition struct {
	ID              string   `yaml:"id"`
	Replaces        []string `yaml:"replaces"`
	Name            string   `yaml:"name"`
	Description     string   `yaml:"description"`
	Type            string   `yaml:"type"` // public|semi-private|private
	Language        string   `yaml:"language"`
	Encoding        string   `yaml:"encoding"`
	RequestDelay    float64  `yaml:"requestDelay"`
	Links           []string `yaml:"links"`
	LegacyLinks     []string `yaml:"legacylinks"`
	FollowRedirect  bool     `yaml:"followredirect"`
	TestLinkTorrent *bool    `yaml:"testlinktorrent"`
	// Protocol is not part of the upstream schema (every upstream
	// definition is a torrent site); "usenet" is honoured if present.
	Protocol string `yaml:"protocol"`

	Caps     CapsBlock      `yaml:"caps"`
	Settings []SettingField `yaml:"settings"`
	Login    *LoginBlock    `yaml:"login"`
	Search   SearchBlock    `yaml:"search"`
	Download *DownloadBlock `yaml:"download"`
}

// CapsBlock describes the site's categories and search modes.
type CapsBlock struct {
	Categories       map[string]string `yaml:"categories"` // site id -> Newznab category name
	CategoryMappings []CategoryMapping `yaml:"categorymappings"`
	Modes            map[string][]string
	AllowRawSearch   bool `yaml:"allowrawsearch"`
}

// CategoryMapping maps one site category to a Newznab category.
type CategoryMapping struct {
	ID      string `yaml:"id"`
	Cat     string `yaml:"cat"`
	Desc    string `yaml:"desc"`
	Default bool   `yaml:"default"`
}

// SettingField is one user setting the definition asks for.
type SettingField struct {
	Name     string             `yaml:"name"`
	Type     string             `yaml:"type"`
	Label    string             `yaml:"label"`
	Default  string             `yaml:"default"`
	Defaults []string           `yaml:"defaults"`
	Options  OrderedMap[string] `yaml:"options"`
}

// LoginBlock describes how to sign in to a private site.
type LoginBlock struct {
	Path              string                    `yaml:"path"`
	SubmitPath        string                    `yaml:"submitpath"`
	Cookies           []string                  `yaml:"cookies"`
	Method            string                    `yaml:"method"`
	Form              string                    `yaml:"form"`
	Selectors         bool                      `yaml:"selectors"`
	Inputs            OrderedMap[string]        `yaml:"inputs"`
	SelectorInputs    OrderedMap[SelectorField] `yaml:"selectorinputs"`
	GetSelectorInputs OrderedMap[SelectorField] `yaml:"getselectorinputs"`
	Error             []ErrorBlock              `yaml:"error"`
	Test              *PageTestBlock            `yaml:"test"`
	Captcha           *CaptchaBlock             `yaml:"captcha"`
	Headers           map[string][]string       `yaml:"headers"`
}

// CaptchaBlock marks a login form that shows a CAPTCHA.
type CaptchaBlock struct {
	Type     string `yaml:"type"`
	Selector string `yaml:"selector"`
	Input    string `yaml:"input"`
}

// ErrorBlock detects an error message on a page.
type ErrorBlock struct {
	Path     string         `yaml:"path"`
	Selector string         `yaml:"selector"`
	Message  *SelectorField `yaml:"message"`
}

// PageTestBlock checks that a page shows we are logged in.
type PageTestBlock struct {
	Path     string `yaml:"path"`
	Selector string `yaml:"selector"`
}

// SearchBlock describes the search requests and how to read the results.
type SearchBlock struct {
	Path                 string                    `yaml:"path"`
	Paths                []SearchPath              `yaml:"paths"`
	Headers              map[string][]string       `yaml:"headers"`
	KeywordsFilters      []FilterBlock             `yaml:"keywordsfilters"`
	PreprocessingFilters []FilterBlock             `yaml:"preprocessingfilters"`
	AllowEmptyInputs     bool                      `yaml:"allowEmptyInputs"`
	Inputs               OrderedMap[string]        `yaml:"inputs"`
	Error                []ErrorBlock              `yaml:"error"`
	Rows                 RowsBlock                 `yaml:"rows"`
	Fields               OrderedMap[SelectorField] `yaml:"fields"`
}

// SearchPath is one request made for a search.
type SearchPath struct {
	Path           string             `yaml:"path"`
	Method         string             `yaml:"method"`
	Inputs         OrderedMap[string] `yaml:"inputs"`
	InheritInputs  *bool              `yaml:"inheritinputs"`
	Categories     []string           `yaml:"categories"`
	FollowRedirect bool               `yaml:"followredirect"`
	Response       *ResponseBlock     `yaml:"response"`
}

// ResponseBlock declares the response format of a search path.
type ResponseBlock struct {
	Type             string `yaml:"type"` // html (default) or json
	NoResultsMessage string `yaml:"noResultsMessage"`
}

// RowsBlock selects the result rows.
type RowsBlock struct {
	Selector                        string         `yaml:"selector"`
	Attribute                       string         `yaml:"attribute"`
	Remove                          string         `yaml:"remove"`
	After                           int            `yaml:"after"`
	DateHeaders                     *SelectorField `yaml:"dateheaders"`
	Count                           *SelectorField `yaml:"count"`
	Multiple                        bool           `yaml:"multiple"`
	MissingAttributeEqualsNoResults bool           `yaml:"missingAttributeEqualsNoResults"`
	Filters                         []FilterBlock  `yaml:"filters"`
}

// SelectorField reads one value from a page (or JSON object).
type SelectorField struct {
	Selector  string             `yaml:"selector"`
	Optional  bool               `yaml:"optional"`
	Default   *string            `yaml:"default"`
	Text      *string            `yaml:"text"`
	Attribute string             `yaml:"attribute"`
	Remove    string             `yaml:"remove"`
	Filters   []FilterBlock      `yaml:"filters"`
	Case      OrderedMap[string] `yaml:"case"`
}

// DownloadBlock describes how to turn a result link into a download.
type DownloadBlock struct {
	Selectors []DownloadSelector  `yaml:"selectors"`
	Method    string              `yaml:"method"`
	Before    *BeforeBlock        `yaml:"before"`
	InfoHash  *InfoHashBlock      `yaml:"infohash"`
	Headers   map[string][]string `yaml:"headers"`
}

// DownloadSelector finds the real download link on a details page.
type DownloadSelector struct {
	Selector          string        `yaml:"selector"`
	Attribute         string        `yaml:"attribute"`
	UseBeforeResponse bool          `yaml:"usebeforeresponse"`
	Filters           []FilterBlock `yaml:"filters"`
}

// BeforeBlock is a request made before downloading.
type BeforeBlock struct {
	Path         string             `yaml:"path"`
	Method       string             `yaml:"method"`
	Inputs       OrderedMap[string] `yaml:"inputs"`
	PathSelector *SelectorField     `yaml:"pathselector"`
}

// InfoHashBlock builds a magnet link from an info hash on a page.
type InfoHashBlock struct {
	Hash              SelectorField `yaml:"hash"`
	Title             SelectorField `yaml:"title"`
	UseBeforeResponse bool          `yaml:"usebeforeresponse"`
}

// OrderedMap keeps a YAML mapping's key order, which matters for fields
// (later fields read earlier ones), case blocks (first match wins) and
// select options (display order).
type OrderedMap[V any] struct {
	Keys   []string
	Values map[string]V
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (m *OrderedMap[V]) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a mapping", n.Line)
	}
	m.Values = make(map[string]V, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		var v V
		if err := n.Content[i+1].Decode(&v); err != nil {
			return fmt.Errorf("key %q: %w", k, err)
		}
		if _, dup := m.Values[k]; !dup {
			m.Keys = append(m.Keys, k)
		}
		m.Values[k] = v
	}
	return nil
}

// Len returns the number of entries.
func (m OrderedMap[V]) Len() int { return len(m.Keys) }

// slashEscapeRe finds a "\/" escape (not preceded by an escaped backslash).
var slashEscapeRe = regexp.MustCompile(`(^|[^\\])((?:\\\\)*)\\/`)

// ErrUnsupportedDefinition wraps every reason a definition can't be run.
var ErrUnsupportedDefinition = errors.New("unsupported indexer definition")

// ParseDefinition reads a Cardigann YAML indexer definition. It only checks
// that the YAML is well formed and has an id; Validate checks the engine
// can actually run it.
func ParseDefinition(data []byte) (*Definition, error) {
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		// YAML 1.2 allows "\/" in double-quoted strings (as JSON does);
		// yaml.v3 doesn't. Retry with those escapes resolved.
		if !strings.Contains(err.Error(), "unknown escape character") {
			return nil, fmt.Errorf("parse cardigann definition: %w", err)
		}
		def = Definition{}
		if err2 := yaml.Unmarshal(slashEscapeRe.ReplaceAll(data, []byte("$1$2/")), &def); err2 != nil {
			return nil, fmt.Errorf("parse cardigann definition: %w", err)
		}
	}
	if def.ID == "" {
		return nil, fmt.Errorf("cardigann definition missing required 'id' field")
	}
	if def.Search.Path != "" && len(def.Search.Paths) == 0 {
		def.Search.Paths = []SearchPath{{Path: def.Search.Path}}
	}
	return &def, nil
}

// DefinitionProtocol reports whether the site serves torrents or NZBs.
func (d *Definition) DefinitionProtocol() Protocol {
	if strings.EqualFold(d.Protocol, "usenet") || strings.EqualFold(d.Protocol, "nzb") {
		return ProtocolUsenet
	}
	return ProtocolTorrent
}

// jsonSearch reports whether the search results are JSON.
func (d *Definition) jsonSearch() bool {
	for _, p := range d.Search.Paths {
		if p.Response != nil && strings.EqualFold(p.Response.Type, "json") {
			return true
		}
	}
	return false
}

var knownSettingTypes = map[string]bool{
	"text": true, "password": true, "checkbox": true, "select": true, "multi-select": true,
	"info": true, "info_cookie": true, "info_flaresolverr": true, "info_useragent": true,
	"info_category_8000": true,
}

// Validate checks that the engine supports everything the definition uses,
// so an unsupported site fails once, clearly, instead of misbehaving on
// every search. Templated selectors and patterns are checked when they are
// used.
func (d *Definition) Validate() error {
	if p := d.Problems(); len(p) > 0 {
		return fmt.Errorf("%w %q: %s", ErrUnsupportedDefinition, d.ID, strings.Join(p, "; "))
	}
	return nil
}

// Problems lists what the engine can't run in this definition (at most a
// handful), or nothing.
func (d *Definition) Problems() []string {
	v := &defValidator{def: d}
	v.run()
	return v.problems
}

type defValidator struct {
	def      *Definition
	problems []string
}

func (v *defValidator) add(format string, args ...any) {
	if len(v.problems) < 5 {
		v.problems = append(v.problems, fmt.Sprintf(format, args...))
	}
}

func (v *defValidator) run() {
	d := v.def
	if len(d.Links) == 0 {
		v.add("no site links")
	}
	for _, s := range d.Settings {
		if !knownSettingTypes[s.Type] {
			v.add("setting %q has unsupported type %q", s.Name, s.Type)
		}
	}
	if d.Encoding != "" {
		if _, _, err := lookupEncoding(d.Encoding); err != nil {
			v.add("%v", err)
		}
	}
	if l := d.Login; l != nil {
		switch strings.ToLower(l.Method) {
		case "", "post", "form", "cookie", "get":
		default:
			v.add("login method %q is not supported", l.Method)
		}
		v.template("login path", l.Path)
		v.template("login submit path", l.SubmitPath)
		v.css("login form", l.Form)
		for _, k := range l.Inputs.Keys {
			v.template("login input "+k, l.Inputs.Values[k])
		}
		for _, k := range l.SelectorInputs.Keys {
			v.field("login selector input "+k, l.SelectorInputs.Values[k], false)
		}
		for _, k := range l.GetSelectorInputs.Keys {
			v.field("login selector input "+k, l.GetSelectorInputs.Values[k], false)
		}
		for _, e := range l.Error {
			v.css("login error", e.Selector)
			if e.Message != nil {
				v.field("login error message", *e.Message, false)
			}
		}
		if l.Test != nil {
			v.template("login test path", l.Test.Path)
			v.css("login test", l.Test.Selector)
		}
	}

	s := d.Search
	if len(s.Paths) == 0 {
		v.add("no search paths")
	}
	json := d.jsonSearch()
	for _, p := range s.Paths {
		v.template("search path", p.Path)
		if p.Response != nil {
			switch strings.ToLower(p.Response.Type) {
			case "", "html", "json", "xml":
			default:
				v.add("search response type %q is not supported", p.Response.Type)
			}
		}
		if strings.Contains(p.Method, "{{") {
			v.template("search method", p.Method)
		} else {
			switch strings.ToLower(p.Method) {
			case "", "get", "post":
			default:
				v.add("search method %q is not supported", p.Method)
			}
		}
		for _, k := range p.Inputs.Keys {
			v.template("search input "+k, p.Inputs.Values[k])
		}
	}
	for _, k := range s.Inputs.Keys {
		v.template("search input "+k, s.Inputs.Values[k])
	}
	for _, vals := range s.Headers {
		for _, h := range vals {
			v.template("search header", h)
		}
	}
	v.filters("keywords", s.KeywordsFilters)
	v.filters("preprocessing", s.PreprocessingFilters)
	for _, e := range s.Error {
		v.css("search error", e.Selector)
	}
	if s.Rows.Selector == "" {
		v.add("no rows selector")
	} else if json {
		v.jsonSel("rows", s.Rows.Selector)
	} else {
		v.css("rows", s.Rows.Selector)
		v.css("rows remove", s.Rows.Remove)
	}
	for _, f := range s.Rows.Filters {
		if f.Name != "andmatch" && f.Name != "strdump" {
			v.add("row filter %q is not supported", f.Name)
		}
	}
	if s.Rows.DateHeaders != nil {
		v.field("date headers", *s.Rows.DateHeaders, json)
	}
	hasTitle := false
	for _, k := range s.Fields.Keys {
		name := strings.SplitN(k, "|", 2)[0]
		if name == "title" {
			hasTitle = true
		}
		v.field("field "+k, s.Fields.Values[k], json)
	}
	if !hasTitle {
		v.add("no title field")
	}
	if dl := d.Download; dl != nil {
		for _, sel := range dl.Selectors {
			v.css("download selector", sel.Selector)
			v.filters("download selector", sel.Filters)
		}
		if dl.Before != nil {
			v.template("download before path", dl.Before.Path)
			if dl.Before.PathSelector != nil {
				v.field("download before path selector", *dl.Before.PathSelector, false)
			}
		}
		if dl.InfoHash != nil {
			v.field("download infohash", dl.InfoHash.Hash, false)
			v.field("download infohash title", dl.InfoHash.Title, false)
		}
		switch strings.ToLower(dl.Method) {
		case "", "get", "post":
		default:
			v.add("download method %q is not supported", dl.Method)
		}
	}
}

func (v *defValidator) template(what, src string) {
	if err := checkTemplate(src); err != nil {
		v.add("%s: %v", what, err)
	}
}

func (v *defValidator) css(what, sel string) {
	if sel == "" {
		return
	}
	if strings.Contains(sel, "{{") {
		v.template(what, sel)
		return
	}
	if _, err := CompileSelector(sel); err != nil {
		v.add("%s: %v", what, err)
	}
}

func (v *defValidator) jsonSel(what, sel string) {
	if sel == "" {
		return
	}
	if strings.Contains(sel, "{{") {
		v.template(what, sel)
		return
	}
	if _, _, err := parseJSONSelector(strings.TrimLeft(sel, ".")); err != nil {
		v.add("%s: %v", what, err)
	}
}

func (v *defValidator) field(what string, f SelectorField, json bool) {
	if f.Text != nil {
		v.template(what+" text", *f.Text)
	} else if json {
		v.jsonSel(what, f.Selector)
	} else {
		v.css(what, f.Selector)
	}
	if !json {
		v.css(what+" remove", f.Remove)
		for _, k := range f.Case.Keys {
			v.css(what+" case", k)
		}
	}
	for _, k := range f.Case.Keys {
		v.template(what+" case value", f.Case.Values[k])
	}
	if f.Default != nil {
		v.template(what+" default", *f.Default)
	}
	v.filters(what, f.Filters)
}

func (v *defValidator) filters(what string, fs []FilterBlock) {
	for _, f := range fs {
		if !knownFilters[f.Name] {
			v.add("%s: filter %q is not supported", what, f.Name)
			continue
		}
		args := filterArgs(f.Args)
		switch f.Name {
		case "regexp": // (re_replace falls back to leaving the text unchanged)
			if p := argAt(args, 0); !strings.Contains(p, "{{") {
				if _, err := compileDotNetRegex(p); err != nil {
					v.add("%s: %v", what, err)
				}
			}
		case "diacritics":
			if argAt(args, 0) != "replace" {
				v.add("%s: diacritics mode %q is not supported", what, argAt(args, 0))
			}
		}
	}
}
