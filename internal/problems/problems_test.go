package problems_test

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/store"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newLogDB(t *testing.T) (*problems.Log, *clock, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	c := &clock{t: time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)}
	l := problems.Open(db)
	l.SetClock(c.now)
	return l, c, db
}

func newLog(t *testing.T) (*problems.Log, *clock) {
	t.Helper()
	l, c, _ := newLogDB(t)
	return l, c
}

func list(t *testing.T, l *problems.Log, f problems.Filter) []problems.Entry {
	t.Helper()
	l.Flush()
	got, _, err := l.List(f)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return got
}

func TestEveryCodeHasPlainHelp(t *testing.T) {
	seen := map[string]bool{}
	for _, h := range problems.Catalog() {
		if seen[h.Code] {
			t.Errorf("code %s is listed twice", h.Code)
		}
		seen[h.Code] = true
		if !strings.Contains(h.Code, ".") || strings.ToLower(h.Code) != h.Code || strings.ContainsAny(h.Code, " -") {
			t.Errorf("%s: a code is lower case words joined by _ with a dot after the topic", h.Code)
		}
		for name, text := range map[string]string{"title": h.Title, "explain": h.Explain, "try": h.Try} {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s: %s is empty", h.Code, name)
			}
			if strings.Contains(text, "  ") || strings.HasSuffix(text, " ") {
				t.Errorf("%s: stray spaces in %s", h.Code, name)
			}
		}
		// Short and plain, the way a person would say it.
		if len(h.Title) > 50 || len(h.Explain) > 170 || len(h.Try) > 290 {
			t.Errorf("%s: too long (title %d, explain %d, try %d)", h.Code, len(h.Title), len(h.Explain), len(h.Try))
		}
		for _, text := range []string{h.Title, h.Explain, h.Try} {
			lower := strings.ToLower(text)
			for _, word := range []string{"please", "unable to", "utilize", "kindly", "invalid ", "failed to ", "an error occurred", "—", "–", "endpoint", "exception", "parameter"} {
				if strings.Contains(lower, word) {
					t.Errorf("%s: %q reads like a machine wrote it (%q)", h.Code, text, word)
				}
			}
		}
		if !strings.HasSuffix(h.Explain, ".") || !strings.HasSuffix(h.Try, ".") {
			t.Errorf("%s: explain and try are sentences and end with a full stop", h.Code)
		}
		if strings.HasSuffix(h.Title, ".") {
			t.Errorf("%s: a title has no full stop", h.Code)
		}
		if h.Level != problems.LevelError && h.Level != problems.LevelWarning {
			t.Errorf("%s: level %q", h.Code, h.Level)
		}
		if problems.AreaLabel(h.Area) == string(h.Area) {
			t.Errorf("%s: area %q has no label", h.Code, h.Area)
		}
		if (h.LinkPath == "") != (h.LinkLabel == "") {
			t.Errorf("%s: a link needs both a label and a path", h.Code)
		}
		if h.LinkPath != "" && !strings.HasPrefix(h.LinkPath, "/") {
			t.Errorf("%s: link %q must be a path inside the app", h.Code, h.LinkPath)
		}
		got, ok := problems.Lookup(h.Code)
		if !ok || got.Title != h.Title {
			t.Errorf("%s: lookup returned %+v", h.Code, got)
		}
	}
	for _, code := range []string{
		"usenet.too_many_connections", "usenet.auth_refused", "indexer.rate_limited", "indexer.unreachable", "indexer.fetch_failed", "disk.full",
		"folder.permission_denied", "unpack.failed", "par2.failed", "mediaserver.unreachable", "tmdb.rate_limited",
		"database.slow", "app.restarted_itself",
	} {
		if !seen[code] {
			t.Errorf("the code %s is missing from the help table", code)
		}
	}
	if _, ok := problems.Lookup("no.such_code"); ok {
		t.Error("an unknown code must not be found")
	}
}

