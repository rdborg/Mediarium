package books

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Google Books is asked after Open Library and Hardcover, and only to fill a
// gap: a missing cover, a missing description, or a book neither of the others
// lists. It works with no API key. A saved key is sent when there is one, for
// people who reach Google's limit. The picture is copied onto this server.

const googleBooksBase = "https://www.googleapis.com/books/v1"

// ErrGoogleBooksKey means the key was refused or the Books API is not enabled for it.
var ErrGoogleBooksKey = errors.New("google books rejected the api key")

var errGoogleBooksDown = errors.New("google books didn't answer")

// GoogleHit is one volume as Google returned it. CoverURL is the picture
// Google hosts; search copies it onto this server before the page sees it.
type GoogleHit struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Author      string `json:"author,omitempty"`
	Year        int    `json:"year,omitempty"`
	CoverURL    string `json:"coverUrl,omitempty"`
	InfoURL     string `json:"infoUrl"`
	ISBN        string `json:"isbn,omitempty"`
	Language    string `json:"language,omitempty"`
	Description string `json:"description,omitempty"`
}

// GoogleBooks talks to the Google Books API.
type GoogleBooks struct {
	Base      string // https://www.googleapis.com/books/v1; tests point it elsewhere
	Key       string
	UserAgent string
	HTTP      *http.Client
}

// NewGoogleBooks returns a client for the real API. key may be empty: Search
// still calls Google, and sends the key only when one is set.
func NewGoogleBooks(key, userAgent string) *GoogleBooks {
	return &GoogleBooks{
		Base:      googleBooksBase,
		Key:       strings.TrimSpace(key),
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: 15 * time.Second},
	}
}

// CleanGoogleBooksKey trims a key. An empty key is valid and means "remove".
// The bool is false when the text cannot be a key.
func CleanGoogleBooksKey(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	if len(s) < 10 || len(s) > 256 || strings.ContainsAny(s, " \r\n\t") {
		return "", false
	}
	return s, true
}

// GoogleBooksLanguage is the ISO 639-1 code Google Books' langRestrict wants,
// taken from a movie-and-show info tag such as "it-IT".
func GoogleBooksLanguage(infoTag string) string {
	tag := strings.ToLower(strings.TrimSpace(infoTag))
	if i := strings.IndexByte(tag, '-'); i > 0 {
		tag = tag[:i]
	}
	switch tag {
	case "nb", "nn":
		return "no"
	}
	if len(tag) == 2 || len(tag) == 3 {
		return tag
	}
	return "en"
}

const googleKeyPrefix = "gb-"

// ValidGoogleID reports whether id can be a Google Books volume id in a
// library key. The key has to stay short enough for the series address.
func ValidGoogleID(id string) bool {
	if len(id) < 1 || len(id) > 29 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// GoogleKey is the library key of a book known by its Google Books volume id.
func GoogleKey(id string) string {
	if !ValidGoogleID(id) {
		return ""
	}
	return googleKeyPrefix + id
}

// ParseGoogleKey reports the volume id in a key from GoogleKey.
func ParseGoogleKey(key string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(key), googleKeyPrefix)
	if !ok || !ValidGoogleID(rest) {
		return "", false
	}
	return rest, true
}

// GoogleCoverURL is the address of a Google Books cover kept on this server.
func GoogleCoverURL(id string) string {
	if !ValidGoogleID(id) {
		return ""
	}
	return "/api/book-covers/gb-" + id + ".jpg"
}

// ParseGoogleCover reports the volume id in a cover file name from GoogleCoverURL.
func ParseGoogleCover(file string) (string, bool) {
	stem, ok := strings.CutSuffix(file, ".jpg")
	if !ok {
		return "", false
	}
	return ParseGoogleKey(stem)
}

// Found is this volume as a search result. The key is "gb-<id>" until the
// book can be matched to Open Library.
func (h GoogleHit) Found() (Found, bool) {
	id := strings.TrimSpace(h.ID)
	if !ValidGoogleID(id) || strings.TrimSpace(h.Title) == "" {
		return Found{}, false
	}
	return Found{
		Key:         GoogleKey(id),
		Title:       h.Title,
		Author:      h.Author,
		Year:        h.Year,
		GoogleID:    id,
		GoogleCover: h.CoverURL,
		GoogleBlurb: h.Description,
	}, true
}

// Results lists volumes for q as search results, in Google's order.
func (g *GoogleBooks) Results(ctx context.Context, q, lang string, limit int) ([]Found, error) {
	hits, err := g.Search(ctx, q, lang, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Found, 0, len(hits))
	seen := map[string]bool{}
	for _, h := range hits {
		f, ok := h.Found()
		if !ok || seen[f.GoogleID] {
			continue
		}
		seen[f.GoogleID] = true
		out = append(out, f)
	}
	return out, nil
}

