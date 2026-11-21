package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/requests"
)

// mcpEnv is a server with the MCP address switched on, an administrator's key,
// a read-only key and a member's key.
type mcpEnv struct {
	server   *api.Server
	base     string
	admin    *http.Client
	adminKey string
	roKey    string
	member   string
}

func newMCPEnv(t *testing.T) *mcpEnv {
	t.Helper()
	server, base, admin, member, _ := familyServer(t)
	server.TestSetTMDBBaseURL("fixture-key", arrTMDB(t).URL)
	mk := func(c *http.Client, body map[string]any) string {
		return postJSON[map[string]any](t, c, base+"/api/auth/api-keys", body, http.StatusCreated)["key"].(string)
	}
	e := &mcpEnv{server: server, base: base, admin: admin,
		adminKey: mk(admin, map[string]any{"name": "assistant"}),
		roKey:    mk(admin, map[string]any{"name": "looker", "readOnly": true}),
		member:   mk(member, map[string]any{"name": "mine"}),
	}
	return e
}

func (e *mcpEnv) enable(t *testing.T) {
	t.Helper()
	postJSONMethod[map[string]any](t, e.admin, http.MethodPut, e.base+"/api/system/mcp", map[string]any{"enabled": true}, http.StatusOK)
}

// allowDownloads switches "Let assistants pick releases and start downloads"
// on or off, as the Settings page does.
func (e *mcpEnv) allowDownloads(t *testing.T, on bool) {
	t.Helper()
	postJSONMethod[map[string]any](t, e.admin, http.MethodPut, e.base+"/api/system/mcp", map[string]any{"downloads": on}, http.StatusOK)
}

// rpc posts one raw body to /mcp with a key and returns the status and the decoded answer.
func (e *mcpEnv) rpc(t *testing.T, key, body string, headers ...string) (int, map[string]any, *http.Response) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.base+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, resp
}

func rpcBody(id int, method string, params any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return string(b)
}

// call runs a tool and returns the result map (content, isError) and its text parsed as JSON when it is.
func (e *mcpEnv) call(t *testing.T, key, tool string, args map[string]any) (isError bool, text string, parsed map[string]any) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	status, out, _ := e.rpc(t, key, rpcBody(1, "tools/call", map[string]any{"name": tool, "arguments": args}))
	if status != http.StatusOK || out["error"] != nil {
		t.Fatalf("%s: status %d, answer %v", tool, status, out)
	}
	res := out["result"].(map[string]any)
	isError, _ = res["isError"].(bool)
	text = res["content"].([]any)[0].(map[string]any)["text"].(string)
	_ = json.Unmarshal([]byte(text), &parsed)
	return
}

func TestMCPSwitchAndSignIn(t *testing.T) {
	e := newMCPEnv(t)
	ping := rpcBody(1, "ping", nil)

	// Off by default: the address doesn't exist.
	if st, _, _ := e.rpc(t, e.adminKey, ping); st != http.StatusNotFound {
		t.Fatalf("switched off: status %d, want 404", st)
	}
	// An API key can't switch it on, a signed-in session can.
	req, _ := http.NewRequest(http.MethodPut, e.base+"/api/system/mcp", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("X-API-Key", e.adminKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("an API key switching MCP on: status %d, want 403", resp.StatusCode)
	}
	if st, _, _ := e.rpc(t, e.adminKey, ping); st != http.StatusNotFound {
		t.Fatalf("still off: status %d, want 404", st)
	}
	e.enable(t)
	if got := getJSON[map[string]any](t, e.admin, e.base+"/api/system/mcp"); got["enabled"] != true {
		t.Fatalf("settings: %v", got)
	}

	tests := []struct {
		name   string
		key    string
		header []string
		want   int
	}{
		{"no key", "", nil, http.StatusUnauthorized},
		{"wrong key", "not-a-key", nil, http.StatusUnauthorized},
		{"member key", e.member, nil, http.StatusForbidden},
		{"admin key as Bearer", e.adminKey, nil, http.StatusOK},
		{"admin key as X-Api-Key", "", []string{"X-Api-Key", e.adminKey}, http.StatusOK},
		{"read-only key", e.roKey, nil, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if st, _, _ := e.rpc(t, tc.key, ping, tc.header...); st != tc.want {
				t.Fatalf("status %d, want %d", st, tc.want)
			}
		})
	}

	// GET isn't how this protocol is spoken here.
	for _, path := range []string{"/mcp", "/api/mcp"} {
		resp, err := http.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "POST" {
			t.Fatalf("GET %s: status %d allow %q", path, resp.StatusCode, resp.Header.Get("Allow"))
		}
	}
	// The same server answers under /api/mcp too.
	req, _ = http.NewRequest(http.MethodPost, e.base+"/api/mcp", strings.NewReader(ping))
	req.Header.Set("Authorization", "Bearer "+e.adminKey)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/mcp: status %d", resp.StatusCode)
	}
}