func TestCodeFor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nothing", nil, "download.failed"},
		{"permission", fmt.Errorf("couldn't move the file into your library: %w", fs.ErrPermission), problems.CodeFolderPermission},
		{"permission text", errors.New("mkdir /movies/x: permission denied"), problems.CodeFolderPermission},
		{"windows permission", errors.New("open D:\\x: Access is denied."), problems.CodeFolderPermission},
		{"disk", errors.New("couldn't unpack the download: not enough free disk space to unpack the archive"), problems.CodeDiskFull},
		{"disk text", errors.New("write /d/x: no space left on device"), problems.CodeDiskFull},
		{"too many", errors.New("Your Usenet provider says there are too many connections on this login."), problems.CodeUsenetTooMany},
		{"auth", errors.New("the download failed: news.example.com: The provider refused this username and password."), problems.CodeUsenetAuthRefused},
		{"unreachable", errors.New("the download failed: dial tcp: lookup news.example.com: no such host"), problems.CodeUsenetUnreachable},
		{"missing parts", errors.New("the download failed: 12 articles couldn't be found on your Usenet servers and the release has no PAR2 files to repair it"), problems.CodeUsenetMissingParts},
		{"par2", errors.New("repairing the download failed: par2 repair x: exit status 2"), problems.CodePar2Failed},
		{"password", errors.New("couldn't unpack the download: archive is password protected"), problems.CodeUnpackPassword},
		{"7z", errors.New("couldn't unpack the download: the 7z tool is not installed, so .7z archives can't be unpacked"), problems.CodeUnpackToolMissing},
		{"unpack", errors.New("couldn't unpack the download: bad header"), problems.CodeUnpackFailed},
		{"nzb", errors.New("the download failed: couldn't get the NZB file: status 500"), problems.CodeIndexerFetchFailed},
		{"nzb fetch, no host", errors.New(`the download failed: couldn't get the NZB file: Get "https://api.example.com/getnzb/x.nzb": dial tcp: lookup api.example.com on 10.0.0.1:53: no such host`), problems.CodeIndexerFetchFailed},
		{"nzb fetch, refused", errors.New("the download failed: couldn't get the NZB file: dial tcp 1.2.3.4:443: connect: connection refused"), problems.CodeIndexerFetchFailed},
		{"nzb fetch, timeout", errors.New("the download failed: couldn't get the NZB file: i/o timeout"), problems.CodeIndexerFetchFailed},
		{"torrent fetch", errors.New("the download failed: couldn't get the torrent: dial tcp: lookup t.example.com: no such host"), problems.CodeIndexerFetchFailed},
		{"provider down is still usenet", errors.New("the download failed: news.example.com: dial tcp 1.2.3.4:563: connect: connection refused"), problems.CodeUsenetUnreachable},
		{"no video", errors.New("couldn't find the movie file in the finished download: no video"), problems.CodeImportNoVideo},
		{"exists", errors.New("a file already exists at /m/x.mkv. Remove it"), problems.CodeImportFileExists},
		{"move", errors.New("couldn't move the file into your library: invalid cross-device link"), problems.CodeImportMoveFailed},
		{"missing folder", fmt.Errorf("scan: %w", fs.ErrNotExist), problems.CodeFolderMissing},
		{"anything else", errors.New("something odd"), "download.failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := problems.CodeFor(tt.err, "download.failed"); got != tt.want {
				t.Errorf("CodeFor(%v) = %s, want %s", tt.err, got, tt.want)
			}
		})
	}
}

