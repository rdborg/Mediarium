package importlists

import "testing"

// FuzzParseFeed gives the feed reader whatever a list address might answer
// with: broken XML, HTML, binary, deeply nested elements.
func FuzzParseFeed(f *testing.F) {
	f.Add([]byte(`<rss><channel><item><title>A (2020)</title><link>https://www.imdb.com/title/tt1234567/</link></item></channel></rss>`))
	f.Add([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>B</title><link href="x"/></entry></feed>`))
	f.Add([]byte(`<html><body>not a feed`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseFeed(data)
	})
}
