package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/announce"
	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// ircDecider is what the IRC announce engine asks about every release it
// reads: is this wanted, and if so, grab it. It reuses the checks of the
// scheduled searches (the same title matching, quality profile, size,
// language, blocklist, upgrade and "already downloading" rules), so a release
// announced on IRC is treated exactly like the same release found by an RSS
// sync. It only ever acts on titles that are already in the library.
type ircDecider struct{ s *Server }

// ircWantedTTL is how long the list of titles is kept between announcements.
// A busy tracker announces many releases a minute; reading the whole library
// for each would be wasteful. A grab re-checks the title's state itself, so a
// slightly old list cannot cause a second download.
const ircWantedTTL = 20 * time.Second

// ircLibrary is the monitored titles, read at most every ircWantedTTL.
type ircLibrary struct {
	mu     sync.Mutex
	at     time.Time
	movies []library.Movie
	series []library.Series
}

func (c *ircLibrary) load(s *Server) ([]library.Movie, []library.Series, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) < ircWantedTTL && (c.movies != nil || c.series != nil) {
		return c.movies, c.series, nil
	}
	movies, err := s.MovieRepo.ListWithCopies()
	if err != nil {
		return nil, nil, fmt.Errorf("list movies: %w", err)
	}
	series, err := s.MovieRepo.ListSeriesWithCopies()
	if err != nil {
		return nil, nil, fmt.Errorf("list series: %w", err)
	}
	c.at = time.Now()
	c.movies, c.series = movies, series
	if c.movies == nil {
		c.movies = []library.Movie{}
	}
	if c.series == nil {
		c.series = []library.Series{}
	}
	return c.movies, c.series, nil
}

func (c *ircLibrary) forget() {
	c.mu.Lock()
	c.at = time.Time{}
	c.mu.Unlock()
}

func skipped(format string, a ...any) announce.Decision {
	return announce.Decision{Action: announce.DecisionSkipped, Reason: fmt.Sprintf(format, a...)}
}

// isTVRelease reports whether a parsed release name is an episode or season
// of a show rather than a film.
func isTVRelease(r parser.Release) bool {
	return r.Season > 0 || r.Special || r.AirDate != "" || len(r.Absolute) > 0
}

// Decide implements announce.Decider.
func (d ircDecider) Decide(ctx context.Context, c announce.Connection, a announce.Announce, o announce.DecideOptions) announce.Decision {
	res := indexers.Result{Title: a.Release, SizeBytes: a.SizeBytes, Protocol: indexers.ProtocolTorrent, Torrent: true, DownloadURL: "irc-announce", IndexerName: c.Name, PublishDate: time.Now()}
	return d.decideRelease(ctx, res, o)
}

// decideRelease is Decide for any one release, from an IRC announce or an RSS
// feed list: is it wanted for a title in the library, and if so, grab it.
func (d ircDecider) decideRelease(ctx context.Context, res indexers.Result, o announce.DecideOptions) announce.Decision {
	s := d.s
	if !s.automationEnabled() {
		return skipped("Automatic searching and downloading is switched off.")
	}
	rel := parser.Parse(res.Title)
	if rel.Title == "" {
		return skipped("Couldn't work out a title from the release name.")
	}
	movies, series, err := s.irc.load(s)
	if err != nil {
		slog.Warn("irc announce: could not read the library", "error", err)
		return announce.Decision{Action: announce.DecisionFailed, Reason: "Couldn't read your library."}
	}
	// Most announcements are for something nobody asked for: say so from the
	// cached titles before anything else is read.
	tv := isTVRelease(rel)
	if tv && !anyShowMatches(res.Title, rel, series) {
		return skipped("No show in your library matches this.")
	}
	if !tv && !anyMovieMatches(res.Title, movies) {
		return skipped("No movie in your library matches this.")
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		slog.Warn("irc announce: could not read the quality profiles", "error", err)
		return announce.Decision{Action: announce.DecisionFailed, Reason: "Couldn't read the quality profiles."}
	}
	if tv {
		if !s.tvEnabled() {
			return skipped("Shows are switched off in Settings > Media types.")
		}
		return d.decideTV(ctx, rel, res, series, profiles, o)
	}
	if !s.moviesEnabled() {
		return skipped("Movies are switched off in Settings > Media types.")
	}
	return d.decideMovie(ctx, res, movies, profiles, o)
}

