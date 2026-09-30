package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/listenbrainz"
	"github.com/rdborg/mediarium/internal/music"
)

// Music discovery: what people are listening to, what came out lately and
// what is about to. The lists come from a free public service that needs no
// key (internal/listenbrainz) and are kept for an hour. The interface never
// talks to it or to an image host itself: covers go through
// /api/music/covers/release-group/{mbid}. When the service cannot be reached
// the routes still answer 200 with an empty list and a plain note, so a
// Discover page never shows an error.

const (
	discoverListPopular  = "popular"
	discoverListNew      = "new"
	discoverListUpcoming = "upcoming"

	discoverPageSize    = 24
	discoverMaxPageSize = 100
	// discoverSourcePages is how many pages of 100 the popular lists ask for.
	discoverSourcePages  = 2
	discoverSourceCount  = 100
	discoverFreshDays    = 14
	discoverUpcomingDays = 90
	discoverTimeout      = 45 * time.Second

	// discoverUnavailable is the note sent with an empty list when the
	// source failed. It names no provider on purpose.
	discoverUnavailable = "The list isn't available right now. Try again later."
)

// discoverClient is the shared client for the Discover lists.
func (s *Server) discoverClient() *listenbrainz.Client {
	s.music.lbMu.Lock()
	defer s.music.lbMu.Unlock()
	if s.music.lb == nil {
		s.music.lb = listenbrainz.New(s.version)
	}
	return s.music.lb
}

type musicDiscoverItem struct {
	MBID        string   `json:"mbid"` // release group id
	Title       string   `json:"title"`
	Type        string   `json:"type"` // album, ep or single
	ArtistName  string   `json:"artistName"`
	ArtistMBID  string   `json:"artistMbid"`
	ReleaseDate string   `json:"releaseDate"` // "YYYY[-MM[-DD]]", "" if unknown
	Genres      []string `json:"genres"`
	CoverURL    string   `json:"coverUrl"`
	InLibrary   bool     `json:"inLibrary"`          // the album itself is in the library
	ArtistID    int64    `json:"artistId,omitempty"` // the artist is in the library
	AlbumID     int64    `json:"albumId,omitempty"`
}

type musicDiscoverPayload struct {
	Page       int                 `json:"page"`
	TotalPages int                 `json:"totalPages"`
	Items      []musicDiscoverItem `json:"items"`
	Note       string              `json:"note,omitempty"`
}

// discoverCandidate is one entry of a source list before it is filtered,
// paged and matched against the library.
type discoverCandidate struct {
	mbid, title, typ, artistName, artistMBID, date string
	genres                                         []string
	listens                                        int
}

// discoverType maps a release group's primary type to ours: the studio
// releases the library keeps. ok is false for anything else (broadcasts,
// "other" and releases without a type).
func discoverType(primary string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(primary)) {
	case "album":
		return music.TypeAlbum, true
	case "ep":
		return music.TypeEP, true
	case "single":
		return music.TypeSingle, true
	}
	return "", false
}

// pageParams reads page and pageSize; bad values fall back to the defaults.
func pageParams(r *http.Request) (page, size int) {
	page, size = 1, discoverPageSize
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n > 1 {
		page = n
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("pageSize")); err == nil && n > 0 {
		size = min(n, discoverMaxPageSize)
	}
	return page, size
}

// pageBounds returns the slice [from, to) of page out of total items, and
// the number of pages (at least 1).
func pageBounds(total, page, size int) (from, to, pages int) {
	pages = max(1, (total+size-1)/size)
	from = (page - 1) * size
	if from > total {
		from = total
	}
	return from, min(from+size, total), pages
}

