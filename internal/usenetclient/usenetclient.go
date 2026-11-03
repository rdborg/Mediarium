// Package usenetclient hands NZB downloads to an external Usenet downloader:
// SABnzbd or NZBGet. The downloader fetches, repairs and unpacks the release
// into its own folder; Mediarium asks how it is doing, and once it is done
// imports the files from that folder without moving them.
package usenetclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Kinds of client.
const (
	SABnzbd = "sabnzbd"
	NZBGet  = "nzbget"
)

// Label is a kind's name as people know it.
func Label(kind string) string {
	switch kind {
	case SABnzbd:
		return "SABnzbd"
	case NZBGet:
		return "NZBGet"
	}
	return "your Usenet downloader"
}

// ValidKind reports whether kind is one of the clients above.
func ValidKind(kind string) bool { return kind == SABnzbd || kind == NZBGet }

// ErrAuth means the client refused the API key or the login.
var ErrAuth = errors.New("the Usenet downloader refused the API key or login")

// ErrNotThisClient means something answered at the address, but not like the
// client it was said to be (a web page, another program).
var ErrNotThisClient = errors.New("that address doesn't answer like the Usenet downloader")

// CategoryError means the client has no category with this name.
type CategoryError struct{ Name string }

func (e *CategoryError) Error() string {
	return fmt.Sprintf("the Usenet downloader has no category called %q", e.Name)
}

// RefusedError is the client saying no to a request, in its own words.
type RefusedError struct{ Message string }

func (e *RefusedError) Error() string { return "the Usenet downloader said: " + e.Message }

// State is what a job is doing, in the few words Mediarium cares about.
type State string

const (
	StateQueued      State = "queued"
	StateDownloading State = "downloading"
	StatePaused      State = "paused"
	StateProcessing  State = "processing" // checking, repairing, unpacking, moving
	StateCompleted   State = "completed"
	StateFailed      State = "failed"
)

// Job is one download in the client, finished or not.
type Job struct {
	ID             string
	Name           string
	Category       string
	State          State
	StateText      string  // the client's own word for it
	Progress       float64 // 0 to 100
	SizeBytes      int64
	RemainingBytes int64
	Speed          int64 // bytes per second; the client's overall speed on the job that is downloading
	ETASeconds     int64 // 0 = unknown
	Path           string
	FailMessage    string
	// ReleaseFault is set on a failed job when the release is to blame (articles
	// missing, repair or unpack failed), not the client's own trouble (disk full).
	ReleaseFault bool
	// Removed is set when the job was deleted by hand in the client.
	Removed bool
	// AddedAt is when the job was added, or for a job in the history when it
	// finished (both clients only tell that).
	AddedAt time.Time
}

// Priority is how urgent a job is in the client.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityForce  Priority = "force"
)

// ValidPriority reports whether p is one of the priorities above ("" counts: normal).
func ValidPriority(p string) bool {
	switch Priority(p) {
	case "", PriorityLow, PriorityNormal, PriorityHigh, PriorityForce:
		return true
	}
	return false
}

// AddOptions is what is sent to the client. Either URL (the client fetches the
// NZB itself) or NZB (the file) is set.
type AddOptions struct {
	Name     string // shown in the client; without ".nzb"
	URL      string
	NZB      []byte
	Category string
	Priority Priority
}

// API is what the pipeline needs from a Usenet downloader.
type API interface {
	// Check reaches the client, signs in and makes sure the categories exist.
	// It returns the client's version.
	Check(ctx context.Context, categories []string) (string, error)
	// Add sends a release and returns the job's id in the client.
	Add(ctx context.Context, o AddOptions) (string, error)
	// Get finds one job, running or finished.
	Get(ctx context.Context, id string) (Job, bool, error)
	// List returns every job: what is running and what is in the history.
	List(ctx context.Context) ([]Job, error)
	Pause(ctx context.Context, id string) error
	Resume(ctx context.Context, id string) error
	// Delete removes a job (running or finished) from the client.
	Delete(ctx context.Context, id string, deleteFiles bool) error
}

// NewAPI connects to the client at base. apiKey is SABnzbd's; user and pass
// are NZBGet's.
func NewAPI(kind, base, apiKey, user, pass string, hc *http.Client) API {
	if kind == NZBGet {
		return newNZBGet(base, user, pass, hc)
	}
	return newSAB(base, apiKey, hc)
}

// maxBody is the most that is read from an answer: job lists are small.
const maxBody = 32 << 20

// readBody reads an answer, and says what it was when it isn't what the
// client would send.
func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return b, ErrAuth
	case http.StatusNotFound:
		return b, ErrNotThisClient
	}
	if resp.StatusCode >= 500 {
		return b, fmt.Errorf("the Usenet downloader answered with status %d", resp.StatusCode)
	}
	return b, nil
}

// baseURL cleans the address a person typed: no trailing slash, and no
// path the client's own pages use (so the address of the web page works).
func baseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	p := strings.TrimRight(u.Path, "/")
	for _, tail := range []string{"/api", "/jsonrpc", "/sabnzbd/api", "/sabnzbd"} {
		if strings.HasSuffix(p, tail) {
			p = strings.TrimSuffix(p, tail)
			break
		}
	}
	u.Path, u.RawQuery, u.Fragment = p, "", ""
	// SABnzbd lives under /sabnzbd on some set-ups: keep a path given on purpose.
	return strings.TrimRight(u.String(), "/")
}

func jsonUnmarshal(b []byte, v any) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || (b[0] != '{' && b[0] != '[') {
		return ErrNotThisClient
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%w (%v)", ErrNotThisClient, err)
	}
	return nil
}

// parseETA reads "H:MM:SS" or "D:HH:MM:SS" into seconds.
func parseETA(s string) int64 {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || len(parts) > 4 {
		return 0
	}
	mult := []int64{1, 60, 3600, 86400}
	var total int64
	for i, p := range parts {
		var n int64
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil || n < 0 {
			return 0
		}
		total += n * mult[len(parts)-1-i]
	}
	return total
}

func equalFold(list []string, name string) bool {
	for _, c := range list {
		if strings.EqualFold(strings.TrimSpace(c), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}