// finish checks the limits, builds the download address and does the grab
// (unless this is a dry run). grab gets the real address.
func (d ircDecider) finish(o announce.DecideOptions, item string, grab func(url string) error) announce.Decision {
	if o.Allow != nil {
		if ok, why := o.Allow(); !ok {
			return announce.Decision{Action: announce.DecisionSkipped, Reason: why, Item: item}
		}
	}
	if o.Dry {
		return announce.Decision{Action: announce.DecisionWouldGrab, Reason: "It is wanted and would be downloaded.", Item: item}
	}
	if !d.s.autoGrabRoom() {
		return announce.Decision{Action: announce.DecisionSkipped, Reason: fmt.Sprintf("Held back: automatic downloads are limited to %d an hour and %d waiting at once, and that is reached for now.", maxAutoDownloadsPerHr, maxAutoInLine), Item: item}
	}
	if o.URL == nil {
		return announce.Decision{Action: announce.DecisionFailed, Reason: "No download address.", Item: item}
	}
	u, err := o.URL()
	if err != nil {
		return announce.Decision{Action: announce.DecisionFailed, Reason: capitalizeSentence(err.Error()), Item: item}
	}
	if err := grab(u); err != nil {
		switch {
		case errors.Is(err, errAlreadyGrabbed):
			return announce.Decision{Action: announce.DecisionSkipped, Reason: "Already being downloaded.", Item: item}
		case errors.Is(err, errNZBLinkHeld):
			return announce.Decision{Action: announce.DecisionSkipped, Reason: "That release is held back for a few hours because its indexer sent a web page instead of the NZB file.", Item: item}
		case errors.Is(err, errBlocklisted):
			return announce.Decision{Action: announce.DecisionSkipped, Reason: "That release is on the blocklist.", Item: item}
		case errors.Is(err, errTorrentsDisabled):
			return announce.Decision{Action: announce.DecisionSkipped, Reason: "Torrents are switched off in Settings > Downloading > Torrents.", Item: item}
		}
		// The message is already written for a person; the address is never in it.
		slog.Warn("irc announce: grab failed", "item", item, "error", err)
		return announce.Decision{Action: announce.DecisionFailed, Reason: capitalizeSentence(err.Error()), Item: item}
	}
	d.s.irc.forget()
	return announce.Decision{Action: announce.DecisionGrabbed, Reason: "Sent to the download line.", Item: item}
}

// onlyOtherSource says a title takes only the other kind of download.
func onlyOtherSource(title string, res indexers.Result) string {
	if res.Protocol == indexers.ProtocolTorrent {
		return fmt.Sprintf("%s only uses Usenet downloads.", title)
	}
	return fmt.Sprintf("%s only uses torrent downloads.", title)
}

func capitalizeSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") {
		s += "."
	}
	return s
}

// limitFor is how the size table and the delay profile apply to an announce:
// like an automatic search, except that the test box records nothing.
func limitFor(o announce.DecideOptions) limitMode {
	if o.Dry {
		return limitDry
	}
	return limitAutomatic
}

