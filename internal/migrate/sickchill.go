package migrate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// SickChill is read through its command API: GET /api/<key>/?cmd=shows lists
// the shows (by TVDB id) and cmd=show&tvdbid=<id> gives each show's folder.
// The key is part of the path; an answer of {"result": "denied"} means it
// was not accepted. (SickBeard and SickRage answer the same way.)

type sickchillEnvelope struct {
	Result  string `json:"result"`
	Message string `json:"message"`
}

type sickchillShowsAnswer struct {
	sickchillEnvelope
	Data map[string]struct {
		ShowName string  `json:"show_name"`
		TVDBID   flexInt `json:"tvdbid"`
		IndexID  flexInt `json:"indexerid"`
		Paused   flexInt `json:"paused"`
	} `json:"data"`
}

type sickchillShowAnswer struct {
	sickchillEnvelope
	Data struct {
		Location string  `json:"location"`
		Paused   flexInt `json:"paused"`
		ShowName string  `json:"show_name"`
	} `json:"data"`
}

func (e sickchillEnvelope) check() error {
	switch strings.ToLower(e.Result) {
	case "denied":
		return errors.New("the API key was not accepted")
	case "", "success":
		return nil
	}
	return fmt.Errorf("SickChill refused the request: %s", e.Message)
}

func sickchillCall(cmd string, extra url.Values) call {
	q := url.Values{"cmd": {cmd}}
	for k, v := range extra {
		q[k] = v
	}
	return call{path: "/api/{key}/", query: q}
}

func fetchSickChill(ctx context.Context, hc *http.Client, c Conn) ([]legacyShow, error) {
	var list sickchillShowsAnswer
	if err := read(ctx, hc, c, sickchillCall("shows", nil), &list); err != nil {
		return nil, err
	}
	if err := list.check(); err != nil {
		return nil, err
	}
	type entry struct {
		id     int
		name   string
		paused bool
	}
	var entries []entry
	for key, s := range list.Data {
		id := int(s.TVDBID)
		if id == 0 {
			id = int(s.IndexID)
		}
		if id == 0 {
			id, _ = strconv.Atoi(key)
		}
		entries = append(entries, entry{id: id, name: s.ShowName, paused: s.Paused != 0})
	}
	sort.Slice(entries, func(i, j int) bool { return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name) })

	// The listing has no folders: one small request per show.
	shows := make([]legacyShow, len(entries))
	errs := make([]error, len(entries))
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, lookupWorkers)
	)
	for i, e := range entries {
		title, year := yearFromTitle(e.name)
		shows[i] = legacyShow{Title: title, Year: year, TVDBID: e.id, Paused: e.paused}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var det sickchillShowAnswer
			if err := read(ctx, hc, c, sickchillCall("show", url.Values{"tvdbid": {strconv.Itoa(e.id)}}), &det); err != nil {
				errs[i] = err
				return
			}
			if err := det.check(); err != nil {
				errs[i] = err
				return
			}
			shows[i].Location = strings.TrimSpace(det.Data.Location)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("read show %q: %w", shows[i].Title, err)
		}
	}
	return shows, nil
}
