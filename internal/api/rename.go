package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/fileperms"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
)

// Bulk rename: files already in the library keep the names they had when
// they arrived. After changing the naming setting, this renames them to
// match: a preview first, then only the ones you tick. Subtitles and other
// files named after a video move with it. Nothing outside the library folder
// is touched, and a file is never put where another one already is.

const maxRenamePreview = 2000

type renameItem struct {
	Kind  string `json:"kind"` // "movie" or "episode"
	ID    int64  `json:"id"`
	Title string `json:"title"`
	From  string `json:"from"`
	To    string `json:"to"`
	root  string // the library folder the file stays inside
}

// renamePlan lists every library file whose name differs from what the
// naming setting would give it now. While "Keep the original file names" is
// on, nothing is renamed.
func (s *Server) renamePlan(kind string) ([]renameItem, error) {
	var out []renameItem
	if s.keepOriginalNames() {
		return out, nil
	}
	if kind == "" || kind == "movie" {
		movies, err := s.MovieRepo.ListWithCopies()
		if err != nil {
			return nil, err
		}
		for _, m := range movies {
			// The file is renamed where it is: a folder chosen for the movie
			// after it arrived is only for its next file.
			root := s.fileRoot(s.movieRootFor(m), m.FilePath)
			if m.FilePath == "" || root == "" {
				continue
			}
			to, err := s.buildDestPath(root, m.Title, m.Year, m.TMDBID, strings.TrimSuffix(filepath.Base(m.FilePath), filepath.Ext(m.FilePath)), m.FilePath)
			if err != nil {
				return nil, err
			}
			to = s.keepMovieFolder(m, root, to) // the folder format is for new movies
			if to != m.FilePath {
				out = append(out, renameItem{Kind: "movie", ID: m.ID, Title: movieLabel(m), From: m.FilePath, To: to, root: root})
			}
		}
	}
	if kind == "" || kind == "tv" {
		shows, err := s.MovieRepo.ListSeriesWithCopies()
		if err != nil {
			return nil, err
		}
		for _, sr := range shows {
			title := s.seriesRootFor(sr)
			if title == "" {
				continue
			}
			eps, err := s.MovieRepo.ListEpisodes(sr.ID)
			if err != nil {
				return nil, err
			}
			// A multi-episode file is renamed once, named for all its episodes.
			inFile := map[string][]library.Episode{}
			for _, ep := range eps {
				if ep.FilePath != "" {
					inFile[ep.FilePath] = append(inFile[ep.FilePath], ep)
				}
			}
			done := map[string]bool{}
			for _, ep := range eps {
				if ep.FilePath == "" || done[ep.FilePath] {
					continue
				}
				done[ep.FilePath] = true
				stem := strings.TrimSuffix(filepath.Base(ep.FilePath), filepath.Ext(ep.FilePath))
				to, _, err := s.buildTVDestPathFor(sr, inFile[ep.FilePath], parser.Parse(stem), stem, ep.FilePath, false)
				if err != nil {
					return nil, err
				}
				// As for movies, the file stays in the library folder it is in.
				root := s.fileRoot(title, ep.FilePath)
				if root != title {
					if rel, err := filepath.Rel(title, to); err == nil {
						to = filepath.Join(root, rel)
					}
				}
				if to != ep.FilePath {
					out = append(out, renameItem{Kind: "episode", ID: ep.ID, Title: fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.Season, ep.Episode), From: ep.FilePath, To: to, root: root})
				}
			}
		}
	}
	if len(out) > maxRenamePreview {
		out = out[:maxRenamePreview]
	}
	return out, nil
}

func (s *Server) handleRenamePreview(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "movie" && kind != "tv" {
		writeError(w, http.StatusBadRequest, `kind is "movie" or "tv".`)
		return
	}
	plan, err := s.renamePlan(kind)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if plan == nil {
		plan = []renameItem{}
	}
	writeJSON(w, http.StatusOK, plan)
}

type renameRequest struct {
	Items []struct {
		Kind string `json:"kind"`
		ID   int64  `json:"id"`
	} `json:"items"`
}

type renameResult struct {
	Renamed int      `json:"renamed"`
	Failed  []string `json:"failed"`
}

