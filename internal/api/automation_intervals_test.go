package api_test

import (
	"net/http"
	"testing"
)

func TestAutomationIntervalSettings(t *testing.T) {
	_, base, client := loginNewServer(t)
	read := func(key string) any { return getJSON[map[string]any](t, client, base+"/api/settings")[key] }
	put := func(key string, v any, want int) map[string]any {
		return postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{key: v}, want)
	}

	if read("huntIntervalHours") != float64(6) || read("releaseCheckMinutes") != float64(15) {
		t.Fatalf("defaults should be 6 hours and 15 minutes, got %v and %v", read("huntIntervalHours"), read("releaseCheckMinutes"))
	}

	for _, tc := range []struct {
		name, key string
		value     int
		status    int
	}{
		{"hours in range", "huntIntervalHours", 12, http.StatusOK},
		{"hours at the top", "huntIntervalHours", 168, http.StatusOK},
		{"hours zero", "huntIntervalHours", 0, http.StatusBadRequest},
		{"hours too many", "huntIntervalHours", 169, http.StatusBadRequest},
		{"hours negative", "huntIntervalHours", -3, http.StatusBadRequest},
		{"minutes in range", "releaseCheckMinutes", 30, http.StatusOK},
		{"minutes at the bottom", "releaseCheckMinutes", 5, http.StatusOK},
		{"minutes too few", "releaseCheckMinutes", 4, http.StatusBadRequest},
		{"minutes too many", "releaseCheckMinutes", 1441, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := read(tc.key)
			out := put(tc.key, tc.value, tc.status)
			if tc.status == http.StatusOK {
				if out[tc.key] != float64(tc.value) || read(tc.key) != float64(tc.value) {
					t.Fatalf("%s should read back as %d, got %v", tc.key, tc.value, out[tc.key])
				}
				return
			}
			if read(tc.key) != before {
				t.Fatalf("a rejected value must not change %s: was %v, now %v", tc.key, before, read(tc.key))
			}
		})
	}
}
