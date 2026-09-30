package sysinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// selfStat is a /proc/self/stat line: the comm field holds spaces and
// parentheses on purpose, utime and stime are fields 14 and 15.
func selfStat(utime, stime int) string {
	return fmt.Sprintf("42 (my (odd) app) S 1 42 42 0 -1 4194560 100 0 0 0 %d %d 0 0 20 0 5 0 1000 1000000 500 18446744073709551615\n", utime, stime)
}

// clockAt is a fake clock that reads t0 plus an offset the test moves.
type clockAt struct {
	t0  time.Time
	off time.Duration
}

func (c *clockAt) now() time.Time { return c.t0.Add(c.off) }

func fakeSampler(root string, ncpu int) (*Sampler, *clockAt) {
	c := &clockAt{t0: time.Unix(1_700_000_000, 0)}
	return &Sampler{root: root, now: c.now, nap: func(d time.Duration) { c.off += d }, ncpu: ncpu}, c
}

func TestSnapshotFromProc(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/meminfo", "MemTotal:       8000000 kB\nMemFree:         500000 kB\nMemAvailable:   6000000 kB\n")
	writeFile(t, root, "proc/loadavg", "0.50 0.40 0.30 1/100 12345\n")
	writeFile(t, root, "proc/uptime", "3600.50 7000.00\n")
	writeFile(t, root, "proc/stat", "cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 1 1 1 1 1 1 1 1 1 1\n")
	writeFile(t, root, "proc/self/stat", selfStat(150, 50))
	writeFile(t, root, "sys/fs/cgroup/memory.current", "123456789\n")
	writeFile(t, root, "sys/fs/cgroup/memory.max", "max\n")

	s, c := fakeSampler(root, 8)
	// The previous reading, 2 s ago: busy 200 of 1000 now against 100 of 900
	// before is +100 busy of +100 total; the process used 100 ticks (1 s) in 2 s.
	s.last = reading{at: c.now().Add(-2 * time.Second), host: cpuTimes{busy: 100, total: 900}, hostOK: true, self: 100, selfOK: true}
	s.hasLast = true
	snap := s.Snapshot()

	if snap.Memory == nil || snap.Memory.TotalBytes != 8000000*1024 || snap.Memory.UsedBytes != 2000000*1024 {
		t.Fatalf("memory = %+v", snap.Memory)
	}
	if len(snap.Load) != 3 || snap.Load[0] != 0.5 {
		t.Fatalf("load = %v", snap.Load)
	}
	if snap.UptimeSeconds != 3600.5 {
		t.Fatalf("uptime = %v", snap.UptimeSeconds)
	}
	if snap.CPU == nil || snap.CPU.Percent != 100 || snap.CPU.Cores != 8 {
		t.Fatalf("cpu = %+v", snap.CPU)
	}
	if snap.App.CPUPercent == nil || *snap.App.CPUPercent != 50 {
		t.Fatalf("app cpu = %v, want 50 (half of one core)", snap.App.CPUPercent)
	}
	if snap.App.MemoryBytes != 123456789 || snap.App.LimitBytes != 0 {
		t.Fatalf("app = %+v", snap.App)
	}
}

func TestSnapshotWithoutProc(t *testing.T) {
	s := &Sampler{root: t.TempDir(), nap: func(time.Duration) {}}
	snap := s.Snapshot()
	if snap.CPU != nil || snap.Memory != nil || snap.Load != nil || snap.App.CPUPercent != nil {
		t.Fatalf("no /proc: machine figures must be left out, got %+v", snap)
	}
	if snap.App.Goroutines == 0 {
		t.Fatal("the app's own figures are always there")
	}
}

// The first Snapshot samples for a short while instead of reporting nothing,
// and the second one, a while later, reports the change since the first.
func TestFirstSnapshotSamplesAndLaterOnesUseTheirWindow(t *testing.T) {
	root := t.TempDir()
	s, c := fakeSampler(root, 4)
	// The first sample is taken with the counters at 0 busy / 1000 total; by
	// the end of its 200 ms window the machine did 20 ticks of work in 100.
	writeFile(t, root, "proc/stat", "cpu  0 0 0 1000 0 0 0 0 0 0\n")
	s.nap = func(d time.Duration) {
		c.off += d
		writeFile(t, root, "proc/stat", "cpu  20 0 0 1080 0 0 0 0 0 0\n")
	}
	first := s.Snapshot()
	if first.CPU == nil || first.CPU.Percent != 20 {
		t.Fatalf("first sample: %+v", first.CPU)
	}

	// Ten seconds later the machine is 50% busy over that stretch.
	c.off += 10 * time.Second
	writeFile(t, root, "proc/stat", "cpu  70 0 0 1130 0 0 0 0 0 0\n")
	second := s.Snapshot()
	if second.CPU == nil || second.CPU.Percent != 50 {
		t.Fatalf("second sample: %+v", second.CPU)
	}

	// A request a moment after that shares the answer instead of computing a
	// meaningless figure over a few milliseconds.
	c.off += 50 * time.Millisecond
	writeFile(t, root, "proc/stat", "cpu  70 0 0 1230 0 0 0 0 0 0\n")
	third := s.Snapshot()
	if third.CPU == nil || third.CPU.Percent != 50 {
		t.Fatalf("a request inside the minimum window must repeat the last figure: %+v", third.CPU)
	}
	// And the window still runs from the second sample: a second later the
	// whole stretch is counted.
	c.off += time.Second
	writeFile(t, root, "proc/stat", "cpu  170 0 0 1230 0 0 0 0 0 0\n")
	fourth := s.Snapshot()
	if fourth.CPU == nil || fourth.CPU.Percent != 50 {
		t.Fatalf("fourth sample: %+v (100 busy of 200 since the second sample)", fourth.CPU)
	}
}

