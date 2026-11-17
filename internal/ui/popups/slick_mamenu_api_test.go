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

// maPicks are the picks (maPick) the menus showed so far, in order.
func maPicks(h *tzHost) []*maPick {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*maPick
	for _, o := range h.shown {
		if p, ok := o.(*maPick); ok {
			out = append(out, p)
		}
	}
	return out
}

// answered is what the plugin answered for the pick (maWaiting, maTaken,
// maPassed).
func (p *maPick) answered() maVerdict {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.verdict
}
