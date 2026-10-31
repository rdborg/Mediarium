package indexers

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/netproxy"
	"github.com/rdborg/mediarium/internal/parser"
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
	Seeders     int // torrent-only (Torznab attr), 0 for usenet results
	// SeedersKnown is true when the indexer said how many seeders there are, so a
	// 0 in Seeders is a dead torrent and not just a missing number.
	SeedersKnown bool
	Peers        int    // torrent-only (Torznab attr), 0 for usenet results
	InfoHash     string // torrent-only, when the indexer reports it
	Priority     int    // the indexer's priority (1 to 50, lower is preferred); set by the engine
	// MinSeeders is the minimum the indexer was given for torrents (0 = off);
	// set by the engine on torrent results only.
	MinSeeders int
	// Cached is set by the search code (never by an indexer) when the debrid
	// service in use already has this torrent. False means "not cached or not
	// known".
	Cached bool
	// Torrent is true when the feed itself says this result is a torrent
	// (a torrent enclosure, an info hash, a magnet link). It wins over the
	// protocol the indexer was added with: a Torznab feed from Prowlarr that
	// was added as a Usenet indexer still hands out torrents.
	Torrent bool

	// parsed is the parse of Title, filled in by MergeResults so the checks
	// that follow do not parse the same name again and again.
	parsed *parser.Release
}

// Parsed is the release name parsed. A merged result already carries it; any
// other result is parsed on the spot.
func (r Result) Parsed() parser.Release {
	if r.parsed != nil {
		return *r.parsed
	}
	return parser.Parse(r.Title)
}

// NewznabClient talks to a single Newznab/Torznab-compatible API endpoint.
type NewznabClient struct {
	Name       string // display name for tagging results
	BaseURL    string // e.g. https://api.example.com
	APIKey     string
	httpClient *http.Client
	// ExtraRequests is how many further pages the last Search asked for, so the
	// caller can count them against the indexer's daily limit.
	ExtraRequests int
}

func NewNewznabClient(name, baseURL, apiKey string) *NewznabClient {
	return &NewznabClient{
		Name:       name,
		BaseURL:    newznabBase(baseURL),
		APIKey:     apiKey,
		httpClient: netproxy.Client(FeedWait(baseURL)+5*time.Second, netproxy.Indexers),
	}
}

// Waits for a Newznab or Torznab feed. A feed that Prowlarr or Jackett serves
// for a site they scrape page by page can take 20 to 40 seconds to answer, so
// those get a longer wait than an indexer's own API.
const (
	normalFeedWait = 15 * time.Second
	slowFeedWait   = 60 * time.Second
)

// The path may start with a folder when they sit behind a reverse proxy
// ("/prowlarr/5/api", "/jackett/api/v2.0/indexers/x/results/torznab").
var slowFeedPath = regexp.MustCompile(`^/\d+(/|$)|(^|/)(\d+/api|api/v2\.0/indexers/[^/]+/results/(torznab|newznab))(/|$)`)

// FeedWait is how long to wait for the feed at baseURL: longer for the feeds
// that Prowlarr (its own address and an indexer number, usually port 9696) or
// Jackett (/api/v2.0/indexers/<name>/results/torznab) serve.
func FeedWait(baseURL string) time.Duration {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return normalFeedWait
	}
	if slowFeedPath.MatchString(u.Path) || u.Port() == "9696" || u.Port() == "9117" {
		return slowFeedWait
	}
	return normalFeedWait
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

	// An indexer answers with a page of its own choosing when no size is asked
	// for (50 or 60 on some), so ask for a full page and read on while the feed
	// says there is more.
	q.Set("limit", strconv.Itoa(newznabPageSize))
	c.ExtraRequests = 0
	maxPages := newznabMaxPages
	if isManual(ctx) {
		maxPages = newznabMaxPagesManual // a person looking at the list wants all of it
	}
	var all []Result
	for page := 0; ; page++ {
		if page > 0 {
			q.Set("offset", strconv.Itoa(len(all)))
		}
		body, err := c.get(ctx, q)
		if err != nil {
			if page > 0 {
				return all, nil // the first page is still worth showing
			}
			return nil, err
		}
		results, total, err := parseNewznabPage(body, c.Name, c.APIKey)
		if err != nil {
			if page > 0 {
				return all, nil
			}
			return nil, err
		}
		all = append(all, results...)
		if len(results) == 0 || total <= len(all) || page+1 >= maxPages {
			return all, nil
		}
		c.ExtraRequests++
	}
}

const (
	// newznabPageSize is the number of releases asked for in one request.
	newznabPageSize = 100
	// newznabMaxPages is how many pages one automatic search reads at most;
	// newznabMaxPagesManual is the same for a search a person started, which
	// shows the whole list (a popular title can have far more than 300).
	newznabMaxPages       = 3
	newznabMaxPagesManual = 10
)

