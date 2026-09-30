package indexers

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/net/html"
)

// This file is a small CSS selector engine on top of golang.org/x/net/html,
// covering the subset Cardigann definitions actually use:
//
//	type, *, #id, .class
//	[attr] [attr=v] [attr~=v] [attr|=v] [attr^=v] [attr$=v] [attr*=v] [attr!=v]
//	descendant (space), child (>), adjacent sibling (+), general sibling (~)
//	comma-separated lists
//	:first-child :last-child :only-child :first-of-type :last-of-type
//	:only-of-type :empty :root :scope
//	:nth-child() :nth-last-child() :nth-of-type() :nth-last-of-type()
//	:not(list) :is(list) :matches(list) :has(relative list) :contains(text)
//
// Anything else is a parse error, never a panic, so a definition that relies
// on something unsupported fails with a clear message.

// Selector is a compiled selector list.
type Selector struct {
	src  string
	list []*complexSel
}

// String returns the source text of the selector.
func (s *Selector) String() string { return s.src }

type matchCtx struct {
	scope *html.Node
	depth int
	work  *workBudget
}

// workBudget limits how much matching one page may cost. A hostile page can be
// built so that an innocent-looking selector takes hours (a million siblings
// and :nth-child, or hundreds of nested elements and a long descendant
// chain). Every step of matching spends from the budget; once it is used up,
// nothing matches any more, so the search finishes with fewer results instead
// of never finishing. A nil budget is unlimited.
type workBudget struct{ left int64 }

// pageWorkBudget is what one page (or one lone selector call) may spend. Real
// pages use a small fraction of it.
const pageWorkBudget = 100_000_000

func newWorkBudget() *workBudget { return &workBudget{left: pageWorkBudget} }

// spend takes n steps and reports whether the budget still holds.
func (w *workBudget) spend(n int64) bool {
	if w == nil {
		return true
	}
	w.left -= n
	return w.left >= 0
}

func (c *matchCtx) spend(n int64) bool { return c.work.spend(n) }

type complexSel struct {
	parts []*compound
	combs []byte // combs[i] joins parts[i] and parts[i+1]: ' ', '>', '+', '~'
	// lead is the leading combinator of a relative selector (inside :has),
	// 0 for an ordinary selector.
	lead byte
}

type compound struct {
	tag    string // lower case; "" matches any element
	checks []check
}

type check func(n *html.Node, ctx *matchCtx) bool

const maxSelectorDepth = 32

var (
	selectorCacheMu sync.Mutex
	selectorCache   = map[string]*Selector{}
)

// CompileSelector parses a selector list, caching the result.
func CompileSelector(src string) (*Selector, error) {
	selectorCacheMu.Lock()
	if s, ok := selectorCache[src]; ok {
		selectorCacheMu.Unlock()
		return s, nil
	}
	selectorCacheMu.Unlock()

	p := &selParser{src: src}
	list, err := p.parseList(false, 0)
	if err != nil {
		return nil, fmt.Errorf("css selector %q: %w", src, err)
	}
	p.skipSpace()
	if !p.eof() {
		return nil, fmt.Errorf("css selector %q: unexpected %q at offset %d", src, p.src[p.pos:], p.pos)
	}
	sel := &Selector{src: src, list: list}

	selectorCacheMu.Lock()
	if len(selectorCache) > 4096 {
		selectorCache = map[string]*Selector{}
	}
	selectorCache[src] = sel
	selectorCacheMu.Unlock()
	return sel, nil
}

// Matches reports whether n itself matches the selector.
func (s *Selector) Matches(n *html.Node) bool { return s.MatchesWith(n, newWorkBudget()) }

// MatchesWith is Matches spending from budget.
func (s *Selector) MatchesWith(n *html.Node, budget *workBudget) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	ctx := &matchCtx{scope: n, work: budget}
	for _, c := range s.list {
		if c.matches(n, ctx) {
			return true
		}
	}
	return false
}

