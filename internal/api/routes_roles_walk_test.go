package api_test

import (
	"net/http"
	"sort"
	"testing"
)

// Walks the whole route table with the two callers viewer_test.go does not
// cover: someone who is not signed in at all, and a basic user (a member).
// Neither may get through to a route that is not theirs, whatever the
// handler behind it does.
func TestAnonymousAndMemberCannotReachWhatIsNotTheirs(t *testing.T) {
	e := newSignInEnv(t, nil)
	e.addAccount("mia", "member")
	member, status, _ := e.login("mia", "long-enough-password")
	if status != http.StatusOK {
		t.Fatalf("member sign-in: %d", status)
	}
	anon := e.client()

	routes := e.server.TestRoutes()
	if len(routes) < 200 {
		t.Fatalf("only %d routes found, the walk is not seeing the table", len(routes))
	}
	patterns := make([]string, 0, len(routes))
	for p := range routes {
		patterns = append(patterns, p)
	}
	sort.Strings(patterns)

	var checkedAnon, checkedMember int
	for _, pattern := range patterns {
		method, path := split(pattern)

		// Nobody signed in: every route in the table needs an account.
		status, out := e.call(anon, method, path, map[string]any{})
		if status != http.StatusUnauthorized {
			t.Errorf("without signing in, %s answered %d instead of 401: %v", pattern, status, out["_raw"])
		}
		checkedAnon++

		// A basic user: nothing meant for administrators.
		if routes[pattern] == "admin" {
			status, out := e.call(member, method, path, map[string]any{})
			if status != http.StatusForbidden {
				t.Errorf("a basic user got through to %s (%d): %v", pattern, status, out["_raw"])
			}
			checkedMember++
		}
	}
	t.Logf("refused %d signed-out calls and %d basic-user calls across %d routes", checkedAnon, checkedMember, len(patterns))
	if checkedAnon < 200 || checkedMember < 150 {
		t.Errorf("the walk checked too little: %d, %d", checkedAnon, checkedMember)
	}
}
