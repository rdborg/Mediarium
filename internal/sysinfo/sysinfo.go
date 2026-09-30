// Package sysinfo reports how busy the server is: CPU use, memory, load and
// uptime of the machine Mediarium runs on, and Mediarium's own memory and CPU.
// It reads Linux's /proc and cgroup files (what a Docker container or NAS
// sees); on other systems the machine figures are left out.
package sysinfo

import (
	"bufio"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CPU is the share of CPU time in use since the previous sample. Normally
// that is the whole machine (all cores together: 100 means every core busy).
// In a container limited to fewer cores than the machine has (Docker --cpus,
// a NAS app limit) it is the container's own use of that allowance and Cores
// is the allowance rounded up.
type CPU struct {
	Percent float64 `json:"percent"`
	Cores   int     `json:"cores"`
}

// Memory is the machine's memory: in use (not counting reclaimable cache)
// and total.
type Memory struct {
	UsedBytes  uint64 `json:"usedBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

// App is Mediarium's own share of the machine.
type App struct {
	MemoryBytes   uint64  `json:"memoryBytes"`          // the container's memory in use when known, else the Go heap
	LimitBytes    uint64  `json:"limitBytes,omitempty"` // the container's memory limit, when one is set
	UptimeSeconds float64 `json:"uptimeSeconds"`
	Goroutines    int     `json:"goroutines"`
	// CPUPercent is Mediarium's own CPU use as a share of ONE core: 100 means
	// one core fully busy, 250 two and a half cores. Absent where the system
	// does not expose it.
	CPUPercent *float64 `json:"cpuPercent,omitempty"`
}

// Snapshot is one reading. CPU, Memory, Load and UptimeSeconds are empty when
// the system does not expose them.
type Snapshot struct {
	CPU           *CPU      `json:"cpu,omitempty"`
	Memory        *Memory   `json:"memory,omitempty"`
	Load          []float64 `json:"load,omitempty"` // 1, 5 and 15 minute load averages
	UptimeSeconds float64   `json:"uptimeSeconds,omitempty"`
	App           App       `json:"app"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
}

const (
	// firstWindow is how long the very first Snapshot samples for.
	firstWindow = 200 * time.Millisecond
	// minWindow is the shortest stretch a reading is computed over. Two
	// requests closer together than this share the previous result: over a
	// few milliseconds the kernel counters (10 ms ticks) say nothing.
	minWindow = time.Second
	// clockTicks is USER_HZ, the unit of the times in /proc/<pid>/stat. It is
	// 100 on every Linux Mediarium runs on.
	clockTicks = 100
)

// Sampler remembers the previous readings, so each Snapshot reports CPU use
// since the last one (the first call takes a short sample).
type Sampler struct {
	mu      sync.Mutex
	started time.Time
	root    string // "/" normally; tests point it at a fake tree

	now  func() time.Time    // time.Now; tests replace it
	nap  func(time.Duration) // time.Sleep; tests replace it
	ncpu int                 // runtime.NumCPU(); tests replace it

	last      reading
	hasLast   bool
	cached    cpuResult
	hasCached bool
}

// New returns a Sampler reading the real system.
func New() *Sampler { return &Sampler{started: time.Now(), root: "/"} }

type cpuTimes struct{ busy, total uint64 }

// reading is one look at every CPU counter.
type reading struct {
	at     time.Time
	host   cpuTimes // /proc/stat, all cores of the machine
	hostOK bool
	cg     cgroupCPU
	cgOK   bool
	self   uint64 // this process's user+system time in clock ticks
	selfOK bool
}

// cgroupCPU is the container's CPU allowance and the time it has used.
type cgroupCPU struct {
	usageUsec uint64
	cores     float64 // the quota as a number of cores
}

// cpuResult is what a window of two readings comes to.
type cpuResult struct {
	cpu    *CPU
	appPct *float64
}

func (s *Sampler) path(p string) string { return strings.TrimSuffix(s.root, "/") + p }

func (s *Sampler) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Sampler) numCPU() int {
	if s.ncpu > 0 {
		return s.ncpu
	}
	return runtime.NumCPU()
}

func (s *Sampler) sleep(d time.Duration) {
	if s.nap != nil {
		s.nap(d)
		return
	}
	time.Sleep(d)
}

