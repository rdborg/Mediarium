// Package qbit hands torrents to an external download client that speaks the
// qBittorrent Web API (version 2): qBittorrent itself, and programs that
// copy its API such as RDT-Client and Decypharr. The client downloads the
// torrent into its own folder; Mediarium asks how it is doing, and once it
// is finished imports the files from that folder without moving them, so
// the client can keep seeding.
package qbit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/rdborg/mediarium/internal/bencsafe"
	"github.com/rdborg/mediarium/internal/netguard"
)

// Kinds of client, as people know them. All but Transmission speak the same
// API (qBittorrent's); for those the kind only changes the names and hints
// shown. Transmission has its own protocol (see transmission.go).
const (
	QBittorrent  = "qbittorrent"
	RDTClient    = "rdtclient"
	Decypharr    = "decypharr"
	Transmission = "transmission"
)

// Label is a kind's name as people know it.
func Label(kind string) string {
	switch kind {
	case QBittorrent:
		return "qBittorrent"
	case RDTClient:
		return "RDT-Client"
	case Decypharr:
		return "Decypharr"
	case Transmission:
		return "Transmission"
	}
	return "your download client"
}

// ValidKind reports whether kind is one of the clients above.
func ValidKind(kind string) bool {
	return kind == QBittorrent || kind == RDTClient || kind == Decypharr || kind == Transmission
}

// ErrAuth means the client refused the user name or password.
var ErrAuth = errors.New("the download client refused the user name or password")

// Torrent is one torrent as the client lists it.
type Torrent struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	Progress    float64 `json:"progress"` // 0..1
	State       string  `json:"state"`
	ContentPath string  `json:"content_path"` // the file, or the torrent's folder, as the client sees it
	SavePath    string  `json:"save_path"`
	Size        int64   `json:"size"`
	Seeds       int     `json:"num_seeds"`
	Tags        string  `json:"tags"` // comma separated
	Category    string  `json:"category"`
	// What the Activity page shows about a torrent in the client.
	DLSpeed      int64   `json:"dlspeed"`        // bytes per second down
	UPSpeed      int64   `json:"upspeed"`        // bytes per second up
	Leeches      int     `json:"num_leechs"`     // peers connected that are still downloading
	SwarmSeeds   int     `json:"num_complete"`   // seeds in the swarm, as far as the tracker knows
	SwarmLeeches int     `json:"num_incomplete"` // downloaders in the swarm
	ETA          int64   `json:"eta"`            // seconds left; 8640000 means never
	Ratio        float64 `json:"ratio"`
	AddedOn      int64   `json:"added_on"`      // unix seconds
	LastActivity int64   `json:"last_activity"` // unix seconds
	Downloaded   int64   `json:"downloaded"`
	Uploaded     int64   `json:"uploaded"`
}

// Done reports whether the client has the whole torrent: everything is
// downloaded and the torrent is seeding, queued to seed or stopped after
// finishing. A torrent still being checked or moved isn't done yet.
func (t Torrent) Done() bool {
	if t.Progress < 1 {
		return false
	}
	switch t.State {
	case "uploading", "stalledUP", "pausedUP", "stoppedUP", "queuedUP", "forcedUP":
		return true
	}
	return false
}

// Paused reports whether the torrent is paused (stopped, on qBittorrent 5)
// before it has everything.
func (t Torrent) Paused() bool {
	return t.State == "pausedDL" || t.State == "stoppedDL"
}

// Waiting reports whether the torrent is in line or being checked in the
// client, not downloading yet: the client's own limit on active downloads has
// not let it start, or it is checking the files on disk. That is the client's
// delay, not a torrent that nobody shares.
func (t Torrent) Waiting() bool {
	switch t.State {
	case "queuedDL", "checkingDL", "checkingUP", "checkingResumeData", "allocating", "moving":
		return true
	}
	return false
}

// Failed reports whether the client gave up on the torrent.
func (t Torrent) Failed() bool {
	return t.State == "error" || t.State == "missingFiles"
}

