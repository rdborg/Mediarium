package announce

import (
	"testing"
	"time"
)

// FuzzIRCLine feeds the line reader whatever a server could send. A made-up
// or hostile network must not be able to crash the app.
func FuzzIRCLine(f *testing.F) {
	f.Add(":nick!user@host PRIVMSG #chan :New torrent: Some.Release.1080p")
	f.Add("PING :token")
	f.Add(":")
	f.Add(":only-a-prefix")
	f.Add(" :")
	f.Add("\x00\x01\x02 :\xff\xfe")
	f.Fuzz(func(t *testing.T, line string) {
		_, _ = parseIRC(line)
		_ = Clean(line)
		_ = ParseSize(line, true)
		_ = ParseSize(line, false)
	})
}

// FuzzAnnounceReader runs every built-in template over arbitrary lines.
func FuzzAnnounceReader(f *testing.F) {
	f.Add("New Torrent: Some.Release.1080p [Movies] - Size: 1.5 GiB - https://t.example/torrent/12")
	f.Add("")
	f.Add("\x1b[1m\x03Foo\x0f")
	var readers []*Reader
	var compiled []*Compiled
	for _, p := range Presets() {
		if c, err := Compile(p.Template); err == nil {
			compiled = append(compiled, c)
			readers = append(readers, NewReader(c))
		}
	}
	f.Fuzz(func(t *testing.T, line string) {
		now := time.Now()
		for i, r := range readers {
			if fields, ok := r.Feed("#chan", "bot", Clean(line), now); ok {
				if a, err := compiled[i].Build(fields); err == nil {
					_, _ = compiled[i].DownloadURL(a, URLOptions{TrackerURL: "https://tracker.example", Keys: map[string]string{"passkey": "k", "rsskey": "k", "apikey": "k"}})
				}
			}
		}
	})
}
