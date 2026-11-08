package css

import (
	"strconv"
	"strings"
)

// Selectors Level 4 subset: parsed from tokens into right-to-left compound
// chains, matched right to left with early outs.

// complexSel is one complex selector. comps[0] is the subject (rightmost)
// compound; combs[i] joins comps[i] to comps[i+1] (its left neighbour).
type complexSel struct {
	comps  []compound
	combs  []byte // ' ', '>', '+', '~'
	spec   uint32 // a<<16 | b<<8 | c, each clamped to 255
	pseudo Pseudo
	never  bool // contains a pseudo that can never match in Clack
	lead   byte // relative selectors (:has): the leading combinator
}

type compound struct {
	tag     string // lowercase, "" = any
	id      string
	classes []string
	attrs   []attrSel
	pcs     []pseudoClass
	anchor  bool // :has anchor: matches only the :has subject
}

type attrSel struct {
	name string // lowercase
	op   byte   // 0 exists, '=', '~', '|', '^', '$', '*'
	val  string
	ci   bool
}

type pcKind uint8

const (
	pcHover pcKind = iota
	pcFocus
	pcFocusVisible
	pcFocusWithin
	pcActive
	pcChecked
	pcDisabled
	pcEnabled
	pcFirstChild
	pcLastChild
	pcOnlyChild
	pcFirstOfType
	pcLastOfType
	pcOnlyOfType
	pcEmpty
	pcRoot
	pcPlaceholderShown
	pcAnyLink
	pcRequired
	pcOptional
	pcReadOnly
	pcReadWrite
	pcAlways // :defined, :valid, :in-range, :dir(ltr) …
	pcNever  // :visited, :target, :indeterminate …
	pcNot
	pcIs // also :where (spec 0), :matches, :-webkit-any
	pcHas
	pcNthChild
	pcNthLastChild
	pcNthOfType
	pcNthLastOfType
	pcLang
	pcModal // a dialog opened with showModal()
	pcOpen  // an open <dialog> or <details>
)

type pseudoClass struct {
	kind pcKind
	sels []*complexSel
	a, b int
	arg  string
}

var simplePseudo = map[string]pcKind{
	"hover": pcHover, "focus": pcFocus, "focus-visible": pcFocusVisible, "-moz-focusring": pcFocusVisible,
	"focus-within": pcFocusWithin, "active": pcActive, "checked": pcChecked,
	"disabled": pcDisabled, "enabled": pcEnabled,
	"first-child": pcFirstChild, "last-child": pcLastChild, "only-child": pcOnlyChild,
	"first-of-type": pcFirstOfType, "last-of-type": pcLastOfType, "only-of-type": pcOnlyOfType,
	"empty": pcEmpty, "root": pcRoot, "scope": pcRoot, "placeholder-shown": pcPlaceholderShown,
	"link": pcAnyLink, "any-link": pcAnyLink, "-webkit-any-link": pcAnyLink,
	"required": pcRequired, "optional": pcOptional, "read-only": pcReadOnly, "read-write": pcReadWrite,
	"defined": pcAlways, "valid": pcAlways, "in-range": pcAlways, "user-valid": pcNever,
	"visited": pcNever, "target": pcNever, "target-within": pcNever, "indeterminate": pcNever,
	"default": pcNever, "invalid": pcNever, "out-of-range": pcNever, "user-invalid": pcNever,
	"autofill": pcNever, "-webkit-autofill": pcNever, "fullscreen": pcNever, "modal": pcModal,
	"popover-open": pcNever, "playing": pcNever, "paused": pcNever, "blank": pcNever,
	"open": pcOpen, "closed": pcNever, "picture-in-picture": pcNever, "current": pcNever,
	"past": pcNever, "future": pcNever, "host": pcNever, "-webkit-full-screen": pcNever,
	"window-inactive": pcNever, "-webkit-scrollbar": pcNever, "horizontal": pcNever, "vertical": pcNever,
	"decrement": pcNever, "increment": pcNever, "start": pcNever, "end": pcNever,
	"double-button": pcNever, "single-button": pcNever, "no-button": pcNever, "corner-present": pcNever,
}