// First returns the first descendant of root (in document order) matching
// the selector, or nil. root itself is not considered.
func (s *Selector) First(root *html.Node) *html.Node { return s.FirstWith(root, newWorkBudget()) }

// FirstWith is First spending from budget.
func (s *Selector) FirstWith(root *html.Node, budget *workBudget) *html.Node {
	var found *html.Node
	s.walk(root, budget, func(n *html.Node) bool {
		found = n
		return false
	})
	return found
}

// All returns every descendant of root matching the selector, in document
// order.
func (s *Selector) All(root *html.Node) []*html.Node { return s.AllWith(root, newWorkBudget()) }

// AllWith is All spending from budget.
func (s *Selector) AllWith(root *html.Node, budget *workBudget) []*html.Node {
	var out []*html.Node
	s.walk(root, budget, func(n *html.Node) bool {
		out = append(out, n)
		return true
	})
	return out
}

func (s *Selector) walk(root *html.Node, budget *workBudget, visit func(*html.Node) bool) {
	if root == nil {
		return
	}
	ctx := &matchCtx{scope: root, work: budget}
	var rec func(n *html.Node) bool
	rec = func(n *html.Node) bool {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				for _, cs := range s.list {
					if cs.matches(c, ctx) {
						if !visit(c) {
							return false
						}
						break
					}
				}
			}
			if !rec(c) {
				return false
			}
		}
		return true
	}
	rec(root)
}

func (c *complexSel) matches(n *html.Node, ctx *matchCtx) bool {
	return c.matchAt(n, len(c.parts)-1, ctx)
}

func (c *complexSel) matchAt(n *html.Node, i int, ctx *matchCtx) bool {
	if !c.parts[i].matches(n, ctx) {
		return false
	}
	if i == 0 {
		return true
	}
	switch c.combs[i-1] {
	case '>':
		p := n.Parent
		return p != nil && p.Type == html.ElementNode && c.matchAt(p, i-1, ctx) ||
			// the relative anchor of :has may be the scope itself, even a
			// document node
			(p != nil && c.parts[i-1].isScopeAnchor() && p == ctx.scope)
	case ' ':
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Type == html.ElementNode && c.matchAt(p, i-1, ctx) {
				return true
			}
			if c.parts[i-1].isScopeAnchor() && p == ctx.scope {
				return true
			}
		}
		return false
	case '+':
		p := prevElement(n)
		return p != nil && c.matchAt(p, i-1, ctx)
	case '~':
		for p := prevElement(n); p != nil; p = prevElement(p) {
			if c.matchAt(p, i-1, ctx) {
				return true
			}
		}
		return false
	}
	return false
}

// scopeAnchor marks the implicit leading compound of a relative selector.
var scopeAnchorTag = "\x00scope"

func (cp *compound) isScopeAnchor() bool { return cp.tag == scopeAnchorTag }

func (cp *compound) matches(n *html.Node, ctx *matchCtx) bool {
	if !ctx.spend(1) {
		return false
	}
	if cp.tag == scopeAnchorTag {
		return n == ctx.scope
	}
	if n.Type != html.ElementNode {
		return false
	}
	if cp.tag != "" && cp.tag != "*" && !strings.EqualFold(n.Data, cp.tag) {
		return false
	}
	for _, chk := range cp.checks {
		if !chk(n, ctx) {
			return false
		}
	}
	return true
}

// --- parser ---

type selParser struct {
	src   string
	pos   int
	depth int
}

func (p *selParser) eof() bool { return p.pos >= len(p.src) }

func (p *selParser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *selParser) skipSpace() bool {
	start := p.pos
	for !p.eof() && isSelSpace(p.src[p.pos]) {
		p.pos++
	}
	return p.pos > start
}

func isSelSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' }

