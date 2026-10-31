package torrentclient

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
)

// fakeNames answers from a fixed table and counts the questions, so a test
// can see that a name went to the tunnel's DNS.
type fakeNames struct {
	table map[string][]net.IP
	asked []string
}

func (f *fakeNames) LookupHost(_ context.Context, host string) ([]net.IP, error) {
	f.asked = append(f.asked, host)
	if ips, ok := f.table[host]; ok {
		return ips, nil
	}
	return nil, errors.New("no such name")
}

func TestUDPTrackersAreLookedUpThroughTheTunnel(t *testing.T) {
	names := &fakeNames{table: map[string][]net.IP{
		"tracker.example.org": {net.ParseIP("203.0.113.7")},
		"v6.example.org":      {net.ParseIP("2001:db8::5")},
	}}
	cases := []struct {
		name string
		in   [][]string
		want [][]string
	}{
		{"name becomes an address", [][]string{{"udp://tracker.example.org:6969/announce"}}, [][]string{{"udp://203.0.113.7:6969/announce"}}},
		{"ipv6 answer", [][]string{{"udp://v6.example.org:80/announce"}}, [][]string{{"udp://[2001:db8::5]:80/announce"}}},
		{"address is left alone", [][]string{{"udp://198.51.100.9:1337/announce"}}, [][]string{{"udp://198.51.100.9:1337/announce"}}},
		{"http trackers are left alone", [][]string{{"https://t.example.org/announce"}}, [][]string{{"https://t.example.org/announce"}}},
		{"unknown name is dropped, not tried at home", [][]string{{"udp://gone.example.org:6969/announce", "https://t.example.org/announce"}}, [][]string{{"https://t.example.org/announce"}}},
		{"a tier with nothing left disappears", [][]string{{"udp://gone.example.org:6969/announce"}, {"udp://203.0.113.1:6969"}}, [][]string{{"udp://203.0.113.1:6969"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tunnelTrackers(context.Background(), names, tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBootstrapNamesComeFromTheTunnelOnly(t *testing.T) {
	names := &fakeNames{table: map[string][]net.IP{}}
	if _, err := bootstrapThroughTunnel("udp4", names); err == nil {
		t.Fatal("bootstrap with no answers must fail instead of falling back to the host")
	}
	if len(names.asked) == 0 {
		t.Fatal("the tunnel was never asked")
	}
	names = &fakeNames{table: map[string][]net.IP{"router.bittorrent.com": {net.ParseIP("203.0.113.20")}}}
	addrs, err := bootstrapThroughTunnel("udp4", names)
	if err != nil || len(addrs) == 0 {
		t.Fatalf("addrs=%v err=%v", addrs, err)
	}
}
