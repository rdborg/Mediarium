package subtitles

import "errors"

// Where subtitles can be searched. Each source is switched on or off by the
// person; a source also needs something to work with (a key, or at least one
// add-on), and a source with nothing to work with is never on, whatever its
// switch says.

// The ids the settings payload and the interface use for the sources.
const (
	SourceIDOpenSubtitles = "opensubtitles"
	SourceIDSubDL         = "subdl"
	SourceIDAddons        = "addons"
)

// ErrNoSource means every source that is set up has been switched off, or none
// is set up at all.
var ErrNoSource = errors.New("No subtitle source is switched on. Switch one on in Settings > Library and files > Subtitles.")

// SourceSetup is what one source has to go on.
type SourceSetup struct {
	Ready  bool // it has what it needs: a key, or at least one add-on
	Switch bool // the person's on/off choice (on unless switched off)
}

// On reports whether the source is searched.
func (s SourceSetup) On() bool { return s.Ready && s.Switch }

// The states a source can be in, for the settings page.
const (
	StateOn       = "on"
	StateOff      = "off"       // set up, but switched off
	StateNeedsKey = "needs_key" // nothing to work with yet
	StateLimit    = "limit"     // on, but today's download limit is used up
)

// State names the state of a source. limitReached only matters for a source
// that is on.
func (s SourceSetup) State(limitReached bool) string {
	switch {
	case !s.Ready:
		return StateNeedsKey
	case !s.Switch:
		return StateOff
	case limitReached:
		return StateLimit
	}
	return StateOn
}

// Sources are the three kinds of source Mediarium can search.
type Sources struct {
	OpenSubtitles SourceSetup
	SubDL         SourceSetup
	Addons        SourceSetup
}

// Any reports whether at least one source is switched on and set up.
func (s Sources) Any() bool { return s.OpenSubtitles.On() || s.SubDL.On() || s.Addons.On() }

// AnySetUp reports whether at least one source is set up, switched on or not.
func (s Sources) AnySetUp() bool { return s.OpenSubtitles.Ready || s.SubDL.Ready || s.Addons.Ready }

// OthersThanOpenSubtitles reports whether SubDL or an add-on is on, so that
// searching can carry on when OpenSubtitles runs out of downloads.
func (s Sources) OthersThanOpenSubtitles() bool { return s.SubDL.On() || s.Addons.On() }

// Count is how many sources are on.
func (s Sources) Count() int {
	n := 0
	for _, x := range []SourceSetup{s.OpenSubtitles, s.SubDL, s.Addons} {
		if x.On() {
			n++
		}
	}
	return n
}
