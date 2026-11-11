package css

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// engineImpl holds the sheets (insertion order) and the compiled,
// immutable snapshot that Cascade reads without locking.
type engineImpl struct {
	mu       sync.Mutex // serializes Set/Remove
	sheets   []keyedSheet
	base     *sheet // SetBase: first in the cascade, never makes the engine non-empty
	hostBase *sheet // SetHostBase: after base; its rules match host-tree nodes only
	snap     atomic.Pointer[snapshot]
}

type keyedSheet struct {
	key string
	sh  *sheet
}

// entry is one complex selector of one rule, ready to match.
type entry struct {
	sel   *complexSel
	r     *rule
	key   uint64 // normal-importance sort key: layer rank, specificity, order
	ikey  uint64 // !important sort key (layer order reversed)
	multi bool   // indexed under several keys (dedupe on match)
	imp   bool   // the rule has !important declarations
	norm  bool   // the rule has normal declarations
	host  bool   // from the host base sheet: the subject must be a HostElement
}

// ruleIndex buckets entries by the subject compound's most selective
// simple selector.
type ruleIndex struct {
	ids       map[string][]*entry
	classes   map[string][]*entry
	tags      map[string][]*entry
	attrs     map[string][]*entry
	attrNames []string
	universal []*entry
}

type snapshot struct {
	idx     [PseudoBackdrop + 1]ruleIndex // per Pseudo
	layered bool                          // some rule is in a layer (important order differs)
	// the host base sheet's subject classes (HostStyled); hostAll: some
	// subject isn't keyed by a class, so any host node may match
	hostClasses map[string]bool
	hostAll     bool
}

// NewEngine returns an empty engine.
func NewEngine() *Engine {
	e := &Engine{}
	e.empty.Store(true)
	return e
}

// Set installs or replaces the sheet under key. A replaced sheet keeps
// its original position in the cascade order.
func (e *Engine) Set(key, src string) {
	sh := parseSheet(src)
	im := &e.impl
	im.mu.Lock()
	defer im.mu.Unlock()
	found := false
	for i := range im.sheets {
		if im.sheets[i].key == key {
			im.sheets[i].sh = sh
			found = true
			break
		}
	}
	if !found {
		im.sheets = append(im.sheets, keyedSheet{key: key, sh: sh})
	}
	e.rebuild()
}

// SetBase installs the base sheet: the page's own stylesheet, which comes
// before every keyed sheet in source order and competes with them by
// importance and specificity as usual. It doesn't count for Empty: with
// only the base sheet installed, callers skip all CSS work. Clack uses it
// for Slack's own rules on native widgets (value NativeValue: "the
// widget's own value"), so a plugin rule beats them exactly when it would
// beat Slack's stylesheet in the browser.
func (e *Engine) SetBase(src string) {
	sh := parseSheet(src)
	im := &e.impl
	im.mu.Lock()
	defer im.mu.Unlock()
	im.base = sh
	e.rebuild()
}

// SetHostBase installs the host base sheet: Slack's own rules for the
// Slack classes plugins put on elements they render themselves (a
// plugin's button with c-reaction_add, a header button with
// p-top_nav__button). It comes after the base sheet in source order and
// competes like it, but its rules match only host-tree nodes (elements
// whose HostNode reports true): a native widget with the same class
// draws its own look, and the rule would style it twice. It doesn't
// count for Empty either.
func (e *Engine) SetHostBase(src string) {
	sh := parseSheet(src)
	im := &e.impl
	im.mu.Lock()
	defer im.mu.Unlock()
	im.hostBase = sh
	e.rebuild()
}

// HostElement is implemented by host-tree nodes, the elements a plugin's
// React tree renders; the host base sheet's rules match only them.
type HostElement interface {
	HostNode() bool
}

func isHost(el Element) bool {
	h, ok := el.(HostElement)
	return ok && h.HostNode()
}

// HostStyled reports whether a host base rule may apply to el (a host
// node): one of its classes is a host rule's subject class. A host node
// cascades then even while Empty, so a plugin's Slack-classed element
// looks like Slack's with no other CSS installed.
func (e *Engine) HostStyled(el Element) bool {
	if e == nil {
		return false
	}
	s := e.impl.snap.Load()
	if s == nil || s.hostClasses == nil && !s.hostAll {
		return false
	}
	if s.hostAll {
		return true
	}
	for _, c := range el.Classes() {
		if s.hostClasses[c] {
			return true
		}
	}
	return false
}

