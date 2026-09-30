package libimport

import (
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// maxFolderLevels is how many folders above a file are read for a quality the
// file name leaves out ("Show/Season 01/Episode.mkv" has two).
const maxFolderLevels = 3

// tierFromNames reads a quality from a file name and, for whatever the name
// leaves out (the resolution, the source), from the folders above it, nearest
// first. What the file name does say is never overridden. Nothing found
// anywhere gives quality.TierUnknown.
func tierFromNames(fileName string, folders []string) quality.Tier {
	rel := parser.Parse(fileName)
	for _, folder := range folders {
		if rel.Resolution != "" && rel.Source != "" {
			break
		}
		fr := parser.Parse(folder)
		if rel.Resolution == "" {
			rel.Resolution = fr.Resolution
		}
		if rel.Source == "" {
			rel.Source = fr.Source
		}
	}
	return quality.Classify(rel)
}

// nearestFolders returns the folder names above the last element of rel,
// nearest first, without going more than maxFolderLevels up.
func nearestFolders(rel []string) []string {
	var out []string
	for i := len(rel) - 2; i >= 0 && len(out) < maxFolderLevels; i-- {
		out = append(out, rel[i])
	}
	return out
}

// TierFromPath reads the quality of a video file that is already on disk from
// its name and, failing that, the folders it sits in. The result is
// quality.TierUnknown when none of them say.
func TierFromPath(path string) quality.Tier {
	path = filepath.ToSlash(path)
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	return tierFromNames(fileBase(name), nearestFolders(parts))
}
