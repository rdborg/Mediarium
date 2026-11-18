package api

import (
	"fmt"
	"path/filepath"
)

// Two titles can render to the same file name: a remake with the same title
// and year, or titles that differ only in characters a file name can't hold.
// Importing over the other title's file would take it away from that title
// and leave its record pointing at the wrong film, so an import never
// overwrites a file the library has down for another movie or episode,
// whatever "If a file already exists" says.

// fileOwner is a movie or episode whose file is at a given path.
type fileOwner struct {
	movieID   int64
	episodeID int64
	label     string // "Title (Year)" or "Show S01E02"
}

// trackedFileOwners maps every file the library has down for a movie or
// episode (cleaned path) to who it belongs to. Read it once per import run.
func (s *Server) trackedFileOwners() map[string][]fileOwner {
	out := map[string][]fileOwner{}
	if movies, err := s.MovieRepo.ListWithCopies(); err == nil {
		for _, m := range movies {
			if m.FilePath == "" {
				continue
			}
			label := m.Title
			if m.Year > 0 {
				label = fmt.Sprintf("%s (%d)", m.Title, m.Year)
			}
			p := filepath.Clean(m.FilePath)
			out[p] = append(out[p], fileOwner{movieID: m.ID, label: label})
		}
	}
	if shows, err := s.MovieRepo.ListSeriesWithCopies(); err == nil {
		for _, sr := range shows {
			eps, err := s.MovieRepo.ListEpisodes(sr.ID)
			if err != nil {
				continue
			}
			for _, ep := range eps {
				if ep.FilePath == "" {
					continue
				}
				p := filepath.Clean(ep.FilePath)
				out[p] = append(out[p], fileOwner{episodeID: ep.ID, label: fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.Season, ep.Episode)})
			}
		}
	}
	return out
}

// otherFileOwner names the movie or episode, other than movieID or
// episodeIDs (what the import is for), whose file is dest; "" when there is
// none.
func otherFileOwner(owners map[string][]fileOwner, dest string, movieID int64, episodeIDs []int64) string {
	for _, o := range owners[filepath.Clean(dest)] {
		if o.movieID != 0 && o.movieID == movieID {
			continue
		}
		mine := false
		for _, id := range episodeIDs {
			if o.episodeID != 0 && o.episodeID == id {
				mine = true
				break
			}
		}
		if !mine {
			return o.label
		}
	}
	return ""
}

// errFileOfAnother is the failure of an import whose destination is another
// title's file.
func errFileOfAnother(owner, dest string) error {
	return fmt.Errorf("%s already has its file at %s, so this download wasn't put in its place. The two titles get the same file name: add the TMDB id or the edition to the naming format in Settings > Library and files > File names and importing", owner, dest)
}