// NativeValue is a base-sheet value meaning "the native widget's own
// value": the widget keeps its built-in look for the property, and its
// descendants don't inherit an ancestor's CSS value through it. It is
// invalid for Compute (the property computes as unset).
const NativeValue = "-clack-native"

// Remove drops the sheet under key.
func (e *Engine) Remove(key string) {
	im := &e.impl
	im.mu.Lock()
	defer im.mu.Unlock()
	for i := range im.sheets {
		if im.sheets[i].key == key {
			im.sheets = append(im.sheets[:i:i], im.sheets[i+1:]...)
			break
		}
	}
	e.rebuild()
}

// rebuild compiles the snapshot from all sheets (mu held).
func (e *Engine) rebuild() {
	im := &e.impl
	// layer order across sheets: a tree, ranked post-order (sublayers
	// before their parent's own rules; unlayered last)
	type lnode struct {
		kids []string
	}
	all := im.sheets
	if im.base != nil || im.hostBase != nil {
		all = make([]keyedSheet, 0, len(im.sheets)+2)
		if im.base != nil {
			all = append(all, keyedSheet{key: "\x00base", sh: im.base})
		}
		if im.hostBase != nil {
			all = append(all, keyedSheet{key: "\x00host", sh: im.hostBase})
		}
		all = append(all, im.sheets...)
	}
	// the base sheets' layers (Slack's sk.*) come first, as Slack's own
	// stylesheet declares them before any plugin's
	tree := map[string]*lnode{"": {}}
	for _, ks := range all {
		for _, path := range ks.sh.layers {
			if _, ok := tree[path]; ok {
				continue
			}
			parent := ""
			if i := strings.LastIndexByte(path, '.'); i >= 0 {
				parent = path[:i]
			}
			if tree[parent] == nil {
				tree[parent] = &lnode{}
			}
			tree[parent].kids = append(tree[parent].kids, path)
			tree[path] = &lnode{}
		}
	}
	rank := map[string]uint64{}
	var next uint64
	var walk func(p string)
	walk = func(p string) {
		for _, k := range tree[p].kids {
			walk(k)
		}
		rank[p] = next
		next++
	}
	walk("")
	maxRank := next - 1

	s := &snapshot{}
	for i := range s.idx {
		s.idx[i] = ruleIndex{ids: map[string][]*entry{}, classes: map[string][]*entry{}, tags: map[string][]*entry{}, attrs: map[string][]*entry{}}
	}
	var order uint64
	total := 0
	for _, ks := range all {
		for _, r := range ks.sh.rules {
			order++
			lr, ok := rank[r.layer]
			if !ok {
				lr = maxRank
			}
			if r.layer != "" {
				s.layered = true
			}
			var hasImp, hasNorm bool
			for _, d := range r.decls {
				if d.Important {
					hasImp = true
				} else {
					hasNorm = true
				}
			}
			for _, sel := range r.sels {
				if sel.never || sel.pseudo == pseudoNever {
					continue
				}
				ord := min(order, 1<<24-1)
				en := &entry{sel: sel, r: r, imp: hasImp, norm: hasNorm, host: ks.sh == im.hostBase,
					key:  lr<<48 | uint64(sel.spec)<<24 | ord,
					ikey: (maxRank-lr)<<48 | uint64(sel.spec)<<24 | ord}
				s.idx[sel.pseudo].add(en)
				if en.host {
					if k, keys := bucketKeys(&sel.comps[0]); k == 'c' {
						if s.hostClasses == nil {
							s.hostClasses = map[string]bool{}
						}
						for _, c := range keys {
							s.hostClasses[c] = true
						}
					} else {
						s.hostAll = true
					}
				}
				if ks.sh != im.base && ks.sh != im.hostBase {
					total++
				}
			}
		}
	}
	for i := range s.idx {
		s.idx[i].finish()
	}
	im.snap.Store(s)
	e.empty.Store(total == 0)
	e.ver.Add(1)
}

// bucketKeys returns the index keys for a compound: one id, class, attr
// or tag; for a compound that is only :is()/:where() of indexable
// selectors (CSS nesting's &), the union of their keys.
func bucketKeys(c *compound) (kind byte, keys []string) {
	switch {
	case c.id != "":
		return 'i', []string{c.id}
	case len(c.classes) > 0:
		return 'c', []string{c.classes[0]}
	case len(c.attrs) > 0:
		return 'a', []string{c.attrs[0].name}
	case c.tag != "":
		return 't', []string{c.tag}
	}
	for i := range c.pcs {
		pc := &c.pcs[i]
		if pc.kind != pcIs || len(pc.sels) == 0 {
			continue
		}
		var k0 byte
		var all []string
		ok := true
		for _, s := range pc.sels {
			k, ks := bucketKeys(&s.comps[0])
			if k == 0 || k0 != 0 && k != k0 || s.never {
				ok = false
				break
			}
			k0 = k
			all = append(all, ks...)
		}
		if ok {
			return k0, all
		}
	}
	return 0, nil
}

