package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// TestMatchesMovieCorpus holds release names against films that are easy to
// confuse: the same title in other years, sequels, remakes, titles with years,
// numbers, accents and punctuation, and other titles of the film.
func TestMatchesMovieCorpus(t *testing.T) {
	type c struct {
		release string
		want    bool
	}
	groups := []struct {
		movie library.Movie
		cases []c
	}{
		{library.Movie{Title: "Dune", Year: 2021}, []c{
			{"Dune.2021.1080p.BluRay.x264-GRP", true},
			{"Dune.2021.2160p.UHD.BluRay.x265.HDR-GRP", true},
			{"Dune (2021) 1080p", true},
			{"Dune.1984.1080p.BluRay.x264-GRP", false},
			{"Dune.Part.Two.2024.1080p.WEB-DL", false},
			{"Dune.2000.1080p", false},
			{"Dune.1080p.BluRay.x264-GRP", true},
			{"Dune.Prophecy.S01E01.1080p.WEB-DL", false},
			{"Dune.2021.S01E01.1080p", false},
			{"Dune.Part.One.2021.1080p.BluRay", false},
			{"The.Dune.2021.1080p", true},
			{"Dunes.2021.1080p", false},
			{"Dune.2022.1080p", false},
			{"Dune.2020.1080p", false},
		}},
		{library.Movie{Title: "Blade Runner 2049", Year: 2017}, []c{
			{"Blade.Runner.2049.2017.1080p.BluRay.x264-GRP", true},
			{"Blade Runner 2049 (2017) 1080p", true},
			{"Blade.Runner.1982.Final.Cut.1080p.BluRay", false},
			{"Blade.Runner.2049.1080p.BluRay", true},
			{"Blade.Runner.2017.1080p.BluRay", false},
			{"Blade.Runner.The.Final.Cut.1982.1080p", false},
		}},
		{library.Movie{Title: "Blade Runner", Year: 1982}, []c{
			{"Blade.Runner.1982.Final.Cut.1080p.BluRay.x264-GRP", true},
			{"Blade.Runner.2049.2017.1080p.BluRay.x264-GRP", false},
			{"Blade.Runner.1982.REMASTERED.1080p", true},
			{"Blade.Runner.2049.1080p", false},
		}},
		{library.Movie{Title: "2012", Year: 2009}, []c{
			{"2012.2009.1080p.BluRay.x264-GRP", true},
			{"2012 (2009) 1080p", true},
			{"2012.1080p.BluRay", true},
			{"2012.2012.1080p", false},
			{"Mayan.Apocalypse.2012.1080p", false},
		}},
		{library.Movie{Title: "1917", Year: 2019}, []c{
			{"1917.2019.1080p.BluRay.x264-GRP", true},
			{"1917.2019.2160p.UHD.BluRay.x265", true},
			{"1917.1080p.BluRay", true},
			{"1917.1919.1080p", false},
			{"1917.2020.1080p", false},
		}},
		{library.Movie{Title: "2001: A Space Odyssey", Year: 1968}, []c{
			{"2001.A.Space.Odyssey.1968.1080p.BluRay.x264-GRP", true},
			{"2001 A Space Odyssey (1968) 4K", true},
			{"2010.1984.1080p", false},
			{"2001.A.Space.Odyssey.1068.1080p", false},
		}},
		{library.Movie{Title: "The Thing", Year: 1982}, []c{
			{"The.Thing.1982.1080p.BluRay.x264-GRP", true},
			{"Thing.1982.1080p.BluRay", true},
			{"The.Thing.2011.1080p.BluRay", false},
			{"The.Thing.About.Pam.2022.1080p", false},
			{"The.Thing.from.Another.World.1951.1080p", false},
		}},
		{library.Movie{Title: "The Thing", Year: 2011}, []c{
			{"The.Thing.2011.1080p.BluRay", true},
			{"The.Thing.1982.1080p.BluRay", false},
		}},
		{library.Movie{Title: "Total Recall", Year: 1990}, []c{
			{"Total.Recall.1990.1080p", true},
			{"Total.Recall.2012.1080p", false},
			{"Total.Recall.1990.Directors.Cut.1080p", true},
		}},
		{library.Movie{Title: "Ghostbusters", Year: 1984}, []c{
			{"Ghostbusters.1984.REMASTERED.1080p", true},
			{"Ghostbusters.2016.1080p", false},
			{"Ghostbusters.II.1989.1080p", false},
			{"Ghostbusters.Afterlife.2021.1080p", false},
			{"Ghostbusters.2.1989.1080p", false},
		}},
		{library.Movie{Title: "Toy Story 2", Year: 1999}, []c{
			{"Toy.Story.2.1999.1080p.BluRay", true},
			{"Toy.Story.II.1999.1080p.BluRay", false},
			{"Toy.Story.1995.1080p", false},
			{"Toy.Story.3.2010.1080p", false},
			{"Toy.Story.2.2019.1080p", false},
		}},
		{library.Movie{Title: "Kill Bill: Vol. 1", Year: 2003}, []c{
			{"Kill.Bill.Vol.1.2003.1080p.BluRay", true},
			{"Kill.Bill.Volume.1.2003.1080p", false},
			{"Kill.Bill.Vol.2.2004.1080p", false},
			{"Kill.Bill.Vol.1.2004.1080p", false},
		}},
		{library.Movie{Title: "Harry Potter and the Deathly Hallows: Part 1", Year: 2010}, []c{
			{"Harry.Potter.and.the.Deathly.Hallows.Part.1.2010.1080p.BluRay", true},
			{"Harry.Potter.and.the.Deathly.Hallows.Part.I.2010.1080p.BluRay", false},
			{"Harry.Potter.and.the.Deathly.Hallows.Part.2.2011.1080p.BluRay", false},
			{"Harry.Potter.and.the.Deathly.Hallows.2010.1080p.BluRay", false},
		}},
		{library.Movie{Title: "The Godfather Part II", Year: 1974}, []c{
			{"The.Godfather.Part.II.1974.1080p", true},
			{"Godfather.Part.II.1974.1080p", true},
			{"The.Godfather.1972.1080p", false},
			{"The.Godfather.Part.III.1990.1080p", false},
			{"The.Godfather.Part.2.1974.1080p", false},
		}},
		{library.Movie{Title: "Fast & Furious", Year: 2009}, []c{
			{"Fast.and.Furious.2009.1080p", true},
			{"Fast.&.Furious.2009.1080p", true},
			{"Fast.Furious.2009.1080p", false},
			{"The.Fast.and.the.Furious.2001.1080p", false},
			{"Fast.and.Furious.6.2013.1080p", false},
			{"Fast.&.Furious.Presents.Hobbs.&.Shaw.2019.1080p", false},
		}},
		{library.Movie{Title: "Mr. & Mrs. Smith", Year: 2005}, []c{
			{"Mr.and.Mrs.Smith.2005.1080p", true},
			{"Mr.&.Mrs.Smith.2005.1080p", true},
			{"Mr.Mrs.Smith.2005.1080p", false},
			{"Mr.and.Mrs.Smith.1941.1080p", false},
			{"Mr.and.Mrs.Smith.2024.S01E01.1080p", false},
		}},
		{library.Movie{Title: "WALL·E", Year: 2008}, []c{
			{"WALL-E.2008.1080p.BluRay", true},
			{"WALL.E.2008.1080p.BluRay", true},
			{"Wall-E.2008.1080p", true},
			{"WallE.2008.1080p", true},
		}},
		{library.Movie{Title: "Se7en", Year: 1995}, []c{
			{"Se7en.1995.1080p", true},
			{"Seven.1995.1080p", false},
			{"Se7en.1995.REMASTERED.1080p", true},
		}},
		{library.Movie{Title: "Amélie", Year: 2001, AltTitles: []string{"Le Fabuleux Destin d'Amélie Poulain", "The Fabulous Destiny of Amélie Poulain"}}, []c{
			{"Amelie.2001.1080p.BluRay", true},
			{"Le.Fabuleux.Destin.dAmelie.Poulain.2001.1080p", true},
			{"Le.Fabuleux.Destin.d.Amelie.Poulain.2001.1080p", true},
			{"Le.Fabuleux.Destin.dAmelie.Poulain.1080p", false}, // another name has to say its year
			{"Amelie.2003.1080p", false},
			{"Amelie.Poulain.2001.1080p", false},
		}},
		{library.Movie{Title: "Das Boot", Year: 1981, AltTitles: []string{"The Boat"}}, []c{
			{"Das.Boot.1981.Directors.Cut.1080p", true},
			{"Das.Boot.1981.1080p", true},
			{"The.Boat.1981.1080p", true},
			{"Boot.1981.1080p", false},
			{"Das.Boot.1985.1080p", false},
		}},
		{library.Movie{Title: "Straße", Year: 2020}, []c{
			{"Strasse.2020.1080p", true},
			{"Straße.2020.1080p", true},
		}},
		{library.Movie{Title: "Üb", Year: 2020, AltTitles: []string{"Über den Wolken"}}, []c{
			{"Ueber.den.Wolken.2020.1080p", true},
			{"Uber.den.Wolken.2020.1080p", true},
			{"Über.den.Wolken.2020.1080p", true},
		}},
		{library.Movie{Title: "Мастер и Маргарита", Year: 2024}, []c{
			{"Мастер.и.Маргарита.2024.1080p.WEB-DL", true},
			{"Master.i.Margarita.2024.1080p.WEB-DL", false},
			{"Мастер.и.Маргарита.1994.1080p", false},
		}},
		{library.Movie{Title: "千と千尋の神隠し", Year: 2001, AltTitles: []string{"Spirited Away"}}, []c{
			{"Spirited.Away.2001.1080p.BluRay", true},
			{"千と千尋の神隠し.2001.1080p.BluRay", true},
			{"Spirited.Away.1080p.BluRay", false},
			{"Spirited.2001.1080p", false},
		}},
		{library.Movie{Title: "Cam", Year: 2018}, []c{
			{"Cam.2018.1080p.WEBRip", true},
			{"Cam.2018.HDCAM", true},
			{"Cam.2019.1080p", false},
		}},
		{library.Movie{Title: "It", Year: 2017}, []c{
			{"It.2017.1080p.BluRay", true},
			{"It.2017.IMAX.1080p", true},
			{"It.Chapter.Two.2019.1080p", false},
			{"It.1990.1080p", false},
			{"It.Follows.2014.1080p", false},
		}},
		{library.Movie{Title: "Bad Boys", Year: 1995}, []c{
			{"Bad.Boys.1995.1080p", true},
			{"Bad.Boys.for.Life.2020.1080p", false},
			{"Bad.Boys.II.2003.1080p", false},
			{"Bad.Boys.2.2003.1080p", false},
			{"Star.Wars.Bad.Boys.1995.1080p", false},
		}},
		{library.Movie{Title: "The Nightmare Before Christmas", Year: 1993}, []c{
			{"The.Nightmare.Before.Christmas.1993.1080p", true},
			{"Tim.Burtons.The.Nightmare.Before.Christmas.1993.1080p", true},
			{"Disneys.The.Nightmare.Before.Christmas.1993.1080p", true},
			{"Tim.Burtons.The.Nightmare.Before.Christmas.1080p", false},
			{"Tim.Burtons.The.Nightmare.Before.Christmas.2000.1080p", false},
		}},
		{library.Movie{Title: "Godzilla", Year: 1998}, []c{
			{"Godzilla.1998.1080p", true},
			{"Godzilla.2014.1080p", false},
			{"Godzilla.King.of.the.Monsters.2019.1080p", false},
			{"Godzilla.vs.Kong.2021.1080p", false},
			{"Godzilla.1954.1080p", false},
		}},
		{library.Movie{Title: "Alien", Year: 1979}, []c{
			{"Alien.1979.Directors.Cut.1080p", true},
			{"Alien.1979.Final.Cut.2160p", true},
			{"Aliens.1986.1080p", false},
			{"Alien.3.1992.1080p", false},
			{"Alien.Resurrection.1997.1080p", false},
			{"Alien.Romulus.2024.1080p", false},
		}},
		{library.Movie{Title: "Paris, Texas", Year: 1984}, []c{
			{"Paris.Texas.1984.1080p", true},
			{"Paris,.Texas.1984.1080p", true},
			{"Paris.1984.1080p", false},
		}},
		{library.Movie{Title: "Léon: The Professional", Year: 1994}, []c{
			{"Leon.The.Professional.1994.1080p", true},
			{"Léon.The.Professional.1994.Directors.Cut.1080p", true},
			{"Leon.1994.1080p", false},
		}},
		{library.Movie{Title: "Ocean's Eleven", Year: 2001}, []c{
			{"Oceans.Eleven.2001.1080p", true},
			{"Ocean's.Eleven.2001.1080p", true},
			{"Ocean.s.Eleven.2001.1080p", true},
			{"Oceans.11.2001.1080p", false},
			{"Oceans.Eleven.1960.1080p", false},
		}},
		{library.Movie{Title: "Jurassic Park", Year: 1993}, []c{
			{"Jurassic.Park.1993.1080p", true},
			{"Jurassic.Park.III.2001.1080p", false},
			{"Jurassic.Park.3.2001.1080p", false},
			{"Jurassic.World.2015.1080p", false},
			{"Jurassic.Park.1993.3D.1080p", true},
			{"Jurassic.Park.1993.IMAX.1080p", true},
		}},
		{library.Movie{Title: "Wonder Woman 1984", Year: 2020}, []c{
			{"Wonder.Woman.1984.2020.1080p.WEB-DL", true},
			{"Wonder.Woman.2017.1080p", false},
		}},
		{library.Movie{Title: "Everything Everywhere All at Once", Year: 2022}, []c{
			{"Everything.Everywhere.All.at.Once.2022.1080p", true},
			{"Everything.Everywhere.All.At.Once.2022.1080p", true},
			{"Everything.Everywhere.All.At.Once.1080p", true},
			{"Everything.Everywhere.2022.1080p", false},
		}},
		{library.Movie{Title: "Dragon Ball Z: Resurrection 'F'", Year: 2015}, []c{
			{"Dragon.Ball.Z.Resurrection.F.2015.1080p", true},
			{"Dragon.Ball.Z.Resurrection.2015.1080p", false},
			{"Dragon.Ball.Z.2015.1080p", false},
		}},
		{library.Movie{Title: "Rocky", Year: 1976}, []c{
			{"Rocky.1976.1080p", true},
			{"Rocky.II.1979.1080p", false},
			{"Rocky.Balboa.2006.1080p", false},
			{"Rocky.IV.1985.1080p", false},
			{"Rocky.1976.REMASTERED.1080p", true},
			{"Rocky.2.1979.1080p", false},
		}},
		{library.Movie{Title: "A Quiet Place", Year: 2018}, []c{
			{"A.Quiet.Place.2018.1080p", true},
			{"Quiet.Place.2018.1080p", true},
			{"A.Quiet.Place.Part.II.2020.1080p", false},
			{"A.Quiet.Place.Day.One.2024.1080p", false},
		}},
	}
	checked := 0
	for _, g := range groups {
		for _, tc := range g.cases {
			checked++
			if got := matchesMovie(tc.release, g.movie); got != tc.want {
				t.Errorf("matchesMovie(%q, %q %d) = %v, want %v", tc.release, g.movie.Title, g.movie.Year, got, tc.want)
			}
		}
	}
	t.Logf("%d movie matches checked", checked)
}

