package qbit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/rdborg/mediarium/internal/netguard"
)

// API is what Mediarium asks of a download client. The qBittorrent Web API
// client (and the programs that copy it) and the Transmission client both
// have it, and both describe a torrent with the Torrent type above.
type API interface {
	Login(ctx context.Context) error
	Version(ctx context.Context) (string, error)
	Add(ctx context.Context, o AddOptions) error
	List(ctx context.Context, category string) ([]Torrent, error)
	Pause(ctx context.Context, hash string) error
	Resume(ctx context.Context, hash string) error
	Recheck(ctx context.Context, hash string) error
	InfoByTag(ctx context.Context, tag string) ([]Torrent, error)
	Info(ctx context.Context, hash string) (t Torrent, ok bool, err error)
	Delete(ctx context.Context, hash string, deleteFiles bool) error
}

// NewAPI returns the client for a kind: Transmission has its own protocol,
// every other kind speaks the qBittorrent one.
func NewAPI(kind, base, user, pass string, hc *http.Client) API {
	if kind == Transmission {
		return NewTransmission(base, user, pass, hc)
	}
	return New(base, user, pass, hc)
}

var (
	_ API = (*Client)(nil)
	_ API = (*TransmissionClient)(nil)
)

// TransmissionClient talks to Transmission's RPC (its Web UI address, for
// example http://192.168.1.10:9091). Categories and tags become Transmission
// labels, which need Transmission 3.00 or newer.
type TransmissionClient struct {
	rpc        string
	user, pass string
	http       *http.Client
	mu         sync.Mutex // the session id
	session    string
}

// NewTransmission returns a client for the Transmission at base. The address
// may be the Web UI's (http://host:9091), or end in /transmission/rpc.
func NewTransmission(base, user, pass string, hc *http.Client) *TransmissionClient {
	c := *hc
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	rpc := base
	switch {
	case strings.HasSuffix(base, "/rpc"):
	case strings.HasSuffix(base, "/transmission/web"):
		rpc = strings.TrimSuffix(base, "/web") + "/rpc"
	case strings.HasSuffix(base, "/transmission"):
		rpc = base + "/rpc"
	default:
		rpc = base + "/transmission/rpc"
	}
	return &TransmissionClient{rpc: rpc, user: user, pass: pass, http: &c}
}

type tmRequest struct {
	Method    string `json:"method"`
	Arguments any    `json:"arguments,omitempty"`
}

type tmResponse struct {
	Result    string          `json:"result"`
	Arguments json.RawMessage `json:"arguments"`
}

// call sends one RPC request. Transmission answers 409 with a session id the
// first time (and when it changes): the request is sent again with it.
func (c *TransmissionClient) call(ctx context.Context, method string, args any, out any) error {
	body, err := json.Marshal(tmRequest{Method: method, Arguments: args})
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpc, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("bad download client address: %w", netguard.CleanError(err))
		}
		req.Header.Set("Content-Type", "application/json")
		if c.user != "" || c.pass != "" {
			req.SetBasicAuth(c.user, c.pass)
		}
		c.mu.Lock()
		sid := c.session
		c.mu.Unlock()
		if sid != "" {
			req.Header.Set("X-Transmission-Session-Id", sid)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("couldn't reach the download client: %w", netguard.CleanError(err))
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusConflict && attempt < 2:
			if id := resp.Header.Get("X-Transmission-Session-Id"); id != "" {
				c.mu.Lock()
				c.session = id
				c.mu.Unlock()
				continue
			}
			return errors.New("the download client answered HTTP 409 without a session id. Check that the address is Transmission's Web UI address")
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return ErrAuth
		case resp.StatusCode == http.StatusMisdirectedRequest:
			// Transmission 4 answers 421 when the address Mediarium used is not in its
			// host whitelist (rpc-host-whitelist).
			return errors.New("Transmission refused the address Mediarium used (HTTP 421). Add that host name to rpc-host-whitelist in Transmission's settings, or turn the whitelist off")
		case resp.StatusCode >= 300:
			return fmt.Errorf("the download client answered HTTP %d for %s", resp.StatusCode, method)
		case readErr != nil:
			return fmt.Errorf("read the download client's answer: %w", readErr)
		}
		var r tmResponse
		if err := json.Unmarshal(data, &r); err != nil {
			return errors.New("the address didn't answer like Transmission. Check that it is Transmission's Web UI address, for example http://192.168.1.10:9091")
		}
		if r.Result != "success" {
			return fmt.Errorf("Transmission said: %s", r.Result)
		}
		if out != nil && len(r.Arguments) > 0 {
			if err := json.Unmarshal(r.Arguments, out); err != nil {
				return fmt.Errorf("read the download client's answer: %w", err)
			}
		}
		return nil
	}
}

