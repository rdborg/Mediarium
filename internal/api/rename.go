package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/cleanup"
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
}

// renamePlan lists every library file whose name differs from what the
// naming setting would give it now.
func (s *Server) renamePlan(kind string) ([]renameItem, error) {
	var out []renameItem
	if kind == "" || kind == "movie" {
		movies, err := s.MovieRepo.List()
		if err != nil {
			return nil, err
		}
		for _, m := range movies {
			if m.FilePath == "" {
				continue
			}
			to, err := s.buildDestPath(s.movieFileRoot(m.FilePath), m.Title, m.Year, m.TMDBID, strings.TrimSuffix(filepath.Base(m.FilePath), filepath.Ext(m.FilePath)), m.FilePath)
			if err != nil {
				return nil, err
			}
			if to != m.FilePath {
				out = append(out, renameItem{Kind: "movie", ID: m.ID, Title: movieLabel(m), From: m.FilePath, To: to})
			}
		}
	}
	if kind == "" || kind == "tv" {
		shows, err := s.MovieRepo.ListSeries()
		if err != nil {
			return nil, err
		}
		for _, sr := range shows {
			eps, err := s.MovieRepo.ListEpisodes(sr.ID)
			if err != nil {
				return nil, err
			}
			done := map[string]bool{} // a multi-episode file is renamed once, as its first episode
			for _, ep := range eps {
				if ep.FilePath == "" || done[ep.FilePath] {
					continue
				}
				done[ep.FilePath] = true
				rel := parser.Parse(strings.TrimSuffix(filepath.Base(ep.FilePath), filepath.Ext(ep.FilePath)))
				here := sr
				here.RootPath = s.tvFileRoot(ep.FilePath)
				to, err := s.buildTVDestPath(here, ep, rel, ep.FilePath)
				if err != nil {
					return nil, err
				}
				if to != ep.FilePath {
					out = append(out, renameItem{Kind: "episode", ID: ep.ID, Title: fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.Season, ep.Episode), From: ep.FilePath, To: to})
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
		root := s.movieFileRoot(p.From)
		if p.Kind == "episode" {
			root = s.tvFileRoot(p.From)
		}
		if err := renameWithSidecars(root, p.From, p.To); err != nil {
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
	if _, err := os.Lstat(to); err == nil {
		return errRenameTaken
	}
	files, err := cleanup.FileAndSidecars(from)
	if err != nil {
		return err
	}
	oldStem := strings.TrimSuffix(filepath.Base(from), filepath.Ext(from))
	newStem := strings.TrimSuffix(filepath.Base(to), filepath.Ext(to))
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	for _, f := range files {
		name := filepath.Base(f)
		dest := to
		if f != from {
			dest = filepath.Join(filepath.Dir(to), newStem+strings.TrimPrefix(name, oldStem))
			if _, err := os.Lstat(dest); err == nil {
				continue // leave a side file where it is rather than overwrite one
			}
		}
		if err := os.Rename(f, dest); err != nil {
			return fmt.Errorf("rename %s: %w", name, err)
		}
	}
	cleanup.PruneEmptyFolders(root, filepath.Dir(from))
	return nil
}
