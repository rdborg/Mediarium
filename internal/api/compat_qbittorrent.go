package api

import (
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/speed"
)

// The qBittorrent look-alike: <mediarium>/qbittorrent/api/v2/..., answered from
// the built-in torrent downloader. Apps such as nzb360 read the list (state,
// progress, speed, time left, seeds) and can pause, resume, remove and add.
//
// Signing in: the app's "password" is an administrator's Mediarium API key (the
// user name can be anything). The answer sets the SID cookie the real program
// sets. An API key sent as "Authorization: Bearer <key>" or X-Api-Key works
// too, without signing in first.
//
// What is not the same as in qBittorrent:
//   - The hash is made up from the download's number in Mediarium; it is the
//     same every time, but it is not the torrent's own info hash.
//   - Only torrents that are not finished, still seed, or finished in the last
//     day are listed; older finished ones live in Mediarium's history.
//   - Adding needs a release name that matches a movie or show in the library.
//     The category and save path sent with an add are ignored.
//   - Per-file lists, priorities, trackers, tags and speed limits are not
//     available.

const (
	qbAppVersion = "v4.6.7"
	qbAPIVersion = "2.9.3"
	// qbFinishedFor is how long a finished torrent that no longer seeds stays in the list.
	qbFinishedFor = 24 * time.Hour
	qbNoETA       = 8640000
)

func qbText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

func (s *Server) handleQBittorrent(w http.ResponseWriter, r *http.Request) {
	if !s.dlapiOn() {
		dlapiNotFound(w)
		return
	}
	path := strings.TrimRight(strings.TrimPrefix(r.URL.Path, "/qbittorrent"), "/")
	path, ok := strings.CutPrefix(path, "/api/v2/")
	if !ok {
		dlapiNotFound(w)
		return
	}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/") {
		allowLongUpload(w)
		r.Body = http.MaxBytesReader(w, r.Body, dlapiMaxAddBody)
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			qbText(w, http.StatusBadRequest, "Bad request")
			return
		}
		defer r.MultipartForm.RemoveAll()
	} else if err := r.ParseForm(); err != nil {
		qbText(w, http.StatusBadRequest, "Bad request")
		return
	}

	if path == "auth/login" {
		s.qbLogin(w, r)
		return
	}
	user, res := s.qbUser(r)
	switch res {
	case dlAuthLocked:
		qbText(w, http.StatusForbidden, "Your IP address has been banned after too many failed authentication attempts.")
		return
	case dlAuthNotAdmin:
		qbText(w, http.StatusForbidden, "Use an administrator's Mediarium API key.")
		return
	case dlAuthNone, dlAuthWrong:
		qbText(w, http.StatusForbidden, "Forbidden")
		return
	}
	writes := qbWriteCall(path)
	if writes {
		if r.Method != http.MethodPost {
			qbText(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		if user.ReadOnly() {
			qbText(w, http.StatusForbidden, readOnlyKeyMessage)
			return
		}
	}

	switch path {
	case "auth/logout":
		if c, err := r.Cookie("SID"); err == nil {
			d := s.dlapiInit()
			d.mu.Lock()
			delete(d.sessions, c.Value)
			d.mu.Unlock()
		}
		qbText(w, http.StatusOK, "Ok.")
	case "app/version":
		qbText(w, http.StatusOK, qbAppVersion)
	case "app/webapiVersion":
		qbText(w, http.StatusOK, qbAPIVersion)
	case "app/buildInfo":
		writeJSON(w, http.StatusOK, map[string]any{"qt": "6.5.3", "libtorrent": "2.0.10.0", "boost": "1.83.0", "openssl": "3.1.4", "bitness": 64})
	case "app/defaultSavePath":
		qbText(w, http.StatusOK, s.downloadsRoot())
	case "app/preferences":
		writeJSON(w, http.StatusOK, map[string]any{
			"save_path": s.downloadsRoot(), "temp_path_enabled": false, "dl_limit": speed.Current(), "up_limit": speed.CurrentUpload(),
			"queueing_enabled": false, "locale": "en", "web_ui_port": 0, "create_subfolder_enabled": true,
		})
	case "torrents/info":
		s.qbInfo(w, r)
	case "torrents/properties":
		s.qbProperties(w, r)
	case "torrents/files", "torrents/trackers", "torrents/webseeds", "torrents/tags":
		writeJSON(w, http.StatusOK, []any{})
	case "torrents/categories":
		s.qbCategories(w)
	case "torrents/pause", "torrents/stop", "torrents/resume", "torrents/start":
		s.qbPauseResume(w, r, strings.HasSuffix(path, "pause") || strings.HasSuffix(path, "stop"))
	case "torrents/delete":
		s.qbDelete(w, r)
	case "torrents/add":
		s.qbAdd(w, r)
	case "transfer/info":
		writeJSON(w, http.StatusOK, s.qbTransfer())
	case "transfer/speedLimitsMode":
		qbText(w, http.StatusOK, "0")
	case "transfer/downloadLimit":
		qbText(w, http.StatusOK, strconv.FormatInt(speed.Current(), 10))
	case "transfer/uploadLimit":
		qbText(w, http.StatusOK, strconv.FormatInt(speed.CurrentUpload(), 10))
	case "sync/maindata":
		s.qbMainData(w, r)
	default:
		slog.Info("downloader api: an app asked for a qBittorrent call Mediarium does not answer", "method", r.Method, "path", path)
		qbText(w, http.StatusNotFound, "Not Found")
	}
}

// qbWriteCall says whether a call changes something.
func qbWriteCall(path string) bool {
	switch path {
	case "auth/logout", "torrents/pause", "torrents/stop", "torrents/resume", "torrents/start", "torrents/delete", "torrents/add":
		return true
	}
	return false
}

// --- signing in ---

func (s *Server) qbLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		qbText(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	// The key is the password; a key typed in the user name box works too.
	key := r.PostFormValue("password")
	if key == "" {
		key = r.PostFormValue("username")
	}
	_, res := s.dlapiCheckKey(r, key)
	switch res {
	case dlAuthLocked:
		qbText(w, http.StatusForbidden, "Your IP address has been banned after too many failed authentication attempts.")
		return
	case dlAuthOK:
	default:
		qbText(w, http.StatusOK, "Fails.")
		return
	}
	sid, err := randomHex(16)
	if err != nil {
		qbText(w, http.StatusInternalServerError, "Fails.")
		return
	}
	d := s.dlapiInit()
	now := time.Now()
	d.mu.Lock()
	for k, v := range d.sessions {
		if now.After(v.expires) {
			delete(d.sessions, k)
		}
	}
	for len(d.sessions) >= dlapiMaxSession { // too many: the oldest goes
		var oldest string
		for k, v := range d.sessions {
			if oldest == "" || v.expires.Before(d.sessions[oldest].expires) {
				oldest = k
			}
		}
		delete(d.sessions, oldest)
	}
	d.sessions[sid] = dlSession{key: key, expires: now.Add(dlapiSessionFor)}
	d.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "SID", Value: sid, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.isHTTPS(r), MaxAge: int(dlapiSessionFor.Seconds())})
	qbText(w, http.StatusOK, "Ok.")
}

