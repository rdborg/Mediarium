package api

import (
	"fmt"
	"strings"

	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
)

// Subtitle sources: OpenSubtitles, SubDL and the add-ons each have an on/off
// switch. A source is searched only when it is switched on and has what it
// needs (a key, or at least one add-on). Switched off, it is never asked: not
// by an automatic search, a manual one, an upgrade or the bulk "get subtitles"
// job, and what it would have used does not count towards any limit.

// subtitleSources reads the current state of the three sources.
func (s *Server) subtitleSources() subtitles.Sources {
	return subtitles.Sources{
		OpenSubtitles: subtitles.SourceSetup{Ready: s.Subtitles().HasAPIKey(), Switch: s.settingOn(settings.KeyOpenSubtitlesEnabled, true)},
		SubDL:         subtitles.SourceSetup{Ready: s.SubDL().HasAPIKey(), Switch: s.settingOn(settings.KeySubDLEnabled, true)},
		Addons:        subtitles.SourceSetup{Ready: len(s.loadAddons()) > 0, Switch: s.settingOn(settings.KeySubtitleAddonsEnabled, true)},
	}
}

// useOpenSubtitles, useSubDL and useAddons say whether that source is searched.
func (s *Server) useOpenSubtitles() bool { return s.subtitleSources().OpenSubtitles.On() }
func (s *Server) useSubDL() bool         { return s.subtitleSources().SubDL.On() }
func (s *Server) useAddons() bool        { return s.subtitleSources().Addons.On() }

// hasSubtitleSource reports whether subtitles can be searched anywhere: some
// source is switched on and set up.
func (s *Server) hasSubtitleSource() bool { return s.subtitleSources().Any() || s.wizdomOn() }

// errNoSubtitleSource is the setup hint while no source is set up.
const errNoSubtitleSource = "Add an OpenSubtitles or SubDL API key, or a subtitle add-on, first (Settings > Library and files > Subtitles)."

// errAllSubtitleSourcesOff is the hint while sources are set up but all switched off.
const errAllSubtitleSourcesOff = "No subtitle source is switched on. Switch one on in Settings > Library and files > Subtitles."

// noSubtitleSourceMessage says why nothing can be searched: nothing is set up
// yet, or everything that is has been switched off.
func (s *Server) noSubtitleSourceMessage() string {
	if s.subtitleSources().AnySetUp() {
		return errAllSubtitleSourcesOff
	}
	return errNoSubtitleSource
}

// subtitleSourceStatus is one row of the sources card on the settings page.
type subtitleSourceStatus struct {
	ID string `json:"id"` // "opensubtitles", "subdl" or "addons"
	// Enabled is the switch as the person left it (on unless switched off),
	// whether or not the source is set up.
	Enabled bool `json:"enabled"`
	// Ready: it has what it needs, a saved key or at least one add-on.
	Ready bool `json:"ready"`
	// State is "on", "off" (set up but switched off), "needs_key" or "limit"
	// (on, but today's downloads are used up).
	State string `json:"state"`
	// Count is how many add-ons are added (the add-ons row only).
	Count int `json:"count,omitempty"`
}

// subtitleSourceStatuses lists the sources in the order the page shows them.
func (s *Server) subtitleSourceStatuses() []subtitleSourceStatus {
	src := s.subtitleSources()
	limit := src.OpenSubtitles.On() && s.subtitleQuota().Exceeded
	return []subtitleSourceStatus{
		{ID: subtitles.SourceIDOpenSubtitles, Enabled: src.OpenSubtitles.Switch, Ready: src.OpenSubtitles.Ready, State: src.OpenSubtitles.State(limit)},
		{ID: subtitles.SourceIDSubDL, Enabled: src.SubDL.Switch, Ready: src.SubDL.Ready, State: src.SubDL.State(false)},
		{ID: subtitles.SourceIDAddons, Enabled: src.Addons.Switch, Ready: src.Addons.Ready, State: src.Addons.State(false), Count: len(s.loadAddons())},
	}
}

