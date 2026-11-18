package debrid

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// What each service says when it refuses, throttles or has a bad moment, and
// that none of it looks like a wrong API key unless it is one.
func TestServiceErrorsInPlainWords(t *testing.T) {
	cases := []struct {
		name    string
		service string
		status  int
		body    string
		auth    bool
		want    string
	}{
		{"real-debrid throttled", RealDebrid, 429, `{}`, false, "too many requests"},
		{"real-debrid throttled with its own code", RealDebrid, 429, `{"error":"x","error_code":34}`, false, "too many requests"},
		{"real-debrid outage page", RealDebrid, 520, `<html>error</html>`, false, "trouble at the moment"},
		{"real-debrid bad token", RealDebrid, 401, `{"error":"bad_token","error_code":8}`, true, ""},
		{"real-debrid not premium", RealDebrid, 403, `{"error":"permission_denied","error_code":9}`, false, "premium"},
		{"torbox plan restriction is not a bad key", TorBox, 403, `{"success":false,"error":"PLAN_RESTRICTED_FEATURE","detail":"Your plan does not include this."}`, false, "does not include"},
		{"torbox bad key", TorBox, 403, `{"success":false,"error":"BAD_TOKEN","detail":"Invalid token"}`, true, ""},
		{"torbox no answer body", TorBox, 401, ``, true, ""},
		{"torbox throttled", TorBox, 429, ``, false, "too many requests"},
		{"premiumize throttled", Premiumize, 429, ``, false, "too many requests"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			svc, err := New(tc.service, "secret-key-123", srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = WithBaseURL(svc, srv.URL).Account(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if errors.Is(err, ErrAuth) != tc.auth {
				t.Fatalf("auth error = %v, want %v (%v)", errors.Is(err, ErrAuth), tc.auth, err)
			}
			if tc.want != "" && !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "secret-key-123") {
				t.Fatalf("the error leaks the key: %v", err)
			}
		})
	}
}

// The TorBox download request carries the key in its address (TorBox asks for
// that), so a network failure must not put the address in the message.
func TestTorBoxNetworkErrorLeavesKeyOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/torrents/mylist" {
			_, _ = w.Write([]byte(`{"success":true,"data":{"files":[{"id":1,"name":"a.mkv","size":5}]}}`))
			return
		}
		// drop the connection without answering
		if hj, ok := w.(http.Hijacker); ok {
			c, _, _ := hj.Hijack()
			c.Close()
		}
	}))
	defer srv.Close()
	svc, _ := New(TorBox, "secret-key-123", srv.Client())
	_, err := WithBaseURL(svc, srv.URL).Files(context.Background(), "7")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "secret-key-123") {
		t.Fatalf("the error leaks the key: %v", err)
	}
}

// Real-Debrid names a link by the bare file name. Two files of a pack that
// share one name (a subtitle in every episode's folder) must keep their own
// folders, or the second would overwrite the first.
func TestRealDebridKeepsFolders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /torrents/info/PACK", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"downloaded","progress":100,"files":[
			{"id":1,"path":"/Show.S01/E01/English.srt","bytes":5,"selected":1},
			{"id":2,"path":"/Show.S01/E02/English.srt","bytes":6,"selected":1},
			{"id":3,"path":"/Show.S01/Sample/x.mkv","bytes":7,"selected":0}],
			"links":["L1","L2"]}`))
	})
	mux.HandleFunc("POST /unrestrict/link", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.Form.Get("link") {
		case "L1":
			_, _ = w.Write([]byte(`{"filename":"English.srt","filesize":5,"download":"https://cdn.example/1"}`))
		case "L2":
			_, _ = w.Write([]byte(`{"filename":"English.srt","filesize":6,"download":"https://cdn.example/2"}`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	svc := WithBaseURL(&realDebrid{key: "k", http: srv.Client()}, srv.URL)
	files, err := svc.Files(context.Background(), "PACK")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "Show.S01/E01/English.srt" || files[1].Name != "Show.S01/E02/English.srt" {
		t.Fatalf("got %+v", files)
	}
}
