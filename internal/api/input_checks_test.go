package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
)

// wantBad sends a request and wants a 400 whose message contains want.
func wantBad(t *testing.T, client *http.Client, method, url string, body any, want string) {
	t.Helper()
	status, out := doJSONStatus(t, client, method, url, body)
	msg, _ := out["error"].(string)
	if status != http.StatusBadRequest {
		t.Errorf("%s %s %v: status %d (%q), want 400 mentioning %q", method, url, body, status, msg, want)
		return
	}
	if !strings.Contains(msg, want) {
		t.Errorf("%s %s %v: message %q does not mention %q", method, url, body, msg, want)
	}
}

// wantOK sends a request and wants a success status.
func wantOK(t *testing.T, client *http.Client, method, url string, body any) map[string]any {
	t.Helper()
	status, out := doJSONStatus(t, client, method, url, body)
	if status >= 300 {
		t.Errorf("%s %s %v: status %d (%v), want success", method, url, body, status, out["error"])
	}
	return out
}

func TestSettingsRejectValuesThatCannotWork(t *testing.T) {
	_, base, client := loginNewServer(t)
	url := base + "/api/settings"
	long := strings.Repeat("a", 5000)

	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"movies folder not absolute", map[string]any{"moviesPath": "movies/here"}, "folder path isn't complete"},
		{"movies folder with a NUL", map[string]any{"moviesPath": "/media/mov\x00ies"}, "control characters"},
		{"movies folder far too long", map[string]any{"moviesPath": "/" + long}, "at most 4096"},
		{"TV folder not absolute", map[string]any{"tvPath": "tv"}, "folder path isn't complete"},
		{"downloads folder with ~", map[string]any{"downloadsPath": "~/downloads"}, "folder path isn't complete"},
		{"music folder not absolute", map[string]any{"musicPath": "music"}, "folder path isn't complete"},
		{"unknown naming style", map[string]any{"namingPreset": "fancy"}, "naming styles"},
		{"custom format with a slash", map[string]any{"namingPreset": "custom", "movieNameFormat": "{Movie Title}/{Year}"}, "can't contain /"},
		{"custom format with an unknown token", map[string]any{"namingPreset": "custom", "movieNameFormat": "{Movie Title} {Nope}"}, "isn't a token"},
		{"custom format missing a brace", map[string]any{"namingPreset": "custom", "movieNameFormat": "{Movie Title"}, "{ or } is missing"},
		{"custom format without the title", map[string]any{"namingPreset": "custom", "movieNameFormat": "{Year}"}, "Include the movie's title"},
		{"custom format with a control character", map[string]any{"movieNameFormat": "{Movie Title}\n{Year}"}, "control characters"},
		{"torrent port text", map[string]any{"torrentListenPort": "abc"}, "torrent port"},
		{"torrent port negative", map[string]any{"torrentListenPort": "-5"}, "torrent port"},
		{"torrent port too big", map[string]any{"torrentListenPort": "70000"}, "torrent port"},
		{"seed ratio negative", map[string]any{"torrentSeedRatioLimit": "-1"}, "seed ratio limit must be a number"},
		{"seed ratio not a number", map[string]any{"torrentSeedRatioLimit": "NaN"}, "seed ratio limit must be a number"},
		{"seed ratio in exponent form", map[string]any{"torrentSeedRatioLimit": "1e3"}, "seed ratio limit must be a number"},
		{"seed time far too big", map[string]any{"torrentSeedTimeLimitH": "99999999"}, "seed time limit must be a number"},
		{"unknown character mode", map[string]any{"illegalCharMode": "explode"}, "strip"},
		{"replacement not allowed in file names", map[string]any{"illegalCharReplacement": "a<"}, "isn't allowed in file names"},
		{"replacement too long", map[string]any{"illegalCharReplacement": "abcd"}, "at most 3 characters"},
		{"unknown conflict policy", map[string]any{"importConflictPolicy": "delete"}, "already exists"},
		{"unknown download source", map[string]any{"defaultSources": "carrier-pigeon"}, "usenet"},
		{"monitor interval negative", map[string]any{"monitorIntervalMinutes": -1}, "check interval"},
		{"monitor interval too big", map[string]any{"monitorIntervalMinutes": 100000}, "check interval"},
		{"history days negative", map[string]any{"historyRetentionDays": -1}, "History retention"},
		{"history days too big", map[string]any{"historyRetentionDays": 40000}, "History retention"},
		{"language with a space", map[string]any{"subtitleLanguages": []string{"en", "x y"}}, "isn't a language code"},
		{"language that climbs out of a folder", map[string]any{"subtitleLanguages": []string{"../etc"}}, "isn't a language code"},
		{"only blank languages", map[string]any{"subtitleLanguages": []string{" "}}, "at least one subtitle language"},
		{"legal date that is not a date", map[string]any{"legalAcknowledgedAt": "yesterday"}, "date doesn't look right"},
		{"TMDB key with a space", map[string]any{"tmdbApiKey": "abc def"}, "space or line break"},
		{"OpenSubtitles key with a line break", map[string]any{"openSubtitlesApiKey": "abc\ndef"}, "space or line break"},
		{"Trakt ID with a space", map[string]any{"traktClientId": "abc def"}, "space or line break"},
		{"OpenSubtitles username with a line break", map[string]any{"openSubtitlesUsername": "ry\nan"}, "control characters"},
		{"helper address without http", map[string]any{"flareSolverrUrl": "flaresolverr:8191"}, "http://"},
		{"helper address with a space", map[string]any{"flareSolverrUrl": "http://flare solverr:8191"}, "spaces"},
		{"helper address on another scheme", map[string]any{"flareSolverrUrl": "ftp://nope"}, "http://"},
		{"default profile below zero", map[string]any{"defaultProfileId": -3}, "doesn't exist"},
		{"default profile that is not there", map[string]any{"defaultProfileId": 99999}, "doesn't exist"},
		{"music profile below zero", map[string]any{"musicDefaultProfileId": -3}, "doesn't exist"},
	} {
		t.Run(tc.name, func(t *testing.T) { wantBad(t, client, http.MethodPut, url, tc.body, tc.want) })
	}
}

