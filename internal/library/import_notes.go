package library

import (
	"fmt"
	"strings"
)

// importNote is the note of a show's import item: what was placed by
// TheTVDB's season numbers or by their titles, so the person knows why those
// episodes sit where they do, then what was left out and which files. Each is a sentence of its own on its own
// line, so the screen can show both in the reader's language.
func importNote(bySeason, byTitle int, leftOut string, leftFiles []string, orderHint string) string {
	var lines []string
	switch {
	case bySeason == 1:
		lines = append(lines, "Placed 1 episode by the TheTVDB season numbers.")
	case bySeason > 1:
		lines = append(lines, fmt.Sprintf("Placed %d episodes by the TheTVDB season numbers.", bySeason))
	}
	switch {
	case byTitle == 1:
		lines = append(lines, "Placed 1 episode by its title.")
	case byTitle > 1:
		lines = append(lines, fmt.Sprintf("Placed %d episodes by their titles.", byTitle))
	}
	if leftOut != "" {
		lines = append(lines, leftOut)
		if names := leftFilesNote(leftFiles); names != "" {
			lines = append(lines, names)
		}
		if orderHint != "" {
			lines = append(lines, orderHint)
		}
	}
	return strings.Join(lines, "\n")
}

// leftFilesNote names the files that were left out, the first few of them.
func leftFilesNote(paths []string) string {
	const most = 5
	var names []string
	seen := map[string]bool{}
	for _, p := range paths {
		name := p
		if i := strings.LastIndexAny(name, `/\`); i >= 0 {
			name = name[i+1:]
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	switch {
	case len(names) == 0:
		return ""
	case len(names) > most:
		return fmt.Sprintf("Files left out: %s and %d more.", strings.Join(names[:most], ", "), len(names)-most)
	}
	return fmt.Sprintf("Files left out: %s.", strings.Join(names, ", "))
}

// tvdbOrderNote is the sentence that tells a person whose show left files out
// that the show can follow TheTVDB's episode order, which counts a double
// episode as two. offer is true when a TheTVDB key is saved and the show still
// follows the movie database's order. Files named by their number from the
// start of the show (anime) are a different matter, so they get no hint.
func tvdbOrderNote(unknown int, byAbsolute, offer bool) string {
	if unknown <= 0 || byAbsolute || !offer {
		return ""
	}
	return "If these files are numbered the way TheTVDB counts the show's episodes, set this show's Episode order to TheTVDB order and import the folder again."
}

// leftOutNote is the import report's note for a show whose files named
// episodes the movie database doesn't list: how many, and what to do about
// it when the names say. byAbsolute is true when some of those files were
// named by their number counted from the start of the show, byNumber when
// some had a season and episode. Each case is one whole sentence, so the
// screen can show it in the reader's language.
func leftOutNote(unknown int, byAbsolute, byNumber, anime bool) string {
	if unknown <= 0 {
		return ""
	}
	setAnime := byAbsolute && !anime
	rename := !setAnime && (byAbsolute || (byNumber && anime))
	switch {
	case unknown == 1 && setAnime:
		return "Left out 1 episode that the movie database doesn't list for this show. Its name carries the episode number counted from the start of the show, so set this show's Episode numbering to Anime and import the folder again."
	case setAnime:
		return fmt.Sprintf("Left out %d episodes that the movie database doesn't list for this show. Their names carry the episode number counted from the start of the show, so set this show's Episode numbering to Anime and import the folder again.", unknown)
	case unknown == 1 && rename:
		return "Left out 1 episode that the movie database doesn't list for this show. Its number doesn't match the movie database's list, so nothing was guessed. Rename the file to the season and episode shown on the show's page and import the folder again."
	case rename:
		return fmt.Sprintf("Left out %d episodes that the movie database doesn't list for this show. Their numbers don't match the movie database's list, so nothing was guessed. Rename the files to the season and episode shown on the show's page and import the folder again.", unknown)
	case unknown == 1:
		return "Left out 1 episode that the movie database doesn't list for this show."
	}
	return fmt.Sprintf("Left out %d episodes that the movie database doesn't list for this show.", unknown)
}
