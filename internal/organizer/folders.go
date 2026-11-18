package organizer

import (
	"fmt"
	"strings"
)

// Title folders. A new movie gets a folder of its own and so does a new show.
// By default they are named "{Movie Title} ({Year})" and
// "{Series Title} ({Year})"; with the Custom naming style a person can write
// their own, with the same tokens as the file names, limited to the ones that
// say something about the title itself (no quality or release group: those
// change with every file).

// DefaultMovieFolder is how a movie's folder is named when no folder format
// is set. It is the same text as the Plex file name preset, as it always was.
const DefaultMovieFolder = "{Movie Title} ({Year})"

// DefaultSeriesFolder is how a show's folder is named when no folder format is set.
const DefaultSeriesFolder = TVSeriesFolder

// FolderScheme is a ready-made pair of folder formats to start from in Settings.
type FolderScheme struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Movie  string `json:"movie"`
	Series string `json:"series"`
}

// FolderSchemes are starting points for the folder formats; each can be
// changed. TRaSH's folder formats put the TVDB or IMDb id in the name;
// Mediarium knows the TMDb id of every title, so the id variants use that.
var FolderSchemes = []FolderScheme{
	{"default", "Mediarium default", DefaultMovieFolder, DefaultSeriesFolder},
	{"trash", "TRaSH recommended", "{Movie CleanTitle} ({Release Year})", "{Series TitleYear}"},
	{"trash-plex-tmdb", "TRaSH recommended, Plex with TMDb id", "{Movie CleanTitle} ({Release Year}) {tmdb-{TmdbId}}", "{Series TitleYear} {tmdb-{TmdbId}}"},
	{"trash-emby-tmdb", "TRaSH recommended, Emby with TMDb id", "{Movie CleanTitle} ({Release Year}) [tmdb-{TmdbId}]", "{Series TitleYear} [tmdb-{TmdbId}]"},
	{"trash-jellyfin-tmdb", "TRaSH recommended, Jellyfin with TMDb id", "{Movie CleanTitle} ({Release Year}) [tmdbid-{TmdbId}]", "{Series TitleYear} [tmdbid-{TmdbId}]"},
}

// CheckFolderFormat checks a folder format before it is saved and says what's
// wrong in a sentence, or "" when it's fine. kind is TokenMovie or TokenTV.
func CheckFolderFormat(format string, kind TokenKind) string {
	v := strings.TrimSpace(format)
	if v == "" {
		return ""
	}
	if strings.ContainsAny(v, `/\`) {
		return `A folder name can't contain / or \. Season folders are made for you, so only write the name of the title's folder.`
	}
	if strings.Count(v, "{") != strings.Count(v, "}") {
		return "A { or } is missing. Each token is wrapped in curly braces, like {Movie Title}."
	}
	var names []string
	check := func(inner string) string {
		p := parseToken(inner)
		if !p.known {
			return fmt.Sprintf("{%s} isn't a token Mediarium knows. Pick one from the token list next to the box.", strings.TrimSpace(inner))
		}
		if p.pad != "" && strings.Trim(p.pad, "0") != "" {
			return fmt.Sprintf(`After the colon, use zeros to set padding, like {Season:00}. "%s" won't work.`, p.pad)
		}
		if p.def.kind != TokenBoth && p.def.kind != kind {
			if kind == TokenMovie {
				return fmt.Sprintf("{%s} is for shows, not movies.", p.def.name)
			}
			return fmt.Sprintf("{%s} is for movies, not shows.", p.def.name)
		}
		if p.def.name == "Edition Tags" || (p.def.group != "Movie" && p.def.group != "Show" && p.def.group != "Ids") {
			return fmt.Sprintf("{%s} changes from one file to the next, so it can't be in a folder name. Use the title, the year or an id.", p.def.name)
		}
		names = append(names, strings.ToLower(p.def.name))
		return ""
	}
	for _, m := range nestedRe.FindAllStringSubmatch(v, -1) {
		if msg := check(m[2]); msg != "" {
			return msg
		}
	}
	rest := nestedRe.ReplaceAllString(v, "")
	for _, m := range tokenRe.FindAllStringSubmatch(rest, -1) {
		if msg := check(m[1]); msg != "" {
			return msg
		}
	}
	titles := []string{"series title", "series cleantitle", "series titleyear", "series cleantitleyear", "series titlethe", "series titlewithoutyear", "series cleantitlewithoutyear"}
	if kind == TokenMovie {
		titles = []string{"movie title", "movie cleantitle", "movie titlethe", "movie cleantitlethe"}
	}
	for _, n := range names {
		for _, w := range titles {
			if n == w {
				return ""
			}
		}
	}
	if kind == TokenMovie {
		return "Include the movie's title, for example {Movie Title} or {Movie CleanTitle}, so every folder says which movie it is."
	}
	return "Include the show's title, for example {Series Title} or {Series TitleYear}, so every folder says which show it is."
}