func TestIndexerMediaServerAndUsenetCodes(t *testing.T) {
	idx := map[string]string{
		"newznab error 429: too many requests":              problems.CodeIndexerRateLimited,
		"API limit reached for today":                       problems.CodeIndexerRateLimited,
		"newznab error 100: Incorrect user credentials":     problems.CodeIndexerAuthRefused,
		"HTTP 403 from the site":                            problems.CodeIndexerAuthRefused,
		"Get https://x: context deadline exceeded":          problems.CodeIndexerUnreachable,
		"dial tcp 1.2.3.4:443: connect: connection refused": problems.CodeIndexerUnreachable,
		"the site returned no results table":                problems.CodeIndexerFailed,
	}
	for msg, want := range idx {
		if got := problems.IndexerCode(errors.New(msg)); got != want {
			t.Errorf("IndexerCode(%q) = %s, want %s", msg, got, want)
		}
	}
	ms := map[string]string{
		"Could not reach Jellyfin at http://x:1. Check the address and port.": problems.CodeMediaServerDown,
		"plex returned 401 Unauthorized":                                      problems.CodeMediaServerAuth,
		"Get http://plex:32400: connection refused":                           problems.CodeMediaServerDown,
		"the server said no":                                                  problems.CodeMediaServerRefresh,
	}
	for msg, want := range ms {
		if got := problems.MediaServerCode(errors.New(msg)); got != want {
			t.Errorf("MediaServerCode(%q) = %s, want %s", msg, got, want)
		}
	}
	nn := map[string]string{
		"nntp 502 Too many connections":  problems.CodeUsenetTooMany,
		"nntp 481 authentication failed": problems.CodeUsenetAuthRefused,
		"nntp 502 access denied":         problems.CodeUsenetAuthRefused,
		"dial tcp: no such host":         problems.CodeUsenetUnreachable,
		"400 quota exceeded":             problems.CodeUsenetQuota,
	}
	for msg, want := range nn {
		if got := problems.UsenetCode(errors.New(msg)); got != want {
			t.Errorf("UsenetCode(%q) = %s, want %s", msg, got, want)
		}
	}
}

func TestRecordKeepsWhatHappened(t *testing.T) {
	l, _ := newLog(t)
	l.Record(problems.Problem{
		Code: problems.CodeUnpackFailed, Err: errors.New("bad header"), Title: "The Matrix", Link: "/title/603", DownloadID: 42,
	})
	got := list(t, l, problems.Filter{})
	if len(got) != 1 {
		t.Fatalf("want 1 problem, got %d", len(got))
	}
	e := got[0]
	if e.Code != problems.CodeUnpackFailed || e.Level != problems.LevelError || e.Area != problems.AreaDownloads {
		t.Errorf("wrong classification: %+v", e)
	}
	if e.Message != "Could not unpack a download" {
		t.Errorf("the message defaults to the title of the code, got %q", e.Message)
	}
	if e.Detail != "bad header" || e.Title != "The Matrix" || e.Link != "/title/603" || e.DownloadID != 42 || e.Count != 1 || e.Read {
		t.Errorf("wrong details: %+v", e)
	}
}

func TestAnUnknownCodeIsStillKept(t *testing.T) {
	l, _ := newLog(t)
	l.Record(problems.Problem{Code: "brand.new", Message: "Something new broke"})
	l.Record(problems.Problem{Code: " "}) // no code: nothing to keep
	got := list(t, l, problems.Filter{})
	if len(got) != 1 || got[0].Message != "Something new broke" || got[0].Area != problems.AreaSystem || got[0].Level != problems.LevelError {
		t.Fatalf("got %+v", got)
	}
}

func TestRepeatsFoldIntoOneRow(t *testing.T) {
	l, c := newLog(t)
	for i := 0; i < 14; i++ {
		l.Record(problems.Problem{Code: problems.CodeUsenetTooMany, Subject: "news.example.com", Message: fmt.Sprintf("try %d", i)})
		l.Flush()
		c.advance(30 * time.Second)
	}
	got := list(t, l, problems.Filter{})
	if len(got) != 1 {
		t.Fatalf("want one row, got %d", len(got))
	}
	if got[0].Count != 14 {
		t.Errorf("count = %d, want 14", got[0].Count)
	}
	if got[0].Message != "try 13" {
		t.Errorf("the row shows the latest message, got %q", got[0].Message)
	}
	if !got[0].LastAt.After(got[0].FirstAt) {
		t.Errorf("last (%v) should be after first (%v)", got[0].LastAt, got[0].FirstAt)
	}
}

