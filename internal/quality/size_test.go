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

func TestSizePlausibleRuntime(t *testing.T) {
	gb := int64(1) << 30
	cases := []struct {
		name     string
		tier     Tier
		size     int64
		episodes int
		minutes  float64
		ok       bool
	}{
		{"5 minute episode of 60 MB passes", TierWebDL1080p, 60 * mb, 1, 5, true},
		{"the same release fails with an unknown runtime", TierWebDL1080p, 60 * mb, 1, 0, false},
		{"45 minute episode behaves as before (fails)", TierWebDL1080p, 100 * mb, 1, 45, false},
		{"45 minute episode behaves as before (passes)", TierWebDL1080p, 150 * mb, 1, 45, true},
		{"exactly 30 minutes is not scaled", TierWebDL1080p, 100 * mb, 1, 30, false},
		{"the floor is 15 MB for a very short episode", TierWebDL1080p, 10 * mb, 1, 1, false},
		{"just above the floor passes", TierWebDL1080p, 16 * mb, 1, 1, true},
		{"20 minute episode needs two thirds", TierWebDL1080p, 90 * mb, 1, 20, false},
		{"20 minute episode, enough", TierWebDL1080p, 100 * mb, 1, 20, true},
		{"two short episodes need twice the minimum", TierWebDL1080p, 40 * mb, 2, 5, false},
		{"movies ignore the runtime", TierWebDL1080p, 100 * mb, 0, 5, false},
		{"a short season pack", TierWebDL1080p, 80 * mb, SeasonPack, 5, true},
		{"a normal season pack is unchanged", TierWebDL1080p, 3 * gb, SeasonPack, 45, true},
	}
	for _, tc := range cases {
		if got := SizePlausibleRuntime(tc.tier, tc.size, tc.episodes, tc.minutes); got != tc.ok {
			t.Errorf("%s: = %v, want %v", tc.name, got, tc.ok)
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

func TestSizeAllowedByCapAndRoom(t *testing.T) {
	gb := int64(1) << 30
	cases := []struct {
		name string
		p    Profile
		size int64
		ok   bool
	}{
		{"no limits at all", Profile{}, 90 * gb, true},
		{"under the install's cap", Profile{CapGB: 20}, 19 * gb, true},
		{"over the install's cap", Profile{CapGB: 20}, 21 * gb, false},
		{"the cap beats a looser profile", Profile{MaxSizeGB: 50, CapGB: 20}, 30 * gb, false},
		{"the profile beats a looser cap", Profile{MaxSizeGB: 10, CapGB: 50}, 30 * gb, false},
		{"fits the room", Profile{RoomBytes: 30 * gb}, 29 * gb, true},
		{"too big for the room", Profile{RoomBytes: 30 * gb}, 31 * gb, false},
		{"a full disk takes nothing", Profile{RoomBytes: 1}, gb, false},
		{"an unknown size always passes", Profile{CapGB: 1, RoomBytes: 1}, 0, true},
	}
	for _, tc := range cases {
		if got := tc.p.SizeAllowed(tc.size); got != tc.ok {
			t.Errorf("%s: SizeAllowed = %v, want %v", tc.name, got, tc.ok)
		}
	}
}

func TestPackSizeAllowed(t *testing.T) {
	gb := int64(1) << 30
	p := Profile{MaxSizeGB: 10, CapGB: 50, RoomBytes: 60 * gb}
	if !p.PackSizeAllowed(40*gb, 8*gb) {
		t.Error("a pack under every limit should pass")
	}
	if p.PackSizeAllowed(80*gb, 8*gb) {
		t.Error("the whole pack counts against the cap and the room, not one season")
	}
	if p.PackSizeAllowed(40*gb, 11*gb) {
		t.Error("a season over the profile limit should fail")
	}
}
