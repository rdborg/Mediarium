package organizer

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// par2Packet builds one PAR2 packet with a correct header and hash.
func par2Packet(setID []byte, ptype string, body []byte) []byte {
	h := md5.New()
	h.Write(setID)
	h.Write([]byte(ptype))
	h.Write(body)
	var b bytes.Buffer
	b.Write(par2Magic)
	l := make([]byte, 8)
	binary.LittleEndian.PutUint64(l, uint64(par2HeaderSize+len(body)))
	b.Write(l)
	b.Write(h.Sum(nil))
	b.Write(setID)
	b.Write([]byte(ptype))
	b.Write(body)
	return b.Bytes()
}

func fileDesc(setID []byte, name string, data []byte) []byte {
	body := make([]byte, 56)
	n := len(data)
	if n > hash16kSize {
		n = hash16kSize
	}
	sum := md5.Sum(data[:n])
	copy(body[32:48], sum[:])
	binary.LittleEndian.PutUint64(body[48:56], uint64(len(data)))
	nameBytes := []byte(name)
	for len(nameBytes)%4 != 0 {
		nameBytes = append(nameBytes, 0)
	}
	return par2Packet(setID, string(par2FileDescType), append(body, nameBytes...))
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDeobfuscateRestoresRealNames(t *testing.T) {
	dir := t.TempDir()
	setID := bytes.Repeat([]byte{7}, 16)
	video := randomBytes(t, 40000)
	rar := randomBytes(t, 20000)
	sameSize := randomBytes(t, 20000) // same size as the rar, other content
	sample := randomBytes(t, 3000)

	var par bytes.Buffer
	par.Write(par2Packet(setID, "PAR 2.0\x00Main\x00\x00\x00\x00", make([]byte, 12))) // skipped over
	par.Write(fileDesc(setID, "Show.S01E02.2160p.WEB.mkv", video))
	par.Write(fileDesc(setID, "Show.S01E02.part01.rar", rar))
	par.Write(fileDesc(setID, "subs/../../evil.srt", sample)) // a folder in the name is dropped
	par.Write(fileDesc(setID, "Already.Right.nfo", []byte("x")))
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "-Bz4tuzCT0GRU49jgjrQx.par2"), par.Bytes(), 0o644))
	must(os.WriteFile(filepath.Join(dir, "wFvDJHhDrbESDBLq"), video, 0o644))
	must(os.WriteFile(filepath.Join(dir, "a8Fk2.bin"), sameSize, 0o644))
	must(os.WriteFile(filepath.Join(dir, "x9.bin"), rar, 0o644))
	must(os.WriteFile(filepath.Join(dir, "q1"), sample, 0o644))
	must(os.WriteFile(filepath.Join(dir, "Already.Right.nfo"), []byte("x"), 0o644))

	n, err := Deobfuscate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("renamed %d files, want 3", n)
	}
	for _, want := range []string{"Show.S01E02.2160p.WEB.mkv", "Show.S01E02.part01.rar", "evil.srt", "a8Fk2.bin", "Already.Right.nfo", "-Bz4tuzCT0GRU49jgjrQx.par2"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("%s missing after renaming: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.srt")); err == nil {
		t.Fatal("a name with ../ must never leave the download folder")
	}
	// A second run changes nothing.
	if n, err := Deobfuscate(dir); err != nil || n != 0 {
		t.Fatalf("second run renamed %d (%v), want 0", n, err)
	}
}

func TestDeobfuscateIgnoresDamagedPackets(t *testing.T) {
	dir := t.TempDir()
	setID := bytes.Repeat([]byte{3}, 16)
	data := randomBytes(t, 5000)
	pkt := fileDesc(setID, "Real.Name.mkv", data)
	pkt[len(pkt)-5] ^= 0xff // break the name, so the packet hash no longer matches
	if err := os.WriteFile(filepath.Join(dir, "a.par2"), pkt, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "obf"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if n, _ := Deobfuscate(dir); n != 0 {
		t.Fatalf("a damaged packet must not rename anything, renamed %d", n)
	}
}

func TestOnePar2FilePerSet(t *testing.T) {
	dir := t.TempDir()
	a := bytes.Repeat([]byte{1}, 16)
	b := bytes.Repeat([]byte{2}, 16)
	small := par2Packet(a, "PAR 2.0\x00Main\x00\x00\x00\x00", make([]byte, 12))
	big := append(append([]byte{}, small...), par2Packet(a, "PAR 2.0\x00RecvSlic", make([]byte, 400))...)
	other := par2Packet(b, "PAR 2.0\x00Main\x00\x00\x00\x00", make([]byte, 12))
	files := map[string][]byte{"k3J.par2": big, "Zq.par2": small, "other.par2": other}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := FindMainPar2Files(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want one file per set, got %v", got)
	}
	for _, g := range got {
		if filepath.Base(g) == "k3J.par2" {
			t.Fatalf("the smaller file of a set should be used, got %v", got)
		}
	}
}

func TestPar2ProblemIsOneSentence(t *testing.T) {
	out := "Loading \"x.par2\".\nLoading: 11.6%\rLoading: 11.7%\rLoading: 24.1%\n\nRepair is not possible.\nYou need 12 more recovery blocks to be able to repair.\n"
	if got := par2Problem([]byte(out)); got != "PAR2 can't repair this download: it needs 12 more recovery blocks than the release has" {
		t.Fatalf("got %q", got)
	}
	if got := par2Problem([]byte("Loading: 3.0%\rLoading: 6.1%\r")); got != "PAR2 couldn't repair this download" {
		t.Fatalf("got %q", got)
	}
	if got := par2Problem([]byte("Loading: 3%\nRepair is not possible.\n")); !strings.Contains(got, "too much of it is missing") {
		t.Fatalf("got %q", got)
	}
}

// With the real par2 tool: an obfuscated, slightly damaged release is
// repaired after its files get their real names back.
func TestObfuscatedReleaseRepairsRealBinary(t *testing.T) {
	if _, err := exec.LookPath("par2"); err != nil {
		t.Skip("par2 binary not available on this machine")
	}
	dir := t.TempDir()
	data := randomBytes(t, 600000)
	real := filepath.Join(dir, "Show.S01E02.mkv")
	if err := os.WriteFile(real, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("par2", "create", "-q", "-r10", filepath.Join(dir, "Show.S01E02.par2"), real).CombinedOutput(); err != nil {
		t.Fatalf("par2 create: %v %s", err, out)
	}
	entries, _ := os.ReadDir(dir)
	for i, e := range entries {
		if strings.HasSuffix(e.Name(), ".par2") {
			_ = os.Rename(filepath.Join(dir, e.Name()), filepath.Join(dir, "obf"+string(rune('a'+i))+".par2"))
		}
	}
	// Damage past the first 16 KB, so the name can still be matched.
	data[300000] ^= 0xff
	if err := os.WriteFile(filepath.Join(dir, "wFvDJHhDrbESDBLq.mkv"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(real)

	if n, err := Deobfuscate(dir); err != nil || n != 1 {
		t.Fatalf("Deobfuscate = %d, %v", n, err)
	}
	mains, err := FindMainPar2Files(dir)
	if err != nil || len(mains) != 1 {
		t.Fatalf("mains = %v, %v", mains, err)
	}
	r := NewRepairer()
	if err := r.Repair(mains[0]); err != nil {
		t.Fatalf("repair: %v", err)
	}
	got, _ := os.ReadFile(real)
	data[300000] ^= 0xff
	if !bytes.Equal(got, data) {
		t.Fatal("the file was not repaired")
	}
}
