package slackc

import (
	"sync/atomic"

	"gioui.org/layout"
)

// A launch's expected patches: the components last session's plugins
// patched (the ui root keeps them in ui.json), until the window first
// shows the client. A frame that draws one of them natively, with no
// plugin tree for it yet and no slot reserved for it (reserve.go), notes
// it: the reveal then waits for the plugins, up to its cap
// (internal/ui/reveal.go), instead of showing chrome that a plugin's tree
// would replace a moment later. Components no frame drew (a message row's,
// a popup's) hold nothing. While no set is installed the render paths'
// fast checks are as before: one more atomic load.
var (
	expected      atomic.Pointer[map[string]bool]
	expectedDrawn atomic.Bool
)

// SetExpected installs the components to watch for (nil or empty: none)
// and forgets what was drawn so far.
func SetExpected(names []string) {
	expectedDrawn.Store(false)
	if len(names) == 0 {
		expected.Store(nil)
		return
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	expected.Store(&m)
}

// ExpectedDrawn reports whether a frame since SetExpected drew an
// expected component natively, outside a reserved slot.
func ExpectedDrawn() bool { return expectedDrawn.Load() }

// noteExpected is the render paths' report that name was drawn natively;
// covered: inside the slot reserved for its plugin's tree.
func noteExpected(name string, covered bool) {
	if covered || expectedDrawn.Load() {
		return
	}
	if m := expected.Load(); m != nil && (*m)[name] {
		expectedDrawn.Store(true)
	}
}

// reserveCovers is drawReserved's condition for drawing inside the slot:
// exact constraints (a component that fills its space).
func reserveCovers(gtx layout.Context) bool {
	c := gtx.Constraints
	return c.Min == c.Max && c.Max.X > 0 && c.Max.Y > 0
}
