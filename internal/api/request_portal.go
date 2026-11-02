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
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/requests"
	"github.com/rdborg/mediarium/internal/settings"
)

// The request portal around Activity > Requests: messages to the person who
// asked, a limit on how many requests a person may make, 4K requests, and the
// settings for those (Settings > Accounts > All accounts). All of it is off or
// unlimited until an administrator sets it.

// requestOptions are the portal's settings, kept as one JSON value.
type requestOptions struct {
	// NotifyRequester tells people (by email and on the devices they allowed)
	// when their request is answered or ready, and when their report is.
	NotifyRequester bool `json:"notifyRequester"`
	// QuotaCount is how many requests an account may make in a QuotaPeriod
	// ("day" or "week"); 0 is no limit.
	QuotaCount  int    `json:"quotaCount"`
	QuotaPeriod string `json:"quotaPeriod"`
	// FourK lets people ask for a 4K copy; FourKProfileID is the quality
	// profile that copy is kept in.
	FourK          bool  `json:"fourK"`
	FourKProfileID int64 `json:"fourKProfileId"`
	// Reports lets people report a problem with a title.
	Reports bool `json:"reports"`
}

func (s *Server) requestOptions() requestOptions {
	o := requestOptions{QuotaPeriod: "week"}
	if v, _ := s.Settings.Get(settings.KeyRequestOptions); v != "" {
		_ = json.Unmarshal([]byte(v), &o)
	}
	if o.QuotaPeriod != "day" {
		o.QuotaPeriod = "week"
	}
	return o
}

func (s *Server) saveRequestOptions(o requestOptions) error {
	b, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("save request settings: %w", err)
	}
	return s.Settings.Set(settings.KeyRequestOptions, string(b), false)
}

// portalState holds what tests change about the request portal.
type portalState struct {
	// extraSenders adds ways to reach a person; tests use it to see the
	// messages without an email server.
	extraSenders func(userID int64) []notify.Sender
}

// ---- Quota ----

// quotaInfo is how many requests an account has left.
type quotaInfo struct {
	Limit    int    `json:"limit"` // 0 = no limit
	Used     int    `json:"used"`
	Left     int    `json:"left"`
	Period   string `json:"period"`             // day or week
	ResetsAt string `json:"resetsAt,omitempty"` // when the oldest counted request stops counting
}

func quotaWindow(period string) time.Duration {
	if period == "day" {
		return 24 * time.Hour
	}
	return 7 * 24 * time.Hour
}

// requestQuota works out an account's quota: its own number if an
// administrator gave it one, else the default.
func (s *Server) requestQuota(userID int64) quotaInfo {
	o := s.requestOptions()
	q := quotaInfo{Limit: o.QuotaCount, Period: o.QuotaPeriod}
	var own int
	if err := s.db.QueryRow(`SELECT request_quota FROM users WHERE id = ?`, userID).Scan(&own); err == nil && own >= 0 {
		q.Limit = own
	}
	if q.Limit <= 0 {
		q.Limit = 0
		return q
	}
	now := time.Now().UTC()
	win := quotaWindow(q.Period)
	n, oldest, err := s.Requests.CountSince(userID, now.Add(-win).Format(time.RFC3339))
	if err != nil {
		slog.Warn("requests: count for the quota", "err", err)
		return q
	}
	q.Used = n
	q.Left = max(q.Limit-n, 0)
	if t, err := time.Parse(time.RFC3339, oldest); err == nil {
		q.ResetsAt = t.Add(win).UTC().Format(time.RFC3339)
	}
	return q
}

// quotaRefusal is what a person is told when they have no requests left.
func quotaRefusal(q quotaInfo, now time.Time) string {
	span := "in the last week"
	if q.Period == "day" {
		span = "in the last day"
	}
	msg := fmt.Sprintf("You've made %s %s, which is as many as you may.", plural(q.Limit, "request"), span)
	if t, err := time.Parse(time.RFC3339, q.ResetsAt); err == nil && t.After(now) {
		msg += " You can ask again " + inAbout(t.Sub(now)) + "."
	}
	return msg
}

