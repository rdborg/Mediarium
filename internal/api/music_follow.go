package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/rdborg/mediarium/internal/music"
)

// musicRefreshInterval is how often followed artists are checked for new
// releases.
const musicRefreshInterval = 12 * time.Hour

type updateArtistRequest struct {
	Monitored *bool  `json:"monitored"`
	ProfileID *int64 `json:"profileId"` // 0 = the default profile
}

// handleUpdateArtist changes an artist (administrators): whether it is
// followed and which quality profile it uses. It answers the artist with its
// albums, like GET /api/music/artists/{id}.
//
// Following (monitored) only decides whether releases found later are picked
// up and monitored (see refreshMusicArtists). It does not change the
// monitored flag of any album already listed: those keep deciding, one by
// one, what is searched for. profileId 0 puts the artist back on the
// default profile.
func (s *Server) handleUpdateArtist(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "artist")
	if !ok {
		return
	}
	var req updateArtistRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if _, err := s.MusicRepo.GetArtist(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That artist isn't in your library.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.ProfileID != nil {
		if err := s.checkMusicProfileChoice(*req.ProfileID); err != nil {
			writeMusicProfileError(w, err)
			return
		}
	}
	if req.Monitored != nil {
		if err := s.MusicRepo.SetArtistFollow(id, *req.Monitored); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.ProfileID != nil {
		if err := s.MusicRepo.SetArtistProfile(id, *req.ProfileID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.handleGetArtist(w, r)
}

// refreshMusicArtists looks up the releases of every followed artist again
// on MusicBrainz: an album that is new is added (monitored, when the artist
// was added to be followed) and an announced album that now has a release
// date gets it. It runs on a schedule while the music module is on; the
// MusicBrainz rate limit spaces the requests out.
func (s *Server) refreshMusicArtists(ctx context.Context) {
	if !s.musicEnabled() {
		return
	}
	artists, err := s.MusicRepo.ListArtists()
	if err != nil {
		slog.Warn("music: refresh: list artists", "err", err)
		return
	}
	for _, sum := range artists {
		if ctx.Err() != nil {
			return
		}
		if !sum.Artist.Monitored {
			continue
		}
		if _, err := s.refreshMusicArtist(ctx, sum.Artist); err != nil {
			slog.Warn("music: refresh artist", "artist", sum.Name, "err", err)
		}
	}
}

// refreshMusicArtist is refreshMusicArtists for one artist; it returns the
// albums it added.
func (s *Server) refreshMusicArtist(ctx context.Context, artist music.Artist) ([]music.Album, error) {
	groups, err := s.MusicBrainz().ArtistReleaseGroups(ctx, artist.MBID, nil, false)
	if err != nil {
		return nil, err
	}
	have, err := s.MusicRepo.ListAlbums(artist.ID)
	if err != nil {
		return nil, err
	}
	known := map[string]music.Album{}
	for _, a := range have {
		known[a.MBID] = a
	}
	var fresh []music.Album
	for _, g := range groups {
		if a, ok := known[g.ID]; ok {
			if g.FirstReleaseDate != "" && g.FirstReleaseDate != a.ReleaseDate {
				if err := s.MusicRepo.SetAlbumReleaseDate(a.ID, g.FirstReleaseDate); err != nil {
					slog.Warn("music: refresh: release date", "album", a.Title, "err", err)
				}
			}
			continue
		}
		fresh = append(fresh, music.Album{
			MBID: g.ID, Title: g.Title, Type: albumType(g.PrimaryType), ReleaseDate: g.FirstReleaseDate, Monitored: artist.MonitorNew,
		})
	}
	added, err := s.MusicRepo.AddAlbums(artist.ID, fresh)
	if err != nil {
		return added, err
	}
	if len(added) > 0 {
		_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("%s: %s found", artist.Name, plural(len(added), "new release")))
	}
	return added, nil
}
