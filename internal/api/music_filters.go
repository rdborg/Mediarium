package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/musicbrainz"
	"github.com/rdborg/mediarium/internal/settings"
)

// Which releases of an artist are listed and added: the release types, the
// secondary types (live, remix...) to leave out and, when switched on, the
// release groups that only exist as bootlegs. One choice for the whole
// library (the music.* settings) and an optional one per artist. Albums that
// are already in the library are never touched by a change of filter: it only
// decides what is added from now on, what the artist page offers below the
// albums and which albums without files its list leaves out.

// libraryMusicFilter is the library-wide choice; unset or unreadable parts
// are the defaults (what the app always did).
func (s *Server) libraryMusicFilter() music.Filter {
	f := music.DefaultFilter()
	if v, _ := s.Settings.Get(settings.KeyMusicShowTypes); v != "" {
		if k, ok := music.DecodeKeys(v); ok {
			f.Types = k
		}
	}
	if v, _ := s.Settings.Get(settings.KeyMusicHideSecondary); v != "" {
		if k, ok := music.DecodeKeys(v); ok {
			f.HideSecondary = k
		}
	}
	if v, _ := s.Settings.Get(settings.KeyMusicHideBootlegs); v == "1" {
		f.HideBootlegs = true
	}
	norm, err := f.Normalize()
	if err != nil {
		return music.DefaultFilter()
	}
	return norm
}

// artistMusicFilter is the choice that counts for one artist, and whether it
// is the artist's own.
func (s *Server) artistMusicFilter(artistID int64) (music.Filter, bool) {
	own, err := s.MusicRepo.ArtistFilter(artistID)
	if err != nil || own == nil {
		return s.libraryMusicFilter(), false
	}
	norm, err := own.Normalize()
	if err != nil {
		return s.libraryMusicFilter(), false
	}
	return norm, true
}

// musicFilterPayload is a filter as the API shows it.
type musicFilterPayload struct {
	music.Filter
	// UseLibrary is true for an artist that follows the library setting.
	UseLibrary bool `json:"useLibrary"`
}

type musicFilterChoices struct {
	Filter         music.Filter       `json:"filter"`
	Defaults       music.Filter       `json:"defaults"`
	Studio         music.Filter       `json:"studio"` // the preset "Official studio releases only"
	SecondaryTypes []musicSecondaryOp `json:"secondaryTypes"`
}

type musicSecondaryOp struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func secondaryOptions() []musicSecondaryOp {
	out := make([]musicSecondaryOp, len(music.SecondaryTypes))
	for i, t := range music.SecondaryTypes {
		out[i] = musicSecondaryOp{Key: t.Key, Name: t.Name}
	}
	return out
}

// handleGetMusicFilters is GET /api/music/filters (any account).
func (s *Server) handleGetMusicFilters(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, musicFilterChoices{
		Filter: s.libraryMusicFilter(), Defaults: music.DefaultFilter(), Studio: music.StudioOnly(), SecondaryTypes: secondaryOptions(),
	})
}

// normalizeFilterRequest checks a filter sent by a person and answers 400
// with a plain sentence when it can't be used.
func normalizeFilterRequest(w http.ResponseWriter, f music.Filter) (music.Filter, bool) {
	if len(f.Types) == 0 {
		writeError(w, http.StatusBadRequest, "Pick at least one of albums, EPs and singles to show.")
		return music.Filter{}, false
	}
	norm, err := f.Normalize()
	if err != nil {
		writeError(w, http.StatusBadRequest, "Those filter choices aren't valid. Reload the page and try again.")
		return music.Filter{}, false
	}
	return norm, true
}

