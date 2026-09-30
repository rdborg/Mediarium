package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
)

const (
	subtitleSweepInterval   = 6 * time.Hour
	subtitleRetryAfter      = 3 * 24 * time.Hour
	maxSubtitleDownloadsRun = 20
)

var errNoSubtitle = errors.New("no subtitle found")

// errSubtitlesOff means subtitles are switched off in Settings, so nothing is
// searched for or downloaded.
var errSubtitlesOff = errors.New("Subtitles are switched off. Turn them on in Settings > Info, lists and subtitles > Subtitles.")

func pathBase(p string) string   { return filepath.Base(p) }
func lowerASCII(s string) string { return strings.ToLower(s) }
func sortSubtitleResults(r []subtitleResultPayload) {
	sort.SliceStable(r, func(i, j int) bool { return r[i].Score > r[j].Score })
}

// subtitleItem is a downloaded movie or episode that subtitles attach to.
type subtitleItem struct {
	kind       string // "movie" or "episode"
	id         int64
	title      string
	subtitle   string // "S01E02 · Name" for episodes
	tmdbID     int    // movie
	parentTMDB int    // series (episodes)
	season     int
	episode    int
	filePath   string
}

func (it subtitleItem) ref() subtitles.Item { return subtitles.Item{Kind: it.kind, ID: it.id} }

func movieSubtitleItem(m library.Movie) subtitleItem {
	return subtitleItem{kind: "movie", id: m.ID, title: m.Title, tmdbID: m.TMDBID, filePath: m.FilePath}
}

func episodeSubtitleItem(sr library.Series, ep library.Episode) subtitleItem {
	label := fmt.Sprintf("S%02dE%02d", ep.Season, ep.Episode)
	if ep.Title != "" {
		label += " · " + ep.Title
	}
	return subtitleItem{
		kind: "episode", id: ep.ID, title: sr.Title, subtitle: label,
		parentTMDB: sr.TMDBID, season: ep.Season, episode: ep.Episode, filePath: ep.FilePath,
	}
}

// subtitleLanguages are the languages to keep subtitles for (setting,
// comma-separated OpenSubtitles codes; default English).
func (s *Server) subtitleLanguages() []string {
	v, _ := s.Settings.Get(settings.KeySubtitleLanguages)
	var out []string
	for _, l := range strings.Split(v, ",") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return []string{"en"}
	}
	return out
}

// subtitlesEnabled reports whether the subtitles switch in Settings is on. It
// is off unless the person turned it on ("1"). While it is off nothing
// searches for or downloads subtitles, and nothing in the app mentions them
// (subtitles that come inside a download are still imported: that is import
// handling, not downloading).
func (s *Server) subtitlesEnabled() bool {
	v, _ := s.Settings.Get(settings.KeySubtitlesEnabled)
	return v == "1"
}

// refuseIfSubtitlesOff answers 409 and returns true while subtitles are
// switched off; the subtitle endpoints call it first.
func (s *Server) refuseIfSubtitlesOff(w http.ResponseWriter) bool {
	if s.subtitlesEnabled() {
		return false
	}
	writeError(w, http.StatusConflict, errSubtitlesOff.Error())
	return true
}

// autoSubtitleSetting is the stored "download automatically" choice, whether
// or not the subtitles switch is on.
func (s *Server) autoSubtitleSetting() bool {
	v, _ := s.Settings.Get(settings.KeySubtitleAutoDownload)
	return v == "1"
}

// autoSubtitlesEnabled reports whether Mediarium fetches subtitles on its own:
// subtitles are switched on and the person also chose automatic downloading
// ("1"). By default subtitles are offered, not fetched, so nothing spends
// OpenSubtitles' small daily download allowance without being asked.
func (s *Server) autoSubtitlesEnabled() bool {
	return s.subtitlesEnabled() && s.autoSubtitleSetting()
}

func (it subtitleItem) query(lang string) subtitles.Query {
	if it.kind == "episode" {
		return subtitles.Query{ParentTMDBID: it.parentTMDB, Season: it.season, Episode: it.episode, Type: "episode", Language: lang}
	}
	return subtitles.Query{TMDBID: it.tmdbID, Type: "movie", Language: lang}
}

// subtitleBase is the video path without its extension; external subtitles
// live next to it as "<base>.<lang>.srt", the convention Plex, Jellyfin and
// Kodi all recognise.
func subtitleBase(videoPath string) string {
	return strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
}

