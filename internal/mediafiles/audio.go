package mediafiles

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// KindAudio is the kind of a music file. It is used by the music module's
// listings only; KindOf still calls an audio file "other", so the movie and
// show listings do not change.
const KindAudio Kind = "audio"

var audioTypes = map[string]string{
	".flac": "audio/flac", ".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".alac": "audio/mp4",
	".aac": "audio/aac", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/opus",
}

// AudioContentType is the Content-Type to serve an audio file with, or ""
// when name is not an audio file.
func AudioContentType(name string) string {
	return audioTypes[strings.ToLower(filepath.Ext(name))]
}

// AlbumKindOf classifies a file in an album folder: audio, or what KindOf
// says.
func AlbumKindOf(name string) Kind {
	if AudioContentType(name) != "" {
		return KindAudio
	}
	return KindOf(name)
}

// ForDir is ForFile for a folder that is known already (an album's folder):
// it is resolved with symlinks followed and must lie strictly inside one of
// roots. A missing folder is reported as fs.ErrNotExist, a folder outside
// (or the root itself) as ErrOutside.
func ForDir(dir string, roots []string) (Folder, error) {
	if dir == "" {
		return Folder{}, fmt.Errorf("no folder: %w", fs.ErrNotExist)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Folder{}, fmt.Errorf("resolve %s: %w", dir, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Folder{}, fmt.Errorf("resolve folder: %w", err)
	}
	for _, root := range resolveRoots(roots) {
		rel, ok := within(root, real)
		if !ok {
			continue
		}
		if rel == "." {
			return Folder{}, fmt.Errorf("the library folder itself is not a title's folder: %w", ErrOutside)
		}
		return Folder{Dir: real, Name: filepath.ToSlash(rel)}, nil
	}
	return Folder{}, ErrOutside
}
