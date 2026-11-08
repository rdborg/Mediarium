package api

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/queue"
)

// pauseClientTorrent pauses the torrent of a download that was paused here, so
// the download client doesn't carry on without Mediarium. It is started again
// when the download is resumed (downloadViaClient). Pausing only ended
// Mediarium's own watching before; the torrent kept downloading in the client.
func (s *Server) pauseClientTorrent(item queue.Item) {
	if item.Protocol != queue.ProtocolTorrent || !s.downloadClientOn() || s.itemTorrentRoute(item) != indexers.RouteClient {
		return
	}
	hash := ""
	if dir, ok := s.existingWorkDir(item.ID); ok {
		hash, _ = readClientHash(filepath.Join(dir, clientHashFile))
	}
	cfg := s.downloadClientConfig()
	s.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c := newDownloadClient(cfg.Kind, cfg.URL, cfg.Username, cfg.Password)
		if hash == "" {
			// Added before its hash was known: the tag finds it.
			list, err := c.InfoByTag(ctx, clientTag(item.ID))
			if err != nil || len(list) == 0 {
				return
			}
			hash = list[0].Hash
		}
		if err := c.Pause(ctx, hash); err != nil {
			slog.Warn("download client: couldn't pause a torrent", "queueId", item.ID, "err", err)
		}
	})
}
