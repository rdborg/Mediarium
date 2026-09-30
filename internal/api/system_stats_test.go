package api_test

import (
	"net/http"
	"testing"
)

type statsAnswer struct {
	App struct {
		Goroutines int      `json:"goroutines"`
		CPUPercent *float64 `json:"cpuPercent"`
	} `json:"app"`
	OS      string `json:"os"`
	Storage *struct {
		UsedBytes  uint64 `json:"usedBytes"`
		FreeBytes  uint64 `json:"freeBytes"`
		TotalBytes uint64 `json:"totalBytes"`
	} `json:"storage"`
	Disks []struct {
		Label      string `json:"label"`
		FreeBytes  uint64 `json:"freeBytes"`
		TotalBytes uint64 `json:"totalBytes"`
		UsedBytes  uint64 `json:"usedBytes"`
	} `json:"disks"`
}

func TestSystemStatsReportsStorageOncePerDisk(t *testing.T) {
	_, base, client := loginNewServer(t)
	stats := getJSON[statsAnswer](t, client, base+"/api/system/stats")

	if stats.OS == "" || stats.App.Goroutines == 0 {
		t.Fatalf("the existing fields must stay: %+v", stats)
	}
	if len(stats.Disks) == 0 {
		t.Skip("no disk could be read in this environment")
	}
	if stats.Storage == nil {
		t.Fatalf("storage is missing although %d disks were listed", len(stats.Disks))
	}
	var sum, biggest uint64
	same := true
	for _, d := range stats.Disks {
		sum += d.TotalBytes
		biggest = max(biggest, d.TotalBytes)
		same = same && d.TotalBytes == stats.Disks[0].TotalBytes && d.FreeBytes == stats.Disks[0].FreeBytes
	}
	if stats.Storage.TotalBytes < biggest || stats.Storage.TotalBytes > sum || stats.Storage.UsedBytes+stats.Storage.FreeBytes != stats.Storage.TotalBytes {
		t.Fatalf("storage = %+v for disks %+v", stats.Storage, stats.Disks)
	}
	// The test folders share one temp directory, so listed disks that look
	// identical are one disk and must be counted once.
	if same && len(stats.Disks) > 1 && stats.Storage.TotalBytes != stats.Disks[0].TotalBytes {
		t.Fatalf("one disk listed %d times was counted %d times: %+v", len(stats.Disks), stats.Storage.TotalBytes/stats.Disks[0].TotalBytes, stats.Storage)
	}
}

func TestSystemStatsIsForAdministratorsOnly(t *testing.T) {
	_, base, _, member, _ := familyServer(t)
	if status, _ := doStatus(t, member, http.MethodGet, base+"/api/system/stats"); status != http.StatusForbidden {
		t.Fatalf("member: %d", status)
	}
}
