package indexers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// search runs a keyword search ("" lists the newest releases).
func (s *cgSession) search(ctx context.Context, query string, cats []int) ([]Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLogin(ctx); err != nil {
		return nil, err
	}

	vars := s.vars()
	vars.Query["Q"] = query
	vars.Query["Keywords"] = query
	kw, err := applyFilters(query, s.def.Search.KeywordsFilters, vars)
	if err != nil {
		return nil, fmt.Errorf("%s: keywords: %w", s.name, err)
	}
	vars.Keywords = strings.TrimSpace(kw)
	siteCats := s.def.siteCategories(cats)
	vars.Categories = siteCats

	var (
		all      []Result
		seen     = map[string]bool{}
		firstErr error
		rows     int
	)
	for _, p := range s.def.Search.Paths {
		if !pathWanted(p, siteCats) {
			continue
		}
		results, nRows, rowErr, err := s.searchPath(ctx, p, vars, query, false)
		if err != nil {
			return nil, err
		}
		rows += nRows
		if firstErr == nil && rowErr != nil {
			firstErr = rowErr
		}
		for _, r := range results {
			key := r.DownloadURL + "\x00" + r.InfoURL + "\x00" + r.Title
			if seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, r)
		}
	}
	if len(all) == 0 && firstErr != nil && rows > 0 {
		return nil, fmt.Errorf("%s: could not read the results: %w", s.name, firstErr)
	}
	return filterByCategory(all, cats), nil
}

// searchPath requests one search path and parses its rows. It returns the
// results, how many rows the page had, the first row-level error (rows
// that fail are skipped) and any request-level error.
func (s *cgSession) searchPath(ctx context.Context, p SearchPath, vars *tmplVars, query string, retried bool) ([]Result, int, error, error) {
	sb := s.def.Search
	pathStr, err := applyTemplate(p.Path, vars, urlPathEscape)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%s: search path: %w", s.name, err)
	}
	u, err := s.resolvePath(pathStr)
	if err != nil {
		return nil, 0, nil, err
	}

	inputs := OrderedMap[string]{Values: map[string]string{}}
	merge := func(m OrderedMap[string]) {
		for _, k := range m.Keys {
			if _, ok := inputs.Values[k]; !ok {
				inputs.Keys = append(inputs.Keys, k)
			}
			inputs.Values[k] = m.Values[k]
		}
	}
	if p.InheritInputs == nil || *p.InheritInputs {
		merge(sb.Inputs)
	}
	merge(p.Inputs)
	var (
		keys []string
		vals = map[string]string{}
		raw  string
	)
	for _, k := range inputs.Keys {
		v, err := applyTemplate(inputs.Values[k], vars, nil)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("%s: search input %s: %w", s.name, k, err)
		}
		if k == "$raw" {
			raw += v
			continue
		}
		if v == "" && !sb.AllowEmptyInputs {
			continue
		}
		keys = append(keys, k)
		vals[k] = v
	}
	headers, err := templatedHeaders(sb.Headers, vars)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%s: search headers: %w", s.name, err)
	}
	req := cgRequest{url: u, headers: headers, noFollow: !(s.def.FollowRedirect || p.FollowRedirect)}
	method, err := applyTemplate(p.Method, vars, nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%s: search method: %w", s.name, err)
	}
	if strings.EqualFold(strings.TrimSpace(method), "post") {
		req.method = http.MethodPost
		req.form = s.encodeForm(keys, vals, raw)
	} else {
		req.url = appendQuery(u, s.encodeForm(keys, vals, raw))
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, 0, nil, err
	}
	for hops := 0; resp.location != "" && hops < 5; hops++ {
		if s.def.Login != nil && !retried {
			// redirected away from the results: the session has expired
			if err := s.relogin(ctx); err != nil {
				return nil, 0, nil, err
			}
			return s.searchPath(ctx, p, vars, query, true)
		}
		if strings.HasPrefix(resp.location, "magnet:") {
			break
		}
		if resp, err = s.do(ctx, cgRequest{url: resp.location, headers: headers, noFollow: true}); err != nil {
			return nil, 0, nil, err
		}
	}
	if resp.status >= 400 {
		return nil, 0, nil, fmt.Errorf("%s: the site returned HTTP %d for a search", s.name, resp.status)
	}

	body := string(resp.body)
	if len(sb.PreprocessingFilters) > 0 {
		if body, err = applyFilters(body, sb.PreprocessingFilters, vars); err != nil {
			return nil, 0, nil, fmt.Errorf("%s: preprocessing: %w", s.name, err)
		}
	}
	if p.Response != nil && p.Response.NoResultsMessage != "" &&
		(strings.TrimSpace(body) == p.Response.NoResultsMessage || strings.Contains(body, p.Response.NoResultsMessage)) {
		return nil, 0, nil, nil
	}

	if p.Response != nil && strings.EqualFold(p.Response.Type, "json") {
		return s.parseJSONRows([]byte(body), vars, resp.url, query)
	}

	var doc *html.Node
	if p.Response != nil && strings.EqualFold(p.Response.Type, "xml") {
		doc, err = parseXMLDoc([]byte(body))
	} else {
		doc, err = parseHTML([]byte(body))
	}
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%s: read results page: %w", s.name, err)
	}
	isXML := p.Response != nil && strings.EqualFold(p.Response.Type, "xml")
	if l := s.def.Login; l != nil && l.Test != nil && l.Test.Selector != "" && !retried && !isXML {
		if ok, err := s.pageMatches(doc, l.Test.Selector, vars); err == nil && !ok {
			if err := s.relogin(ctx); err != nil {
				return nil, 0, nil, err
			}
			return s.searchPath(ctx, p, vars, query, true)
		}
	}
	if msg, found, err := s.findError(sb.Error, &cgResponse{body: []byte(body), url: resp.url}, vars); err != nil {
		return nil, 0, nil, err
	} else if found {
		return nil, 0, nil, fmt.Errorf("%s: %s", s.name, msg)
	}
	return s.parseHTMLRows(doc, vars, resp.url, query)
}