// qbUser signs a request in: with the SID cookie from a login, or with an API
// key sent straight away.
func (s *Server) qbUser(r *http.Request) (*auth.User, dlAuthResult) {
	key := ""
	if c, err := r.Cookie("SID"); err == nil && c.Value != "" {
		d := s.dlapiInit()
		now := time.Now()
		d.mu.Lock()
		sess, found := d.sessions[c.Value]
		if found && now.After(sess.expires) {
			delete(d.sessions, c.Value)
			found = false
		}
		if found {
			sess.expires = now.Add(dlapiSessionFor)
			d.sessions[c.Value] = sess
			key = sess.key
		}
		d.mu.Unlock()
	}
	if key == "" {
		if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			key = strings.TrimSpace(h[7:])
		}
	}
	if key == "" {
		key = r.Header.Get("X-Api-Key")
	}
	return s.dlapiCheckKey(r, key)
}

// --- the list ---

type qbTorrent struct {
	AddedOn           int64   `json:"added_on"`
	AmountLeft        int64   `json:"amount_left"`
	AutoTMM           bool    `json:"auto_tmm"`
	Availability      float64 `json:"availability"`
	Category          string  `json:"category"`
	Completed         int64   `json:"completed"`
	CompletionOn      int64   `json:"completion_on"`
	ContentPath       string  `json:"content_path"`
	DlLimit           int64   `json:"dl_limit"`
	DlSpeed           int64   `json:"dlspeed"`
	Downloaded        int64   `json:"downloaded"`
	DownloadedSession int64   `json:"downloaded_session"`
	ETA               int64   `json:"eta"`
	FLPiecePrio       bool    `json:"f_l_piece_prio"`
	ForceStart        bool    `json:"force_start"`
	Hash              string  `json:"hash"`
	InfohashV1        string  `json:"infohash_v1"`
	InfohashV2        string  `json:"infohash_v2"`
	LastActivity      int64   `json:"last_activity"`
	MagnetURI         string  `json:"magnet_uri"`
	MaxRatio          float64 `json:"max_ratio"`
	MaxSeedingTime    int64   `json:"max_seeding_time"`
	Name              string  `json:"name"`
	NumComplete       int     `json:"num_complete"`
	NumIncomplete     int     `json:"num_incomplete"`
	NumLeechs         int     `json:"num_leechs"`
	NumSeeds          int     `json:"num_seeds"`
	Priority          int     `json:"priority"`
	Progress          float64 `json:"progress"`
	Ratio             float64 `json:"ratio"`
	RatioLimit        float64 `json:"ratio_limit"`
	SavePath          string  `json:"save_path"`
	SeedingTime       int64   `json:"seeding_time"`
	SeedingTimeLimit  int64   `json:"seeding_time_limit"`
	SeenComplete      int64   `json:"seen_complete"`
	SeqDl             bool    `json:"seq_dl"`
	Size              int64   `json:"size"`
	State             string  `json:"state"`
	SuperSeeding      bool    `json:"super_seeding"`
	Tags              string  `json:"tags"`
	TimeActive        int64   `json:"time_active"`
	TotalSize         int64   `json:"total_size"`
	Tracker           string  `json:"tracker"`
	UpLimit           int64   `json:"up_limit"`
	Uploaded          int64   `json:"uploaded"`
	UploadedSession   int64   `json:"uploaded_session"`
	UpSpeed           int64   `json:"upspeed"`
}

