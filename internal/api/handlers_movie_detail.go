package api

import (
	"net/http"
	"strconv"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/queue"
)

type castPayload struct {
	Name       string `json:"name"`
	Character  string `json:"character"`
	ProfileURL string `json:"profileUrl,omitempty"`
}

// detailCastCount is how many billed actors the detail pages show.
const detailCastCount = 12

func castPayloads(cast []metadata.CastMember) []castPayload {
	out := make([]castPayload, len(cast))
	for i, c := range cast {
		out[i] = castPayload{Name: c.Name, Character: c.Character, ProfileURL: metadata.ProfileURL(c.ProfilePath)}
	}
	return out
}

type movieDetailPayload struct {
	TMDBID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Overview  string `json:"overview,omitempty"`
	PosterURL string `json:"posterUrl,omitempty"`
	LibraryID int64  `json:"libraryId,omitempty"` // 0 if not yet added to the library
	Status    string `json:"status,omitempty"`    // library state: missing/downloading/downloaded
	Quality   string `json:"quality,omitempty"`
	FilePath  string `json:"filePath,omitempty"`

	// From TMDB. ReleaseStatus is TMDB's own status ("Released", "In
	// Production"...), kept apart from Status above, the library state.
	Genres        []string           `json:"genres"`
	Rating        float64            `json:"rating"`
	VoteCount     int                `json:"voteCount"`
	Runtime       int                `json:"runtime,omitempty"` // minutes
	Tagline       string             `json:"tagline,omitempty"`
	Certification string             `json:"certification,omitempty"`
	ReleaseDate   string             `json:"releaseDate,omitempty"`
	ReleaseStatus string             `json:"releaseStatus,omitempty"`
	Language      string             `json:"language,omitempty"`
	Homepage      string             `json:"homepage,omitempty"`
	IMDBID        string             `json:"imdbId,omitempty"`
	Directors     []string           `json:"directors"`
	Cast          []castPayload      `json:"cast"`
	Trailers      []metadata.Trailer `json:"trailers"`
}

// handleTMDBMovieDetail gives everything the movie page shows from a TMDB id
// alone, so it works for titles whether or not they are in the library yet;
// library membership is looked up afterwards.
func (s *Server) handleTMDBMovieDetail(w http.ResponseWriter, r *http.Request) {
	tmdbID, err := strconv.Atoi(r.PathValue("tmdbId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid TMDB ID.")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie info and lists).")
		return
	}

	tmdbMovie, err := s.TMDB().GetMovieDetail(r.Context(), tmdbID)
	if err != nil {
		writeUpstreamError(w, "look up that movie", err)
		return
	}

	payload := movieDetailPayload{
		TMDBID: tmdbMovie.TMDBID, Title: tmdbMovie.Title, Year: tmdbMovie.Year(),
		Overview: tmdbMovie.Overview, PosterURL: metadata.PosterURL(tmdbMovie.PosterPath),
		Genres: s.TMDB().MovieGenres(r.Context(), tmdbMovie.Movie), Rating: metadata.RoundRating(tmdbMovie.VoteAverage),
		VoteCount: tmdbMovie.VoteCount, Runtime: tmdbMovie.Runtime, Tagline: tmdbMovie.Tagline,
		Certification: tmdbMovie.Certification(), ReleaseDate: tmdbMovie.ReleaseDate, ReleaseStatus: tmdbMovie.Status,
		Language: tmdbMovie.OriginalLanguage, Homepage: tmdbMovie.Homepage, IMDBID: tmdbMovie.IMDBID,
		Directors: tmdbMovie.Directors(),
		Cast:      castPayloads(tmdbMovie.Cast(detailCastCount)), Trailers: tmdbMovie.Trailers(),
	}

	if libMovie, ok, err := s.MovieRepo.GetByTMDBID(tmdbID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if ok {
		payload.LibraryID = libMovie.ID
		payload.Status = string(libMovie.Status)
		payload.Quality = libMovie.Quality
		payload.FilePath = libMovie.FilePath
	}

	writeJSON(w, http.StatusOK, payload)
}

// handleTMDBSimilarMovies lists movies like a given one from its TMDB id alone.
// Unlike handleSimilarMovies it works for a Discover title that isn't in the
// library yet, since there is no library id to go by.
func (s *Server) handleTMDBSimilarMovies(w http.ResponseWriter, r *http.Request) {
	tmdbID, err := strconv.Atoi(r.PathValue("tmdbId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid TMDB ID.")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, []discoverPayload{})
		return
	}
	similar, err := s.TMDB().SimilarMovies(r.Context(), tmdbID)
	if err != nil {
		writeUpstreamError(w, "load similar movies", err)
		return
	}
	out := make([]discoverPayload, len(similar))
	for i, m := range similar {
		out[i] = s.movieDiscover(r.Context(), m)
	}
	writeJSON(w, http.StatusOK, out)
}

