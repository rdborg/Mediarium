package indexers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Result links from definition-based indexers usually need the indexer's
// session (login cookies, Cloudflare clearance) and sometimes a visit to the
// details page to find the real file, so they are handed out as references
// the grab pipeline resolves through the manager at download time:
//
//	mediarium-indexer://<indexer id>?link=<escaped site link>
//
// Magnet links are handed out as they are.

const linkRefScheme = "mediarium-indexer"

// EncodeLinkRef wraps a site link as a reference to indexer id.
func EncodeLinkRef(id int64, link string) string {
	return linkRefScheme + "://" + strconv.FormatInt(id, 10) + "?link=" + url.QueryEscape(link)
}

// IsLinkRef reports whether s came from EncodeLinkRef.
func IsLinkRef(s string) bool { return strings.HasPrefix(s, linkRefScheme+"://") }

// DecodeLinkRef splits a reference into the indexer id and the site link.
func DecodeLinkRef(s string) (int64, string, bool) {
	if !IsLinkRef(s) {
		return 0, "", false
	}
	u, err := url.Parse(s)
	if err != nil {
		return 0, "", false
	}
	id, err := strconv.ParseInt(u.Host, 10, 64)
	link := u.Query().Get("link")
	if err != nil || link == "" {
		return 0, "", false
	}
	lu, err := url.Parse(link)
	if err != nil || (lu.Scheme != "http" && lu.Scheme != "https" && lu.Scheme != "magnet") {
		return 0, "", false
	}
	if lu.Scheme != "magnet" && lu.Host == "" {
		return 0, "", false // "https:0" is not an address
	}
	if lu.Scheme == "magnet" && !strings.HasPrefix(link, "magnet:") {
		return 0, "", false // the rest of the code looks for exactly "magnet:"
	}
	return id, link, true
}

var errNotAFile = errors.New("the site returned a web page instead of the file")