// TestMatchesShowCorpus is the same for shows: other countries and years with
// the same name, spin-offs, punctuation, other alphabets and aliases.
func TestMatchesShowCorpus(t *testing.T) {
	type c struct {
		release string
		want    bool
	}
	groups := []struct {
		show  library.Series
		cases []c
	}{
		{library.Series{Title: "The Office", Year: 2005, OriginCountry: []string{"US"}}, []c{
			{"The.Office.US.S02E01.720p.WEB-DL", true},
			{"The.Office.S02E01.720p.WEB-DL", true},
			{"The.Office.UK.S01E01.DVDRip", false},
			{"The.Office.2005.S01E01.720p", true},
			{"The.Office.2001.S01E01.720p", false},
			{"The.Office.(US).S02E01.720p", true},
			{"The.Office.AU.S01E01.720p", false},
			{"Office.S02E01.720p", true},
			{"The.Office.Christmas.Party.S01E01.720p", false},
		}},
		{library.Series{Title: "The Office", Year: 2001, OriginCountry: []string{"GB"}}, []c{
			{"The.Office.UK.S01E01.DVDRip", true},
			{"The.Office.GB.S01E01.DVDRip", true},
			{"The.Office.US.S01E01.DVDRip", false},
			{"The.Office.2001.S01E01.DVDRip", true},
			{"The.Office.2005.S01E01.DVDRip", false},
			{"The.Office.(UK).S01E01.DVDRip", true},
		}},
		{library.Series{Title: "Heroes", Year: 2006}, []c{
			{"Heroes.S01E01.720p.HDTV", true},
			{"Heroes.2006.S01E01.720p.HDTV", true},
			{"Heroes.Reborn.S01E01.720p.HDTV", false},
			{"Heroes.Reborn.2015.S01E01.720p.HDTV", false},
			{"Heroes.2016.S01E01.720p.HDTV", false},
		}},
		{library.Series{Title: "Heroes Reborn", Year: 2015}, []c{
			{"Heroes.Reborn.S01E01.720p.HDTV", true},
			{"Heroes.S01E01.720p.HDTV", false},
		}},
		{library.Series{Title: "Dragon Ball"}, []c{
			{"Dragon.Ball.S01E01.720p", true},
			{"Dragon.Ball.Z.S01E01.720p", false},
			{"Dragon.Ball.Super.S01E01.720p", false},
			{"Dragon.Ball.GT.S01E01.720p", false},
			{"Dragon.Ball.Z.Kai.S01E01.720p", false},
		}},
		{library.Series{Title: "Dragon Ball Z"}, []c{
			{"Dragon.Ball.Z.S01E01.720p", true},
			{"Dragon.Ball.S01E01.720p", false},
			{"Dragon.Ball.Z.Kai.S01E01.720p", false},
		}},
		{library.Series{Title: "Doctor Who", Year: 2005}, []c{
			{"Doctor.Who.2005.S01E01.720p.HDTV", true},
			{"Doctor.Who.S01E01.720p.HDTV", true},
			{"Doctor.Who.1963.S01E01.720p.HDTV", false},
			{"Doctor.Who.2023.S01E01.720p.HDTV", false},
			{"Doctor.Who.Confidential.S01E01.720p", false},
		}},
		{library.Series{Title: "Doctor Who", Year: 1963}, []c{
			{"Doctor.Who.1963.S01E01.720p.HDTV", true},
			{"Doctor.Who.2005.S01E01.720p.HDTV", false},
		}},
		{library.Series{Title: "Scrubs", Year: 2026}, []c{
			{"Scrubs.2026.S01E01.1080p.WEB-DL", true},
			{"Scrubs.2001.S01E01.1080p.WEB-DL", false},
			{"Scrubs.S01E01.1080p.WEB-DL", true},
			{"Scrubs.S01E01.2026.1080p.WEB-DL", true},
		}},
		{library.Series{Title: "Scrubs", Year: 2001}, []c{
			{"Scrubs.2001.S01E01.1080p.WEB-DL", true},
			{"Scrubs.2026.S01E01.1080p.WEB-DL", false},
			{"Scrubs.S01E01.1080p.WEB-DL", true},
		}},
		{library.Series{Title: "Shameless", Year: 2011, OriginCountry: []string{"US"}}, []c{
			{"Shameless.US.S01E01.720p", true},
			{"Shameless.S01E01.720p", true},
			{"Shameless.UK.S01E01.720p", false},
			{"Shameless.2004.S01E01.720p", false},
			{"Shameless.2011.S01E01.720p", true},
		}},
		{library.Series{Title: "Marvel's Agents of S.H.I.E.L.D.", Year: 2013}, []c{
			{"Marvels.Agents.of.S.H.I.E.L.D.S01E01.720p", true},
			{"Marvels.Agents.of.SHIELD.S01E01.720p", true},
			{"Agents.of.S.H.I.E.L.D.S01E01.720p", false},
			{"Marvel's.Agents.of.S.H.I.E.L.D.S01E01.720p", true},
		}},
		{library.Series{Title: "Law & Order: Special Victims Unit", Year: 1999}, []c{
			{"Law.and.Order.Special.Victims.Unit.S01E01.720p", true},
			{"Law.&.Order.Special.Victims.Unit.S01E01.720p", true},
			{"Law.and.Order.SVU.S01E01.720p", false},
			{"Law.and.Order.S01E01.720p", false},
			{"Law.and.Order.Criminal.Intent.S01E01.720p", false},
		}},
		{library.Series{Title: "Law & Order: Special Victims Unit", Year: 1999, Aliases: []string{"Law & Order: SVU"}}, []c{
			{"Law.and.Order.SVU.S01E01.720p", true},
			{"Law.&.Order.SVU.S01E01.720p", true},
		}},
		{library.Series{Title: "9-1-1", Year: 2018}, []c{
			{"9-1-1.S01E01.720p", true},
			{"9.1.1.S01E01.720p", true},
			{"911.S01E01.720p", true},
			{"9-1-1.Lone.Star.S01E01.720p", false},
			{"9-1-1.Nashville.S01E01.720p", false},
		}},
		{library.Series{Title: "Mr. Robot", Year: 2015}, []c{
			{"Mr.Robot.S01E01.720p", true},
			{"Mr..Robot.S01E01.720p", true},
			{"Mr Robot S01E01 720p", true},
			{"MrRobot.S01E01.720p", true},
			{"Robot.S01E01.720p", false},
		}},
		{library.Series{Title: "M*A*S*H", Year: 1972}, []c{
			{"M.A.S.H.S01E01.DVDRip", true},
			{"MASH.S01E01.DVDRip", true},
			{"M*A*S*H.S01E01.DVDRip", true},
			{"AfterMASH.S01E01.DVDRip", false},
			{"Trapper.John.MD.S01E01.DVDRip", false},
		}},
		{library.Series{Title: "Grey's Anatomy", Year: 2005}, []c{
			{"Greys.Anatomy.S01E01.720p", true},
			{"Grey's.Anatomy.S01E01.720p", true},
			{"Grey.s.Anatomy.S01E01.720p", true},
			{"Grey.Anatomy.S01E01.720p", false},
			{"Station.19.S01E01.720p", false},
		}},
		{library.Series{Title: "NCIS", Year: 2003}, []c{
			{"NCIS.S01E01.720p", true},
			{"NCIS.Los.Angeles.S01E01.720p", false},
			{"NCIS.New.Orleans.S01E01.720p", false},
			{"NCIS.Hawaii.S01E01.720p", false},
			{"NCIS.Sydney.S01E01.720p", false},
		}},
		{library.Series{Title: "CSI: Crime Scene Investigation", Year: 2000}, []c{
			{"CSI.Crime.Scene.Investigation.S01E01.720p", true},
			{"CSI.S01E01.720p", false},
			{"CSI.Miami.S01E01.720p", false},
			{"CSI.NY.S01E01.720p", false},
		}},
		{library.Series{Title: "Star Trek", Year: 1966}, []c{
			{"Star.Trek.S01E01.720p", true},
			{"Star.Trek.1966.S01E01.720p", true},
			{"Star.Trek.The.Next.Generation.S01E01.720p", false},
			{"Star.Trek.Discovery.S01E01.720p", false},
			{"Star.Trek.Picard.S01E01.720p", false},
			{"Star.Trek.Lower.Decks.S01E01.720p", false},
		}},
		{library.Series{Title: "Hawaii Five-0", Year: 2010}, []c{
			{"Hawaii.Five-0.S01E01.720p", true},
			{"Hawaii.Five.0.S01E01.720p", true},
			{"Hawaii.Five-0.2010.S01E01.720p", true},
			{"Hawaii.Five-O.1968.S01E01.720p", false},
		}},
		{library.Series{Title: "Black-ish", Year: 2014}, []c{
			{"Black-ish.S01E01.720p", true},
			{"Blackish.S01E01.720p", true},
			{"Black.ish.S01E01.720p", true},
			{"Mixed-ish.S01E01.720p", false},
			{"Grown-ish.S01E01.720p", false},
		}},
		{library.Series{Title: "Игра престолов", Year: 2011, Aliases: []string{"Game of Thrones"}}, []c{
			{"Игра.престолов.S01E01.720p", true},
			{"Game.of.Thrones.S01E01.720p", true},
			{"Game.of.Thrones.2011.S01E01.720p", true},
			{"House.of.the.Dragon.S01E01.720p", false},
		}},
		{library.Series{Title: "Money Heist", Year: 2017, AltTitles: []string{"La casa de papel", "Haus des Geldes"}}, []c{
			{"Money.Heist.S01E01.1080p", true},
			{"La.Casa.de.Papel.S01E01.1080p", true},
			{"Haus.des.Geldes.S01E01.1080p", true},
			{"La.Casa.de.Papel.Korea.S01E01.1080p", false},
			{"Money.Heist.Korea.Joint.Economic.Area.S01E01.1080p", false},
		}},
		{library.Series{Title: "One Piece", Year: 1999}, []c{
			{"One.Piece.S01E01.1080p", true},
			{"One.Piece.1999.S01E01.1080p", true},
			{"One.Piece.2023.S01E01.1080p", false},
			{"[SubsPlease] One Piece - 1085 (1080p) [ABCD1234]", true},
			{"One.Piece.Film.Red.2022.1080p", false},
			{"One.Piece.-.1085.-.720p", true},
		}},
		{library.Series{Title: "One Piece", Year: 2023}, []c{
			{"One.Piece.2023.S01E01.1080p", true},
			{"One.Piece.1999.S01E01.1080p", false},
			{"One.Piece.S01E01.1080p", true},
		}},
		{library.Series{Title: "Attack on Titan", Aliases: []string{"Shingeki no Kyojin"}}, []c{
			{"[Erai-raws] Shingeki no Kyojin - 12 [1080p]", true},
			{"Attack.on.Titan.S04E01.1080p", true},
			{"[Group] Attack on Titan - 12 [1080p]", true},
			{"[Group] Attack on Titan - Junior High - 12 [1080p]", false},
			{"Attack.on.Titan.Junior.High.S01E01.1080p", false},
		}},
		{library.Series{Title: "Love Island", Year: 2015, OriginCountry: []string{"GB"}}, []c{
			{"Love.Island.UK.S10E01.1080p", true},
			{"Love.Island.S10E01.1080p", true},
			{"Love.Island.US.S05E01.1080p", false},
			{"Love.Island.AU.S05E01.1080p", false},
			{"Love.Island.USA.S05E01.1080p", false},
		}},
		{library.Series{Title: "Love Island USA", Year: 2019, OriginCountry: []string{"US"}}, []c{
			{"Love.Island.USA.S05E01.1080p", true},
			{"Love.Island.US.S05E01.1080p", true},
			{"Love.Island.UK.S10E01.1080p", false},
		}},
		{library.Series{Title: "Top Gear", Year: 2002, OriginCountry: []string{"GB"}}, []c{
			{"Top.Gear.S33E01.1080p", true},
			{"Top.Gear.UK.S33E01.1080p", true},
			{"Top.Gear.US.S01E01.1080p", false},
			{"Top.Gear.AU.S01E01.1080p", false},
			{"Top.Gear.2002.S33E01.1080p", true},
			{"Top.Gear.Winter.Olympics.S01E01.1080p", false},
		}},
		{library.Series{Title: "Sherlock", Year: 2010}, []c{
			{"Sherlock.S01E01.720p", true},
			{"Sherlock.2010.S01E01.720p", true},
			{"Sherlock.Holmes.S01E01.720p", false},
			{"Young.Sherlock.S01E01.720p", false},
			{"Sherlock.2022.S01E01.720p", false},
		}},
		{library.Series{Title: "Lost", Year: 2004}, []c{
			{"Lost.S01E01.720p", true},
			{"Lost.2004.S01E01.720p", true},
			{"Lost.in.Space.S01E01.720p", false},
			{"Lost.Girl.S01E01.720p", false},
		}},
		{library.Series{Title: "24", Year: 2001}, []c{
			{"24.S01E01.720p", true},
			{"24.2001.S01E01.720p", true},
			{"24.Legacy.S01E01.720p", false},
			{"24.Hours.S01E01.720p", false},
		}},
		{library.Series{Title: "Fear the Walking Dead", Year: 2015}, []c{
			{"Fear.the.Walking.Dead.S01E01.720p", true},
			{"The.Walking.Dead.S01E01.720p", false},
			{"Fear.the.Walking.Dead.Dead.in.the.Water.S01E01.720p", false},
		}},
		{library.Series{Title: "The Walking Dead", Year: 2010}, []c{
			{"The.Walking.Dead.S01E01.720p", true},
			{"Walking.Dead.S01E01.720p", true},
			{"Fear.the.Walking.Dead.S01E01.720p", false},
			{"The.Walking.Dead.World.Beyond.S01E01.720p", false},
			{"The.Walking.Dead.Dead.City.S01E01.720p", false},
			{"The.Walking.Dead.Daryl.Dixon.S01E01.720p", false},
		}},
		{library.Series{Title: "Élite", Year: 2018}, []c{
			{"Elite.S01E01.1080p", true},
			{"Élite.S01E01.1080p", true},
			{"Elite.Squad.S01E01.1080p", false},
			{"Elite.Short.Stories.S01E01.1080p", false},
		}},
		{library.Series{Title: "Tatort", Year: 1970, AltTitles: []string{"Crime Scene"}}, []c{
			{"Tatort.S01E01.720p", true},
			{"Crime.Scene.S01E01.720p", true},
			{"Tatort.Münster.S01E01.720p", false},
		}},
		{library.Series{Title: "Wetten, dass..?", Year: 1981}, []c{
			{"Wetten.dass.S01E01.720p", true},
			{"Wetten.dass.2021.01.01.720p", true},
		}},
		{library.Series{Title: "Jeopardy!", Year: 1984}, []c{
			{"Jeopardy.2026.11.17.720p.WEB", true},
			{"Jeopardy.S42E01.720p.WEB", true},
			{"Jeopardy.Masters.2026.11.17.720p.WEB", false},
			{"Celebrity.Jeopardy.2026.11.17.720p.WEB", false},
		}},
		{library.Series{Title: "The Daily Show", Year: 1996}, []c{
			{"The.Daily.Show.2026.11.17.Guest.720p.WEB", true},
			{"The.Daily.Show.2026-11-17.720p.WEB", true},
			{"Daily.Show.2026.11.17.720p.WEB", true},
			{"The.Daily.Show.with.Trevor.Noah.2026.11.17.720p.WEB", false},
			{"The.Colbert.Report.2026.11.17.720p.WEB", false},
			{"The.Daily.Show.17.11.2026.720p.WEB", true},
		}},
		{library.Series{Title: "Taskmaster", Year: 2015, OriginCountry: []string{"GB"}}, []c{
			{"Taskmaster.S15E01.720p", true},
			{"Taskmaster.NZ.S03E01.720p", false},
			{"Taskmaster.AU.S03E01.720p", false},
			{"Taskmaster.US.S01E01.720p", false},
		}},
		{library.Series{Title: "Big Brother", Year: 2000, OriginCountry: []string{"US"}}, []c{
			{"Big.Brother.US.S25E01.720p", true},
			{"Big.Brother.S25E01.720p", true},
			{"Big.Brother.UK.S25E01.720p", false},
			{"Big.Brother.Brasil.S25E01.720p", false},
			{"Celebrity.Big.Brother.S25E01.720p", false},
		}},
		{library.Series{Title: "Roseanne", Year: 1988}, []c{
			{"Roseanne.S01E01.720p", true},
			{"Roseanne.2018.S01E01.720p", false},
			{"Roseanne.1988.S01E01.720p", true},
		}},
		{library.Series{Title: "Battlestar Galactica", Year: 2004}, []c{
			{"Battlestar.Galactica.2003.S01E01.720p", false},
			{"Battlestar.Galactica.2004.S01E01.720p", true},
			{"Battlestar.Galactica.S01E01.720p", true},
			{"Battlestar.Galactica.1978.S01E01.720p", false},
			{"Battlestar.Galactica.Blood.and.Chrome.S01E01.720p", false},
		}},
		{library.Series{Title: "Show", Year: 2010}, []c{
			{"Show.S01E01.720p", true},
			{"The.Show.S01E01.720p", true},
			{"Showtime.S01E01.720p", false},
			{"Show.Name.S01E01.720p", false},
			{"S01E01.720p", false},
			{"1080p.WEB-DL.S01E01", false},
		}},
	}
	checked := 0
	for _, g := range groups {
		for _, tc := range g.cases {
			checked++
			if got := matchesShow(tc.release, g.show); got != tc.want {
				t.Errorf("matchesShow(%q, %q %d %v) = %v, want %v", tc.release, g.show.Title, g.show.Year, g.show.OriginCountry, got, tc.want)
			}
		}
	}
	t.Logf("%d show matches checked", checked)
}