// languagesOnDisk lists the subtitle languages already sitting next to a
// video file (any of .srt/.ass/.ssa/.sub/.sup named "<base>.<lang>.<ext>").
// Forced-only subtitles (only the foreign-language lines) do not count: they
// are not a full subtitle in that language. Other flags such as "en.sdh" do.
func languagesOnDisk(videoPath string) map[string]bool {
	out := map[string]bool{}
	if videoPath == "" {
		return out
	}
	dir := filepath.Dir(videoPath)
	prefix := strings.ToLower(filepath.Base(subtitleBase(videoPath))) + "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if e.IsDir() || !strings.HasPrefix(name, prefix) {
			continue
		}
		ext := filepath.Ext(name)
		switch ext {
		case ".srt", ".ass", ".ssa", ".sub", ".sup":
		default:
			continue
		}
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ext), ".")
		forced := false
		for _, flag := range parts[1:] {
			if flag == "forced" || flag == "foreign" {
				forced = true
			}
		}
		if forced || parts[0] == "" {
			continue
		}
		out[parts[0]] = true
	}
	return out
}

func (s *Server) missingSubtitleLanguages(it subtitleItem) []string {
	have := languagesOnDisk(it.filePath)
	var missing []string
	for _, l := range s.subtitleLanguages() {
		if !subtitles.LanguageSatisfied(have, l) {
			missing = append(missing, l)
		}
	}
	return missing
}

// dismissedSubtitles returns the titles marked "no subtitles wanted". A read
// error is logged and treated as none dismissed.
func (s *Server) dismissedSubtitles() map[subtitles.Item]bool {
	m, err := s.SubtitleDismissed.All()
	if err != nil {
		log.Printf("subtitles: read dismissed titles: %v", err)
		return map[subtitles.Item]bool{}
	}
	return m
}

// noteSubtitleDownload records a download OpenSubtitles counted against the
// daily limit, and the quota figures its response carried.
func (s *Server) noteSubtitleDownload(info subtitles.DownloadInfo) {
	now := time.Now()
	if err := s.SubtitleQuota.RecordDownload(now); err != nil {
		log.Printf("subtitles: %v", err)
	}
	if info.HasRemaining {
		s.saveSubtitleQuotaReport(info, now)
		if info.Remaining == 0 {
			// That was the last download the limit allows.
			s.Usage.RecordLimitHit(serviceOpenSubtitles)
		}
	}
}

// noteSubtitleQuotaError remembers what a refused download said about the limit.
func (s *Server) noteSubtitleQuotaError(info subtitles.DownloadInfo) {
	if info.HasRemaining {
		s.saveSubtitleQuotaReport(info, time.Now())
	}
}

func (s *Server) saveSubtitleQuotaReport(info subtitles.DownloadInfo, at time.Time) {
	rep := subtitles.QuotaReport{
		Remaining: info.Remaining, Requests: info.Requests, HasRequests: info.HasRequests,
		ResetAt: info.ResetAt, ObservedAt: at,
	}
	if err := s.SubtitleQuota.SaveReport(rep); err != nil {
		log.Printf("subtitles: %v", err)
	}
}

// subtitleQuota is the current best knowledge of today's download allowance.
func (s *Server) subtitleQuota() subtitles.QuotaState {
	now := time.Now()
	rep, err := s.SubtitleQuota.LoadReport()
	if err != nil {
		log.Printf("subtitles: %v", err)
	}
	downloads, err := s.SubtitleQuota.DownloadsSince(now.Add(-subtitles.QuotaWindow))
	if err != nil {
		log.Printf("subtitles: %v", err)
	}
	return subtitles.ComputeQuota(now, s.Subtitles().HasCredentials(), rep, downloads)
}

func (s *Server) writeSubtitle(ctx context.Context, it subtitleItem, lang string, fileID int) (string, error) {
	info, err := s.Subtitles().RequestDownloadInfo(ctx, fileID)
	if err != nil {
		var qe *subtitles.QuotaError
		if errors.As(err, &qe) {
			s.noteSubtitleQuotaError(qe.Info)
		}
		return "", err
	}
	// OpenSubtitles counts the download once it hands out the link, whether or
	// not the file fetch below then succeeds.
	s.noteSubtitleDownload(info)
	data, err := s.Subtitles().DownloadFile(ctx, info.Link)
	if err != nil {
		return "", err
	}
	path := fmt.Sprintf("%s.%s.srt", subtitleBase(it.filePath), lang)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write subtitle file: %w", err)
	}
	movieID := int64(0)
	if it.kind == "movie" {
		movieID = it.id
	}
	name := it.title
	if it.subtitle != "" {
		name += " " + strings.SplitN(it.subtitle, " ", 2)[0]
	}
	subtitleMessage := fmt.Sprintf("Downloaded %s subtitle for %s", lang, name)
	_ = s.QueueRepo.LogActivity(movieID, "subtitle", subtitleMessage)
	subItem := notify.Item{Media: it.kind, Title: it.title, Language: subtitles.LanguageLabel(lang), Path: path}
	if it.kind == "movie" {
		subItem.LinkPath = fmt.Sprintf("/title/%d", it.tmdbID)
	} else {
		subItem.Episode = strings.SplitN(it.subtitle, " ", 2)[0]
	}
	s.notifyItem("subtitle", subItem)
	return path, nil
}

