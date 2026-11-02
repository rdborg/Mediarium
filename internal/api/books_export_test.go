package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

// TestSetOpenLibrary points the Open Library client at a fake server.
func (s *Server) TestSetOpenLibrary(baseURL string) {
	s.OpenLibrary = &books.Client{Base: baseURL, UserAgent: "Mediarium/test", HTTP: s.OpenLibrary.HTTP}
}

// TestDropHardcoverCover removes one cached Hardcover cover and forgets that
// the id was seen, as if the cache had trimmed it and the search had expired.
func (s *Server) TestDropHardcoverCover(id int) {
	name := fmt.Sprintf("hc-%d", id)
	_ = os.Remove(filepath.Join(s.bookCoverDir(), name+".img"))
	_ = os.Remove(filepath.Join(s.bookCoverDir(), name+".none"))
	hardcoverCoverMem.Lock()
	delete(hardcoverCoverMem.seen, id)
	delete(hardcoverCoverMem.fail, id)
	hardcoverCoverMem.Unlock()
}

// TestSetBookWait sets how long a book release search may take.
func (s *Server) TestSetBookWait(d time.Duration) { s.bookWait = d }

// TestSetAudnexus points the Audnexus client at fake Audible and Audnexus servers.
func (s *Server) TestSetAudnexus(audibleBase, audnexusBase string) {
	s.Audnexus = &books.Audnexus{AudibleBase: audibleBase, AudnexusBase: audnexusBase, Region: "us", UserAgent: "Mediarium/test", HTTP: s.OpenLibrary.HTTP}
}

// TestCheckFollowedAuthors runs the followed-authors check now.
func (s *Server) TestCheckFollowedAuthors() int { return s.checkFollowedAuthors(context.Background()) }
