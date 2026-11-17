package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/listenbrainz"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/origin"
)

// Adding a book, an audiobook or an album by hand, for when the metadata
// sources don't have it or find the wrong one. The entry is searched for,
// downloaded and imported like a matched one. Its id starts with "manual:", and
// everything that would ask a metadata source about an entry (refreshing an
// artist, a series, an audiobook's details, a tracklist) leaves it alone, so
// what was typed here is never replaced.

const (
	maxManualText = 300
	minManualYear = 1000
)

// manualYearOK accepts no year, or a believable one (a little ahead of today for announced releases).
func manualYearOK(year int) bool {
	return year == 0 || (year >= minManualYear && year <= time.Now().Year()+5)
}

// manualBlank gives v for an ordinary id and "" for one added by hand, for
// links that would point at a catalogue entry that isn't there.
func manualBlank(id, v string) string {
	if music.IsManualID(id) {
		return ""
	}
	return v
}

// manualTopRecordings is the most played recordings of an artist; an artist
// added by hand has no id the listening service knows.
func (s *Server) manualTopRecordings(ctx context.Context, artist music.Artist) ([]listenbrainz.RecordingStat, error) {
	if artist.Manual() {
		return nil, errors.New("added by hand")
	}
	return s.discoverClient().TopRecordings(ctx, artist.MBID)
}

type manualBookRequest struct {
	Title       string `json:"title"`
	Author      string `json:"author"`
	Year        int    `json:"year"`
	Description string `json:"description"`
	ExternalID  string `json:"externalId"` // an ISBN, an ASIN or any id of your own; optional
	Ebook       bool   `json:"ebook"`
	Audiobook   bool   `json:"audiobook"`
	SearchNow   bool   `json:"searchNow"`
}

// handleAddBookManual is POST /api/books/manual: a book, an audiobook or both,
// typed in by hand. The cover can be added afterwards with POST
// /api/books/{id}/cover.
func (s *Server) handleAddBookManual(w http.ResponseWriter, r *http.Request) {
	if !s.mayAddNew(w, r, auth.PermBooks) {
		return
	}
	var req manualBookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That didn't come through right. Try again.")
		return
	}
	req.Title, req.Author = strings.TrimSpace(req.Title), strings.TrimSpace(req.Author)
	req.Description = strings.TrimSpace(req.Description)
	switch {
	case req.Title == "":
		writeError(w, http.StatusBadRequest, "Type the title of the book.")
		return
	case len([]rune(req.Title)) > maxManualText || len([]rune(req.Author)) > maxManualText:
		writeError(w, http.StatusBadRequest, "The title or the author is too long.")
		return
	case len([]rune(req.Description)) > 5000:
		writeError(w, http.StatusBadRequest, "The description is too long.")
		return
	case !manualYearOK(req.Year):
		writeError(w, http.StatusBadRequest, "That year doesn't look right.")
		return
	case !req.Ebook && !req.Audiobook:
		writeError(w, http.StatusBadRequest, "Choose ebook, audiobook or both.")
		return
	case (req.Ebook && !s.ebooksEnabled()) || (req.Audiobook && !s.audiobooksEnabled()):
		writeError(w, http.StatusConflict, "That format is switched off. Switch it on in Settings > Media types.")
		return
	}
	key, err := books.NewManualKey(req.ExternalID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	library, err := s.BookRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, have := range library {
		if books.SameTitle(have.Title, req.Title, true) && books.SameAuthor(have.Author, req.Author) {
			writeError(w, http.StatusConflict, fmt.Sprintf("%s is already in your library.", have.Title))
			return
		}
	}
	userID, byline := requester(r)
	created, err := s.BookRepo.Add(books.Book{OLKey: key, Title: req.Title, Author: req.Author, Year: req.Year, Description: req.Description,
		WantEbook: req.Ebook, WantAudiobook: req.Audiobook, AddedBy: userID, Origin: addOrigin(r)})
	if errors.Is(err, books.ErrExists) {
		writeError(w, http.StatusConflict, fmt.Sprintf("A book with the id %s is already in your library.", strings.TrimPrefix(key, books.ManualKeyPrefix)))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("%s by %s added to the library by hand%s", created.Name(), created.Author, byline))
	if req.SearchNow {
		s.searchNewBook(created, userOrigin(r, origin.Search))
	}
	writeJSON(w, http.StatusCreated, toBookPayload(created, isAdminRequest(r)))
}