func TestRepeatsFoldOnlyForTheSameSubjectAndWithinTheWindow(t *testing.T) {
	l, c := newLog(t)
	l.Record(problems.Problem{Code: problems.CodeIndexerRateLimited, Subject: "NZBgeek"})
	l.Record(problems.Problem{Code: problems.CodeIndexerRateLimited, Subject: "Other site"})
	l.Record(problems.Problem{Code: problems.CodeIndexerRateLimited, Subject: "NZBgeek"})
	if got := list(t, l, problems.Filter{}); len(got) != 2 {
		t.Fatalf("two sources are two rows, got %d", len(got))
	}
	c.advance(problems.Window + time.Minute)
	l.Record(problems.Problem{Code: problems.CodeIndexerRateLimited, Subject: "NZBgeek"})
	got := list(t, l, problems.Filter{})
	if len(got) != 3 {
		t.Fatalf("after the window a new row is started, got %d rows", len(got))
	}
	if got[0].Count != 1 || got[1].Count != 1 && got[2].Count != 1 {
		t.Errorf("counts: %+v", got)
	}
}

func TestARepeatBecomesUnreadAndKeepsTheHigherLevel(t *testing.T) {
	l, c := newLog(t)
	l.Record(problems.Problem{Code: problems.CodeUsenetTooMany, Level: problems.LevelError})
	got := list(t, l, problems.Filter{})
	if _, err := l.MarkRead([]int64{got[0].ID}); err != nil {
		t.Fatal(err)
	}
	c.advance(time.Minute)
	l.Record(problems.Problem{Code: problems.CodeUsenetTooMany, Level: problems.LevelWarning})
	got = list(t, l, problems.Filter{})
	if len(got) != 1 || got[0].Read || got[0].Level != problems.LevelError || got[0].Count != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestSecretsNeverReachTheDatabase(t *testing.T) {
	l, _, db := newLogDB(t)
	l.Record(problems.Problem{
		Code:    problems.CodeIndexerFailed,
		Message: "Could not search https://nzb.example.com/api?t=search&apikey=SECRETKEY123456",
		Err:     errors.New(`Get "https://user:hunter2@nzb.example.com/api?apikey=abcdef123456&q=x": password=hunter2 refused`),
		Detail:  "Authorization: Bearer abcdefghijklmnop\nX-Api-Key: 5f4dcc3b5aa765d61d8327deb882cf99",
	})
	got := list(t, l, problems.Filter{})
	if len(got) != 1 {
		t.Fatalf("got %d rows", len(got))
	}
	stored := got[0].Message + "\n" + got[0].Detail
	for _, secret := range []string{"SECRETKEY123456", "hunter2", "abcdef123456", "abcdefghijklmnop", "5f4dcc3b5aa765d61d8327deb882cf99"} {
		if strings.Contains(stored, secret) {
			t.Errorf("the secret %q was stored: %s", secret, stored)
		}
	}
	// Nothing is left in any column of the table either.
	var all string
	if err := db.QueryRow(`SELECT group_concat(message || detail || title || link || subject, ' ') FROM problems`).Scan(&all); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(all, "hunter2") || strings.Contains(all, "SECRETKEY") {
		t.Errorf("a secret is in the table: %s", all)
	}
}

func TestLongTextIsCutAtACharacter(t *testing.T) {
	l, _ := newLog(t)
	l.Record(problems.Problem{Code: problems.CodeDownloadFailed, Detail: strings.Repeat("é", 3000), Title: strings.Repeat("ü", 500)})
	got := list(t, l, problems.Filter{})
	if len(got[0].Detail) > 2010 || !strings.HasSuffix(got[0].Detail, "...") {
		t.Errorf("detail is %d bytes", len(got[0].Detail))
	}
	if strings.ContainsRune(got[0].Detail, '\uFFFD') || strings.ContainsRune(got[0].Title, '\uFFFD') {
		t.Error("a character was cut in half")
	}
}

func TestFilters(t *testing.T) {
	l, c := newLog(t)
	base := c.now()
	rec := func(p problems.Problem, ago time.Duration) {
		c.t = base.Add(-ago)
		l.Record(p)
		l.Flush()
	}
	rec(problems.Problem{Code: problems.CodeDiskFull, Title: "Alien"}, 40*24*time.Hour)
	rec(problems.Problem{Code: problems.CodeUsenetTooMany, Subject: "a"}, 3*24*time.Hour)
	rec(problems.Problem{Code: problems.CodeIndexerRateLimited, Subject: "NZBgeek", Err: errors.New("429 too many requests")}, 2*time.Hour)
	rec(problems.Problem{Code: problems.CodeUnpackFailed, Title: "The Matrix", DownloadID: 7}, time.Hour)
	c.t = base

	all := list(t, l, problems.Filter{})
	if len(all) != 4 || all[0].Code != problems.CodeUnpackFailed {
		t.Fatalf("newest first, got %+v", all)
	}
	first := list(t, l, problems.Filter{UnreadOnly: true})
	if _, err := l.MarkRead([]int64{first[0].ID}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		f    problems.Filter
		want []string
	}{
		{"errors", problems.Filter{Level: problems.LevelError}, []string{problems.CodeUnpackFailed, problems.CodeDiskFull}},
		{"warnings", problems.Filter{Level: problems.LevelWarning}, []string{problems.CodeIndexerRateLimited, problems.CodeUsenetTooMany}},
		{"area", problems.Filter{Area: problems.AreaSearch}, []string{problems.CodeIndexerRateLimited}},
		{"code", problems.Filter{Code: problems.CodeDiskFull}, []string{problems.CodeDiskFull}},
		{"since", problems.Filter{Since: base.Add(-4 * 24 * time.Hour)}, []string{problems.CodeUnpackFailed, problems.CodeIndexerRateLimited, problems.CodeUsenetTooMany}},
		{"until", problems.Filter{Until: base.Add(-4 * 24 * time.Hour)}, []string{problems.CodeDiskFull}},
		{"since and until", problems.Filter{Since: base.Add(-4 * 24 * time.Hour), Until: base.Add(-90 * time.Minute)}, []string{problems.CodeIndexerRateLimited, problems.CodeUsenetTooMany}},
		{"text in the title", problems.Filter{Text: "matrix"}, []string{problems.CodeUnpackFailed}},
		{"text in the detail", problems.Filter{Text: "429"}, []string{problems.CodeIndexerRateLimited}},
		{"text in the code", problems.Filter{Text: "too_many"}, []string{problems.CodeUsenetTooMany}},
		{"every word must match", problems.Filter{Text: "matrix nzbgeek"}, nil},
		{"a percent sign is not a wildcard", problems.Filter{Text: "%"}, nil},
		{"unread only", problems.Filter{UnreadOnly: true}, []string{problems.CodeIndexerRateLimited, problems.CodeUsenetTooMany, problems.CodeDiskFull}},
		{"level and area", problems.Filter{Level: problems.LevelError, Area: problems.AreaDisk}, []string{problems.CodeDiskFull}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := l.List(tt.f)
			if err != nil {
				t.Fatal(err)
			}
			var codes []string
			for _, e := range got {
				codes = append(codes, e.Code)
			}
			if strings.Join(codes, ",") != strings.Join(tt.want, ",") || total != len(tt.want) {
				t.Errorf("got %v (total %d), want %v", codes, total, tt.want)
			}
		})
	}

	t.Run("paging", func(t *testing.T) {
		page, total, err := l.List(problems.Filter{Limit: 2, Offset: 1})
		if err != nil || total != 4 || len(page) != 2 || page[0].Code != problems.CodeIndexerRateLimited {
			t.Fatalf("page = %+v total=%d err=%v", page, total, err)
		}
	})
}