// decideMovie matches the release to a wanted movie the way the RSS sync does.
func (d ircDecider) decideMovie(ctx context.Context, res indexers.Result, movies []library.Movie, profiles profileSet, o announce.DecideOptions) announce.Decision {
	s := d.s
	var why string
	var matched bool
	blocked := s.searchSkipKeys()
	for _, m := range movies {
		if !matchesMovie(res.Title, m) {
			continue
		}
		matched = true
		if !m.Monitored {
			why = fmt.Sprintf("Matches %s, which isn't monitored.", movieLabel(m))
			continue
		}
		profile := s.profileForMovie(profiles, m)
		sources := s.sourcesFor(m.SourcePref)
		cands := filterSources(dropBlocked([]indexers.Result{res}, blocked), sources)
		if len(cands) == 0 {
			switch {
			case blocked[blocklist.Key(res.Title)]:
				why = "That release is on the blocklist."
			default:
				why = onlyOtherSource(movieLabel(m), res)
			}
			continue
		}
		var best *indexers.Result
		switch m.Status {
		case library.StatusMissing:
			if waitingForRelease(m.ReleaseDate) {
				why = fmt.Sprintf("%s isn't released yet.", movieLabel(m))
				continue
			}
			if note, hold := s.homeReleaseHold(ctx, m, profile); hold {
				why = fmt.Sprintf("%s is only in cinemas so far. %s", movieLabel(m), firstSentence(note))
				continue
			}
			if pickBestResult(cands, profile, m.Year) == nil {
				why = fmt.Sprintf("Matches %s, but doesn't fit its quality profile (%s): quality, size or language.", movieLabel(m), profile.Name)
				continue
			}
			// The size table and the delay profile, as for the automatic searches.
			limited, rep := s.limitMovie(ctx, m, profile, limitFor(o), cands)
			if best = pickBestResult(limited, profile, m.Year); best == nil {
				why = fmt.Sprintf("Matches %s, but %s.", movieLabel(m), rep.why())
				continue
			}
		case library.StatusDownloaded:
			if m.NoUpgrade {
				why = fmt.Sprintf("%s is downloaded and set not to be upgraded.", movieLabel(m))
				continue
			}
			if !wantsUpgrade(profile, m.Quality, m.ReleaseTitle) {
				why = fmt.Sprintf("%s is already at the quality its profile asks for.", movieLabel(m))
				continue
			}
			if pickUpgradeResult(cands, profile, quality.Tier(m.Quality), m.ReleaseTitle, m.Year) == nil {
				why = fmt.Sprintf("Matches %s, but isn't a better version than the file you have.", movieLabel(m))
				continue
			}
			limited, rep := s.limitMovie(ctx, m, profile, limitFor(o), cands)
			if best = pickUpgradeResult(limited, profile, quality.Tier(m.Quality), m.ReleaseTitle, m.Year); best == nil {
				why = fmt.Sprintf("Matches %s, but %s.", movieLabel(m), rep.why())
				continue
			}
		default:
			why = fmt.Sprintf("%s is already being downloaded.", movieLabel(m))
			continue
		}
		m := m
		return d.finish(o, movieLabel(m), func(url string) error {
			if _, err := s.grabMovie(m, res.Title, url, res.SizeBytes, res.Protocol, grabAutomatic); err != nil {
				return err
			}
			if m.Status == library.StatusMissing {
				s.noteFallbackGrab(m.ID, 0, m.Title, profile, res.Title)
			}
			return nil
		})
	}
	if !matched {
		return skipped("No movie in your library matches this.")
	}
	return skipped("%s", why)
}

