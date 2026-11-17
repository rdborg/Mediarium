package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/origin"
)

// The Lidarr part of the arr-style API (see arr.go): artists and albums,
// enough for dashboards and phone apps to show the music library and add
// an artist.

type arrArtistStats struct {
	AlbumCount      int     `json:"albumCount"`
	TrackFileCount  int     `json:"trackFileCount"`
	TrackCount      int     `json:"trackCount"`
	TotalTrackCount int     `json:"totalTrackCount"`
	SizeOnDisk      int64   `json:"sizeOnDisk"`
	PercentOfTracks float64 `json:"percentOfTracks"`
}

type arrLink struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

type arrArtist struct {
	ID                int64           `json:"id,omitempty"`
	ArtistName        string          `json:"artistName"`
	ForeignArtistID   string          `json:"foreignArtistId"`
	SortName          string          `json:"sortName"`
	CleanName         string          `json:"cleanName"`
	TitleSlug         string          `json:"titleSlug"`
	Disambiguation    string          `json:"disambiguation"`
	Overview          string          `json:"overview"`
	ArtistType        string          `json:"artistType"`
	Status            string          `json:"status"`
	Ended             bool            `json:"ended"`
	Monitored         bool            `json:"monitored"`
	MonitorNewItems   string          `json:"monitorNewItems"`
	QualityProfileID  int64           `json:"qualityProfileId"`
	MetadataProfileID int             `json:"metadataProfileId"`
	Path              string          `json:"path,omitempty"`
	RootFolderPath    string          `json:"rootFolderPath,omitempty"`
	Added             string          `json:"added"`
	Tags              []int64         `json:"tags"`
	Genres            []string        `json:"genres"`
	Images            []arrImage      `json:"images"`
	Links             []arrLink       `json:"links"`
	Statistics        *arrArtistStats `json:"statistics,omitempty"`
}

type arrAlbum struct {
	ID             int64           `json:"id"`
	Title          string          `json:"title"`
	ForeignAlbumID string          `json:"foreignAlbumId"`
	ArtistID       int64           `json:"artistId"`
	AlbumType      string          `json:"albumType"`
	ReleaseDate    string          `json:"releaseDate,omitempty"`
	Monitored      bool            `json:"monitored"`
	AnyReleaseOk   bool            `json:"anyReleaseOk"`
	ProfileID      int64           `json:"profileId"`
	Duration       int             `json:"duration"`
	Grabbed        bool            `json:"grabbed"`
	Genres         []string        `json:"genres"`
	Images         []arrImage      `json:"images"`
	Links          []arrLink       `json:"links"`
	Statistics     *arrArtistStats `json:"statistics,omitempty"`
	Artist         *arrArtist      `json:"artist,omitempty"`
}

func coverImages(releaseGroupMBID string) []arrImage {
	if releaseGroupMBID == "" {
		return []arrImage{}
	}
	u := "https://coverartarchive.org/release-group/" + releaseGroupMBID + "/front-250"
	return []arrImage{{CoverType: "cover", URL: u, RemoteURL: u}}
}

func toArrArtist(a music.Artist, albums, downloaded int) arrArtist {
	monitorNew := "none"
	if a.MonitorNew {
		monitorNew = "all"
	}
	added := arrTime(a.AddedAt)
	if added == "" {
		added = "2000-01-01T00:00:00Z"
	}
	return arrArtist{
		ID: a.ID, ArtistName: a.Name, ForeignArtistID: a.MBID, SortName: a.SortName, CleanName: arrCleanTitle(a.Name),
		TitleSlug: arrSlug(a.Name, int(a.ID)), Disambiguation: a.Disambiguation, Status: "continuing", Monitored: a.Monitored,
		MonitorNewItems: monitorNew, QualityProfileID: a.ProfileID, MetadataProfileID: 1, Added: added, Tags: []int64{},
		Genres: []string{}, Images: []arrImage{}, Links: []arrLink{{URL: "https://musicbrainz.org/artist/" + a.MBID, Name: "musicbrainz"}},
		Statistics: &arrArtistStats{AlbumCount: albums, TrackFileCount: downloaded, TrackCount: downloaded, TotalTrackCount: albums,
			PercentOfTracks: arrPercent(downloaded, albums)},
	}
}

func albumTypeName(t string) string {
	switch strings.ToLower(t) {
	case "ep":
		return "EP"
	case "single":
		return "Single"
	}
	return "Album"
}

func (s *Server) toArrAlbum(a music.Album, artist *music.Artist) arrAlbum {
	out := arrAlbum{
		ID: a.ID, Title: a.Title, ForeignAlbumID: a.MBID, ArtistID: a.ArtistID, AlbumType: albumTypeName(a.Type),
		ReleaseDate: arrTime(a.ReleaseDate), Monitored: a.Monitored, AnyReleaseOk: true, Grabbed: a.Status == music.StatusDownloading,
		Genres: []string{}, Images: coverImages(a.MBID), Links: []arrLink{{URL: "https://musicbrainz.org/release-group/" + a.MBID, Name: "musicbrainz"}},
	}
	have := 0
	if a.Status == music.StatusDownloaded {
		have = 1
	}
	out.Statistics = &arrArtistStats{TrackFileCount: have, TrackCount: have, TotalTrackCount: 1, PercentOfTracks: float64(have * 100)}
	if artist != nil {
		out.ProfileID = artist.ProfileID
		ar := toArrArtist(*artist, 0, 0)
		ar.Statistics = nil
		out.Artist = &ar
	}
	return out
}

