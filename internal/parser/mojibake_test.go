package parser

import "testing"

func TestRepairMojibake(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"u umlaut", "Sams.im.GlÃ¼ck.2003.German.1080p", "Sams.im.Glück.2003.German.1080p"},
		{"a and o umlaut", "BÃ¤r.und.Ã–l", "Bär.und.Öl"},
		{"sharp s through cp1252", "StraÃŸe.S01E01", "Straße.S01E01"},
		{"accent", "CafÃ©.2020", "Café.2020"},
		{"already fine", "Sams.im.Glück.2003", "Sams.im.Glück.2003"},
		{"plain ascii", "Show.S01E01.1080p", "Show.S01E01.1080p"},
		{"real A-tilde in a name is kept", "SÃO.Paulo", "SÃO.Paulo"},
		{"mixed with a real non-latin letter", "ÃÂ Привет", "ÃÂ Привет"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := repairMojibake(tc.in); got != tc.want {
				t.Errorf("repairMojibake(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	if r := Parse("Sams.im.GlÃ¼ck.2003.German.1080p.WEB.x264-GRP"); r.Title != "Sams im Glück" {
		t.Errorf("Parse title = %q", r.Title)
	}
}
