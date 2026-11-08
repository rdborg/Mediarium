package imgproxy

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gen2brain/avif"
)

// testJPEG is a gradient picture of the given size.
func testJPEG(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: uint8((x + y) % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// upstream is a stand-in for the picture host: it serves one picture for any
// size and counts the requests.
type upstream struct {
	*httptest.Server
	hits   atomic.Int64
	status int
	body   []byte
	ctype  string
	delay  time.Duration
	paths  []string
	mu     sync.Mutex
}

func newUpstream(t *testing.T, body []byte) *upstream {
	u := &upstream{status: 200, body: body, ctype: "image/jpeg"}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		u.mu.Lock()
		u.paths = append(u.paths, r.URL.Path)
		u.mu.Unlock()
		if u.delay > 0 {
			time.Sleep(u.delay)
		}
		if u.status != 200 {
			if u.status == 302 {
				http.Redirect(w, r, "http://127.0.0.1:1/x.jpg", 302)
				return
			}
			http.Error(w, "no", u.status)
			return
		}
		w.Header().Set("Content-Type", u.ctype)
		_, _ = w.Write(u.body)
	}))
	t.Cleanup(u.Close)
	return u
}

func newProxy(t *testing.T, u *upstream) *Proxy {
	p := New(filepath.Join(t.TempDir(), "img"))
	p.Sources = map[string]string{"tmdb": u.URL + "/t/p"}
	p.Client = u.Client()
	p.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	t.Cleanup(p.Wait)
	return p
}

func get(p *Proxy, kind, size, file string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/api/img/"+kind+"/"+size+"/"+file, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	p.Handle(w, r, kind, size, file)
	return w
}

func width(t *testing.T, data []byte) int {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a picture: %v", err)
	}
	return cfg.Width
}

func TestNegotiate(t *testing.T) {
	tests := []struct{ accept, want string }{
		{"", FormatJPEG},
		{"*/*", FormatJPEG},
		{"image/jpeg,image/png", FormatJPEG},
		{"image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8", FormatAVIF},
		{"image/webp,*/*", FormatWebP},
		{"image/webp;q=0.9, image/avif;q=0.1", FormatAVIF},
		{"image/avif;q=0, image/webp", FormatWebP},
		{"IMAGE/AVIF", FormatAVIF},
		{"image/avif;q=0,image/webp;q=0", FormatJPEG},
		{"text/html, garbage;;;q=x", FormatJPEG},
	}
	for _, tt := range tests {
		if got := Negotiate(tt.accept); got != tt.want {
			t.Errorf("Negotiate(%q) = %q, want %q", tt.accept, got, tt.want)
		}
	}
}

func TestRefusals(t *testing.T) {
	u := newUpstream(t, testJPEG(t, 100, 150))
	p := newProxy(t, u)
	tests := []struct{ name, kind, size, file string }{
		{"unknown kind", "evil", "w342", "abcdef.jpg"},
		{"a host is not a kind", "http://127.0.0.1", "w342", "abcdef.jpg"},
		{"unknown size", "tmdb", "w999", "abcdef.jpg"},
		{"size with path", "tmdb", "../w342", "abcdef.jpg"},
		{"no size", "tmdb", "", "abcdef.jpg"},
		{"path in file", "tmdb", "w342", "../../etc/passwd.jpg"},
		{"slash in file", "tmdb", "w342", "a/b.jpg"},
		{"encoded slash", "tmdb", "w342", "a%2fb.jpg"},
		{"query in file", "tmdb", "w342", "abcdef.jpg?x=1"},
		{"full address", "tmdb", "w342", "http://127.0.0.1/x.jpg"},
		{"wrong ending", "tmdb", "w342", "abcdef.gif"},
		{"no ending", "tmdb", "w342", "abcdef"},
		{"hidden file", "tmdb", "w342", ".jpg"},
		{"double dots", "tmdb", "w342", "ab..cd.jpg"},
		{"too long", "tmdb", "w342", string(bytes.Repeat([]byte("a"), 80)) + ".jpg"},
	}
	for _, tt := range tests {
		w := get(p, tt.kind, tt.size, tt.file, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", tt.name, w.Code)
		}
	}
	if n := u.hits.Load(); n != 0 {
		t.Errorf("the upstream was asked %d times for refused requests", n)
	}
}