// handleSetMusicFilters is PUT /api/music/filters (administrators): the
// library-wide choice, as {types, hideSecondary, hideBootlegs}.
func (s *Server) handleSetMusicFilters(w http.ResponseWriter, r *http.Request) {
	var req music.Filter
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	f, ok := normalizeFilterRequest(w, req)
	if !ok {
		return
	}
	bootlegs := ""
	if f.HideBootlegs {
		bootlegs = "1"
	}
	for _, kv := range [][2]string{
		{settings.KeyMusicShowTypes, music.EncodeKeys(f.Types)},
		{settings.KeyMusicHideSecondary, music.EncodeKeys(f.HideSecondary)},
		{settings.KeyMusicHideBootlegs, bootlegs},
	} {
		if err := s.Settings.Set(kv[0], kv[1], false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.handleGetMusicFilters(w, r)
}

func filterGroup(g musicbrainz.ReleaseGroup) music.Group {
	return music.Group{PrimaryType: g.PrimaryType, SecondaryTypes: g.SecondaryTypes}
}

// ---- bootleg look-ups ----

// groupChecks is the line of release groups waiting to be asked about. One
// worker walks it, so the look-ups go out one after another at the pace of the
// MusicBrainz rate limit and nothing waits for them.
type groupChecks struct {
	mu      sync.Mutex
	queue   []string
	queued  map[string]bool
	running bool
}

const maxQueuedGroupChecks = 1000

// cachedGroupStatuses returns what is known about the groups: true when a
// group has an official release. Unknown groups are left out.
func (s *Server) cachedGroupStatuses(ids []string) map[string]bool {
	known, err := s.MusicRepo.GroupStatuses(ids, time.Now())
	if err != nil {
		slog.Warn("music: read release group statuses", "err", err)
		return map[string]bool{}
	}
	return known
}

// lookUpGroup asks MusicBrainz about one release group and keeps the answer.
func (s *Server) lookUpGroup(ctx context.Context, id string) (bool, error) {
	official, err := s.MusicBrainz().HasOfficialRelease(ctx, id)
	if err != nil {
		return false, err
	}
	if err := s.MusicRepo.SetGroupStatus(id, official, time.Now()); err != nil {
		slog.Warn("music: keep release group status", "err", err)
	}
	return official, nil
}

// resolveGroups returns the status of every group, looking up the ones not
// known yet one by one. A group it could not look up is left out.
func (s *Server) resolveGroups(ctx context.Context, ids []string) map[string]bool {
	out := s.cachedGroupStatuses(ids)
	for _, id := range ids {
		if _, ok := out[id]; ok {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		official, err := s.lookUpGroup(ctx, id)
		if err != nil {
			slog.Info("music: could not check a release group", "releaseGroup", id, "err", err)
			continue
		}
		out[id] = official
	}
	return out
}

// queueGroupChecks has the groups looked up in the background, skipping the
// ones already waiting.
func (s *Server) queueGroupChecks(ids []string) {
	c := &s.music.checks
	c.mu.Lock()
	if c.queued == nil {
		c.queued = map[string]bool{}
	}
	for _, id := range ids {
		if c.queued[id] || len(c.queue) >= maxQueuedGroupChecks {
			continue
		}
		c.queued[id] = true
		c.queue = append(c.queue, id)
	}
	start := !c.running && len(c.queue) > 0
	if start {
		c.running = true
	}
	c.mu.Unlock()
	if start {
		s.background(s.runGroupChecks)
	}
}

func (s *Server) runGroupChecks() {
	c := &s.music.checks
	for {
		c.mu.Lock()
		if len(c.queue) == 0 || !s.musicEnabled() {
			c.queue, c.queued, c.running = nil, nil, false
			c.mu.Unlock()
			return
		}
		id := c.queue[0]
		c.queue = c.queue[1:]
		c.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		if _, ok := s.cachedGroupStatuses([]string{id})[id]; !ok {
			if _, err := s.lookUpGroup(ctx, id); err != nil {
				slog.Info("music: could not check a release group", "releaseGroup", id, "err", err)
			}
		}
		cancel()

		c.mu.Lock()
		delete(c.queued, id)
		c.mu.Unlock()
	}
}

// passingGroups keeps the groups that pass f. With bootlegs hidden a group is
// checked against the saved answers; those not asked about yet are looked up
// now when lookUp is set, otherwise they come back in later. A group that
// can't be looked up is kept: when the status is unknown it is shown.
func (s *Server) passingGroups(ctx context.Context, f music.Filter, groups []musicbrainz.ReleaseGroup, lookUp bool) (pass, later []musicbrainz.ReleaseGroup) {
	var typed []musicbrainz.ReleaseGroup
	for _, g := range groups {
		if f.Reason(filterGroup(g)) == "" {
			typed = append(typed, g)
		}
	}
	if !f.HideBootlegs {
		return typed, nil
	}
	ids := make([]string, len(typed))
	for i, g := range typed {
		ids[i] = g.ID
	}
	var status map[string]bool
	if lookUp {
		status = s.resolveGroups(ctx, ids)
	} else {
		status = s.cachedGroupStatuses(ids)
	}
	for _, g := range typed {
		official, known := status[g.ID]
		switch {
		case known && !official:
			// bootleg-only: left out
		case known || lookUp:
			pass = append(pass, g)
		default:
			later = append(later, g)
		}
	}
	return pass, later
}

// ---- the artist page ----

type musicHiddenGroup struct {
	MBID           string   `json:"mbid"`
	Title          string   `json:"title"`
	Type           string   `json:"type"`
	ReleaseDate    string   `json:"releaseDate"`
	SecondaryTypes []string `json:"secondaryTypes"`
	HiddenBy       string   `json:"hiddenBy"` // a release type, a secondary type key or "bootleg"
}

// musicHiddenAlbum is an album in the library that the filter would hide.
type musicHiddenAlbum struct {
	AlbumID  int64  `json:"albumId"`
	MBID     string `json:"mbid"`
	HiddenBy string `json:"hiddenBy"`
}

type musicDiscography struct {
	Filter musicFilterPayload `json:"filter"`
	// Hidden lists the release groups the filter leaves out that are not in the library.
	Hidden []musicHiddenGroup `json:"hidden"`
	// Library lists the albums already in the library that the filter leaves out
	// of the list: only those without files, albums with files always stay.
	// Nothing is ever removed or unmonitored by a filter.
	Library []musicHiddenAlbum `json:"library"`
	// Counts is how many of them each reason accounts for.
	Counts map[string]int `json:"counts"`
	// Checking is how many groups are still being looked at for the bootleg test (they are shown until known).
	Checking int `json:"checking"`
}

func (s *Server) artistFilterPayload(artistID int64) musicFilterPayload {
	f, own := s.artistMusicFilter(artistID)
	return musicFilterPayload{Filter: f, UseLibrary: !own}
}

// handleArtistDiscography is GET /api/music/artists/{id}/discography (any
// account): what the effective filter leaves out of the artist's page on
// MusicBrainz, so the page can say what is hidden and why. It answers at once
// from what is known; groups the bootleg test has not looked at yet are
// counted in checking and looked up in the background, and asking again later
// gives the finished answer.
func (s *Server) handleArtistDiscography(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "artist")
	if !ok {
		return
	}
	artist, err := s.MusicRepo.GetArtist(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That artist isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	groups, err := s.MusicBrainz().ArtistReleaseGroups(ctx, artist.MBID, nil, true)
	if err != nil {
		musicBrainzError(w, err, "Couldn't read this artist from the music database.")
		return
	}
	albums, err := s.MusicRepo.ListAlbums(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	filed, err := s.MusicRepo.AlbumsWithFiles(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	have := map[string]music.Album{}
	for _, a := range albums {
		have[a.MBID] = a
		if a.Status == music.StatusDownloaded {
			filed[a.ID] = true // downloaded, even if no track is linked to its file
		}
	}
	f, own := s.artistMusicFilter(id)
	out := musicDiscography{Filter: musicFilterPayload{Filter: f, UseLibrary: !own}, Hidden: []musicHiddenGroup{}, Library: []musicHiddenAlbum{}, Counts: map[string]int{}}

	var candidates []string
	var typePass []musicbrainz.ReleaseGroup
	for _, g := range groups {
		if reason := f.Reason(filterGroup(g)); reason != "" {
			out.addHidden(g, reason, have, filed)
			continue
		}
		typePass = append(typePass, g)
		candidates = append(candidates, g.ID)
	}
	if f.HideBootlegs && len(candidates) > 0 {
		status := s.cachedGroupStatuses(candidates)
		var unknown []string
		for _, g := range typePass {
			official, known := status[g.ID]
			switch {
			case !known:
				unknown = append(unknown, g.ID)
			case !official:
				out.addHidden(g, music.ReasonBootleg, have, filed)
			}
		}
		out.Checking = len(unknown)
		if len(unknown) > 0 {
			s.queueGroupChecks(unknown)
		}
	}
	sort.SliceStable(out.Hidden, func(i, j int) bool { return out.Hidden[i].ReleaseDate < out.Hidden[j].ReleaseDate })
	writeJSON(w, http.StatusOK, out)
}

// addHidden files a hidden group under the library when the album is in it,
// otherwise under the releases on offer.
func (d *musicDiscography) addHidden(g musicbrainz.ReleaseGroup, reason string, have map[string]music.Album, filed map[int64]bool) {
	if a, in := have[g.ID]; in {
		if !filed[a.ID] {
			d.Library = append(d.Library, musicHiddenAlbum{AlbumID: a.ID, MBID: a.MBID, HiddenBy: reason})
		}
		return
	}
	d.add(g, reason)
}

func (d *musicDiscography) add(g musicbrainz.ReleaseGroup, reason string) {
	sec := g.SecondaryTypes
	if sec == nil {
		sec = []string{}
	}
	d.Hidden = append(d.Hidden, musicHiddenGroup{
		MBID: g.ID, Title: g.Title, Type: albumType(g.PrimaryType), ReleaseDate: g.FirstReleaseDate, SecondaryTypes: sec, HiddenBy: reason,
	})
	d.Counts[reason]++
}

// handleAddArtistAlbum is POST /api/music/artists/{id}/albums {mbid}: adds
// one release the filter left out to the library, monitored.
func (s *Server) handleAddArtistAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "artist")
	if !ok {
		return
	}
	var req struct {
		MBID string `json:"mbid"`
	}
	if err := decodeJSON(r, &req); err != nil || !mbidPattern.MatchString(req.MBID) {
		writeError(w, http.StatusBadRequest, "That release ID doesn't look right.")
		return
	}
	artist, err := s.MusicRepo.GetArtist(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That artist isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	groups, err := s.MusicBrainz().ArtistReleaseGroups(ctx, artist.MBID, nil, true)
	if err != nil {
		musicBrainzError(w, err, "Couldn't read this artist from the music database.")
		return
	}
	for _, g := range groups {
		if g.ID != req.MBID {
			continue
		}
		added, err := s.MusicRepo.AddAlbums(id, []music.Album{{
			MBID: g.ID, Title: g.Title, Type: albumType(g.PrimaryType), ReleaseDate: g.FirstReleaseDate, Monitored: true,
		}})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(added) == 0 {
			writeError(w, http.StatusConflict, "That release is already in your library.")
			return
		}
		writeJSON(w, http.StatusCreated, toMusicAlbumPayload(added[0]))
		return
	}
	// Not one of the artist's own releases: a soundtrack or compilation they
	// are on, found through one of its songs.
	added, err := s.addAlbumByMBID(r.Context(), id, req.MBID)
	switch {
	case errors.Is(err, errAlbumExists):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		musicBrainzError(w, err, "That release isn't on the music database.")
	default:
		writeJSON(w, http.StatusCreated, toMusicAlbumPayload(added))
	}
}

// addAlbumByMBID adds one release group to an artist, monitored, looked up by
// its id: a soundtrack or compilation the artist is on but that isn't one of
// their own albums.
func (s *Server) addAlbumByMBID(ctx context.Context, artistID int64, mbid string) (music.Album, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	g, err := s.MusicBrainz().GetReleaseGroup(ctx, mbid)
	if err != nil {
		return music.Album{}, err
	}
	added, err := s.MusicRepo.AddAlbums(artistID, []music.Album{{
		MBID: g.ID, Title: g.Title, Type: albumType(g.PrimaryType), ReleaseDate: g.FirstReleaseDate, Monitored: true,
	}})
	if err != nil {
		return music.Album{}, err
	}
	if len(added) == 0 {
		return music.Album{}, errAlbumExists
	}
	return added[0], nil
}

// errAlbumExists: the release is already one of the artist's.
var errAlbumExists = errors.New("That release is already in your library.")
