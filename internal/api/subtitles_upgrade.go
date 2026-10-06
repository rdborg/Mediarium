package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
)

// Subtitle upgrades. When Mediarium picks a subtitle by its release name, it
// is usually in sync but not always. For a month after that, while subtitles
// download automatically, it looks again every few days for one made for the
// exact video file (OpenSubtitles matches it by the file's hash) and swaps
// it in. Subtitles picked by hand, or changed since (moved in time, edited),
// are left alone.

const (
	subtitleUpgradeWindow  = 30 * 24 * time.Hour
	subtitleUpgradeRecheck = 3 * 24 * time.Hour
	maxSubtitleUpgradeRun  = 5  // swaps per run
	maxSubtitleUpgradeLook = 40 // searches per run
	subtitleUpgradeReserve = 3  // daily downloads left for new imports
)

// subtitlePick is how a subtitle came to be chosen, kept with it.
type subtitlePick struct {
	fileID    int
	hashMatch bool   // made for the exact video file
	chosen    bool   // picked by hand
	videoHash string // the video's hash, when known
	upgrade   bool   // replaces a subtitle Mediarium picked before
}

// subtitleUpgradesOn: on unless switched off, and only while subtitles
// download automatically.
func (s *Server) subtitleUpgradesOn() bool {
	return s.autoSubtitlesEnabled() && s.subtitleUpgradeSetting()
}

func (s *Server) subtitleUpgradeSetting() bool { return s.settingOn(settings.KeySubtitleUpgrade, true) }

// videoHash is the OpenSubtitles hash of a video, or "" when it can't be read.
func videoHash(path string) string {
	if path == "" {
		return ""
	}
	h, err := subtitles.FileHash(path)
	if err != nil {
		return ""
	}
	return h
}

// recordSubtitle remembers a saved subtitle, so an upgrade can find it.
func (s *Server) recordSubtitle(it subtitleItem, lang, path string, pick subtitlePick) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	err = s.SubtitleFiles.Record(subtitles.SavedFile{
		Kind: it.kind, MediaID: it.id, Language: lang, FileID: pick.fileID, HashMatch: pick.hashMatch, Chosen: pick.chosen,
		VideoHash: pick.videoHash, Path: path, Size: info.Size(), ModTime: info.ModTime().Unix(),
	})
	if err != nil {
		log.Printf("subtitles: %v", err)
	}
}

// unchangedSubtitle reports whether a saved subtitle is still where and as
// Mediarium left it, next to the same video file.
func unchangedSubtitle(f subtitles.SavedFile, it subtitleItem) bool {
	if it.filePath == "" || f.Path != fmt.Sprintf("%s.%s.srt", subtitleBase(it.filePath), f.Language) {
		return false
	}
	if f.VideoHash != "" && videoHash(it.filePath) != f.VideoHash {
		return false // the video was replaced
	}
	info, err := os.Stat(f.Path)
	return err == nil && info.Size() == f.Size && info.ModTime().Unix() == f.ModTime
}

type subtitleUpgradeResult struct {
	looked, swapped int
}

// upgradeSubtitles looks for subtitles made for the exact video file to
// replace ones Mediarium picked by name in the last month.
func (s *Server) upgradeSubtitles(ctx context.Context) (subtitleUpgradeResult, error) {
	var res subtitleUpgradeResult
	now := time.Now()
	cands, err := s.SubtitleFiles.UpgradeCandidates(now.Add(-subtitleUpgradeWindow), now.Add(-subtitleUpgradeRecheck), maxSubtitleUpgradeLook)
	if err != nil {
		return res, err
	}
	for _, f := range cands {
		if ctx.Err() != nil || res.swapped >= maxSubtitleUpgradeRun {
			break
		}
		if st := s.subtitleQuota(); st.Remaining <= subtitleUpgradeReserve {
			break
		}
		it, err := s.loadSubtitleItem(f.Kind, f.MediaID)
		if errors.Is(err, errSubtitleItemNotFound) {
			_ = s.SubtitleFiles.Forget(f.Kind, f.MediaID, f.Language)
			continue
		}
		if err != nil {
			return res, err
		}
		if !unchangedSubtitle(f, it) {
			// Moved in time, edited, removed, or the video was replaced: not
			// Mediarium's to swap any more.
			_ = s.SubtitleFiles.Forget(f.Kind, f.MediaID, f.Language)
			continue
		}
		hash := videoHash(it.filePath)
		_ = s.SubtitleFiles.MarkChecked(f.Kind, f.MediaID, f.Language)
		if hash == "" {
			continue
		}
		q := it.query(f.Language)
		q.MovieHash = hash
		results, err := s.Subtitles().Find(ctx, q)
		res.looked++
		switch {
		case errors.Is(err, subtitles.ErrInvalidKey), errors.Is(err, subtitles.ErrLoginFailed), errors.Is(err, subtitles.ErrNoAPIKey):
			return res, err
		case err != nil:
			log.Printf("subtitles: look for a better %s subtitle for %s %d: %v", f.Language, f.Kind, f.MediaID, err)
			continue
		}
		best := subtitles.Pick(results, pathBase(it.filePath))
		if best == nil || !best.HashMatch || best.FileID == f.FileID {
			continue
		}
		_, err = s.writeSubtitle(ctx, it, f.Language, subtitlePick{fileID: best.FileID, hashMatch: true, videoHash: hash, upgrade: true})
		switch {
		case errors.Is(err, subtitles.ErrQuota):
			return res, nil
		case err != nil:
			log.Printf("subtitles: swap the %s subtitle for %s %d: %v", f.Language, f.Kind, f.MediaID, err)
			continue
		}
		res.swapped++
	}
	return res, nil
}
