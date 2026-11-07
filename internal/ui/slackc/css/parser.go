package css

import (
	"strconv"
	"strings"
)

// sheet is one compiled stylesheet.
type sheet struct {
	rules  []*rule
	layers []string // layer paths in first-appearance order
}

// rule is a style rule (or a nested-declarations group) with its context.
type rule struct {
	sels  []*complexSel
	decls Decls
	media []*mediaList // all must match (nested @media)
	layer string       // "" = unlayered
}

type sheetParser struct {
	src    string
	sh     *sheet
	media  []*mediaList
	layer  string
	anon   int
	layers map[string]bool
	depth  int
}

// parseSheet compiles a stylesheet. It never fails: invalid parts are
// dropped the way browsers drop them.
func parseSheet(src string) *sheet {
	p := &sheetParser{src: src, sh: &sheet{}, layers: map[string]bool{}}
	toks := tokenize(src)
	p.rules(toks, true)
	return p.sh
}

// componentEnd returns the index just past the component value at i.
func componentEnd(toks []token, i int) int {
	switch toks[i].k {
	case tFunction, tLParen, tLBracket, tLBrace:
		depth := 0
		for j := i; j < len(toks); j++ {
			switch toks[j].k {
			case tFunction, tLParen, tLBracket, tLBrace:
				depth++
			case tRParen, tRBracket, tRBrace:
				depth--
				if depth == 0 {
					return j + 1
				}
			}
		}
		return len(toks)
	}
	return i + 1
}

// scanTo finds the first top-level token of one of the kinds from i.
func scanTo(toks []token, i int, kinds ...tokKind) int {
	for i < len(toks) {
		for _, k := range kinds {
			if toks[i].k == k {
				return i
			}
		}
		i = componentEnd(toks, i)
	}
	return len(toks)
}

// blockAt returns the contents of the {} block starting at i and the
// index after it.
func blockAt(toks []token, i int) ([]token, int) {
	end := componentEnd(toks, i)
	inner := end - 1
	if inner < i+1 || toks[inner].k != tRBrace {
		inner = end
	}
	return toks[i+1 : inner], end
}

func trimWS(ts []token) []token {
	for len(ts) > 0 && ts[0].k == tWS {
		ts = ts[1:]
	}
	for len(ts) > 0 && ts[len(ts)-1].k == tWS {
		ts = ts[:len(ts)-1]
	}
	return ts
}

// valueText is the source text of ts, comments replaced by a space when
// they separated two tokens.
func (p *sheetParser) valueText(ts []token) string {
	return tokText(p.src, ts)
}

func tokText(src string, ts []token) string {
	ts = trimWS(ts)
	if len(ts) == 0 {
		return ""
	}
	contiguous := true
	for i := 1; i < len(ts); i++ {
		if ts[i].start != ts[i-1].end {
			contiguous = false
			break
		}
	}
	if contiguous {
		return src[ts[0].start:ts[len(ts)-1].end]
	}
	var b strings.Builder
	for i, t := range ts {
		if i > 0 && t.start != ts[i-1].end && t.k != tWS && ts[i-1].k != tWS {
			b.WriteByte(' ')
		}
		b.WriteString(src[t.start:t.end])
	}
	return b.String()
}

// rules parses a list of rules (top level or inside a grouping at-rule
// outside any style rule).
func (p *sheetParser) rules(toks []token, top bool) {
	i := 0
	for i < len(toks) {
		t := toks[i]
		switch t.k {
		case tWS, tCDO, tCDC, tSemicolon:
			i++
			continue
		case tAtKeyword:
			i = p.atRule(toks, i, nil)
			continue
		}
		j := scanTo(toks, i, tLBrace)
		if j >= len(toks) {
			return
		}
		prelude := toks[i:j]
		block, end := blockAt(toks, j)
		i = end
		sels, ok := parseSelectorList(trimWS(prelude), nil, false, false, false)
		if !ok {
			continue
		}
		p.styleBlock(block, sels)
	}
}

