package books

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Audiobook details: who reads the book and how long it runs, from Audnexus
// (api.audnex.us), a free, open service over Audible's catalogue that
// Audiobookshelf uses too. Audnexus looks books up by their Audible id
// (ASIN), which Audible's public catalogue search gives for a title and
// author. Neither needs an account.

// Where the two services are. Variables so a test run can point them at a
// closed port and never reach the internet.
var (
	AudibleSearchURL = "https://api.audible.com/1.0/catalog/products"
	AudnexusURL      = "https://api.audnex.us"
)

// AudioDetails is what Audnexus knows about an audiobook.
type AudioDetails struct {
	ASIN           string
	Title          string
	Narrators      []string
	RuntimeMin     int
	ReleaseDate    string // "2021-05-04"
	SeriesName     string
	SeriesPosition string
	Abridged       bool
}

// Audnexus finds audiobook details.
type Audnexus struct {
	AudibleBase  string // tests point these elsewhere
	AudnexusBase string
	Region       string // Audible store: "us" unless set
	UserAgent    string
	HTTP         *http.Client
}

// NewAudnexus returns a client for the real services.
func NewAudnexus(userAgent string) *Audnexus {
	return &Audnexus{AudibleBase: AudibleSearchURL, AudnexusBase: AudnexusURL, Region: "us", UserAgent: userAgent, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

func (a *Audnexus) getJSON(ctx context.Context, u string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if a.UserAgent != "" {
		req.Header.Set("User-Agent", a.UserAgent)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("audiobook details: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("audiobook details: the service answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(into); err != nil {
		return fmt.Errorf("audiobook details: read the answer: %w", err)
	}
	return nil
}

// candidates asks Audible's catalogue for the audiobooks matching title and
// author, best match first.
func (a *Audnexus) candidates(ctx context.Context, title, author string) ([]string, error) {
	q := url.Values{"num_results": {"5"}, "products_sort_by": {"Relevance"}, "title": {title}}
	if author != "" {
		q.Set("author", author)
	}
	var res struct {
		Products []struct {
			ASIN string `json:"asin"`
		} `json:"products"`
	}
	if err := a.getJSON(ctx, strings.TrimRight(a.AudibleBase, "/")+"?"+q.Encode(), &res); err != nil {
		return nil, err
	}
	var out []string
	for _, p := range res.Products {
		if asinPattern(p.ASIN) {
			out = append(out, p.ASIN)
		}
	}
	return out, nil
}

func asinPattern(s string) bool {
	if len(s) != 10 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// Details fetches one audiobook by its ASIN.
func (a *Audnexus) Details(ctx context.Context, asin string) (AudioDetails, error) {
	if !asinPattern(asin) {
		return AudioDetails{}, ErrNotFound
	}
	region := a.Region
	if region == "" {
		region = "us"
	}
	var res struct {
		ASIN      string `json:"asin"`
		Title     string `json:"title"`
		Narrators []struct {
			Name string `json:"name"`
		} `json:"narrators"`
		Runtime       int    `json:"runtimeLengthMin"`
		FormatType    string `json:"formatType"`
		ReleaseDate   string `json:"releaseDate"`
		SeriesPrimary *struct {
			Name     string `json:"name"`
			Position string `json:"position"`
		} `json:"seriesPrimary"`
	}
	u := fmt.Sprintf("%s/books/%s?region=%s", strings.TrimRight(a.AudnexusBase, "/"), url.PathEscape(asin), url.QueryEscape(region))
	if err := a.getJSON(ctx, u, &res); err != nil {
		return AudioDetails{}, err
	}
	d := AudioDetails{ASIN: res.ASIN, Title: res.Title, RuntimeMin: res.Runtime, Abridged: strings.EqualFold(res.FormatType, "abridged")}
	if d.ASIN == "" {
		d.ASIN = asin
	}
	for _, n := range res.Narrators {
		if name := strings.TrimSpace(n.Name); name != "" {
			d.Narrators = append(d.Narrators, name)
		}
	}
	if len(res.ReleaseDate) >= 10 {
		d.ReleaseDate = res.ReleaseDate[:10]
	}
	if res.SeriesPrimary != nil {
		d.SeriesName, d.SeriesPosition = strings.TrimSpace(res.SeriesPrimary.Name), strings.TrimSpace(res.SeriesPrimary.Position)
	}
	return d, nil
}

// translated marks Audible editions in another language ("Project Hail Mary
// (Italian edition)").
func translated(title string) bool {
	t := strings.ToLower(title)
	return strings.Contains(t, " edition)") && !strings.Contains(t, "anniversary edition)") && !strings.Contains(t, "unabridged edition)")
}

// Lookup finds the audiobook of a title by an author. Of Audible's best
// matches whose title is the book's own (not a translation), it picks the
// full reading: unabridged first, then the longest, so a short dramatisation
// or a summary doesn't win. ErrNotFound when there is none.
func (a *Audnexus) Lookup(ctx context.Context, title, author string) (AudioDetails, error) {
	asins, err := a.candidates(ctx, title, author)
	if err != nil {
		return AudioDetails{}, err
	}
	var best *AudioDetails
	for i, asin := range asins {
		if i >= 4 {
			break
		}
		d, err := a.Details(ctx, asin)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return AudioDetails{}, err
		}
		if translated(d.Title) || !SameTitle(d.Title, title, false) {
			continue
		}
		if best == nil || (best.Abridged && !d.Abridged) || (best.Abridged == d.Abridged && d.RuntimeMin > best.RuntimeMin) {
			d := d
			best = &d
		}
	}
	if best == nil {
		return AudioDetails{}, ErrNotFound
	}
	return *best, nil
}

// SetAudioDetails stores what Audnexus knows about a book's audiobook, and
// that it was looked up (also when nothing was found, so it isn't asked again
// and again).
func (r *Repo) SetAudioDetails(id int64, d AudioDetails) error {
	_, err := r.db.Exec(`UPDATE books SET asin = ?, narrators = ?, runtime_min = ?, audio_checked_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
		d.ASIN, strings.Join(d.Narrators, ", "), d.RuntimeMin, id)
	if err != nil {
		return fmt.Errorf("set audiobook details: %w", err)
	}
	return nil
}
