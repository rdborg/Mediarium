package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/musicbrainz"
)

// Music library: finding artists on MusicBrainz, adding them with their
// albums, browsing, monitoring and removing. Searching for and grabbing
// albums is in music_search.go, the download pipeline in pipeline_music.go.

// musicArtistResultPayload is one MusicBrainz artist search result.
type musicArtistResultPayload struct {
	MBID           string `json:"mbid"`
	Name           string `json:"name"`
	SortName       string `json:"sortName"`
	Disambiguation string `json:"disambiguation,omitempty"`
	Type           string `json:"type,omitempty"`
	Country        string `json:"country,omitempty"`
	Score          int    `json:"score"`
	// ArtistID is the library id when the artist is already added, else 0.
	ArtistID int64 `json:"artistId"`
}

// musicArtistPayload is an artist in the library.
type musicArtistPayload struct {
	ID              int64    `json:"id"`
	MBID            string   `json:"mbid"`
	Name            string   `json:"name"`
	SortName        string   `json:"sortName"`
	Disambiguation  string   `json:"disambiguation,omitempty"`
	Monitored       bool     `json:"monitored"`
	MonitorNew      bool     `json:"monitorNew"`
	ProfileID       int64    `json:"profileId"` // 0 = the default profile
	ProfileName     string   `json:"profileName"`
	AddedAt         string   `json:"addedAt"`
	AddedBy         *userRef `json:"addedBy"`
	CoverURL        string   `json:"coverUrl"` // a picture of the artist on this server: GET /api/music/artists/{id}/cover
	ImageURL        string   `json:"imageUrl"` // the same address, under the name the artist cards use
	AlbumCount      int      `json:"albumCount"`
	MonitoredCount  int      `json:"monitoredCount"`
	DownloadedCount int      `json:"downloadedCount"`
	// Albums is only set on GET /api/music/artists/{id} and the add answer.
	Albums []musicAlbumPayload `json:"albums,omitempty"`
}

// musicAlbumPayload is one album, EP or single.
type musicAlbumPayload struct {
	ID              int64  `json:"id"`
	ArtistID        int64  `json:"artistId"`
	ArtistName      string `json:"artistName,omitempty"`
	MBID            string `json:"mbid"`
	Title           string `json:"title"`
	Type            string `json:"type"` // album, ep or single
	ReleaseDate     string `json:"releaseDate"`
	Year            int    `json:"year"`
	Monitored       bool   `json:"monitored"`
	Status          string `json:"status"` // missing, downloading or downloaded
	Quality         string `json:"quality,omitempty"`
	Path            string `json:"path,omitempty"`
	CoverURL        string `json:"coverUrl"`        // the cover on this server: GET /api/music/albums/{id}/cover
	CoverArchiveURL string `json:"coverArchiveUrl"` // the same cover at the Cover Art Archive
	// Tracks is only set on GET /api/music/albums/{id} (empty until the
	// tracklist has been fetched, which happens on the first grab or import).
	Tracks []musicTrackPayload `json:"tracks,omitempty"`
}

type musicTrackPayload struct {
	ID       int64  `json:"id"`
	Disc     int    `json:"disc"`
	Position int    `json:"position"`
	Title    string `json:"title"`
	LengthMs int    `json:"lengthMs"`
	HasFile  bool   `json:"hasFile"`
	FilePath string `json:"filePath,omitempty"`
}

func toMusicAlbumPayload(a music.Album) musicAlbumPayload {
	return musicAlbumPayload{
		ID: a.ID, ArtistID: a.ArtistID, MBID: a.MBID, Title: a.Title, Type: a.Type, ReleaseDate: a.ReleaseDate,
		Year: a.Year(), Monitored: a.Monitored, Status: string(a.Status), Quality: a.Quality, Path: a.Path,
		CoverURL: albumCoverURL(a.ID), CoverArchiveURL: musicbrainz.CoverArtURL(a.MBID),
	}
}