// handleMusicDiscover is GET /api/music/discover?list=popular|new|upcoming
// &type=album|ep|single|all&range=week|month|year|all_time&page=&pageSize=
// &genre=&yearFrom=&yearTo=&sort=popular|newest|oldest. range only applies to
// "popular". A year filter drops entries with no known date. Any account.
func (s *Server) handleMusicDiscover(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list := q.Get("list")
	if list == "" {
		list = discoverListPopular
	}
	if list != discoverListPopular && list != discoverListNew && list != discoverListUpcoming {
		writeError(w, http.StatusBadRequest, `list must be "popular", "new" or "upcoming"`)
		return
	}
	typ := strings.ToLower(q.Get("type"))
	if typ == "" {
		typ = "all"
	}
	if typ != "all" {
		if _, ok := discoverType(typ); !ok {
			writeError(w, http.StatusBadRequest, `type must be "album", "ep", "single" or "all"`)
			return
		}
	}
	rng := q.Get("range")
	if rng == "" {
		rng = listenbrainz.RangeWeek
	}
	if !listenbrainz.ValidRange(rng) {
		writeError(w, http.StatusBadRequest, `range must be "week", "month", "year" or "all_time"`)
		return
	}
	genre := strings.ToLower(strings.TrimSpace(q.Get("genre")))
	yearFrom, err := yearParam(q.Get("yearFrom"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "yearFrom must be a four digit year")
		return
	}
	yearTo, err := yearParam(q.Get("yearTo"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "yearTo must be a four digit year")
		return
	}
	order := strings.ToLower(q.Get("sort"))
	if order != "" && order != "popular" && order != "newest" && order != "oldest" {
		writeError(w, http.StatusBadRequest, `sort must be "popular", "newest" or "oldest"`)
		return
	}
	page, size := pageParams(r)

	ctx, cancel := context.WithTimeout(r.Context(), discoverTimeout)
	defer cancel()
	lb := s.discoverClient()

	var cands []discoverCandidate
	switch list {
	case discoverListPopular:
		cands, err = discoverPopular(ctx, lb, rng)
	default:
		cands, err = discoverFresh(ctx, lb, list == discoverListUpcoming, time.Now().UTC())
	}
	out := musicDiscoverPayload{Page: page, TotalPages: 1, Items: []musicDiscoverItem{}}
	if err != nil {
		slog.Warn("music: discover list unavailable", "list", list, "err", err)
		out.Note = discoverUnavailable
		writeJSON(w, http.StatusOK, out)
		return
	}
	if len(cands) == 0 {
		out.Note = discoverUnavailable
		writeJSON(w, http.StatusOK, out)
		return
	}

	if typ != "all" {
		cands = filterCandidates(cands, func(c discoverCandidate) bool { return c.typ == typ })
	}
	// Genres come with the source's tags. A list without any tag cannot be
	// filtered by genre, so the filter is left out rather than emptying it.
	if genre != "" && anyGenres(cands) {
		cands = filterCandidates(cands, func(c discoverCandidate) bool { return hasGenre(c.genres, genre) })
	}

	if yearFrom > 0 || yearTo > 0 {
		cands = filterCandidates(cands, func(c discoverCandidate) bool {
			y := candidateYear(c.date)
			return y > 0 && (yearFrom == 0 || y >= yearFrom) && (yearTo == 0 || y <= yearTo)
		})
	}
	if order == "newest" || order == "oldest" {
		sortByDate(cands, order == "newest")
	}

	from, to, pages := pageBounds(len(cands), page, size)
	out.TotalPages = pages
	pageItems := cands[from:to]
	if list != discoverListPopular {
		s.fillGenres(ctx, lb, pageItems)
	}
	items, err := s.discoverItems(pageItems)
	if err != nil {
		slog.Error("music: discover library lookup failed", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the library. Check the Mediarium log for details.")
		return
	}
	out.Items = items
	writeJSON(w, http.StatusOK, out)
}

func filterCandidates(in []discoverCandidate, keep func(discoverCandidate) bool) []discoverCandidate {
	out := make([]discoverCandidate, 0, len(in))
	for _, c := range in {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

func anyGenres(cands []discoverCandidate) bool {
	for _, c := range cands {
		if len(c.genres) > 0 {
			return true
		}
	}
	return false
}

// hasGenre reports whether one of the tags is the genre or a kind of it, so
// "rock" also finds "punk rock" and "hard rock".
func hasGenre(genres []string, want string) bool {
	for _, g := range genres {
		if strings.Contains(strings.ToLower(strings.TrimSpace(g)), want) {
			return true
		}
	}
	return false
}

// yearParam reads an optional year; an empty value is 0 (no limit).
func yearParam(v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1000 || n > 9999 {
		return 0, fmt.Errorf("not a year: %q", v)
	}
	return n, nil
}

// candidateYear is the year of a "YYYY[-MM[-DD]]" date, 0 when unknown.
func candidateYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	n, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return n
}

// sortByDate orders entries by release date, newest or oldest first. Entries
// without a date go last either way, and equal dates keep their order.
func sortByDate(cands []discoverCandidate, newestFirst bool) {
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i].date, cands[j].date
		switch {
		case a == "" || b == "":
			return a != "" && b == ""
		case newestFirst:
			return a > b
		default:
			return a < b
		}
	})
}