// Login checks the address and the sign-in.
func (c *TransmissionClient) Login(ctx context.Context) error {
	return c.call(ctx, "session-get", map[string]any{"fields": []string{"version"}}, nil)
}

// Version is Transmission's version, as the Test button shows it.
func (c *TransmissionClient) Version(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := c.call(ctx, "session-get", map[string]any{"fields": []string{"version"}}, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Version), nil
}

// Add hands a torrent to Transmission, with the category and the tag as labels.
func (c *TransmissionClient) Add(ctx context.Context, o AddOptions) error {
	if o.Magnet == "" && len(o.TorrentFile) == 0 {
		return errors.New("there is no magnet link or torrent file to add")
	}
	args := map[string]any{"paused": false}
	if o.Magnet != "" {
		args["filename"] = o.Magnet
	} else {
		args["metainfo"] = base64.StdEncoding.EncodeToString(o.TorrentFile)
	}
	var labels []string
	for _, l := range []string{o.Category, o.Tag} {
		if l = strings.TrimSpace(l); l != "" {
			labels = append(labels, l)
		}
	}
	if len(labels) > 0 {
		args["labels"] = labels
	}
	if err := c.call(ctx, "torrent-add", args, nil); err != nil {
		return err
	}
	// Transmission has a ratio limit per torrent but no total seeding time
	// (its idle limit is something else), so only the ratio is passed on.
	// A torrent that is not to be shared at all gets ratio 0, which it has
	// reached the moment it is done.
	if (o.Share.Ratio > 0 || o.Share.NoSeed) && o.Hash != "" {
		err := c.call(ctx, "torrent-set", map[string]any{
			"ids": []string{o.Hash}, "seedRatioLimit": o.Share.Ratio, "seedRatioMode": 1,
		}, nil)
		if err != nil {
			slog.Debug("download client: couldn't set the share ratio", "err", err)
		}
	}
	return nil
}

var tmFields = []string{
	"hashString", "name", "percentDone", "status", "error", "errorString", "downloadDir",
	"totalSize", "sizeWhenDone", "peersSendingToUs", "peersGettingFromUs", "peersConnected",
	"rateDownload", "rateUpload", "eta", "uploadRatio", "addedDate", "activityDate",
	"downloadedEver", "uploadedEver", "labels", "trackerStats",
}

type tmTorrent struct {
	Hash         string   `json:"hashString"`
	Name         string   `json:"name"`
	PercentDone  float64  `json:"percentDone"`
	Status       int      `json:"status"`
	Error        int      `json:"error"`
	ErrorString  string   `json:"errorString"`
	DownloadDir  string   `json:"downloadDir"`
	TotalSize    int64    `json:"totalSize"`
	SizeWhenDone int64    `json:"sizeWhenDone"`
	SendingToUs  int      `json:"peersSendingToUs"`
	GettingFrom  int      `json:"peersGettingFromUs"`
	Connected    int      `json:"peersConnected"`
	RateDown     int64    `json:"rateDownload"`
	RateUp       int64    `json:"rateUpload"`
	ETA          int64    `json:"eta"`
	Ratio        float64  `json:"uploadRatio"`
	AddedDate    int64    `json:"addedDate"`
	ActivityDate int64    `json:"activityDate"`
	DownloadedEv int64    `json:"downloadedEver"`
	UploadedEver int64    `json:"uploadedEver"`
	Labels       []string `json:"labels"`
	Trackers     []struct {
		Seeders  int `json:"seederCount"`
		Leechers int `json:"leecherCount"`
	} `json:"trackerStats"`
}

// Transmission's torrent status numbers.
const (
	tmStopped      = 0
	tmCheckWait    = 1
	tmChecking     = 2
	tmDownloadWait = 3
	tmDownloading  = 4
	tmSeedWait     = 5
	tmSeeding      = 6
)

// state puts Transmission's status into the words the rest of Mediarium
// reads (they are qBittorrent's).
func (t tmTorrent) state() string {
	// error 3 is a local error (disk full, files missing); 1 and 2 are
	// trackers having a bad moment, which don't stop a torrent.
	if t.Error == 3 {
		return "error"
	}
	done := t.PercentDone >= 1
	switch t.Status {
	case tmStopped:
		if done {
			return "pausedUP"
		}
		return "pausedDL"
	case tmCheckWait, tmChecking:
		if done {
			return "checkingUP"
		}
		return "checkingDL"
	case tmDownloadWait:
		return "queuedDL"
	case tmDownloading:
		if t.Connected == 0 || (t.SendingToUs == 0 && t.RateDown == 0) {
			return "stalledDL"
		}
		return "downloading"
	case tmSeedWait:
		return "queuedUP"
	case tmSeeding:
		if t.RateUp == 0 {
			return "stalledUP"
		}
		return "uploading"
	}
	return "unknown"
}

