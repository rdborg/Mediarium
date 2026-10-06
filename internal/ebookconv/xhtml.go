package ebookconv

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ---- Resources: images and fonts

// resource is one image or font stored in the book.
type resource struct {
	name string // "image0001.jpg"
	mime string
	data []byte
}

// sniffImage names an image record by its first bytes ("" = not an image).
func sniffImage(b []byte) (ext, mime string) {
	switch {
	case len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpg", "image/jpeg"
	case len(b) > 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "png", "image/png"
	case len(b) > 6 && (bytes.Equal(b[:6], []byte("GIF87a")) || bytes.Equal(b[:6], []byte("GIF89a"))):
		return "gif", "image/gif"
	case len(b) > 2 && b[0] == 'B' && b[1] == 'M':
		return "bmp", "image/bmp"
	case len(b) > 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "webp", "image/webp"
	}
	return "", ""
}

// fontRecord unpacks a KF8 "FONT" record (zlib, sometimes with its first
// bytes scrambled by a key).
func fontRecord(rec []byte) ([]byte, string) {
	if len(rec) < 24 || string(rec[:4]) != "FONT" {
		return nil, ""
	}
	usize := int(binary.BigEndian.Uint32(rec[4:]))
	flags := binary.BigEndian.Uint32(rec[8:])
	dstart := int(binary.BigEndian.Uint32(rec[12:]))
	xorLen := int(binary.BigEndian.Uint32(rec[16:]))
	xorStart := int(binary.BigEndian.Uint32(rec[20:]))
	if dstart > len(rec) {
		return nil, ""
	}
	data := append([]byte(nil), rec[dstart:]...)
	if flags&2 != 0 && xorLen > 0 && xorStart+xorLen <= len(rec) {
		key := rec[xorStart : xorStart+xorLen]
		for i := 0; i < len(data) && i < 1040; i++ {
			data[i] ^= key[i%xorLen]
		}
	}
	if flags&1 != 0 {
		z, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, ""
		}
		out, err := io.ReadAll(io.LimitReader(z, int64(usize)+1))
		if err != nil {
			return nil, ""
		}
		data = out
	}
	switch {
	case len(data) > 4 && (bytes.Equal(data[:4], []byte("OTTO"))):
		return data, "otf"
	case len(data) > 4 && bytes.Equal(data[:4], []byte("wOFF")):
		return data, "woff"
	}
	return data, "ttf"
}

// ---- Text encoding

// cp1252 maps Windows-1252's 0x80-0x9F to Unicode; the rest is Latin-1.
var cp1252 = [32]rune{
	'€', 0x81, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0x8D, 'Ž', 0x8F,
	0x90, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0x9D, 'ž', 'Ÿ',
}

// toUTF8 decodes Windows-1252 text, or tidies invalid UTF-8.
func toUTF8(b []byte, encoding uint32) []byte {
	if encoding == 65001 {
		if utf8.Valid(b) {
			return b
		}
		return []byte(strings.ToValidUTF8(string(b), "�"))
	}
	var out bytes.Buffer
	out.Grow(len(b) + len(b)/8)
	for _, c := range b {
		switch {
		case c < 0x80:
			out.WriteByte(c)
		case c < 0xA0:
			out.WriteRune(cp1252[c-0x80])
		default:
			out.WriteRune(rune(c))
		}
	}
	return out.Bytes()
}

// ---- XHTML

var voidElements = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}

// Elements dropped with their content, and elements dropped but whose
// content is kept (Kindle-only tags).
var (
	dropElements = map[string]bool{"script": true, "noscript": true, "guide": true, "object": true, "iframe": true, "form": true}
	unwrapPrefix = []string{"mbp:", "amzn:", "kindle:", "aid:"}
)

