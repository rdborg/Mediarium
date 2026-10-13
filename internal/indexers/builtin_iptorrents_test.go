package indexers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The IPTorrents definition was written from public descriptions of the
// site's pages, not from the live site. These tests run it against a fake
// site laid out the way those descriptions say, so they prove the definition
// reads that layout, not that the real site still looks like it.

const iptCookie = "uid=4711; pass=0123456789abcdef"
const iptUserAgent = "Mozilla/5.0 (X11; Fedora; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0"

// iptRow is one result row. withFiles adds the optional files column that
// the profile option "show files count" turns on (ten cells instead of nine).
func iptRow(cat, id int, title, tags, ago, uploader, size string, snatches, seeders, leechers int, free, withFiles bool) string {
	freeMark := ""
	if free {
		freeMark = ` <span class="free">FREE</span>`
	}
	files := ""
	if withFiles {
		files = `<td class="ac t_files">12</td>`
	}
	return fmt.Sprintf(`<tr>
<td class="ac"><a href="?%d"><img src="/icons/c%d.png" alt="cat"></a></td>
<td class="al"><a class="hv" href="/details.php?id=%d">%s</a>%s<div class="sub"><span class="t_tag">%s</span> | %s by <a href="/u/%s">%s</a></div></td>
<td class="ac"><a href="/download.php/%d/%s.torrent"><img src="/dl.png" alt="download"></a></td>
<td class="ac"><a href="/bookmark.php?id=%d">+</a></td>
<td class="ac">3</td>
<td class="ac">%s</td>%s
<td class="ac">%d</td>
<td class="ac t_seeders">%d</td>
<td class="ac t_leechers">%d</td>
</tr>`, cat, cat, id, title, freeMark, tags, ago, uploader, uploader, id, strings.ReplaceAll(title, " ", "."), id, size, files, snatches, seeders, leechers)
}

type iptSite struct {
	*httptest.Server
	lastQS      atomic.Value // raw query of the last search
	lastUA      atomic.Value // User-Agent of the last request
	withFiles   atomic.Bool
	signedOutAt atomic.Bool // when set, the site no longer accepts the cookie
	iconLogout  atomic.Bool // when set, the sign-out link is an icon with a tooltip, not the /lout.php address
	downloads   atomic.Int32
}

