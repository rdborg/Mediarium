package download

import (
	"strings"
	"testing"
	"time"
)

func TestNNTPConnLifecycle(t *testing.T) {
	yenc := EncodeYenc("hello.txt", []byte("Hello, Usenet!\nLine two.\n.leading dot line\n"))
	srv := newFakeNNTPServer(t, map[string][]byte{
		"seg1@example": yenc,
	})

	conn, err := DialNNTP(srv.addr, srv.port, false, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Quit()

	if err := conn.Authenticate("user", "pass"); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if err := conn.SelectGroup("alt.binaries.test"); err != nil {
		t.Fatalf("select group: %v", err)
	}

	body, err := conn.FetchBody("<seg1@example>")
	if err != nil {
		t.Fatalf("fetch body: %v", err)
	}
	if !strings.Contains(string(body), "=ybegin") {
		t.Fatalf("expected yenc body content, got: %s", body)
	}

	part, err := DecodeYenc(bytesReader(body))
	if err != nil {
		t.Fatalf("decode fetched body: %v", err)
	}
	if string(part.Data) != "Hello, Usenet!\nLine two.\n.leading dot line\n" {
		t.Fatalf("unexpected decoded data (dot-unstuffing likely broken): %q", part.Data)
	}
}

func TestNNTPConnMissingArticle(t *testing.T) {
	srv := newFakeNNTPServer(t, map[string][]byte{})
	conn, err := DialNNTP(srv.addr, srv.port, false, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Quit()

	_, err = conn.FetchBody("<missing@example>")
	if err == nil {
		t.Fatal("expected error for missing article")
	}
	nntpErr, ok := err.(*NNTPError)
	if !ok {
		t.Fatalf("expected *NNTPError, got %T: %v", err, err)
	}
	if nntpErr.Code != 430 {
		t.Fatalf("expected code 430, got %d", nntpErr.Code)
	}
}