func TestSettingsAcceptGoodValuesAndBlanks(t *testing.T) {
	_, base, client := loginNewServer(t)
	url := base + "/api/settings"
	dir := t.TempDir()

	for _, body := range []map[string]any{
		{"moviesPath": dir, "tvPath": filepath.Join(dir, "tv"), "downloadsPath": " " + filepath.Join(dir, "dl") + " "},
		{"namingPreset": "custom", "movieNameFormat": "{Movie Title} ({Year:0000}) [{Quality}]"},
		{"namingPreset": "plex", "movieNameFormat": "anything goes while another style is picked"},
		{"torrentListenPort": "0", "torrentSeedRatioLimit": "0", "torrentSeedTimeLimitH": "0"},
		{"torrentListenPort": "51413", "torrentSeedRatioLimit": "2.5", "torrentSeedTimeLimitH": "48"},
		{"illegalCharMode": "replace", "illegalCharReplacement": "_", "importConflictPolicy": "overwrite_if_better"},
		{"monitorIntervalMinutes": 0}, {"monitorIntervalMinutes": 45},
		{"historyRetentionDays": 0}, {"historyRetentionDays": 90},
		{"subtitleLanguages": []string{"en", "pt-BR", "zh-CN"}},
		{"legalAcknowledgedAt": "2026-01-02T03:04:05Z"},
		{"flareSolverrUrl": "http://flaresolverr:8191/"}, {"flareSolverrUrl": ""},
		{"defaultSources": "both"},
		{},
	} {
		wantOK(t, client, http.MethodPut, url, body)
	}
	got := getJSON[map[string]any](t, client, url)
	if got["subtitleLanguages"] == nil {
		t.Fatalf("settings lost the languages: %v", got)
	}
	if !strings.HasSuffix(fmt.Sprint(got["downloadsPath"]), "dl") || strings.Contains(fmt.Sprint(got["downloadsPath"]), " ") {
		t.Fatalf("the downloads folder should be saved without spaces around it: %q", got["downloadsPath"])
	}
}

// The Settings page can send back the whole object it read. Nothing in it may
// be rejected just for being a default, blank or older value.
func TestSettingsWholeObjectRoundTrip(t *testing.T) {
	_, base, client := loginNewServer(t)
	url := base + "/api/settings"
	current := getJSON[map[string]any](t, client, url)
	wantOK(t, client, http.MethodPut, url, current)

	// After some real settings are saved, echoing them still works.
	wantOK(t, client, http.MethodPut, url, map[string]any{
		"moviesPath": t.TempDir(), "namingPreset": "custom", "movieNameFormat": "{Movie Title} ({Year})",
		"illegalCharMode": "replace", "illegalCharReplacement": "-", "torrentSeedRatioLimit": "1.5",
		"flareSolverrUrl": "http://flaresolverr:8191",
	})
	current = getJSON[map[string]any](t, client, url)
	wantOK(t, client, http.MethodPut, url, current)
}

