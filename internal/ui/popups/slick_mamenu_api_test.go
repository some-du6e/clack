package popups

// What the MAMenu tests reach of the menu's internals, in one place (a
// branch proving the tests fail on the code before MAMenu stands these in).

import (
	"image"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slick"
)

// maFrom reports whether m's MAMenu tree arrived and was applied.
func maFrom(m *Menu) bool { return m.ma != nil && m.ma.from != nil }

// maNative is m's native rows (what MAMenu got as actions).
func maNative(m *Menu) []MenuItem { return m.ma.native }

func maRowID(it *MenuItem) string { return maID(it) }

func maClass(it *MenuItem) string { return it.Class }

func maSearchMenu(h Host, m core.Message, anchor image.Rectangle) *Menu {
	return newSearchResultMenu(h, m, anchor, anchor, nil)
}

func slPanelOf(p slick.PluginInfo) bool { return p.Panel }

func slRenderPanel(s *slick.Slick, root uint64, id string) { s.RenderPanel(root, id) }

// maPickOverlays are the overlays the menus' picks showed (maPick), in
// order: the driver draws them above the menu, as the App draws its
// overlays.
func maPickOverlays(h *tzHost) []Overlay {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []Overlay
	for _, o := range h.shown {
		if p, ok := o.(*maPick); ok {
			out = append(out, p)
		}
	}
	return out
}

// maLastAnswer is what the plugin answered for the last pick (maWaiting,
// maTaken, maPassed); ok is false when no pick was shown.
func maLastAnswer(h *tzHost) (v maVerdict, ok bool) {
	ps := maPickOverlays(h)
	if len(ps) == 0 {
		return maWaiting, false
	}
	p := ps[len(ps)-1].(*maPick)
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.verdict, true
}
