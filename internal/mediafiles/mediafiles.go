// Package mediafiles lists and opens the files in a library title's folder
// on disk, for the "Files" view and in-browser playback. Every path is kept
// inside the library folders: the title's folder is resolved (symlinks
// included) and must lie inside a library root, and files are opened
// through an os.Root on that folder, so neither "../" nor a symlink can
// reach anything outside it.
package mediafiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Kind groups a file by what it is for.
type Kind string

const (
	KindVideo    Kind = "video"
	KindSubtitle Kind = "subtitle"
	KindImage    Kind = "image"
	KindNFO      Kind = "nfo"
	KindOther    Kind = "other"
)

var kinds = map[string]Kind{
	".mkv": KindVideo, ".mp4": KindVideo, ".m4v": KindVideo, ".webm": KindVideo, ".avi": KindVideo,
	".mov": KindVideo, ".wmv": KindVideo, ".ts": KindVideo, ".m2ts": KindVideo, ".mpg": KindVideo, ".mpeg": KindVideo,
	".srt": KindSubtitle, ".ass": KindSubtitle, ".ssa": KindSubtitle, ".sub": KindSubtitle, ".idx": KindSubtitle,
	".vtt": KindSubtitle, ".sup": KindSubtitle, ".smi": KindSubtitle,
	".jpg": KindImage, ".jpeg": KindImage, ".png": KindImage, ".gif": KindImage, ".webp": KindImage, ".bmp": KindImage, ".tbn": KindImage,
	".nfo": KindNFO,
}

// KindOf classifies a file name by its extension.
func KindOf(name string) Kind {
	if k, ok := kinds[strings.ToLower(filepath.Ext(name))]; ok {
		return k
	}
	return KindOther
}

var videoTypes = map[string]string{
	".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mkv": "video/x-matroska",
	".avi": "video/x-msvideo", ".mov": "video/quicktime", ".wmv": "video/x-ms-wmv",
	".ts": "video/mp2t", ".m2ts": "video/mp2t", ".mpg": "video/mpeg", ".mpeg": "video/mpeg",
}

// VideoContentType is the Content-Type to serve a video file with, or ""
// when name is not a video file.
func VideoContentType(name string) string {
	return videoTypes[strings.ToLower(filepath.Ext(name))]
}

// Pictures and plain-text files that can be previewed in the browser. SVG is
// deliberately not an image here (it can carry scripts), and nothing that a
// browser would render as a page (html, xml, js) is ever served.
var (
	imageTypes = map[string]string{
		".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp", ".gif": "image/gif",
	}
	textExts = map[string]bool{
		".nfo": true, ".srt": true, ".ass": true, ".ssa": true, ".vtt": true, ".txt": true,
	}
)

// MaxTextPreview is the largest text file served for preview (2 MB). Text
// files in a title's folder are small; anything bigger is not what a preview
// is for.
const MaxTextPreview = 2 << 20

// TextContentType is what every previewable text file is served as, whatever
// its format, so the browser only ever shows it as text.
const TextContentType = "text/plain; charset=utf-8"

// PreviewContentType is the Content-Type to serve a file with in the browser:
// a video to play, an image to show or a text file to read. isText is true
// for text files (which are size-capped by the caller). It returns "" for
// every other file, which must not be served.
func PreviewContentType(name string) (contentType string, isText bool) {
	ext := strings.ToLower(filepath.Ext(name))
	if ct, ok := videoTypes[ext]; ok {
		return ct, false
	}
	if ct, ok := imageTypes[ext]; ok {
		return ct, false
	}
	if textExts[ext] {
		return TextContentType, true
	}
	return "", false
}

// File is one file in a title's folder.
type File struct {
	Path     string // relative to the folder, "/"-separated
	Size     int64
	Modified time.Time
	Kind     Kind
}

var (
	// ErrOutside means a path is not inside the library folders.
	ErrOutside = errors.New("not inside a library folder")
	// ErrInvalidPath means a requested relative path is malformed or tries
	// to leave the folder.
	ErrInvalidPath = errors.New("invalid file path")
)

// maxFiles caps a listing, so one odd folder cannot produce a huge answer.
const maxFiles = 2000

// Folder is a title's folder that has been checked to lie inside a library
// root. When the title's files sit directly in the root (no folder of their
// own), Prefix limits the folder to the root's top-level files whose names
// start with the title's file name, so the rest of the library stays hidden.
type Folder struct {
	Dir    string // resolved absolute path
	Name   string // the folder's path relative to its library root, "/"-separated
	Prefix string // "" = the whole folder
}