func (ix *ruleIndex) add(en *entry) {
	kind, keys := bucketKeys(&en.sel.comps[0])
	en.multi = len(keys) > 1
	var m map[string][]*entry
	switch kind {
	case 'i':
		m = ix.ids
	case 'c':
		m = ix.classes
	case 'a':
		m = ix.attrs
	case 't':
		m = ix.tags
	default:
		ix.universal = append(ix.universal, en)
		return
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		m[k] = append(m[k], en)
	}
}

func (ix *ruleIndex) finish() {
	for k := range ix.attrs {
		ix.attrNames = append(ix.attrNames, k)
	}
	if len(ix.ids) == 0 {
		ix.ids = nil
	}
	if len(ix.classes) == 0 {
		ix.classes = nil
	}
	if len(ix.tags) == 0 {
		ix.tags = nil
	}
}

type match struct {
	en  *entry
	key uint64
}

// Cascade returns the winning declaration per longhand for el (or its
// pseudo-element p): !important first, then inline over author rules,
// then cascade layers, specificity and source order.
func (e *Engine) Cascade(el Element, p Pseudo, inline Decls, env Env) Cascaded {
	var s *snapshot
	if e != nil {
		s = e.impl.snap.Load()
	}
	var buf [24]match
	ms := buf[:0]
	if s != nil && int(p) < len(s.idx) {
		ms = s.idx[p].collect(el, env, ms)
	}
	if len(ms) == 0 {
		if len(inline) == 0 {
			return Cascaded{}
		}
		return Cascaded{Decls: resolveContentAttr(inlineOnly(inline), el, true)}
	}
	sortMatches(ms)
	n := len(inline)
	for _, m := range ms {
		n += len(m.en.r.decls)
	}
	w := winners{out: make(Decls, 0, min(n, 64))}
	for _, m := range ms {
		if m.en.norm {
			for _, d := range m.en.r.decls {
				if !d.Important {
					w.set(d)
				}
			}
		}
	}
	for _, d := range expandInline(inline) {
		if !d.Important {
			w.set(d)
		}
	}
	anyImp := false
	for _, m := range ms {
		if m.en.imp {
			anyImp = true
			break
		}
	}
	if anyImp {
		if s.layered {
			for i := range ms {
				ms[i].key = ms[i].en.ikey
			}
			sortMatches(ms)
		}
		for _, m := range ms {
			if m.en.imp {
				for _, d := range m.en.r.decls {
					if d.Important {
						w.set(d)
					}
				}
			}
		}
	}
	for _, d := range expandInline(inline) {
		if d.Important {
			w.set(d)
		}
	}
	return Cascaded{Decls: resolveContentAttr(w.out, el, false)}
}

// resolveContentAttr substitutes attr() in a winning content value (the
// element is only known here). shared: ds may alias the caller's slice.
func resolveContentAttr(ds Decls, el Element, shared bool) Decls {
	for i := range ds {
		if ds[i].Prop == "content" && strings.Contains(ds[i].Value, "attr(") {
			if shared {
				ds = append(Decls(nil), ds...)
			}
			ds[i].Value = resolveAttr(ds[i].Value, el)
			return ds
		}
	}
	return ds
}

// cssQuote writes s as a CSS string literal.
func cssQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString("\\a ")
		case r < 0x20 || r == 0x7f:
			b.WriteString("\\" + strconv.FormatInt(int64(r), 16) + " ")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// collect appends the entries matching el.
