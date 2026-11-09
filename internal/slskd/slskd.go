// Package slskd talks to a person's own slskd (https://github.com/slskd/slskd),
// the Soulseek client with a web API, so Mediarium can find and download
// music on Soulseek (V4 D19, #143). Mediarium never speaks the Soulseek
// protocol itself: slskd runs it, with the person's own account and shares.
//
// What is used of the API (v0): the version (to test the connection), a
// search and its responses, queueing files from one user, and reading and
// cancelling those downloads. Requests carry the API key in X-API-Key and go
// through netguard; slskd usually runs on the same machine or network.
//
// The package depends on nothing else in Mediarium except netguard.
package slskd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

const (
	requestTimeout = 30 * time.Second
	maxBodyBytes   = 32 << 20 // search responses can be big
)

var (
	// ErrBadKey is returned when slskd turns the API key down, or the key is
	// read-only (searching and downloading need a read-write key).
	ErrBadKey = errors.New("slskd didn't accept the API key")
	// ErrUserOffline is returned when the user who has the files is gone.
	ErrUserOffline = errors.New("that Soulseek user is offline")
	// ErrBusy is returned when slskd asks to wait (another search is starting).
	ErrBusy = errors.New("slskd is busy, try again in a moment")
)

// Client talks to one slskd. It is safe for concurrent use.
type Client struct {
	base, key string
	http      *http.Client
}

