package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/mediafiles"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/queue"
)

// Rescanning one album's folder, and deleting one file of it. Both end the
// same way: the tracks that have a file, the album's quality and its status
// are worked out again from what is on disk, so the page shows the truth and
// automation knows whether the album is wanted.

// maxMissingTitles caps the track titles listed in an answer.
const maxMissingTitles = 25

// albumRescanReport is what a rescan or a file deletion leaves the album as.
type albumRescanReport struct {
	Status   string `json:"status"`            // missing, downloading or downloaded
	Quality  string `json:"quality,omitempty"` // the lowest tier among the tracks' files
	Folder   bool   `json:"folder"`            // the album still has a folder with files
	Files    int    `json:"files"`             // audio files found in the folder
	Tracks   int    `json:"tracks"`            // tracks on the album's tracklist
	Matched  int    `json:"matched"`           // tracks that have a file now
	Linked   int    `json:"linked"`            // of those, newly linked by this rescan
	Dropped  int    `json:"dropped"`           // links removed because the file is gone
	Extra    int    `json:"extra"`             // audio files that fit no track
	Complete bool   `json:"complete"`          // every track has a file
	// MissingTitles are the tracks without a file (at most maxMissingTitles).
	MissingTitles []string `json:"missingTitles"`
	Monitored     bool     `json:"monitored"`
	// Deleted is set when the answer follows a file deletion.
	Deleted string `json:"deleted,omitempty"`
	Trashed bool   `json:"trashed,omitempty"`
}

// albumFolderOnDisk finds where an album's files are: its recorded folder or,
// for an album not registered with one, the folder Mediarium would have made
// or one of the artist's folders that carries the album's name. The folder
// is checked to lie inside the music folder. found is false when there is
// none.
func (s *Server) albumFolderOnDisk(artist music.Artist, album music.Album) (path string, folder mediafiles.Folder, found bool, err error) {
	root := s.musicRoot()
	if root == "" {
		return "", mediafiles.Folder{}, false, errors.New("the music folder isn't set")
	}
	candidates := []string{}
	if album.Path != "" {
		candidates = append(candidates, album.Path)
	} else {
		clean := music.Sanitizer(s.musicSanitizer())
		made := music.AlbumPath(root, artist.Name, album.Title, album.Year(), clean)
		candidates = append(candidates, made)
		if entries, e := os.ReadDir(filepath.Dir(made)); e == nil {
			want := music.NormalizeName(album.Title)
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				if t, _ := music.ParseAlbumFolder(e.Name(), artist.Name); music.NormalizeName(t) == want {
					candidates = append(candidates, filepath.Join(filepath.Dir(made), e.Name()))
				}
			}
		}
	}
	for _, c := range candidates {
		f, e := mediafiles.ForDir(c, []string{root})
		switch {
		case e == nil:
			return c, f, true, nil
		case errors.Is(e, fs.ErrNotExist):
			continue
		default:
			return "", mediafiles.Folder{}, false, e
		}
	}
	return "", mediafiles.Folder{}, false, nil
}

// rebase gives a file found under folder.Dir (the folder with links
// resolved) the spelling of the album's own path, which is how track files
// are recorded everywhere else.
func rebase(path string, folder mediafiles.Folder, file string) string {
	rel, err := filepath.Rel(folder.Dir, file)
	if err != nil {
		return file
	}
	return filepath.Join(path, rel)
}

