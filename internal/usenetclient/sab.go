package usenetclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// sab talks to SABnzbd's HTTP API: /api?mode=...&output=json&apikey=...
type sab struct {
	base   string
	apiKey string
	hc     *http.Client
}

func newSAB(base, apiKey string, hc *http.Client) *sab {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &sab{base: baseURL(base), apiKey: strings.TrimSpace(apiKey), hc: hc}
}

func (c *sab) endpoint(mode string, extra url.Values) string {
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	q.Set("mode", mode)
	q.Set("output", "json")
	if c.apiKey != "" {
		q.Set("apikey", c.apiKey)
	}
	return c.base + "/api?" + q.Encode()
}

// do asks for mode and decodes the answer into out. SABnzbd answers
// {"status": false, "error": "..."} for a refusal.
func (c *sab) do(ctx context.Context, mode string, extra url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(mode, extra), nil)
	if err != nil {
		return err
	}
	return c.send(req, out)
}

func (c *sab) send(req *http.Request, out any) error {
	resp, err := c.hc.Do(req)
	if err != nil {
		return cleanURLError(err)
	}
	body, err := readBody(resp)
	if err != nil {
		return err
	}
	var probe struct {
		Status any    `json:"status"`
		Error  string `json:"error"`
	}
	if err := jsonUnmarshal(body, &probe); err != nil {
		// A bare "error: API Key Incorrect" line is what older versions say.
		if msg := strings.ToLower(string(body)); strings.Contains(msg, "api key") {
			return ErrAuth
		}
		return err
	}
	if probe.Error != "" {
		low := strings.ToLower(probe.Error)
		if strings.Contains(low, "api key") || strings.Contains(low, "not authorized") || strings.Contains(low, "login") {
			return ErrAuth
		}
		return &RefusedError{Message: probe.Error}
	}
	if s, ok := probe.Status.(bool); ok && !s {
		return &RefusedError{Message: "it didn't accept the request"}
	}
	if out == nil {
		return nil
	}
	return jsonUnmarshal(body, out)
}

// cleanURLError keeps the API key (which is in the address) out of an error.
func cleanURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

func (c *sab) Check(ctx context.Context, categories []string) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	// The version needs no key, so it only proves that SABnzbd is there.
	if err := c.do(ctx, "version", nil, &v); err != nil {
		return "", err
	}
	if v.Version == "" {
		return "", ErrNotThisClient
	}
	// Anything that needs the key shows whether it is the right one.
	var cats struct {
		Categories []string `json:"categories"`
	}
	if err := c.do(ctx, "get_cats", nil, &cats); err != nil {
		return "", err
	}
	for _, want := range categories {
		want = strings.TrimSpace(want)
		if want == "" || want == "*" {
			continue
		}
		if !equalFold(cats.Categories, want) {
			return v.Version, &CategoryError{Name: want}
		}
	}
	return v.Version, nil
}

func sabPriority(p Priority) string {
	switch p {
	case PriorityLow:
		return "-1"
	case PriorityHigh:
		return "1"
	case PriorityForce:
		return "2"
	}
	return "0"
}

type sabAdded struct {
	NZOIDs []string `json:"nzo_ids"`
}

func (c *sab) Add(ctx context.Context, o AddOptions) (string, error) {
	extra := url.Values{}
	if o.Category != "" {
		extra.Set("cat", o.Category)
	}
	extra.Set("priority", sabPriority(o.Priority))
	if o.Name != "" {
		extra.Set("nzbname", o.Name)
	}
	var added sabAdded
	if o.URL != "" {
		extra.Set("name", o.URL)
		if err := c.do(ctx, "addurl", extra, &added); err != nil {
			return "", err
		}
	} else {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, err := mw.CreateFormFile("name", safeFileName(o.Name)+".nzb")
		if err != nil {
			return "", err
		}
		if _, err := fw.Write(o.NZB); err != nil {
			return "", err
		}
		if err := mw.Close(); err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("addfile", extra), &buf)
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if err := c.send(req, &added); err != nil {
			return "", err
		}
	}
	if len(added.NZOIDs) == 0 || added.NZOIDs[0] == "" {
		return "", &RefusedError{Message: "it took the request but gave no job number (is the NZB empty or broken?)"}
	}
	return added.NZOIDs[0], nil
}

// safeFileName makes name usable as the file name in an upload.
func safeFileName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		return "release"
	}
	if len(name) > 150 {
		name = name[:150]
	}
	return name
}