// New makes a client for the slskd at base (for example http://192.168.1.10:5030)
// with key, a read-write API key from slskd's settings.
func New(base, key string) *Client {
	return &Client{base: strings.TrimRight(strings.TrimSpace(base), "/"), key: strings.TrimSpace(key), http: netguard.Client(requestTimeout)}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("slskd: encode request: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v0"+path, rd)
	if err != nil {
		return 0, fmt.Errorf("slskd: build request: %w", err)
	}
	req.Header.Set("X-API-Key", c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("slskd: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("slskd: read the answer to %s %s: %w", method, path, err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return resp.StatusCode, ErrBadKey
	case resp.StatusCode == http.StatusTooManyRequests:
		return resp.StatusCode, ErrBusy
	case resp.StatusCode >= 400:
		return resp.StatusCode, fmt.Errorf("slskd: %s %s answered %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data[:min(len(data), 200)])))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("slskd: read the answer to %s %s: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

// Version asks slskd for its version: the connection test.
func (c *Client) Version(ctx context.Context) (string, error) {
	var v json.RawMessage
	if _, err := c.do(ctx, http.MethodGet, "/application/version", nil, &v); err != nil {
		return "", err
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s, nil
	}
	return strings.Trim(string(v), `"`), nil
}

// ServerStatus is slskd's own connection to the Soulseek network. slskd can
// answer its web API while it is not connected or not logged in, and then a
// search finds nothing.
type ServerStatus struct {
	IsConnected bool   `json:"isConnected"`
	IsLoggedIn  bool   `json:"isLoggedIn"`
	State       string `json:"state"` // for example "Connected, LoggedIn" or "Disconnected"
}

// Online reports whether slskd is connected and logged in to Soulseek.
func (s ServerStatus) Online() bool { return s.IsConnected && s.IsLoggedIn }

// Server asks slskd whether it is connected to the Soulseek network.
func (c *Client) Server(ctx context.Context) (ServerStatus, error) {
	var st ServerStatus
	_, err := c.do(ctx, http.MethodGet, "/server", nil, &st)
	return st, err
}

// File is one file a user shares, as a search found it.
type File struct {
	Filename   string `json:"filename"` // the full path on the user's side, with \ or /
	Size       int64  `json:"size"`
	Extension  string `json:"extension"`
	BitRate    int    `json:"bitRate"`
	BitDepth   int    `json:"bitDepth"`
	SampleRate int    `json:"sampleRate"`
	Length     int    `json:"length"` // seconds
	IsLocked   bool   `json:"isLocked"`
}

// Response is what one user answered to a search.
type Response struct {
	Username          string `json:"username"`
	HasFreeUploadSlot bool   `json:"hasFreeUploadSlot"`
	QueueLength       int64  `json:"queueLength"`
	UploadSpeed       int64  `json:"uploadSpeed"` // bytes a second
	Files             []File `json:"files"`
}

type search struct {
	ID         string `json:"id"`
	IsComplete bool   `json:"isComplete"`
}

// Search asks Soulseek for text through slskd and waits until slskd has
// finished collecting answers (wait at most, then the answers so far).
func (c *Client) Search(ctx context.Context, text string, wait time.Duration) ([]Response, error) {
	id := newID()
	body := map[string]any{"id": id, "searchText": text, "searchTimeout": 10, "responseLimit": 200, "fileLimit": 20000, "filterResponses": true, "minimumResponseFileCount": 1}
	if _, err := c.do(ctx, http.MethodPost, "/searches", body, nil); err != nil {
		return nil, err
	}
	// The search stays in slskd's Searches page, so a person can see that
	// Mediarium asked; it is tidied up on a later search, an hour on.
	c.tidyOld()
	defer remember(c.base, id)
	deadline := time.Now().Add(wait)
	for {
		var s search
		if _, err := c.do(ctx, http.MethodGet, "/searches/"+id, nil, &s); err != nil {
			return nil, err
		}
		if s.IsComplete || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	var out []Response
	if _, err := c.do(ctx, http.MethodGet, "/searches/"+id+"/responses", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ownSearches are the searches Mediarium started, with slskd's address, so
// they can be removed from slskd's list after a while.
var (
	ownMu       sync.Mutex
	ownSearches []ownSearch
)

type ownSearch struct {
	base, id string
	at       time.Time
}

const keepSearchFor = time.Hour

func remember(base, id string) {
	ownMu.Lock()
	defer ownMu.Unlock()
	ownSearches = append(ownSearches, ownSearch{base, id, time.Now()})
}

// tidyOld deletes the searches this client's slskd was given more than an hour ago.
func (c *Client) tidyOld() {
	ownMu.Lock()
	var old []ownSearch
	keep := ownSearches[:0]
	for _, o := range ownSearches {
		if o.base == c.base && time.Since(o.at) > keepSearchFor {
			old = append(old, o)
		} else {
			keep = append(keep, o)
		}
	}
	ownSearches = keep
	ownMu.Unlock()
	for _, o := range old {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, _ = c.do(cctx, http.MethodDelete, "/searches/"+o.id, nil, nil)
		cancel()
	}
}

// Want is a file to download.
type Want struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

// Enqueue asks slskd to download files from username. Newer slskd takes them
// as one batch; an older one only knows the per-user call, which is tried
// when the batch call isn't there.
func (c *Client) Enqueue(ctx context.Context, username string, files []Want) error {
	status, err := c.do(ctx, http.MethodPost, "/transfers/downloads/batches", map[string]any{"username": username, "files": files}, nil)
	switch {
	case err == nil && status == http.StatusOK:
		return fmt.Errorf("slskd couldn't queue any of the files from %s", username) // 200 = every file failed
	case err == nil:
		return nil
	case status == http.StatusNotFound && !strings.Contains(strings.ToLower(err.Error()), "offline"):
		// no batch endpoint: an older slskd
	case status == http.StatusNotFound:
		return ErrUserOffline
	case status == http.StatusMethodNotAllowed:
	default:
		return err
	}
	status, err = c.do(ctx, http.MethodPost, "/transfers/downloads/"+url.PathEscape(username), files, nil)
	if status == http.StatusNotFound {
		return ErrUserOffline
	}
	return err
}

// State is a transfer's state as slskd reports it: a set of flags, written
// either as text ("Completed, Succeeded") or as a number, depending on the
// slskd version.
type State uint32

// The flags (Soulseek.NET's TransferStates).
const (
	StateQueued     State = 2
	StateInProgress State = 8
	StateCompleted  State = 16
	StateSucceeded  State = 32
	StateCancelled  State = 64
	StateTimedOut   State = 128
	StateErrored    State = 256
	StateRejected   State = 512
	StateAborted    State = 1024
)

var stateNames = map[string]State{
	"none": 0, "requested": 1, "queued": 2, "initializing": 4, "inprogress": 8, "completed": 16, "succeeded": 32,
	"cancelled": 64, "timedout": 128, "errored": 256, "rejected": 512, "aborted": 1024, "locally": 2048, "remotely": 4096,
}

// UnmarshalJSON reads either form.
func (s *State) UnmarshalJSON(b []byte) error {
	if n, err := strconv.ParseUint(string(b), 10, 32); err == nil {
		*s = State(n)
		return nil
	}
	var text string
	if err := json.Unmarshal(b, &text); err != nil {
		return err
	}
	var v State
	for _, part := range strings.Split(text, ",") {
		v |= stateNames[strings.ToLower(strings.TrimSpace(part))]
	}
	*s = v
	return nil
}

// Done reports whether the transfer is over, one way or another.
func (s State) Done() bool { return s&StateCompleted != 0 }

// Succeeded reports whether it finished well.
func (s State) Succeeded() bool { return s&StateCompleted != 0 && s&StateSucceeded != 0 }

// Transfer is one file being downloaded.
type Transfer struct {
	ID               string `json:"id"`
	Filename         string `json:"filename"`
	Size             int64  `json:"size"`
	State            State  `json:"state"`
	BytesTransferred int64  `json:"bytesTransferred"`
}

type userTransfers struct {
	Directories []struct {
		Files []Transfer `json:"files"`
	} `json:"directories"`
}

// Downloads lists the downloads from username (none when slskd has none).
func (c *Client) Downloads(ctx context.Context, username string) ([]Transfer, error) {
	var ut userTransfers
	status, err := c.do(ctx, http.MethodGet, "/transfers/downloads/"+url.PathEscape(username), nil, &ut)
	if status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Transfer
	for _, d := range ut.Directories {
		out = append(out, d.Files...)
	}
	return out, nil
}

// Cancel stops one download and takes it off slskd's list.
func (c *Client) Cancel(ctx context.Context, username, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/transfers/downloads/"+url.PathEscape(username)+"/"+url.PathEscape(id)+"?remove=true", nil, nil)
	return err
}

// BaseName is the file name at the end of a Soulseek path, which may use \ or /.
func BaseName(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Dir is the folder part of a Soulseek path.
func Dir(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[:i]
	}
	return ""
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
