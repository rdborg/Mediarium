package migrate

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Overseerr and Jellyseerr share one API: GET /api/v1/request (paged) with
// the key in the X-Api-Key header. A request's media has the TMDB id and the
// media status; the person is requestedBy. Titles are not in the answer, so
// they are looked up on TMDB for the ones being added.

// Overseerr request status.
const (
	overseerrReqPending  = 1
	overseerrReqApproved = 2
	overseerrReqDeclined = 3
	overseerrReqFailed   = 4
	overseerrReqDone     = 5 // completed (newer versions)
)

// Overseerr media status.
const (
	overseerrMediaProcessing = 3
	overseerrMediaPartial    = 4
	overseerrMediaAvailable  = 5
)

type overseerrRequest struct {
	ID     int    `json:"id"`
	Status int    `json:"status"`
	Type   string `json:"type"`
	Media  struct {
		TMDBID    int    `json:"tmdbId"`
		TVDBID    int    `json:"tvdbId"`
		Status    int    `json:"status"`
		MediaType string `json:"mediaType"`
	} `json:"media"`
	RequestedBy struct {
		DisplayName      string `json:"displayName"`
		Username         string `json:"username"`
		PlexUsername     string `json:"plexUsername"`
		JellyfinUsername string `json:"jellyfinUsername"`
	} `json:"requestedBy"`
	Seasons []struct {
		SeasonNumber int `json:"seasonNumber"`
	} `json:"seasons"`
}

type overseerrPage struct {
	PageInfo struct {
		Results int `json:"results"`
	} `json:"pageInfo"`
	Results []overseerrRequest `json:"results"`
}

type overseerrData struct {
	Version  string
	Requests []overseerrRequest
}

// overseerrPageSize is a variable so a test can page through a short list.
var overseerrPageSize = 100

const overseerrMaxPages = 200

func fetchOverseerr(ctx context.Context, hc *http.Client, c Conn) (overseerrData, error) {
	var d overseerrData
	var st struct {
		Version string `json:"version"`
	}
	if err := read(ctx, hc, c, call{path: "/api/v1/status", keyHeader: "X-Api-Key"}, &st); err != nil {
		return d, err
	}
	d.Version = st.Version
	for page := 0; page < overseerrMaxPages; page++ {
		var out overseerrPage
		q := url.Values{"take": {fmt.Sprint(overseerrPageSize)}, "skip": {fmt.Sprint(page * overseerrPageSize)}, "filter": {"all"}, "sort": {"added"}}
		if err := read(ctx, hc, c, call{path: "/api/v1/request", query: q, keyHeader: "X-Api-Key"}, &out); err != nil {
			return d, fmt.Errorf("read requests: %w", err)
		}
		d.Requests = append(d.Requests, out.Results...)
		if len(out.Results) < overseerrPageSize || (out.PageInfo.Results > 0 && len(d.Requests) >= out.PageInfo.Results) {
			break
		}
	}
	return d, nil
}

// requester is the name to show for the person who asked.
func (r overseerrRequest) requester() string {
	for _, n := range []string{r.RequestedBy.DisplayName, r.RequestedBy.Username, r.RequestedBy.PlexUsername, r.RequestedBy.JellyfinUsername} {
		if n = strings.TrimSpace(n); n != "" {
			return n
		}
	}
	return "someone"
}

// status combines the request's own status with the media's.
func (r overseerrRequest) status() string {
	switch {
	case r.Media.Status == overseerrMediaAvailable || r.Status == overseerrReqDone:
		return RequestAvailable
	case r.Media.Status == overseerrMediaPartial:
		return RequestPartiallyAvailable
	case r.Status == overseerrReqDeclined:
		return RequestDeclined
	case r.Status == overseerrReqFailed:
		return RequestFailed
	case r.Media.Status == overseerrMediaProcessing:
		return RequestProcessing
	case r.Status == overseerrReqApproved:
		return RequestApproved
	case r.Status == overseerrReqPending:
		return RequestPending
	}
	return RequestPending
}

func (d overseerrData) entries() []requestEntry {
	var out []requestEntry
	for _, r := range d.Requests {
		media := r.Type
		if media == "" {
			media = r.Media.MediaType
		}
		if media != "movie" && media != "tv" {
			continue
		}
		e := requestEntry{MediaType: media, TMDBID: r.Media.TMDBID, TVDBID: r.Media.TVDBID, Status: r.status(), By: []string{r.requester()}}
		for _, s := range r.Seasons {
			if s.SeasonNumber > 0 {
				e.Seasons = append(e.Seasons, s.SeasonNumber)
			}
		}
		out = append(out, e)
	}
	return out
}
