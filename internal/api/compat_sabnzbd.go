package api

import (
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/queue"
)

// The SABnzbd look-alike: <mediarium>/sabnzbd/api?mode=...&apikey=...&output=json,
// answered from the built-in Usenet downloader. Apps such as nzb360 read the
// queue (speed, time left, percentages), the history, and can pause, resume,
// remove and add an NZB.
//
// What is not the same as in SABnzbd:
//   - SABnzbd has one global pause. Here "pause" pauses every Usenet download
//     that is running or waiting, and "resume" puts every paused one back.
//   - Categories are the kinds of title (movies, tv, music, books). A category
//     sent with an add is ignored: the title decides.
//   - Adding needs a release name that matches a movie or show in the library.
//   - Priority, renaming, sorting and the speed limit are not available.

// sabVersion is the SABnzbd version reported to the apps.
const sabVersion = "4.3.3"

func (s *Server) handleSABnzbd(w http.ResponseWriter, r *http.Request) {
	if !s.dlapiOn() {
		dlapiNotFound(w)
		return
	}
	if p := strings.TrimRight(strings.TrimPrefix(r.URL.Path, "/sabnzbd"), "/"); p != "/api" {
		dlapiNotFound(w)
		return
	}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/") {
		allowLongUpload(w)
		r.Body = http.MaxBytesReader(w, r.Body, dlapiMaxAddBody)
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			sabFail(w, http.StatusBadRequest, "The upload couldn't be read. Try again.")
			return
		}
		defer r.MultipartForm.RemoveAll()
	} else if err := r.ParseForm(); err != nil {
		sabFail(w, http.StatusBadRequest, "The request couldn't be read.")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.FormValue("mode")))
	switch mode {
	case "version":
		// Like SABnzbd, the version and the way to sign in are told without a key.
		if strings.EqualFold(r.FormValue("output"), "json") {
			writeJSON(w, http.StatusOK, map[string]string{"version": sabVersion})
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, sabVersion)
		return
	case "auth":
		writeJSON(w, http.StatusOK, map[string]string{"auth": "apikey"})
		return
	}

	key := r.FormValue("apikey")
	if key == "" {
		key = r.Header.Get("X-Api-Key")
	}
	user, res := s.dlapiCheckKey(r, key)
	switch res {
	case dlAuthLocked:
		w.Header().Set("Retry-After", "900")
		sabFail(w, http.StatusTooManyRequests, "Too many wrong tries. Wait a few minutes and try again.")
		return
	case dlAuthNone, dlAuthWrong:
		sabFail(w, http.StatusForbidden, "API Key Incorrect")
		return
	case dlAuthNotAdmin:
		sabFail(w, http.StatusForbidden, "Use an administrator's Mediarium API key.")
		return
	}
	if user.ReadOnly() && !sabReadMode(mode, strings.ToLower(r.FormValue("name"))) {
		sabFail(w, http.StatusForbidden, readOnlyKeyMessage)
		return
	}

	switch mode {
	case "queue":
		s.sabQueueMode(w, r)
	case "history":
		s.sabHistoryMode(w, r)
	case "pause":
		n := s.dlapiPauseAll(queue.ProtocolUsenet)
		slog.Info("downloader api: Usenet downloads paused", "count", n)
		writeJSON(w, http.StatusOK, map[string]bool{"status": true})
	case "resume":
		n := s.dlapiResumeAll(queue.ProtocolUsenet)
		slog.Info("downloader api: Usenet downloads resumed", "count", n)
		writeJSON(w, http.StatusOK, map[string]bool{"status": true})
	case "addurl", "addfile", "addlocalfile":
		s.sabAdd(w, r, mode)
	case "get_cats":
		writeJSON(w, http.StatusOK, map[string]any{"categories": append([]string{"*"}, sabCategories()...)})
	case "get_scripts":
		writeJSON(w, http.StatusOK, map[string]any{"scripts": []string{"None"}})
	case "get_config":
		s.sabConfig(w)
	case "fullstatus":
		s.sabFullStatus(w)
	case "warnings":
		writeJSON(w, http.StatusOK, map[string]any{"warnings": []string{}})
	case "server_stats":
		writeJSON(w, http.StatusOK, map[string]any{"total": 0, "month": 0, "week": 0, "day": 0, "servers": map[string]any{}})
	case "":
		sabFail(w, http.StatusBadRequest, "No mode given.")
	default:
		sabFail(w, http.StatusOK, "Mediarium doesn't answer this call ("+mode+").")
	}
}