// qbShown says whether a torrent is in the list the apps see.
func (s *Server) qbShown(it queue.Item, now time.Time) bool {
	if it.Protocol != queue.ProtocolTorrent {
		return false
	}
	if it.Status != queue.StatusCompleted {
		return true
	}
	if s.torrents.active(it.ID) && !s.torrents.seedingDone(it.ID) {
		return true
	}
	at := queueTime(it.CompletedAt)
	return !at.IsZero() && now.Sub(at) < qbFinishedFor
}

// qbStateOf is the state word of a torrent.
func qbStateOf(it queue.Item, seeding bool, dlRate, upRate int64, peers int) string {
	switch it.Status {
	case queue.StatusQueued:
		return "queuedDL"
	case queue.StatusDownloading:
		if it.SizeBytes <= 0 {
			return "metaDL"
		}
		if dlRate > 0 || peers > 0 {
			return "downloading"
		}
		return "stalledDL"
	case queue.StatusImporting:
		return "moving"
	case queue.StatusPaused, queue.StatusStopped:
		return "pausedDL"
	case queue.StatusFailed:
		return "error"
	case queue.StatusCompleted:
		if seeding {
			if upRate > 0 {
				return "uploading"
			}
			return "stalledUP"
		}
	}
	return "pausedUP" // finished, and a download parked on a file that already exists
}

func (s *Server) qbTorrentOf(it queue.Item, cats map[int64]string, now time.Time) qbTorrent {
	seeding := it.Status == queue.StatusCompleted && s.torrents.active(it.ID) && !s.torrents.seedingDone(it.ID)
	dr, ur := s.dlSpeeds(it, now)
	peers, seeds, _ := s.torrents.swarm(it.ID)
	progress := it.ProgressPct / 100
	if it.Status == queue.StatusCompleted || it.Status == queue.StatusConflict {
		progress = 1
	}
	if progress > 1 {
		progress = 1
	}
	done := int64(progress * float64(it.SizeBytes))
	var uploaded int64
	if _, wr, ok := s.torrents.transferred(it.ID); ok {
		uploaded = wr
	}
	eta := int64(qbNoETA)
	if secs := dlEstimate(it, dr); secs >= 0 && it.Status == queue.StatusDownloading {
		eta = secs
	}
	cat := cats[it.ID]
	if cat == "" {
		cat = dlKind(it)
	}
	hash := qbHash(it.ID)
	save := s.workDirFor(it.ID)
	added := queueTime(it.AddedAt).Unix()
	completion := int64(-1)
	if at := queueTime(it.CompletedAt); !at.IsZero() {
		completion = at.Unix()
	}
	leechs := peers - seeds
	if leechs < 0 {
		leechs = 0
	}
	ratio := 0.0
	if it.SizeBytes > 0 {
		ratio = float64(uploaded) / float64(it.SizeBytes)
	}
	return qbTorrent{
		AddedOn: added, AmountLeft: it.SizeBytes - done, Category: cat, Completed: done, CompletionOn: completion,
		ContentPath: save, DlSpeed: dr, Downloaded: done, DownloadedSession: done, ETA: eta, Hash: hash, InfohashV1: hash,
		LastActivity: added, MaxRatio: -1, MaxSeedingTime: -1, Name: it.ReleaseTitle, NumComplete: seeds, NumIncomplete: leechs,
		NumLeechs: leechs, NumSeeds: seeds, Progress: progress, Ratio: ratio, RatioLimit: -2,
		SavePath: save, SeedingTimeLimit: -2, Size: it.SizeBytes, State: qbStateOf(it, seeding, dr, ur, peers), TotalSize: it.SizeBytes,
		Uploaded: uploaded, UploadedSession: uploaded, UpSpeed: ur, Availability: -1,
	}
}

