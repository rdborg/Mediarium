package api

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/httpsec"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// The SABnzbd and qBittorrent look-alike addresses (compat_sabnzbd.go and
// compat_qbittorrent.go). Phone apps such as nzb360 can be pointed at
// <mediarium>/sabnzbd and <mediarium>/qbittorrent to show what the built-in
// downloaders are doing: speed, time left, the list, pause, resume, remove,
// and adding a release. This file holds what both share: the on/off switch,
// the sign-in with an administrator's API key, the speed meter and the
// matching of an added release to a title in the library.
//
// Both are off until an administrator switches them on, and both sign in with
// an API key only, never with the web page's cookie.

const (
	// dlapiFailAttempts wrong keys from one address in dlapiFailWindow lock that
	// address out of both addresses for the rest of the window. The counters are
	// kept apart from the web sign-in's so a stranger guessing at the phone-app
	// addresses can never lock the owner out of the web page.
	dlapiFailAttempts = 20
	dlapiFailWindow   = 15 * time.Minute

	// dlapiSessionFor is how long an idle qBittorrent-style sign-in (the SID
	// cookie) lasts, like the real program's default.
	dlapiSessionFor = time.Hour
	dlapiMaxSession = 64
	// dlapiSampleGap is the longest pause between two looks at a download for
	// the speed to still be worked out from them.
	dlapiSampleGap = 20 * time.Second
	// dlapiMaxAddBody is the most an add request may send.
	dlapiMaxAddBody = maxManualNZB + (1 << 20)
)

// dlapiState is what the two addresses keep in memory.
type dlapiState struct {
	once   sync.Once
	failIP *auth.LoginLimiter

	mu        sync.Mutex
	samples   map[int64]*dlSample
	lastPrune time.Time
	sessions  map[string]dlSession // qBittorrent-style sign-ins by SID
}

// dlSample is the last look at one download, for working out its speed.
type dlSample struct {
	at       time.Time
	down, up int64
	downRate float64
	upRate   float64
}

// dlSession is a qBittorrent-style sign-in. It keeps the API key, in memory
// only, so a key that is revoked stops working at once.
type dlSession struct {
	key     string
	expires time.Time
}

func (s *Server) dlapiInit() *dlapiState {
	d := &s.dlapi
	d.once.Do(func() {
		d.failIP = auth.NewLoginLimiter(dlapiFailAttempts, dlapiFailWindow)
		d.samples = map[int64]*dlSample{}
		d.sessions = map[string]dlSession{}
	})
	return d
}

// dlapiOn is the switch in Settings (off by default).
func (s *Server) dlapiOn() bool { return s.settingOn(settings.KeyDownloaderAPIEnabled, false) }

// downloaderAPIRoutes mounts both addresses on root.
func (s *Server) downloaderAPIRoutes(root *http.ServeMux) {
	root.Handle("/sabnzbd/", s.busyGuard(noStore(http.HandlerFunc(s.handleSABnzbd))))
	root.Handle("/qbittorrent/", s.busyGuard(noStore(http.HandlerFunc(s.handleQBittorrent))))
}

// dlapiNotFound is the answer while the feature is off, or for an address
// nothing is served at.
func dlapiNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
}

// --- the switch in Settings ---

type downloaderAPIPayload struct {
	Enabled bool `json:"enabled"`
}

// handleGetDownloaderAPI says whether the SABnzbd and qBittorrent look-alike addresses are on.
func (s *Server) handleGetDownloaderAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, downloaderAPIPayload{Enabled: s.dlapiOn()})
}

// handlePutDownloaderAPI switches both addresses on or off. Switching them on
// can only be done from a signed-in browser session, never with an API key: a
// leaked key must not be able to open the door it would then use.
func (s *Server) handlePutDownloaderAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if *req.Enabled && !requireSession(w, r) {
		return
	}
	if err := setBool(s.Settings, settings.KeyDownloaderAPIEnabled, *req.Enabled); err != nil {
		slog.Error("downloader api: could not save the setting", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save that. Try again.")
		return
	}
	if !*req.Enabled {
		d := s.dlapiInit()
		d.mu.Lock()
		d.sessions = map[string]dlSession{} // everyone signs in again if it is switched on later
		d.mu.Unlock()
	}
	state := "off"
	if *req.Enabled {
		state = "on"
	}
	slog.Warn("downloader api: switched "+state, "by", actorName(r), "from", s.clientIP(r))
	writeJSON(w, http.StatusOK, downloaderAPIPayload{Enabled: s.dlapiOn()})
}

