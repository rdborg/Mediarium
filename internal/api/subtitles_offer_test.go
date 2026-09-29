package api_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanborg/mediarium/internal/library"
)

// seedDownloadedMovie adds a downloaded movie whose video file exists on disk
// (with no subtitles next to it).
func seedDownloadedMovie(t *testing.T, env *tvAutoEnv, tmdbID int, title string) library.Movie {
	t.Helper()
	video := filepath.Join(t.TempDir(), title+" (2001).mkv")
	if err := os.WriteFile(video, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: tmdbID, Title: title, Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", video); err != nil {
		t.Fatal(err)
	}
	m.FilePath = video
	return m
}

func items(kind string, ids ...int64) map[string]any {
	var list []map[string]any
	for _, id := range ids {
		list = append(list, map[string]any{"kind": kind, "id": id})
	}
	return map[string]any{"items": list}
}

func healthItemIDs(t *testing.T, env *tvAutoEnv) map[string]map[string]any {
	t.Helper()
	h := getJSON[map[string]any](t, env.client, env.baseURL+"/api/health")
	out := map[string]map[string]any{}
	for _, raw := range h["items"].([]any) {
		it := raw.(map[string]any)
		out[it["id"].(string)] = it
	}
	return out
}

func TestSubtitlesAreOfferedNotFetchedByDefault(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	fake := newFakeOpenSubtitles(t)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)
	seedDownloadedMovie(t, env, 9, "Some Movie")

	// Unset means off: the setting reads as false and the scheduled sweep does nothing.
	if s := getJSON[map[string]any](t, env.client, env.baseURL+"/api/settings"); s["subtitleAutoDownload"] != false {
		t.Fatalf("automatic subtitle download should default to off, got %v", s["subtitleAutoDownload"])
	}
	env.server.TestSubtitleSweepJob(context.Background())
	if n := fake.searches.Load(); n != 0 {
		t.Fatalf("the sweep searched %d times although automatic download is off", n)
	}

	// The health list offers them instead, and only then.
	offer := healthItemIDs(t, env)["subtitles-offer"]
	if offer == nil {
		t.Fatalf("expected a subtitles-offer health item, got %v", healthItemIDs(t, env))
	}
	if offer["level"] != "info" || offer["title"] != "1 downloaded title has no subtitles yet" {
		t.Fatalf("unexpected offer: %+v", offer)
	}
	if !strings.Contains(offer["impact"].(string), "about 5 downloads today") {
		t.Fatalf("the offer should say how many downloads today's limit allows: %v", offer["impact"])
	}
	if action := offer["action"].(map[string]any); action["label"] != "Get subtitles" || action["path"] != "/wanted?tab=subtitles" {
		t.Fatalf("unexpected action: %+v", action)
	}
	if _, ok := healthItemIDs(t, env)["subtitles-anonymous"]; ok {
		t.Fatal("the no-account notice is only for automatic mode")
	}

	// Dismissing the only title takes the offer away.
	m, _ := env.server.MovieRepo.List()
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/dismiss", items("movie", m[0].ID), http.StatusOK)
	if _, ok := healthItemIDs(t, env)["subtitles-offer"]; ok {
		t.Fatal("a dismissed title must not be offered")
	}
	postJSONMethod[map[string]any](t, env.client, http.MethodDelete, env.baseURL+"/api/subtitles/dismiss", items("movie", m[0].ID), http.StatusOK)

	// Automatic mode: the sweep now runs, the offer is replaced by the account notice.
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitleAutoDownload": true}, http.StatusOK)
	health := healthItemIDs(t, env)
	if _, ok := health["subtitles-offer"]; ok {
		t.Fatal("no offer while automatic download is on")
	}
	if _, ok := health["subtitles-anonymous"]; !ok {
		t.Fatalf("expected the no-account notice in automatic mode, got %v", health)
	}
	env.server.TestSubtitleSweepJob(context.Background())
	if fake.searches.Load() == 0 {
		t.Fatal("the sweep should search once automatic download is on")
	}
}