var pseudoElements = map[string]Pseudo{
	"before": PseudoBefore, "after": PseudoAfter,
	"placeholder": PseudoPlaceholder, "-webkit-input-placeholder": PseudoPlaceholder,
	"-ms-input-placeholder": PseudoPlaceholder,
	"backdrop":              PseudoBackdrop,
}

// never-matching but valid pseudo-elements (Chrome accepts any ::-webkit-*).
var neverElements = map[string]bool{
	"selection": true, "marker": true, "first-line": true, "first-letter": true,
	"file-selector-button": true, "cue": true, "grammar-error": true, "spelling-error": true,
	"target-text": true, "view-transition": true, "view-transition-group": true,
	"view-transition-image-pair": true, "view-transition-old": true, "view-transition-new": true,
	"part": true, "slotted": true, "highlight": true, "details-content": true, "scroll-marker": true,
	"scroll-marker-group": true, "checkmark": true, "picker-icon": true, "picker": true, "column": true,
}

const pseudoNever Pseudo = 255

type selParser struct {
	toks   []token
	i      int
	parent []*complexSel // nesting context (& = :is(parent))
	nested bool
	amp    bool // & seen
}

// parseSelectorList parses a selector list. Unforgiving lists fail as a
// whole on any invalid selector; forgiving ones (:is/:where) drop it.
func parseSelectorList(toks []token, parent []*complexSel, nested, relative, forgiving bool) ([]*complexSel, bool) {
	parts := splitTopComma(toks)
	out := make([]*complexSel, 0, len(parts))
	for _, part := range parts {
		sp := selParser{toks: part, parent: parent, nested: nested}
		s, ok := sp.complex(relative)
		if ok && nested && !sp.amp && !relative {
			s = prependParent(s, parent)
		}
		if !ok {
			if forgiving {
				continue
			}
			return nil, false
		}
		out = append(out, s)
	}
	return out, len(out) > 0 || forgiving
}

// prependParent turns a nested selector without & into "& <sel>" (or
// "& > sel" with its leading combinator).
func prependParent(s *complexSel, parent []*complexSel) *complexSel {
	lead := s.lead
	if lead == 0 {
		lead = ' '
	}
	s.lead = 0
	c := ampCompound(parent)
	s.comps = append(s.comps, c)
	s.combs = append(s.combs, lead)
	if len(parent) > 0 {
		s.spec = addSpec(s.spec, maxSpec(parent))
	}
	return s
}

func ampCompound(parent []*complexSel) compound {
	if len(parent) == 0 {
		return compound{pcs: []pseudoClass{{kind: pcRoot}}}
	}
	return compound{pcs: []pseudoClass{{kind: pcIs, sels: parent}}}
}

func splitTopComma(toks []token) [][]token {
	var out [][]token
	depth, start := 0, 0
	for i, t := range toks {
		switch t.k {
		case tFunction, tLParen, tLBracket, tLBrace:
			depth++
		case tRParen, tRBracket, tRBrace:
			if depth > 0 {
				depth--
			}
		case tComma:
			if depth == 0 {
				out = append(out, toks[start:i])
				start = i + 1
			}
		}
	}
	return append(out, toks[start:])
}

func addSpec(a, b uint32) uint32 {
	ca := min(a>>16+b>>16, 255)
	cb := min(a>>8&255+b>>8&255, 255)
	cc := min(a&255+b&255, 255)
	return ca<<16 | cb<<8 | cc
}

func maxSpec(ss []*complexSel) uint32 {
	var m uint32
	for _, s := range ss {
		m = max(m, s.spec)
	}
	return m
}

func (p *selParser) peek() *token {
	if p.i < len(p.toks) {
		return &p.toks[p.i]
	}
	return nil
}