// Snapshot takes a reading.
func (s *Sampler) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{OS: runtime.GOOS, Arch: runtime.GOARCH}

	cur := s.read()
	first := !s.hasLast
	if first {
		s.last, s.hasLast = cur, true
		s.sleep(firstWindow)
		cur = s.read()
	}
	if first || cur.at.Sub(s.last.at) >= minWindow {
		s.cached, s.hasCached = compute(s.last, cur, s.numCPU()), true
		s.last = cur
	}
	if s.hasCached {
		snap.CPU = s.cached.cpu
	}
	snap.Memory = s.readMemory()
	snap.Load = s.readLoad()
	snap.UptimeSeconds = s.readUptime()

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	snap.App = App{MemoryBytes: ms.Sys, UptimeSeconds: time.Since(s.started).Seconds(), Goroutines: runtime.NumGoroutine()}
	if s.hasCached {
		snap.App.CPUPercent = s.cached.appPct
	}
	if used, ok := readUint(s.path("/sys/fs/cgroup/memory.current")); ok {
		snap.App.MemoryBytes = used
	} else if used, ok := readUint(s.path("/sys/fs/cgroup/memory/memory.usage_in_bytes")); ok {
		snap.App.MemoryBytes = used
	}
	if limit, ok := readUint(s.path("/sys/fs/cgroup/memory.max")); ok && (snap.Memory == nil || limit < snap.Memory.TotalBytes) {
		snap.App.LimitBytes = limit
	}
	return snap
}

// read takes every CPU counter at once.
func (s *Sampler) read() reading {
	r := reading{at: s.clock()}
	r.host, r.hostOK = s.readCPU()
	r.cg, r.cgOK = s.readCgroupCPU()
	r.self, r.selfOK = s.readSelfTicks()
	return r
}

// compute turns two readings into CPU percentages. The machine figure uses
// /proc/stat; a container limited to fewer cores than the machine has uses
// its own cgroup time against that limit instead, because /proc/stat there
// still describes the whole host and would hide the container's own load.
func compute(prev, cur reading, ncpu int) cpuResult {
	var res cpuResult
	elapsed := cur.at.Sub(prev.at)
	if elapsed <= 0 {
		return res
	}
	switch {
	case prev.cgOK && cur.cgOK && cur.cg.cores > 0 && cur.cg.cores < float64(ncpu) && cur.cg.usageUsec >= prev.cg.usageUsec:
		used := float64(cur.cg.usageUsec - prev.cg.usageUsec)
		pct := used / (float64(elapsed.Microseconds()) * cur.cg.cores) * 100
		res.cpu = &CPU{Percent: round1(clampPercent(pct)), Cores: int(math.Ceil(cur.cg.cores))}
	case prev.hostOK && cur.hostOK && cur.host.total > prev.host.total && cur.host.busy >= prev.host.busy:
		pct := float64(cur.host.busy-prev.host.busy) / float64(cur.host.total-prev.host.total) * 100
		res.cpu = &CPU{Percent: round1(clampPercent(pct)), Cores: ncpu}
	}
	if prev.selfOK && cur.selfOK && cur.self >= prev.self {
		v := round1(float64(cur.self-prev.self) / clockTicks / elapsed.Seconds() * 100)
		res.appPct = &v
	}
	return res
}