func TestMCPProtocol(t *testing.T) {
	e := newMCPEnv(t)
	e.enable(t)

	errorCode := func(out map[string]any) float64 {
		if m, ok := out["error"].(map[string]any); ok {
			return m["code"].(float64)
		}
		return 0
	}
	tests := []struct {
		name     string
		body     string
		wantCode float64
	}{
		{"not JSON", `{"jsonrpc":`, -32700},
		{"empty body", ``, -32700},
		{"no version", `{"id":1,"method":"ping"}`, -32600},
		{"id is an object", `{"jsonrpc":"2.0","id":{},"method":"ping"}`, -32600},
		{"empty batch", `[]`, -32600},
		{"unknown method", rpcBody(1, "no/such", nil), -32601},
		{"call without a name", rpcBody(1, "tools/call", map[string]any{}), -32602},
		{"unknown tool", rpcBody(1, "tools/call", map[string]any{"name": "format_disk"}), -32602},
		{"unknown prompt", rpcBody(1, "prompts/get", map[string]any{"name": "nope"}), -32602},
		{"prompt missing its argument", rpcBody(1, "prompts/get", map[string]any{"name": "release_group_check"}), -32602},
		{"unknown resource", rpcBody(1, "resources/read", map[string]any{"uri": "mediarium://nothing"}), -32002},
		{"ping works", rpcBody(1, "ping", nil), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, out, _ := e.rpc(t, e.adminKey, tc.body)
			if got := errorCode(out); got != tc.wantCode {
				t.Fatalf("error code %v, want %v: %v", got, tc.wantCode, out)
			}
			if out["jsonrpc"] != "2.0" {
				t.Fatalf("answer isn't JSON-RPC 2.0: %v", out)
			}
		})
	}

	// The protocol version: the newest we know that the client offered, else ours.
	for ask, want := range map[string]string{"2025-03-26": "2025-03-26", "2024-11-05": "2024-11-05", "2099-01-01": "2025-03-26", "": "2025-03-26"} {
		_, out, _ := e.rpc(t, e.adminKey, rpcBody(1, "initialize", map[string]any{"protocolVersion": ask, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}))
		res, _ := out["result"].(map[string]any)
		if res["protocolVersion"] != want {
			t.Errorf("asked %q: got %v, want %s", ask, res["protocolVersion"], want)
		}
		if info, _ := res["serverInfo"].(map[string]any); info["name"] != "Mediarium" {
			t.Errorf("serverInfo: %v", res["serverInfo"])
		}
	}

	// A notification gets no body, only 202.
	st, _, _ := e.rpc(t, e.adminKey, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if st != http.StatusAccepted {
		t.Fatalf("notification: status %d, want 202", st)
	}

	// A batch answers each message that has an id.
	req, _ := http.NewRequest(http.MethodPost, e.base+"/mcp", strings.NewReader(`[`+rpcBody(1, "ping", nil)+`,{"jsonrpc":"2.0","method":"notifications/initialized"},`+rpcBody(2, "tools/list", nil)+`]`))
	req.Header.Set("Authorization", "Bearer "+e.adminKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var batch []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&batch)
	resp.Body.Close()
	if len(batch) != 2 {
		t.Fatalf("batch of 3 (one notification) answered %d messages: %v", len(batch), batch)
	}

	// A client that only accepts a stream gets one event.
	_, _, resp = e.rpc(t, e.adminKey, rpcBody(1, "ping", nil), "Accept", "text/event-stream")
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q, want text/event-stream", ct)
	}
	// One that takes both gets JSON.
	_, _, resp = e.rpc(t, e.adminKey, rpcBody(1, "ping", nil), "Accept", "application/json, text/event-stream")
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type %q, want application/json", ct)
	}

	// Requests are size-limited.
	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", 300<<10) + `"}}`
	if st, out, _ := e.rpc(t, e.adminKey, big); st != http.StatusRequestEntityTooLarge || errorCode(out) != -32600 {
		t.Fatalf("a 300 KB request: status %d, %v", st, out)
	}
}

func TestMCPRateLimit(t *testing.T) {
	e := newMCPEnv(t)
	e.enable(t)
	limited := 0
	for i := 0; i < 300; i++ {
		if st, _, resp := e.rpc(t, e.adminKey, rpcBody(i, "ping", nil)); st == http.StatusTooManyRequests {
			limited++
			if resp.Header.Get("Retry-After") == "" {
				t.Fatal("a 429 needs Retry-After")
			}
		}
	}
	if limited == 0 {
		t.Fatal("300 requests in a burst were never limited")
	}
}

func TestMCPToolList(t *testing.T) {
	e := newMCPEnv(t)
	e.enable(t)
	names := func(key string) map[string]bool {
		_, out, _ := e.rpc(t, key, rpcBody(1, "tools/list", nil))
		got := map[string]bool{}
		for _, tl := range out["result"].(map[string]any)["tools"].([]any) {
			m := tl.(map[string]any)
			if m["description"] == "" || m["inputSchema"] == nil {
				t.Errorf("tool %v has no description or schema", m["name"])
			}
			got[m["name"].(string)] = true
		}
		return got
	}
	// The tools that can end in a download are not listed until an administrator
	// allows downloads for assistants; the other change tools are.
	if got := names(e.adminKey); got["add_movie"] || got["add_series"] || got["search_now"] || !got["set_movie_profile"] {
		t.Errorf("with downloads off: add_movie %v, add_series %v, search_now %v, set_movie_profile %v", got["add_movie"], got["add_series"], got["search_now"], got["set_movie_profile"])
	}
	e.allowDownloads(t, true)
	admin, ro := names(e.adminKey), names(e.roKey)
	reads := []string{"library_search", "library_stats", "lookup_title", "movie_details", "series_details", "wanted_list", "queue_status", "recent_history", "downloads_failed", "health_report", "requests_list", "profiles_list", "release_group_report"}
	writes := []string{"add_movie", "add_series", "search_now", "set_movie_profile", "set_series_profile", "retry_failed_download", "add_preferred_term"}
	for _, n := range reads {
		if !admin[n] || !ro[n] {
			t.Errorf("read tool %s missing (admin %v, read-only %v)", n, admin[n], ro[n])
		}
	}
	for _, n := range writes {
		if !admin[n] {
			t.Errorf("write tool %s missing for an administrator's key", n)
		}
		if ro[n] {
			t.Errorf("write tool %s is listed for a read-only key", n)
		}
	}
	// Resources and prompts are there too.
	_, out, _ := e.rpc(t, e.adminKey, rpcBody(1, "resources/list", nil))
	if len(out["result"].(map[string]any)["resources"].([]any)) == 0 {
		t.Error("no resources")
	}
	_, out, _ = e.rpc(t, e.adminKey, rpcBody(1, "prompts/list", nil))
	if len(out["result"].(map[string]any)["prompts"].([]any)) == 0 {
		t.Error("no prompts")
	}
}

// library puts a small made-up library in place: three movies (two with
// files, from different release groups), a show with one missing aired
// episode, a failed download and two requests.
func (e *mcpEnv) library(t *testing.T) (night, other library.Movie) {
	t.Helper()
	s := e.server
	add := func(tmdb int, title string, year int, file, release, q string) library.Movie {
		m, err := s.MovieRepo.Add(library.Movie{TMDBID: tmdb, Title: title, Year: year, Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		if file != "" {
			if err := s.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, q, file); err != nil {
				t.Fatal(err)
			}
			if release != "" {
				if err := s.MovieRepo.SetReleaseTitle(m.ID, release); err != nil {
					t.Fatal(err)
				}
			}
		}
		return m
	}
	night = add(701, "Night Harbour", 2021, "/movies/Night Harbour (2021)/Night.Harbour.2021.2160p.BluRay.x265-FuN.mkv", "Night.Harbour.2021.2160p.BluRay.x265-FuN", "Bluray-2160p")
	other = add(702, "Tide Pool", 2019, "/movies/Tide Pool (2019)/Tide.Pool.2019.1080p.WEB-DL.x264-NTb.mkv", "Tide.Pool.2019.1080p.WEB-DL.x264-NTb", "WEBDL-1080p")
	add(703, "Lighthouse Keepers", 2023, "", "", "")

	sr, err := s.MovieRepo.AddSeries(library.Series{TMDBID: 9001, Title: "Salt & Static", Year: 2019, Monitored: true}, []library.Episode{
		{Season: 1, Episode: 1, Title: "Static", AirDate: "2019-02-01", Monitored: true},
		{Season: 1, Episode: 2, Title: "Salt", AirDate: "2019-02-08", Monitored: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := s.MovieRepo.ListEpisodes(sr.ID)
	if err := s.MovieRepo.SetEpisodeStatus(eps[0].ID, library.StatusDownloaded, "WEBDL-720p", "/tv/Salt & Static/S01E01.Salt.and.Static.720p.WEB-DL.x264-BAD.mkv"); err != nil {
		t.Fatal(err)
	}

	id, err := s.QueueRepo.Enqueue(queue.Item{MovieID: other.ID, ReleaseTitle: "Tide.Pool.2019.720p.WEB-DL.x264-BAD", Protocol: queue.ProtocolUsenet, NZBURL: "http://127.0.0.1:1/x.nzb"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.QueueRepo.SetStatus(id, queue.StatusFailed, "download failed: not enough parts"); err != nil {
		t.Fatal(err)
	}
	for _, r := range []requests.Request{
		{Kind: "movie", Title: "Request A", Year: 2020, Payload: "{}"},
		{Kind: "tv", Title: "Request B", Year: 2021, Payload: "{}"},
	} {
		if _, err := s.Requests.Create(r); err != nil {
			t.Fatal(err)
		}
	}
	return night, other
}

func TestMCPReadTools(t *testing.T) {
	e := newMCPEnv(t)
	e.enable(t)
	night, other := e.library(t)
	_ = night

	// Every read tool answers with a result that is not an error, and the ones
	// with a checkable answer give the right one.
	type check func(t *testing.T, text string, p map[string]any)
	total := func(want float64) check {
		return func(t *testing.T, _ string, p map[string]any) {
			t.Helper()
			if p["total"] != want {
				t.Fatalf("total %v, want %v: %v", p["total"], want, p)
			}
		}
	}
	contains := func(parts ...string) check {
		return func(t *testing.T, text string, _ map[string]any) {
			t.Helper()
			for _, s := range parts {
				if !strings.Contains(text, s) {
					t.Fatalf("answer lacks %q: %s", s, text)
				}
			}
		}
	}
	tests := []struct {
		name string
		tool string
		args map[string]any
		ok   check
	}{
		{"all movies", "library_search", nil, total(3)},
		{"by title", "library_search", map[string]any{"query": "tide"}, total(1)},
		{"4k", "library_search", map[string]any{"quality": "4k"}, total(1)},
		{"no file", "library_search", map[string]any{"has_file": false}, total(1)},
		{"release group", "library_search", map[string]any{"release_group": "ntb"}, total(1)},
		{"size filter needs a file on disk", "library_search", map[string]any{"min_size_gb": 15}, total(0)},
		{"limit and total", "library_search", map[string]any{"limit": 1}, func(t *testing.T, _ string, p map[string]any) {
			if p["total"] != float64(3) || len(p["results"].([]any)) != 1 {
				t.Fatalf("%v", p)
			}
		}},
		{"tv", "library_search", map[string]any{"kind": "tv"}, total(1)},
		{"tv by quality", "library_search", map[string]any{"kind": "tv", "quality": "720p"}, total(1)},
		{"tv by release group that isn't there", "library_search", map[string]any{"kind": "tv", "release_group": "FuN"}, total(0)},
		{"bad kind", "library_search", map[string]any{"kind": "games"}, contains("kind must be")},
		{"stats", "library_stats", nil, contains("movies_by_file_size", "top_release_groups_movies", "FuN")},
		{"movie by title", "movie_details", map[string]any{"query": "night"}, contains("Night Harbour", "FuN", "Bluray-2160p")},
		{"movie by id", "movie_details", map[string]any{"id": other.ID}, contains("Tide Pool", "NTb")},
		{"movie that isn't there", "movie_details", map[string]any{"query": "zzzz"}, contains("No movie")},
		{"series", "series_details", map[string]any{"query": "salt"}, contains("S01E02", "BAD", "missing_aired")},
		{"wanted", "wanted_list", nil, contains("rows", "Lighthouse Keepers")},
		{"queue", "queue_status", map[string]any{"include_finished": true}, contains("counts_by_status", "failed")},
		{"history", "recent_history", map[string]any{"errors_only": true}, contains("entries")},
		{"failed downloads", "downloads_failed", nil, contains("not enough parts", "queue_id", "Tide")},
		{"health", "health_report", nil, contains("items")},
		{"requests", "requests_list", nil, total(2)},
		{"requests by kind", "requests_list", map[string]any{"kind": "tv"}, total(1)},
		{"requests by status", "requests_list", map[string]any{"status": "declined"}, total(0)},
		{"profiles", "profiles_list", nil, contains("allowed_qualities", "preferred_terms")},
		{"release groups", "release_group_report", map[string]any{"group": "FuN"}, func(t *testing.T, text string, p map[string]any) {
			if p["from_group"] != float64(1) || p["not_from_group"] != float64(1) || !strings.Contains(text, "Tide Pool") {
				t.Fatalf("%s", text)
			}
		}},
		{"release groups of shows", "release_group_report", map[string]any{"group": "FuN", "kind": "tv"}, contains("Salt")},
		{"release group needed", "release_group_report", map[string]any{"group": " "}, contains("Give the release group")},
		{"lookup", "lookup_title", map[string]any{"query": "night"}, contains("Night Harbour", "in_library")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isErr, text, parsed := e.call(t, e.roKey, tc.tool, tc.args) // a read-only key is enough for every read tool
			if isErr && !strings.Contains(tc.name, "bad") && !strings.Contains(tc.name, "isn't") && !strings.Contains(tc.name, "needed") {
				t.Fatalf("isError: %s", text)
			}
			tc.ok(t, text, parsed)
		})
	}
}

func TestMCPWritesNeedConfirmAndAdmin(t *testing.T) {
	e := newMCPEnv(t)
	e.enable(t)
	e.allowDownloads(t, true)
	_, other := e.library(t)

	profiles := getJSON[map[string]any](t, e.admin, e.base+"/api/quality-profiles")["profiles"].([]any)
	if len(profiles) < 2 {
		t.Fatal("expected seeded profiles")
	}
	target := profiles[1].(map[string]any)
	targetName := target["name"].(string)

	movieProfile := func() float64 {
		return getJSON[map[string]any](t, e.admin, e.base+"/api/movies/"+mcpID(other.ID))["profileId"].(float64)
	}
	before := movieProfile()
	if before == target["id"].(float64) {
		target = profiles[2].(map[string]any)
		targetName = target["name"].(string)
	}

	// A read-only key is refused for every write tool, whatever it sends.
	for _, tool := range []string{"add_movie", "add_series", "search_now", "set_movie_profile", "set_series_profile", "retry_failed_download", "add_preferred_term"} {
		isErr, text, _ := e.call(t, e.roKey, tool, map[string]any{"confirm": true, "movie_id": other.ID, "profile": targetName, "tmdb_id": 701, "queue_id": 1, "term": "x", "series_ids": []int{1}})
		if !isErr || !strings.Contains(text, "read-only") {
			t.Fatalf("%s with a read-only key: isError %v, %s", tool, isErr, text)
		}
	}
	if movieProfile() != before {
		t.Fatal("a read-only key changed a profile")
	}

	// Without confirm: a preview, and nothing changes.
	isErr, text, p := e.call(t, e.adminKey, "set_movie_profile", map[string]any{"movie_id": other.ID, "profile": targetName})
	if isErr || p["preview"] != true || p["changed"] != false || !strings.Contains(text, "confirm") || !strings.Contains(p["would_do"].(string), targetName) {
		t.Fatalf("preview: %s", text)
	}
	if movieProfile() != before {
		t.Fatal("the preview changed the profile")
	}
	// confirm: false is the same as leaving it out.
	if _, _, p = e.call(t, e.adminKey, "set_movie_profile", map[string]any{"movie_id": other.ID, "profile": targetName, "confirm": false}); p["preview"] != true {
		t.Fatalf("confirm false: %v", p)
	}
	if movieProfile() != before {
		t.Fatal("confirm false changed the profile")
	}
	// With confirm: it happens.
	if isErr, text, p = e.call(t, e.adminKey, "set_movie_profile", map[string]any{"movie_id": other.ID, "profile": targetName, "confirm": true}); isErr || p["changed"] != true {
		t.Fatalf("confirmed: %s", text)
	}
	if movieProfile() != target["id"].(float64) {
		t.Fatalf("profile is %v, want %v", movieProfile(), target["id"])
	}
	// Mistakes come back as text the assistant can use.
	if isErr, text, _ = e.call(t, e.adminKey, "set_movie_profile", map[string]any{"movie_id": other.ID, "profile": "No Such Profile", "confirm": true}); !isErr || !strings.Contains(text, "The profiles are") {
		t.Fatalf("unknown profile: %v %s", isErr, text)
	}

	// A preferred term.
	listTerms := func() int {
		for _, pr := range getJSON[map[string]any](t, e.admin, e.base+"/api/quality-profiles")["profiles"].([]any) {
			if pr.(map[string]any)["id"] == target["id"] {
				return len(pr.(map[string]any)["preferred"].([]any))
			}
		}
		return -1
	}
	n := listTerms()
	if _, _, p = e.call(t, e.adminKey, "add_preferred_term", map[string]any{"profile": targetName, "term": "FuN", "score": 25}); p["preview"] != true || listTerms() != n {
		t.Fatalf("term preview: %v (terms %d, was %d)", p, listTerms(), n)
	}
	if isErr, text, _ = e.call(t, e.adminKey, "add_preferred_term", map[string]any{"profile": targetName, "term": "FuN", "score": 25, "confirm": true}); isErr || listTerms() != n+1 {
		t.Fatalf("term confirmed: %s (terms %d, was %d)", text, listTerms(), n)
	}

	// Retrying a failed download blocklists its release.
	_, _, failed := e.call(t, e.adminKey, "downloads_failed", nil)
	qid := failed["failed"].([]any)[0].(map[string]any)["queue_id"].(float64)
	if _, _, p = e.call(t, e.adminKey, "retry_failed_download", map[string]any{"queue_id": qid}); p["preview"] != true {
		t.Fatalf("retry preview: %v", p)
	}
	if bl := getJSON[any](t, e.admin, e.base+"/api/blocklist"); strings.Contains(mcpJSON(bl), "BAD") {
		t.Fatalf("the preview blocklisted the release: %v", bl)
	}
	if isErr, text, _ = e.call(t, e.adminKey, "retry_failed_download", map[string]any{"queue_id": qid, "confirm": true}); isErr {
		t.Fatalf("retry: %s", text)
	}
	if bl := getJSON[any](t, e.admin, e.base+"/api/blocklist"); !strings.Contains(mcpJSON(bl), "BAD") {
		t.Fatalf("the release was not blocklisted: %v", bl)
	}

	// Adding titles: preview first, then for real.
	if _, _, p = e.call(t, e.adminKey, "add_movie", map[string]any{"tmdb_id": 701}); p["preview"] != nil {
		// 701 is not in the library yet only in a fresh test; library() adds it, so it is refused.
		t.Fatalf("add_movie of a title already there: %v", p)
	}
	if isErr, text, _ = e.call(t, e.adminKey, "add_movie", map[string]any{"tmdb_id": 701}); !isErr || !strings.Contains(text, "already in the library") {
		t.Fatalf("duplicate: %v %s", isErr, text)
	}
	if isErr, text, _ = e.call(t, e.adminKey, "add_series", map[string]any{"tmdb_id": 9001}); !isErr || !strings.Contains(text, "already in the library") {
		t.Fatalf("duplicate show: %v %s", isErr, text)
	}
	if isErr, text, _ = e.call(t, e.adminKey, "search_now", nil); !isErr || !strings.Contains(text, "at least one") {
		t.Fatalf("empty search_now: %v %s", isErr, text)
	}
}

func TestMCPAddTitles(t *testing.T) {
	e := newMCPEnv(t) // an empty library: the fake TMDB knows movie 701 and show 9001
	e.enable(t)
	e.allowDownloads(t, true)

	count := func(path string) int { return len(getJSON[[]any](t, e.admin, e.base+path)) }

	if _, _, p := e.call(t, e.adminKey, "add_movie", map[string]any{"tmdb_id": 701, "search_now": false}); p["preview"] != true || !strings.Contains(p["would_do"].(string), "Night Harbour") || count("/api/movies") != 0 {
		t.Fatalf("movie preview: %v", p)
	}
	if isErr, text, p := e.call(t, e.adminKey, "add_movie", map[string]any{"tmdb_id": 701, "search_now": false, "confirm": true}); isErr || p["changed"] != true || count("/api/movies") != 1 {
		t.Fatalf("movie added: %s", text)
	}
	if _, _, p := e.call(t, e.adminKey, "add_series", map[string]any{"tmdb_id": 9001, "monitor": "future", "search_now": false}); p["preview"] != true || count("/api/series") != 0 {
		t.Fatalf("series preview: %v", p)
	}
	if isErr, text, _ := e.call(t, e.adminKey, "add_series", map[string]any{"tvdb_id": 777001, "search_now": false, "confirm": true}); isErr || count("/api/series") != 1 {
		t.Fatalf("series added by TVDB id: %s", text)
	}
	if isErr, text, _ := e.call(t, e.adminKey, "add_series", map[string]any{"tmdb_id": 9001, "monitor": "sometimes"}); !isErr || !strings.Contains(text, "monitor must be") {
		t.Fatalf("bad monitor: %v %s", isErr, text)
	}
	// Searching with no indexer set up says so per title instead of failing the call.
	movies := getJSON[[]map[string]any](t, e.admin, e.base+"/api/movies")
	isErr, text, p := e.call(t, e.adminKey, "search_now", map[string]any{"movie_ids": []any{movies[0]["id"]}, "confirm": true})
	if isErr || p["changed"] != true || !strings.Contains(text, "Night Harbour") {
		t.Fatalf("search_now movie: %s", text)
	}
	series := getJSON[[]map[string]any](t, e.admin, e.base+"/api/series")
	isErr, text, p = e.call(t, e.adminKey, "search_now", map[string]any{"series_ids": []any{series[0]["id"]}, "confirm": true})
	if isErr || p["changed"] != true {
		t.Fatalf("search_now series: %s", text)
	}
}

func TestMCPNeverLogsArguments(t *testing.T) {
	// The tool call log line names the tool, never what it was called with.
	// The wording is checked in the source so a later edit can't add them.
	src, err := os.ReadFile("mcp_tools.go")
	if err != nil {
		t.Skip(err)
	}
	i := bytes.Index(src, []byte(`slog.Info("mcp: tool call"`))
	if i < 0 {
		t.Fatal("the tool call log line is gone")
	}
	line := string(src[i : i+bytes.IndexByte(src[i:], '\n')])
	for _, bad := range []string{"p.Arguments", "args", "raw"} {
		if strings.Contains(line, bad) {
			t.Fatalf("the log line mentions %s: %s", bad, line)
		}
	}
}

func mcpID(n int64) string { return strconv.FormatInt(n, 10) }

func mcpJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