// rescanAlbum reads the audio files in an album's folder again, matches
// them to the tracklist by tags, track numbers and titles, and updates the
// tracks, the album's quality and its status. It touches no file.
func (s *Server) rescanAlbum(ctx context.Context, artist music.Artist, album music.Album) (albumRescanReport, error) {
	path, folder, found, err := s.albumFolderOnDisk(artist, album)
	if err != nil {
		return albumRescanReport{}, err
	}
	tracks, err := s.ensureTracks(ctx, album)
	if err != nil {
		return albumRescanReport{}, err
	}
	var files []string
	if found {
		files, err = music.FindAudioFiles(folder.Dir)
		if err != nil {
			return albumRescanReport{}, err
		}
	}
	reader := &skippedTags{inner: musicTagReader}
	infos := make([]music.FileInfo, len(files))
	for i, f := range files {
		if ctx.Err() != nil {
			return albumRescanReport{}, ctx.Err()
		}
		infos[i] = music.IdentifyFile(f, reader)
	}
	slots := make([]music.TrackSlot, len(tracks))
	for i, t := range tracks {
		slots[i] = music.TrackSlot{Disc: t.Disc, Position: t.Position, Title: t.Title}
	}
	matches, unmatched := music.MatchTracks(infos, slots)

	claimed := map[string]bool{} // files a track is matched to
	slotFile := map[int]string{}
	for _, m := range matches {
		p := rebase(path, folder, files[m.File])
		claimed[p] = true
		slotFile[m.Slot] = p
	}
	rep := albumRescanReport{Files: len(files), Tracks: len(tracks), Extra: len(unmatched), Monitored: album.Monitored}
	for i := range tracks {
		t := &tracks[i]
		if p, ok := slotFile[i]; ok {
			if t.FilePath != p {
				if err := s.MusicRepo.SetTrackFile(t.ID, p); err != nil {
					return albumRescanReport{}, err
				}
				if t.FilePath == "" {
					rep.Linked++
				}
				t.FilePath = p
			}
			continue
		}
		// No file fits this track now. A link to a file that is still in
		// the folder and fits no other track stays; any other link goes.
		if t.FilePath == "" {
			continue
		}
		if !claimed[t.FilePath] && regularFileExists(t.FilePath) && isUnder(path, t.FilePath) {
			continue
		}
		if err := s.MusicRepo.SetTrackFile(t.ID, ""); err != nil {
			return albumRescanReport{}, err
		}
		t.FilePath = ""
		rep.Dropped++
	}
	if err := s.settleAlbum(album, path, found, tracks, len(files), &rep); err != nil {
		return albumRescanReport{}, err
	}
	s.albumEvent(album.ID, "rescanned", queue.LevelInfo, fmt.Sprintf("Rescanned the album folder: %d of %d tracks have a file. Files that fit no track: %d.", rep.Matched, rep.Tracks, rep.Extra))
	return rep, nil
}