// resolveRoots returns the library roots with symlinks resolved, skipping
// unset or missing ones.
func resolveRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			continue
		}
		out = append(out, real)
	}
	return out
}

// within returns p's path relative to root when p is root or inside it.
func within(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// ForFile finds the folder of a library file. With topLevel false that is the
// folder holding the file (a movie's folder); with topLevel true it is the
// file's first folder below the library root (a show's folder, above its
// season folders). The folder is resolved with symlinks followed and must be
// inside one of roots; a missing folder is reported as fs.ErrNotExist.
func ForFile(file string, roots []string, topLevel bool) (Folder, error) {
	if file == "" {
		return Folder{}, fmt.Errorf("no file: %w", fs.ErrNotExist)
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return Folder{}, fmt.Errorf("resolve %s: %w", file, err)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return Folder{}, fmt.Errorf("resolve folder: %w", err)
	}
	for _, root := range resolveRoots(roots) {
		rel, ok := within(root, dir)
		if !ok {
			continue
		}
		if rel == "." {
			if topLevel {
				// A show's episode directly in the root: there is no show folder.
				return Folder{}, fmt.Errorf("file directly in the library folder: %w", ErrOutside)
			}
			stem := strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
			return Folder{Dir: root, Name: "", Prefix: stem}, nil
		}
		if topLevel {
			first := strings.SplitN(rel, string(filepath.Separator), 2)[0]
			return Folder{Dir: filepath.Join(root, first), Name: filepath.ToSlash(first)}, nil
		}
		return Folder{Dir: dir, Name: filepath.ToSlash(rel)}, nil
	}
	return Folder{}, ErrOutside
}

// Rel returns a library file's path relative to the folder ("/"-separated),
// resolving symlinks in its folder, or false when the file is not in it.
func (f Folder) Rel(file string) (string, bool) {
	if file == "" {
		return "", false
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", false
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", false
	}
	relDir, ok := within(f.Dir, dir)
	if !ok {
		return "", false
	}
	rel := filepath.ToSlash(filepath.Join(relDir, filepath.Base(abs)))
	if f.Prefix != "" && (strings.Contains(rel, "/") || !f.visible(rel)) {
		return "", false
	}
	return rel, true
}

// visible reports whether a top-level name is part of a prefix-limited folder.
func (f Folder) visible(name string) bool {
	if f.Prefix == "" {
		return true
	}
	return name == f.Prefix || strings.HasPrefix(name, f.Prefix+".")
}

// List returns the folder's files (not directories, not hidden files),
// sorted by path. Symlinks are listed only when they point inside the folder.
func (f Folder) List() ([]File, error) {
	root, err := os.OpenRoot(f.Dir)
	if err != nil {
		return nil, fmt.Errorf("open folder: %w", err)
	}
	defer root.Close()

	out := []File{}
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			return nil // an unreadable subfolder is skipped, not fatal
		}
		if p == "." {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if f.Prefix != "" {
				return fs.SkipDir // prefix mode shows top-level files only
			}
			return nil
		}
		if !f.visible(p) {
			return nil
		}
		// Stat through the root: it follows a symlink only if the target is
		// inside the folder, and fails otherwise.
		info, err := root.Stat(filepath.FromSlash(p))
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		out = append(out, File{Path: p, Size: info.Size(), Modified: info.ModTime().UTC(), Kind: KindOf(name)})
		if len(out) >= maxFiles {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list folder: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Open opens a file by its "/"-separated path relative to the folder. It
// rejects absolute paths, "..", hidden files, and any symlink that leads out
// of the folder. The caller closes the file.
func (f Folder) Open(rel string) (*os.File, fs.FileInfo, error) {
	if rel == "" || strings.Contains(rel, `\`) || !fs.ValidPath(rel) || rel == "." {
		return nil, nil, ErrInvalidPath
	}
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return nil, nil, ErrInvalidPath
		}
	}
	if f.Prefix != "" && (strings.Contains(rel, "/") || !f.visible(path.Base(rel))) {
		return nil, nil, fmt.Errorf("%s: %w", rel, fs.ErrNotExist)
	}
	root, err := os.OpenRoot(f.Dir)
	if err != nil {
		return nil, nil, fmt.Errorf("open folder: %w", err)
	}
	defer root.Close()
	file, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, fmt.Errorf("%s: %w", rel, fs.ErrNotExist)
		}
		// Escaping symlinks and similar come back as other errors.
		return nil, nil, fmt.Errorf("%s: %w", rel, ErrInvalidPath)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("stat %s: %w", rel, err)
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, fmt.Errorf("%s is not a file: %w", rel, fs.ErrNotExist)
	}
	return file, info, nil
}