func (p *selParser) skipWS() bool {
	ws := false
	for p.i < len(p.toks) && p.toks[p.i].k == tWS {
		p.i++
		ws = true
	}
	return ws
}

func isComb(t *token) byte {
	if t == nil || t.k != tDelim || len(t.s) != 1 {
		return 0
	}
	switch t.s[0] {
	case '>', '+', '~':
		return t.s[0]
	}
	return 0
}

func (p *selParser) complex(relative bool) (*complexSel, bool) {
	s := &complexSel{}
	var comps []compound
	var combs []byte // combs[i] between comps[i] and comps[i+1] (left to right)
	p.skipWS()
	if c := isComb(p.peek()); c != 0 {
		if !relative && !p.nested {
			return nil, false
		}
		s.lead = c
		p.i++
		p.skipWS()
	}
	for {
		c, ok := p.compound(s, len(comps) == 0)
		if !ok {
			return nil, false
		}
		comps = append(comps, c)
		ws := p.skipWS()
		t := p.peek()
		if t == nil {
			break
		}
		if cb := isComb(t); cb != 0 {
			p.i++
			p.skipWS()
			combs = append(combs, cb)
		} else if ws {
			combs = append(combs, ' ')
		} else {
			return nil, false
		}
		if p.peek() == nil {
			return nil, false
		}
		if s.pseudo != PseudoNone {
			return nil, false // pseudo-element not in the subject
		}
	}
	// reverse into right-to-left order
	n := len(comps)
	s.comps = make([]compound, n)
	s.combs = make([]byte, len(combs))
	for i := range comps {
		s.comps[n-1-i] = comps[i]
	}
	for i := range combs {
		s.combs[len(combs)-1-i] = combs[i]
	}
	if relative {
		lead := s.lead
		if lead == 0 {
			lead = ' '
		}
		s.comps = append(s.comps, compound{anchor: true})
		s.combs = append(s.combs, lead)
	}
	return s, true
}

// compound parses one compound selector, adding to s's specificity.
func (p *selParser) compound(s *complexSel, first bool) (compound, bool) {
	var c compound
	start := p.i
	for p.i < len(p.toks) {
		t := &p.toks[p.i]
		if s.pseudo != PseudoNone && !(t.k == tColon) {
			break
		}
		switch {
		case t.k == tIdent && p.i == start:
			p.i++
			if p.i+1 < len(p.toks) && p.toks[p.i].is('|') && p.toks[p.i+1].k != tDelim {
				p.i++ // ns|tag: ignore the namespace
				if p.toks[p.i].k == tIdent {
					c.tag = strings.ToLower(p.toks[p.i].s)
				}
				p.i++
			} else {
				c.tag = strings.ToLower(t.s)
			}
			s.spec = addSpec(s.spec, 1)
		case t.is('*') && p.i == start:
			p.i++
			if p.i+1 < len(p.toks) && p.toks[p.i].is('|') {
				p.i++
				if p.toks[p.i].k == tIdent {
					c.tag = strings.ToLower(p.toks[p.i].s)
					s.spec = addSpec(s.spec, 1)
				}
				p.i++
			}
		case t.is('|') && p.i == start && p.i+1 < len(p.toks) && p.toks[p.i+1].k == tIdent:
			c.tag = strings.ToLower(p.toks[p.i+1].s)
			s.spec = addSpec(s.spec, 1)
			p.i += 2
		case t.k == tHash:
			if !t.id {
				return c, false
			}
			if c.id != "" && c.id != t.s {
				s.never = true
			}
			c.id = t.s
			s.spec = addSpec(s.spec, 1<<16)
			p.i++
		case t.is('.'):
			if p.i+1 >= len(p.toks) || p.toks[p.i+1].k != tIdent {
				return c, false
			}
			c.classes = append(c.classes, p.toks[p.i+1].s)
			s.spec = addSpec(s.spec, 1<<8)
			p.i += 2
		case t.k == tLBracket:
			a, ok := p.attr()
			if !ok {
				return c, false
			}
			c.attrs = append(c.attrs, a)
			s.spec = addSpec(s.spec, 1<<8)
		case t.is('&'):
			p.i++
			p.amp = true
			c.pcs = append(c.pcs, ampCompound(p.parent).pcs...)
			if len(p.parent) > 0 {
				s.spec = addSpec(s.spec, maxSpec(p.parent))
			}
		case t.k == tColon:
			if !p.pseudo(s, &c) {
				return c, false
			}
		default:
			goto done
		}
	}
done:
	if p.i == start {
		return c, false
	}
	return c, true
}

