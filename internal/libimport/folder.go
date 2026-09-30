package libimport

import (
	"fmt"
	"path/filepath"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// ScanMovieFolder lists the video files in one movie's own folder, when the
// title is already known (another app said which movie lives there). Unlike
// ScanMovies nothing is dropped for having an unreadable title: every video
// file counts, with its quality read from the file name and, failing that,
// from the folder name.
func ScanMovieFolder(dir string) ([]File, error) {
	files, err := walkVideos(dir)
	if err != nil {
		return nil, err
	}
	folderTier := quality.Classify(parser.Parse(filepath.Base(dir)))
	out := make([]File, 0, len(files))
	for _, f := range files {
		tier := quality.Classify(parser.Parse(fileBase(f.rel[len(f.rel)-1])))
		if tier == quality.TierUnknown && len(f.rel) >= 2 {
			tier = quality.Classify(parser.Parse(f.rel[len(f.rel)-2]))
		}
		if tier == quality.TierUnknown {
			tier = folderTier
		}
		out = append(out, File{Path: f.path, SizeBytes: f.size, Quality: string(tier)})
	}
	return out, nil
}

// ScanSeriesFolder lists the episode files in one show's own folder, when
// the show is already known. Only the season and episode numbers are read
// from each file name; a file without them ("Pilot.mkv") is reported as
// skipped. Two files for the same episode keep the bigger one, as in ScanTV.
func ScanSeriesFolder(dir string) ([]File, []string, error) {
	files, err := walkVideos(dir)
	if err != nil {
		return nil, nil, err
	}
	var (
		out     []File
		skipped []string
		seen    = map[string]int{}
	)
	for _, f := range files {
		fr := parser.Parse(fileBase(f.rel[len(f.rel)-1]))
		if len(fr.Episodes) == 0 { // season 0 is the specials
			skipped = append(skipped, f.path+" - couldn't read a season/episode number from the name")
			continue
		}
		// Quality comes from the file name, then the folders above it, then
		// the show's own folder.
		folders := append(nearestFolders(f.rel), filepath.Base(dir))
		tier := tierFromNames(fileBase(f.rel[len(f.rel)-1]), folders)
		file := File{Path: f.path, SizeBytes: f.size, Season: fr.Season, Episodes: fr.Episodes, Quality: string(tier)}
		key := fmt.Sprintf("%d#%d", fr.Season, fr.Episodes[0])
		if idx, dup := seen[key]; dup {
			if out[idx].SizeBytes >= file.SizeBytes {
				skipped = append(skipped, f.path+" - duplicate copy of the same episode")
				continue
			}
			skipped = append(skipped, out[idx].Path+" - duplicate copy of the same episode")
			out[idx] = file
			continue
		}
		seen[key] = len(out)
		out = append(out, file)
	}
	return out, skipped, nil
}