func (s *Server) toMusicArtistPayload(a music.Artist, profiles []music.Profile, who func(int64) *userRef) musicArtistPayload {
	p, _ := s.resolveMusicProfile(profiles, a.ProfileID)
	return musicArtistPayload{
		ID: a.ID, MBID: a.MBID, Name: a.Name, SortName: a.SortName, Disambiguation: a.Disambiguation,
		Monitored: a.Monitored, MonitorNew: a.MonitorNew, ProfileID: a.ProfileID, ProfileName: p.Name,
		AddedAt: a.AddedAt, AddedBy: who(a.AddedBy), CoverURL: artistCoverURL(a.ID), ImageURL: artistCoverURL(a.ID),
	}
}

// musicID parses the {id} path value.
func musicID(w http.ResponseWriter, r *http.Request, what string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't a valid "+what+" ID.")
		return 0, false
	}
	return id, true
}

// loadAlbum looks up the {id} album and its artist, answering 404/500 itself.
func (s *Server) loadAlbum(w http.ResponseWriter, r *http.Request) (music.Album, music.Artist, bool) {
	id, ok := musicID(w, r, "album")
	if !ok {
		return music.Album{}, music.Artist{}, false
	}
	album, err := s.MusicRepo.GetAlbum(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That album isn't in your library.")
		return music.Album{}, music.Artist{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return music.Album{}, music.Artist{}, false
	}
	artist, err := s.MusicRepo.GetArtist(album.ArtistID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return music.Album{}, music.Artist{}, false
	}
	return album, artist, true
}

// musicBrainzError answers a failed MusicBrainz call: 404 for an unknown
// id, 502 for anything else (the service is down or busy).
func musicBrainzError(w http.ResponseWriter, err error, notFound string) {
	if errors.Is(err, musicbrainz.ErrNotFound) {
		writeError(w, http.StatusNotFound, notFound)
		return
	}
	writeUpstreamError(w, "reach the music database", err)
}

// handleMusicSearchArtists finds artists on MusicBrainz by name, marking
// the ones already in the library.
func (s *Server) handleMusicSearchArtists(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	found, err := s.MusicBrainz().SearchArtists(r.Context(), q, 25)
	if err != nil {
		musicBrainzError(w, err, "nothing found")
		return
	}
	out := make([]musicArtistResultPayload, len(found))
	for i, a := range found {
		out[i] = musicArtistResultPayload{MBID: a.ID, Name: a.Name, SortName: a.SortName, Disambiguation: a.Disambiguation, Type: a.Type, Country: a.Country, Score: a.Score}
		if existing, ok, err := s.MusicRepo.GetArtistByMBID(a.ID); err == nil && ok {
			out[i].ArtistID = existing.ID
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// Monitoring choices when adding an artist.
const (
	monitorAll    = "all"    // every album, EP and single
	monitorFuture = "future" // only releases that are not out yet, and new ones found later
	monitorNone   = "none"   // nothing; the artist is only listed
)

// artistID is the shape of a music database ID: a UUID.
var artistID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type addArtistRequest struct {
	MBID      string `json:"mbid"`
	Monitor   string `json:"monitor"`   // all (default), future or none
	ProfileID int64  `json:"profileId"` // 0 = the default music profile
	SearchNow bool   `json:"searchNow"` // search for the monitored albums straight away
}

// handleAddArtist adds an artist from MusicBrainz with its albums, EPs and
// singles (studio releases; live albums and compilations are left out), and
// monitors them as asked: all, only future releases, or none.
func (s *Server) handleAddArtist(w http.ResponseWriter, r *http.Request) {
	if s.mustRequest(r) {
		s.fileRequest(w, r, "music")
		return
	}
	var req addArtistRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.MBID) == "" {
		writeError(w, http.StatusBadRequest, "mbid is required")
		return
	}
	req.MBID = strings.TrimSpace(req.MBID)
	if !artistID.MatchString(req.MBID) {
		writeError(w, http.StatusBadRequest, "That artist ID doesn't look right. Search for the artist and pick them from the results.")
		return
	}
	if req.Monitor == "" {
		req.Monitor = monitorAll
	}
	if req.Monitor != monitorAll && req.Monitor != monitorFuture && req.Monitor != monitorNone {
		writeError(w, http.StatusBadRequest, `monitor must be "all", "future" or "none"`)
		return
	}
	profiles, err := s.MusicRepo.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.ProfileID != 0 {
		if p, _ := s.resolveMusicProfile(profiles, req.ProfileID); p.ID != req.ProfileID {
			writeError(w, http.StatusBadRequest, "There's no music profile with that ID.")
			return
		}
	}
	if existing, ok, err := s.MusicRepo.GetArtistByMBID(req.MBID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if ok {
		writeError(w, http.StatusConflict, existing.Name+" is already in your library")
		return
	}

	userID, byline := requester(r)
	artist, albums, err := s.addArtistFromMusicBrainz(r.Context(), req.MBID, req.Monitor, req.ProfileID, userID)
	if errors.Is(err, music.ErrArtistExists) {
		writeError(w, http.StatusConflict, "That artist is already in your library.")
		return
	}
	if err != nil {
		musicBrainzError(w, err, "Couldn't find that artist in the music database.")
		return
	}
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("%s added to the music library with %s%s", artist.Name, plural(len(albums), "release"), byline))
	if req.SearchNow {
		s.background(func() { s.searchArtistAlbums(artist.ID) })
	}
	payload := s.toMusicArtistPayload(artist, profiles, s.accountNames())
	payload.Albums = make([]musicAlbumPayload, 0, len(albums))
	for _, a := range albums {
		payload.AlbumCount++
		if a.Monitored {
			payload.MonitoredCount++
		}
		payload.Albums = append(payload.Albums, toMusicAlbumPayload(a))
	}
	writeJSON(w, http.StatusCreated, payload)
}

// addArtistFromMusicBrainz looks an artist and its release groups up and
// adds them. monitor is all, future or none.
func (s *Server) addArtistFromMusicBrainz(ctx context.Context, mbid, monitor string, profileID, userID int64) (music.Artist, []music.Album, error) {
	mb := s.MusicBrainz()
	a, err := mb.GetArtist(ctx, mbid)
	if err != nil {
		return music.Artist{}, nil, err
	}
	groups, err := mb.ArtistReleaseGroups(ctx, mbid, nil, false)
	if err != nil {
		return music.Artist{}, nil, err
	}
	today := time.Now().UTC().Format("2006-01-02")
	albums := make([]music.Album, 0, len(groups))
	for _, g := range groups {
		albums = append(albums, music.Album{
			MBID: g.ID, Title: g.Title, Type: albumType(g.PrimaryType), ReleaseDate: g.FirstReleaseDate,
			Monitored: monitor == monitorAll || (monitor == monitorFuture && !albumReleased(g.FirstReleaseDate, today)),
		})
	}
	return s.MusicRepo.AddArtist(music.Artist{
		MBID: a.ID, Name: a.Name, SortName: a.SortName, Disambiguation: a.Disambiguation,
		Monitored: monitor != monitorNone, MonitorNew: monitor != monitorNone, ProfileID: profileID, AddedBy: userID,
	}, albums)
}

// albumType maps a MusicBrainz primary type onto album, ep or single.
func albumType(primary string) string {
	switch strings.ToLower(primary) {
	case "ep":
		return music.TypeEP
	case "single":
		return music.TypeSingle
	}
	return music.TypeAlbum
}

// albumReleased reports whether a release date ("YYYY[-MM[-DD]]") is today
// or earlier. An unknown date counts as not released yet (MusicBrainz lists
// announced albums without one).
func albumReleased(date, today string) bool {
	return date != "" && date <= today
}

// handleListArtists lists the artists in the library with album counts.
func (s *Server) handleListArtists(w http.ResponseWriter, r *http.Request) {
	list, err := s.MusicRepo.ListArtists()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profiles, err := s.MusicRepo.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	who := s.accountNames()
	out := make([]musicArtistPayload, len(list))
	for i, a := range list {
		out[i] = s.toMusicArtistPayload(a.Artist, profiles, who)
		out[i].AlbumCount, out[i].MonitoredCount, out[i].DownloadedCount = a.Albums, a.Monitored, a.Downloaded
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetArtist returns one artist with its albums, EPs and singles.
func (s *Server) handleGetArtist(w http.ResponseWriter, r *http.Request) {
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
	albums, err := s.MusicRepo.ListAlbums(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profiles, err := s.MusicRepo.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := s.toMusicArtistPayload(artist, profiles, s.accountNames())
	out.Albums = make([]musicAlbumPayload, 0, len(albums))
	for _, a := range albums {
		out.AlbumCount++
		if a.Monitored {
			out.MonitoredCount++
		}
		if a.Status == music.StatusDownloaded {
			out.DownloadedCount++
		}
		out.Albums = append(out.Albums, toMusicAlbumPayload(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetAlbum returns one album with its tracklist and which tracks have
// a file.
func (s *Server) handleGetAlbum(w http.ResponseWriter, r *http.Request) {
	album, artist, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	tracks, err := s.MusicRepo.ListTracks(album.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := toMusicAlbumPayload(album)
	out.ArtistName = artist.Name
	out.Tracks = make([]musicTrackPayload, len(tracks))
	for i, t := range tracks {
		out.Tracks[i] = musicTrackPayload{ID: t.ID, Disc: t.Disc, Position: t.Position, Title: t.Title, LengthMs: t.LengthMs, HasFile: t.FilePath != "", FilePath: t.FilePath}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSetAlbumMonitored switches monitoring on or off for one album, with a
// body of {"monitored": true} or {"monitored": false}.
func (s *Server) handleSetAlbumMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "album")
	if !ok {
		return
	}
	var req monitoredRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	err := s.MusicRepo.SetAlbumMonitored(id, req.Monitored)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That album isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// handleAlbumEvents lists an album's own activity log, newest first (at most
// 200 events): searches and why nothing was taken, grabs, download steps,
// imports and failures.
func (s *Server) handleAlbumEvents(w http.ResponseWriter, r *http.Request) {
	album, _, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	events, err := s.QueueRepo.AlbumEvents(album.ID, maxItemEvents)
	writeItemEvents(w, events, err)
}

// handleDeleteArtist removes an artist and its albums from the library. Its
// downloads are always cancelled and their working folders deleted; with
// ?deleteFiles=true the album folders in the music library go too (only
// folders inside the music folder, never anything outside it).
func (s *Server) handleDeleteArtist(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "artist")
	if !ok {
		return
	}
	if _, err := s.removeArtist(id, r.URL.Query().Get("deleteFiles") == "true"); err != nil {
		writeRemoveError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// removeArtist takes one artist out of the music library. Their downloads
// are cancelled either way; the album folders are deleted only when
// deleteFiles is set. It returns the artist's name; the error is a
// *removeProblem when the person can be told what went wrong.
func (s *Server) removeArtist(id int64, deleteFiles bool) (string, error) {
	artist, err := s.MusicRepo.GetArtist(id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", &removeProblem{http.StatusNotFound, "That artist isn't in your library."}
	}
	if err != nil {
		return "", &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	albums, err := s.MusicRepo.ListAlbums(id)
	if err != nil {
		return artist.Name, &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	root := s.musicRoot()
	var folders []string
	if deleteFiles {
		for _, a := range albums {
			if a.Path != "" {
				folders = append(folders, a.Path)
			}
		}
		if err := checkRemovable(root, folders); err != nil {
			return artist.Name, &removeProblem{http.StatusConflict, err.Error()}
		}
	}
	albumIDs := map[int64]bool{}
	for _, a := range albums {
		albumIDs[a.ID] = true
	}
	if err := s.removeAlbumDownloads(albumIDs, artist.Name); err != nil {
		return artist.Name, &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	for _, f := range folders {
		if _, err := s.discardFolder(root, f, artist.Name+" - "+filepath.Base(f)); err != nil {
			return artist.Name, &removeProblem{http.StatusInternalServerError, err.Error()}
		}
		cleanup.PruneEmptyFolders(root, filepath.Dir(f)) // the artist folder, once empty
	}
	if err := s.MusicRepo.DeleteArtist(id); err != nil {
		return artist.Name, &removeProblem{http.StatusInternalServerError, err.Error()}
	}
	msg := artist.Name + " removed from the music library"
	if len(folders) > 0 {
		msg += fmt.Sprintf(", with %s", plural(len(folders), "album folder"))
	}
	_ = s.QueueRepo.LogActivity(0, "removed", msg)
	return artist.Name, nil
}

// removeAlbumDownloads cancels and forgets every download of the given
// albums, deleting their working folders (like removeDownloadsFor for
// movies and shows).
func (s *Server) removeAlbumDownloads(albumIDs map[int64]bool, title string) error {
	items, err := s.QueueRepo.List()
	if err != nil {
		return err
	}
	cancelled := 0
	for _, it := range items {
		if it.AlbumID == 0 || !albumIDs[it.AlbumID] {
			continue
		}
		running := s.pipelines.cancel(it.ID, cancelWait)
		if s.torrents.stopQueue(it.ID) {
			running = true
		}
		if running || isActiveStatus(it.Status) {
			cancelled++
		}
		s.removeWorkDir(s.workDirFor(it.ID))
		_ = s.QueueRepo.Delete(it.ID)
	}
	if cancelled > 0 {
		_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("Cancelled %s of %s and removed their files from the downloads folder", plural(cancelled, "download"), title))
	}
	return nil
}

// musicWantedPayload is one album automation is still looking for.
type musicWantedPayload struct {
	musicAlbumPayload
	ProfileName  string `json:"profileName"`
	Cutoff       string `json:"cutoff,omitempty"`
	LastSearch   string `json:"lastSearch,omitempty"`
	LastSearchAt string `json:"lastSearchAt,omitempty"`
}

// handleMusicWanted lists what automation is still looking for (?kind=):
// "missing" (default: monitored albums of monitored artists, out already,
// nothing downloaded) or "cutoff" (downloaded below the profile cutoff, or
// only on a fallback profile).
func (s *Server) handleMusicWanted(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "missing"
	}
	if kind != "missing" && kind != "cutoff" {
		writeError(w, http.StatusBadRequest, `kind must be "missing" or "cutoff"`)
		return
	}
	wanted, err := s.wantedAlbums(kind == "cutoff")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]musicWantedPayload, 0, len(wanted))
	for _, wa := range wanted {
		p := musicWantedPayload{musicAlbumPayload: toMusicAlbumPayload(wa.album), ProfileName: wa.profile.Name}
		p.ArtistName = wa.artist.Name
		if kind == "cutoff" {
			p.Cutoff = string(wa.profile.Cutoff)
		}
		if events, err := s.QueueRepo.AlbumEvents(wa.album.ID, 40); err == nil {
			p.LastSearch, p.LastSearchAt = lastSearch(events)
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

// wantedAlbum is an album automation looks for, with its artist and profile.
type wantedAlbum struct {
	album   music.Album
	artist  music.Artist
	profile music.Profile
}

// wantedAlbums lists the monitored albums that are
// missing (and out already) or, with upgrades set, downloaded but wanting
// an upgrade.
func (s *Server) wantedAlbums(upgrades bool) ([]wantedAlbum, error) {
	artists, err := s.MusicRepo.ListArtists()
	if err != nil {
		return nil, err
	}
	profiles, err := s.MusicRepo.ListProfiles()
	if err != nil {
		return nil, err
	}
	byID := map[int64]music.Artist{}
	for _, a := range artists {
		byID[a.ID] = a.Artist
	}
	albums, err := s.MusicRepo.ListAllAlbums()
	if err != nil {
		return nil, err
	}
	today := time.Now().UTC().Format("2006-01-02")
	var out []wantedAlbum
	for _, al := range albums {
		// The album's own flag decides; the artist's only says whether new
		// releases are picked up (see handleUpdateArtist).
		artist, ok := byID[al.ArtistID]
		if !ok || !al.Monitored {
			continue
		}
		profile, ok := s.resolveMusicProfile(profiles, artist.ProfileID)
		if !ok {
			slog.Warn("music: no quality profiles")
			return nil, nil
		}
		switch {
		case !upgrades && al.Status == music.StatusMissing && albumReleased(al.ReleaseDate, today):
		case upgrades && al.Status == music.StatusDownloaded && profile.WantsUpgrade(music.Tier(al.Quality)):
		default:
			continue
		}
		out = append(out, wantedAlbum{album: al, artist: artist, profile: profile})
	}
	return out, nil
}
