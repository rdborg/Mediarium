package quality

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/dlclark/regexp2"

	"github.com/rdborg/mediarium/internal/parser"
)

// Custom formats, the way Radarr and Sonarr have them: a named set of
// conditions on a release ("is Dolby Vision", "comes from this group", "is
// a season pack"), and a profile gives each format a score. A release's
// format score is the sum of the scores of the formats it matches; profiles
// use it to refuse releases below a minimum, to rank releases of the same
// quality, and to look for better-scoring releases until a cutoff.
//
// Formats are imported as they are written for Radarr and Sonarr, so the
// TRaSH Guides ones work unchanged: their patterns are .NET regular
// expressions (look-arounds and all), run here with regexp2.

// FormatApp says whose numbering a format's source, language and modifier
// values use: "radarr" or "sonarr".
type FormatApp string

const (
	AppRadarr FormatApp = "radarr"
	AppSonarr FormatApp = "sonarr"
)

// The condition kinds, by their Radarr/Sonarr names.
const (
	SpecReleaseTitle    = "ReleaseTitleSpecification"
	SpecReleaseGroup    = "ReleaseGroupSpecification"
	SpecEdition         = "EditionSpecification"
	SpecSource          = "SourceSpecification"
	SpecResolution      = "ResolutionSpecification"
	SpecQualityModifier = "QualityModifierSpecification"
	SpecLanguage        = "LanguageSpecification"
	SpecReleaseType     = "ReleaseTypeSpecification"
	SpecSize            = "SizeSpecification"
	SpecIndexerFlag     = "IndexerFlagSpecification"
)

// FormatSpec is one condition of a custom format.
type FormatSpec struct {
	Name     string  `json:"name"`
	Kind     string  `json:"implementation"`
	Negate   bool    `json:"negate"`
	Required bool    `json:"required"`
	Value    string  `json:"value,omitempty"` // a pattern, or a number as text
	Min      float64 `json:"min,omitempty"`   // SizeSpecification, in GB
	Max      float64 `json:"max,omitempty"`
	Except   bool    `json:"exceptLanguage,omitempty"`
}

// CustomFormat is a named set of conditions.
type CustomFormat struct {
	ID      int64        `json:"id"`
	Name    string       `json:"name"`
	App     FormatApp    `json:"app"`
	TrashID string       `json:"trashId,omitempty"`
	Specs   []FormatSpec `json:"specifications"`
	// IncludeInName is Radarr's and Sonarr's "include custom format when
	// renaming" switch. It is kept so it can be read back; it changes nothing
	// here.
	IncludeInName bool `json:"includeInName,omitempty"`
	// TrashScore is the score the TRaSH Guides gave the format when it was last
	// read from them; nil for a format that was pasted or made by hand, which
	// is never updated from the guides.
	TrashScore *int `json:"-"`

	compiled map[int]*regexp2.Regexp // by spec index, filled by Compile
	problems []string
}

// regexTimeout stops a badly written pattern from holding up a search.
const regexTimeout = 200 * time.Millisecond

var supportedSpecs = map[string]bool{
	SpecReleaseTitle: true, SpecReleaseGroup: true, SpecEdition: true, SpecSource: true, SpecResolution: true,
	SpecQualityModifier: true, SpecLanguage: true, SpecReleaseType: true, SpecSize: true, SpecIndexerFlag: true,
}

func isPatternSpec(kind string) bool {
	return kind == SpecReleaseTitle || kind == SpecReleaseGroup || kind == SpecEdition
}

// Compile prepares a format's patterns. A pattern that can't be read, or a
// condition kind Mediarium doesn't know, never matches; Problems says which.
func (f *CustomFormat) Compile() {
	f.compiled = map[int]*regexp2.Regexp{}
	f.problems = nil
	for i, s := range f.Specs {
		switch {
		case !supportedSpecs[s.Kind]:
			f.problems = append(f.problems, fmt.Sprintf("%q: Mediarium doesn't know the condition %q", s.Name, s.Kind))
		case s.Kind == SpecIndexerFlag:
			f.problems = append(f.problems, fmt.Sprintf("%q: indexer flags aren't known to Mediarium, so this condition never matches", s.Name))
		case isPatternSpec(s.Kind):
			re, err := regexp2.Compile(s.Value, regexp2.IgnoreCase)
			if err != nil {
				f.problems = append(f.problems, fmt.Sprintf("%q: the pattern can't be read: %v", s.Name, err))
				continue
			}
			re.MatchTimeout = regexTimeout
			f.compiled[i] = re
		}
	}
}