type sabSlot struct {
	NZOID      string `json:"nzo_id"`
	Filename   string `json:"filename"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Cat        string `json:"cat"`
	Category   string `json:"category"`
	MB         string `json:"mb"`
	MBLeft     string `json:"mbleft"`
	Percentage string `json:"percentage"`
	TimeLeft   string `json:"timeleft"`
	Bytes      int64  `json:"bytes"`
	Storage    string `json:"storage"`
	Path       string `json:"path"`
	FailMsg    string `json:"fail_message"`
	Completed  int64  `json:"completed"`
}

func mbToBytes(s string) int64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 {
		return 0
	}
	return int64(f * 1024 * 1024)
}

func (s sabSlot) category() string {
	c := s.Cat
	if c == "" {
		c = s.Category
	}
	if c == "*" {
		return ""
	}
	return c
}

// sabQueueState maps a status of a job in SABnzbd's queue.
func sabQueueState(status string, queuePaused bool) State {
	switch strings.ToLower(status) {
	case "paused":
		return StatePaused
	case "downloading", "fetching":
		if queuePaused {
			return StatePaused
		}
		return StateDownloading
	case "checking", "verifying", "repairing", "extracting", "moving", "running", "quickcheck", "quick check":
		return StateProcessing
	}
	// Queued, Grabbing (fetching the NZB from its address), Propagating (waiting for the articles).
	return StateQueued
}

// sabHistoryState maps a status of a job in SABnzbd's history.
func sabHistoryState(status string) State {
	switch strings.ToLower(status) {
	case "completed":
		return StateCompleted
	case "failed":
		return StateFailed
	}
	// Queued (waiting its turn for post-processing), Verifying, Repairing,
	// Extracting, Moving, Running (a script), Fetching (extra repair blocks).
	return StateProcessing
}

// sabClientTrouble says whether a failure message is about the client's own
// machine (a full disk, a folder it can't write to) and not the release.
func sabClientTrouble(msg string) bool {
	low := strings.ToLower(msg)
	for _, w := range []string{"disk", "space", "permission", "access is denied", "read-only", "not writable", "cannot create", "can't create", "couldn't create", "no such file or directory", "moving"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

func (s sabSlot) historyJob() Job {
	name := s.Name
	if name == "" {
		name = s.Filename
	}
	j := Job{ID: s.NZOID, Name: name, Category: s.category(), State: sabHistoryState(s.Status), StateText: s.Status, SizeBytes: s.Bytes}
	j.Path = s.Storage
	if j.Path == "" {
		j.Path = s.Path
	}
	if s.Completed > 0 {
		j.AddedAt = time.Unix(s.Completed, 0)
	}
	switch j.State {
	case StateCompleted:
		j.Progress = 100
	case StateProcessing:
		j.Progress = 99
	case StateFailed:
		j.FailMessage = strings.TrimSpace(s.FailMsg)
		j.ReleaseFault = !sabClientTrouble(j.FailMessage)
	}
	return j
}

func (s sabSlot) queueJob(speed int64, queuePaused bool) Job {
	name := s.Filename
	if name == "" {
		name = s.Name
	}
	j := Job{ID: s.NZOID, Name: name, Category: s.category(), State: sabQueueState(s.Status, queuePaused), StateText: s.Status}
	j.SizeBytes, j.RemainingBytes = mbToBytes(s.MB), mbToBytes(s.MBLeft)
	if pct, err := strconv.ParseFloat(strings.TrimSpace(s.Percentage), 64); err == nil {
		j.Progress = min(max(pct, 0), 100)
	} else if j.SizeBytes > 0 {
		j.Progress = float64(j.SizeBytes-j.RemainingBytes) / float64(j.SizeBytes) * 100
	}
	if j.State == StateProcessing {
		j.Progress = 99
	}
	if j.State == StateDownloading {
		j.Speed = speed
		j.ETASeconds = parseETA(s.TimeLeft)
	}
	return j
}

type sabQueueAnswer struct {
	Queue struct {
		Paused    bool      `json:"paused"`
		KBPerSec  string    `json:"kbpersec"`
		Slots     []sabSlot `json:"slots"`
		SpeedText string    `json:"speed"`
	} `json:"queue"`
}

type sabHistoryAnswer struct {
	History struct {
		Slots []sabSlot `json:"slots"`
	} `json:"history"`
}

func (c *sab) queue(ctx context.Context) ([]Job, error) {
	var a sabQueueAnswer
	if err := c.do(ctx, "queue", url.Values{"limit": {"500"}}, &a); err != nil {
		return nil, err
	}
	kb, _ := strconv.ParseFloat(strings.TrimSpace(a.Queue.KBPerSec), 64)
	speed := int64(kb * 1024)
	out := make([]Job, 0, len(a.Queue.Slots))
	for _, s := range a.Queue.Slots {
		j := s.queueJob(speed, a.Queue.Paused)
		out = append(out, j)
		speed = 0 // the speed is for the queue; it goes with the first job
	}
	return out, nil
}

func (c *sab) history(ctx context.Context, ids ...string) ([]Job, error) {
	extra := url.Values{"limit": {"500"}}
	if len(ids) > 0 {
		extra.Set("nzo_ids", strings.Join(ids, ","))
	}
	var a sabHistoryAnswer
	if err := c.do(ctx, "history", extra, &a); err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(a.History.Slots))
	for _, s := range a.History.Slots {
		out = append(out, s.historyJob())
	}
	return out, nil
}

func (c *sab) List(ctx context.Context) ([]Job, error) {
	q, err := c.queue(ctx)
	if err != nil {
		return nil, err
	}
	h, err := c.history(ctx)
	if err != nil {
		return nil, err
	}
	return append(q, h...), nil
}

func (c *sab) Get(ctx context.Context, id string) (Job, bool, error) {
	q, err := c.queue(ctx)
	if err != nil {
		return Job{}, false, err
	}
	for _, j := range q {
		if j.ID == id {
			return j, true, nil
		}
	}
	h, err := c.history(ctx, id)
	if err != nil {
		return Job{}, false, err
	}
	for _, j := range h {
		if j.ID == id {
			return j, true, nil
		}
	}
	return Job{}, false, nil
}

func (c *sab) Pause(ctx context.Context, id string) error {
	return c.do(ctx, "queue", url.Values{"name": {"pause"}, "value": {id}}, nil)
}

func (c *sab) Resume(ctx context.Context, id string) error {
	return c.do(ctx, "queue", url.Values{"name": {"resume"}, "value": {id}}, nil)
}

func (c *sab) Delete(ctx context.Context, id string, deleteFiles bool) error {
	extra := url.Values{"name": {"delete"}, "value": {id}}
	if deleteFiles {
		extra.Set("del_files", "1")
	}
	// A job is either running (queue) or finished (history): ask both.
	errQ := c.do(ctx, "queue", extra, nil)
	errH := c.do(ctx, "history", extra, nil)
	if errQ != nil && errH != nil {
		return errQ
	}
	return nil
}
