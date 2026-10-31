package api

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
)

// resolveTVTarget works out which season and episodes a release covers.
// The release title is authoritative (a search hit for "Show S02E05" is
// episode 5 no matter which episode's page the user clicked Grab from);
// the hints only fill in when the title carries no season/episode marker
// at all. An empty episode list with a season means a season pack.
func resolveTVTarget(releaseTitle string, hintSeason, hintEpisode int) (season int, episodes []int) {
	rel := parser.Parse(releaseTitle)
	if rel.Season > 0 || (rel.Special && len(rel.Episodes) > 0) {
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
	return s.grabTV(series, hintSeason, hintEpisode, releaseTitle, downloadURL, sizeBytes, protocol, grabPicked)
}

// grabTV is grabTVRelease with auto set for automation's grabs. The
// episodes the release will deliver (for a season pack, every episode of
// the season not yet downloaded) are marked as downloading in the same step
// as the grab is queued, so no automation run that looks afterwards can grab
// them again separately; an automatic grab is refused with
// errAlreadyGrabbed when any of them is already being downloaded.
func (s *Server) grabTV(series library.Series, hintSeason, hintEpisode int, releaseTitle, downloadURL string, sizeBytes int64, protocol indexers.Protocol, kind grabKind) (int64, error) {
	return s.grabTVOpts(series, hintSeason, hintEpisode, releaseTitle, downloadURL, sizeBytes, protocol, kind, false)
}

// grabTVOpts is grabTV for a release a person picked by hand and agreed to
// replace the files of the episodes it covers (byHand, see grabRequest.Replace).
func (s *Server) grabTVOpts(series library.Series, hintSeason, hintEpisode int, releaseTitle, downloadURL string, sizeBytes int64, protocol indexers.Protocol, kind grabKind, byHand bool) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentReleasesOn() {
		return 0, errTorrentsDisabled
	}
	season, episodes := resolveTVTarget(releaseTitle, hintSeason, hintEpisode)
	// Anime and daily names carry no season marker: read them the way the
	// show numbers its episodes ("[Group] Show - 14" is S02E01), so one
	// episode isn't taken for a whole season.
	raw := parser.Parse(releaseTitle)
	if rel := s.releaseMapper(series)(raw); raw.Season == 0 && !raw.Special {
		if (rel.Season > 0 || rel.Special) && len(rel.Episodes) > 0 {
			season, episodes = rel.Season, rel.Episodes
		}
	} else if rel.Season != raw.Season || !equalInts(rel.Episodes, raw.Episodes) {
		// Scene numbering ("Show S01E21" is S02E01 of the show): the name is
		// read through the show's numbering.
		season, episodes = rel.Season, rel.Episodes
	}
	if season == 0 && len(episodes) == 0 {
		return 0, fmt.Errorf("can't tell which season %q is for. Its title has no season marker and none was given", releaseTitle)
	}
	firstEpisode := 0
	if len(episodes) > 0 {
		firstEpisode = episodes[0]
	}
	if dest, err := s.buildTVDestPath(series, library.Episode{Season: season, Episode: firstEpisode}, parser.Parse(releaseTitle), releaseTitle, "check.mkv"); err == nil {
		if err := destWritable(filepath.Dir(dest)); err != nil {
			return 0, err
		}
	}

	s.grabMu.Lock()
	if !kind.refusesDuplicates() {
		// One download per episode: a grab by hand is refused while any
		// episode it covers is still downloading (automation skips those on
		// its own). Checked with the lock held, so two clicks at once cannot
		// both get through.
		if err := s.checkEpisodesNotDownloading(series.ID, hintSeason, hintEpisode, releaseTitle); err != nil {
			s.grabMu.Unlock()
			return 0, err
		}
	}
	if err := s.grabRefused(kind, releaseTitle, downloadURL); err != nil {
		s.grabMu.Unlock()
		return 0, err
	}
	targets, err := s.tvGrabTargets(series.ID, season, episodes)
	if err != nil {
		s.grabMu.Unlock()
		return 0, err
	}
	if err := s.claimEpisodes(targets, kind.refusesDuplicates()); err != nil {
		s.grabMu.Unlock()
		return 0, err
	}
	queueID, err := s.QueueRepo.Enqueue(queue.Item{
		SeriesID: series.ID, Season: season, Episode: firstEpisode,
		ReleaseTitle: releaseTitle, NZBURL: downloadURL, SizeBytes: sizeBytes, Protocol: queue.Protocol(protocol), Priority: kind.priority(), ReplaceByHand: byHand && kind == grabPicked,
	})
	if err != nil {
		// Nothing was queued: hand the episodes back.
		for _, ep := range targets {
			_ = s.MovieRepo.SetEpisodeStatus(ep.ID, ep.Status, "", "")
		}
		s.grabMu.Unlock()
		return 0, err
	}
	s.grabMu.Unlock()
	// The release covers these episodes (or the whole season): whatever was
	// being held for them no longer matters.
	for _, ep := range targets {
		s.forgetHeldEpisode(series.ID, ep.Season, ep.Episode)
	}
	if len(episodes) == 0 {
		s.forgetHeldEpisode(series.ID, season, 0)
	}

	grabbed := s.grabbedMessage(releaseTitle, series.Title)
	_ = s.QueueRepo.LogSeriesActivity(series.ID, "grabbed", grabbed)
	grabbedItem := s.localizedSeriesItem(series, season, episodes)
	grabbedItem.Quality, grabbedItem.SizeBytes, grabbedItem.Release = string(quality.Classify(parser.Parse(releaseTitle))), sizeBytes, releaseTitle
	s.seriesReleaseDetails(&grabbedItem, series, releaseTitle, downloadURL)
	s.notifyItem("grabbed", grabbedItem)

	s.dispatch.Kick()
	return queueID, nil
}

