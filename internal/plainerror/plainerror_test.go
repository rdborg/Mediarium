package plainerror_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"
	"testing"

	"github.com/rdborg/mediarium/internal/plainerror"
)

func TestMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nothing", nil, ""},
		{"dns from the audit",
			errors.New(`couldn't get the NZB file: Get "https://api.example.com/getnzb/abc123.nzb": dial tcp: lookup api.example.com on 192.168.65.7:53: no such host`),
			"Couldn't get the NZB file. The address could not be found. Check the address and your internet connection."},
		{"generic wrapper is dropped",
			errors.New(`the download failed: couldn't get the NZB file: dial tcp 1.2.3.4:443: connect: connection refused`),
			"Couldn't get the NZB file. The server refused the connection. Check it's running and the address and port are right."},
		{"the download failed alone is kept",
			errors.New(`the download failed: dial tcp: lookup news.example.com: no such host`),
			"The download failed. The address could not be found. Check the address and your internet connection."},
		{"timeout", errors.New(`couldn't get the torrent: Get "https://x/y": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`),
			"Couldn't get the torrent. The server took too long to answer. Try again in a few minutes."},
		{"i/o timeout", errors.New(`dial tcp 10.0.0.1:563: i/o timeout`), "The server took too long to answer. Try again in a few minutes."},
		{"tls", errors.New(`Get "https://x/y": tls: failed to verify certificate: x509: certificate has expired`),
			"The secure connection could not be set up. The server's certificate may be wrong or expired."},
		{"reset", errors.New(`couldn't get the NZB file: read tcp 10.0.0.2:5555->1.2.3.4:443: connection reset by peer`),
			"Couldn't get the NZB file. The connection was dropped. Try again in a few minutes."},
		{"eof", errors.New(`couldn't get the NZB file: Get "https://x/y": EOF`), "Couldn't get the NZB file. The connection was dropped. Try again in a few minutes."},
		{"unreachable", errors.New(`dial tcp 1.2.3.4:443: connect: network is unreachable`), "The network could not be reached. Check your internet connection."},
		{"401", errors.New(`couldn't get the NZB file: the server answered with status 401 for https://x/getnzb`),
			"Couldn't get the NZB file. The site refused the login or key (error 401)."},
		{"403", errors.New(`couldn't get the torrent: status 403`), "Couldn't get the torrent. The site refused the login or key (error 403)."},
		{"404", errors.New(`the server answered with status 404 for https://x/y`), "The site no longer has that file (error 404)."},
		{"429", errors.New(`couldn't get the NZB file: the server answered with status 429 for https://x/y`),
			"Couldn't get the NZB file. The site says there were too many requests. Wait a while and try again."},
		{"500", errors.New(`couldn't get the NZB file: the server answered with status 503 for https://x/y`),
			"Couldn't get the NZB file. The site had a problem on its side (error 503). Try again later."},
		{"no such file", errors.New(`couldn't move the file into your library: rename /d/a.mkv /m/a.mkv: no such file or directory`),
			"Couldn't move the file into your library. A file or folder could not be found."},
		{"windows path", errors.New(`open D:\Movies\x.mkv: The system cannot find the path specified.`), "A file or folder could not be found."},
		{"permission", errors.New(`couldn't move the file into your library: mkdir /movies/x: permission denied`),
			"Couldn't move the file into your library. Mediarium is not allowed to use that file or folder. Check its permissions."},
		{"permission typed", fmt.Errorf("couldn't save: open /x/y: %w", fs.ErrPermission),
			"Couldn't save. Mediarium is not allowed to use that file or folder. Check its permissions."},
		{"disk full", errors.New(`write /downloads/x.part: no space left on device`), "The disk is full. Free up some space and try again."},
		{"disk full typed", fmt.Errorf("write /downloads/x: %w", syscall.ENOSPC), "The disk is full. Free up some space and try again."},
		{"canceled", fmt.Errorf("the download failed: %w", context.Canceled), "The download failed. It was stopped before it finished."},
		{"canceled raw", errors.New(`Get "https://x/y": context canceled`), "It was stopped before it finished."},
		{"own sentence is untouched", errors.New("no Usenet server is set up. Add your provider's server in Settings first"),
			"no Usenet server is set up. Add your provider's server in Settings first"},
		{"own sentence about disk is untouched", errors.New("couldn't unpack the download: not enough free disk space to unpack the archive"),
			"couldn't unpack the download: not enough free disk space to unpack the archive"},
		{"unknown raw stays", errors.New(`lookup failed`), "lookup failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plainerror.Message(tt.err)
			if got != tt.want {
				t.Errorf("Message(%v)\n got: %q\nwant: %q", tt.err, got, tt.want)
			}
			if tt.err != nil {
				// Already-plain text must come through unchanged.
				if again := plainerror.Text(got); again != got {
					t.Errorf("not idempotent: %q became %q", got, again)
				}
			}
		})
	}
}

func TestPlainMessagesNeverCarryRawNetworkText(t *testing.T) {
	raw := []string{
		`Get "https://api.example.com/x?apikey=SECRET": dial tcp: lookup api.example.com on 10.0.0.1:53: no such host`,
		`dial tcp 192.168.1.5:119: connect: connection refused`,
		`read tcp 10.0.0.2:1->10.0.0.3:2: i/o timeout`,
	}
	for _, r := range raw {
		got := plainerror.Text(r)
		for _, bad := range []string{"dial tcp", "lookup ", "192.168", "10.0.0", "SECRET", "https://"} {
			if strings.Contains(got, bad) {
				t.Errorf("%q leaked %q into %q", r, bad, got)
			}
		}
	}
}

func TestForResponse(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		want        string
		wantChanged bool
	}{
		{"plain stays", "Couldn't save the setting. Try again.", "Couldn't save the setting. Try again.", false},
		{"network text is rewritten", `Get "https://x/y": dial tcp: lookup x: no such host`, "The address could not be found. Check the address and your internet connection.", true},
		{"database text is hidden", "update settings: sqlite: database is locked", plainerror.Generic, true},
		{"json text is hidden", "decode: invalid character 'x' looking for beginning of value", plainerror.Generic, true},
		{"crash text is hidden", "runtime error: invalid memory address or nil pointer dereference", plainerror.Generic, true},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := plainerror.ForResponse(tt.in)
			if got != tt.want || changed != tt.wantChanged {
				t.Errorf("ForResponse(%q) = %q, %v; want %q, %v", tt.in, got, changed, tt.want, tt.wantChanged)
			}
		})
	}
}