func (p *selParser) attr() (attrSel, bool) {
	var a attrSel
	p.i++ // [
	end := -1
	depth := 0
	for j := p.i; j < len(p.toks); j++ {
		k := p.toks[j].k
		if k == tLBracket || k == tLParen || k == tFunction {
			depth++
		} else if k == tRParen {
			depth--
		} else if k == tRBracket {
			if depth == 0 {
				end = j
				break
			}
			depth--
		}
	}
	if end < 0 {
		return a, false
	}
	in := p.toks[p.i:end]
	p.i = end + 1
	// strip whitespace
	var ts []token
	for _, t := range in {
		if t.k != tWS {
			ts = append(ts, t)
		}
	}
	// namespace prefix
	if len(ts) >= 3 && ts[1].is('|') && (ts[0].k == tIdent || ts[0].is('*')) && ts[2].k == tIdent {
		ts = ts[2:]
	} else if len(ts) >= 2 && ts[0].is('|') && ts[1].k == tIdent {
		ts = ts[1:]
	}
	if len(ts) == 0 || ts[0].k != tIdent {
		return a, false
	}
	a.name = strings.ToLower(ts[0].s)
	ts = ts[1:]
	if len(ts) == 0 {
		return a, true
	}
	if ts[0].is('=') {
		a.op = '='
		ts = ts[1:]
	} else if len(ts) >= 2 && ts[0].k == tDelim && ts[1].is('=') && strings.ContainsAny(ts[0].s, "~|^$*") && len(ts[0].s) == 1 {
		a.op = ts[0].s[0]
		ts = ts[2:]
	} else {
		return a, false
	}
	if len(ts) == 0 || ts[0].k != tIdent && ts[0].k != tString {
		return a, false
	}
	a.val = ts[0].s
	ts = ts[1:]
	if len(ts) == 1 && ts[0].k == tIdent {
		switch strings.ToLower(ts[0].s) {
		case "i":
			a.ci = true
		case "s":
		default:
			return a, false
		}
		ts = ts[1:]
	}
	return a, len(ts) == 0
}

// fnArgs returns the tokens inside the function at p.i (a tFunction) and
// advances past its closing paren.
func (p *selParser) fnArgs() ([]token, bool) {
	p.i++
	start := p.i
	depth := 0
	for ; p.i < len(p.toks); p.i++ {
		switch p.toks[p.i].k {
		case tFunction, tLParen, tLBracket, tLBrace:
			depth++
		case tRBracket, tRBrace:
			depth--
		case tRParen:
			if depth == 0 {
				args := p.toks[start:p.i]
				p.i++
				return args, true
			}
			depth--
		}
	}
	return nil, false
}