// qbList builds the list the apps see.
func (s *Server) qbList(now time.Time) ([]qbTorrent, error) {
	items, err := s.dlapiItems()
	if err != nil {
		return nil, err
	}
	cats, _ := s.TorrentOpts.Categories()
	out := []qbTorrent{}
	for _, it := range items {
		if s.qbShown(it, now) {
			out = append(out, s.qbTorrentOf(it, cats, now))
		}
	}
	return out, nil
}

func qbHashes(r *http.Request) (all bool, set map[string]bool) {
	v := strings.TrimSpace(r.FormValue("hashes"))
	if v == "" {
		v = strings.TrimSpace(r.FormValue("hash"))
	}
	if strings.EqualFold(v, "all") {
		return true, nil
	}
	set = map[string]bool{}
	for _, h := range strings.Split(v, "|") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			set[h] = true
		}
	}
	return false, set
}

// qbFilterMatch is qBittorrent's "filter" option.
func qbFilterMatch(filter string, t qbTorrent) bool {
	paused := t.State == "pausedDL" || t.State == "pausedUP"
	switch strings.ToLower(filter) {
	case "", "all":
		return true
	case "downloading":
		return strings.HasSuffix(t.State, "DL") && t.State != "pausedDL"
	case "seeding":
		return t.State == "uploading" || t.State == "stalledUP"
	case "completed":
		return t.Progress >= 1
	case "paused", "stopped":
		return paused
	case "resumed", "running":
		return !paused
	case "active":
		return t.DlSpeed > 0 || t.UpSpeed > 0
	case "inactive":
		return t.DlSpeed == 0 && t.UpSpeed == 0
	case "stalled":
		return t.State == "stalledDL" || t.State == "stalledUP"
	case "stalled_uploading":
		return t.State == "stalledUP"
	case "stalled_downloading":
		return t.State == "stalledDL"
	case "errored":
		return t.State == "error"
	}
	return true
}

func (s *Server) qbInfo(w http.ResponseWriter, r *http.Request) {
	list, err := s.qbList(time.Now())
	if err != nil {
		qbText(w, http.StatusInternalServerError, "Couldn't read the download list.")
		return
	}
	all, set := qbHashes(r)
	filter, cat := r.FormValue("filter"), r.FormValue("category")
	_, catGiven := r.Form["category"]
	var out []qbTorrent
	for _, t := range list {
		if !all && len(set) > 0 && !set[t.Hash] {
			continue
		}
		if !qbFilterMatch(filter, t) {
			continue
		}
		if catGiven && t.Category != cat {
			continue
		}
		out = append(out, t)
	}
	sortKey := r.FormValue("sort")
	if sortKey != "" {
		less := qbLess(sortKey)
		reverse := r.FormValue("reverse") == "true"
		sort.SliceStable(out, func(i, j int) bool {
			if reverse {
				return less(out[j], out[i])
			}
			return less(out[i], out[j])
		})
	}
	offset, _ := strconv.Atoi(r.FormValue("offset"))
	if offset < 0 {
		offset = len(out) + offset
		if offset < 0 {
			offset = 0
		}
	}
	if offset > len(out) {
		offset = len(out)
	}
	out = out[offset:]
	if limit, _ := strconv.Atoi(r.FormValue("limit")); limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	if out == nil {
		out = []qbTorrent{}
	}
	writeJSON(w, http.StatusOK, out)
}