// fetchBestSubtitle finds the subtitle that best fits the video file and
// saves it next to it.
func (s *Server) fetchBestSubtitle(ctx context.Context, it subtitleItem, lang string) (string, error) {
	results, err := s.Subtitles().Find(ctx, it.query(lang))
	if err != nil {
		return "", err
	}
	best := subtitles.Pick(results, filepath.Base(it.filePath))
	if best == nil {
		return "", errNoSubtitle
	}
	return s.writeSubtitle(ctx, it, lang, best.FileID)
}

// downloadedSubtitleItems lists every downloaded movie and episode that has
// a file on disk.
func (s *Server) downloadedSubtitleItems() ([]subtitleItem, error) {
	var items []subtitleItem
	movies, err := s.MovieRepo.List()
	if err != nil {
		return nil, err
	}
	for _, m := range movies {
		if m.Status == library.StatusDownloaded && m.FilePath != "" {
			items = append(items, movieSubtitleItem(m))
		}
	}
	seriesList, err := s.MovieRepo.ListSeries()
	if err != nil {
		return nil, err
	}
	for _, sr := range seriesList {
		eps, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			return nil, err
		}
		for _, ep := range eps {
			if ep.Status == library.StatusDownloaded && ep.FilePath != "" {
				items = append(items, episodeSubtitleItem(sr, ep))
			}
		}
	}
	return items, nil
}

// subtitleBacklog counts the downloaded titles still missing a wanted-language
// subtitle (leaving out titles marked "no subtitles wanted") and how many
// subtitle files that comes to (title x language).
func (s *Server) subtitleBacklog() (titles, files int, err error) {
	items, err := s.downloadedSubtitleItems()
	if err != nil {
		return 0, 0, err
	}
	dismissed := s.dismissedSubtitles()
	for _, it := range items {
		if dismissed[it.ref()] {
			continue
		}
		if _, err := os.Stat(it.filePath); err != nil {
			continue
		}
		if n := len(s.missingSubtitleLanguages(it)); n > 0 {
			titles++
			files += n
		}
	}
	return titles, files, nil
}

type subtitleFetchOptions struct {
	force bool // ignore the "looked for it recently" back-off
	limit int  // stop after this many downloads; 0 means no limit
}

type subtitleFetchResult struct {
	downloaded int
	notFound   int  // asked OpenSubtitles, which had nothing
	skipped    int  // wanted subtitle files not tried: dismissed title, missing video file, or looked for recently
	remaining  int  // wanted subtitle files not tried because the run stopped early
	quota      bool // stopped because OpenSubtitles' daily limit is used up
	timedOut   bool // stopped because the time allowed ran out
}

// fetchSubtitles downloads the missing wanted-language subtitles for items,
// one per (title, language), pacing itself through the client. It leaves out
// titles marked "no subtitles wanted", stops cleanly at the first quota error
// (or when the context ends) and counts what it did not get to. Errors that
// mean the setup is wrong (bad key, bad login) end the run with that error.
func (s *Server) fetchSubtitles(ctx context.Context, items []subtitleItem, opts subtitleFetchOptions) (subtitleFetchResult, error) {
	var res subtitleFetchResult
	if !s.subtitlesEnabled() {
		return res, errSubtitlesOff
	}
	dismissed := s.dismissedSubtitles()
	stopped := false
	if st := s.subtitleQuota(); st.Source == subtitles.QuotaReported && st.Exceeded {
		// OpenSubtitles already said the limit is used up until its reset.
		stopped, res.quota = true, true
	}
	for _, it := range items {
		langs := s.missingSubtitleLanguages(it)
		if len(langs) == 0 {
			continue
		}
		if dismissed[it.ref()] || it.filePath == "" {
			res.skipped += len(langs)
			continue
		}
		if _, err := os.Stat(it.filePath); err != nil {
			res.skipped += len(langs) // file moved or deleted outside Mediarium
			continue
		}
		for _, lang := range langs {
			switch {
			case stopped:
				res.remaining++
				continue
			case opts.limit > 0 && res.downloaded >= opts.limit:
				stopped = true
				res.remaining++
				continue
			case ctx.Err() != nil:
				stopped, res.timedOut = true, true
				res.remaining++
				continue
			}
			if !opts.force {
				if recent, err := s.SubtitleAttempts.Recent(it.kind, it.id, lang, subtitleRetryAfter); err == nil && recent {
					res.skipped++
					continue
				}
			}
			_, err := s.fetchBestSubtitle(ctx, it, lang)
			switch {
			case err == nil:
				res.downloaded++
			case errors.Is(err, subtitles.ErrQuota):
				stopped, res.quota = true, true
				res.remaining++
			case ctx.Err() != nil:
				stopped, res.timedOut = true, true
				res.remaining++
			case errors.Is(err, subtitles.ErrInvalidKey), errors.Is(err, subtitles.ErrLoginFailed), errors.Is(err, subtitles.ErrNoAPIKey):
				return res, err
			default:
				if errors.Is(err, errNoSubtitle) {
					res.notFound++
				} else {
					log.Printf("subtitles: %s %d (%s): %v", it.kind, it.id, lang, err)
				}
				_ = s.SubtitleAttempts.Record(it.kind, it.id, lang)
			}
		}
	}
	return res, nil
}

