package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/rdborg/mediarium/internal/listenbrainz"
	"github.com/rdborg/mediarium/internal/musicbrainz"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/settings"
)

// musicState is the music module's in-memory state.
type musicState struct {
	mu         sync.Mutex
	huntOffset int        // where the next scheduled album search starts (see huntMusic)
	covers     coverLocks // one cover download per album at a time (music_cover.go)
	scans      musicScans // existing-library scans (music_import.go)

	lbMu sync.Mutex
	lb   *listenbrainz.Client // Discover lists (music_discover.go); created on first use
}

// MusicBrainz returns the MusicBrainz client. There is one per server, so
// every caller shares its one-request-per-second limit and its cache.
func (s *Server) MusicBrainz() *musicbrainz.Client {
	s.mbMu.RLock()
	defer s.mbMu.RUnlock()
	return s.mb
}

// describeAlbum names an album for the queue and dashboard: "Artist – Album",
// its year (or type for an EP or single) and its cover.
func (s *Server) describeAlbum(albumID int64) (title, subtitle, cover string, ok bool) {
	album, err := s.MusicRepo.GetAlbum(albumID)
	if err != nil {
		return "", "", "", false
	}
	artist, err := s.MusicRepo.GetArtist(album.ArtistID)
	if err != nil {
		return "", "", "", false
	}
	subtitle = "Album"
	switch album.Type {
	case "ep":
		subtitle = "EP"
	case "single":
		subtitle = "Single"
	}
	if y := album.Year(); y > 0 {
		subtitle = fmt.Sprintf("%s · %d", subtitle, y)
	}
	return artist.Name + " – " + album.Title, subtitle, albumCoverURL(album.ID), true
}

// musicSanitizer cleans one path component with the configured illegal
// character handling, the same as movie and TV names.
func (s *Server) musicSanitizer() func(string) string {
	mode, replacement := s.illegalCharSettings()
	return func(v string) string { return organizer.Sanitize(v, mode, replacement) }
}

// The music module (artists, albums and tracks) is compiled in but off until
// an administrator switches it on (modules.go). While it is off every
// /api/music/ route answers 404, and automation never searches for albums.

// musicOffMessage is the 404 body of a music route while the module is off.
const musicOffMessage = "The music module is switched off. An administrator can switch it on in Settings."

// musicRoot is where the music library lives: the Settings value, falling
// back to the MUSIC_DIR container default (/music).
func (s *Server) musicRoot() string {
	if v, _ := s.Settings.Get(settings.KeyMusicPath); v != "" {
		return v
	}
	return s.cfg.MusicDir
}

// musicRouteOpen is the route table's gate: every /api/music/ request is
// answered with 404 while the music module is switched off. It runs after
// the role check (a member still gets 403 on an administrator route) and
// covers every music route, so a new one can never be reached with the
// module off through a forgotten check.
func (s *Server) musicRouteOpen(w http.ResponseWriter, r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/music/") && !s.musicEnabled() {
		writeError(w, http.StatusNotFound, musicOffMessage)
		return false
	}
	return true
}