func (p *selParser) pseudo(s *complexSel, c *compound) bool {
	p.i++ // :
	element := false
	if p.i < len(p.toks) && p.toks[p.i].k == tColon {
		element = true
		p.i++
	}
	if p.i >= len(p.toks) {
		return false
	}
	t := p.toks[p.i]
	name := strings.ToLower(t.s)
	if element || (t.k == tIdent && (name == "before" || name == "after" || name == "first-line" || name == "first-letter")) {
		if s.pseudo != PseudoNone {
			return false
		}
		if t.k == tFunction {
			if _, ok := p.fnArgs(); !ok {
				return false
			}
			if !neverElements[name] {
				return false
			}
			s.pseudo, s.never = pseudoNever, true
			s.spec = addSpec(s.spec, 1)
			return true
		}
		if t.k != tIdent {
			return false
		}
		p.i++
		s.spec = addSpec(s.spec, 1)
		if pe, ok := pseudoElements[name]; ok {
			s.pseudo = pe
			return true
		}
		if neverElements[name] || strings.HasPrefix(name, "-webkit-") {
			s.pseudo, s.never = pseudoNever, true
			return true
		}
		return false
	}
	if t.k == tIdent {
		k, ok := simplePseudo[name]
		if !ok {
			return false
		}
		p.i++
		s.spec = addSpec(s.spec, 1<<8)
		if k == pcNever {
			s.never = true
		}
		c.pcs = append(c.pcs, pseudoClass{kind: k})
		return true
	}
	if t.k != tFunction {
		return false
	}
	args, ok := p.fnArgs()
	if !ok {
		return false
	}
	switch name {
	case "not", "is", "where", "matches", "-webkit-any", "has":
		forgiving := name == "is" || name == "where" || name == "matches" || name == "-webkit-any"
		sels, ok := parseSelectorList(args, p.parent, false, name == "has", forgiving)
		if !ok {
			return false
		}
		// & inside :is(&) counts as using the nesting selector
		if containsAmp(args) {
			p.amp = true
		}
		for _, x := range sels {
			if x.pseudo != PseudoNone {
				return false // pseudo-elements aren't valid inside these
			}
		}
		kind := pcIs
		switch name {
		case "not":
			kind = pcNot
		case "has":
			kind = pcHas
		}
		if name != "where" {
			s.spec = addSpec(s.spec, maxSpec(sels))
		}
		if kind == pcIs && len(sels) == 0 {
			s.never = true
		}
		c.pcs = append(c.pcs, pseudoClass{kind: kind, sels: sels})
		return true
	case "nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type":
		var of []token
		for j, a := range args {
			if a.ident("of") && (name == "nth-child" || name == "nth-last-child") {
				of = args[j+1:]
				args = args[:j]
				break
			}
		}
		a, b, ok := parseNth(args)
		if !ok {
			return false
		}
		pc := pseudoClass{a: a, b: b}
		switch name {
		case "nth-child":
			pc.kind = pcNthChild
		case "nth-last-child":
			pc.kind = pcNthLastChild
		case "nth-of-type":
			pc.kind = pcNthOfType
		default:
			pc.kind = pcNthLastOfType
		}
		s.spec = addSpec(s.spec, 1<<8)
		if of != nil {
			sels, ok := parseSelectorList(of, p.parent, false, false, false)
			if !ok {
				return false
			}
			pc.sels = sels
			s.spec = addSpec(s.spec, maxSpec(sels))
		}
		c.pcs = append(c.pcs, pc)
		return true
	case "lang":
		var arg string
		for _, a := range args {
			if a.k == tIdent || a.k == tString {
				arg = strings.ToLower(a.s)
				break
			}
		}
		if arg == "" {
			return false
		}
		s.spec = addSpec(s.spec, 1<<8)
		c.pcs = append(c.pcs, pseudoClass{kind: pcLang, arg: arg})
		return true
	case "dir":
		s.spec = addSpec(s.spec, 1<<8)
		k := pcNever
		for _, a := range args {
			if a.ident("ltr") {
				k = pcAlways
			}
		}
		if k == pcNever {
			s.never = true
		}
		c.pcs = append(c.pcs, pseudoClass{kind: k})
		return true
	case "host", "host-context", "state", "active-view-transition-type", "heading":
		s.spec = addSpec(s.spec, 1<<8)
		s.never = true
		c.pcs = append(c.pcs, pseudoClass{kind: pcNever})
		return true
	}
	return false
}

func containsAmp(ts []token) bool {
	for i := range ts {
		if ts[i].is('&') {
			return true
		}
	}
	return false
}