func clampPercent(p float64) float64 { return math.Max(0, math.Min(100, p)) }

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// readCgroupCPU reads the container CPU quota and the CPU time it has used.
// ok is false when there is no quota (or no cgroup files), and then the
// machine-wide figure is the one to report.
func (s *Sampler) readCgroupCPU() (cgroupCPU, bool) {
	// cgroup v2: cpu.max is "<quota> <period>" or "max <period>".
	if b, err := os.ReadFile(s.path("/sys/fs/cgroup/cpu.max")); err == nil {
		f := strings.Fields(string(b))
		if len(f) == 2 && f[0] != "max" {
			quota, err1 := strconv.ParseUint(f[0], 10, 64)
			period, err2 := strconv.ParseUint(f[1], 10, 64)
			usage, ok := s.cgroupStatUsage("/sys/fs/cgroup/cpu.stat")
			if err1 == nil && err2 == nil && period > 0 && quota > 0 && ok {
				return cgroupCPU{usageUsec: usage, cores: float64(quota) / float64(period)}, true
			}
		}
		return cgroupCPU{}, false
	}
	// cgroup v1: quota and period in cpu.cfs_*_us (-1 = no quota), time in
	// cpuacct.usage in nanoseconds.
	for _, dir := range []string{"/sys/fs/cgroup/cpu", "/sys/fs/cgroup/cpu,cpuacct"} {
		quota, ok1 := readInt(s.path(dir + "/cpu.cfs_quota_us"))
		period, ok2 := readInt(s.path(dir + "/cpu.cfs_period_us"))
		if !ok1 || !ok2 || quota <= 0 || period <= 0 {
			continue
		}
		for _, acct := range []string{dir + "/cpuacct.usage", "/sys/fs/cgroup/cpuacct/cpuacct.usage", "/sys/fs/cgroup/cpu,cpuacct/cpuacct.usage"} {
			if ns, ok := readUint(s.path(acct)); ok {
				return cgroupCPU{usageUsec: ns / 1000, cores: float64(quota) / float64(period)}, true
			}
		}
	}
	return cgroupCPU{}, false
}

// cgroupStatUsage reads usage_usec out of a cgroup v2 cpu.stat file.
func (s *Sampler) cgroupStatUsage(rel string) (uint64, bool) {
	b, err := os.ReadFile(s.path(rel))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "usage_usec" {
			n, err := strconv.ParseUint(f[1], 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// readSelfTicks is this process's user plus system CPU time in clock ticks:
// fields 14 and 15 of /proc/self/stat, counted after the "(name)" field,
// which may itself contain spaces and parentheses.
func (s *Sampler) readSelfTicks() (uint64, bool) {
	b, err := os.ReadFile(s.path("/proc/self/stat"))
	if err != nil {
		return 0, false
	}
	str := string(b)
	i := strings.LastIndexByte(str, ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(str[i+1:]) // f[0] is the state, field 3 of the file
	if len(f) < 13 {
		return 0, false
	}
	utime, err1 := strconv.ParseUint(f[11], 10, 64)
	stime, err2 := strconv.ParseUint(f[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return utime + stime, true
}

// readCPU sums the first "cpu" line of /proc/stat: busy = everything but
// idle and iowait. The guest columns are already inside user and nice, so
// they are not added again.
func (s *Sampler) readCPU() (cpuTimes, bool) {
	f, err := os.Open(s.path("/proc/stat"))
	if err != nil {
		return cpuTimes{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, v := range fields[1:] {
			if i >= 8 { // user nice system idle iowait irq softirq steal
				break
			}
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return cpuTimes{}, false
			}
			t.total += n
			if i != 3 && i != 4 { // idle, iowait
				t.busy += n
			}
		}
		return t, true
	}
	return cpuTimes{}, false
}

func (s *Sampler) readMemory() *Memory {
	f, err := os.Open(s.path("/proc/meminfo"))
	if err != nil {
		return nil
	}
	defer f.Close()
	vals := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		vals[strings.TrimSuffix(fields[0], ":")] = n * 1024 // kB
	}
	total, ok := vals["MemTotal"]
	if !ok || total == 0 {
		return nil
	}
	avail, ok := vals["MemAvailable"]
	if !ok {
		avail = vals["MemFree"] + vals["Buffers"] + vals["Cached"]
	}
	if avail > total {
		avail = total
	}
	return &Memory{UsedBytes: total - avail, TotalBytes: total}
}

func (s *Sampler) readLoad() []float64 {
	b, err := os.ReadFile(s.path("/proc/loadavg"))
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return nil
	}
	out := make([]float64, 0, 3)
	for _, v := range fields[:3] {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func (s *Sampler) readUptime() float64 {
	b, err := os.ReadFile(s.path("/proc/uptime"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	n, _ := strconv.ParseFloat(fields[0], 64)
	return n
}

// readInt reads a file holding one (possibly negative) number.
func readInt(path string) (int64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// readUint reads a file holding one number ("max" or anything else means
// unknown).
func readUint(path string) (uint64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
