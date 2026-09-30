package api

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOfferedURLs(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	o := &offeredURLs{now: func() time.Time { return now }}

	if o.has("http://a/1") {
		t.Fatal("nothing offered yet")
	}
	o.add("")
	if o.has("") {
		t.Fatal("an empty URL is never offered")
	}
	o.add("http://a/1")
	if !o.has("http://a/1") || o.has("http://a/2") {
		t.Fatal("only the offered URL counts")
	}
	now = now.Add(offeredURLTTL)
	if o.has("http://a/1") {
		t.Fatal("an offered URL expires")
	}

	// The cache stays bounded, dropping the oldest entries first.
	for i := 0; i < offeredURLMax+10; i++ {
		now = now.Add(time.Millisecond)
		o.add("http://a/" + strconv.Itoa(i))
	}
	if len(o.seen) != offeredURLMax {
		t.Fatalf("holds %d URLs, want %d", len(o.seen), offeredURLMax)
	}
	if o.has("http://a/0") || !o.has("http://a/"+strconv.Itoa(offeredURLMax+9)) {
		t.Fatal("the oldest URLs should go first")
	}
}

// Search results carry an opaque reference, never the real download link,
// which for Usenet indexers includes the indexer's API key.
func TestOfferedReferencesHideTheLink(t *testing.T) {
	var o offeredURLs
	link := "https://indexer.example/api?t=get&id=abc&apikey=SECRET"
	ref := o.ref(link)
	if ref == link || !strings.HasPrefix(ref, refPrefix) || strings.Contains(ref, "SECRET") {
		t.Fatalf("reference %q must not reveal the link", ref)
	}
	if again := o.ref(link); again != ref {
		t.Fatalf("one link should keep one reference: %q then %q", ref, again)
	}
	if got := o.resolve(ref); got != link {
		t.Fatalf("resolve(%q) = %q, want the link", ref, got)
	}
	if got := o.resolve("rel_0000"); got != "rel_0000" {
		t.Fatalf("an unknown reference must stay unresolved, got %q", got)
	}
	if got := o.resolve("https://other.example/x"); got != "https://other.example/x" {
		t.Fatalf("a plain link passes through unchanged, got %q", got)
	}
	if !o.has(link) {
		t.Fatal("a referenced link counts as offered")
	}
}
