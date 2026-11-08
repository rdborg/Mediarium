package api

import (
	"net/http"
	"path/filepath"

	"github.com/rdborg/mediarium/internal/imgproxy"
)

// artworkProxyFor is the picture proxy of this server, made on first use. Its
// pictures are kept in the img-cache folder inside the config folder. A test
// can set Artwork first to point it at its own picture host.
func (s *Server) artworkProxyFor() *imgproxy.Proxy {
	s.artworkOnce.Do(func() {
		if s.Artwork == nil {
			s.Artwork = imgproxy.New(filepath.Join(s.cfg.ConfigDir, "img-cache"))
		}
	})
	return s.Artwork
}

// handleArtwork serves a poster, backdrop or picture of a person from a known
// picture host, at one of a fixed set of sizes and in the best format the
// browser accepts. Anything else is a plain 404.
func (s *Server) handleArtwork(w http.ResponseWriter, r *http.Request) {
	s.artworkProxyFor().Handle(w, r, r.PathValue("kind"), r.PathValue("size"), r.PathValue("file"))
}
