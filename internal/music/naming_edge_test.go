package music_test

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/organizer"
)

func cleaner(mode organizer.SanitizeMode) music.Sanitizer {
	return func(v string) string { return organizer.Sanitize(v, mode, "_") }
}

// The folder and file names of a library must be legal on every file system a
// library may live on (Windows and SMB shares included) whatever the artist,
// album or track is called, and one title may not swallow the year that tells
// two albums of one name apart.
func TestMusicNamesAreLegalForEveryTitle(t *testing.T) {
	long := strings.Repeat("Ä", 200) // 400 bytes
	names := []string{
		"AC/DC", "AC\\DC", "Sigur Rós", "Björk", "🎸 Guitar Heroes 🎸", "CON", "con.", "NUL", "COM1", "LPT9.txt", "Aux",
		"...", "..", ".", " ", "  Leading and trailing  ", "Trailing dots...", "Trailing space ", "What?", `Say "Hello"`, "Left<Right>", "Pipe|Pipe", "Star*Star", "Colon: The Album",
		"Artist\x00Null", "Tab\tName", "Line\nBreak", "日本語のタイトル", "Ωmega", "e\u0301 (combining)", long, "x", "",
		"CONIN$", "Prn", "A/B/C/../../etc", "C:\\Windows", "~$temp", "-rf", "Foo.", "Bar . ",
	}
	for _, mode := range []organizer.SanitizeMode{organizer.SanitizeStrip, organizer.SanitizeReplace} {
		clean := cleaner(mode)
		for _, n := range names {
			for _, c := range []struct{ kind, got string }{
				{"artist", music.ArtistFolder(n, clean)},
				{"album", music.AlbumFolder(n, 2011, clean)},
				{"album without year", music.AlbumFolder(n, 0, clean)},
				{"track", music.TrackFileName(1, 7, n, false, ".FLAC", clean)},
				{"track, several discs", music.TrackFileName(2, 7, n, true, ".mp3", clean)},
			} {
				got := c.got
				t.Run(c.kind+"/"+strings.ReplaceAll(n[:min(len(n), 20)], "/", "_"), func(t *testing.T) {
					if got == "" || got == "." || got == ".." {
						t.Fatalf("%q gives the name %q", n, got)
					}
					if strings.ContainsAny(got, "/\\:*?\"<>|\x00\t\n") {
						t.Errorf("%q gives %q, which has a character a file system refuses", n, got)
					}
					if !utf8.ValidString(got) {
						t.Errorf("%q gives %q, not valid UTF-8", n, got)
					}
					if len(got) > 255 {
						t.Errorf("%q gives a name of %d bytes", n, len(got))
					}
					stem := strings.ToUpper(strings.TrimSuffix(got, filepath.Ext(got)))
					if i := strings.IndexByte(stem, '.'); i >= 0 {
						stem = stem[:i]
					}
					for _, dev := range []string{"CON", "PRN", "AUX", "NUL", "COM1", "LPT1", "CONIN$"} {
						if strings.TrimSpace(stem) == dev {
							t.Errorf("%q gives %q, a Windows device name", n, got)
						}
					}
					if strings.HasSuffix(got, ".") || strings.HasSuffix(got, " ") || strings.HasPrefix(got, " ") {
						t.Errorf("%q gives %q, which Windows would change", n, got)
					}
					if c.kind == "album" && len(n) > 0 && !strings.HasSuffix(got, "(2011)") {
						t.Errorf("%q gives %q: the year that tells editions apart is gone", n, got)
					}
				})
			}
		}
	}
}

// Two albums with the same long name and different years have different folders.
func TestLongAlbumNamesKeepTheirYears(t *testing.T) {
	clean := cleaner(organizer.SanitizeStrip)
	long := strings.Repeat("Long Title ", 40)
	a := music.AlbumFolder(long, 2001, clean)
	b := music.AlbumFolder(long, 2002, clean)
	if a == b {
		t.Fatalf("two different years give one folder: %q", a)
	}
}

