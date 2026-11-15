package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/music"
)

// Music playlists. A list belongs to the account that made it. It is private
// unless that account shares it with everyone; others can then open and play
// it but only the owner changes it.

const topTracksMax = 20

type playlistJSON struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Shared     bool    `json:"shared"`
	Mine       bool    `json:"mine"`
	Owner      string  `json:"owner"`
	TrackCount int     `json:"trackCount"`
	DurationMs int     `json:"durationMs"`
	AlbumIDs   []int64 `json:"albumIds"` // the first few albums, for a cover mosaic
	UpdatedAt  string  `json:"updatedAt"`
}

type playlistTrackJSON struct {
	EntryID  int64  `json:"entryId"`
	TrackID  int64  `json:"trackId"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	ArtistID int64  `json:"artistId"`
	Album    string `json:"album"`
	AlbumID  int64  `json:"albumId"`
	LengthMs int    `json:"lengthMs"`
	HasFile  bool   `json:"hasFile"`
}

type playlistDetailJSON struct {
	playlistJSON
	Tracks []playlistTrackJSON `json:"tracks"`
}

func (s *Server) playlistToJSON(p music.Playlist, me int64, names map[int64]string) playlistJSON {
	owner, ok := names[p.UserID]
	if !ok {
		if u, err := s.Auth.GetUser(p.UserID); err == nil && u != nil {
			owner = u.DisplayName()
		}
		names[p.UserID] = owner
	}
	ids := p.AlbumIDs
	if ids == nil {
		ids = []int64{}
	}
	return playlistJSON{ID: p.ID, Name: p.Name, Shared: p.Shared, Mine: p.UserID == me, Owner: owner,
		TrackCount: p.TrackCount, DurationMs: p.DurationMs, AlbumIDs: ids, UpdatedAt: p.UpdatedAt}
}

// signedInAccount answers 403 itself when the caller is an API key without an
// account behind it (playlists and scrobbling belong to an account).
func signedInAccount(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, _ := requester(r)
	if id == 0 {
		writeError(w, http.StatusForbidden, "Sign in with an account to use this.")
		return 0, false
	}
	return id, true
}

func writePlaylistError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, music.ErrPlaylistNotFound):
		writeError(w, http.StatusNotFound, "That playlist isn't there, or it isn't yours to change.")
	case errors.Is(err, music.ErrPlaylistFull):
		writeError(w, http.StatusBadRequest, "A playlist can hold up to 5000 tracks.")
	case errors.Is(err, music.ErrBadOrder):
		writeError(w, http.StatusBadRequest, "The order has to list every track on the playlist once.")
	default:
		slog.Warn("playlist request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "Something went wrong with that playlist. Check the log for details.")
	}
}

// GET /api/music/playlists
func (s *Server) handleListPlaylists(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	lists, err := s.MusicRepo.ListPlaylists(me)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	names := map[int64]string{}
	out := make([]playlistJSON, 0, len(lists))
	for _, p := range lists {
		out = append(out, s.playlistToJSON(p, me, names))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) playlistDetail(id, me int64) (playlistDetailJSON, error) {
	p, err := s.MusicRepo.GetPlaylist(id, me)
	if err != nil {
		return playlistDetailJSON{}, err
	}
	entries, err := s.MusicRepo.PlaylistEntries(id)
	if err != nil {
		return playlistDetailJSON{}, err
	}
	p.AlbumIDs = nil
	seen := map[int64]bool{}
	d := playlistDetailJSON{playlistJSON: s.playlistToJSON(p, me, map[int64]string{}), Tracks: make([]playlistTrackJSON, 0, len(entries))}
	for _, e := range entries {
		d.Tracks = append(d.Tracks, playlistTrackJSON{EntryID: e.EntryID, TrackID: e.Track.ID, Title: e.Title, Artist: e.Artist, ArtistID: e.ArtistID,
			Album: e.Album, AlbumID: e.Track.AlbumID, LengthMs: e.Track.LengthMs, HasFile: e.Track.FilePath != ""})
		if len(d.AlbumIDs) < 4 && !seen[e.Track.AlbumID] {
			seen[e.Track.AlbumID] = true
			d.AlbumIDs = append(d.AlbumIDs, e.Track.AlbumID)
		}
	}
	return d, nil
}

// GET /api/music/playlists/{id}
func (s *Server) handleGetPlaylist(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	id, ok := musicID(w, r, "playlist")
	if !ok {
		return
	}
	d, err := s.playlistDetail(id, me)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// playlistSelection says which tracks to add: tracks by id, a whole album, or
// the top tracks of an artist. They are added in that order.
type playlistSelection struct {
	TrackIDs []int64 `json:"trackIds"`
	AlbumID  int64   `json:"albumId"`
	ArtistID int64   `json:"artistId"`
}

// resolve turns a selection into track ids. Only tracks that have a file are
// taken from an album or artist. ok is false (with the reply written) when
// the album or artist doesn't exist, or has nothing to add.
func (s *Server) resolveSelection(ctx context.Context, w http.ResponseWriter, sel playlistSelection) ([]int64, bool) {
	ids := append([]int64(nil), sel.TrackIDs...)
	if sel.AlbumID > 0 {
		if _, err := s.MusicRepo.GetAlbum(sel.AlbumID); err != nil {
			writeError(w, http.StatusNotFound, "That album isn't in your library.")
			return nil, false
		}
		album, err := s.MusicRepo.PlayableTrackIDs(sel.AlbumID)
		if err != nil {
			writePlaylistError(w, err)
			return nil, false
		}
		if len(album) == 0 {
			writeError(w, http.StatusBadRequest, "None of this album is in your library yet.")
			return nil, false
		}
		ids = append(ids, album...)
	}
	if sel.ArtistID > 0 {
		top, err := s.artistTopTrackIDs(ctx, sel.ArtistID)
		if errors.Is(err, errNoSuchArtist) {
			writeError(w, http.StatusNotFound, "That artist isn't in your library.")
			return nil, false
		}
		if err != nil {
			writePlaylistError(w, err)
			return nil, false
		}
		if len(top) == 0 {
			writeError(w, http.StatusBadRequest, "None of this artist is in your library yet.")
			return nil, false
		}
		ids = append(ids, top...)
	}
	return ids, true
}

var errNoSuchArtist = errors.New("no such artist")

// artistTopTrackIDs is up to twenty of an artist's tracks that have a file:
// the most listened-to ones first when ListenBrainz knows them (matched by
// title), otherwise the artist's tracks from the newest album on.
func (s *Server) artistTopTrackIDs(ctx context.Context, artistID int64) ([]int64, error) {
	artist, err := s.MusicRepo.GetArtist(artistID)
	if err != nil {
		return nil, errNoSuchArtist
	}
	have, err := s.MusicRepo.ArtistPlayableTracks(artistID)
	if err != nil {
		return nil, err
	}
	if len(have) == 0 {
		return nil, nil
	}
	var out []int64
	used := map[int64]bool{}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if top, err := s.manualTopRecordings(cctx, artist); err == nil {
		byTitle := map[string][]music.Track{}
		for _, t := range have {
			k := music.NormalizeName(t.Title)
			byTitle[k] = append(byTitle[k], t)
		}
		for _, rec := range top {
			if len(out) >= topTracksMax {
				break
			}
			for _, t := range byTitle[music.NormalizeName(rec.Name)] {
				if !used[t.ID] {
					used[t.ID] = true
					out = append(out, t.ID)
					break // one copy of each song
				}
			}
		}
	} else {
		slog.Debug("artist top tracks: ListenBrainz not available, using the library order", "err", err)
	}
	if len(out) > 0 {
		return out, nil
	}
	// Nothing matched (or ListenBrainz couldn't be reached): the library order.
	for _, t := range have {
		if len(out) >= topTracksMax {
			break
		}
		out = append(out, t.ID)
	}
	return out, nil
}

// POST /api/music/playlists {name, shared?, trackIds?, albumId?, artistId?}
func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		Name   string `json:"name"`
		Shared bool   `json:"shared"`
		playlistSelection
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Send a name for the playlist.")
		return
	}
	if _, ok := music.CleanPlaylistName(req.Name); !ok {
		writeError(w, http.StatusBadRequest, "Give the playlist a name.")
		return
	}
	ids, ok := s.resolveSelection(r.Context(), w, req.playlistSelection)
	if !ok {
		return
	}
	p, err := s.MusicRepo.CreatePlaylist(me, req.Name, req.Shared)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	if _, err := s.MusicRepo.AddToPlaylist(p.ID, me, ids); err != nil {
		_ = s.MusicRepo.DeletePlaylist(p.ID, me)
		writePlaylistError(w, err)
		return
	}
	d, err := s.playlistDetail(p.ID, me)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// PUT /api/music/playlists/{id} {name?, shared?}
func (s *Server) handleUpdatePlaylist(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	id, ok := musicID(w, r, "playlist")
	if !ok {
		return
	}
	var req struct {
		Name   *string `json:"name"`
		Shared *bool   `json:"shared"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Name == nil && req.Shared == nil) {
		writeError(w, http.StatusBadRequest, "Send a new name, or whether to share the playlist.")
		return
	}
	if req.Name != nil {
		if _, ok := music.CleanPlaylistName(*req.Name); !ok {
			writeError(w, http.StatusBadRequest, "Give the playlist a name.")
			return
		}
	}
	if err := s.MusicRepo.UpdatePlaylist(id, me, req.Name, req.Shared); err != nil {
		writePlaylistError(w, err)
		return
	}
	d, err := s.playlistDetail(id, me)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// DELETE /api/music/playlists/{id}
