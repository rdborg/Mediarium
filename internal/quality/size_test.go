package quality

import "testing"

func TestSizePlausible(t *testing.T) {
	gb := int64(1) << 30
	cases := []struct {
		name     string
		tier     Tier
		size     int64
		episodes int
		ok       bool
	}{
		{"unknown size passes", TierBluray1080p, 0, 0, true},
		{"a 40 MB 1080p movie is a fake", TierBluray1080p, 40 * mb, 0, false},
		{"a normal 1080p movie", TierBluray1080p, 8 * gb, 0, true},
		{"a 2 GB 4K remux is not a remux", TierRemux2160p, 2 * gb, 0, false},
		{"a small SD movie is fine", TierSDTV, 400 * mb, 0, true},
		{"one 720p episode of 300 MB", TierWebDL720p, 300 * mb, 1, true},
		{"one 1080p episode of 20 MB", TierWebDL1080p, 20 * mb, 1, false},
		{"two episodes need twice as much", TierWebDL1080p, 200 * mb, 2, false},
		{"a season pack of 3 GB", TierWebDL1080p, 3 * gb, SeasonPack, true},
		{"a season pack of 100 MB", TierWebDL1080p, 100 * mb, SeasonPack, false},
		{"an unknown quality has no minimum", TierUnknown, 1 * mb, 0, true},
	}
	for _, tc := range cases {
		if got := SizePlausible(tc.tier, tc.size, tc.episodes); got != tc.ok {
			t.Errorf("%s: SizePlausible = %v, want %v", tc.name, got, tc.ok)
		}
	}
}

func TestSizeAllowed(t *testing.T) {
	gb := int64(1) << 30
	cases := []struct {
		max  float64
		size int64
		ok   bool
	}{
		{0, 80 * gb, true},
		{10, 9 * gb, true},
		{10, 11 * gb, false},
		{10, 0, true},
		{2.5, 2*gb + gb/2, true},
	}
	for _, tc := range cases {
		if got := (Profile{MaxSizeGB: tc.max}).SizeAllowed(tc.size); got != tc.ok {
			t.Errorf("max %v, size %d: %v, want %v", tc.max, tc.size, got, tc.ok)
		}
	}
}