// decideTV matches the release to a show with missing or upgradable
// episodes. It follows the same picks as the automatic search for a single
// episode or season pack (autoGrabTV).
func (d ircDecider) decideTV(ctx context.Context, rel parser.Release, res indexers.Result, series []library.Series, profiles profileSet, o announce.DecideOptions) announce.Decision {
	s := d.s
	blocked := s.searchSkipKeys()
	var matched bool
	var why string
	for _, sr := range showsForRelease(res.Title, rel.Season, series) {
		matched = true
		if !sr.Monitored {
			why = fmt.Sprintf("Matches %s, which isn't monitored.", sr.Title)
			continue
		}
		cands := filterSources(dropBlocked([]indexers.Result{res}, blocked), s.sourcesFor(sr.SourcePref))
		if len(cands) == 0 {
			if blocked[nzbLinkHoldKey(res.DownloadURL, res.Title)] {
				why = "That release is held back for a few hours because its indexer sent a web page instead of the NZB file."
			} else if blocked[blocklist.Key(res.Title)] {
				why = "That release is on the blocklist."
			} else {
				why = onlyOtherSource(sr.Title, res)
			}
			continue
		}
		wants, upgrades, err := s.tvWants(profiles, tvScope{seriesID: sr.ID})
		if err != nil {
			slog.Warn("irc announce: could not read the episodes", "series", sr.Title, "error", err)
			return announce.Decision{Action: announce.DecisionFailed, Reason: "Couldn't read the episodes of " + sr.Title + "."}
		}
		if len(wants) == 0 && len(upgrades) == 0 {
			why = fmt.Sprintf("Nothing is missing or waiting for an upgrade for %s.", sr.Title)
			continue
		}
		mapper := s.releaseMapper(sr)
		var season, episode int
		var found bool
		var profile quality.Profile
		// fits says whether the release is a pick for this target, with the size
		// table and the delay profile applied as for the automatic searches.
		var limitWhy string
		fits := func(series library.Series, season, episode int, prof quality.Profile, cur *upgradeBase) bool {
			if pickTVResult(cands, series, season, episode, prof, cur, mapper) == nil {
				return false
			}
			limited, rep := s.limitTV(series, season, episode, prof, cur, limitFor(o), cands)
			if pickTVResult(limited, series, season, episode, prof, cur, mapper) == nil {
				if limitWhy == "" {
					limitWhy = rep.why()
				}
				return false
			}
			return true
		}
		for _, w := range wants {
			if w.wholeSeason && fits(w.series, w.season, 0, w.profile, nil) {
				season, episode, profile, found = w.season, 0, w.profile, true
				break
			}
			for _, ep := range w.episodes {
				if fits(w.series, w.season, ep.Episode, w.profile, nil) {
					season, episode, profile, found = w.season, ep.Episode, w.profile, true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			for _, u := range upgrades {
				cur := upgradeBase{Tier: quality.Tier(u.episode.Quality), Release: u.episode.ReleaseTitle}
				if fits(u.series, u.episode.Season, u.episode.Episode, u.profile, &cur) {
					season, episode, profile, found = u.episode.Season, u.episode.Episode, u.profile, true
					break
				}
			}
		}
		if !found {
			if limitWhy != "" {
				why = fmt.Sprintf("Matches %s, but %s.", sr.Title, limitWhy)
				continue
			}
			why = fmt.Sprintf("Matches %s, but it isn't an episode you are missing, or doesn't fit the quality profile (quality, size or language).", sr.Title)
			continue
		}
		sr := sr
		item := sr.Title
		switch {
		case episode > 0:
			item = fmt.Sprintf("%s S%02dE%02d", sr.Title, season, episode)
		case season > 0:
			item = fmt.Sprintf("%s season %d", sr.Title, season)
		}
		return d.finish(o, item, func(url string) error {
			if _, err := s.grabTV(sr, season, episode, res.Title, url, res.SizeBytes, res.Protocol, grabAutomatic); err != nil {
				return err
			}
			s.noteFallbackGrab(0, sr.ID, sr.Title, profile, res.Title)
			return nil
		})
	}
	if !matched {
		return skipped("No show in your library matches this.")
	}
	return skipped("%s", why)
}

func anyMovieMatches(release string, movies []library.Movie) bool {
	for _, m := range movies {
		if matchesMovie(release, m) {
			return true
		}
	}
	return false
}

func anyShowMatches(release string, rel parser.Release, series []library.Series) bool {
	for _, sr := range series {
		if matchesShowFor(release, sr, rel.Season) {
			return true
		}
	}
	return false
}

func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}
