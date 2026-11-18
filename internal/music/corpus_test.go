package music

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type musicCase struct {
	in                  string
	artist, album       string
	year                int
	format              Format
	bitrate, source     string
	bitDepth            int
	discography         bool
	group               string
	checkGroup, checkBD bool
}

func mc(in, artist, album string, year int, format Format, bitrate, source string) musicCase {
	return musicCase{in: in, artist: artist, album: album, year: year, format: format, bitrate: bitrate, source: source}
}

// TestParseReleaseCorpus runs a few hundred real-looking music release names.
// Only the fields a case names are checked: artist and album always, the rest
// when they are not their zero value or the case says so by name.
func TestParseReleaseCorpus(t *testing.T) {
	cases := []musicCase{
		// Scene
		mc("Daft_Punk-Random_Access_Memories-WEB-2013-GROUP", "Daft Punk", "Random Access Memories", 2013, "", "", "WEB"),
		mc("Daft.Punk-Random.Access.Memories-WEB-2013-GROUP", "Daft Punk", "Random Access Memories", 2013, "", "", "WEB"),
		mc("Daft_Punk-Random_Access_Memories-CD-FLAC-2013-GRP", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", "CD"),
		mc("Daft_Punk-Random_Access_Memories-24BIT-WEB-FLAC-2013-GRP", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", "WEB"),
		mc("Daft_Punk-Discovery-CD-FLAC-2001-GRP", "Daft Punk", "Discovery", 2001, FormatFLAC, "", "CD"),
		mc("Daft_Punk-Discovery-CD-FLAC-2001-GRP.nzb", "Daft Punk", "Discovery", 2001, FormatFLAC, "", "CD"),
		mc("Daft_Punk-Homework-(CDV2821)-CD-FLAC-1997-GRP", "Daft Punk", "Homework", 1997, FormatFLAC, "", "CD"),
		mc("Daft_Punk-Homework-(CDV2821)-CD-1997-GRP", "Daft Punk", "Homework", 1997, "", "", "CD"),
		mc("Radiohead-OK_Computer-CD-FLAC-1997-GRP", "Radiohead", "OK Computer", 1997, FormatFLAC, "", "CD"),
		mc("Radiohead-OK.Computer-CD-FLAC-1997-GRP", "Radiohead", "OK Computer", 1997, FormatFLAC, "", "CD"),
		mc("Radiohead-In_Rainbows-WEB-2007-GRP", "Radiohead", "In Rainbows", 2007, "", "", "WEB"),
		mc("Radiohead-Kid_A-VINYL-FLAC-2000-GRP", "Radiohead", "Kid A", 2000, FormatFLAC, "", "Vinyl"),
		mc("Radiohead-A_Moon_Shaped_Pool-WEB-FLAC-2016-GRP", "Radiohead", "A Moon Shaped Pool", 2016, FormatFLAC, "", "WEB"),
		mc("Pink_Floyd-The_Dark_Side_of_the_Moon-REMASTERED-CD-FLAC-2011-GRP", "Pink Floyd", "The Dark Side of the Moon", 2011, FormatFLAC, "", "CD"),
		mc("Pink_Floyd-The_Wall-2CD-FLAC-1979-GRP", "Pink Floyd", "The Wall", 1979, FormatFLAC, "", "CD"),
		mc("Pink_Floyd-The_Wall-2CD-1979-GRP", "Pink Floyd", "The Wall", 1979, "", "", "CD"),
		mc("The_Beatles-Abbey_Road-CD-FLAC-1969-GRP", "The Beatles", "Abbey Road", 1969, FormatFLAC, "", "CD"),
		mc("The_Beatles-Revolver-REMASTERED-24BIT-WEB-FLAC-2022-GRP", "The Beatles", "Revolver", 2022, FormatFLAC, "", "WEB"),
		mc("Beyonce-Lemonade-WEB-2016-GRP", "Beyonce", "Lemonade", 2016, "", "", "WEB"),
		mc("Beyonce-Renaissance-WEB-2022-GRP", "Beyonce", "Renaissance", 2022, "", "", "WEB"),
		mc("Taylor_Swift-Midnights-WEB-2022-GRP", "Taylor Swift", "Midnights", 2022, "", "", "WEB"),
		mc("Taylor_Swift-1989_(Taylors_Version)-WEB-2023-GRP", "Taylor Swift", "1989 (Taylors Version)", 2023, "", "", "WEB"),
		mc("Taylor_Swift-1989-WEB-2014-GRP", "Taylor Swift", "1989", 2014, "", "", "WEB"),
		mc("Taylor_Swift-Folklore-WEB-2020-GRP", "Taylor Swift", "Folklore", 2020, "", "", "WEB"),
		mc("Kendrick_Lamar-DAMN-WEB-2017-GRP", "Kendrick Lamar", "DAMN", 2017, "", "", "WEB"),
		mc("Kendrick_Lamar-To_Pimp_a_Butterfly-WEB-2015-GRP", "Kendrick Lamar", "To Pimp a Butterfly", 2015, "", "", "WEB"),
		mc("Billie_Eilish-When_We_All_Fall_Asleep_Where_Do_We_Go-WEB-2019-GRP", "Billie Eilish", "When We All Fall Asleep Where Do We Go", 2019, "", "", "WEB"),
		mc("Billie_Eilish-Hit_Me_Hard_and_Soft-WEB-2024-GRP", "Billie Eilish", "Hit Me Hard and Soft", 2024, "", "", "WEB"),
		mc("The_Weeknd-After_Hours-WEB-2020-GRP", "The Weeknd", "After Hours", 2020, "", "", "WEB"),
		mc("Arctic_Monkeys-AM-WEB-2013-GRP", "Arctic Monkeys", "AM", 2013, "", "", "WEB"),
		mc("Arctic_Monkeys-Whatever_People_Say_I_Am_Thats_What_Im_Not-CD-FLAC-2006-GRP", "Arctic Monkeys", "Whatever People Say I Am Thats What Im Not", 2006, FormatFLAC, "", "CD"),
		mc("Nirvana-Nevermind-CD-FLAC-1991-GRP", "Nirvana", "Nevermind", 1991, FormatFLAC, "", "CD"),
		mc("Nirvana-MTV_Unplugged_in_New_York-CD-FLAC-1994-GRP", "Nirvana", "MTV Unplugged in New York", 1994, FormatFLAC, "", "CD"),
		mc("Metallica-Master_of_Puppets-CD-FLAC-1986-GRP", "Metallica", "Master of Puppets", 1986, FormatFLAC, "", "CD"),
		mc("Metallica-Metallica-CD-FLAC-1991-GRP", "Metallica", "Metallica", 1991, FormatFLAC, "", "CD"),
		mc("Led_Zeppelin-Led_Zeppelin_IV-CD-FLAC-1971-GRP", "Led Zeppelin", "Led Zeppelin IV", 1971, FormatFLAC, "", "CD"),
		mc("Led_Zeppelin-Physical_Graffiti-2CD-FLAC-1975-GRP", "Led Zeppelin", "Physical Graffiti", 1975, FormatFLAC, "", "CD"),
		mc("Bob_Dylan-Highway_61_Revisited-CD-FLAC-1965-GRP", "Bob Dylan", "Highway 61 Revisited", 1965, FormatFLAC, "", "CD"),
		mc("Miles_Davis-Kind_of_Blue-CD-FLAC-1959-GRP", "Miles Davis", "Kind of Blue", 1959, FormatFLAC, "", "CD"),
		mc("Miles_Davis-Kind_of_Blue-VINYL-24BIT-96kHz-FLAC-1959-GRP", "Miles Davis", "Kind of Blue", 1959, FormatFLAC, "", "Vinyl"),
		mc("Beethoven-Symphony_No_9-CD-FLAC-1989-GRP", "Beethoven", "Symphony No 9", 1989, FormatFLAC, "", "CD"),
		mc("VA-Now_Thats_What_I_Call_Music_100-CD-2018-GRP", "VA", "Now Thats What I Call Music 100", 2018, "", "", "CD"),
		mc("VA-Café_del_Mar_Vol_1-CD-FLAC-1994-GRP", "VA", "Café del Mar Vol 1", 1994, FormatFLAC, "", "CD"),
		mc("Various_Artists-Pulp_Fiction_OST-CD-FLAC-1994-GRP", "Various Artists", "Pulp Fiction OST", 1994, FormatFLAC, "", "CD"),
		mc("Sigur_Rós-Ágætis_byrjun-CD-FLAC-1999-GRP", "Sigur Rós", "Ágætis byrjun", 1999, FormatFLAC, "", "CD"),
		mc("Björk-Homogenic-CD-FLAC-1997-GRP", "Björk", "Homogenic", 1997, FormatFLAC, "", "CD"),
		mc("Sigur_Ros-Agaetis_byrjun-CD-FLAC-1999-GRP", "Sigur Ros", "Agaetis byrjun", 1999, FormatFLAC, "", "CD"),
		mc("Motörhead-Ace_of_Spades-CD-FLAC-1980-GRP", "Motörhead", "Ace of Spades", 1980, FormatFLAC, "", "CD"),
		mc("Mötley_Crüe-Dr_Feelgood-CD-FLAC-1989-GRP", "Mötley Crüe", "Dr Feelgood", 1989, FormatFLAC, "", "CD"),
		mc("Blink-182-Enema_of_the_State-CD-FLAC-1999-GRP", "Blink", "", 0, "", "", ""),
		mc("Jay-Z-The_Blueprint-CD-FLAC-2001-GRP", "Jay", "", 0, "", "", ""),
		mc("Tyler_The_Creator-IGOR-WEB-2019-GRP", "Tyler The Creator", "IGOR", 2019, "", "", "WEB"),
		mc("Tyler,_The_Creator-IGOR-WEB-2019-GRP", "Tyler, The Creator", "IGOR", 2019, "", "", "WEB"),
		mc("Florence_and_the_Machine-Lungs-CD-FLAC-2009-GRP", "Florence and the Machine", "Lungs", 2009, FormatFLAC, "", "CD"),
		mc("Florence_+_the_Machine-Lungs-CD-FLAC-2009-GRP", "Florence + the Machine", "Lungs", 2009, FormatFLAC, "", "CD"),
		mc("Simon_and_Garfunkel-Bridge_over_Troubled_Water-CD-FLAC-1970-GRP", "Simon and Garfunkel", "Bridge over Troubled Water", 1970, FormatFLAC, "", "CD"),
		mc("Coldplay-Parachutes-CD-MP3-320-2000-GRP", "Coldplay", "Parachutes", 2000, FormatMP3, "320", "CD"),
		mc("Coldplay-Parachutes-CD-320-2000-GRP", "Coldplay", "Parachutes", 2000, "", "320", "CD"),
		mc("Coldplay-Parachutes-CD-V0-2000-GRP", "Coldplay", "Parachutes", 2000, "", "V0", "CD"),
		mc("Coldplay-Parachutes-CD-MP3-V0-2000-GRP", "Coldplay", "Parachutes", 2000, FormatMP3, "V0", "CD"),
		mc("Coldplay-Parachutes-CD-MP3-V2-2000-GRP", "Coldplay", "Parachutes", 2000, FormatMP3, "V2", "CD"),
		mc("Coldplay-Parachutes-CD-MP3-192-2000-GRP", "Coldplay", "Parachutes", 2000, FormatMP3, "192", "CD"),
		mc("Coldplay-Parachutes-CD-AAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatAAC, "", "CD"),
		mc("Coldplay-Parachutes-CD-ALAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatALAC, "", "CD"),
		mc("Coldplay-Parachutes-CD-M4A-2000-GRP", "Coldplay", "Parachutes", 2000, FormatAAC, "", "CD"),
		mc("Coldplay-Parachutes-WEB-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "WEB"),
		mc("Coldplay-Parachutes-WEBFLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "WEB"),
		mc("Coldplay-Parachutes-WEB-DL-2000-GRP", "Coldplay", "Parachutes", 2000, "", "", "WEB"),
		mc("Coldplay-Parachutes-VINYLRIP-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "Vinyl"),
		mc("Coldplay-Parachutes-LPRIP-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "Vinyl"),
		mc("Coldplay-Parachutes-LP-2000-GRP", "Coldplay", "Parachutes", 2000, "", "", ""),
		mc("Coldplay-Parachutes-16BIT-CD-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "CD"),
		mc("Coldplay-Parachutes-24BIT-96KHZ-WEB-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "WEB"),
		mc("Coldplay-Parachutes-HIRES-WEB-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "WEB"),
		mc("Coldplay-Parachutes-EP-WEB-2000-GRP", "Coldplay", "Parachutes", 2000, "", "", "WEB"),
		mc("Coldplay-Parachutes-SINGLE-WEB-2000-GRP", "Coldplay", "Parachutes", 2000, "", "", "WEB"),
		mc("Coldplay-Parachutes-PROPER-CD-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "CD"),
		mc("Coldplay-Parachutes-REPACK-CD-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "CD"),
		mc("Coldplay-Parachutes-READNFO-CD-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "CD"),
		mc("Coldplay-Parachutes-LIMITED_EDITION-CD-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "CD"),
		mc("Coldplay-Parachutes-JAPAN-CD-FLAC-2000-GRP", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "CD"),
		mc("Coldplay-Parachutes-CD-FLAC-2000-GRP-postbot", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "CD"),
		mc("Coldplay-Parachutes-CD-FLAC-2000-GRP-xpost", "Coldplay", "Parachutes", 2000, FormatFLAC, "", "CD"),
		mc("Coldplay-X_and_Y-CD-FLAC-2005-GRP", "Coldplay", "X and Y", 2005, FormatFLAC, "", "CD"),
		mc("Coldplay-Mylo_Xyloto-CD-FLAC-2011-GRP", "Coldplay", "Mylo Xyloto", 2011, FormatFLAC, "", "CD"),
		mc("Coldplay-Music_of_the_Spheres-WEB-2021-GRP", "Coldplay", "Music of the Spheres", 2021, "", "", "WEB"),
		mc("Coldplay-Moon_Music-WEB-2024-GRP", "Coldplay", "Moon Music", 2024, "", "", "WEB"),
		mc("Coldplay-2005-CD-FLAC-2005-GRP", "Coldplay", "2005", 2005, FormatFLAC, "", "CD"),
		mc("Prince-1999-CD-FLAC-1982-GRP", "Prince", "1999", 1982, FormatFLAC, "", "CD"),
		mc("Prince-1999-WEB-FLAC-2019-GRP", "Prince", "1999", 2019, FormatFLAC, "", "WEB"),
		mc("Blur-13-CD-FLAC-1999-GRP", "Blur", "13", 1999, FormatFLAC, "", "CD"),
		mc("Adele-21-WEB-2011-GRP", "Adele", "21", 2011, "", "", "WEB"),
		mc("Adele-25-WEB-2015-GRP", "Adele", "25", 2015, "", "", "WEB"),
		mc("Adele-30-WEB-2021-GRP", "Adele", "30", 2021, "", "", "WEB"),
		mc("Ed_Sheeran-÷-WEB-2017-GRP", "Ed Sheeran", "÷", 2017, "", "", "WEB"),
		mc("Ed_Sheeran-Plus-WEB-2011-GRP", "Ed Sheeran", "Plus", 2011, "", "", "WEB"),
		mc("The_1975-I_Like_It_When_You_Sleep-WEB-2016-GRP", "The 1975", "I Like It When You Sleep", 2016, "", "", "WEB"),
		mc("Mumford_and_Sons-Sigh_No_More-CD-FLAC-2009-GRP", "Mumford and Sons", "Sigh No More", 2009, FormatFLAC, "", "CD"),
		mc("AC_DC-Back_in_Black-CD-FLAC-1980-GRP", "AC DC", "Back in Black", 1980, FormatFLAC, "", "CD"),
		mc("Guns_N_Roses-Appetite_for_Destruction-CD-FLAC-1987-GRP", "Guns N Roses", "Appetite for Destruction", 1987, FormatFLAC, "", "CD"),
		mc("Guns_N'_Roses-Appetite_for_Destruction-CD-FLAC-1987-GRP", "Guns N' Roses", "Appetite for Destruction", 1987, FormatFLAC, "", "CD"),
		mc("Pearl_Jam-Ten-CD-FLAC-1991-GRP", "Pearl Jam", "Ten", 1991, FormatFLAC, "", "CD"),
		mc("U2-The_Joshua_Tree-CD-FLAC-1987-GRP", "U2", "The Joshua Tree", 1987, FormatFLAC, "", "CD"),
		mc("U2-War-CD-FLAC-1983-GRP", "U2", "War", 1983, FormatFLAC, "", "CD"),
		mc("Queen-A_Night_at_the_Opera-CD-FLAC-1975-GRP", "Queen", "A Night at the Opera", 1975, FormatFLAC, "", "CD"),
		mc("ABBA-Gold-CD-FLAC-1992-GRP", "ABBA", "Gold", 1992, FormatFLAC, "", "CD"),
		mc("Dire_Straits-Brothers_in_Arms-CD-FLAC-1985-GRP", "Dire Straits", "Brothers in Arms", 1985, FormatFLAC, "", "CD"),
		// P2P with spaces
		mc("Daft Punk - Random Access Memories (2013) [FLAC]", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", ""),
		mc("Daft Punk - Random Access Memories (2013) [FLAC 24bit-88.2kHz]", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", ""),
		mc("Daft Punk - Random Access Memories (2013) [MP3 320]", "Daft Punk", "Random Access Memories", 2013, FormatMP3, "320", ""),
		mc("Daft Punk - Random Access Memories (2013) [MP3 320kbps]", "Daft Punk", "Random Access Memories", 2013, FormatMP3, "320", ""),
		mc("Daft Punk - Random Access Memories (2013) [MP3 V0]", "Daft Punk", "Random Access Memories", 2013, FormatMP3, "V0", ""),
		mc("Daft Punk - Random Access Memories (2013) [WEB FLAC]", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", "WEB"),
		mc("Daft Punk - Random Access Memories (2013) [Vinyl FLAC]", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", "Vinyl"),
		mc("Daft Punk - Random Access Memories (2013) [CD FLAC]", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", "CD"),
		mc("Daft Punk - Random Access Memories [2013] [FLAC]", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", ""),
		mc("Daft Punk - Random Access Memories (FLAC) 2013", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", ""),
		mc("Daft Punk - Random Access Memories - 2013 - FLAC", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", ""),
		mc("Daft Punk - 2013 - Random Access Memories [FLAC]", "Daft Punk", "", 2013, FormatFLAC, "", ""),
		mc("Daft Punk - Random Access Memories", "Daft Punk", "Random Access Memories", 0, "", "", ""),
		mc("Daft Punk - Random Access Memories (Deluxe Edition) (2013) [FLAC]", "Daft Punk", "", 2013, FormatFLAC, "", ""),
		mc("Daft Punk - Random Access Memories (Deluxe Edition) [FLAC]", "Daft Punk", "", 0, FormatFLAC, "", ""),
		mc("Daft Punk - Random Access Memories (Drumless Edition) (2023) [FLAC]", "Daft Punk", "", 2023, FormatFLAC, "", ""),
		mc("Daft Punk - Alive 2007 (2007) [FLAC]", "Daft Punk", "Alive 2007", 2007, FormatFLAC, "", ""),
		mc("Daft Punk - Alive 1997 (2001) [FLAC]", "Daft Punk", "Alive 1997", 2001, FormatFLAC, "", ""),
		mc("Daft Punk - Tron Legacy (2010)", "Daft Punk", "Tron Legacy", 2010, "", "", ""),
		mc("Daft Punk - TRON Legacy (Original Motion Picture Soundtrack) (2010) FLAC", "Daft Punk", "", 2010, FormatFLAC, "", ""),
		mc("Daft Punk - Discography (1997-2021) [MP3 320]", "Daft Punk", "Discography", 0, FormatMP3, "320", ""),
		mc("Daft Punk - Discography [FLAC]", "Daft Punk", "Discography", 0, FormatFLAC, "", ""),
		mc("Daft Punk - Complete Discography (1993-2021) FLAC", "Daft Punk", "", 0, FormatFLAC, "", ""),
		mc("Daft Punk - The Collection [MP3]", "Daft Punk", "The Collection", 0, FormatMP3, "", ""),
		mc("Daft Punk - Anthology (FLAC)", "Daft Punk", "Anthology", 0, FormatFLAC, "", ""),
		mc("Daft Punk - Box Set (2021) [FLAC]", "Daft Punk", "", 2021, FormatFLAC, "", ""),
		mc("Daft Punk - Studio Albums Collection (1997-2013) [FLAC]", "Daft Punk", "", 0, FormatFLAC, "", ""),
		mc("[FLAC] Daft Punk - Random Access Memories (2013)", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", ""),
		mc("(2013) Daft Punk - Random Access Memories [FLAC]", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", ""),
		mc("[2013] Daft Punk - Random Access Memories [FLAC]", "Daft Punk", "Random Access Memories", 2013, FormatFLAC, "", ""),
		mc("AC-DC - Back in Black (1980) [FLAC]", "AC-DC", "Back in Black", 1980, FormatFLAC, "", ""),
		mc("AC/DC - Back in Black (1980) [FLAC]", "AC/DC", "Back in Black", 1980, FormatFLAC, "", ""),
		mc("Blink-182 - Enema of the State (1999) [FLAC]", "Blink-182", "Enema of the State", 1999, FormatFLAC, "", ""),
		mc("Jay-Z - The Blueprint (2001) [FLAC]", "Jay-Z", "The Blueprint", 2001, FormatFLAC, "", ""),
		mc("Wu-Tang Clan - Enter the Wu-Tang (36 Chambers) (1993) [FLAC]", "Wu-Tang Clan", "", 1993, FormatFLAC, "", ""),
		mc("Sigur Rós - ( ) (2002) [FLAC]", "Sigur Rós", "", 2002, FormatFLAC, "", ""),
		mc("Sigur Rós - Ágætis byrjun (1999) [FLAC]", "Sigur Rós", "Ágætis byrjun", 1999, FormatFLAC, "", ""),
		mc("Björk - Post (1995) [FLAC]", "Björk", "Post", 1995, FormatFLAC, "", ""),
		mc("Mötley Crüe - Dr. Feelgood (1989) [FLAC]", "Mötley Crüe", "Dr. Feelgood", 1989, FormatFLAC, "", ""),
		mc("Beyoncé - Lemonade (2016) [FLAC]", "Beyoncé", "Lemonade", 2016, FormatFLAC, "", ""),
		mc("Tyler, The Creator - IGOR (2019) [FLAC]", "Tyler, The Creator", "IGOR", 2019, FormatFLAC, "", ""),
		mc("Florence + the Machine - Lungs (2009) [FLAC]", "Florence + the Machine", "Lungs", 2009, FormatFLAC, "", ""),
		mc("Various Artists - Pulp Fiction OST (1994) [FLAC]", "Various Artists", "Pulp Fiction OST", 1994, FormatFLAC, "", ""),
		mc("VA - Now That's What I Call Music! 100 (2018) [MP3 320]", "VA", "Now That's What I Call Music! 100", 2018, FormatMP3, "320", ""),
		mc("VA - Café del Mar Vol. 1 (1994) [FLAC]", "VA", "Café del Mar Vol. 1", 1994, FormatFLAC, "", ""),
		mc("The Beatles - Abbey Road (Remastered) (2009) [FLAC]", "The Beatles", "", 2009, FormatFLAC, "", ""),
		mc("The Beatles - 1 (2000) [FLAC]", "The Beatles", "1", 2000, FormatFLAC, "", ""),
		mc("The Beatles - 1962-1966 (1973) [FLAC]", "The Beatles", "1962-1966", 1973, FormatFLAC, "", ""),
		mc("The Beatles - 1967-1970 (Remastered) [FLAC]", "The Beatles", "", 0, FormatFLAC, "", ""),
		mc("Prince - 1999 (1982) [FLAC]", "Prince", "1999", 1982, FormatFLAC, "", ""),
		mc("Prince - 1999 [FLAC]", "Prince", "1999", 0, FormatFLAC, "", ""),
		mc("Prince and The Revolution - Purple Rain (1984) [FLAC]", "Prince and The Revolution", "Purple Rain", 1984, FormatFLAC, "", ""),
		mc("Pink Floyd - The Dark Side of the Moon (1973) [24-96 FLAC]", "Pink Floyd", "The Dark Side of the Moon", 1973, FormatFLAC, "", ""),
		mc("Pink Floyd - The Dark Side of the Moon (1973) [24-192 FLAC]", "Pink Floyd", "The Dark Side of the Moon", 1973, FormatFLAC, "", ""),
		mc("Pink Floyd - The Dark Side of the Moon (1973) [16-44 FLAC]", "Pink Floyd", "The Dark Side of the Moon", 1973, FormatFLAC, "", ""),
		mc("Pink Floyd - The Dark Side of the Moon (1973) [Hi-Res FLAC]", "Pink Floyd", "The Dark Side of the Moon", 1973, FormatFLAC, "", ""),
		mc("Pink Floyd - The Dark Side of the Moon (1973) [Vinyl Rip 24bit 96kHz]", "Pink Floyd", "The Dark Side of the Moon", 1973, "", "", "Vinyl"),
		mc("Pink Floyd - The Wall (1979) [2CD] [FLAC]", "Pink Floyd", "The Wall", 1979, FormatFLAC, "", ""),
		mc("Pink Floyd - The Wall (1979) {2CD} FLAC", "Pink Floyd", "The Wall", 1979, FormatFLAC, "", ""),
		mc("Pink Floyd - The Wall - 1979 - CD - FLAC", "Pink Floyd", "The Wall", 1979, FormatFLAC, "", ""),
		mc("Pink Floyd - The Wall 1979 FLAC", "Pink Floyd", "The Wall", 1979, FormatFLAC, "", ""),
		mc("Pink Floyd - The Wall FLAC", "Pink Floyd", "The Wall", 0, FormatFLAC, "", ""),
		mc("Pink Floyd - The Wall MP3", "Pink Floyd", "The Wall", 0, FormatMP3, "", ""),
		mc("Pink Floyd - The Wall mp3 320 kbps", "Pink Floyd", "The Wall", 0, FormatMP3, "320", ""),
		mc("Pink Floyd - The Wall (1979) [MP3 CBR 320]", "Pink Floyd", "The Wall", 1979, FormatMP3, "320", ""),
		mc("Pink Floyd - The Wall (1979) [MP3 VBR V0]", "Pink Floyd", "The Wall", 1979, FormatMP3, "V0", ""),
		mc("Pink Floyd - The Wall (1979) [AAC 256]", "Pink Floyd", "The Wall", 1979, FormatAAC, "256", ""),
		mc("Pink Floyd - The Wall (1979) [ALAC]", "Pink Floyd", "The Wall", 1979, FormatALAC, "", ""),
		mc("Pink Floyd - The Wall (1979) [Apple Lossless]", "Pink Floyd", "The Wall", 1979, "", "", ""),
		mc("Pink Floyd - The Wall (1979) [WAV]", "Pink Floyd", "The Wall", 1979, "", "", ""),
		mc("Pink Floyd - The Wall (1979) [OGG]", "Pink Floyd", "The Wall", 1979, "", "", ""),
		mc("Pink Floyd - The Wall (1979) [APE]", "Pink Floyd", "The Wall", 1979, "", "", ""),
		mc("Pink Floyd - The Wall (1979) [DSD]", "Pink Floyd", "The Wall", 1979, "", "", ""),
		mc("Pink Floyd - The Wall (1979) [SACD]", "Pink Floyd", "The Wall", 1979, "", "", ""),
		mc("Pink Floyd - The Wall (1979) [FLAC] [Japan]", "Pink Floyd", "The Wall", 1979, FormatFLAC, "", ""),
		mc("Pink Floyd - The Wall (1979) [FLAC] [Mobile Fidelity]", "Pink Floyd", "The Wall", 1979, FormatFLAC, "", ""),
		mc("Pink Floyd - The Wall (1979) [FLAC] [CAT123]", "Pink Floyd", "The Wall", 1979, FormatFLAC, "", ""),
		mc("Pink Floyd - The Wall.zip", "Pink Floyd", "The Wall.zip", 0, "", "", ""),
		mc("Pink Floyd - The Wall.nzb", "Pink Floyd", "The Wall", 0, "", "", ""),
		mc("Pink Floyd - The Wall.torrent", "Pink Floyd", "The Wall", 0, "", "", ""),
		mc("  Pink Floyd - The Wall  ", "Pink Floyd", "The Wall", 0, "", "", ""),
		mc("Pink Floyd   -   The Wall", "Pink Floyd", "The Wall", 0, "", "", ""),
		mc("Pink Floyd - The Wall - Live in Berlin (1990) [FLAC]", "Pink Floyd", "The Wall - Live in Berlin", 1990, FormatFLAC, "", ""),
		mc("Pink Floyd - Pulse (Live) (1995) [FLAC]", "Pink Floyd", "", 1995, FormatFLAC, "", ""),
		mc("Pink Floyd - A Momentary Lapse of Reason (1987) 2011 Remaster [FLAC]", "Pink Floyd", "A Momentary Lapse of Reason", 1987, FormatFLAC, "", ""),
		mc("Pink Floyd - A Momentary Lapse of Reason (2011 Remaster) [FLAC]", "Pink Floyd", "A Momentary Lapse of Reason", 2011, FormatFLAC, "", ""),
		mc("Pink Floyd - A Momentary Lapse of Reason (Remastered 2011) [FLAC]", "Pink Floyd", "A Momentary Lapse of Reason", 2011, FormatFLAC, "", ""),
		mc("Pink Floyd - Animals (2018 Remix) (2018) [FLAC]", "Pink Floyd", "Animals", 2018, FormatFLAC, "", ""),
		mc("Pink Floyd - 2001 (1977) [FLAC]", "Pink Floyd", "2001", 1977, FormatFLAC, "", ""),
		mc("Pink Floyd - Animals (1977) FLAC (tracks+cue)", "Pink Floyd", "Animals", 1977, FormatFLAC, "", ""),
		mc("Pink Floyd - Animals (1977) FLAC (image+.cue)", "Pink Floyd", "Animals", 1977, FormatFLAC, "", ""),
		mc("Pink Floyd - Animals (1977) FLAC EAC", "Pink Floyd", "Animals", 1977, FormatFLAC, "", ""),
		mc("Pink Floyd - Animals (1977) EAC FLAC", "Pink Floyd", "Animals", 1977, FormatFLAC, "", ""),
		mc("Pink Floyd - Animals (1977) [EAC] [FLAC]", "Pink Floyd", "Animals", 1977, FormatFLAC, "", ""),
		mc("Pink Floyd - Animals (1977) [CD] [EAC-FLAC]", "Pink Floyd", "Animals", 1977, FormatFLAC, "", "CD"),
		mc("Pink Floyd - Animals (1977) [LP] [FLAC]", "Pink Floyd", "Animals", 1977, FormatFLAC, "", ""),
		mc("Pink Floyd - Animals (1977) [Vinyl] [FLAC 24bit]", "Pink Floyd", "Animals", 1977, FormatFLAC, "", "Vinyl"),
		mc("Pink Floyd - Animals (1977) [Vinyl, 24-96] [FLAC]", "Pink Floyd", "Animals", 1977, FormatFLAC, "", "Vinyl"),
		mc("Pink Floyd - Animals (1977) [Web] [FLAC]", "Pink Floyd", "Animals", 1977, FormatFLAC, "", "WEB"),
		mc("Pink Floyd - Animals (1977) [CDDA]", "Pink Floyd", "Animals", 1977, "", "", "CD"),
		mc("Pink Floyd - Animals (1977) [CD-R]", "Pink Floyd", "Animals", 1977, "", "", "CD"),
		mc("Pink Floyd - Animals (1977) [CDRip]", "Pink Floyd", "Animals", 1977, "", "", "CD"),
		mc("Pink Floyd - Animals (1977) [2xCD]", "Pink Floyd", "Animals", 1977, "", "", ""),
		mc("Pink Floyd - Animals (1977) [3CD]", "Pink Floyd", "Animals", 1977, "", "", "CD"),
	}

	failed := 0
	for _, c := range cases {
		got := ParseRelease(c.in)
		var bad []string
		if got.Artist != c.artist {
			bad = append(bad, fmt.Sprintf("artist %q want %q", got.Artist, c.artist))
		}
		if c.album != "" && got.Album != c.album {
			bad = append(bad, fmt.Sprintf("album %q want %q", got.Album, c.album))
		}
		if c.year != 0 && got.Year != c.year {
			bad = append(bad, fmt.Sprintf("year %d want %d", got.Year, c.year))
		}
		if c.format != "" && got.Format != c.format {
			bad = append(bad, fmt.Sprintf("format %q want %q", got.Format, c.format))
		}
		if c.bitrate != "" && got.Bitrate != c.bitrate {
			bad = append(bad, fmt.Sprintf("bitrate %q want %q", got.Bitrate, c.bitrate))
		}
		if c.source != "" && got.Source != c.source {
			bad = append(bad, fmt.Sprintf("source %q want %q", got.Source, c.source))
		}
		if len(bad) > 0 {
			failed++
			t.Errorf("%q: %s", c.in, strings.Join(bad, "; "))
		}
	}
	t.Logf("%d music names, %d wrong", len(cases), failed)
}

func TestParseReleaseTimeBudget(t *testing.T) {
	long := 10000
	unit := func(s string) string { return strings.Repeat(s, long/len(s)+1)[:long] }
	for name, in := range map[string]string{
		"dashes":     unit("-"),
		"scene":      unit("Artist-Album-"),
		"brackets":   unit("[a]"),
		"parens":     unit("("),
		"space-dash": unit(" - "),
		"years":      unit("2019-"),
		"letters":    unit("a"),
		"cat":        unit("(ABC123)"),
		"unicode":    unit("é"),
	} {
		start := time.Now()
		ParseRelease(in)
		ParseReleaseFor(in, "Artist")
		if d := time.Since(start); d > 2*time.Second*slowdown {
			t.Errorf("%s: took %v", name, d)
		}
	}
}

// A scene name cuts at its first dash, so an artist with a dash in the name is
// only read right when the artist is known (a search always knows it).
func TestParseReleaseForHyphenatedArtists(t *testing.T) {
	for _, c := range []struct{ in, artist, album string }{
		{"Run-DMC-Raising_Hell-CD-FLAC-1986-GRP", "Run-DMC", "Raising Hell"},
		{"Blink-182-Enema_of_the_State-CD-FLAC-1999-GRP", "Blink-182", "Enema of the State"},
		{"Jay-Z-The_Blueprint-CD-FLAC-2001-GRP", "Jay-Z", "The Blueprint"},
		{"AC-DC-Back_in_Black-CD-FLAC-1980-GRP", "AC-DC", "Back in Black"},
		{"Alt-J-An_Awesome_Wave-WEB-2012-GRP", "Alt-J", "An Awesome Wave"},
		{"Ne-Yo-Year_of_the_Gentleman-CD-FLAC-2008-GRP", "Ne-Yo", "Year of the Gentleman"},
		{"Pearl_Jam-Ten-CD-FLAC-1991-GRP", "Pearl Jam", "Ten"},
	} {
		got := ParseReleaseFor(c.in, c.artist)
		if got.Artist != c.artist || got.Album != c.album {
			t.Errorf("%q: artist %q album %q, want %q and %q", c.in, got.Artist, got.Album, c.artist, c.album)
		}
	}
}

func TestParseReleaseReadsYearBeforeAlbum(t *testing.T) {
	for in, year := range map[string]int{
		"Daft Punk - 2013 - Random Access Memories [FLAC]": 2013,
		"Daft Punk - (2013) - Random Access Memories":      2013,
		"Daft Punk - [2013] - Random Access Memories":      2013,
	} {
		got := ParseRelease(in)
		if got.Year != year || got.Album != "Random Access Memories" {
			t.Errorf("%q: year %d album %q", in, got.Year, got.Album)
		}
	}
	if got := ParseRelease("Prince - 1999 [FLAC]"); got.Album != "1999" {
		t.Errorf("an album named after a year lost its name: %q", got.Album)
	}
}
