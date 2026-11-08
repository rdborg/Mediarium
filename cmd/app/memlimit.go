package main

import (
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/rdborg/mediarium/internal/sysinfo"
)

// memoryLimitShare is how much of the container's memory limit the Go runtime
// aims to stay under. The rest is left for what the runtime does not count
// (the program itself, stacks, memory-mapped files).
const memoryLimitShare = 0.85

// minMemoryLimit stops a tiny or wrongly read limit from making the garbage
// collector run all the time.
const minMemoryLimit = 128 << 20

// applyMemoryLimit gives the garbage collector a soft memory limit that
// follows the container's own limit, so a container with a small limit
// (a NAS, a Raspberry Pi) cleans up before the kernel kills the process.
// GOMEMLIMIT, when set, wins and nothing changes. It returns the limit it set,
// or 0.
func applyMemoryLimit(root string) int64 {
	if os.Getenv("GOMEMLIMIT") != "" {
		return 0
	}
	limit, ok := sysinfo.ContainerMemoryLimit(root)
	if !ok {
		return 0
	}
	soft := int64(float64(limit) * memoryLimitShare)
	if soft < minMemoryLimit {
		return 0
	}
	debug.SetMemoryLimit(soft)
	slog.Info("memory: the garbage collector follows the container limit", "limitBytes", limit, "softLimitBytes", soft)
	return soft
}
