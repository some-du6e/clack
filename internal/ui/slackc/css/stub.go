package css

// STUB: replaced by the CSS engine (parser.go, selector.go, cascade.go,
// compute.go, values.go, slackvars.go). Delete this file when they land.

import "image/color"

type engineImpl struct{}

// NewEngine returns an empty engine.
func NewEngine() *Engine {
	e := &Engine{}
	e.empty.Store(true)
	return e
}

// Set installs or replaces the sheet under key.
func (e *Engine) Set(key, src string) { e.ver.Add(1) }

// Remove drops the sheet under key.
func (e *Engine) Remove(key string) { e.ver.Add(1) }

// Cascade returns the winning declarations for el.
func (e *Engine) Cascade(el Element, p Pseudo, inline Decls, env Env) Cascaded {
	return Cascaded{Decls: inline}
}

// Compute resolves a cascade into a computed style inheriting from parent.
func Compute(c *Cascaded, parent *Style, env Env) Style {
	if parent != nil {
		return *parent
	}
	return Initial(env.Fg)
}

// FromReactStyle converts a React style object into declarations.
func FromReactStyle(m map[string]any) Decls { return nil }

// ParseDecls parses a declaration block ("color: red; margin: 0").
func ParseDecls(src string) Decls { return nil }

// SlackVars are Slack's design-token custom properties.
func SlackVars(dark bool) map[string]string { return nil }

var _ = color.NRGBA{}
