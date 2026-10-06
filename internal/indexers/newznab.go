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

	"github.com/rdborg/mediarium/internal/netguard"
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
	Priority    int    // the indexer's priority (1 preferred, 2 normal, 3 last resort); set by the engine
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
		BaseURL:    newznabBase(baseURL),
		APIKey:     apiKey,
		httpClient: netguard.Client(20 * time.Second),
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
		return nil, fmt.Errorf("build newznab request: %w", netguard.CleanError(err))
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxNewznabBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read newznab response from %s: %w", c.Name, err)
	}
	if len(body) > maxNewznabBytes {
		return nil, fmt.Errorf("newznab request to %s: the reply is larger than %d MB", c.Name, maxNewznabBytes>>20)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("newznab request to %s: unexpected status %d: %s", c.Name, resp.StatusCode, c.mask(truncate(string(body), 200)))
	}
	return parseNewznabFeed(body, c.Name, c.APIKey)
}

// maxNewznabBytes is the largest search reply that is read. A page of a
// hundred results is well under a megabyte.
const maxNewznabBytes = 16 << 20

// mask hides the API key if an indexer repeats it in the text of a reply.
func (c *NewznabClient) mask(s string) string { return maskSecret(s, c.APIKey) }

func maskSecret(s, secret string) string {
	if len(secret) < 4 {
		return s
	}
	return strings.ReplaceAll(s, secret, "REDACTED")
}

// newznabBase is the indexer's address without the "/api" every request
// adds. Prowlarr, Jackett and many indexers show their feed address with
// "/api" already on the end (and sometimes "?t=...&apikey=..."), and an
// address pasted like that must work too.
func newznabBase(raw string) string {
	base := strings.TrimSpace(raw)
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(strings.ToLower(base), "/api") {
		base = base[:len(base)-len("/api")]
	}
	return strings.TrimRight(base, "/")
}

// looksLikeWebPage reports whether a reply is an HTML page (a home page or a
// login page) rather than a feed.
func looksLikeWebPage(body []byte) bool {
	head := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 512)])))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html") || strings.Contains(head, "<html")
}

// parseNewznabFeed reads a Newznab or Torznab reply: an RSS feed of releases,
// or an <error> element saying why there are none. apiKey, when the reply
// repeats it, is hidden in any message returned.
func parseNewznabFeed(body []byte, indexerName, apiKey string) ([]Result, error) {
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
			return nil, fmt.Errorf("newznab error from %s: %s (code %s)", indexerName, maskSecret(truncate(apiErr.Description, 300), apiKey), truncate(apiErr.Code, 20))
		}
		if looksLikeWebPage(body) {
			return nil, fmt.Errorf("%s answered with a web page, not an indexer feed. Check the address: it must be the indexer's API address, not the site's home page. For an indexer in Prowlarr, open it in Prowlarr and copy its Torznab or Newznab feed address (it looks like http://192.168.1.10:9696/5/api), and use Prowlarr's API key. Or copy all your Prowlarr indexers at once under Settings > System > Move from other apps", indexerName)
		}
		// the parser quotes the text it stopped at, which could hold the key
		return nil, fmt.Errorf("parse newznab response from %s: %s", indexerName, maskSecret(err.Error(), apiKey))
	}
	if feed.Channel.Error.Description != "" {
		return nil, fmt.Errorf("newznab error from %s: %s", indexerName, maskSecret(truncate(feed.Channel.Error.Description, 300), apiKey))
	}

	items := feed.Channel.Items
	if len(items) > maxNewznabItems {
		items = items[:maxNewznabItems]
	}
	results := make([]Result, 0, len(items))
	for _, item := range items {
		results = append(results, item.toResult(indexerName))
	}
	return results, nil
}

// maxNewznabItems is how many releases of one reply are used.
const maxNewznabItems = 5000

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
	if r.SizeBytes < 0 {
		r.SizeBytes = 0
	}
	r.Title = shortenText(strings.Join(strings.Fields(r.Title), " "), maxTitleBytes)
	if catStr := item.attr("category"); catStr != "" {
		if n, err := strconv.Atoi(catStr); err == nil {
			r.Categories = append(r.Categories, n)
		}
	}
	if seeders := item.attr("seeders"); seeders != "" {
		if n, err := strconv.Atoi(seeders); err == nil && n > 0 {
			r.Seeders = n
		}
	}
	r.InfoHash = strings.ToLower(item.attr("infohash"))
	if peers := item.attr("peers"); peers != "" {
		if n, err := strconv.Atoi(peers); err == nil && n > 0 {
			r.Peers = n
		}
	}
	if item.PubDate != "" {
		for _, layout := range pubDateLayouts {
			if t, err := time.Parse(layout, strings.TrimSpace(item.PubDate)); err == nil {
				r.PublishDate = t
				break
			}
		}
	}
	return r
}

// pubDateLayouts are the ways indexers write an item's date: RFC 822 with a
// numeric zone (what the spec asks for), with a zone name, and with a
// single-digit day.
var pubDateLayouts = []string{time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", time.RFC3339}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return shortenText(s, n) + "..."
}
