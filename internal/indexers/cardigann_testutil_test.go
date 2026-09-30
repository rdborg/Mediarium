package indexers

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// fixtureDef reads a test definition from testdata/cardigann.
func fixtureDef(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "cardigann", name+".yml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// tarGz builds a .tar.gz shaped like a GitHub archive of the definitions
// repository: files maps paths below "Indexers-master/" to contents.
func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: "Indexers-master/" + name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create("Indexers-master/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// archiveServer serves an archive and counts downloads.
func archiveServer(t *testing.T, archive []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archive)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// fixtureManager returns a manager whose store holds the named fixture
// definitions, downloaded from a local fake archive server.
func fixtureManager(t *testing.T, names ...string) *CardigannManager {
	t.Helper()
	files := map[string][]byte{}
	for _, n := range names {
		files["definitions/v11/"+n+".yml"] = fixtureDef(t, n)
	}
	srv, _ := archiveServer(t, tarGz(t, files))
	store := NewDefinitionStore(filepath.Join(t.TempDir(), "indexer-definitions"))
	store.SourceURL = srv.URL + "/archive.tar.gz"
	m := NewCardigannManager(store, nil)
	m.DefaultDelay = 0
	return m
}
