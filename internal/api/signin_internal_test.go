package api

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestCeremonyStore(t *testing.T) {
	now := time.Now()
	c := newCeremonyStore()
	sd := webauthn.SessionData{Challenge: "abc"}

	id, err := c.put(kindLogin, 0, sd, now)
	if err != nil || len(id) < 32 {
		t.Fatalf("put: %q, %v", id, err)
	}
	// The wrong kind of ceremony is refused, and that uses it up.
	if _, ok := c.take(id, kindRegister, now); ok {
		t.Error("a login challenge was taken as a registration")
	}
	if _, ok := c.take(id, kindLogin, now); ok {
		t.Error("a challenge survived a wrong attempt")
	}
	// A challenge works once.
	id, _ = c.put(kindRegister, 7, sd, now)
	got, ok := c.take(id, kindRegister, now)
	if !ok || got.userID != 7 || got.session.Challenge != "abc" {
		t.Errorf("take = %+v, %v", got, ok)
	}
	if _, ok := c.take(id, kindRegister, now); ok {
		t.Error("a challenge was taken twice")
	}
	// It expires.
	id, _ = c.put(kindLogin, 0, sd, now)
	if _, ok := c.take(id, kindLogin, now.Add(ceremonyTTL+time.Second)); ok {
		t.Error("an old challenge was accepted")
	}
	// Unknown and empty ids.
	for _, bad := range []string{"", "nope", id} {
		if _, ok := c.take(bad, kindLogin, now); ok {
			t.Errorf("%q was accepted", bad)
		}
	}
	// A flood stops at a limit, and the oldest waiting one makes room, so real
	// people are not locked out; expired ones are swept.
	first, _ := c.put(kindLogin, 0, sd, now.Add(-time.Second)) // expires before all the others
	for i := 1; i < maxCeremony; i++ {
		if _, err := c.put(kindLogin, 0, sd, now.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	later := now.Add(time.Second)
	newest, err := c.put(kindLogin, 0, sd, later)
	if err != nil {
		t.Fatalf("a full store must still take a new challenge: %v", err)
	}
	if len(c.m) > maxCeremony {
		t.Errorf("%d challenges waiting, want at most %d", len(c.m), maxCeremony)
	}
	if _, ok := c.take(first, kindLogin, later); ok {
		t.Error("the oldest challenge should have made room")
	}
	if _, ok := c.take(newest, kindLogin, later); !ok {
		t.Error("the newest challenge was lost")
	}
	if _, err := c.put(kindLogin, 0, sd, now.Add(ceremonyTTL+time.Hour)); err != nil {
		t.Errorf("expired challenges were not swept: %v", err)
	}
}

func TestReadOnlyAllowsTable(t *testing.T) {
	// readOnlyAllows is the one rule; the route walk in viewer_test.go proves it on every route.
	cases := []struct {
		name            string
		viewer, roKey   bool
		admin           bool
		method, pattern string
		want            bool
	}{
		{"viewer reads", true, false, false, "GET", "GET /api/movies", true},
		{"viewer HEAD", true, false, false, "HEAD", "GET /api/movies", true},
		{"viewer writes", true, false, false, "POST", "POST /api/movies", false},
		{"viewer deletes", true, false, false, "DELETE", "DELETE /api/queue/{id}", false},
		{"viewer own profile", true, false, false, "PUT", "PUT /api/auth/profile", true},
		{"viewer reads the keys", true, false, false, "GET", "GET /api/auth/api-keys", false},
		{"viewer makes a key", true, false, false, "POST", "POST /api/auth/api-keys", false},
		{"read-only key reads", false, true, false, "GET", "GET /api/movies", true},
		{"read-only key writes", false, true, false, "POST", "POST /api/movies", false},
		{"read-only key own profile", false, true, false, "PUT", "PUT /api/auth/profile", false},
		{"read-only admin key writes", false, true, true, "PUT", "PUT /api/settings", false},
		{"read-only key on a viewer's own profile", true, true, false, "PUT", "PUT /api/auth/profile", false},
	}
	for _, c := range cases {
		u := newTestUser(c.viewer, c.roKey, c.admin)
		got, msg := readOnlyAllows(u, c.method, c.pattern)
		if got != c.want || (!got && msg == "") {
			t.Errorf("%s: allowed=%v (%q), want %v", c.name, got, msg, c.want)
		}
	}
}

// A flood from many networks cannot push out one real person's sign-in: each
// network holds a few at most, and a full store takes room from the busiest.
func TestCeremonyStoreFloodKeepsARealSignIn(t *testing.T) {
	now := time.Now()
	c := newCeremonyStore()
	sd := webauthn.SessionData{Challenge: "abc"}
	real, _ := c.putFrom(kindLogin, 0, "net:real", sd, now.Add(-time.Minute)) // the oldest of all
	for i := 0; i < 3*maxCeremony; i++ {
		if _, err := c.putFrom(kindLogin, 0, "net:"+twoDigits(i%300/10)+twoDigits(i%10), sd, now.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.m) > maxCeremony {
		t.Errorf("%d waiting, want at most %d", len(c.m), maxCeremony)
	}
	per := map[string]int{}
	for _, v := range c.m {
		per[v.owner]++
	}
	for o, n := range per {
		if n > maxCeremonyPerOwner {
			t.Errorf("%s holds %d, want at most %d", o, n, maxCeremonyPerOwner)
		}
	}
	if _, ok := c.take(real, kindLogin, now); !ok {
		t.Error("the real sign-in was pushed out by the flood")
	}
}
