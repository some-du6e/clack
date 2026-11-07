package css

import (
	"hash/fnv"
	"image/color"
	"sort"
	"strings"
)

// RootTokens are the custom properties in scope at the document root:
// Slack's own design tokens (SlackVars, unresolved so overrides propagate
// through their var() chains) with every custom property the active
// sheets declare on html, body (.sk-client-theme--dark/--light) or
// .p-client_container laid over them. A Slick theme's
// `:root,html,body,.sk-client-theme--dark,…{--dt_color-base-pry:…}` lands
// here, and so does a user.css `body{--x:…}`.
//
// Build one with Engine.RootTokens when sheets change, not per frame; a
// RootTokens memoizes lookups and is for one goroutine.
type RootTokens struct {
	raw  map[string]string // Slack's raw tokens + sheet overrides
	own  map[string]bool   // names a sheet declared
	memo map[string]tokenVal
	dark bool
	// copied: raw is this RootTokens' own map (else shared, read-only)
	copied bool

	eng    *Engine
	env    Env
	client *rootEl
	chain  []Style // computed html, body, .p-client_container (lazy)
}

type tokenVal struct {
	v       string
	ok, own bool
}

// rootEl is a synthetic root-chain element (html > body > .p-client_container).
type rootEl struct {
	tag     string
	classes []string
	parent  *rootEl
}

func (e *rootEl) Tag() string { return e.tag }
func (e *rootEl) ID() string  { return "" }
func (e *rootEl) HasClass(c string) bool {
	for _, x := range e.classes {
		if x == c {
			return true
		}
	}
	return false
}
func (e *rootEl) Classes() []string { return e.classes }
func (e *rootEl) Attr(name string) (string, bool) {
	if name == "class" {
		return strings.Join(e.classes, " "), len(e.classes) > 0
	}
	return "", false
}
func (e *rootEl) Parent() Element {
	if e.parent == nil {
		return nil
	}
	return e.parent
}
func (e *rootEl) Prev() Element         { return nil }
func (e *rootEl) Index() (int, int)     { return 0, 0 }
func (e *rootEl) TypeIndex() (int, int) { return 0, 0 }
func (e *rootEl) State() State          { return 0 }

// RootTokens cascades the root chain against the active sheets. env.Dark
// picks Slack's light or dark token set and body's sk-client-theme class;
// env.RootVars is ignored.
func (e *Engine) RootTokens(env Env) *RootTokens {
	base := slackVarsLight
	mode := "sk-client-theme--light"
	if env.Dark {
		base = slackVarsDark
		mode = "sk-client-theme--dark"
	}
	html := &rootEl{tag: "html"}
	body := &rootEl{tag: "body", classes: []string{mode}, parent: html}
	client := &rootEl{tag: "div", classes: []string{"p-client_container"}, parent: body}
	env.RootVars = SlackVars(env.Dark)
	t := &RootTokens{raw: base, memo: map[string]tokenVal{}, dark: env.Dark, eng: e, env: env, client: client}
	if e.Empty() {
		return t
	}
	for _, el := range []*rootEl{html, body, client} {
		t.overlay(el)
	}
	return t
}

// overlay lays the custom properties the sheets declare on el over raw.
func (t *RootTokens) overlay(el *rootEl) {
	c := t.eng.Cascade(el, PseudoNone, nil, t.env)
	for _, d := range c.Decls {
		if !strings.HasPrefix(d.Prop, "--") {
			continue
		}
		v := strings.TrimSpace(d.Value)
		switch wideKeyword(v) {
		case "inherit", "unset", "revert", "revert-layer":
			continue // keeps the parent's (already in raw)
		}
		if t.own == nil {
			t.own = map[string]bool{}
		}
		if !t.copied {
			m := make(map[string]string, len(t.raw)+16)
			for k, x := range t.raw {
				m[k] = x
			}
			t.raw, t.copied = m, true
		}
		t.raw[d.Prop] = v
		t.own[d.Prop] = true
	}
}

// Scope returns the tokens in scope inside a div with the classes of path
// (as in Background): a sheet can set Slack's tokens on a subtree, e.g.
// `.p-view_contents--sidebar{--dt_color-content-pry:#a00}`. Values are
// the root's with the subtree's declarations over them, but own (in
// Value/Color) only counts names declared on path itself. With nothing
// declared on path it is t itself.
func (t *RootTokens) Scope(path ...string) *RootTokens {
	if t == nil || t.eng.Empty() {
		return t
	}
	s := &RootTokens{raw: t.raw, dark: t.dark, eng: t.eng, env: t.env, client: t.client, chain: t.chain}
	pel := t.client
	for _, cls := range path {
		el := &rootEl{tag: "div", classes: strings.Fields(cls), parent: pel}
		s.overlay(el)
		pel = el
	}
	if len(s.own) == 0 {
		return t
	}
	s.memo = map[string]tokenVal{}
	return s
}