type manualMusicRequest struct {
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	Year       int    `json:"year"`
	Type       string `json:"type"`       // album (default), ep or single
	ExternalID string `json:"externalId"` // a catalogue number or any id of your own; optional
	SearchNow  bool   `json:"searchNow"`
}

type manualMusicResponse struct {
	ArtistID  int64             `json:"artistId"`
	AlbumID   int64             `json:"albumId"`
	Album     musicAlbumPayload `json:"album"`
	NewArtist bool              `json:"newArtist"`
}

// handleAddMusicManual is POST /api/music/manual: an album typed in by hand.
// It goes to the artist of that name when the library has one, otherwise a new
// artist is made for it. The cover can be added afterwards with POST
// /api/music/albums/{id}/cover.
func (s *Server) handleAddMusicManual(w http.ResponseWriter, r *http.Request) {
	if !s.mayAddNew(w, r, auth.PermMusic) {
		return
	}
	var req manualMusicRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That didn't come through right. Try again.")
		return
	}
	req.Artist, req.Album = strings.TrimSpace(req.Artist), strings.TrimSpace(req.Album)
	switch {
	case req.Artist == "":
		writeError(w, http.StatusBadRequest, "Type the name of the artist.")
		return
	case req.Album == "":
		writeError(w, http.StatusBadRequest, "Type the title of the album.")
		return
	case len([]rune(req.Artist)) > maxManualText || len([]rune(req.Album)) > maxManualText:
		writeError(w, http.StatusBadRequest, "The artist or the album title is too long.")
		return
	case !manualYearOK(req.Year):
		writeError(w, http.StatusBadRequest, "That year doesn't look right.")
		return
	}
	switch req.Type {
	case "":
		req.Type = music.TypeAlbum
	case music.TypeAlbum, music.TypeEP, music.TypeSingle:
	default:
		writeError(w, http.StatusBadRequest, "The type has to be album, ep or single.")
		return
	}
	albumID, err := music.NewManualID(req.ExternalID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	date := ""
	if req.Year > 0 {
		date = fmt.Sprintf("%04d", req.Year)
	}
	userID, byline := requester(r)
	how := addOrigin(r)
	album := music.Album{MBID: albumID, Title: req.Album, Type: req.Type, ReleaseDate: date, Monitored: true,
		Origin: origin.Origin{Kind: how.Kind, Name: how.Name, UserID: userID}}

	artist, found, err := s.MusicRepo.FindArtistByName(req.Artist)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if found {
		// Adding an album to an artist already in the library is what POST
		// /api/music/artists/{id}/albums does, and that needs the manage
		// permission too.
		if !s.mayManage(r) {
			writeError(w, http.StatusForbidden, "That artist is already in your library. Adding an album to an artist you have needs the Search now and monitoring permission, so ask an administrator.")
			return
		}
		have, err := s.MusicRepo.ListAlbums(artist.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, a := range have {
			if music.NormalizeName(a.Title) == music.NormalizeName(req.Album) {
				writeError(w, http.StatusConflict, fmt.Sprintf("%s by %s is already in your library.", a.Title, artist.Name))
				return
			}
		}
		added, err := s.MusicRepo.AddAlbums(artist.ID, []music.Album{album})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(added) == 0 {
			writeError(w, http.StatusConflict, "An album with that id is already in your library.")
			return
		}
		album = added[0]
	} else {
		artistKey, err := music.NewManualID("")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a, added, err := s.MusicRepo.AddArtist(music.Artist{MBID: artistKey, Name: req.Artist, SortName: req.Artist, Monitored: true, MonitorNew: false, AddedBy: userID, Origin: how}, []music.Album{album})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(added) == 0 {
			writeError(w, http.StatusConflict, "An album with that id is already in your library.")
			return
		}
		artist, album = a, added[0]
	}
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("%s by %s added to the music library by hand%s", album.Title, artist.Name, byline))
	if req.SearchNow {
		by := userOrigin(r, origin.Search)
		s.background(func() {
			instances, err := s.searchableIndexers()
			if err != nil || len(instances) == 0 {
				return
			}
			profile, err := s.albumProfile(artist)
			if err != nil {
				return
			}
			s.huntAlbum(withGrabBy(context.Background(), by), artist, album, profile, instances, s.searchSkipKeys(), false)
		})
	}
	writeJSON(w, http.StatusCreated, manualMusicResponse{ArtistID: artist.ID, AlbumID: album.ID, Album: toMusicAlbumPayload(album), NewArtist: !found})
}