func TestBusyMachineReportsHighUseAndIdleOneReportsLow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stat  string
		want  float64
		wantN bool
	}{
		{"idle", "cpu  10 0 10 980 0 0 0 0 0 0\n", 2, true},
		{"busy", "cpu  900 0 90 10 0 0 0 0 0 0\n", 99, true},
		{"iowait is not busy", "cpu  0 0 0 100 900 0 0 0 0 0\n", 0, true},
		{"steal is busy", "cpu  0 0 0 500 0 0 0 500 0 0\n", 50, true},
		{"guest time is not counted twice", "cpu  500 0 0 500 0 0 0 0 500 0\n", 50, true},
		{"counters went backwards", "cpu  1 0 0 1 0 0 0 0 0 0\n", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			s, c := fakeSampler(root, 4)
			writeFile(t, root, "proc/stat", tc.stat)
			s.last = reading{at: c.now().Add(-5 * time.Second), host: cpuTimes{}, hostOK: true}
			// The previous reading is all zeros, so the figure is the share since boot of that line.
			s.last.host = cpuTimes{busy: 0, total: 0}
			s.hasLast = true
			if tc.name == "counters went backwards" {
				s.last.host = cpuTimes{busy: 50, total: 100}
			}
			got := s.Snapshot().CPU
			if (got != nil) != tc.wantN {
				t.Fatalf("cpu = %+v", got)
			}
			if got != nil && got.Percent != tc.want {
				t.Fatalf("percent = %v, want %v", got.Percent, tc.want)
			}
		})
	}
}

func TestContainerWithACPUQuotaReportsItsOwnUse(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		ncpu  int
		want  float64
		cores int
		host  bool // reports the machine figure instead
	}{
		{"cgroup v2, 1.5 cores, 1 s of CPU in 2 s",
			map[string]string{"sys/fs/cgroup/cpu.max": "150000 100000\n", "sys/fs/cgroup/cpu.stat": "usage_usec 2000000\nuser_usec 1500000\nsystem_usec 500000\n"}, 8, 33.3, 2, false},
		{"cgroup v2, fully used quota", // 1 core allowed, 2 s used in 2 s
			map[string]string{"sys/fs/cgroup/cpu.max": "100000 100000\n", "sys/fs/cgroup/cpu.stat": "usage_usec 3000000\n"}, 4, 100, 1, false},
		{"cgroup v2, nothing used",
			map[string]string{"sys/fs/cgroup/cpu.max": "200000 100000\n", "sys/fs/cgroup/cpu.stat": "usage_usec 1000000\n"}, 4, 0, 2, false},
		{"cgroup v2, no quota uses the machine",
			map[string]string{"sys/fs/cgroup/cpu.max": "max 100000\n", "sys/fs/cgroup/cpu.stat": "usage_usec 3000000\n"}, 4, 75, 4, true},
		{"cgroup v2, quota bigger than the machine is no limit",
			map[string]string{"sys/fs/cgroup/cpu.max": "800000 100000\n", "sys/fs/cgroup/cpu.stat": "usage_usec 3000000\n"}, 4, 75, 4, true},
		{"cgroup v1, 2 cores, 2 s of CPU in 2 s",
			map[string]string{"sys/fs/cgroup/cpu/cpu.cfs_quota_us": "200000\n", "sys/fs/cgroup/cpu/cpu.cfs_period_us": "100000\n", "sys/fs/cgroup/cpuacct/cpuacct.usage": "3000000000\n"}, 8, 50, 2, false},
		{"cgroup v1, quota -1 uses the machine",
			map[string]string{"sys/fs/cgroup/cpu/cpu.cfs_quota_us": "-1\n", "sys/fs/cgroup/cpu/cpu.cfs_period_us": "100000\n", "sys/fs/cgroup/cpuacct/cpuacct.usage": "3000000000\n"}, 8, 75, 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			// The machine as a whole is 75% busy in every case.
			writeFile(t, root, "proc/stat", "cpu  300 0 0 100 0 0 0 0 0 0\n")
			for rel, body := range tc.files {
				writeFile(t, root, rel, body)
			}
			s, c := fakeSampler(root, tc.ncpu)
			// Two seconds ago the container had used 1 s (v2 usage_usec 1000000).
			s.last = reading{at: c.now().Add(-2 * time.Second), host: cpuTimes{}, hostOK: true, cg: cgroupCPU{usageUsec: 1_000_000, cores: 1}, cgOK: true}
			s.hasLast = true
			got := s.Snapshot().CPU
			if got == nil || got.Percent != tc.want || got.Cores != tc.cores {
				t.Fatalf("cpu = %+v, want %v%% of %d", got, tc.want, tc.cores)
			}
		})
	}
}

func TestSelfTicksSurviveOddProcessNames(t *testing.T) {
	root := t.TempDir()
	s := &Sampler{root: root}
	writeFile(t, root, "proc/self/stat", selfStat(7, 5))
	if n, ok := s.readSelfTicks(); !ok || n != 12 {
		t.Fatalf("ticks = %d, %v", n, ok)
	}
	writeFile(t, root, "proc/self/stat", "garbage")
	if _, ok := s.readSelfTicks(); ok {
		t.Fatal("garbage must not be read as a number")
	}
	writeFile(t, root, "proc/self/stat", "1 (x) S 1 2\n")
	if _, ok := s.readSelfTicks(); ok {
		t.Fatal("a short line must not be read")
	}
}
