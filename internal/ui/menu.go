package ui

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ShowOverlay pushes an overlay on the stack (drawn above the shell).
func (a *App) ShowOverlay(o Overlay) {
	if o == nil {
		return
	}
	a.overlays = append(a.overlays, o)
	a.Invalidate()
}

// dismisser is implemented by overlays the root can close (Esc / outside
// click routing).
type dismisser interface{ Dismiss() }

// dismissTop closes the topmost overlay; reports whether one was closed.
func (a *App) dismissTop() bool {
	for i := len(a.overlays) - 1; i >= 0; i-- {
		o := a.overlays[i]
		if o.Dismissed() {
			continue
		}
		if d, ok := o.(dismisser); ok {
			d.Dismiss()
		}
		a.overlays = append(a.overlays[:i], a.overlays[i+1:]...)
		a.Invalidate()
		return true
	}
	return false
}

// shellMenu is the shell's own Slack-style menu (team menu, channel kebab,
// More, history) used until/unless ui-popups provides one.
type shellMenu struct {
	a      *App
	anchor image.Point
	items  []MenuItem
	clicks []widget.Clickable
	done   bool
	scrim  int
}

func newShellMenu(a *App, anchor image.Point, items []MenuItem) *shellMenu {
	return &shellMenu{a: a, anchor: anchor, items: items, clicks: make([]widget.Clickable, len(items))}
}

func (m *shellMenu) Dismissed() bool { return m.done }
func (m *shellMenu) Dismiss()        { m.done = true }

func (m *shellMenu) Layout(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	c := &m.a.th.C
	// scrim: transparent, closes on press outside the menu.
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &m.scrim, Kinds: pointer.Press})
		if !ok {
			break
		}
		if _, ok := e.(pointer.Event); ok {
			m.done = true
		}
	}
	func() {
		defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &m.scrim)
	}()

	itemH := gtx.Dp(28)
	divH := gtx.Dp(17)
	w := gtx.Dp(300)
	for _, it := range m.items {
		if !it.Divider {
			if tw := m.a.th.Measure(gtx, 15, theme.Regular, it.Label) + gtx.Dp(96); tw > w {
				w = tw
			}
		}
	}
	if w > gtx.Dp(420) {
		w = gtx.Dp(420)
	}
	h := gtx.Dp(12)
	for _, it := range m.items {
		if it.Divider {
			h += divH
		} else {
			h += itemH
		}
	}
	p := m.anchor
	if p.X+w > sz.X-gtx.Dp(8) {
		p.X = sz.X - gtx.Dp(8) - w
	}
	if p.Y+h > sz.Y-gtx.Dp(8) {
		p.Y = sz.Y - gtx.Dp(8) - h
	}
	if p.X < gtx.Dp(8) {
		p.X = gtx.Dp(8)
	}
	if p.Y < gtx.Dp(8) {
		p.Y = gtx.Dp(8)
	}
	r := image.Rectangle{Min: p, Max: p.Add(image.Pt(w, h))}
	theme.Shadow(gtx, r, gtx.Dp(8), gtx.Dp(10), theme.WithAlpha(c.PanelShadow, 0x50))
	theme.RRect(gtx, r, gtx.Dp(8), c.BgRaised)
	theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), c.Border)
	// swallow presses inside the menu so they don't reach the scrim.
	func() {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, m)
	}()
	y := p.Y + gtx.Dp(6)
	for i := range m.items {
		it := &m.items[i]
		if it.Divider {
			theme.Fill(gtx, image.Rect(p.X, y+divH/2, p.X+w, y+divH/2+gtx.Dp(1)), c.Border)
			y += divH
			continue
		}
		btn := &m.clicks[i]
		at(gtx, image.Pt(p.X, y), image.Pt(w, itemH), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hov bool) {
				if hov && !it.Disabled {
					bg := c.Accent
					if it.Danger {
						bg = c.Danger
					}
					theme.Fill(gtx, image.Rectangle{Max: s}, bg)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				fg := c.Fg
				if it.Danger {
					fg = c.Danger
				}
				if btn.Hovered() && !it.Disabled {
					fg = c.SendText
				}
				if it.Disabled {
					fg = c.FgSubtle
				}
				x := gtx.Dp(24)
				if it.Icon != "" {
					at(gtx, image.Pt(gtx.Dp(16), (itemH-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
						return icons.Get(it.Icon)(gtx, 16, fg)
					})
					x = gtx.Dp(42)
				}
				at(gtx, image.Pt(x, 0), image.Pt(w-x-gtx.Dp(16), itemH), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, itemH, func(gtx layout.Context) layout.Dimensions {
						return m.a.th.Label(gtx, 15, theme.Regular, fg, it.Label)
					})
				})
				if it.Hint != "" {
					hw := m.a.th.Measure(gtx, 13, theme.Regular, it.Hint)
					hc := c.FgSubtle
					if btn.Hovered() {
						hc = c.SendText
					}
					at(gtx, image.Pt(w-gtx.Dp(20)-hw, 0), image.Pt(hw+2, itemH), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, itemH, func(gtx layout.Context) layout.Dimensions {
							return m.a.th.Label(gtx, 13, theme.Regular, hc, it.Hint)
						})
					})
				}
				return layout.Dimensions{Size: image.Pt(w, itemH)}
			})
		})
		if clicked(btn) && !it.Disabled {
			m.done = true
			if it.Do != nil {
				it.Do()
			}
		}
		y += itemH
	}
	return layout.Dimensions{Size: sz}
}