// parseList parses a comma separated list. relative allows each item to
// start with a combinator (used by :has). If inParen, parsing stops at ')'.
func (p *selParser) parseList(relative bool, closer byte) ([]*complexSel, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxSelectorDepth {
		return nil, fmt.Errorf("selector nested too deeply")
	}
	var list []*complexSel
	for {
		p.skipSpace()
		c, err := p.parseComplex(relative, closer)
		if err != nil {
			return nil, err
		}
		list = append(list, c)
		p.skipSpace()
		if p.peek() == ',' {
			p.pos++
			continue
		}
		return list, nil
	}
}

func (p *selParser) parseComplex(relative bool, closer byte) (*complexSel, error) {
	c := &complexSel{}
	if relative {
		if b := p.peek(); b == '>' || b == '+' || b == '~' {
			c.lead = b
			p.pos++
			p.skipSpace()
		} else {
			c.lead = ' '
		}
		c.parts = append(c.parts, &compound{tag: scopeAnchorTag})
		c.combs = append(c.combs, c.lead)
	}
	first, err := p.parseCompound()
	if err != nil {
		return nil, err
	}
	c.parts = append(c.parts, first)
	for {
		hadSpace := p.skipSpace()
		b := p.peek()
		if p.eof() || b == ',' || (closer != 0 && b == closer) {
			return c, nil
		}
		comb := byte(' ')
		switch b {
		case '>', '+', '~':
			comb = b
			p.pos++
			p.skipSpace()
		default:
			if !hadSpace {
				return nil, fmt.Errorf("unexpected %q at offset %d", string(b), p.pos)
			}
		}
		next, err := p.parseCompound()
		if err != nil {
			return nil, err
		}
		c.combs = append(c.combs, comb)
		c.parts = append(c.parts, next)
	}
}

func (p *selParser) parseCompound() (*compound, error) {
	cp := &compound{}
	start := p.pos
	if p.peek() == '*' {
		p.pos++
		cp.tag = "*"
	} else if isIdentStart(p.peek()) {
		cp.tag = strings.ToLower(p.parseIdent())
	}
	for !p.eof() {
		switch p.peek() {
		case '#':
			p.pos++
			id := p.parseIdent()
			if id == "" {
				return nil, fmt.Errorf("empty #id at offset %d", p.pos)
			}
			cp.checks = append(cp.checks, func(n *html.Node, _ *matchCtx) bool {
				v, ok := attr(n, "id")
				return ok && v == id
			})
		case '.':
			p.pos++
			class := p.parseIdent()
			if class == "" {
				return nil, fmt.Errorf("empty .class at offset %d", p.pos)
			}
			cp.checks = append(cp.checks, func(n *html.Node, _ *matchCtx) bool {
				v, ok := attr(n, "class")
				return ok && containsWord(v, class)
			})
		case '[':
			chk, err := p.parseAttr()
			if err != nil {
				return nil, err
			}
			cp.checks = append(cp.checks, chk)
		case ':':
			chk, err := p.parsePseudo()
			if err != nil {
				return nil, err
			}
			cp.checks = append(cp.checks, chk)
		default:
			if p.pos == start {
				return nil, fmt.Errorf("expected a selector at offset %d, found %q", p.pos, string(p.peek()))
			}
			return cp, nil
		}
	}
	if p.pos == start {
		return nil, fmt.Errorf("expected a selector at end of input")
	}
	return cp, nil
}