var xmlName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9._-]*$`)

// rewriter changes an attribute value as it is written ("" drops it).
type rewriter func(element, attr, value string) (string, bool)

// writeXHTML writes a parsed document as well-formed XHTML: every element
// closed, attributes quoted and escaped, Kindle-only tags left out.
func writeXHTML(w *bytes.Buffer, doc *html.Node, title string, rewrite rewriter) {
	w.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<!DOCTYPE html>\n")
	w.WriteString(`<html xmlns="http://www.w3.org/1999/xhtml">`)
	var head, body *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Head && head == nil {
			head = n
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Body && body == nil {
			body = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	w.WriteString("<head><meta charset=\"utf-8\"/>")
	hasTitle := false
	if head != nil {
		for c := head.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.DataAtom == atom.Meta) {
				continue // our own charset line stands
			}
			if c.Type == html.ElementNode && c.DataAtom == atom.Title {
				hasTitle = true
			}
			writeNode(w, c, rewrite, false)
		}
	}
	if !hasTitle {
		w.WriteString("<title>")
		w.WriteString(html.EscapeString(title))
		w.WriteString("</title>")
	}
	w.WriteString("</head><body")
	if body != nil {
		writeAttrs(w, body, rewrite)
	}
	w.WriteString(">")
	if body != nil {
		for c := body.FirstChild; c != nil; c = c.NextSibling {
			writeNode(w, c, rewrite, false)
		}
	}
	w.WriteString("</body></html>\n")
}

func escapeText(w *bytes.Buffer, s string) {
	for _, r := range s {
		switch r {
		case '&':
			w.WriteString("&amp;")
		case '<':
			w.WriteString("&lt;")
		case '>':
			w.WriteString("&gt;")
		case '"':
			w.WriteString("&quot;")
		default:
			if r < 0x20 && r != '\n' && r != '\t' && r != '\r' {
				continue // not allowed in XML
			}
			w.WriteRune(r)
		}
	}
}

func writeAttrs(w *bytes.Buffer, n *html.Node, rewrite rewriter) {
	seen := map[string]bool{}
	for _, a := range n.Attr {
		name := a.Key
		if a.Namespace == "xlink" {
			name = "xlink:" + a.Key
		} else if a.Namespace != "" || !xmlName.MatchString(name) {
			continue
		}
		// aid: Kindle's own position marks, of no use in an EPUB.
		if name == "xmlns" || strings.HasPrefix(name, "xmlns:") || name == "aid" || seen[name] {
			continue
		}
		val := a.Val
		if rewrite != nil {
			v, keep := rewrite(n.Data, name, val)
			if !keep {
				continue
			}
			val = v
		}
		seen[name] = true
		w.WriteByte(' ')
		w.WriteString(name)
		w.WriteString(`="`)
		escapeText(w, val)
		w.WriteByte('"')
	}
}

func writeNode(w *bytes.Buffer, n *html.Node, rewrite rewriter, inSVG bool) {
	switch n.Type {
	case html.TextNode:
		escapeText(w, n.Data)
		return
	case html.ElementNode:
	default:
		return // comments, doctypes
	}
	name := n.Data
	if dropElements[name] {
		return
	}
	for _, p := range unwrapPrefix {
		if strings.HasPrefix(name, p) {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				writeNode(w, c, rewrite, inSVG)
			}
			return
		}
	}
	if !xmlName.MatchString(name) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			writeNode(w, c, rewrite, inSVG)
		}
		return
	}
	w.WriteByte('<')
	w.WriteString(name)
	if n.Namespace == "svg" && name == "svg" && !inSVG {
		w.WriteString(` xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"`)
		inSVG = true
	}
	writeAttrs(w, n, rewrite)
	if n.FirstChild == nil && (voidElements[name] || inSVG) {
		w.WriteString("/>")
		return
	}
	w.WriteByte('>')
	if voidElements[name] {
		w.WriteString("</" + name + ">")
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if (name == "style") && c.Type == html.TextNode {
			escapeText(w, c.Data)
			continue
		}
		writeNode(w, c, rewrite, inSVG)
	}
	w.WriteString("</")
	w.WriteString(name)
	w.WriteByte('>')
}

// headingText is the first heading's text in a document ("" = none).
func headingText(doc *html.Node) string {
	var found string
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if n.Type == html.ElementNode && (n.DataAtom == atom.H1 || n.DataAtom == atom.H2 || n.DataAtom == atom.H3) {
			found = strings.Join(strings.Fields(textOf(n)), " ")
			if found != "" {
				return true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(doc)
	if len(found) > 120 {
		found = found[:120]
	}
	return found
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// ids lists the id and name attributes in a document.
func ids(doc *html.Node) []string {
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if (a.Key == "id" || a.Key == "name") && a.Val != "" {
					out = append(out, a.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}
