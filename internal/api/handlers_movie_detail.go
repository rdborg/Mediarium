package api

import (
	"net/http"
	"strconv"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/queue"
)

type castPayload struct {
	// ID is the person's TMDB id, which opens their page; 0 when TMDB gave none.
	ID         int    `json:"id,omitempty"`
	Name       string `json:"name"`
	Character  string `json:"character"`
	ProfileURL string `json:"profileUrl,omitempty"`
}

// crewPayload is a director, writer or creator of a title.
type crewPayload struct {
	ID         int    `json:"id,omitempty"`
	Name       string `json:"name"`
	Job        string `json:"job"` // "Director", "Writer" or "Creator"
	ProfileURL string `json:"profileUrl,omitempty"`
}

// detailCastCount is how many billed actors the detail pages send; the page
// shows the first dozen and the rest behind "Show all".
const detailCastCount = 30

func castPayloads(cast []metadata.CastMember) []castPayload {
	out := make([]castPayload, len(cast))
	for i, c := range cast {
		out[i] = castPayload{ID: c.ID, Name: c.Name, Character: c.Character, ProfileURL: metadata.ProfileURL(c.ProfilePath)}
	}
	return out
}

func crewPayloads(groups ...[]metadata.CrewMember) []crewPayload {
	out := []crewPayload{}
	for _, g := range groups {
		for _, c := range g {
			out = append(out, crewPayload{ID: c.ID, Name: c.Name, Job: c.Job, ProfileURL: metadata.ProfileURL(c.ProfilePath)})
		}
	}
	return out
}

