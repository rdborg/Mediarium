package api

// What compat_downloaders_test.go (package api_test) looks at directly.

// TestMatchLibraryTitle says which movie, or which show with its season and
// episode, a release name is for.
func (s *Server) TestMatchLibraryTitle(releaseTitle string) (kind string, id int64, season, episode int, err error) {
	t, err := s.matchLibraryTitle(releaseTitle)
	if err != nil {
		return "", 0, 0, 0, err
	}
	if t.isMovie {
		return "movie", t.movie.ID, 0, 0, nil
	}
	return "show", t.series.ID, t.season, t.episode, nil
}
