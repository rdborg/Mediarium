package api

import "testing"

func TestWithReleaseGroup(t *testing.T) {
	for _, c := range []struct{ in, group, want string }{
		{"Heat.1995.1080p.BluRay.x264-OLD", "NEW", "Heat.1995.1080p.BluRay.x264-NEW"},
		{"Heat.1995.1080p.BluRay.x264-OLD", "", "Heat.1995.1080p.BluRay.x264"},
		{"Heat.1995.1080p.BluRay.x264", "NEW", "Heat.1995.1080p.BluRay.x264-NEW"},
		{"Heat.1995.1080p.BluRay.x264-old", "NEW", "Heat.1995.1080p.BluRay.x264-NEW"},
		{"Heat 1995 1080p BluRay x264", "", "Heat 1995 1080p BluRay x264"},
		{"  Heat.1995.1080p.WEB-DL.DDP5.1.H.264-FLUX ", "NTb", "Heat.1995.1080p.WEB-DL.DDP5.1.H.264-NTb"},
	} {
		if got := withReleaseGroup(c.in, c.group); got != c.want {
			t.Errorf("withReleaseGroup(%q, %q) = %q, want %q", c.in, c.group, got, c.want)
		}
	}
}