func TestSettingsRejectedUpdateChangesNothing(t *testing.T) {
	_, base, client := loginNewServer(t)
	url := base + "/api/settings"
	before := getJSON[map[string]any](t, client, url)["moviesPath"]

	wantBad(t, client, http.MethodPut, url, map[string]any{"moviesPath": t.TempDir(), "flareSolverrUrl": "nope"}, "http://")
	wantBad(t, client, http.MethodPut, url, map[string]any{"moviesPath": t.TempDir(), "defaultProfileId": 99999}, "doesn't exist")
	if after := getJSON[map[string]any](t, client, url)["moviesPath"]; after != before {
		t.Fatalf("a rejected update must not change anything: %v -> %v", before, after)
	}
}

func TestTestServiceRejectsBadKeys(t *testing.T) {
	_, base, client := loginNewServer(t)
	url := base + "/api/settings/test-service"
	for _, tc := range []struct {
		body map[string]any
		want string
	}{
		{map[string]any{"service": "tmdb", "key": "abc def"}, "space or line break"},
		{map[string]any{"service": "trakt", "key": strings.Repeat("x", 600)}, "much longer"},
		{map[string]any{"service": "opensubtitles", "key": "k", "username": "a\nb"}, "control characters"},
		{map[string]any{"service": "nope"}, "Choose which service"},
	} {
		wantBad(t, client, http.MethodPost, url, tc.body, tc.want)
	}
}

