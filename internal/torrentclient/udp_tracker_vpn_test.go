package torrentclient_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	mediarium "github.com/rdborg/mediarium/internal/torrentclient"
)

type refusingTunnel struct{}

func (refusingTunnel) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("tunnel test: no connection")
}

// A magnet link that lists a UDP tracker must not take the download down while
// the VPN is on. UDP trackers can't go through the tunnel, so they are
// skipped; the engine used to panic when the torrent tried to use one.
func TestMagnetWithUDPTrackerDoesNotCrashWithVPN(t *testing.T) {
	engine, err := mediarium.New(mediarium.Config{DataDir: t.TempDir(), Tunnel: refusingTunnel{}})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=fixture" +
		"&tr=udp%3A%2F%2Ftracker.example%3A6969%2Fannounce" +
		"&tr=http%3A%2F%2Ftracker.example%3A80%2Fannounce"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tor, err := engine.AddMagnetIn(ctx, magnet, t.TempDir())
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("add magnet: %v", err)
	}
	// Give the announcer time to try every tracker; a panic ends the test run.
	time.Sleep(1500 * time.Millisecond)
	if tor != nil {
		engine.Remove(tor)
	}
}