func TestCounts(t *testing.T) {
	l, c := newLog(t)
	base := c.now() // 14:00 on the 30th
	rec := func(code string, ago time.Duration) {
		c.t = base.Add(-ago)
		l.Record(problems.Problem{Code: code, Subject: fmt.Sprint(ago)})
		l.Flush()
	}
	rec(problems.CodeDiskFull, time.Hour)           // error, today
	rec(problems.CodeUnpackFailed, 2*time.Hour)     // error, today
	rec(problems.CodeUsenetTooMany, 3*time.Hour)    // warning, today
	rec(problems.CodeIndexerFailed, 20*time.Hour)   // warning, yesterday
	rec(problems.CodeDiskFull, 5*24*time.Hour)      // error, this week
	rec(problems.CodePar2Failed, 26*time.Hour)      // error, yesterday, over 24 hours ago
	rec(problems.CodeDatabaseSlow, 10*24*time.Hour) // warning, older than a week
	got, err := l.Counts(base)
	if err != nil {
		t.Fatal(err)
	}
	want := problems.Counts{ErrorsToday: 2, WarningsToday: 1, ErrorsWeek: 4, WarningsWeek: 2, Unread: 7, UnreadErrors24: 2}
	if got != want {
		t.Errorf("counts = %+v, want %+v", got, want)
	}
	if n, err := l.MarkAllRead(problems.Filter{Level: problems.LevelWarning}); err != nil || n != 3 {
		t.Fatalf("mark warnings read: n=%d err=%v", n, err)
	}
	if got, _ := l.Counts(base); got.Unread != 4 {
		t.Errorf("unread after marking warnings = %d, want 4", got.Unread)
	}
	if n, err := l.MarkAllRead(problems.Filter{}); err != nil || n != 4 {
		t.Fatalf("mark all read: n=%d err=%v", n, err)
	}
	if got, _ := l.Counts(base); got.Unread != 0 || got.UnreadErrors24 != 0 {
		t.Errorf("everything is read, got %+v", got)
	}
}