func TestIndexerChecks(t *testing.T) {
	server, base, client := loginNewServer(t)
	idx := newTVIndexerWith(t, nil)
	url := base + "/api/indexers"
	long := strings.Repeat("n", 81)

	good := func(over map[string]any) map[string]any {
		b := map[string]any{"name": "Geek", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"no name", good(map[string]any{"name": " "}), "Give this indexer a name"},
		{"name too long", good(map[string]any{"name": long}), "at most 80"},
		{"name with a line break", good(map[string]any{"name": "a\nb"}), "control characters"},
		{"no address", good(map[string]any{"baseUrl": ""}), "Add the address"},
		{"address without http", good(map[string]any{"baseUrl": "api.example.com"}), "Start the address with http://"},
		{"address on another scheme", good(map[string]any{"baseUrl": "ftp://api.example.com"}), "http:// or https://"},
		{"address with a space", good(map[string]any{"baseUrl": "http://api example.com"}), "spaces"},
		{"address with a bad port", good(map[string]any{"baseUrl": "http://api.example.com:99999"}), "port"},
		{"key with a space", good(map[string]any{"apiKey": "ab cd"}), "space or line break"},
		{"key with a line break", good(map[string]any{"apiKey": "ab\ncd"}), "space or line break"},
		{"unknown kind of source", good(map[string]any{"protocol": "carrier-pigeon"}), `"usenet" or "torrent"`},
	} {
		t.Run("create/"+tc.name, func(t *testing.T) { wantBad(t, client, http.MethodPost, url, tc.body, tc.want) })
	}
	if list := getJSON[[]map[string]any](t, client, url); len(list) != 0 {
		t.Fatalf("nothing should have been saved: %v", list)
	}

	created := postJSON[map[string]any](t, client, url, good(nil), http.StatusCreated)
	one := fmt.Sprintf("%s/%.0f", url, created["id"])
	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"new address without http", map[string]any{"baseUrl": "api.example.com"}, "Start the address with http://"},
		{"new address with a space", map[string]any{"baseUrl": "http://a b.example"}, "spaces"},
		{"new key with a space", map[string]any{"apiKey": "ab cd"}, "space or line break"},
		{"name too long", map[string]any{"name": long}, "at most 80"},
		{"unknown kind", map[string]any{"protocol": "smoke-signal"}, `"usenet" or "torrent"`},
	} {
		t.Run("update/"+tc.name, func(t *testing.T) { wantBad(t, client, http.MethodPut, one, tc.body, tc.want) })
	}
	// Blank name or key means "keep what is saved", as before.
	wantOK(t, client, http.MethodPut, one, map[string]any{"name": " ", "apiKey": ""})

	t.Run("test/bad address", func(t *testing.T) {
		wantBad(t, client, http.MethodPost, url+"/test", map[string]any{"name": "x", "baseUrl": "nonsense", "apiKey": "k"}, "Start the address with http://")
		wantBad(t, client, http.MethodPost, url+"/test", map[string]any{"name": "x", "baseUrl": idx.URL, "apiKey": "a b"}, "space or line break")
	})

	// A source saved by an older version keeps working when only something else is edited.
	old, err := server.IndexerRepo.Create(indexers.Instance{Name: "Old", BaseURL: "legacy-host:8080/api", APIKey: "k", Protocol: indexers.ProtocolUsenet, Enabled: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldURL := fmt.Sprintf("%s/%d", url, old.ID)
	wantOK(t, client, http.MethodPut, oldURL, map[string]any{"name": "Renamed", "baseUrl": "legacy-host:8080/api", "enabled": false})
	wantBad(t, client, http.MethodPut, oldURL, map[string]any{"baseUrl": "still-not-a-web-address"}, "Start the address with http://")
}

func TestVPNConfigChecks(t *testing.T) {
	_, base, client := loginNewServer(t)
	url := base + "/api/vpn/configs"
	key := strings.Repeat("A", 43) + "="
	good := func(over map[string]any) map[string]any {
		b := map[string]any{
			"label": "My VPN", "provider": "custom", "privateKey": key, "peerPublicKey": key,
			"endpoint": "vpn.example.com:51820", "localAddresses": []string{"10.2.0.2/32"},
			"allowedIps": []string{"0.0.0.0/0", "::/0"}, "dns": []string{"10.64.0.1"},
		}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"no name", good(map[string]any{"label": ""}), "Give this connection a name"},
		{"name too long", good(map[string]any{"label": strings.Repeat("n", 61)}), "at most 60"},
		{"provider with a line break", good(map[string]any{"provider": "a\nb"}), "control characters"},
		{"no private key", good(map[string]any{"privateKey": ""}), "Paste the private key"},
		{"private key too short", good(map[string]any{"privateKey": "abc="}), "44 characters"},
		{"private key that is a whole config line", good(map[string]any{"privateKey": "PrivateKey = " + key}), "44 characters"},
		{"no public key", good(map[string]any{"peerPublicKey": ""}), "Paste the server's public key"},
		{"public key not base64", good(map[string]any{"peerPublicKey": strings.Repeat("!", 43) + "="}), "44 characters"},
		{"preshared key wrong", good(map[string]any{"presharedKey": "nope"}), "preshared key"},
		{"no endpoint", good(map[string]any{"endpoint": ""}), "Add the server address and port"},
		{"endpoint without a port", good(map[string]any{"endpoint": "vpn.example.com"}), "Add the port"},
		{"endpoint port zero", good(map[string]any{"endpoint": "vpn.example.com:0"}), "between 1 and 65535"},
		{"endpoint port too big", good(map[string]any{"endpoint": "vpn.example.com:70000"}), "between 1 and 65535"},
		{"endpoint with a scheme", good(map[string]any{"endpoint": "udp://vpn.example.com:51820"}), "address and port only"},
		{"endpoint that is not a host", good(map[string]any{"endpoint": "bad host!:51820"}), "address and port only"},
		{"no local address", good(map[string]any{"localAddresses": []string{}}), "Add the address your VPN gave"},
		{"local address not an IP", good(map[string]any{"localAddresses": []string{"my-laptop"}}), "IP address"},
		{"local address prefix too big", good(map[string]any{"localAddresses": []string{"10.2.0.2/33"}}), "between 0 and 32"},
		{"allowed range without a size", good(map[string]any{"allowedIps": []string{"10.0.0.0"}}), "range size"},
		{"DNS server as a name", good(map[string]any{"dns": []string{"dns.example.com"}}), "IP address"},
		{"too many addresses", good(map[string]any{"localAddresses": []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5", "10.0.0.6", "10.0.0.7", "10.0.0.8", "10.0.0.9"}}), "at most 8"},
	} {
		t.Run(tc.name, func(t *testing.T) { wantBad(t, client, http.MethodPost, url, tc.body, tc.want) })
	}
	if list := getJSON[[]map[string]any](t, client, url); len(list) != 0 {
		t.Fatalf("nothing should have been saved: %v", list)
	}
	for _, body := range []map[string]any{
		good(nil),
		good(map[string]any{"label": "IPv6", "endpoint": "[2001:db8::1]:51820", "localAddresses": []string{"fd00::2/128"}, "presharedKey": key}),
		good(map[string]any{"label": "Bare", "provider": "", "allowedIps": nil, "dns": nil, "localAddresses": []string{" 10.2.0.3 ", ""}}),
	} {
		postJSON[map[string]any](t, client, url, body, http.StatusCreated)
	}
}

func TestMigrateRequestChecks(t *testing.T) {
	_, base, client := loginNewServer(t)
	conn := func(url, key string) map[string]string { return map[string]string{"url": url, "apiKey": key} }
	for _, path := range []string{"/api/migrate/preview", "/api/migrate/run"} {
		for _, tc := range []struct {
			name string
			body map[string]any
			want string
		}{
			{"no address", map[string]any{"radarr": conn("", "k")}, "Add the address of Radarr"},
			{"address on another scheme", map[string]any{"sonarr": conn("ftp://nas:8989", "k")}, "http:// or https://"},
			{"address with a space", map[string]any{"sonarr": conn("http://my nas:8989", "k")}, "spaces"},
			{"no key", map[string]any{"radarr": conn("http://nas:7878", " ")}, "Add the API key for Radarr"},
			{"key with a space", map[string]any{"radarr": conn("http://nas:7878", "ab cd")}, "space or line break"},
			{"key that would change the path", map[string]any{"sickchill": conn("http://nas:8081", "ab/../cd")}, "characters an API key doesn't use"},
			{"NZBGet address missing", map[string]any{"nzbget": map[string]string{"url": "", "username": "u", "password": "p"}}, "Add the address of NZBGet"},
			{"NZBGet username with a line break", map[string]any{"nzbget": map[string]string{"url": "http://nas:6789", "username": "u\nx", "password": "p"}}, "control characters"},
			{"half a folder mapping", map[string]any{"radarr": conn("http://nas:7878", "k"), "pathMap": []map[string]string{{"from": "/data/movies", "to": ""}}}, "both sides"},
			{"mapping to a relative folder", map[string]any{"radarr": conn("http://nas:7878", "k"), "pathMap": []map[string]string{{"from": "/data/movies", "to": "movies"}}}, "folder path isn't complete"},
			{"profile that cannot exist", map[string]any{"radarr": conn("http://nas:7878", "k"), "profileMapping": map[string]int{"HD": -2}}, "doesn't exist"},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) { wantBad(t, client, http.MethodPost, base+path, tc.body, tc.want) })
		}
	}
}

func TestLibraryScanPathChecks(t *testing.T) {
	_, base, client := loginNewServer(t)
	url := base + "/api/library/scan"
	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"no folder", map[string]any{"path": "  ", "kind": "movie"}, "Enter the folder to scan"},
		{"relative folder", map[string]any{"path": "movies", "kind": "movie"}, "folder path isn't complete"},
		{"folder with a NUL", map[string]any{"path": "/media/mov\x00ies", "kind": "movie"}, "control characters"},
		{"folder far too long", map[string]any{"path": "/" + strings.Repeat("a", 5000), "kind": "movie"}, "at most 4096"},
		{"unknown kind", map[string]any{"path": "/media", "kind": "books"}, `"movie" or "tv"`},
	} {
		t.Run(tc.name, func(t *testing.T) { wantBad(t, client, http.MethodPost, url, tc.body, tc.want) })
	}
}