func isIdentStart(b byte) bool {
	return b == '_' || b == '-' || b == '\\' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

func isIdentChar(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func (p *selParser) parseIdent() string {
	var sb strings.Builder
	for !p.eof() {
		b := p.src[p.pos]
		if b == '\\' && p.pos+1 < len(p.src) {
			sb.WriteByte(p.src[p.pos+1])
			p.pos += 2
			continue
		}
		if !isIdentChar(b) {
			break
		}
		sb.WriteByte(b)
		p.pos++
	}
	return sb.String()
}

func (p *selParser) parseString() (string, error) {
	q := p.peek()
	p.pos++
	var sb strings.Builder
	for !p.eof() {
		b := p.src[p.pos]
		switch {
		case b == '\\' && p.pos+1 < len(p.src):
			sb.WriteByte(p.src[p.pos+1])
			p.pos += 2
		case b == q:
			p.pos++
			return sb.String(), nil
		default:
			sb.WriteByte(b)
			p.pos++
		}
	}
	return "", fmt.Errorf("unterminated string")
}

func (p *selParser) parseAttr() (check, error) {
	p.pos++ // [
	p.skipSpace()
	name := strings.ToLower(p.parseIdent())
	if name == "" {
		return nil, fmt.Errorf("attribute selector without a name at offset %d", p.pos)
	}
	p.skipSpace()
	if p.peek() == ']' {
		p.pos++
		return func(n *html.Node, _ *matchCtx) bool { _, ok := attr(n, name); return ok }, nil
	}
	var op string
	switch p.peek() {
	case '=':
		op = "="
		p.pos++
	case '~', '|', '^', '$', '*', '!':
		op = string(p.peek())
		p.pos++
		if p.peek() != '=' {
			return nil, fmt.Errorf("bad attribute operator at offset %d", p.pos)
		}
		p.pos++
		op += "="
	default:
		return nil, fmt.Errorf("bad attribute selector at offset %d", p.pos)
	}
	p.skipSpace()
	var val string
	if q := p.peek(); q == '"' || q == '\'' {
		v, err := p.parseString()
		if err != nil {
			return nil, err
		}
		val = v
	} else {
		start := p.pos
		for !p.eof() && p.src[p.pos] != ']' && !isSelSpace(p.src[p.pos]) {
			p.pos++
		}
		val = p.src[start:p.pos]
	}
	p.skipSpace()
	fold := false
	if b := p.peek(); b == 'i' || b == 'I' {
		fold = true
		p.pos++
		p.skipSpace()
	} else if b == 's' || b == 'S' {
		p.pos++
		p.skipSpace()
	}
	if p.peek() != ']' {
		return nil, fmt.Errorf("unterminated attribute selector")
	}
	p.pos++
	cmpVal := val
	if fold {
		cmpVal = strings.ToLower(val)
	}
	return func(n *html.Node, _ *matchCtx) bool {
		v, ok := attr(n, name)
		if op == "!=" {
			return !ok || v != val
		}
		if !ok {
			return false
		}
		if fold {
			v = strings.ToLower(v)
		}
		switch op {
		case "=":
			return v == cmpVal
		case "~=":
			return containsWord(v, cmpVal)
		case "|=":
			return v == cmpVal || strings.HasPrefix(v, cmpVal+"-")
		case "^=":
			return cmpVal != "" && strings.HasPrefix(v, cmpVal)
		case "$=":
			return cmpVal != "" && strings.HasSuffix(v, cmpVal)
		case "*=":
			return cmpVal != "" && strings.Contains(v, cmpVal)
		}
		return false
	}, nil
}

func (p *selParser) parsePseudo() (check, error) {
	p.pos++ // :
	if p.peek() == ':' {
		return nil, fmt.Errorf("pseudo-elements are not supported")
	}
	name := strings.ToLower(p.parseIdent())
	if name == "" {
		return nil, fmt.Errorf("empty pseudo-class at offset %d", p.pos)
	}
	hasArgs := p.peek() == '('
	if hasArgs {
		p.pos++
	}
	noArgs := func(c check) (check, error) {
		if hasArgs {
			return nil, fmt.Errorf(":%s takes no argument", name)
		}
		return c, nil
	}
	switch name {
	case "first-child":
		return noArgs(func(n *html.Node, _ *matchCtx) bool { return prevElement(n) == nil })
	case "last-child":
		return noArgs(func(n *html.Node, _ *matchCtx) bool { return nextElement(n) == nil })
	case "only-child":
		return noArgs(func(n *html.Node, _ *matchCtx) bool { return prevElement(n) == nil && nextElement(n) == nil })
	case "first-of-type":
		return noArgs(func(n *html.Node, ctx *matchCtx) bool { return nthIndex(n, true, false, ctx) == 1 })
	case "last-of-type":
		return noArgs(func(n *html.Node, ctx *matchCtx) bool { return nthIndex(n, true, true, ctx) == 1 })
	case "only-of-type":
		return noArgs(func(n *html.Node, ctx *matchCtx) bool {
			return nthIndex(n, true, false, ctx) == 1 && nthIndex(n, true, true, ctx) == 1
		})
	case "empty":
		return noArgs(func(n *html.Node, _ *matchCtx) bool {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode || (c.Type == html.TextNode && c.Data != "") {
					return false
				}
			}
			return true
		})
	case "root":
		return noArgs(func(n *html.Node, _ *matchCtx) bool {
			return n.Parent != nil && n.Parent.Type == html.DocumentNode
		})
	case "scope":
		return noArgs(func(n *html.Node, ctx *matchCtx) bool { return n == ctx.scope })
	case "nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type":
		if !hasArgs {
			return nil, fmt.Errorf(":%s needs an argument", name)
		}
		arg, err := p.rawArg()
		if err != nil {
			return nil, err
		}
		a, b, err := parseNth(arg)
		if err != nil {
			return nil, fmt.Errorf(":%s(%s): %w", name, arg, err)
		}
		ofType := strings.HasSuffix(name, "of-type")
		fromEnd := strings.Contains(name, "last")
		return func(n *html.Node, ctx *matchCtx) bool {
			return nthMatches(a, b, nthIndex(n, ofType, fromEnd, ctx))
		}, nil
	case "contains":
		if !hasArgs {
			return nil, fmt.Errorf(":contains needs an argument")
		}
		p.skipSpace()
		var text string
		if q := p.peek(); q == '"' || q == '\'' {
			s, err := p.parseString()
			if err != nil {
				return nil, err
			}
			text = s
			p.skipSpace()
			if p.peek() != ')' {
				return nil, fmt.Errorf("unterminated :contains")
			}
			p.pos++
		} else {
			s, err := p.rawArg()
			if err != nil {
				return nil, err
			}
			text = strings.TrimSpace(s)
		}
		return func(n *html.Node, ctx *matchCtx) bool {
			cost := countNodes(n, maxContainsNodes)
			if cost >= maxContainsNodes {
				ctx.spend(pageWorkBudget) // far too much text to search: give the page up
				return false
			}
			if !ctx.spend(int64(cost)) {
				return false
			}
			return strings.Contains(textContent(n), text)
		}, nil
	case "not", "is", "matches", "has":
		if !hasArgs {
			return nil, fmt.Errorf(":%s needs an argument", name)
		}
		list, err := p.parseList(name == "has", ')')
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.peek() != ')' {
			return nil, fmt.Errorf("unterminated :%s(", name)
		}
		p.pos++
		switch name {
		case "not":
			return func(n *html.Node, ctx *matchCtx) bool {
				for _, c := range list {
					if c.matches(n, ctx) {
						return false
					}
				}
				return true
			}, nil
		case "has":
			return func(n *html.Node, ctx *matchCtx) bool { return hasRelative(n, list, ctx) }, nil
		default:
			return func(n *html.Node, ctx *matchCtx) bool {
				for _, c := range list {
					if c.matches(n, ctx) {
						return true
					}
				}
				return false
			}, nil
		}
	}
	return nil, fmt.Errorf("unsupported pseudo-class :%s", name)
}

// rawArg reads everything up to the matching ')' (consuming it).
func (p *selParser) rawArg() (string, error) {
	depth := 0
	start := p.pos
	for !p.eof() {
		switch p.src[p.pos] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				s := p.src[start:p.pos]
				p.pos++
				return s, nil
			}
			depth--
		}
		p.pos++
	}
	return "", fmt.Errorf("missing ')'")
}