// The real client refuses a private or loopback address, so even a mistaken
// source list can never reach the inside of the network.
func TestDefaultClientRefusesLoopback(t *testing.T) {
	u := newUpstream(t, testJPEG(t, 100, 150))
	p := New(filepath.Join(t.TempDir(), "img"))
	p.Sources = map[string]string{"tmdb": u.URL + "/t/p"}
	w := get(p, "tmdb", "w342", "abcdef.jpg", nil)
	if w.Code != http.StatusBadGateway {
		t.Errorf("status %d, want 502", w.Code)
	}
	if u.hits.Load() != 0 {
		t.Error("the loopback server was reached")
	}
}

func TestDefaultSourceIsTMDBOnly(t *testing.T) {
	if len(DefaultSources) != 1 || DefaultSources["tmdb"] != "https://image.tmdb.org/t/p" {
		t.Errorf("unexpected sources %v", DefaultSources)
	}
}

func TestSizeSelection(t *testing.T) {
	big := testJPEG(t, 1000, 1500)
	small := testJPEG(t, 120, 180)
	tests := []struct {
		name      string
		src       []byte
		size      string
		wantWidth int
		sameBytes bool
	}{
		{"w92", big, "w92", 92, false},
		{"w154", big, "w154", 154, false},
		{"w185", big, "w185", 185, false},
		{"w342", big, "w342", 342, false},
		{"w500", big, "w500", 500, false},
		{"w780", big, "w780", 780, false},
		{"w1280 is not larger than the source", big, "w1280", 1000, true},
		{"original keeps the size", big, "original", 1000, true},
		{"never enlarged", small, "w342", 120, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := newUpstream(t, tt.src)
			p := newProxy(t, u)
			w := get(p, "tmdb", tt.size, "abcdef.jpg", nil)
			if w.Code != 200 {
				t.Fatalf("status %d", w.Code)
			}
			if got := width(t, w.Body.Bytes()); got != tt.wantWidth {
				t.Errorf("width %d, want %d", got, tt.wantWidth)
			}
			if tt.sameBytes != bytes.Equal(w.Body.Bytes(), tt.src) {
				t.Errorf("sameBytes = %v, want %v", !tt.sameBytes, tt.sameBytes)
			}
			if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
				t.Errorf("content type %q", ct)
			}
			// What is asked of the host is the same size name.
			if len(u.paths) != 1 || u.paths[0] != "/t/p/"+tt.size+"/abcdef.jpg" {
				t.Errorf("upstream paths %v", u.paths)
			}
		})
	}
}

func TestCacheHitMissAndNotModified(t *testing.T) {
	u := newUpstream(t, testJPEG(t, 800, 1200))
	p := newProxy(t, u)
	w1 := get(p, "tmdb", "w342", "abcdef.jpg", nil)
	if w1.Code != 200 || u.hits.Load() != 1 {
		t.Fatalf("first: status %d, upstream %d", w1.Code, u.hits.Load())
	}
	h := w1.Header()
	if h.Get("Vary") != "Accept" {
		t.Errorf("Vary %q", h.Get("Vary"))
	}
	if got := h.Get("Cache-Control"); got != "public, max-age=604800, immutable" {
		t.Errorf("Cache-Control %q", got)
	}
	etag := h.Get("ETag")
	if etag == "" || etag[0] != '"' {
		t.Fatalf("ETag %q is not a strong tag", etag)
	}
	w2 := get(p, "tmdb", "w342", "abcdef.jpg", nil)
	if w2.Code != 200 || u.hits.Load() != 1 {
		t.Errorf("second: status %d, upstream %d (want a cache hit)", w2.Code, u.hits.Load())
	}
	if !bytes.Equal(w1.Body.Bytes(), w2.Body.Bytes()) || w2.Header().Get("ETag") != etag {
		t.Error("the cached answer differs")
	}
	w3 := get(p, "tmdb", "w342", "abcdef.jpg", map[string]string{"If-None-Match": etag})
	if w3.Code != http.StatusNotModified || w3.Body.Len() != 0 {
		t.Errorf("conditional: status %d, body %d bytes", w3.Code, w3.Body.Len())
	}
	if w4 := get(p, "tmdb", "w342", "abcdef.jpg", map[string]string{"If-None-Match": `"other"`}); w4.Code != 200 {
		t.Errorf("a different tag got status %d", w4.Code)
	}
	// Another size is another cache entry.
	get(p, "tmdb", "w185", "abcdef.jpg", nil)
	if u.hits.Load() != 2 {
		t.Errorf("upstream %d, want 2 after a second size", u.hits.Load())
	}
}