func newIPTSite(t *testing.T) *iptSite {
	t.Helper()
	s := &iptSite{}
	signedIn := func(r *http.Request) bool {
		if s.signedOutAt.Load() {
			return false
		}
		return r.Header.Get("Cookie") == iptCookie
	}
	page := func(w http.ResponseWriter, r *http.Request, body string) {
		nav := `<a href="/login.php">Login</a>`
		if signedIn(r) {
			nav = `<a href="/lout.php">Logout</a>`
			if s.iconLogout.Load() {
				nav = `<span class="tTipWrap"><i class="fa fa-power-off"></i><div class="tTip ttop">Log out</div></span>`
			}
		}
		io.WriteString(w, `<html><body>`+nav+body+`</body></html>`)
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lastUA.Store(r.UserAgent())
		switch {
		case r.URL.Path == "/":
			page(w, r, "<p>front page</p>")
		case r.URL.Path == "/t":
			s.lastQS.Store(r.URL.RawQuery)
			if !signedIn(r) {
				page(w, r, "")
				return
			}
			f := s.withFiles.Load()
			rows := iptRow(48, 101, "Night.Harbour.2021.1080p.BluRay.x264-GRP", "Drama, Thriller", "3.2 hours", "olena", "9.37 GB", 410, 42, 3, true, f) +
				iptRow(5, 102, "Quiet.Street.S02E03.1080p.WEB.H264-GRP", "Drama", "2.0 days", "ivan", "2.1 GB", 55, 17, 1, false, f) +
				iptRow(65, 103, "Quiet.Street.S01.720p.WEB-DL.x264-GRP [REQUEST]", "", "1.1 weeks", "ivan", "14.20 GB", 9, 5, 0, false, f)
			thead := `<thead><tr><th>&nbsp;</th><th>Name</th><th>&nbsp;</th><th>&nbsp;</th><th>&nbsp;</th><th><a title="Sort by size">Size</a></th><th>Snatches</th><th>Seeders</th><th>Leechers</th></tr></thead>`
			page(w, r, `<table id="torrents">`+thead+`<tbody>`+rows+`</tbody></table>`)
		case strings.HasPrefix(r.URL.Path, "/download.php/"):
			if !signedIn(r) {
				page(w, r, "")
				return
			}
			s.downloads.Add(1)
			w.Header().Set("Content-Type", "application/x-bittorrent")
			io.WriteString(w, torrentBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func iptInstance(m *CardigannManager, site *iptSite, settings map[string]string) Instance {
	return Instance{ID: 21, Name: "IPTorrents", Kind: KindCardigann, DefinitionID: "iptorrents", BaseURL: site.URL + "/", Cardigann: m, Settings: settings}
}

func TestIPTorrentsIsBuiltIn(t *testing.T) {
	data, ok := builtinDefinitions()["iptorrents"]
	if !ok {
		t.Fatal("iptorrents is not shipped")
	}
	def, err := ParseDefinition(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := def.Validate(); err != nil {
		t.Fatal(err)
	}
	sum := summarize(def)
	if !sum.Supported {
		t.Fatalf("not supported: %s", sum.Problem)
	}
	if sum.Type != "private" || sum.Protocol != "torrent" {
		t.Errorf("type %q protocol %q", sum.Type, sum.Protocol)
	}
	if len(sum.Links) < 2 || sum.Links[0] != "https://iptorrents.com/" {
		t.Errorf("links = %v, want the main site first and the mirrors after", sum.Links)
	}
	if !strings.Contains(strings.ToLower(sum.Description), "not tested") {
		t.Errorf("the description should say the site is untested: %q", sum.Description)
	}
	byName := map[string]SettingSummary{}
	for _, s := range sum.Settings {
		byName[s.Name] = s
	}
	for _, name := range []string{"cookie", "useragent", "freeleech", "sort"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("setting %q is missing", name)
		}
	}
	if !SecretSetting(byName["cookie"].Type, "cookie") {
		t.Error("the cookie must be stored as a secret")
	}
	if SecretSetting(byName["useragent"].Type, "useragent") {
		t.Error("the user agent is not a secret")
	}
	if got := len(byName["sort"].Options); got < 3 {
		t.Errorf("sort options = %d", got)
	}
}

func TestIPTorrentsSearchBothLayouts(t *testing.T) {
	for _, withFiles := range []bool{false, true} {
		t.Run(fmt.Sprintf("files column %v", withFiles), func(t *testing.T) {
			site := newIPTSite(t)
			site.withFiles.Store(withFiles)
			m := fixtureManager(t, "fixturepublic")
			inst := iptInstance(m, site, map[string]string{"cookie": iptCookie, "useragent": iptUserAgent})

			results, err := m.Search(context.Background(), inst, "Quiet Street S02E03", []int{5000, 5040})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			qs, _ := url.ParseQuery(site.lastQS.Load().(string))
			if qs.Get("q") != "+(Quiet Street S02E03)" {
				t.Errorf("q = %q", qs.Get("q"))
			}
			for _, id := range []string{"73", "5", "22"} {
				if _, ok := qs[id]; !ok {
					t.Errorf("category %s not asked for: %v", id, qs)
				}
			}
			if _, ok := qs["72"]; ok {
				t.Errorf("a movie category was asked for in a TV search: %v", qs)
			}
			if qs.Has("free") || qs.Has("o") {
				t.Errorf("freeleech or sort sent without being asked: %v", qs)
			}
			if ua, _ := site.lastUA.Load().(string); ua != iptUserAgent {
				t.Errorf("user agent = %q, want the one from the settings", ua)
			}
			if len(results) != 2 {
				t.Fatalf("got %d TV results: %+v", len(results), results)
			}

			movies, err := m.Search(context.Background(), inst, "Night Harbour 2021", []int{2000})
			if err != nil {
				t.Fatalf("movie search: %v", err)
			}
			if len(movies) != 1 {
				t.Fatalf("got %d movie results: %+v", len(movies), movies)
			}
			qs, _ = url.ParseQuery(site.lastQS.Load().(string))
			if qs.Get("q") != "+(Night Harbour 2021)" {
				t.Errorf("movie q = %q", qs.Get("q"))
			}
			if _, ok := qs["72"]; !ok {
				t.Errorf("movie categories not asked for: %v", qs)
			}

			all := append(movies, results...)
			want := []struct {
				title    string
				size     int64
				seeders  int
				ago      time.Duration
				category int
				id       int
			}{
				{"Night.Harbour.2021.1080p.BluRay.x264-GRP", 9_370_000_000, 42, 3*time.Hour + 12*time.Minute, 2040, 101},
				{"Quiet.Street.S02E03.1080p.WEB.H264-GRP", 2_100_000_000, 17, 48 * time.Hour, 5040, 102},
				{"Quiet.Street.S01.720p.WEB-DL.x264-GRP", 14_200_000_000, 5, 0, 5000, 103}, // [REQUEST] tag removed
			}
			for i, w := range want {
				r := all[i]
				if r.Title != w.title {
					t.Errorf("row %d: title %q, want %q", i, r.Title, w.title)
				}
				if r.Seeders != w.seeders {
					t.Errorf("row %d: seeders %d, want %d", i, r.Seeders, w.seeders)
				}
				// sizes are 1 GB = 10^9 or 2^30: accept either reading
				if r.SizeBytes < w.size*9/10 || r.SizeBytes > w.size*12/10 {
					t.Errorf("row %d: size %d, want about %d", i, r.SizeBytes, w.size)
				}
				if len(r.Categories) != 1 || r.Categories[0] != w.category {
					t.Errorf("row %d: categories %v, want [%d]", i, r.Categories, w.category)
				}
				if r.PublishDate.IsZero() {
					t.Errorf("row %d: no date", i)
				} else if w.ago > 0 {
					if d := time.Since(r.PublishDate) - w.ago; d < -2*time.Minute || d > 2*time.Minute {
						t.Errorf("row %d: published %v ago, want about %v", i, time.Since(r.PublishDate), w.ago)
					}
				}
				if want := fmt.Sprintf("%s/details.php?id=%d", site.URL, w.id); r.InfoURL != want {
					t.Errorf("row %d: info url %q, want %q", i, r.InfoURL, want)
				}
				_, link, ok := DecodeLinkRef(r.DownloadURL)
				if !ok || !strings.HasPrefix(link, fmt.Sprintf("%s/download.php/%d/", site.URL, w.id)) || !strings.HasSuffix(link, ".torrent") {
					t.Errorf("row %d: download %q", i, link)
				}
			}
		})
	}
}

func TestIPTorrentsQueryOptions(t *testing.T) {
	site := newIPTSite(t)
	m := fixtureManager(t, "fixturepublic")
	inst := iptInstance(m, site, map[string]string{"cookie": iptCookie, "freeleech": "true", "sort": "seeders"})

	// a whole season gets a wildcard so S02E01, S02E02... are found too, and
	// characters the site's search treats as commands are dropped
	if _, err := m.Search(context.Background(), inst, "Marvel's Agents-of S.H.I.E.L.D. (S02)", []int{5000}); err != nil {
		t.Fatal(err)
	}
	qs, _ := url.ParseQuery(site.lastQS.Load().(string))
	if got := qs.Get("q"); got != "+(Marvel s Agents of S H I E L D S02*)" {
		t.Errorf("q = %q", got)
	}
	if qs.Get("free") != "on" || qs.Get("o") != "seeders" {
		t.Errorf("free/o = %q/%q", qs.Get("free"), qs.Get("o"))
	}

	// with no words (the Test button, the newest list) no q is sent
	if _, err := m.Search(context.Background(), inst, "", []int{2000}); err != nil {
		t.Fatal(err)
	}
	qs, _ = url.ParseQuery(site.lastQS.Load().(string))
	if qs.Has("q") {
		t.Errorf("q sent for an empty search: %v", qs)
	}

	// sort "time" is the site's own default and is not sent
	inst2 := iptInstance(m, site, map[string]string{"cookie": iptCookie, "sort": "time"})
	inst2.ID = 22
	if _, err := m.Search(context.Background(), inst2, "Night Harbour", []int{2000}); err != nil {
		t.Fatal(err)
	}
	qs, _ = url.ParseQuery(site.lastQS.Load().(string))
	if qs.Has("o") {
		t.Errorf("o sent for the default sort: %v", qs)
	}
}

func TestIPTorrentsDefaultUserAgent(t *testing.T) {
	site := newIPTSite(t)
	m := fixtureManager(t, "fixturepublic")
	inst := iptInstance(m, site, map[string]string{"cookie": iptCookie})
	if _, err := m.Search(context.Background(), inst, "Night Harbour", []int{2000}); err != nil {
		t.Fatal(err)
	}
	ua, _ := site.lastUA.Load().(string)
	if !strings.HasPrefix(ua, "Mozilla/5.0") {
		t.Errorf("user agent = %q, want a browser-like default when none is set", ua)
	}
}

func TestIPTorrentsDownloadAndTest(t *testing.T) {
	site := newIPTSite(t)
	m := fixtureManager(t, "fixturepublic")
	inst := iptInstance(m, site, map[string]string{"cookie": iptCookie, "useragent": iptUserAgent})

	results, err := m.Search(context.Background(), inst, "Night Harbour", []int{2000})
	if err != nil || len(results) == 0 {
		t.Fatalf("search: %v %+v", err, results)
	}
	_, link, ok := DecodeLinkRef(results[0].DownloadURL)
	if !ok {
		t.Fatalf("download ref = %q", results[0].DownloadURL)
	}
	dl, err := m.Download(context.Background(), inst, link)
	if err != nil || string(dl.Data) != torrentBytes {
		t.Fatalf("download: %v %+v", err, dl)
	}
	if ua, _ := site.lastUA.Load().(string); ua != iptUserAgent {
		t.Errorf("download sent user agent %q", ua)
	}

	res, err := m.Test(context.Background(), inst)
	if err != nil || res.Releases != 3 {
		t.Fatalf("test: n=%d err=%v", res.Releases, err)
	}
}

func TestIPTorrentsCookieProblems(t *testing.T) {
	site := newIPTSite(t)
	m := fixtureManager(t, "fixturepublic")

	// no cookie typed in
	inst := iptInstance(m, site, map[string]string{})
	_, err := m.Search(context.Background(), inst, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "cookie") {
		t.Fatalf("empty cookie: err = %v", err)
	}

	// a cookie the site does not accept (expired, or copied wrongly)
	bad := iptInstance(m, site, map[string]string{"cookie": "uid=1; pass=nope"})
	bad.ID = 23
	_, err = m.Search(context.Background(), bad, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "cookie was not accepted") {
		t.Fatalf("wrong cookie: err = %v", err)
	}

	// the site signs the member out in the middle of a session
	good := iptInstance(m, site, map[string]string{"cookie": iptCookie})
	good.ID = 24
	if _, err := m.Search(context.Background(), good, "Night Harbour", []int{2000}); err != nil {
		t.Fatal(err)
	}
	site.signedOutAt.Store(true)
	_, err = m.Search(context.Background(), good, "Night Harbour", []int{2000})
	if err == nil || !strings.Contains(err.Error(), "cookie was not accepted") {
		t.Fatalf("expired cookie: err = %v", err)
	}
}

// On the live site the sign-out link is an icon with a "Log out" tooltip, so
// the sign-in check goes by the words as well as the address.
func TestIPTorrentsSignInCheckAcceptsAnIconLogoutLink(t *testing.T) {
	site := newIPTSite(t)
	site.iconLogout.Store(true)
	m := fixtureManager(t, "fixturepublic")
	inst := iptInstance(m, site, map[string]string{"cookie": iptCookie})
	inst.ID = 31
	if _, err := m.Search(context.Background(), inst, "Night Harbour", []int{2000}); err != nil {
		t.Fatalf("a signed-in page with an icon sign-out link should be accepted: %v", err)
	}
}

// A rejected cookie says what the site answered with.
func TestIPTorrentsRejectedCookieSaysWhatTheSiteShowed(t *testing.T) {
	site := newIPTSite(t)
	m := fixtureManager(t, "fixturepublic")
	bad := iptInstance(m, site, map[string]string{"cookie": "uid=1; pass=nope"})
	bad.ID = 32
	_, err := m.Search(context.Background(), bad, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "cookie was not accepted") || !strings.Contains(err.Error(), "The site answered at /") {
		t.Fatalf("the message should say what the site answered with, got %v", err)
	}
	if strings.Contains(err.Error(), "pass=nope") {
		t.Fatalf("the message must never contain the cookie: %v", err)
	}
}
