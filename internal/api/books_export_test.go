package api

import (
	"context"

	"github.com/rdborg/mediarium/internal/books"
)

// TestSetOpenLibrary points the Open Library client at a fake server.
func (s *Server) TestSetOpenLibrary(baseURL string) {
	s.OpenLibrary = &books.Client{Base: baseURL, UserAgent: "Mediarium/test", HTTP: s.OpenLibrary.HTTP}
}

// TestSetAudnexus points the Audnexus client at fake Audible and Audnexus servers.
func (s *Server) TestSetAudnexus(audibleBase, audnexusBase string) {
	s.Audnexus = &books.Audnexus{AudibleBase: audibleBase, AudnexusBase: audnexusBase, Region: "us", UserAgent: "Mediarium/test", HTTP: s.OpenLibrary.HTTP}
}

// TestCheckFollowedAuthors runs the followed-authors check now.
func (s *Server) TestCheckFollowedAuthors() int { return s.checkFollowedAuthors(context.Background()) }