// inAbout says a wait in plain words: "in about 3 hours".
func inAbout(d time.Duration) string {
	switch {
	case d < 90*time.Minute:
		return "in about an hour"
	case d < 36*time.Hour:
		return fmt.Sprintf("in about %d hours", int((d+30*time.Minute)/time.Hour))
	}
	return fmt.Sprintf("in about %d days", int((d+12*time.Hour)/(24*time.Hour)))
}

// ---- 4K ----

// fourKReady reports whether people may ask for a 4K copy of a kind of media:
// it is switched on, a profile is chosen and the folder for second copies is set.
func (s *Server) fourKReady(kind string) bool {
	o := s.requestOptions()
	if !o.FourK || o.FourKProfileID <= 0 {
		return false
	}
	if _, err := s.QualityRepo.Get(o.FourKProfileID); err != nil {
		return false
	}
	switch kind {
	case "movie":
		return s.movieCopiesRoot() != ""
	case "tv":
		return s.tvCopiesRoot() != ""
	}
	return false
}

// payloadFourK reports whether the add a request carries asks for the 4K copy.
func payloadFourK(payload []byte) bool {
	var v struct {
		FourK bool `json:"fourK"`
	}
	return json.Unmarshal(payload, &v) == nil && v.FourK
}

func payloadTMDBID(payload string) int {
	var v struct {
		TMDBID int `json:"tmdbId"`
	}
	_ = json.Unmarshal([]byte(payload), &v)
	return v.TMDBID
}

// describeLibraryTitle names a title that is in the library already, for a
// request for its 4K copy.
func (s *Server) describeLibraryTitle(kind string, tmdbID int) (requestInfo, bool) {
	switch kind {
	case "movie":
		if m, ok, _ := s.MovieRepo.GetByTMDBID(tmdbID); ok {
			title, poster := s.shownFace("movie", m.TMDBID, m.Title, m.PosterPath)
			return requestInfo{title: title, year: m.Year, poster: poster, inLibrary: true}, true
		}
	case "tv":
		if sr, ok, _ := s.MovieRepo.GetSeriesByTMDBID(tmdbID); ok {
			title, poster := s.shownFace("tv", sr.TMDBID, sr.Title, sr.PosterPath)
			return requestInfo{title: title, year: sr.Year, poster: poster, inLibrary: true}, true
		}
	}
	return requestInfo{}, false
}

// hasFourKCopy reports whether a title in the library already has a copy in
// the 4K quality.
func (s *Server) hasFourKCopy(kind string, tmdbID int) bool {
	want := s.requestOptions().FourKProfileID
	switch kind {
	case "movie":
		if m, ok, _ := s.MovieRepo.GetByTMDBID(tmdbID); ok {
			copies, _ := s.MovieRepo.ListCopies(m.ID)
			for _, c := range copies {
				if c.ProfileID == want {
					return true
				}
			}
		}
	case "tv":
		if sr, ok, _ := s.MovieRepo.GetSeriesByTMDBID(tmdbID); ok {
			copies, _ := s.MovieRepo.ListSeriesCopies(sr.ID)
			for _, c := range copies {
				if c.ProfileID == want {
					return true
				}
			}
		}
	}
	return false
}

