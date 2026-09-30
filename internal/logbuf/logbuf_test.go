package logbuf

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestScrubTakesSecretsOut(t *testing.T) {
	tests := []struct {
		name string
		in   string
		gone []string // must not appear in the result
		kept []string // must still appear
	}{
		{
			name: "api key in a url",
			in:   `2026/09/29 18:00:00 indexer search failed: Get "https://indexer.example/api?t=search&apikey=abc123SECRET&q=heat": timeout`,
			gone: []string{"abc123SECRET"},
			kept: []string{"indexer.example", "timeout"},
		},
		{
			name: "login in a url",
			in:   "connecting to https://user:hunter2@nas.local:5000/webapi",
			gone: []string{"hunter2", "user:"},
			kept: []string{"nas.local"},
		},
		{
			name: "key in the path",
			in:   "GET https://api.example.com/api/0123456789abcdef0123456789ab/movies failed",
			gone: []string{"0123456789abcdef0123456789ab"},
			kept: []string{"api.example.com"},
		},
		{
			name: "password pair",
			in:   "login failed username=ryan password=correct-horse-battery",
			gone: []string{"correct-horse-battery"},
			kept: []string{"username=ryan"},
		},
		{
			name: "json token",
			in:   `plex reply {"token":"xyzTOKEN12345","name":"Living Room"}`,
			gone: []string{"xyzTOKEN12345"},
			kept: []string{"Living Room"},
		},
		{
			name: "authorization header",
			in:   "request header Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig",
			gone: []string{"eyJhbGciOiJIUzI1NiJ9"},
		},
		{
			name: "bearer on its own",
			in:   "sent Bearer abcdefghijklmnop to the server",
			gone: []string{"abcdefghijklmnop"},
			kept: []string{"to the server"},
		},
		{
			name: "plex token header",
			in:   "X-Plex-Token=AbCdEf123456 rejected",
			gone: []string{"AbCdEf123456"},
			kept: []string{"rejected"},
		},
		{
			name: "long hex string",
			in:   "session 5f4dcc3b5aa765d61d8327deb882cf995f4dcc3b5aa765d6 expired",
			gone: []string{"5f4dcc3b5aa765d61d8327deb882cf99"},
			kept: []string{"expired"},
		},
		{
			name: "cookie",
			in:   "cookie: mediarium_session=deadbeefdeadbeef",
			gone: []string{"deadbeefdeadbeef"},
		},
		{
			name: "plain sentence untouched",
			in:   `database: slow call took=3.2s sql="UPDATE download_queue SET progress_pct = ?"`,
			kept: []string{"slow call took=3.2s", "UPDATE download_queue SET progress_pct"},
		},
		{
			name: "url at the end of a sentence",
			in:   "see https://example.com/help.",
			kept: []string{"https://example.com/help", "."},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Scrub(tc.in)
			for _, s := range tc.gone {
				if strings.Contains(got, s) {
					t.Errorf("%q still contains %q", got, s)
				}
			}
			for _, s := range tc.kept {
				if !strings.Contains(got, s) {
					t.Errorf("%q lost %q", got, s)
				}
			}
		})
	}
}

func TestBufferKeepsTheLastLinesOldestFirst(t *testing.T) {
	b := New(3)
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(b, "line %d\n", i)
	}
	got := strings.Join(b.Lines(), "|")
	if got != "line 3|line 4|line 5" {
		t.Fatalf("lines = %q", got)
	}
}

func TestBufferJoinsLinesSplitAcrossWrites(t *testing.T) {
	b := New(10)
	b.Write([]byte("first half, "))
	b.Write([]byte("second half\nnext line\n\n   \n"))
	got := b.Lines()
	if len(got) != 2 || got[0] != "first half, second half" || got[1] != "next line" {
		t.Fatalf("lines = %q", got)
	}
}

func TestBufferScrubsBeforeKeeping(t *testing.T) {
	b := New(5)
	fmt.Fprintln(b, "sign in failed password=hunter2")
	for _, l := range b.Lines() {
		if strings.Contains(l, "hunter2") {
			t.Fatalf("kept a secret: %q", l)
		}
	}
}

func TestBufferIsSafeForConcurrentWriters(t *testing.T) {
	b := New(50)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				fmt.Fprintf(b, "worker line %d\n", i)
				_ = b.Lines()
			}
		}()
	}
	wg.Wait()
	if n := len(b.Lines()); n != 50 {
		t.Fatalf("kept %d lines, want 50", n)
	}
}