// arrMusic is every album and the artists by id.
func (s *Server) arrMusic() ([]music.Album, map[int64]music.Artist, error) {
	albums, err := s.MusicRepo.ListAllAlbums()
	if err != nil {
		return nil, nil, err
	}
	list, err := s.MusicRepo.ListArtists()
	if err != nil {
		return nil, nil, err
	}
	artists := map[int64]music.Artist{}
	for _, a := range list {
		artists[a.ID] = a.Artist
	}
	return albums, artists, nil
}

func (s *Server) arrListArtists(w http.ResponseWriter, r *http.Request) {
	list, err := s.MusicRepo.ListArtists()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []arrArtist{}
	for _, a := range list {
		out = append(out, toArrArtist(a.Artist, a.Albums, a.Downloaded))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].SortName) < strings.ToLower(out[j].SortName) })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) arrGetArtist(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	list, err := s.MusicRepo.ListArtists()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, a := range list {
		if a.ID == id {
			writeJSON(w, http.StatusOK, toArrArtist(a.Artist, a.Albums, a.Downloaded))
			return
		}
	}
	arrNotFound(w)
}

// arrArtistLookup answers ?term=lidarr:<MusicBrainz id> or a name.
func (s *Server) arrArtistLookup(w http.ResponseWriter, r *http.Request) {
	kind, value := arrQueryTerm(r)
	if value == "" {
		writeJSON(w, http.StatusOK, []arrArtist{})
		return
	}
	if kind == "lidarr" || kind == "mbid" {
		if a, ok, err := s.MusicRepo.GetArtistByMBID(value); err == nil && ok {
			writeJSON(w, http.StatusOK, []arrArtist{toArrArtist(a, 0, 0)})
			return
		}
		mb, err := s.MusicBrainz().GetArtist(r.Context(), value)
		if err != nil {
			writeJSON(w, http.StatusOK, []arrArtist{})
			return
		}
		writeJSON(w, http.StatusOK, []arrArtist{toArrArtist(music.Artist{MBID: mb.ID, Name: mb.Name, SortName: mb.SortName, Disambiguation: mb.Disambiguation}, 0, 0)})
		return
	}
	found, err := s.MusicBrainz().SearchArtists(r.Context(), value, 25)
	if err != nil {
		writeUpstreamError(w, "search for artists", err)
		return
	}
	out := []arrArtist{}
	for _, mb := range found {
		if a, ok, err := s.MusicRepo.GetArtistByMBID(mb.ID); err == nil && ok {
			out = append(out, toArrArtist(a, 0, 0))
			continue
		}
		out = append(out, toArrArtist(music.Artist{MBID: mb.ID, Name: mb.Name, SortName: mb.SortName, Disambiguation: mb.Disambiguation}, 0, 0))
	}
	writeJSON(w, http.StatusOK, out)
}

// arrAddArtist adds an artist by its MusicBrainz id (foreignArtistId).
func (s *Server) arrAddArtist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ForeignArtistID  string `json:"foreignArtistId"`
		QualityProfileID int64  `json:"qualityProfileId"`
		Monitored        *bool  `json:"monitored"`
		AddOptions       *struct {
			Monitor                string `json:"monitor"`
			SearchForMissingAlbums bool   `json:"searchForMissingAlbums"`
		} `json:"addOptions"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.ForeignArtistID) == "" {
		arrBadRequest(w, "ForeignArtistId", "An artist needs its MusicBrainz id (foreignArtistId).")
		return
	}
	if _, ok, err := s.MusicRepo.GetArtistByMBID(req.ForeignArtistID); err == nil && ok {
		arrAlreadyAdded(w, "ForeignArtistId", "This artist has already been added", "ArtistExistsValidator")
		return
	}
	monitor := monitorAll
	if req.AddOptions != nil {
		switch strings.ToLower(req.AddOptions.Monitor) {
		case "future":
			monitor = monitorFuture
		case "none":
			monitor = monitorNone
		}
	}
	if req.Monitored != nil && !*req.Monitored {
		monitor = monitorNone
	}
	profileID := req.QualityProfileID
	if profileID > 0 {
		if _, err := s.MusicRepo.GetProfile(profileID); err != nil {
			profileID = 0
		}
	}
	userID, _ := requester(r)
	artist, albums, err := s.addArtistFromMusicBrainz(r.Context(), req.ForeignArtistID, monitor, profileID, userID, origin.Origin{Kind: origin.API})
	if err != nil {
		writeUpstreamError(w, "add that artist", err)
		return
	}
	if req.AddOptions != nil && req.AddOptions.SearchForMissingAlbums && monitor != monitorNone {
		id := artist.ID
		s.background(func() { s.searchArtistAlbums(context.Background(), id) })
	}
	writeJSON(w, http.StatusCreated, toArrArtist(artist, len(albums), 0))
}

func (s *Server) arrListAlbums(w http.ResponseWriter, r *http.Request) {
	artistID, _ := strconv.ParseInt(r.URL.Query().Get("artistId"), 10, 64)
	if artistID == 0 {
		artistID, _ = strconv.ParseInt(r.URL.Query().Get("artistid"), 10, 64)
	}
	albums, artists, err := s.arrMusic()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []arrAlbum{}
	for _, a := range albums {
		if artistID != 0 && a.ArtistID != artistID {
			continue
		}
		ar := artists[a.ArtistID]
		out = append(out, s.toArrAlbum(a, &ar))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) arrGetAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	a, err := s.MusicRepo.GetAlbum(id)
	if errors.Is(err, sql.ErrNoRows) {
		arrNotFound(w)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var artist *music.Artist
	if ar, err := s.MusicRepo.GetArtist(a.ArtistID); err == nil {
		artist = &ar
	}
	writeJSON(w, http.StatusOK, s.toArrAlbum(a, artist))
}
