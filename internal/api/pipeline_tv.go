package api

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/organizer"
	"github.com/ryanborg/mediarium/internal/parser"
	"github.com/ryanborg/mediarium/internal/quality"
	"github.com/ryanborg/mediarium/internal/queue"
	"github.com/ryanborg/mediarium/internal/settings"
)

// resolveTVTarget works out which season and episodes a release covers.
// The release title is authoritative (a search hit for "Show S02E05" is
// episode 5 no matter which episode's page the user clicked Grab from);
// the hints only fill in when the title carries no season/episode marker
// at all. An empty episode list with a season means a season pack.
func resolveTVTarget(releaseTitle string, hintSeason, hintEpisode int) (season int, episodes []int) {
	rel := parser.Parse(releaseTitle)
	if rel.Season > 0 {
		return rel.Season, rel.Episodes
	}
	if hintEpisode > 0 {
		return hintSeason, []int{hintEpisode}
	}
	return hintSeason, nil
}

// grabTVRelease enqueues a TV release for series and starts its pipeline in
// the background — the TV counterpart of grabRelease.
func (s *Server) grabTVRelease(series library.Series, hintSeason, hintEpisode int, releaseTitle, downloadURL string, sizeBytes int64, protocol indexers.Protocol) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentsEnabled() {
		return 0, errTorrentsDisabled
	}
	season, episodes := resolveTVTarget(releaseTitle, hintSeason, hintEpisode)
	if season == 0 {
		return 0, fmt.Errorf("can't tell which season %q is for — its title has no season marker and none was given", releaseTitle)
	}
	firstEpisode := 0
	if len(episodes) > 0 {
		firstEpisode = episodes[0]
	}

	queueID, err := s.QueueRepo.Enqueue(queue.Item{
		SeriesID: series.ID, Season: season, Episode: firstEpisode,
		ReleaseTitle: releaseTitle, NZBURL: downloadURL, SizeBytes: sizeBytes, Protocol: queue.Protocol(protocol),
	})
	if err != nil {
		return 0, err
	}
	grabbed := fmt.Sprintf("Grabbed %q for %s", releaseTitle, series.Title)
	_ = s.QueueRepo.LogActivity(0, "grabbed", grabbed)
	s.notifyEvent("grabbed", series.Title, grabbed)

	go func() {
		if err := s.runTVPipeline(queueID, series, season, episodes, releaseTitle, downloadURL, protocol); err != nil {
			log.Printf("api: tv pipeline for queue item %d failed: %v", queueID, err)
		}
	}()
	return queueID, nil
}

// runTVPipeline is the TV grab -> download -> organize flow: the shared
// download/repair/unpack half (prepareDownload), then each finished video
// file is matched to a library episode by the season/episode in its own
// filename and imported into "Series (Year)/Season NN/". A season pack
// therefore imports every episode it contains in one pass.
func (s *Server) runTVPipeline(queueID int64, series library.Series, season int, episodes []int, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx := context.Background()

	targets, err := s.tvGrabTargets(series.ID, season, episodes)
	if err != nil {
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, err.Error())
		return err
	}

	fail := func(stepErr error) error {
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, stepErr.Error())
		for _, ep := range targets {
			_ = s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusMissing, "", "")
		}
		message := fmt.Sprintf("%s: %v", series.Title, stepErr)
		_ = s.QueueRepo.LogActivity(0, "failed", message)
		s.notifyEvent("failed", series.Title, message)
		if isBadRelease(stepErr) {
			s.onBadRelease(releaseTitle, protocol, 0, series.ID, stepErr)
		}
		return stepErr
	}

	for _, ep := range targets {
		_ = s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloading, "", "")
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusDownloading, ""); err != nil {
		return fail(err)
	}

	dir, err := s.prepareDownload(ctx, queueID, downloadURL, protocol)
	if err != nil {
		return fail(err)
	}

	files, err := s.tvVideoFiles(dir, len(targets) == 1)
	if err != nil {
		return fail(err)
	}

	release := parser.Parse(releaseTitle)
	tier := quality.Classify(release)

	imported, skipped := 0, 0
	handled := map[int64]bool{}
	var subtitleItems []subtitleItem
	for _, file := range files {
		fileSeason, fileEpisodes := s.tvFileEpisodes(file, season, targets)
		if len(fileEpisodes) == 0 {
			continue // e.g. an extra that isn't recognisably an episode of this show
		}
		var eps []library.Episode
		for _, n := range fileEpisodes {
			ep, err := s.MovieRepo.GetEpisode(series.ID, fileSeason, n)
			if err != nil {
				continue // the file names an episode TMDB doesn't list for this series
			}
			eps = append(eps, ep)
		}
		if len(eps) == 0 {
			continue
		}

		destPath, err := s.buildTVDestPath(series, eps[0], release, file)
		if err != nil {
			return fail(err)
		}
		policy, isBetter := s.conflictPolicy(tier, func() (quality.Tier, bool) {
			return quality.Tier(eps[0].Quality), true
		})
		result, err := organizer.Import(file, destPath, policy, isBetter)
		if err != nil {
			return fail(fmt.Errorf("import %s: %w", filepath.Base(file), err))
		}
		if result.Skipped {
			// "Always ask" parking only exists for movies (a season pack
			// can hold dozens of files, and the resolve UI is per-item), so
			// for TV it behaves like skip — the existing file is kept.
			skipped++
			continue
		}
		for _, ep := range eps {
			if err := s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, string(tier), result.DestPath); err != nil {
				return fail(err)
			}
			handled[ep.ID] = true
			ep.FilePath = result.DestPath
			subtitleItems = append(subtitleItems, episodeSubtitleItem(series, ep))
		}
		imported += len(eps)
	}

	// Targets the release didn't actually deliver (a pack missing an
	// episode, or every file skipped) go back to missing rather than
	// staying stuck in "downloading".
	for _, ep := range targets {
		if !handled[ep.ID] {
			_ = s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusMissing, "", "")
		}
	}

	if imported == 0 {
		if skipped > 0 {
			return fail(fmt.Errorf("files for this release already exist in the library — nothing imported (import conflict policy: skip)"))
		}
		return fail(badRelease(fmt.Errorf("no episode of %s could be matched in the downloaded files", series.Title)))
	}

	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(dir, protocol)
	s.autoSubtitlesFor(subtitleItems...)
	message := fmt.Sprintf("%s: imported %d episode(s) from %s", series.Title, imported, releaseTitle)
	_ = s.QueueRepo.LogActivity(0, "imported", message)
	s.notifyEvent("imported", series.Title, message)
	return nil
}

