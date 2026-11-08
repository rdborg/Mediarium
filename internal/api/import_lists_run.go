package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/importlists"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/simkl"
	"github.com/rdborg/mediarium/internal/trakt"
)

// Reading an import list and adding what is on it.

const (
	// listLookupGap spaces out the TMDB lookups of one run, and listAddGap the
	// adds (each can start a search at the indexers).
	listLookupGap = 120 * time.Millisecond
	listAddGap    = time.Second
	// listFailureStop ends a run after this many lookups in a row failed: the
	// service is down or refusing, and more tries would only add load.
	listFailureStop = 5
)

var listAgentOnce sync.Once

// setListAgents makes the clients that read lists introduce themselves with
// the version of this Mediarium.
func (s *Server) setListAgents() {
	listAgentOnce.Do(func() {
		agent := "Mediarium/" + s.version + " (+https://mediarium.app)"
		trakt.UserAgent = agent
		simkl.UserAgent = agent
		simkl.AppVersion = s.version
		importlists.UserAgent = agent
	})
}

func wantedKinds(media string) map[string]bool {
	switch media {
	case "movie":
		return map[string]bool{"movie": true}
	case "tv":
		return map[string]bool{"tv": true}
	}
	return map[string]bool{"movie": true, "tv": true}
}

func orderedKinds(media string) []string {
	switch media {
	case "movie":
		return []string{"movie"}
	case "tv":
		return []string{"tv"}
	}
	return []string{"movie", "tv"}
}

// listRef names a list entry the way the list does, so the same entry is
// recognised next time without looking anything up.
func listRef(it importlists.Item) string {
	kind := it.Kind
	switch {
	case it.TMDBID > 0:
		if kind == "" {
			kind = "movie"
		}
		return fmt.Sprintf("tmdb:%s:%d", kind, it.TMDBID)
	case it.IMDBID != "":
		return "imdb:" + it.IMDBID + ":" + kind
	case it.TVDBID > 0:
		return fmt.Sprintf("tvdb:%d", it.TVDBID)
	}
	return "title:" + importlists.NormalizeTitle(it.Title) + "|" + strconv.Itoa(it.Year) + "|" + kind
}

func dedupeListItems(items []importlists.Item) []importlists.Item {
	seen := map[string]bool{}
	out := items[:0:0]
	for _, it := range items {
		ref := listRef(it)
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, it)
	}
	return out
}

// listErrorText is the sentence shown for a list that couldn't be read.
func listErrorText(err error) string {
	var rl *importlists.RateLimitError
	var tl *trakt.RateLimitError
	var sl *simkl.RateLimitError
	var sk *simkl.StatusError
	var se *metadata.StatusError
	switch {
	case errors.Is(err, metadata.ErrNoAPIKey):
		return "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie and show info)."
	case errors.Is(err, metadata.ErrInvalidKey):
		return "TMDB rejected the API key. Check it in Settings."
	case errors.Is(err, trakt.ErrNoClientID):
		return "Add your Trakt client ID first (Settings > Info, lists and subtitles > Import lists)."
	case errors.Is(err, trakt.ErrInvalidClientID):
		return "Trakt rejected the client ID. Check it in Settings."
	case errors.Is(err, simkl.ErrNoClientID):
		return "Add your Simkl client ID first (Settings > Info, lists and subtitles > Import lists)."
	case errors.Is(err, simkl.ErrInvalidClientID):
		return "Simkl rejected the client ID. Check it in Settings."
	case errors.Is(err, simkl.ErrNotConnected):
		return "Connect your Simkl account first (Settings > Info, lists and subtitles > Import lists)."
	case errors.Is(err, simkl.ErrUnauthorized):
		return "Simkl no longer accepts the saved sign-in. Connect your Simkl account again."
	case errors.Is(err, errSimklTokenUnreadable):
		return "The saved Simkl sign-in can't be read. Connect your Simkl account again."
	case errors.As(err, &se) && se.Code == http.StatusNotFound && strings.HasPrefix(se.Path, "/list/"):
		return "TMDB has no public list with that number."
	case errors.As(err, &rl), errors.As(err, &tl), errors.As(err, &sl), errors.As(err, &sk),
		errors.Is(err, trakt.ErrNotFound), errors.Is(err, trakt.ErrPrivate),
		errors.Is(err, importlists.ErrBlocked), errors.Is(err, importlists.ErrMDBListKey), errors.Is(err, importlists.ErrNotAList):
		return sentence(err.Error())
	}
	if msg := plainerror.Message(err); msg != "" {
		return sentence(msg)
	}
	return "The list couldn't be read."
}

