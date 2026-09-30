package sysinfo

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestLiveSampler prints readings from the real /proc for a while. It only
// runs with SYSINFO_LIVE=1, inside a Linux container, for manual load tests:
// SYSINFO_SPIN=n keeps n goroutines busy so the process's own CPU use shows.
func TestLiveSampler(t *testing.T) {
	if os.Getenv("SYSINFO_LIVE") == "" {
		t.Skip("set SYSINFO_LIVE=1 to sample the real system")
	}
	spin, _ := strconv.Atoi(os.Getenv("SYSINFO_SPIN"))
	stop := make(chan struct{})
	defer close(stop)
	for i := 0; i < spin; i++ {
		go func() {
			for x := 0; ; x++ {
				select {
				case <-stop:
					return
				default:
				}
			}
		}()
	}
	s := New()
	for i := 0; i < 6; i++ {
		start := time.Now()
		snap := s.Snapshot()
		cpu, app := "n/a", "n/a"
		if snap.CPU != nil {
			cpu = fmt.Sprintf("%.1f%% of %d cores", snap.CPU.Percent, snap.CPU.Cores)
		}
		if snap.App.CPUPercent != nil {
			app = fmt.Sprintf("%.1f%% of one core", *snap.App.CPUPercent)
		}
		fmt.Printf("sample %d (took %v): cpu %s, this process %s\n", i, time.Since(start).Round(time.Millisecond), cpu, app)
		time.Sleep(2 * time.Second)
	}
}