type tvDetailPayload struct {
	TMDBID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Overview  string `json:"overview,omitempty"`
	PosterURL string `json:"posterUrl,omitempty"`
	LibraryID int64  `json:"libraryId,omitempty"` // series id; 0 if not yet added to the library
	Status    string `json:"status,omitempty"`    // library state: missing/downloading/downloaded

	FirstAirDate  string             `json:"firstAirDate,omitempty"`
	Seasons       int                `json:"seasons"`
	Episodes      int                `json:"episodes"`
	Networks      []string           `json:"networks"`
	ContentRating string             `json:"contentRating,omitempty"`
	Genres        []string           `json:"genres"`
	Rating        float64            `json:"rating"`
	VoteCount     int                `json:"voteCount"`
	ReleaseStatus string             `json:"releaseStatus,omitempty"` // "Returning Series", "Ended"...
	Tagline       string             `json:"tagline,omitempty"`
	Language      string             `json:"language,omitempty"`
	Homepage      string             `json:"homepage,omitempty"`
	IMDBID        string             `json:"imdbId,omitempty"`
	Creators      []string           `json:"creators"`
	SeasonList    []seasonPayload    `json:"seasonList"`
	Cast          []castPayload      `json:"cast"`
	Trailers      []metadata.Trailer `json:"trailers"`
}

// seasonPayload is one season of a show as the preview page lists it.
type seasonPayload struct {
	Number   int    `json:"number"`
	Name     string `json:"name"`
	Episodes int    `json:"episodes"`
	AirDate  string `json:"airDate,omitempty"`
}

// seasonPayloads lists the real seasons (not "Specials") that have episodes.
func seasonPayloads(seasons []metadata.SeasonSummary) []seasonPayload {
	out := []seasonPayload{}
	for _, s := range seasons {
		if s.SeasonNumber < 1 || s.EpisodeCount < 1 {
			continue
		}
		name := s.Name
		if name == "" {
			name = "Season " + strconv.Itoa(s.SeasonNumber)
		}
		out = append(out, seasonPayload{Number: s.SeasonNumber, Name: name, Episodes: s.EpisodeCount, AirDate: s.AirDate})
	}
	return out
}

// handleTMDBTVDetail gives everything the TV page shows from a TMDB id alone,
// plus library membership if the show has been added. It is the same as
// handleTMDBMovieDetail, for shows.
func (s *Server) handleTMDBTVDetail(w http.ResponseWriter, r *http.Request) {
	tmdbID, err := strconv.Atoi(r.PathValue("tmdbId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid TMDB ID.")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie info and lists).")
		return
	}
	show, err := s.TMDB().GetShowFull(r.Context(), tmdbID)
	if err != nil {
		writeUpstreamError(w, "look up that show", err)
		return
	}
	payload := tvDetailPayload{
		TMDBID: show.TMDBID, Title: show.Name, Year: show.Year(), Overview: show.Overview,
		PosterURL: metadata.PosterURL(show.PosterPath), FirstAirDate: show.FirstAirDate,
		Seasons: show.SeasonCount(), Episodes: show.NumberOfEpisodes, Networks: show.NetworkNames(),
		ContentRating: show.ContentRating(), Genres: s.TMDB().ShowGenres(r.Context(), show.Show),
		Rating: metadata.RoundRating(show.VoteAverage), VoteCount: show.VoteCount,
		ReleaseStatus: show.Status, Tagline: show.Tagline, Language: show.OriginalLanguage, Homepage: show.Homepage,
		IMDBID: show.ExternalIDs.IMDBID, Creators: show.Creators(), SeasonList: seasonPayloads(show.Seasons),
		Cast: castPayloads(show.Cast(detailCastCount)), Trailers: show.Trailers(),
	}

	if series, ok, err := s.MovieRepo.GetSeriesByTMDBID(tmdbID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if ok {
		payload.LibraryID = series.ID
		payload.Status = s.seriesLibraryState(series)
	}
	writeJSON(w, http.StatusOK, payload)
}

// seriesLibraryState collapses a series into the same missing/downloading/
// downloaded vocabulary movies use: downloading while anything for it is in
// flight, downloaded once every episode has a file.
func (s *Server) seriesLibraryState(series library.Series) string {
	if items, err := s.QueueRepo.List(); err == nil {
		for _, it := range items {
			if it.SeriesID == series.ID && (it.Status == queue.StatusQueued || it.Status == queue.StatusDownloading || it.Status == queue.StatusImporting) {
				return string(library.StatusDownloading)
			}
		}
	}
	if series.EpisodeCount > 0 && series.DownloadedCount >= series.EpisodeCount {
		return string(library.StatusDownloaded)
	}
	return string(library.StatusMissing)
}
