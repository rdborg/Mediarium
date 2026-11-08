package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		extra []string
		want  string // "" means refused
	}{
		{"channel handle gets /videos", "https://www.youtube.com/@SomeChannel", nil, "https://www.youtube.com/@SomeChannel/videos"},
		{"channel handle with tab kept", "https://www.youtube.com/@SomeChannel/videos", nil, "https://www.youtube.com/@SomeChannel/videos"},
		{"channel id", "https://youtube.com/channel/UCabc123/", nil, "https://youtube.com/channel/UCabc123/videos"},
		{"playlist", "https://www.youtube.com/playlist?list=PL123", nil, "https://www.youtube.com/playlist?list=PL123"},
		{"music host", "https://music.youtube.com/playlist?list=OLAK5", nil, "https://music.youtube.com/playlist?list=OLAK5"},
		{"short host", "https://youtu.be/abcdefghijk", nil, "https://youtu.be/abcdefghijk"},
		{"upper case host", "https://WWW.YOUTUBE.COM/playlist?list=PL1", nil, "https://www.youtube.com/playlist?list=PL1"},
		{"fragment dropped", "https://www.youtube.com/playlist?list=PL1#x", nil, "https://www.youtube.com/playlist?list=PL1"},
		{"port 443 is fine", "https://www.youtube.com:443/playlist?list=PL1", nil, "https://www.youtube.com/playlist?list=PL1"},
		{"http refused", "http://www.youtube.com/playlist?list=PL1", nil, ""},
		{"file refused", "file:///etc/passwd", nil, ""},
		{"option as address", "--exec=touch /tmp/x", nil, ""},
		{"dash address", "-o", nil, ""},
		{"no scheme", "www.youtube.com/playlist?list=PL1", nil, ""},
		{"other host refused", "https://vimeo.com/channels/staffpicks", nil, ""},
		{"look-alike host refused", "https://www.youtube.com.evil.example/x", nil, ""},
		{"suffix trick refused", "https://notyoutube.com/x", nil, ""},
		{"user info refused", "https://user:pw@www.youtube.com/@x", nil, ""},
		{"other port refused", "https://www.youtube.com:8443/@x", nil, ""},
		{"space refused", "https://www.youtube.com/playlist?list=PL1 --exec", nil, ""},
		{"newline refused", "https://www.youtube.com/playlist?list=PL1\n--exec", nil, ""},
		{"empty refused", "", nil, ""},
		{"extra host allowed", "https://vimeo.com/channels/staffpicks", []string{"vimeo.com"}, "https://vimeo.com/channels/staffpicks"},
		{"extra host does not allow its sub-domains", "https://evil.vimeo.com/x", []string{"vimeo.com"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateURL(tc.in, tc.extra)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("ValidateURL(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ValidateURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}
}

func TestCleanHost(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"vimeo.com", "vimeo.com", true},
		{"  Vimeo.COM ", "vimeo.com", true},
		{"player.vimeo.com", "player.vimeo.com", true},
		{"localhost", "", false},
		{"nas.local", "", false},
		{"192.168.1.5", "", false},
		{"10.0.0.1:8080", "", false},
		{"https://vimeo.com", "", false},
		{"vimeo.com/path", "", false},
		{"vimeo", "", false},
		{"-bad.com", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		got, err := CleanHost(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("CleanHost(%q) = %q, %v; want %q ok=%v", tc.in, got, err, tc.want, tc.ok)
		}
	}
}

// The address is always the last argument, straight after "--", whatever it
// holds; no argument is built from a title.
func TestArgsKeepTheAddressAfterDoubleDash(t *testing.T) {
	hostile := "https://www.youtube.com/watch?v=abc; rm -rf / --exec=touch$(id)"
	for name, args := range map[string][]string{
		"list":  ListArgs(hostile),
		"video": DownloadArgs(hostile, FormatVideo),
		"mp3":   DownloadArgs(hostile, FormatMP3),
		"m4a":   DownloadArgs(hostile, FormatM4A),
	} {
		n := len(args)
		if n < 3 || args[n-1] != hostile || args[n-2] != "--" {
			t.Errorf("%s: the address is not after --: %q", name, args)
		}
		for _, a := range args[:n-2] {
			if strings.Contains(a, "rm -rf") || strings.Contains(a, "youtube.com") {
				t.Errorf("%s: the address leaked into an option: %q", name, a)
			}
		}
		if args[0] != "--ignore-config" {
			t.Errorf("%s: config files are not ignored", name)
		}
	}
	if a := DownloadArgs("https://www.youtube.com/watch?v=x", FormatVideo); !contains(a, "--no-playlist") || !contains(a, "%(id)s.%(ext)s") {
		t.Errorf("a download names its file by id only and takes no playlist: %q", a)
	}
	if a := DownloadArgs("https://www.youtube.com/watch?v=x", FormatMP3); !contains(a, "-x") || !contains(a, "mp3") {
		t.Errorf("mp3 keeps only the sound: %q", a)
	}
	if a := ListArgs("https://www.youtube.com/@x/videos"); !contains(a, "--flat-playlist") || !contains(a, "--dump-json") {
		t.Errorf("the list is flat json: %q", a)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestParseListing(t *testing.T) {
	in := strings.Join([]string{
		`{"id":"aaaaaaaaaaa","title":"First; rm -rf /","channel":"Chan","duration":61.5,"url":"https://www.youtube.com/watch?v=aaaaaaaaaaa"}`,
		`WARNING: something said on stdout`,
		`{"id":"bbbbbbbbbbb","title":"Second","uploader":"Uploader Name"}`,
		`{"id":"aaaaaaaaaaa","title":"Twice"}`,
		`{"id":"../../etc","title":"Path trick"}`,
		`{"id":"a b","title":"Space"}`,
		`{"id":"","title":"No id"}`,
		`{"id":"UCtabtabtab","_type":"url","ie_key":"YoutubeTab","title":"Videos"}`,
		`{"id":"PLplaylist","_type":"playlist","title":"A playlist"}`,
		`not json {`,
		`{"id":"ccccccccccc"`,
		``,
	}, "\n")
	got, err := ParseListing(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d videos, want 2: %+v", len(got), got)
	}
	if got[0].ID != "aaaaaaaaaaa" || got[0].Channel != "Chan" || got[0].Duration != 61 || got[0].Title != "First; rm -rf /" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].ID != "bbbbbbbbbbb" || got[1].Channel != "Uploader Name" {
		t.Errorf("second = %+v", got[1])
	}
}

func TestVideoAddress(t *testing.T) {
	tests := []struct {
		name     string
		followed string
		v        Video
		extra    []string
		want     string
	}{
		{"youtube built from the id", "https://www.youtube.com/@x/videos", Video{ID: "abc_DEF-123", URL: "https://evil.example/x"}, nil, "https://www.youtube.com/watch?v=abc_DEF-123"},
		{"youtu.be follows", "https://youtu.be/xyz", Video{ID: "abc"}, nil, "https://www.youtube.com/watch?v=abc"},
		{"other site uses the listed address", "https://vimeo.com/channels/a", Video{ID: "123", URL: "https://vimeo.com/123"}, []string{"vimeo.com"}, "https://vimeo.com/123"},
		{"other site, listed address elsewhere", "https://vimeo.com/channels/a", Video{ID: "123", URL: "https://evil.example/123"}, []string{"vimeo.com"}, ""},
		{"other site without an address", "https://vimeo.com/channels/a", Video{ID: "123"}, []string{"vimeo.com"}, ""},
		{"bad id", "https://www.youtube.com/@x", Video{ID: "a;b"}, nil, ""},
	}
	for _, tc := range tests {
		got, err := VideoAddress(tc.followed, tc.v, tc.extra)
		if (tc.want == "") != (err != nil) || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestSafeName(t *testing.T) {
	tests := []struct {
		in, want string
		max      int
	}{
		{"Plain title", "Plain title", 100},
		{"../../etc/passwd", "etc passwd", 100},
		{`a/b\c:d*e?f"g<h>i|j`, "a b c d e f g h i j", 100},
		{"...hidden", "hidden", 100},
		{"  spaced   out  ", "spaced out", 100},
		{"", "fallback", 100},
		{"///", "fallback", 100},
		{"tab\tand\nnewline", "tab and newline", 100},
		{"ääääää", "äää", 6},
		{"ends with dot.", "ends with dot", 100},
	}
	for _, tc := range tests {
		got := SafeName(tc.in, "fallback", tc.max)
		if got != tc.want {
			t.Errorf("SafeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if strings.ContainsAny(got, `/\`) || strings.HasPrefix(got, ".") {
			t.Errorf("SafeName(%q) = %q is not a safe name", tc.in, got)
		}
	}
}

// ---- a fake yt-dlp: a small shell script that prints canned answers

func fakeYtdlp(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake yt-dlp is a shell script")
	}
	p := filepath.Join(t.TempDir(), "yt-dlp")
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

const fakeBody = `
case "$*" in
  *--version*) echo "2026.10.01"; exit 0 ;;
esac
# the address is the last argument
for a; do last="$a"; done
case "$*" in
  *--dump-json*)
    echo '{"id":"vid00000001","title":"One","channel":"The Channel"}'
    echo '{"id":"vid00000002","title":"Two","channel":"The Channel"}'
    exit 0 ;;
esac
case "$last" in
  *fail*) echo "ERROR: [youtube] fail: Video unavailable" >&2; exit 1 ;;
  *slow*) sleep 30; exit 0 ;;
esac
id="${last##*v=}"
case "$*" in
  *"-x"*) printf audio > "$id.mp3" ;;
  *) printf video > "$id.mp4" ;;
esac
echo '{"id":"'"$id"'"}' > "$id.info.json"
`

func TestVersionAndList(t *testing.T) {
	r := Runner{Path: fakeYtdlp(t, fakeBody)}
	v, err := r.Version(context.Background())
	if err != nil || v != "2026.10.01" {
		t.Fatalf("Version = %q, %v", v, err)
	}
	vids, _, err := r.List(context.Background(), "https://www.youtube.com/@x/videos")
	if err != nil || len(vids) != 2 || vids[0].Channel != "The Channel" {
		t.Fatalf("List = %+v, %v", vids, err)
	}
}

func TestVersionRefusesWhatIsNotYtdlp(t *testing.T) {
	tests := []struct{ name, body string }{
		{"prints something else", `echo "hello world, I am not a version"; exit 0`},
		{"prints nothing", `exit 0`},
		{"fails", `echo "boom" >&2; exit 3`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := (Runner{Path: fakeYtdlp(t, tc.body)}).Version(context.Background()); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	for _, p := range []string{"", "relative/yt-dlp", "/no/such/yt-dlp", t.TempDir()} {
		if _, err := (Runner{Path: p}).Version(context.Background()); err == nil {
			t.Errorf("path %q should be refused", p)
		}
	}
}

func TestDownload(t *testing.T) {
	r := Runner{Path: fakeYtdlp(t, fakeBody)}
	for _, tc := range []struct{ format, ext string }{{FormatVideo, ".mp4"}, {FormatMP3, ".mp3"}} {
		dir := t.TempDir()
		res, _, err := r.Download(context.Background(), "https://www.youtube.com/watch?v=abcdefghijk", tc.format, dir)
		if err != nil {
			t.Fatalf("%s: %v", tc.format, err)
		}
		if filepath.Base(res.Media) != "abcdefghijk"+tc.ext || filepath.Base(res.Info) != "abcdefghijk.info.json" {
			t.Errorf("%s: got %+v", tc.format, res)
		}
	}
	_, _, err := r.Download(context.Background(), "https://www.youtube.com/watch?v=fail", FormatVideo, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "Video unavailable") {
		t.Errorf("a failure should carry yt-dlp's words, got %v", err)
	}
}

func TestDownloadTimeoutAndCancel(t *testing.T) {
	r := Runner{Path: fakeYtdlp(t, fakeBody), Timeout: 300 * time.Millisecond}
	start := time.Now()
	_, _, err := r.Download(context.Background(), "https://www.youtube.com/watch?v=slow", FormatVideo, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "too long") || time.Since(start) > 10*time.Second {
		t.Fatalf("a slow download should be stopped: %v after %v", err, time.Since(start))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r.Timeout = time.Minute
	if _, _, err := r.Download(ctx, "https://www.youtube.com/watch?v=slow", FormatVideo, t.TempDir()); err == nil {
		t.Fatal("a cancelled download should return an error")
	}
}

func TestStderrIsBounded(t *testing.T) {
	r := Runner{Path: fakeYtdlp(t, `i=0; while [ $i -lt 3000 ]; do echo "ERROR: line $i padding padding padding padding padding padding" >&2; i=$((i+1)); done; exit 1`)}
	_, stderr, err := r.Download(context.Background(), "https://www.youtube.com/watch?v=x", FormatVideo, t.TempDir())
	if err == nil || len(stderr) > 4096 {
		t.Fatalf("stderr kept %d bytes, err %v", len(stderr), err)
	}
	if len(err.Error()) > 400 {
		t.Errorf("the error is too long: %d", len(err.Error()))
	}
}
