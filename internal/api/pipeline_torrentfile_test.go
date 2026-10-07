package api

import "testing"

func TestLooksLikeTorrentFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want bool
	}{
		{"a torrent with an announce list", "d13:announce-listll43:http://tracker.example/announceee4:infod6:lengthi1eee", true},
		{"a torrent with just info", "d4:infod6:lengthi1e4:name3:abcee", true},
		{"an NZB", `<?xml version="1.0" encoding="UTF-8"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"></nzb>`, false},
		{"an error page", "<html>Not found</html>", false},
		{"empty", "", false},
		{"too short", "d1:a", false},
	} {
		if got := looksLikeTorrentFile([]byte(tc.data)); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
