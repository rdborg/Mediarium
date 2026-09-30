package api

import "testing"

// TestTorrentRegistrySharesOneEngine proves every torrent on the same port
// runs in one engine (one listening socket), and that the engine closes with
// its last torrent.
func TestTorrentRegistrySharesOneEngine(t *testing.T) {
	var r torrentRegistry
	root := t.TempDir()
	cancelled := map[int64]bool{}
	start := func(id int64) *torrentJob {
		job, err := r.start(id, root+"/queue", root, 0, nil, func() { cancelled[id] = true })
		if err != nil {
			t.Fatalf("start %d: %v", id, err)
		}
		return job
	}

	a, b := start(1), start(2)
	if a.engine != b.engine {
		t.Fatal("two torrents on the same port must share one engine")
	}
	st := r.status()
	if !st.Listening || st.Port == 0 || st.Torrents != 2 {
		t.Fatalf("status with two torrents = %+v", st)
	}
	if !r.active(1) || !r.activeDir(root+"/queue/") {
		t.Fatal("running torrents should be reported as active")
	}

	r.stop(a)
	r.stop(a) // twice is harmless
	if !cancelled[1] || cancelled[2] {
		t.Fatalf("stop must cancel only its own torrent: %v", cancelled)
	}
	if st := r.status(); !st.Listening || st.Torrents != 1 {
		t.Fatalf("the engine must keep running for the other torrent: %+v", st)
	}

	if n := r.stopAll(); n != 1 || !cancelled[2] {
		t.Fatalf("stopAll stopped %d, cancelled %v", n, cancelled)
	}
	if st := r.status(); st.Listening || st.Torrents != 0 || len(r.engines) != 0 {
		t.Fatalf("the engine must close with its last torrent: %+v", st)
	}

	// A torrent that was stopped while it was being added is dropped.
	c := start(3)
	r.stop(c)
	if r.attach(c, nil) {
		t.Fatal("attach after stop must report false")
	}
}
