package api

import (
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/rdborg/mediarium/internal/settings"
)

// checkFolderChoice reads the folder picked for a title. "" or the main folder
// (the first on offer) store as "" (the default); another has to be one of the
// folders on offer. what is "movie" or "show", for the message.
func (s *Server) checkFolderChoice(want string, offered []string, what string) (string, string) {
	if want == "" || len(offered) == 0 {
		return "", ""
	}
	want = resolveFolderChoice(want, offered) // a label is as good as a path
	c := filepath.Clean(want)
	if c == filepath.Clean(offered[0]) {
		return "", ""
	}
	for _, f := range offered[1:] {
		if filepath.Clean(f) == c {
			return c, ""
		}
	}
	return "", "Pick one of your " + what + " folders from the list. They are set in Settings > Library and files > Folders."
}

// handleLibraryFolders is GET /api/library/folders (any account): the folders
// a movie or a show can be kept in, the main one first, and under newTitles
// where a title added without a choice goes right now. The add dialog uses it
// to offer a choice. Administrators get the full paths; everyone else gets a
// label for each folder (its name), which is accepted wherever a folder is
// chosen.
func (s *Server) handleLibraryFolders(w http.ResponseWriter, r *http.Request) {
	movies, tv := s.movieFolders(), s.tvFolders()
	shownMovies, shownTV := movies, tv
	if !isAdminRequest(r) {
		shownMovies, shownTV = folderLabels(movies), folderLabels(tv)
	}
	writeJSON(w, http.StatusOK, libraryFoldersPayload{
		Movies: nonNil(shownMovies), TV: nonNil(shownTV),
		NewTitles: newTitlesPayload{
			Movies: s.newTitlePayloadFor(settings.KeyMoviesNewFolder, movies, shownMovies),
			TV:     s.newTitlePayloadFor(settings.KeyTVNewFolder, tv, shownTV),
		},
	})
}

type libraryFoldersPayload struct {
	Movies    []string         `json:"movies"`
	TV        []string         `json:"tv"`
	NewTitles newTitlesPayload `json:"newTitles"`
}

type newTitlesPayload struct {
	Movies newTitleFolderPayload `json:"movies"`
	TV     newTitleFolderPayload `json:"tv"`
}

type setFolderRequest struct {
	RootPath string `json:"rootPath"`
}

// handleSetMovieFolder is PUT /api/movies/{id}/folder: where the movie's next
// files go. Files already in the library stay where they are.
func (s *Server) handleSetMovieFolder(w http.ResponseWriter, r *http.Request) {
	s.setFolder(w, r, "movie", s.movieFolders, s.MovieRepo.SetRoot)
}

// handleSetSeriesFolder is the same for a show.
func (s *Server) handleSetSeriesFolder(w http.ResponseWriter, r *http.Request) {
	s.setFolder(w, r, "show", s.tvFolders, s.MovieRepo.SetSeriesRoot)
}

func (s *Server) setFolder(w http.ResponseWriter, r *http.Request, what string, offered func() []string, set func(id int64, root string) error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid "+what+" ID.")
		return
	}
	var req setFolderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	root, problem := s.checkFolderChoice(req.RootPath, offered(), what)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	if err := set(id, root); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"rootPath": folderShown(r, offered(), root)})
}