func TestQualityProfileChecks(t *testing.T) {
	_, base, client := loginNewServer(t)
	url := base + "/api/quality-profiles"
	tiers := getJSON[map[string]any](t, client, url)["tiers"].([]any)
	first, second := tiers[len(tiers)-2].(string), tiers[len(tiers)-1].(string)
	good := func(over map[string]any) map[string]any {
		b := map[string]any{"name": "Checked", "allowed": []string{first, second}, "cutoff": second, "upgradeAllowed": true}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"no name", good(map[string]any{"name": "  "}), "Give this profile a name"},
		{"name too long", good(map[string]any{"name": strings.Repeat("n", 61)}), "at most 60"},
		{"name with a line break", good(map[string]any{"name": "a\nb"}), "control characters"},
		{"no qualities", good(map[string]any{"allowed": []string{}}), "at least one quality"},
		{"unknown quality", good(map[string]any{"allowed": []string{"Ultra Mega"}, "cutoff": "Ultra Mega"}), "isn't a quality"},
		{"no cutoff", good(map[string]any{"cutoff": ""}), "cutoff"},
		{"cutoff not allowed", good(map[string]any{"allowed": []string{first}, "cutoff": second}), "cutoff has to be one of"},
		{"required word with a pipe", good(map[string]any{"mustContain": []string{"a|b"}}), "can't contain |"},
		{"excluded word too long", good(map[string]any{"mustNotContain": []string{strings.Repeat("w", 61)}}), "at most 60"},
		{"too many required words", good(map[string]any{"mustContain": manyWords(51)}), "at most 50"},
		{"score out of range", good(map[string]any{"preferred": []map[string]any{{"term": "hdr", "score": 100000}}}), "between -10000 and 10000"},
	} {
		t.Run("create/"+tc.name, func(t *testing.T) { wantBad(t, client, http.MethodPost, url, tc.body, tc.want) })
	}
	created := postJSON[map[string]any](t, client, url, good(nil), http.StatusCreated)
	wantBad(t, client, http.MethodPut, fmt.Sprintf("%s/%.0f", url, created["id"]), good(map[string]any{"name": ""}), "Give this profile a name")
	wantOK(t, client, http.MethodPut, fmt.Sprintf("%s/%.0f", url, created["id"]), good(map[string]any{"name": "Renamed"}))
}