// sabReadMode says whether a call only reads, so a read-only key may make it.
func sabReadMode(mode, name string) bool {
	switch mode {
	case "queue", "history":
		return name == ""
	case "get_cats", "get_scripts", "get_config", "fullstatus", "warnings", "server_stats", "version", "auth":
		return true
	}
	return false
}

func sabFail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"status": false, "error": message})
}

// sabCategories are the category names offered to the apps.
func sabCategories() []string { return []string{"movies", "tv", "music", "books"} }

// --- numbers the way SABnzbd writes them ---

func sabBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// sabSpeed is a speed written short: "1.2 M" for about a megabyte a second.
func sabSpeed(bps int64) string {
	units := []string{"", "K", "M", "G"}
	f := float64(bps)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

func sabMB(n int64) string { return fmt.Sprintf("%.2f", float64(n)/(1<<20)) }

// sabTime is a time left as H:MM:SS, with the days in front once there are any.
func sabTime(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	d, h, m, sec := seconds/86400, seconds/3600%24, seconds/60%60, seconds%60
	if d > 0 {
		return fmt.Sprintf("%d:%02d:%02d:%02d", d, h, m, sec)
	}
	return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
}

// sabStatusOf is the status word of a queue slot.
func sabStatusOf(it queue.Item, rate int64) string {
	switch it.Status {
	case queue.StatusDownloading:
		if rate == 0 && it.ProgressPct == 0 {
			return "Fetching"
		}
		return "Downloading"
	case queue.StatusQueued:
		return "Queued"
	case queue.StatusImporting:
		return "Extracting"
	}
	return "Paused" // paused, and a finished download parked on a file that already exists
}

func sabPriority(it queue.Item) string {
	if it.Priority == queue.PriorityManual {
		return "High"
	}
	return "Normal"
}

// sabInQueue says whether a Usenet download is shown in the queue (anything
// that is not finished) rather than in the history.
func sabInQueue(it queue.Item) bool {
	if it.Protocol != queue.ProtocolUsenet {
		return false
	}
	switch it.Status {
	case queue.StatusQueued, queue.StatusDownloading, queue.StatusImporting, queue.StatusPaused, queue.StatusConflict:
		return true
	}
	return false
}

// --- the queue ---

func (s *Server) sabQueueMode(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(r.FormValue("name"))
	value := r.FormValue("value")
	switch name {
	case "":
		s.sabQueue(w, r)
	case "delete", "purge":
		ids := sabIDs(value)
		if name == "purge" || strings.EqualFold(value, "all") {
			ids = nil
			if items, err := s.QueueRepo.List(); err == nil {
				for _, it := range items {
					if sabInQueue(it) {
						ids = append(ids, sabID(it.ID))
					}
				}
			}
		}
		deleteFiles := r.FormValue("del_files") == "1"
		var gone []string
		for _, raw := range ids {
			id, ok := sabQueueID(raw)
			if !ok {
				continue
			}
			if err := s.dlapiRemove(id, deleteFiles); err != nil {
				slog.Info("downloader api: could not remove a download", "id", id, "reason", err.Error())
				continue
			}
			gone = append(gone, raw)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": true, "nzo_ids": nonNil(gone)})
	case "pause", "resume":
		var done []string
		for _, raw := range sabIDs(value) {
			id, ok := sabQueueID(raw)
			if !ok {
				continue
			}
			var err error
			if name == "pause" {
				err = s.dlapiPause(id)
			} else {
				err = s.dlapiResume(id)
			}
			if err == nil {
				done = append(done, raw)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": true, "nzo_ids": nonNil(done)})
	default:
		sabFail(w, http.StatusOK, "Mediarium can't do that ("+name+") with a queue entry.")
	}
}

// sabIDs splits the ids an app sends: one, or several joined with commas.
func sabIDs(value string) []string {
	var out []string
	for _, p := range strings.Split(value, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type sabSlot struct {
	Status     string   `json:"status"`
	Index      int      `json:"index"`
	Password   string   `json:"password"`
	AvgAge     string   `json:"avg_age"`
	Script     string   `json:"script"`
	MB         string   `json:"mb"`
	MBLeft     string   `json:"mbleft"`
	MBMissing  string   `json:"mbmissing"`
	Size       string   `json:"size"`
	SizeLeft   string   `json:"sizeleft"`
	Filename   string   `json:"filename"`
	Labels     []string `json:"labels"`
	Priority   string   `json:"priority"`
	Cat        string   `json:"cat"`
	TimeLeft   string   `json:"timeleft"`
	Percentage string   `json:"percentage"`
	NzoID      string   `json:"nzo_id"`
	UnpackOpts string   `json:"unpackopts"`
}

func (s *Server) sabQueue(w http.ResponseWriter, r *http.Request) {
	items, err := s.dlapiItems()
	if err != nil {
		sabFail(w, http.StatusInternalServerError, "Couldn't read the download list. Try again.")
		return
	}
	now := time.Now()
	var all []sabSlot
	var rateTotal, sizeTotal, leftTotal int64
	paused, running := false, false
	for _, it := range items {
		if !sabInQueue(it) {
			continue
		}
		rate, _ := s.dlSpeeds(it, now)
		left := it.SizeBytes - int64(it.ProgressPct/100*float64(it.SizeBytes))
		if left < 0 {
			left = 0
		}
		sizeTotal += it.SizeBytes
		leftTotal += left
		rateTotal += rate
		switch it.Status {
		case queue.StatusPaused, queue.StatusConflict:
			paused = true
		default:
			running = true
		}
		eta := "0:00:00"
		if secs := dlEstimate(it, rate); secs >= 0 {
			eta = sabTime(secs)
		}
		all = append(all, sabSlot{
			Status: sabStatusOf(it, rate), Index: len(all), AvgAge: sabAge(it.AddedAt, now), Script: "None",
			MB: sabMB(it.SizeBytes), MBLeft: sabMB(left), MBMissing: "0.0", Size: sabBytes(it.SizeBytes), SizeLeft: sabBytes(left),
			Filename: it.ReleaseTitle, Labels: []string{}, Priority: sabPriority(it), Cat: sabCat(it), TimeLeft: eta,
			Percentage: strconv.Itoa(int(it.ProgressPct)), NzoID: sabID(it.ID), UnpackOpts: "3",
		})
	}
	start, limit := sabPage(r, len(all))
	end := len(all)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	slots := all[start:end]
	if slots == nil {
		slots = []sabSlot{}
	}
	eta, timeLeft := "unknown", "0:00:00"
	if rateTotal > 0 {
		secs := leftTotal / rateTotal
		timeLeft = sabTime(secs)
		eta = time.Now().Add(time.Duration(secs) * time.Second).Format("15:04 Mon 2 Jan")
	}
	global := paused && !running
	free1, total1 := s.sabDisk()
	writeJSON(w, http.StatusOK, map[string]any{"queue": map[string]any{
		"version": sabVersion, "paused": global, "pause_int": "0", "paused_all": global,
		"diskspace1": free1, "diskspace2": free1, "diskspacetotal1": total1, "diskspacetotal2": total1,
		"diskspace1_norm": s.sabDiskNorm(), "diskspace2_norm": s.sabDiskNorm(),
		"speedlimit": "", "speedlimit_abs": "", "have_warnings": "0", "finishaction": nil,
		"quota": "0 ", "have_quota": false, "left_quota": "0 ", "cache_art": "0", "cache_size": "0 B",
		"kbpersec": fmt.Sprintf("%.2f", float64(rateTotal)/1024), "speed": sabSpeed(rateTotal),
		"mb": sabMB(sizeTotal), "mbleft": sabMB(leftTotal), "size": sabBytes(sizeTotal), "sizeleft": sabBytes(leftTotal),
		"eta": eta, "timeleft": timeLeft,
		"noofslots_total": len(all), "noofslots": len(slots), "start": start, "limit": limit,
		"slots": slots,
	}})
}

// sabPage reads start and limit (limit 0 is everything).
func sabPage(r *http.Request, n int) (start, limit int) {
	start, _ = strconv.Atoi(r.FormValue("start"))
	limit, _ = strconv.Atoi(r.FormValue("limit"))
	if start < 0 || start > n {
		start = 0
	}
	if limit < 0 {
		limit = 0
	}
	return start, limit
}

func sabCat(it queue.Item) string {
	if c := dlKind(it); c != "" {
		return c
	}
	return "*"
}

// sabAge is how long ago a download was added, written short ("2d", "5h", "12m").
func sabAge(added string, now time.Time) string {
	at := queueTime(added)
	if at.IsZero() {
		return ""
	}
	d := now.Sub(at)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// sabDisk is the free and total space of the downloads folder in gigabytes, as text.
func (s *Server) sabDisk() (free, total string) {
	u, err := fsinfo.DiskUsage(s.downloadsRoot())
	if err != nil {
		return "0", "0"
	}
	return fmt.Sprintf("%.2f", float64(u.FreeBytes)/(1<<30)), fmt.Sprintf("%.2f", float64(u.TotalBytes)/(1<<30))
}

func (s *Server) sabDiskNorm() string {
	u, err := fsinfo.DiskUsage(s.downloadsRoot())
	if err != nil {
		return "0 B"
	}
	return sabBytes(int64(u.FreeBytes))
}

// --- the history ---

type sabHistorySlot struct {
	Completed   int64    `json:"completed"`
	Name        string   `json:"name"`
	NzbName     string   `json:"nzb_name"`
	Category    string   `json:"category"`
	PP          string   `json:"pp"`
	Script      string   `json:"script"`
	Report      string   `json:"report"`
	URL         string   `json:"url"`
	Status      string   `json:"status"`
	NzoID       string   `json:"nzo_id"`
	Storage     string   `json:"storage"`
	Path        string   `json:"path"`
	ScriptLine  string   `json:"script_line"`
	DownloadSec int      `json:"download_time"`
	PostSec     int      `json:"postproc_time"`
	StageLog    []string `json:"stage_log"`
	Downloaded  int64    `json:"downloaded"`
	Bytes       int64    `json:"bytes"`
	Size        string   `json:"size"`
	FailMessage string   `json:"fail_message"`
	Password    string   `json:"password"`
	Archive     bool     `json:"archive"`
	ActionLine  string   `json:"action_line"`
	Retry       int      `json:"retry"`
}

// sabInHistory says whether a Usenet download is shown in the history: it
// finished, failed or was cancelled.
func sabInHistory(it queue.Item) bool {
	if it.Protocol != queue.ProtocolUsenet {
		return false
	}
	switch it.Status {
	case queue.StatusCompleted, queue.StatusFailed, queue.StatusStopped:
		return true
	}
	return false
}

func (s *Server) sabHistoryMode(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(r.FormValue("name"))
	if name == "delete" {
		items, err := s.QueueRepo.List()
		if err != nil {
			sabFail(w, http.StatusInternalServerError, "Couldn't read the download list. Try again.")
			return
		}
		value := r.FormValue("value")
		var raws []string
		switch strings.ToLower(value) {
		case "all", "failed":
			for _, it := range items {
				if sabInHistory(it) && (strings.EqualFold(value, "all") || it.Status != queue.StatusCompleted) {
					raws = append(raws, sabID(it.ID))
				}
			}
		default:
			raws = sabIDs(value)
		}
		deleteFiles := r.FormValue("del_files") == "1"
		var gone []string
		for _, raw := range raws {
			id, ok := sabQueueID(raw)
			if !ok {
				continue
			}
			if it, err := s.QueueRepo.Get(id); err != nil || !sabInHistory(it) {
				continue
			}
			if err := s.dlapiRemove(id, deleteFiles); err == nil {
				gone = append(gone, raw)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": true, "nzo_ids": nonNil(gone)})
		return
	}
	if name != "" {
		sabFail(w, http.StatusOK, "Mediarium can't do that ("+name+") with the history.")
		return
	}

	items, err := s.dlapiItems()
	if err != nil {
		sabFail(w, http.StatusInternalServerError, "Couldn't read the download list. Try again.")
		return
	}
	cat := strings.ToLower(r.FormValue("category"))
	if cat == "" {
		cat = strings.ToLower(r.FormValue("cat"))
	}
	search := strings.ToLower(r.FormValue("search"))
	failedOnly := r.FormValue("failed_only") == "1"
	var all []sabHistorySlot
	var totalSize int64
	for _, it := range items {
		if !sabInHistory(it) {
			continue
		}
		failed := it.Status != queue.StatusCompleted
		if failedOnly && !failed {
			continue
		}
		if cat != "" && cat != "*" && cat != sabCat(it) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(it.ReleaseTitle), search) {
			continue
		}
		slot := sabHistorySlot{
			Name: it.ReleaseTitle, NzbName: it.ReleaseTitle + ".nzb", Category: sabCat(it), PP: "D", Script: "None",
			Status: "Completed", NzoID: sabID(it.ID), StageLog: []string{}, Downloaded: it.SizeBytes, Bytes: it.SizeBytes, Size: sabBytes(it.SizeBytes),
		}
		if at := queueTime(it.CompletedAt); !at.IsZero() {
			slot.Completed = at.Unix()
		} else if at := queueTime(it.AddedAt); !at.IsZero() {
			slot.Completed = at.Unix()
		}
		if failed {
			slot.Status = "Failed"
			slot.FailMessage = "Stopped."
			if it.Status == queue.StatusFailed {
				slot.FailMessage = memberlessText(it.Error)
			}
		}
		totalSize += it.SizeBytes
		all = append(all, slot)
	}
	start, limit := sabPage(r, len(all))
	end := len(all)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	slots := all[start:end]
	if slots == nil {
		slots = []sabHistorySlot{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": map[string]any{
		"noofslots": len(all), "ppslots": 0, "day_size": "0 B", "week_size": "0 B", "month_size": "0 B",
		"total_size": sabBytes(totalSize), "last_history_update": time.Now().Unix(), "slots": slots,
	}})
}

// memberlessText is a download's error as plain text for an app.
func memberlessText(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

// --- adding ---

func (s *Server) sabAdd(w http.ResponseWriter, r *http.Request, mode string) {
	if mode == "addlocalfile" {
		// A path on the machine Mediarium runs on: never read for an app.
		sabFail(w, http.StatusOK, "Mediarium can't add a file from its own disk this way. Send the NZB file or its address.")
		return
	}
	var (
		fh      *multipart.FileHeader
		address string
	)
	if mode == "addfile" {
		if r.MultipartForm != nil {
			for _, field := range []string{"name", "nzbfile"} {
				if l := r.MultipartForm.File[field]; len(l) > 0 {
					fh = l[0]
					break
				}
			}
		}
		if fh == nil {
			sabFail(w, http.StatusOK, "No NZB file came with the request.")
			return
		}
	} else {
		address = strings.TrimSpace(r.FormValue("name"))
		if address == "" {
			sabFail(w, http.StatusOK, "No address came with the request.")
			return
		}
	}
	id, err := s.dlapiAdd(r.Context(), indexers.ProtocolUsenet, fh, address, r.FormValue("nzbname"))
	if err != nil {
		sabFail(w, http.StatusOK, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": true, "nzo_ids": []string{sabID(id)}})
}

// --- settings and status ---

func (s *Server) sabConfig(w http.ResponseWriter) {
	cats := []map[string]any{{"name": "*", "order": 0, "pp": "3", "script": "Default", "dir": "", "newzbin": "", "priority": -100}}
	for i, c := range sabCategories() {
		cats = append(cats, map[string]any{"name": c, "order": i + 1, "pp": "", "script": "Default", "dir": "", "newzbin": "", "priority": -100})
	}
	root := s.downloadsRoot()
	writeJSON(w, http.StatusOK, map[string]any{"config": map[string]any{
		"misc":       map[string]any{"complete_dir": root, "download_dir": root, "port": "", "host": ""},
		"categories": cats,
		"servers":    []any{},
	}})
}

func (s *Server) sabFullStatus(w http.ResponseWriter) {
	items, _ := s.QueueRepo.List()
	now := time.Now()
	var rate, size, left int64
	slots := 0
	paused, running := false, false
	for _, it := range items {
		if !sabInQueue(it) {
			continue
		}
		slots++
		dr, _ := s.dlSpeeds(it, now)
		rate += dr
		size += it.SizeBytes
		left += it.SizeBytes - int64(it.ProgressPct/100*float64(it.SizeBytes))
		if it.Status == queue.StatusPaused || it.Status == queue.StatusConflict {
			paused = true
		} else {
			running = true
		}
	}
	free, total := s.sabDisk()
	writeJSON(w, http.StatusOK, map[string]any{"status": map[string]any{
		"version": sabVersion, "paused": paused && !running, "pause_int": "0", "paused_all": paused && !running,
		"diskspace1": free, "diskspace2": free, "diskspacetotal1": total, "diskspacetotal2": total,
		"speedlimit": "", "speedlimit_abs": "", "have_warnings": "0", "finishaction": nil,
		"kbpersec": fmt.Sprintf("%.2f", float64(rate)/1024), "speed": sabSpeed(rate),
		"mb": sabMB(size), "mbleft": sabMB(left), "noofslots": slots, "noofslots_total": slots,
		"loadavg": "", "folders": []string{}, "servers": []any{}, "warnings": []string{},
	}})
}