// --- signing in ---

type dlAuthResult int

const (
	dlAuthOK dlAuthResult = iota
	dlAuthNone
	dlAuthWrong
	dlAuthNotAdmin
	dlAuthLocked
)

// dlapiLimitKey is the key the failure counter uses for the caller.
func (s *Server) dlapiLimitKey(r *http.Request) string {
	return httpsec.LimitKey(s.clientIP(r))
}

// dlapiLocked reports whether the caller's address has failed too often.
func (s *Server) dlapiLocked(r *http.Request) bool {
	return !s.dlapiInit().failIP.Allow(s.dlapiLimitKey(r))
}

// dlapiCheckKey signs in with a raw API key. An empty key is "none" (the app
// has not tried yet) and is not counted as a failure; a wrong key is, and the
// key itself is never logged.
func (s *Server) dlapiCheckKey(r *http.Request, key string) (*auth.User, dlAuthResult) {
	if s.dlapiLocked(r) {
		return nil, dlAuthLocked
	}
	if key == "" {
		return nil, dlAuthNone
	}
	user, err := s.Auth.UserForAPIKey(key)
	if err != nil {
		s.dlapiInit().failIP.RecordFailure(s.dlapiLimitKey(r))
		slog.Info("downloader api: a wrong API key was refused", "from", s.clientIP(r), "path", r.URL.Path)
		return nil, dlAuthWrong
	}
	if !user.IsAdmin {
		return nil, dlAuthNotAdmin
	}
	return user, dlAuthOK
}

// --- the speed meter ---

// dlRate works out the speed of a download from how far it had come the last
// time somebody looked, smoothed so the number does not jump. The first look
// at a download, and one after a long gap, give 0. Speeds come from the apps
// asking, so there is no timer running when nobody is connected.
func (s *Server) dlRate(id int64, down, up int64, now time.Time) (downRate, upRate int64) {
	d := s.dlapiInit()
	d.mu.Lock()
	defer d.mu.Unlock()
	if now.Sub(d.lastPrune) > time.Minute {
		for k, v := range d.samples {
			if now.Sub(v.at) > 2*time.Minute {
				delete(d.samples, k)
			}
		}
		d.lastPrune = now
	}
	prev := d.samples[id]
	if prev == nil || now.Sub(prev.at) > dlapiSampleGap || down < prev.down || up < prev.up {
		d.samples[id] = &dlSample{at: now, down: down, up: up}
		return 0, 0
	}
	dt := now.Sub(prev.at).Seconds()
	if dt < 0.8 {
		return int64(prev.downRate), int64(prev.upRate) // asked again at once: the same answer
	}
	smooth := func(old, instant float64) float64 {
		if old == 0 {
			return instant
		}
		return 0.5*old + 0.5*instant
	}
	next := &dlSample{
		at: now, down: down, up: up,
		downRate: smooth(prev.downRate, float64(down-prev.down)/dt),
		upRate:   smooth(prev.upRate, float64(up-prev.up)/dt),
	}
	d.samples[id] = next
	return int64(next.downRate), int64(next.upRate)
}

// transferred is how many bytes a running torrent has downloaded and
// uploaded, from the built-in engine. ok is false when it has no torrent
// running or no details yet.
func (r *torrentRegistry) transferred(queueID int64) (down, up int64, ok bool) {
	r.mu.Lock()
	job := r.jobs[queueID]
	if job == nil || job.stopped || job.t == nil {
		r.mu.Unlock()
		return 0, 0, false
	}
	t := job.t
	r.mu.Unlock()
	st := t.Stats()
	return st.BytesReadData.Int64(), st.BytesWrittenData.Int64(), true
}

