package indexers

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// ensureLogin signs in once per session when the definition has a login
// block. Callers hold s.mu.
func (s *cgSession) ensureLogin(ctx context.Context) error {
	if s.def.Login == nil || s.loggedIn {
		return nil
	}
	if err := s.login(ctx); err != nil {
		return err
	}
	s.loggedIn = true
	log.Printf("cardigann: indexer=%q logged in", s.name)
	return nil
}

// relogin forgets the current login and signs in again.
func (s *cgSession) relogin(ctx context.Context) error {
	s.loggedIn = false
	return s.ensureLogin(ctx)
}

func (s *cgSession) login(ctx context.Context) error {
	l := s.def.Login
	vars := s.vars()
	for _, c := range l.Cookies {
		s.setCookieString(c)
	}
	headers, err := templatedHeaders(l.Headers, vars)
	if err != nil {
		return fmt.Errorf("%s: login headers: %w", s.name, err)
	}

	method := strings.ToLower(l.Method)
	if method == "" {
		method = "post"
	}
	switch method {
	case "cookie":
		cookie := strings.TrimSpace(vars.Config["cookie"])
		if cookie == "" {
			return fmt.Errorf("%s: login failed: enter the cookie from your browser in this indexer's settings", s.name)
		}
		s.setCookieString(cookie)
	case "get":
		path, err := applyTemplate(l.Path, vars, nil)
		if err != nil {
			return fmt.Errorf("%s: login path: %w", s.name, err)
		}
		u, err := s.resolvePath(path)
		if err != nil {
			return err
		}
		keys, vals, err := templatedInputs(l.Inputs, vars)
		if err != nil {
			return fmt.Errorf("%s: login inputs: %w", s.name, err)
		}
		resp, err := s.do(ctx, cgRequest{url: appendQuery(u, s.encodeForm(keys, vals, "")), headers: headers})
		if err != nil {
			return err
		}
		if err := s.checkLoginResponse(resp, vars); err != nil {
			return err
		}
	case "post":
		path, err := applyTemplate(l.Path, vars, nil)
		if err != nil {
			return fmt.Errorf("%s: login path: %w", s.name, err)
		}
		u, err := s.resolvePath(path)
		if err != nil {
			return err
		}
		keys, vals, err := templatedInputs(l.Inputs, vars)
		if err != nil {
			return fmt.Errorf("%s: login inputs: %w", s.name, err)
		}
		if l.SelectorInputs.Len() > 0 || l.Selectors {
			landing, err := s.do(ctx, cgRequest{url: u, headers: headers})
			if err != nil {
				return err
			}
			doc, err := parseHTML(landing.body)
			if err != nil {
				return fmt.Errorf("%s: read login page: %w", s.name, err)
			}
			keys, err = s.addSelectorInputs(doc, l.SelectorInputs, vars, keys, vals)
			if err != nil {
				return err
			}
		}
		resp, err := s.do(ctx, cgRequest{method: http.MethodPost, url: u, form: s.encodeForm(keys, vals, ""), headers: headers})
		if err != nil {
			return err
		}
		if err := s.checkLoginResponse(resp, vars); err != nil {
			return err
		}
	case "form":
		if err := s.formLogin(ctx, vars, headers); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s: login method %q is not supported", s.name, l.Method)
	}
	return s.testLogin(ctx, vars, method == "cookie")
}