func TestMusicProfileAndArtistChecks(t *testing.T) {
	e := newMusicEnv(t)
	url := e.base + "/api/music/profiles"
	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"no name", map[string]any{"name": " ", "allowed": []string{"FLAC"}, "cutoff": "FLAC"}, "Give this profile a name"},
		{"name too long", map[string]any{"name": strings.Repeat("n", 61), "allowed": []string{"FLAC"}, "cutoff": "FLAC"}, "at most 60"},
		{"no qualities", map[string]any{"name": "X", "allowed": []string{}, "cutoff": "FLAC"}, "at least one quality"},
		{"unknown quality", map[string]any{"name": "X", "allowed": []string{"WAV"}, "cutoff": "WAV"}, "isn't a quality"},
		{"cutoff not allowed", map[string]any{"name": "X", "allowed": []string{"FLAC"}, "cutoff": "MP3-192"}, "cutoff has to be one of"},
	} {
		t.Run("profile/"+tc.name, func(t *testing.T) { wantBad(t, e.client, http.MethodPost, url, tc.body, tc.want) })
	}
	for _, id := range []string{"x", "1234", "11111111-1111-4111-8111-11111111111", "11111111111141118111111111111111"} {
		t.Run("artist/"+id, func(t *testing.T) {
			wantBad(t, e.client, http.MethodPost, e.base+"/api/music/artists", map[string]any{"mbid": id}, "artist ID doesn't look right")
		})
	}
}

func TestSubtitleDownloadChecks(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitlesEnabled": true}, http.StatusOK)
	video := filepath.Join(t.TempDir(), "Some Movie (2001).mkv")
	if err := os.WriteFile(video, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", video); err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("%s/api/movies/%d/subtitles/download", env.baseURL, m.ID)
	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"no file", map[string]any{"language": "en"}, "Pick a subtitle"},
		{"negative file", map[string]any{"fileId": -4, "language": "en"}, "Pick a subtitle"},
		{"language that climbs out of the folder", map[string]any{"fileId": 1, "language": "../../etc/x"}, "language code"},
		{"language with a slash", map[string]any{"fileId": 1, "language": "en/us"}, "language code"},
		{"language with a space", map[string]any{"fileId": 1, "language": "e n"}, "language code"},
	} {
		t.Run(tc.name, func(t *testing.T) { wantBad(t, env.client, http.MethodPost, url, tc.body, tc.want) })
	}
}

func manyWords(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("word%d", i)
	}
	return out
}

func TestFolderCheckPathChecks(t *testing.T) {
	_, base, client := loginNewServer(t)
	for _, tc := range []struct {
		name, path, want string
	}{
		{"folder-check", "/settings/folder-check", "Enter a folder path"},
		{"folder-check?path=%20", "/settings/folder-check?path=%20", "Enter a folder path"},
		{"folder-check?path=relative/dir", "/settings/folder-check?path=relative/dir", "folder path isn't complete"},
		{"folder-check?path=NUL", "/settings/folder-check?path=%2Fmedia%2Fa%00b", "control characters"},
		{"filesystem-check", "/settings/filesystem-check", "both folder paths"},
		{"filesystem-check one relative", "/settings/filesystem-check?a=/downloads&b=movies", "folder path isn't complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := rawRequest(t, client, http.MethodGet, base+"/api"+tc.path, nil)
			if status != http.StatusBadRequest || !strings.Contains(body, tc.want) {
				t.Errorf("GET %s: status %d, body %s; want 400 mentioning %q", tc.path, status, body, tc.want)
			}
		})
	}
	dir := t.TempDir()
	getJSON[map[string]any](t, client, base+"/api/settings/folder-check?path="+dir)
}