// Book is one volume's details for its info page, or ErrNotFound.
func (g *GoogleBooks) Book(ctx context.Context, id string) (WorkInfo, error) {
	if !ValidGoogleID(id) {
		return WorkInfo{}, ErrNotFound
	}
	status, body, err := g.fetch(ctx, "/volumes/"+url.PathEscape(id), nil)
	if err != nil {
		return WorkInfo{}, err
	}
	if status == http.StatusNotFound {
		return WorkInfo{}, ErrNotFound
	}
	if status == http.StatusBadRequest || status == http.StatusForbidden {
		return WorkInfo{}, ErrGoogleBooksKey
	}
	if status != http.StatusOK {
		return WorkInfo{}, errGoogleBooksDown
	}
	var v googleVolume
	if err := json.Unmarshal(body, &v); err != nil {
		return WorkInfo{}, errGoogleBooksDown
	}
	hit, ok := v.hit()
	if !ok || hit.ID != id {
		return WorkInfo{}, ErrNotFound
	}
	// A volume page can carry the blurb only as the search extract. The list
	// often omits both, so the work page asks for the volume when it has none.
	if hit.Description == "" && v.SearchInfo != nil {
		hit.Description = plainDescription(v.SearchInfo.TextSnippet)
	}
	f, ok := hit.Found()
	if !ok {
		return WorkInfo{}, ErrNotFound
	}
	return WorkInfo{Found: f, Description: hit.Description, Subjects: []string{}}, nil
}

// ImageURL is the picture address Google Books has for a volume, or ErrNotFound.
func (g *GoogleBooks) ImageURL(ctx context.Context, id string) (string, error) {
	info, err := g.Book(ctx, id)
	if err != nil {
		return "", err
	}
	if info.GoogleCover == "" {
		return "", ErrNotFound
	}
	return info.GoogleCover, nil
}

// MergeGoogle adds Google Books hits to a search. A hit that names the same
// book keeps the Open Library or Hardcover key, and fills in a cover or a
// description only when that result has none. Years more than one apart are
// different books. A hit the others don't list is appended, with Google's
// title, author, year, description and cover.
func MergeGoogle(openLib, goog []Found) []Found {
	if len(goog) == 0 {
		return openLib
	}
	out := append([]Found(nil), openLib...)
	for _, h := range goog {
		if h.GoogleID == "" || strings.TrimSpace(h.Title) == "" {
			continue
		}
		folded := false
		for i := range out {
			if !SameTitle(out[i].Title, h.Title, true) || !SameAuthor(out[i].Author, h.Author) || yearsTooFar(out[i].Year, h.Year) {
				continue
			}
			folded = true
			if out[i].GoogleID == "" {
				out[i].GoogleID = h.GoogleID
			}
			if out[i].CoverID == 0 && out[i].CoverImage == "" && h.GoogleCover != "" {
				out[i].GoogleCover = h.GoogleCover
			}
			if out[i].GoogleBlurb == "" && h.GoogleBlurb != "" {
				out[i].GoogleBlurb = h.GoogleBlurb
			}
			break
		}
		if folded {
			continue
		}
		if h.Key == "" {
			h.Key = GoogleKey(h.GoogleID)
		}
		out = append(out, h)
	}
	return out
}

// Check asks Google whether the key is accepted.
func (g *GoogleBooks) Check(ctx context.Context) error {
	status, _, err := g.fetch(ctx, "/volumes", url.Values{
		"q":          {"isbn:9780140328721"},
		"maxResults": {"1"},
	})
	if err != nil {
		return err
	}
	if status == http.StatusBadRequest || status == http.StatusForbidden {
		return ErrGoogleBooksKey
	}
	if status != http.StatusOK {
		return errGoogleBooksDown
	}
	return nil
}

