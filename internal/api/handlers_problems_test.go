package api_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
)

func problemsList(t *testing.T, client *http.Client, base, query string) map[string]any {
	t.Helper()
	return getJSON[map[string]any](t, client, base+"/api/system/problems"+query)
}

func problemCodes(list map[string]any) []string {
	var out []string
	for _, raw := range list["items"].([]any) {
		out = append(out, raw.(map[string]any)["code"].(string))
	}
	return out
}

func TestProblemLogListsWhatHappenedWithPlainHelp(t *testing.T) {
	server, base, client := loginNewServer(t)

	empty := problemsList(t, client, base, "")
	if len(empty["items"].([]any)) != 0 || empty["total"].(float64) != 0 || empty["keepDays"].(float64) != 30 {
		t.Fatalf("a new install has no problems: %+v", empty)
	}
	if len(empty["areas"].([]any)) < 8 {
		t.Fatalf("the page needs the list of areas: %+v", empty["areas"])
	}

	problems.Record(problems.Problem{Code: problems.CodeUsenetTooMany, Subject: "news.example.com", Message: "news.example.com says this login has too many connections."})
	problems.Record(problems.Problem{Code: problems.CodeUsenetTooMany, Subject: "news.example.com"})
	problems.Record(problems.Problem{Code: problems.CodeUnpackFailed, Err: errors.New("bad header password=hunter2"), Title: "The Matrix", Link: "/title/603", DownloadID: 9})
	problems.Record(problems.Problem{Code: "brand.new", Message: "Something nobody wrote help for"})
	server.Problems.Flush()

	list := problemsList(t, client, base, "")
	if list["total"].(float64) != 3 {
		t.Fatalf("want 3 rows: %+v", list)
	}
	byCode := map[string]map[string]any{}
	for _, raw := range list["items"].([]any) {
		it := raw.(map[string]any)
		byCode[it["code"].(string)] = it
	}

	tooMany := byCode["usenet.too_many_connections"]
	if tooMany["count"].(float64) != 2 || tooMany["level"] != "warning" || tooMany["area"] != "usenet" || tooMany["areaLabel"] != "Usenet" {
		t.Errorf("too many connections: %+v", tooMany)
	}
	if tooMany["title"] != "Too many connections to your Usenet provider" ||
		!strings.Contains(tooMany["explain"].(string), "too many connections open") ||
		!strings.Contains(tooMany["try"].(string), "SABnzbd") || tooMany["linkPath"] != "/settings/downloads" || tooMany["known"] != true {
		t.Errorf("the help is missing: %+v", tooMany)
	}

	unpack := byCode["unpack.failed"]
	if unpack["forTitle"] != "The Matrix" || unpack["forLink"] != "/title/603" || unpack["downloadId"].(float64) != 9 || unpack["read"] != false {
		t.Errorf("related title: %+v", unpack)
	}
	if d := unpack["detail"].(string); strings.Contains(d, "hunter2") || !strings.Contains(d, "bad header") {
		t.Errorf("the detail must have the error but not the password: %q", d)
	}

	unknown := byCode["brand.new"]
	if unknown["known"] != false || unknown["title"] != "Something nobody wrote help for" || unknown["explain"] != nil {
		t.Errorf("an unknown code shows its own message: %+v", unknown)
	}
}

func TestProblemLogFilters(t *testing.T) {
	server, base, client := loginNewServer(t)
	problems.Record(problems.Problem{Code: problems.CodeDiskFull, Title: "Alien"})
	problems.Record(problems.Problem{Code: problems.CodeIndexerRateLimited, Subject: "NZBgeek", Message: "NZBgeek says you have made too many requests."})
	problems.Record(problems.Problem{Code: problems.CodeUnpackFailed, Title: "The Matrix"})
	server.Problems.Flush()
	first := problemsList(t, client, base, "?level=error")
	id := first["items"].([]any)[0].(map[string]any)["id"].(float64)
	postJSON[map[string]any](t, client, base+"/api/system/problems/read", map[string]any{"ids": []float64{id}}, http.StatusOK)

	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	today := time.Now().Format("2006-01-02")
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"everything", "", 3},
		{"errors", "?level=error", 2},
		{"warnings", "?level=warning", 1},
		{"all levels", "?level=all", 3},
		{"an area", "?area=search", 1},
		{"every area", "?area=all", 3},
		{"free text", "?q=matrix", 1},
		{"free text is not case sensitive", "?q=NZBGEEK", 1},
		{"unread", "?unread=1", 2},
		{"from today", "?from=" + today, 3},
		{"from tomorrow", "?from=" + tomorrow, 0},
		{"up to today includes today", "?to=" + today, 3},
		{"up to yesterday", "?to=" + time.Now().AddDate(0, 0, -1).Format("2006-01-02"), 0},
		{"paging", "?limit=2&offset=2", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := problemsList(t, client, base, tt.query)
			n := len(got["items"].([]any))
			if tt.name == "paging" {
				if got["total"].(float64) != 3 || n != 1 {
					t.Errorf("paging: %+v", got)
				}
				return
			}
			if n != tt.want || got["total"].(float64) != float64(tt.want) {
				t.Errorf("%s: got %d (total %v), want %d: %v", tt.query, n, got["total"], tt.want, problemCodes(got))
			}
		})
	}

	for _, bad := range []string{"?level=loud", "?from=yesterday-ish"} {
		resp, err := client.Get(base + "/api/system/problems" + bad)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", bad, resp.StatusCode)
		}
	}
}