func (s *cgSession) formLogin(ctx context.Context, vars *tmplVars, headers map[string]string) error {
	l := s.def.Login
	path, err := applyTemplate(l.Path, vars, nil)
	if err != nil {
		return fmt.Errorf("%s: login path: %w", s.name, err)
	}
	loginURL, err := s.resolvePath(path)
	if err != nil {
		return err
	}
	landing, err := s.do(ctx, cgRequest{url: loginURL, headers: headers})
	if err != nil {
		return err
	}
	if landing.status >= 400 {
		return fmt.Errorf("%s: login page returned HTTP %d", s.name, landing.status)
	}
	doc, err := parseHTML(landing.body)
	if err != nil {
		return fmt.Errorf("%s: read login page: %w", s.name, err)
	}
	if c := l.Captcha; c != nil && c.Selector != "" {
		if sel, err := CompileSelector(c.Selector); err == nil && sel.First(doc) != nil {
			return fmt.Errorf("%s: login failed: the site asks for a CAPTCHA, which Mediarium can't solve. If the site offers a cookie-based setup, use that instead", s.name)
		}
	}
	formSelStr := l.Form
	if formSelStr == "" {
		formSelStr = "form"
	}
	formSelStr, err = applyTemplate(formSelStr, vars, nil)
	if err != nil {
		return err
	}
	formSel, err := CompileSelector(formSelStr)
	if err != nil {
		return fmt.Errorf("%s: login form selector: %w", s.name, err)
	}
	form := formSel.First(doc)
	if form == nil {
		return fmt.Errorf("%s: login failed: no login form (%s) on %s", s.name, formSelStr, loginURL)
	}

	vals := map[string]string{}
	var keys []string
	set := func(k, v string) {
		if _, ok := vals[k]; !ok {
			keys = append(keys, k)
		}
		vals[k] = v
	}
	inputSel, _ := CompileSelector("input, select, textarea")
	for _, in := range inputSel.All(form) {
		name, ok := attr(in, "name")
		if !ok || name == "" {
			continue
		}
		v, _ := attr(in, "value")
		if in.Data == "textarea" {
			v = textContent(in)
		}
		if in.Data == "select" {
			if opt, _ := CompileSelector("option[selected]"); opt != nil {
				if o := opt.First(in); o != nil {
					v, _ = attr(o, "value")
				}
			}
		}
		set(name, v)
	}
	ikeys, ivals, err := templatedInputs(l.Inputs, vars)
	if err != nil {
		return fmt.Errorf("%s: login inputs: %w", s.name, err)
	}
	for _, k := range ikeys {
		set(k, ivals[k])
	}
	keys, err = s.addSelectorInputs(doc, l.SelectorInputs, vars, keys, vals)
	if err != nil {
		return err
	}

	submit := loginURL
	if l.SubmitPath != "" {
		p, err := applyTemplate(l.SubmitPath, vars, nil)
		if err != nil {
			return err
		}
		if submit, err = s.resolvePath(p); err != nil {
			return err
		}
	} else if action, ok := attr(form, "action"); ok && strings.TrimSpace(action) != "" {
		if submit, err = s.resolve(action, landing.url); err != nil {
			return err
		}
		// the form carries the username and password: it may only go to the
		// site itself, whatever the page says
		if err := s.checkSiteAddress(submit); err != nil {
			return err
		}
	}
	if l.GetSelectorInputs.Len() > 0 {
		gvals := map[string]string{}
		gkeys, err := s.addSelectorInputs(doc, l.GetSelectorInputs, vars, nil, gvals)
		if err != nil {
			return err
		}
		submit = appendQuery(submit, s.encodeForm(gkeys, gvals, ""))
	}

	method := http.MethodPost
	if m, ok := attr(form, "method"); ok && strings.EqualFold(strings.TrimSpace(m), "get") {
		method = http.MethodGet
	}
	req := cgRequest{method: method, url: submit, headers: headers}
	if method == http.MethodGet {
		req.url = appendQuery(submit, s.encodeForm(keys, vals, ""))
	} else {
		req.form = s.encodeForm(keys, vals, "")
	}
	resp, err := s.do(ctx, req)
	if err != nil {
		return err
	}
	return s.checkLoginResponse(resp, vars)
}

// addSelectorInputs reads extra form values (CSRF tokens and the like) from
// the login page.
func (s *cgSession) addSelectorInputs(doc *html.Node, inputs OrderedMap[SelectorField], vars *tmplVars, keys []string, vals map[string]string) ([]string, error) {
	root := htmlRow{n: doc}
	for _, k := range inputs.Keys {
		f := inputs.Values[k]
		v, found, err := root.value(f, vars)
		if err != nil {
			return nil, fmt.Errorf("%s: login input %s: %w", s.name, k, err)
		}
		if !found {
			if f.Optional {
				continue
			}
			return nil, fmt.Errorf("%s: login failed: could not find %q on the login page", s.name, k)
		}
		if _, ok := vals[k]; !ok {
			keys = append(keys, k)
		}
		vals[k] = v
	}
	return keys, nil
}

func (s *cgSession) checkLoginResponse(resp *cgResponse, vars *tmplVars) error {
	if resp.status >= 500 {
		return fmt.Errorf("%s: login failed: the site returned HTTP %d", s.name, resp.status)
	}
	if msg, found, err := s.findError(s.def.Login.Error, resp, vars); err != nil {
		return err
	} else if found {
		return fmt.Errorf("%s: login failed: %s", s.name, msg)
	}
	return nil
}

// findError checks a page against a definition's error blocks.
func (s *cgSession) findError(blocks []ErrorBlock, resp *cgResponse, vars *tmplVars) (string, bool, error) {
	if len(blocks) == 0 || len(bytes.TrimSpace(resp.body)) == 0 {
		return "", false, nil
	}
	doc, err := parseHTML(resp.body)
	if err != nil {
		return "", false, nil
	}
	for _, e := range blocks {
		if e.Path != "" && resp.url != nil && !strings.Contains(resp.url.String(), e.Path) {
			continue
		}
		selStr, err := applyTemplate(e.Selector, vars, nil)
		if err != nil {
			return "", false, err
		}
		sel, err := CompileSelector(selStr)
		if err != nil {
			return "", false, fmt.Errorf("%s: error selector: %w", s.name, err)
		}
		hit := sel.First(doc)
		if hit == nil {
			continue
		}
		msg := strings.TrimSpace(textContent(hit))
		if e.Message != nil {
			if v, found, err := (htmlRow{n: doc}).value(*e.Message, vars); err == nil && found {
				msg = v
			}
		}
		msg = strings.Join(strings.Fields(msg), " ")
		if len(msg) > 300 {
			msg = msg[:300] + "..."
		}
		if msg == "" {
			msg = "the site reported an error"
		}
		return msg, true, nil
	}
	return "", false, nil
}