// tvGrabTargets lists the library episodes this grab is expected to
// deliver: the specific episode(s) resolved for it, or — with none, meaning
// a season pack — every episode of that season not already downloaded.
func (s *Server) tvGrabTargets(seriesID int64, season int, episodes []int) ([]library.Episode, error) {
	if len(episodes) == 0 {
		return s.seasonEpisodes(seriesID, season, true)
	}
	var out []library.Episode
	for _, n := range episodes {
		ep, err := s.MovieRepo.GetEpisode(seriesID, season, n)
		if err != nil {
			continue
		}
		out = append(out, ep)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("none of the requested episodes (season %d) exist for this series", season)
	}
	return out, nil
}

func (s *Server) seasonEpisodes(seriesID int64, season int, skipDownloaded bool) ([]library.Episode, error) {
	all, err := s.MovieRepo.ListEpisodes(seriesID)
	if err != nil {
		return nil, err
	}
	var out []library.Episode
	for _, ep := range all {
		if ep.Season != season {
			continue
		}
		if skipDownloaded && ep.Status == library.StatusDownloaded {
			continue
		}
		out = append(out, ep)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no episodes to fill in season %d", season)
	}
	return out, nil
}

// tvVideoFiles picks the files worth importing: for a single-episode grab
// just the largest video (like a movie — anything else is sample/extras
// noise), for packs every non-sample video.
func (s *Server) tvVideoFiles(dir string, singleEpisode bool) ([]string, error) {
	if singleEpisode {
		f, err := organizer.FindLargestVideoFile(dir)
		if err != nil {
			return nil, badRelease(fmt.Errorf("locate episode file in completed download: %w", err))
		}
		return []string{f}, nil
	}
	files, err := organizer.FindVideoFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, badRelease(fmt.Errorf("no video files found in completed download"))
	}
	return files, nil
}

// tvFileEpisodes decides which season/episodes one downloaded file holds:
// its own filename wins; if the filename has no marker and the grab was for
// exactly one episode, that's the answer; otherwise the file is unknown.
func (s *Server) tvFileEpisodes(file string, grabSeason int, targets []library.Episode) (int, []int) {
	rel := parser.Parse(filepath.Base(file))
	if rel.Season > 0 && len(rel.Episodes) > 0 {
		return rel.Season, rel.Episodes
	}
	if len(targets) == 1 {
		return targets[0].Season, []int{targets[0].Episode}
	}
	return grabSeason, nil
}

// tvRoot is where the TV library lives: the Settings value, falling back to
// the TV_DIR container default so TV works out of the box in Docker.
func (s *Server) tvRoot() string {
	if v, _ := s.Settings.Get(settings.KeyTVPath); v != "" {
		return v
	}
	return s.cfg.TVDir
}

// buildTVDestPath renders "<tv root>/Series (Year)/Season NN/<episode file>"
// using the same naming preset and illegal-character handling as movies.
func (s *Server) buildTVDestPath(series library.Series, ep library.Episode, release parser.Release, videoFile string) (string, error) {
	root := s.tvRoot()
	if root == "" {
		return "", fmt.Errorf("TV library path not configured — set it in Settings")
	}

	preset, _ := s.Settings.Get(settings.KeyNamingPreset)
	format := organizer.TVPresets["plex"]
	if preset == "custom" {
		if custom, _ := s.Settings.Get(settings.KeyEpisodeNameFormat); custom != "" {
			format = custom
		}
	} else if p, ok := organizer.TVPresets[preset]; ok {
		format = p
	}

	ctx := organizer.NamingContext{
		SeriesTitle: series.Title, EpisodeTitle: ep.Title, Year: series.Year, TMDBID: series.TMDBID,
		Season: ep.Season, Episode: ep.Episode,
		Quality: release.Resolution, Source: release.Source, Codec: release.Codec,
		Edition: release.Edition, ReleaseGroup: release.Group,
	}
	mode, replacement := s.illegalCharSettings()
	clean := func(f string) string { return organizer.Sanitize(organizer.Render(f, ctx), mode, replacement) }

	return filepath.Join(root, clean(organizer.TVSeriesFolder), clean(organizer.TVSeasonFolder), clean(format)+filepath.Ext(videoFile)), nil
}
