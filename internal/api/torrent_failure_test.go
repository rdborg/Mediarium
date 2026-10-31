package api

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/torrentclient"
)

func TestFailureNote(t *testing.T) {
	now := time.Now()
	shook := now.Add(-42 * time.Second)
	listed := torrentclient.Diag{TrackersTried: 3}
	bare := torrentclient.Diag{}
	tests := []struct {
		name    string
		route   torrentRoute
		diag    torrentclient.Diag
		want    []string
		notWant []string
	}{
		{
			name: "VPN on and answering", route: torrentRoute{VPN: true, VPNConnected: true, Handshake: shook}, diag: listed,
			want: []string{"The VPN was on and connected.", "Try the same torrent with the VPN off"},
		},
		{
			name: "VPN on, never answered", route: torrentRoute{VPN: true}, diag: listed,
			want:    []string{"its server never answered", "Press Test on the VPN page"},
			notWant: []string{"with the VPN off"},
		},
		{
			name: "VPN on, went quiet", route: torrentRoute{VPN: true, Handshake: now.Add(-10 * time.Minute)}, diag: listed,
			want:    []string{"had stopped answering", "Press Test"},
			notWant: []string{"with the VPN off"},
		},
		{
			name: "VPN on, magnet without trackers", route: torrentRoute{VPN: true, VPNConnected: true, Handshake: shook}, diag: bare,
			want:    []string{"lists no trackers", "DHT is off while the VPN is on"},
			notWant: []string{"with the VPN off"},
		},
		{
			name: "VPN on, DHT through the tunnel, magnet without trackers", route: torrentRoute{VPN: true, VPNConnected: true, Handshake: shook, Discover: true}, diag: bare,
			want:    []string{"lists no trackers", "DHT ran through the VPN"},
			notWant: []string{"DHT is off", "with the VPN off"},
		},
		{
			name: "no VPN", route: torrentRoute{}, diag: listed,
			want:    []string{"The VPN was off."},
			notWant: []string{"with the VPN off", "Press Test"},
		},
		{
			name: "no VPN, no trackers", route: torrentRoute{}, diag: bare,
			want: []string{"The VPN was off.", "peers could only come from DHT"},
		},
		{
			name: "proxy", route: torrentRoute{Proxy: true}, diag: listed,
			want:    []string{"went through your proxy"},
			notWant: []string{"with the VPN off"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := failureNote(tc.route, tc.diag)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("note %q is missing %q", got, w)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(got, w) {
					t.Errorf("note %q should not contain %q", got, w)
				}
			}
		})
	}
}

// The wrapped failure reads as sentences and still matches the original error.
func TestTorrentFailureKeepsTheSentinel(t *testing.T) {
	inner := &torrentclient.NoMetadataError{Waited: 10 * time.Minute, Diag: torrentclient.Diag{TrackersTried: 2}}
	err := error(&torrentFailure{err: inner, note: "The VPN was off."})
	if !errors.Is(err, torrentclient.ErrNoMetadata) {
		t.Fatal("the wrapped error must still match ErrNoMetadata")
	}
	want := "No metadata after 10 minutes: no peer sent the torrent's details (0 of 2 trackers answered, 0 peers found, 0 connected). The VPN was off."
	if err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
}

// An error that is not about finding peers is passed on as it is.
func TestExplainTorrentFailureLeavesOtherErrorsAlone(t *testing.T) {
	s := newBareServer(t)
	plain := errors.New("the disk is full")
	if got := s.explainTorrentFailure(plain, nil); got != plain {
		t.Fatalf("got %v, want the original error", got)
	}
	stalled := &torrentclient.StalledError{Waited: 2 * time.Hour}
	got := s.explainTorrentFailure(stalled, nil)
	if !errors.Is(got, torrentclient.ErrStalled) || !strings.Contains(got.Error(), "The VPN was off.") {
		t.Fatalf("got %q", got.Error())
	}
}