func TestProblemLogSummaryAndMarkAsRead(t *testing.T) {
	server, base, client := loginNewServer(t)
	problems.Record(problems.Problem{Code: problems.CodeDiskFull})
	problems.Record(problems.Problem{Code: problems.CodeUnpackFailed})
	problems.Record(problems.Problem{Code: problems.CodeUsenetTooMany})
	server.Problems.Flush()

	sum := problemsList(t, client, base, "?summary=1")
	counts := sum["counts"].(map[string]any)
	if len(sum["items"].([]any)) != 0 || counts["errorsToday"].(float64) != 2 || counts["warningsToday"].(float64) != 1 ||
		counts["errorsWeek"].(float64) != 2 || counts["unread"].(float64) != 3 || counts["unreadErrors24h"].(float64) != 2 {
		t.Fatalf("summary: %+v", sum)
	}

	// Nothing to mark is refused, not silently ignored.
	postJSON[map[string]any](t, client, base+"/api/system/problems/read", map[string]any{}, http.StatusBadRequest)

	all := problemsList(t, client, base, "")
	id := all["items"].([]any)[0].(map[string]any)["id"].(float64)
	res := postJSON[map[string]any](t, client, base+"/api/system/problems/read", map[string]any{"ids": []float64{id}}, http.StatusOK)
	if res["marked"].(float64) != 1 || res["counts"].(map[string]any)["unread"].(float64) != 2 {
		t.Fatalf("mark one: %+v", res)
	}
	res = postJSON[map[string]any](t, client, base+"/api/system/problems/read", map[string]any{"all": true}, http.StatusOK)
	if res["marked"].(float64) != 2 || res["counts"].(map[string]any)["unread"].(float64) != 0 || res["counts"].(map[string]any)["unreadErrors24h"].(float64) != 0 {
		t.Fatalf("mark all: %+v", res)
	}
	if got := problemsList(t, client, base, "?unread=1"); got["total"].(float64) != 0 {
		t.Fatalf("nothing is unread now: %+v", got)
	}
}

func TestProblemLogDownloadAsTextFile(t *testing.T) {
	server, base, client := loginNewServer(t)
	problems.Record(problems.Problem{Code: problems.CodeDiskFull, Err: errors.New("write /downloads/x: no space left on device"), Title: "Alien"})
	problems.Record(problems.Problem{Code: problems.CodeIndexerRateLimited, Subject: "NZBgeek"})
	server.Problems.Flush()

	resp, err := client.Get(base + "/api/system/problems/export?level=error")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), "attachment; filename=\"mediarium-problems-") {
		t.Fatalf("headers: %d %v", resp.StatusCode, resp.Header)
	}
	text := string(body)
	for _, want := range []string{"Mediarium problem log", "Problems: 1", "disk.full", "The disk is full", "For: Alien", "no space left on device"} {
		if !strings.Contains(text, want) {
			t.Errorf("the file lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "indexer.rate_limited") {
		t.Errorf("the filter must apply to the file:\n%s", text)
	}
}

func TestOnlyAdministratorsUseTheProblemLog(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/system/problems"},
		{http.MethodGet, "/api/system/problems?summary=1"},
		{http.MethodPost, "/api/system/problems/read"},
		{http.MethodGet, "/api/system/problems/export"},
		{http.MethodPut, "/api/system/problems/notify"},
	} {
		status, body := doStatus(t, member, tc.method, base+tc.path)
		if status != http.StatusForbidden || body["error"] != "Only an administrator can do this." {
			t.Errorf("member %s %s: want 403, got %d %v", tc.method, tc.path, status, body)
		}
	}
	if status, _ := doStatus(t, admin, http.MethodGet, base+"/api/system/problems"); status != http.StatusOK {
		t.Errorf("admin: %d", status)
	}
}

func TestErrorsCanBeSentToNotificationTargets(t *testing.T) {
	server, base, client := loginNewServer(t)
	hook, rec := newHookRecorder(t, 200)
	postJSON[map[string]any](t, client, base+"/api/notifications", map[string]any{
		"name": "Phone", "type": "webhook", "url": hook.URL, "events": []string{"health"},
	}, http.StatusCreated)

	// Off unless switched on.
	if got := problemsList(t, client, base, ""); got["notifyOnProblems"] != false {
		t.Fatalf("notifications about errors start switched off: %+v", got)
	}
	problems.Record(problems.Problem{Code: problems.CodeDiskFull, Subject: "one"})
	server.Problems.Flush()
	time.Sleep(200 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("nothing should be sent while it is off: %v", rec.bodies)
	}

	res := postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/system/problems/notify", map[string]any{"enabled": true}, http.StatusOK)
	if res["notifyOnProblems"] != true || problemsList(t, client, base, "")["notifyOnProblems"] != true {
		t.Fatalf("switching on: %+v", res)
	}
	problems.Record(problems.Problem{Code: problems.CodeDiskFull, Subject: "two"})                   // an error: sent
	problems.Record(problems.Problem{Code: problems.CodeUsenetTooMany, Subject: "three"})            // a warning: not sent
	problems.Record(problems.Problem{Code: problems.CodeUnpackFailed, Subject: "four", Quiet: true}) // told already elsewhere: not sent
	problems.Record(problems.Problem{Code: problems.CodeDiskFull, Subject: "two", Message: "again"}) // a repeat: not sent
	server.Problems.Flush()
	deadline := time.Now().Add(3 * time.Second)
	for rec.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if rec.count() != 1 {
		t.Fatalf("want exactly one notification, got %d: %v", rec.count(), rec.bodies)
	}
	if !strings.Contains(rec.bodies[0], "Mediarium needs attention: the disk is full") || !strings.Contains(rec.bodies[0], "Free up some space") {
		t.Errorf("the notification should say what happened and what to try: %s", rec.bodies[0])
	}
}