func pathWanted(p SearchPath, siteCats []string) bool {
	if len(p.Categories) == 0 {
		return true
	}
	cats := p.Categories
	invert := cats[0] == "!"
	if invert {
		cats = cats[1:]
	}
	hit := false
	for _, c := range cats {
		for _, sc := range siteCats {
			if c == sc {
				hit = true
			}
		}
	}
	return hit != invert
}

// --- rows ---

func (s *cgSession) parseHTMLRows(doc *html.Node, vars *tmplVars, page *url.URL, query string) ([]Result, int, error, error) {
	rb := s.def.Search.Rows
	selStr, err := applyTemplate(rb.Selector, vars, nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%s: rows selector: %w", s.name, err)
	}
	sel, err := CompileSelector(selStr)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%s: rows selector: %w", s.name, err)
	}
	rows := sel.All(doc)
	if rb.After > 0 {
		var merged []*html.Node
		for i := 0; i < len(rows); i += 1 + rb.After {
			row := rows[i]
			for j := 1; j <= rb.After && i+j < len(rows); j++ {
				for c := rows[i+j].FirstChild; c != nil; c = c.NextSibling {
					row.AppendChild(cloneNode(c))
				}
			}
			merged = append(merged, row)
		}
		rows = merged
	}
	var removeSel *Selector
	if rb.Remove != "" {
		rs, err := applyTemplate(rb.Remove, vars, nil)
		if err != nil {
			return nil, 0, nil, err
		}
		if removeSel, err = CompileSelector(rs); err != nil {
			return nil, 0, nil, fmt.Errorf("%s: rows remove: %w", s.name, err)
		}
	}

	var (
		out      []Result
		firstErr error
	)
	for _, row := range rows {
		if removeSel != nil {
			removeMatches(row, removeSel)
		}
		res, err := s.parseRow(htmlRow{n: row}, vars, page, query)
		if err == nil && res != nil && rb.DateHeaders != nil {
			if t, ok := s.dateHeader(row, *rb.DateHeaders, vars); ok {
				res.PublishDate = t
			}
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if res != nil {
			out = append(out, *res)
		}
	}
	return out, len(rows), firstErr, nil
}

