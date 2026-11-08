package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/fileperms"
	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
)

// moviesRoot is where movies are organized: the Settings value, falling back
// to the MOVIES_DIR container default so a fresh Docker install works.
func (s *Server) moviesRoot() string {
	if v, _ := s.Settings.Get(settings.KeyMoviesPath); v != "" {
		return v
	}
	return s.cfg.MoviesDir
}

// movieCopiesRoot and tvCopiesRoot are where second copies go (a 4K copy
// of a film kept next to the 1080p one): a folder of their own, so the two
// never collide and a media server can show them as a separate library.
// "" until set, and then second copies can't be added.
func (s *Server) movieCopiesRoot() string {
	v, _ := s.Settings.Get(settings.KeyMovieCopiesPath)
	return v
}

func (s *Server) tvCopiesRoot() string {
	v, _ := s.Settings.Get(settings.KeyTVCopiesPath)
	return v
}

// extraFolders are the folders added next to the main one for a kind of media
// (Settings > Library > Folders), tidied and without repeats.
func (s *Server) extraFolders(key, main string) []string {
	v, _ := s.Settings.Get(key)
	if strings.TrimSpace(v) == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(v), &list); err != nil {
		return nil
	}
	seen := map[string]bool{filepath.Clean(main): true}
	var out []string
	for _, p := range list {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		c := filepath.Clean(p)
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// movieFolders are the folders a movie can be kept in: the main movies folder
// first, then the ones added next to it.
func (s *Server) movieFolders() []string {
	main := s.moviesRoot()
	out := []string{}
	if main != "" {
		out = append(out, main)
	}
	out = append(out, s.extraFolders(settings.KeyMoviesExtraPaths, main)...)
	// The anime films folder (anime.go) is one of them while it is set, like
	// the anime folder for shows.
	if a, _ := s.Settings.Get(settings.KeyAnimeMoviesPath); strings.TrimSpace(a) != "" {
		a = filepath.Clean(strings.TrimSpace(a))
		if !slices.ContainsFunc(out, func(f string) bool { return filepath.Clean(f) == a }) {
			out = append(out, a)
		}
	}
	return out
}

// tvFolders is movieFolders for shows. The anime folder (anime.go) is one of
// them while it is set, also with the anime module off, so the shows already
// in it keep their folder.
func (s *Server) tvFolders() []string {
	main := s.tvRoot()
	out := []string{}
	if main != "" {
		out = append(out, main)
	}
	out = append(out, s.extraFolders(settings.KeyTVExtraPaths, main)...)
	if a, _ := s.Settings.Get(settings.KeyAnimePath); strings.TrimSpace(a) != "" {
		a = filepath.Clean(strings.TrimSpace(a))
		if !slices.ContainsFunc(out, func(f string) bool { return filepath.Clean(f) == a }) {
			out = append(out, a)
		}
	}
	return out
}

// chosenFolder returns want when it is one of the folders on offer, else def.
func chosenFolder(want string, offered []string, def string) string {
	if want == "" {
		return def
	}
	c := filepath.Clean(want)
	for _, f := range offered {
		if filepath.Clean(f) == c {
			return f
		}
	}
	return def
}

// rootContaining is the folder out of roots that holds path ("" when none does).
func rootContaining(roots []string, path string) string {
	for _, r := range roots {
		if r == "" {
			continue
		}
		if rel, err := filepath.Rel(r, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return r
		}
	}
	return ""
}

// movieRootFor is the library folder a movie's file belongs in: the copies
// folder for a second copy, the folder chosen for the movie, else the main
// movies folder.
func (s *Server) movieRootFor(m library.Movie) string {
	if m.CopyOf != 0 {
		return s.movieCopiesRoot()
	}
	return chosenFolder(m.RootPath, s.movieFolders(), s.moviesRoot())
}

// seriesRootFor is movieRootFor for a show.
func (s *Server) seriesRootFor(sr library.Series) string {
	if sr.CopyOf != 0 {
		return s.tvCopiesRoot()
	}
	return chosenFolder(sr.RootPath, s.tvFolders(), s.tvRoot())
}

// fileRoot is the library folder an existing file is in. A title's folder can
// be changed after its files arrived (only new files go to the new one), so
// work on a file it already has uses the folder that holds it: preferred (the
// title's folder) when it does, else whichever video folder does, else
// preferred.
func (s *Server) fileRoot(preferred, file string) string {
	if preferred != "" && rootContaining([]string{preferred}, file) != "" {
		return preferred
	}
	if r := rootContaining(s.videoRoots(), file); r != "" {
		return r
	}
	return preferred
}

// videoRoots are the movie and TV library folders, extra and copies folders included.
func (s *Server) videoRoots() []string {
	out := append(s.movieFolders(), s.tvFolders()...)
	for _, r := range []string{s.movieCopiesRoot(), s.tvCopiesRoot()} {
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

// checkCopiesFolder refuses a copies folder that would mix with a main
// library folder: the same folder, or one inside the other, would put a
// copy where the main file is.
func checkCopiesFolder(copies string, mains ...string) error {
	if copies == "" {
		return nil
	}
	if !filepath.IsAbs(copies) {
		return errors.New("the folder for second copies has to be a full path, starting with /")
	}
	c := filepath.Clean(copies)
	for _, m := range mains {
		if m == "" {
			continue
		}
		m = filepath.Clean(m)
		if c == m || strings.HasPrefix(c+string(filepath.Separator), m+string(filepath.Separator)) || strings.HasPrefix(m+string(filepath.Separator), c+string(filepath.Separator)) {
			return fmt.Errorf("the folder for second copies has to be separate from %s, not the same folder or one inside it", m)
		}
	}
	return nil
}

// downloadsRoot is the parent of the incomplete/ working folder.
func (s *Server) downloadsRoot() string {
	if v, _ := s.Settings.Get(settings.KeyDownloadsPath); v != "" {
		return v
	}
	return s.cfg.DownloadsDir
}

// ebooksRoot and audiobooksRoot are the ebooks and audiobooks libraries: the
// Settings value, falling back to EBOOKS_DIR / AUDIOBOOKS_DIR.
func (s *Server) ebooksRoot() string {
	if v, _ := s.Settings.Get(settings.KeyEbooksPath); v != "" {
		return v
	}
	return s.cfg.EbooksDir
}

func (s *Server) audiobooksRoot() string {
	if v, _ := s.Settings.Get(settings.KeyAudiobooksPath); v != "" {
		return v
	}
	return s.cfg.AudiobooksDir
}

type folderPayload struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Writable   bool   `json:"writable"`
	FreeBytes  uint64 `json:"freeBytes"`
	TotalBytes uint64 `json:"totalBytes"`
	Mounted    bool   `json:"mounted"`
	MountKnown bool   `json:"mountKnown"`
	// InDocker is true when Mediarium runs in a container. Only then does
	// "map this folder in your compose file" make sense.
	InDocker bool     `json:"inDocker"`
	Warnings []string `json:"warnings"`
	// CanCreate is true for a folder that doesn't exist yet but can be made
	// safely: its parent exists, can be written, and is mapped to the device
	// (so it isn't lost when the container is updated).
	CanCreate bool `json:"canCreate,omitempty"`
	// Marker is set for a library folder that has no .mediarium-root file.
	Marker *rootMarkerPayload `json:"marker,omitempty"`
}

func toFolderPayload(f fsinfo.Folder) folderPayload {
	warnings := f.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return folderPayload{
		Path: f.Path, Exists: f.Exists && f.IsDir, Writable: f.Writable, FreeBytes: f.FreeBytes, TotalBytes: f.TotalBytes,
		Mounted: f.Mounted, MountKnown: f.MountKnown, InDocker: fsinfo.InDocker(), Warnings: warnings,
	}
}

// handleFolderCheck inspects one folder for the setup screens: does it
// exist, can Mediarium write to it, how much room is left and — in Docker —
// was it actually mapped in from the host. It only reads (plus one probe file
// that is created and removed); it never changes existing files.
func (s *Server) handleFolderCheck(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if rejectBad(w,
		checkRequired(path, "Enter a folder path to check, for example /media/movies."),
		checkAbsPath(path, "/media/movies"),
		checkMaxLen(path, "The folder path", maxPathLen),
	) {
		return
	}
	writeJSON(w, http.StatusOK, s.folderPayloadFor(path))
}

// folderPayloadFor is toFolderPayload plus whether a missing folder can be created.
func (s *Server) folderPayloadFor(path string) folderPayload {
	p := toFolderPayload(fsinfo.Inspect(path))
	if !p.Exists {
		if parent, ok := creatableParent(path); ok {
			p.CanCreate = true
			p.Warnings = []string{fmt.Sprintf("This folder doesn't exist yet. Mediarium can create it inside %s, which is mapped to your device.", parent)}
		}
	}
	if p.Exists {
		p.Marker = s.rootMarkerInfo(path)
	}
	if p.Exists && p.Writable && insideWorthChecking(path) {
		if bad, _ := unwritableInside(path); len(bad) > 0 {
			p.Warnings = append(p.Warnings, insideWarning(bad))
		}
	}
	return p
}

// insideWarning says which folders inside a library folder can't be written to.
func insideWarning(bad []string) string {
	if len(bad) == 1 {
		return fmt.Sprintf("A folder inside can't be written to: %s. Mediarium can't add or upgrade files there. Give the user Mediarium runs as (its PUID and PGID) write access to it, for example by changing its owner.", bad[0])
	}
	return fmt.Sprintf("There are %d folders inside that can't be written to, such as %s. Mediarium can't add or upgrade files there. Give the user Mediarium runs as (its PUID and PGID) write access to them, for example by changing their owner.", len(bad), bad[0])
}

// insideWorthChecking is false for the root of the filesystem and the
// system's own folders: the look inside is for library folders, and an
// administrator checking "/" or "/proc" shouldn't set off a walk through them.
func insideWorthChecking(path string) bool {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean == string(filepath.Separator) {
		return false
	}
	for _, sys := range []string{"/proc", "/sys", "/dev", "/etc", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/boot", "/run", "/root"} {
		if clean == sys || strings.HasPrefix(clean, sys+"/") {
			return false
		}
	}
	return true
}

// insideCacheMax caps how many folders the look inside remembers, so checking
// many different paths can't grow it without end.
const insideCacheMax = 64

// unwritableInside is fsinfo.UnwritableInside, remembered for a few minutes
// so the health list doesn't walk a big library on every visit.
//
// The walk looks at up to 5000 folders, which on a network drive takes seconds.
// It never blocks anyone but the first caller: once an answer has been kept, an
// older one is still handed out at once while a single fresh walk runs in the
// background, and the walk does not hold the lock, so other pages are not held
// up by it.
func unwritableInside(root string) ([]string, int) {
	insideCache.Lock()
	c, ok := insideCache.byRoot[root]
	if ok && time.Since(c.at) < 10*time.Minute {
		insideCache.Unlock()
		return c.bad, c.checked
	}
	if ok {
		if !c.refreshing {
			c.refreshing = true
			insideCache.byRoot[root] = c
			go func() {
				bad, checked := fsinfo.UnwritableInside(root, 5000)
				rememberInside(root, bad, checked)
			}()
		}
		insideCache.Unlock()
		return c.bad, c.checked
	}
	insideCache.Unlock()
	bad, checked := fsinfo.UnwritableInside(root, 5000)
	rememberInside(root, bad, checked)
	return bad, checked
}

func rememberInside(root string, bad []string, checked int) {
	insideCache.Lock()
	defer insideCache.Unlock()
	if insideCache.byRoot == nil || len(insideCache.byRoot) >= insideCacheMax {
		insideCache.byRoot = map[string]insideResult{}
	}
	insideCache.byRoot[root] = insideResult{bad: bad, checked: checked, at: time.Now()}
}

type insideResult struct {
	bad        []string
	checked    int
	at         time.Time
	refreshing bool // a fresh walk is on its way
}

var insideCache struct {
	sync.Mutex
	byRoot map[string]insideResult
}

// creatableParent reports whether path can be created safely: one level
// below a folder that exists, is writable and (in Docker) is mapped from the
// host. A folder made anywhere else would live inside the container and
// disappear with the next update.
func creatableParent(path string) (string, bool) {
	clean := filepath.Clean(path)
	parent := filepath.Dir(clean)
	if !filepath.IsAbs(clean) || parent == clean || parent == "/" || parent == "." {
		return parent, false
	}
	f := fsinfo.Inspect(parent)
	if !f.Exists || !f.IsDir || !f.Writable {
		return parent, false
	}
	if f.MountKnown && !f.Mounted && !insideMount(parent) {
		return parent, false
	}
	return parent, true
}

// insideMount reports whether dir is below a folder mapped from the device
// (a mapped /data makes /data/Media/Books safe too).
func insideMount(dir string) bool {
	for d := filepath.Dir(dir); d != "/" && d != "." && d != filepath.Dir(d); d = filepath.Dir(d) {
		if f := fsinfo.Inspect(d); f.MountKnown && f.Mounted {
			return true
		}
	}
	return false
}

type createFolderRequest struct {
	Path string `json:"path"`
}

// handleCreateFolder makes a missing library folder (administrators), only
// where creatableParent allows. It answers with the folder's new check.
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var req createFolderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Send the folder to create.")
		return
	}
	path := strings.TrimSpace(req.Path)
	if rejectBad(w,
		checkRequired(path, "Enter the folder to create, for example /data/Ebooks."),
		checkAbsPath(path, "/data/Ebooks"),
		checkMaxLen(path, "The folder path", maxPathLen),
	) {
		return
	}
	if _, err := os.Stat(path); err == nil {
		writeJSON(w, http.StatusOK, s.folderPayloadFor(path))
		return
	}
	parent, ok := creatableParent(path)
	if !ok {
		writeError(w, http.StatusConflict, fmt.Sprintf("Mediarium can only create a folder inside one that is mapped to your device and writable, and %s isn't. Map a folder for it in your compose file instead.", parent))
		return
	}
	if err := os.Mkdir(filepath.Clean(path), 0o775); err != nil && !errors.Is(err, os.ErrExist) {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Couldn't create %s: %v", path, err))
		return
	}
	fileperms.ApplyDir(filepath.Clean(path))
	writeJSON(w, http.StatusOK, s.folderPayloadFor(path))
}

// destWritable checks, before anything is downloaded, that Mediarium may
// write where the file will go: the destination folder, or the closest folder
// above it that exists. A folder owned by another user (often one an older
// app created) used to be found only after the whole download, which was
// then thrown away. Other errors don't stop a download.
func destWritable(destDir string) error {
	dir := destDir
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
	f, err := os.CreateTemp(dir, ".mediarium-write-check-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("Mediarium may not write to %s, so nothing was downloaded. The folder belongs to another user: give the user Mediarium runs as (PUID and PGID) write access to it: %w", dir, os.ErrPermission)
		}
		return nil
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}