func TestMarkReadOfNothingChangesNothing(t *testing.T) {
	l, _ := newLog(t)
	l.Record(problems.Problem{Code: problems.CodeDiskFull})
	l.Flush()
	if n, err := l.MarkRead(nil); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, err := l.MarkRead([]int64{9999}); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestPruneKeepsThirtyDaysAtMost(t *testing.T) {
	l, c := newLog(t)
	now := c.now()
	rec := func(subject string, ago time.Duration) {
		c.t = now.Add(-ago)
		l.Record(problems.Problem{Code: problems.CodeDiskFull, Subject: subject})
		l.Flush()
	}
	rec("recent", time.Hour)
	rec("ten days", 10*24*time.Hour)
	rec("twenty-nine days", 29*24*time.Hour)
	rec("thirty-one days", 31*24*time.Hour)
	rec("ninety days", 90*24*time.Hour)

	n, err := l.Prune(now, 0)
	if err != nil || n != 2 {
		t.Fatalf("default retention removed %d (err %v), want 2", n, err)
	}
	// A shorter history setting wins.
	n, err = l.Prune(now, 7)
	if err != nil || n != 2 {
		t.Fatalf("7 days removed %d (err %v), want 2", n, err)
	}
	// A longer one never keeps more than 30 days.
	if n, err = l.Prune(now, 365); err != nil || n != 0 {
		t.Fatalf("365 days removed %d (err %v), want 0", n, err)
	}
	if got := list(t, l, problems.Filter{}); len(got) != 1 || got[0].Subject != "recent" {
		t.Fatalf("left: %+v", got)
	}
}

func TestTheLogIsCappedAtFiveThousandRows(t *testing.T) {
	l, c, db := newLogDB(t)
	// Fill the table directly: 5000 rows would take a while to record one by one.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	base := c.now()
	for i := 0; i < problems.MaxRows+50; i++ {
		at := base.Add(-time.Duration(i) * time.Second).Format("2006-01-02T15:04:05Z")
		if _, err := tx.Exec(`INSERT INTO problems (first_at, last_at, level, area, code, subject, message) VALUES (?, ?, 'error', 'disk', 'disk.full', ?, 'x')`, at, at, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	c.advance(time.Hour)
	l.Record(problems.Problem{Code: problems.CodeDiskFull, Subject: "new"})
	l.Flush()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM problems`).Scan(&n); err != nil || n != problems.MaxRows {
		t.Fatalf("rows = %d (err %v), want %d", n, err, problems.MaxRows)
	}
	got := list(t, l, problems.Filter{Limit: 1})
	if got[0].Subject != "new" {
		t.Errorf("the newest row must survive, got %+v", got[0])
	}
}

func TestOnNewHearsOnlyAboutNewErrors(t *testing.T) {
	l, _ := newLog(t)
	var mu sync.Mutex
	var heard []string
	l.OnNew = func(e problems.Entry) {
		mu.Lock()
		heard = append(heard, e.Code)
		mu.Unlock()
	}
	l.Record(problems.Problem{Code: problems.CodeDiskFull})                   // new error: heard
	l.Record(problems.Problem{Code: problems.CodeDiskFull})                   // repeat: not heard
	l.Record(problems.Problem{Code: problems.CodeUsenetTooMany})              // warning: not heard
	l.Record(problems.Problem{Code: problems.CodeUnpackFailed, Quiet: true})  // quiet: not heard
	l.Record(problems.Problem{Code: problems.CodeUnpackFailed, Subject: "b"}) // new error: heard
	l.Flush()
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(heard, ",") != "disk.full,unpack.failed" {
		t.Errorf("heard %v", heard)
	}
}

func TestRecordDoesNothingWithoutADefaultLog(t *testing.T) {
	problems.SetDefault(nil)
	problems.Record(problems.Problem{Code: problems.CodeDiskFull}) // must not panic
	var nilLog *problems.Log
	nilLog.Record(problems.Problem{Code: problems.CodeDiskFull})
}

func TestDefaultLogReceivesRecords(t *testing.T) {
	l, _ := newLog(t)
	problems.SetDefault(l)
	t.Cleanup(func() { problems.SetDefault(nil) })
	problems.Record(problems.Problem{Code: problems.CodeDiskFull})
	if got := list(t, l, problems.Filter{}); len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
}

func TestManyReportsAtOnceAreSafe(t *testing.T) {
	l, _ := newLog(t)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				l.Record(problems.Problem{Code: problems.CodeUsenetTooMany, Subject: "s"})
			}
		}()
	}
	wg.Wait()
	got := list(t, l, problems.Filter{})
	if len(got) != 1 || got[0].Count+l.Dropped() != 320 {
		t.Fatalf("rows=%d count=%d dropped=%d", len(got), got[0].Count, l.Dropped())
	}
}

func TestTextFile(t *testing.T) {
	l, c := newLog(t)
	l.Record(problems.Problem{Code: problems.CodeUnpackFailed, Err: errors.New("bad header\nsecond line"), Title: "The Matrix", DownloadID: 3})
	l.Flush()
	c.advance(time.Minute)
	l.Record(problems.Problem{Code: problems.CodeUnpackFailed, Err: errors.New("bad header\nsecond line"), DownloadID: 3})
	entries := list(t, l, problems.Filter{})
	out := problems.Text(entries, c.now())
	for _, want := range []string{
		"Mediarium problem log", "Problems: 1", "ERROR", "Downloads", "unpack.failed", "Could not unpack a download",
		"Happened 2 times", "For: The Matrix", "Download: 3", "    bad header", "    second line",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the text file lacks %q:\n%s", want, out)
		}
	}
	if empty := problems.Text(nil, c.now()); !strings.Contains(empty, "No problems.") {
		t.Errorf("empty file: %s", empty)
	}
}

func TestAgoAndTimes(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{time.Minute, "1 minute ago"},
		{2 * time.Minute, "2 minutes ago"},
		{59 * time.Minute, "59 minutes ago"},
		{time.Hour, "1 hour ago"},
		{5 * time.Hour, "5 hours ago"},
		{49 * time.Hour, "2 days ago"},
	}
	for _, tt := range tests {
		if got := problems.Ago(now, now.Add(-tt.ago)); got != tt.want {
			t.Errorf("Ago(%v) = %q, want %q", tt.ago, got, tt.want)
		}
	}
	if problems.Times(1) != "once" || problems.Times(14) != "14 times" {
		t.Error("Times")
	}
}