// parseNth parses the An+B microsyntax.
func parseNth(ts []token) (a, b int, ok bool) {
	var sb strings.Builder
	for _, t := range ts {
		switch t.k {
		case tWS:
		case tIdent:
			sb.WriteString(t.s)
		case tNumber:
			if !t.isInt {
				return 0, 0, false
			}
			if t.num >= 0 && sb.Len() > 0 {
				last := sb.String()[sb.Len()-1]
				if last != '+' && last != '-' {
					sb.WriteByte('+')
				}
			}
			sb.WriteString(strconv.Itoa(int(t.num)))
		case tDimension:
			if !t.isInt {
				return 0, 0, false
			}
			sb.WriteString(strconv.Itoa(int(t.num)))
			sb.WriteString(t.s)
		case tDelim:
			if t.s != "+" && t.s != "-" {
				return 0, 0, false
			}
			sb.WriteString(t.s)
		default:
			return 0, 0, false
		}
	}
	s := strings.ToLower(sb.String())
	switch s {
	case "odd":
		return 2, 1, true
	case "even":
		return 2, 0, true
	case "":
		return 0, 0, false
	}
	n := strings.IndexByte(s, 'n')
	if n < 0 {
		v, err := strconv.Atoi(s)
		return 0, v, err == nil
	}
	as := s[:n]
	switch as {
	case "", "+":
		a = 1
	case "-":
		a = -1
	default:
		v, err := strconv.Atoi(as)
		if err != nil {
			return 0, 0, false
		}
		a = v
	}
	rest := s[n+1:]
	if rest == "" {
		return a, 0, true
	}
	if rest[0] != '+' && rest[0] != '-' {
		return 0, 0, false
	}
	sign := 1
	if rest[0] == '-' {
		sign = -1
	}
	rest = rest[1:]
	if rest == "" || rest[0] == '+' || rest[0] == '-' {
		return 0, 0, false
	}
	v, err := strconv.Atoi(rest)
	if err != nil {
		return 0, 0, false
	}
	return a, sign * v, true
}

// nthMatch: position pos (1-based) is a*n+b for some n >= 0.
func nthMatch(a, b, pos int) bool {
	if a == 0 {
		return pos == b
	}
	d := pos - b
	if d%a != 0 {
		return false
	}
	return d/a >= 0
}

// --- matching ---

// matchSel reports whether s matches el. anchor is the :has subject for
// relative selectors (nil otherwise).
func matchSel(s *complexSel, el Element, anchor Element) bool {
	if s.never {
		return false
	}
	return matchFrom(s, 0, el, anchor)
}

func matchFrom(s *complexSel, i int, el Element, anchor Element) bool {
	if !matchCompound(&s.comps[i], el, anchor) {
		return false
	}
	if i == len(s.comps)-1 {
		return true
	}
	switch s.combs[i] {
	case '>':
		p := el.Parent()
		return p != nil && matchFrom(s, i+1, p, anchor)
	case ' ':
		for p := el.Parent(); p != nil; p = p.Parent() {
			if matchFrom(s, i+1, p, anchor) {
				return true
			}
		}
	case '+':
		p := el.Prev()
		return p != nil && matchFrom(s, i+1, p, anchor)
	case '~':
		for p := el.Prev(); p != nil; p = p.Prev() {
			if matchFrom(s, i+1, p, anchor) {
				return true
			}
		}
	}
	return false
}

func matchCompound(c *compound, el Element, anchor Element) bool {
	if c.anchor {
		return anchor != nil && el == anchor
	}
	if c.tag != "" && el.Tag() != c.tag {
		return false
	}
	if c.id != "" && el.ID() != c.id {
		return false
	}
	for _, cl := range c.classes {
		if !el.HasClass(cl) {
			return false
		}
	}
	for i := range c.attrs {
		if !matchAttr(&c.attrs[i], el) {
			return false
		}
	}
	for i := range c.pcs {
		if !matchPseudo(&c.pcs[i], el, anchor) {
			return false
		}
	}
	return true
}

