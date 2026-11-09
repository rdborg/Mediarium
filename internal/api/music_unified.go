package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/musicbrainz"
)

// The one music search (#143): artists, albums and songs found at the same
// time, ranked together and returned as one list (GET /api/music/search?type=all).
// The older searches (artists only, ?type=album, ?type=song) are unchanged.

// musicSourceWait is how long one of the three searches may take. A slow or
// failing one is left out of the answer, with a note, instead of holding the
// other two back.
const musicSourceWait = 20 * time.Second

// musicSearchPartial is the note when one of the three searches could not run.
const musicSearchPartial = "Part of the music search couldn't run right now, so some artists, albums or songs may be missing from this list."

// maxMusicHits is how many results the one list holds.
const maxMusicHits = 60

// musicHit is one row of the list. Albums and songs carry the same fields as
// a Discover album (so the add dialog takes them as they are); a song also
// names the album it was found on in those fields and the song in Song.
type musicHit struct {
	Kind string  `json:"kind"` // artist, album or song
	Rank float64 `json:"rank"`
	// Name is what the row is called: the artist, the album or the song.
	Name           string `json:"name"`
	Country        string `json:"country,omitempty"`        // artists
	Disambiguation string `json:"disambiguation,omitempty"` // artists
	Soundtrack     bool   `json:"soundtrack,omitempty"`     // an album that is a soundtrack
	musicDiscoverItem
}

type musicSearchAnswer struct {
	Items []musicHit `json:"items"`
	// Note says in plain words which part could not be searched right now.
	Note string `json:"note,omitempty"`
}

// musicSearchAll answers ?type=all.
func (s *Server) musicSearchAll(w http.ResponseWriter, r *http.Request, q string) {
	mb := s.MusicBrainz()
	var (
		wg      sync.WaitGroup
		artists []musicbrainz.Artist
		albums  []musicbrainz.FoundAlbum
		songs   []musicbrainz.FoundAlbum
		errs    [3]error
	)
	run := func(i int, f func(ctx context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), musicSourceWait)
			defer cancel()
			errs[i] = f(ctx)
		}()
	}
	run(0, func(ctx context.Context) (err error) { artists, err = mb.SearchArtistsAny(ctx, q, 25); return })
	run(1, func(ctx context.Context) (err error) { albums, err = mb.SearchAlbumsAny(ctx, q, 25); return })
	run(2, func(ctx context.Context) (err error) { songs, err = mb.SearchSongsAny(ctx, q, 40); return })
	wg.Wait()
	if r.Context().Err() != nil {
		return // the page asked again or went away
	}
	if errs[0] != nil && errs[1] != nil && errs[2] != nil {
		musicBrainzError(w, errs[0], "nothing found")
		return
	}
	var missed []string // logged; the screen only says that part of the search failed
	for i, what := range []string{"artists", "albums", "songs"} {
		if errs[i] != nil {
			slog.Info("music: one-box search: a part failed", "part", what, "err", errs[i])
			missed = append(missed, what)
		}
	}

	hits, cands := buildMusicHits(artists, albums, songs)
	ranked := music.RankSearch(q, cands)
	if len(ranked) > maxMusicHits {
		ranked = ranked[:maxMusicHits]
	}
	out := make([]musicHit, len(ranked))
	var forLibrary []discoverCandidate
	var at []int // out index of each forLibrary entry
	for i, rk := range ranked {
		out[i] = hits[rk.Ref]
		out[i].Rank = rk.Rank
		if out[i].Kind == string(music.SearchArtist) {
			if existing, ok, err := s.MusicRepo.GetArtistByMBID(out[i].MBID); err == nil && ok {
				out[i].ArtistID, out[i].InLibrary = existing.ID, true
			}
			continue
		}
		forLibrary = append(forLibrary, discoverCandidate{
			mbid: out[i].MBID, title: out[i].Title, typ: out[i].Type, artistName: out[i].ArtistName, artistMBID: out[i].ArtistMBID, date: out[i].ReleaseDate,
		})
		at = append(at, i)
	}
	items, err := s.discoverItems(forLibrary)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for j, it := range items {
		h := &out[at[j]]
		h.InLibrary, h.ArtistID, h.AlbumID, h.CoverURL, h.Genres = it.InLibrary, it.ArtistID, it.AlbumID, it.CoverURL, it.Genres
	}
	ans := musicSearchAnswer{Items: out}
	if len(missed) > 0 {
		ans.Note = musicSearchPartial
	}
	writeJSON(w, http.StatusOK, ans)
}