// addFourKCopy adds the 4K second copy a request asked for and starts looking
// for it. The note says what happened when it is not a plain "added".
func (s *Server) addFourKCopy(req requests.Request) (note string, err error) {
	want := s.requestOptions().FourKProfileID
	tmdbID := payloadTMDBID(req.Payload)
	if !s.fourKReady(req.Kind) {
		return "", errors.New("4K requests are switched off, or the 4K quality or the folder for second copies isn't set. Check Settings > Accounts > Requests and reports and Settings > Library > Folders.")
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return "", err
	}
	switch req.Kind {
	case "movie":
		m, ok, _ := s.MovieRepo.GetByTMDBID(tmdbID)
		if !ok {
			return "", errors.New("The movie isn't in the library, so it has no 4K copy yet.")
		}
		if profiles.resolve(m.ProfileID).ID == want {
			return "The movie already uses the 4K quality, so no second copy was needed.", nil
		}
		c, err := s.MovieRepo.AddCopy(m.ID, want)
		if errors.Is(err, library.ErrCopyExists) {
			return "It already had a 4K copy.", nil
		}
		if err != nil {
			return "", err
		}
		_ = s.QueueRepo.LogActivity(m.ID, "added", fmt.Sprintf("%s: a second copy in %s was added", m.Title, profiles.resolve(c.ProfileID).Name))
		go s.searchMovieInBackground(c.ID)
	case "tv":
		sr, ok, _ := s.MovieRepo.GetSeriesByTMDBID(tmdbID)
		if !ok {
			return "", errors.New("The show isn't in the library, so it has no 4K copy yet.")
		}
		if profiles.resolveTV(sr.ProfileID).ID == want {
			return "The show already uses the 4K quality, so no second copy was needed.", nil
		}
		c, err := s.MovieRepo.AddSeriesCopy(sr.ID, want)
		if errors.Is(err, library.ErrCopyExists) {
			return "It already had a 4K copy.", nil
		}
		if err != nil {
			return "", err
		}
		_ = s.QueueRepo.LogSeriesActivity(sr.ID, "added", fmt.Sprintf("%s: a second copy in %s was added", sr.Title, profiles.resolve(c.ProfileID).Name))
		go s.searchSeriesCopyInBackground(c.ID)
	default:
		return "", errors.New("Only movies and shows have a 4K copy.")
	}
	return "", nil
}

// ---- Status and settings ----

// GET /api/requests/status: what the request pages need to know about the
// caller: requests left, whether 4K can be asked for, whether reports are on.
func (s *Server) handleRequestStatus(w http.ResponseWriter, r *http.Request) {
	uid, _ := requester(r)
	o := s.requestOptions()
	writeJSON(w, http.StatusOK, map[string]any{
		"quota":          s.requestQuota(uid),
		"fourK":          map[string]bool{"movie": s.fourKReady("movie"), "tv": s.fourKReady("tv")},
		"reports":        o.Reports,
		"notifyOn":       o.NotifyRequester,
		"asksToAdd":      s.mustRequest(r),
		"fourKLabel":     s.fourKProfileName(),
		"fourKProfileId": o.FourKProfileID,
	})
}

func (s *Server) fourKProfileName() string {
	o := s.requestOptions()
	if o.FourKProfileID <= 0 {
		return ""
	}
	p, err := s.QualityRepo.Get(o.FourKProfileID)
	if err != nil {
		return ""
	}
	return p.Name
}

type requestAccount struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
	// MustAsk is true for an account whose adds become requests.
	MustAsk bool `json:"mustAsk"`
	// Quota is the account's own limit: -1 follows the default, 0 is no limit.
	Quota int `json:"quota"`
}

// GET /api/requests/settings
func (s *Server) handleGetRequestSettings(w http.ResponseWriter, r *http.Request) {
	accounts := []requestAccount{}
	list, err := s.Auth.ListAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, a := range list {
		q := -1
		_ = s.db.QueryRow(`SELECT request_quota FROM users WHERE id = ?`, a.ID).Scan(&q)
		accounts = append(accounts, requestAccount{ID: a.ID, Name: a.DisplayName(), Admin: a.IsAdmin, MustAsk: !a.IsAdmin && !a.Permissions.AddDirect, Quota: q})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"options":  s.requestOptions(),
		"accounts": accounts,
		"folders":  map[string]bool{"movie": s.movieCopiesRoot() != "", "tv": s.tvCopiesRoot() != ""},
	})
}