// runTVPipeline is the TV grab -> download -> organize flow: the shared
// download/repair/unpack half (prepareDownload), then each finished video
// file is matched to a library episode by the season/episode in its own
// filename and imported into "Series (Year)/Season NN/". A season pack
// therefore imports every episode it contains in one pass. targets are the
// episodes grabTV claimed for this grab.
func (s *Server) runTVPipeline(queueID int64, series library.Series, season int, targets []library.Episode, pack bool, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}

	handled := map[int64]bool{}
	// releaseTargets hands back the claimed episodes this grab did not
	// deliver: to "downloaded" for an upgrade that didn't happen (the old
	// file is still there), otherwise to "missing" so automation can try
	// again.
	releaseTargets := func() {
		for _, ep := range targets {
			if handled[ep.ID] {
				continue
			}
			status := library.StatusMissing
			if ep.Status == library.StatusDownloaded {
				status = library.StatusDownloaded
			}
			_ = s.MovieRepo.SetEpisodeStatus(ep.ID, status, "", "")
		}
	}

	fail := func(stepErr error) error {
		if run.cancelled.Load() {
			// The show was removed from the library: nothing to report or retry.
			_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, errRemovedFromLibrary.Error())
			return errRemovedFromLibrary
		}
		if handled, err := s.settleInterrupted(queueID, run); handled {
			return err // paused or stopped by a person: not a failure
		}
		if dup := asDuplicate(stepErr); dup != nil {
			s.settleDuplicate(queueID, dup)
			if !dup.holdsTitle() {
				releaseTargets()
			}
			return stepErr
		}
		// The person reads this message; the raw error goes to the log only
		// (noteDownloadFailure keeps it as the technical detail).
		plain := plainerror.Message(stepErr)
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, plain)
		noteDownloadFailure(queueID, series.Title, fmt.Sprintf("/series/%d", series.ID), stepErr)
		blocklisted := isBadRelease(stepErr) && s.blocklistBadRelease(releaseTitle, protocol, 0, series.ID, stepErr)
		releaseTargets()
		message := fmt.Sprintf("%s: %s", series.Title, plain)
		_ = s.QueueRepo.LogSeriesActivity(series.ID, "failed", message)
		failedItem := s.localizedSeriesItemFor(series, season, targets)
		failedItem.Reason, failedItem.Release = plain, releaseTitle
		if s.failureNotifyWanted(stepErr, downloadURL, releaseTitle) {
			s.notifyItem("failed", failedItem)
		}
		if blocklisted {
			s.retryAfterBadRelease(0, series.ID)
		}
		return stepErr
	}

	if err := s.QueueRepo.SetStatus(queueID, queue.StatusDownloading, ""); err != nil {
		return fail(err)
	}

	dir, err := s.prepareDownload(ctx, queueID, downloadURL, protocol)
	if err != nil {
		return fail(err)
	}

	// A season pack with only one episode still missing is a pack: every file
	// goes to the episode its own name says, not the biggest one to the
	// missing episode.
	single := len(targets) == 1 && !pack
	files, err := s.tvVideoFiles(dir, single)
	if err != nil {
		return fail(err)
	}
	fileTargets := targets // a file without an episode marker can only be the one episode asked for
	if !single {
		fileTargets = nil
	}

	release := parser.Parse(releaseTitle)
	tier := quality.Classify(release)

	sidecars := s.sidecarsIn(dir)
	if ctx.Err() != nil {
		return fail(ctx.Err()) // cancelled: never import for a show that was removed
	}

	mapper := s.releaseMapper(series) // anime and daily file names
	imported, skipped := 0, 0
	keptFile := "" // the first file of the library that a release of the download was turned away for
	// A release picked by hand, after the person agreed to replace the files
	// the episodes have: it takes their place whatever "Better versions" and
	// "If a file already exists" say.
	byHand := false
	if it, err := s.QueueRepo.Get(queueID); err == nil {
		byHand = it.ReplaceByHand
	}
	importProfile := quality.Profile{} // ranks the new file against the old one ("overwrite if better")
	if ps, err := s.loadProfiles(); err == nil {
		importProfile = s.profileForSeries(ps, series)
	}
	copiedNote := "" // set when files had to be copied over from the temporary download folder
	// Files the library has down for other titles, and the places this run
	// has already filled: two episodes of one pack never overwrite each other,
	// and no file of another episode or movie is ever replaced.
	owners := s.trackedFileOwners()
	usedDest := map[string]bool{}
	var importedFiles []string // for the media server refresh
	var importedEps []library.Episode
	var subtitleItems []subtitleItem
	var tooShort error // the first file that was far shorter than its episode
	// How many files had a random name replaced, and by what.
	namedByReleaseCount, namedByPresetCount := 0, 0
	for _, file := range files {
		fileSeason, fileEpisodes := s.tvFileEpisodes(file, season, fileTargets, mapper)
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

		if err := s.checkEpisodeLength(ctx, series.TMDBID, eps[0].Season, file); err != nil {
			// Not filed: this file is a fake, a sample or cut off. The rest of a
			// pack is imported as usual.
			slog.Warn("import: a file is far shorter than its episode, left out", "file", filepath.Base(file))
			if tooShort == nil {
				tooShort = err
			}
			if seconds, ok := videoSeconds(file); ok {
				_ = s.QueueRepo.LogSeriesActivity(series.ID, "skipped", fmt.Sprintf("%s: the file %s was left out because it looks like a fake or broken release (it is only %d min long).", series.Title, filepath.Base(file), lengthMinutes(seconds)))
			}
			continue
		}
		// The release's name can stand in for a random file name only when it
		// says which episode the one file is: a single episode, one file, and a
		// release name that names exactly one episode.
		releaseMayName := releaseNamesTheFile(single, len(files), len(eps), release)
		destPath, nameFrom, err := s.buildTVDestPathFor(series, eps[0], release, releaseTitle, file, releaseMayName)
		if err != nil {
			return fail(err)
		}
		epIDs := make([]int64, len(eps))
		for i, ep := range eps {
			epIDs[i] = ep.ID
		}
		if usedDest[filepath.Clean(destPath)] {
			slog.Warn("import: two files of one release would get the same name, the second is left out", "file", filepath.Base(file), "dest", destPath)
			skipped++
			continue
		}
		if owner := otherFileOwner(owners, destPath, 0, epIDs); owner != "" {
			slog.Warn("import: the destination is another title's file, left out", "file", filepath.Base(file), "dest", destPath, "owner", owner)
			_ = s.QueueRepo.LogSeriesActivity(series.ID, "skipped", errFileOfAnother(owner, destPath).Error())
			skipped++
			continue
		}
		policy, isBetter := s.conflictPolicy(incomingRelease{title: releaseTitle, tier: tier}, !series.NoUpgrade, importProfile, func() (currentFile, bool) {
			t, ok := existingTier(eps[0].Quality, eps[0].ReleaseTitle, eps[0].FilePath)
			return currentFile{tier: t, release: eps[0].ReleaseTitle}, ok
		})
		replaceEarlier := replacesOldFiles(policy, isBetter)
		earlierFiles := make([]string, len(eps))
		for i, ep := range eps {
			earlierFiles[i] = ep.FilePath
		}
		swapInPlace := handPickedSwap(byHand, destPath, earlierFiles)
		if handPickedReplaces(byHand, earlierFiles) {
			replaceEarlier = true
		}
		keepsEarlier := ""
		for _, old := range earlierFiles {
			if keepsEarlier == "" && keepsEarlierFile(old, destPath, replaceEarlier) {
				keepsEarlier = old
			}
		}
		if keepsEarlier != "" {
			// An episode's file under another name stays when this release may not
			// replace it: no second copy next to it.
			skipped++
			if keptFile == "" {
				keptFile = keepsEarlier
			}
			continue
		}
		var result organizer.ImportResult
		if swapInPlace {
			var binned bool
			result, binned, err = s.importSwapping(s.fileRoot(s.seriesRootFor(series), destPath), file, destPath, seriesLabel(series), 0, epIDs)
			if err == nil && binned {
				_ = s.QueueRepo.LogSeriesActivity(series.ID, "removed", fmt.Sprintf("%s: removed %s, which the new file replaced", fmt.Sprintf("%s S%02dE%02d", series.Title, eps[0].Season, eps[0].Episode), filepath.Base(destPath)))
			}
		} else {
			result, err = s.importDownloaded(file, destPath, policy, isBetter)
		}
		if err != nil {
			return fail(fmt.Errorf("import %s: %w", filepath.Base(file), err))
		}
		if result.Skipped {
			// "Always ask" parking only exists for movies (a season pack
			// can hold dozens of files, and the resolve UI is per-item), so
			// for TV it behaves like skip — the existing file is kept.
			skipped++
			if keptFile == "" {
				keptFile = destPath
			}
			continue
		}
		if n := s.copiedFromTempNote(file, result); n != "" {
			copiedNote = n
		}
		switch nameFrom {
		case namedByRelease:
			namedByReleaseCount++
		case namedByPresetInstead:
			namedByPresetCount++
		}
		usedDest[filepath.Clean(result.DestPath)] = true
		usedDest[filepath.Clean(destPath)] = true
		for _, ep := range eps {
			if err := s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, string(tier), result.DestPath); err == nil {
				s.afterEpisodeImported(ep.ID, releaseTitle)
				s.forgetHeldEpisode(ep.SeriesID, ep.Season, ep.Episode)
			} else {
				return fail(err)
			}
			handled[ep.ID] = true
			ep.FilePath = result.DestPath
			subtitleItems = append(subtitleItems, episodeSubtitleItem(series, ep))
		}
		if replaceEarlier {
			// The new file has another name than the ones it replaces: they go,
			// unless another episode still uses one of them.
			for _, old := range earlierFiles {
				s.removeReplacedFile(s.seriesRootFor(series), old, result.DestPath, 0, series.ID, fmt.Sprintf("%s S%02dE%02d", series.Title, eps[0].Season, eps[0].Episode))
			}
		}
		// Subtitles that came with the release go next to this episode. In a
		// pack they are matched by season and episode; one that cannot be
		// matched reliably is left out.
		var mine []subtitles.Sidecar
		for _, sc := range sidecars {
			if sc.MatchesEpisodes(fileSeason, fileEpisodes, len(files) == 1) {
				mine = append(mine, sc)
			}
		}
		s.importSidecarSubtitles(mine, result.DestPath, 0, fmt.Sprintf("%s S%02dE%02d", series.Title, eps[0].Season, eps[0].Episode))
		imported += len(eps)
		importedFiles = append(importedFiles, result.DestPath)
		importedEps = append(importedEps, eps...)
	}

	// Targets the release didn't actually deliver (a pack missing an
	// episode, or every file skipped) are handed back rather than staying
	// stuck in "downloading".
	releaseTargets()

	if imported == 0 {
		if tooShort != nil && skipped == 0 {
			// Nothing in the download was long enough: it is a bad release.
			s.discardWorkDir(dir, protocol)
			return fail(tooShort)
		}
		if skipped > 0 {
			// "If a file already exists" said to keep what is there: a decision,
			// not a failure (no "failed" notification, nothing in Logs and errors).
			if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, keptExistingMessage); err != nil {
				return fail(err)
			}
			data := s.discardWorkDirReport(dir, protocol)
			s.recordKept(queueID, keptFile, data)
			_ = s.QueueRepo.LogSeriesActivity(series.ID, "kept", fmt.Sprintf("%s: the files of %s are already in your library, so Mediarium kept them and imported nothing.", series.Title, releaseTitle))
			s.logKeptData(0, series.ID, data, releaseTitle)
			return nil
		}
		return fail(badRelease(fmt.Errorf("no episode of %s could be matched in the downloaded files", series.Title)))
	}

	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(dir, protocol)
	// Earlier failed tries for the episodes this delivered are settled now.
	bySeason := map[int][]int{}
	for _, ep := range importedEps {
		bySeason[ep.Season] = append(bySeason[ep.Season], ep.Episode)
	}
	for sn, eps := range bySeason {
		_, _ = s.QueueRepo.ClearFailedForEpisodes(series.ID, sn, eps, queueID)
	}
	subtitleNote := s.importSubtitleNote(subtitleItems...)
	s.autoSubtitlesFor(subtitleItems...)
	message := fmt.Sprintf("%s: imported %s from %s", series.Title, plural(imported, "episode"), releaseTitle) + copiedNote
	_ = s.QueueRepo.LogSeriesActivity(series.ID, "imported", message)
	for _, note := range []string{renamedNote(series.Title, namedByRelease, namedByReleaseCount), renamedNote(series.Title, namedByPresetInstead, namedByPresetCount)} {
		if note != "" {
			_ = s.QueueRepo.LogSeriesActivity(series.ID, "renamed", note)
		}
	}
	s.checkFinishedShowSoon(series.ID)
	importedItem := s.localizedSeriesItemFor(series, season, importedEps)
	importedItem.Quality, importedItem.SizeBytes, importedItem.Release = string(tier), fileSize(importedFiles...), releaseTitle
	s.seriesReleaseDetails(&importedItem, series, releaseTitle, downloadURL)
	importedItem.Subtitles = subtitleNote
	if len(importedFiles) == 1 {
		importedItem.Path = importedFiles[0]
	} else if len(importedFiles) > 1 {
		importedItem.Path = filepath.Dir(importedFiles[0])
	}
	s.notifyItem("imported", importedItem)
	s.episodesImported(importedFiles...)
	s.pushTagsAfterImport(library.TagSeries, series.ID)
	return nil
}

