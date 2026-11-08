package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/migrate"
	"github.com/rdborg/mediarium/internal/settings"
)

// A link to a running Prowlarr (V4 D16, #134, mx4k). Mediarium reads the
// definitions Prowlarr's sites are written in, but some of Prowlarr's
// indexers exist only as code in Prowlarr. With a link, every indexer Prowlarr
// has is added to Mediarium as the feed Prowlarr serves for it, and kept in
// step: a new one is added, a renamed or switched off one follows, and one
// removed from Prowlarr goes. What is set on Mediarium's side of an indexer
// (its priority, minimum seeders, where torrents go, extra categories) stays.
// The one-time import (Settings > Import from other apps) still exists for
// people who want to leave Prowlarr.

// prowlarrSyncEvery is how often a linked Prowlarr is read.
const prowlarrSyncEvery = 6 * time.Hour

var prowlarrSyncMu sync.Mutex // one sync at a time

type prowlarrResult struct {
	At      string `json:"at"` // RFC 3339
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func (s *Server) prowlarrConn() (migrate.Conn, bool) {
	url, _ := s.Settings.Get(settings.KeyProwlarrURL)
	key, _ := s.Settings.Get(settings.KeyProwlarrAPIKey)
	url, key = strings.TrimSpace(url), strings.TrimSpace(key)
	return migrate.Conn{URL: url, APIKey: key}, url != "" && key != ""
}

// prowlarrLinks is which Mediarium indexer each Prowlarr indexer is.
func (s *Server) prowlarrLinks() map[int]int64 {
	v, _ := s.Settings.Get(settings.KeyProwlarrLinks)
	out := map[int]int64{}
	var raw map[string]int64
	if json.Unmarshal([]byte(v), &raw) == nil {
		for k, id := range raw {
			if n, err := strconv.Atoi(k); err == nil {
				out[n] = id
			}
		}
	}
	return out
}

func (s *Server) saveProwlarrLinks(links map[int]int64) error {
	raw := map[string]int64{}
	for k, id := range links {
		raw[strconv.Itoa(k)] = id
	}
	b, _ := json.Marshal(raw)
	return s.Settings.Set(settings.KeyProwlarrLinks, string(b), false)
}

func normFeed(u string) string { return strings.TrimRight(strings.ToLower(strings.TrimSpace(u)), "/") }

// syncProwlarr brings Mediarium's copy of the linked Prowlarr's indexers up
// to date and says what it did in a sentence.
func (s *Server) syncProwlarr(ctx context.Context) (string, error) {
	prowlarrSyncMu.Lock()
	defer prowlarrSyncMu.Unlock()
	conn, ok := s.prowlarrConn()
	if !ok {
		return "", errors.New("Add Prowlarr's address and API key first.")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	feeds, err := migrate.ProwlarrFeeds(ctx, nil, conn)
	if err != nil {
		s.noteProwlarrResult(false, "Couldn't read Prowlarr: "+err.Error())
		return "", fmt.Errorf("couldn't read Prowlarr: %w", err)
	}
	list, err := s.IndexerRepo.List()
	if err != nil {
		return "", err
	}
	byID := map[int64]indexers.Instance{}
	byFeed := map[string]indexers.Instance{}
	for _, inst := range list {
		byID[inst.ID] = inst
		byFeed[normFeed(inst.BaseURL)] = inst
	}
	links := s.prowlarrLinks()
	seen := map[int]bool{}
	added, updated, removed := 0, 0, 0
	for _, f := range feeds {
		seen[f.ID] = true
		inst, ok := byID[links[f.ID]]
		if !ok {
			// One added by hand or by the import, with Prowlarr's address for it: take it over.
			inst, ok = byFeed[normFeed(f.FeedURL)]
		}
		if !ok {
			kind := indexers.KindNewznab
			if f.Protocol == indexers.ProtocolTorrent {
				kind = indexers.KindTorznab
			}
			created, err := s.IndexerRepo.Create(indexers.Instance{
				Name: f.Name, Kind: kind, BaseURL: f.FeedURL, APIKey: f.APIKey, Protocol: f.Protocol, Enabled: f.Enabled,
				Priority: indexers.ClampPriority(f.Priority), MinSeeders: f.MinSeeders,
			}, nil)
			if err != nil {
				slog.Warn("prowlarr: add an indexer", "indexer", f.Name, "err", err)
				continue
			}
			links[f.ID] = created.ID
			added++
			continue
		}
		links[f.ID] = inst.ID
		if inst.Name == f.Name && normFeed(inst.BaseURL) == normFeed(f.FeedURL) && inst.APIKey == f.APIKey && inst.Enabled == f.Enabled && inst.Protocol == f.Protocol {
			continue
		}
		inst.Name, inst.BaseURL, inst.APIKey, inst.Enabled, inst.Protocol = f.Name, f.FeedURL, f.APIKey, f.Enabled, f.Protocol
		if err := s.IndexerRepo.Update(inst, nil); err != nil {
			slog.Warn("prowlarr: update an indexer", "indexer", f.Name, "err", err)
			continue
		}
		updated++
	}
	for pid, id := range links {
		if seen[pid] {
			continue
		}
		delete(links, pid)
		if _, ok := byID[id]; !ok {
			continue // removed in Mediarium already
		}
		if err := s.IndexerRepo.Delete(id); err != nil {
			slog.Warn("prowlarr: remove an indexer", "id", id, "err", err)
			continue
		}
		removed++
	}
	if err := s.saveProwlarrLinks(links); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("Prowlarr has %s: %d added, %d changed, %d removed.", plural(len(feeds), "indexer"), added, updated, removed)
	s.noteProwlarrResult(true, msg)
	return msg, nil
}

func (s *Server) noteProwlarrResult(ok bool, msg string) {
	b, _ := json.Marshal(prowlarrResult{At: time.Now().UTC().Format(time.RFC3339), OK: ok, Message: msg})
	_ = s.Settings.Set(settings.KeyProwlarrResult, string(b), false)
}

// prowlarrJob keeps a linked Prowlarr in step, when that is switched on.
func (s *Server) prowlarrJob(ctx context.Context) {
	if on, _ := s.Settings.GetBool(settings.KeyProwlarrSync); !on {
		return
	}
	if _, ok := s.prowlarrConn(); !ok {
		return
	}
	if _, err := s.syncProwlarr(ctx); err != nil {
		slog.Warn("prowlarr: keep the indexers in step", "err", err)
	}
}

// ---- Settings > Indexers & Search > Prowlarr

type prowlarrState struct {
	URL    string          `json:"url"`
	HasKey bool            `json:"hasKey"`
	Sync   bool            `json:"sync"`
	Linked int             `json:"linked"` // indexers that came from Prowlarr
	Last   *prowlarrResult `json:"last,omitempty"`
}

func (s *Server) prowlarrStateNow() prowlarrState {
	conn, _ := s.prowlarrConn()
	sync, _ := s.Settings.GetBool(settings.KeyProwlarrSync)
	st := prowlarrState{URL: conn.URL, HasKey: conn.APIKey != "", Sync: sync, Linked: len(s.prowlarrLinks())}
	if v, _ := s.Settings.Get(settings.KeyProwlarrResult); v != "" {
		var r prowlarrResult
		if json.Unmarshal([]byte(v), &r) == nil {
			st.Last = &r
		}
	}
	return st
}

// handleGetProwlarr says whether a Prowlarr is linked and what the last sync did.
func (s *Server) handleGetProwlarr(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.prowlarrStateNow())
}

// handlePutProwlarr saves the link to a running Prowlarr (its address and API
// key, checked first) and the switch that keeps it in step; it syncs at once.
// Body: {"url", "apiKey", "sync", "remove"}. An empty key keeps the saved one;
// remove takes the link away and leaves the indexers as they are.
func (s *Server) handlePutProwlarr(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"apiKey"`
		Sync   *bool  `json:"sync"`
		Remove bool   `json:"remove"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.Remove {
		for _, k := range []string{settings.KeyProwlarrURL, settings.KeyProwlarrLinks, settings.KeyProwlarrResult, settings.KeyProwlarrSync} {
			_ = s.Settings.Set(k, "", false)
		}
		_ = s.Settings.Set(settings.KeyProwlarrAPIKey, "", true)
		writeJSON(w, http.StatusOK, s.prowlarrStateNow())
		return
	}
	url, key := strings.TrimSpace(req.URL), strings.TrimSpace(req.APIKey)
	if url == "" {
		writeError(w, http.StatusBadRequest, "Give Prowlarr's address, for example http://192.168.1.10:9696.")
		return
	}
	if key == "" {
		conn, _ := s.prowlarrConn()
		key = conn.APIKey
	}
	if key == "" || len(key) > 200 || strings.ContainsAny(key, " \r\n\t") {
		writeError(w, http.StatusBadRequest, "Add Prowlarr's API key. It is under Settings > General in Prowlarr.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if _, err := migrate.ProwlarrFeeds(ctx, nil, migrate.Conn{URL: url, APIKey: key}); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't reach Prowlarr with that address and key: "+err.Error())
		return
	}
	if s.Settings.Set(settings.KeyProwlarrURL, url, false) != nil || s.Settings.Set(settings.KeyProwlarrAPIKey, key, true) != nil {
		writeError(w, http.StatusInternalServerError, "The link couldn't be saved.")
		return
	}
	if req.Sync != nil {
		v := "0"
		if *req.Sync {
			v = "1"
		}
		_ = s.Settings.Set(settings.KeyProwlarrSync, v, false)
	}
	if _, err := s.syncProwlarr(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.prowlarrStateNow())
}

// handleSyncProwlarr reads the linked Prowlarr now.
func (s *Server) handleSyncProwlarr(w http.ResponseWriter, r *http.Request) {
	if _, err := s.syncProwlarr(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.prowlarrStateNow())
}