// Problems lists the conditions that can never match, in words.
func (f *CustomFormat) Problems() []string {
	if f.compiled == nil {
		f.Compile()
	}
	return f.problems
}

// FormatInput is what a release is judged on.
type FormatInput struct {
	Title     string
	Release   parser.Release
	SizeBytes int64 // 0 when unknown: size conditions then don't match
	// OriginalLanguage is the language the film or show was made in, as the
	// name a release uses ("Japanese"), or "" when it isn't known. A condition
	// on the original language never matches without it.
	OriginalLanguage string
}

// InputFor parses a release title for format matching.
func InputFor(title string) FormatInput {
	return FormatInput{Title: title, Release: parser.Parse(title)}
}

// Matches reports whether a release matches the format: for each kind of
// condition, every required one must hold and at least one must hold (the
// rule Radarr and Sonarr use). A format with no conditions matches nothing.
func (f *CustomFormat) Matches(in FormatInput) bool {
	if f.compiled == nil {
		f.Compile()
	}
	if len(f.Specs) == 0 {
		return false
	}
	type group struct{ any, requiredFailed bool }
	groups := map[string]*group{}
	order := []string{}
	for i, s := range f.Specs {
		g, ok := groups[s.Kind]
		if !ok {
			g = &group{}
			groups[s.Kind] = g
			order = append(order, s.Kind)
		}
		hit := f.specHolds(i, s, in) != s.Negate
		if s.Kind == SpecLanguage && specNumber(s) == languageOriginal && languageOf(in) == "" {
			hit = false // the title's own language isn't known, so "not original" can't be said either
		}
		if hit {
			g.any = true
		} else if s.Required {
			g.requiredFailed = true
		}
	}
	for _, k := range order {
		if g := groups[k]; !g.any || g.requiredFailed {
			return false
		}
	}
	return true
}

// languageOf is the title's original language of an input, "" when unknown.
func languageOf(in FormatInput) string { return in.OriginalLanguage }

func (f *CustomFormat) regexMatch(i int, s string) bool {
	re := f.compiled[i]
	if re == nil || s == "" {
		return false
	}
	ok, err := re.MatchString(s)
	return err == nil && ok
}

func (f *CustomFormat) specHolds(i int, s FormatSpec, in FormatInput) bool {
	r := in.Release
	switch s.Kind {
	case SpecReleaseTitle:
		return f.regexMatch(i, in.Title)
	case SpecReleaseGroup:
		return f.regexMatch(i, r.Group)
	case SpecEdition:
		return f.regexMatch(i, r.Edition)
	case SpecResolution:
		return specNumber(s) == resolutionNumber(r.Resolution)
	case SpecSource:
		return specNumber(s) == sourceNumber(f.App, r)
	case SpecQualityModifier:
		return specNumber(s) == qualityModifier(in)
	case SpecLanguage:
		return languageHolds(s, r, in.OriginalLanguage)
	case SpecReleaseType:
		return specNumber(s) == releaseType(r)
	case SpecSize:
		if in.SizeBytes <= 0 {
			return false
		}
		gb := float64(in.SizeBytes) / (1 << 30)
		return gb >= s.Min && (s.Max <= 0 || gb <= s.Max)
	}
	return false
}

func specNumber(s FormatSpec) int {
	var n float64
	if _, err := fmt.Sscan(strings.TrimSpace(s.Value), &n); err != nil {
		return math.MinInt32
	}
	return int(n)
}

func resolutionNumber(res string) int {
	switch strings.ToLower(res) {
	case "2160p", "4k":
		return 2160
	case "1080p":
		return 1080
	case "720p":
		return 720
	case "576p":
		return 576
	case "480p":
		return 480
	case "360p":
		return 360
	}
	return 0
}

