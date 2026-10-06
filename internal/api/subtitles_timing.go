package api

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/mediafiles"
	"github.com/rdborg/mediarium/internal/subtitles"
)

// Subtitle timing (a title's Files panel): move a subtitle earlier or later,
// line it up with another subtitle of the same title that is in time, or put
// the original back. The original is kept next to it once, as "<name>.bak".

const maxSubtitleBytes = 10 << 20

type subtitleTimingRequest struct {
	Kind      string `json:"kind"` // "movie" or "series"
	ID        int64  `json:"id"`
	File      string `json:"file"`                // the subtitle, relative to the title's folder
	ShiftMs   int64  `json:"shiftMs,omitempty"`   // move by this much (negative = earlier)
	Reference string `json:"reference,omitempty"` // or: line it up with this subtitle
	Undo      bool   `json:"undo,omitempty"`      // or: put the original back
}

func subtitleFile(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".srt", ".vtt":
		return true
	}
	return false
}

func readSubtitle(folder mediafiles.Folder, rel string) ([]byte, error) {
	if !subtitleFile(rel) {
		return nil, fmt.Errorf("%s: %w", rel, mediafiles.ErrInvalidPath)
	}
	f, info, err := folder.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info.Size() > maxSubtitleBytes {
		return nil, errors.New("that subtitle file is too big")
	}
	return io.ReadAll(io.LimitReader(f, maxSubtitleBytes))
}

// writeSubtitle replaces a subtitle, keeping the first original as .bak.
func writeSubtitle(folder mediafiles.Folder, rel string, data []byte) error {
	root, err := os.OpenRoot(folder.Dir)
	if err != nil {
		return fmt.Errorf("open folder: %w", err)
	}
	defer root.Close()
	name := filepath.FromSlash(rel)
	if _, err := root.Stat(name + ".bak"); errors.Is(err, fs.ErrNotExist) {
		old, err := root.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read the subtitle: %w", err)
		}
		if err := root.WriteFile(name+".bak", old, 0o644); err != nil {
			return fmt.Errorf("keep the original: %w", err)
		}
	}
	tmp := name + ".mediarium-tmp"
	if err := root.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write the subtitle: %w", err)
	}
	if err := root.Rename(tmp, name); err != nil {
		_ = root.Remove(tmp)
		return fmt.Errorf("save the subtitle: %w", err)
	}
	return nil
}

// POST /api/subtitles/timing
func (s *Server) handleSubtitleTiming(w http.ResponseWriter, r *http.Request) {
	var req subtitleTimingRequest
	if err := decodeJSON(r, &req); err != nil || req.ID <= 0 || req.File == "" {
		writeError(w, http.StatusBadRequest, "Pick a subtitle file first.")
		return
	}
	var folder mediafiles.Folder
	var err error
	switch req.Kind {
	case "movie":
		m, gerr := s.MovieRepo.Get(req.ID)
		if gerr != nil {
			writeError(w, http.StatusNotFound, "That movie isn't in your library.")
			return
		}
		folder, err = s.movieFolder(m)
	case "series":
		folder, _, err = s.seriesFolder(req.ID)
	default:
		writeError(w, http.StatusBadRequest, `kind must be "movie" or "series".`)
		return
	}
	if err != nil {
		writeFolderError(w, err)
		return
	}
	if req.Undo {
		original, err := readSubtitleBackup(folder, req.File)
		if err != nil {
			writeError(w, http.StatusNotFound, "There is no earlier version of this subtitle to put back.")
			return
		}
		if err := writeSubtitle(folder, req.File, original); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": "The original timing is back."})
		return
	}
	data, err := readSubtitle(folder, req.File)
	if !s.openedOK(w, err) {
		return
	}
	scale, offset := 1.0, req.ShiftMs
	matched := 0.0
	if req.Reference != "" {
		ref, err := readSubtitle(folder, req.Reference)
		if !s.openedOK(w, err) {
			return
		}
		m, err := subtitles.Align(data, ref)
		if err != nil {
			writeError(w, http.StatusBadRequest, "One of the two subtitles has no timed lines, so they can't be lined up.")
			return
		}
		if m.Matched < 0.4 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("These two subtitles don't line up well (only %d%% of the lines fit at best), so nothing was changed. They may be for different cuts of the video.", int(m.Matched*100)))
			return
		}
		scale, offset, matched = m.Scale, m.OffsetMs, m.Matched
	} else if offset == 0 || offset < -3600000 || offset > 3600000 {
		writeError(w, http.StatusBadRequest, "Give how many seconds to move the subtitle, up to an hour either way.")
		return
	}
	out, err := subtitles.Retime(data, scale, offset)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That file has no timed subtitle lines.")
		return
	}
	if err := writeSubtitle(folder, req.File, out); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	msg := fmt.Sprintf("Moved %s by %.1f seconds.", path.Base(req.File), float64(offset)/1000)
	if req.Reference != "" {
		msg = fmt.Sprintf("Lined up with %s: %d%% of the lines now start together.", path.Base(req.Reference), int(matched*100+0.5))
		if scale != 1 {
			msg += " Its speed was adjusted too (made for another frame rate)."
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": msg, "offsetMs": offset, "scale": scale, "matched": matched})
}

func readSubtitleBackup(folder mediafiles.Folder, rel string) ([]byte, error) {
	if !subtitleFile(rel) {
		return nil, mediafiles.ErrInvalidPath
	}
	root, err := os.OpenRoot(folder.Dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if _, _, err := folder.Open(rel); err != nil { // the same path checks as reading
		return nil, err
	}
	return root.ReadFile(filepath.FromSlash(rel) + ".bak")
}