// dateHeader finds the date of a row from the nearest header row above it
// (sites that group results under "Today", "Yesterday"...).
func (s *cgSession) dateHeader(row *html.Node, f SelectorField, vars *tmplVars) (time.Time, bool) {
	for p := prevElement(row); p != nil; p = prevElement(p) {
		v, found, err := htmlRow{n: p}.value(f, vars)
		if err != nil || !found || strings.TrimSpace(v) == "" {
			continue
		}
		if t, err := fromUnknownDate(v, time.Now()); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (s *cgSession) parseJSONRows(body []byte, vars *tmplVars, page *url.URL, query string) ([]Result, int, error, error) {
	rb := s.def.Search.Rows
	root, err := decodeJSON(body)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%s: the site did not return JSON: %w", s.name, err)
	}
	selStr, err := applyTemplate(rb.Selector, vars, nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%s: rows selector: %w", s.name, err)
	}
	v, ok, err := jsonQuery(root, strings.TrimSpace(selStr))
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%s: rows selector: %w", s.name, err)
	}
	if !ok {
		if rb.MissingAttributeEqualsNoResults {
			return nil, 0, nil, nil
		}
		return nil, 0, nil, fmt.Errorf("%s: unexpected reply from the site (no %q in it): %s", s.name, selStr, truncate(string(body), 200))
	}
	var rows []any
	switch x := v.(type) {
	case []any:
		rows = x
	case nil:
	default:
		rows = []any{x}
	}
	var (
		out      []Result
		firstErr error
	)
	for _, row := range rows {
		items := []any{row}
		if rb.Attribute != "" {
			sub, ok := jsonSelect(row, rb.Attribute)
			if !ok || sub == nil {
				continue
			}
			items = []any{sub}
			if arr, isArr := sub.([]any); isArr && rb.Multiple {
				items = arr
			}
		}
		for _, item := range items {
			res, err := s.parseRow(jsonRow{item: item, parent: row}, vars, page, query)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if res != nil {
				out = append(out, *res)
			}
		}
	}
	return out, len(rows), firstErr, nil
}

// --- fields ---

// rowSource reads a field's value from one result row.
type rowSource interface {
	value(f SelectorField, vars *tmplVars) (string, bool, error)
}

// alwaysOptional fields never fail a row when missing.
var alwaysOptional = map[string]bool{
	"imdb": true, "imdbid": true, "tmdbid": true, "rageid": true, "tvdbid": true, "tvmazeid": true,
	"traktid": true, "doubanid": true, "poster": true, "banner": true, "description": true, "genre": true,
}

type releaseBuilder struct {
	title, link, magnet, details, infohash string
	size, seeders, leechers                int64
	haveSeeders, haveLeechers              bool
	date                                   time.Time
	cats                                   []int
}

func (s *cgSession) parseRow(src rowSource, vars *tmplVars, page *url.URL, query string) (*Result, error) {
	fields := s.def.Search.Fields
	vars.Result = map[string]string{}
	rb := &releaseBuilder{}
	for _, key := range fields.Keys {
		f := fields.Values[key]
		parts := strings.Split(key, "|")
		name, mods := parts[0], parts[1:]
		optional := f.Optional || alwaysOptional[name] || contains(mods, "optional")

		val, found, err := src.value(f, vars)
		if err == nil && (!found || strings.TrimSpace(val) == "") && f.Default != nil {
			if val, err = applyTemplate(*f.Default, vars, nil); err == nil {
				found = true
			}
		}
		if err == nil && !found {
			err = fmt.Errorf("selector %q matched nothing", f.Selector)
		}
		if err == nil && optional && strings.TrimSpace(val) == "" {
			vars.Result[name] = ""
			continue
		}
		if err == nil {
			val, err = s.applyField(rb, name, val, mods, page)
		}
		if err != nil {
			if optional {
				vars.Result[name] = ""
				continue
			}
			return nil, fmt.Errorf("field %s: %w", key, err)
		}
		vars.Result[name] = val
	}

	for _, f := range s.def.Search.Rows.Filters {
		if f.Name == "andmatch" && !andMatch(rb.title, query, filterArgs(f.Args)) {
			return nil, nil
		}
	}
	return s.finish(rb)
}

