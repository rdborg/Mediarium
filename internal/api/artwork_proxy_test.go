package api_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/imgproxy"
)

// Artwork goes through the server: only for signed-in accounts, only from the
// known picture host, resized, and kept so the host is asked once.
func TestArtworkProxy(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 600, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 600; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 80, 255})
		}
	}
	var src bytes.Buffer
	if err := jpeg.Encode(&src, img, nil); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/t/p/w342/abcdef123.jpg" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(src.Bytes())
	}))
	defer host.Close()

	server, base, client := loginNewServer(t)
	p := imgproxy.New(filepath.Join(t.TempDir(), "img"))
	p.Sources = map[string]string{"tmdb": host.URL + "/t/p"}
	p.Client = http.DefaultClient
	p.NoAVIF = true
	server.Artwork = p

	get := func(c *http.Client, path string) (*http.Response, []byte) {
		resp, err := c.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, body
	}

	if resp, _ := get(&http.Client{}, "/api/img/tmdb/w342/abcdef123.jpg"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("signed out: status %d, want 401", resp.StatusCode)
	}
	resp, body := get(client, "/api/img/tmdb/w342/abcdef123.jpg")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("status %d type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(body)); err != nil || cfg.Width != 342 {
		t.Errorf("width %d, err %v", cfg.Width, err)
	}
	if resp.Header.Get("Vary") != "Accept" || resp.Header.Get("ETag") == "" {
		t.Errorf("Vary %q ETag %q", resp.Header.Get("Vary"), resp.Header.Get("ETag"))
	}
	get(client, "/api/img/tmdb/w342/abcdef123.jpg")
	if hits.Load() != 1 {
		t.Errorf("the host was asked %d times, want 1", hits.Load())
	}
	for _, path := range []string{"/api/img/other/w342/abcdef123.jpg", "/api/img/tmdb/w1/abcdef123.jpg", "/api/img/tmdb/w342/..%2Fx.jpg", "/api/img/tmdb/w342/abcdef123.gif"} {
		if resp, _ := get(client, path); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, resp.StatusCode)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("a refused request reached the host (%d asks)", hits.Load())
	}
}

// Loading artwork through the server is on until an administrator switches it
// off, and every account is told which it is.
func TestArtworkProxySwitch(t *testing.T) {
	_, base, client := loginNewServer(t)
	if m := getJSON[map[string]any](t, client, base+"/api/modules"); m["artworkProxy"] != true {
		t.Fatalf("modules say artworkProxy = %v, want true by default", m["artworkProxy"])
	}
	if s := getJSON[map[string]any](t, client, base+"/api/settings"); s["artworkProxy"] != true {
		t.Fatalf("settings say artworkProxy = %v, want true by default", s["artworkProxy"])
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"artworkProxy": false}, http.StatusOK)
	if m := getJSON[map[string]any](t, client, base+"/api/modules"); m["artworkProxy"] != false {
		t.Errorf("after switching off, modules say %v", m["artworkProxy"])
	}
	if s := getJSON[map[string]any](t, client, base+"/api/settings"); s["artworkProxy"] != false {
		t.Errorf("after switching off, settings say %v", s["artworkProxy"])
	}
}
