package mediaservers

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// FuzzMapPath: a folder path in any shape maps without a panic, and a path
// inside the mapped folder ends up inside the target folder.
func FuzzMapPath(f *testing.F) {
	f.Add(`/media/movies/A (2020)/a.mkv`, `/media/movies`, `/data/movies`)
	f.Add(`C:\Media\Movies\a.mkv`, `c:\media`, `/mnt/media`)
	f.Add(`\\nas\share\x`, `\\NAS\Share`, `Z:\`)
	f.Add("C:\\"+strings.Repeat("Ⱥ", 8)+`\x`, "C:\\"+strings.Repeat("Ⱥ", 8), `/m`)
	f.Add("İİİİİİ/x", "İİİİİİ", `/y`)
	f.Add(``, ``, ``)
	f.Add(`/`, `/`, `/`)
	f.Add("\xff\xfe/x", "\xff\xfe", "/z")
	f.Fuzz(func(t *testing.T, path, from, to string) {
		got := MapPath(path, []PathMapping{{From: from, To: to}})
		if rest, ok := within(path, from); ok && rest != "" && rest[0] != '/' && comparable(from) != "" {
			t.Fatalf("rest %q of %q inside %q does not start at a folder boundary", rest, path, from)
		}
		if _, ok := within(path, from); ok && to != "" && comparable(from) != "" {
			if !strings.HasPrefix(got, strings.TrimRight(to, `/\`)) {
				t.Fatalf("MapPath(%q) = %q does not start with the target %q", path, got, to)
			}
		}
		_ = MapPath(path, nil)
		_, _ = NormalizePathMap([]PathMapping{{From: from, To: to}})
	})
}

func TestWithinSurvivesCharactersThatChangeLengthWhenLowered(t *testing.T) {
	// "Ⱥ" is 2 bytes and its lower case 3: cutting the original path at the
	// lower-cased length used to run past its end
	dir := `C:\` + strings.Repeat("Ⱥ", 6)
	path := dir + `\x`
	rest, ok := within(path, dir)
	if !ok || rest != "/x" {
		t.Fatalf("within = %q, %v", rest, ok)
	}
	if got := MapPath(path, []PathMapping{{From: dir, To: "/data"}}); got != "/data/x" {
		t.Fatalf("MapPath = %q", got)
	}
	if _, ok := within(`C:\media2\x`, `C:\media`); ok {
		t.Fatal("media2 is not inside media")
	}
	if rest, ok := within(`c:\MEDIA\Movies\`, `C:\Media`); !ok || rest != "/Movies" {
		t.Fatalf("case-insensitive Windows match: %q %v", rest, ok)
	}
}

func FuzzNormalizeURL(f *testing.F) {
	for _, s := range []string{"192.168.1.10:32400", "http://host/", "https://user:pass@host:8096/emby/", "ftp://x", "http://", "http://[::1]:8096", "//x", "http://a b", "http://h/?token=abc#f", "", "\x00", "javascript:alert(1)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got, err := NormalizeURL(raw)
		if err != nil {
			return
		}
		if !strings.HasPrefix(got, "http://") && !strings.HasPrefix(got, "https://") {
			t.Fatalf("NormalizeURL(%q) = %q", raw, got)
		}
		if strings.ContainsAny(got, "?#") || strings.HasSuffix(got, "/") {
			t.Fatalf("NormalizeURL(%q) = %q keeps a query, fragment or trailing slash", raw, got)
		}
		_ = addressKey(raw)
	})
}

// FuzzDiscoveryReplies: what a stranger on the network sends back can only
// ever point Mediarium at an allowed (private) address.
func FuzzDiscoveryReplies(f *testing.F) {
	f.Add([]byte(`{"Address":"http://192.168.1.5:8096","Id":"abc","Name":"Home"}`), "192.168.1.5")
	f.Add([]byte(`{"Address":"http://169.254.169.254:80","Id":"abc"}`), "192.168.1.6")
	f.Add([]byte(`{"Address":"http://evil.example:1","Id":"abc"}`), "10.0.0.2")
	f.Add([]byte("HTTP/1.0 200 OK\r\nContent-Type: plex/media-server\r\nName: Plex\r\nPort: 32400\r\nResource-Identifier: xyz\r\nVersion: 1\r\n\r\n"), "192.168.1.7")
	f.Add([]byte(strings.Repeat("a: b\r\n", 100000)), "192.168.1.7")
	f.Fuzz(func(t *testing.T, data []byte, src string) {
		ip := net.ParseIP(src)
		if ip == nil {
			return
		}
		if found, ok := parseEmbyReply(data, ip, KindJellyfin, PrivateIPv4); ok {
			host, _, err := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(found.Address, "https://"), "http://"))
			if err != nil {
				t.Fatalf("address %q: %v", found.Address, err)
			}
			if h := net.ParseIP(host); h == nil || (!PrivateIPv4(h) && !h.Equal(ip)) {
				t.Fatalf("reply from %v pointed Mediarium at %q", ip, found.Address)
			}
		}
		if found, ok := parseGDM(data, ip); ok {
			host, _, err := net.SplitHostPort(strings.TrimPrefix(found.Address, "http://"))
			if err != nil || !net.ParseIP(host).Equal(ip) {
				t.Fatalf("GDM reply from %v gave %q", ip, found.Address)
			}
		}
	})
}

func FuzzPlexTMDBID(f *testing.F) {
	for _, s := range []string{"tmdb://603", "com.plexapp.agents.themoviedb://603?lang=en", "tmdb://", "tmdb://-5", "tmdb://99999999999999999999", "x", "tmdb://1/2"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, guid string) {
		if id, ok := plexTMDBID(guid); ok && id <= 0 {
			t.Fatalf("plexTMDBID(%q) = %d", guid, id)
		}
	})
}

func FuzzParseSubnets(f *testing.F) {
	for _, s := range []string{"192.168.1.0/24", "10.0.0.0/8", "192.168.1.5", "::1", "192.168.1.0/33", "8.8.8.0/24", "100.64.0.0/24", "", "  ", "1.2.3", "192.168.1.0/24,10.0.0.0/24"} {
		f.Add(s, s)
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		d := &Discoverer{}
		nets, err := d.ParseSubnets([]string{a, b})
		if err != nil {
			return
		}
		for _, n := range nets {
			ones, _ := n.Mask.Size()
			if ones < 24 || !PrivateIPv4(n.IP) || !PrivateIPv4(lastIP(n)) {
				t.Fatalf("network %v accepted", n)
			}
			if got := hosts(n); len(got) > 256 {
				t.Fatalf("%v has %d hosts", n, len(got))
			}
		}
		if len(nets) > MaxSubnets {
			t.Fatalf("%d networks", len(nets))
		}
	})
}

// FuzzServerAnswer: a media server reply, whatever it holds and however it is
// labelled, decodes or is refused without a panic.
func FuzzServerAnswer(f *testing.F) {
	f.Add([]byte(`<MediaContainer machineIdentifier="abc" version="1"/>`), "text/xml")
	f.Add([]byte(`{"Id":"x","Version":"10","ServerName":"n"}`), "application/json")
	f.Add([]byte(strings.Repeat("<a>", 100000)), "application/xml")
	f.Add([]byte(strings.Repeat("[", 100000)), "application/json")
	f.Add([]byte(`<!DOCTYPE x [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;&a;&a;&a;">]><MediaContainer machineIdentifier="&b;"/>`), "text/xml")
	var body atomic.Value
	var ctype atomic.Value
	body.Store([]byte(nil))
	ctype.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ctype.Load().(string))
		w.Write(body.Load().([]byte))
	}))
	f.Cleanup(srv.Close)
	client := &http.Client{Timeout: 5 * time.Second}
	f.Fuzz(func(t *testing.T, data []byte, ct string) {
		if strings.ContainsAny(ct, "\r\n") {
			return
		}
		body.Store(data)
		ctype.Store(ct)
		start := time.Now()
		for _, kind := range []Kind{KindPlex, KindJellyfin} {
			var doc plexDoc
			var info embyPublicInfo
			_ = getInto(context.Background(), client, kind, srv.URL, "/identity", &doc)
			_ = getInto(context.Background(), client, kind, srv.URL, "/System/Info/Public", &info)
			_ = doc.container().items()
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("took %v", time.Since(start))
		}
	})
}

func TestProbeAnswerIsLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Id":"x","Version":"1","pad":"`))
		w.Write([]byte(strings.Repeat("a", 4<<20)))
		w.Write([]byte(`"}`))
	}))
	defer srv.Close()
	var info embyPublicInfo
	err := getInto(context.Background(), srv.Client(), KindJellyfin, srv.URL, "/System/Info/Public", &info)
	if err == nil {
		t.Fatalf("a 4 MB answer to a probe was read in full: %+v", info)
	}
}

func TestNormalizeURLDropsAnEmptyQuery(t *testing.T) {
	for in, want := range map[string]string{
		"http://192.168.1.10:32400?":        "http://192.168.1.10:32400",
		"192.168.1.10:8096/?#":              "http://192.168.1.10:8096",
		"https://host/emby/?api_key=x#frag": "https://host/emby",
	} {
		if got, err := NormalizeURL(in); err != nil || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
