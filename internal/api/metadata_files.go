package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/metafiles"
	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/settings"
)

// NFO and artwork files for Kodi, Jellyfin and Emby (and the artwork Plex reads).
// Off by default, per kind of media. They are written when a file is imported
// and, on request, for what is in the library already. A file that is there is
// never overwritten unless the person chose to replace.

const (
	// metaHookTimeout bounds the lookups and downloads one import waits for.
	metaHookTimeout = 45 * time.Second
	// metaJobPause spaces the titles of the whole-library job out, so a big
	// library does not hit TMDB in one burst.
	metaJobPause = 120 * time.Millisecond
	// maxMetaImage is the biggest picture accepted from TMDB.
	maxMetaImage = 25 << 20
	// metaFailedShown is how many failures the job keeps to show.
	metaFailedShown = 40
)

// tmdbImageBase is where pictures are fetched from (tests point it at a fake).
var tmdbImageBase = metadata.ImageHost

type metaKindChoice struct {
	NFO     bool `json:"nfo"`
	Artwork bool `json:"artwork"`
}

func (c metaKindChoice) want() metafiles.Want { return metafiles.Want{NFO: c.NFO, Artwork: c.Artwork} }
func (c metaKindChoice) any() bool            { return c.NFO || c.Artwork }

// metaFilesSettings is what Settings > Library and files says: which kinds of file to
// write for movies and for shows, and whether a file that is there may be
// replaced.
type metaFilesSettings struct {
	Movies  metaKindChoice `json:"movies"`
	TV      metaKindChoice `json:"tv"`
	Replace bool           `json:"replace"`
}

func (s *Server) metaFiles() metaFilesSettings {
	var m metaFilesSettings
	v, _ := s.Settings.Get(settings.KeyMetadataFiles)
	if strings.TrimSpace(v) != "" {
		_ = json.Unmarshal([]byte(v), &m) // damaged text reads as everything off
	}
	return m
}

type metaFilesPayload struct {
	metaFilesSettings
	// HasTMDBKey is false while there is no TMDB key: the files are made from
	// TMDB's data, so nothing can be written until there is one.
	HasTMDBKey bool `json:"hasTmdbKey"`
}

func (s *Server) handleGetMetadataFiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, metaFilesPayload{metaFilesSettings: s.metaFiles(), HasTMDBKey: s.TMDB().HasAPIKey()})
}