// fetchListItems reads the titles on a list from its source.
func (s *Server) fetchListItems(ctx context.Context, l importList) ([]importlists.Item, error) {
	s.setListAgents()
	kinds := orderedKinds(l.Media)
	wanted := wantedKinds(l.Media)
	cfg := l.Config
	switch l.Source {
	case listTraktList, listTraktWatchlist, listTraktChart:
		tc := s.Trakt()
		if !tc.HasClientID() {
			return nil, trakt.ErrNoClientID
		}
		var media []trakt.Media
		var err error
		switch l.Source {
		case listTraktList:
			user, id, perr := trakt.ParseAnyListAddress(cfg.URL)
			if perr != nil {
				return nil, perr
			}
			media, err = tc.UserListMedia(ctx, user, id)
		case listTraktWatchlist:
			user, perr := trakt.ParseWatchlistAddress(cfg.URL)
			if perr != nil {
				return nil, perr
			}
			media, err = tc.WatchlistMedia(ctx, user)
		default:
			for i, k := range kinds {
				if i > 0 {
					time.Sleep(s.lists.gap(time.Second))
				}
				part, cerr := tc.ChartMedia(ctx, k, cfg.Chart, cfg.Count)
				if cerr != nil {
					return nil, cerr
				}
				media = append(media, part...)
			}
		}
		if err != nil {
			return nil, err
		}
		out := make([]importlists.Item, 0, len(media))
		for _, m := range media {
			if wanted[m.Kind] {
				out = append(out, importlists.Item{Kind: m.Kind, TMDBID: m.TMDBID, IMDBID: m.IMDBID, Title: m.Title, Year: m.Year})
			}
		}
		return out, nil

	case listTMDBList:
		n, err := parseTMDBListNumber(cfg.URL)
		if err != nil {
			return nil, err
		}
		entries, err := s.TMDB().ListEntries(ctx, n)
		if err != nil {
			return nil, err
		}
		out := make([]importlists.Item, 0, len(entries))
		for _, e := range entries {
			if wanted[e.Kind] {
				out = append(out, importlists.Item{Kind: e.Kind, TMDBID: e.TMDBID, Title: e.Title, Year: e.Year, Rating: e.Rating})
			}
		}
		return out, nil

	case listTMDBChart:
		var out []importlists.Item
		for _, k := range kinds {
			part, err := s.tmdbChartItems(ctx, k, cfg)
			if err != nil {
				return nil, err
			}
			out = append(out, part...)
		}
		return out, nil

	case listIMDB, listLetterboxd:
		text := cfg.Csv
		if text == "" {
			addr, err := importlists.CSVAddress(cfg.URL)
			if err != nil {
				return nil, err
			}
			if text, err = importlists.DownloadCSV(ctx, addr); err != nil {
				return nil, err
			}
		}
		items, err := importlists.ParseCSV(text)
		if err != nil {
			return nil, err
		}
		out := make([]importlists.Item, 0, len(items))
		for _, it := range items {
			if l.Source == listLetterboxd && it.Kind == "" {
				it.Kind = "movie"
			}
			if it.Kind == "" || wanted[it.Kind] {
				out = append(out, it)
			}
		}
		return out, nil

	case listSimkl:
		sc := s.Simkl()
		if !sc.HasClientID() {
			return nil, simkl.ErrNoClientID
		}
		token, terr := s.simklAccessToken(ctx)
		if terr != nil {
			return nil, terr
		}
		status := cfg.Status
		if status == "" {
			status = simkl.StatusPlanToWatch
		}
		media, err := sc.Items(ctx, token, status, s.lists.gap(time.Second))
		if err != nil {
			return nil, err
		}
		out := make([]importlists.Item, 0, len(media))
		for _, m := range media {
			if wanted[m.Kind] {
				out = append(out, importlists.Item{Kind: m.Kind, TMDBID: m.TMDBID, IMDBID: m.IMDBID, TVDBID: m.TVDBID, Title: m.Title, Year: m.Year})
			}
		}
		return out, nil

	case listRSS:
		entries, err := s.readFeedEntries(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return feedListItems(entries, l.Media), nil

	case listMDBList:
		user, name, err := importlists.ParseMDBListAddress(cfg.URL)
		if err != nil {
			return nil, err
		}
		key, err := s.box.Decrypt(l.secret)
		if err != nil || key == "" {
			return nil, errors.New("Add your MDBList API key to read this list")
		}
		client := importlists.NewMDBListClient()
		if s.lists.mdblistBase != "" {
			client = importlists.NewMDBListClientAt(s.lists.mdblistBase)
		}
		items, err := client.Items(ctx, key, user, name)
		if err != nil {
			return nil, err
		}
		out := items[:0:0]
		for _, it := range items {
			if wanted[it.Kind] {
				out = append(out, it)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown list source %q", l.Source)
}

// tmdbChartItems reads the first cfg.Count titles of a TMDB chart for one kind.
func (s *Server) tmdbChartItems(ctx context.Context, kind string, cfg listConfig) ([]importlists.Item, error) {
	filtered := cfg.Genre > 0 || cfg.Language != ""
	sort := metadata.SortPopular
	if cfg.Chart == "top-rated" {
		sort = metadata.SortRating
	}
	count := cfg.Count
	if count < 1 {
		count = 40
	}
	pages := (count + 19) / 20
	var out []importlists.Item
	tm := s.TMDB()
	for page := 1; page <= pages; page++ {
		if page > 1 {
			time.Sleep(s.lists.gap(listLookupGap))
		}
		totalPages := 1
		if kind == "movie" {
			var pg *metadata.MoviePage
			var err error
			switch {
			case filtered:
				pg, err = tm.BrowseMovies(ctx, metadata.BrowseFilter{Sort: sort, Genre: cfg.Genre, Language: cfg.Language, Page: page})
			case cfg.Chart == "top-rated":
				pg, err = tm.TopRatedMovies(ctx, page)
			default:
				pg, err = tm.MovieList(ctx, cfg.Chart, page)
			}
			if err != nil {
				return nil, err
			}
			totalPages = pg.TotalPages
			for _, m := range pg.Results {
				out = append(out, importlists.Item{Kind: "movie", TMDBID: m.TMDBID, Title: m.Title, Year: m.Year(), Rating: m.VoteAverage})
			}
		} else {
			var pg *metadata.ShowPage
			var err error
			switch {
			case filtered:
				pg, err = tm.BrowseTV(ctx, metadata.BrowseFilter{Sort: sort, Genre: cfg.Genre, Language: cfg.Language, Page: page})
			case cfg.Chart == "top-rated":
				pg, err = tm.TopRatedTV(ctx, page)
			default:
				pg, err = tm.TVList(ctx, cfg.Chart, page)
			}
			if err != nil {
				return nil, err
			}
			totalPages = pg.TotalPages
			for _, sh := range pg.Results {
				out = append(out, importlists.Item{Kind: "tv", TMDBID: sh.TMDBID, Title: sh.Name, Year: sh.Year(), Rating: sh.VoteAverage})
			}
		}
		if page >= totalPages {
			break
		}
	}
	if len(out) > count {
		out = out[:count]
	}
	return out, nil
}

// gap is a pause between requests; tests make it 0.
func (st *importListState) gap(d time.Duration) time.Duration {
	if st.noPause {
		return 0
	}
	return d
}

type seenItem struct {
	kind    string
	tmdbID  int
	year    int
	rating  float64
	outcome string
}

// A title with one of these outcomes is not looked at again.
func terminalOutcome(o string) bool {
	switch o {
	case "added", "in-library", "existing", "no-match":
		return true
	}
	return false
}

func (s *Server) loadSeen(listID int64) (map[string]seenItem, error) {
	rows, err := s.db.Query(`SELECT ref, kind, tmdb_id, year, rating, outcome FROM import_list_items WHERE list_id = ?`, listID)
	if err != nil {
		return nil, fmt.Errorf("read what the list has handled: %w", err)
	}
	defer rows.Close()
	out := map[string]seenItem{}
	for rows.Next() {
		var ref string
		var it seenItem
		if err := rows.Scan(&ref, &it.kind, &it.tmdbID, &it.year, &it.rating, &it.outcome); err != nil {
			return nil, err
		}
		out[ref] = it
	}
	return out, rows.Err()
}

func (s *Server) recordSeen(listID int64, ref, kind string, tmdbID int, title string, year int, rating float64, outcome string) {
	_, err := s.db.Exec(`INSERT INTO import_list_items (list_id, ref, kind, tmdb_id, title, year, rating, outcome) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(list_id, ref) DO UPDATE SET kind = excluded.kind, tmdb_id = excluded.tmdb_id, title = excluded.title, year = excluded.year,
		rating = excluded.rating, outcome = excluded.outcome, seen_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		listID, ref, kind, tmdbID, title, year, rating, outcome)
	if err != nil {
		slog.Warn("import list: couldn't remember a title", "list", listID, "err", err)
	}
}

// listRunResult counts what one read of a list did.
type listRunResult struct {
	found, added, inLibrary, excluded, filtered, noMatch, earlier int
	wrongKind, waiting, failed, off                               int
	addedTitles                                                   []string
	firstFailure                                                  string
	// searchMovies and searchSeries are the library ids of what this read added
	// to a list that is set to search for what it adds.
	searchMovies, searchSeries []int64
	// feed is what an RSS feed list read, for the downloads it may start;
	// grabbed is how many of its releases went to the download line.
	feed    []importlists.FeedEntry
	grabbed int
}

func (r listRunResult) skipped() int {
	return r.inLibrary + r.excluded + r.filtered + r.noMatch + r.earlier
}

// message is the sentence kept as the list's last result.
func (r listRunResult) message() string {
	if r.found == 0 {
		return "The list is empty."
	}
	parts := []string{fmt.Sprintf("Found %s.", plural(r.found, "title"))}
	if r.added > 0 {
		parts = append(parts, fmt.Sprintf("Added %d.", r.added))
	} else {
		parts = append(parts, "Nothing new to add.")
	}
	var why []string
	add := func(n int, text string) {
		if n > 0 {
			why = append(why, fmt.Sprintf("%d %s", n, text))
		}
	}
	add(r.earlier, "handled before")
	add(r.inLibrary, "already in the library")
	add(r.excluded, "on your Not interested list")
	add(r.filtered, "below the minimum rating or year")
	add(r.noMatch, "not found on TMDB")
	if len(why) > 0 {
		parts = append(parts, "Skipped: "+strings.Join(why, ", ")+".")
	}
	if r.waiting > 0 {
		parts = append(parts, fmt.Sprintf("%d more wait for the next read.", r.waiting))
	}
	if r.off > 0 {
		parts = append(parts, fmt.Sprintf("%d were left out because that kind of media is switched off.", r.off))
	}
	if r.grabbed > 0 {
		parts = append(parts, fmt.Sprintf("Downloading %s from the feed.", plural(r.grabbed, "release")))
	}
	if r.failed > 0 {
		msg := fmt.Sprintf("%d couldn't be added and will be tried again.", r.failed)
		if r.firstFailure != "" {
			msg += " " + sentence(r.firstFailure)
		}
		parts = append(parts, msg)
	}
	return strings.Join(parts, " ")
}

// runImportList reads one list and adds what is new on it. One list is read
// at a time.
func (s *Server) runImportList(ctx context.Context, id int64) {
	s.lists.run.Lock()
	defer s.lists.run.Unlock()
	l, err := s.getImportList(id)
	if err != nil {
		return // the list was removed while it waited
	}
	var res listRunResult
	err = s.processImportList(ctx, l, &res)
	s.searchListAdditions(res)
	if err == nil && l.Source == listRSS && l.Config.Grab {
		res.grabbed = s.grabFromFeed(ctx, l, res.feed)
	}
	status, msg := "ok", res.message()
	if err != nil {
		status, msg = "error", listErrorText(err)
		slog.Warn("import list: couldn't read", "list", l.ID, "source", l.Source, "err", err)
	} else if res.added > 0 {
		names := res.addedTitles
		more := ""
		if len(names) > 5 {
			more, names = fmt.Sprintf(" and %d more", len(names)-5), names[:5]
		}
		_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("List %s added %s: %s%s", l.Name, plural(res.added, "title"), strings.Join(names, ", "), more))
	}
	_, derr := s.db.Exec(`UPDATE import_lists SET last_run_at = ?, last_status = ?, last_message = ?, last_found = ?, last_added = ?, last_skipped = ?, total_added = total_added + ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), status, msg, res.found, res.added, res.skipped(), res.added, id)
	if derr != nil {
		slog.Warn("import list: couldn't save the result", "list", id, "err", derr)
	}
}

func (s *Server) processImportList(ctx context.Context, l importList, res *listRunResult) error {
	if !s.TMDB().HasAPIKey() {
		return metadata.ErrNoAPIKey
	}
	var items []importlists.Item
	var err error
	if l.Source == listRSS {
		// The feed is read once: for its titles, and for its releases after.
		if res.feed, err = s.readFeedEntries(ctx, l.Config); err == nil {
			items = feedListItems(res.feed, l.Media)
		}
	} else {
		items, err = s.fetchListItems(ctx, l)
	}
	if err != nil {
		return err
	}
	items = dedupeListItems(items)
	res.found = len(items)
	seen, err := s.loadSeen(l.ID)
	if err != nil {
		return err
	}
	if l.LastRunAt == "" && len(seen) == 0 && l.StartFresh {
		for _, it := range items {
			s.recordSeen(l.ID, listRef(it), it.Kind, it.TMDBID, it.Title, it.Year, 0, "existing")
		}
		res.earlier = len(items)
		return nil
	}
	wanted := wantedKinds(l.Media)
	filter := importlists.Filter{MinRating: l.MinRating, MinYear: l.MinYear}
	user := &auth.User{Username: "list", FirstName: "Import list", LastName: l.Name, IsAdmin: true}
	failuresInARow := 0
	for _, it := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ref := listRef(it)
		row, had := seen[ref]
		if had && terminalOutcome(row.outcome) {
			res.earlier++
			continue
		}
		if it.Kind != "" && !wanted[it.Kind] {
			res.wrongKind++
			continue
		}
		kind, tmdbID, rating, year := row.kind, row.tmdbID, row.rating, row.year
		if !had || tmdbID == 0 {
			kind, tmdbID, rating, year = it.Kind, it.TMDBID, it.Rating, it.Year
			if tmdbID == 0 || kind == "" {
				var rerr error
				kind, tmdbID, rerr = s.resolveListItem(ctx, it, l.Media)
				if rerr != nil {
					if errors.Is(rerr, metadata.ErrInvalidKey) || errors.Is(rerr, metadata.ErrNoAPIKey) {
						return rerr
					}
					res.failed++
					failuresInARow++
					if res.firstFailure == "" {
						res.firstFailure = listErrorText(rerr)
					}
					if failuresInARow >= listFailureStop {
						return fmt.Errorf("the lookups kept failing: %w", rerr)
					}
					continue
				}
				time.Sleep(s.lists.gap(listLookupGap))
			}
			failuresInARow = 0
			if tmdbID == 0 {
				s.recordSeen(l.ID, ref, it.Kind, 0, it.Title, it.Year, 0, "no-match")
				res.noMatch++
				continue
			}
			if !wanted[kind] {
				res.wrongKind++
				continue
			}
		}
		if s.titleInLibrary(kind, tmdbID) {
			s.recordSeen(l.ID, ref, kind, tmdbID, it.Title, it.Year, rating, "in-library")
			res.inLibrary++
			continue
		}
		if s.titleExcluded(kind, tmdbID) {
			s.recordSeen(l.ID, ref, kind, tmdbID, it.Title, it.Year, rating, "excluded")
			res.excluded++
			continue
		}
		if filter.Active() {
			verdict := filter.Check(rating, year, false)
			if verdict == importlists.NeedDetails {
				var derr error
				rating, year, derr = s.titleDetails(ctx, kind, tmdbID, rating, year)
				time.Sleep(s.lists.gap(listLookupGap))
				if derr != nil {
					if errors.Is(derr, metadata.ErrInvalidKey) {
						return derr
					}
					res.failed++
					if res.firstFailure == "" {
						res.firstFailure = listErrorText(derr)
					}
					continue
				}
				verdict = filter.Check(rating, year, true)
			}
			if verdict != importlists.Pass {
				s.recordSeen(l.ID, ref, kind, tmdbID, it.Title, year, rating, "filtered")
				res.filtered++
				continue
			}
		}
		if res.added >= l.MaxPerRun {
			res.waiting++
			continue
		}
		outcome, detail, libraryID := s.addListTitle(ctx, l, user, kind, tmdbID)
		switch outcome {
		case "added":
			s.recordSeen(l.ID, ref, kind, tmdbID, it.Title, year, rating, "added")
			res.added++
			if l.SearchNow && l.Monitored && libraryID > 0 {
				if kind == "tv" {
					res.searchSeries = append(res.searchSeries, libraryID)
				} else {
					res.searchMovies = append(res.searchMovies, libraryID)
				}
			}
			name := it.Title
			if name == "" {
				name = fmt.Sprintf("TMDB %d", tmdbID)
			}
			res.addedTitles = append(res.addedTitles, name)
			time.Sleep(s.lists.gap(listAddGap))
		case "in-library":
			s.recordSeen(l.ID, ref, kind, tmdbID, it.Title, year, rating, "in-library")
			res.inLibrary++
		case "off":
			res.off++
		default:
			s.recordSeen(l.ID, ref, kind, tmdbID, it.Title, year, rating, "failed")
			res.failed++
			if res.firstFailure == "" {
				res.firstFailure = detail
			}
		}
	}
	return nil
}

// resolveListItem finds the TMDB id (and the kind) of a list entry that only
// names the title another way: by IMDb id, or by title and year. A title TMDB
// doesn't have gives id 0 and no error; an error means TMDB couldn't be asked.
func (s *Server) resolveListItem(ctx context.Context, it importlists.Item, media string) (string, int, error) {
	kind := it.Kind
	if it.TMDBID > 0 { // a bare TMDB id in a file: a movie unless the file says otherwise
		if kind == "" {
			kind = "movie"
		}
		return kind, it.TMDBID, nil
	}
	tm := s.TMDB()
	if it.IMDBID != "" {
		movieID, showID, err := tm.FindByIMDB(ctx, it.IMDBID)
		if err != nil {
			return "", 0, err
		}
		switch {
		case kind == "movie":
			return "movie", movieID, nil
		case kind == "tv":
			return "tv", showID, nil
		case movieID > 0 && wantedKinds(media)["movie"]:
			return "movie", movieID, nil
		case showID > 0:
			return "tv", showID, nil
		}
		if it.TVDBID == 0 { // a show that also has a TheTVDB id is tried that way next
			return kind, 0, nil
		}
	}
	if it.TVDBID > 0 && kind != "movie" {
		showID, err := tm.FindShowByTVDB(ctx, it.TVDBID)
		if err != nil {
			return "", 0, err
		}
		if showID > 0 && wantedKinds(media)["tv"] {
			return "tv", showID, nil
		}
	}
	if strings.TrimSpace(it.Title) == "" {
		return kind, 0, nil
	}
	tryKinds := []string{"movie", "tv"}
	if kind != "" {
		tryKinds = []string{kind}
	}
	for _, k := range tryKinds {
		if !wantedKinds(media)[k] {
			continue
		}
		var cands []importlists.Candidate
		if k == "movie" {
			found, err := tm.SearchMovies(ctx, it.Title)
			if err != nil {
				return "", 0, err
			}
			for _, m := range found {
				cands = append(cands, importlists.Candidate{ID: m.TMDBID, Title: m.Title, Year: m.Year()})
			}
		} else {
			found, err := tm.SearchTV(ctx, it.Title)
			if err != nil {
				return "", 0, err
			}
			for _, sh := range found {
				cands = append(cands, importlists.Candidate{ID: sh.TMDBID, Title: sh.Name, Year: sh.Year()})
			}
		}
		time.Sleep(s.lists.gap(listLookupGap))
		if id, ok := importlists.BestMatch(it.Title, it.Year, cands); ok {
			return k, id, nil
		}
	}
	return kind, 0, nil
}

func (s *Server) titleInLibrary(kind string, tmdbID int) bool {
	if kind == "tv" {
		_, ok, _ := s.MovieRepo.GetSeriesByTMDBID(tmdbID)
		return ok
	}
	_, ok, _ := s.MovieRepo.GetByTMDBID(tmdbID)
	return ok
}

func (s *Server) titleExcluded(kind string, tmdbID int) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM exclusions WHERE kind = ? AND tmdb_id = ?`, kind, tmdbID).Scan(&n)
	return n > 0
}

// titleDetails asks TMDB for a title's rating and year, keeping what the list
// already told when TMDB has nothing.
func (s *Server) titleDetails(ctx context.Context, kind string, tmdbID int, rating float64, year int) (float64, int, error) {
	if kind == "tv" {
		sh, err := s.TMDB().GetShow(ctx, tmdbID)
		if err != nil {
			return rating, year, err
		}
		if sh.VoteAverage > 0 {
			rating = sh.VoteAverage
		}
		if y := sh.Year(); y > 0 {
			year = y
		}
		return rating, year, nil
	}
	m, err := s.TMDB().GetMovie(ctx, tmdbID)
	if err != nil {
		return rating, year, err
	}
	if m.VoteAverage > 0 {
		rating = m.VoteAverage
	}
	if y := m.Year(); y > 0 {
		year = y
	}
	return rating, year, nil
}

// searchListAdditions looks for releases of the titles a read just added, when
// the list is set to search for what it adds. It is the same automatic search
// the scheduled hunt runs, not a forced one: it stops at the hourly limit for
// automatic downloads, waits for the delay profile, skips a movie that is not
// out yet and leaves indexers that are paused alone. Whatever it does not get
// to is found by the next scheduled search.
func (s *Server) searchListAdditions(res listRunResult) {
	if len(res.searchMovies) == 0 && len(res.searchSeries) == 0 {
		return
	}
	s.background(func() {
		ctx := context.Background()
		if !s.automationEnabled() || !s.autoGrabRoom() {
			return
		}
		instances, err := s.searchableIndexers()
		if err != nil || len(instances) == 0 {
			return
		}
		profiles, err := s.loadProfiles()
		if err != nil {
			slog.Warn("import list: load quality profiles for the new titles", "err", err)
			return
		}
		blocked := s.searchSkipKeys()
		for _, id := range res.searchMovies {
			if !s.autoGrabRoom() {
				holdBack("import-list")
				return
			}
			if m, err := s.MovieRepo.Get(id); err == nil && s.moviesEnabled() {
				s.huntMovie(ctx, m, instances, profiles, blocked, false)
			}
		}
		for _, id := range res.searchSeries {
			if !s.autoGrabRoom() {
				holdBack("import-list")
				return
			}
			if s.tvEnabled() {
				s.autoGrabTV(s.targetedTVSearcher(ctx, instances, maxTVSearchesPerHunt, false), "import-list", profiles, tvScope{seriesID: id})
			}
		}
	})
}

// addListTitle adds one title the way the add button on its page would, with
// the list's own choices, except that it never searches on its own: the
// search that follows goes through the automatic path (searchListAdditions). It
// says "added" (with the library id of the new title), "in-library" (it was
// there after all), "off" (that kind of media is switched off) or "failed" with
// the reason.
func (s *Server) addListTitle(ctx context.Context, l importList, user *auth.User, kind string, tmdbID int) (string, string, int64) {
	var (
		handler http.HandlerFunc
		payload map[string]any
	)
	const searchNow = false
	switch kind {
	case "tv":
		if !s.tvEnabled() {
			return "off", "", 0
		}
		monitor := "all"
		if !l.Monitored {
			monitor = "none"
		}
		handler = s.handleAddSeries
		payload = map[string]any{"tmdbId": tmdbID, "profileId": l.ProfileID, "monitor": monitor, "searchNow": searchNow, "rootPath": l.TVRoot}
	default:
		if !s.moviesEnabled() {
			return "off", "", 0
		}
		handler = s.handleAddMovie
		payload = map[string]any{"tmdbId": tmdbID, "profileId": l.ProfileID, "monitored": l.Monitored, "searchNow": searchNow, "rootPath": l.MovieRoot}
	}
	body, _ := json.Marshal(payload)
	inner := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)).WithContext(auth.WithUser(ctx, user))
	inner.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, inner)
	switch {
	case rec.Code == http.StatusCreated || rec.Code == http.StatusOK:
		var added struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &added)
		if len(l.Tags) > 0 && added.ID > 0 {
			k := library.TagMovie
			if kind == "tv" {
				k = library.TagSeries
			}
			if on, off, err := s.MovieRepo.SetTitleTags(k, added.ID, l.Tags); err == nil {
				s.pushTags(k, tmdbID, on, off)
			}
		}
		return "added", "", added.ID
	case rec.Code == http.StatusConflict:
		return "in-library", "", 0
	case rec.Code == http.StatusForbidden:
		return "off", "", 0
	}
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if e.Error == "" {
		e.Error = "A title couldn't be added."
	}
	slog.Info("import list: couldn't add a title", "list", l.ID, "tmdb", tmdbID, "status", rec.Code, "answer", e.Error)
	return "failed", e.Error, 0
}
