package books

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Lists for the Discover page: what people are reading on Open Library now,
// and a browse by subject, year range and order.

// olDoc is one book in an Open Library search or trending answer.
type olDoc struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	AuthorName []string `json:"author_name"`
	AuthorKey  []string `json:"author_key"`
	Year       int      `json:"first_publish_year"`
	Cover      int      `json:"cover_i"`
	Format     []string `json:"format"`
}

const docFields = "key,title,author_name,author_key,first_publish_year,cover_i,format"

// founds turns answers into results, leaving out entries without a key or a
// title, and (when needCover) those without a cover: a Discover grid of
// blank cards is no use to anyone.
func founds(docs []olDoc, needCover bool) []Found {
	out := make([]Found, 0, len(docs))
	seen := map[string]bool{}
	for _, d := range docs {
		f := Found{Key: strings.TrimPrefix(d.Key, "/works/"), Title: d.Title, Year: d.Year, CoverID: d.Cover}
		f.HasEbook, f.HasAudio = Editions(d.Format)
		if len(d.AuthorName) > 0 {
			f.Author = d.AuthorName[0]
		}
		if len(d.AuthorKey) > 0 {
			f.AuthorKey = d.AuthorKey[0]
		}
		if f.Key == "" || f.Title == "" || seen[f.Key] || (needCover && f.CoverID == 0) {
			continue
		}
		seen[f.Key] = true
		out = append(out, f)
	}
	return out
}

// TrendingPeriods are the trending lists Open Library keeps.
var TrendingPeriods = map[string]bool{"daily": true, "weekly": true, "monthly": true, "yearly": true, "forever": true}

// Trending is what is being read most on Open Library over a period
// (daily, weekly, monthly, yearly or forever). page starts at 1.
func (c *Client) Trending(ctx context.Context, period string, page, limit int) ([]Found, error) {
	if !TrendingPeriods[period] {
		return nil, fmt.Errorf("unknown trending period %q", period)
	}
	if page < 1 {
		page = 1
	}
	return c.cachedFound(fmt.Sprintf("trending|%s|%d|%d", period, page, limit), func() ([]Found, error) {
		var res struct {
			Works []olDoc `json:"works"`
		}
		if err := c.get(ctx, "/trending/"+period+".json", url.Values{"limit": {fmt.Sprint(limit)}, "offset": {fmt.Sprint((page - 1) * limit)}}, &res); err != nil {
			return nil, err
		}
		list := founds(res.Works, true)
		c.addEditions(ctx, list)
		return list, nil
	})
}

// BrowseQuery picks books by subject and first publication year.
type BrowseQuery struct {
	Subject  string // an Open Library subject, "fantasy"; "" for any
	YearFrom int
	YearTo   int
	Sort     string // popular, rating, newest or oldest
	Page     int    // from 1
	Limit    int
}

// browseSorts maps our order names to Open Library's.
var browseSorts = map[string]string{"popular": "readinglog", "rating": "rating", "newest": "new", "oldest": "old"}

// SearchQuery builds the Open Library query for q (exported for tests).
func (q BrowseQuery) SearchQuery() string {
	var parts []string
	if s := strings.TrimSpace(q.Subject); s != "" {
		parts = append(parts, fmt.Sprintf("subject:%q", strings.ToLower(s)))
	}
	from, to := "*", "*"
	if q.YearFrom > 0 {
		from = fmt.Sprint(q.YearFrom)
	}
	if q.YearTo > 0 {
		to = fmt.Sprint(q.YearTo)
	}
	if from != "*" || to != "*" || len(parts) == 0 {
		if from == "*" && to == "*" {
			from, to = "1000", "3000" // Open Library needs something to search for
		}
		parts = append(parts, fmt.Sprintf("first_publish_year:[%s TO %s]", from, to))
	}
	return strings.Join(parts, " ")
}

// Browse lists books that match q, best first by its order.
func (c *Client) Browse(ctx context.Context, q BrowseQuery) ([]Found, error) {
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 40
	}
	if q.Page < 1 {
		q.Page = 1
	}
	sort, ok := browseSorts[q.Sort]
	if !ok {
		sort = browseSorts["popular"]
	}
	query := q.SearchQuery()
	return c.cachedFound(fmt.Sprintf("browse|%s|%s|%d|%d", query, sort, q.Page, q.Limit), func() ([]Found, error) {
		var res struct {
			Docs []olDoc `json:"docs"`
		}
		if err := c.get(ctx, "/search.json", url.Values{
			"q":      {query},
			"sort":   {sort},
			"fields": {docFields},
			"limit":  {fmt.Sprint(q.Limit)},
			"page":   {fmt.Sprint(q.Page)},
		}, &res); err != nil {
			return nil, err
		}
		return founds(res.Docs, true), nil
	})
}

// Subjects are the genres offered on the Discover page: label and Open
// Library subject.
var Subjects = []struct {
	Label   string `json:"label"`
	Subject string `json:"subject"`
}{
	{"Fantasy", "fantasy"},
	{"Science fiction", "science fiction"},
	{"Mystery", "mystery"},
	{"Thrillers", "thrillers"},
	{"Romance", "romance"},
	{"Horror", "horror"},
	{"Historical fiction", "historical fiction"},
	{"Young adult", "young adult fiction"},
	{"Children's", "juvenile fiction"},
	{"Biography", "biography"},
	{"History", "history"},
	{"Science", "science"},
	{"Self-help", "self-help"},
	{"Business", "business"},
	{"Cooking", "cooking"},
	{"Poetry", "poetry"},
	{"Comics", "comics & graphic novels"},
}

// Editions reads Open Library's list of edition formats ("Paperback",
// "eBook", "Audio CD", "Audible eAudiobook", "MP3 CD"...) and reports whether
// an ebook and an audiobook edition exist. Spelling and case vary a lot.
func Editions(formats []string) (ebook, audio bool) {
	for _, f := range formats {
		l := strings.ToLower(f)
		switch {
		case strings.Contains(l, "audio") || strings.Contains(l, "sound recording") || strings.Contains(l, "mp3") ||
			strings.Contains(l, "cassette") || strings.Contains(l, "audible"):
			audio = true
		case strings.Contains(l, "ebook") || strings.Contains(l, "e-book") || strings.Contains(l, "epub") || strings.Contains(l, "kindle") ||
			strings.Contains(l, "electronic resource") || strings.Contains(l, "digital"):
			ebook = true
		}
	}
	return ebook, audio
}

// addEditions fills in which editions exist for books from a list that
// doesn't say (Open Library's trending lists), with one search for all of
// them. Without an answer the books simply show no edition banner.
func (c *Client) addEditions(ctx context.Context, list []Found) {
	if len(list) == 0 {
		return
	}
	keys := make([]string, 0, len(list))
	for _, f := range list {
		keys = append(keys, "/works/"+f.Key)
	}
	var res struct {
		Docs []struct {
			Key    string   `json:"key"`
			Format []string `json:"format"`
		} `json:"docs"`
	}
	q := url.Values{"q": {"key:(" + strings.Join(keys, " OR ") + ")"}, "fields": {"key,format"}, "limit": {fmt.Sprint(len(keys))}}
	if err := c.get(ctx, "/search.json", q, &res); err != nil {
		return
	}
	byKey := map[string][]string{}
	for _, d := range res.Docs {
		byKey[strings.TrimPrefix(d.Key, "/works/")] = d.Format
	}
	for i := range list {
		list[i].HasEbook, list[i].HasAudio = Editions(byKey[list[i].Key])
	}
}