// sourceNumber is the release's source in the format's own numbering.
// Radarr: 1 CAM, 2 TELESYNC, 3 TELECINE, 4 WORKPRINT, 5 DVD, 6 TV, 7 WEBDL,
// 8 WEBRIP, 9 BLURAY. Sonarr: 1 Television, 2 TelevisionRaw, 3 Web,
// 4 WebRip, 5 DVD, 6 Bluray, 7 BlurayRaw (a remux).
func sourceNumber(app FormatApp, r parser.Release) int {
	src := strings.ToUpper(r.Source)
	if app == AppSonarr {
		switch src {
		case "HDTV", "PDTV", "SDTV", "HDRIP":
			return 1
		case "WEB-DL", "WEB":
			return 3
		case "WEBRIP":
			return 4
		case "DVDRIP", "DVDR", "DVD":
			return 5
		case "BLURAY", "BDRIP", "BRRIP":
			return 6
		case "REMUX":
			return 7
		}
		return 0
	}
	switch src {
	case "CAM":
		return 1
	case "TELESYNC":
		return 2
	case "TELECINE":
		return 3
	case "WORKPRINT":
		return 4
	case "DVDRIP", "DVDR", "DVD":
		return 5
	case "HDTV", "PDTV", "SDTV", "HDRIP":
		return 6
	case "WEB-DL", "WEB":
		return 7
	case "WEBRIP":
		return 8
	case "BLURAY", "BDRIP", "BRRIP", "REMUX":
		return 9
	}
	return 0
}

var (
	brDiskRe   = regexp.MustCompile(`(?i)\b(BDMV|BR[-_. ]?DISK|COMPLETE[-_. ]+(UHD[-_. ]+)?BLU[-_. ]?RAY|BD(25|50|66|100)|AVC[-_. ]+REMUX)\b`)
	screenerRe = regexp.MustCompile(`(?i)\b(SCR|SCREENER|DVDSCR|BDSCR|WEBSCR)\b`)
	regionalRe = regexp.MustCompile(`(?i)\bR[56]\b`)
	rawHDRe    = regexp.MustCompile(`(?i)\b(RAW[-_. ]?HD|1080i)\b`)
)

// qualityModifier is Radarr's modifier: 0 none, 1 regional, 2 screener,
// 3 raw HD, 4 BR-DISK, 5 remux.
func qualityModifier(in FormatInput) int {
	switch {
	case strings.EqualFold(in.Release.Source, "Remux"):
		return 5
	case brDiskRe.MatchString(in.Title):
		return 4
	case rawHDRe.MatchString(in.Title):
		return 3
	case screenerRe.MatchString(in.Title):
		return 2
	case regionalRe.MatchString(in.Title):
		return 1
	}
	return 0
}

// releaseType is Sonarr's: 1 single episode, 2 several episodes, 3 a season
// pack; 0 for anything else.
func releaseType(r parser.Release) int {
	switch {
	case len(r.Episodes) > 1:
		return 2
	case len(r.Episodes) == 1:
		return 1
	case r.Season > 0 || r.Special:
		return 3
	}
	return 0
}

// formatLanguages are Radarr's and Sonarr's language numbers for the
// languages the release-name parser knows.
var formatLanguages = map[string]int{
	"English": 1, "French": 2, "Spanish": 3, "German": 4, "Italian": 5, "Danish": 6, "Dutch": 7, "Japanese": 8,
	"Chinese": 10, "Russian": 11, "Polish": 12, "Vietnamese": 13, "Swedish": 14, "Norwegian": 15, "Finnish": 16,
	"Turkish": 17, "Portuguese": 18, "Greek": 20, "Korean": 21, "Hungarian": 22, "Hebrew": 23, "Czech": 25,
	"Hindi": 26, "Romanian": 27, "Thai": 28, "Arabic": 31, "Ukrainian": 32, "Tamil": 43, "Telugu": 45,
}

const (
	languageAny      = -1
	languageOriginal = -2
)

// languageHolds checks a language condition. A release that names no
// language is taken to be English, as Radarr and Sonarr do. "Original" means
// the language the title was made in, which a release name can't tell, so it
// is taken from original (the title's own language); without it, it never
// matches.
func languageHolds(s FormatSpec, r parser.Release, original string) bool {
	want := specNumber(s)
	if want == languageOriginal {
		want = formatLanguages[original]
		if want == 0 {
			return false
		}
	}
	langs := r.Languages
	if len(langs) == 0 {
		langs = []string{"English"}
	}
	switch want {
	case languageAny:
		return true
	}
	has := false
	other := false
	for _, l := range langs {
		if formatLanguages[l] == want {
			has = true
		} else {
			other = true
		}
	}
	if s.Except {
		return other
	}
	return has
}

// --- reading formats as Radarr, Sonarr and TRaSH write them ---