// checkSourceSwitches refuses to switch on a source that has nothing to work
// with. The keys sent in the same request count, since they are saved first.
// It returns a plain message, or "" when the request is fine.
func (s *Server) checkSourceSwitches(req *settingsPayload) string {
	// A switch that already reads "on" may be sent back unchanged (a client
	// that saves the whole settings object does), so only turning one on that
	// was switched off is checked.
	cur := s.subtitleSources()
	if req.OpenSubtitlesEnabled != nil && *req.OpenSubtitlesEnabled && !cur.OpenSubtitles.Switch {
		ready := s.Subtitles().HasAPIKey()
		if req.OpenSubtitlesAPIKey != nil {
			ready = strings.TrimSpace(*req.OpenSubtitlesAPIKey) != "" || s.builtin.OpenSubtitles != ""
		}
		if !ready {
			return "Add your OpenSubtitles API key first, then switch it on."
		}
	}
	if req.SubDLEnabled != nil && *req.SubDLEnabled && !cur.SubDL.Switch {
		ready := s.SubDL().HasAPIKey()
		if req.SubDLAPIKey != nil {
			ready = strings.TrimSpace(*req.SubDLAPIKey) != ""
		}
		if !ready {
			return "Add your SubDL API key first, then switch it on."
		}
	}
	if req.SubtitleAddonsEnabled != nil && *req.SubtitleAddonsEnabled && !cur.Addons.Switch && !cur.Addons.Ready {
		return "Add a subtitle add-on first, then switch add-ons on."
	}
	return ""
}

// saveSourceSwitches stores the switches a request carries.
func (s *Server) saveSourceSwitches(st settingSetter, req *settingsPayload) error {
	for _, c := range []struct {
		key string
		val *bool
	}{
		{settings.KeyOpenSubtitlesEnabled, req.OpenSubtitlesEnabled},
		{settings.KeySubDLEnabled, req.SubDLEnabled},
		{settings.KeySubtitleAddonsEnabled, req.SubtitleAddonsEnabled},
	} {
		if c.val == nil {
			continue
		}
		if err := setBool(st, c.key, *c.val); err != nil {
			return err
		}
	}
	return nil
}

// subtitleOfferImpact is the second line of the "N downloaded titles have no
// subtitles yet" notice. It speaks only of the limits of sources that are
// switched on: OpenSubtitles' small daily allowance counts while it is on,
// and not at all while only SubDL or add-ons are.
func subtitleOfferImpact(src subtitles.Sources, st subtitles.QuotaState, files int) string {
	impact := "Subtitles download only when you ask."
	if !src.OpenSubtitles.On() {
		return impact
	}
	if src.OthersThanOpenSubtitles() {
		// The other sources carry on when OpenSubtitles' downloads run out.
		if st.Remaining > 0 {
			return impact + fmt.Sprintf(" About %s left today on OpenSubtitles, then the other sources carry on.", plural(st.Remaining, "download"))
		}
		return impact + " Today's OpenSubtitles limit is used up, so the other sources are used."
	}
	impact += " "
	if st.Remaining > 0 {
		impact += fmt.Sprintf("About %s left today", plural(st.Remaining, "download"))
	} else {
		impact += "Today's OpenSubtitles limit is used up"
	}
	if files > st.Remaining && st.Limit > 0 {
		impact += fmt.Sprintf(", and %s are wanted, so the rest takes about %s", plural(files, "subtitle"), plural(daysToFinish(files, st.Limit), "day"))
	}
	return impact + "."
}

// sourceOnFor reports whether the source a search result came from ("subdl",
// "addon" or empty for OpenSubtitles) is switched on. The second value starts
// a sentence: "OpenSubtitles is", "Subtitle add-ons are".
func (s *Server) sourceOnFor(source string) (on bool, name string) {
	src := s.subtitleSources()
	switch source {
	case subtitles.SourceSubDL:
		return src.SubDL.On(), "SubDL is"
	case subtitles.SourceAddon:
		return src.Addons.On(), "Subtitle add-ons are"
	}
	return src.OpenSubtitles.On(), "OpenSubtitles is"
}