// Background is the background color a sheet gives a div with the
// classes of path (one space-separated class list per element, outermost
// first) under html > body > .p-client_container. ok is false when no
// rule sets one on the last element or it is fully transparent (a lone
// gradient). A gradient over a color yields the color.
func (t *RootTokens) Background(path ...string) (c color.NRGBA, ok bool) {
	if t == nil || t.eng.Empty() || len(path) == 0 {
		return color.NRGBA{}, false
	}
	if t.chain == nil {
		var parent *Style
		for _, el := range []*rootEl{t.client.parent.parent, t.client.parent, t.client} {
			c := t.eng.Cascade(el, PseudoNone, nil, t.env)
			st := Compute(&c, parent, t.env)
			t.chain = append(t.chain, st)
			parent = &t.chain[len(t.chain)-1]
		}
	}
	parent := &t.chain[len(t.chain)-1]
	pel := t.client
	var c Cascaded
	var st Style
	for _, cls := range path {
		el := &rootEl{tag: "div", classes: strings.Fields(cls), parent: pel}
		c = t.eng.Cascade(el, PseudoNone, nil, t.env)
		st = Compute(&c, parent, t.env)
		parent, pel = &st, el
	}
	if !c.Has("background-color") && !c.Has("background") {
		return color.NRGBA{}, false
	}
	if st.Background.Color.A == 0 {
		return color.NRGBA{}, false
	}
	return st.Background.Color, true
}

// Key fingerprints the sheet overrides (0: none), so callers can tell
// whether a rebuilt RootTokens changes anything.
func (t *RootTokens) Key() uint64 {
	if t == nil || len(t.own) == 0 {
		return 0
	}
	names := make([]string, 0, len(t.own))
	for k := range t.own {
		names = append(names, k)
	}
	sort.Strings(names)
	h := fnv.New64a()
	if t.dark {
		h.Write([]byte{1})
	}
	for _, k := range names {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(t.raw[k]))
		h.Write([]byte{0})
	}
	return h.Sum64() | 1
}

// Overridden reports that some sheet declared a root custom property.
func (t *RootTokens) Overridden() bool { return t != nil && len(t.own) > 0 }

// lookup resolves one token; own reports that a sheet's declaration was
// used for it or anywhere in its var() chain.
func (t *RootTokens) lookup(name string, depth int) tokenVal {
	if r, ok := t.memo[name]; ok {
		return r
	}
	raw, ok := t.raw[name]
	if !ok || depth > 24 || wideKeyword(raw) == "initial" {
		return tokenVal{}
	}
	r := tokenVal{v: raw, ok: true, own: t.own[name]}
	if hasVar(raw) {
		t.memo[name] = tokenVal{} // cycle guard
		v, ok := substVars(raw, func(n string) (string, bool) {
			x := t.lookup(n, depth+1)
			r.own = r.own || x.own
			return x.v, x.ok
		}, depth+1)
		r.v, r.ok = v, ok
	}
	t.memo[name] = r
	return r
}

// Value substitutes the tokens into expr ("var(--dt_color-base-pry)",
// "rgb(var(--sk_primary_background))"). own reports that a sheet's
// declaration took part, so callers can keep their measured defaults when
// nothing overrides the tokens expr reads.
func (t *RootTokens) Value(expr string) (v string, own, ok bool) {
	if t == nil {
		return "", false, false
	}
	if !hasVar(expr) {
		return expr, false, true
	}
	v, ok = substVars(expr, func(n string) (string, bool) {
		x := t.lookup(n, 0)
		own = own || x.own
		return x.v, x.ok
	}, 0)
	return v, own, ok
}

// Color resolves expr to a color. own is as in Value; ok is false when
// expr doesn't resolve to a single valid <color>.
func (t *RootTokens) Color(expr string) (c color.NRGBA, own, ok bool) {
	v, own, ok := t.Value(expr)
	if !ok {
		return color.NRGBA{}, own, false
	}
	c, ok = parseColor(v, t.dark)
	return c, own, ok
}

// ParseColor parses one CSS <color> (hex, named, rgb()/hsl()/…, no var()).
// A bare "r,g,b" triple, the form of Slack's --sk_* and palette tokens, is
// read as rgb(r,g,b).
func ParseColor(s string) (color.NRGBA, bool) { return parseColor(s, false) }

// parseColor is ParseColor with light-dark() picking dark's side.
func parseColor(s string, dark bool) (color.NRGBA, bool) {
	s = strings.TrimSpace(s)
	if s == "" || hasVar(s) {
		return color.NRGBA{}, false
	}
	if s[0] >= '0' && s[0] <= '9' && strings.Count(s, ",") == 2 {
		s = "rgb(" + s + ")"
	}
	cs := parseCVs(s)
	if len(cs) != 1 {
		return color.NRGBA{}, false
	}
	st := Initial(color.NRGBA{A: 0xff})
	cx := cctx{st: &st, env: Env{Dark: dark}}
	return cx.color(&cs[0])
}