// handleRename renames the chosen files, working out the new name again so
// it matches the preview.
func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var req renameRequest
	if err := decodeJSON(r, &req); err != nil || len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "Choose the files to rename.")
		return
	}
	if s.keepOriginalNames() {
		writeError(w, http.StatusConflict, "Files keep their original names right now, so nothing is renamed. Turn off Keep the original file names first.")
		return
	}
	plan, err := s.renamePlan("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byKey := map[string]renameItem{}
	for _, p := range plan {
		byKey[fmt.Sprintf("%s-%d", p.Kind, p.ID)] = p
	}
	res := renameResult{Failed: []string{}}
	var movieFiles, episodeFiles []string
	for _, it := range req.Items {
		p, ok := byKey[fmt.Sprintf("%s-%d", it.Kind, it.ID)]
		if !ok {
			continue // already right, or gone
		}
		if err := renameWithSidecars(p.root, p.From, p.To); err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", p.Title, err))
			continue
		}
		if p.Kind == "movie" {
			if m, err := s.MovieRepo.Get(p.ID); err == nil {
				_ = s.MovieRepo.SetStatus(m.ID, m.Status, m.Quality, p.To)
			}
			movieFiles = append(movieFiles, p.To)
		} else {
			if err := s.MovieRepo.MoveEpisodeFile(p.From, p.To); err != nil {
				res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", p.Title, err))
				continue
			}
			episodeFiles = append(episodeFiles, p.To)
		}
		res.Renamed++
	}
	if res.Renamed > 0 {
		_ = s.QueueRepo.LogActivity(0, "renamed", fmt.Sprintf("Renamed %s to match the naming setting", plural(res.Renamed, "file")))
		for _, f := range movieFiles {
			s.movieImported(f)
		}
		if len(episodeFiles) > 0 {
			s.episodesImported(episodeFiles...)
		}
	}
	writeJSON(w, http.StatusOK, res)
}

var errRenameTaken = errors.New("another file is already at the new name")

// renameWithSidecars moves a video and the files named after it to the new
// name, inside root, then removes folders left empty.
func renameWithSidecars(root, from, to string) error {
	if _, err := cleanup.InsideRoot(root, from); err != nil {
		return err
	}
	absRoot, _ := filepath.Abs(root)
	absTo, _ := filepath.Abs(to)
	if r, err := filepath.Rel(absRoot, absTo); err != nil || strings.HasPrefix(r, "..") {
		return fmt.Errorf("the new name is outside the library folder: %w", cleanup.ErrOutside)
	}
	if toInfo, err := os.Lstat(to); err == nil {
		// On a disk that ignores case (Windows, macOS, many network shares) a
		// name that only differs in capitals finds the file itself.
		fromInfo, ferr := os.Lstat(from)
		if ferr != nil || !strings.EqualFold(from, to) || !os.SameFile(fromInfo, toInfo) {
			return errRenameTaken
		}
	}
	files, err := cleanup.FileAndSidecars(from)
	if err != nil {
		return err
	}
	oldStem := strings.TrimSuffix(filepath.Base(from), filepath.Ext(from))
	newStem := strings.TrimSuffix(filepath.Base(to), filepath.Ext(to))
	if err := fileperms.MkdirAll(filepath.Dir(to)); err != nil {
		return err
	}
	// The video goes last, so a side file that can't be renamed stops the
	// work before the video (the file the library points at) has moved; what
	// was already renamed is put back.
	var order []string
	video := false
	absFrom, _ := filepath.Abs(from)
	for _, f := range files {
		if f == absFrom {
			video = true
		} else {
			order = append(order, f)
		}
	}
	if video {
		order = append(order, absFrom)
	}
	var done [][2]string
	undo := func() {
		for i := len(done) - 1; i >= 0; i-- {
			if err := renameFile(done[i][1], done[i][0]); err != nil {
				slog.Warn("rename: couldn't put a file back", "file", done[i][1], "err", err)
			}
		}
		cleanup.PruneEmptyFolders(root, filepath.Dir(to))
	}
	for _, f := range order {
		name := filepath.Base(f)
		dest := to
		if f != absFrom {
			dest = filepath.Join(filepath.Dir(to), newStem+strings.TrimPrefix(name, oldStem))
			if _, err := os.Lstat(dest); err == nil {
				continue // leave a side file where it is rather than overwrite one
			}
		}
		if err := renameFile(f, dest); err != nil {
			undo()
			return fmt.Errorf("rename %s: %w", name, err)
		}
		done = append(done, [2]string{f, dest})
	}
	cleanup.PruneEmptyFolders(root, filepath.Dir(from))
	return nil
}

// renameFile is os.Rename; tests replace it to make one rename fail.
var renameFile = os.Rename
