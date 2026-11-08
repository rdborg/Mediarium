package api

import (
	"testing"
	"time"
)

// The background jobs do not all start in the same second: the ones that move
// downloads and find releases go first, the jobs that walk the library's
// files go last, and every job keeps its own interval.
func TestAutomationJobsStartOneAfterTheOther(t *testing.T) {
	s, _, _ := bigServer(t)
	sched := s.StartAutomation()
	defer sched.Stop()
	jobs := sched.Jobs()
	if len(jobs) < 20 {
		t.Fatalf("only %d jobs", len(jobs))
	}

	delay := map[string]time.Duration{}
	seen := map[time.Duration]string{}
	for i, j := range jobs {
		want := startupDelay + time.Duration(i)*startupStep
		if j.InitialDelay != want {
			t.Errorf("job %d %q first runs after %v, want %v", i, j.Name, j.InitialDelay, want)
		}
		if other, dup := seen[j.InitialDelay]; dup {
			t.Errorf("%q and %q start at the same moment", j.Name, other)
		}
		seen[j.InitialDelay] = j.Name
		delay[j.Name] = j.InitialDelay
		if j.Interval <= 0 {
			t.Errorf("%q lost its interval", j.Name)
		}
	}

	// Nothing starts before the pages have had their minute, and all of it is
	// under way within about seven minutes.
	var last time.Duration
	for _, d := range delay {
		if d < startupDelay {
			t.Errorf("a job starts before the startup delay: %v", d)
		}
		last = max(last, d)
	}
	if last > 7*time.Minute {
		t.Errorf("the last job first runs after %v, want within 7 minutes", last)
	}

	// Downloads, the release feeds and the hunt come before everything else.
	for _, name := range []string{"download-line", "rss-sync", "hunt"} {
		if delay[name] > startupDelay+2*startupStep {
			t.Errorf("%q starts at %v, want it among the first three", name, delay[name])
		}
	}
	// The jobs that read the library's files come after the web-facing ones.
	for _, name := range []string{"media-info", "subtitle-sweep", "import-details"} {
		if delay[name] <= delay["series-refresh"] || delay[name] <= delay["cleanup"] {
			t.Errorf("%q (%v) should start after the refresh and bookkeeping jobs", name, delay[name])
		}
	}

	// The same schedule every start.
	s2, _, _ := bigServer(t)
	sched2 := s2.StartAutomation()
	defer sched2.Stop()
	for i, j := range sched2.Jobs() {
		if j.Name != jobs[i].Name || j.InitialDelay != jobs[i].InitialDelay {
			t.Errorf("slot %d differs between starts: %q/%v and %q/%v", i, jobs[i].Name, jobs[i].InitialDelay, j.Name, j.InitialDelay)
		}
	}
}
