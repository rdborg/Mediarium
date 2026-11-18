package download

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// A release added by hand often comes without a name the person typed, and
// the NZB's own file name can be anything ("m2.nzb"). The posts inside say what
// the release is: their subjects quote the file names, and a poster may also put
// a name in the NZB's head. These helpers read them, so the quality and the
// season and episode can be worked out from a real release name.

// metaTitle is the name a poster wrote in the NZB's head (<meta type="name">
// or "title"), cleaned of control characters; "" when there is none.
func metaTitle(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 300 {
		return ""
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return raw
}

// plainExtensions are the endings of a file that is the release itself (the
// video, the music, the book). A tag such as ".x264" or ".1080p" is not one.
var plainExtensions = map[string]bool{
	"mkv": true, "mp4": true, "avi": true, "m4v": true, "ts": true, "m2ts": true, "wmv": true, "mov": true, "mpg": true, "mpeg": true, "iso": true,
	"flac": true, "mp3": true, "m4a": true, "m4b": true, "ogg": true, "opus": true, "wav": true,
	"epub": true, "mobi": true, "azw3": true, "pdf": true, "cbz": true, "cbr": true,
}

// packedExtensions are the endings of the archives and repair files a release
// is posted in. Their counters ("part01", "r00", "001", "vol000+01") are not
// part of the name.
var packedExtensions = map[string]bool{"rar": true, "zip": true, "7z": true, "par2": true}

// notTheRelease are files that sit next to the release and say nothing about
// it: notes, checksums, pictures and samples.
var notTheRelease = map[string]bool{
	"nfo": true, "sfv": true, "srr": true, "srs": true, "nzb": true, "txt": true, "jpg": true, "jpeg": true, "png": true, "url": true, "md5": true, "sha1": true, "diz": true, "srt": true, "sub": true, "idx": true,
}

var (
	partSuffixRe = regexp.MustCompile(`(?i)\.(part\d+|r\d{2,3}|\d{3})$`)
	parVolumeRe  = regexp.MustCompile(`(?i)\.vol\d+[+-]\d+$`)
)

// releaseStem is the name of the release a file belongs to: the file name
// without its ending and without the part counter of an archive or the volume
// of a repair file ("Heat.1995.1080p.BluRay.x264-GRP.part01.rar" gives
// "Heat.1995.1080p.BluRay.x264-GRP"). "" when nothing readable is left.
func releaseStem(file string) string {
	file = file[strings.LastIndexAny(file, `/\`)+1:]
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(file), "."))
	stem := file
	switch {
	case notTheRelease[ext]:
		return ""
	case plainExtensions[ext]:
		stem = strings.TrimSuffix(file, filepath.Ext(file))
	case packedExtensions[ext] || partSuffixRe.MatchString(file):
		// "Name.part01.rar", "Name.7z.001", "Name.vol000+01.par2", "Name.r00".
		for i := 0; i < 3; i++ {
			before := stem
			stem = parVolumeRe.ReplaceAllString(stem, "")
			stem = partSuffixRe.ReplaceAllString(stem, "")
			if packedExtensions[strings.ToLower(strings.TrimPrefix(filepath.Ext(stem), "."))] {
				stem = strings.TrimSuffix(stem, filepath.Ext(stem))
			}
			if stem == before {
				break
			}
		}
	default:
		return ""
	}
	stem = strings.TrimSpace(stem)
	if len([]rune(stem)) < 4 || looksObfuscated(stem+".mkv") {
		return ""
	}
	return stem
}

// ReleaseName is the best name for this release that the NZB itself offers:
// the name in its head when the poster wrote one, else the name of the biggest
// file in it (the video or the archive), else the one of its repair files. ""
// when every name is hidden or random, which is how obfuscated posts look.
func (n *NZB) ReleaseName() string {
	if n.Title != "" {
		return n.Title
	}
	names := outputFileNames(n.Files)
	type entry struct {
		name  string
		bytes int64
	}
	files := make([]entry, len(n.Files))
	for i, f := range n.Files {
		var total int64
		for _, s := range f.Segments {
			total += s.Bytes
		}
		files[i] = entry{names[i], total}
	}
	sort.SliceStable(files, func(a, b int) bool { return files[a].bytes > files[b].bytes })
	var repair string
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.name), "sample") {
			continue
		}
		stem := releaseStem(f.name)
		if stem == "" {
			continue
		}
		if strings.EqualFold(filepath.Ext(f.name), ".par2") {
			if repair == "" {
				repair = stem
			}
			continue
		}
		return stem
	}
	return repair
}
