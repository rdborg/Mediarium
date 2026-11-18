package debrid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

// RealDebridURL is Real-Debrid's API.
const RealDebridURL = "https://api.real-debrid.com/rest/1.0"

type realDebrid struct {
	key  string
	http *http.Client
	base string
}

func (r *realDebrid) Name() string { return RealDebrid }

// call sends one request; the key travels in a header only.
func (r *realDebrid) call(ctx context.Context, method, path string, form url.Values, raw []byte, out any) error {
	var body io.Reader
	switch {
	case form != nil:
		body = strings.NewReader(form.Encode())
	case raw != nil:
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, body)
	if err != nil {
		return fmt.Errorf("build Real-Debrid request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.key)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Real-Debrid: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		var e struct {
			Error     string `json:"error"`
			ErrorCode int    `json:"error_code"`
		}
		_ = json.Unmarshal(data, &e)
		if e.ErrorCode == 8 || resp.StatusCode == http.StatusUnauthorized {
			return ErrAuth
		}
		return fmt.Errorf("Real-Debrid refused: %s", rdError(e.Error, e.ErrorCode))
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error     string `json:"error"`
			ErrorCode int    `json:"error_code"`
		}
		_ = json.Unmarshal(data, &e)
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			e.ErrorCode = 34
		case resp.StatusCode >= 500 && e.Error == "":
			// Its servers had a bad moment (or Cloudflare answered for them).
			return fmt.Errorf("Real-Debrid is having trouble at the moment, so Mediarium will try again (HTTP %d)", resp.StatusCode)
		}
		return fmt.Errorf("Real-Debrid: %s (HTTP %d)", rdError(e.Error, e.ErrorCode), resp.StatusCode)
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("Real-Debrid sent an answer Mediarium couldn't read: %w", err)
	}
	return nil
}

// rdError puts the codes people hit most into words.
func rdError(msg string, code int) string {
	switch code {
	case 9:
		return "permission denied (is the account premium?)"
	case 14:
		return "the account is locked"
	case 23:
		return "the account's traffic is used up"
	case 24:
		return "the file is no longer available"
	case 34, 36:
		return "too many requests, try again in a minute"
	}
	if msg == "" {
		return "an error"
	}
	return strings.ReplaceAll(msg, "_", " ")
}

func (r *realDebrid) Account(ctx context.Context) (Account, error) {
	var u struct {
		Username   string `json:"username"`
		Type       string `json:"type"`
		Expiration string `json:"expiration"`
	}
	if err := r.call(ctx, http.MethodGet, "/user", nil, nil, &u); err != nil {
		return Account{}, err
	}
	a := Account{Name: u.Username, Premium: u.Type == "premium"}
	a.Expires, _ = time.Parse(time.RFC3339, u.Expiration)
	return a, nil
}

func (r *realDebrid) Add(ctx context.Context, magnet string, torrentFile []byte) (string, error) {
	var added struct {
		ID string `json:"id"`
	}
	var err error
	if magnet != "" {
		err = r.call(ctx, http.MethodPost, "/torrents/addMagnet", url.Values{"magnet": {magnet}}, nil, &added)
	} else {
		err = r.call(ctx, http.MethodPut, "/torrents/addTorrent", nil, torrentFile, &added)
	}
	if err != nil {
		return "", err
	}
	if added.ID == "" {
		return "", fmt.Errorf("Real-Debrid didn't take the torrent")
	}
	return added.ID, nil
}

type rdInfo struct {
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Seeders  *int    `json:"seeders"`
	Files    []struct {
		ID       int    `json:"id"`
		Path     string `json:"path"`
		Bytes    int64  `json:"bytes"`
		Selected int    `json:"selected"`
	} `json:"files"`
	Links []string `json:"links"`
}

func (r *realDebrid) info(ctx context.Context, id string) (rdInfo, error) {
	var in rdInfo
	err := r.call(ctx, http.MethodGet, "/torrents/info/"+url.PathEscape(id), nil, nil, &in)
	return in, err
}