// Caps asks for the indexer's capabilities (t=caps). It is the cheapest
// question an indexer answers, and it needs a right API key, so it tells
// whether the indexer is up and the key still works. Some indexers count it
// against a daily limit too, but it is no search.
func (c *NewznabClient) Caps(ctx context.Context) error {
	q := url.Values{}
	q.Set("t", "caps")
	q.Set("apikey", c.APIKey)
	q.Set("o", "xml")
	body, err := c.get(ctx, q)
	if err != nil {
		return err
	}
	var root struct {
		XMLName     xml.Name
		Code        string `xml:"code,attr"`
		Description string `xml:"description,attr"`
	}
	switch err := xml.Unmarshal(body, &root); {
	case err == nil && root.XMLName.Local == "error":
		return fmt.Errorf("newznab error from %s: %s (code %s)", c.Name, maskSecret(truncate(root.Description, 300), c.APIKey), truncate(root.Code, 20))
	case err == nil && (root.XMLName.Local == "caps" || root.XMLName.Local == "rss"): // some feeds answer with a feed
		return nil
	case looksLikeWebPage(body):
		return fmt.Errorf("%s answered with a web page, not an indexer feed. Check the address: it must be the indexer's API address, not the site's home page", c.Name)
	}
	return fmt.Errorf("%s did not answer the capabilities question the way an indexer does", c.Name)
}

// CapCategory is one category an indexer lists in its capabilities.
type CapCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Categories asks for the indexer's capabilities (t=caps) and returns the
// categories it lists, sub-categories included (named "Parent/Child"), in
// the order of their numbers. It is only a help for choosing extra categories.
func (c *NewznabClient) Categories(ctx context.Context) ([]CapCategory, error) {
	q := url.Values{}
	q.Set("t", "caps")
	q.Set("apikey", c.APIKey)
	q.Set("o", "xml")
	body, err := c.get(ctx, q)
	if err != nil {
		return nil, err
	}
	return parseCapCategories(body)
}