// PUT /api/requests/settings: any of the options, and quotas {"<account id>": n}.
func (s *Server) handlePutRequestSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NotifyRequester *bool          `json:"notifyRequester"`
		QuotaCount      *int           `json:"quotaCount"`
		QuotaPeriod     *string        `json:"quotaPeriod"`
		FourK           *bool          `json:"fourK"`
		FourKProfileID  *int64         `json:"fourKProfileId"`
		Reports         *bool          `json:"reports"`
		Quotas          map[string]int `json:"quotas"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	o := s.requestOptions()
	wasNotifying := o.NotifyRequester
	if body.QuotaCount != nil {
		if *body.QuotaCount < 0 || *body.QuotaCount > 1000 {
			writeError(w, http.StatusBadRequest, "Choose a number of requests from 0 (no limit) to 1000.")
			return
		}
		o.QuotaCount = *body.QuotaCount
	}
	if body.QuotaPeriod != nil {
		if *body.QuotaPeriod != "day" && *body.QuotaPeriod != "week" {
			writeError(w, http.StatusBadRequest, `Choose "day" or "week" for the limit.`)
			return
		}
		o.QuotaPeriod = *body.QuotaPeriod
	}
	if body.NotifyRequester != nil {
		o.NotifyRequester = *body.NotifyRequester
	}
	if body.Reports != nil {
		o.Reports = *body.Reports
	}
	if body.FourKProfileID != nil {
		if *body.FourKProfileID < 0 {
			writeError(w, http.StatusBadRequest, "Pick a quality profile for 4K copies.")
			return
		}
		if err := s.checkProfileChoice(*body.FourKProfileID); err != nil {
			writeError(w, http.StatusBadRequest, "Pick a quality profile that exists.")
			return
		}
		o.FourKProfileID = *body.FourKProfileID
	}
	if body.FourK != nil {
		o.FourK = *body.FourK
	}
	if o.FourK && o.FourKProfileID <= 0 {
		writeError(w, http.StatusBadRequest, "Pick the quality profile for 4K copies first, such as 4K & over.")
		return
	}
	for id, n := range body.Quotas {
		uid, err := strconv.ParseInt(id, 10, 64)
		if err != nil || uid <= 0 || n < -1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "A person's limit is -1 (follow the default), 0 (no limit) or a number up to 1000.")
			return
		}
		if _, err := s.db.Exec(`UPDATE users SET request_quota = ? WHERE id = ?`, n, uid); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if o.NotifyRequester && !wasNotifying {
		s.markAvailableQuietly() // titles that arrived before the messages were on are not announced
	}
	if err := s.saveRequestOptions(o); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.handleGetRequestSettings(w, r)
}

// ---- Messages to the person ----

// tellPerson sends a message to the account that asked: by email (through the
// email server set up under Settings > Connections > Notifications, to the
// address on the account) and to the devices that account allowed
// notifications on. Nothing is sent while the option is off, or when the person
// switched these messages off.
func (s *Server) tellPerson(userID int64, title, message, path string) {
	if userID <= 0 || !s.requestOptions().NotifyRequester {
		return
	}
	pref := 1
	if err := s.db.QueryRow(`SELECT notify_requests FROM users WHERE id = ?`, userID).Scan(&pref); err != nil || pref == 0 {
		return
	}
	u, err := s.Auth.GetUser(userID)
	if err != nil || u.Disabled {
		return
	}
	links := s.publicLinks()
	ev := notify.ComposeText("request", title, message, links, time.Now())
	if links.Base != "" {
		ev.LinkURL = links.Base + path
		ev.SettingsURL = links.Base + "/settings/profile"
	}
	s.background(func() {
		senders := s.personSenders(userID, strings.TrimSpace(u.Email))
		if hook := s.portal.extraSenders; hook != nil {
			senders = append(senders, hook(userID)...)
		}
		if len(senders) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		for _, err := range notify.NotifyAll(ctx, senders, ev) {
			slog.Warn("request message not delivered", "user", userID, "err", err)
		}
	})
}

// personSenders are the ways to reach one account.
func (s *Server) personSenders(userID int64, email string) []notify.Sender {
	var out []notify.Sender
	if email != "" {
		if targets, err := s.NotifyRepo.List(); err == nil {
			for _, t := range targets {
				if t.Type != notify.TargetEmail || !t.Enabled {
					continue
				}
				if sender, err := notify.EmailSenderTo(t, email); err == nil {
					out = append(out, sender)
					break // one email is enough
				}
			}
		}
	}
	rows, err := s.db.Query(`SELECT subscription FROM user_push_devices WHERE user_id = ?`, userID)
	if err != nil {
		return out
	}
	var encs []string
	for rows.Next() {
		var enc string
		if rows.Scan(&enc) == nil {
			encs = append(encs, enc)
		}
	}
	rows.Close()
	for _, enc := range encs {
		raw, err := s.box.Decrypt(enc)
		if err != nil {
			continue
		}
		if sender, err := notify.PushSenderFor(raw); err == nil {
			out = append(out, sender)
		}
	}
	return out
}

// requestPath is where a request's title is, for the link in a message.
func requestPath(req requests.Request) string {
	switch req.Kind {
	case "movie":
		if id := payloadTMDBID(req.Payload); id > 0 {
			return "/title/" + strconv.Itoa(id)
		}
	case "tv":
		if id := payloadTMDBID(req.Payload); id > 0 {
			return "/show/" + strconv.Itoa(id)
		}
	}
	return "/queue"
}

func requestLabel(req requests.Request) string {
	name := requestName(req)
	if req.FourK {
		name += " in 4K"
	}
	return name
}

// GET /api/auth/request-notices
func (s *Server) handleGetRequestNotices(w http.ResponseWriter, r *http.Request) {
	uid, _ := requester(r)
	pref := 1
	_ = s.db.QueryRow(`SELECT notify_requests FROM users WHERE id = ?`, uid).Scan(&pref)
	email := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		if fresh, err := s.Auth.GetUser(u.ID); err == nil {
			email = strings.TrimSpace(fresh.Email)
		}
	}
	emailReady := false
	if targets, err := s.NotifyRepo.List(); err == nil {
		for _, t := range targets {
			if t.Type == notify.TargetEmail && t.Enabled {
				emailReady = true
			}
		}
	}
	type device struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		CreatedAt string `json:"createdAt"`
	}
	devices := []device{}
	if rows, err := s.db.Query(`SELECT id, name, created_at FROM user_push_devices WHERE user_id = ? ORDER BY id`, uid); err == nil {
		for rows.Next() {
			var d device
			if rows.Scan(&d.ID, &d.Name, &d.CreatedAt) == nil {
				devices = append(devices, d)
			}
		}
		rows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": pref != 0, "serverOn": s.requestOptions().NotifyRequester,
		"hasEmail": email != "", "emailReady": emailReady, "devices": devices,
	})
}

// PUT /api/auth/request-notices {"enabled": bool}
func (s *Server) handlePutRequestNotices(w http.ResponseWriter, r *http.Request) {
	uid, _ := requester(r)
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "Say whether you want these messages.")
		return
	}
	if _, err := s.db.Exec(`UPDATE users SET notify_requests = ? WHERE id = ?`, b2i(*body.Enabled), uid); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.handleGetRequestNotices(w, r)
}

const maxNoticeDevices = 5

// POST /api/auth/request-notices/devices {"subscription": "<json>", "name": "..."}
func (s *Server) handleAddNoticeDevice(w http.ResponseWriter, r *http.Request) {
	uid, _ := requester(r)
	var body struct {
		Subscription string `json:"subscription"`
		Name         string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	sub, err := notify.ParsePushSubscription(body.Subscription)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM user_push_devices WHERE user_id = ?`, uid).Scan(&n)
	if n >= maxNoticeDevices {
		writeError(w, http.StatusConflict, "That is as many devices as can get these messages. Remove one first.")
		return
	}
	enc, err := s.box.Encrypt(body.Subscription)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	// The same browser asking again replaces its old entry.
	rows, _ := s.db.Query(`SELECT id, subscription FROM user_push_devices WHERE user_id = ?`, uid)
	var same []int64
	if rows != nil {
		for rows.Next() {
			var id int64
			var old string
			if rows.Scan(&id, &old) == nil {
				if raw, err := s.box.Decrypt(old); err == nil {
					if o, err := notify.ParsePushSubscription(raw); err == nil && o.Endpoint == sub.Endpoint {
						same = append(same, id)
					}
				}
			}
		}
		rows.Close()
	}
	for _, id := range same {
		_, _ = s.db.Exec(`DELETE FROM user_push_devices WHERE id = ?`, id)
	}
	if _, err := s.db.Exec(`INSERT INTO user_push_devices (user_id, name, subscription) VALUES (?, ?, ?)`, uid, name, enc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.handleGetRequestNotices(w, r)
}

