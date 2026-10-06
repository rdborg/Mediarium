package ebookconv

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// MaxInput is the largest book the converter takes (Kindle books with
// pictures stay well under it).
const MaxInput = 200 << 20

// Convert reads a MOBI, AZW or AZW3 book and writes it to w as an EPUB. A
// book with both halves (MOBI 6 and KF8) is converted from its KF8 half, with
// the MOBI 6 half as the fallback.
func Convert(data []byte, w io.Writer) (meta Metadata, err error) {
	// A book is written by strangers: a file broken in a way not foreseen
	// here fails the conversion, it never takes Mediarium down.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrNotMobi, r)
		}
	}()
	if len(data) > MaxInput {
		return Metadata{}, ErrUnsupported
	}
	p, err := readPDB(data)
	if err != nil {
		return Metadata{}, err
	}
	h0, err := parseHeader(p, 0)
	if err != nil {
		return Metadata{}, err
	}
	meta = metadataOf(h0)

	var kf8 *header
	if h0.version >= 8 {
		kf8 = h0
	} else if b := h0.exthInt(121); b > 0 {
		for _, c := range []int{b, b + 1} {
			if h, err := parseHeader(p, c); err == nil && h.version >= 8 {
				kf8 = h
				// The two halves share one set of images, listed in the MOBI 6 header.
				kf8.resBase = firstResource(p, h0)
				break
			}
		}
	}
	var book *epubBook
	if kf8 != nil {
		if m := metadataOf(kf8); m.Title != "" {
			meta = m
		}
		book, err = convertKF8(p, kf8, meta)
	}
	if book == nil && h0.version < 8 {
		book, err = convertMobi6(p, h0, meta)
	}
	if err != nil {
		return meta, err
	}
	var buf bytes.Buffer
	if err := book.write(&buf); err != nil {
		return meta, err
	}
	_, err = w.Write(buf.Bytes())
	return meta, err
}

func metadataOf(h *header) Metadata {
	m := Metadata{Title: strings.TrimSpace(h.exthString(503)), Language: strings.TrimSpace(h.exthString(524))}
	if m.Title == "" {
		m.Title = strings.TrimSpace(h.fullName)
	}
	var authors []string
	for _, a := range h.exth[100] {
		if s := strings.TrimSpace(string(a)); s != "" {
			authors = append(authors, s)
		}
	}
	m.Author = strings.Join(authors, ", ")
	if h.encoding != 65001 {
		m.Title = string(toUTF8([]byte(m.Title), h.encoding))
		m.Author = string(toUTF8([]byte(m.Author), h.encoding))
	}
	return m
}
