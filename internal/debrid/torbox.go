package debrid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

// TorBoxURL is TorBox's API.
const TorBoxURL = "https://api.torbox.app/v1/api"

type torBox struct {
	key  string
	http *http.Client
	base string
}

func (t *torBox) Name() string { return TorBox }

type tbAnswer struct {
	Success bool            `json:"success"`
	Detail  string          `json:"detail"`
	Error   any             `json:"error"`
	Data    json.RawMessage `json:"data"`
}

func (t *torBox) call(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string, out any) error {
	target := t.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("build TorBox request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach TorBox: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var a tbAnswer
	_ = json.Unmarshal(data, &a)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// A 403 is also how TorBox says the plan doesn't include something:
		// that is not a wrong key, and its own words say what it is.
		if code, _ := a.Error.(string); code != "" && a.Detail != "" && !strings.Contains(strings.ToUpper(code), "AUTH") && !strings.Contains(strings.ToUpper(code), "TOKEN") {
			return fmt.Errorf("TorBox: %s", a.Detail)
		}
		return ErrAuth
	}
	if resp.StatusCode >= 300 || !a.Success {
		msg := a.Detail
		if msg == "" && resp.StatusCode == http.StatusTooManyRequests {
			msg = "too many requests, Mediarium will try again in a moment"
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("TorBox: %s", msg)
	}
	if out != nil && len(a.Data) > 0 && string(a.Data) != "null" {
		if err := json.Unmarshal(a.Data, out); err != nil {
			return fmt.Errorf("TorBox sent an answer Mediarium couldn't read: %w", err)
		}
	}
	return nil
}

func (t *torBox) Account(ctx context.Context) (Account, error) {
	var u struct {
		Email            string `json:"email"`
		Plan             int    `json:"plan"`
		PremiumExpiresAt string `json:"premium_expires_at"`
	}
	if err := t.call(ctx, http.MethodGet, "/user/me", nil, nil, "", &u); err != nil {
		return Account{}, err
	}
	a := Account{Name: u.Email, Premium: u.Plan > 0}
	a.Expires, _ = time.Parse(time.RFC3339, u.PremiumExpiresAt)
	return a, nil
}

func (t *torBox) Add(ctx context.Context, magnet string, torrentFile []byte) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if magnet != "" {
		_ = mw.WriteField("magnet", magnet)
	} else {
		part, err := mw.CreateFormFile("file", "release.torrent")
		if err != nil {
			return "", err
		}
		_, _ = part.Write(torrentFile)
	}
	_ = mw.WriteField("seed", "3") // don't seed: the files come straight home
	_ = mw.WriteField("allow_zip", "false")
	_ = mw.Close()
	var added struct {
		TorrentID int64 `json:"torrent_id"`
	}
	if err := t.call(ctx, http.MethodPost, "/torrents/createtorrent", nil, &buf, mw.FormDataContentType(), &added); err != nil {
		return "", err
	}
	if added.TorrentID == 0 {
		return "", fmt.Errorf("TorBox didn't take the torrent")
	}
	return strconv.FormatInt(added.TorrentID, 10), nil
}