// dlSpeeds is the current download and upload speed of one queue item in bytes
// per second, and the bytes done so far. Only a download that is running has a
// download speed; only a torrent that is running has an upload speed.
func (s *Server) dlSpeeds(it queue.Item, now time.Time) (downRate, upRate int64) {
	running := it.Status == queue.StatusDownloading
	seeding := it.Protocol == queue.ProtocolTorrent && it.Status == queue.StatusCompleted && s.torrents.active(it.ID) && !s.torrents.seedingDone(it.ID)
	if !running && !seeding {
		return 0, 0
	}
	done := int64(it.ProgressPct / 100 * float64(it.SizeBytes))
	var up int64
	if it.Protocol == queue.ProtocolTorrent {
		if rd, wr, ok := s.torrents.transferred(it.ID); ok {
			up = wr
			if running {
				done = rd
			}
		}
	}
	if seeding {
		done = it.SizeBytes
	}
	dr, ur := s.dlRate(it.ID, done, up, now)
	if !running {
		dr = 0
	}
	return dr, ur
}

// dlEstimate is the seconds left for a download, or -1 when it cannot be told.
func dlEstimate(it queue.Item, rate int64) int64 {
	if rate <= 0 || it.SizeBytes <= 0 {
		return -1
	}
	left := it.SizeBytes - int64(it.ProgressPct/100*float64(it.SizeBytes))
	if left <= 0 {
		return 0
	}
	return left / rate
}

// --- ids the apps see ---

const sabIDPrefix = "SABnzbd_nzo_mediarium"

func sabID(queueID int64) string { return fmt.Sprintf("%s%d", sabIDPrefix, queueID) }

// sabQueueID reads an id the apps send back. ok is false when it is not one of ours.
func sabQueueID(id string) (int64, bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(id), sabIDPrefix)
	if !found || rest == "" {
		return 0, false
	}
	var n int64
	for _, c := range rest {
		if c < '0' || c > '9' || n > 1<<50 {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	return n, n > 0
}

// qbHash is the 40-letter "info hash" the apps see for a queue item. It is
// worked out from the item's number, so it stays the same however the torrent
// is added; it is not the torrent's real hash.
func qbHash(queueID int64) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("mediarium-queue-item-%d", queueID)))
	return hex.EncodeToString(sum[:])
}

// dlKind is the kind of title a download is for, as a category name ("" when
// it is for none).
func dlKind(it queue.Item) string {
	switch {
	case it.MovieID > 0:
		return "movies"
	case it.SeriesID > 0:
		return "tv"
	case it.AlbumID > 0:
		return "music"
	case it.BookID > 0:
		return "books"
	}
	return ""
}

// dlapiItems lists the queue in the order the Activity page shows it.
func (s *Server) dlapiItems() ([]queue.Item, error) {
	items, err := s.QueueRepo.List()
	if err != nil {
		return nil, fmt.Errorf("read the download list: %w", err)
	}
	sortQueueItems(items)
	return items, nil
}

// --- adding a release ---

// dlTarget is the title in the library a release belongs to.
type dlTarget struct {
	movie   library.Movie
	series  library.Series
	season  int
	episode int
	isMovie bool
}

var (
	errDLNoMatch   = errors.New("That name doesn't match a movie or show in your library. Add the title to Mediarium first.")
	errDLNoEpisode = errors.New("The name doesn't say which season or episode this is for.")
)

// matchLibraryTitle finds the movie, or the show with its season and episode,
// a release name is for. Mediarium only downloads for titles it has, so a
// release the phone app adds has to name one of them.
func (s *Server) matchLibraryTitle(releaseTitle string) (dlTarget, error) {
	rel := parser.Parse(releaseTitle)
	clean := parseClean(rel.Title)
	if clean == "" {
		return dlTarget{}, errDLNoMatch
	}
	isTV := rel.Season > 0 || rel.Episode > 0 || len(rel.Episodes) > 0
	if isTV {
		if !s.tvEnabled() {
			return dlTarget{}, errDLNoMatch
		}
		list, err := s.MovieRepo.ListSeries()
		if err != nil {
			return dlTarget{}, fmt.Errorf("read the shows: %w", err)
		}
		for _, sr := range list {
			if parseClean(sr.Title) != clean {
				continue
			}
			episode := rel.Episode
			if len(rel.Episodes) > 0 {
				episode = rel.Episodes[0]
			}
			return dlTarget{series: sr, season: rel.Season, episode: episode}, nil
		}
		return dlTarget{}, errDLNoMatch
	}
	if rel.AirDate != "" || len(rel.Absolute) > 0 {
		return dlTarget{}, errDLNoEpisode
	}
	if !s.moviesEnabled() {
		return dlTarget{}, errDLNoMatch
	}
	movies, err := s.MovieRepo.List()
	if err != nil {
		return dlTarget{}, fmt.Errorf("read the movies: %w", err)
	}
	for _, m := range movies {
		if parseClean(m.Title) == clean && (rel.Year == 0 || m.Year == 0 || m.Year == rel.Year) {
			return dlTarget{movie: m, isMovie: true}, nil
		}
	}
	return dlTarget{}, errDLNoMatch
}

