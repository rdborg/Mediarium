// Package download implements the NNTP (Usenet) download client (NNTP
// written/adapted in-house). Phase 2 will add a torrent
// client alongside this via anacrolix/torrent; nothing here assumes it's
// the only download protocol.
package download

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
)

// NZB is a parsed .nzb file: one or more posted files, each split into
// numbered article segments that must be fetched and reassembled in order.
type NZB struct {
	Files []NZBFile
}

type NZBFile struct {
	Subject  string
	Groups   []string
	Segments []NZBSegment
}

type NZBSegment struct {
	Number    int
	Bytes     int64
	MessageID string
}

// TotalBytes sums every segment's declared size across all files.
func (n NZB) TotalBytes() int64 {
	var total int64
	for _, f := range n.Files {
		for _, s := range f.Segments {
			total += s.Bytes
		}
	}
	return total
}

// --- NZB XML shape (https://sabnzbd.org/wiki/extra/nzb-spec) ---

type nzbXML struct {
	XMLName xml.Name     `xml:"nzb"`
	Files   []nzbFileXML `xml:"file"`
}

type nzbFileXML struct {
	Subject  string          `xml:"subject,attr"`
	Groups   []string        `xml:"groups>group"`
	Segments []nzbSegmentXML `xml:"segments>segment"`
}

type nzbSegmentXML struct {
	Number    int    `xml:"number,attr"`
	Bytes     int64  `xml:"bytes,attr"`
	MessageID string `xml:",chardata"`
}

// ParseNZB parses raw .nzb XML content, sorting each file's segments into
// download order (segment "number" is 1-based and not guaranteed to
// already be in document order). Many real-world .nzb files declare
// iso-8859-1 rather than UTF-8; that's handled explicitly since Go's
// encoding/xml has no built-in non-UTF-8 charset support.
func ParseNZB(data []byte) (*NZB, error) {
	if len(data) > maxNZBBytes {
		return nil, fmt.Errorf("parse nzb: the file is larger than %d MB", maxNZBBytes>>20)
	}
	var raw nzbXML
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = charsetReader
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse nzb: %w", err)
	}
	if len(raw.Files) == 0 {
		return nil, fmt.Errorf("nzb contains no files")
	}
	// Every file is opened for writing at once when the download starts, so an
	// NZB naming a million of them would fill the folder with empty files.
	if len(raw.Files) > maxNZBFiles {
		return nil, fmt.Errorf("parse nzb: it lists more than %d files", maxNZBFiles)
	}

	nzb := &NZB{Files: make([]NZBFile, len(raw.Files))}
	for i, rf := range raw.Files {
		groups := make([]string, 0, len(rf.Groups))
		for _, g := range rf.Groups {
			g = strings.TrimSpace(g)
			if !validNNTPToken(g) {
				return nil, fmt.Errorf("parse nzb: %q is not a newsgroup name", g)
			}
			groups = append(groups, g)
		}
		segments := make([]NZBSegment, len(rf.Segments))
		for j, rs := range rf.Segments {
			id := strings.TrimSpace(rs.MessageID)
			if !validMessageID(id) {
				return nil, fmt.Errorf("parse nzb: %q is not a message id", id)
			}
			size := rs.Bytes
			if size < 0 {
				size = 0
			} else if size > maxSegmentBytes {
				size = maxSegmentBytes
			}
			segments[j] = NZBSegment{Number: rs.Number, Bytes: size, MessageID: id}
		}
		sort.Slice(segments, func(a, b int) bool { return segments[a].Number < segments[b].Number })
		nzb.Files[i] = NZBFile{Subject: rf.Subject, Groups: groups, Segments: segments}
	}
	return nzb, nil
}

// maxNZBBytes is the biggest NZB file that is parsed. Real ones are a few
// hundred KB to a few MB; even a 100 GB release is well under this.
const maxNZBBytes = 64 << 20

// maxNZBFiles is the most files one NZB may hold. A large season pack has a
// few thousand.
const maxNZBFiles = 20000

// maxSegmentBytes is the largest declared article size that is believed. Real
// articles are well under a megabyte and the declared size only feeds progress
// figures, so a hostile negative or enormous value must not skew them or
// overflow the total.
const maxSegmentBytes = 1 << 30

// validMessageID accepts a message-id as it appears in an NZB: printable
// characters without spaces, and no angle brackets other than one pair around
// the whole id. The id is sent to the news server as part of a command, so a
// line break or a space in it must never get through.
func validMessageID(id string) bool {
	id = strings.TrimSuffix(strings.TrimPrefix(id, "<"), ">")
	if id == "" || len(id) > 500 {
		return false
	}
	for _, r := range id {
		if r <= ' ' || r == 0x7f || r == '<' || r == '>' || r > 0x7e {
			return false
		}
	}
	return true
}

// validNNTPToken accepts a newsgroup name: letters, digits and . + - _
func validNNTPToken(name string) bool {
	if name == "" || len(name) > 200 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '+', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// charsetReader supports the handful of non-UTF-8 charsets real .nzb files
// declare in practice — chiefly iso-8859-1/latin1, whose code points 0-255
// map 1:1 onto Unicode, so decoding is a plain byte-to-rune widen with no
// external dependency needed.
func charsetReader(name string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(name) {
	case "iso-8859-1", "latin1", "windows-1252":
		data, err := io.ReadAll(input)
		if err != nil {
			return nil, fmt.Errorf("read %s-encoded content: %w", name, err)
		}
		// Latin-1 bytes become UTF-8 (one or two bytes each), without the
		// four-fold rune slice a plain widening would build.
		out := make([]byte, 0, len(data)+len(data)/8)
		for _, b := range data {
			out = utf8.AppendRune(out, rune(b))
		}
		return bytes.NewReader(out), nil
	default:
		// Any other charset an NZB names (windows-1251, iso-8859-15, ...): the
		// standard tables know them. One that is not known is read as UTF-8.
		if r, err := charset.NewReaderLabel(name, input); err == nil {
			return r, nil
		}
		return input, nil
	}
}
