package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
)

// Collections: a series of films on TMDB (a trilogy, a franchise). A movie
// knows which one it belongs to; from the movie's page the whole collection
// can be added, and a collection can be followed so that films added to it
// later are added too.

const (
	// collectionsInterval is how often the collections job runs.
	collectionsInterval = 20 * time.Minute
	// collectionBackfillBatch caps the TMDB lookups one run makes to find the
	// collection of movies added before collections were stored, and
	// collectionBackfillPause spaces them out.
	collectionBackfillBatch = 150
	collectionBackfillPause = 150 * time.Millisecond
	// collectionRecheck is how long a followed collection is left alone
	// before it is looked at for new films again.
	collectionRecheck = 6 * time.Hour
)

type collectionPart struct {
	TMDBID      int    `json:"tmdbId"`
	Title       string `json:"title"`
	Year        int    `json:"year,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	PosterURL   string `json:"posterUrl,omitempty"`
	Released    bool   `json:"released"`
	LibraryID   int64  `json:"libraryId,omitempty"` // 0 when the film is not in the library
	Status      string `json:"status,omitempty"`    // library state: missing/downloading/downloaded
}

type collectionPayload struct {
	ID          int              `json:"id"`
	Name        string           `json:"name"`
	Overview    string           `json:"overview,omitempty"`
	PosterURL   string           `json:"posterUrl,omitempty"`
	BackdropURL string           `json:"backdropUrl,omitempty"`
	Parts       []collectionPart `json:"parts"`
	InLibrary   int              `json:"inLibrary"`
	// What "also add new films" is set to now.
	Follow          bool   `json:"follow"`
	FollowProfileID int64  `json:"followProfileId,omitempty"`
	FollowRootPath  string `json:"followRootPath,omitempty"`
	FollowMonitored bool   `json:"followMonitored"`
}

// collectionRef is the short form a movie page carries.
type collectionRef struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	PosterURL string `json:"posterUrl,omitempty"`
}

func collectionRefOf(c *metadata.CollectionRef) *collectionRef {
	if c == nil || c.ID <= 0 {
		return nil
	}
	return &collectionRef{ID: c.ID, Name: c.Name, PosterURL: metadata.PosterURL(c.PosterPath)}
}

// noteMovieCollection records the collection a just-added movie belongs to.
func (s *Server) noteMovieCollection(movieID int64, tm *metadata.Movie) {
	id := 0
	if c := tm.BelongsToCollection; c != nil && c.ID > 0 {
		id = c.ID
		if err := s.MovieRepo.SaveCollection(library.Collection{TMDBID: c.ID, Name: c.Name, PosterPath: c.PosterPath, BackdropPath: c.BackdropPath}); err != nil {
			slog.Warn("collections: save", "collection", c.ID, "err", err)
			return
		}
	}
	if err := s.MovieRepo.SetMovieCollection(movieID, id); err != nil {
		slog.Warn("collections: note the collection of a movie", "movie", movieID, "err", err)
	}
}

func parseCollectionID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't a valid collection ID.")
		return 0, false
	}
	return id, true
}

type collectionListItem struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	PosterURL string  `json:"posterUrl,omitempty"`
	MovieIDs  []int64 `json:"movieIds"`
	Follow    bool    `json:"follow"`
}

// handleListCollections is GET /api/collections: the collections that have a
// film in the library, with the library ids of those films. The Library page
// filters by them.
func (s *Server) handleListCollections(w http.ResponseWriter, r *http.Request) {
	list, err := s.MovieRepo.ListCollections()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]collectionListItem, 0, len(list))
	for _, c := range list {
		out = append(out, collectionListItem{ID: c.TMDBID, Name: c.Name, PosterURL: metadata.PosterURL(c.PosterPath), MovieIDs: c.MovieIDs, Follow: c.Follow})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetCollection is GET /api/collections/{id}: the collection from TMDB
// with which of its films are in the library, and the follow choice.
func (s *Server) handleGetCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := parseCollectionID(w, r)
	if !ok {
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie and show info).")
		return
	}
	col, err := s.displayTMDB().GetCollection(r.Context(), id)
	if err != nil {
		writeUpstreamError(w, "look up that collection", err)
		return
	}
	payload, err := s.collectionPayloadFor(col)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	payload.FollowRootPath = folderShown(r, s.movieFolders(), payload.FollowRootPath)
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) collectionPayloadFor(col *metadata.Collection) (collectionPayload, error) {
	movies, err := s.MovieRepo.List()
	if err != nil {
		return collectionPayload{}, err
	}
	byTMDB := make(map[int]library.Movie, len(movies))
	for _, m := range movies {
		byTMDB[m.TMDBID] = m
	}
	today := time.Now().UTC().Format("2006-01-02")
	p := collectionPayload{
		ID: col.ID, Name: col.Name, Overview: col.Overview,
		PosterURL: metadata.PosterURL(col.PosterPath), BackdropURL: metadata.BackdropURL(col.BackdropPath),
		Parts: make([]collectionPart, 0, len(col.Parts)), FollowMonitored: true,
	}
	for _, part := range col.Parts {
		cp := collectionPart{
			TMDBID: part.TMDBID, Title: part.DisplayTitle(), Year: part.Year(), ReleaseDate: isoDate(part.ReleaseDate),
			PosterURL: metadata.PosterURL(part.PosterPath), Released: part.ReleaseDate != "" && part.ReleaseDate <= today,
		}
		if m, ok := byTMDB[part.TMDBID]; ok {
			cp.LibraryID, cp.Status = m.ID, string(m.Status)
			p.InLibrary++
		}
		p.Parts = append(p.Parts, cp)
	}
	stored, ok, err := s.MovieRepo.GetCollection(col.ID)
	if err != nil {
		return collectionPayload{}, err
	}
	if ok {
		p.Follow, p.FollowProfileID, p.FollowRootPath, p.FollowMonitored = stored.Follow, stored.FollowProfileID, stored.FollowRootPath, stored.FollowMonitored
	}
	return p, nil
}

// collectionAddOptions are the choices films are added with.
type collectionAddOptions struct {
	ProfileID int64
	RootPath  string
	Monitored bool
	Sources   string
	AddedBy   int64
}

// addCollectionMovie adds one film of a collection to the library, as
// handleAddMovie does for a single film.
func (s *Server) addCollectionMovie(ctx context.Context, tmdbID, collectionID int, o collectionAddOptions) (library.Movie, error) {
	tm, err := s.TMDB().GetMovie(ctx, tmdbID)
	if err != nil {
		return library.Movie{}, err
	}
	rootPath, problem := s.folderForNewMovie(o.RootPath)
	if problem != "" {
		rootPath = s.newMovieFolder() // the folder was taken out of the settings
	}
	rootPath = s.animeMovieFolderFor(*tm, rootPath)
	created, err := s.MovieRepo.Add(library.Movie{
		TMDBID: tm.TMDBID, Title: tm.Title, Year: tm.Year(),
		Overview: tm.Overview, PosterPath: tm.PosterPath, Monitored: o.Monitored, ReleaseDate: tm.ReleaseDate,
		NoUpgrade: true, AddedBy: o.AddedBy, Genres: s.TMDB().MovieGenres(ctx, *tm),
		RootPath: rootPath,
	})
	if err != nil {
		return library.Movie{}, err
	}
	if err := s.MovieRepo.SetMovieCollection(created.ID, collectionID); err != nil {
		slog.Warn("collections: note the collection of a movie", "movie", created.ID, "err", err)
	}
	if o.ProfileID != 0 {
		if err := s.MovieRepo.SetProfile(created.ID, o.ProfileID); err == nil {
			created.ProfileID = o.ProfileID
		}
	}
	if o.Sources != "" {
		if err := s.MovieRepo.SetSourcePref(created.ID, o.Sources); err == nil {
			created.SourcePref = o.Sources
		}
	}
	return created, nil
}

type addCollectionRequest struct {
	ProfileID int64  `json:"profileId"`
	RootPath  string `json:"rootPath"` // the movie folder to keep the films in; left out = where new movies go
	Monitored *bool  `json:"monitored"`
	Sources   string `json:"sources"`
	SearchNow bool   `json:"searchNow"`
	// TMDBIDs limits the films to add; left out = every film that is not in the library.
	TMDBIDs []int `json:"tmdbIds"`
	// Follow also adds the films that join the collection later.
	Follow *bool `json:"follow"`
}

type addCollectionResult struct {
	Added      int      `json:"added"`
	AlreadyHad int      `json:"alreadyHad"`
	Failed     []string `json:"failed"`
	Following  bool     `json:"following"`
	Message    string   `json:"message"`
}

// handleAddCollection is POST /api/collections/{id}/add: adds the films of a
// collection that are not in the library yet, with the profile, folder and
// monitoring chosen, and can start following the collection.
func (s *Server) handleAddCollection(w http.ResponseWriter, r *http.Request) {
	if !s.requireModule(w, moduleMovies) {
		return
	}
	id, ok := parseCollectionID(w, r)
	if !ok {
		return
	}
	var req addCollectionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie and show info).")
		return
	}
	if !validSourcePref(req.Sources) {
		writeError(w, http.StatusBadRequest, `Choose where to download from: "usenet", "torrent" or "both".`)
		return
	}
	if err := s.checkProfileChoice(req.ProfileID); err != nil {
		writeProfileError(w, err)
		return
	}
	rootPath, problem := s.checkFolderSetting(req.RootPath, s.movieFolders(), "movie")
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	col, err := s.displayTMDB().GetCollection(r.Context(), id)
	if err != nil {
		writeUpstreamError(w, "look up that collection", err)
		return
	}
	if err := s.MovieRepo.SaveCollection(library.Collection{TMDBID: col.ID, Name: col.Name, Overview: col.Overview, PosterPath: col.PosterPath, BackdropPath: col.BackdropPath}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	want := map[int]bool{}
	for _, t := range req.TMDBIDs {
		want[t] = true
	}
	have, err := s.MovieRepo.MovieTMDBIDs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	userID, byline := requester(r)
	opts := collectionAddOptions{ProfileID: req.ProfileID, RootPath: rootPath, Monitored: boolOr(req.Monitored, true), Sources: req.Sources, AddedBy: userID}
	res := addCollectionResult{Failed: []string{}}
	var added []library.Movie
	for _, part := range col.Parts {
		if len(want) > 0 && !want[part.TMDBID] {
			continue
		}
		if movieID, in := have[part.TMDBID]; in {
			res.AlreadyHad++
			// A film already in the library still joins the collection.
			_ = s.MovieRepo.SetMovieCollection(movieID, col.ID)
			continue
		}
		m, err := s.addCollectionMovie(r.Context(), part.TMDBID, col.ID, opts)
		if err != nil {
			res.Failed = append(res.Failed, part.DisplayTitle())
			log.Printf("collections: add %q to the library: %v", part.DisplayTitle(), err)
			continue
		}
		have[part.TMDBID] = m.ID
		added = append(added, m)
		res.Added++
		_ = s.QueueRepo.LogActivity(m.ID, "added", m.Title+" added to library"+byline+", with the "+col.Name)
	}

	if req.Follow != nil {
		follow := *req.Follow
		if err := s.MovieRepo.SetCollectionFollow(col.ID, follow, req.ProfileID, rootPath, opts.Monitored); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if follow {
			known := make([]int, 0, len(col.Parts))
			for _, p := range col.Parts {
				known = append(known, p.TMDBID)
			}
			_ = s.MovieRepo.SetCollectionKnown(col.ID, known)
			_ = s.MovieRepo.MarkCollectionChecked(col.ID, time.Now())
		}
		res.Following = follow
	}

	if req.SearchNow && len(added) > 0 {
		// One after the other, so a whole collection does not ask every
		// indexer at once. Films that have not come out are left until they do.
		today := time.Now().UTC().Format("2006-01-02")
		var ids []int64
		for _, m := range added {
			if m.ReleaseDate != "" && m.ReleaseDate <= today {
				ids = append(ids, m.ID)
			}
		}
		if len(ids) > 0 {
			go func() {
				for _, movieID := range ids {
					s.searchMovieInBackground(movieID)
				}
			}()
		}
	}
	res.Message = collectionAddMessage(res, col.Name)
	writeJSON(w, http.StatusOK, res)
}

func collectionAddMessage(r addCollectionResult, name string) string {
	var msg string
	switch {
	case r.Added == 0 && len(r.Failed) == 0:
		msg = fmt.Sprintf("Everything in the %s is in your library already.", name)
	case r.Added == 0:
		msg = "Nothing was added."
	default:
		msg = fmt.Sprintf("Added %s from the %s.", plural(r.Added, "film"), name)
	}
	if len(r.Failed) > 0 {
		msg += " Couldn't add " + strings.Join(r.Failed, ", ") + "."
	}
	if r.Following {
		msg += " New films in this collection will be added too."
	}
	return msg
}

type followCollectionRequest struct {
	Follow    bool   `json:"follow"`
	ProfileID int64  `json:"profileId"`
	RootPath  string `json:"rootPath"`
	Monitored *bool  `json:"monitored"`
}

// handleFollowCollection is PUT /api/collections/{id}/follow: turns "also add
// new films in this collection" on or off. Films the collection has now are
// not added by this; only ones that join it later are.
func (s *Server) handleFollowCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := parseCollectionID(w, r)
	if !ok {
		return
	}
	var req followCollectionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if err := s.checkProfileChoice(req.ProfileID); err != nil {
		writeProfileError(w, err)
		return
	}
	rootPath, problem := s.checkFolderSetting(req.RootPath, s.movieFolders(), "movie")
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie and show info).")
		return
	}
	col, err := s.displayTMDB().GetCollection(r.Context(), id)
	if err != nil {
		writeUpstreamError(w, "look up that collection", err)
		return
	}
	if err := s.MovieRepo.SaveCollection(library.Collection{TMDBID: col.ID, Name: col.Name, Overview: col.Overview, PosterPath: col.PosterPath, BackdropPath: col.BackdropPath}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.MovieRepo.SetCollectionFollow(col.ID, req.Follow, req.ProfileID, rootPath, boolOr(req.Monitored, true)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Follow {
		known := make([]int, 0, len(col.Parts))
		for _, p := range col.Parts {
			known = append(known, p.TMDBID)
		}
		if err := s.MovieRepo.SetCollectionKnown(col.ID, known); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.MovieRepo.MarkCollectionChecked(col.ID, time.Now())
	}
	payload, err := s.collectionPayloadFor(col)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	payload.FollowRootPath = folderShown(r, s.movieFolders(), payload.FollowRootPath)
	writeJSON(w, http.StatusOK, payload)
}

// collectionsJob finds the collection of movies that were added before
// collections were stored, and adds the new films of followed collections.
func (s *Server) collectionsJob(ctx context.Context) {
	if !s.TMDB().HasAPIKey() {
		return
	}
	s.backfillCollections(ctx)
	if s.automationEnabled() {
		s.addFollowedCollectionFilms(ctx)
	}
}

// backfillCollections asks TMDB which collection library movies belong to.
// A failure for a passing reason ends the run (the next run tries again); a
// film TMDB no longer knows is recorded as on its own so it is not asked again.
func (s *Server) backfillCollections(ctx context.Context) {
	gaps, err := s.MovieRepo.MoviesWithoutCollectionCheck(collectionBackfillBatch)
	if err != nil {
		log.Printf("automation: collections: %v", err)
		return
	}
	for i, g := range gaps {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(collectionBackfillPause):
			}
		}
		tm, err := s.TMDB().GetMovie(ctx, g.TMDBID)
		if err != nil {
			var status *metadata.StatusError
			if errors.As(err, &status) && status.Code == http.StatusNotFound {
				_ = s.MovieRepo.SetMovieCollection(g.ID, 0)
				continue
			}
			log.Printf("automation: collections: %q: %v", g.Title, err)
			return
		}
		s.noteMovieCollection(g.ID, tm)
	}
}

// addFollowedCollectionFilms adds the films that joined a followed collection
// since it was last looked at, unless the person put them on the Not interested
// list.
func (s *Server) addFollowedCollectionFilms(ctx context.Context) {
	followed, err := s.MovieRepo.FollowedCollections()
	if err != nil {
		log.Printf("automation: collections: %v", err)
		return
	}
	now := time.Now()
	for _, c := range followed {
		if ctx.Err() != nil {
			return
		}
		if t, err := time.Parse(time.RFC3339, c.CheckedAt); err == nil && now.Sub(t) < collectionRecheck {
			continue
		}
		col, err := s.TMDB().GetCollection(ctx, c.TMDBID)
		if err != nil {
			log.Printf("automation: collections: %q: %v", c.Name, err)
			continue
		}
		known := map[int]bool{}
		for _, id := range c.KnownParts {
			known[id] = true
		}
		have, err := s.MovieRepo.MovieTMDBIDs()
		if err != nil {
			log.Printf("automation: collections: %v", err)
			return
		}
		opts := collectionAddOptions{ProfileID: c.FollowProfileID, RootPath: c.FollowRootPath, Monitored: c.FollowMonitored}
		if opts.RootPath != "" {
			if _, problem := s.checkFolderSetting(opts.RootPath, s.movieFolders(), "movie"); problem != "" {
				opts.RootPath = "" // the folder was taken out of the settings: wherever new movies go
			}
		}
		allKnown := make([]int, 0, len(col.Parts))
		for _, part := range col.Parts {
			allKnown = append(allKnown, part.TMDBID)
			if known[part.TMDBID] {
				continue
			}
			if _, in := have[part.TMDBID]; in || s.excluded("movie", part.TMDBID) {
				continue
			}
			m, err := s.addCollectionMovie(ctx, part.TMDBID, col.ID, opts)
			if err != nil {
				log.Printf("automation: collections: add %q: %v", part.DisplayTitle(), err)
				// Not known yet, so the next run tries again.
				allKnown = allKnown[:len(allKnown)-1]
				continue
			}
			_ = s.QueueRepo.LogActivity(m.ID, "added", m.Title+" added to library, because you follow the "+col.Name)
		}
		_ = s.MovieRepo.SetCollectionKnown(c.TMDBID, allKnown)
		_ = s.MovieRepo.MarkCollectionChecked(c.TMDBID, now)
	}
}

// excluded reports whether a title is on the Not interested list.
func (s *Server) excluded(kind string, tmdbID int) bool {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM exclusions WHERE kind = ? AND tmdb_id = ?`, kind, tmdbID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}
