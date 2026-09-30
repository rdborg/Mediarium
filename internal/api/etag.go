package api

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
)

// minGzipBytes is the smallest answer worth compressing. Below it the saving
// is lost in the headers.
const minGzipBytes = 1024

// revalidated makes a large list answer cheap to ask for again and again. The
// Library page reads the whole movie list every few seconds, and most of the
// time nothing has changed:
//
//   - the answer carries an ETag, and a request that sends it back in
//     If-None-Match gets "304 Not Modified" with no body;
//   - a browser that accepts gzip gets the body compressed.
//
// The browser may keep its copy (privately) but must ask before using it,
// which is what "no-cache" means; the default for the API is "no-store". The
// tag is weak because the same content is sent compressed and plain. Only a
// successful GET is touched; everything else passes through unchanged.
func revalidated(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			h(w, r)
			return
		}
		rec := &recordedResponse{header: make(http.Header)}
		h(rec, r)

		out := w.Header()
		for k, v := range rec.header {
			out[k] = v
		}
		body := rec.body.Bytes()
		if rec.code != 0 && rec.code != http.StatusOK {
			w.WriteHeader(rec.code)
			_, _ = w.Write(body)
			return
		}

		sum := sha256.Sum256(body)
		etag := `W/"` + hex.EncodeToString(sum[:16]) + `"`
		out.Set("ETag", etag)
		out.Set("Cache-Control", "private, no-cache")
		out.Add("Vary", "Accept-Encoding")
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			out.Del("Content-Type")
			out.Del("Content-Length")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if len(body) >= minGzipBytes && acceptsGzip(r.Header.Get("Accept-Encoding")) {
			var zipped bytes.Buffer
			zw, err := gzip.NewWriterLevel(&zipped, gzip.BestSpeed)
			if err == nil {
				if _, err = zw.Write(body); err == nil {
					err = zw.Close()
				}
			}
			if err == nil {
				out.Set("Content-Encoding", "gzip")
				out.Set("Content-Length", strconv.Itoa(zipped.Len()))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(zipped.Bytes())
				return
			}
		}
		out.Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// etagMatches reports whether an If-None-Match header names etag. It compares
// weakly (as the standard says for this header) and understands "*" and lists.
func etagMatches(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, part := range strings.Split(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(part), "W/") == want {
			return true
		}
	}
	return false
}

// acceptsGzip reports whether an Accept-Encoding header allows gzip.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		// "gzip;q=0" means "not gzip".
		if q := strings.TrimSpace(params); strings.HasPrefix(q, "q=") {
			if v, err := strconv.ParseFloat(strings.TrimPrefix(q, "q="), 64); err == nil && v == 0 {
				return false
			}
		}
		return true
	}
	return false
}

// recordedResponse holds a handler's answer so revalidated can look at it
// before anything is sent.
type recordedResponse struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (r *recordedResponse) Header() http.Header { return r.header }

func (r *recordedResponse) Write(p []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.body.Write(p)
}

func (r *recordedResponse) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
}