func matchAttr(a *attrSel, el Element) bool {
	v, ok := el.Attr(a.name)
	if !ok {
		return false
	}
	if a.op == 0 {
		return true
	}
	want := a.val
	if a.ci {
		v, want = strings.ToLower(v), strings.ToLower(want)
	}
	switch a.op {
	case '=':
		return v == want
	case '~':
		if want == "" || strings.ContainsAny(want, " \t\n\r\f") {
			return false
		}
		for _, f := range strings.Fields(v) {
			if f == want {
				return true
			}
		}
		return false
	case '|':
		return v == want || strings.HasPrefix(v, want+"-")
	case '^':
		return want != "" && strings.HasPrefix(v, want)
	case '$':
		return want != "" && strings.HasSuffix(v, want)
	case '*':
		return want != "" && strings.Contains(v, want)
	}
	return false
}

var formTags = map[string]bool{"button": true, "input": true, "select": true, "textarea": true, "option": true, "optgroup": true, "fieldset": true}

func isDisabled(el Element) bool {
	if el.State()&StateDisabled != 0 {
		return true
	}
	if formTags[el.Tag()] {
		_, ok := el.Attr("disabled")
		return ok
	}
	return false
}

// textElement is optionally implemented by elements that know whether
// they have text children (for :empty).
type textElement interface {
	HasText() bool
}

func matchPseudo(pc *pseudoClass, el Element, anchor Element) bool {
	st := el.State()
	switch pc.kind {
	case pcHover:
		return st&StateHover != 0
	case pcFocus:
		return st&StateFocus != 0
	case pcModal:
		m, ok := el.(interface{ Modal() bool })
		return ok && m.Modal()
	case pcOpen:
		if t := el.Tag(); t != "dialog" && t != "details" {
			return false
		}
		_, ok := el.Attr("open")
		return ok
	case pcFocusVisible:
		return st&StateFocusVisible != 0
	case pcFocusWithin:
		return st&(StateFocusWithin|StateFocus) != 0
	case pcActive:
		return st&StateActive != 0
	case pcChecked:
		if st&StateChecked != 0 {
			return true
		}
		if t := el.Tag(); t == "input" || t == "option" {
			_, ok := el.Attr("checked")
			if t == "option" {
				_, ok = el.Attr("selected")
			}
			return ok
		}
		return false
	case pcDisabled:
		return isDisabled(el)
	case pcEnabled:
		return formTags[el.Tag()] && !isDisabled(el)
	case pcRoot:
		return el.Parent() == nil
	case pcEmpty:
		if st&StateEmpty != 0 {
			return true
		}
		if k, ok := el.(KidsElement); ok {
			if len(k.Kids()) != 0 {
				return false
			}
			if t, ok := el.(textElement); ok && t.HasText() {
				return false
			}
			return true
		}
		return false
	case pcPlaceholderShown:
		return st&StatePlaceholderShown != 0
	case pcAnyLink:
		if t := el.Tag(); t == "a" || t == "area" {
			_, ok := el.Attr("href")
			return ok
		}
		return false
	case pcRequired, pcOptional:
		if t := el.Tag(); t != "input" && t != "select" && t != "textarea" {
			return false
		}
		_, ok := el.Attr("required")
		return ok == (pc.kind == pcRequired)
	case pcReadWrite, pcReadOnly:
		rw := false
		switch el.Tag() {
		case "input", "textarea":
			_, ro := el.Attr("readonly")
			rw = !ro && !isDisabled(el)
		default:
			if v, ok := el.Attr("contenteditable"); ok && v != "false" {
				rw = true
			}
		}
		return rw == (pc.kind == pcReadWrite)
	case pcAlways:
		return true
	case pcNever:
		return false
	case pcNot:
		for _, s := range pc.sels {
			if matchSel(s, el, anchor) {
				return false
			}
		}
		return true
	case pcIs:
		for _, s := range pc.sels {
			if matchSel(s, el, anchor) {
				return true
			}
		}
		return false
	case pcHas:
		return matchHas(pc.sels, el)
	case pcFirstChild:
		i, n := el.Index()
		return n > 0 && i == 0
	case pcLastChild:
		i, n := el.Index()
		return n > 0 && i == n-1
	case pcOnlyChild:
		_, n := el.Index()
		return n == 1
	case pcFirstOfType:
		i, n := el.TypeIndex()
		return n > 0 && i == 0
	case pcLastOfType:
		i, n := el.TypeIndex()
		return n > 0 && i == n-1
	case pcOnlyOfType:
		_, n := el.TypeIndex()
		return n == 1
	case pcNthChild, pcNthLastChild:
		i, n := el.Index()
		if n == 0 {
			return false
		}
		if pc.sels != nil {
			return nthOf(pc, el, anchor)
		}
		pos := i + 1
		if pc.kind == pcNthLastChild {
			pos = n - i
		}
		return nthMatch(pc.a, pc.b, pos)
	case pcNthOfType, pcNthLastOfType:
		i, n := el.TypeIndex()
		if n == 0 {
			return false
		}
		pos := i + 1
		if pc.kind == pcNthLastOfType {
			pos = n - i
		}
		return nthMatch(pc.a, pc.b, pos)
	case pcLang:
		for e := el; e != nil; e = e.Parent() {
			if v, ok := e.Attr("lang"); ok {
				v = strings.ToLower(v)
				return v == pc.arg || strings.HasPrefix(v, pc.arg+"-")
			}
		}
		return false
	}
	return false
}