// rawFormat is a format in Radarr/Sonarr/TRaSH JSON. fields is either an
// object ({"value": …}) as TRaSH and the export button write it, or a list
// ([{"name": "value", "value": …}]) as the API returns it.
type rawFormat struct {
	TrashID     string         `json:"trash_id"`
	TrashScores map[string]int `json:"trash_scores"`
	Name        string         `json:"name"`
	IncludeName bool           `json:"includeCustomFormatWhenRenaming"`
	Specs       []rawSpec      `json:"specifications"`
}

type rawSpec struct {
	Name           string          `json:"name"`
	Implementation string          `json:"implementation"`
	Negate         bool            `json:"negate"`
	Required       bool            `json:"required"`
	Fields         json.RawMessage `json:"fields"`
}

// ImportedFormat is a format read from JSON, with the default score TRaSH
// suggests for it (0 when it suggests none).
type ImportedFormat struct {
	Format       CustomFormat
	DefaultScore int
	HasScore     bool
	// Scores is every score TRaSH suggests, by score set ("default",
	// "sqp-1-1080p", "anime-radarr"…).
	Scores map[string]int
}

// ParseFormats reads one format, or a list of them, as Radarr, Sonarr or the
// TRaSH Guides write them. app is whose numbering they use; "" guesses from
// the conditions (a release-type condition only exists in Sonarr).
func ParseFormats(data []byte, app FormatApp) ([]ImportedFormat, error) {
	data = []byte(strings.TrimSpace(string(data)))
	var raws []rawFormat
	if len(data) > 0 && data[0] == '[' {
		if err := json.Unmarshal(data, &raws); err != nil {
			return nil, fmt.Errorf("that isn't a list of custom formats: %w", err)
		}
	} else {
		var one rawFormat
		if err := json.Unmarshal(data, &one); err != nil {
			return nil, fmt.Errorf("that isn't a custom format: %w", err)
		}
		raws = []rawFormat{one}
	}
	out := make([]ImportedFormat, 0, len(raws))
	for _, rf := range raws {
		f, err := rf.toFormat(app)
		if err != nil {
			return nil, err
		}
		score, has := rf.TrashScores["default"]
		out = append(out, ImportedFormat{Format: f, DefaultScore: score, HasScore: has, Scores: rf.TrashScores})
	}
	return out, nil
}

func (rf rawFormat) toFormat(app FormatApp) (CustomFormat, error) {
	name := strings.TrimSpace(rf.Name)
	if name == "" {
		return CustomFormat{}, errors.New("a custom format has no name")
	}
	if len(name) > 100 {
		return CustomFormat{}, fmt.Errorf("the custom format name %.40q… is too long", name)
	}
	if len(rf.Specs) == 0 {
		return CustomFormat{}, fmt.Errorf("the custom format %q has no conditions", name)
	}
	if len(rf.Specs) > 200 {
		return CustomFormat{}, fmt.Errorf("the custom format %q has too many conditions", name)
	}
	f := CustomFormat{Name: name, TrashID: strings.TrimSpace(rf.TrashID), App: app, IncludeInName: rf.IncludeName}
	for _, rs := range rf.Specs {
		if rs.Implementation == SpecReleaseType && f.App == "" {
			f.App = AppSonarr
		}
		s := FormatSpec{Name: rs.Name, Kind: rs.Implementation, Negate: rs.Negate, Required: rs.Required}
		fields, err := readFields(rs.Fields)
		if err != nil {
			return CustomFormat{}, fmt.Errorf("the custom format %q: condition %q: %w", name, rs.Name, err)
		}
		if v, ok := fields["value"]; ok {
			s.Value = fieldText(v)
		}
		if v, ok := fields["min"]; ok {
			fmt.Sscan(fieldText(v), &s.Min)
		}
		if v, ok := fields["max"]; ok {
			fmt.Sscan(fieldText(v), &s.Max)
		}
		if v, ok := fields["exceptLanguage"]; ok {
			s.Except = fieldText(v) == "true"
		}
		if len(s.Value) > 20000 {
			return CustomFormat{}, fmt.Errorf("the custom format %q: condition %q is too long", name, rs.Name)
		}
		f.Specs = append(f.Specs, s)
	}
	if f.App == "" {
		f.App = AppRadarr
	}
	f.Compile()
	return f, nil
}

func readFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if raw[0] == '{' {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	var list []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, errors.New("its fields can't be read")
	}
	for _, f := range list {
		out[f.Name] = f.Value
	}
	return out, nil
}

// fieldText is a JSON value as plain text: a string unquoted, a number or
// boolean as written.
func fieldText(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(v))
}
