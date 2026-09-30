package api

import (
	"net/http"
	"sync"

	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/sysinfo"
)

// sysinfoSampler is created on first use, so the zero Server works.
type sysinfoSampler struct {
	once sync.Once
	s    *sysinfo.Sampler
}

func (x *sysinfoSampler) Snapshot() sysinfo.Snapshot {
	x.once.Do(func() { x.s = sysinfo.New() })
	return x.s.Snapshot()
}

// statsDisk is the space on the disk holding one of Mediarium's folders.
type statsDisk struct {
	Label      string `json:"label"` // "Downloads", "Movies", "TV", "Settings and database"
	Path       string `json:"path"`
	FreeBytes  uint64 `json:"freeBytes"`
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}

// statsStorage is the space on all the disks Mediarium uses together, each
// disk counted once however many of the folders are on it.
type statsStorage struct {
	UsedBytes  uint64 `json:"usedBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

type statsPayload struct {
	sysinfo.Snapshot
	Disks   []statsDisk   `json:"disks"`
	Storage *statsStorage `json:"storage,omitempty"`
}

// sumStorage adds up the space of the volumes holding paths, counting each
// volume once however many of the paths are on it. keys gives the identities
// of the volume holding a path (device number, filesystem id, matching space
// figures: see fsinfo.SameVolumeKeys); paths sharing any key are one volume.
// A path with no keys counts as its own; one whose space cannot be read is
// skipped. It returns nil when nothing could be read.
func sumStorage(paths []string, keys func(string, fsinfo.Usage) []string, usage func(string) (fsinfo.Usage, error)) *statsStorage {
	seen := map[string]bool{}
	var out statsStorage
	found := false
	for _, p := range paths {
		if p == "" {
			continue
		}
		u, err := usage(p)
		if err != nil || u.TotalBytes == 0 {
			continue
		}
		ks := keys(p, u)
		if len(ks) == 0 {
			ks = []string{"path:" + p}
		}
		duplicate := false
		for _, k := range ks {
			if seen[k] {
				duplicate = true
			}
		}
		for _, k := range ks {
			seen[k] = true
		}
		if duplicate {
			continue
		}
		found = true
		out.TotalBytes += u.TotalBytes
		out.FreeBytes += u.FreeBytes
		if u.TotalBytes > u.FreeBytes {
			out.UsedBytes += u.TotalBytes - u.FreeBytes
		}
	}
	if !found {
		return nil
	}
	return &out
}

// handleSystemStats reports how busy the server is (CPU, memory, load,
// uptime, and Mediarium's own CPU as app.cpuPercent), the free space for each
// folder Mediarium uses, and the space on all the disks together with each
// counted once (storage). Admin only: it shows folder paths.
func (s *Server) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	snap := s.stats.Snapshot()
	type folder struct{ label, path string }
	folders := []folder{
		{"Downloads", s.downloadsRoot()},
		{"Movies", s.moviesRoot()},
		{"TV", s.tvRoot()},
	}
	if s.musicEnabled() {
		folders = append(folders, folder{"Music", s.musicRoot()})
	}
	folders = append(folders, folder{"Settings and database", s.cfg.ConfigDir})
	disks := []statsDisk{}
	paths := make([]string, 0, len(folders))
	for _, f := range folders {
		if f.path == "" {
			continue
		}
		u, err := fsinfo.DiskUsage(f.path)
		if err != nil || u.TotalBytes == 0 {
			continue
		}
		used := uint64(0)
		if u.TotalBytes > u.FreeBytes {
			used = u.TotalBytes - u.FreeBytes
		}
		disks = append(disks, statsDisk{Label: f.label, Path: f.path, FreeBytes: u.FreeBytes, TotalBytes: u.TotalBytes, UsedBytes: used})
		paths = append(paths, f.path)
	}
	writeJSON(w, http.StatusOK, statsPayload{Snapshot: snap, Disks: disks, Storage: sumStorage(paths, fsinfo.VolumeKeys, fsinfo.DiskUsage)})
}
