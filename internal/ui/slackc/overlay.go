package slackc

import (
	"image"
	"sort"
	"sync"

	"gioui.org/layout"
)

// Overlay roots are the trees JS mounts on its own: open api.modal
// modals, api.menu menus, and whatever plugins append to document.body.
// Their root ids have bit 63 (or taut's bit 52) set; the body is root
// OverlayBody. The ui
// root draws them above everything with LayoutOverlays.

// OverlayBody is the root id of document.body's children.
const OverlayBody uint64 = 1<<63 | 0

var overlays struct {
	sync.Mutex
	m     map[uint64][]*Node
	views map[uint64]*View
}

func deliverOverlay(root uint64, nodes []*Node) bool {
	if root&(1<<63|1<<52) == 0 {
		return false
	}
	overlays.Lock()
	defer overlays.Unlock()
	if overlays.m == nil {
		overlays.m = map[uint64][]*Node{}
		overlays.views = map[uint64]*View{}
	}
	if len(nodes) == 0 {
		delete(overlays.m, root)
		if v := overlays.views[root]; v != nil {
			v.Close()
			delete(overlays.views, root)
		}
		return true
	}
	overlays.m[root] = nodes
	return true
}

// SetDocument installs document.body's children (JS → Go, any goroutine)
// and the text of every <style> in document.head.
func SetDocument(body []byte, styles []string) error {
	nodes, err := ParseTree(body)
	if err != nil {
		return err
	}
	deliverOverlay(OverlayBody, nodes)
	setDocStyles(styles)
	invalidate()
	return nil
}

var docStyles struct {
	sync.Mutex
	n int
}

func setDocStyles(styles []string) {
	docStyles.Lock()
	defer docStyles.Unlock()
	for i, s := range styles {
		Styles.Set(docStyleKey(i), s)
	}
	for i := len(styles); i < docStyles.n; i++ {
		Styles.Remove(docStyleKey(i))
	}
	docStyles.n = len(styles)
	resetNativeStyles()
}

func docStyleKey(i int) string { return "doc:" + keyString(i) }

// HasOverlays reports whether any overlay tree is live (cheap).
func HasOverlays() bool {
	overlays.Lock()
	defer overlays.Unlock()
	return len(overlays.m) > 0
}

// LayoutOverlays draws the overlay trees over the whole window, in root
// order (the body first, then modals and menus as they opened). The ui
// root calls it last in the frame.
func LayoutOverlays(gtx layout.Context) {
	windowSize = gtx.Constraints.Max
	overlays.Lock()
	if len(overlays.m) == 0 {
		overlays.Unlock()
		return
	}
	ids := make([]uint64, 0, len(overlays.m))
	for id := range overlays.m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	type item struct {
		v     *View
		nodes []*Node
	}
	items := make([]item, 0, len(ids))
	for _, id := range ids {
		v := overlays.views[id]
		if v == nil {
			v = NewView(id)
			v.overlay = true
			overlays.views[id] = v
		}
		items = append(items, item{v, overlays.m[id]})
	}
	overlays.Unlock()
	for _, it := range items {
		g := gtx
		g.Constraints = layout.Exact(gtx.Constraints.Max)
		g.Constraints.Min = image.Point{}
		it.v.Layout(g, it.nodes, "", nil, nil)
	}
}