// manualCoverDir keeps the covers given for albums added by hand. It is not the
// cover cache: nothing there is ever cleaned away.
func (s *Server) manualCoverDir() string { return filepath.Join(s.cfg.ConfigDir, "music-own-covers") }

func (s *Server) manualCoverPath(albumID int64) string {
	return filepath.Join(s.manualCoverDir(), fmt.Sprintf("%d.jpg", albumID))
}

// forgetManualCovers deletes the covers given by hand for albums that have
// left the library (with their artist), so the folder only holds covers of
// albums that are still there.
func (s *Server) forgetManualCovers(albums []music.Album) {
	for _, a := range albums {
		if !a.Manual() {
			continue
		}
		if err := os.Remove(s.manualCoverPath(a.ID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("music: remove the cover of an album added by hand", "album", a.Title, "err", err)
		}
	}
}

// handleUploadAlbumCover is POST /api/music/albums/{id}/cover (multipart,
// field "file"): the picture for an album added by hand.
func (s *Server) handleUploadAlbumCover(w http.ResponseWriter, r *http.Request) {
	album, artist, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	if !album.Manual() {
		writeError(w, http.StatusBadRequest, "Only an album added by hand can be given its own cover. Other albums get theirs from the Cover Art Archive.")
		return
	}
	allowLongUpload(w)
	r.Body = http.MaxBytesReader(w, r.Body, books.MaxOwnCoverBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "That picture is too big.")
			return
		}
		writeError(w, http.StatusBadRequest, "Choose a picture to upload.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	var raw []byte
	if r.MultipartForm != nil {
		if fh := r.MultipartForm.File["file"]; len(fh) > 0 {
			f, err := fh[0].Open()
			if err != nil {
				writeError(w, http.StatusBadRequest, "Choose a picture to upload.")
				return
			}
			defer f.Close()
			raw, err = io.ReadAll(io.LimitReader(f, books.MaxOwnCoverBytes+1))
			if err != nil {
				writeError(w, http.StatusBadRequest, "Couldn't read that file. Try choosing it again.")
				return
			}
		}
	}
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "Choose a picture to upload.")
		return
	}
	jpeg, err := books.PrepareOwnCover(raw)
	if err != nil {
		if errors.Is(err, books.ErrCoverTooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "That picture is too big.")
			return
		}
		writeError(w, http.StatusBadRequest, "That doesn't look like a picture Mediarium can use. Choose a JPEG, PNG or WebP file.")
		return
	}
	if err := os.MkdirAll(s.manualCoverDir(), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't save the cover: "+err.Error())
		return
	}
	dest := s.manualCoverPath(album.ID)
	tmp := dest + ".tmp"
	err = os.WriteFile(tmp, jpeg, 0o644)
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	if err != nil {
		_ = os.Remove(tmp)
		writeError(w, http.StatusInternalServerError, "Couldn't save the cover: "+err.Error())
		return
	}
	s.forgetArtistCover(artist.ID)
	writeJSON(w, http.StatusOK, toMusicAlbumPayload(album))
}

// tracksForManual gives an album added by hand a tracklist made from the files
// found for it, when it has none yet. For any other album, or one that
// already has tracks, it returns tracks as they were.
func (s *Server) tracksForManual(album music.Album, tracks []music.Track, infos []music.FileInfo) ([]music.Track, error) {
	if !album.Manual() || len(tracks) > 0 || len(infos) == 0 {
		return tracks, nil
	}
	made := music.TracksFromFiles(infos)
	if err := s.MusicRepo.ReplaceTracks(album.ID, made); err != nil {
		return nil, err
	}
	slog.Info("music: made a tracklist from the files of an album added by hand", "album", album.Title, "tracks", len(made))
	return s.MusicRepo.ListTracks(album.ID)
}