func (s *cgSession) testLogin(ctx context.Context, vars *tmplVars, cookieLogin bool) error {
	t := s.def.Login.Test
	if t == nil {
		return nil
	}
	path, err := applyTemplate(t.Path, vars, nil)
	if err != nil {
		return err
	}
	u, err := s.resolvePath(path)
	if err != nil {
		return err
	}
	resp, err := s.do(ctx, cgRequest{url: u})
	if err != nil {
		return err
	}
	if resp.status >= 400 {
		return fmt.Errorf("%s: login check failed: the site returned HTTP %d", s.name, resp.status)
	}
	if t.Selector == "" {
		return nil
	}
	doc, err := parseHTML(resp.body)
	if err != nil {
		return fmt.Errorf("%s: read login check page: %w", s.name, err)
	}
	ok, err := s.pageMatches(doc, t.Selector, vars)
	if err != nil {
		return err
	}
	if !ok {
		if cookieLogin {
			return fmt.Errorf("%s: login failed: the cookie was not accepted (it may have expired; copy a fresh one from your browser)", s.name)
		}
		if msg, found, _ := s.findError(s.def.Login.Error, resp, vars); found {
			return fmt.Errorf("%s: login failed: %s", s.name, msg)
		}
		return fmt.Errorf("%s: login failed: the site did not show a signed-in page; check the username and password", s.name)
	}
	return nil
}

// pageMatches reports whether a (templated) selector finds anything.
func (s *cgSession) pageMatches(doc *html.Node, selector string, vars *tmplVars) (bool, error) {
	selStr, err := applyTemplate(selector, vars, nil)
	if err != nil {
		return false, err
	}
	sel, err := CompileSelector(selStr)
	if err != nil {
		return false, fmt.Errorf("%s: selector: %w", s.name, err)
	}
	return sel.First(doc) != nil, nil
}

// setCookieString stores "a=b; c=d" cookies for the site.
func (s *cgSession) setCookieString(raw string) {
	var cookies []*http.Cookie
	for _, part := range strings.Split(raw, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		cookies = append(cookies, &http.Cookie{Name: strings.TrimSpace(name), Value: strings.TrimSpace(value), Path: "/"})
	}
	if len(cookies) > 0 {
		s.jar.SetCookies(&url.URL{Scheme: s.base.Scheme, Host: s.base.Host, Path: "/"}, cookies)
	}
}

func templatedInputs(in OrderedMap[string], vars *tmplVars) ([]string, map[string]string, error) {
	vals := make(map[string]string, len(in.Keys))
	keys := make([]string, 0, len(in.Keys))
	for _, k := range in.Keys {
		v, err := applyTemplate(in.Values[k], vars, nil)
		if err != nil {
			return nil, nil, err
		}
		keys = append(keys, k)
		vals[k] = v
	}
	return keys, vals, nil
}

func parseHTML(body []byte) (*html.Node, error) {
	return html.Parse(bytes.NewReader(body))
}

// maxXMLDepth is how deep an XML reply may nest, the same limit the HTML
// parser applies. Everything that walks the tree recurses, so a reply with
// millions of levels would overflow the stack.
const maxXMLDepth = 512

// parseXMLDoc reads an XML reply (e.g. an RSS/Torznab feed) into the same
// node tree HTML pages use, so the CSS selectors work on it. Element names
// keep their local part (torznab:attr becomes attr).
func parseXMLDoc(body []byte) (*html.Node, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = func(label string, in io.Reader) (io.Reader, error) {
		return charset.NewReaderLabel(label, in)
	}
	doc := &html.Node{Type: html.DocumentNode}
	cur := doc
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &html.Node{Type: html.ElementNode, Data: t.Name.Local}
			for _, a := range t.Attr {
				n.Attr = append(n.Attr, html.Attribute{Key: a.Name.Local, Val: a.Value})
			}
			depth++
			if depth > maxXMLDepth {
				return nil, fmt.Errorf("parse xml: the elements are nested more than %d deep", maxXMLDepth)
			}
			cur.AppendChild(n)
			cur = n
		case xml.EndElement:
			if cur.Parent != nil {
				cur = cur.Parent
				depth--
			}
		case xml.CharData:
			if cur != doc {
				cur.AppendChild(&html.Node{Type: html.TextNode, Data: string(t)})
			}
		}
	}
	return doc, nil
}
