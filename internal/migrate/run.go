package migrate

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/libimport"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

// Steps of an import, in order.
const (
	StepReading       = "reading"
	StepMovies        = "movies"
	StepSeries        = "series"
	StepIndexers      = "indexers"
	StepUsenetServers = "usenetServers"
	StepRequests      = "requests"
	StepSubtitles     = "subtitleLanguages"
	StepDone          = "done"
	StepFailed        = "failed"
)

// Outcomes of an imported item.
const (
	OutcomeAdded   = "added"
	OutcomeExists  = "exists"
	OutcomeSkipped = "skipped"
	OutcomeFailed  = "failed"
)

// Counts are an import's results for one kind of thing.
type Counts struct {
	Added    int `json:"added"`
	Existing int `json:"existing"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
	// FilesLinked counts files registered in place: movie files for
	// movies, episodes for series.
	FilesLinked int `json:"filesLinked"`
	// Disabled counts indexers or servers added switched off.
	Disabled int `json:"disabled"`
}

func (c *Counts) add(outcome string) {
	switch outcome {
	case OutcomeAdded:
		c.Added++
	case OutcomeExists:
		c.Existing++
	case OutcomeSkipped:
		c.Skipped++
	default:
		c.Failed++
	}
}

// Results are an import's counts, and the quality profile mapping used.
type Results struct {
	Movies        Counts `json:"movies"`
	Series        Counts `json:"series"`
	Indexers      Counts `json:"indexers"`
	UsenetServers Counts `json:"usenetServers"`
	Requests      Counts `json:"requests"`
	// SubtitleLanguages: added 1 = the languages were replaced, existing 1 = already the same.
	SubtitleLanguages Counts           `json:"subtitleLanguages"`
	QualityProfiles   []ProfileMapping `json:"qualityProfiles"`
}

// Outcome is what happened to one item.
type Outcome struct {
	Kind    string `json:"kind"` // movie, series, indexer, usenetServer, request, subtitleLanguages
	Name    string `json:"name"`
	Outcome string `json:"outcome"` // added, exists, skipped, failed
	Reason  string `json:"reason,omitempty"`
}

// Status is the current (or last) import.
type Status struct {
	Running    bool      `json:"running"`
	Step       string    `json:"step"`
	Done       int       `json:"done"`
	Total      int       `json:"total"`
	StartedAt  string    `json:"startedAt,omitempty"`
	FinishedAt string    `json:"finishedAt,omitempty"`
	Results    Results   `json:"results"`
	Items      []Outcome `json:"items"`
	Errors     []string  `json:"errors"`
	Notes      []string  `json:"notes"`
}

func emptyStatus() Status {
	return Status{Results: Results{QualityProfiles: []ProfileMapping{}}, Items: []Outcome{}, Errors: []string{}, Notes: []string{}}
}

// Status returns a copy of the current (or last) import's progress.
func (im *Importer) Status() Status {
	im.mu.Lock()
	defer im.mu.Unlock()
	s := im.status
	s.Items = append([]Outcome{}, s.Items...)
	s.Errors = append([]string{}, s.Errors...)
	s.Notes = append([]string{}, s.Notes...)
	s.Results.QualityProfiles = append([]ProfileMapping{}, s.Results.QualityProfiles...)
	return s
}

func (im *Importer) update(f func(s *Status)) {
	im.mu.Lock()
	f(&im.status)
	im.mu.Unlock()
}

func (im *Importer) fail(msg string) {
	im.update(func(s *Status) { s.Errors = append(s.Errors, msg) })
}

// Start begins an import in the background; Status follows it. It returns
// ErrRunning while one is already going.
func (im *Importer) Start(opts Options) error {
	if err := im.begin(); err != nil {
		return err
	}
	go im.run(context.Background(), opts)
	return nil
}

// Run carries out an import and waits for it to finish (Start runs it in
// the background).
func (im *Importer) Run(ctx context.Context, opts Options) (Status, error) {
	if err := im.begin(); err != nil {
		return Status{}, err
	}
	im.run(ctx, opts)
	return im.Status(), nil
}

func (im *Importer) begin() error {
	im.mu.Lock()
	defer im.mu.Unlock()
	if im.status.Running {
		return ErrRunning
	}
	im.status = emptyStatus()
	im.status.Running, im.status.Step = true, StepReading
	im.status.StartedAt = time.Now().UTC().Format(time.RFC3339)
	return nil
}

func (im *Importer) finish(step string) {
	im.update(func(s *Status) {
		s.Running, s.Step = false, step
		s.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	})
}

func (im *Importer) record(kind, name, outcome, reason string, counts func(r *Results) *Counts) {
	im.update(func(s *Status) {
		counts(&s.Results).add(outcome)
		s.Items = append(s.Items, Outcome{Kind: kind, Name: name, Outcome: outcome, Reason: reason})
		s.Done++
		if outcome == OutcomeFailed {
			s.Errors = append(s.Errors, fmt.Sprintf("%s: %s", name, reason))
		}
	})
}

func (im *Importer) run(ctx context.Context, opts Options) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("migrate: import crashed", "panic", r)
			im.fail(fmt.Sprintf("the import stopped unexpectedly: %v", r))
			im.finish(StepFailed)
		}
	}()
	inc := opts.Include
	src := opts.Sources
	// Only read what is being imported.
	if !on(inc.Movies) {
		src.Radarr = nil
	}
	if !on(inc.Series) {
		src.Sonarr = nil
	}
	if !on(inc.Indexers) {
		src.Prowlarr = nil
	}
	if !on(inc.UsenetServers) {
		src.SABnzbd, src.NZBGet = nil, nil
	}
	gateExtras(&src, inc)
	snap := im.fetch(ctx, src)
	for _, e := range []struct {
		app string
		err error
	}{{"Radarr", snap.radarrErr}, {"Sonarr", snap.sonarrErr}, {"Prowlarr", snap.prowlErr}, {"SABnzbd", snap.sabErr}, {"NZBGet", snap.nzbgetErr}} {
		if e.err != nil {
			im.fail(e.app + ": " + e.err.Error())
		}
	}
	for _, e := range snap.extra.errs() {
		if e.err != nil {
			im.fail(e.app + ": " + e.err.Error())
		}
	}
	p, err := im.plan(ctx, snap, opts)
	if err != nil {
		slog.Error("migrate: plan import", "err", err)
		im.fail("could not read Mediarium's own library: " + err.Error())
		im.finish(StepFailed)
		return
	}

	total := len(p.movies) + len(p.series) + len(p.indexers) + len(p.servers) + len(p.requests)
	if p.subtitles != nil {
		total++
	}
	im.update(func(s *Status) {
		s.Total = total
		if on(inc.QualityProfiles) {
			s.Results.QualityProfiles = append([]ProfileMapping{}, p.profiles...)
		}
	})

	if len(p.movies)+len(p.series)+len(p.requests) > 0 {
		if im.deps.TMDB == nil || !im.deps.TMDB().HasAPIKey() {
			im.fail("Add a TMDB API key in Settings first. Movies and shows can't be added without one.")
		}
		im.update(func(s *Status) {
			s.Notes = append(s.Notes, "No search was started for the imported titles. The automatic search picks up monitored titles without files, or search for them yourself.")
		})
	}

	im.update(func(s *Status) { s.Step = StepMovies })
	for _, mp := range p.movies {
		if ctx.Err() != nil {
			break
		}
		outcome, reason, linked := im.importMovie(ctx, mp)
		im.update(func(s *Status) { s.Results.Movies.FilesLinked += linked })
		im.record("movie", mp.item.Title, outcome, reason, func(r *Results) *Counts { return &r.Movies })
	}
	im.update(func(s *Status) { s.Step = StepSeries })
	for _, sp := range p.series {
		if ctx.Err() != nil {
			break
		}
		outcome, reason, linked := im.importSeries(ctx, sp)
		im.update(func(s *Status) { s.Results.Series.FilesLinked += linked })
		im.record("series", sp.item.Title, outcome, reason, func(r *Results) *Counts { return &r.Series })
	}
	im.update(func(s *Status) { s.Step = StepIndexers })
	for _, ip := range p.indexers {
		if ctx.Err() != nil {
			break
		}
		outcome, reason, disabled := im.importIndexer(ctx, ip)
		if disabled {
			im.update(func(s *Status) { s.Results.Indexers.Disabled++ })
		}
		im.record("indexer", ip.item.Name, outcome, reason, func(r *Results) *Counts { return &r.Indexers })
	}
	im.update(func(s *Status) { s.Step = StepUsenetServers })
	for _, sp := range p.servers {
		if ctx.Err() != nil {
			break
		}
		outcome, reason, disabled := im.importServer(sp)
		if disabled {
			im.update(func(s *Status) { s.Results.UsenetServers.Disabled++ })
		}
		im.record("usenetServer", sp.item.Name, outcome, reason, func(r *Results) *Counts { return &r.UsenetServers })
	}
	im.update(func(s *Status) { s.Step = StepRequests })
	for _, rq := range p.requests {
		if ctx.Err() != nil {
			break
		}
		outcome, reason := im.importRequest(ctx, rq)
		im.record("request", rq.item.Title, outcome, reason, func(r *Results) *Counts { return &r.Requests })
	}
	if p.subtitles != nil {
		im.update(func(s *Status) { s.Step = StepSubtitles })
		outcome, reason := im.importSubtitleLanguages(p.subtitles)
		im.record("subtitleLanguages", "Subtitle languages", outcome, reason, func(r *Results) *Counts { return &r.SubtitleLanguages })
	}
	res := im.Status().Results
	slog.Info("migrate: import finished",
		"movies_added", res.Movies.Added, "movie_files", res.Movies.FilesLinked,
		"series_added", res.Series.Added, "episodes", res.Series.FilesLinked,
		"indexers_added", res.Indexers.Added, "servers_added", res.UsenetServers.Added, "requests_added", res.Requests.Added)
	im.finish(StepDone)
}

func (im *Importer) activity(movieID, seriesID int64, msg string) {
	if im.deps.Activity != nil {
		im.deps.Activity(movieID, seriesID, msg)
	}
}

// importMovie adds one Radarr movie (if new) and links its file in place.
func (im *Importer) importMovie(ctx context.Context, mp moviePlan) (outcome, reason string, linked int) {
	if mp.item.Action == ActionSkip {
		return OutcomeSkipped, mp.item.Reason, 0
	}
	lib := im.deps.Library
	m, exists, err := lib.GetByTMDBID(mp.movie.TMDBID)
	if err != nil {
		return OutcomeFailed, err.Error(), 0
	}
	outcome = OutcomeExists
	if !exists {
		if im.deps.TMDB == nil || !im.deps.TMDB().HasAPIKey() {
			return OutcomeFailed, "no TMDB API key has been added yet", 0
		}
		tmdb := im.deps.TMDB()
		tm, err := tmdb.GetMovie(ctx, mp.movie.TMDBID)
		if err != nil {
			return OutcomeFailed, "couldn't look up the movie on TMDB: " + err.Error(), 0
		}
		m, err = lib.Add(library.Movie{
			TMDBID: tm.TMDBID, Title: tm.Title, Year: tm.Year(), Overview: tm.Overview, PosterPath: tm.PosterPath,
			Monitored: mp.movie.Monitored, ReleaseDate: tm.ReleaseDate, Genres: tmdb.MovieGenres(ctx, *tm),
		})
		if err != nil {
			return OutcomeFailed, err.Error(), 0
		}
		if mp.item.ProfileID != 0 {
			if err := lib.SetProfile(m.ID, mp.item.ProfileID); err != nil {
				slog.Warn("migrate: set movie profile", "movie", m.Title, "err", err)
			}
		}
		outcome = OutcomeAdded
		im.activity(m.ID, 0, m.Title+" added to library from Radarr")
	}

	switch {
	case m.Status == library.StatusDownloaded:
		return outcome, "already in your library", 0
	case !mp.movie.HasFile:
		if outcome == OutcomeAdded {
			return outcome, "added as wanted (Radarr had no file); no search started", 0
		}
		return outcome, "already in your library", 0
	case !isDir(mp.folder):
		return outcome, "folder not found at " + mp.folder + ", so its file isn't linked", 0
	}

	files, err := libimport.ScanMovieFolder(mp.folder)
	if err != nil {
		return outcome, "couldn't read " + mp.folder + ": " + err.Error(), 0
	}
	if len(files) == 0 {
		return outcome, "no video file found in " + mp.folder, 0
	}
	main := files[0]
	want := arrFileName(mp.movie)
	found := false
	for _, f := range files {
		if want != "" && filepath.Base(f.Path) == want {
			main, found = f, true
			break
		}
	}
	if !found {
		for _, f := range files[1:] {
			if f.SizeBytes > main.SizeBytes {
				main = f
			}
		}
	}
	tier := main.Quality
	if tier == "" || tier == string(quality.TierUnknown) {
		if mp.movie.MovieFile != nil {
			if t := tierFromArr(mp.movie.MovieFile.Quality.Quality.Name); t != quality.TierUnknown {
				tier = string(t)
			}
		}
	}
	if err := lib.SetStatus(m.ID, library.StatusDownloaded, tier, main.Path); err != nil {
		return OutcomeFailed, err.Error(), 0
	}
	im.activity(m.ID, 0, fmt.Sprintf("%s: registered existing file %s", m.Title, main.Path))
	return outcome, fmt.Sprintf("file linked where it is (%s)", tier), 1
}

// importSeries adds one Sonarr show (if new) with its monitoring and links
// the episode files in its folder.
func (im *Importer) importSeries(ctx context.Context, sp seriesPlan) (outcome, reason string, linked int) {
	from := sp.app
	if from == "" {
		from = "Sonarr"
	}
	if sp.item.Action == ActionSkip {
		return OutcomeSkipped, sp.item.Reason, 0
	}
	lib := im.deps.Library
	tmdbID := sp.item.TMDBID
	series, exists, err := lib.GetSeriesByTMDBID(tmdbID)
	if err != nil {
		return OutcomeFailed, err.Error(), 0
	}
	outcome = OutcomeExists
	if !exists {
		if im.deps.TMDB == nil || !im.deps.TMDB().HasAPIKey() {
			return OutcomeFailed, "no TMDB API key has been added yet", 0
		}
		tmdb := im.deps.TMDB()
		detail, infos, err := tmdb.GetShowEpisodes(ctx, tmdbID)
		if err != nil {
			return OutcomeFailed, "couldn't look up the show on TMDB: " + err.Error(), 0
		}
		eps := make([]library.Episode, len(infos))
		for i, e := range infos {
			eps[i] = library.Episode{Season: e.Season, Episode: e.Episode, Title: e.Name, Overview: e.Overview, AirDate: e.AirDate}
		}
		// Sonarr already knows how the show is numbered; otherwise guess.
		seriesType := library.ValidSeriesType(strings.ToLower(sp.series.SeriesType))
		if sp.series.SeriesType == "" {
			var ids []int
			var names []string
			for _, g := range detail.Genres {
				ids, names = append(ids, g.ID), append(names, g.Name)
			}
			seriesType = library.GuessSeriesType(ids, names, detail.OriginCountry, detail.Type)
		}
		series, err = lib.AddSeries(library.Series{
			TMDBID: detail.TMDBID, Title: detail.Name, Year: detail.Year(), Overview: detail.Overview,
			PosterPath: detail.PosterPath, FirstAirDate: detail.FirstAirDate, Monitored: sp.series.Monitored,
			Genres: tmdb.ShowGenres(ctx, detail.Show), SeriesType: seriesType,
		}, eps)
		if err != nil {
			return OutcomeFailed, err.Error(), 0
		}
		if sp.item.ProfileID != 0 {
			if err := lib.SetSeriesProfile(series.ID, sp.item.ProfileID); err != nil {
				slog.Warn("migrate: set series profile", "series", series.Title, "err", err)
			}
		}
		for _, n := range sp.item.UnmonitoredSeasons {
			if err := lib.SetSeasonMonitored(series.ID, n, false); err != nil {
				slog.Warn("migrate: unmonitor season", "series", series.Title, "season", n, "err", err)
			}
		}
		outcome = OutcomeAdded
		im.activity(0, series.ID, series.Title+" (series) added to library from "+from)
	}

	if sp.series.Statistics.EpisodeFileCount == 0 {
		if outcome == OutcomeAdded {
			return outcome, "added (Sonarr had no episode files); no search started", 0
		}
		return outcome, "already in your library", 0
	}
	if !isDir(sp.folder) {
		return outcome, "folder not found at " + sp.folder + ", so its episodes aren't linked", 0
	}
	files, skipped, err := libimport.ScanSeriesFolder(sp.folder)
	if err != nil {
		return outcome, "couldn't read " + sp.folder + ": " + err.Error(), 0
	}
	eps, err := lib.ListEpisodes(series.ID)
	if err != nil {
		return OutcomeFailed, err.Error(), 0
	}
	byNumber := map[[2]int]library.Episode{}
	for _, ep := range eps {
		byNumber[[2]int{ep.Season, ep.Episode}] = ep
	}
	already, unknown := 0, 0
	for _, f := range files {
		for _, n := range f.Episodes {
			ep, ok := byNumber[[2]int{f.Season, n}]
			if !ok {
				unknown++
				continue
			}
			if ep.Status == library.StatusDownloaded {
				already++
				continue
			}
			if err := lib.SetEpisodeStatus(ep.ID, library.StatusDownloaded, f.Quality, f.Path); err != nil {
				return OutcomeFailed, err.Error(), linked
			}
			linked++
		}
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%d episode(s) linked where they are", linked))
	if already > 0 {
		parts = append(parts, fmt.Sprintf("%d already linked", already))
	}
	if unknown > 0 {
		parts = append(parts, fmt.Sprintf("%d aren't listed on TMDB for this show (specials or different numbering) and were left out", unknown))
	}
	if len(skipped) > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s) without a readable episode number were left out", len(skipped)))
	}
	if found := len(files); found < sp.series.Statistics.EpisodeFileCount {
		parts = append(parts, fmt.Sprintf("Sonarr lists %d episode file(s), %d were found in the folder", sp.series.Statistics.EpisodeFileCount, found))
	}
	if linked > 0 {
		im.activity(0, series.ID, fmt.Sprintf("%s: registered %d existing episode(s)", series.Title, linked))
	}
	return outcome, strings.Join(parts, "; "), linked
}

const indexerTestTimeout = 45 * time.Second

func (im *Importer) testIndexer(ctx context.Context, inst indexers.Instance) error {
	ctx, cancel := context.WithTimeout(ctx, indexerTestTimeout)
	defer cancel()
	if im.deps.TestIndexer != nil {
		return im.deps.TestIndexer(ctx, inst)
	}
	if inst.IsCardigann() {
		return nil
	}
	_, err := indexers.NewNewznabClient(inst.Name, inst.BaseURL, inst.APIKey).Search(ctx, "", []int{2000, 5000})
	return err
}

// importIndexer adds one Prowlarr indexer, switched off if its connection
// test fails (or a secret couldn't be copied).
func (im *Importer) importIndexer(ctx context.Context, ip indexerPlan) (outcome, reason string, disabled bool) {
	switch ip.item.Action {
	case ActionSkip:
		return OutcomeSkipped, ip.item.Reason, false
	case ActionExists:
		return OutcomeExists, ip.item.Reason, false
	}
	inst := ip.inst
	reason = ip.item.Reason
	testMsg, tested := "", false
	if inst.Enabled {
		tested = true
		if err := im.testIndexer(ctx, inst); err != nil {
			testMsg = err.Error()
			inst.Enabled = false
			reason = joinReason(reason, "added switched off: the connection test failed ("+testMsg+")")
		}
	}
	created, err := im.deps.Indexers.Create(inst, ip.secret)
	if err != nil {
		return OutcomeFailed, err.Error(), false
	}
	if tested {
		if err := im.deps.Indexers.SetTestResult(created.ID, testMsg, time.Now()); err != nil {
			slog.Warn("migrate: record indexer test", "indexer", inst.Name, "err", err)
		}
	}
	if reason == "" {
		reason = "added and tested"
	}
	return OutcomeAdded, reason, !inst.Enabled
}

// importServer adds one SABnzbd or NZBGet server, switched off when the app had it
// off or hid its password.
func (im *Importer) importServer(sp serverPlan) (outcome, reason string, disabled bool) {
	switch sp.item.Action {
	case ActionSkip:
		return OutcomeSkipped, sp.item.Reason, false
	case ActionExists:
		return OutcomeExists, sp.item.Reason, false
	}
	created, err := im.deps.Servers.Create(sp.sc)
	if err != nil {
		return OutcomeFailed, err.Error(), false
	}
	if !sp.sc.Enabled {
		created.Enabled = false
		created.Config.Password = "" // keeps the stored one
		if err := im.deps.Servers.Update(created); err != nil {
			return OutcomeFailed, err.Error(), false
		}
	}
	reason = sp.item.Reason
	if reason == "" {
		reason = "added"
	}
	return OutcomeAdded, reason, !sp.sc.Enabled
}

func joinReason(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
