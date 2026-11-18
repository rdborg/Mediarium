package api

import (
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/settings"
)

// Where a title goes when it is added without a folder choice: by a Plex
// watchlist, an import list, a request, the Radarr or Sonarr compatible API,
// a collection that is followed, or the Add dialog left on its default. One
// rule per kind (Settings > Library and files > Folders): the main folder, one of the
// extra folders, or whichever folder has the most room at the time (two
// drives filling up side by side). The rule is read when a title is added and
// the folder is stored on the title, so changing the rule later never moves a
// title's next files somewhere else.

// mostRoomFolder is the setting value for "the folder with the most room".
const mostRoomFolder = "most-room"

// folderUsage reads a folder's disk space. Tests replace it.
var folderUsage = fsinfo.DiskUsage

// newTitleRule is the saved rule for a kind: "" (the main folder),
// mostRoomFolder, or the path of one of the extra folders on offer. A path
// that is no longer one of the folders reads as "".
func (s *Server) newTitleRule(key string, offered []string) string {
	v, _ := s.Settings.Get(key)
	v = strings.TrimSpace(v)
	if v == "" || v == mostRoomFolder {
		return v
	}
	c := filepath.Clean(v)
	for _, f := range offered[min(1, len(offered)):] {
		if filepath.Clean(f) == c {
			return f
		}
	}
	return ""
}

// mostRoom is the folder out of offered with the most free space. A folder
// whose space can't be read is passed over, and a tie goes to the earlier one
// (the main folder comes first). "" when none can be read.
func mostRoom(offered []string, usage func(string) (fsinfo.Usage, error)) string {
	best, bestFree := "", uint64(0)
	for _, f := range offered {
		if f == "" {
			continue
		}
		u, err := usage(f)
		if err != nil || u.TotalBytes == 0 {
			continue
		}
		if best == "" || u.FreeBytes > bestFree {
			best, bestFree = f, u.FreeBytes
		}
	}
	return best
}

// newTitleFolder is what is stored on a title added now without a choice:
// "" for the main folder, else the extra folder's path.
func (s *Server) newTitleFolder(key string, offered []string) string {
	rule := s.newTitleRule(key, offered)
	if rule != mostRoomFolder {
		return rule
	}
	if len(offered) < 2 {
		return ""
	}
	pick := mostRoom(offered, folderUsage)
	if pick == "" || filepath.Clean(pick) == filepath.Clean(offered[0]) {
		return ""
	}
	return pick
}

// newMovieFolder and newShowFolder are newTitleFolder for each kind.
func (s *Server) newMovieFolder() string {
	return s.newTitleFolder(settings.KeyMoviesNewFolder, s.movieFolders())
}

func (s *Server) newShowFolder() string {
	return s.newTitleFolder(settings.KeyTVNewFolder, s.tvFolders())
}

// folderForNewMovie is the folder stored on a movie being added: the one
// asked for (checked against the folders on offer; the main one stores as
// ""), or when nothing was asked for, the one the rule for new movies gives.
func (s *Server) folderForNewMovie(want string) (string, string) {
	if strings.TrimSpace(want) == "" {
		return s.newMovieFolder(), ""
	}
	return s.checkFolderChoice(want, s.movieFolders(), "movie")
}

// folderForNewShow is folderForNewMovie for a show.
func (s *Server) folderForNewShow(want string) (string, string) {
	if strings.TrimSpace(want) == "" {
		return s.newShowFolder(), ""
	}
	return s.checkFolderChoice(want, s.tvFolders(), "show")
}

// checkFolderSetting reads a folder kept in a setting that adds titles later
// (an import list, a followed collection). Unlike checkFolderChoice it keeps
// the main folder's path, so "the main folder" stays that when the rule for
// new titles changes; "" means "wherever new titles go".
func (s *Server) checkFolderSetting(want string, offered []string, what string) (string, string) {
	if strings.TrimSpace(want) == "" || len(offered) == 0 {
		return "", ""
	}
	want = resolveFolderChoice(want, offered)
	c := filepath.Clean(want)
	for _, f := range offered {
		if filepath.Clean(f) == c {
			return f, ""
		}
	}
	return "", "Pick one of your " + what + " folders from the list. They are set in Settings > Library and files > Folders."
}

// newTitleFolderPayload says where a title added now goes, for the dialogs.
type newTitleFolderPayload struct {
	// Folder is the folder a title added without a choice goes to right now
	// (a path for administrators, a label for everyone else).
	Folder string `json:"folder"`
	// MostRoom is true when that is whichever folder has the most room.
	MostRoom bool `json:"mostRoom"`
}

// newTitlePayloadFor works out newTitleFolderPayload for one kind. offered is
// the full list of folders; shown is how the folders are shown to the person
// asking (paths or labels, in the same order).
func (s *Server) newTitlePayloadFor(key string, offered, shown []string) newTitleFolderPayload {
	if len(offered) == 0 {
		return newTitleFolderPayload{}
	}
	stored := s.newTitleFolder(key, offered)
	i := 0
	if stored != "" {
		for j, f := range offered {
			if filepath.Clean(f) == filepath.Clean(stored) {
				i = j
			}
		}
	}
	return newTitleFolderPayload{Folder: shown[i], MostRoom: s.newTitleRule(key, offered) == mostRoomFolder}
}

// checkNewTitleRule reads a rule sent to the settings: "", mostRoomFolder, or
// one of the extra folders in list (the folders next to the main one). It
// returns the value to store, or a message.
func checkNewTitleRule(want string, extra []string, what string) (string, string) {
	want = strings.TrimSpace(want)
	if want == "" || want == mostRoomFolder {
		return want, ""
	}
	c := filepath.Clean(want)
	for _, f := range extra {
		if filepath.Clean(f) == c {
			return c, ""
		}
	}
	return "", "New " + what + " can only go to the main folder, one of the folders in the list, or the folder with the most room."
}
