package music

import "testing"

// A scene name can keep spaces inside its fields and still use dashes as the
// only separators between them (#193).
func TestParseReleaseDashesWithSpacesInside(t *testing.T) {
	cases := []struct {
		title string
		want  Release
	}{
		{"Lichterkinder-Weihnacht auch fuer Dich-16BIT-44-KHZ-WEB-FLAC-2024-WALKMAN",
			Release{Artist: "Lichterkinder", Album: "Weihnacht auch fuer Dich", Year: 2024, Format: FormatFLAC, BitDepth: 16, Source: "WEB", Group: "WALKMAN"}},
		{"Bjoerk-Vespertine Live-WEB-FLAC-2001-GRP",
			Release{Artist: "Bjoerk", Album: "Vespertine Live", Year: 2001, Format: FormatFLAC, Source: "WEB", Group: "GRP"}},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			got := ParseRelease(tc.title)
			if got != tc.want {
				t.Fatalf("ParseRelease(%q)\n got  %+v\n want %+v", tc.title, got, tc.want)
			}
		})
	}
}

// A hyphenated name in a spaced title is not cut at its hyphen.
func TestParseReleaseKeepsHyphenatedName(t *testing.T) {
	if got := ParseRelease("Blink-182 Enema of the State 1999 FLAC"); got.Artist == "Blink" || got.Album != "Blink-182 Enema of the State" {
		t.Fatalf("cut a hyphenated name: %+v", got)
	}
}

// German umlauts match their transliterations in both directions, for the
// artist and the album (#148, #193).
func TestReleaseMatchUmlautsBothWays(t *testing.T) {
	cases := []struct {
		title, artist, album string
		year                 int
		ok                   bool
	}{
		{"Lichterkinder-Weihnacht auch fuer Dich-16BIT-44-KHZ-WEB-FLAC-2024-WALKMAN", "Lichterkinder", "Weihnacht auch für Dich", 2024, true},
		{"Lichterkinder - Weihnacht auch für Dich (2024) [FLAC]", "Lichterkinder", "Weihnacht auch für Dich", 2024, true},
		{"Lichterkinder-Weihnacht_auch_fuer_Dich-WEB-FLAC-2024-GRP", "Lichterkinder", "Weihnacht auch fuer Dich", 2024, true},
		{"Lichterkinder-Weihnacht auch fuer Dich-16BIT-44-KHZ-WEB-FLAC-2024-WALKMAN", "Lichterkinder", "Weihnacht auch für Dich", 2024, true},
		{"Bjoerk-Vespertine-WEB-FLAC-2001-GRP", "Björk", "Vespertine", 2001, true},
		{"Björk - Vespertine (2001) [FLAC]", "Bjoerk", "Vespertine", 2001, true},
		{"Bjork - Vespertine (2001) [FLAC]", "Björk", "Vespertine", 2001, true},
		{"Moetley_Cruee-Dr_Feelgood-CD-FLAC-1989-GRP", "Mötley Crüe", "Dr. Feelgood", 1989, true},
		{"Mötley Crüe - Dr. Feelgood (1989) [FLAC]", "Moetley Cruee", "Dr. Feelgood", 1989, true},
		{"Motley Crue - Dr. Feelgood (1989) [FLAC]", "Mötley Crüe", "Dr. Feelgood", 1989, true},
		{"Die_Aerzte-Jazz_ist_anders-CD-FLAC-2007-GRP", "Die Ärzte", "Jazz ist anders", 2007, true},
		{"Die Ärzte - Jazz ist anders (2007) [FLAC]", "Die Aerzte", "Jazz ist anders", 2007, true},
		{"Aerzte-Geräusch-CD-FLAC-2003-GRP", "Ärzte", "Geräusch", 2003, true},
		{"Die Ärzte - Jazz ist anders (2007) [FLAC]", "Die Toten Hosen", "Jazz ist anders", 2007, false},
		{"Lichterkinder-Weihnacht auch fuer Dich-16BIT-44-KHZ-WEB-FLAC-2024-WALKMAN", "Lichterkinder", "Kinderlieder", 2024, false},
	}
	for _, tc := range cases {
		t.Run(tc.title+" vs "+tc.artist+" / "+tc.album, func(t *testing.T) {
			ok, reason := ReleaseMatch(ParseRelease(tc.title), tc.artist, tc.album, tc.year)
			if ok != tc.ok {
				t.Fatalf("ReleaseMatch = (%v, %q), want %v; parsed %+v", ok, reason, tc.ok, ParseRelease(tc.title))
			}
		})
	}
}

func TestSpellUmlauts(t *testing.T) {
	for in, want := range map[string]string{
		"Björk":                   "Bjoerk",
		"Mötley Crüe":             "Moetley Cruee",
		"Die Ärzte":               "Die Aerzte",
		"Weihnacht auch für Dich": "Weihnacht auch fuer Dich",
		"Straße":                  "Strasse",
		"ÄRZTE":                   "AeRZTE",
		"Radiohead":               "Radiohead",
		"Sigur Rós":               "Sigur Rós", // only German umlauts are written out
	} {
		if got := SpellUmlauts(in); got != want {
			t.Errorf("SpellUmlauts(%q) = %q, want %q", in, got, want)
		}
	}
}
