package download

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ResumeFile is the name of the small file a download keeps in its folder
// listing which articles are already saved. A download that was paused,
// stopped or cut off by a restart reads it and fetches only the rest.
const ResumeFile = ".mediarium-progress"

type resumeRecord struct {
	Version int                `json:"v"`
	Release string             `json:"release"` // fingerprint of the NZB's articles
	Files   []resumeFileRecord `json:"files"`
}

type resumeFileRecord struct {
	Done  string `json:"done"`  // one bit per article, base64
	Bytes int64  `json:"bytes"` // decoded bytes saved so far
}

// resumeState is which articles of one NZB are saved in the download folder.
type resumeState struct {
	path    string
	release string

	mu    sync.Mutex
	done  [][]bool
	bytes []int64
	dirty bool
}

// nzbFingerprint identifies an NZB by its articles, so a progress file is
// only ever used for the release it was written for.
func nzbFingerprint(nzb *NZB) string {
	h := sha256.New()
	for _, f := range nzb.Files {
		fmt.Fprintf(h, "file %d\n", len(f.Segments))
		for _, s := range f.Segments {
			fmt.Fprintf(h, "%s\n", s.MessageID)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// loadResume reads the folder's progress file. What it recorded is used only
// when it belongs to this NZB and the files it describes are still there;
// otherwise the download starts from nothing. paths are the output files in
// NZB order.
func loadResume(dir string, nzb *NZB, paths []string) *resumeState {
	st := &resumeState{
		path:    filepath.Join(dir, ResumeFile),
		release: nzbFingerprint(nzb),
		done:    make([][]bool, len(nzb.Files)),
		bytes:   make([]int64, len(nzb.Files)),
	}
	for i, f := range nzb.Files {
		st.done[i] = make([]bool, len(f.Segments))
	}
	data, err := os.ReadFile(st.path)
	if err != nil {
		return st
	}
	var rec resumeRecord
	if json.Unmarshal(data, &rec) != nil || rec.Version != 1 || rec.Release != st.release || len(rec.Files) != len(nzb.Files) {
		_ = os.Remove(st.path)
		return st
	}
	for i, fr := range rec.Files {
		bits, err := base64.StdEncoding.DecodeString(fr.Done)
		if err != nil || len(bits)*8 < len(st.done[i]) {
			continue
		}
		// A file that has gone (or is empty) cannot hold what was recorded.
		if info, err := os.Stat(paths[i]); err != nil || info.Size() == 0 {
			continue
		}
		var n int64
		for j := range st.done[i] {
			if bits[j/8]&(1<<(j%8)) != 0 {
				st.done[i][j] = true
				n++
			}
		}
		if n > 0 {
			// what the file says was saved cannot be negative or more than the file holds
			st.bytes[i] = min(max(fr.Bytes, 0), fileSizeLimit(nzb.Files[i]))
		}
	}
	return st
}

// has reports whether file i holds any saved article.
func (r *resumeState) has(i int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.done[i] {
		if d {
			return true
		}
	}
	return false
}

func (r *resumeState) isDone(file, segment int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done[file][segment]
}

// mark records that an article of n decoded bytes is saved.
func (r *resumeState) mark(file, segment int, n int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done[file][segment] {
		r.done[file][segment] = true
		r.bytes[file] += n
		r.dirty = true
	}
}

// savedBytes is how many decoded bytes are already saved.
func (r *resumeState) savedBytes() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int64
	for _, b := range r.bytes {
		total += b
	}
	return total
}

// save writes the progress file if anything changed since the last save. It
// writes a temporary file first, so a crash never leaves half a record.
func (r *resumeState) save() {
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return
	}
	rec := resumeRecord{Version: 1, Release: r.release, Files: make([]resumeFileRecord, len(r.done))}
	for i, d := range r.done {
		bits := make([]byte, (len(d)+7)/8)
		for j, ok := range d {
			if ok {
				bits[j/8] |= 1 << (j % 8)
			}
		}
		rec.Files[i] = resumeFileRecord{Done: base64.StdEncoding.EncodeToString(bits), Bytes: r.bytes[i]}
	}
	r.dirty = false
	r.mu.Unlock()

	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, r.path); err != nil {
		_ = os.Remove(tmp)
	}
}

// remove deletes the progress file once the download is complete.
func (r *resumeState) remove() {
	_ = os.Remove(r.path)
	_ = os.Remove(r.path + ".tmp")
}