func TestAVIFIsMadeInTheBackground(t *testing.T) {
	u := newUpstream(t, testJPEG(t, 800, 1200))
	p := newProxy(t, u)
	accept := map[string]string{"Accept": "image/avif,image/webp,*/*"}
	w1 := get(p, "tmdb", "w342", "abcdef.jpg", accept)
	if w1.Code != 200 || w1.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("first answer: %d %s, want the JPEG at once", w1.Code, w1.Header().Get("Content-Type"))
	}
	if cc := w1.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Errorf("the stand-in must be cached shortly, got %q", cc)
	}
	start := time.Now()
	p.Wait()
	t.Logf("AVIF of a 342 wide poster took %v", time.Since(start))
	w2 := get(p, "tmdb", "w342", "abcdef.jpg", accept)
	if w2.Header().Get("Content-Type") != "image/avif" {
		t.Fatalf("second answer is %s, want image/avif", w2.Header().Get("Content-Type"))
	}
	if cc := w2.Header().Get("Cache-Control"); cc != "public, max-age=604800, immutable" {
		t.Errorf("Cache-Control %q", cc)
	}
	img, err := avif.Decode(bytes.NewReader(w2.Body.Bytes()))
	if err != nil {
		t.Fatalf("the AVIF does not decode: %v", err)
	}
	if img.Bounds().Dx() != 342 {
		t.Errorf("AVIF width %d", img.Bounds().Dx())
	}
	if w2.Body.Len() >= w1.Body.Len() {
		t.Errorf("AVIF (%d bytes) is not smaller than the JPEG (%d)", w2.Body.Len(), w1.Body.Len())
	}
	// A browser without AVIF still gets the JPEG, and the answers differ in tag.
	w3 := get(p, "tmdb", "w342", "abcdef.jpg", map[string]string{"Accept": "image/webp,*/*"})
	if w3.Header().Get("Content-Type") != "image/jpeg" || w3.Header().Get("ETag") == w2.Header().Get("ETag") {
		t.Errorf("non-AVIF browser got %s with tag %s", w3.Header().Get("Content-Type"), w3.Header().Get("ETag"))
	}
	if u.hits.Load() != 1 {
		t.Errorf("upstream asked %d times", u.hits.Load())
	}
}

func TestAVIFOffSendsJPEG(t *testing.T) {
	u := newUpstream(t, testJPEG(t, 800, 1200))
	p := newProxy(t, u)
	p.NoAVIF = true
	w := get(p, "tmdb", "w342", "abcdef.jpg", map[string]string{"Accept": "image/avif"})
	p.Wait()
	if w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Cache-Control") != "public, max-age=604800, immutable" {
		t.Errorf("got %s, %s", w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"))
	}
	if _, err := os.Stat(filepath.Join(p.Dir, key("tmdb", "w342", "abcdef.jpg")+".avif")); err == nil {
		t.Error("an AVIF was made although it is off")
	}
}

func TestPNGKeepsItsTransparency(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 600, 200))
	img.Set(10, 10, color.NRGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	u := newUpstream(t, buf.Bytes())
	u.ctype = "image/png"
	p := newProxy(t, u)
	w := get(p, "tmdb", "w185", "logo123.png", nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || width(t, w.Body.Bytes()) != 185 {
		t.Fatalf("status %d type %s", w.Code, w.Header().Get("Content-Type"))
	}
	out, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := out.At(150, 50).RGBA(); a != 0 {
		t.Errorf("transparent part became opaque (alpha %d)", a)
	}
}

func TestFailuresAreRemembered(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   int
		cache  string
	}{
		{"missing", 404, 404, "public, max-age=300"},
		{"host error", 500, 502, "no-store"},
		{"redirect is not followed", 302, 502, "no-store"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := newUpstream(t, nil)
			u.status = tt.status
			p := newProxy(t, u)
			for i := 0; i < 3; i++ {
				w := get(p, "tmdb", "w342", "abcdef.jpg", nil)
				if w.Code != tt.want || w.Header().Get("Cache-Control") != tt.cache {
					t.Fatalf("try %d: status %d cache %q", i, w.Code, w.Header().Get("Cache-Control"))
				}
			}
			if n := u.hits.Load(); n != 1 {
				t.Errorf("upstream asked %d times, want 1 (the failure is remembered)", n)
			}
			// After the memory expires the host is asked again.
			p.mu.Lock()
			for k, f := range p.failed {
				f.until = time.Now().Add(-time.Second)
				p.failed[k] = f
			}
			p.mu.Unlock()
			get(p, "tmdb", "w342", "abcdef.jpg", nil)
			if n := u.hits.Load(); n != 2 {
				t.Errorf("upstream asked %d times after expiry, want 2", n)
			}
		})
	}
}