// discoverPopular is the most listened-to release groups over a range, with
// their type, date and genres looked up in bulk. A failed lookup only costs
// the details (every entry is then taken for an album without genres).
func discoverPopular(ctx context.Context, lb *listenbrainz.Client, rng string) ([]discoverCandidate, error) {
	var stats []listenbrainz.ReleaseGroupStat
	seen := map[string]bool{}
	for p := 0; p < discoverSourcePages; p++ {
		got, err := lb.TopReleaseGroups(ctx, rng, discoverSourceCount, p*discoverSourceCount)
		if err != nil {
			if p == 0 {
				return nil, fmt.Errorf("popular releases: %w", err)
			}
			slog.Info("music: popular releases, later page unavailable", "err", err)
			break
		}
		for _, rg := range got {
			if !seen[rg.MBID] {
				seen[rg.MBID] = true
				stats = append(stats, rg)
			}
		}
		if len(got) < discoverSourceCount {
			break
		}
	}
	ids := make([]string, len(stats))
	for i, rg := range stats {
		ids[i] = rg.MBID
	}
	infos, err := lb.ReleaseGroupInfos(ctx, ids)
	if err != nil {
		slog.Info("music: popular releases without details", "err", err)
	}
	out := make([]discoverCandidate, 0, len(stats))
	for _, rg := range stats {
		c := discoverCandidate{mbid: rg.MBID, title: rg.Title, typ: music.TypeAlbum, artistName: rg.ArtistName, listens: rg.ListenCount}
		if len(rg.ArtistMBIDs) > 0 {
			c.artistMBID = rg.ArtistMBIDs[0]
		}
		if info, ok := infos[rg.MBID]; ok {
			if strings.TrimSpace(info.Type) != "" {
				t, ok := discoverType(info.Type)
				if !ok {
					continue // a broadcast or other odd release
				}
				c.typ = t
			}
			c.date, c.genres = info.Date, info.Genres
		}
		out = append(out, c)
	}
	return out, nil
}

// discoverFresh is the releases of the last two weeks (newest first), or the
// ones dated after today within the next 90 days (soonest first). Live
// albums, compilations, remixes and other secondary types are left out, as
// they are when an artist is added.
func discoverFresh(ctx context.Context, lb *listenbrainz.Client, upcoming bool, now time.Time) ([]discoverCandidate, error) {
	days := discoverFreshDays
	if upcoming {
		days = discoverUpcomingDays
	}
	releases, err := lb.FreshReleases(ctx, days, upcoming)
	if err != nil {
		return nil, fmt.Errorf("fresh releases: %w", err)
	}
	today := now.Format("2006-01-02")
	out := make([]discoverCandidate, 0, len(releases))
	for _, rel := range releases {
		typ, ok := discoverType(rel.PrimaryType)
		if !ok || strings.TrimSpace(rel.SecondaryType) != "" || len(rel.Date) < 10 {
			continue
		}
		if upcoming == (rel.Date <= today) { // upcoming: after today; new: today or before
			continue
		}
		c := discoverCandidate{mbid: rel.MBID, title: rel.Title, typ: typ, artistName: rel.ArtistName, date: rel.Date[:10], listens: rel.ListenCount}
		if len(rel.ArtistMBIDs) > 0 {
			c.artistMBID = rel.ArtistMBIDs[0]
		}
		for _, tag := range rel.Tags {
			if tag = strings.TrimSpace(tag); tag != "" {
				c.genres = append(c.genres, tag)
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].date != out[j].date {
			if upcoming {
				return out[i].date < out[j].date
			}
			return out[i].date > out[j].date
		}
		return out[i].listens > out[j].listens
	})
	return out, nil
}

// fillGenres gives the entries of one page that came without tags their
// genres from the details service. It is best effort: a failure leaves them
// empty.
func (s *Server) fillGenres(ctx context.Context, lb *listenbrainz.Client, cands []discoverCandidate) {
	var ids []string
	for _, c := range cands {
		if len(c.genres) == 0 {
			ids = append(ids, c.mbid)
		}
	}
	if len(ids) == 0 {
		return
	}
	infos, err := lb.ReleaseGroupInfos(ctx, ids)
	if err != nil {
		slog.Info("music: discover genres unavailable", "err", err)
	}
	for i := range cands {
		if len(cands[i].genres) == 0 {
			cands[i].genres = infos[cands[i].mbid].Genres
		}
	}
}