func parseCapCategories(body []byte) ([]CapCategory, error) {
	var root struct {
		Categories []struct {
			ID     string `xml:"id,attr"`
			Name   string `xml:"name,attr"`
			Subcat []struct {
				ID   string `xml:"id,attr"`
				Name string `xml:"name,attr"`
			} `xml:"subcat"`
		} `xml:"categories>category"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("read the categories of the indexer: %w", err)
	}
	var out []CapCategory
	seen := map[int]bool{}
	add := func(id, name string) {
		n, err := strconv.Atoi(strings.TrimSpace(id))
		if err != nil || n <= 0 || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, CapCategory{ID: n, Name: truncate(strings.TrimSpace(name), 80)})
	}
	for _, cat := range root.Categories {
		add(cat.ID, cat.Name)
		for _, sub := range cat.Subcat {
			add(sub.ID, cat.Name+"/"+sub.Name)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// get sends one request to the indexer's API and returns the body of an answer
// with status 200.
func (c *NewznabClient) get(ctx context.Context, q url.Values) ([]byte, error) {
	reqURL := c.BaseURL + "/api?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build newznab request: %w", netguard.CleanError(err))
	}
	req.Header.Set("User-Agent", UserAgent)
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
		return nil, NewStatusError(resp.StatusCode, resp.Header,
			fmt.Sprintf("newznab request to %s: unexpected status %d: %s", c.Name, resp.StatusCode, c.mask(truncate(string(body), 200))), time.Now())
	}
	return body, nil
}

// UserAgent is what Mediarium calls itself when it asks an indexer for a
// search or an NZB. Go's own default ("Go-http-client") is on the block lists
// of many sites' Cloudflare rules, which answer it with a 403 page.
const UserAgent = "Mediarium (+https://github.com/rdborg/Mediarium)"

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
	results, _, err := parseNewznabPage(body, indexerName, apiKey)
	return results, err
}

// parseNewznabPage is parseNewznabFeed that also returns the total number of
// releases the indexer says it has for the search (0 when it does not say).
func parseNewznabPage(body []byte, indexerName, apiKey string) ([]Result, int, error) {
	var feed newznabRSS
	var err error
	if feed.Channel, err = decodeNewznabFeed(body); err != nil {
		// Newznab reports bad credentials and the like as a bare <error> root
		// element instead of an RSS feed; surface its description.
		var apiErr struct {
			XMLName     xml.Name `xml:"error"`
			Code        string   `xml:"code,attr"`
			Description string   `xml:"description,attr"`
		}
		if xml.Unmarshal(body, &apiErr) == nil && apiErr.Description != "" {
			return nil, 0, fmt.Errorf("newznab error from %s: %s (code %s)", indexerName, maskSecret(truncate(apiErr.Description, 300), apiKey), truncate(apiErr.Code, 20))
		}
		if looksLikeWebPage(body) {
			return nil, 0, fmt.Errorf("%s answered with a web page, not an indexer feed. Check the address: it must be the indexer's API address, not the site's home page. For an indexer in Prowlarr, open it in Prowlarr and copy its Torznab or Newznab feed address (it looks like http://192.168.1.10:9696/5/api), and use Prowlarr's API key. Or copy all your Prowlarr indexers at once under Settings > System > Move from other apps", indexerName)
		}
		// the parser quotes the text it stopped at, which could hold the key
		return nil, 0, fmt.Errorf("parse newznab response from %s: %s", indexerName, maskSecret(err.Error(), apiKey))
	}
	if feed.Channel.Error.Description != "" {
		return nil, 0, fmt.Errorf("newznab error from %s: %s", indexerName, maskSecret(truncate(feed.Channel.Error.Description, 300), apiKey))
	}

	items := feed.Channel.Items
	results := make([]Result, 0, len(items))
	for _, item := range items {
		results = append(results, item.toResult(indexerName))
	}
	return results, feed.Channel.Total, nil
}

// maxNewznabItems is how many releases of one reply are used.
const maxNewznabItems = 5000

// decodeNewznabFeed reads the channel of an RSS feed one item at a time and
// stops after maxNewznabItems: a reply of nothing but empty items would
// otherwise turn 16 MB of text into hundreds of MB of releases first.
func decodeNewznabFeed(body []byte) (newznabChannel, error) {
	var ch newznabChannel
	dec := xml.NewDecoder(bytes.NewReader(body))
	var root xml.StartElement
	for {
		tok, err := dec.Token()
		if err != nil {
			return ch, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			root = se
			break
		}
	}
	if root.Name.Local != "rss" {
		return ch, fmt.Errorf("expected element type <rss> but have <%s>", root.Name.Local)
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return ch, err
		}
		switch t := tok.(type) {
		case xml.EndElement: // </rss>
			return ch, nil
		case xml.StartElement:
			if t.Name.Local != "channel" {
				if err := dec.Skip(); err != nil {
					return ch, err
				}
				continue
			}
			full, err := decodeNewznabChannel(dec, &ch)
			if err != nil || full {
				return ch, err
			}
		}
	}
}

// decodeNewznabChannel reads one <channel> into ch. full is true once
// maxNewznabItems items have been read; the rest of the reply is not read.
func decodeNewznabChannel(dec *xml.Decoder, ch *newznabChannel) (full bool, err error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return false, err
		}
		switch t := tok.(type) {
		case xml.EndElement: // </channel>
			return false, nil
		case xml.StartElement:
			switch t.Name.Local {
			case "item":
				var item newznabItem
				if err := dec.DecodeElement(&item, &t); err != nil {
					return false, err
				}
				ch.Items = append(ch.Items, item)
				if len(ch.Items) >= maxNewznabItems {
					return true, nil
				}
			case "error":
				if err := dec.DecodeElement(&ch.Error, &t); err != nil {
					return false, err
				}
			case "response":
				for _, at := range t.Attr {
					if at.Name.Local == "total" {
						ch.Total, _ = strconv.Atoi(strings.TrimSpace(at.Value))
					}
				}
				if err := dec.Skip(); err != nil {
					return false, err
				}
			default:
				if err := dec.Skip(); err != nil {
					return false, err
				}
			}
		}
	}
}

// --- Newznab XML response shapes ---
// https://newznab.readthedocs.io/en/latest/misc/api/

type newznabRSS struct {
	XMLName xml.Name       `xml:"rss"`
	Channel newznabChannel `xml:"channel"`
}

type newznabChannel struct {
	Items []newznabItem `xml:"item"`
	Total int           // from <newznab:response total="..."/>, 0 when the feed has none
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
	Type   string `xml:"type,attr"`
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
	if magnet := item.attr("magneturl"); magnet != "" && strings.HasPrefix(magnet, "magnet:") {
		r.DownloadURL = magnet
	}
	r.Torrent = strings.Contains(strings.ToLower(item.Enclosure.Type), "bittorrent") ||
		strings.HasPrefix(r.DownloadURL, "magnet:") ||
		item.attr("infohash") != "" ||
		item.attr("seeders") != ""
	if size := item.attr("size"); size != "" {
		if n, err := strconv.ParseInt(size, 10, 64); err == nil {
			r.SizeBytes = n
		}
	}
	r.SizeBytes = saneSize(r.SizeBytes)
	r.Title = shortenText(strings.Join(strings.Fields(r.Title), " "), maxTitleBytes)
	if catStr := item.attr("category"); catStr != "" {
		if n, err := strconv.Atoi(catStr); err == nil {
			r.Categories = append(r.Categories, n)
		}
	}
	if seeders := item.attr("seeders"); seeders != "" {
		if n, err := strconv.Atoi(seeders); err == nil && n >= 0 {
			r.Seeders = int(sane(int64(n)))
			r.SeedersKnown = true
		}
	}
	r.InfoHash = strings.ToLower(item.attr("infohash"))
	if peers := item.attr("peers"); peers != "" {
		if n, err := strconv.Atoi(peers); err == nil && n > 0 {
			r.Peers = int(sane(int64(n)))
		}
	}
	if item.PubDate != "" {
		for _, layout := range pubDateLayouts {
			if t, err := time.Parse(layout, strings.TrimSpace(item.PubDate)); err == nil {
				// a date years off (year 9999) is no date at all
				r.PublishDate = saneDate(t)
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