// Search lists volumes for q in Google's order, restricted to lang (an info
// tag such as "it-IT", or already an ISO code). It is never sorted again.
// An empty key still asks Google.
func (g *GoogleBooks) Search(ctx context.Context, q, lang string, limit int) ([]GoogleHit, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return []GoogleHit{}, nil
	}
	if limit <= 0 || limit > 40 {
		limit = 12
	}
	status, body, err := g.fetch(ctx, "/volumes", url.Values{
		"q":            {q},
		"maxResults":   {strconv.Itoa(limit)},
		"langRestrict": {GoogleBooksLanguage(lang)},
		"printType":    {"books"},
	})
	if err != nil {
		return nil, err
	}
	if status == http.StatusBadRequest || status == http.StatusForbidden {
		return nil, ErrGoogleBooksKey
	}
	if status != http.StatusOK {
		return nil, errGoogleBooksDown
	}
	var resp struct {
		Items []googleVolume `json:"items"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errGoogleBooksDown
	}
	out := make([]GoogleHit, 0, len(resp.Items))
	for _, v := range resp.Items {
		if hit, ok := v.hit(); ok {
			out = append(out, hit)
		}
	}
	return out, nil
}

func (g *GoogleBooks) fetch(ctx context.Context, path string, query url.Values) (int, []byte, error) {
	if query == nil {
		query = url.Values{}
	}
	if key := strings.TrimSpace(g.Key); key != "" {
		query.Set("key", key)
	}
	endpoint := strings.TrimRight(g.Base, "/") + path + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, errGoogleBooksDown
	}
	if g.UserAgent != "" {
		req.Header.Set("User-Agent", g.UserAgent)
	}
	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// The request address contains the key, so the error from the client is not returned.
		return 0, nil, errGoogleBooksDown
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, errGoogleBooksDown
	}
	return resp.StatusCode, body, nil
}

type googleVolume struct {
	ID         string            `json:"id"`
	VolumeInfo googleVolumeInfo  `json:"volumeInfo"`
	SearchInfo *googleSearchInfo `json:"searchInfo"`
}

type googleVolumeInfo struct {
	Title               string             `json:"title"`
	Authors             []string           `json:"authors"`
	PublishedDate       string             `json:"publishedDate"`
	Description         string             `json:"description"`
	Language            string             `json:"language"`
	InfoLink            string             `json:"infoLink"`
	CanonicalVolumeLink string             `json:"canonicalVolumeLink"`
	ImageLinks          *googleImageLinks  `json:"imageLinks"`
	IndustryIdentifiers []googleIdentifier `json:"industryIdentifiers"`
}

type googleImageLinks struct {
	SmallThumbnail string `json:"smallThumbnail"`
	Thumbnail      string `json:"thumbnail"`
	Small          string `json:"small"`
	Medium         string `json:"medium"`
	Large          string `json:"large"`
	ExtraLarge     string `json:"extraLarge"`
}

type googleIdentifier struct {
	Type       string `json:"type"`
	Identifier string `json:"identifier"`
}

// googleSearchInfo is the short extract Google sometimes sends beside the
// volume, when volumeInfo.description itself is empty.
type googleSearchInfo struct {
	TextSnippet string `json:"textSnippet"`
}

func (v googleVolume) hit() (GoogleHit, bool) {
	title := strings.TrimSpace(v.VolumeInfo.Title)
	id := strings.TrimSpace(v.ID)
	if title == "" || id == "" {
		return GoogleHit{}, false
	}
	info := strings.TrimSpace(v.VolumeInfo.CanonicalVolumeLink)
	if info == "" {
		info = strings.TrimSpace(v.VolumeInfo.InfoLink)
	}
	if info == "" {
		info = "https://books.google.com/books?id=" + url.QueryEscape(id)
	}
	return GoogleHit{
		ID:          id,
		Title:       title,
		Author:      strings.Join(v.VolumeInfo.Authors, ", "),
		Year:        googleYear(v.VolumeInfo.PublishedDate),
		CoverURL:    googleCover(v.VolumeInfo.ImageLinks),
		InfoURL:     info,
		ISBN:        googleISBN(v.VolumeInfo.IndustryIdentifiers),
		Language:    strings.TrimSpace(v.VolumeInfo.Language),
		Description: plainDescription(v.VolumeInfo.Description),
	}, true
}

func googleYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil || y <= 0 {
		return 0
	}
	return y
}

func googleISBN(ids []googleIdentifier) string {
	isbn := ""
	for _, id := range ids {
		switch id.Type {
		case "ISBN_13":
			return strings.TrimSpace(id.Identifier)
		case "ISBN_10":
			if isbn == "" {
				isbn = strings.TrimSpace(id.Identifier)
			}
		}
	}
	return isbn
}

func googleCover(links *googleImageLinks) string {
	if links == nil {
		return ""
	}
	for _, value := range []string{links.ExtraLarge, links.Large, links.Medium, links.Small, links.Thumbnail, links.SmallThumbnail} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if parsed.Scheme == "http" && (host == "books.google.com" || strings.HasSuffix(host, ".google.com") || strings.HasSuffix(host, ".googleusercontent.com")) {
			parsed.Scheme = "https"
		}
		// Leave zoom as Google sent it. zoom=1 is the thumbnail that exists for
		// almost every book. Asking for zoom=3 when Google has no larger scan
		// returns a blank "image not available" picture, which we would then keep.
		out := strings.ReplaceAll(parsed.String(), "&edge=curl", "")
		out = strings.ReplaceAll(out, "?edge=curl&", "?")
		out = strings.ReplaceAll(out, "?edge=curl", "")
		return out
	}
	return ""
}
