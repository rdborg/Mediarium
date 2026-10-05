package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/settings"
)

// The recycle bin: removing a title "with its files" moves them into a
// hidden folder inside the same library folder for a few days (seven unless
// changed), so a slip can be undone from Activity > Recycle bin. The daily
// clean-up empties what is older.

const defaultTrashDays = 7

// trashDays is how many days removed files are kept; 0 deletes them straight
// away.
func (s *Server) trashDays() int {
	v, _ := s.Settings.Get(settings.KeyTrashDays)
	if v == "" {
		return defaultTrashDays
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return defaultTrashDays
	}
	return n
}

// discardFolder removes a title's folder: into the recycle bin when it is on,
// otherwise for good. It reports whether the folder went to the bin. A move
// that fails for any reason but safety (a folder outside the library is
// never touched) falls back to deleting, as asked, and says so in Activity.
func (s *Server) discardFolder(root, folder, label string) (bool, error) {
	if s.trashDays() > 0 {
		_, err := cleanup.TrashFolder(root, folder, label, time.Now())
		if err == nil {
			return true, nil
		}
		if errors.Is(err, cleanup.ErrOutside) {
			return false, err
		}
		log.Printf("api: recycle bin: %v; deleting instead", err)
		_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("Couldn't move %s to the recycle bin, so its files were deleted", label))
	}
	_, err := cleanup.RemoveFolder(root, folder)
	return false, err
}

// discardFileWithSidecars is discardFolder for a video file and the files
// named after it. It returns how many files went.
func (s *Server) discardFileWithSidecars(root, file, label string) (int, bool, error) {
	if s.trashDays() > 0 {
		e, err := cleanup.TrashFileWithSidecars(root, file, label, time.Now())
		if err == nil {
			return len(e.Paths), true, nil
		}
		if errors.Is(err, cleanup.ErrOutside) {
			return 0, false, err
		}
		log.Printf("api: recycle bin: %v; deleting instead", err)
		_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("Couldn't move %s to the recycle bin, so its files were deleted", label))
	}
	gone, err := cleanup.RemoveFileWithSidecars(root, file)
	return len(gone), false, err
}

// trashRoots names each library folder that can hold a recycle bin.
func (s *Server) trashRoots() map[string]string {
	roots := map[string]string{}
	if m := s.moviesRoot(); m != "" {
		roots["movies"] = m
	}
	if t := s.tvRoot(); t != "" {
		roots["tv"] = t
	}
	if m := s.musicRoot(); m != "" {
		roots["music"] = m
	}
	if e := s.ebooksRoot(); e != "" {
		roots["ebooks"] = e
	}
	if a := s.audiobooksRoot(); a != "" {
		roots["audiobooks"] = a
	}
	return roots
}

// purgeTrash empties what has been in the bins longer than the setting.
func (s *Server) purgeTrash(now time.Time) (int, int64, []string) {
	// With the bin switched off (0 days) anything left from before goes too.
	cutoff := now.AddDate(0, 0, -s.trashDays())
	total, freed := 0, int64(0)
	var errs []string
	for _, root := range s.trashRoots() {
		if root == "" {
			continue
		}
		n, b, err := cleanup.PurgeTrash(root, cutoff)
		total += n
		freed += b
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	return total, freed, errs
}

type trashItem struct {
	Kind      string   `json:"kind"`
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	DeletedAt string   `json:"deletedAt"`
	ExpiresAt string   `json:"expiresAt,omitempty"`
	Size      int64    `json:"size"`
	Files     int      `json:"files"`
	Paths     []string `json:"paths"`
}

type trashPayload struct {
	Days  int         `json:"days"`
	Items []trashItem `json:"items"`
}

func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	days := s.trashDays()
	out := trashPayload{Days: days, Items: []trashItem{}}
	for kind, root := range s.trashRoots() {
		entries, err := cleanup.ListTrash(root)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, e := range entries {
			it := trashItem{Kind: kind, ID: e.ID, Label: e.Label, DeletedAt: e.DeletedAt.Format(time.RFC3339), Size: e.Size, Files: e.Files, Paths: e.Paths}
			if days > 0 {
				it.ExpiresAt = e.DeletedAt.AddDate(0, 0, days).Format(time.RFC3339)
			}
			out.Items = append(out.Items, it)
		}
	}
	sortTrash(out.Items)
	writeJSON(w, http.StatusOK, out)
}

func sortTrash(items []trashItem) {
	sort.Slice(items, func(i, j int) bool { return items[i].DeletedAt > items[j].DeletedAt })
}

// trashRootFor returns the library folder for a bin kind, or "" when the kind
// is not one.
func (s *Server) trashRootFor(kind string) string {
	return s.trashRoots()[kind]
}

func (s *Server) handleRestoreTrash(w http.ResponseWriter, r *http.Request) {
	root := s.trashRootFor(r.PathValue("kind"))
	if root == "" {
		writeError(w, http.StatusNotFound, "That item is not in the recycle bin.")
		return
	}
	e, err := cleanup.RestoreTrash(root, r.PathValue("id"))
	switch {
	case errors.Is(err, cleanup.ErrTrashEntry):
		writeError(w, http.StatusNotFound, "That item is not in the recycle bin any more.")
		return
	case errors.Is(err, cleanup.ErrRestoreBlocked):
		writeError(w, http.StatusConflict, "Something is already where these files were, so nothing was moved. Move it out of the way and try again.")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	folder := ""
	if len(e.Paths) > 0 {
		folder = filepath.Dir(e.Paths[0])
		if len(e.Paths) == 1 {
			folder = e.Paths[0]
		}
	}
	_ = s.QueueRepo.LogActivity(0, "restored", fmt.Sprintf("Put the files of %s back from the recycle bin", e.Label))
	writeJSON(w, http.StatusOK, map[string]any{"label": e.Label, "folder": folder})
}

func (s *Server) handleDeleteTrash(w http.ResponseWriter, r *http.Request) {
	root := s.trashRootFor(r.PathValue("kind"))
	if root == "" {
		writeError(w, http.StatusNotFound, "That item is not in the recycle bin.")
		return
	}
	if err := cleanup.DeleteTrash(root, r.PathValue("id")); errors.Is(err, cleanup.ErrTrashEntry) {
		writeError(w, http.StatusNotFound, "That item is not in the recycle bin any more.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	n, freed := 0, int64(0)
	for _, root := range s.trashRoots() {
		c, b, err := cleanup.PurgeTrash(root, time.Now().Add(time.Hour))
		n += c
		freed += b
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if n > 0 {
		_ = s.QueueRepo.LogActivity(0, "cleanup", fmt.Sprintf("Emptied the recycle bin: %s, %s freed", plural(n, "item"), humanBytes(freed)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": n, "freed": freed})
}
