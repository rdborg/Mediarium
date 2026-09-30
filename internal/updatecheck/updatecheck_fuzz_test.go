package updatecheck

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// Release notes, release lists, checksum lists and archives all come from the
// network. None of them may panic the program or write outside its folder.

func FuzzPlainNotes(f *testing.F) {
	f.Add("## Fixed\n- a **bold** [link](http://x) ![img](y)\n\n\n\n<script>alert(1)</script>\n---\n", 5)
	f.Add(strings.Repeat("<", 100000), 3)
	f.Add("\x00\x1b[31mred\r\rnext\u202e", 1)
	f.Add(strings.Repeat("[a](", 20000), 0)
	f.Fuzz(func(t *testing.T, body string, maxLines int) {
		if maxLines > 1000 {
			maxLines = 1000
		}
		notes, _ := PlainNotes(body, maxLines)
		if !utf8.ValidString(notes) {
			t.Fatalf("notes are not valid UTF-8: %q", notes)
		}
		for _, r := range notes {
			if r < 0x20 && r != '\n' || r == 0x7f {
				t.Fatalf("control character %q in the notes", r)
			}
		}
		// (angle brackets may remain: the page shows the notes as text, never as HTML)
	})
}

func FuzzPickRelease(f *testing.F) {
	f.Add([]byte(`[{"tag_name":"v1.2.3","name":"n","body":"b","prerelease":false,"assets":[{"name":"mediarium_1.2.3_linux_amd64.tar.gz","size":5},{"name":"../../x","size":1}]},{"tag_name":"v9.9.9-rc.1","prerelease":true},{"tag_name":"latest"},{"tag_name":"v1.2.3/../../x"}]`), "1.0.0")
	f.Add([]byte(`[{"tag_name":"v99999999999999999999.0.0"}]`), "v1.0.0-beta.1")
	f.Add([]byte(`[null,1,"x",[]]`), "")
	f.Fuzz(func(t *testing.T, data []byte, running string) {
		var list []githubRelease
		if json.Unmarshal(data, &list) != nil {
			return
		}
		rel, ok := pick(DefaultRepo, running, list)
		if !ok {
			return
		}
		if !strings.HasPrefix(rel.URL, "https://github.com/"+DefaultRepo+"/releases/tag/") || strings.ContainsAny(rel.URL, " \r\n") {
			t.Fatalf("release page address %q", rel.URL)
		}
		for _, a := range rel.Assets {
			if !safeAssetName.MatchString(a.Name) || strings.Contains(a.Name, "..") && strings.Contains(a.Name, "/") {
				t.Fatalf("asset name %q", a.Name)
			}
		}
		_ = Newer(running, rel)
	})
}

func FuzzSumFor(f *testing.F) {
	f.Add([]byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  mediarium_1_linux_amd64.tar.gz\n"), "mediarium_1_linux_amd64.tar.gz")
	f.Add([]byte("zz *./x\n"+strings.Repeat("a", 100000)), "x")
	f.Fuzz(func(t *testing.T, sums []byte, name string) {
		sum, err := SumFor(sums, name)
		if err == nil && (len(sum) != 64 || strings.ToLower(sum) != sum) {
			t.Fatalf("checksum %q", sum)
		}
	})
}

func FuzzVerifySignature(f *testing.F) {
	f.Add([]byte("data"), "AAAA")
	f.Add([]byte(""), "")
	f.Add([]byte("x"), strings.Repeat("A", 200))
	pub, _, _ := ed25519.GenerateKey(nil)
	f.Fuzz(func(t *testing.T, data []byte, sig string) {
		if err := VerifySignature(pub, data, sig); err == nil {
			t.Fatal("a signature by nobody was accepted")
		}
	})
}

// FuzzExtractProgram: whatever an archive holds, at most one new file appears,
// inside the folder it was told to use.
func FuzzExtractProgram(f *testing.F) {
	f.Add([]byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 0xff})
	f.Add([]byte("not gzip"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "a.tar.gz")
		if err := os.WriteFile(archive, data, 0o600); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "out")
		if err := os.Mkdir(out, 0o755); err != nil {
			t.Fatal(err)
		}
		dl, err := ExtractProgram(archive, out)
		entries, _ := os.ReadDir(dir)
		if len(entries) != 2 {
			t.Fatalf("the folder holds %d entries", len(entries))
		}
		if err != nil {
			if left, _ := os.ReadDir(out); len(left) != 0 {
				t.Fatalf("a failed unpack left %d files", len(left))
			}
			return
		}
		if filepath.Dir(dl.Path) != out {
			t.Fatalf("program written to %s", dl.Path)
		}
	})
}
