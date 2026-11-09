package slackc

import (
	"sync"
	"sync/atomic"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// What a component's native draws as its root element, learned whenever
// slackc calls it: the first element it enters (a ClassDef's tag, "div"
// unless declared otherwise) or pushes (the host nodes of a patched child
// drawn first). An "@" node for the component then lays out the way
// Slack's DOM has that root: a block element as a block, an inline one
// (span, a, button) as an inline-block, and a native that drew nothing
// (Slack renders null, or an offscreen span) taking no space. Laid out as
// an inline-block, a block native sat on a line box that added the
// strut's descent under it (3.5dp under every message row a plugin
// wrapped in <div><Original/></div>), and an empty one made an 18dp line.

type rootKind uint8

const (
	rootUnknown rootKind = iota
	rootInline
	rootBlock
	rootEmpty
)

var (
	nativeRoots    sync.Map // component name → rootKind
	nativeRootsGen atomic.Uint64
)

// probe is the native being called (UI goroutine): the root it drew so far.
var probe struct {
	active bool
	kind   rootKind
}

// probeNative calls draw, the native of component name, and learns its
// root.
func probeNative(name string, draw func() layout.Dimensions) layout.Dimensions {
	outer := probe
	probe.active, probe.kind = true, rootUnknown
	d := draw()
	k := probe.kind
	if k == rootUnknown && d.Size.X == 0 && d.Size.Y == 0 {
		k = rootEmpty
	}
	probe = outer
	if name != "" && k != rootUnknown {
		// drawing nothing this time (a row without files) says nothing
		// about a root already seen; every change restyles the views
		if old, ok := nativeRoots.Load(name); !ok || old.(rootKind) != k && !(k == rootEmpty && old.(rootKind) != rootEmpty) {
			nativeRoots.Store(name, k)
			nativeRootsGen.Add(1)
			invalidate()
		}
	}
	// the inner native's root is the outer one's too when it came first
	if probe.active && probe.kind == rootUnknown && (k == rootInline || k == rootBlock) {
		probe.kind = k
	}
	return d
}

// probeDisplay records the first element a probed native draws.
func probeDisplay(d css.Display) {
	if !probe.active || probe.kind != rootUnknown {
		return
	}
	probe.kind = rootBlock
	if isInlineLevel(d) {
		probe.kind = rootInline
	}
}

// blockRoot reports a component whose native's root is a block element or
// nothing at all, so its "@" node is a block by default.
func blockRoot(name string) bool {
	k, ok := nativeRoots.Load(name)
	return ok && (k.(rootKind) == rootBlock || k.(rootKind) == rootEmpty)
}