// atRule parses the at-rule at i; parent is the enclosing style rule's
// selector list (nil outside style rules). Returns the index after it.
func (p *sheetParser) atRule(toks []token, i int, parent []*complexSel) int {
	name := strings.ToLower(toks[i].s)
	j := scanTo(toks, i+1, tSemicolon, tLBrace)
	prelude := trimWS(toks[i+1 : min(j, len(toks))])
	if j >= len(toks) || toks[j].k == tSemicolon {
		if name == "layer" && parent == nil {
			for _, part := range splitTopComma(prelude) {
				if n := layerName(trimWS(part)); n != "" {
					p.addLayer(p.joinLayer(n))
				}
			}
		}
		return j + 1
	}
	block, end := blockAt(toks, j)
	if p.depth > 64 {
		return end
	}
	p.depth++
	defer func() { p.depth-- }()
	inner := func() {
		if parent != nil {
			p.styleBlock(block, parent)
		} else {
			p.rules(block, false)
		}
	}
	switch name {
	case "media":
		ml := parseMediaList(p.src, prelude)
		p.media = append(p.media, ml)
		inner()
		p.media = p.media[:len(p.media)-1]
	case "supports":
		if evalSupports(p.src, parseCVsTok(prelude)) {
			inner()
		}
	case "container":
		inner()
	case "layer":
		n := layerName(prelude)
		if n == "" {
			p.anon++
			n = "\x00anon" + strconv.Itoa(p.anon)
		}
		saved := p.layer
		p.layer = p.joinLayer(n)
		p.addLayer(p.layer)
		inner()
		p.layer = saved
	case "scope":
		var scope []*complexSel
		ps := parseCVsTok(prelude)
		if len(ps) > 0 && ps[0].t.k == tLParen {
			// the (scope-start) block: re-tokenize its source text
			txt := ps[0].text(p.src)
			if len(txt) >= 2 {
				st := tokenize(txt[1 : len(txt)-1])
				s, ok := parseSelectorList(trimWS(st), parent, parent != nil, false, false)
				if !ok {
					return end
				}
				scope = s
			}
		}
		if scope == nil {
			scope = parent
		}
		if scope == nil {
			p.rules(block, false)
		} else {
			p.styleBlock(block, scope)
		}
	default:
		// @import, @font-face, @keyframes, @property, @page, @namespace,
		// @counter-style, @starting-style, unknown: skipped.
	}
	return end
}

func layerName(ts []token) string {
	var b strings.Builder
	for _, t := range ts {
		switch {
		case t.k == tIdent:
			b.WriteString(t.s)
		case t.is('.'):
			b.WriteByte('.')
		case t.k == tWS:
		default:
			return ""
		}
	}
	return b.String()
}

func (p *sheetParser) joinLayer(n string) string {
	if p.layer == "" {
		return n
	}
	return p.layer + "." + n
}

func (p *sheetParser) addLayer(path string) {
	// register every prefix so parents are ordered at first mention
	for k := 0; k <= len(path); k++ {
		if k == len(path) || path[k] == '.' {
			pre := path[:k]
			if !p.layers[pre] {
				p.layers[pre] = true
				p.sh.layers = append(p.sh.layers, pre)
			}
		}
	}
}

// styleBlock parses a style rule's contents: declarations, nested rules
// and nested grouping at-rules. sels is the rule's selector list.
func (p *sheetParser) styleBlock(toks []token, sels []*complexSel) {
	var decls Decls
	flush := func() {
		if len(decls) > 0 {
			p.sh.rules = append(p.sh.rules, &rule{sels: sels, decls: decls, media: append([]*mediaList(nil), p.media...), layer: p.layer})
			decls = nil
		}
	}
	i := 0
	for i < len(toks) {
		t := toks[i]
		switch t.k {
		case tWS, tSemicolon:
			i++
			continue
		case tAtKeyword:
			flush()
			i = p.atRule(toks, i, sels)
			continue
		case tIdent:
			custom := strings.HasPrefix(t.s, "--")
			var j int
			if custom {
				j = scanTo(toks, i, tSemicolon)
			} else {
				j = scanTo(toks, i, tSemicolon, tLBrace)
			}
			if j < len(toks) && toks[j].k == tLBrace {
				// nested rule like "div:hover { … }"
				i = p.nestedRule(toks, i, j, sels, flush)
				continue
			}
			decls = p.decl(toks[i:j], decls)
			i = j + 1
			continue
		}
		j := scanTo(toks, i, tSemicolon, tLBrace)
		if j >= len(toks) || toks[j].k == tSemicolon {
			i = j + 1
			continue
		}
		i = p.nestedRule(toks, i, j, sels, flush)
	}
	flush()
}

