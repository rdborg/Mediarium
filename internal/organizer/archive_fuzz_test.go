package organizer

import (
	"path/filepath"
	"strings"
	"testing"
)

// FuzzSafeJoin checks that no archive entry name resolves to a path outside
// the folder it is unpacked into.
func FuzzSafeJoin(f *testing.F) {
	for _, s := range []string{
		"movie.mkv", "a/b/c.mkv", "./a", "a/./b", "a//b", "a/../b", "..", "../x", "a/..", "/etc/passwd", `\etc\passwd`,
		`C:\x`, "C:x", "c:/x", `a\..\..\x`, "a/b/", "", ".", "./", "a\x00b", "..\x00", "....//x", ".../x", " ../x", "a/ ../x",
		strings.Repeat("../", 100) + "x", strings.Repeat("a/", 1000) + "b", "日本語/x.mkv", "\xff\xfe/x", "a/\u202e/x",
	} {
		f.Add(s)
	}
	dest := filepath.Join(string(filepath.Separator), "work", "unpack")
	f.Fuzz(func(t *testing.T, name string) {
		got, err := safeJoin(dest, name)
		if err != nil {
			return
		}
		rel, relErr := filepath.Rel(dest, got)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			t.Fatalf("safeJoin(%q) = %q leaves %q", name, got, dest)
		}
		if strings.ContainsRune(got, 0) {
			t.Fatalf("safeJoin(%q) = %q holds a NUL byte", name, got)
		}
		if again, _ := safeJoin(dest, name); again != got {
			t.Fatalf("safeJoin(%q) is not deterministic", name)
		}
	})
}

// FuzzCheckLink checks that a link accepted by checkLink cannot point outside
// the destination, however it is written.
func FuzzCheckLink(f *testing.F) {
	for _, s := range [][2]string{
		{"a/link", "../b"}, {"a/link", "../../b"}, {"link", ".."}, {"link", "/etc"}, {"a/link", `..\..\x`}, {"link", `C:\x`},
		{"a/b/link", "../../c"}, {"a/b/link", "../../../c"}, {"link", ""}, {"link", "a/../../b"}, {"../link", "x"},
	} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, linkName, target string) {
		if checkLink("/work/unpack", linkName, target) != nil {
			return
		}
		if target == "" {
			return
		}
		norm, err := normalizeEntryName(linkName)
		if err != nil {
			t.Fatalf("checkLink accepted the bad link name %q", linkName)
		}
		t2 := strings.ReplaceAll(target, `\`, "/")
		if strings.HasPrefix(t2, "/") {
			t.Fatalf("checkLink accepted the absolute target %q", target)
		}
		// Resolve as a file system would: walk the target from the link's folder.
		depth := len(strings.Split(strings.Trim(filepath.ToSlash(filepath.Dir(norm)), "./"), "/"))
		if filepath.Dir(norm) == "." {
			depth = 0
		}
		for _, part := range strings.Split(t2, "/") {
			switch part {
			case "..":
				depth--
			case "", ".":
			default:
				depth++
			}
			if depth < 0 {
				t.Fatalf("checkLink accepted %q -> %q which climbs out", linkName, target)
			}
		}
	})
}