func hasRelative(n *html.Node, list []*complexSel, ctx *matchCtx) bool {
	if ctx.depth > maxSelectorDepth {
		return false
	}
	inner := &matchCtx{scope: n, depth: ctx.depth + 1, work: ctx.work}
	for _, c := range list {
		// candidates: descendants of n for ' ' and '>', following siblings
		// (and their descendants) for '+' and '~'
		var roots []*html.Node
		if c.lead == '+' || c.lead == '~' {
			for s := nextElement(n); s != nil; s = nextElement(s) {
				if !ctx.spend(1) {
					return false
				}
				roots = append(roots, s)
			}
		} else {
			roots = []*html.Node{n}
		}
		for _, r := range roots {
			if r != n && c.matches(r, inner) {
				return true
			}
			if anyDescendant(r, func(d *html.Node) bool { return c.matches(d, inner) }) {
				return true
			}
		}
	}
	return false
}

func anyDescendant(n *html.Node, f func(*html.Node) bool) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && f(c) {
			return true
		}
		if anyDescendant(c, f) {
			return true
		}
	}
	return false
}

func parseNth(s string) (a, b int, err error) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	switch s {
	case "odd":
		return 2, 1, nil
	case "even":
		return 2, 0, nil
	case "":
		return 0, 0, fmt.Errorf("empty argument")
	}
	i := strings.IndexByte(s, 'n')
	if i < 0 {
		b, err = strconv.Atoi(strings.TrimPrefix(s, "+"))
		return 0, b, err
	}
	switch as := s[:i]; as {
	case "", "+":
		a = 1
	case "-":
		a = -1
	default:
		if a, err = strconv.Atoi(as); err != nil {
			return 0, 0, err
		}
	}
	if rest := s[i+1:]; rest != "" {
		if b, err = strconv.Atoi(strings.TrimPrefix(rest, "+")); err != nil {
			return 0, 0, err
		}
	}
	return a, b, nil
}