func TestNoOfferWithoutAKey(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	seedDownloadedMovie(t, env, 9, "Some Movie")
	health := healthItemIDs(t, env)
	if _, ok := health["subtitles-offer"]; ok {
		t.Fatal("no offer without an OpenSubtitles key")
	}
	if _, ok := health["subtitles-off"]; !ok {
		t.Fatalf("expected subtitles-off, got %v", health)
	}
}

func TestDismissedTitlesAreLeftOutOfWantedAndTheSweep(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	fake := newFakeOpenSubtitles(t)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)
	a := seedDownloadedMovie(t, env, 1, "Alpha")
	b := seedDownloadedMovie(t, env, 2, "Beta")

	url := env.baseURL + "/api/subtitles/dismiss"
	postJSON[map[string]any](t, env.client, url, map[string]any{}, http.StatusBadRequest)
	postJSON[map[string]any](t, env.client, url, items("tv", 1), http.StatusBadRequest)
	postJSON[map[string]any](t, env.client, url, items("movie", 9999), http.StatusBadRequest)
	if res := postJSON[map[string]int](t, env.client, url, items("movie", b.ID), http.StatusOK); res["dismissed"] != 1 {
		t.Fatalf("dismiss reply: %v", res)
	}
	postJSON[map[string]int](t, env.client, url, items("movie", b.ID), http.StatusOK) // twice is fine

	wanted := getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/subtitles/wanted")
	if len(wanted) != 1 || wanted[0]["title"] != "Alpha" {
		t.Fatalf("only Alpha should be wanted, got %+v", wanted)
	}
	if _, ok := wanted[0]["dismissed"]; ok {
		t.Fatalf("an undismissed row must not carry the flag: %+v", wanted[0])
	}
	all := getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/subtitles/wanted?includeDismissed=1")
	if len(all) != 2 {
		t.Fatalf("includeDismissed should list both, got %+v", all)
	}
	for _, row := range all {
		want := row["title"] == "Beta"
		if got, _ := row["dismissed"].(bool); got != want {
			t.Fatalf("row %v: dismissed = %v, want %v", row["title"], got, want)
		}
	}

	// The manual sweep skips the dismissed title.
	res := postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/sweep", nil, http.StatusOK)
	if res["downloaded"] != float64(1) {
		t.Fatalf("sweep should fetch Alpha only, got %+v", res)
	}
	if _, err := os.Stat(strings.TrimSuffix(a.FilePath, ".mkv") + ".en.srt"); err != nil {
		t.Fatalf("Alpha's subtitle: %v", err)
	}
	if _, err := os.Stat(strings.TrimSuffix(b.FilePath, ".mkv") + ".en.srt"); err == nil {
		t.Fatal("Beta is dismissed and must not get a subtitle")
	}

	// Undo.
	if res := postJSONMethod[map[string]int](t, env.client, http.MethodDelete, url, items("movie", b.ID), http.StatusOK); res["restored"] != 1 {
		t.Fatalf("restore reply: %v", res)
	}
	wanted = getJSON[[]map[string]any](t, env.client, env.baseURL+"/api/subtitles/wanted")
	if len(wanted) != 1 || wanted[0]["title"] != "Beta" {
		t.Fatalf("Beta should be wanted again (Alpha is done), got %+v", wanted)
	}
}