// releaseNamesTheFile reports whether the release's name can stand in for a
// random file name: the grab was for a single episode, the download holds one
// video, the file is one episode and the release's name names exactly one.
// Anything else (a season pack, a double episode) would give several files
// the same name, or a name that says nothing about which episode a file is.
func releaseNamesTheFile(single bool, files, episodes int, release parser.Release) bool {
	return single && files == 1 && episodes == 1 && len(release.Episodes) == 1
}

// tvGrabTargets lists the library episodes this grab is expected to
// deliver: the specific episode(s) resolved for it, or — with none, meaning
// a season pack — every episode of that season not already downloaded.
func (s *Server) tvGrabTargets(seriesID int64, season int, episodes []int) ([]library.Episode, error) {
	if len(episodes) == 0 {
		eps, err := s.seasonEpisodes(seriesID, season, true)
		if err != nil {
			// Every episode is downloaded already: a season pack picked to
			// replace them goes through the import settings, which decide
			// per file (better quality, or "overwrite").
			if all, err2 := s.seasonEpisodes(seriesID, season, false); err2 == nil {
				return all, nil
			}
		}
		return eps, err
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
		f, err := largestVideoFile(dir)
		if err != nil {
			return nil, badRelease(fmt.Errorf("couldn't find the episode file in the finished download: %w", err))
		}
		return []string{f}, nil
	}
	files, err := organizer.FindVideoFiles(dir, findOptions(dir)...)
	if err != nil {
		return nil, err
	}
	files = dropSkipped(dir, files, readSkipped(dir)) // files the torrent left out
	if len(files) == 0 {
		return nil, badRelease(fmt.Errorf("no video files found in the finished download"))
	}
	return files, nil
}