func (t tmTorrent) torrent(category string) Torrent {
	size := t.SizeWhenDone
	if size == 0 {
		size = t.TotalSize
	}
	out := Torrent{
		Hash: strings.ToLower(t.Hash), Name: t.Name, Progress: t.PercentDone, State: t.state(),
		SavePath: t.DownloadDir, Size: size, Seeds: t.SendingToUs, Tags: strings.Join(t.Labels, ","),
		DLSpeed: t.RateDown, UPSpeed: t.RateUp, Leeches: t.GettingFrom, ETA: t.ETA, Ratio: t.Ratio,
		AddedOn: t.AddedDate, LastActivity: t.ActivityDate, Downloaded: t.DownloadedEv, Uploaded: t.UploadedEver,
		Category: category,
	}
	if t.DownloadDir != "" && t.Name != "" {
		sep := "/"
		if strings.Contains(t.DownloadDir, `\`) && !strings.Contains(t.DownloadDir, "/") {
			sep = `\`
		}
		out.ContentPath = strings.TrimRight(t.DownloadDir, `/\`) + sep + t.Name
	}
	for _, tr := range t.Trackers {
		out.SwarmSeeds = max(out.SwarmSeeds, tr.Seeders)
		out.SwarmLeeches = max(out.SwarmLeeches, tr.Leechers)
	}
	if t.ETA < 0 {
		out.ETA = 8640000 // "never", as qBittorrent says it
	}
	return out
}

func (c *TransmissionClient) get(ctx context.Context, ids []string) ([]tmTorrent, error) {
	args := map[string]any{"fields": tmFields}
	if ids != nil {
		args["ids"] = ids
	}
	var out struct {
		Torrents []tmTorrent `json:"torrents"`
	}
	if err := c.call(ctx, "torrent-get", args, &out); err != nil {
		return nil, err
	}
	return out.Torrents, nil
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

// List lists the torrents that carry the category as a label (all of them for "").
func (c *TransmissionClient) List(ctx context.Context, category string) ([]Torrent, error) {
	all, err := c.get(ctx, nil)
	if err != nil {
		return nil, err
	}
	var out []Torrent
	for _, t := range all {
		if category == "" || hasLabel(t.Labels, category) {
			out = append(out, t.torrent(category))
		}
	}
	return out, nil
}

// InfoByTag finds the torrents that carry the tag as a label.
func (c *TransmissionClient) InfoByTag(ctx context.Context, tag string) ([]Torrent, error) {
	all, err := c.get(ctx, nil)
	if err != nil {
		return nil, err
	}
	var out []Torrent
	for _, t := range all {
		if hasLabel(t.Labels, tag) {
			out = append(out, t.torrent(""))
		}
	}
	return out, nil
}

// Info finds one torrent by its hash.
func (c *TransmissionClient) Info(ctx context.Context, hash string) (Torrent, bool, error) {
	list, err := c.get(ctx, []string{strings.ToLower(hash)})
	if err != nil {
		return Torrent{}, false, err
	}
	for _, t := range list {
		if strings.EqualFold(t.Hash, hash) {
			return t.torrent(""), true, nil
		}
	}
	return Torrent{}, false, nil
}

func (c *TransmissionClient) act(ctx context.Context, method, hash string, extra map[string]any) error {
	args := map[string]any{"ids": []string{strings.ToLower(hash)}}
	for k, v := range extra {
		args[k] = v
	}
	return c.call(ctx, method, args, nil)
}

// Pause stops the torrent.
func (c *TransmissionClient) Pause(ctx context.Context, hash string) error {
	return c.act(ctx, "torrent-stop", hash, nil)
}

// Resume starts the torrent again.
func (c *TransmissionClient) Resume(ctx context.Context, hash string) error {
	return c.act(ctx, "torrent-start", hash, nil)
}

// Recheck makes Transmission check the files again.
func (c *TransmissionClient) Recheck(ctx context.Context, hash string) error {
	return c.act(ctx, "torrent-verify", hash, nil)
}

// Delete removes the torrent, and with deleteFiles its files.
func (c *TransmissionClient) Delete(ctx context.Context, hash string, deleteFiles bool) error {
	return c.act(ctx, "torrent-remove", hash, map[string]any{"delete-local-data": deleteFiles})
}