func TestSubtitleQuotaEndpoint(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	quotaURL := env.baseURL + "/api/subtitles/quota"

	// No key: reported as such, and OpenSubtitles is never contacted.
	q := getJSON[map[string]any](t, env.client, quotaURL)
	if q["hasKey"] != false || q["message"] != "" || q["source"] != "estimated" {
		t.Fatalf("no key: %+v", q)
	}

	fake := newFakeOpenSubtitles(t)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)
	for i := 1; i <= 7; i++ {
		seedDownloadedMovie(t, env, i, fmt.Sprintf("Movie%d", i))
	}

	q = getJSON[map[string]any](t, env.client, quotaURL)
	want := map[string]any{
		"hasKey": true, "hasAccount": false, "limit": float64(5), "used": float64(0), "remaining": float64(5),
		"windowHours": float64(24), "resetsAt": nil, "source": "estimated",
		"missingItems": float64(7), "missingFiles": float64(7), "daysToFinish": float64(2), "exceeded": false,
	}
	for k, v := range want {
		if q[k] != v {
			t.Fatalf("%s = %v (%T), want %v\nfull: %+v", k, q[k], q[k], v, q)
		}
	}
	if q["message"] != "You have 7 subtitles to get but 5 downloads left today. At 5 a day that takes 2 days. A free OpenSubtitles account raises it to about 20 a day." {
		t.Fatalf("message = %q", q["message"])
	}
	if n := fake.searches.Load(); n != 0 {
		t.Fatalf("the quota endpoint contacted OpenSubtitles (%d searches)", n)
	}

	// With an account the limit is about 20 and 7 files fit, so there is no warning.
	env.server.Subtitles().SetCredentials("user", "pass")
	q = getJSON[map[string]any](t, env.client, quotaURL)
	if q["hasAccount"] != true || q["limit"] != float64(20) || q["remaining"] != float64(20) || q["message"] != "" || q["daysToFinish"] != float64(1) {
		t.Fatalf("with an account: %+v", q)
	}

	// Several languages multiply the files wanted (titles stay the same).
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitleLanguages": []string{"en", "fr", "it"}}, http.StatusOK)
	q = getJSON[map[string]any](t, env.client, quotaURL)
	if q["missingItems"] != float64(7) || q["missingFiles"] != float64(21) || q["daysToFinish"] != float64(2) {
		t.Fatalf("three languages: %+v", q)
	}
	if msg := q["message"].(string); !strings.HasPrefix(msg, "You have 21 subtitles to get but 20 downloads left today. At 20 a day that takes 2 days.") || strings.Contains(msg, "free OpenSubtitles account") {
		t.Fatalf("account message: %q", msg)
	}
}

func TestSubtitlesGetStopsAtTheQuotaAndReportsIt(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	fake := newFakeOpenSubtitles(t)
	fake.dlLimit.Store(2)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)
	var ids []int64
	for i := 1; i <= 4; i++ {
		ids = append(ids, seedDownloadedMovie(t, env, i, fmt.Sprintf("Movie%d", i)).ID)
	}
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/dismiss", items("movie", ids[3]), http.StatusOK)

	get := env.baseURL + "/api/subtitles/get"
	postJSON[map[string]any](t, env.client, get, map[string]any{}, http.StatusBadRequest)
	postJSON[map[string]any](t, env.client, get, items("movie", 0), http.StatusBadRequest)

	res := postJSON[map[string]any](t, env.client, get, items("movie", ids...), http.StatusOK)
	if res["downloaded"] != float64(2) || res["stopped"] != true || res["skipped"] != float64(1) || res["remaining"] != float64(1) {
		t.Fatalf("expected 2 downloaded, stopped at the quota, 1 skipped (dismissed), 1 waiting: %+v", res)
	}
	if msg := res["message"].(string); !strings.Contains(msg, "Got 2 subtitles.") || !strings.Contains(msg, "daily download limit is reached") || !strings.Contains(msg, "1 subtitle still waiting") {
		t.Fatalf("message = %q", msg)
	}
	quota := res["quota"].(map[string]any)
	if quota["source"] != "reported" || quota["limit"] != float64(2) || quota["used"] != float64(2) || quota["remaining"] != float64(0) || quota["exceeded"] != true || quota["resetsAt"] == nil {
		t.Fatalf("quota after the run: %+v", quota)
	}
	if quota["missingItems"] != float64(1) || quota["missingFiles"] != float64(1) || quota["message"] == "" {
		t.Fatalf("one title is still missing and that should be warned about: %+v", quota)
	}
	for _, id := range ids[:2] {
		m, _ := env.server.MovieRepo.Get(id)
		if _, err := os.Stat(strings.TrimSuffix(m.FilePath, ".mkv") + ".en.srt"); err != nil {
			t.Fatalf("movie %d should have its subtitle: %v", id, err)
		}
	}

	// The quota is remembered: asking again does not even search.
	searches, downloads := fake.searches.Load(), fake.downloads.Load()
	res = postJSON[map[string]any](t, env.client, get, map[string]any{"all": true}, http.StatusOK)
	if res["downloaded"] != float64(0) || res["stopped"] != true {
		t.Fatalf("second run: %+v", res)
	}
	if fake.searches.Load() != searches || fake.downloads.Load() != downloads {
		t.Fatal("a known-exhausted quota must not be tried again")
	}
	// The same numbers come from the GET endpoint.
	q := getJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/quota")
	if q["source"] != "reported" || q["remaining"] != float64(0) || q["exceeded"] != true {
		t.Fatalf("GET quota: %+v", q)
	}
}

