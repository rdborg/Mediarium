package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
)

// Editing the release group of a file by hand. The group is read from the
// release name the file came from (or from its own name), and the naming
// setting writes it into the file name, so changing it changes that stored
// name and, unless files keep their original names, renames the file.

var releaseGroupOK = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}._!&'+]{0,39}$`)

type releaseGroupRequest struct {
	Group string `json:"group"`
}

type releaseGroupAnswer struct {
	Group   string `json:"group"`
	Renamed bool   `json:"renamed"`
	File    string `json:"file"`
}

// withReleaseGroup returns release with its group replaced by group, or
// removed when group is empty. A release that names no group gets one added.
func withReleaseGroup(release, group string) string {
	base := strings.TrimSpace(release)
	if old := parser.Parse(base).Group; old != "" {
		low := strings.ToLower(base)
		o := strings.ToLower(old)
		switch {
		case strings.HasSuffix(low, "-"+o):
			base = base[:len(base)-len(old)-1]
		case strings.HasSuffix(low, "["+o+"]"):
			base = base[:len(base)-len(old)-2]
		case strings.HasPrefix(low, "["+o+"]"):
			base = base[len(old)+2:]
		}
	}
	base = strings.Trim(base, " -.")
	if group == "" {
		return base
	}
	return base + "-" + group
}

func readReleaseGroup(r *http.Request) (string, bool) {
	var req releaseGroupRequest
	if err := decodeJSON(r, &req); err != nil {
		return "", false
	}
	g := strings.TrimSpace(req.Group)
	if g != "" && !releaseGroupOK.MatchString(g) {
		return "", false
	}
	return g, true
}

const badReleaseGroup = "A release group is letters and numbers, up to 40 characters, with no spaces or slashes. Leave it empty to remove it."

// handleSetMovieReleaseGroup is PUT /api/movies/{id}/release-group.
func (s *Server) handleSetMovieReleaseGroup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	group, ok := readReleaseGroup(r)
	if !ok {
		writeError(w, http.StatusBadRequest, badReleaseGroup)
		return
	}
	m, err := s.MovieRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
		return
	}
	if m.Status != library.StatusDownloaded || m.FilePath == "" {
		writeError(w, http.StatusConflict, "This movie has no file yet.")
		return
	}
	old := m.ReleaseTitle
	m.ReleaseTitle = withReleaseGroup(renameRelease(old, m.FilePath), group)
	if err := s.MovieRepo.SetReleaseTitle(m.ID, m.ReleaseTitle); err != nil {
		slog.Warn("release group: couldn't save", "movie", m.Title, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save the release group. Check the Mediarium log for details.")
		return
	}
	ans := releaseGroupAnswer{Group: group, File: m.FilePath}
	if !s.keepOriginalNames() {
		it, need, err := s.moviePlanItem(m)
		if err == nil && need {
			err = renameWithSidecars(it.root, it.From, it.To)
			if err == nil {
				_ = s.MovieRepo.SetStatus(m.ID, m.Status, m.Quality, it.To)
				ans.Renamed, ans.File = true, it.To
				s.movieImported(it.To)
			}
		}
		if err != nil {
			_ = s.MovieRepo.SetReleaseTitle(m.ID, old)
			slog.Warn("release group: couldn't rename", "movie", m.Title, "err", err)
			writeError(w, http.StatusConflict, fmt.Sprintf("The file couldn't be renamed: %v. Nothing was changed.", err))
			return
		}
	}
	_ = s.QueueRepo.LogActivity(m.ID, "renamed", fmt.Sprintf("%s: release group set to %s by hand", movieLabel(m), groupLabel(group)))
	writeJSON(w, http.StatusOK, ans)
}

// handleSetEpisodeReleaseGroup is PUT /api/episodes/{id}/release-group. A
// file holding several episodes is one release, so all of them change.
func (s *Server) handleSetEpisodeReleaseGroup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid episode ID.")
		return
	}
	group, ok := readReleaseGroup(r)
	if !ok {
		writeError(w, http.StatusBadRequest, badReleaseGroup)
		return
	}
	ep, err := s.MovieRepo.GetEpisodeByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That episode isn't in your library.")
		return
	}
	sr, err := s.MovieRepo.GetSeries(ep.SeriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	}
	if ep.Status != library.StatusDownloaded || ep.FilePath == "" {
		writeError(w, http.StatusConflict, "This episode has no file yet.")
		return
	}
	eps, err := s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't read the show. Check the Mediarium log for details.")
		return
	}
	var same []library.Episode
	stored := ""
	for _, e := range eps {
		if e.FilePath == ep.FilePath {
			same = append(same, e)
			if stored == "" {
				stored = e.ReleaseTitle
			}
		}
	}
	next := withReleaseGroup(renameRelease(stored, ep.FilePath), group)
	olds := map[int64]string{}
	for _, e := range same {
		olds[e.ID] = e.ReleaseTitle
		if err := s.MovieRepo.SetEpisodeReleaseTitle(e.ID, next); err != nil {
			slog.Warn("release group: couldn't save", "series", sr.Title, "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't save the release group. Check the Mediarium log for details.")
			return
		}
	}
	undo := func() {
		for id, t := range olds {
			_ = s.MovieRepo.SetEpisodeReleaseTitle(id, t)
		}
	}
	ans := releaseGroupAnswer{Group: group, File: ep.FilePath}
	if !s.keepOriginalNames() {
		items, err := s.seriesPlanItems(sr)
		if err == nil {
			for _, it := range items {
				if it.From != ep.FilePath {
					continue
				}
				if err = renameWithSidecars(it.root, it.From, it.To); err == nil {
					if err = s.MovieRepo.MoveEpisodeFile(it.From, it.To); err == nil {
						ans.Renamed, ans.File = true, it.To
						s.episodesImported(it.To)
					}
				}
				break
			}
		}
		if err != nil {
			undo()
			slog.Warn("release group: couldn't rename", "series", sr.Title, "err", err)
			writeError(w, http.StatusConflict, fmt.Sprintf("The file couldn't be renamed: %v. Nothing was changed.", err))
			return
		}
	}
	_ = s.QueueRepo.LogSeriesActivity(sr.ID, "renamed", fmt.Sprintf("%s S%02dE%02d: release group set to %s by hand", seriesLabel(sr), ep.Season, ep.Episode, groupLabel(group)))
	writeJSON(w, http.StatusOK, ans)
}

func groupLabel(g string) string {
	if g == "" {
		return "none"
	}
	return g
}
