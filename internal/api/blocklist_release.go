package api

import (
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/queue"
)

// The same release name is sometimes uploaded more than once (another size, a
// missing password, an old bad upload). A release that fails is blocklisted
// together with which upload it was (blocklist.ReleaseID), so the others with
// that name are still offered. When the indexer gave the upload a guid, the
// entry is for that guid alone, so even a copy of the same size from the same
// indexer is offered. Entries made before the guid was kept (by size or info
// hash), and those without any id, keep blocking as they did.

// firstID is the id a blocklist entry is saved under: the most exact one. An
// entry saved under a guid also keeps the size-based id of the same upload, so
// a later copy that comes without a guid is still recognised (blocklist.PackIDs).
func firstID(ids []string) string {
	return blocklist.PackIDs(ids)
}

// resultReleaseIDs are the ids a search result is known by.
func resultReleaseIDs(r indexers.Result) []string {
	return blocklist.ReleaseIDs(r.IndexerID, r.GUID, r.SizeBytes, r.DownloadURL)
}

// resultReleaseID is the id a search result is saved under on the blocklist.
func resultReleaseID(r indexers.Result) string {
	return firstID(resultReleaseIDs(r))
}

// releaseBlocked reports whether a search result is on the blocklist for a
// search for sc: by its name alone, or as the upload that failed.
func releaseBlocked(set map[string]bool, r indexers.Result, sc blocklist.Scope) bool {
	return blocklist.BlockedAny(set, r.Title, resultReleaseIDs(r), sc)
}

// downloadReleaseIDs are the ids of the upload at a download link, for a grab:
// the indexer and the guid are the ones that gave the link.
func (s *Server) downloadReleaseIDs(downloadURL string, sizeBytes int64) []string {
	id, _ := s.indexerOfRelease(downloadURL)
	var guid string
	if s.IndexerRepo != nil {
		guid = s.IndexerRepo.Tracker().GUIDOf(downloadURL)
	}
	return blocklist.ReleaseIDs(id, guid, sizeBytes, downloadURL)
}

// itemReleaseIDs are the ids of the upload a download is.
func (s *Server) itemReleaseIDs(item queue.Item) []string {
	id, ok := item.IndexerID.Int64, item.IndexerID.Valid
	if !ok {
		id, _ = s.indexerOfRelease(item.NZBURL)
	}
	return blocklist.ReleaseIDs(id, item.ReleaseGUID, item.SizeBytes, item.NZBURL)
}

// itemReleaseID is the id a download is saved under on the blocklist.
func (s *Server) itemReleaseID(item queue.Item) string {
	return firstID(s.itemReleaseIDs(item))
}

// queueReleaseID is itemReleaseID for a download by its number ("" when the
// download is gone: the entry then blocks every upload with that name).
func (s *Server) queueReleaseID(queueID int64) string {
	if queueID == 0 || s.QueueRepo == nil {
		return ""
	}
	item, err := s.QueueRepo.Get(queueID)
	if err != nil {
		return ""
	}
	return s.itemReleaseID(item)
}

// sameNameGap is how much later one indexer's copy of a release must be posted
// than another indexer's before it counts as another upload: a release shows
// up on several indexers some minutes or an hour apart.
const sameNameGap = 2 * time.Hour

// newerSameName tells which of two results with the same release name was
// posted later, or nil when they are not a repost of each other (different
// names, a date that is not known, or on two indexers too close in time to be
// anything but the same upload).
func newerSameName(a, b *indexers.Result) *indexers.Result {
	if a == nil || b == nil || a.PublishDate.IsZero() || b.PublishDate.IsZero() || blocklist.Key(a.Title) != blocklist.Key(b.Title) {
		return nil
	}
	gap := b.PublishDate.Sub(a.PublishDate)
	min := time.Duration(0)
	if a.IndexerName != b.IndexerName {
		min = sameNameGap
	}
	switch {
	case gap > min:
		return b
	case -gap > min:
		return a
	}
	return nil
}
