package api

import (
	"context"
	"time"

	"github.com/rdborg/mediarium/internal/announce"
	"github.com/rdborg/mediarium/internal/importlists"
	"github.com/rdborg/mediarium/internal/indexers"
)

// RSS feed lists (#160, mirisbowring). The titles a feed names are added like
// those of any list. With "Also download from this feed" on, an entry that has
// a download link is also offered to the same check an IRC announce goes
// through (irc_announce_decide.go): grabbed when it is wanted for a title in
// the library, missing or as a better version than the file it has, under the
// title's quality profile and the hourly limit on automatic downloads.

// maxFeedGrabChecks is how many entries of one read are looked at for a grab.
const maxFeedGrabChecks = 200

// readFeedEntries fetches and reads the feed of a list.
func (s *Server) readFeedEntries(ctx context.Context, cfg listConfig) ([]importlists.FeedEntry, error) {
	addr, err := importlists.FeedAddress(cfg.URL)
	if err != nil {
		return nil, err
	}
	body, err := importlists.DownloadFeed(ctx, addr)
	if err != nil {
		return nil, err
	}
	return importlists.ParseFeed(body)
}

// feedListItems are the titles the feed names, of the kinds the list is for.
func feedListItems(entries []importlists.FeedEntry, media string) []importlists.Item {
	wanted := wantedKinds(media)
	var out []importlists.Item
	for _, it := range importlists.FeedItems(entries) {
		if wanted[it.Kind] {
			out = append(out, it)
		}
	}
	return out
}

// grabFromFeed offers the feed's releases to the automatic download check. It
// returns how many were sent to the download line.
func (s *Server) grabFromFeed(ctx context.Context, l importList, entries []importlists.FeedEntry) int {
	d := ircDecider{s}
	grabbed, checked := 0, 0
	for _, e := range entries {
		if ctx.Err() != nil || checked >= maxFeedGrabChecks {
			break
		}
		if e.Link == "" {
			continue
		}
		checked++
		res := indexers.Result{Title: e.Title, SizeBytes: e.Size, Protocol: indexers.ProtocolUsenet, DownloadURL: e.Link, IndexerName: l.Name, PublishDate: time.Now()}
		if e.Torrent {
			res.Protocol, res.Torrent = indexers.ProtocolTorrent, true
		}
		link := e.Link
		dec := d.decideRelease(ctx, res, announce.DecideOptions{URL: func() (string, error) { return link, nil }})
		if dec.Action == announce.DecisionGrabbed {
			grabbed++
		}
	}
	return grabbed
}
