package slackc

import (
	"strings"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// The document's <html> and <body> elements as JS sees them: plugins put
// classes on documentElement (StreamerMode) and set custom properties
// through documentElement.style.setProperty (FasterSlack). They sit at
// the bottom of the element stack so `.streamer .p-channel_sidebar__name`
// and var(--x) work on native widgets and host trees alike.

type docEl struct {
	tag    string
	id     string
	class  []string
	attrs  Props
	inline css.Decls
	parent *docEl
}

func (e *docEl) Tag() string { return e.tag }
func (e *docEl) ID() string  { return e.id }
func (e *docEl) HasClass(c string) bool {
	for _, x := range e.class {
		if x == c {
			return true
		}
	}
	return false
}
func (e *docEl) Classes() []string { return e.class }
func (e *docEl) Attr(name string) (string, bool) {
	switch name {
	case "class":
		return strings.Join(e.class, " "), len(e.class) > 0
	case "id":
		return e.id, e.id != ""
	}
	n := &Node{P: e.attrs}
	return n.AttrString(name)
}
func (e *docEl) Parent() css.Element {
	if e.parent == nil {
		return nil
	}
	return e.parent
}
func (e *docEl) Prev() css.Element     { return nil }
func (e *docEl) Index() (int, int)     { return 0, 1 }
func (e *docEl) TypeIndex() (int, int) { return 0, 1 }
func (e *docEl) State() css.State      { return 0 }

var doc struct {
	sync.Mutex
	html, body *docEl
	ver        uint64
	// computed for the current Styles version / theme (UI goroutine)
	builtVer  uint64
	builtCSS  uint64
	builtDark bool
	hs, bs    css.Style
	hi, bi    propSet
}

// SetDocumentProps installs the "p" objects of <html> and <body> (JS →
// Go, any goroutine; same shape as host-tree props).
func SetDocumentProps(html, body []byte) error {
	hp, err := DecodeProps(html)
	if err != nil {
		return err
	}
	bp, err := DecodeProps(body)
	if err != nil {
		return err
	}
	h := docElFrom("html", hp, html)
	b := docElFrom("body", bp, body)
	b.parent = h
	doc.Lock()
	doc.html, doc.body = h, b
	doc.ver++
	doc.Unlock()
	resetNativeStyles()
	invalidate()
	return nil
}

func docElFrom(tag string, p Props, raw []byte) *docEl {
	e := &docEl{tag: tag, attrs: p}
	if p == nil {
		return e
	}
	if s, ok := p["className"].(string); ok {
		e.class = strings.Fields(s)
	} else if s, ok := p["class"].(string); ok {
		e.class = strings.Fields(s)
	}
	if s, ok := p["id"].(string); ok {
		e.id = s
	}
	switch st := p["style"].(type) {
	case Props:
		e.inline = css.FromReactStyle(map[string]any(st))
	case string:
		e.inline = css.ParseDecls(st)
	}
	return e
}

// pushDocument puts <html> and <body> at the bottom of the element stack
// for this frame (UI goroutine). No-op until JS reports the document.
func pushDocument() {
	stack = stack[:0]
	doc.Lock()
	h, b, ver := doc.html, doc.body, doc.ver
	doc.Unlock()
	if h == nil || Styles.Empty() {
		return
	}
	e := cssEnv()
	cv := Styles.Version()
	if doc.builtVer != ver || doc.builtCSS != cv || doc.builtDark != e.Dark {
		doc.builtVer, doc.builtCSS, doc.builtDark = ver, cv, e.Dark
		hc := Styles.Cascade(h, css.PseudoNone, h.inline, e)
		doc.hs = css.Compute(&hc, nil, e)
		bc := Styles.Cascade(b, css.PseudoNone, b.inline, e)
		doc.bs = css.Compute(&bc, &doc.hs, e)
		doc.hi = propsOf(&hc) & inheritedMask
		doc.bi = doc.hi | propsOf(&bc)&inheritedMask
	}
	k := mix(ver*0x9e3779b97f4a7c15 ^ cv)
	stack = append(stack,
		stackEntry{el: h, style: &doc.hs, inh: doc.hi, key: k},
		stackEntry{el: b, style: &doc.bs, inh: doc.bi, key: mix(k + 1)})
}
