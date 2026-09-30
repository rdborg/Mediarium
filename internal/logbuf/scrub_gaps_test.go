package logbuf

import (
	"bytes"
	"strings"
	"testing"
)

// Realistic secrets, as they show up in logs and error messages of the
// clients Mediarium talks to. Each must be gone from the result.
func TestScrubRealisticSecrets(t *testing.T) {
	tests := []struct {
		name string
		in   string
		gone []string
		kept []string
	}{
		{"prefixed api key names", "config tmdb_api_key=TMDBSECRET99 and x_api_key=XKEYSECRET1 newznab_apikey=NZBSECRET77", []string{"TMDBSECRET99", "XKEYSECRET1", "NZBSECRET77"}, []string{"config"}},
		{"prefixed token name", "plex_token=PLEXTOK123456 refused", []string{"PLEXTOK123456"}, []string{"refused"}},
		{"tracker passkeys", "announce failed passkey=PASSKEYVALUE1 rsskey=RSSKEYVALUE2 authkey=AUTHKEYVALUE3 torrent_pass=TPASSVALUE4", []string{"PASSKEYVALUE1", "RSSKEYVALUE2", "AUTHKEYVALUE3", "TPASSVALUE4"}, []string{"announce failed"}},
		{"wireguard keys", "PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=\nPresharedKey: FpCyhws9cxwWoV4xELtfJvjJN+zQVRPISllRWgeopVE=", []string{"yAnz5TF", "FpCyhws9"}, nil},
		{"telegram bot url", `Post "https://api.telegram.org/bot123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw/sendMessage": timeout`, []string{"AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw", "123456789:"}, []string{"api.telegram.org", "timeout"}},
		{"discord webhook", "post https://discord.com/api/webhooks/1234567890/abcDEF_ghi-JKL failed", []string{"abcDEF_ghi-JKL", "1234567890"}, []string{"discord.com", "failed"}},
		{"slack webhook", "post https://hooks.slack.com/services/T000/B000/XXXXsecretXXXX failed", []string{"XXXXsecretXXXX", "T000"}, []string{"hooks.slack.com"}},
		{"generic webhook id", "post http://ha.local:8123/api/webhook/my-secret-hook-id failed", []string{"my-secret-hook-id"}, []string{"ha.local"}},
		{"magnet with a tracker passkey", "add magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Movie&tr=https%3A%2F%2Ftracker.example%2Fannounce%3Fpasskey%3DABCDEF123456 failed", []string{"ABCDEF123456", "tracker.example"}, []string{"failed"}},
		{"tracker url with the key in the path", "announce https://t.example/abc123passkeyXYZ0123456789abcd/announce failed", []string{"abc123passkeyXYZ0123456789abcd"}, []string{"t.example"}},
		{"second cookie", "Cookie: uid=1; pass=abc123XYZ", []string{"abc123XYZ"}, nil},
		{"set-cookie", "Set-Cookie: session=SESSVALUE99; Path=/; HttpOnly", []string{"SESSVALUE99"}, nil},
		{"digest authorization", `Authorization: Digest username="bob", realm="x", response="deadbeef00cafe"`, []string{"deadbeef00cafe", "bob"}, nil},
		{"proxy authorization", "Proxy-Authorization: Basic Zm9vOmJhcg==", []string{"Zm9vOmJhcg"}, nil},
		{"password with spaces in json", `{"user":"ryan","password":"correct horse battery staple"}`, []string{"horse", "battery", "staple"}, []string{"ryan"}},
		{"password with a semicolon or comma", `{"password":"abc;def"} {"password":"a,b,c"}`, []string{"def", "b,c"}, nil},
		{"password with an escaped quote", `{"password":"ab\"cdefgh"}`, []string{"cdefgh"}, nil},
		{"single-quoted password", `password='my secret pass'`, []string{"secret pass"}, nil},
		{"login in a non-http address", "using proxy socks5://user:PASSWORD1@proxy.local:1080", []string{"PASSWORD1", "user:"}, []string{"proxy.local"}},
		{"login whose password has a slash", "GET https://user:pa/ss@host.example/x", []string{"pa/ss"}, []string{"host.example"}},
		{"mixed case names", "ApiKey=CaseSecret123 API_KEY=CaseSecret456", []string{"CaseSecret123", "CaseSecret456"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Scrub(tt.in)
			for _, s := range tt.gone {
				if strings.Contains(got, s) {
					t.Errorf("%q\nstill contains %q", got, s)
				}
			}
			for _, s := range tt.kept {
				if !strings.Contains(got, s) {
					t.Errorf("%q\nlost %q", got, s)
				}
			}
		})
	}
}

// Ordinary log lines stay readable.
func TestScrubLeavesOrdinaryLinesAlone(t *testing.T) {
	for _, line := range []string{
		"2026/09/30 11:00:00 Mediarium listening on :8264 (db: /config/app.db)",
		`update: could not check for a new version err="GitHub did not give a usable answer: status 404"`,
		"downloads: Movie.2026.1080p.WEB-DL.x264-GROUP.nzb finished in 3m10s",
		"library: imported /data/Movies/Movie (2026)/Movie (2026).mkv",
		"loaded the encryption key from /config/secret.key",
	} {
		if got := Scrub(line); got != line {
			t.Errorf("changed an ordinary line:\n in: %s\nout: %s", line, got)
		}
	}
}

// Scrubbing twice gives the same result, so a line that passes two scrubbers
// (the stderr writer and the buffer) is not garbled.
func TestScrubIsIdempotent(t *testing.T) {
	in := `password=hunter2 https://user:pw@host/x?apikey=SECRET1 Cookie: a=1; b=2 magnet:?xt=urn:btih:abc&tr=https://t.example/announce?passkey=XYZ`
	once := Scrub(in)
	if twice := Scrub(once); twice != once {
		t.Errorf("not idempotent:\n once: %s\ntwice: %s", once, twice)
	}
}

// What is written to stderr is scrubbed line by line, and the bytes reported
// written are the bytes given.
func TestScrubWriter(t *testing.T) {
	var out bytes.Buffer
	w := ScrubWriter{W: &out}
	in := "first line password=hunter2\nsecond line token=abcdef123456\nno newline at the end apikey=LASTSECRET"
	n, err := w.Write([]byte(in))
	if err != nil || n != len(in) {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(in))
	}
	got := out.String()
	for _, s := range []string{"hunter2", "abcdef123456", "LASTSECRET"} {
		if strings.Contains(got, s) {
			t.Errorf("stderr still shows %q: %q", s, got)
		}
	}
	if strings.Count(got, "\n") != 2 || !strings.Contains(got, "first line") || !strings.Contains(got, "second line") {
		t.Errorf("line structure changed: %q", got)
	}
}
