package css

import (
	"image/color"
	"sync/atomic"
)

// State is an element's dynamic pseudo-class state.
type State uint16

const (
	StateHover State = 1 << iota
	StateFocus
	StateFocusVisible
	StateFocusWithin
	StateActive
	StateChecked
	StateDisabled
	StateEmpty // no children (natives set it when they know)
	StatePlaceholderShown
)

// Element is what selectors match against. Host-tree nodes and Clack's
// native widgets (through their Slack class names) both implement it.
type Element interface {
	Tag() string // lowercase tag; native widgets report "div" unless they say otherwise
	ID() string
	HasClass(c string) bool
	Classes() []string
	// Attr returns an attribute (data-qa, aria-label, role, type, href,
	// data-*, …). "class" and "id" are also answered.
	Attr(name string) (string, bool)
	Parent() Element // nil at the root
	// Prev is the previous element sibling (nil when first or unknown),
	// for the + and ~ combinators.
	Prev() Element
	// Index is the element's 0-based position among its element siblings
	// and the sibling count, for :first-child/:last-child/:nth-child.
	// n == 0 when unknown (native widgets): structural pseudo-classes then
	// don't match.
	Index() (i, n int)
	// TypeIndex is Index among siblings of the same tag (:first-of-type…).
	TypeIndex() (i, n int)
	State() State
}

// KidsElement is implemented by elements that know their children
// (host-tree nodes), for :has() and :empty. Kids are element children;
// an element with text children may also implement HasText() bool so
// :empty doesn't match it.
type KidsElement interface {
	Element
	Kids() []Element
}

// Decl is one declaration; Prop is lowercase kebab-case ("margin-left",
// "--my-var"), Value is the raw value text without "!important".
type Decl struct {
	Prop      string
	Value     string
	Important bool
	// Shorthand is set on a longhand that came from a shorthand whose
	// value has var(): Value is then the shorthand's value, expanded in
	// Compute after substitution ("pending-substitution value").
	Shorthand string
}

// Decls is a declaration list in source order.
type Decls []Decl

// Pseudo selects a pseudo-element.
type Pseudo uint8

const (
	PseudoNone Pseudo = iota
	PseudoBefore
	PseudoAfter
	PseudoPlaceholder
	PseudoBackdrop // ::backdrop of a modal <dialog>
)

// Env is the media/viewport context for @media and units.
type Env struct {
	Dark         bool    // prefers-color-scheme: dark
	Width        float32 // viewport, CSS px (min-width/max-width queries, vw)
	Height       float32
	RootFont     float32 // rem base; 0 = 16
	ReduceMotion bool
	// Fg is the root text color (Initial's color).
	Fg color.NRGBA
	// RootVars are the custom properties defined at :root (Slack's design
	// tokens, see SlackVars), visible to every var().
	RootVars map[string]string
}

// Cascaded is the winning declaration per property for one element (and
// pseudo-element), after specificity, source order and !important.
// Inline declarations are included with the highest specificity.
type Cascaded struct {
	Decls Decls // one entry per property, in no particular order
}

// Has reports whether prop has a winning declaration.
func (c *Cascaded) Has(prop string) bool {
	for i := range c.Decls {
		if c.Decls[i].Prop == prop {
			return true
		}
	}
	return false
}

// Get returns the winning value for prop.
func (c *Cascaded) Get(prop string) (string, bool) {
	for i := range c.Decls {
		if c.Decls[i].Prop == prop {
			return c.Decls[i].Value, true
		}
	}
	return "", false
}

// Engine holds the active stylesheets (keyed like api.setStyle, plus the
// user CSS box) in insertion order. It is safe for concurrent use: sheets
// change from the JS goroutine, matching runs on the UI goroutine.
type Engine struct {
	ver   atomic.Uint64
	empty atomic.Bool
	impl  engineImpl
}

// Empty reports that no sheet has any rule: callers skip all CSS work.
// It is one atomic load.
func (e *Engine) Empty() bool { return e == nil || e.empty.Load() }

// Version increases on every sheet change (cache key for computed styles).
func (e *Engine) Version() uint64 { return e.ver.Load() }
