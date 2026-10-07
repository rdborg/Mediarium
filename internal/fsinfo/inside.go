package fsinfo

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// UnwritableInside looks at the folders inside a library folder, a title's
// folder and the season folders in it, and lists the ones Mediarium may not
// write to. A library folder can be writable while folders an older app
// created inside it belong to another user; files can't be added or upgraded
// there. It looks at most at limit folders and only checks permissions: no
// file is created. checked is how many folders it looked at.
func UnwritableInside(root string, limit int) (bad []string, checked int) {
	titles, err := os.ReadDir(root)
	if err != nil {
		return nil, 0
	}
	for _, t := range titles {
		if checked >= limit {
			break
		}
		if !t.IsDir() || strings.HasPrefix(t.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, t.Name())
		checked++
		if !canWrite(dir) {
			bad = append(bad, dir)
			continue
		}
		inner, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, s := range inner {
			if checked >= limit {
				break
			}
			if !s.IsDir() || strings.HasPrefix(s.Name(), ".") {
				continue
			}
			sub := filepath.Join(dir, s.Name())
			checked++
			if !canWrite(sub) {
				bad = append(bad, sub)
			}
		}
	}
	sort.Strings(bad)
	return bad, checked
}