// discoverItems builds the payload of one page and marks what is already in
// the library.
func (s *Server) discoverItems(cands []discoverCandidate) ([]musicDiscoverItem, error) {
	albumIDs := make([]string, 0, len(cands))
	artistIDs := make([]string, 0, len(cands))
	for _, c := range cands {
		albumIDs = append(albumIDs, c.mbid)
		if c.artistMBID != "" {
			artistIDs = append(artistIDs, c.artistMBID)
		}
	}
	albums, err := s.MusicRepo.AlbumsByMBID(albumIDs)
	if err != nil {
		return nil, err
	}
	artists, err := s.MusicRepo.ArtistsByMBID(artistIDs)
	if err != nil {
		return nil, err
	}
	out := make([]musicDiscoverItem, len(cands))
	for i, c := range cands {
		it := musicDiscoverItem{
			MBID: c.mbid, Title: c.title, Type: c.typ, ArtistName: c.artistName, ArtistMBID: c.artistMBID,
			ReleaseDate: c.date, Genres: c.genres, CoverURL: releaseGroupCoverURL(c.mbid),
		}
		if it.Genres == nil {
			it.Genres = []string{}
		}
		if a, ok := artists[c.artistMBID]; ok {
			it.ArtistID = a.ID
		}
		if al, ok := albums[c.mbid]; ok {
			it.InLibrary, it.AlbumID, it.ArtistID = true, al.ID, al.ArtistID
			it.CoverURL = albumCoverURL(al.ID)
		}
		out[i] = it
	}
	return out, nil
}

// releaseGroupCoverURL is the address of a release group's cover on this server.
func releaseGroupCoverURL(mbid string) string {
	return "/api/music/covers/release-group/" + mbid
}

type discoverArtistPayload struct {
	MBID        string `json:"mbid"`
	Name        string `json:"name"`
	ListenCount int    `json:"listenCount"`
	InLibrary   bool   `json:"inLibrary"`
	ArtistID    int64  `json:"artistId,omitempty"`
	CoverURL    string `json:"coverUrl,omitempty"` // only for artists in the library
}

type discoverArtistsPayload struct {
	Page       int                     `json:"page"`
	TotalPages int                     `json:"totalPages"`
	Items      []discoverArtistPayload `json:"items"`
	Note       string                  `json:"note,omitempty"`
}

// handleMusicDiscoverArtists is GET /api/music/discover/artists?range=&page=
// &pageSize=: the most listened-to artists, marking the ones in the library.
func (s *Server) handleMusicDiscoverArtists(w http.ResponseWriter, r *http.Request) {
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = listenbrainz.RangeWeek
	}
	if !listenbrainz.ValidRange(rng) {
		writeError(w, http.StatusBadRequest, `range must be "week", "month", "year" or "all_time"`)
		return
	}
	page, size := pageParams(r)
	out := discoverArtistsPayload{Page: page, TotalPages: 1, Items: []discoverArtistPayload{}}

	ctx, cancel := context.WithTimeout(r.Context(), discoverTimeout)
	defer cancel()
	stats, err := s.discoverClient().TopArtists(ctx, rng, discoverSourceCount, 0)
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("music: discover artists unavailable", "err", err)
	}
	if err != nil || len(stats) == 0 {
		out.Note = discoverUnavailable
		writeJSON(w, http.StatusOK, out)
		return
	}
	from, to, pages := pageBounds(len(stats), page, size)
	out.TotalPages = pages
	stats = stats[from:to]
	ids := make([]string, len(stats))
	for i, a := range stats {
		ids[i] = a.MBID
	}
	known, err := s.MusicRepo.ArtistsByMBID(ids)
	if err != nil {
		slog.Error("music: discover library lookup failed", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the library. Check the Mediarium log for details.")
		return
	}
	for _, a := range stats {
		it := discoverArtistPayload{MBID: a.MBID, Name: a.Name, ListenCount: a.ListenCount}
		if k, ok := known[a.MBID]; ok {
			it.InLibrary, it.ArtistID, it.CoverURL = true, k.ID, artistCoverURL(k.ID)
		}
		out.Items = append(out.Items, it)
	}
	writeJSON(w, http.StatusOK, out)
}