func (ix *ruleIndex) collect(el Element, env Env, ms []match) []match {
	try := func(list []*entry) {
		for _, en := range list {
			if en.multi {
				dup := false
				for _, m := range ms {
					if m.en == en {
						dup = true
						break
					}
				}
				if dup {
					continue
				}
			}
			if en.host && !isHost(el) || !matchSel(en.sel, el, nil) {
				continue
			}
			ok := true
			for _, ml := range en.r.media {
				if !ml.match(env) {
					ok = false
					break
				}
			}
			if ok {
				ms = append(ms, match{en: en, key: en.key})
			}
		}
	}
	if ix.ids != nil {
		if id := el.ID(); id != "" {
			try(ix.ids[id])
		}
	}
	if ix.classes != nil {
		for _, c := range el.Classes() {
			if l := ix.classes[c]; l != nil {
				try(l)
			}
		}
	}
	for _, a := range ix.attrNames {
		if _, ok := el.Attr(a); ok {
			try(ix.attrs[a])
		}
	}
	if ix.tags != nil {
		if l := ix.tags[el.Tag()]; l != nil {
			try(l)
		}
	}
	if len(ix.universal) > 0 {
		try(ix.universal)
	}
	return ms
}

func sortMatches(ms []match) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && ms[j].key < ms[j-1].key; j-- {
			ms[j], ms[j-1] = ms[j-1], ms[j]
		}
	}
}

// winners keeps one declaration per property; later set calls win.
type winners struct {
	out Decls
	idx map[string]int
}

func (w *winners) set(d Decl) {
	if w.idx != nil {
		if i, ok := w.idx[d.Prop]; ok {
			w.out[i] = d
			return
		}
	} else {
		for i := range w.out {
			if w.out[i].Prop == d.Prop {
				w.out[i] = d
				return
			}
		}
	}
	w.out = append(w.out, d)
	if w.idx == nil && len(w.out) > 24 {
		w.idx = make(map[string]int, len(w.out)*2)
		for i := range w.out {
			w.idx[w.out[i].Prop] = i
		}
	} else if w.idx != nil {
		w.idx[d.Prop] = len(w.out) - 1
	}
}

// needsExpand reports inline declarations that are shorthands, aliases
// or duplicates (callers may pass raw declarations).
func needsExpand(ds Decls) bool {
	for i := range ds {
		p := ds[i].Prop
		if _, ok := shorthands[p]; ok {
			return true
		}
		if _, ok := propAlias[p]; ok {
			return true
		}
		if p == "all" {
			return true
		}
		for j := 0; j < i; j++ {
			if ds[j].Prop == p {
				return true
			}
		}
	}
	return false
}

func expandInline(ds Decls) Decls {
	if !needsExpand(ds) {
		return ds
	}
	var out Decls
	for _, d := range ds {
		if d.Shorthand != "" {
			out = append(out, d)
			continue
		}
		out = expandDecl(d.Prop, d.Value, d.Important, out)
	}
	return out
}

// inlineOnly is the cascade of inline declarations alone.
func inlineOnly(ds Decls) Decls {
	if len(ds) == 0 {
		return nil
	}
	ds = expandInline(ds)
	dup := false
	imp := false
	for i := range ds {
		if ds[i].Important {
			imp = true
		}
		for j := 0; j < i; j++ {
			if ds[j].Prop == ds[i].Prop {
				dup = true
			}
		}
	}
	if !dup {
		return ds
	}
	w := winners{}
	for _, d := range ds {
		if !d.Important {
			w.set(d)
		}
	}
	if imp {
		for _, d := range ds {
			if d.Important {
				w.set(d)
			}
		}
	}
	return w.out
}

// resolveAttr replaces attr(name [type] [, fallback]) in a content value
// with the element's attribute as a string.
func resolveAttr(v string, el Element) string {
	toks := tokenize(v)
	var b strings.Builder
	last := 0
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.k != tFunction || !strings.EqualFold(t.s, "attr") {
			continue
		}
		end := -1
		depth := 0
		for j := i + 1; j < len(toks); j++ {
			k := toks[j].k
			if k == tFunction || k == tLParen {
				depth++
			} else if k == tRParen {
				if depth == 0 {
					end = j
					break
				}
				depth--
			}
		}
		if end < 0 {
			break
		}
		inner := toks[i+1 : end]
		name, fallback := "", ""
		hasFallback := false
		for j, it := range inner {
			if it.k == tIdent && name == "" {
				name = strings.ToLower(it.s)
			}
			if it.k == tComma {
				fb := trimWS(inner[j+1:])
				if len(fb) == 1 && fb[0].k == tString {
					fallback = fb[0].s
				} else {
					fallback = tokText(v, fb)
				}
				hasFallback = true
				break
			}
		}
		val, ok := "", false
		if el != nil && name != "" {
			val, ok = el.Attr(name)
		}
		if !ok && hasFallback {
			val = fallback
		}
		b.WriteString(v[last:t.start])
		b.WriteString(cssQuote(val))
		last = toks[end].end
		i = end
	}
	b.WriteString(v[last:])
	return b.String()
}
