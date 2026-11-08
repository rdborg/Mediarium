package importlists

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
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/parser"
)

// An RSS feed list (#160): a feed of releases (an indexer's own feed, a
// tracker's, a site that posts new releases) or of titles. Each entry names a
// movie or a show: by the IMDb, TMDB or TheTVDB id a Newznab or Torznab feed
// puts in it, by a link to IMDb or TMDB, or else by its name read as a release
// name ("Dune.Part.Two.2024.1080p.WEB-DL-GRP" is Dune: Part Two from 2024).
// Entries with a download link can also be grabbed for titles in the library
// (internal/api decides that).

// MaxFeedBytes is the largest feed that is read.
const MaxFeedBytes = 8 << 20

// FeedEntry is one entry of a feed.
type FeedEntry struct {
	Title   string
	GUID    string
	Link    string // the download link (NZB, torrent file or magnet), when there is one
	Size    int64
	Torrent bool // the link is a torrent file or a magnet link
	IMDBID  string
	TMDBID  int
	TVDBID  int
}

type feedAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type rssDoc struct {
	XMLName xml.Name
	Items   []struct {
		Title     string `xml:"title"`
		GUID      string `xml:"guid"`
		Link      string `xml:"link"`
		Comments  string `xml:"comments"`
		Enclosure struct {
			URL    string `xml:"url,attr"`
			Length string `xml:"length,attr"`
			Type   string `xml:"type,attr"`
		} `xml:"enclosure"`
		Size  string     `xml:"size"`
		Attrs []feedAttr `xml:"attr"`
	} `xml:"channel>item"`
	Entries []struct {
		Title string `xml:"title"`
		ID    string `xml:"id"`
		Links []struct {
			Href   string `xml:"href,attr"`
			Rel    string `xml:"rel,attr"`
			Type   string `xml:"type,attr"`
			Length string `xml:"length,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

var (
	imdbInText = regexp.MustCompile(`imdb\.com/title/(tt\d{5,10})`)
	tmdbInText = regexp.MustCompile(`themoviedb\.org/(movie|tv)/(\d{1,9})`)
)

// ParseFeed reads an RSS 2.0 or Atom feed.
func ParseFeed(body []byte) ([]FeedEntry, error) {
	var doc rssDoc
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = false
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := dec.Decode(&doc); err != nil {
		return nil, errors.New("That address doesn't give an RSS or Atom feed")
	}
	switch strings.ToLower(doc.XMLName.Local) {
	case "rss", "feed", "rdf":
	default:
		return nil, errors.New("That address doesn't give an RSS or Atom feed")
	}
	var out []FeedEntry
	for _, it := range doc.Items {
		e := FeedEntry{Title: cleanFeedText(it.Title), GUID: strings.TrimSpace(it.GUID)}
		e.Size, _ = strconv.ParseInt(strings.TrimSpace(it.Size), 10, 64)
		switch {
		case it.Enclosure.URL != "":
			e.Link = strings.TrimSpace(it.Enclosure.URL)
			e.Torrent = isTorrentLink(e.Link, it.Enclosure.Type)
			if n, err := strconv.ParseInt(strings.TrimSpace(it.Enclosure.Length), 10, 64); err == nil && n > 0 {
				e.Size = n
			}
		case downloadLink(it.Link):
			e.Link = strings.TrimSpace(it.Link)
			e.Torrent = isTorrentLink(e.Link, "")
		}
		for _, a := range it.Attrs {
			v := strings.TrimSpace(a.Value)
			switch strings.ToLower(a.Name) {
			case "imdb", "imdbid":
				e.IMDBID = imdbID(v)
			case "tmdbid", "tmdb":
				e.TMDBID, _ = strconv.Atoi(v)
			case "tvdbid", "tvdb":
				e.TVDBID, _ = strconv.Atoi(v)
			case "size":
				if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
					e.Size = n
				}
			case "magneturl":
				if e.Link == "" {
					e.Link, e.Torrent = v, true
				}
			}
		}
		idsFromText(&e, it.Link+" "+it.Comments+" "+it.GUID)
		if e.Title != "" {
			out = append(out, e)
		}
	}
	for _, en := range doc.Entries {
		e := FeedEntry{Title: cleanFeedText(en.Title), GUID: strings.TrimSpace(en.ID)}
		var all []string
		for _, l := range en.Links {
			all = append(all, l.Href)
			if e.Link == "" && (strings.EqualFold(l.Rel, "enclosure") || downloadLink(l.Href)) {
				e.Link = strings.TrimSpace(l.Href)
				e.Torrent = isTorrentLink(e.Link, l.Type)
				e.Size, _ = strconv.ParseInt(strings.TrimSpace(l.Length), 10, 64)
			}
		}
		idsFromText(&e, strings.Join(all, " ")+" "+en.ID)
		if e.Title != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

func cleanFeedText(s string) string { return strings.Join(strings.Fields(s), " ") }

// imdbID accepts "tt0111161", "0111161" and "111161".
func imdbID(v string) string {
	v = strings.TrimPrefix(strings.ToLower(v), "tt")
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return fmt.Sprintf("tt%07d", n)
	}
	return ""
}

func idsFromText(e *FeedEntry, text string) {
	if e.IMDBID == "" {
		if m := imdbInText.FindStringSubmatch(text); m != nil {
			e.IMDBID = m[1]
		}
	}
	if e.TMDBID == 0 {
		if m := tmdbInText.FindStringSubmatch(text); m != nil {
			e.TMDBID, _ = strconv.Atoi(m[2])
		}
	}
}

// downloadLink reports whether a link points at a file to download rather
// than a web page about the release.
func downloadLink(link string) bool {
	l := strings.ToLower(strings.TrimSpace(link))
	if strings.HasPrefix(l, "magnet:") {
		return true
	}
	u, err := url.Parse(l)
	if err != nil {
		return false
	}
	return strings.HasSuffix(u.Path, ".nzb") || strings.HasSuffix(u.Path, ".torrent") || u.Query().Get("t") == "get" || strings.Contains(u.Path, "/getnzb") || strings.Contains(u.Path, "/download")
}

func isTorrentLink(link, mime string) bool {
	l := strings.ToLower(link)
	m := strings.ToLower(mime)
	return strings.HasPrefix(l, "magnet:") || strings.Contains(m, "bittorrent") || strings.Contains(strings.SplitN(l, "?", 2)[0], ".torrent")
}

// FeedItems turns the entries into the titles they name. The kind comes from
// the name: one with a season or episode is a show.
func FeedItems(entries []FeedEntry) []Item {
	out := make([]Item, 0, len(entries))
	for _, e := range entries {
		rel := parser.Parse(e.Title)
		it := Item{IMDBID: e.IMDBID, TMDBID: e.TMDBID, TVDBID: e.TVDBID, Title: rel.Title, Year: rel.Year, Kind: "movie"}
		if rel.Season > 0 || len(rel.Episodes) > 0 || rel.AirDate != "" || len(rel.Absolute) > 0 || rel.Special || e.TVDBID > 0 {
			it.Kind = "tv"
		}
		if it.Title == "" && it.IMDBID == "" && it.TMDBID == 0 && it.TVDBID == 0 {
			continue
		}
		out = append(out, it)
	}
	return out
}

// FeedAddress checks the address of a feed: a web address, http or https.
func FeedAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("Give the feed's address, starting with http:// or https://")
	}
	return u.String(), nil
}

// DownloadFeed fetches a feed. Feeds on the person's own network (an indexer
// they run) are allowed; only metadata services are refused.
func DownloadFeed(ctx context.Context, addr string) ([]byte, error) {
	return fetchFeed(ctx, newClient(), addr)
}

func fetchFeed(ctx context.Context, client *http.Client, addr string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, fmt.Errorf("read the feed: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, */*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach the feed: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{Service: "The feed", RetryAfter: retryAfter(resp.Header)}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errors.New("The feed turned Mediarium away. Check that its address has your key in it, if it needs one")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the feed answered with status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read the feed: %w", err)
	}
	if len(body) > MaxFeedBytes {
		return nil, errors.New("That feed is too big to read")
	}
	head := body
	if len(head) > 2048 {
		head = head[:2048]
	}
	if looksBlocked(resp, head) {
		return nil, ErrBlocked
	}
	return body, nil
}