func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	id, ok := musicID(w, r, "playlist")
	if !ok {
		return
	}
	if err := s.MusicRepo.DeletePlaylist(id, me); err != nil {
		writePlaylistError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/music/playlists/{id}/tracks {trackIds?, albumId?, artistId?}
func (s *Server) handleAddToPlaylist(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	id, ok := musicID(w, r, "playlist")
	if !ok {
		return
	}
	var sel playlistSelection
	if err := decodeJSON(r, &sel); err != nil || (len(sel.TrackIDs) == 0 && sel.AlbumID == 0 && sel.ArtistID == 0) {
		writeError(w, http.StatusBadRequest, "Say which tracks to add.")
		return
	}
	if _, err := s.MusicRepo.GetPlaylist(id, me); err != nil {
		writePlaylistError(w, err)
		return
	}
	ids, ok := s.resolveSelection(r.Context(), w, sel)
	if !ok {
		return
	}
	added, err := s.MusicRepo.AddToPlaylist(id, me, ids)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	d, err := s.playlistDetail(id, me)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "playlist": d})
}

// DELETE /api/music/playlists/{id}/tracks/{entry}
func (s *Server) handleRemoveFromPlaylist(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	id, ok := musicID(w, r, "playlist")
	if !ok {
		return
	}
	entry, err := strconv.ParseInt(r.PathValue("entry"), 10, 64)
	if err != nil || entry <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't a valid playlist entry.")
		return
	}
	if err := s.MusicRepo.RemoveFromPlaylist(id, me, entry); err != nil {
		writePlaylistError(w, err)
		return
	}
	d, err := s.playlistDetail(id, me)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// PUT /api/music/playlists/{id}/order {entryIds: [...]}
func (s *Server) handleReorderPlaylist(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	id, ok := musicID(w, r, "playlist")
	if !ok {
		return
	}
	var req struct {
		EntryIDs []int64 `json:"entryIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Send the new order of the playlist.")
		return
	}
	if err := s.MusicRepo.ReorderPlaylist(id, me, req.EntryIDs); err != nil {
		writePlaylistError(w, err)
		return
	}
	d, err := s.playlistDetail(id, me)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
