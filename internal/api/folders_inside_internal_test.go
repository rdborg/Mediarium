package api

import "testing"

func TestInsideWorthChecking(t *testing.T) {
	cases := map[string]bool{
		"/": false, "/proc": false, "/proc/1": false, "/etc": false, "/sys/fs": false, "relative/path": false,
		"/movies": true, "/data/media/tv": true, "/volume1/video": true, "/mnt/user/media": true, "/tmp/test": true,
	}
	for path, want := range cases {
		if got := insideWorthChecking(path); got != want {
			t.Errorf("insideWorthChecking(%q) = %v, want %v", path, got, want)
		}
	}
}
