package api

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/qbit"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// Stopping or removing a download that Mediarium handed to an external client
// takes the torrent out of that client too, with what it had downloaded, so the
// person doesn't have to clean up in two places. Only a torrent that Mediarium
// itself added (it carries the mediarium-q<ID> tag) and that is not finished
// is touched. A torrent that finished is left to seed, the way "Remove from the
// client after import" decides, unless the release is being thrown away
// (Blocklist & search again). Nothing here can fail a stop or a removal: if the
// client is down or has lost the torrent, the entry goes anyway.

// removeOnStopOn is the "when I stop or remove a download, remove it from the
// client too" setting. On unless it was switched off.
func (s *Server) removeOnStopOn() bool {
	v, _ := s.Settings.Get(settings.KeyDownloadClientRemoveOnStop)
	return v != "0"
}

// clientRef is what is needed to find a download's torrent in the client. It
// is read before the download's folder is deleted, since the hash lives there.
type clientRef struct {
	queueID int64
	hash    string // "" when it was never recorded: the tag finds it
	// usenetJob is the job's id in an external Usenet downloader; set instead of
	// hash for a Usenet download.
	usenetJob string
}

// clientRefFor says whether stopping or removing this item should also remove
// its torrent from the download client, and which torrent. Call it before the
// item's folder is deleted.
func (s *Server) clientRefFor(item queue.Item) (clientRef, bool) {
	if item.Protocol != queue.ProtocolTorrent {
		return s.usenetRefFor(item)
	}
	if !s.removeOnStopOn() || !s.downloadClientOn() || s.itemTorrentRoute(item) != indexers.RouteClient {
		return clientRef{}, false
	}
	switch item.Status {
	case queue.StatusQueued, queue.StatusDownloading, queue.StatusPaused, queue.StatusStopped, queue.StatusFailed:
	default:
		return clientRef{}, false // imported or being imported: the client's torrent is not Mediarium's to remove
	}
	hash, keep := readClientHash(filepath.Join(s.workDirFor(item.ID), clientHashFile))
	if keep {
		return clientRef{}, false // it was in the client before Mediarium sent it
	}
	return clientRef{queueID: item.ID, hash: hash}, true
}

// removeFromClient removes the torrent behind ref from the client without
// making the caller wait: the client may be slow or down. force also removes a
// torrent that has finished downloading.
func (s *Server) removeFromClient(ref clientRef, ok, force bool) {
	if !ok {
		return
	}
	s.background(func() {
		if ref.usenetJob != "" {
			s.removeUsenetJob(ref, force)
			return
		}
		s.removeClientTorrent(ref, force)
	})
}

// removeClientTorrent finds the torrent by its hash or, when that was never
// recorded, by its tag, checks that it carries Mediarium's tag for this
// download, and removes it with its files. Anything unexpected is left alone.
func (s *Server) removeClientTorrent(ref clientRef, force bool) {
	cfg := s.downloadClientConfig()
	if !qbit.ValidKind(cfg.Kind) || cfg.URL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := newDownloadClient(cfg.Kind, cfg.URL, cfg.Username, cfg.Password)
	tag := clientTag(ref.queueID)
	var t qbit.Torrent
	if ref.hash != "" {
		found, ok, err := c.Info(ctx, ref.hash)
		if err != nil {
			slog.Warn("download client: couldn't look up a torrent to remove", "queueId", ref.queueID, "err", err)
			return
		}
		if !ok {
			slog.Debug("download client: the torrent to remove is already gone", "queueId", ref.queueID)
			return
		}
		t = found
	} else {
		list, err := c.InfoByTag(ctx, tag)
		if err != nil {
			slog.Warn("download client: couldn't look up a torrent to remove", "queueId", ref.queueID, "err", err)
			return
		}
		if len(list) == 0 {
			return
		}
		t = list[0]
	}
	if !t.HasTag(tag) {
		slog.Debug("download client: not removing a torrent that isn't tagged as this download's", "queueId", ref.queueID)
		return
	}
	if !force && t.Progress >= 1 {
		slog.Debug("download client: leaving a finished torrent in the client", "queueId", ref.queueID)
		return
	}
	if err := c.Delete(ctx, t.Hash, true); err != nil {
		slog.Warn("download client: couldn't remove a stopped download's torrent", "queueId", ref.queueID, "err", err)
		return
	}
	slog.Info("download client: removed the torrent of a stopped download", "queueId", ref.queueID)
}