// A folder the app made is read back as the album and year it was made for, so
// a rescan of the library does not take its own folders for new albums.
func TestAlbumFolderReadsBackAsTheSameAlbum(t *testing.T) {
	clean := cleaner(organizer.SanitizeStrip)
	tests := []struct {
		album string
		year  int
	}{
		{"Back in Black", 1980}, {"1999", 1982}, {"21", 2011}, {"1989", 2014}, {"2112", 1976}, {"(What's the Story) Morning Glory?", 1995},
		{"x (multiply)", 2014}, {"A.M.", 2013}, {"10,000 Days", 2006}, {"Is This It", 2001}, {"Live at the BBC", 1994}, {"Ágætis byrjun", 1999},
		{"Nevermind", 1991}, {"The Wall", 1979}, {"Blue [Deluxe]", 2003}, {"19", 2008}, {"OK Computer OKNOTOK 1997 2017", 2017}, {"Dummy", 0},
	}
	for _, tt := range tests {
		t.Run(tt.album, func(t *testing.T) {
			folder := music.AlbumFolder(tt.album, tt.year, clean)
			title, year := music.ParseAlbumFolder(folder, "Some Artist")
			if !music.SameAlbum(title, tt.album) || year != tt.year {
				t.Errorf("%q (%d) is the folder %q, read back as %q (%d)", tt.album, tt.year, folder, title, year)
			}
		})
	}
}

