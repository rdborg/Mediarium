// Package mediainfo reads what a video file holds: its container, length,
// video, audio tracks and subtitle tracks. Only the headers are read (the
// Matroska track list, the MP4 moov box), never the picture or the sound, so
// it is quick even on a network share, and every read is bounded so a huge or
// hostile file cannot hang the app or use much memory. It is pure Go and needs
// no ffprobe.
package mediainfo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Info is what is known about one file.
type Info struct {
	Container string     `json:"container"`          // "matroska", "webm", "mp4", "mov"; "rar", "zip" or "7z" for an archive under a video's name (see IsArchive); or the extension for a container this can't open
	Duration  float64    `json:"duration,omitempty"` // seconds
	Video     *Video     `json:"video,omitempty"`    // the first video track
	Audio     []Audio    `json:"audio,omitempty"`
	Subtitles []Subtitle `json:"subtitles,omitempty"`
}

// Video is the picture track.
type Video struct {
	Codec     string  `json:"codec"` // "H.264", "HEVC", "AV1", "VP9", "MPEG-2"...
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	BitDepth  int     `json:"bitDepth,omitempty"` // 8, 10, 12; 0 when the file doesn't say
	HDR       string  `json:"hdr,omitempty"`      // SDR, HDR10, HDR10+, Dolby Vision, HLG
	DVProfile int     `json:"dvProfile,omitempty"`
	FPS       float64 `json:"fps,omitempty"`
}

