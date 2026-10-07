package organizer

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Many Usenet releases are posted obfuscated: every file, the PAR2 files
// included, carries a random name ("wFvDJHhDrbESDBLq.mkv",
// "-Bz4tuzCT0GRU49jgjrQx.par2"). The PAR2 data still knows the real names,
// and for each file it keeps an MD5 of the first 16 KB, so a file can be
// matched to its real name before anything else looks at it. Without this,
// par2 looks for files by their real names, finds none and calls the
// release beyond repair.

var (
	par2Magic        = []byte("PAR2\x00PKT")
	par2FileDescType = []byte("PAR 2.0\x00FileDesc")
)

const (
	par2HeaderSize   = 64
	par2MaxPackets   = 100000
	par2MaxNameBytes = 4096
	hash16kSize      = 16 * 1024
)

// par2Target is one file a PAR2 set describes.
type par2Target struct {
	name    string
	hash16k [16]byte
	length  uint64
}

// readPar2Header reads one packet header at the current position and
// returns its length, recovery set id and type. io.EOF means no more packets.
func readPar2Header(r io.Reader) (length uint64, setID, ptype []byte, hash []byte, err error) {
	h := make([]byte, par2HeaderSize)
	if _, err := io.ReadFull(r, h); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, nil, nil, nil, io.EOF
		}
		return 0, nil, nil, nil, err
	}
	if !bytes.Equal(h[:8], par2Magic) {
		return 0, nil, nil, nil, fmt.Errorf("not a PAR2 packet")
	}
	length = binary.LittleEndian.Uint64(h[8:16])
	if length < par2HeaderSize || length%4 != 0 {
		return 0, nil, nil, nil, fmt.Errorf("bad PAR2 packet length %d", length)
	}
	return length, h[32:48], h[48:64], h[16:32], nil
}

// readPar2Targets lists the files a PAR2 file describes. Recovery data is
// skipped over, so even a large volume file is read quickly.
func readPar2Targets(path string) ([]par2Target, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []par2Target
	seen := map[string]bool{}
	for i := 0; i < par2MaxPackets; i++ {
		length, setID, ptype, hash, err := readPar2Header(f)
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		bodyLen := length - par2HeaderSize
		if !bytes.Equal(ptype, par2FileDescType) || bodyLen < 56 || bodyLen > 56+par2MaxNameBytes {
			if _, err := f.Seek(int64(bodyLen), io.SeekCurrent); err != nil {
				return out, err
			}
			continue
		}
		body := make([]byte, bodyLen)
		if _, err := io.ReadFull(f, body); err != nil {
			return out, nil // a cut-off file: keep what was read
		}
		// The packet hash covers the set id, the type and the body; a packet
		// that doesn't match it is damaged and ignored.
		sum := md5.New()
		sum.Write(setID)
		sum.Write(ptype)
		sum.Write(body)
		if !bytes.Equal(sum.Sum(nil), hash) {
			continue
		}
		var t par2Target
		copy(t.hash16k[:], body[32:48])
		t.length = binary.LittleEndian.Uint64(body[48:56])
		t.name = strings.TrimRight(string(body[56:]), "\x00")
		if t.name == "" || seen[t.name] {
			continue
		}
		seen[t.name] = true
		out = append(out, t)
	}
	return out, nil
}

// par2SetID returns the recovery set id of a PAR2 file ("" when it can't
// be read), so the files of one set can be told apart from another's.
func par2SetID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	_, setID, _, _, err := readPar2Header(f)
	if err != nil {
		return ""
	}
	return string(setID)
}

// safeTargetName is the plain file name to give a matched file: PAR2 names
// may carry folders, which are dropped, and anything that could point
// outside the download folder is refused.
func safeTargetName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == ".." || name == "/" || name == "" || strings.ContainsAny(name, "/\x00") {
		return ""
	}
	return name
}

func hashFirst16k(path string) ([16]byte, error) {
	var sum [16]byte
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.CopyN(h, f, hash16kSize); err != nil && err != io.EOF {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// Deobfuscate gives files in dir their real names from the PAR2 data, by
// matching size and the MD5 of the first 16 KB. Files that already have a
// real name, PAR2 files and files nothing matches are left alone. It
// returns how many files were renamed.
func Deobfuscate(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read dir %s: %w", dir, err)
	}
	type file struct {
		name string
		size int64
	}
	var par2s []file
	var others []file
	present := map[string]bool{}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		present[e.Name()] = true
		if strings.HasSuffix(strings.ToLower(e.Name()), ".par2") {
			par2s = append(par2s, file{e.Name(), info.Size()})
		} else {
			others = append(others, file{e.Name(), info.Size()})
		}
	}
	if len(par2s) == 0 {
		return 0, nil
	}
	// Every PAR2 file of a set repeats the file list: the smallest is enough,
	// but sets can differ, so each set's smallest file is read.
	sort.Slice(par2s, func(i, j int) bool { return par2s[i].size < par2s[j].size })
	readSets := map[string]bool{}
	var targets []par2Target
	have := map[string]bool{}
	for _, p := range par2s {
		path := filepath.Join(dir, p.name)
		set := par2SetID(path)
		if set == "" || readSets[set] {
			continue
		}
		readSets[set] = true
		ts, _ := readPar2Targets(path)
		for _, t := range ts {
			if !have[t.name] {
				have[t.name] = true
				targets = append(targets, t)
			}
		}
	}

	// The real names, so a file that already has one is never renamed.
	realNames := map[string]bool{}
	for _, t := range targets {
		if n := safeTargetName(t.name); n != "" {
			realNames[n] = true
		}
	}
	renamed := 0
	used := map[string]bool{}
	for _, t := range targets {
		name := safeTargetName(t.name)
		if name == "" || present[name] {
			continue
		}
		for _, o := range others {
			if used[o.name] || realNames[o.name] || uint64(o.size) != t.length {
				continue
			}
			sum, err := hashFirst16k(filepath.Join(dir, o.name))
			if err != nil || sum != t.hash16k {
				continue
			}
			if err := os.Rename(filepath.Join(dir, o.name), filepath.Join(dir, name)); err != nil {
				return renamed, fmt.Errorf("rename %s to %s: %w", o.name, name, err)
			}
			used[o.name] = true
			present[name] = true
			renamed++
			break
		}
	}
	return renamed, nil
}
