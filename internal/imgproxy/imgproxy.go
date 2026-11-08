// Package imgproxy serves artwork (posters, backdrops, profile pictures) from
// this server instead of letting the browser fetch it from the picture host.
// The browser then never contacts that host (a privacy gain), and each picture
// is sent at the size its slot needs, in the best format the browser reads.
//
// Only pictures on a short list of known hosts can be asked for, and only by
// a size name and a plain file name: nobody can make the server fetch an
// address of their choosing. Every picture is fetched once per size, kept on
// disk (the oldest are removed when the folder grows past its limit) and sent
// with long browser caching and conditional requests.
package imgproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gen2brain/avif"
	xdraw "golang.org/x/image/draw"

	"github.com/rdborg/mediarium/internal/netguard"
)

// Limits. A poster at its largest is a few megabytes and a few million pixels;
// these leave room for that and refuse anything made to hurt.
const (
	// MaxSourceBytes bounds what is downloaded from the picture host.
	MaxSourceBytes = 15 << 20
	// MaxSourcePixels bounds what is decoded.
	MaxSourcePixels = 25_000_000
	// DefaultMaxCacheBytes is how much the cache folder may hold.
	DefaultMaxCacheBytes = 1 << 30

	fetchTimeout   = 15 * time.Second
	notFoundFor    = 10 * time.Minute // "the host has no such picture"
	failedFor      = time.Minute      // "the host could not be reached"
	pruneEvery     = 25               // cache files written between clean-ups
	touchAfter     = time.Hour        // how old a file is before a visit refreshes it
	maxPendingAVIF = 64
	jpegQuality    = 82
	avifQuality    = 60
	avifSpeed      = 8

	week = 7 * 24 * time.Hour
)

// Sizes are the widths a picture can be asked for. The names are the ones the
// picture host itself uses, which is also what is fetched from it, so a size
// is never made up out of a larger download. "original" is the host's biggest
// picture, sent at its own size.
var Sizes = map[string]int{
	"w92": 92, "w154": 154, "w185": 185, "w342": 342, "w500": 500, "w780": 780, "w1280": 1280, "original": 0,
}

// DefaultSources are the hosts a picture can come from: the kind used in the
// address, and the place the picture is fetched from.
var DefaultSources = map[string]string{
	"tmdb": "https://image.tmdb.org/t/p",
}