func (p *sheetParser) nestedRule(toks []token, i, j int, parent []*complexSel, flush func()) int {
	block, end := blockAt(toks, j)
	if p.depth > 64 {
		return end
	}
	ns, ok := parseSelectorList(trimWS(toks[i:j]), parent, true, false, false)
	if !ok {
		return end
	}
	flush()
	p.depth++
	p.styleBlock(block, ns)
	p.depth--
	return end
}

// decl parses one declaration's tokens and appends its (expanded,
// validated) longhands to out.
func (p *sheetParser) decl(ts []token, out Decls) Decls {
	return parseDecl(p.src, ts, out)
}

func parseDecl(src string, ts []token, out Decls) Decls {
	ts = trimWS(ts)
	if len(ts) < 2 || ts[0].k != tIdent {
		return out
	}
	name := ts[0].s
	k := 1
	for k < len(ts) && ts[k].k == tWS {
		k++
	}
	if k >= len(ts) || ts[k].k != tColon {
		return out
	}
	val := ts[k+1:]
	imp := false
	// !important at the end
	e := len(val) - 1
	for e >= 0 && val[e].k == tWS {
		e--
	}
	if e >= 0 && val[e].ident("important") {
		f := e - 1
		for f >= 0 && val[f].k == tWS {
			f--
		}
		if f >= 0 && val[f].is('!') {
			imp = true
			val = val[:f]
		}
	}
	text := tokText(src, val)
	if !strings.HasPrefix(name, "--") {
		name = strings.ToLower(name)
		if text == "" {
			return out
		}
		// a stray "!" or top-level {} block makes the declaration invalid
		for _, t := range val {
			if t.is('!') || t.k == tLBrace || t.k == tBadString || t.k == tBadURL {
				return out
			}
		}
	}
	return expandDecl(name, text, imp, out)
}

// ParseDecls parses a declaration block ("color: red; margin: 0"), as in
// a style attribute. Shorthands are expanded and invalid declarations
// dropped.
func ParseDecls(src string) Decls {
	toks := tokenize(src)
	var out Decls
	i := 0
	for i < len(toks) {
		if toks[i].k == tWS || toks[i].k == tSemicolon {
			i++
			continue
		}
		j := scanTo(toks, i, tSemicolon)
		out = parseDecl(src, toks[i:j], out)
		i = j + 1
	}
	return out
}

// parseCVsTok builds component values from already tokenized input.
func parseCVsTok(ts []token) []cv {
	i := 0
	return cvList(ts, &i, tEOF)
}

// --- @media ---

type mediaList struct {
	qs []mediaQuery // OR
}

type mediaQuery struct {
	not  bool
	typ  string // "" or "all" = all
	cond *mcond
	bad  bool // unparseable: never matches
}

type mcond struct {
	op   byte // 'f' feature, '!' not, '&' and, '|' or, '0' false
	kids []*mcond
	name string // feature name, lowercase, without min-/max-
	// comparisons: value op name (op stored relative to the feature: name OP v)
	cmps    []mcmp
	boolean bool // (name) boolean context
}

type mcmp struct {
	op byte // '<' '>' '=' 'l' (<=) 'g' (>=)
	v  mval
}

type mval struct {
	num   float64
	kind  byte // 'n' number, 'l' length px, 'r' ratio, 'i' ident, 'd' resolution dppx
	ident string
}

func parseMediaList(src string, prelude []token) *mediaList {
	ml := &mediaList{}
	if len(trimWS(prelude)) == 0 {
		ml.qs = []mediaQuery{{}}
		return ml
	}
	for _, part := range splitTopComma(prelude) {
		cs := parseCVsTok(part)
		ml.qs = append(ml.qs, parseMediaQuery(src, cs))
	}
	return ml
}