// applyField records a field's meaning on the release and returns the
// value later templates see as .Result.<name>.
func (s *cgSession) applyField(rb *releaseBuilder, name, val string, mods []string, page *url.URL) (string, error) {
	switch name {
	case "title":
		if contains(mods, "append") {
			rb.title += val
		} else {
			rb.title = val
		}
		return rb.title, nil
	case "download":
		if strings.TrimSpace(val) == "" {
			rb.link = ""
			return "", nil
		}
		if strings.HasPrefix(val, "magnet:") {
			rb.magnet = val
			return val, nil
		}
		u, err := s.resolve(val, page)
		if err != nil {
			return "", err
		}
		rb.link = u
		return u, nil
	case "magnet":
		rb.magnet = strings.TrimSpace(val)
		return rb.magnet, nil
	case "details", "comments":
		u, err := s.resolve(val, page)
		if err != nil {
			return "", err
		}
		if name == "details" || rb.details == "" {
			rb.details = u
		}
		return u, nil
	case "infohash":
		rb.infohash = strings.TrimSpace(val)
		return rb.infohash, nil
	case "size":
		rb.size = parseSize(val)
		return strconv.FormatInt(rb.size, 10), nil
	case "seeders":
		rb.seeders, rb.haveSeeders = sane(parseCount(val)), true
		return strconv.FormatInt(rb.seeders, 10), nil
	case "leechers":
		rb.leechers, rb.haveLeechers = sane(parseCount(val)), true
		return strconv.FormatInt(rb.leechers, 10), nil
	case "grabs", "files":
		return strconv.FormatInt(parseCount(val), 10), nil
	case "date":
		t, err := fromUnknownDate(val, time.Now())
		if err != nil {
			return val, nil // an unreadable date shouldn't cost the release
		}
		rb.date = t
		return t.Format(time.RFC1123Z), nil
	case "category":
		rb.cats = appendUnique(rb.cats, s.def.newznabForSite(val)...)
		return val, nil
	case "categorydesc":
		rb.cats = appendUnique(rb.cats, s.def.newznabForDesc(val)...)
		return val, nil
	}
	return val, nil
}

func sane(n int64) int64 {
	if n < 0 || n >= 5000000 {
		return 0
	}
	return n
}

var publicTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.demonii.com:1337/announce",
}

var btihRe = regexp.MustCompile(`(?i)xt=urn:btih:([a-z0-9]+)`)

func (s *cgSession) finish(rb *releaseBuilder) (*Result, error) {
	title := strings.Join(strings.Fields(rb.title), " ")
	if title == "" {
		return nil, fmt.Errorf("empty title")
	}
	link, magnet := rb.link, rb.magnet
	if link == "" && magnet == "" && rb.infohash != "" {
		magnet = buildMagnet(rb.infohash, title)
	}
	if link == "" && magnet == "" && rb.details != "" && s.def.Download != nil {
		link = rb.details // the download rules find the file on the details page
	}
	if link == "" && magnet == "" {
		return nil, fmt.Errorf("no download link")
	}
	res := &Result{
		Title:       title,
		IndexerName: s.name,
		Protocol:    s.def.DefinitionProtocol(),
		InfoURL:     rb.details,
		SizeBytes:   rb.size,
		PublishDate: rb.date,
		Categories:  rb.cats,
		Seeders:     int(rb.seeders),
		Peers:       int(rb.seeders + rb.leechers),
		InfoHash:    strings.ToLower(rb.infohash),
	}
	if link != "" {
		res.DownloadURL = EncodeLinkRef(s.id, link)
	} else {
		res.DownloadURL = magnet
	}
	if res.InfoHash == "" && magnet != "" {
		if m := btihRe.FindStringSubmatch(magnet); m != nil {
			res.InfoHash = strings.ToLower(m[1])
		}
	}
	return res, nil
}

func buildMagnet(hash, title string) string {
	var sb strings.Builder
	sb.WriteString("magnet:?xt=urn:btih:")
	sb.WriteString(strings.TrimSpace(hash))
	if title != "" {
		sb.WriteString("&dn=")
		sb.WriteString(url.QueryEscape(title))
	}
	for _, tr := range publicTrackers {
		sb.WriteString("&tr=")
		sb.WriteString(url.QueryEscape(tr))
	}
	return sb.String()
}

var nonWordRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// andMatch keeps a row only if its title contains every word of the query.
func andMatch(title, query string, args []string) bool {
	q := query
	if n, err := strconv.Atoi(strings.TrimSpace(argAt(args, 0))); err == nil && n > 0 && len(q) > n {
		q = q[:n]
	}
	t := strings.ToLower(title)
	for _, w := range strings.Fields(nonWordRe.ReplaceAllString(strings.ToLower(q), " ")) {
		if !strings.Contains(t, w) {
			return false
		}
	}
	return true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// --- HTML rows ---

type htmlRow struct{ n *html.Node }

// value reads one field from an HTML element, as Cardigann does: the
// selector may match the element itself or a descendant; then remove,
// case, attribute (or text) and filters.
func (r htmlRow) value(f SelectorField, vars *tmplVars) (string, bool, error) {
	if f.Text != nil {
		v, err := applyTemplate(*f.Text, vars, nil)
		if err != nil {
			return "", false, err
		}
		v, err = applyFilters(v, f.Filters, vars)
		return v, err == nil, err
	}
	selection := r.n
	if selection != nil && selection.Type == html.DocumentNode && f.Selector == "" {
		return "", false, nil
	}
	if f.Selector != "" {
		selStr, err := applyTemplate(f.Selector, vars, nil)
		if err != nil {
			return "", false, err
		}
		sel, err := CompileSelector(selStr)
		if err != nil {
			return "", false, err
		}
		if !sel.Matches(r.n) {
			selection = sel.First(r.n)
		}
		if selection == nil {
			return "", false, nil
		}
	}
	if f.Remove != "" {
		rs, err := applyTemplate(f.Remove, vars, nil)
		if err != nil {
			return "", false, err
		}
		rsel, err := CompileSelector(rs)
		if err != nil {
			return "", false, err
		}
		selection = cloneNode(selection)
		removeMatches(selection, rsel)
	}
	var val string
	switch {
	case f.Case.Len() > 0:
		matched := false
		for _, k := range f.Case.Keys {
			ks, err := applyTemplate(k, vars, nil)
			if err != nil {
				return "", false, err
			}
			cs, err := CompileSelector(ks)
			if err != nil {
				return "", false, err
			}
			if cs.Matches(selection) || cs.First(selection) != nil {
				if val, err = applyTemplate(f.Case.Values[k], vars, nil); err != nil {
					return "", false, err
				}
				matched = true
				break
			}
		}
		if !matched {
			return "", false, nil
		}
	case f.Attribute != "":
		v, ok := attr(selection, f.Attribute)
		if !ok {
			return "", false, nil
		}
		val = v
	default:
		val = textContent(selection)
	}
	val, err := applyFilters(strings.TrimSpace(val), f.Filters, vars)
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

func removeMatches(root *html.Node, sel *Selector) {
	for _, n := range sel.All(root) {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
	}
}

func cloneNode(n *html.Node) *html.Node {
	c := &html.Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace}
	c.Attr = append([]html.Attribute(nil), n.Attr...)
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.AppendChild(cloneNode(ch))
	}
	return c
}

// --- JSON rows ---

type jsonRow struct{ item, parent any }

func (r jsonRow) value(f SelectorField, vars *tmplVars) (string, bool, error) {
	if f.Text != nil {
		v, err := applyTemplate(*f.Text, vars, nil)
		if err != nil {
			return "", false, err
		}
		v, err = applyFilters(v, f.Filters, vars)
		return v, err == nil, err
	}
	selStr, err := applyTemplate(f.Selector, vars, nil)
	if err != nil {
		return "", false, err
	}
	selStr = strings.TrimSpace(selStr)
	target := r.item
	useParent := strings.HasPrefix(selStr, "..")
	if useParent {
		target = r.parent
	}
	selStr = strings.TrimLeft(selStr, ".")
	v, ok, err := jsonQuery(target, selStr)
	if err != nil {
		return "", false, err
	}
	if !ok && !useParent && r.parent != nil {
		v, ok, err = jsonQuery(r.parent, selStr)
		if err != nil {
			return "", false, err
		}
	}
	if !ok || v == nil {
		return "", false, nil
	}
	val := jsonScalar(v)
	if f.Case.Len() > 0 {
		matched := false
		for _, k := range f.Case.Keys {
			if val == k || k == "*" {
				if val, err = applyTemplate(f.Case.Values[k], vars, nil); err != nil {
					return "", false, err
				}
				matched = true
				break
			}
		}
		if !matched {
			return "", false, nil
		}
	}
	val, err = applyFilters(val, f.Filters, vars)
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}