func (r *realDebrid) Status(ctx context.Context, id string) (Status, error) {
	in, err := r.info(ctx, id)
	if err != nil {
		return Status{}, err
	}
	st := Status{Progress: in.Progress / 100, Seeders: -1}
	if in.Seeders != nil {
		st.Seeders = *in.Seeders
	}
	switch in.Status {
	case "waiting_files_selection":
		// Everything is taken: the import picks what it needs.
		if err := r.call(ctx, http.MethodPost, "/torrents/selectFiles/"+url.PathEscape(id), url.Values{"files": {"all"}}, nil, nil); err != nil {
			return Status{}, err
		}
		st.State = Waiting
	case "magnet_conversion", "queued", "compressing", "uploading":
		st.State = Waiting
	case "downloading":
		st.State = Downloading
	case "downloaded":
		st.State, st.Progress = Ready, 1
	default: // magnet_error, error, virus, dead
		st.State, st.Message = Failed, "Real-Debrid says: "+strings.ReplaceAll(in.Status, "_", " ")
	}
	return st, nil
}

func (r *realDebrid) Files(ctx context.Context, id string) ([]File, error) {
	in, err := r.info(ctx, id)
	if err != nil {
		return nil, err
	}
	// Real-Debrid lists one link per selected file, in the order of the files.
	// Its link answer only has the bare file name, so two files of a pack with
	// the same name (every episode's English.srt) would overwrite each other:
	// the torrent's own paths are used when they line up with the links.
	var paths []string
	for _, f := range in.Files {
		if f.Selected == 1 {
			paths = append(paths, strings.TrimPrefix(f.Path, "/"))
		}
	}
	if len(paths) != len(in.Links) {
		paths = nil
	}
	var out []File
	for i, link := range in.Links {
		var u struct {
			Filename string `json:"filename"`
			Filesize int64  `json:"filesize"`
			Download string `json:"download"`
		}
		if err := r.call(ctx, http.MethodPost, "/unrestrict/link", url.Values{"link": {link}}, nil, &u); err != nil {
			return nil, err
		}
		if u.Download == "" {
			continue
		}
		name := u.Filename
		if paths != nil && paths[i] != "" && path.Base(paths[i]) == path.Base(strings.ReplaceAll(u.Filename, `\`, "/")) {
			name = paths[i]
		}
		out = append(out, File{Name: name, Size: u.Filesize, URL: u.Download})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("Real-Debrid has no files for this torrent")
	}
	return out, nil
}

func (r *realDebrid) Remove(ctx context.Context, id string) error {
	return r.call(ctx, http.MethodDelete, "/torrents/delete/"+url.PathEscape(id), nil, nil, nil)
}

type rdListed struct {
	ID       string  `json:"id"`
	Filename string  `json:"filename"`
	Bytes    int64   `json:"bytes"`
	Progress float64 `json:"progress"`
	Status   string  `json:"status"`
	Speed    int64   `json:"speed"`
	Seeders  *int    `json:"seeders"`
	Added    string  `json:"added"`
}

func (r *realDebrid) List(ctx context.Context) ([]Item, error) {
	var list []rdListed
	if err := r.call(ctx, http.MethodGet, "/torrents?limit=100", nil, nil, &list); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(list))
	for _, l := range list {
		it := Item{ID: l.ID, Name: l.Filename, Progress: l.Progress / 100, Size: l.Bytes, Speed: l.Speed, Seeders: -1, Text: strings.ReplaceAll(l.Status, "_", " ")}
		if l.Seeders != nil {
			it.Seeders = *l.Seeders
		}
		if at, err := time.Parse(time.RFC3339, l.Added); err == nil {
			it.Added = at
		}
		switch l.Status {
		case "downloaded":
			it.State, it.Progress = Ready, 1
		case "downloading":
			it.State = Downloading
		case "magnet_error", "error", "virus", "dead":
			it.State = Failed
		default:
			it.State = Waiting
		}
		out = append(out, it)
	}
	return out, nil
}