// fileName is what a picture's file name must look like: TMDB names are 27
// letters and digits with a .jpg or .png ending. No slash, no dots but the one
// before the ending, so it can never reach another path.
var fileName = regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}\.(jpg|jpeg|png)$`)

// Format is what a picture is sent as.
const (
	FormatAVIF = "avif"
	FormatWebP = "webp"
	FormatJPEG = "jpeg"
)

// Negotiate picks the best format the Accept header of a request allows. It
// answers "avif", "webp" or "jpeg" (anything unclear, or no header, is JPEG).
func Negotiate(accept string) string {
	best := FormatJPEG
	for _, part := range strings.Split(accept, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		typ := strings.ToLower(strings.TrimSpace(fields[0]))
		q := 1.0
		for _, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if v, ok := strings.CutPrefix(strings.ToLower(f), "q="); ok {
				if n, err := strconv.ParseFloat(v, 64); err == nil {
					q = n
				}
			}
		}
		if q <= 0 {
			continue
		}
		switch typ {
		case "image/avif":
			return FormatAVIF
		case "image/webp":
			best = FormatWebP
		}
	}
	return best
}

// Proxy fetches, resizes, converts and caches pictures.
type Proxy struct {
	// Dir is the cache folder.
	Dir string
	// Sources maps a kind to the place its pictures are fetched from.
	Sources map[string]string
	// Client fetches from the picture host. It must not follow redirects to
	// other hosts; New gives one that follows none.
	Client *http.Client
	// MaxCacheBytes is the size the cache folder is kept under.
	MaxCacheBytes int64
	// NoAVIF turns the AVIF encoder off (JPEG is sent to everybody).
	NoAVIF bool

	// TouchAfter, when set, replaces the default age at which a visit refreshes
	// a file's time (the clean-up removes the least recently used first).
	TouchAfter time.Duration

	once    sync.Once
	work    chan struct{} // decoding and resizing, a few at a time
	avifRun chan struct{} // AVIF encodes, two at a time
	flights flights
	bg      sync.WaitGroup

	mu      sync.Mutex
	failed  map[string]failure // negative cache, by key
	pending map[string]bool    // AVIF jobs queued or running, by key
	writes  atomic.Int64
	pruning atomic.Bool
}

type failure struct {
	until    time.Time
	notFound bool
}

// New is a Proxy for the default sources whose cache lives in dir.
func New(dir string) *Proxy {
	c := netguard.PublicClient(fetchTimeout)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Proxy{Dir: dir, Sources: DefaultSources, Client: c, MaxCacheBytes: DefaultMaxCacheBytes}
}

func (p *Proxy) init() {
	p.once.Do(func() {
		p.work = make(chan struct{}, 4)
		p.avifRun = make(chan struct{}, 2)
		p.failed = map[string]failure{}
		p.pending = map[string]bool{}
	})
}

// Wait blocks until the AVIF pictures being made in the background are done.
// Tests use it.
func (p *Proxy) Wait() { p.bg.Wait() }

// key is the name a picture is kept under, from where it comes, its size and
// its file name. The format is the ending of the file.
func key(kind, size, file string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + size + "\x00" + file))
	return hex.EncodeToString(sum[:16])
}

var errNotFound = errors.New("the picture host has no such picture")

// Handle answers a request for GET /api/img/{kind}/{size}/{file}.
func (p *Proxy) Handle(w http.ResponseWriter, r *http.Request, kind, size, file string) {
	p.init()
	base, ok := p.Sources[kind]
	width, okSize := Sizes[size]
	if !ok || !okSize || !fileName.MatchString(file) {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Add("Vary", "Accept")
	k := key(kind, size, file)
	if f, ok := p.failure(k); ok {
		failResponse(w, f.notFound)
		return
	}
	wantAVIF := !p.NoAVIF && Negotiate(r.Header.Get("Accept")) == FormatAVIF
	if wantAVIF {
		if path, st, ok := p.cached(k + ".avif"); ok {
			p.serve(w, r, path, st, "image/avif", k, true)
			return
		}
	}
	path, st, ok := p.cachedFallback(k)
	if !ok {
		var err error
		path, st, err = p.makeFallback(r.Context(), k, base, size, width, file)
		if err != nil {
			if r.Context().Err() == nil {
				slog.Info("artwork: picture unavailable", "kind", kind, "size", size, "err", err)
			}
			failResponse(w, errors.Is(err, errNotFound))
			return
		}
	}
	ct := "image/jpeg"
	if strings.HasSuffix(path, ".png") {
		ct = "image/png"
	}
	if wantAVIF {
		// The AVIF takes a while to make: send the JPEG now, with a short
		// browser cache so the next visit gets the AVIF.
		p.scheduleAVIF(k, path)
		p.serve(w, r, path, st, ct, k, false)
		return
	}
	p.serve(w, r, path, st, ct, k, true)
}

func failResponse(w http.ResponseWriter, notFound bool) {
	h := w.Header()
	if notFound {
		h.Set("Cache-Control", "public, max-age=300")
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	h.Set("Cache-Control", "no-store")
	http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
}

func (p *Proxy) failure(k string) (failure, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f, ok := p.failed[k]
	if !ok {
		return f, false
	}
	if time.Now().After(f.until) {
		delete(p.failed, k)
		return f, false
	}
	return f, true
}

func (p *Proxy) noteFailure(k string, notFound bool) {
	d := failedFor
	if notFound {
		d = notFoundFor
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.failed) > 2000 { // a made-up name each time must not grow the map for ever
		now := time.Now()
		for key, f := range p.failed {
			if now.After(f.until) {
				delete(p.failed, key)
			}
		}
		if len(p.failed) > 2000 {
			p.failed = map[string]failure{}
		}
	}
	p.failed[k] = failure{until: time.Now().Add(d), notFound: notFound}
}

// serve sends a cached file. final says the picture is the one this address
// will always give (a week of browser caching); otherwise a short cache lets
// the better one replace it soon.
func (p *Proxy) serve(w http.ResponseWriter, r *http.Request, path string, st os.FileInfo, ct, k string, final bool) {
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	age := p.TouchAfter
	if age == 0 {
		age = touchAfter
	}
	if time.Since(st.ModTime()) > age {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("ETag", `"`+k[:16]+"-"+filepath.Ext(path)[1:]+"-"+strconv.FormatInt(st.Size(), 16)+`"`)
	if final {
		h.Set("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", int(week/time.Second)))
	} else {
		h.Set("Cache-Control", "public, max-age=300")
	}
	http.ServeContent(w, r, "", time.Time{}, f)
}

// cached returns a cache file that exists and is not empty.
func (p *Proxy) cached(name string) (string, os.FileInfo, bool) {
	path := filepath.Join(p.Dir, name)
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return "", nil, false
	}
	return path, st, true
}

func (p *Proxy) cachedFallback(k string) (string, os.FileInfo, bool) {
	for _, ext := range []string{".jpg", ".png"} {
		if path, st, ok := p.cached(k + ext); ok {
			return path, st, true
		}
	}
	return "", nil, false
}

type made struct {
	path string
	st   os.FileInfo
}

// makeFallback fetches a picture, brings it to the size and saves it as JPEG
// (PNG for a picture that came as PNG, which may have see-through parts).
// Requests for the same picture share one download.
func (p *Proxy) makeFallback(ctx context.Context, k, base, size string, width int, file string) (string, os.FileInfo, error) {
	v, err := p.flights.do(ctx, k, func() (made, error) {
		if path, st, ok := p.cachedFallback(k); ok { // made while this call waited
			return made{path, st}, nil
		}
		fctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		src, err := p.fetch(fctx, base+"/"+size+"/"+file)
		if err != nil {
			p.noteFailure(k, errors.Is(err, errNotFound))
			return made{}, err
		}
		if err := p.acquire(fctx, p.work); err != nil {
			return made{}, err
		}
		defer func() { <-p.work }()
		out, ext, err := resize(src, width)
		if err != nil {
			p.noteFailure(k, false)
			return made{}, err
		}
		path, err := p.store(k+ext, out)
		if err != nil {
			return made{}, err
		}
		st, err := os.Stat(path)
		return made{path, st}, err
	})
	return v.path, v.st, err
}

func (p *Proxy) acquire(ctx context.Context, ch chan struct{}) error {
	select {
	case ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fetch downloads a picture from the host, refusing anything big or not a
// picture.
func (p *Proxy) fetch(ctx context.Context, address string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("build picture request: %w", netguard.CleanError(err))
	}
	req.Header.Set("Accept", "image/jpeg,image/png")
	req.Header.Set("User-Agent", "Mediarium")
	res, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch picture: %w", netguard.CleanError(err))
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusForbidden:
		return nil, errNotFound
	case res.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("fetch picture: the host answered %d", res.StatusCode)
	case res.ContentLength > MaxSourceBytes:
		return nil, fmt.Errorf("fetch picture: %d bytes is more than allowed", res.ContentLength)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, MaxSourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read picture: %w", netguard.CleanError(err))
	}
	if len(data) > MaxSourceBytes {
		return nil, fmt.Errorf("fetch picture: more than %d bytes", MaxSourceBytes)
	}
	return data, nil
}

// resize brings a JPEG or PNG to at most width pixels wide (0 keeps its size)
// and returns it with the file ending to keep it under. A picture that is not
// wider than asked is returned as it came, so nothing is lost twice.
func resize(src []byte, width int) ([]byte, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, "", fmt.Errorf("read picture size: %w", err)
	}
	if format != "jpeg" && format != "png" {
		return nil, "", fmt.Errorf("unsupported picture type %q", format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSourcePixels || cfg.Height > MaxSourcePixels || cfg.Width*cfg.Height > MaxSourcePixels {
		return nil, "", fmt.Errorf("the picture is too large (%dx%d)", cfg.Width, cfg.Height)
	}
	ext := ".jpg"
	if format == "png" {
		ext = ".png"
	}
	if width <= 0 || cfg.Width <= width {
		return src, ext, nil
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, "", fmt.Errorf("decode picture: %w", err)
	}
	h := (cfg.Height*width + cfg.Width/2) / cfg.Width
	if h < 1 {
		h = 1
	}
	var buf bytes.Buffer
	if format == "png" {
		dst := image.NewNRGBA(image.Rect(0, 0, width, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
		err = png.Encode(&buf, dst)
	} else {
		dst := image.NewRGBA(image.Rect(0, 0, width, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality})
	}
	if err != nil {
		return nil, "", fmt.Errorf("write picture: %w", err)
	}
	return buf.Bytes(), ext, nil
}

// store writes a cache file in one piece, so a reader never sees half of it.
func (p *Proxy) store(name string, data []byte) (string, error) {
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return "", fmt.Errorf("create picture cache: %w", err)
	}
	tmp, err := os.CreateTemp(p.Dir, "img-*.tmp")
	if err != nil {
		return "", fmt.Errorf("cache picture: %w", err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("cache picture: %w", errors.Join(werr, cerr))
	}
	path := filepath.Join(p.Dir, name)
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("cache picture: %w", err)
	}
	if p.writes.Add(1)%pruneEvery == 0 {
		p.pruneInBackground()
	}
	return path, nil
}

// scheduleAVIF makes the AVIF of a cached JPEG or PNG in the background, once
// however many requests ask, with two encodes at a time at most.
func (p *Proxy) scheduleAVIF(k, from string) {
	p.mu.Lock()
	if p.pending[k] || len(p.pending) >= maxPendingAVIF {
		p.mu.Unlock()
		return
	}
	p.pending[k] = true
	p.mu.Unlock()
	p.bg.Add(1)
	go func() {
		defer p.bg.Done()
		defer func() {
			p.mu.Lock()
			delete(p.pending, k)
			p.mu.Unlock()
		}()
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("artwork: AVIF encoder failed", "err", r)
			}
		}()
		if _, _, ok := p.cached(k + ".avif"); ok {
			return
		}
		p.avifRun <- struct{}{}
		defer func() { <-p.avifRun }()
		data, err := os.ReadFile(from)
		if err != nil {
			return
		}
		out, err := EncodeAVIF(data)
		if err != nil {
			slog.Info("artwork: could not make AVIF", "err", err)
			return
		}
		_, _ = p.store(k+".avif", out)
	}()
}

// EncodeAVIF turns a JPEG or PNG into an AVIF.
func EncodeAVIF(src []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode picture: %w", err)
	}
	var buf bytes.Buffer
	if err := avif.Encode(&buf, img, avif.Options{Quality: avifQuality, QualityAlpha: avifQuality, Speed: avifSpeed, ChromaSubsampling: image.YCbCrSubsampleRatio420}); err != nil {
		return nil, fmt.Errorf("write AVIF: %w", err)
	}
	return buf.Bytes(), nil
}

func (p *Proxy) pruneInBackground() {
	if !p.pruning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer p.pruning.Store(false)
		p.Prune()
	}()
}

// Prune removes leftovers, then the least recently used pictures until the
// cache is under 90% of its limit (when it is over the limit).
func (p *Proxy) Prune() {
	limit := p.MaxCacheBytes
	if limit <= 0 {
		limit = DefaultMaxCacheBytes
	}
	entries, err := os.ReadDir(p.Dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(p.Dir, name)
		if strings.HasSuffix(name, ".tmp") {
			if time.Since(info.ModTime()) > time.Hour {
				_ = os.Remove(path)
			}
			continue
		}
		if !(strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".avif")) {
			continue
		}
		files = append(files, file{path, info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total <= limit {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	target := limit / 10 * 9
	for _, f := range files {
		if total <= target {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}

// flights runs one call at a time for a key and gives its answer to every
// request that asked meanwhile. The call is not tied to the request that
// started it: when the browser gives up, the work finishes anyway.
type flights struct {
	mu sync.Mutex
	m  map[string]*flight
}

type flight struct {
	done chan struct{}
	val  made
	err  error
}

func (g *flights) do(ctx context.Context, k string, fn func() (made, error)) (made, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = map[string]*flight{}
	}
	c, ok := g.m[k]
	if !ok {
		c = &flight{done: make(chan struct{})}
		g.m[k] = c
		go func() {
			defer func() {
				if r := recover(); r != nil {
					c.err = fmt.Errorf("picture work failed: %v", r)
				}
				g.mu.Lock()
				delete(g.m, k)
				g.mu.Unlock()
				close(c.done)
			}()
			c.val, c.err = fn()
		}()
	}
	g.mu.Unlock()
	select {
	case <-c.done:
		return c.val, c.err
	case <-ctx.Done():
		return made{}, ctx.Err()
	}
}