// DELETE /api/auth/request-notices/devices/{id}
func (s *Server) handleRemoveNoticeDevice(w http.ResponseWriter, r *http.Request) {
	uid, _ := requester(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a device.")
		return
	}
	res, err := s.db.Exec(`DELETE FROM user_push_devices WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "That device isn't on your list.")
		return
	}
	s.handleGetRequestNotices(w, r)
}

// ---- Ready to watch ----

// requestAvailableInterval is how often approved requests are checked against
// the library.
const requestAvailableInterval = 10 * time.Minute

// requestReady reports whether what an approved request asked for has files in
// the library. ok is false for a kind that isn't checked (music).
func (s *Server) requestReady(req requests.Request) (ready, ok bool) {
	switch req.Kind {
	case "movie":
		m, found, _ := s.MovieRepo.GetByTMDBID(payloadTMDBID(req.Payload))
		if !found {
			return false, true
		}
		if !req.FourK {
			return m.Status == library.StatusDownloaded, true
		}
		copies, _ := s.MovieRepo.ListCopies(m.ID)
		for _, c := range copies {
			if c.ProfileID == s.requestOptions().FourKProfileID && c.Status == library.StatusDownloaded {
				return true, true
			}
		}
		return false, true
	case "tv":
		sr, found, _ := s.MovieRepo.GetSeriesByTMDBID(payloadTMDBID(req.Payload))
		if !found {
			return false, true
		}
		if !req.FourK {
			return sr.DownloadedCount > 0, true
		}
		copies, _ := s.MovieRepo.ListSeriesCopies(sr.ID)
		for _, c := range copies {
			if c.ProfileID == s.requestOptions().FourKProfileID && c.DownloadedCount > 0 {
				return true, true
			}
		}
		return false, true
	case "book":
		var v struct {
			OLKey string `json:"olKey"`
		}
		_ = json.Unmarshal([]byte(req.Payload), &v)
		b, found, _ := s.BookRepo.GetByKey(strings.TrimSpace(v.OLKey))
		return found && (b.EbookStatus == books.StatusDownloaded || b.AudioStatus == books.StatusDownloaded), true
	}
	return false, false
}

// requestAvailableJob notices when an approved request's title is in the
// library, and tells the person who asked (when messages are on).
func (s *Server) requestAvailableJob(ctx context.Context) {
	pending, err := s.Requests.Unannounced()
	if err != nil || len(pending) == 0 {
		return
	}
	for _, req := range pending {
		if ctx.Err() != nil {
			return
		}
		ready, checked := s.requestReady(req)
		if !checked {
			_ = s.Requests.SetAvailable(req.ID, "skipped")
			continue
		}
		if !ready {
			continue
		}
		if err := s.Requests.SetAvailable(req.ID, time.Now().UTC().Format(time.RFC3339)); err != nil {
			slog.Warn("requests: mark available", "request", req.ID, "err", err)
			continue
		}
		label := requestLabel(req)
		s.tellPerson(req.RequestedBy, label+" is ready to watch", label+" is in the library now.", requestPath(req))
	}
}

// markAvailableQuietly stamps the approved requests whose titles are already in
// the library, without telling anyone: it runs when the messages are switched
// on, so what arrived long ago isn't announced.
func (s *Server) markAvailableQuietly() {
	pending, err := s.Requests.Unannounced()
	if err != nil {
		return
	}
	for _, req := range pending {
		if ready, _ := s.requestReady(req); ready {
			_ = s.Requests.SetAvailable(req.ID, "skipped")
		}
	}
}
