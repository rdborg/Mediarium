package organizer

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestNormalizeRARVolumeNames(t *testing.T) {
	cases := []struct {
		name   string
		files  []string
		first  string
		want   []string // directory listing afterwards, sorted
		wantFP string   // expected first-volume name afterwards
	}{
		{
			// The real-world case: part01..part09, then part010..part012.
			name:   "mixed padding",
			files:  []string{"a.part01.rar", "a.part02.rar", "a.part09.rar", "a.part010.rar", "a.part011.rar", "a.part012.rar", "a.sfv"},
			first:  "a.part01.rar",
			want:   []string{"a.part01.rar", "a.part02.rar", "a.part09.rar", "a.part10.rar", "a.part11.rar", "a.part12.rar", "a.sfv"},
			wantFP: "a.part01.rar",
		},
		{
			name:   "consistent set untouched",
			files:  []string{"b.part001.rar", "b.part002.rar", "b.part010.rar"},
			first:  "b.part001.rar",
			want:   []string{"b.part001.rar", "b.part002.rar", "b.part010.rar"},
			wantFP: "b.part001.rar",
		},
		{
			name:   "single digits growing to two",
			files:  []string{"c.part1.rar", "c.part2.rar", "c.part9.rar", "c.part10.rar"},
			first:  "c.part1.rar",
			want:   []string{"c.part1.rar", "c.part10.rar", "c.part2.rar", "c.part9.rar"},
			wantFP: "c.part1.rar",
		},
		{
			// Over 99 volumes with a two-digit first volume: the first is renamed too.
			name:   "first volume renamed",
			files:  []string{"d.part01.rar", "d.part02.rar", "d.part0100.rar"},
			first:  "d.part01.rar",
			want:   []string{"d.part001.rar", "d.part002.rar", "d.part100.rar"},
			wantFP: "d.part001.rar",
		},
		{
			name:   "other sets in the folder are left alone",
			files:  []string{"e.part01.rar", "e.part010.rar", "other.part1.rar", "other.part010.rar"},
			first:  "e.part01.rar",
			want:   []string{"e.part01.rar", "e.part10.rar", "other.part010.rar", "other.part1.rar"},
			wantFP: "e.part01.rar",
		},
		{
			name:   "not a part set",
			files:  []string{"f.rar", "f.r00"},
			first:  "f.rar",
			want:   []string{"f.r00", "f.rar"},
			wantFP: "f.rar",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte(f), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := normalizeRARVolumeNames(filepath.Join(dir, tc.first))
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if filepath.Base(got) != tc.wantFP {
				t.Errorf("first volume = %s, want %s", filepath.Base(got), tc.wantFP)
			}
			entries, _ := os.ReadDir(dir)
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			sort.Strings(names)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if len(names) != len(want) {
				t.Fatalf("files = %v, want %v", names, want)
			}
			for i := range names {
				if names[i] != want[i] {
					t.Fatalf("files = %v, want %v", names, want)
				}
			}
			// Content moved with the name (nothing overwritten).
			for _, n := range names {
				b, _ := os.ReadFile(filepath.Join(dir, n))
				if len(b) == 0 {
					t.Errorf("%s is empty", n)
				}
			}
		})
	}
}

func TestNormalizeRARVolumeNamesRefusesDuplicates(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"g.part01.rar", "g.part1.rar", "g.part010.rar"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := normalizeRARVolumeNames(filepath.Join(dir, "g.part01.rar")); err == nil {
		t.Fatal("two volumes numbered 1 should be refused, not overwritten")
	}
}
