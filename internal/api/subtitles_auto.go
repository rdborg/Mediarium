package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/settings"
	"github.com/ryanborg/mediarium/internal/subtitles"
)

const (
	subtitleSweepInterval   = 6 * time.Hour
	subtitleRetryAfter      = 3 * 24 * time.Hour
	maxSubtitleDownloadsRun = 20
)

var errNoSubtitle = errors.New("no subtitle found")

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

func (s *Server) autoSubtitlesEnabled() bool {
	v, _ := s.Settings.Get(settings.KeySubtitleAutoDownload)
	return v != "0"
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
// video file (any of .srt/.ass/.ssa/.sub named "<base>.<lang>.<ext>").
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
		case ".srt", ".ass", ".ssa", ".sub":
		default:
			continue
		}
		lang := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ext)
		// Tolerate trailing flags such as "en.forced" or "en.hi".
		if i := strings.Index(lang, "."); i >= 0 {
			lang = lang[:i]
		}
		out[lang] = true
	}
	return out
}

func (s *Server) missingSubtitleLanguages(it subtitleItem) []string {
	have := languagesOnDisk(it.filePath)
	var missing []string
	for _, l := range s.subtitleLanguages() {
		if !have[strings.ToLower(l)] {
			missing = append(missing, l)
		}
	}
	return missing
}

func (s *Server) writeSubtitle(ctx context.Context, it subtitleItem, lang string, fileID int) (string, error) {
	link, err := s.Subtitles().RequestDownload(ctx, fileID)
	if err != nil {
		return "", err
	}
	data, err := s.Subtitles().DownloadFile(ctx, link)
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
	s.notifyEvent("subtitle", name, subtitleMessage)
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

// subtitleSweep fetches missing subtitles for the whole library, at most
// limit downloads per run (OpenSubtitles meters downloads per day). Items it
// already failed to find something for recently are skipped unless force is
// set. It stops early when the daily quota is reached.
func (s *Server) subtitleSweep(ctx context.Context, force bool, limit int) (int, error) {
	if !s.Subtitles().HasAPIKey() {
		return 0, subtitles.ErrNoAPIKey
	}
	items, err := s.downloadedSubtitleItems()
	if err != nil {
		return 0, err
	}
	downloaded := 0
	for _, it := range items {
		if _, err := os.Stat(it.filePath); err != nil {
			continue // file moved or deleted outside Mediarium
		}
		for _, lang := range s.missingSubtitleLanguages(it) {
			if downloaded >= limit {
				return downloaded, nil
			}
			if !force {
				if recent, err := s.SubtitleAttempts.Recent(it.kind, it.id, lang, subtitleRetryAfter); err == nil && recent {
					continue
				}
			}
			_, err := s.fetchBestSubtitle(ctx, it, lang)
			switch {
			case err == nil:
				downloaded++
			case errors.Is(err, subtitles.ErrQuota):
				return downloaded, err
			default:
				if !errors.Is(err, errNoSubtitle) {
					log.Printf("subtitles: %s %d (%s): %v", it.kind, it.id, lang, err)
				}
				_ = s.SubtitleAttempts.Record(it.kind, it.id, lang)
			}
		}
	}
	return downloaded, nil
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
// item, in the background.
func (s *Server) autoSubtitlesFor(items ...subtitleItem) {
	if !s.Subtitles().HasAPIKey() {
		return
	}
	go func() {
		if !s.autoSubtitlesEnabled() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, it := range items {
			for _, lang := range s.missingSubtitleLanguages(it) {
				if _, err := s.fetchBestSubtitle(ctx, it, lang); err != nil {
					if errors.Is(err, subtitles.ErrQuota) {
						return
					}
					_ = s.SubtitleAttempts.Record(it.kind, it.id, lang)
				}
			}
		}
	}()
}
