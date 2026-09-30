package api

import (
	"errors"
	"testing"

	"github.com/rdborg/mediarium/internal/fsinfo"
)

// fakeVolume is what the system says about the filesystem holding a folder.
type fakeVolume struct {
	ids   []string // device number and filesystem id, when the system gives them
	usage fsinfo.Usage
}

func fakeStat(vols map[string]fakeVolume, bySpace bool) (func(string, fsinfo.Usage) []string, func(string) (fsinfo.Usage, error)) {
	keys := func(p string, u fsinfo.Usage) []string {
		return fsinfo.SameVolumeKeys(vols[p].ids, u, bySpace)
	}
	usage := func(p string) (fsinfo.Usage, error) {
		if v, ok := vols[p]; ok {
			return v.usage, nil
		}
		return fsinfo.Usage{}, errors.New("no such folder")
	}
	return keys, usage
}

const tb = uint64(1) << 40

func TestSumStorage(t *testing.T) {
	volume := fsinfo.Usage{TotalBytes: 7670 * (tb / 1000), FreeBytes: 3000 * (tb / 1000)}
	volumeUsed := volume.TotalBytes - volume.FreeBytes

	tests := []struct {
		name  string
		paths []string
		vols  map[string]fakeVolume
		want  *statsStorage
	}{
		{
			name:  "Btrfs subvolumes on one Synology volume, every folder a different device",
			paths: []string{"/data/downloads", "/data/Movies", "/data/tv", "/data/Music", "/config"},
			vols: map[string]fakeVolume{
				"/data/downloads": {ids: []string{"dev:70", "fsid:a-1"}, usage: volume},
				"/data/Movies":    {ids: []string{"dev:71", "fsid:a-2"}, usage: volume},
				"/data/tv":        {ids: []string{"dev:72", "fsid:a-3"}, usage: volume},
				"/data/Music":     {ids: []string{"dev:73", "fsid:a-4"}, usage: volume},
				"/config":         {ids: []string{"dev:74", "fsid:a-5"}, usage: volume},
			},
			want: &statsStorage{TotalBytes: volume.TotalBytes, FreeBytes: volume.FreeBytes, UsedBytes: volumeUsed},
		},
		{
			name:  "two different disks add up",
			paths: []string{"/data", "/backup"},
			vols: map[string]fakeVolume{
				"/data":   {ids: []string{"dev:1"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 100}},
				"/backup": {ids: []string{"dev:2"}, usage: fsinfo.Usage{TotalBytes: 200, FreeBytes: 30}},
			},
			want: &statsStorage{TotalBytes: 1200, FreeBytes: 130, UsedBytes: 1070},
		},
		{
			name:  "same size disks with different free space stay separate",
			paths: []string{"/a", "/b"},
			vols: map[string]fakeVolume{
				"/a": {ids: []string{"dev:1"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 100}},
				"/b": {ids: []string{"dev:2"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 400}},
			},
			want: &statsStorage{TotalBytes: 2000, FreeBytes: 500, UsedBytes: 1500},
		},
		{
			name:  "a bind mount of a subfolder shares the device",
			paths: []string{"/data", "/data/tv-bind"},
			vols: map[string]fakeVolume{
				"/data":         {ids: []string{"dev:9", "fsid:c-1"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 100}},
				"/data/tv-bind": {ids: []string{"dev:9", "fsid:c-1"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 100}},
			},
			want: &statsStorage{TotalBytes: 1000, FreeBytes: 100, UsedBytes: 900},
		},
		{
			name:  "one shared id is enough even when the space differs a little",
			paths: []string{"/a", "/b"},
			vols: map[string]fakeVolume{
				"/a": {ids: []string{"dev:1", "fsid:x-1"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 100}},
				"/b": {ids: []string{"dev:2", "fsid:x-1"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 101}},
			},
			want: &statsStorage{TotalBytes: 1000, FreeBytes: 100, UsedBytes: 900},
		},
		{
			name:  "unreadable and empty paths are skipped",
			paths: []string{"", "/gone", "/data"},
			vols: map[string]fakeVolume{
				"/data": {ids: []string{"dev:1"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 100}},
			},
			want: &statsStorage{TotalBytes: 1000, FreeBytes: 100, UsedBytes: 900},
		},
		{
			name:  "nothing readable gives no figure",
			paths: []string{"", "/gone"},
			vols:  map[string]fakeVolume{},
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			keys, usage := fakeStat(tc.vols, true)
			got := sumStorage(tc.paths, keys, usage)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("storage = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Where the space figures are not compared (Windows drive letters), folders
// with no identity of their own each count once and are never merged.
func TestSumStorageWithoutSpaceMatching(t *testing.T) {
	vols := map[string]fakeVolume{
		"D:/media":  {ids: []string{"vol:D:"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 100}},
		"D:/tv":     {ids: []string{"vol:D:"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 100}},
		"E:/backup": {ids: []string{"vol:E:"}, usage: fsinfo.Usage{TotalBytes: 1000, FreeBytes: 100}},
		"/mystery":  {usage: fsinfo.Usage{TotalBytes: 500, FreeBytes: 50}},
		"/unknown":  {usage: fsinfo.Usage{TotalBytes: 500, FreeBytes: 50}},
	}
	keys, usage := fakeStat(vols, false)
	got := sumStorage([]string{"D:/media", "D:/tv", "E:/backup", "/mystery", "/unknown"}, keys, usage)
	want := statsStorage{TotalBytes: 3000, FreeBytes: 300, UsedBytes: 2700}
	if got == nil || *got != want {
		t.Fatalf("storage = %+v, want %+v", got, want)
	}
}
