package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// describeFolder says in one line what a download folder holds: how many files,
// how much, and the biggest few with their names. It goes into the "no video
// file found" error so a support report shows what was really downloaded (a
// set of archives nothing unpacked, files with random names, a disc image)
// without anyone having to look in the folder.
func describeFolder(dir string) string {
	type entry struct {
		name string
		size int64
	}
	var files []entry
	var total int64
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			rel = info.Name()
		}
		files = append(files, entry{filepath.ToSlash(rel), info.Size()})
		total += info.Size()
		return nil
	})
	if len(files) == 0 {
		return "the folder is empty"
	}
	sort.Slice(files, func(i, j int) bool { return files[i].size > files[j].size })
	var biggest []string
	for i, f := range files {
		if i == 3 {
			break
		}
		name := f.name
		if len(name) > 60 {
			name = name[:57] + "..."
		}
		biggest = append(biggest, fmt.Sprintf("%s (%s)", name, humanBytes(f.size)))
	}
	return fmt.Sprintf("%d files, %s in all; the biggest are %s", len(files), humanBytes(total), strings.Join(biggest, ", "))
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}