// Two bands of one name, and an album and a single of one name, each get a
// folder of their own; the one that was there first keeps its folder.
func TestUniqueAlbumPathTellsSameNamedFoldersApart(t *testing.T) {
	clean := cleaner(organizer.SanitizeStrip)
	root := filepath.Join("music")
	band1 := music.Artist{ID: 1, MBID: "aaaaaaaa-1111", Name: "Blue Ruin"}
	band2 := music.Artist{ID: 2, MBID: "bbbbbbbb-2222", Name: "Blue Ruin", Disambiguation: "US punk"}
	band3 := music.Artist{ID: 3, MBID: "cccccccc-3333", Name: "Blue Ruin"}
	selfTitled := music.Album{ID: 10, ArtistID: 1, MBID: "rg-10", Title: "Blue Ruin", Type: "album", ReleaseDate: "2013-06-01"}
	selfTitled.Path = music.AlbumPath(root, "Blue Ruin", "Blue Ruin", 2013, clean)
	single := music.Album{ID: 11, ArtistID: 1, MBID: "dddddddd-4444", Title: "Blue Ruin", Type: "single", ReleaseDate: "2013-01-01"}
	ep := music.Album{ID: 12, ArtistID: 1, MBID: "eeeeeeee-5555", Title: "Blue Ruin", Type: "ep", ReleaseDate: "2013-02-01"}
	otherBands := music.Album{ID: 20, ArtistID: 2, MBID: "rg-20", Title: "Blue Ruin", Type: "album", ReleaseDate: "2013-03-03"}
	third := music.Album{ID: 30, ArtistID: 3, MBID: "rg-30", Title: "Blue Ruin", Type: "album", ReleaseDate: "2013-09-09"}

	tests := []struct {
		name   string
		artist music.Artist
		album  music.Album
		taken  []music.Album
		want   string
	}{
		{"nothing in the way", band1, selfTitled, nil, filepath.Join(root, "Blue Ruin", "Blue Ruin (2013)")},
		{"the album that holds it keeps it", band1, selfTitled, []music.Album{selfTitled}, filepath.Join(root, "Blue Ruin", "Blue Ruin (2013)")},
		{"a single next to the album of the same name and year", band1, music.Album{ID: 11, ArtistID: 1, MBID: "dddddddd-4444", Title: "Blue Ruin", Type: "single", ReleaseDate: "2013-01-01"}, []music.Album{selfTitled}, filepath.Join(root, "Blue Ruin", "Blue Ruin [Single] (2013)")},
		{"an EP next to it", band1, ep, []music.Album{selfTitled}, filepath.Join(root, "Blue Ruin", "Blue Ruin [EP] (2013)")},
		{"another band of the same name, told by its disambiguation", band2, otherBands, []music.Album{selfTitled}, filepath.Join(root, "Blue Ruin (US punk)", "Blue Ruin (2013)")},
		{"a third band with nothing to tell it by", band3, third, []music.Album{selfTitled}, filepath.Join(root, "Blue Ruin [cccccccc]", "Blue Ruin (2013)")},
		{"an album of the same name, type and year", band1, music.Album{ID: 13, ArtistID: 1, MBID: "ffffffff-6666", Title: "Blue Ruin", Type: "album", ReleaseDate: "2013-12-12"}, []music.Album{selfTitled}, filepath.Join(root, "Blue Ruin", "Blue Ruin [ffffffff] (2013)")},
		{"the folder is held in other letters", band1, single, []music.Album{{ID: 10, ArtistID: 1, Path: strings.ToUpper(selfTitled.Path)}}, filepath.Join(root, "Blue Ruin", "Blue Ruin [Single] (2013)")},
		{"an album with no files holds nothing", band1, single, []music.Album{{ID: 10, ArtistID: 1}}, filepath.Join(root, "Blue Ruin", "Blue Ruin [Single] (2013)")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := music.UniqueAlbumPath(root, tt.artist, tt.album, tt.taken, clean); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// A new single or EP always carries its type in the folder name, an album never
// does, and a release that already has a folder keeps it.
func TestUniqueAlbumPathNamesSinglesAndEPsByType(t *testing.T) {
	clean := cleaner(organizer.SanitizeStrip)
	root := filepath.Join("music")
	artist := music.Artist{ID: 1, MBID: "aaaaaaaa-1111", Name: "Nina Simone"}
	rel := func(typ, title, date, path string) music.Album {
		return music.Album{ID: 5, ArtistID: 1, MBID: "11111111-2222", Title: title, Type: typ, ReleaseDate: date, Path: path}
	}
	oldSingle := filepath.Join(root, "Nina Simone", "Sinnerman (1965)")
	tests := []struct {
		name  string
		album music.Album
		taken []music.Album
		want  string
	}{
		{"a single", rel("single", "Sinnerman", "1965-01-01", ""), nil, filepath.Join(root, "Nina Simone", "Sinnerman [Single] (1965)")},
		{"an EP", rel("EP", "Four Women", "1966-01-01", ""), nil, filepath.Join(root, "Nina Simone", "Four Women [EP] (1966)")},
		{"the type in capitals or spaces", rel(" Single ", "Sinnerman", "1965-01-01", ""), nil, filepath.Join(root, "Nina Simone", "Sinnerman [Single] (1965)")},
		{"an album", rel("album", "Pastel Blues", "1965-01-01", ""), nil, filepath.Join(root, "Nina Simone", "Pastel Blues (1965)")},
		{"no type", rel("", "Pastel Blues", "1965-01-01", ""), nil, filepath.Join(root, "Nina Simone", "Pastel Blues (1965)")},
		{"another kind of release", rel("live", "Live at Newport", "1960-01-01", ""), nil, filepath.Join(root, "Nina Simone", "Live at Newport (1960)")},
		{"a single without a year", rel("single", "Sinnerman", "", ""), nil, filepath.Join(root, "Nina Simone", "Sinnerman [Single]")},
		{"a title that already says it", rel("single", "Sinnerman [Single]", "1965-01-01", ""), nil, filepath.Join(root, "Nina Simone", "Sinnerman [Single] (1965)")},
		{"a title that says it in round brackets", rel("ep", "Four Women (EP)", "1966-01-01", ""), nil, filepath.Join(root, "Nina Simone", "Four Women (EP) (1966)")},
		{"a newer single keeps its typed folder", rel("single", "Sinnerman", "1965-01-01", filepath.Join(root, "Nina Simone", "Sinnerman [Single] (1965)")), nil, filepath.Join(root, "Nina Simone", "Sinnerman [Single] (1965)")},
		{"an older single keeps its folder", rel("single", "Sinnerman", "1965-01-01", oldSingle), nil, oldSingle},
		{"an older single moves aside when another release holds its name", rel("single", "Sinnerman", "1965-01-01", oldSingle),
			[]music.Album{{ID: 9, ArtistID: 1, Path: oldSingle}}, filepath.Join(root, "Nina Simone", "Sinnerman [Single] (1965)")},
		{"two singles of one name and year", rel("single", "Sinnerman", "1965-01-01", ""),
			[]music.Album{{ID: 9, ArtistID: 1, Path: filepath.Join(root, "Nina Simone", "Sinnerman [Single] (1965)")}}, filepath.Join(root, "Nina Simone", "Sinnerman [Single] [11111111] (1965)")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := music.UniqueAlbumPath(root, artist, tt.album, tt.taken, clean); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The scan reads "Title [Single] (2020)" back as the title, so a library scan
// and a rescan find the folders that are made now.
func TestTypedAlbumFolderReadsBack(t *testing.T) {
	for _, tt := range []struct{ folder, title string; year int }{
		{"Sinnerman [Single] (1965)", "Sinnerman", 1965},
		{"Four Women [EP] (1966)", "Four Women", 1966},
		{"Sinnerman [Single]", "Sinnerman", 0},
	} {
		title, year := music.ParseAlbumFolder(tt.folder, "Nina Simone")
		if music.NormalizeName(title) != music.NormalizeName(tt.title) || year != tt.year {
			t.Errorf("%q read as %q, %d; want %q, %d", tt.folder, title, year, tt.title, tt.year)
		}
	}
}