func (s *Server) handlePutMetadataFiles(w http.ResponseWriter, r *http.Request) {
	var req metaFilesSettings
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	b, _ := json.Marshal(req)
	if err := s.Settings.Set(settings.KeyMetadataFiles, string(b), false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.handleGetMetadataFiles(w, r)
}

// ---- Content ---------------------------------------------------------------

// fetchImage downloads a TMDB picture. A path that is not a plain TMDB image
// path is refused, so nothing but TMDB's picture server is ever asked.
func (s *Server) fetchImage(ctx context.Context, path string) ([]byte, error) {
	if !metadata.ValidImagePath(path) {
		return nil, errors.New("TMDB gave a picture address that doesn't look right")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tmdbImageBase+"/t/p/original"+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := netguard.Client(40 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("download the picture: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download the picture: the picture server answered with status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMetaImage+1))
	if err != nil {
		return nil, fmt.Errorf("download the picture: %w", netguard.CleanError(err))
	}
	if len(data) > maxMetaImage {
		return nil, errors.New("the picture is too big")
	}
	return data, nil
}

// movieMeta makes the content of a movie's files from TMDB.
type movieMeta struct {
	s      *Server
	m      library.Movie
	detail *metadata.MovieDetail
	err    error
	loaded bool
}

func (mm *movieMeta) load(ctx context.Context) (*metadata.MovieDetail, error) {
	if !mm.loaded {
		mm.loaded = true
		mm.detail, mm.err = mm.s.displayTMDB().GetMovieDetail(ctx, mm.m.TMDBID)
	}
	return mm.detail, mm.err
}

func (mm *movieMeta) NFO(ctx context.Context, it metafiles.Item) ([]byte, error) {
	d, err := mm.load(ctx)
	if err != nil {
		return nil, fmt.Errorf("look the movie up: %w", err)
	}
	info := metafiles.Movie{
		Title: d.Title, OriginalTitle: d.OriginalTitle, Year: d.Year(), Premiered: isoDate(d.ReleaseDate),
		Plot: d.Overview, Tagline: d.Tagline, Runtime: d.Runtime, MPAA: d.Certification(),
		Rating: metadata.RoundRating(d.VoteAverage), Votes: d.VoteCount, TMDBID: d.TMDBID, IMDBID: d.IMDBID,
		Countries: d.Countries(), Studios: d.Studios(), Directors: d.Directors(), Writers: d.Writers(),
	}
	if info.Title == "" {
		info.Title = mm.m.Title
	}
	for _, g := range d.Movie.Genres {
		info.Genres = append(info.Genres, g.Name)
	}
	if c := d.BelongsToCollection; c != nil && c.ID > 0 {
		set := &metafiles.Set{Name: c.Name}
		if stored, ok, _ := mm.s.MovieRepo.GetCollection(c.ID); ok {
			set.Overview = stored.Overview
		}
		info.Set = set
	}
	for _, a := range d.Cast(20) {
		info.Actors = append(info.Actors, metafiles.Actor{Name: a.Name, Role: a.Character, Order: a.Order})
	}
	if t := d.Trailers(); len(t) > 0 {
		info.TrailerKey = t[0].Key
	}
	return info.NFO(), nil
}

func (mm *movieMeta) Image(ctx context.Context, it metafiles.Item) ([]byte, error) {
	d, err := mm.load(ctx)
	if err != nil {
		return nil, fmt.Errorf("look the movie up: %w", err)
	}
	path := ""
	switch it.Kind {
	case metafiles.KindPoster:
		path = d.PosterPath
	case metafiles.KindFanart:
		path = d.BackdropPath
	case metafiles.KindLogo:
		img, err := mm.s.displayTMDB().GetImages(ctx, "movie", mm.m.TMDBID)
		if err != nil {
			return nil, fmt.Errorf("look the logos up: %w", err)
		}
		path = metadata.BestLogo(img.Logos, mm.s.infoLanguage())
	}
	if path == "" {
		return nil, nil
	}
	return mm.s.fetchImage(ctx, path)
}

// showMeta makes the content of a show's files.
type showMeta struct {
	s      *Server
	sr     library.Series
	byFile map[string][]library.Episode
	detail *metadata.ShowFull
	err    error
	loaded bool
}

func (sm *showMeta) load(ctx context.Context) (*metadata.ShowFull, error) {
	if !sm.loaded {
		sm.loaded = true
		sm.detail, sm.err = sm.s.displayTMDB().GetShowFull(ctx, sm.sr.TMDBID)
	}
	return sm.detail, sm.err
}

func (sm *showMeta) NFO(ctx context.Context, it metafiles.Item) ([]byte, error) {
	if it.Kind == metafiles.KindEpisodeNFO {
		var eps []metafiles.Episode
		for _, e := range sm.byFile[it.Video] {
			eps = append(eps, metafiles.Episode{ShowTitle: sm.sr.Title, Title: e.Title, Season: e.Season, Episode: e.Episode, Aired: isoDate(e.AirDate), Plot: e.Overview})
		}
		if len(eps) == 0 {
			return nil, nil
		}
		return metafiles.EpisodesNFO(eps), nil
	}
	d, err := sm.load(ctx)
	if err != nil {
		return nil, fmt.Errorf("look the show up: %w", err)
	}
	info := metafiles.Show{
		Title: d.Name, OriginalTitle: d.OriginalName, Year: d.Year(), Premiered: isoDate(d.FirstAirDate),
		Plot: d.Overview, MPAA: d.ContentRating(), Status: showStatus(d.Status),
		Rating: metadata.RoundRating(d.VoteAverage), Votes: d.VoteCount,
		TMDBID: d.TMDBID, TVDBID: sm.sr.TVDBID, IMDBID: firstNonEmpty(sm.sr.IMDBID, d.ExternalIDs.IMDBID),
		Studios: d.NetworkNames(),
	}
	if info.Title == "" {
		info.Title = sm.sr.Title
	}
	for _, g := range d.Show.Genres {
		info.Genres = append(info.Genres, g.Name)
	}
	for _, a := range d.Cast(20) {
		info.Actors = append(info.Actors, metafiles.Actor{Name: a.Name, Role: a.Character, Order: a.Order})
	}
	return info.NFO(), nil
}

// showStatus turns TMDB's status into the two words media servers know.
func showStatus(tmdb string) string {
	switch strings.ToLower(tmdb) {
	case "returning series", "in production", "planned", "pilot":
		return "Continuing"
	case "ended", "canceled", "cancelled":
		return "Ended"
	}
	return ""
}

func (sm *showMeta) Image(ctx context.Context, it metafiles.Item) ([]byte, error) {
	d, err := sm.load(ctx)
	if err != nil {
		return nil, fmt.Errorf("look the show up: %w", err)
	}
	path := ""
	switch it.Kind {
	case metafiles.KindPoster:
		path = d.PosterPath
	case metafiles.KindFanart:
		path = d.BackdropPath
	case metafiles.KindSeasonPoster:
		for _, season := range d.Seasons {
			if season.SeasonNumber == it.Season {
				path = season.PosterPath
			}
		}
	case metafiles.KindLogo:
		img, err := sm.s.displayTMDB().GetImages(ctx, "tv", sm.sr.TMDBID)
		if err != nil {
			return nil, fmt.Errorf("look the logos up: %w", err)
		}
		path = metadata.BestLogo(img.Logos, sm.s.infoLanguage())
	}
	if path == "" {
		return nil, nil
	}
	return sm.s.fetchImage(ctx, path)
}

// ---- Planning --------------------------------------------------------------

type metaPlan struct {
	items  []metafiles.Item
	source metafiles.Source
}

// planMovie plans a movie's files from what is on disk. It makes no network
// call. A movie whose file is gone, or that is outside its library folder, has none.
func (s *Server) planMovie(m library.Movie, want metafiles.Want) metaPlan {
	if m.FilePath == "" || !want.NFO && !want.Artwork {
		return metaPlan{}
	}
	if _, err := os.Stat(m.FilePath); err != nil {
		return metaPlan{}
	}
	layout, ok := metafiles.MovieLayout(s.movieRootFor(m), m.FilePath)
	if !ok {
		return metaPlan{}
	}
	return metaPlan{items: metafiles.PlanMovie(layout, m.FilePath, want), source: &movieMeta{s: s, m: m}}
}

// planShow plans the files of a show from the episodes given (all that have a
// file, or just the ones imported now).
func (s *Server) planShow(sr library.Series, eps []library.Episode, want metafiles.Want) metaPlan {
	if !want.NFO && !want.Artwork {
		return metaPlan{}
	}
	byFile := map[string][]library.Episode{}
	var files []string
	seasons := map[int]bool{}
	for _, e := range eps {
		if e.FilePath == "" {
			continue
		}
		if _, err := os.Stat(e.FilePath); err != nil {
			continue
		}
		if _, seen := byFile[e.FilePath]; !seen {
			files = append(files, e.FilePath)
		}
		byFile[e.FilePath] = append(byFile[e.FilePath], e)
		seasons[e.Season] = true
	}
	if len(files) == 0 {
		return metaPlan{}
	}
	sort.Strings(files)
	var items []metafiles.Item
	if dir, ok := metafiles.ShowDir(s.seriesRootFor(sr), files); ok {
		var seasonList []int
		for n := range seasons {
			seasonList = append(seasonList, n)
		}
		items = append(items, metafiles.PlanShow(dir, seasonList, want)...)
	}
	if want.NFO {
		for _, f := range files {
			items = append(items, metafiles.PlanEpisode(f))
		}
	}
	return metaPlan{items: items, source: &showMeta{s: s, sr: sr, byFile: byFile}}
}

// ---- After an import -------------------------------------------------------

// metaFilesAfterMovie writes the NFO and pictures of a movie that was just
// imported, if that is switched on. Nothing here can fail an import: a problem
// is logged and the whole-library run can be used later.
func (s *Server) metaFilesAfterMovie(file string) {
	cfg := s.metaFiles()
	if !cfg.Movies.any() || !s.TMDB().HasAPIKey() {
		return
	}
	m, ok, err := s.MovieRepo.MovieByFile(file)
	if err != nil || !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), metaHookTimeout)
	defer cancel()
	s.runMetaPlan(ctx, s.planMovie(m, cfg.Movies.want()), cfg.Replace, m.Title)
}

// metaFilesAfterEpisodes does the same for episodes that were just imported.
func (s *Server) metaFilesAfterEpisodes(files []string) {
	cfg := s.metaFiles()
	if !cfg.TV.any() || !s.TMDB().HasAPIKey() {
		return
	}
	eps, err := s.MovieRepo.EpisodesByFiles(files)
	if err != nil || len(eps) == 0 {
		return
	}
	bySeries := map[int64][]library.Episode{}
	var order []int64
	for _, e := range eps {
		if _, seen := bySeries[e.SeriesID]; !seen {
			order = append(order, e.SeriesID)
		}
		bySeries[e.SeriesID] = append(bySeries[e.SeriesID], e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), metaHookTimeout)
	defer cancel()
	for _, id := range order {
		sr, err := s.MovieRepo.GetSeries(id)
		if err != nil {
			continue
		}
		s.runMetaPlan(ctx, s.planShow(sr, bySeries[id], cfg.TV.want()), cfg.Replace, sr.Title)
	}
}

func (s *Server) runMetaPlan(ctx context.Context, p metaPlan, replace bool, title string) metafiles.Result {
	if p.source == nil || len(metafiles.Count(p.items, replace)) == 0 {
		return metafiles.Result{Written: metafiles.Counts{}}
	}
	res := metafiles.Execute(ctx, p.items, p.source, replace)
	for _, f := range res.Failed {
		slog.Warn("NFO and artwork files: couldn't write one", "title", title, "file", f)
	}
	return res
}

// ---- The whole library -----------------------------------------------------

type metaRunRequest struct {
	DryRun bool   `json:"dryRun"`
	Kind   string `json:"kind"` // "all" (default), "movies" or "tv"
}

// metaCounts is a tally by kind of file.
type metaCounts struct {
	NFO           int `json:"nfo"`
	Posters       int `json:"posters"`
	Fanart        int `json:"fanart"`
	Logos         int `json:"logos"`
	SeasonPosters int `json:"seasonPosters"`
	EpisodeNFOs   int `json:"episodeNfos"`
}

func (c *metaCounts) add(k metafiles.Counts) {
	c.NFO += k[metafiles.KindNFO]
	c.Posters += k[metafiles.KindPoster]
	c.Fanart += k[metafiles.KindFanart]
	c.Logos += k[metafiles.KindLogo]
	c.SeasonPosters += k[metafiles.KindSeasonPoster]
	c.EpisodeNFOs += k[metafiles.KindEpisodeNFO]
}

func (c metaCounts) total() int {
	return c.NFO + c.Posters + c.Fanart + c.Logos + c.SeasonPosters + c.EpisodeNFOs
}

type metaStatus struct {
	Running  bool       `json:"running"`
	DryRun   bool       `json:"dryRun"`
	Kind     string     `json:"kind"`
	Titles   int        `json:"titles"` // titles to look at
	Done     int        `json:"done"`   // titles looked at so far
	Current  string     `json:"current,omitempty"`
	Needed   metaCounts `json:"needed"`  // what a dry run found it would write (logos may turn out unavailable)
	Written  metaCounts `json:"written"` // what a real run wrote
	Failed   []string   `json:"failed"`
	FailedN  int        `json:"failedCount"`
	Started  string     `json:"startedAt,omitempty"`
	Finished string     `json:"finishedAt,omitempty"`
	Canceled bool       `json:"canceled"`
	Message  string     `json:"message,omitempty"`
}

type metaFilesJob struct {
	mu     sync.Mutex
	status metaStatus
	cancel context.CancelFunc
}

func (j *metaFilesJob) snapshot() metaStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	st := j.status
	st.Failed = append([]string{}, st.Failed...)
	return st
}

// metaTitle is one movie or show the run looks at.
type metaTitle struct {
	movie    *library.Movie
	series   *library.Series
	episodes []library.Episode
}

func (s *Server) metaTitles(kind string, cfg metaFilesSettings) ([]metaTitle, error) {
	var out []metaTitle
	if (kind == "all" || kind == "movies") && cfg.Movies.any() {
		movies, err := s.MovieRepo.ListWithCopies()
		if err != nil {
			return nil, err
		}
		for i := range movies {
			if movies[i].FilePath != "" {
				out = append(out, metaTitle{movie: &movies[i]})
			}
		}
	}
	if (kind == "all" || kind == "tv") && cfg.TV.any() {
		shows, err := s.MovieRepo.ListSeriesWithCopies()
		if err != nil {
			return nil, err
		}
		for i := range shows {
			eps, err := s.MovieRepo.ListEpisodes(shows[i].ID)
			if err != nil {
				return nil, err
			}
			var have []library.Episode
			for _, e := range eps {
				if e.FilePath != "" {
					have = append(have, e)
				}
			}
			if len(have) > 0 {
				out = append(out, metaTitle{series: &shows[i], episodes: have})
			}
		}
	}
	return out, nil
}

func (s *Server) planTitle(t metaTitle, cfg metaFilesSettings) metaPlan {
	if t.movie != nil {
		return s.planMovie(*t.movie, cfg.Movies.want())
	}
	return s.planShow(*t.series, t.episodes, cfg.TV.want())
}

func titleName(t metaTitle) string {
	if t.movie != nil {
		return t.movie.Title
	}
	return t.series.Title
}

// handleRunMetadataFiles is POST /api/library/metadata-files/run: writes the
// files for what is in the library already. With dryRun it only counts what
// would be written and answers at once; otherwise it starts the run in the
// background and answers 202.
func (s *Server) handleRunMetadataFiles(w http.ResponseWriter, r *http.Request) {
	var req metaRunRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if req.Kind == "" {
		req.Kind = "all"
	}
	if req.Kind != "all" && req.Kind != "movies" && req.Kind != "tv" {
		writeError(w, http.StatusBadRequest, `kind is "all", "movies" or "tv".`)
		return
	}
	cfg := s.metaFiles()
	if !cfg.Movies.any() && !cfg.TV.any() {
		writeError(w, http.StatusConflict, "Turn on NFO or artwork files for movies or shows first, and save.")
		return
	}
	if !req.DryRun && !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Library and files > Movie and show info).")
		return
	}
	titles, err := s.metaTitles(req.Kind, cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if req.DryRun {
		st := metaStatus{DryRun: true, Kind: req.Kind, Titles: len(titles), Failed: []string{}}
		for _, t := range titles {
			p := s.planTitle(t, cfg)
			c := metafiles.Count(p.items, cfg.Replace)
			if len(c) > 0 {
				st.Done++ // here: titles that have something to write
			}
			st.Needed.add(c)
		}
		st.Message = dryRunMessage(st)
		writeJSON(w, http.StatusOK, st)
		return
	}

	j := &s.metaJob
	j.mu.Lock()
	if j.status.Running {
		j.mu.Unlock()
		writeError(w, http.StatusConflict, "The files are being written already. Wait for it to finish, or stop it first.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	j.status = metaStatus{Running: true, Kind: req.Kind, Titles: len(titles), Failed: []string{}, Started: time.Now().UTC().Format(time.RFC3339)}
	j.mu.Unlock()
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		s.runMetaJob(ctx, titles, cfg)
	}()
	writeJSON(w, http.StatusAccepted, j.snapshot())
}

func dryRunMessage(st metaStatus) string {
	total := st.Needed.total()
	if total == 0 {
		return "Nothing to write: every title already has the files you turned on."
	}
	msg := fmt.Sprintf("%s would be written for %s.", plural(total, "file"), plural(st.Done, "title"))
	if st.Needed.Logos > 0 {
		msg += " Some logos may not exist on TMDB, so the real number can be a little lower."
	}
	return msg
}

func (s *Server) runMetaJob(ctx context.Context, titles []metaTitle, cfg metaFilesSettings) {
	j := &s.metaJob
	for i, t := range titles {
		if ctx.Err() != nil {
			break
		}
		name := titleName(t)
		j.mu.Lock()
		j.status.Current = name
		j.mu.Unlock()

		p := s.planTitle(t, cfg)
		var res metafiles.Result
		if len(metafiles.Count(p.items, cfg.Replace)) > 0 {
			tctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			res = s.runMetaPlan(tctx, p, cfg.Replace, name)
			cancel()
			select {
			case <-ctx.Done():
			case <-time.After(metaJobPause):
			}
		}

		j.mu.Lock()
		j.status.Done = i + 1
		j.status.Written.add(res.Written)
		for _, f := range res.Failed {
			j.status.FailedN++
			if len(j.status.Failed) < metaFailedShown {
				j.status.Failed = append(j.status.Failed, name+": "+f)
			}
		}
		j.mu.Unlock()
	}

	j.mu.Lock()
	j.status.Running = false
	j.status.Current = ""
	j.status.Canceled = ctx.Err() != nil
	j.status.Finished = time.Now().UTC().Format(time.RFC3339)
	st := j.status
	j.status.Message = metaDoneMessage(st)
	j.cancel = nil
	msg := j.status.Message
	j.mu.Unlock()
	_ = s.QueueRepo.LogActivity(0, "metadata", msg)
}

func metaDoneMessage(st metaStatus) string {
	var msg string
	switch {
	case st.Canceled:
		msg = fmt.Sprintf("Stopped. %s written for %s so far.", plural(st.Written.total(), "file"), plural(st.Done, "title"))
	case st.Written.total() == 0 && st.FailedN == 0:
		msg = "Nothing to write: every title already has the files you turned on."
	default:
		msg = fmt.Sprintf("%s written.", plural(st.Written.total(), "file"))
	}
	if st.FailedN > 0 {
		msg += fmt.Sprintf(" %s could not be made.", plural(st.FailedN, "file"))
	}
	return msg
}

func (s *Server) handleMetadataFilesStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.metaJob.snapshot())
}

func (s *Server) handleCancelMetadataFiles(w http.ResponseWriter, r *http.Request) {
	j := &s.metaJob
	j.mu.Lock()
	if j.cancel != nil {
		j.cancel()
	}
	j.mu.Unlock()
	writeJSON(w, http.StatusOK, j.snapshot())
}