// tvFileEpisodes decides which season/episodes one downloaded file holds:
// its own filename wins; if the filename has no marker and the grab was for
// exactly one episode, that's the answer; otherwise the file is unknown.
func (s *Server) tvFileEpisodes(file string, grabSeason int, targets []library.Episode, mapper func(parser.Release) parser.Release) (int, []int) {
	rel := parseFor(mapper, filepath.Base(file))
	if (rel.Season > 0 || rel.Special) && len(rel.Episodes) > 0 {
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
// releaseTitle is the release's own name, for {Original Title}.
func (s *Server) buildTVDestPath(series library.Series, ep library.Episode, release parser.Release, releaseTitle, videoFile string) (string, error) {
	dest, _, err := s.buildTVDestPathFor(series, ep, release, releaseTitle, videoFile, false)
	return dest, err
}

// buildTVDestPathFor is buildTVDestPath that also says where the file's name
// came from. releaseMayName is true for a single episode in a download of its
// own, where the release's name can stand in for a random file name.
func (s *Server) buildTVDestPathFor(series library.Series, ep library.Episode, release parser.Release, releaseTitle, videoFile string, releaseMayName bool) (string, nameSource, error) {
	root := s.seriesRootFor(series)
	if root == "" && series.CopyOf != 0 {
		return "", namedByPreset, fmt.Errorf("the folder for second copies isn't set. Choose one in Settings > Library > Folders")
	}
	if root == "" {
		return "", namedByPreset, fmt.Errorf("the TV folder isn't set. Choose one in Settings > Library > Folders")
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
		Season: ep.Season, Episode: ep.Episode, AirDate: ep.AirDate,
		Quality: release.Resolution, Source: release.Source, Codec: release.Codec,
		Edition: release.Edition, ReleaseGroup: release.Group,
		AudioCodec: release.AudioCodec, HDR: release.HDR, Is3D: release.Is3D,
		Proper: release.Proper, Repack: release.Repack, Languages: release.Languages,
		OriginalTitle: releaseTitle, OriginalFilename: originalStem(videoFile),
	}
	mode, replacement := s.illegalCharSettings()
	// The show folder and the episode file use the title in the info language.
	// Indexers are still searched for the English title the library stores.
	ctx.SeriesTitle = s.folderTitle(series.TMDBID, "tv", series.Title)
	clean := func(f string) string { return organizer.Sanitize(organizer.Render(f, ctx), mode, replacement) }
	showFolder := clean(s.seriesFolderFormat())

	folder := filepath.Join(root, showFolder, clean(organizer.TVSeasonFolder))
	if s.seriesFolderFormat() != organizer.DefaultSeriesFolder || s.folderNameWouldChange(series.TMDBID, "tv", series.Title) {
		// The folder format is for new shows: one that has files keeps its folder.
		// A show named in another language keeps it too, so a language change
		// never splits the episodes into a second folder.
		if dir, ok := s.existingSeriesDir(series, root); ok {
			folder = filepath.Join(dir, clean(organizer.TVSeasonFolder))
		}
	}
	// Each file of a season pack keeps its own name.
	name, src := s.keptName(videoFile, releaseTitle, releaseMayName, mode, replacement)
	if name != "" {
		return filepath.Join(folder, name), src, nil
	}
	return filepath.Join(folder, clean(format)+filepath.Ext(videoFile)), src, nil
}
