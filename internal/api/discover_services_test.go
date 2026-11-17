package api_test

import (
	"net/http"
	"reflect"
	"testing"
)

// The streaming services picked on Discover belong to the account: they are
// kept on the server, start as "never saved", and one person's pick does not
// reach another's.
func TestDiscoverServicesFollowTheAccount(t *testing.T) {
	_, base, admin, sam, _ := familyServer(t)
	url := base + "/api/auth/discover-services"

	first := getJSON[map[string]any](t, sam, url)
	if first["saved"] != false || first["account"] != true || first["on"] != false {
		t.Fatalf("a new account: %v", first)
	}

	put := func(c *http.Client, body map[string]any, want int) map[string]any {
		return postJSONMethod[map[string]any](t, c, http.MethodPut, url, body, want)
	}
	got := put(sam, map[string]any{"services": []string{"disney", "netflix", "disney"}, "on": true}, http.StatusOK)
	if got["saved"] != true || got["on"] != true || !reflect.DeepEqual(got["services"], []any{"disney", "netflix"}) {
		t.Fatalf("after saving: %v", got)
	}
	if again := getJSON[map[string]any](t, sam, url); !reflect.DeepEqual(again, got) {
		t.Errorf("read back %v, saved %v", again, got)
	}
	if other := getJSON[map[string]any](t, admin, url); other["saved"] != false {
		t.Errorf("another account sees it: %v", other)
	}

	// The switch cannot be on with nothing picked, and an empty pick is still a saved choice.
	cleared := put(sam, map[string]any{"services": []string{}, "on": true}, http.StatusOK)
	if cleared["saved"] != true || cleared["on"] != false || len(cleared["services"].([]any)) != 0 {
		t.Errorf("after clearing: %v", cleared)
	}

	put(sam, map[string]any{"services": []string{"netflix", "nope"}}, http.StatusBadRequest)
	if kept := getJSON[map[string]any](t, sam, url); kept["saved"] != true {
		t.Errorf("a bad request changed the saved choice: %v", kept)
	}
}