// HasTag reports whether the torrent carries tag.
func (t Torrent) HasTag(tag string) bool {
	for _, have := range strings.Split(t.Tags, ",") {
		if strings.TrimSpace(have) == tag {
			return true
		}
	}
	return false
}

// Content is where the torrent's files are, as the client sees them: the
// file of a single-file torrent, or the torrent's own folder. Older
// clients that don't report content_path get the save folder and name
// joined together.
func (t Torrent) Content() string {
	if t.ContentPath != "" {
		return t.ContentPath
	}
	if t.SavePath == "" || t.Name == "" {
		return ""
	}
	sep := "/"
	if strings.Contains(t.SavePath, `\`) && !strings.Contains(t.SavePath, "/") {
		sep = `\`
	}
	return strings.TrimRight(t.SavePath, `/\`) + sep + t.Name
}

// Client talks to one download client. It signs in when it has to and
// keeps the session cookie.
type Client struct {
	base       string
	user, pass string
	http       *http.Client
	mu         sync.Mutex // one sign-in at a time
}

// New returns a client for the Web UI at base (for example
// http://192.168.1.10:8080). hc is copied and given its own cookie jar.
func New(base, user, pass string, hc *http.Client) *Client {
	c := *hc
	c.Jar, _ = cookiejar.New(nil)
	return &Client{base: strings.TrimRight(strings.TrimSpace(base), "/"), user: user, pass: pass, http: &c}
}

// Login signs in. A client that lets this address in without a password
// (qBittorrent's "bypass authentication" options) answers Ok to any
// name.
func (c *Client) Login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	form := url.Values{"username": {c.user}, "password": {c.pass}}
	req, err := c.request(ctx, http.MethodPost, "/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach the download client: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("the download client blocked Mediarium after too many wrong passwords. Wait a while, or restart the client: %w", ErrAuth)
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrAuth
	case resp.StatusCode >= 300:
		return fmt.Errorf("the download client answered HTTP %d when signing in", resp.StatusCode)
	case strings.TrimSpace(string(body)) == "Fails.":
		return ErrAuth
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, p string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+p, body)
	if err != nil {
		return nil, fmt.Errorf("bad download client address: %w", netguard.CleanError(err))
	}
	// qBittorrent checks that Referer, when sent, is its own address.
	req.Header.Set("Referer", c.base)
	return req, nil
}

// call sends a request, signing in and trying once more when the client
// says the session isn't valid (403).
func (c *Client) call(ctx context.Context, method, p string, body []byte, contentType string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		req, err := c.request(ctx, method, p, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("couldn't reach the download client: %w", netguard.CleanError(err))
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden && attempt == 0 {
			if err := c.Login(ctx); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrAuth
		}
		if resp.StatusCode >= 300 {
			endpoint, _, _ := strings.Cut(path.Base(p), "?")
			return nil, fmt.Errorf("the download client answered HTTP %d for %s", resp.StatusCode, endpoint)
		}
		if readErr != nil {
			return nil, fmt.Errorf("read the download client's answer: %w", readErr)
		}
		return data, nil
	}
}

// Version is the client's version, as the Test button shows it.
func (c *Client) Version(ctx context.Context) (string, error) {
	if err := c.Login(ctx); err != nil {
		return "", err
	}
	data, err := c.call(ctx, http.MethodGet, "/api/v2/app/version", nil, "")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// AddOptions is one torrent to hand over: a magnet link or the bytes of a
// .torrent file.
type AddOptions struct {
	Magnet      string
	TorrentFile []byte
	Category    string
	Tag         string
	// Hash is the torrent's info hash when it is known up front. It lets the
	// share limits be set after the add for clients that ignore them in it.
	Hash string
	// Share is how long the client should seed this torrent. Zero in both
	// fields means "leave the client's own defaults alone".
	Share ShareLimits
}

// ShareLimits is when the client itself should stop seeding one torrent.
// A zero field means no limit of that kind.
//
// NoSeed means the torrent is not to be shared at all: the client is told to
// stop it at ratio 0, which is right when it finishes.
type ShareLimits struct {
	Ratio  float64
	Time   time.Duration
	NoSeed bool
}

// Set reports whether there is anything to send.
func (l ShareLimits) Set() bool { return l.NoSeed || l.Ratio > 0 || l.Time > 0 }

// ratioValue and minutesValue are the numbers qBittorrent wants: -1 is no
// limit of that kind, the time is in whole minutes (at least one).
func (l ShareLimits) ratioValue() string {
	if l.NoSeed {
		return "0"
	}
	if l.Ratio <= 0 {
		return "-1"
	}
	return strconv.FormatFloat(l.Ratio, 'f', -1, 64)
}

func (l ShareLimits) minutesValue() string {
	if l.NoSeed {
		return "-1"
	}
	if l.Time <= 0 {
		return "-1"
	}
	m := int(math.Ceil(l.Time.Minutes()))
	if m < 1 {
		m = 1
	}
	return strconv.Itoa(m)
}

// Add hands a torrent to the client.
func (c *Client) Add(ctx context.Context, o AddOptions) error {
	if o.Magnet == "" && len(o.TorrentFile) == 0 {
		return errors.New("there is no magnet link or torrent file to add")
	}
	if o.Category != "" {
		c.createCategory(ctx, o.Category)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if o.Magnet != "" {
		_ = mw.WriteField("urls", o.Magnet)
	} else {
		part, err := mw.CreateFormFile("torrents", "release.torrent")
		if err != nil {
			return err
		}
		_, _ = part.Write(o.TorrentFile)
	}
	if o.Category != "" {
		_ = mw.WriteField("category", o.Category)
	}
	if o.Tag != "" {
		_ = mw.WriteField("tags", o.Tag)
	}
	if o.Share.Set() {
		_ = mw.WriteField("ratioLimit", o.Share.ratioValue())
		_ = mw.WriteField("seedingTimeLimit", o.Share.minutesValue())
	}
	_ = mw.Close()
	data, err := c.call(ctx, http.MethodPost, "/api/v2/torrents/add", buf.Bytes(), mw.FormDataContentType())
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) == "Fails." {
		return errors.New("the download client didn't take the torrent")
	}
	if o.Share.Set() && o.Hash != "" {
		// Older clients and the programs that copy the API may ignore the
		// limits in the add call, so set them on the torrent too. A failure
		// here never fails the grab.
		if err := c.SetShareLimits(ctx, o.Hash, o.Share); err != nil {
			slog.Debug("download client: couldn't set the share limits", "err", err)
		}
	}
	return nil
}

// SetShareLimits gives one torrent its own seeding limits. The client decides
// what happens when they are met (that is its own global setting).
func (c *Client) SetShareLimits(ctx context.Context, hash string, l ShareLimits) error {
	return c.post(ctx, "/api/v2/torrents/setShareLimits", url.Values{
		"hashes":                   {hash},
		"ratioLimit":               {l.ratioValue()},
		"seedingTimeLimit":         {l.minutesValue()},
		"inactiveSeedingTimeLimit": {"-2"},
	})
}

// createCategory makes sure the category exists. Clients that already have
// it, or don't do categories, answer with an error that doesn't matter.
func (c *Client) createCategory(ctx context.Context, category string) {
	form := url.Values{"category": {category}}
	_, _ = c.call(ctx, http.MethodPost, "/api/v2/torrents/createCategory", []byte(form.Encode()), "application/x-www-form-urlencoded")
}

func (c *Client) info(ctx context.Context, q url.Values) ([]Torrent, error) {
	data, err := c.call(ctx, http.MethodGet, "/api/v2/torrents/info?"+q.Encode(), nil, "")
	if err != nil {
		return nil, err
	}
	var list []Torrent
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("the download client sent a torrent list Mediarium couldn't read: %w", err)
	}
	return list, nil
}

// List lists every torrent in the client, or only those in category when it is
// not empty.
func (c *Client) List(ctx context.Context, category string) ([]Torrent, error) {
	q := url.Values{}
	if category != "" {
		q.Set("category", category)
	}
	list, err := c.info(ctx, q)
	if err != nil {
		return nil, err
	}
	if category == "" {
		return list, nil
	}
	out := list[:0] // the filter is checked here too, for clients that ignore it
	for _, t := range list {
		if t.Category == category {
			out = append(out, t)
		}
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, p string, form url.Values) error {
	_, err := c.call(ctx, http.MethodPost, p, []byte(form.Encode()), "application/x-www-form-urlencoded")
	return err
}

// Pause stops a torrent (qBittorrent 5 calls it stop, 4 calls it pause).
func (c *Client) Pause(ctx context.Context, hash string) error {
	form := url.Values{"hashes": {hash}}
	if err := c.post(ctx, "/api/v2/torrents/stop", form); err != nil {
		return c.post(ctx, "/api/v2/torrents/pause", form)
	}
	return nil
}

// Resume starts a stopped torrent again (start on qBittorrent 5, resume on 4).
func (c *Client) Resume(ctx context.Context, hash string) error {
	form := url.Values{"hashes": {hash}}
	if err := c.post(ctx, "/api/v2/torrents/start", form); err != nil {
		return c.post(ctx, "/api/v2/torrents/resume", form)
	}
	return nil
}

// Recheck makes the client check the torrent's files again.
func (c *Client) Recheck(ctx context.Context, hash string) error {
	return c.post(ctx, "/api/v2/torrents/recheck", url.Values{"hashes": {hash}})
}

// InfoByTag lists the torrents that carry tag. The list is checked here
// too, since some clients ignore the filter and list everything.
func (c *Client) InfoByTag(ctx context.Context, tag string) ([]Torrent, error) {
	list, err := c.info(ctx, url.Values{"tag": {tag}})
	if err != nil {
		return nil, err
	}
	out := list[:0]
	for _, t := range list {
		if t.HasTag(tag) {
			out = append(out, t)
		}
	}
	return out, nil
}

// Info returns the torrent with this hash, or ok false when the client
// doesn't have it.
func (c *Client) Info(ctx context.Context, hash string) (t Torrent, ok bool, err error) {
	list, err := c.info(ctx, url.Values{"hashes": {hash}})
	if err != nil {
		return Torrent{}, false, err
	}
	for _, t := range list {
		if strings.EqualFold(t.Hash, hash) {
			return t, true, nil
		}
	}
	return Torrent{}, false, nil
}

// Delete removes a torrent from the client, with its files when deleteFiles
// is true.
func (c *Client) Delete(ctx context.Context, hash string, deleteFiles bool) error {
	form := url.Values{"hashes": {hash}, "deleteFiles": {fmt.Sprint(deleteFiles)}}
	_, err := c.call(ctx, http.MethodPost, "/api/v2/torrents/delete", []byte(form.Encode()), "application/x-www-form-urlencoded")
	return err
}

// InfoHash is the hash the client will list a torrent under (lower-case
// hex of the v1 info hash), worked out from a magnet link or a .torrent
// file. It returns "" when it can't be known up front (a v2-only magnet,
// a damaged file); the torrent is then found by its tag.
func InfoHash(magnet string, torrentFile []byte) string {
	if magnet != "" {
		m, err := metainfo.ParseMagnetUri(magnet)
		if err != nil || m.InfoHash == (metainfo.Hash{}) {
			return ""
		}
		return m.InfoHash.HexString()
	}
	// The file comes from an indexer and the library's decoder has no limit on
	// nesting, so a crafted one is turned away before it gets there.
	if err := bencsafe.Check(torrentFile); err != nil {
		return ""
	}
	mi, err := metainfo.Load(bytes.NewReader(torrentFile))
	if err != nil {
		return ""
	}
	return mi.HashInfoBytes().HexString()
}