func parseMediaQuery(src string, cs []cv) mediaQuery {
	var q mediaQuery
	if len(cs) == 0 {
		q.bad = true
		return q
	}
	if cs[0].t.k == tIdent {
		w := strings.ToLower(cs[0].t.s)
		if w == "not" && len(cs) > 1 && cs[1].t.k == tIdent {
			q.not = true
			cs = cs[1:]
			w = strings.ToLower(cs[0].t.s)
		} else if w == "only" && len(cs) > 1 && cs[1].t.k == tIdent {
			cs = cs[1:]
			w = strings.ToLower(cs[0].t.s)
		}
		if w != "not" {
			if w == "and" || w == "or" || w == "only" {
				q.bad = true
				return q
			}
			q.typ = w
			cs = cs[1:]
			if len(cs) == 0 {
				return q
			}
			if !cs[0].t.ident("and") || len(cs) < 2 {
				q.bad = true
				return q
			}
			cs = cs[1:]
		}
	}
	c, ok := parseMCond(src, cs)
	if !ok {
		q.bad = true
		return q
	}
	q.cond = c
	return q
}

func parseMCond(src string, cs []cv) (*mcond, bool) {
	if len(cs) == 0 {
		return nil, false
	}
	if cs[0].t.ident("not") {
		if len(cs) != 2 {
			return nil, false
		}
		k, ok := parseMInParens(src, &cs[1])
		if !ok {
			return nil, false
		}
		return &mcond{op: '!', kids: []*mcond{k}}, true
	}
	first, ok := parseMInParens(src, &cs[0])
	if !ok {
		return nil, false
	}
	if len(cs) == 1 {
		return first, true
	}
	var op byte
	out := &mcond{kids: []*mcond{first}}
	for i := 1; i < len(cs); i += 2 {
		if i+1 >= len(cs) || cs[i].t.k != tIdent {
			return nil, false
		}
		var o byte
		switch strings.ToLower(cs[i].t.s) {
		case "and":
			o = '&'
		case "or":
			o = '|'
		default:
			return nil, false
		}
		if op != 0 && o != op {
			return nil, false
		}
		op = o
		k, ok := parseMInParens(src, &cs[i+1])
		if !ok {
			return nil, false
		}
		out.kids = append(out.kids, k)
	}
	out.op = op
	return out, true
}

func parseMInParens(src string, c *cv) (*mcond, bool) {
	if c.t.k == tFunction {
		return &mcond{op: '0'}, true // general enclosed
	}
	if c.t.k != tLParen {
		return nil, false
	}
	in := c.args
	if len(in) == 0 {
		return nil, false
	}
	if in[0].t.k == tLParen || in[0].t.ident("not") {
		return parseMCond(src, in)
	}
	f, ok := parseMFeature(in)
	if !ok {
		return &mcond{op: '0'}, true // unknown syntax: general enclosed → false
	}
	return f, true
}

func parseMValue(cs []cv) (mval, bool) {
	if len(cs) == 3 && cs[1].t.is('/') && cs[0].t.k == tNumber && cs[2].t.k == tNumber {
		if cs[2].t.num == 0 {
			return mval{}, false
		}
		return mval{kind: 'r', num: cs[0].t.num / cs[2].t.num}, true
	}
	if len(cs) != 1 {
		return mval{}, false
	}
	t := cs[0].t
	switch t.k {
	case tNumber:
		return mval{kind: 'n', num: t.num}, true
	case tIdent:
		return mval{kind: 'i', ident: strings.ToLower(t.s)}, true
	case tDimension:
		u := strings.ToLower(t.s)
		switch u {
		case "dppx", "x":
			return mval{kind: 'd', num: t.num}, true
		case "dpi":
			return mval{kind: 'd', num: t.num / 96}, true
		case "dpcm":
			return mval{kind: 'd', num: t.num * 2.54 / 96}, true
		}
		if px, ok := absLen(t.num, u, 16); ok {
			return mval{kind: 'l', num: px}, true
		}
	}
	return mval{}, false
}

// absLen converts an absolute or font-relative length (em = fs) to px.
func absLen(v float64, unit string, fs float64) (float64, bool) {
	switch unit {
	case "px":
		return v, true
	case "em", "rem":
		return v * fs, true
	case "ex", "ch", "rex", "rch", "cap", "ic", "lh", "rlh":
		return v * fs * 0.5, true
	case "pt":
		return v * 96 / 72, true
	case "pc":
		return v * 16, true
	case "in":
		return v * 96, true
	case "cm":
		return v * 96 / 2.54, true
	case "mm":
		return v * 96 / 25.4, true
	case "q":
		return v * 96 / 101.6, true
	}
	return 0, false
}