func nthMatches(a, b, idx int) bool {
	if idx < 1 {
		return false
	}
	if a == 0 {
		return idx == b
	}
	d := idx - b
	return d%a == 0 && d/a >= 0
}

// nthIndex is the 1-based position of n among its siblings (of its own type
// when ofType), counted from the end when fromEnd. When the budget runs out
// it answers 0, which matches nothing.
func nthIndex(n *html.Node, ofType, fromEnd bool, ctx *matchCtx) int {
	idx := 1
	step := prevElement
	if fromEnd {
		step = nextElement
	}
	for s := step(n); s != nil; s = step(s) {
		if !ctx.spend(1) {
			return 0
		}
		if !ofType || s.Data == n.Data {
			idx++
		}
	}
	return idx
}

func prevElement(n *html.Node) *html.Node {
	for s := n.PrevSibling; s != nil; s = s.PrevSibling {
		if s.Type == html.ElementNode {
			return s
		}
	}
	return nil
}

func nextElement(n *html.Node) *html.Node {
	for s := n.NextSibling; s != nil; s = s.NextSibling {
		if s.Type == html.ElementNode {
			return s
		}
	}
	return nil
}

func attr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, name) {
			return a.Val, true
		}
	}
	return "", false
}

func containsWord(list, word string) bool {
	if word == "" {
		return false
	}
	for _, f := range strings.FieldsFunc(list, unicode.IsSpace) {
		if f == word {
			return true
		}
	}
	return false
}

// textContent is the DOM textContent of n: every descendant text node,
// concatenated.
func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				sb.WriteString(c.Data)
			case html.ElementNode, html.DocumentNode:
				rec(c)
			}
		}
	}
	rec(n)
	return sb.String()
}

// maxContainsNodes is the most nodes :contains reads the text of.
const maxContainsNodes = 1 << 20

// countNodes counts the nodes under n, stopping at limit.
func countNodes(n *html.Node, limit int) int {
	count := 0
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		for c := n.FirstChild; c != nil && count < limit; c = c.NextSibling {
			count++
			rec(c)
		}
	}
	rec(n)
	return count
}
