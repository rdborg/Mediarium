package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
)

// Bazarr is read with GET /api/system/status (version), /api/system/languages
// (the enabled languages) and /api/system/settings (the enabled subtitle
// providers), the key in the X-API-KEY header. Its enabled languages replace
// Mediarium's subtitle languages, only when the owner includes them.
// Mediarium fetches subtitles from OpenSubtitles only, so every other
// provider is reported as having no equivalent.

type bazarrLanguage struct {
	Name    string `json:"name"`
	Code2   string `json:"code2"`
	Code3   string `json:"code3"`
	Enabled bool   `json:"enabled"`
}

type bazarrData struct {
	Version        string
	Languages      []bazarrLanguage // the enabled ones
	Providers      []string
	ProvidersError string
}

// unwrapData reads Bazarr answers that are either the value itself or
// {"data": value}.
func unwrapData(raw json.RawMessage, out any) error {
	var wrapped struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Data) > 0 {
		raw = wrapped.Data
	}
	return json.Unmarshal(raw, out)
}

func fetchBazarr(ctx context.Context, hc *http.Client, c Conn) (bazarrData, error) {
	var d bazarrData
	get := func(path string) (json.RawMessage, error) {
		var raw json.RawMessage
		err := read(ctx, hc, c, call{path: path, keyHeader: "X-API-KEY"}, &raw)
		return raw, err
	}
	raw, err := get("/api/system/status")
	if err != nil {
		return d, err
	}
	var st struct {
		Version string `json:"bazarr_version"`
	}
	if err := unwrapData(raw, &st); err != nil || st.Version == "" {
		return d, fmt.Errorf("this address did not answer like Bazarr (is this the right app?)")
	}
	d.Version = st.Version

	raw, err = get("/api/system/languages")
	if err != nil {
		return d, fmt.Errorf("read languages: %w", err)
	}
	var all []bazarrLanguage
	if err := unwrapData(raw, &all); err != nil {
		return d, fmt.Errorf("read languages: unexpected answer: %w", err)
	}
	for _, l := range all {
		if l.Enabled {
			d.Languages = append(d.Languages, l)
		}
	}

	// Providers are a bonus: without them the languages still import.
	raw, err = get("/api/system/settings")
	if err != nil {
		d.ProvidersError = err.Error()
		return d, nil
	}
	var settings struct {
		General struct {
			EnabledProviders []string `json:"enabled_providers"`
		} `json:"general"`
	}
	if err := unwrapData(raw, &settings); err != nil {
		d.ProvidersError = "unexpected answer: " + err.Error()
		return d, nil
	}
	d.Providers = settings.General.EnabledProviders
	return d, nil
}

var langCodePattern = regexp.MustCompile(`^[a-z]{2,3}(-[a-z]{2,4})?$`)

// bazarrCode maps a Bazarr language to Mediarium's (OpenSubtitles) code.
func bazarrCode(l bazarrLanguage) (string, bool) {
	code := strings.ToLower(strings.TrimSpace(l.Code2))
	switch code {
	case "pb":
		return "pt-BR", true
	case "zt":
		return "zh-TW", true
	case "zh":
		return "zh-CN", true
	}
	if !langCodePattern.MatchString(code) {
		return "", false
	}
	return code, true
}

// providerEquivalent names the Mediarium provider doing a Bazarr
// provider's job.
func providerEquivalent(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "opensubtitles", "opensubtitlescom", "opensubtitlesvip":
		return "OpenSubtitles"
	}
	return ""
}

// subtitlePlan is the subtitle languages Mediarium would end up with.
type subtitlePlan struct {
	languages []string
	same      bool
}

func (im *Importer) planBazarr(p *plan, sp *SubtitlesPreview, d *bazarrData, include bool) {
	var current []string
	if im.deps.SubtitleLanguages != nil {
		current = im.deps.SubtitleLanguages()
	}
	sp.Included = include
	sp.Current = append([]string{}, current...)
	sp.ProvidersError = d.ProvidersError

	have := map[string]bool{}
	for _, c := range current {
		have[strings.ToLower(c)] = true
	}
	mapped := map[string]bool{}
	var fromBazarr []string
	for _, l := range d.Languages {
		item := SubtitleLanguage{Name: l.Name, Code: l.Code2}
		code, ok := bazarrCode(l)
		switch {
		case !ok:
			item.Action, item.Reason = ActionSkip, "Bazarr gives no two-letter code Mediarium can use for it"
		case mapped[strings.ToLower(code)]:
			item.MapsTo, item.Action, item.Reason = code, ActionSkip, "another Bazarr language maps to the same code"
		case have[strings.ToLower(code)]:
			item.MapsTo, item.Action, item.Reason = code, ActionExists, "already one of your subtitle languages"
			mapped[strings.ToLower(code)] = true
			fromBazarr = append(fromBazarr, code)
		default:
			item.MapsTo, item.Action = code, ActionAdd
			mapped[strings.ToLower(code)] = true
			fromBazarr = append(fromBazarr, code)
		}
		sp.Summary.count(item.Action)
		sp.Languages = append(sp.Languages, item)
	}

	// Languages Mediarium already has keep their place (the first one is
	// the default for a manual search); the others follow in Bazarr's order.
	sp.New = []string{}
	for _, c := range current {
		if mapped[strings.ToLower(c)] {
			sp.New = append(sp.New, c)
		}
	}
	for _, c := range fromBazarr {
		if !have[strings.ToLower(c)] {
			sp.New = append(sp.New, c)
		}
	}

	for _, id := range d.Providers {
		pr := SubtitleProvider{Name: id, Equivalent: providerEquivalent(id)}
		if pr.Equivalent == "" {
			pr.Reason = "Mediarium has no equivalent: it gets subtitles from OpenSubtitles only"
		}
		sp.Providers = append(sp.Providers, pr)
	}

	if include && len(sp.New) > 0 {
		p.subtitles = &subtitlePlan{languages: sp.New, same: reflect.DeepEqual(sp.New, current)}
	}
}

// importSubtitleLanguages replaces Mediarium's subtitle languages with the
// ones Bazarr has enabled.
func (im *Importer) importSubtitleLanguages(sp *subtitlePlan) (outcome, reason string) {
	list := strings.Join(sp.languages, ", ")
	switch {
	case sp.same:
		return OutcomeExists, "subtitle languages are already " + list
	case im.deps.SetSubtitleLanguages == nil:
		return OutcomeFailed, "subtitle languages can't be changed here"
	}
	if err := im.deps.SetSubtitleLanguages(sp.languages); err != nil {
		return OutcomeFailed, err.Error()
	}
	return OutcomeAdded, "subtitle languages set to " + list
}
