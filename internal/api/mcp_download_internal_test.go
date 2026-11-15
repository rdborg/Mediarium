package api

import (
	"testing"
	"time"
)

func TestMCPDownloadTokens(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	clock := func(d *mcpDownloads) { d.now = func() time.Time { return now } }
	newStore := func() (*mcpDownloads, string) {
		d := &mcpDownloads{}
		clock(d)
		return d, d.addOffer(mcpOffer{userID: 1, release: "A.Release"})
	}

	tests := []struct {
		name string
		run  func(d *mcpDownloads, offer string) bool
		want bool
	}{
		{"fresh token for its own release", func(d *mcpDownloads, o string) bool {
			tok, _ := d.mint(1, o)
			return d.redeem(1, o, tok)
		}, true},
		{"no token", func(d *mcpDownloads, o string) bool { return d.redeem(1, o, "") }, false},
		{"made-up token", func(d *mcpDownloads, o string) bool { d.mint(1, o); return d.redeem(1, o, "mcpgrab_nope") }, false},
		{"used twice", func(d *mcpDownloads, o string) bool {
			tok, _ := d.mint(1, o)
			d.redeem(1, o, tok)
			return d.redeem(1, o, tok)
		}, false},
		{"another account", func(d *mcpDownloads, o string) bool {
			tok, _ := d.mint(1, o)
			return d.redeem(2, o, tok)
		}, false},
		{"another release", func(d *mcpDownloads, o string) bool {
			tok, _ := d.mint(1, o)
			return d.redeem(1, "r-other", tok)
		}, false},
		{"just before it runs out", func(d *mcpDownloads, o string) bool {
			tok, _ := d.mint(1, o)
			now = now.Add(mcpTokenTTL - time.Second)
			return d.redeem(1, o, tok)
		}, true},
		{"when it runs out", func(d *mcpDownloads, o string) bool {
			tok, _ := d.mint(1, o)
			now = now.Add(mcpTokenTTL)
			return d.redeem(1, o, tok)
		}, false},
		{"a newer preview replaces the older token", func(d *mcpDownloads, o string) bool {
			old, _ := d.mint(1, o)
			d.mint(1, o)
			return d.redeem(1, o, old)
		}, false},
		{"a wrong guess does not spend the real token", func(d *mcpDownloads, o string) bool {
			tok, _ := d.mint(1, o)
			d.redeem(1, o, "mcpgrab_nope")
			return d.redeem(1, o, tok)
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
			d, offer := newStore()
			if got := tc.run(d, offer); got != tc.want {
				t.Fatalf("redeem = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMCPDownloadOffers(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	d := &mcpDownloads{now: func() time.Time { return now }}
	id := d.addOffer(mcpOffer{userID: 7, release: "X"})
	if _, ok := d.offer(7, id); !ok {
		t.Fatal("the account that was shown a release cannot pick it")
	}
	if _, ok := d.offer(8, id); ok {
		t.Fatal("another account picked a release it was not shown")
	}
	if _, ok := d.offer(7, "r-unknown"); ok {
		t.Fatal("an unknown release id was found")
	}
	now = now.Add(mcpOfferTTL)
	if _, ok := d.offer(7, id); ok {
		t.Fatal("a release id outlived its time")
	}
	for i := 0; i < mcpOfferMax+10; i++ {
		d.addOffer(mcpOffer{userID: 7})
	}
	if len(d.offers) > mcpOfferMax {
		t.Fatalf("%d releases kept, at most %d", len(d.offers), mcpOfferMax)
	}
}