// isUnder reports whether file lies inside dir (no links followed: both are
// the paths recorded for the album).
func isUnder(dir, file string) bool {
	rel, err := filepath.Rel(dir, file)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// settleAlbum works the album's quality and status out from its tracks
// (already saved) and fills the report. An album with no linked file and no
// audio left in its folder is wanted again; one with files is downloaded at
// the lowest quality among them.
func (s *Server) settleAlbum(album music.Album, path string, folderFound bool, tracks []music.Track, audioFiles int, rep *albumRescanReport) error {
	var linked []string
	rep.MissingTitles = []string{}
	missing := 0 // wanted tracks without a file; a track that is not wanted is not missing
	for _, t := range tracks {
		if t.FilePath != "" {
			linked = append(linked, t.FilePath)
			continue
		}
		if !t.Monitored {
			continue
		}
		missing++
		if len(rep.MissingTitles) < maxMissingTitles {
			rep.MissingTitles = append(rep.MissingTitles, t.Title)
		}
	}
	rep.Matched, rep.Tracks = len(linked), len(tracks)
	rep.Complete = len(tracks) > 0 && missing == 0
	rep.Files, rep.Folder = audioFiles, folderFound && audioFiles > 0
	if rep.Files == 0 && len(linked) == 0 {
		rep.Folder = false
		if album.Status == music.StatusDownloading {
			rep.Status = string(music.StatusDownloading)
			return nil
		}
		if album.Status == music.StatusDownloaded || album.Path != "" {
			if err := s.MusicRepo.SetAlbumMissing(album.ID); err != nil {
				return err
			}
		}
		rep.Status, rep.Quality = string(music.StatusMissing), ""
		return nil
	}
	if album.Status == music.StatusDownloading {
		rep.Status = string(music.StatusDownloading) // the download's import settles it
		return nil
	}
	tier := music.AlbumTier(linked)
	if err := s.MusicRepo.SetAlbumImported(album.ID, tier, path); err != nil {
		return err
	}
	rep.Status, rep.Quality = string(music.StatusDownloaded), string(tier)
	return nil
}

func regularFileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// handleRescanAlbum rechecks the files in an album's folder (administrators):
// the Rescan album folder button. It works for albums already in the
// library, which the full import scan skips.
func (s *Server) handleRescanAlbum(w http.ResponseWriter, r *http.Request) {
	album, artist, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	if album.Status == music.StatusDownloading {
		writeError(w, http.StatusConflict, "This album is downloading right now. Rescan it when the download has finished.")
		return
	}
	rep, err := s.rescanAlbum(r.Context(), artist, album)
	switch {
	case errors.Is(err, mediafiles.ErrOutside):
		writeError(w, http.StatusForbidden, "This album's files aren't inside your music folder.")
		return
	case err != nil:
		slog.Warn("music: rescan album folder", "album", album.Title, "err", err)
		writeError(w, http.StatusBadGateway, "Couldn't rescan this album. Check the Mediarium log for details.")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleDeleteAlbumFile deletes one file from an album's folder (administrators):
// ?path= is the file's path in the listing, relative to the album's folder.
// The file goes to the recycle bin when it is on (otherwise it is deleted),
// is unlinked from its track, and the album's status and quality are worked
// out again. Only a file inside the music folder is ever touched.
func (s *Server) handleDeleteAlbumFile(w http.ResponseWriter, r *http.Request) {
	album, artist, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" {
		writeError(w, http.StatusBadRequest, "Say which file to delete.")
		return
	}
	if album.Status == music.StatusDownloading {
		writeError(w, http.StatusConflict, "This album is downloading right now. Delete its files when the download has finished.")
		return
	}
	if album.Path == "" {
		writeError(w, http.StatusNotFound, "This album has no files on disk.")
		return
	}
	folder, err := s.albumFolder(album)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "This album has no files on disk.")
		return
	case errors.Is(err, mediafiles.ErrOutside):
		writeError(w, http.StatusForbidden, "This album's files aren't inside your music folder.")
		return
	case err != nil:
		writeFileReadError(w, err)
		return
	}
	// Open applies the path rules (no "..", no hidden names, no link that
	// leads out); only a regular file is deleted.
	f, info, err := folder.Open(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, "That file isn't in the album's folder.")
			return
		}
		writeError(w, http.StatusBadRequest, "That isn't a file in the album's folder.")
		return
	}
	f.Close()
	if !info.Mode().IsRegular() {
		writeError(w, http.StatusBadRequest, "That isn't a file in the album's folder.")
		return
	}
	abs := filepath.Join(folder.Dir, filepath.FromSlash(rel))
	root := s.musicRoot()
	if real, e := filepath.EvalSymlinks(root); e == nil {
		root = real
	}
	_, trashed, err := s.discardFileWithSidecars(root, abs, albumLabel(artist, album))
	if err != nil {
		slog.Warn("music: delete album file", "album", album.Title, "file", rel, "err", err)
		writeError(w, http.StatusConflict, "Couldn't delete the file. Check the Mediarium log for details.")
		return
	}

	tracks, err := s.MusicRepo.ListTracks(album.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range tracks {
		t := &tracks[i]
		if t.FilePath == "" {
			continue
		}
		if tr, ok := folder.Rel(t.FilePath); (ok && tr == rel) || !regularFileExists(t.FilePath) {
			if err := s.MusicRepo.SetTrackFile(t.ID, ""); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			t.FilePath = ""
		}
	}
	left, _ := music.FindAudioFiles(folder.Dir) // a folder removed with its last file has none
	rep := albumRescanReport{Extra: 0, Monitored: album.Monitored, Deleted: rel, Trashed: trashed}
	if err := s.settleAlbum(album, album.Path, dirExists(folder.Dir), tracks, len(left), &rep); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.albumEvent(album.ID, "removed", queue.LevelInfo, fmt.Sprintf("Deleted the file %s from the album folder. %d of %d tracks have a file now.", filepath.Base(rel), rep.Matched, rep.Tracks))
	writeJSON(w, http.StatusOK, rep)
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
