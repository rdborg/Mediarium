package mediaservers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Tags from Mediarium ("Kids", "4K") show up on the media servers as
// collections, which is how people browse and filter there. Plex gets them
// as the title's collection field; Jellyfin and Emby as collections (box
// sets) named after the tag. Only the tags Mediarium is told to add or remove
// are touched: collections made by hand are left alone.

// SyncTags adds the title (found by its TMDB id) to the collections in add
// and takes it out of those in remove. found is false when the server doesn't
// have the title yet (it may not have scanned it).
func (c *Client) SyncTags(ctx context.Context, s Server, kind MediaKind, tmdbID int, add, remove []string) (found bool, err error) {
	if kind != MediaMovie && kind != MediaTV {
		return false, nil
	}
	if len(add) == 0 && len(remove) == 0 {
		return true, nil
	}
	switch s.Kind {
	case KindPlex:
		return c.plexSyncTags(ctx, s, kind, tmdbID, add, remove)
	case KindJellyfin, KindEmby:
		return c.embySyncTags(ctx, s, kind, tmdbID, add, remove)
	}
	return false, nil
}

// flexString reads a value Plex sends as a number in JSON and a string in XML.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexString(n.String())
	return nil
}

type plexTagged struct {
	RatingKey        string     `json:"ratingKey" xml:"ratingKey,attr"`
	LibrarySectionID flexString `json:"librarySectionID" xml:"librarySectionID,attr"`
	Collection       []struct {
		Tag string `json:"tag" xml:"tag,attr"`
	} `json:"Collection" xml:"Collection"`
}

type plexTaggedDoc struct {
	LibrarySectionID flexString   `json:"librarySectionID" xml:"librarySectionID,attr"`
	Metadata         []plexTagged `json:"Metadata" xml:"-"`
	Video            []plexTagged `json:"-" xml:"Video"`
	Directory        []plexTagged `json:"-" xml:"Directory"`
	MediaContainer   *struct {
		LibrarySectionID flexString   `json:"librarySectionID"`
		Metadata         []plexTagged `json:"Metadata"`
	} `json:"MediaContainer" xml:"-"`
}

func (c *Client) plexSyncTags(ctx context.Context, s Server, kind MediaKind, tmdbID int, add, remove []string) (bool, error) {
	item, found, err := c.plexFind(ctx, s, kind, tmdbID, nil)
	if err != nil || !found {
		return false, err
	}
	var doc plexTaggedDoc
	if err := c.do(ctx, s, http.MethodGet, "/library/metadata/"+url.PathEscape(item.ID), nil, nil, &doc); err != nil {
		return true, explain(s, err)
	}
	section, entries := doc.LibrarySectionID, append(append(doc.Metadata, doc.Video...), doc.Directory...)
	if doc.MediaContainer != nil {
		section, entries = doc.MediaContainer.LibrarySectionID, doc.MediaContainer.Metadata
	}
	if len(entries) > 0 && entries[0].LibrarySectionID != "" {
		section = entries[0].LibrarySectionID
	}
	if section == "" {
		return true, explain(s, errUnrecognised)
	}
	has := map[string]bool{}
	for _, e := range entries {
		for _, col := range e.Collection {
			has[strings.ToLower(col.Tag)] = true
		}
	}
	q := url.Values{"type": {"1"}, "id": {item.ID}, "collection.locked": {"1"}}
	if kind == MediaTV {
		q.Set("type", "2")
	}
	n := 0
	for _, t := range add {
		if !has[strings.ToLower(t)] {
			q.Set(fmt.Sprintf("collection[%d].tag.tag", n), t)
			n++
		}
	}
	var drop []string
	for _, t := range remove {
		if has[strings.ToLower(t)] {
			drop = append(drop, t)
		}
	}
	if len(drop) > 0 {
		q.Set("collection[].tag.tag-", strings.Join(drop, ","))
	}
	if n == 0 && len(drop) == 0 {
		return true, nil
	}
	return true, explain(s, c.do(ctx, s, http.MethodPut, "/library/sections/"+url.PathEscape(string(section))+"/all", q, nil, nil))
}

func (c *Client) embySyncTags(ctx context.Context, s Server, kind MediaKind, tmdbID int, add, remove []string) (bool, error) {
	item, found, err := c.embyFind(ctx, s, kind, tmdbID)
	if err != nil || !found {
		return false, err
	}
	for _, t := range add {
		id, err := c.embyCollection(ctx, s, t)
		if err != nil {
			return true, err
		}
		if id == "" {
			var made struct {
				ID string `json:"Id"`
			}
			if err := c.do(ctx, s, http.MethodPost, "/Collections", url.Values{"Name": {t}, "Ids": {item.ID}}, nil, &made); err != nil {
				return true, explain(s, err)
			}
			continue
		}
		if err := c.do(ctx, s, http.MethodPost, "/Collections/"+url.PathEscape(id)+"/Items", url.Values{"Ids": {item.ID}}, nil, nil); err != nil {
			return true, explain(s, err)
		}
	}
	for _, t := range remove {
		id, err := c.embyCollection(ctx, s, t)
		if err != nil {
			return true, err
		}
		if id == "" {
			continue
		}
		if err := c.do(ctx, s, http.MethodDelete, "/Collections/"+url.PathEscape(id)+"/Items", url.Values{"Ids": {item.ID}}, nil, nil); err != nil {
			return true, explain(s, err)
		}
	}
	return true, nil
}

// embyCollection finds the collection called name ("" when there is none).
func (c *Client) embyCollection(ctx context.Context, s Server, name string) (string, error) {
	var res embyItems
	q := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"BoxSet"}, "SearchTerm": {name}, "EnableImages": {"false"}, "EnableUserData": {"false"}}
	if err := c.do(ctx, s, http.MethodGet, "/Items", q, nil, &res); err != nil {
		return "", explain(s, err)
	}
	for _, it := range res.Items {
		if strings.EqualFold(strings.TrimSpace(it.Name), strings.TrimSpace(name)) {
			return it.ID, nil
		}
	}
	return "", nil
}