func TestSourceLimits(t *testing.T) {
	t.Run("not a picture", func(t *testing.T) {
		u := newUpstream(t, []byte("<html>not a picture</html>"))
		p := newProxy(t, u)
		if w := get(p, "tmdb", "w342", "abcdef.jpg", nil); w.Code != http.StatusBadGateway {
			t.Errorf("status %d", w.Code)
		}
	})
	t.Run("too many bytes", func(t *testing.T) {
		u := newUpstream(t, make([]byte, MaxSourceBytes+10))
		p := newProxy(t, u)
		if w := get(p, "tmdb", "w342", "abcdef.jpg", nil); w.Code != http.StatusBadGateway {
			t.Errorf("status %d", w.Code)
		}
	})
	t.Run("too many pixels", func(t *testing.T) {
		var buf bytes.Buffer
		if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 6000, 4200))); err != nil {
			t.Fatal(err)
		}
		u := newUpstream(t, buf.Bytes())
		p := newProxy(t, u)
		if w := get(p, "tmdb", "w342", "abcdef.png", nil); w.Code != http.StatusBadGateway {
			t.Errorf("status %d", w.Code)
		}
	})
}

func TestOneDownloadForManyRequests(t *testing.T) {
	u := newUpstream(t, testJPEG(t, 800, 1200))
	u.delay = 150 * time.Millisecond
	p := newProxy(t, u)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := get(p, "tmdb", "w342", "abcdef.jpg", nil); w.Code != 200 {
				t.Errorf("status %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if n := u.hits.Load(); n != 1 {
		t.Errorf("upstream asked %d times, want 1", n)
	}
}

func TestPruneRemovesLeastRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	p := &Proxy{Dir: dir, MaxCacheBytes: 1000}
	now := time.Now()
	files := []struct {
		name string
		age  time.Duration
	}{
		{"a.jpg", 5 * time.Hour}, {"b.avif", 4 * time.Hour}, {"c.png", 3 * time.Hour}, {"d.jpg", 2 * time.Hour}, {"e.jpg", time.Hour},
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, make([]byte, 300), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-f.age), now.Add(-f.age)); err != nil {
			t.Fatal(err)
		}
	}
	old := filepath.Join(dir, "old.tmp")
	_ = os.WriteFile(old, []byte("x"), 0o644)
	_ = os.Chtimes(old, now.Add(-3*time.Hour), now.Add(-3*time.Hour))
	other := filepath.Join(dir, "notes.txt")
	_ = os.WriteFile(other, make([]byte, 5000), 0o644)

	p.Prune()
	left := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left[e.Name()] = true
	}
	// 1500 bytes over a limit of 1000: down to 900 means the two oldest go.
	for name, want := range map[string]bool{"a.jpg": false, "b.avif": false, "c.png": true, "d.jpg": true, "e.jpg": true, "old.tmp": false, "notes.txt": true} {
		if left[name] != want {
			t.Errorf("%s kept = %v, want %v", name, left[name], want)
		}
	}
}

func TestVisitRefreshesFileTime(t *testing.T) {
	u := newUpstream(t, testJPEG(t, 800, 1200))
	p := newProxy(t, u)
	get(p, "tmdb", "w342", "abcdef.jpg", nil)
	path := filepath.Join(p.Dir, key("tmdb", "w342", "abcdef.jpg")+".jpg")
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(path, old, old)
	get(p, "tmdb", "w342", "abcdef.jpg", nil)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(st.ModTime()) > time.Minute {
		t.Errorf("modified time %v was not refreshed by a visit", st.ModTime())
	}
}

func TestAVIFSpeedOfAPoster(t *testing.T) {
	if testing.Short() {
		t.Skip("measures the encoder")
	}
	src := testJPEG(t, 342, 513)
	start := time.Now()
	out, err := EncodeAVIF(src)
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	t.Logf("first encode (includes loading the encoder) %v, %d bytes (JPEG %d)", took, len(out), len(src))
	start = time.Now()
	if _, err := EncodeAVIF(src); err != nil {
		t.Fatal(err)
	}
	t.Logf("second encode %v", time.Since(start))
	if fmt.Sprint(len(out)) == "0" {
		t.Fatal("empty")
	}
}