// buildMusicHits turns what the three searches found into rows and the
// candidates to rank (Ref is the row's index). Songs are collapsed: a song
// that sits on a single, an album and two compilations is one row, on the album.
func buildMusicHits(artists []musicbrainz.Artist, albums, songs []musicbrainz.FoundAlbum) ([]musicHit, []music.SearchCandidate) {
	var hits []musicHit
	var cands []music.SearchCandidate
	add := func(h musicHit, c music.SearchCandidate) {
		c.Ref = len(hits)
		hits = append(hits, h)
		cands = append(cands, c)
	}
	for _, a := range artists {
		add(musicHit{Kind: "artist", Name: a.Name, Country: a.Country, Disambiguation: a.Disambiguation,
			musicDiscoverItem: musicDiscoverItem{MBID: a.ID, Title: a.Name, Type: a.Type, ArtistName: a.Name, ArtistMBID: a.ID}},
			music.SearchCandidate{Kind: music.SearchArtist, Name: a.Name, Disambiguation: a.Disambiguation, Score: a.Score})
	}
	for _, f := range albums {
		add(albumHit("album", f, ""), music.SearchCandidate{
			Kind: music.SearchAlbum, Name: f.Title, Artist: f.ArtistName, Score: f.Score, PrimaryType: f.PrimaryType, SecondaryTypes: f.SecondaryTypes,
		})
	}
	for _, f := range collapseSongs(songs) {
		add(albumHit("song", f, f.Song), music.SearchCandidate{
			Kind: music.SearchSong, Name: f.Song, Artist: f.ArtistName, Album: f.Title, Score: f.Score,
			PrimaryType: f.PrimaryType, SecondaryTypes: f.SecondaryTypes, Releases: f.Releases,
		})
	}
	return hits, cands
}

func albumHit(kind string, f musicbrainz.FoundAlbum, song string) musicHit {
	typ := strings.ToLower(f.PrimaryType)
	if t, ok := discoverType(f.PrimaryType); ok {
		typ = t
	}
	name := f.Title
	if song != "" {
		name = song
	}
	soundtrack := false
	for _, st := range f.SecondaryTypes {
		if strings.EqualFold(st, "soundtrack") {
			soundtrack = true
		}
	}
	return musicHit{
		Kind: kind, Name: name, Soundtrack: soundtrack,
		musicDiscoverItem: musicDiscoverItem{
			MBID: f.ID, Title: f.Title, Type: typ, ArtistName: f.ArtistName, ArtistMBID: f.ArtistID,
			ReleaseDate: f.FirstReleaseDate, CoverURL: releaseGroupCoverURL(f.ID), Song: song,
		},
	}
}

// collapseSongs makes one entry per song and artist out of the albums the
// song was found on: the one on the studio album (or else the soundtrack)
// wins over singles and compilations, then the earliest. Releases becomes the
// number of albums it was found on, a sign of a well known song.
func collapseSongs(found []musicbrainz.FoundAlbum) []musicbrainz.FoundAlbum {
	type slot struct {
		best musicbrainz.FoundAlbum
		pref float64
		n    int
	}
	order := []string{}
	by := map[string]*slot{}
	for _, f := range found {
		key := music.NormalizeName(f.Song) + "|" + f.ArtistID
		pref := music.GroupPreference(f.PrimaryType, f.SecondaryTypes, f.Title, false)
		sl, ok := by[key]
		if !ok {
			by[key] = &slot{best: f, pref: pref, n: 1}
			order = append(order, key)
			continue
		}
		sl.n++
		if pref > sl.pref || (pref == sl.pref && f.FirstReleaseDate != "" && (sl.best.FirstReleaseDate == "" || f.FirstReleaseDate < sl.best.FirstReleaseDate)) {
			sl.best, sl.pref = f, pref
		}
	}
	out := make([]musicbrainz.FoundAlbum, 0, len(order))
	for _, k := range order {
		sl := by[k]
		sl.best.Releases = max(sl.best.Releases, sl.n)
		out = append(out, sl.best)
	}
	return out
}

// wantOnlySong records, for an album that was just added, the one song that
// is wanted ("Add only this song"). Nothing happens for an empty song. A
// failure is logged: the album is still added, and whole.
func (s *Server) wantOnlySong(albumID int64, song string) {
	song = strings.TrimSpace(song)
	if song == "" {
		return
	}
	if err := s.MusicRepo.SetOnlySong(albumID, song); err != nil {
		slog.Warn("music: remember the wanted song", "album", albumID, "err", err)
		return
	}
	// When the tracklist is already known, the flags are set right away; it
	// is otherwise done when the first import fetches it.
	if _, err := s.MusicRepo.ApplyOnlySong(albumID); err != nil {
		slog.Warn("music: apply the wanted song", "album", albumID, "err", err)
	}
}

// handleSetTrackMonitored switches one track of an album between wanted and
// not wanted, with a body of {"monitored": true} or {"monitored": false}.
func (s *Server) handleSetTrackMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "track")
	if !ok {
		return
	}
	var req monitoredRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	err := s.MusicRepo.SetTrackMonitored(id, req.Monitored)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That track isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// wantAllTracks makes the whole album wanted again.
func (s *Server) wantAllTracks(albumID int64) {
	if err := s.MusicRepo.SetOnlySong(albumID, ""); err != nil {
		slog.Warn("music: clear the wanted song", "album", albumID, "err", err)
		return
	}
	tracks, err := s.MusicRepo.ListTracks(albumID)
	if err != nil {
		return
	}
	for _, t := range tracks {
		if !t.Monitored {
			_ = s.MusicRepo.SetTrackMonitored(t.ID, true)
		}
	}
}
