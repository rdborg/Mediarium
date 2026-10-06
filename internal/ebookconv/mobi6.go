package ebookconv

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Older MOBI files (MOBI 6): the text is one HTML document. Links point at
// byte positions ("filepos"), images at resource records ("recindex"), and
// <mbp:pagebreak/> separates the chapters.

var (
	pagebreak    = regexp.MustCompile(`(?i)<mbp:pagebreak\s*/?\s*>`)
	fileposAttr  = regexp.MustCompile(`(?i)\bfilepos\s*=\s*["']?0*(\d+)["']?`)
	recindexAttr = regexp.MustCompile(`(?i)\brecindex\s*=\s*["']?0*(\d+)["']?`)
	hiloRecindex = regexp.MustCompile(`(?i)\b(?:hi|lo)recindex\s*=\s*["']?\d+["']?`)
)

// insideTag reports whether position pos of b is inside a tag (after a '<'
// that hasn't been closed yet).
func insideTag(b []byte, pos int) bool {
	lt := bytes.LastIndexByte(b[:pos], '<')
	gt := bytes.LastIndexByte(b[:pos], '>')
	return lt > gt
}

// resources gathers the image records a book refers to, by number counted
// from the first resource record (1 = the first).
type resources struct {
	p     *pdb
	first int
	byNum map[int]*resource
	order []int
}

func newResources(p *pdb, first int) *resources {
	return &resources{p: p, first: first, byNum: map[int]*resource{}}
}

// image returns the image for resource number n, or nil.
func (r *resources) image(n int) *resource {
	if res, ok := r.byNum[n]; ok {
		return res
	}
	if r.first < 0 || n < 1 {
		return nil
	}
	rec := r.p.record(r.first + n - 1)
	ext, mime := sniffImage(rec)
	if ext == "" {
		r.byNum[n] = nil
		return nil
	}
	res := &resource{name: fmt.Sprintf("image%04d.%s", n, ext), mime: mime, data: rec}
	r.byNum[n] = res
	r.order = append(r.order, n)
	return res
}

func (r *resources) list() []resource {
	sort.Ints(r.order)
	out := make([]resource, 0, len(r.order))
	for _, n := range r.order {
		if res := r.byNum[n]; res != nil {
			out = append(out, *res)
		}
	}
	return out
}

func convertMobi6(p *pdb, h *header, meta Metadata) (*epubBook, error) {
	text, err := h.text(p)
	if err != nil {
		return nil, err
	}
	// Anchors where links point, from the end so earlier positions stay put.
	targets := map[int]bool{}
	for _, m := range fileposAttr.FindAllSubmatch(text, -1) {
		if n, err := strconv.Atoi(string(m[1])); err == nil && n >= 0 && n <= len(text) {
			targets[n] = true
		}
	}
	positions := make([]int, 0, len(targets))
	for n := range targets {
		positions = append(positions, n)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(positions)))
	for _, pos := range positions {
		at := pos
		if insideTag(text, at) {
			if gt := bytes.IndexByte(text[at:], '>'); gt >= 0 {
				at += gt + 1
			}
		}
		anchor := []byte(fmt.Sprintf(`<a id="filepos%d"></a>`, pos))
		text = append(text[:at], append(anchor, text[at:]...)...)
	}
	text = fileposAttr.ReplaceAll(text, []byte(`href="#filepos$1"`))

	res := newResources(p, firstResource(p, h))
	text = hiloRecindex.ReplaceAll(text, nil)
	text = recindexAttr.ReplaceAllFunc(text, func(m []byte) []byte {
		n, _ := strconv.Atoi(string(recindexAttr.FindSubmatch(m)[1]))
		if im := res.image(n); im != nil {
			return []byte(`src="../images/` + im.name + `"`)
		}
		return nil
	})
	text = toUTF8(text, h.encoding)

	book := &epubBook{meta: meta}
	if c := h.exthInt(201); c >= 0 {
		if im := res.image(c + 1); im != nil {
			book.cover = im.name
		}
	}
	var docs []*html.Node
	for _, chunk := range pagebreak.Split(string(text), -1) {
		if strings.TrimSpace(stripTags(chunk)) == "" && !strings.Contains(strings.ToLower(chunk), "<img") {
			continue
		}
		doc, err := html.Parse(strings.NewReader(chunk))
		if err != nil {
			continue
		}
		docs = append(docs, doc)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("the book has no text")
	}
	fileOf := map[string]string{}
	names := make([]string, len(docs))
	for i, d := range docs {
		names[i] = fmt.Sprintf("part%04d.xhtml", i+1)
		for _, id := range ids(d) {
			if _, ok := fileOf[id]; !ok {
				fileOf[id] = names[i]
			}
		}
	}
	for i, d := range docs {
		var buf bytes.Buffer
		self := names[i]
		writeXHTML(&buf, d, meta.Title, func(el, attr, val string) (string, bool) {
			if attr == "href" && strings.HasPrefix(val, "#") {
				if f, ok := fileOf[val[1:]]; ok && f != self {
					return f + val, true
				}
			}
			return val, true
		})
		book.chapters = append(book.chapters, chapter{name: self, title: headingText(d), data: buf.Bytes()})
	}
	book.images = res.list()
	return book, nil
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return tagPattern.ReplaceAllString(s, "") }

// firstResource finds the book's first resource record. In a "both" file
// the KF8 header's number may count from the KF8 half or from the start of
// the file; the one that holds a resource wins.
func firstResource(p *pdb, h *header) int {
	if h.resBase >= 0 {
		return h.resBase
	}
	if h.firstImage < 0 {
		return -1
	}
	for _, c := range []int{h.start + h.firstImage, h.firstImage} {
		rec := p.record(c)
		if rec == nil {
			continue
		}
		if ext, _ := sniffImage(rec); ext != "" {
			return c
		}
		if len(rec) >= 4 {
			switch string(rec[:4]) {
			case "FONT", "RESC", "FLIS", "FCIS", "SRCS", "DATP", "CMET", "PAGE", "CONT", "CRES":
				return c
			}
		}
	}
	return h.start + h.firstImage
}