type movieDetailPayload struct {
	TMDBID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Overview  string `json:"overview,omitempty"`
	PosterURL string `json:"posterUrl,omitempty"`
	// BackdropURL is a wide still for the top of the title page.
	BackdropURL string `json:"backdropUrl,omitempty"`
	LibraryID   int64  `json:"libraryId,omitempty"` // 0 if not yet added to the library
	// Origin is how it came into the library, for administrators; left out when
	// it was added before that was recorded.
	Origin   *originView `json:"origin,omitempty"`
	Status   string      `json:"status,omitempty"` // library state: missing/downloading/downloaded
	Quality  string      `json:"quality,omitempty"`
	FilePath string      `json:"filePath,omitempty"`
	// FolderPath is the full folder of the movie's files in the library (as
	// Mediarium sees it), or where they go when it has none.
	FolderPath string `json:"folderPath,omitempty"`
	// FileInfo is what the movie's file holds (video, audio and subtitle
	// tracks); left out until the file has been read (media_info.go).
	FileInfo *fileInfoPayload `json:"fileInfo,omitempty"`
	// ShortFile is set when the file is far shorter than the movie should be.
	ShortFile *shortFileNote `json:"shortFile,omitempty"`

	// From TMDB. ReleaseStatus is TMDB's own status ("Released", "In
	// Production"...), kept apart from Status above, the library state.
	Genres        []string `json:"genres"`
	Rating        float64  `json:"rating"`
	VoteCount     int      `json:"voteCount"`
	Runtime       int      `json:"runtime,omitempty"` // minutes
	Tagline       string   `json:"tagline,omitempty"`
	Certification string   `json:"certification,omitempty"`
	ReleaseDate   string   `json:"releaseDate,omitempty"`
	// ReleaseDates are the cinema and digital dates in the person's country
	// (the one of the info language), with a fallback when it lists none.
	ReleaseDates  metadata.MovieReleaseDates `json:"releaseDates"`
	ReleaseStatus string                     `json:"releaseStatus,omitempty"`
	Language      string                     `json:"language,omitempty"`
	Homepage      string                     `json:"homepage,omitempty"`
	IMDBID        string                     `json:"imdbId,omitempty"`
	Directors     []string                   `json:"directors"`
	Studios       []string                   `json:"studios"` // the production companies
	Crew          []crewPayload              `json:"crew"`    // directors and writers, with ids
	Cast          []castPayload              `json:"cast"`
	Trailers      []metadata.Trailer         `json:"trailers"`
	// Collection is the series of films this one is part of, when it is.
	Collection *collectionRef `json:"collection,omitempty"`
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
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Library and files > Movie and show info).")
		return
	}

	tmdbMovie, err := s.displayTMDB().GetMovieDetail(r.Context(), tmdbID)
	if err != nil {
		writeUpstreamError(w, "look up that movie", err)
		return
	}

	payload := movieDetailPayload{
		TMDBID: tmdbMovie.TMDBID, Title: tmdbMovie.Title, Year: tmdbMovie.Year(),
		Overview: tmdbMovie.Overview, PosterURL: metadata.PosterURL(tmdbMovie.PosterPath), BackdropURL: metadata.BackdropURL(tmdbMovie.BackdropPath),
		Genres: s.displayTMDB().MovieGenres(r.Context(), tmdbMovie.Movie), Rating: metadata.RoundRating(tmdbMovie.VoteAverage),
		VoteCount: tmdbMovie.VoteCount, Runtime: tmdbMovie.Runtime, Tagline: tmdbMovie.Tagline,
		Certification: tmdbMovie.Certification(), ReleaseDate: tmdbMovie.ReleaseDate, ReleaseDates: tmdbMovie.ReleaseDatesFor(s.releaseRegion()), ReleaseStatus: tmdbMovie.Status,
		Language: tmdbMovie.OriginalLanguage, Homepage: tmdbMovie.Homepage, IMDBID: tmdbMovie.IMDBID,
		Directors: tmdbMovie.Directors(), Studios: tmdbMovie.Studios(),
		Crew: crewPayloads(tmdbMovie.DirectorCrew(), tmdbMovie.WriterCrew()),
		Cast: castPayloads(tmdbMovie.Cast(detailCastCount)), Trailers: tmdbMovie.Trailers(),
		Collection: collectionRefOf(tmdbMovie.BelongsToCollection),
	}

	if libMovie, ok, err := s.MovieRepo.GetByTMDBID(tmdbID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if ok {
		payload.LibraryID = libMovie.ID
		payload.Status = string(libMovie.Status)
		payload.Quality = libMovie.Quality
		payload.FilePath = fileLabel(r, libMovie.FilePath)
		if libMovie.Status == library.StatusDownloaded {
			payload.FileInfo = s.movieFileInfo(libMovie.FilePath)
			payload.ShortFile = s.shortFileOf(libMovie.FilePath)
		}
		if isAdminRequest(r) { // only administrators see where the files lie
			payload.FolderPath = s.movieFolderFor(libMovie)
			payload.Origin = originOut(libMovie.Origin, s.accountNames()(libMovie.AddedBy))
		}
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
	similar, err := s.displayTMDB().SimilarMovies(r.Context(), tmdbID)
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
	// BackdropURL is a wide still for the top of the title page.
	BackdropURL string `json:"backdropUrl,omitempty"`
	LibraryID   int64  `json:"libraryId,omitempty"` // series id; 0 if not yet added to the library
	Status      string `json:"status,omitempty"`    // library state: missing/downloading/downloaded

	FirstAirDate  string             `json:"firstAirDate,omitempty"`
	Seasons       int                `json:"seasons"`
	Episodes      int                `json:"episodes"`
	Networks      []string           `json:"networks"`
	Studios       []string           `json:"studios"` // the production companies
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
	Crew          []crewPayload      `json:"crew"` // the creators, with ids
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
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Library and files > Movie and show info).")
		return
	}
	show, err := s.displayTMDB().GetShowFull(r.Context(), tmdbID)
	if err != nil {
		writeUpstreamError(w, "look up that show", err)
		return
	}
	payload := tvDetailPayload{
		TMDBID: show.TMDBID, Title: show.Name, Year: show.Year(), Overview: show.Overview,
		PosterURL: metadata.PosterURL(show.PosterPath), BackdropURL: metadata.BackdropURL(show.BackdropPath), FirstAirDate: show.FirstAirDate,
		Seasons: show.SeasonCount(), Episodes: show.NumberOfEpisodes, Networks: show.NetworkNames(), Studios: show.Studios(),
		ContentRating: show.ContentRating(), Genres: s.displayTMDB().ShowGenres(r.Context(), show.Show),
		Rating: metadata.RoundRating(show.VoteAverage), VoteCount: show.VoteCount,
		ReleaseStatus: show.Status, Tagline: show.Tagline, Language: show.OriginalLanguage, Homepage: show.Homepage,
		IMDBID: show.ExternalIDs.IMDBID, Creators: show.Creators(), Crew: crewPayloads(show.CreatorCrew()), SeasonList: seasonPayloads(show.Seasons),
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
	if items, err := s.QueueRepo.ListActive(); err == nil {
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
