package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContainerMemoryLimit(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		body   string
		want   uint64
		wantOK bool
	}{
		{"cgroup v2 limit", "sys/fs/cgroup/memory.max", "2147483648\n", 2147483648, true},
		{"cgroup v2 without limit", "sys/fs/cgroup/memory.max", "max\n", 0, false},
		{"cgroup v1 limit", "sys/fs/cgroup/memory/memory.limit_in_bytes", "1073741824\n", 1073741824, true},
		{"cgroup v1 without limit", "sys/fs/cgroup/memory/memory.limit_in_bytes", "9223372036854771712\n", 0, false},
		{"no cgroup files", "", "", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.file != "" {
				p := filepath.Join(root, filepath.FromSlash(tc.file))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, ok := ContainerMemoryLimit(root)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("got %d, %v; want %d, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
