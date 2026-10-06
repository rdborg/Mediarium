package ebookconv

import (
	"archive/zip"
	"bytes"
	"fmt"
	"hash/fnv"
	"html"
	"io"
	"strings"
	"time"
)

// Metadata is what the book says about itself.
type Metadata struct {
	Title    string
	Author   string
	Language string
}

// chapter is one XHTML file of the EPUB.
type chapter struct {
	name  string // "part0001.xhtml"
	title string // for the table of contents
	data  []byte
}

type stylesheet struct {
	name string // "style0001.css"
	data []byte
}

type epubBook struct {
	meta     Metadata
	chapters []chapter
	styles   []stylesheet
	images   []resource
	fonts    []resource
	cover    string // the cover image's name, "" = none
}

func esc(s string) string { return html.EscapeString(s) }

// write packs the book as an EPUB 2 file: OPF package, NCX table of contents,
// text in text/, styles in styles/, images and fonts.
func (b *epubBook) write(w io.Writer) error {
	z := zip.NewWriter(w)
	// The mimetype entry comes first and uncompressed.
	mt, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store, Modified: time.Unix(0, 0)})
	if err != nil {
		return err
	}
	io.WriteString(mt, "application/epub+zip")
	add := func(name string, data []byte) error {
		f, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Unix(0, 0)})
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}
	if err := add("META-INF/container.xml", []byte(`<?xml version="1.0" encoding="utf-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>
`)); err != nil {
		return err
	}
	title := b.meta.Title
	if strings.TrimSpace(title) == "" {
		title = "Untitled"
	}
	lang := b.meta.Language
	if lang == "" {
		lang = "en"
	}
	var opf bytes.Buffer
	fmt.Fprintf(&opf, `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="bookid">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
<dc:title>%s</dc:title>
`, esc(title))
	if b.meta.Author != "" {
		fmt.Fprintf(&opf, "<dc:creator opf:role=\"aut\">%s</dc:creator>\n", esc(b.meta.Author))
	}
	id := fnv.New64a()
	io.WriteString(id, title+"|"+b.meta.Author)
	fmt.Fprintf(&opf, "<dc:language>%s</dc:language>\n<dc:identifier id=\"bookid\">mediarium-converted-%x</dc:identifier>\n", esc(lang), id.Sum64())
	if b.cover != "" {
		opf.WriteString("<meta name=\"cover\" content=\"cover-image\"/>\n")
	}
	opf.WriteString("</metadata>\n<manifest>\n<item id=\"ncx\" href=\"toc.ncx\" media-type=\"application/x-dtbncx+xml\"/>\n")
	for i, c := range b.chapters {
		fmt.Fprintf(&opf, "<item id=\"text%d\" href=\"text/%s\" media-type=\"application/xhtml+xml\"/>\n", i+1, c.name)
	}
	for i, s := range b.styles {
		fmt.Fprintf(&opf, "<item id=\"style%d\" href=\"styles/%s\" media-type=\"text/css\"/>\n", i+1, s.name)
	}
	for i, im := range b.images {
		id := fmt.Sprintf("image%d", i+1)
		if im.name == b.cover {
			id = "cover-image"
		}
		fmt.Fprintf(&opf, "<item id=\"%s\" href=\"images/%s\" media-type=\"%s\"/>\n", id, im.name, im.mime)
	}
	for i, f := range b.fonts {
		fmt.Fprintf(&opf, "<item id=\"font%d\" href=\"fonts/%s\" media-type=\"%s\"/>\n", i+1, f.name, f.mime)
	}
	opf.WriteString("</manifest>\n<spine toc=\"ncx\">\n")
	for i := range b.chapters {
		fmt.Fprintf(&opf, "<itemref idref=\"text%d\"/>\n", i+1)
	}
	opf.WriteString("</spine>\n</package>\n")
	if err := add("OEBPS/content.opf", opf.Bytes()); err != nil {
		return err
	}

	var ncx bytes.Buffer
	fmt.Fprintf(&ncx, `<?xml version="1.0" encoding="utf-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
<head><meta name="dtb:uid" content="mediarium-converted"/><meta name="dtb:depth" content="1"/></head>
<docTitle><text>%s</text></docTitle>
<navMap>
`, esc(title))
	n := 0
	for _, c := range b.chapters {
		if c.title == "" {
			continue
		}
		n++
		fmt.Fprintf(&ncx, "<navPoint id=\"nav%d\" playOrder=\"%d\"><navLabel><text>%s</text></navLabel><content src=\"text/%s\"/></navPoint>\n", n, n, esc(c.title), c.name)
	}
	if n == 0 && len(b.chapters) > 0 {
		fmt.Fprintf(&ncx, "<navPoint id=\"nav1\" playOrder=\"1\"><navLabel><text>%s</text></navLabel><content src=\"text/%s\"/></navPoint>\n", esc(title), b.chapters[0].name)
	}
	ncx.WriteString("</navMap>\n</ncx>\n")
	if err := add("OEBPS/toc.ncx", ncx.Bytes()); err != nil {
		return err
	}
	for _, c := range b.chapters {
		if err := add("OEBPS/text/"+c.name, c.data); err != nil {
			return err
		}
	}
	for _, s := range b.styles {
		if err := add("OEBPS/styles/"+s.name, s.data); err != nil {
			return err
		}
	}
	for _, im := range b.images {
		if err := add("OEBPS/images/"+im.name, im.data); err != nil {
			return err
		}
	}
	for _, f := range b.fonts {
		if err := add("OEBPS/fonts/"+f.name, f.data); err != nil {
			return err
		}
	}
	return z.Close()
}