func flipOp(o byte) byte {
	switch o {
	case '<':
		return '>'
	case '>':
		return '<'
	case 'l':
		return 'g'
	case 'g':
		return 'l'
	}
	return o
}

// rangeOp reads a comparison operator at cs[i] (one or two delims).
func rangeOp(cs []cv, i int) (byte, int) {
	if i >= len(cs) || cs[i].t.k != tDelim {
		return 0, 0
	}
	c := cs[i].t.s
	eq := i+1 < len(cs) && cs[i+1].t.is('=') && cs[i+1].t.start == cs[i].t.end
	switch c {
	case "<":
		if eq {
			return 'l', 2
		}
		return '<', 1
	case ">":
		if eq {
			return 'g', 2
		}
		return '>', 1
	case "=":
		return '=', 1
	}
	return 0, 0
}

func parseMFeature(in []cv) (*mcond, bool) {
	// (name) / (name: value)
	if in[0].t.k == tIdent && (len(in) == 1 || in[1].t.k == tColon) {
		name := strings.ToLower(in[0].t.s)
		if len(in) == 1 {
			return &mcond{op: 'f', name: name, boolean: true}, true
		}
		v, ok := parseMValue(in[2:])
		if !ok {
			return nil, false
		}
		op := byte('=')
		if strings.HasPrefix(name, "min-") {
			op, name = 'g', name[4:]
		} else if strings.HasPrefix(name, "max-") {
			op, name = 'l', name[4:]
		} else if strings.HasPrefix(name, "-webkit-min-") {
			op, name = 'g', "-webkit-"+name[12:]
		} else if strings.HasPrefix(name, "-webkit-max-") {
			op, name = 'l', "-webkit-"+name[12:]
		}
		return &mcond{op: 'f', name: name, cmps: []mcmp{{op: op, v: v}}}, true
	}
	// range forms: the feature name is the ident
	idx := -1
	for i := range in {
		if in[i].t.k == tIdent {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, false
	}
	f := &mcond{op: 'f', name: strings.ToLower(in[idx].t.s)}
	// right side: name op v
	if idx+1 < len(in) {
		op, n := rangeOp(in, idx+1)
		if op == 0 {
			return nil, false
		}
		v, ok := parseMValue(in[idx+1+n:])
		if !ok {
			return nil, false
		}
		f.cmps = append(f.cmps, mcmp{op: op, v: v})
	}
	// left side: v op name
	if idx > 0 {
		// find the operator just before the name
		j := idx - 1
		for j > 0 && cs0delim(in[j-1]) && in[j].t.is('=') {
			j--
		}
		op, n := rangeOp(in, j)
		if op == 0 || j+n != idx {
			return nil, false
		}
		v, ok := parseMValue(in[:j])
		if !ok {
			return nil, false
		}
		f.cmps = append(f.cmps, mcmp{op: flipOp(op), v: v})
	}
	return f, len(f.cmps) > 0
}

func cs0delim(c cv) bool { return c.t.k == tDelim && (c.t.s == "<" || c.t.s == ">") }

func (ml *mediaList) match(env Env) bool {
	for i := range ml.qs {
		if ml.qs[i].match(env) {
			return true
		}
	}
	return false
}

func (q *mediaQuery) match(env Env) bool {
	if q.bad {
		return false
	}
	r := true
	switch q.typ {
	case "", "all", "screen":
	default:
		r = false
	}
	if r && q.cond != nil {
		r = q.cond.eval(env)
	}
	if q.not {
		return !r
	}
	return r
}

func (c *mcond) eval(env Env) bool {
	switch c.op {
	case '0':
		return false
	case '!':
		return !c.kids[0].eval(env)
	case '&':
		for _, k := range c.kids {
			if !k.eval(env) {
				return false
			}
		}
		return true
	case '|':
		for _, k := range c.kids {
			if k.eval(env) {
				return true
			}
		}
		return false
	case 'f':
		return c.feature(env)
	}
	if len(c.kids) == 1 {
		return c.kids[0].eval(env)
	}
	return false
}

func cmpNum(a float64, op byte, b float64) bool {
	switch op {
	case '<':
		return a < b
	case '>':
		return a > b
	case 'l':
		return a <= b
	case 'g':
		return a >= b
	}
	return a == b
}

func (c *mcond) feature(env Env) bool {
	w, h := float64(env.Width), float64(env.Height)
	numeric := func(have float64, kind byte) bool {
		if c.boolean {
			return have != 0
		}
		for _, m := range c.cmps {
			if m.v.kind != kind && !(kind == 'l' && m.v.kind == 'n' && m.v.num == 0) && !(kind == 'r' && m.v.kind == 'n') && !(kind == 'n' && m.v.kind == 'n') {
				return false
			}
			if !cmpNum(have, m.op, m.v.num) {
				return false
			}
		}
		return true
	}
	ident := func(have ...string) bool {
		if c.boolean {
			return have[0] != "none" && have[0] != "no-preference"
		}
		if len(c.cmps) != 1 || c.cmps[0].op != '=' || c.cmps[0].v.kind != 'i' {
			return false
		}
		for _, h := range have {
			if c.cmps[0].v.ident == h {
				return true
			}
		}
		return false
	}
	switch c.name {
	case "width", "device-width", "inline-size":
		return numeric(w, 'l')
	case "height", "device-height", "block-size":
		return numeric(h, 'l')
	case "aspect-ratio", "device-aspect-ratio":
		if h == 0 {
			return false
		}
		return numeric(w/h, 'r')
	case "orientation":
		if h >= w {
			return ident("portrait")
		}
		return ident("landscape")
	case "prefers-color-scheme":
		if env.Dark {
			return ident("dark")
		}
		return ident("light")
	case "prefers-reduced-motion":
		if env.ReduceMotion {
			return ident("reduce")
		}
		return ident("no-preference")
	case "hover", "any-hover":
		return ident("hover")
	case "pointer", "any-pointer":
		return ident("fine")
	case "resolution":
		return numeric(1, 'd')
	case "-webkit-device-pixel-ratio":
		return numeric(1, 'n')
	case "color":
		return numeric(8, 'n')
	case "color-index", "monochrome", "grid":
		return numeric(0, 'n')
	case "color-gamut":
		return ident("srgb")
	case "dynamic-range", "video-dynamic-range":
		return ident("standard")
	case "display-mode":
		return ident("browser")
	case "forced-colors", "inverted-colors":
		return ident("none")
	case "prefers-contrast", "prefers-reduced-transparency", "prefers-reduced-data":
		return ident("no-preference")
	case "scripting":
		return ident("enabled")
	case "update":
		return ident("fast")
	case "scan":
		return ident("progressive")
	case "overflow-block":
		return ident("scroll")
	case "overflow-inline":
		return ident("scroll")
	}
	return false
}

// --- @supports ---

func evalSupports(src string, cs []cv) bool {
	if len(cs) == 0 {
		return false
	}
	if cs[0].t.ident("not") {
		return len(cs) == 2 && !supportsInParens(src, &cs[1])
	}
	r := supportsInParens(src, &cs[0])
	var op string
	for i := 1; i+1 < len(cs); i += 2 {
		if cs[i].t.k != tIdent {
			return false
		}
		o := strings.ToLower(cs[i].t.s)
		if o != "and" && o != "or" || op != "" && o != op {
			return false
		}
		op = o
		k := supportsInParens(src, &cs[i+1])
		if o == "and" {
			r = r && k
		} else {
			r = r || k
		}
	}
	return r
}

func supportsInParens(src string, c *cv) bool {
	if c.t.k == tFunction {
		if c.name == "selector" {
			txt := c.text(src)
			if i := strings.IndexByte(txt, '('); i >= 0 && len(txt) > i+1 {
				inner := strings.TrimSuffix(txt[i+1:], ")")
				_, ok := parseSelectorList(trimWS(tokenize(inner)), nil, false, false, false)
				return ok
			}
		}
		return false
	}
	if c.t.k != tLParen || len(c.args) == 0 {
		return false
	}
	in := c.args
	if in[0].t.k == tLParen || in[0].t.ident("not") || in[0].t.k == tFunction {
		return evalSupports(src, in)
	}
	if in[0].t.k == tIdent && len(in) >= 3 && in[1].t.k == tColon {
		name := in[0].t.s
		val := src[in[2].t.start:in[len(in)-1].end]
		val = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(val), "!important"))
		if !strings.HasPrefix(name, "--") {
			name = strings.ToLower(name)
		}
		return len(expandDecl(name, val, false, nil)) > 0
	}
	return false
}
