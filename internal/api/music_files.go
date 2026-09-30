package api

import (
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/mediafiles"
	"github.com/rdborg/mediarium/internal/music"
)

// The files of an album on disk, like the movie and show file listings: the
// album's folder is checked to lie inside the music folder, and files are
// opened through it, so nothing outside can be reached.

// albumFolder finds an imported album's folder on disk.
func (s *Server) albumFolder(a music.Album) (mediafiles.Folder, error) {
	return mediafiles.ForDir(a.Path, []string{s.musicRoot()})
}

// albumFileEntry is one file in an album's folder.
type albumFileEntry struct {
	Path     string `json:"path"` // relative to the album's folder, "/"-separated
	Size     int64  `json:"size"`
	Modified string `json:"modified"` // RFC 3339, UTC
	Kind     string `json:"kind"`     // audio | image | nfo | subtitle | other
	Main     bool   `json:"main,omitempty"`
	TrackID  int64  `json:"trackId,omitempty"` // the track this file is the file of
}

type albumFilesPayload struct {
	Folder string           `json:"folder"` // relative to the music folder, never the full path on the server
	Files  []albumFileEntry `json:"files"`
}

// handleAlbumFiles lists the files in an album's folder on disk (any
// account): tracks carry their track id, and cover art, logs and cue sheets
// are listed too. An album that is not on disk yet has an empty listing.
func (s *Server) handleAlbumFiles(w http.ResponseWriter, r *http.Request) {
	album, _, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	empty := albumFilesPayload{Files: []albumFileEntry{}}
	if album.Path == "" {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	folder, err := s.albumFolder(album)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeJSON(w, http.StatusOK, empty)
		return
	case errors.Is(err, mediafiles.ErrOutside):
		writeError(w, http.StatusForbidden, "This album's files aren't inside your music folder.")
		return
	case err != nil:
		writeFileReadError(w, err)
		return
	}
	files, err := folder.List()
	if err != nil {
		writeFolderError(w, err)
		return
	}
	tracks, err := s.MusicRepo.ListTracks(album.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	trackOf := map[string]int64{}
	for _, t := range tracks {
		if rel, ok := folder.Rel(t.FilePath); ok {
			trackOf[rel] = t.ID
		}
	}
	out := albumFilesPayload{Folder: folder.Name, Files: make([]albumFileEntry, len(files))}
	for i, f := range files {
		id, isMain := trackOf[f.Path]
		out.Files[i] = albumFileEntry{
			Path: f.Path, Size: f.Size, Modified: f.Modified.Format(time.RFC3339), Kind: string(mediafiles.AlbumKindOf(f.Path)),
			Main: isMain, TrackID: id,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// albumStreamFolder resolves ?album={id} of the stream endpoint to the
// album's folder, answering the error itself. The stream endpoint is not
// under /api/music/, so the music module's switch is checked here.
func (s *Server) albumStreamFolder(w http.ResponseWriter, idText string) (mediafiles.Folder, bool) {
	if !s.musicEnabled() {
		writeError(w, http.StatusNotFound, musicOffMessage)
		return mediafiles.Folder{}, false
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't a valid album ID.")
		return mediafiles.Folder{}, false
	}
	album, err := s.MusicRepo.GetAlbum(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That album isn't in your library.")
		return mediafiles.Folder{}, false
	}
	folder, err := s.albumFolder(album)
	if err != nil {
		writeStreamFolderError(w, err)
		return mediafiles.Folder{}, false
	}
	return folder, true
}