type tbTorrent struct {
	DownloadState    string  `json:"download_state"`
	DownloadFinished bool    `json:"download_finished"`
	DownloadPresent  bool    `json:"download_present"`
	Progress         float64 `json:"progress"`
	Seeds            int     `json:"seeds"`
	Files            []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"files"`
}

func (t *torBox) torrent(ctx context.Context, id string) (tbTorrent, error) {
	var tt tbTorrent
	err := t.call(ctx, http.MethodGet, "/torrents/mylist", url.Values{"id": {id}, "bypass_cache": {"true"}}, nil, "", &tt)
	return tt, err
}

func (t *torBox) Status(ctx context.Context, id string) (Status, error) {
	tt, err := t.torrent(ctx, id)
	if err != nil {
		return Status{}, err
	}
	st := Status{Progress: tt.Progress, Seeders: tt.Seeds}
	state := strings.ToLower(tt.DownloadState)
	switch {
	case tt.DownloadFinished && tt.DownloadPresent:
		st.State, st.Progress = Ready, 1
	case strings.Contains(state, "error") || strings.Contains(state, "failed"):
		st.State, st.Message = Failed, "TorBox says: "+tt.DownloadState
	case strings.HasPrefix(state, "downloading"), strings.HasPrefix(state, "stalled"), state == "metadl":
		st.State = Downloading
	default: // queued, checking, paused, or finished but still moving to storage
		st.State = Waiting
	}
	return st, nil
}

func (t *torBox) Files(ctx context.Context, id string) ([]File, error) {
	tt, err := t.torrent(ctx, id)
	if err != nil {
		return nil, err
	}
	var out []File
	for _, f := range tt.Files {
		var link string
		q := url.Values{"token": {t.key}, "torrent_id": {id}, "file_id": {strconv.FormatInt(f.ID, 10)}}
		if err := t.call(ctx, http.MethodGet, "/torrents/requestdl", q, nil, "", &link); err != nil {
			return nil, err
		}
		if link == "" {
			continue
		}
		out = append(out, File{Name: f.Name, Size: f.Size, URL: link})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("TorBox has no files for this torrent")
	}
	return out, nil
}

// FreshLink asks for a new link to one file, without asking for a link to
// every other file in the torrent.
func (t *torBox) FreshLink(ctx context.Context, id string, f File) (string, error) {
	tt, err := t.torrent(ctx, id)
	if err != nil {
		return "", err
	}
	for _, tf := range tt.Files {
		if tf.Name != f.Name {
			continue
		}
		var link string
		q := url.Values{"token": {t.key}, "torrent_id": {id}, "file_id": {strconv.FormatInt(tf.ID, 10)}}
		if err := t.call(ctx, http.MethodGet, "/torrents/requestdl", q, nil, "", &link); err != nil {
			return "", err
		}
		if link == "" {
			return "", errors.New("TorBox gave no link for this file")
		}
		return link, nil
	}
	return "", errors.New("the file is no longer listed on TorBox")
}

func (t *torBox) Remove(ctx context.Context, id string) error {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"torrent_id": n, "operation": "delete"})
	return t.call(ctx, http.MethodPost, "/torrents/controltorrent", nil, bytes.NewReader(body), "application/json", nil)
}

// checkCachedBatch is how many hashes go into one TorBox question. The hashes
// travel in the address, so the batch stays well short of URL length limits.
const checkCachedBatch = 50

// Cached asks TorBox which of the info hashes it already has
// (GET /torrents/checkcached, hashes comma separated, format=list). TorBox
// leaves the hashes it doesn't have out of the answer.
func (t *torBox) Cached(ctx context.Context, hashes []string) (map[string]bool, error) {
	out := make(map[string]bool, len(hashes))
	var ask []string
	for _, h := range hashes {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if _, dup := out[h]; !dup {
			out[h] = false
			ask = append(ask, h)
		}
	}
	for start := 0; start < len(ask); start += checkCachedBatch {
		end := min(start+checkCachedBatch, len(ask))
		q := url.Values{"hash": {strings.Join(ask[start:end], ",")}, "format": {"list"}, "list_files": {"false"}}
		var raw json.RawMessage
		if err := t.call(ctx, http.MethodGet, "/torrents/checkcached", q, nil, "", &raw); err != nil {
			return nil, err
		}
		found, err := cachedHashes(raw)
		if err != nil {
			return nil, err
		}
		for _, h := range found {
			if _, asked := out[h]; asked {
				out[h] = true
			}
		}
	}
	return out, nil
}

// cachedHashes reads the data part of a checkcached answer: a list of
// {name, size, hash} (format=list) or an object keyed by hash (format=object).
// Nothing cached may come as null, an empty list or an empty object.
func cachedHashes(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "false" {
		return nil, nil
	}
	var out []string
	switch raw[0] {
	case '[':
		var list []struct {
			Hash string `json:"hash"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("TorBox sent an answer Mediarium couldn't read: %w", err)
		}
		for _, e := range list {
			out = append(out, strings.ToLower(e.Hash))
		}
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf("TorBox sent an answer Mediarium couldn't read: %w", err)
		}
		for h := range obj {
			out = append(out, strings.ToLower(h))
		}
	default:
		return nil, fmt.Errorf("TorBox sent an answer Mediarium couldn't read")
	}
	return out, nil
}

type tbListed struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Size          int64   `json:"size"`
	Progress      float64 `json:"progress"`
	DownloadState string  `json:"download_state"`
	Finished      bool    `json:"download_finished"`
	Present       bool    `json:"download_present"`
	Speed         int64   `json:"download_speed"`
	Seeds         int     `json:"seeds"`
	CreatedAt     string  `json:"created_at"`
}

func (t *torBox) List(ctx context.Context) ([]Item, error) {
	var list []tbListed
	if err := t.call(ctx, http.MethodGet, "/torrents/mylist", url.Values{"bypass_cache": {"true"}}, nil, "", &list); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(list))
	for _, l := range list {
		state := strings.ToLower(l.DownloadState)
		it := Item{ID: strconv.FormatInt(l.ID, 10), Name: l.Name, Progress: l.Progress, Size: l.Size, Speed: l.Speed, Seeders: l.Seeds, Text: l.DownloadState}
		if at, err := time.Parse(time.RFC3339, l.CreatedAt); err == nil {
			it.Added = at
		}
		switch {
		case l.Finished && l.Present:
			it.State, it.Progress = Ready, 1
		case strings.Contains(state, "error") || strings.Contains(state, "failed"):
			it.State = Failed
		case strings.HasPrefix(state, "downloading"), strings.HasPrefix(state, "stalled"), state == "metadl":
			it.State = Downloading
		default:
			it.State = Waiting
		}
		out = append(out, it)
	}
	return out, nil
}
