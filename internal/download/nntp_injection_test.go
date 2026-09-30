package download

import (
	"strings"
	"testing"
	"time"
)

func TestParseNZBRefusesMessageIDsAndGroupsThatCouldSendCommands(t *testing.T) {
	nzbWith := func(group, id string) []byte {
		return []byte(`<?xml version="1.0" encoding="UTF-8"?><nzb><file subject="&quot;a.mkv&quot;"><groups><group>` + group +
			`</group></groups><segments><segment bytes="10" number="1">` + id + `</segment></segments></file></nzb>`)
	}
	tests := []struct {
		name      string
		group, id string
		ok        bool
	}{
		{"an ordinary id", "alt.binaries.test", "part1of2.abc123@news.example", true},
		{"an id in angle brackets", "alt.binaries.test", "&lt;part1@news.example&gt;", true},
		{"odd but legal characters", "alt.binaries.test", "a$b%c!d~e@x.y", true},
		{"padded with spaces", "alt.binaries.test", "  part1@news.example \n", true},
		{"a line break inside the id", "alt.binaries.test", "x@y&#13;&#10;POST", false},
		{"a bare line feed", "alt.binaries.test", "x@y&#10;QUIT", false},
		{"a space in the middle", "alt.binaries.test", "x@y GROUP alt.test", false},
		{"a closing bracket in the middle", "alt.binaries.test", "x@y>&#13;&#10;AUTHINFO", false},
		{"a NUL", "alt.binaries.test", "x&#0;y@z", false},
		{"an empty id", "alt.binaries.test", "", false},
		{"only brackets", "alt.binaries.test", "&lt;&gt;", false},
		{"a very long id", "alt.binaries.test", strings.Repeat("a", 600), false},
		{"non-ASCII", "alt.binaries.test", "x@yé", false},
		{"a line break in a group", "alt.binaries.test&#13;&#10;POST", "x@y", false},
		{"a space in a group", "alt.binaries test", "x@y", false},
		{"no group name", "", "x@y", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nzb, err := ParseNZB(nzbWith(tt.group, tt.id))
			if tt.ok {
				if err != nil {
					t.Fatalf("ParseNZB: %v", err)
				}
				if got := nzb.Files[0].Segments[0].MessageID; strings.ContainsAny(got, " \r\n") {
					t.Fatalf("the id kept whitespace: %q", got)
				}
			} else if err == nil {
				t.Fatalf("ParseNZB accepted %q / %q", tt.group, tt.id)
			}
		})
	}
}

func TestParseNZBRefusesAHugeFile(t *testing.T) {
	if _, err := ParseNZB(make([]byte, maxNZBBytes+1)); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("err = %v, want the size refusal", err)
	}
}

// Even a value that got past the NZB check never reaches the wire with a line
// break in it.
func TestNNTPCommandsNeverCarryALineBreak(t *testing.T) {
	srv := newFakeNNTPServer(t, map[string][]byte{"x@y": EncodeYenc("a.bin", []byte("data"))})
	conn, err := DialNNTP(srv.addr, srv.port, false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Quit()
	if _, err := conn.FetchBody("<x@y>\r\nPOST"); err == nil {
		t.Fatal("FetchBody sent a command with a line break in it")
	}
	if err := conn.SelectGroup("alt.test\r\nQUIT"); err == nil {
		t.Fatal("SelectGroup sent a command with a line break in it")
	}
	if err := conn.Authenticate("user\nAUTHINFO PASS x", "pw"); err == nil {
		t.Fatal("Authenticate sent a command with a line break in it")
	}
	if n := srv.totalHits(); n != 0 {
		t.Fatalf("the server saw %d article requests", n)
	}
	// The connection is still usable for a normal request.
	if _, err := conn.FetchBody("<x@y>"); err != nil {
		t.Fatalf("normal fetch after the refusals: %v", err)
	}
}