func (s *cgSession) download(ctx context.Context, link string) (*Download, error) {
	if strings.HasPrefix(link, "magnet:") {
		return &Download{Magnet: link}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLogin(ctx); err != nil {
		return nil, err
	}
	d, err := s.downloadOnce(ctx, link)
	if errors.Is(err, errNotAFile) && s.def.Login != nil {
		// most likely signed out: sign in again and retry once
		if lerr := s.relogin(ctx); lerr != nil {
			return nil, lerr
		}
		d, err = s.downloadOnce(ctx, link)
	}
	if err != nil {
		if errors.Is(err, errNotAFile) {
			return nil, fmt.Errorf("%s: %w (the login may have expired, or the release was removed)", s.name, err)
		}
		return nil, err
	}
	return d, nil
}

func (s *cgSession) downloadOnce(ctx context.Context, link string) (*Download, error) {
	vars := s.vars()
	vars.DownloadURI = downloadURIVars(link)
	dl := s.def.Download
	hdrSrc := s.def.Search.Headers
	if dl != nil && len(dl.Headers) > 0 {
		hdrSrc = dl.Headers
	}
	headers, err := templatedHeaders(hdrSrc, vars)
	if err != nil {
		return nil, fmt.Errorf("%s: download headers: %w", s.name, err)
	}
	if dl == nil {
		return s.fetchFile(ctx, link, headers)
	}

	var before *cgResponse
	if b := dl.Before; b != nil {
		path := ""
		if b.PathSelector != nil {
			page, err := s.fetchPage(ctx, link, headers)
			if err != nil {
				return nil, err
			}
			doc, err := parseHTML(page.body)
			if err != nil {
				return nil, err
			}
			v, found, err := htmlRow{n: doc}.value(*b.PathSelector, vars)
			if err != nil || !found {
				return nil, fmt.Errorf("%s: could not find the download step on the page", s.name)
			}
			path = v
		} else if path, err = applyTemplate(b.Path, vars, nil); err != nil {
			return nil, err
		}
		u, err := s.resolvePath(path)
		if err != nil {
			return nil, err
		}
		keys, vals, err := templatedInputs(b.Inputs, vars)
		if err != nil {
			return nil, err
		}
		req := cgRequest{url: u, headers: headers}
		if strings.EqualFold(b.Method, "post") {
			req.method, req.form = http.MethodPost, s.encodeForm(keys, vals, "")
		} else {
			req.url = appendQuery(u, s.encodeForm(keys, vals, ""))
		}
		if before, err = s.do(ctx, req); err != nil {
			return nil, err
		}
	}

	var page *cgResponse
	pageFor := func(useBefore bool) (*cgResponse, error) {
		if useBefore && before != nil {
			return before, nil
		}
		if page == nil {
			p, err := s.fetchPage(ctx, link, headers)
			if err != nil {
				return nil, err
			}
			page = p
		}
		return page, nil
	}

	if ih := dl.InfoHash; ih != nil {
		pg, err := pageFor(ih.UseBeforeResponse)
		if err != nil {
			return nil, err
		}
		doc, err := parseHTML(pg.body)
		if err != nil {
			return nil, err
		}
		hash, found, err := htmlRow{n: doc}.value(ih.Hash, vars)
		if err != nil || !found || strings.TrimSpace(hash) == "" {
			return nil, fmt.Errorf("%s: no info hash found on the release page", s.name)
		}
		title, _, _ := htmlRow{n: doc}.value(ih.Title, vars)
		return &Download{Magnet: buildMagnet(hash, title)}, nil
	}

	if len(dl.Selectors) > 0 {
		var lastErr error
		for _, ds := range dl.Selectors {
			pg, err := pageFor(ds.UseBeforeResponse)
			if err != nil {
				return nil, err
			}
			doc, err := parseHTML(pg.body)
			if err != nil {
				return nil, err
			}
			selStr, err := applyTemplate(ds.Selector, vars, nil)
			if err != nil {
				return nil, err
			}
			sel, err := CompileSelector(selStr)
			if err != nil {
				return nil, fmt.Errorf("%s: download selector: %w", s.name, err)
			}
			el := sel.First(doc)
			if el == nil {
				continue
			}
			var v string
			if ds.Attribute != "" {
				v, _ = attr(el, ds.Attribute)
			} else {
				v = textContent(el)
			}
			if v, err = applyFilters(strings.TrimSpace(v), ds.Filters, vars); err != nil || v == "" {
				continue
			}
			target, err := s.resolve(v, pg.url)
			if err != nil {
				continue
			}
			if strings.HasPrefix(target, "magnet:") {
				return &Download{Magnet: target}, nil
			}
			d, err := s.fetchFile(ctx, target, headers)
			if err == nil {
				return d, nil
			}
			lastErr = err
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("%s: no download link found on the release page", s.name)
	}
	if before != nil && dl.Before != nil && len(dl.Selectors) == 0 {
		// the "before" request may itself have produced the file
		if d, ok := s.asFile(before); ok {
			return d, nil
		}
	}
	return s.fetchFile(ctx, link, headers)
}

func (s *cgSession) fetchPage(ctx context.Context, link string, headers map[string]string) (*cgResponse, error) {
	resp, err := s.do(ctx, cgRequest{url: link, headers: headers})
	if err != nil {
		return nil, err
	}
	if resp.status >= 400 {
		return nil, fmt.Errorf("%s: the release page returned HTTP %d", s.name, resp.status)
	}
	return resp, nil
}

func (s *cgSession) fetchFile(ctx context.Context, link string, headers map[string]string) (*Download, error) {
	if strings.HasPrefix(link, "magnet:") {
		return &Download{Magnet: link}, nil
	}
	resp, err := s.do(ctx, cgRequest{url: link, headers: headers, binary: true})
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(resp.location, "magnet:") {
		return &Download{Magnet: resp.location}, nil
	}
	if resp.status >= 400 {
		return nil, fmt.Errorf("%s: the download returned HTTP %d", s.name, resp.status)
	}
	if d, ok := s.asFile(resp); ok {
		return d, nil
	}
	return nil, errNotAFile
}

// asFile accepts a response that is a .torrent (bencoded) or an NZB.
func (s *cgSession) asFile(resp *cgResponse) (*Download, bool) {
	data := resp.raw
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, false
	}
	ct := strings.ToLower(resp.header.Get("Content-Type"))
	if s.def.DefinitionProtocol() == ProtocolUsenet {
		if bytes.Contains(trimmed[:min(len(trimmed), 1024)], []byte("<nzb")) || strings.Contains(ct, "nzb") {
			return &Download{Data: data}, true
		}
		return nil, false
	}
	if trimmed[0] == 'd' && bytes.Contains(data, []byte("4:info")) || strings.Contains(ct, "bittorrent") {
		return &Download{Data: data}, true
	}
	return nil, false
}

func downloadURIVars(link string) map[string]any {
	u, err := url.Parse(link)
	if err != nil {
		return map[string]any{"AbsoluteUri": link, "Query": map[string]string{}}
	}
	q := map[string]string{}
	for k, v := range u.Query() {
		if len(v) > 0 {
			q[k] = v[0]
		}
	}
	return map[string]any{
		"AbsoluteUri":  u.String(),
		"AbsolutePath": u.EscapedPath(),
		"PathAndQuery": u.RequestURI(),
		"Host":         u.Host,
		"Scheme":       u.Scheme,
		"Query":        q,
	}
}
