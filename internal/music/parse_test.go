package music

import "testing"

func TestParseRelease(t *testing.T) {
	cases := []struct {
		title string
		want  Release
	}{
		// P2P style, with spaces.
		{"Radiohead - OK Computer (1997) [FLAC]",
			Release{Artist: "Radiohead", Album: "OK Computer", Year: 1997, Format: FormatFLAC}},
		{"Radiohead - OK Computer (1997) [FLAC 24bit-96kHz]",
			Release{Artist: "Radiohead", Album: "OK Computer", Year: 1997, Format: FormatFLAC, BitDepth: 24}},
		{"Daft Punk - Random Access Memories (2013) [24B-88.2kHz] FLAC",
			Release{Artist: "Daft Punk", Album: "Random Access Memories", Year: 2013, Format: FormatFLAC}},
		{"Taylor Swift - 1989 (2014) [MP3 320]",
			Release{Artist: "Taylor Swift", Album: "1989", Year: 2014, Format: FormatMP3, Bitrate: "320"}},
		{"Adele - 25 (2015) Mp3 320kbps [PMEDIA]",
			Release{Artist: "Adele", Album: "25", Year: 2015, Format: FormatMP3, Bitrate: "320"}},
		{"Arctic Monkeys - AM (2013) [V0]",
			Release{Artist: "Arctic Monkeys", Album: "AM", Year: 2013, Format: FormatMP3, Bitrate: "V0"}},
		{"Pink Floyd - The Dark Side of the Moon (Remastered) (1973) [FLAC 16-44.1] {Vinyl}",
			Release{Artist: "Pink Floyd", Album: "The Dark Side of the Moon (Remastered)", Year: 1973, Format: FormatFLAC, BitDepth: 16, Source: "Vinyl"}},
		{"Kendrick Lamar - To Pimp a Butterfly (2015) [WEB FLAC]",
			Release{Artist: "Kendrick Lamar", Album: "To Pimp a Butterfly", Year: 2015, Format: FormatFLAC, Source: "WEB"}},
		{"Fleetwood Mac - Rumours (Super Deluxe Edition) (2013) [ALAC]",
			Release{Artist: "Fleetwood Mac", Album: "Rumours (Super Deluxe Edition)", Year: 2013, Format: FormatALAC}},
		{"Beyoncé - Lemonade [2016] [AAC 256]",
			Release{Artist: "Beyoncé", Album: "Lemonade", Year: 2016, Format: FormatAAC, Bitrate: "256"}},
		{"Blink-182 - Enema of the State 1999 FLAC",
			Release{Artist: "Blink-182", Album: "Enema of the State", Year: 1999, Format: FormatFLAC}},
		{"Prince - 1999 (1982) [CD FLAC]",
			Release{Artist: "Prince", Album: "1999", Year: 1982, Format: FormatFLAC, Source: "CD"}},
		{"The 1975 - The 1975 (2013) [FLAC]",
			Release{Artist: "The 1975", Album: "The 1975", Year: 2013, Format: FormatFLAC}},
		{"(1997) Radiohead - OK Computer [FLAC]",
			Release{Artist: "Radiohead", Album: "OK Computer", Year: 1997, Format: FormatFLAC}},
		{"Muse - Absolution - 2003 - MP3 192kbps",
			Release{Artist: "Muse", Album: "Absolution", Year: 2003, Format: FormatMP3, Bitrate: "192"}},
		{"Bjork - Vespertine (2001) [FLAC] [Hi-Res]",
			Release{Artist: "Bjork", Album: "Vespertine", Year: 2001, Format: FormatFLAC, BitDepth: 24}},

		// Discographies and collections.
		{"Metallica - Discography (1983-2016) [FLAC]",
			Release{Artist: "Metallica", Album: "Discography", Format: FormatFLAC, Discography: true}},
		{"Queen - Complete Studio Albums (1973-1995) [MP3 320]",
			Release{Artist: "Queen", Album: "Complete Studio Albums", Format: FormatMP3, Bitrate: "320", Discography: true}},
		{"Bob Marley - The Collection (2005) [MP3 V0]",
			Release{Artist: "Bob Marley", Album: "The Collection", Year: 2005, Format: FormatMP3, Bitrate: "V0", Discography: true}},

		// Scene style: no spaces, dash-separated fields.
		{"Radiohead-OK_Computer-(CDNODATA29)-REMASTERED-2CD-FLAC-2009-GRP",
			Release{Artist: "Radiohead", Album: "OK Computer", Year: 2009, Format: FormatFLAC, Source: "CD", Group: "GRP"}},
		{"Daft_Punk-Random_Access_Memories-WEB-2013-ENRiCH",
			Release{Artist: "Daft Punk", Album: "Random Access Memories", Year: 2013, Source: "WEB", Group: "ENRiCH"}},
		{"Burial-Untrue-24BIT-WEB-FLAC-2007-TosK",
			Release{Artist: "Burial", Album: "Untrue", Year: 2007, Format: FormatFLAC, BitDepth: 24, Source: "WEB", Group: "TosK"}},
		{"Aphex_Twin-Syro-Vinyl-FLAC-2014-FATHEAD",
			Release{Artist: "Aphex Twin", Album: "Syro", Year: 2014, Format: FormatFLAC, Source: "Vinyl", Group: "FATHEAD"}},
		{"Four_Tet-Three-(TEXT060)-WEB-2024-OMA",
			Release{Artist: "Four Tet", Album: "Three", Year: 2024, Source: "WEB", Group: "OMA"}},
		{"Fleet.Foxes-Shore-WEB-2020-FLAC",
			Release{Artist: "Fleet Foxes", Album: "Shore", Year: 2020, Format: FormatFLAC, Source: "WEB"}},
		{"The_National-Sleep_Well_Beast-320kbps-2017",
			Release{Artist: "The National", Album: "Sleep Well Beast", Year: 2017, Format: FormatMP3, Bitrate: "320"}},

		// Leftovers: a torrent file name, and a title with no separator.
		{"Portishead - Dummy (1994) [FLAC].torrent",
			Release{Artist: "Portishead", Album: "Dummy", Year: 1994, Format: FormatFLAC}},
		{"Some Album FLAC",
			Release{Album: "Some Album", Format: FormatFLAC}},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			if got := ParseRelease(tc.title); got != tc.want {
				t.Fatalf("ParseRelease(%q)\n got  %+v\n want %+v", tc.title, got, tc.want)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		title string
		want  Tier
	}{
		{"Radiohead - OK Computer (1997) [FLAC]", TierFLAC},
		{"Radiohead - OK Computer (1997) [FLAC 24bit-96kHz]", TierFLAC24},
		{"Burial-Untrue-24BIT-WEB-FLAC-2007-TosK", TierFLAC24},
		{"Fleetwood Mac - Rumours (2013) [ALAC]", TierFLAC},
		{"Adele - 25 (2015) Mp3 320kbps", TierMP3320},
		{"Arctic Monkeys - AM (2013) [V0]", TierMP3320},
		{"Arctic Monkeys - AM (2013) [MP3 256]", TierMP3256},
		{"Muse - Absolution - 2003 - MP3 192kbps", TierMP3192},
		{"Muse - Absolution (2003) [MP3]", TierMP3192},
		{"Beyoncé - Lemonade [2016] [AAC 256]", TierAAC256},
		{"Daft_Punk-Random_Access_Memories-WEB-2013-ENRiCH", TierUnknown},
	}
	for _, tc := range cases {
		if got := ParseRelease(tc.title).Tier(); got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.title, got, tc.want)
		}
	}
}
