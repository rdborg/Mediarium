package fsinfo

import (
	"reflect"
	"testing"
)

func TestSameVolumeKeys(t *testing.T) {
	tests := []struct {
		name    string
		ids     []string
		u       Usage
		bySpace bool
		want    []string
	}{
		{"ids and space", []string{"dev:1", "fsid:a-b"}, Usage{TotalBytes: 100, FreeBytes: 40}, true, []string{"dev:1", "fsid:a-b", "space:100/40"}},
		{"space matching off", []string{"vol:D:"}, Usage{TotalBytes: 100, FreeBytes: 40}, false, []string{"vol:D:"}},
		{"no ids still has the space", nil, Usage{TotalBytes: 100, FreeBytes: 40}, true, []string{"space:100/40"}},
		{"empty disk has nothing to compare", nil, Usage{}, true, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SameVolumeKeys(tc.ids, tc.u, tc.bySpace)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("keys = %v, want %v", got, tc.want)
			}
		})
	}
}
