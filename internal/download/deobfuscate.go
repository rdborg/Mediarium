package download

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Some posters hide what a release is: the subject has no file name, or a
// random one, and the files come down as file_3.bin. The yEnc header of an
// article carries the name the file was posted under, so a file whose own name
// tells nothing gets that one. The PAR2 step in the import pipeline then gives
// the rest their real names (organizer.Deobfuscate).

var randomStem = regexp.MustCompile(`^[A-Za-z0-9_-]{24,}$`)

// emptyExtension says whether a file extension tells nothing about the file.
func emptyExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case "", ".bin", ".dat", ".tmp", ".data":
		return true
	}
	return false
}

// looksObfuscated says whether a file name tells nothing: a generic name, one
// without a usable extension, or a long random string.
func looksObfuscated(name string) bool {
	return emptyExtension(filepath.Ext(name)) || randomStem.MatchString(strings.TrimSuffix(name, filepath.Ext(name)))
}

// renameFromYenc gives each file whose name tells nothing the name its yEnc
// header carried, when that one is better: a readable name, or at least a real
// extension (.rar, .par2) so the unpack and repair steps recognise it. It
// returns the paths the files have afterwards, in the same order. Names are made
// safe like any other and only ever name a file inside the download folder.
func renameFromYenc(paths []string, yencNames []string) []string {
	out := append([]string(nil), paths...)
	used := map[string]bool{}
	for _, p := range out {
		used[strings.ToLower(filepath.Base(p))] = true
	}
	for i, p := range out {
		if i >= len(yencNames) || yencNames[i] == "" {
			continue
		}
		base := filepath.Base(p)
		want := filepath.Base(strings.ReplaceAll(yencNames[i], `\`, "/"))
		if !looksObfuscated(base) || (looksObfuscated(want) && emptyExtension(filepath.Ext(want))) {
			continue
		}
		want = shortenFileName(sanitizeFilename(want), maxFileNameBytes)
		if strings.Trim(want, ". ") == "" || strings.HasPrefix(strings.ToLower(want), ".mediarium-") || strings.EqualFold(base, want) {
			continue
		}
		unique := want
		for n := 2; used[strings.ToLower(unique)]; n++ {
			unique = withSuffix(want, " ("+strconv.Itoa(n)+")")
		}
		target := filepath.Join(filepath.Dir(p), unique)
		if err := os.Rename(p, target); err != nil {
			continue
		}
		delete(used, strings.ToLower(base))
		used[strings.ToLower(unique)] = true
		out[i] = target
	}
	return out
}