// qbLess orders by the field an app asks to sort on (unknown fields keep the order).
func qbLess(key string) func(a, b qbTorrent) bool {
	switch key {
	case "name":
		return func(a, b qbTorrent) bool { return strings.ToLower(a.Name) < strings.ToLower(b.Name) }
	case "size", "total_size":
		return func(a, b qbTorrent) bool { return a.Size < b.Size }
	case "progress":
		return func(a, b qbTorrent) bool { return a.Progress < b.Progress }
	case "dlspeed":
		return func(a, b qbTorrent) bool { return a.DlSpeed < b.DlSpeed }
	case "upspeed":
		return func(a, b qbTorrent) bool { return a.UpSpeed < b.UpSpeed }
	case "eta":
		return func(a, b qbTorrent) bool { return a.ETA < b.ETA }
	case "added_on":
		return func(a, b qbTorrent) bool { return a.AddedOn < b.AddedOn }
	case "completion_on":
		return func(a, b qbTorrent) bool { return a.CompletionOn < b.CompletionOn }
	case "state":
		return func(a, b qbTorrent) bool { return a.State < b.State }
	case "category":
		return func(a, b qbTorrent) bool { return a.Category < b.Category }
	case "ratio":
		return func(a, b qbTorrent) bool { return a.Ratio < b.Ratio }
	}
	return func(a, b qbTorrent) bool { return false }
}

func (s *Server) qbProperties(w http.ResponseWriter, r *http.Request) {
	list, err := s.qbList(time.Now())
	if err != nil {
		qbText(w, http.StatusInternalServerError, "Couldn't read the download list.")
		return
	}
	hash := strings.ToLower(strings.TrimSpace(r.FormValue("hash")))
	for _, t := range list {
		if t.Hash != hash {
			continue
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"save_path": t.SavePath, "creation_date": -1, "piece_size": 0, "comment": "", "total_wasted": 0,
			"total_uploaded": t.Uploaded, "total_uploaded_session": t.Uploaded, "total_downloaded": t.Downloaded, "total_downloaded_session": t.Downloaded,
			"up_limit": -1, "dl_limit": -1, "time_elapsed": 0, "seeding_time": 0, "nb_connections": t.NumSeeds + t.NumLeechs, "nb_connections_limit": -1,
			"share_ratio": t.Ratio, "addition_date": t.AddedOn, "completion_date": t.CompletionOn, "created_by": "", "dl_speed_avg": t.DlSpeed, "dl_speed": t.DlSpeed,
			"eta": t.ETA, "last_seen": t.LastActivity, "peers": t.NumLeechs, "peers_total": t.NumLeechs, "pieces_have": 0, "pieces_num": 0, "reannounce": 0,
			"seeds": t.NumSeeds, "seeds_total": t.NumSeeds, "total_size": t.Size, "up_speed_avg": t.UpSpeed, "up_speed": t.UpSpeed,
		})
		return
	}
	qbText(w, http.StatusNotFound, "Not Found")
}