// Audio is one sound track.
type Audio struct {
	Codec    string `json:"codec"` // "AAC", "AC-3", "E-AC-3", "DTS", "DTS-HD MA", "TrueHD", "FLAC", "Opus", "MP3"...
	Channels int    `json:"channels,omitempty"`
	Lang     string `json:"lang,omitempty"` // two-letter code when there is one ("en"), else the file's own
	Title    string `json:"title,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
	Atmos    bool   `json:"atmos,omitempty"` // a hint: the track says so (or its E-AC-3 header does)
}

// Subtitle is one subtitle track.
type Subtitle struct {
	Codec   string `json:"codec"` // "SRT", "ASS", "PGS", "VobSub", "WebVTT", "mov_text"...
	Lang    string `json:"lang,omitempty"`
	Title   string `json:"title,omitempty"`
	Default bool   `json:"default,omitempty"`
	Forced  bool   `json:"forced,omitempty"`
}

// ErrUnreadable is returned for a file that looks like Matroska or MP4 but
// whose headers can't be read (cut off, damaged, or too big to trust).
var ErrUnreadable = errors.New("the file's headers can't be read")

// ErrTimeout is returned when the disk didn't answer in time, as on a share
// that has gone away. It says nothing about the file itself.
var ErrTimeout = errors.New("reading the file took too long")

const (
	// readBudget is the most one file may make this read: a track list, a
	// few small MP4 boxes and (for HDR10+) one slice of video.
	readBudget = 12 << 20
	// readTimeout ends a read that hangs, as on a share that has gone away.
	readTimeout = 45 * time.Second
	maxTracks   = 64
)

// ReadFile reads the file at path. A container this doesn't open returns
// only Container (the lower-case extension). The read gives up after a time
// limit; the goroutine doing it ends as soon as the disk answers.
func ReadFile(path string) (Info, error) {
	type result struct {
		info Info
		err  error
	}
	done := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	go func() {
		f, err := os.Open(path)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			done <- result{err: err}
			return
		}
		info, err := Read(&ctxReader{ctx: ctx, r: f}, st.Size(), strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")))
		done <- result{info, err}
	}()
	select {
	case r := <-done:
		return r.info, r.err
	case <-ctx.Done():
		return Info{}, ErrTimeout
	}
}

// ctxReader stops reading once its context has ended.
type ctxReader struct {
	ctx context.Context
	r   io.ReaderAt
}

func (c *ctxReader) ReadAt(p []byte, off int64) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.ReadAt(p, off)
}

// Read works out the Info of a file given as a reader of the given size.
// ext is the file's extension, used to name a container that isn't read.
func Read(ra io.ReaderAt, size int64, ext string) (Info, error) {
	r := &reader{ra: ra, size: size, budget: readBudget}
	var head [12]byte
	n, _ := ra.ReadAt(head[:], 0)
	if n < 12 {
		return Info{Container: ext}, nil
	}
	if kind := archiveKind(head[:]); kind != "" {
		return Info{Container: kind}, nil
	}
	if head[0] == 0x1A && head[1] == 0x45 && head[2] == 0xDF && head[3] == 0xA3 {
		return readMatroska(r)
	}
	box := string(head[4:8])
	if box == "ftyp" || (ext == "mov" || ext == "mp4" || ext == "m4v") && (box == "moov" || box == "mdat" || box == "wide" || box == "free" || box == "skip") {
		return readMP4(r)
	}
	return Info{Container: ext}, nil
}

// Archives saved under a video's name. A release that was never unpacked (an
// obfuscated one, or a RAR set whose parts were renamed) can be filed as
// "Show - S01E01.mkv" while it holds a RAR volume: no player can open it. Its
// first bytes say what it really is, whatever its name says.
var archiveMagic = []struct {
	kind  string
	magic []byte
}{
	{"rar", []byte("Rar!\x1a\x07")},                  // RAR 4 and 5
	{"zip", []byte("PK\x03\x04")},                    // ZIP
	{"7z", []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C}}, // 7-Zip
}

func archiveKind(head []byte) string {
	for _, a := range archiveMagic {
		if len(head) >= len(a.magic) && string(head[:len(a.magic)]) == string(a.magic) {
			return a.kind
		}
	}
	return ""
}

// IsArchive says whether container is an archive Read found in place of a
// video ("rar", "zip", "7z"), as opposed to a video container.
func IsArchive(container string) bool {
	for _, a := range archiveMagic {
		if container == a.kind {
			return true
		}
	}
	return false
}

// ArchiveContainers lists the containers IsArchive is true for.
func ArchiveContainers() []string {
	out := make([]string, len(archiveMagic))
	for i, a := range archiveMagic {
		out[i] = a.kind
	}
	return out
}

// reader hands out bounded slices of a file.
type reader struct {
	ra     io.ReaderAt
	size   int64
	budget int64
}

// at reads n bytes from off, fewer at the end of the file. It fails when the
// file's read budget is used up.
func (r *reader) at(off, n int64) ([]byte, error) {
	if off < 0 || n < 0 || off > r.size {
		return nil, ErrUnreadable
	}
	if off+n > r.size {
		n = r.size - off
	}
	if n > r.budget {
		return nil, ErrUnreadable
	}
	r.budget -= n
	buf := make([]byte, n)
	got, err := r.ra.ReadAt(buf, off)
	if got < len(buf) {
		if err == nil || errors.Is(err, io.EOF) {
			return buf[:got], nil
		}
		return nil, err
	}
	return buf, nil
}

// Resolution is the picture size as people name it ("2160p", "1080p"...), or
// "" when it isn't known. A wide film cropped to 1920x800 is 1080p: the width
// counts as well as the height.
func (v Video) Resolution() string {
	w, h := v.Width, v.Height
	switch {
	case w == 0 && h == 0:
		return ""
	case w >= 3400 || h >= 2000:
		return "2160p"
	case w >= 2500 || h >= 1300:
		return "1440p"
	case w >= 1800 || h >= 1000:
		return "1080p"
	case w >= 1200 || h >= 700:
		return "720p"
	case h >= 540 || w >= 900:
		return "576p"
	}
	return "480p"
}

// ChannelLayout writes a channel count the usual way: 2.0, 5.1, 7.1.
func ChannelLayout(n int) string {
	switch {
	case n <= 0:
		return ""
	case n >= 6:
		return fmt.Sprintf("%d.1", n-1)
	}
	return fmt.Sprintf("%d.0", n)
}