func matchAny(sels []*complexSel, el Element, anchor Element) bool {
	for _, s := range sels {
		if matchSel(s, el, anchor) {
			return true
		}
	}
	return false
}

// nthOf handles :nth-child(An+B of S) / :nth-last-child(… of S).
func nthOf(pc *pseudoClass, el Element, anchor Element) bool {
	if !matchAny(pc.sels, el, anchor) {
		return false
	}
	pos := 1
	if pc.kind == pcNthChild {
		for p := el.Prev(); p != nil; p = p.Prev() {
			if matchAny(pc.sels, p, anchor) {
				pos++
			}
		}
		return nthMatch(pc.a, pc.b, pos)
	}
	par, ok := el.Parent().(KidsElement)
	if !ok {
		return false
	}
	kids := par.Kids()
	after := false
	for _, k := range kids {
		if after && matchAny(pc.sels, k, anchor) {
			pos++
		}
		if k == el {
			after = true
		}
	}
	return after && nthMatch(pc.a, pc.b, pos)
}

// matchHas evaluates :has(relative selectors) on el. Only elements that
// know their children (KidsElement) can match.
func matchHas(sels []*complexSel, el Element) bool {
	k, ok := el.(KidsElement)
	if !ok {
		return false
	}
	for _, s := range sels {
		lead := s.combs[len(s.combs)-1]
		switch lead {
		case '>', ' ':
			childOnly := lead == '>' && len(s.comps) == 2
			if hasDesc(s, k.Kids(), el, childOnly) {
				return true
			}
		case '+', '~':
			par, ok := el.Parent().(KidsElement)
			if !ok {
				continue
			}
			kids := par.Kids()
			after := false
			for _, sib := range kids {
				if after {
					if matchSel(s, sib, el) {
						return true
					}
					if sk, ok := sib.(KidsElement); ok && len(s.comps) > 2 && hasDesc(s, sk.Kids(), el, false) {
						return true
					}
					if lead == '+' && len(s.comps) == 2 {
						break
					}
				}
				if sib == el {
					after = true
				}
			}
		}
	}
	return false
}

func hasDesc(s *complexSel, kids []Element, anchor Element, childOnly bool) bool {
	for _, kid := range kids {
		if matchSel(s, kid, anchor) {
			return true
		}
		if !childOnly {
			if kk, ok := kid.(KidsElement); ok && hasDesc(s, kk.Kids(), anchor, false) {
				return true
			}
		}
	}
	return false
}
