package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/settings"
)

// When a release's NZB link answers with a login or maintenance page (or a
// Newznab error) instead of the file, that says nothing about the release, so
// it is not blocklisted. But the indexer will most likely send the same page
// again in a few minutes, and the next automatic search would grab the same
// release, fail the same way and send the same notifications, over and over.
// So the release is held back from searches for a while, per indexer, and the
// "failed" notification goes out once, not on every repeat.
const (
	// nzbLinkHoldFor is how long searches leave such a release alone.
	nzbLinkHoldFor = 6 * time.Hour
	// nzbLinkQuietFor is how long after a "failed" notification for such a
	// release no other one is sent for it.
	nzbLinkQuietFor = 24 * time.Hour
)

// nzbLinkError is what notAnNZB returns: the indexer's answer to the NZB link
// was not an NZB file.
type nzbLinkError struct{ msg string }

func (e *nzbLinkError) Error() string { return e.msg }

func isNZBLinkError(err error) bool {
	var l *nzbLinkError
	return errors.As(err, &l)
}

// nzbLinkHold is one held-back release.
type nzbLinkHold struct {
	Until    time.Time `json:"until"`
	Notified time.Time `json:"notified,omitzero"`
}

// nzbLinkHoldsMu guards the read-change-write of the stored holds.
var nzbLinkHoldsMu sync.Mutex

// nzbLinkHoldPrefix keeps the hold keys apart from blocklist keys when both
// are in the same lookup set (see searchSkipKeys).
const nzbLinkHoldPrefix = "nzb-link:"

// nzbLinkHoldKey names a release on one indexer: the host of its link and the
// release name the way the blocklist compares it. The rest of the link is left
// out, since it carries the API key.
func nzbLinkHoldKey(downloadURL, releaseTitle string) string {
	host := ""
	if u, err := url.Parse(downloadURL); err == nil {
		host = strings.ToLower(u.Host)
	}
	return nzbLinkHoldPrefix + host + "|" + blocklist.Key(releaseTitle)
}

func (s *Server) loadNZBLinkHolds() map[string]nzbLinkHold {
	holds := map[string]nzbLinkHold{}
	raw, err := s.Settings.Get(settings.KeyNZBLinkHolds)
	if err != nil || raw == "" {
		return holds
	}
	if err := json.Unmarshal([]byte(raw), &holds); err != nil {
		slog.Warn("api: read the held-back releases", "err", err)
		return map[string]nzbLinkHold{}
	}
	return holds
}

// activeNZBLinkHolds is the set of keys of releases held back right now.
func (s *Server) activeNZBLinkHolds(now time.Time) map[string]bool {
	nzbLinkHoldsMu.Lock()
	defer nzbLinkHoldsMu.Unlock()
	out := map[string]bool{}
	for k, h := range s.loadNZBLinkHolds() {
		if now.Before(h.Until) {
			out[k] = true
		}
	}
	return out
}

// nzbLinkHeld reports whether searches leave this release alone right now.
func (s *Server) nzbLinkHeld(downloadURL, releaseTitle string) bool {
	return s.activeNZBLinkHolds(time.Now())[nzbLinkHoldKey(downloadURL, releaseTitle)]
}

// holdNZBLink holds the release back after its link sent something that is
// not an NZB, and reports whether a "failed" notification should go out: only
// when none went out for it in the last nzbLinkQuietFor.
func (s *Server) holdNZBLink(downloadURL, releaseTitle string, now time.Time) (notify bool) {
	nzbLinkHoldsMu.Lock()
	defer nzbLinkHoldsMu.Unlock()
	holds := s.loadNZBLinkHolds()
	for k, h := range holds { // what has run out is forgotten
		if !now.Before(h.Until) && !now.Before(h.Notified.Add(nzbLinkQuietFor)) {
			delete(holds, k)
		}
	}
	key := nzbLinkHoldKey(downloadURL, releaseTitle)
	h := holds[key]
	h.Until = now.Add(nzbLinkHoldFor)
	notify = h.Notified.IsZero() || !now.Before(h.Notified.Add(nzbLinkQuietFor))
	if notify {
		h.Notified = now
	}
	holds[key] = h
	b, err := json.Marshal(holds)
	if err == nil {
		err = s.Settings.Set(settings.KeyNZBLinkHolds, string(b), false)
	}
	if err != nil {
		slog.Warn("api: save the held-back releases", "err", err)
	}
	return notify
}

// failureNotifyWanted is called by every pipeline when a download fails. For
// a link that sent something that is not an NZB it holds the release back
// and says whether this failure is worth a notification; for anything else
// the answer is always yes.
func (s *Server) failureNotifyWanted(stepErr error, downloadURL, releaseTitle string) bool {
	if !isNZBLinkError(stepErr) {
		return true
	}
	return s.holdNZBLink(downloadURL, releaseTitle, time.Now())
}

// searchSkipKeys is the blocklist plus the releases held back for now: what
// the searches Mediarium makes on its own leave out. A release a person picks
// is never held back.
func (s *Server) searchSkipKeys() map[string]bool {
	keys := s.blockedKeys()
	for k := range s.activeNZBLinkHolds(time.Now()) {
		keys[k] = true
	}
	return keys
}

// searchSkips reports whether a search result is in a set made by searchSkipKeys
// (or blockedKeys) for a search for the title sc (zero: for no one title).
func searchSkips(r indexers.Result, skip map[string]bool, sc blocklist.Scope) bool {
	return releaseBlocked(skip, r, sc) || skip[nzbLinkHoldKey(r.DownloadURL, r.Title)]
}

// nzbLinkHeldNote is said on the Activity row of a download that failed this
// way while its release is held back.
const nzbLinkHeldNote = "Automatic searches leave this release alone for a few hours, in case the indexer is still sending that page."