func (s *Server) qbCategories(w http.ResponseWriter) {
	out := map[string]any{}
	for _, c := range sabCategories() {
		out[c] = map[string]string{"name": c, "savePath": ""}
	}
	for _, c := range s.torrentCategoryConfig().Categories {
		if c.Name != "" {
			out[c.Name] = map[string]string{"name": c.Name, "savePath": ""}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// --- changing things ---

// qbTargets are the queue items the hashes of a request name.
func (s *Server) qbTargets(r *http.Request) ([]queue.Item, error) {
	items, err := s.QueueRepo.List()
	if err != nil {
		return nil, err
	}
	all, set := qbHashes(r)
	var out []queue.Item
	for _, it := range items {
		if it.Protocol != queue.ProtocolTorrent {
			continue
		}
		if all || set[qbHash(it.ID)] {
			out = append(out, it)
		}
	}
	return out, nil
}

func (s *Server) qbPauseResume(w http.ResponseWriter, r *http.Request, pause bool) {
	targets, err := s.qbTargets(r)
	if err != nil {
		qbText(w, http.StatusInternalServerError, "Couldn't read the download list.")
		return
	}
	for _, it := range targets {
		var cerr error
		if pause {
			cerr = s.dlapiPause(it.ID)
		} else if it.Status == queue.StatusPaused {
			cerr = s.dlapiResume(it.ID)
		}
		if cerr != nil {
			slog.Info("downloader api: a torrent could not be paused or resumed", "id", it.ID, "pause", pause, "reason", cerr.Error())
		}
	}
	qbText(w, http.StatusOK, "")
}

func (s *Server) qbDelete(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.FormValue("hashes")) == "" {
		qbText(w, http.StatusBadRequest, "Bad request")
		return
	}
	targets, err := s.qbTargets(r)
	if err != nil {
		qbText(w, http.StatusInternalServerError, "Couldn't read the download list.")
		return
	}
	deleteFiles := strings.EqualFold(r.FormValue("deleteFiles"), "true")
	for _, it := range targets {
		if rerr := s.dlapiRemove(it.ID, deleteFiles); rerr != nil {
			slog.Info("downloader api: a torrent could not be removed", "id", it.ID, "reason", rerr.Error())
		}
	}
	qbText(w, http.StatusOK, "")
}

func (s *Server) qbAdd(w http.ResponseWriter, r *http.Request) {
	var files []*multipart.FileHeader
	if r.MultipartForm != nil {
		files = r.MultipartForm.File["torrents"]
	}
	var addresses []string
	for _, line := range strings.FieldsFunc(r.FormValue("urls"), func(c rune) bool { return c == '\n' || c == '\r' }) {
		if line = strings.TrimSpace(line); line != "" {
			addresses = append(addresses, line)
		}
	}
	if len(files) == 0 && len(addresses) == 0 {
		qbText(w, http.StatusBadRequest, "Bad request")
		return
	}
	name := r.FormValue("rename")
	added := 0
	for _, fh := range files {
		if _, err := s.dlapiAdd(r.Context(), indexers.ProtocolTorrent, fh, "", name); err != nil {
			slog.Info("downloader api: a torrent file was not added", "reason", err.Error())
			continue
		}
		added++
	}
	for _, address := range addresses {
		if _, err := s.dlapiAdd(r.Context(), indexers.ProtocolTorrent, nil, address, name); err != nil {
			slog.Info("downloader api: a torrent address was not added", "reason", err.Error())
			continue
		}
		added++
	}
	if added == 0 {
		qbText(w, http.StatusOK, "Fails.")
		return
	}
	qbText(w, http.StatusOK, "Ok.")
}

// --- totals ---

func (s *Server) qbTransfer() map[string]any {
	list, _ := s.qbList(time.Now())
	var dl, ul, dlData, ulData int64
	for _, t := range list {
		dl += t.DlSpeed
		ul += t.UpSpeed
		dlData += t.Downloaded
		ulData += t.Uploaded
	}
	return map[string]any{
		"connection_status": "connected", "dht_nodes": 0, "dl_info_data": dlData, "dl_info_speed": dl, "dl_rate_limit": speed.Current(),
		"up_info_data": ulData, "up_info_speed": ul, "up_rate_limit": speed.CurrentUpload(),
	}
}

// qbMainData is the "sync" call. Every answer is a full one, which the apps
// take as the whole state; that keeps it simple and the list is small.
func (s *Server) qbMainData(w http.ResponseWriter, r *http.Request) {
	list, err := s.qbList(time.Now())
	if err != nil {
		qbText(w, http.StatusInternalServerError, "Couldn't read the download list.")
		return
	}
	torrents := map[string]qbTorrent{}
	for _, t := range list {
		torrents[t.Hash] = t
	}
	cats := map[string]any{}
	for _, c := range sabCategories() {
		cats[c] = map[string]string{"name": c, "savePath": ""}
	}
	rid, _ := strconv.Atoi(r.FormValue("rid"))
	tr := s.qbTransfer()
	free := int64(0)
	if u, err := fsinfo.DiskUsage(s.downloadsRoot()); err == nil {
		free = int64(u.FreeBytes)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rid": rid + 1, "full_update": true, "torrents": torrents, "categories": cats, "tags": []string{},
		"server_state": map[string]any{
			"connection_status": "connected", "dht_nodes": 0, "dl_info_data": tr["dl_info_data"], "dl_info_speed": tr["dl_info_speed"],
			"dl_rate_limit": tr["dl_rate_limit"], "up_info_data": tr["up_info_data"], "up_info_speed": tr["up_info_speed"], "up_rate_limit": tr["up_rate_limit"],
			"free_space_on_disk": free, "queueing": false, "use_alt_speed_limits": false, "refresh_interval": 1500, "total_peer_connections": 0,
		},
	})
}