// subtitleSweep fetches missing subtitles for the whole library, at most
// limit downloads per run (OpenSubtitles meters downloads per day). Items it
// already failed to find something for recently are skipped unless force is
// set. It stops early when the daily quota is reached, returning
// subtitles.ErrQuota.
func (s *Server) subtitleSweep(ctx context.Context, force bool, limit int) (int, error) {
	if !s.subtitlesEnabled() {
		return 0, errSubtitlesOff
	}
	if !s.Subtitles().HasAPIKey() {
		return 0, subtitles.ErrNoAPIKey
	}
	items, err := s.downloadedSubtitleItems()
	if err != nil {
		return 0, err
	}
	res, err := s.fetchSubtitles(ctx, items, subtitleFetchOptions{force: force, limit: limit})
	if err != nil {
		return res.downloaded, err
	}
	if res.quota {
		return res.downloaded, subtitles.ErrQuota
	}
	return res.downloaded, nil
}

func (s *Server) subtitleSweepJob(ctx context.Context) {
	if !s.Subtitles().HasAPIKey() || !s.autoSubtitlesEnabled() {
		return
	}
	if _, err := s.subtitleSweep(ctx, false, maxSubtitleDownloadsRun); err != nil && !errors.Is(err, subtitles.ErrQuota) {
		log.Printf("subtitles: sweep: %v", err)
	}
}

// autoSubtitlesFor fetches the configured languages for a just-imported
// item, in the background, when automatic downloading is on.
func (s *Server) autoSubtitlesFor(items ...subtitleItem) {
	if !s.autoSubtitlesEnabled() || !s.Subtitles().HasAPIKey() {
		return
	}
	go func() {
		if !s.autoSubtitlesEnabled() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := s.fetchSubtitles(ctx, items, subtitleFetchOptions{force: true}); err != nil {
			log.Printf("subtitles: fetch after import: %v", err)
		}
	}()
}

// sidecarsIn lists the subtitles that came with a finished download. A problem
// reading the folder is logged and means none.
func sidecarsIn(dir string) []subtitles.Sidecar {
	scs, err := subtitles.FindSidecars(dir)
	if err != nil {
		log.Printf("subtitles: %v", err)
		return nil
	}
	return scs
}

// importSidecarSubtitles copies the subtitles that came with a release next to
// an imported video (see subtitles.ImportSidecars) and notes it in the
// activity log. It never fails the import: a problem is only logged.
func (s *Server) importSidecarSubtitles(scs []subtitles.Sidecar, videoPath string, movieID int64, name string) {
	if len(scs) == 0 || videoPath == "" {
		return
	}
	imported, err := subtitles.ImportSidecars(videoPath, scs)
	if err != nil {
		log.Printf("subtitles: import subtitles that came with %s: %v", name, err)
	}
	if len(imported) == 0 {
		return
	}
	files := 0
	var labels []string
	seen := map[string]bool{}
	for _, sc := range imported {
		files += len(sc.Files)
		if l := sc.Label(); !seen[l] {
			seen[l] = true
			labels = append(labels, l)
		}
	}
	sort.Strings(labels)
	_ = s.QueueRepo.LogActivity(movieID, "subtitle", fmt.Sprintf("%s imported with %s: %s", name, plural(files, "subtitle file"), strings.Join(labels, ", ")))
}