// dlapiAdd takes a release file or address from a phone app, checks it is the
// kind (NZB or torrent) that address serves, matches it to a title in the
// library and puts it in the download line like a release picked by hand. It
// returns the queue item's id.
func (s *Server) dlapiAdd(ctx context.Context, want indexers.Protocol, fh *multipart.FileHeader, address, name string) (int64, error) {
	src, _, err := s.readManualSource(ctx, fh, address, name)
	if err != nil {
		return 0, err
	}
	if src.protocol != want {
		s.removeUpload(src.ref)
		if want == indexers.ProtocolUsenet {
			return 0, errors.New("That is a torrent, not an NZB. Add it in the torrent app.")
		}
		return 0, errors.New("That is an NZB, not a torrent. Add it in the Usenet app.")
	}
	target, err := s.matchLibraryTitle(src.title)
	if err != nil {
		s.removeUpload(src.ref)
		slog.Info("downloader api: a release was refused, it matches nothing in the library", "release", src.title)
		return 0, err
	}
	var id int64
	if target.isMovie {
		id, err = s.grabRelease(target.movie, src.title, src.ref, src.size, src.protocol)
	} else {
		id, err = s.grabTVRelease(target.series, target.season, target.episode, src.title, src.ref, src.size, src.protocol)
	}
	if err != nil {
		s.removeUpload(src.ref)
		return 0, err
	}
	return id, nil
}

// --- removing, pausing, resuming ---

// dlapiRemove takes a download off the list: a running one is stopped first.
func (s *Server) dlapiRemove(id int64, deleteFiles bool) error {
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		return errors.New("That download is no longer in the list.")
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	if isActiveStatus(item.Status) || item.Status == queue.StatusPaused {
		if err := s.stopItem(item, deleteFiles); err != nil && !errors.Is(err, errNotRunning) {
			// It may have finished a moment ago: then it is just removed.
			if fresh, gerr := s.QueueRepo.Get(id); gerr != nil || isActiveStatus(fresh.Status) {
				return err
			}
		}
		if fresh, gerr := s.QueueRepo.Get(id); gerr == nil {
			item = fresh
		}
	}
	if isActiveStatus(item.Status) {
		return errors.New("This download is still winding down. Try again in a moment.")
	}
	return s.removeQueueEntry(item, deleteFiles, true)
}

// dlapiPause pauses a download that is running or waiting.
func (s *Server) dlapiPause(id int64) error {
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		return errors.New("That download is no longer in the list.")
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	return s.pauseItem(item)
}

// dlapiResume puts a paused download back in the line.
func (s *Server) dlapiResume(id int64) error {
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		return errors.New("That download is no longer in the list.")
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	return s.resumeItem(item)
}

// dlapiPauseAll and dlapiResumeAll do the same for every download of one kind.
func (s *Server) dlapiPauseAll(proto queue.Protocol) int {
	items, err := s.QueueRepo.List()
	if err != nil {
		return 0
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	defer s.dispatch.Hold()()
	n := 0
	for _, it := range items {
		if it.Protocol == proto && downloadInProgress(it.Status) && s.pauseItem(it) == nil {
			n++
		}
	}
	return n
}

func (s *Server) dlapiResumeAll(proto queue.Protocol) int {
	items, err := s.QueueRepo.List()
	if err != nil {
		return 0
	}
	queueControlMu.Lock()
	defer queueControlMu.Unlock()
	defer s.dispatch.Hold()()
	n := 0
	for _, it := range items {
		if it.Protocol == proto && it.Status == queue.StatusPaused && s.resumeItem(it) == nil {
			n++
		}
	}
	return n
}

// randomHex is a random token for a sign-in cookie.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("make a random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
