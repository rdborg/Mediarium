package api

import (
	"context"
	"net/http"
	"strconv"
	"sync"

	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/metadata"
	"github.com/ryanborg/mediarium/internal/settings"
)

// Which downloaders an item may use: Usenet, torrents, or both. Each movie
// and show can override the default from Settings.
const (
	sourcesBoth    = "both"
	sourcesUsenet  = "usenet"
	sourcesTorrent = "torrent"
)

// validSourcePref accepts "" (follow the default) or one of the three modes.
func validSourcePref(v string) bool {
	return v == "" || v == sourcesBoth || v == sourcesUsenet || v == sourcesTorrent
}

func (s *Server) defaultSources() string {
	if v, _ := s.Settings.Get(settings.KeyDefaultSources); validSourcePref(v) && v != "" {
		return v
	}
	return sourcesBoth
}

// sourcesFor resolves an item's preference against the default. With torrents
// switched off they are unavailable to everything, so every item is Usenet
// only whatever it asked for.
func (s *Server) sourcesFor(pref string) string {
	if !s.torrentsEnabled() {
		return sourcesUsenet
	}
	if pref == "" {
		return s.defaultSources()
	}
	return pref
}

// filterSources keeps only releases from the allowed downloaders.
func filterSources(results []indexers.Result, sources string) []indexers.Result {
	if sources == sourcesBoth || sources == "" {
		return results
	}
	want := indexers.Protocol(sources)
	out := make([]indexers.Result, 0, len(results))
	for _, r := range results {
		if r.Protocol == want {
			out = append(out, r)
		}
	}
	return out
}

// preferUsenet reports whether candidate should replace best when the two are
// otherwise equal: a Usenet release wins over a torrent (no seeding, no swarm
// to depend on, no IP exposure).
func preferUsenet(best, candidate *indexers.Result) bool {
	return best != nil && candidate.Protocol == indexers.ProtocolUsenet && best.Protocol != indexers.ProtocolUsenet
}

// unreleased reports whether a release date is still in the future, so there
// is nothing to download yet.
func unreleased(releaseDate string) bool {
	return releaseDate != "" && releaseDate > todayUTC()
}

type setSourcesRequest struct {
	Sources string `json:"sources"` // "" = follow the default
}

func (s *Server) handleSetMovieSources(w http.ResponseWriter, r *http.Request) {
	s.setItemSources(w, r, "movie", s.MovieRepo.SetSourcePref)
}

func (s *Server) handleSetSeriesSources(w http.ResponseWriter, r *http.Request) {
	s.setItemSources(w, r, "series", s.MovieRepo.SetSeriesSourcePref)
}

func (s *Server) setItemSources(w http.ResponseWriter, r *http.Request, what string, set func(id int64, pref string) error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+what+" id")
		return
	}
	var req setSourcesRequest
	if err := decodeJSON(r, &req); err != nil || !validSourcePref(req.Sources) {
		writeError(w, http.StatusBadRequest, `sources must be "", "usenet", "torrent" or "both"`)
		return
	}
	if err := set(id, req.Sources); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

type titleSearchResult struct {
	Kind      string `json:"kind"` // "movie" or "tv"
	TMDBID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Overview  string `json:"overview,omitempty"`
	PosterURL string `json:"posterUrl,omitempty"`
	InLibrary bool   `json:"inLibrary"`
	LibraryID int64  `json:"libraryId,omitempty"`
	Status    string `json:"status,omitempty"` // movie: missing/downloading/downloaded; show: "3/10"

	MediaType string   `json:"mediaType"` // same as Kind
	Genres    []string `json:"genres"`
	Rating    float64  `json:"rating"`
	VoteCount int      `json:"voteCount"`
}

const maxTitleResults = 24

// handleTitleSearch searches the movie and TV databases together — the first
// step of finding something, as in Radarr and Sonarr. Picking a result adds it
// to the library; the indexers are only searched afterwards, for releases of
// that specific title. The response flags what is already in the library.
func (s *Server) handleTitleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if len(query) < 2 {
		writeJSON(w, http.StatusOK, []titleSearchResult{})
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "TMDB API key not configured yet — set it in Settings > Metadata")
		return
	}
	results, err := s.searchTitles(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusBadGateway, "search TMDB: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) searchTitles(ctx context.Context, query string) ([]titleSearchResult, error) {
	found, err := s.TMDB().SearchMulti(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(found) > maxTitleResults {
		found = found[:maxTitleResults]
	}
	out := make([]titleSearchResult, len(found))
	var wg sync.WaitGroup
	for i, f := range found {
		out[i] = titleSearchResult{
			Kind: f.MediaType, TMDBID: f.ID, Title: f.DisplayTitle(), Year: f.Year(), Overview: f.Overview,
			PosterURL: metadata.PosterURL(f.PosterPath),
			MediaType: f.MediaType, Genres: s.TMDB().MultiGenres(ctx, f),
			Rating: metadata.RoundRating(f.VoteAverage), VoteCount: f.VoteCount,
		}
		wg.Add(1)
		go func(i int, kind string, tmdbID int) {
			defer wg.Done()
			if kind == "movie" {
				if m, ok, err := s.MovieRepo.GetByTMDBID(tmdbID); err == nil && ok {
					out[i].InLibrary, out[i].LibraryID, out[i].Status = true, m.ID, string(m.Status)
				}
				return
			}
			if sr, ok, err := s.MovieRepo.GetSeriesByTMDBID(tmdbID); err == nil && ok {
				out[i].InLibrary, out[i].LibraryID = true, sr.ID
				out[i].Status = strconv.Itoa(sr.DownloadedCount) + "/" + strconv.Itoa(sr.EpisodeCount)
			}
		}(i, f.MediaType, f.ID)
	}
	wg.Wait()
	return out, nil
}