func TestSubtitlesGetBypassesTheRetryBackOffAndCanDoEverything(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	fake := newFakeOpenSubtitles(t)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)
	a := seedDownloadedMovie(t, env, 1, "Alpha")
	b := seedDownloadedMovie(t, env, 2, "Beta")
	c := seedDownloadedMovie(t, env, 3, "Gamma")
	if err := env.server.SubtitleAttempts.Record("movie", a.ID, "en"); err != nil {
		t.Fatal(err)
	}

	// The scheduled sweep honours the back-off...
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitleAutoDownload": true}, http.StatusOK)
	env.server.TestSubtitleSweepJob(context.Background())
	if _, err := os.Stat(strings.TrimSuffix(a.FilePath, ".mkv") + ".en.srt"); err == nil {
		t.Fatal("the sweep should have skipped Alpha, which it looked for recently")
	}
	// ...a manual request does not.
	res := postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/get", items("movie", a.ID), http.StatusOK)
	if res["downloaded"] != float64(1) || res["stopped"] != false {
		t.Fatalf("manual request: %+v", res)
	}
	if _, err := os.Stat(strings.TrimSuffix(a.FilePath, ".mkv") + ".en.srt"); err != nil {
		t.Fatalf("Alpha's subtitle: %v", err)
	}

	// Asking for a title with nothing missing, or one that does not exist, downloads nothing.
	res = postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/get", items("movie", a.ID, 9999), http.StatusOK)
	if res["downloaded"] != float64(0) || res["skipped"] != float64(1) {
		t.Fatalf("done title and unknown title: %+v", res)
	}

	// all = everything still missing (the sweep already did Beta and Gamma).
	_ = b
	_ = c
	res = postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/get", map[string]any{"all": true}, http.StatusOK)
	if res["downloaded"] != float64(0) {
		t.Fatalf("nothing is left to get: %+v", res)
	}
	if !strings.Contains(res["message"].(string), "nothing to get") {
		t.Fatalf("message = %q", res["message"])
	}
}

func TestSubtitlesGetAllFetchesEveryMissingTitle(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	fake := newFakeOpenSubtitles(t)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)
	for i := 1; i <= 3; i++ {
		seedDownloadedMovie(t, env, i, fmt.Sprintf("Movie%d", i))
	}
	res := postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/get", map[string]any{"all": true}, http.StatusOK)
	if res["downloaded"] != float64(3) || res["stopped"] != false || res["remaining"] != float64(0) {
		t.Fatalf("all: %+v", res)
	}
	if q := res["quota"].(map[string]any); q["missingItems"] != float64(0) || q["used"] != float64(3) {
		t.Fatalf("quota after all: %+v", q)
	}
}

func TestSubtitlesGetNeedsAKey(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/get", map[string]any{"all": true}, http.StatusPreconditionFailed)
}

func TestQuotaSurvivesARestart(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	fake := newFakeOpenSubtitles(t)
	fake.dlLimit.Store(3)
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)
	m := seedDownloadedMovie(t, env, 1, "Alpha")
	postJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/get", items("movie", m.ID), http.StatusOK)

	// The figures live in the database, not in the client: a rebuilt client (as
	// after a key change, or a restart) still knows.
	env.server.TestSetSubtitlesBaseURL("fixture-key", fake.srv.URL)
	q := getJSON[map[string]any](t, env.client, env.baseURL+"/api/subtitles/quota")
	if q["source"] != "reported" || q["limit"] != float64(3) || q["used"] != float64(1) || q["remaining"] != float64(2) {
		t.Fatalf("quota after rebuilding the client: %+v", q)
	}
}
