package indexers

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Result is a single normalized search result, regardless of which
// indexer/protocol produced it ("one unified result list").
type Result struct {
	Title       string
	IndexerName string
	Protocol    Protocol // usenet or torrent — set by the engine, not this client
	DownloadURL string   // .nzb URL, or a magnet URI / .torrent URL for torrent results
	InfoURL     string
	SizeBytes   int64
	PublishDate time.Time
	Categories  []int
	Seeders     int    // torrent-only (Torznab attr), 0 for usenet results
	Peers       int    // torrent-only (Torznab attr), 0 for usenet results
	InfoHash    string // torrent-only, when the indexer reports it
}

// NewznabClient talks to a single Newznab/Torznab-compatible API endpoint.
type NewznabClient struct {
	Name       string // display name for tagging results
	BaseURL    string // e.g. https://api.example.com
	APIKey     string
	httpClient *http.Client
}

func NewNewznabClient(name, baseURL, apiKey string) *NewznabClient {
	return &NewznabClient{
		Name:       name,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// Search issues a t=search (or t=movie for an IMDb-id lookup) query.
// categories, if non-empty, are passed through as Newznab category IDs
// (2000 = Movies, per the Newznab category spec).
func (c *NewznabClient) Search(ctx context.Context, query string, categories []int) ([]Result, error) {
	q := url.Values{}
	q.Set("t", "search")
	q.Set("apikey", c.APIKey)
	q.Set("o", "xml")
	if query != "" {
		q.Set("q", query)
	}
	if len(categories) > 0 {
		cats := make([]string, len(categories))
		for i, cat := range categories {
			cats[i] = strconv.Itoa(cat)
		}
		q.Set("cat", strings.Join(cats, ","))
	}

	reqURL := c.BaseURL + "/api?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build newznab request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// net/http's error carries the full request URL, which holds the API
		// key; keep it out of anything that shows or logs this error.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("newznab request to %s: %w", c.Name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read newznab response from %s: %w", c.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("newznab request to %s: unexpected status %d: %s", c.Name, resp.StatusCode, truncate(string(body), 200))
	}

	var feed newznabRSS
	if err := xml.Unmarshal(body, &feed); err != nil {
		// Newznab reports bad credentials and the like as a bare <error> root
		// element instead of an RSS feed; surface its description.
		var apiErr struct {
			XMLName     xml.Name `xml:"error"`
			Code        string   `xml:"code,attr"`
			Description string   `xml:"description,attr"`
		}
		if xml.Unmarshal(body, &apiErr) == nil && apiErr.Description != "" {
			return nil, fmt.Errorf("newznab error from %s: %s (code %s)", c.Name, apiErr.Description, apiErr.Code)
		}
		return nil, fmt.Errorf("parse newznab response from %s: %w", c.Name, err)
	}
	if feed.Channel.Error.Description != "" {
		return nil, fmt.Errorf("newznab error from %s: %s", c.Name, feed.Channel.Error.Description)
	}

	results := make([]Result, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		results = append(results, item.toResult(c.Name))
	}
	return results, nil
}

// --- Newznab XML response shapes ---
// https://newznab.readthedocs.io/en/latest/misc/api/

type newznabRSS struct {
	XMLName xml.Name       `xml:"rss"`
	Channel newznabChannel `xml:"channel"`
}

type newznabChannel struct {
	Items []newznabItem `xml:"item"`
	Error newznabAPIErr `xml:"error"`
}

type newznabAPIErr struct {
	Description string `xml:"description,attr"`
}

type newznabItem struct {
	Title     string           `xml:"title"`
	GUID      string           `xml:"guid"`
	Link      string           `xml:"link"`
	Comments  string           `xml:"comments"`
	PubDate   string           `xml:"pubDate"`
	Enclosure newznabEnclosure `xml:"enclosure"`
	Attrs     []newznabAttr    `xml:"attr"`
}

type newznabEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
}

type newznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

func (item newznabItem) attr(name string) string {
	for _, a := range item.Attrs {
		if strings.EqualFold(a.Name, name) {
			return a.Value
		}
	}
	return ""
}

func (item newznabItem) toResult(indexerName string) Result {
	r := Result{
		Title:       item.Title,
		IndexerName: indexerName,
		DownloadURL: item.Enclosure.URL,
		InfoURL:     item.Comments,
		SizeBytes:   item.Enclosure.Length,
	}
	if r.DownloadURL == "" {
		r.DownloadURL = item.Link
	}
	if size := item.attr("size"); size != "" {
		if n, err := strconv.ParseInt(size, 10, 64); err == nil {
			r.SizeBytes = n
		}
	}
	if catStr := item.attr("category"); catStr != "" {
		if n, err := strconv.Atoi(catStr); err == nil {
			r.Categories = append(r.Categories, n)
		}
	}
	if seeders := item.attr("seeders"); seeders != "" {
		if n, err := strconv.Atoi(seeders); err == nil {
			r.Seeders = n
		}
	}
	r.InfoHash = strings.ToLower(item.attr("infohash"))
	if peers := item.attr("peers"); peers != "" {
		if n, err := strconv.Atoi(peers); err == nil {
			r.Peers = n
		}
	}
	if item.PubDate != "" {
		if t, err := time.Parse(time.RFC1123Z, item.PubDate); err == nil {
			r.PublishDate = t
		}
	}
	return r
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
