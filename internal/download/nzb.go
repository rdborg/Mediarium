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
	var raw nzbXML
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = charsetReader
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse nzb: %w", err)
	}
	if len(raw.Files) == 0 {
		return nil, fmt.Errorf("nzb contains no files")
	}

	nzb := &NZB{Files: make([]NZBFile, len(raw.Files))}
	for i, rf := range raw.Files {
		segments := make([]NZBSegment, len(rf.Segments))
		for j, rs := range rf.Segments {
			segments[j] = NZBSegment{Number: rs.Number, Bytes: rs.Bytes, MessageID: rs.MessageID}
		}
		sort.Slice(segments, func(a, b int) bool { return segments[a].Number < segments[b].Number })
		nzb.Files[i] = NZBFile{Subject: rf.Subject, Groups: rf.Groups, Segments: segments}
	}
	return nzb, nil
}

// charsetReader supports the handful of non-UTF-8 charsets real .nzb files
// declare in practice — chiefly iso-8859-1/latin1, whose code points 0-255
// map 1:1 onto Unicode, so decoding is a plain byte-to-rune widen with no
// external dependency needed.
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "iso-8859-1", "latin1", "windows-1252":
		data, err := io.ReadAll(input)
		if err != nil {
			return nil, fmt.Errorf("read %s-encoded content: %w", charset, err)
		}
		runes := make([]rune, len(data))
		for i, b := range data {
			runes[i] = rune(b)
		}
		return strings.NewReader(string(runes)), nil
	default:
		return input, nil
	}
}
