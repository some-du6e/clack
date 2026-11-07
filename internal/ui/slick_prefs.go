package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/slick"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Preferences > Slick, drawn natively from slick.Settings and matched to
// Slick's own settings UI (its src/app/settings.tsx): four tabs after
// Slack's (Slick, Plugins, Themes, Install plugins), cards of hairline-
// separated fields, the sticky search + filter chips, category groups,
// the plugin detail (settings by type, workspaces, source), theme cards
// with drawn previews, and the install/update panel. No Slick code is
// used; metrics and strings follow its UI.

// slickTabs are the Preferences tabs Slick adds (after Slack's).
var slickTabs = []struct{ id, label, icon string }{
	{"slick", "Slick", "slick-mark"},
	{"slick_plugins", "Plugins", "apps"},
	{"slick_themes", "Themes", "paintbrush"},
	{"slick_install", "Install plugins", "download"},
	{"slick_library", "Library", "bookmark"},
}

func isSlickTab(id string) bool { return strings.HasPrefix(id, "slick") && slickTabIndex(id) >= 0 }

func slickTabIndex(id string) int {
	for i, t := range slickTabs {
		if t.id == id {
			return i
		}
	}
	return -1
}

type slickPage struct {
	a   *App
	k   *tautPage // shared primitives: clickables, editors, labels, inputs
	tab string

	open     string // plugin detail id
	filter   string
	query    widget.Editor
	list     widget.List
	listBack layout.Position // the list's scroll when a detail opened

	// async state
	themeStatus   string
	installStatus string
	installing    bool
	importing     bool
	wsSaving      bool
	wsErr         bool
	daysOpen      string // schedule rule whose days popover is open
	colorOpen     map[string]bool
	urlEd         widget.Editor
	writes        chan func()
	lastText      map[string]string // text settings: last value saved
	focusWas      map[*widget.Editor]bool
	toolbarH      int
	downloadState string
	lib           *libraryView // the Library tab
}

func newSlickPage(a *App) *slickPage {
	p := &slickPage{a: a, k: newTautPage(a), filter: "all", colorOpen: map[string]bool{},
		lastText: map[string]string{}, focusWas: map[*widget.Editor]bool{}}
	p.query.SingleLine = true
	p.urlEd.SingleLine, p.urlEd.Submit = true, true
	p.list.Axis = layout.Vertical
	return p
}

// write queues a settings write; writes run in order, one at a time, like
// Slick's per-config queue.
func (p *slickPage) write(f func(ctx context.Context, s *slick.Slick) error) {
	s := p.a.Slick()
	if s == nil {
		return
	}
	if p.writes == nil {
		p.writes = make(chan func(), 64)
		go func(ch chan func()) {
			for w := range ch {
				w()
			}
		}(p.writes)
	}
	p.writes <- func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := f(ctx, s); err != nil {
			log.Printf("slick: %v", err)
			p.a.Post(func() { p.a.Toast(err.Error()) })
		}
		p.a.Invalidate()
	}
}

func (p *slickPage) close() {
	p.k.close()
	if p.writes != nil {
		close(p.writes)
		p.writes = nil
	}
}

// ---- colors: Slack design tokens (light / dark) Slick's UI uses ----

type slickColors struct {
	pry, sec, ter, imp, hgl, otlTer, otlSec, otlPry, otlHgl, base, hover, invHgl, baseImp, surf, popBg, popLine, green, faint color.NRGBA
}

func hex8(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
}

func (p *slickPage) colors() slickColors {
	th := p.a.th
	o := th.OnBg
	if th.Dark {
		return slickColors{pry: rgbT(0xf8f8f8), sec: rgbT(0xb9babd), ter: rgbT(0x9a9b9e), imp: rgbT(0xde678a), hgl: rgbT(0x2ba5ce),
			otlTer: o(hex8(0x797c814d)), otlSec: o(hex8(0x797c8180)), otlPry: rgbT(0x797c81), otlHgl: rgbT(0x2ba5ce),
			base: th.C.Bg, hover: o(hex8(0xf8f8f814)), invHgl: rgbT(0x0a77a7), baseImp: rgbT(0x300005), surf: o(hex8(0xf8f8f80f)),
			popBg: rgbT(0x1a1d21), popLine: o(rgbaT(0xe8e8e8, .13)), green: rgbT(0x177e56), faint: rgbT(0x9a9b9e)}
	}
	return slickColors{pry: rgbT(0x1d1c1d), sec: rgbT(0x454447), ter: rgbT(0x5e5d60), imp: rgbT(0xc01343), hgl: rgbT(0x1264a3),
		otlTer: o(hex8(0x5e5d6021)), otlSec: o(hex8(0x5e5d6073)), otlPry: rgbT(0x7c7a7f), otlHgl: rgbT(0x1264a3),
		base: th.C.Bg, hover: o(hex8(0x4544470f)), invHgl: rgbT(0x1264a3), baseImp: rgbT(0xffe8ef), surf: o(hex8(0x1d1c1d0f)),
		popBg: rgbT(0xffffff), popLine: o(rgbaT(0x1d1c1d, .13)), green: rgbT(0x007a5a), faint: rgbT(0x7c7a7f)}
}

// ---- entry ----

// layout draws one of Slick's tabs.
func (p *slickPage) layout(gtx layout.Context, tab string) layout.Dimensions {
	if tab != p.tab { // leaving a tab resets its state, like Slick
		p.tab, p.open, p.filter, p.daysOpen = tab, "", "all", ""
		p.query.SetText("")
		p.list.Position = layout.Position{}
	}
	s := p.a.Slick()
	sz := gtx.Constraints.Max
	if s == nil {
		f := &flow{gtx: gtx, w: sz.X}
		p.hint(f, "Slick plugins start once Clack has signed in to Slack.")
		return layout.Dimensions{Size: sz}
	}
	st := s.Settings()
	p.k.gtxNow = gtx
	sticky := tab == "slick_plugins" && p.open == ""
	var bar op.CallOp
	if sticky {
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(sz.X-gtx.Dp(8), 1<<20)}
		d := p.toolbar(g, st)
		bar = m.Stop()
		p.toolbarH = d.Size.Y
	}
	p.list.List.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: image.Pt(sz.X-gtx.Dp(8), 1<<24)}
		f := &flow{gtx: gtx, w: gtx.Constraints.Max.X}
		switch tab {
		case "slick":
			p.overview(f, st)
		case "slick_plugins":
			if p.open != "" {
				p.detail(f, s, st)
			} else {
				f.y += p.toolbarH + gtx.Dp(20)
				p.pluginList(f, st)
			}
		case "slick_themes":
			p.themes(f, st)
		case "slick_install":
			p.install(f, st)
		case "slick_library":
			p.library(f, s)
		}
		f.gap(24) // panel padding-bottom
		return layout.Dimensions{Size: image.Pt(sz.X, f.y)}
	})
	if sticky { // pinned above the scrolling list, opaque
		theme.Fill(gtx, image.Rect(0, 0, sz.X-gtx.Dp(8), p.toolbarH), p.colors().base)
		bar.Add(gtx.Ops)
	}
	return layout.Dimensions{Size: sz}
}

// library is Slick > Library: the official Slick plugins (pluginlib), the
// renderer installed first, then its privileged half.
func (p *slickPage) library(f *flow, s *slick.Slick) {
	if p.lib == nil {
		p.lib = newLibraryView(pluginlib.Slick, p.k)
	}
	p.lib.layout(f, libraryHost{
		installed: s.InstalledSHA256,
		install: func(ctx context.Context, e pluginlib.Entry, code, main []byte, _ bool) error {
			if _, err := s.Import(ctx, code); err != nil {
				return err
			}
			if main != nil {
				if _, err := s.Import(ctx, main); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// ---- building blocks ----

func (p *slickPage) hint(f *flow, s string) {
	if s == "" {
		return // takes no space when empty
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions { return p.k.para(gtx, 13, 18, p.colors().sec, s) })
}

// notice is Slick's banner (neutral or error).
func (p *slickPage) notice(f *flow, s string, isErr bool) {
	cl := p.colors()
	fg, bg := cl.sec, cl.surf
	if isErr {
		fg, bg = cl.imp, cl.baseImp
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		w := f.w
		m := op.Record(gtx.Ops)
		d := at(gtx, image.Pt(gtx.Dp(14), gtx.Dp(10)), image.Pt(w-gtx.Dp(28), 1<<20), func(gtx layout.Context) layout.Dimensions {
			return p.k.para(gtx, 13, 18, fg, s)
		})
		call := m.Stop()
		h := d.Size.Y + gtx.Dp(20)
		theme.RRect(gtx, image.Rect(0, 0, w, h), gtx.Dp(8), bg)
		call.Add(gtx.Ops)
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
	f.gap(20)
}

// empty is the dashed empty-state box.
func (p *slickPage) empty(f *flow, s string) {
	cl := p.colors()
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		w := f.w
		m := op.Record(gtx.Ops)
		d := at(gtx, image.Pt(gtx.Dp(16), gtx.Dp(32)), image.Pt(w-gtx.Dp(32), 1<<20), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return p.a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: cl.sec, Align: text.Middle, Wrap: text.WrapWords}, s)
		})
		call := m.Stop()
		h := d.Size.Y + gtx.Dp(64)
		dashedBorder(gtx, image.Rect(0, 0, w, h), gtx.Dp(12), gtx.Dp(1), cl.otlSec)
		call.Add(gtx.Ops)
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
}

// dashedBorder approximates a 1px dashed rounded border: dashes along the
// straight edges, solid corners.
func dashedBorder(gtx layout.Context, r image.Rectangle, rad, w int, c color.NRGBA) {
	dash, gap := gtx.Dp(3), gtx.Dp(3)
	for x := r.Min.X + rad; x < r.Max.X-rad; x += dash + gap {
		x1 := min(x+dash, r.Max.X-rad)
		theme.Fill(gtx, image.Rect(x, r.Min.Y, x1, r.Min.Y+w), c)
		theme.Fill(gtx, image.Rect(x, r.Max.Y-w, x1, r.Max.Y), c)
	}
	for y := r.Min.Y + rad; y < r.Max.Y-rad; y += dash + gap {
		y1 := min(y+dash, r.Max.Y-rad)
		theme.Fill(gtx, image.Rect(r.Min.X, y, r.Min.X+w, y1), c)
		theme.Fill(gtx, image.Rect(r.Max.X-w, y, r.Max.X, y1), c)
	}
	for _, cr := range []image.Rectangle{
		image.Rect(r.Min.X, r.Min.Y, r.Min.X+rad, r.Min.Y+rad), image.Rect(r.Max.X-rad, r.Min.Y, r.Max.X, r.Min.Y+rad),
		image.Rect(r.Min.X, r.Max.Y-rad, r.Min.X+rad, r.Max.Y), image.Rect(r.Max.X-rad, r.Max.Y-rad, r.Max.X, r.Max.Y),
	} {
		st := clip.Rect(cr).Push(gtx.Ops)
		theme.Border(gtx, r, rad, w, c)
		st.Pop()
	}
}

// badge is Slick's pill (20 tall, 11/700); warn uses the important color.
func (p *slickPage) badge(gtx layout.Context, label, icon string, warn bool) layout.Dimensions {
	cl := p.colors()
	fg := cl.sec
	if warn {
		fg = cl.imp
	}
	h := gtx.Dp(20)
	x := gtx.Dp(8)
	iw := 0
	if icon != "" {
		iw = gtx.Dp(16 + 4)
	}
	tw := p.a.th.Measure(gtx, 11, theme.Bold, label)
	w := x*2 + iw + tw
	theme.RRect(gtx, image.Rect(0, 0, w, h), h/2, cl.surf)
	if icon != "" {
		at(gtx, image.Pt(x, (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.GetSlack(icon)(gtx, 16, fg)
		})
	}
	at(gtx, image.Pt(x+iw, 0), image.Pt(tw+2, h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 11, theme.Bold, fg, label) })
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// sw is Slack's small switch (28×16, knob inset 2); reports a toggle.
func (p *slickPage) sw(gtx layout.Context, k string, on, disabled bool) (layout.Dimensions, bool) {
	cl := p.colors()
	b := p.k.btn(k)
	d := clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
		w, h := gtx.Dp(28), gtx.Dp(16)
		c := cl.faint
		if on {
			c = cl.green
		}
		if disabled {
			c = theme.WithAlpha(c, 0x66)
		}
		theme.RRect(gtx, image.Rect(0, 0, w, h), h/2, p.a.th.OnBg(c))
		kd := h - gtx.Dp(4)
		x := gtx.Dp(2)
		if on {
			x = w - kd - gtx.Dp(2)
		}
		theme.Circle(gtx, image.Pt(x+kd/2, h/2), kd, rgbT(0xffffff))
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
	return d, clicked(b) && !disabled
}

// btn is a Slack button (outline/primary/danger, small 28 / medium 36).
func (p *slickPage) btn(gtx layout.Context, k, label string, kind popups.BtnKind, size popups.BtnSize, disabled bool) (layout.Dimensions, bool) {
	b := p.k.btn(k)
	if disabled {
		kind = popups.BtnDisabled
	}
	hit := b.Clicked(gtx)
	d := popups.Button(gtx, p.a.th, b, kind, size, label, "", 0)
	return d, hit && !disabled
}

// ghost is Slack's small ghost button: text only, hover fill.
func (p *slickPage) ghost(gtx layout.Context, k, label string, w, h int) (layout.Dimensions, bool) {
	cl := p.colors()
	b := p.k.btn(k)
	tw := p.a.th.Measure(gtx, 13, theme.Bold, label)
	if w == 0 {
		w = tw + gtx.Dp(24)
	}
	if h == 0 {
		h = gtx.Dp(28)
	}
	d := clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
		if hov {
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), cl.hover)
		}
	}, func(gtx layout.Context) layout.Dimensions {
		at(gtx, image.Pt((w-tw)/2, 0), image.Pt(tw+2, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 13, theme.Bold, cl.pry, label) })
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
	return d, clicked(b)
}

// box is an outline button face with custom content (days picker trigger).
func (p *slickPage) box(gtx layout.Context, k string, w, h int, content layout.Widget) (layout.Dimensions, bool) {
	cl := p.colors()
	b := p.k.btn(k)
	d := clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
		r := image.Rectangle{Max: s}
		bg := cl.base
		if hov {
			bg = p.a.th.C.BgHover
		}
		theme.RRect(gtx, r, gtx.Dp(4), bg)
		theme.Border(gtx, r, gtx.Dp(4), gtx.Dp(1), p.a.th.OnBg(rgbaT(0x1d1c1d, .3)))
	}, func(gtx layout.Context) layout.Dimensions {
		at(gtx, image.Pt(gtx.Dp(10), 0), image.Pt(w-gtx.Dp(20), h), content)
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
	_ = cl
	return d, clicked(b)
}

// sinput is Slack's c-input_text with a left padding (search: 34).
func (p *slickPage) sinput(gtx layout.Context, e *widget.Editor, w, h, padL int, placeholder string, disabled bool) layout.Dimensions {
	cl := p.colors()
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(8)
	theme.RRect(gtx, r, rad, cl.base)
	bc := p.a.th.OnBg(rgbaT(0x1d1c1d, .3))
	if p.a.th.Dark {
		bc = p.a.th.OnBg(rgbaT(0xd1d2d3, .3))
	}
	if gtx.Focused(e) {
		bc = cl.otlHgl
		theme.Border(gtx, r.Inset(-gtx.Dp(3)), rad+gtx.Dp(3), gtx.Dp(3), theme.WithAlpha(cl.otlHgl, 0x30))
	}
	theme.Border(gtx, r, rad, gtx.Dp(1), bc)
	size := unit.Sp(15)
	if h < gtx.Dp(34) {
		size = 13
	}
	padY := (h - gtx.Sp(size)*4/3) / 2
	padR := gtx.Dp(12)
	if e.Alignment == text.Middle {
		padL, padR = gtx.Dp(6), gtx.Dp(6)
	}
	at(gtx, image.Pt(padL, padY), image.Pt(w-padL-padR, h-2*padY), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if e.Len() == 0 && placeholder != "" {
			p.a.th.Layout(gtx, theme.TextStyle{Size: size, Color: p.a.th.C.FgSubtle, MaxLines: 1, Align: e.Alignment}, placeholder)
		}
		if disabled {
			gtx = gtx.Disabled()
		}
		return p.a.th.Editor(gtx, e, size, cl.pry, p.a.th.C.Selection)
	})
	return layout.Dimensions{Size: r.Max}
}

// sselect draws Slack's BasicSelect face; a click opens the options menu.
func (p *slickPage) sselect(gtx layout.Context, k string, w, h int, cur string, opts []slick.SelectOption, disabled bool, pick func(v string)) layout.Dimensions {
	cl := p.colors()
	b := p.k.btn(k)
	curLabel := cur
	for _, o := range opts {
		if o.Value == cur {
			curLabel = o.Label
		}
	}
	size := unit.Sp(15)
	if h < gtx.Dp(34) {
		size = 13
	}
	d := clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
		r := image.Rect(0, 0, w, h)
		theme.RRect(gtx, r, gtx.Dp(8), cl.base)
		bc := p.a.th.OnBg(rgbaT(0x1d1c1d, .3))
		if b.Hovered() && !disabled {
			bc = cl.otlPry
		}
		theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), bc)
		fg := cl.pry
		if disabled {
			fg = cl.ter
		}
		at(gtx, image.Pt(gtx.Dp(10), 0), image.Pt(w-gtx.Dp(36), h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, size, theme.Regular, fg, curLabel) })
		})
		at(gtx, image.Pt(w-gtx.Dp(26), (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("chevron-down")(gtx, 16, cl.sec)
		})
		return layout.Dimensions{Size: r.Max}
	})
	if clicked(b) && !disabled {
		var items []MenuItem
		for _, o := range opts {
			o := o
			lbl := o.Label
			if o.Value == cur {
				lbl = "✓ " + lbl
			}
			items = append(items, MenuItem{Label: lbl, Do: func() { pick(o.Value) }})
		}
		p.a.menu(p.a.PointerPos(), items)
	}
	return d
}

// card is Slick's titled section with a bordered, hairline-separated box.
func (p *slickPage) card(f *flow, title, icon string, action func(gtx layout.Context) layout.Dimensions, rows []func(gtx layout.Context, w int) layout.Dimensions) {
	cl := p.colors()
	if title != "" || action != nil {
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			x0, w := gtx.Dp(2), f.w-gtx.Dp(4)
			h := gtx.Dp(28)
			var ad layout.Dimensions
			var acall op.CallOp
			if action != nil {
				m := op.Record(gtx.Ops)
				ad = action(gtx)
				acall = m.Stop()
				h = max(h, ad.Size.Y)
			}
			x := x0
			if icon != "" {
				at(gtx, image.Pt(x, (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.GetSlack(icon)(gtx, 16, cl.ter)
				})
				x += gtx.Dp(16 + 8)
			}
			at(gtx, image.Pt(x, 0), image.Pt(w-x-ad.Size.X-gtx.Dp(12), h), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 15, theme.Bold, cl.pry, title) })
			})
			if action != nil {
				st := op.Offset(image.Pt(x0+w-ad.Size.X, (h-ad.Size.Y)/2)).Push(gtx.Ops)
				acall.Add(gtx.Ops)
				st.Pop()
			}
			return layout.Dimensions{Size: image.Pt(f.w, h)}
		})
		f.gap(8)
	}
	if len(rows) > 0 {
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			return p.cardBox(gtx, f.w, rows)
		})
	}
	f.gap(24)
}

// cardBox lays rows out inside a 1px rounded border with hairlines.
func (p *slickPage) cardBox(gtx layout.Context, w int, rows []func(gtx layout.Context, w int) layout.Dimensions) layout.Dimensions {
	cl := p.colors()
	m := op.Record(gtx.Ops)
	y := 0
	var lines []int
	for i, row := range rows {
		if i > 0 {
			lines = append(lines, y)
			y += gtx.Dp(1)
		}
		d := at(gtx, image.Pt(0, y), image.Pt(w, 1<<20), func(gtx layout.Context) layout.Dimensions { return row(gtx, w) })
		y += d.Size.Y
	}
	call := m.Stop()
	r := image.Rect(0, 0, w, y)
	rad := gtx.Dp(12)
	st := clip.UniformRRect(r, rad).Push(gtx.Ops)
	call.Add(gtx.Ops)
	for _, ly := range lines {
		theme.Fill(gtx, image.Rect(0, ly, w, ly+gtx.Dp(1)), cl.otlTer)
	}
	st.Pop()
	theme.Border(gtx, r, rad, gtx.Dp(1), cl.otlTer)
	return layout.Dimensions{Size: r.Max}
}

// field is a card row: label (+ warn badge) and description on the left,
// the control on the right (inline) or below (stacked).
func (p *slickPage) field(gtx layout.Context, w int, label, desc, warn string, stacked bool, control func(gtx layout.Context, maxW int) layout.Dimensions) layout.Dimensions {
	cl := p.colors()
	padX, padY := gtx.Dp(16), gtx.Dp(14)
	inner := w - 2*padX
	var cd layout.Dimensions
	var ccall op.CallOp
	if control != nil && !stacked {
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
		cd = control(g, inner)
		ccall = m.Stop()
	}
	tw := inner
	if control != nil && !stacked {
		tw = inner - cd.Size.X - gtx.Dp(16)
	}
	textBlock := func(gtx layout.Context) layout.Dimensions {
		y := 0
		if label != "" {
			ld := at(gtx, image.Pt(0, 0), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: cl.pry, Wrap: text.WrapWords}, label)
			})
			if warn != "" {
				lw := min(p.a.th.Measure(gtx, 15, theme.Bold, label), tw)
				at(gtx, image.Pt(lw+gtx.Dp(8), gtx.Dp(1)), image.Pt(gtx.Dp(200), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return p.badge(gtx, warn, "", true)
				})
			}
			y += ld.Size.Y
		}
		if desc != "" {
			dd := at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: cl.sec, Wrap: text.WrapHeuristically}, desc)
			})
			y += dd.Size.Y
		}
		return layout.Dimensions{Size: image.Pt(tw, y)}
	}
	m := op.Record(gtx.Ops)
	td := at(gtx, image.Pt(padX, padY), image.Pt(tw, 1<<20), textBlock)
	tcall := m.Stop()
	if stacked {
		tcall.Add(gtx.Ops)
		y := padY + td.Size.Y
		if control != nil {
			if td.Size.Y > 0 {
				y += gtx.Dp(10)
			}
			d := at(gtx, image.Pt(padX, y), image.Pt(inner, 1<<20), func(gtx layout.Context) layout.Dimensions { return control(gtx, inner) })
			y += d.Size.Y
		}
		return layout.Dimensions{Size: image.Pt(w, y+padY)}
	}
	h := max(td.Size.Y, cd.Size.Y)
	st := op.Offset(image.Pt(0, (h-td.Size.Y)/2)).Push(gtx.Ops)
	tcall.Add(gtx.Ops)
	st.Pop()
	if control != nil {
		st := op.Offset(image.Pt(w-padX-cd.Size.X, padY+(h-cd.Size.Y)/2)).Push(gtx.Ops)
		ccall.Add(gtx.Ops)
		st.Pop()
	}
	return layout.Dimensions{Size: image.Pt(w, h+2*padY)}
}

// link is an inline text link (Credits); opens url.
func (p *slickPage) link(gtx layout.Context, k, label, url string, size unit.Sp) layout.Dimensions {
	cl := p.colors()
	b := p.k.btn(k)
	d := clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
		d := p.k.label(gtx, size, theme.Regular, cl.hgl, label)
		if b.Hovered() {
			theme.Fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(2), d.Size.X, d.Size.Y-gtx.Dp(1)), cl.hgl)
		}
		return d
	})
	if clicked(b) {
		openURL(url)
	}
	return d
}

// ---- Overview ----

var slickContributors = []string{"BittuBarnwal7479", "IzieStratt", "jeremy46231", "Hash-AK", "deployor", "KavyanshKhaitan2", "0xDracula",
	"Vendicated", "CyrilSLi", "kanokid", "sadeshmukh", "christianwell", "MatthiasLubbertsen", "SmartSparkCoding", "hanaeatsplanes",
	"leowilkin", "SSoggyTacoMan", "hippogriff101", "s-ajay-2010", "code2344", "part-avocado", "Ghostboo124", "MilesWK",
	"astra-the-boop", "ItsFelix5", "matmanna", "Lordseriouspig", "mpcallahan", "bomberfish", "csd4ni3l", "cloudglides", "EwoudVV",
	"thirtyseventh-xyz", "ImShyMike", "TheTridentGuy", "alf1e", "RezThePerson", "Fantamomo", "LowPolyPhosphorus", "shuuji3"}

func (p *slickPage) overview(f *flow, st slick.Settings) {
	cl := p.colors()
	on, total := 0, len(st.Plugins)
	var failing []string
	for _, pl := range st.Plugins {
		if pl.Enabled {
			on++
			if pl.StartError != "" {
				failing = append(failing, pl.Name)
			}
		}
	}
	// hero
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		s := gtx.Dp(52)
		r := image.Rect(0, 0, s, s)
		theme.RRect(gtx, r, gtx.Dp(14), cl.surf)
		theme.Border(gtx, r, gtx.Dp(14), gtx.Dp(1), cl.otlTer)
		at(gtx, image.Pt((s-gtx.Dp(30))/2, (s-gtx.Dp(30))/2), image.Pt(gtx.Dp(30), gtx.Dp(30)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("slick-mark")(gtx, 30, cl.pry)
		})
		sub := fmt.Sprintf("%d of %d plugins on", on, total)
		if !st.Enabled {
			sub = "Paused: no plugins are running"
		}
		x := s + gtx.Dp(16)
		m := op.Record(gtx.Ops)
		td := at(gtx, image.Pt(0, 0), image.Pt(f.w-x, 1<<20), func(gtx layout.Context) layout.Dimensions {
			d1 := p.a.th.Layout(gtx, theme.TextStyle{Size: 22, LineHeight: 28, Weight: theme.Black, Color: cl.pry, MaxLines: 1}, "Slick")
			d2 := at(gtx, image.Pt(0, d1.Size.Y), image.Pt(f.w-x, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.k.label(gtx, 14, theme.Regular, cl.sec, sub)
			})
			return layout.Dimensions{Size: image.Pt(f.w-x, d1.Size.Y+d2.Size.Y)}
		})
		call := m.Stop()
		st := op.Offset(image.Pt(x, (s-td.Size.Y)/2)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
		return layout.Dimensions{Size: image.Pt(f.w, s)}
	})
	f.gap(24)
	if len(failing) > 0 {
		p.notice(f, strings.Join(failing, ", ")+" could not start. Open them under Plugins to see why.", true)
	}
	p.card(f, "Version", "", nil, []func(gtx layout.Context, w int) layout.Dimensions{
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "Slick", "Clack’s own implementation of Slick’s plugin API", "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				return p.k.label(gtx, 15, theme.Regular, cl.sec, appVersion())
			})
		},
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, AppName, "How Slick is loaded into Slack", "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				return p.k.label(gtx, 15, theme.Regular, cl.sec, appVersion())
			})
		},
	})
	p.card(f, "Credits", "", nil, []func(gtx layout.Context, w int) layout.Dimensions{
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "Slick contributors", "", "", true, func(gtx layout.Context, iw int) layout.Dimensions {
				y := p.k.para(gtx, 13, 18, cl.sec, "Slick is not a one man act, but a effort by tons of epic people. Thank you to everyone who has helped Slick with code, ideas, testing or support.").Size.Y
				y += gtx.Dp(8)
				x, rowH := 0, gtx.Dp(18)
				for _, h := range slickContributors {
					lw := p.a.th.Measure(gtx, 13, theme.Regular, "@"+h)
					if x > 0 && x+lw > iw {
						x, y = 0, y+rowH+gtx.Dp(6)
					}
					h := h
					at(gtx, image.Pt(x, y), image.Pt(lw+2, rowH), func(gtx layout.Context) layout.Dimensions {
						return p.link(gtx, "contrib."+h, "@"+h, "https://github.com/"+h, 13)
					})
					x += lw + gtx.Dp(16)
				}
				y += rowH + gtx.Dp(8)
				d := at(gtx, image.Pt(0, y), image.Pt(iw, rowH), func(gtx layout.Context) layout.Dimensions {
					return p.link(gtx, "contrib.all", "See all contributors on GitHub", "https://github.com/3kh0/slick/graphs/contributors", 13)
				})
				return layout.Dimensions{Size: image.Pt(iw, y+d.Size.Y)}
			})
		},
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "Built on Taut", "", "", true, func(gtx layout.Context, iw int) layout.Dimensions {
				return p.inlineLinks(gtx, iw, []inlinePart{
					{t: "Slick owes much of its foundation to "}, {t: "Taut", url: "https://github.com/jeremy46231/taut"}, {t: " by "},
					{t: "@jeremy46231", url: "https://github.com/jeremy46231"}, {t: " and "},
					{t: "its contributors", url: "https://github.com/jeremy46231/taut/graphs/contributors?all=1"},
					{t: ". Its injection and patching model, settings integration and other innovations helped make Slick amazing."},
				})
			})
		},
	})
	p.card(f, "Links", "", nil, []func(gtx layout.Context, w int) layout.Dimensions{
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "Source code", "Slick is open source. Drop a star if you like it!", "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				d, c := p.btn(gtx, "gh", "GitHub", popups.BtnOutline, popups.BtnSmall, false)
				if c {
					openURL("https://github.com/3kh0/slick")
				}
				return d
			})
		},
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "Report a problem", "Bugs, broken plugins and ideas", "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				d, c := p.btn(gtx, "issue", "Open an issue", popups.BtnOutline, popups.BtnSmall, false)
				if c {
					openURL("https://github.com/3kh0/slick/issues")
				}
				return d
			})
		},
	})
}

type inlinePart struct{ t, url string }

// inlineLinks flows 13/18 text with links, word by word.
func (p *slickPage) inlineLinks(gtx layout.Context, w int, parts []inlinePart) layout.Dimensions {
	cl := p.colors()
	x, y, lh := 0, 0, gtx.Dp(18)
	space := p.a.th.Measure(gtx, 13, theme.Regular, " ")
	for i, part := range parts {
		words := strings.Split(part.t, " ")
		for j, word := range words {
			if word == "" {
				if j > 0 {
					x += space
				}
				continue
			}
			ww := p.a.th.Measure(gtx, 13, theme.Regular, word)
			if x > 0 && x+ww > w {
				x, y = 0, y+lh
			}
			word := word
			k := "il." + strconv.Itoa(i) + "." + strconv.Itoa(j)
			at(gtx, image.Pt(x, y), image.Pt(ww+2, lh), func(gtx layout.Context) layout.Dimensions {
				if part.url != "" {
					return p.link(gtx, k, word, part.url, 13)
				}
				return p.k.label(gtx, 13, theme.Regular, cl.sec, word)
			})
			x += ww
			if j < len(words)-1 {
				x += space
			}
		}
	}
	return layout.Dimensions{Size: image.Pt(w, y+lh)}
}

// ---- Plugins: toolbar, list ----

func (p *slickPage) filters(st slick.Settings) []slick.Filter {
	fs := st.Filters
	ok := false
	for _, f := range fs {
		ok = ok || f.ID == p.filter
	}
	if !ok {
		p.filter = "all"
	}
	return fs
}

func (p *slickPage) toolbar(gtx layout.Context, st slick.Settings) layout.Dimensions {
	cl := p.colors()
	w := gtx.Constraints.Max.X
	top, bottom := gtx.Dp(2), gtx.Dp(10)
	fs := p.filters(st)
	chipW := func(f slick.Filter) int {
		return gtx.Dp(24) + p.a.th.Measure(gtx, 13, theme.Bold, f.Label) + gtx.Dp(6) + p.a.th.Measure(gtx, 13, theme.Regular, strconv.Itoa(f.Count))
	}
	chipsW := 0
	for i, f := range fs {
		if i > 0 {
			chipsW += gtx.Dp(6)
		}
		chipsW += chipW(f)
	}
	searchW, chipsX, chipsY := w-chipsW-gtx.Dp(12), 0, 0
	inputH := gtx.Dp(36)
	if searchW < gtx.Dp(220) { // wraps: search on its own row
		searchW, chipsX, chipsY = w, 0, inputH+gtx.Dp(8)
	} else {
		chipsX, chipsY = searchW+gtx.Dp(12), (inputH-gtx.Dp(28))/2
	}
	at(gtx, image.Pt(0, top), image.Pt(searchW, inputH), func(gtx layout.Context) layout.Dimensions {
		d := p.sinput(gtx, &p.query, searchW, inputH, gtx.Dp(34), "Search plugins", false)
		at(gtx, image.Pt(gtx.Dp(10), (inputH-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("search")(gtx, 16, cl.ter)
		})
		return d
	})
	x, y := chipsX, top+chipsY
	for _, f := range fs {
		cw := chipW(f)
		if x+cw > w && x > chipsX {
			x, y = chipsX, y+gtx.Dp(28+8)
		}
		f := f
		at(gtx, image.Pt(x, y), image.Pt(cw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return p.chip(gtx, f, cw)
		})
		x += cw + gtx.Dp(6)
	}
	h := max(top+inputH, y+gtx.Dp(28)) + bottom
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func (p *slickPage) chip(gtx layout.Context, f slick.Filter, w int) layout.Dimensions {
	cl := p.colors()
	b := p.k.btn("chip." + f.ID)
	h := gtx.Dp(28)
	d := clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
		r := image.Rect(0, 0, w, h)
		active := p.filter == f.ID
		bc, fill, fg := cl.otlSec, color.NRGBA{}, cl.sec
		switch {
		case active:
			bc, fill, fg = cl.invHgl, cl.invHgl, rgbT(0xffffff)
		case b.Hovered():
			bc, fg = cl.otlPry, cl.pry
		}
		if fill.A > 0 {
			theme.RRect(gtx, r, h/2, fill)
		}
		theme.Border(gtx, r, h/2, gtx.Dp(1), bc)
		lw := p.a.th.Measure(gtx, 13, theme.Bold, f.Label)
		at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(lw+2, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 13, theme.Bold, fg, f.Label) })
		})
		at(gtx, image.Pt(gtx.Dp(12)+lw+gtx.Dp(6), 0), image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return p.k.label(gtx, 13, theme.Regular, theme.WithAlpha(fg, 0xbf), strconv.Itoa(f.Count))
			})
		})
		return layout.Dimensions{Size: r.Max}
	})
	if clicked(b) {
		p.filter = f.ID
		p.a.Invalidate()
	}
	return d
}

func (p *slickPage) problem(pl slick.PluginInfo, st slick.Settings) (string, bool) {
	if !pl.Enabled || pl.Running {
		return "", false
	}
	switch {
	case !pl.WorkspaceAllowed:
		return "Not enabled in this workspace", false
	case !st.Enabled:
		return "Slick is paused", false
	case pl.StartError != "":
		return "Could not start: " + pl.StartError, true
	}
	return "Starting…", false
}

func (p *slickPage) matches(pl slick.PluginInfo, words []string) bool {
	switch p.filter {
	case "on":
		if !pl.Enabled {
			return false
		}
	case "off":
		if pl.Enabled {
			return false
		}
	case "user":
		if !pl.User {
			return false
		}
	case "problems":
		if !pl.Enabled || pl.StartError == "" {
			return false
		}
	}
	hay := strings.ToLower(strings.Join([]string{pl.ID, pl.Name, pl.Description, p.mentions(pl.Authors), pl.CategoryLabel}, " "))
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

func (p *slickPage) pluginList(f *flow, st slick.Settings) {
	words := strings.Fields(strings.ToLower(p.query.Text()))
	shown := 0
	for _, sec := range st.Sections {
		var rows []slick.PluginInfo
		for _, pl := range sec.Plugins {
			if p.matches(pl, words) {
				rows = append(rows, pl)
			}
		}
		if len(rows) == 0 {
			continue
		}
		shown += len(rows)
		icon := sec.Icon
		if icon == "" {
			icon = "code"
		}
		on := sec.On
		fns := make([]func(gtx layout.Context, w int) layout.Dimensions, len(rows))
		for i, pl := range rows {
			pl := pl
			fns[i] = func(gtx layout.Context, w int) layout.Dimensions { return p.pluginRow(gtx, w, pl, st) }
		}
		p.card(f, sec.Label, icon, func(gtx layout.Context) layout.Dimensions {
			if on == 0 {
				return layout.Dimensions{}
			}
			return p.k.label(gtx, 13, theme.Regular, p.colors().ter, strconv.Itoa(on)+" on")
		}, fns)
	}
	if shown == 0 {
		if q := strings.TrimSpace(p.query.Text()); q != "" {
			p.empty(f, "No plugins match “"+q+"”.")
		} else {
			p.empty(f, "No plugins in this view.")
		}
	}
}

func (p *slickPage) pluginRow(gtx layout.Context, w int, pl slick.PluginInfo, st slick.Settings) layout.Dimensions {
	cl := p.colors()
	b := p.k.btn("row." + pl.ID)
	prob, probErr := p.problem(pl, st)
	swW, chevW, gap := gtx.Dp(28), gtx.Dp(16), gtx.Dp(14)
	tx := gtx.Dp(16)
	tw := w - tx - gtx.Dp(12) - swW - chevW - 2*gap
	textCol := func(gtx layout.Context) layout.Dimensions {
		y := 0
		nd := p.a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: cl.pry, MaxLines: 1}, pl.Name)
		if pl.User {
			at(gtx, image.Pt(min(nd.Size.X, tw)+gtx.Dp(8), gtx.Dp(1)), image.Pt(gtx.Dp(100), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return p.badge(gtx, "User", "", false)
			})
		}
		y += gtx.Dp(22)
		if pl.Description != "" {
			d := at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: cl.sec, MaxLines: 2, Wrap: text.WrapWords}, plainMrkdwn(pl.Description))
			})
			y += d.Size.Y
		}
		if prob != "" {
			col := cl.ter
			if probErr {
				col = cl.imp
			}
			y += gtx.Dp(4)
			d := at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 12, Weight: theme.Bold, Color: col, Wrap: text.WrapWords}, prob)
			})
			y += d.Size.Y
		}
		return layout.Dimensions{Size: image.Pt(tw, y)}
	}
	m := op.Record(gtx.Ops)
	td := at(gtx, image.Point{}, image.Pt(tw, 1<<20), textCol)
	tcall := m.Stop()
	h := td.Size.Y + gtx.Dp(24)
	d := clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
		if hov {
			theme.Fill(gtx, image.Rectangle{Max: s}, cl.hover)
		}
	}, func(gtx layout.Context) layout.Dimensions {
		st := op.Offset(image.Pt(tx, gtx.Dp(12))).Push(gtx.Ops)
		tcall.Add(gtx.Ops)
		st.Pop()
		x := w - gtx.Dp(12) - chevW
		at(gtx, image.Pt(x, (h-gtx.Dp(16))/2), image.Pt(chevW, chevW), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("caret-right")(gtx, 16, cl.ter)
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
	// the switch sits above the row's click area (its clicks don't open the detail)
	at(gtx, image.Pt(w-gtx.Dp(12)-chevW-gap-swW, (h-gtx.Dp(16))/2), image.Pt(swW, gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
		d, c := p.sw(gtx, "en."+pl.ID, pl.Enabled, false)
		if c {
			on := !pl.Enabled
			p.write(func(ctx context.Context, s *slick.Slick) error { return s.SetEnabled(ctx, pl.ID, on) })
		}
		return d
	})
	if clicked(b) {
		p.listBack = p.list.Position
		p.open = pl.ID
		p.list.Position = layout.Position{}
		p.a.Invalidate()
	}
	return d
}

var mentionRE = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|([^>]*))?>`)

// mentions renders Slick's `<@U…>` author list as plain names.
func (p *slickPage) mentions(s string) string {
	c := p.a.Core()
	return mentionRE.ReplaceAllStringFunc(s, func(m string) string {
		sm := mentionRE.FindStringSubmatch(m)
		if sm[2] != "" {
			return sm[2]
		}
		if c != nil {
			if n := c.S.UserName(sm[1]); n != "" {
				return n
			}
		}
		return sm[1]
	})
}

// ---- Plugin detail ----

func (p *slickPage) detail(f *flow, s *slick.Slick, st slick.Settings) {
	cl := p.colors()
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		d, c := p.k.linkButton(gtx, "back", "All plugins", "back")
		if c {
			p.open, p.daysOpen = "", ""
			p.list.Position = p.listBack
		}
		return d
	})
	f.gap(16)
	var pl slick.PluginInfo
	found := false
	for _, x := range st.Plugins {
		if x.ID == p.open {
			pl, found = x, true
		}
	}
	if !found {
		p.empty(f, "This plugin isn’t installed any more.")
		return
	}
	// header
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		swW := gtx.Dp(28)
		tw := f.w - swW - gtx.Dp(20)
		y := p.a.th.Layout(gtx, theme.TextStyle{Size: 22, LineHeight: 28, Weight: theme.Black, Color: cl.pry, Wrap: text.WrapWords}, pl.Name).Size.Y + gtx.Dp(4)
		if pl.Description != "" {
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.k.para(gtx, 15, 22, cl.sec, plainMrkdwn(pl.Description))
			}).Size.Y
		}
		y += gtx.Dp(10)
		x := 0
		item := func(w layout.Widget) {
			d := at(gtx, image.Pt(x, y), image.Pt(1<<20, gtx.Dp(20)), w)
			x += d.Size.X + gtx.Dp(10)
		}
		catIcon := "code"
		for _, sec := range st.Sections {
			if sec.Label == pl.CategoryLabel && sec.Icon != "" {
				catIcon = sec.Icon
			}
		}
		label := pl.CategoryLabel
		if label == "" {
			label = "Other"
		}
		item(func(gtx layout.Context) layout.Dimensions { return p.badge(gtx, label, catIcon, false) })
		if pl.User {
			item(func(gtx layout.Context) layout.Dimensions { return p.badge(gtx, "User plugin", "", false) })
		}
		if a := p.mentions(pl.Authors); a != "" {
			item(func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, gtx.Dp(20), func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 13, theme.Regular, cl.ter, "By "+a) })
			})
		}
		y += gtx.Dp(20)
		at(gtx, image.Pt(f.w-swW, gtx.Dp(6)), image.Pt(swW, gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			d, c := p.sw(gtx, "den."+pl.ID, pl.Enabled, false)
			if c {
				on := !pl.Enabled
				p.write(func(ctx context.Context, s *slick.Slick) error { return s.SetEnabled(ctx, pl.ID, on) })
			}
			return d
		})
		return layout.Dimensions{Size: image.Pt(f.w, y)}
	})
	f.gap(24)
	if prob, isErr := p.problem(pl, st); prob != "" {
		if isErr {
			prob += ". Turn this plugin off and on to retry."
		}
		p.notice(f, prob, isErr)
	}
	if len(pl.Options) > 0 {
		rows := make([]func(gtx layout.Context, w int) layout.Dimensions, len(pl.Options))
		for i, o := range pl.Options {
			o := o
			rows[i] = func(gtx layout.Context, w int) layout.Dimensions { return p.settingRow(gtx, w, pl, o) }
		}
		p.card(f, "Settings", "", nil, rows)
	}
	p.card(f, "Workspaces", "", nil, p.workspaceRows(pl, st))
	if pl.User {
		label, desc := "Installed from a file", "ID "+pl.ID
		if pl.Source != "" {
			label, desc = "Installed from a URL", pl.Source
		}
		p.card(f, "Source", "", nil, []func(gtx layout.Context, w int) layout.Dimensions{
			func(gtx layout.Context, w int) layout.Dimensions {
				return p.field(gtx, w, label, desc, "", false, func(gtx layout.Context, _ int) layout.Dimensions {
					d, c := p.btn(gtx, "remove."+pl.ID, "Remove plugin", popups.BtnDanger, popups.BtnSmall, false)
					if c {
						id := pl.ID
						p.write(func(ctx context.Context, s *slick.Slick) error { return s.Remove(ctx, id) })
					}
					return d
				})
			},
		})
	}
}

func (p *slickPage) workspaceRows(pl slick.PluginInfo, st slick.Settings) []func(gtx layout.Context, w int) layout.Dimensions {
	cl := p.colors()
	if pl.AppWideReason != "" {
		return []func(gtx layout.Context, w int) layout.Dimensions{func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "All workspaces", pl.AppWideReason, "", false, nil)
		}}
	}
	restricted := pl.Workspaces != nil
	desc := "Every workspace you are signed in to."
	if restricted && len(pl.Workspaces) > 0 {
		desc = "Only the workspaces ticked below. Changes apply immediately."
	} else if restricted {
		desc = "No workspaces selected. This plugin will not run anywhere."
	}
	save := func(ws []string) {
		p.wsSaving, p.wsErr = true, false
		id := pl.ID
		p.write(func(ctx context.Context, s *slick.Slick) error {
			err := s.SetWorkspaces(ctx, id, ws)
			p.a.Post(func() { p.wsSaving, p.wsErr = false, err != nil })
			return err
		})
	}
	cur := "all"
	if restricted {
		cur = "selected"
	}
	rows := []func(gtx layout.Context, w int) layout.Dimensions{func(gtx layout.Context, w int) layout.Dimensions {
		return p.field(gtx, w, "Run this plugin in", desc, "", false, func(gtx layout.Context, _ int) layout.Dimensions {
			return p.sselect(gtx, "ws."+pl.ID, gtx.Dp(240), gtx.Dp(36), cur,
				[]slick.SelectOption{{Value: "all", Label: "All workspaces"}, {Value: "selected", Label: "Selected workspaces"}}, p.wsSaving,
				func(v string) {
					switch {
					case v == "all" && restricted:
						save(nil)
					case v == "selected" && !restricted:
						if st.Workspace != "" {
							save([]string{st.Workspace})
						} else {
							save([]string{})
						}
					}
				})
		})
	}}
	if !restricted {
		return rows
	}
	type ws struct{ id, name string }
	seen := map[string]bool{}
	var list []ws
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		name := id
		if id == st.Workspace {
			if c := p.a.Core(); c != nil {
				c.S.RLock()
				if c.S.TeamName != "" {
					name = c.S.TeamName
				}
				c.S.RUnlock()
			}
		}
		list = append(list, ws{id, name})
	}
	add(st.Workspace)
	for _, id := range pl.Workspaces {
		add(id)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].name != list[j].name {
			return list[i].name < list[j].name
		}
		return list[i].id < list[j].id
	})
	sel := map[string]bool{}
	for _, id := range pl.Workspaces {
		sel[id] = true
	}
	for _, w := range list {
		w := w
		label := w.name
		if w.id == st.Workspace {
			label += " (current)"
		}
		rows = append(rows, func(gtx layout.Context, fw int) layout.Dimensions {
			return p.field(gtx, fw, label, w.id, "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				d, c := p.sw(gtx, "wsel."+pl.ID+"."+w.id, sel[w.id], p.wsSaving)
				if c {
					var next []string
					for _, x := range list {
						if x.id == w.id && !sel[x.id] || x.id != w.id && sel[x.id] {
							next = append(next, x.id)
						}
					}
					if next == nil {
						next = []string{}
					}
					save(next)
				}
				return d
			})
		})
	}
	rows = append(rows, func(gtx layout.Context, w int) layout.Dimensions {
		d := at(gtx, image.Pt(gtx.Dp(16), gtx.Dp(14)), image.Pt(w-gtx.Dp(32), 1<<20), func(gtx layout.Context) layout.Dimensions {
			return p.k.para(gtx, 13, 18, cl.sec, "Open another workspace in Slack to make it available here.")
		})
		return layout.Dimensions{Size: image.Pt(w, d.Size.Y+gtx.Dp(28))}
	})
	if p.wsErr {
		rows = append(rows, func(gtx layout.Context, w int) layout.Dimensions {
			d := at(gtx, image.Pt(gtx.Dp(16), gtx.Dp(18)), image.Pt(w-gtx.Dp(32), 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 12, Weight: theme.Bold, Color: cl.imp, Wrap: text.WrapWords}, "Could not save workspace selection. Try again.")
			})
			return layout.Dimensions{Size: image.Pt(w, d.Size.Y+gtx.Dp(32))}
		})
	}
	return rows
}

// ---- setting rows ----

func (p *slickPage) settingRow(gtx layout.Context, w int, pl slick.PluginInfo, o slick.OptionInfo) layout.Dimensions {
	key := pl.ID + "/" + o.Key
	warn := ""
	if o.RestartRequired || o.Relaunch {
		warn = "Needs restart"
	}
	for _, k := range pl.RelaunchSettings {
		if k == o.Key {
			warn = "Needs restart"
		}
	}
	label := o.Label
	if label == "" {
		label = o.Key
	}
	set := func(v any) {
		id, k := pl.ID, o.Key
		p.write(func(ctx context.Context, s *slick.Slick) error { return s.SetOption(ctx, id, k, v) })
	}
	stacked := o.Type == "text" || o.Type == "file" || o.Type == "schedule" || o.Type == "names"
	var control func(gtx layout.Context, maxW int) layout.Dimensions
	switch o.Type {
	case "boolean":
		on, _ := o.Value.(bool)
		control = func(gtx layout.Context, _ int) layout.Dimensions {
			d, c := p.sw(gtx, "opt."+key, on, false)
			if c {
				set(!on)
			}
			return d
		}
	case "number":
		e := p.k.ed("num."+key, true)
		cur := numText(o.Value)
		p.syncEditor(e, cur)
		p.onBlur(e, false, func(v string) {
			n := parseNumber(v, o)
			e.SetText(numText(n))
			if numText(n) != cur {
				set(n)
			}
		})
		control = func(gtx layout.Context, _ int) layout.Dimensions {
			return p.sinput(gtx, e, gtx.Dp(120), gtx.Dp(36), gtx.Dp(12), "", false)
		}
	case "text":
		e := p.k.ed("text."+key, true)
		cur, _ := o.Value.(string)
		if _, ok := p.lastText[key]; !ok || !gtx.Focused(e) {
			p.syncEditor(e, cur)
			p.lastText[key] = cur
		}
		if o.MaxLength > 0 {
			e.MaxLen = o.MaxLength
		}
		if t := e.Text(); gtx.Focused(e) && t != p.lastText[key] { // saves per keystroke
			p.lastText[key] = t
			set(t)
		}
		control = func(gtx layout.Context, maxW int) layout.Dimensions {
			return p.sinput(gtx, e, maxW, gtx.Dp(36), gtx.Dp(12), "", false)
		}
	case "select":
		cur, _ := o.Value.(string)
		control = func(gtx layout.Context, _ int) layout.Dimensions {
			return p.sselect(gtx, "sel."+key, gtx.Dp(240), gtx.Dp(36), cur, o.Options, false, func(v string) { set(v) })
		}
	case "color":
		control = func(gtx layout.Context, _ int) layout.Dimensions { return p.colorControl(gtx, key, o, set) }
	case "file":
		control = func(gtx layout.Context, maxW int) layout.Dimensions { return p.fileControl(gtx, key, label, o, set) }
	case "names":
		control = func(gtx layout.Context, maxW int) layout.Dimensions { return p.namesControl(gtx, key, o, set) }
	case "schedule":
		control = func(gtx layout.Context, maxW int) layout.Dimensions { return p.scheduleControl(gtx, key, maxW, o, set) }
	default: // unknown kinds: a text field
		e := p.k.ed("text."+key, true)
		cur := fmt.Sprint(o.Value)
		p.syncEditor(e, cur)
		p.onBlur(e, true, func(v string) {
			if v != cur {
				set(v)
			}
		})
		control = func(gtx layout.Context, _ int) layout.Dimensions {
			return p.sinput(gtx, e, gtx.Dp(240), gtx.Dp(36), gtx.Dp(12), "", false)
		}
	}
	return p.field(gtx, w, label, o.Description, warn, stacked, control)
}

func (p *slickPage) syncEditor(e *widget.Editor, v string) {
	if !p.k.gtxNow.Focused(e) && e.Text() != v {
		e.SetText(v)
	}
}

// onBlur commits an editor's text when it loses focus (and on Enter when
// enter is set).
func (p *slickPage) onBlur(e *widget.Editor, enter bool, commit func(string)) {
	for {
		ev, ok := e.Update(p.k.gtxNow)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok && enter {
			commit(e.Text())
		}
	}
	now := p.k.gtxNow.Focused(e)
	if p.focusWas[e] && !now {
		commit(e.Text())
	}
	p.focusWas[e] = now
}

func numText(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case int:
		return strconv.Itoa(n)
	case json.Number:
		return n.String()
	}
	return fmt.Sprint(v)
}

// parseNumber is Slick's number commit: non-finite → default, empty → 0,
// then clamped.
func parseNumber(s string, o slick.OptionInfo) float64 {
	s = strings.TrimSpace(s)
	n := 0.0
	if s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			d, _ := o.Default.(float64)
			return d
		}
		n = v
	}
	if o.Min != nil && n < *o.Min {
		n = *o.Min
	}
	if o.Max != nil && n > *o.Max {
		n = *o.Max
	}
	return n
}

var hexRE = regexp.MustCompile(`^#([0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// colorControl: Slick's 44×28 colour well; Clack has no OS colour picker,
// so a click opens a hex field beside it.
func (p *slickPage) colorControl(gtx layout.Context, key string, o slick.OptionInfo, set func(any)) layout.Dimensions {
	cl := p.colors()
	cur, _ := o.Value.(string)
	x := 0
	h := gtx.Dp(28)
	if p.colorOpen[key] {
		e := p.k.ed("color."+key, true)
		p.syncEditor(e, cur)
		p.onBlur(e, true, func(v string) {
			v = strings.TrimSpace(v)
			if hexRE.MatchString(v) && v != cur {
				set(v)
			}
		})
		ew := gtx.Dp(100)
		at(gtx, image.Pt(0, 0), image.Pt(ew, h), func(gtx layout.Context) layout.Dimensions {
			return p.sinput(gtx, e, ew, h, gtx.Dp(8), "#RRGGBB", false)
		})
		x = ew + gtx.Dp(8)
	}
	b := p.k.btn("well." + key)
	at(gtx, image.Pt(x, 0), image.Pt(gtx.Dp(44), h), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
			r := image.Rect(0, 0, gtx.Dp(44), h)
			theme.Border(gtx, r, gtx.Dp(6), gtx.Dp(1), cl.otlSec)
			theme.RRect(gtx, r.Inset(gtx.Dp(4)), gtx.Dp(3), parseHexColor(cur))
			return layout.Dimensions{Size: r.Max}
		})
	})
	if clicked(b) {
		p.colorOpen[key] = !p.colorOpen[key]
	}
	return layout.Dimensions{Size: image.Pt(x+gtx.Dp(44), h)}
}

func (p *slickPage) fileControl(gtx layout.Context, key, label string, o slick.OptionInfo, set func(any)) layout.Dimensions {
	cl := p.colors()
	cur, _ := o.Value.(string)
	h := gtx.Dp(28)
	d, c := p.btn(gtx, "file."+key, "Choose file…", popups.BtnOutline, popups.BtnSmall, false)
	if c {
		a, accept := p.a, o.Accept
		go func() {
			path, err := a.openFilePath(label, accept)
			if err != nil {
				a.Post(func() { a.Toast(err.Error()) })
				return
			}
			if path != "" {
				a.Post(func() { set(path) })
			}
		}()
	}
	x := d.Size.X + gtx.Dp(8)
	name := "No file selected"
	if cur != "" {
		name = filepath.Base(cur)
	}
	nd := at(gtx, image.Pt(x, 0), image.Pt(gtx.Constraints.Max.X-x-gtx.Dp(70), h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 15, theme.Regular, cl.pry, name) })
	})
	x += nd.Size.X + gtx.Dp(8)
	if cur != "" {
		at(gtx, image.Pt(x, 0), image.Pt(gtx.Dp(80), h), func(gtx layout.Context) layout.Dimensions {
			d, c := p.ghost(gtx, "fileclear."+key, "Clear", 0, 0)
			if c {
				set("")
			}
			x += d.Size.X
			return d
		})
	}
	return layout.Dimensions{Size: image.Pt(x, h)}
}

func (p *slickPage) namesControl(gtx layout.Context, key string, o slick.OptionInfo, set func(any)) layout.Dimensions {
	cl := p.colors()
	m, _ := o.Value.(map[string]any)
	if len(m) == 0 {
		return p.k.para(gtx, 13, 18, cl.sec, "No names configured.")
	}
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return fmt.Sprint(m[ids[i]]) < fmt.Sprint(m[ids[j]]) })
	y := 0
	rowH := gtx.Dp(28)
	for _, id := range ids {
		id := id
		name := fmt.Sprint(m[id])
		at(gtx, image.Pt(0, y), image.Pt(gtx.Constraints.Max.X, rowH), func(gtx layout.Context) layout.Dimensions {
			x := gtx.Dp(6)
			theme.Circle(gtx, image.Pt(x, rowH/2), gtx.Dp(5), cl.pry)
			x = gtx.Dp(20)
			nd := at(gtx, image.Pt(x, 0), image.Pt(1<<20, rowH), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 15, theme.Regular, cl.pry, name) })
			})
			x += nd.Size.X + p.a.th.Measure(gtx, 15, theme.Regular, " ")
			idd := at(gtx, image.Pt(x, 0), image.Pt(1<<20, rowH), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
					return p.k.label(gtx, 15, theme.Regular, theme.WithAlpha(cl.pry, 0xa6), "("+id+")")
				})
			})
			x += idd.Size.X + gtx.Dp(6)
			at(gtx, image.Pt(x, 0), image.Pt(gtx.Dp(100), rowH), func(gtx layout.Context) layout.Dimensions {
				d, c := p.ghost(gtx, "nameRm."+key+"."+id, "Remove", 0, 0)
				if c {
					next := map[string]any{}
					for k, v := range m {
						if k != id {
							next[k] = v
						}
					}
					set(next)
				}
				return d
			})
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, rowH)}
		})
		y += rowH + gtx.Dp(4)
	}
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, y-gtx.Dp(4))}
}

// ---- schedule ----

type schedRule struct {
	Days   []int  `json:"days"`
	Start  string `json:"start"`
	End    string `json:"end"`
	Status string `json:"status"`
}

func schedRules(v any) []schedRule {
	b, _ := json.Marshal(v)
	var out []schedRule
	_ = json.Unmarshal(b, &out)
	return out
}

var dayNames = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

var dayPresets = []struct {
	label, short string
	days         []int
}{
	{"Weekdays", "Mon–Fri", []int{1, 2, 3, 4, 5}},
	{"Weekends", "Sat–Sun", []int{0, 6}},
	{"Every day", "Daily", []int{0, 1, 2, 3, 4, 5, 6}},
}

func sameDays(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func daysLabel(days []int) string {
	for _, pr := range dayPresets {
		if sameDays(days, pr.days) {
			return pr.short
		}
	}
	if len(days) == 0 {
		return "Days"
	}
	names := make([]string, 0, len(days))
	for _, d := range days {
		if d >= 0 && d < 7 {
			names = append(names, dayNames[d])
		}
	}
	return strings.Join(names, ", ")
}

var timeRE = regexp.MustCompile(`^(\d{1,2}):?(\d{2})?$`)

// parseTime is Slick's TimeInput parse ("9" → 09:00, "930" → 09:30).
func parseTime(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) == 3 && !strings.Contains(s, ":") { // 930
		s = s[:1] + ":" + s[1:]
	} else if len(s) == 4 && !strings.Contains(s, ":") { // 1730
		s = s[:2] + ":" + s[2:]
	}
	m := timeRE.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	h, _ := strconv.Atoi(m[1])
	mi := 0
	if m[2] != "" {
		mi, _ = strconv.Atoi(m[2])
	}
	if h > 23 || mi > 59 {
		return "", false
	}
	return fmt.Sprintf("%02d:%02d", h, mi), true
}

func (p *slickPage) scheduleControl(gtx layout.Context, key string, w int, o slick.OptionInfo, set func(any)) layout.Dimensions {
	cl := p.colors()
	rules := schedRules(o.Value)
	save := func(next []schedRule) {
		if len(next) > 50 {
			next = next[:50]
		}
		if next == nil {
			next = []schedRule{}
		}
		var v any
		b, _ := json.Marshal(next)
		_ = json.Unmarshal(b, &v)
		set(v)
	}
	ch := gtx.Dp(32)
	gap := gtx.Dp(6)
	fixed := gtx.Dp(60+8+60+28+172+32) + 6*gap
	daysW := max(gtx.Dp(88), w-fixed)
	y := 0
	var pop op.CallOp
	popAt := image.Point{}
	havePop := false
	for i, r := range rules {
		i, r := i, r
		rk := key + "." + strconv.Itoa(i)
		x := 0
		// days trigger
		at(gtx, image.Pt(x, y), image.Pt(daysW, ch), func(gtx layout.Context) layout.Dimensions {
			d, c := p.box(gtx, "days."+rk, daysW, ch, func(gtx layout.Context) layout.Dimensions {
				w := gtx.Constraints.Max.X
				aw := p.a.th.Measure(gtx, 13, theme.Regular, "▾")
				at(gtx, image.Pt(0, 0), image.Pt(w-aw-gap, ch), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
						return p.k.label(gtx, 13, theme.Bold, cl.pry, daysLabel(r.Days))
					})
				})
				at(gtx, image.Pt(w-aw, 0), image.Pt(aw+2, ch), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
						return p.k.label(gtx, 13, theme.Regular, theme.WithAlpha(cl.pry, 0x99), "▾")
					})
				})
				return layout.Dimensions{Size: image.Pt(w, ch)}
			})
			if c {
				if p.daysOpen == rk {
					p.daysOpen = ""
				} else {
					p.daysOpen = rk
				}
			}
			return d
		})
		if p.daysOpen == rk {
			m := op.Record(gtx.Ops)
			p.daysPopover(gtx, rk, r.Days, func(days []int) {
				next := append([]schedRule(nil), rules...)
				next[i].Days = days
				save(next)
			})
			pop = m.Stop()
			popAt, havePop = image.Pt(x, y+ch+gtx.Dp(4)), true
		}
		x += daysW + gap
		timeIn := func(which, cur, aria string, setT func(string)) {
			e := p.k.ed("time."+rk+"."+which, true)
			e.Alignment = text.Middle
			p.syncEditor(e, cur)
			p.onBlur(e, true, func(v string) {
				t, ok := parseTime(v)
				if !ok {
					e.SetText(cur)
					return
				}
				e.SetText(t)
				if t != cur {
					setT(t)
				}
			})
			at(gtx, image.Pt(x, y), image.Pt(gtx.Dp(60), ch), func(gtx layout.Context) layout.Dimensions {
				return p.sinput(gtx, e, gtx.Dp(60), ch, gtx.Dp(6), "HH:MM", false)
			})
			x += gtx.Dp(60) + gap
		}
		timeIn("start", r.Start, "Start time", func(t string) {
			next := append([]schedRule(nil), rules...)
			next[i].Start = t
			save(next)
		})
		dw := p.a.th.Measure(gtx, 15, theme.Regular, "–") // centred in its 8px column, unclipped
		at(gtx, image.Pt(x+(gtx.Dp(8)-dw)/2, y), image.Pt(dw+gtx.Dp(4), ch), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
				return p.k.label(gtx, 15, theme.Regular, theme.WithAlpha(cl.pry, 0xb3), "–")
			})
		})
		x += gtx.Dp(8) + gap
		timeIn("end", r.End, "End time", func(t string) {
			next := append([]schedRule(nil), rules...)
			next[i].End = t
			save(next)
		})
		if r.End <= r.Start {
			at(gtx, image.Pt(x, y), image.Pt(gtx.Dp(28), ch), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
					return p.k.label(gtx, 12, theme.Regular, theme.WithAlpha(cl.pry, 0xb3), "+1d")
				})
			})
		}
		x += gtx.Dp(28) + gap
		at(gtx, image.Pt(x, y), image.Pt(gtx.Dp(172), ch), func(gtx layout.Context) layout.Dimensions {
			return p.sselect(gtx, "status."+rk, gtx.Dp(172), ch, r.Status, o.Statuses, false, func(v string) {
				next := append([]schedRule(nil), rules...)
				next[i].Status = v
				save(next)
			})
		})
		x += gtx.Dp(172) + gap
		at(gtx, image.Pt(x, y), image.Pt(ch, ch), func(gtx layout.Context) layout.Dimensions {
			d, c := p.ghost(gtx, "rmrule."+rk, "✕", ch, ch)
			if c {
				next := append(append([]schedRule(nil), rules[:i]...), rules[i+1:]...)
				p.daysOpen = ""
				save(next)
			}
			return d
		})
		y += ch + gtx.Dp(8)
	}
	if len(rules) > 0 {
		y += gtx.Dp(10) - gtx.Dp(8)
	}
	d := at(gtx, image.Pt(0, y), image.Pt(gtx.Dp(200), ch), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
			d, c := p.btn(gtx, "addrule."+key, "+ New", popups.BtnOutline, popups.BtnSmall, len(rules) >= 50)
			if c {
				status := ""
				if len(o.Statuses) > 0 {
					status = o.Statuses[0].Value
				}
				save(append(append([]schedRule(nil), rules...), schedRule{Days: []int{}, Start: "17:00", End: "09:00", Status: status}))
			}
			return d
		})
	})
	y += d.Size.Y
	if havePop { // above everything after it
		m := op.Record(gtx.Ops)
		st := op.Offset(popAt).Push(gtx.Ops)
		pop.Add(gtx.Ops)
		st.Pop()
		op.Defer(gtx.Ops, m.Stop())
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// daysPopover: presets, then a checkbox per day (Sun first).
func (p *slickPage) daysPopover(gtx layout.Context, rk string, days []int, setDays func([]int)) layout.Dimensions {
	cl := p.colors()
	w := gtx.Dp(180)
	pad := gtx.Dp(6)
	m := op.Record(gtx.Ops)
	y := pad
	for _, pr := range dayPresets {
		pr := pr
		at(gtx, image.Pt(pad, y), image.Pt(w-2*pad, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			bw := w - 2*pad
			d, c := p.ghost(gtx, "preset."+rk+"."+pr.label, "", bw, gtx.Dp(28))
			at(gtx, image.Pt(gtx.Dp(10), 0), image.Pt(bw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, gtx.Dp(28), func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 13, theme.Bold, cl.pry, pr.label) })
			})
			if sameDays(days, pr.days) {
				at(gtx, image.Pt(bw-gtx.Dp(22), 0), image.Pt(gtx.Dp(14), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, gtx.Dp(28), func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 13, theme.Bold, cl.pry, "✓") })
				})
			}
			if c {
				setDays(append([]int(nil), pr.days...))
			}
			return d
		})
		y += gtx.Dp(28)
	}
	y += pad
	theme.Fill(gtx, image.Rect(pad, y, w-pad, y+gtx.Dp(1)), cl.popLine)
	y += gtx.Dp(1) + pad
	has := map[int]bool{}
	for _, d := range days {
		has[d] = true
	}
	for di, name := range dayNames {
		di, name := di, name
		at(gtx, image.Pt(pad, y), image.Pt(w-2*pad, gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
			b := p.k.btn("day." + rk + "." + name)
			cbHit := false
			d := clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(gtx.Dp(2), gtx.Dp(4)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					d, c := p.k.checkbox(gtx, "daycb."+rk+"."+name, has[di])
					cbHit = c
					return d
				})
				at(gtx, image.Pt(gtx.Dp(2+16+8), 0), image.Pt(w, gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, gtx.Dp(24), func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 15, theme.Regular, cl.pry, name) })
				})
				return layout.Dimensions{Size: image.Pt(w-2*pad, gtx.Dp(24))}
			})
			if clicked(b) || cbHit {
				var next []int
				for d := 0; d < 7; d++ {
					if d == di && !has[d] || d != di && has[d] {
						next = append(next, d)
					}
				}
				if next == nil {
					next = []int{}
				}
				setDays(next)
			}
			return d
		})
		y += gtx.Dp(24)
	}
	y += pad
	call := m.Stop()
	r := image.Rect(0, 0, w, y)
	// pointer-down outside closes it: a catcher behind the popover
	catcher := p.k.btn("popcatch." + rk)
	at(gtx, image.Pt(-4000, -4000), image.Pt(8000, 8000), func(gtx layout.Context) layout.Dimensions {
		return catcher.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(8000, 8000)} })
	})
	if catcher.Clicked(gtx) {
		p.daysOpen = ""
	}
	for i := 3; i >= 1; i-- { // 0 4 12 rgba(0,0,0,.25), approximated
		theme.RRect(gtx, r.Add(image.Pt(0, gtx.Dp(4))).Inset(-gtx.Dp(unit.Dp(i*2))), gtx.Dp(unit.Dp(8+i*2)), color.NRGBA{A: uint8(0x14 / i)})
	}
	theme.RRect(gtx, r, gtx.Dp(8), cl.popBg)
	theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), cl.popLine)
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: r.Max}
}

// ---- Themes ----

func (p *slickPage) themes(f *flow, st slick.Settings) {
	cl := p.colors()
	bundled := 0
	for _, t := range st.Themes {
		if t.ID != "" && t.ID != "custom" && !t.Imported {
			bundled++
		}
	}
	p.card(f, "Theme", "", func(gtx layout.Context) layout.Dimensions {
		d, c := p.btn(gtx, "importtheme", "Import theme JSON…", popups.BtnOutline, popups.BtnSmall, p.importing)
		if c {
			p.importTheme()
		}
		return d
	}, nil)
	f.y -= f.gtx.Dp(24) // the grid belongs to the section
	// grid
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		gap := gtx.Dp(12)
		cols := max(1, (f.w+gap)/(gtx.Dp(190)+gap))
		cw := (f.w - gap*(cols-1)) / cols
		y, rowH := 0, 0
		for i, t := range st.Themes {
			t := t
			col := i % cols
			if col == 0 && i > 0 {
				y += rowH + gap
				rowH = 0
			}
			d := at(gtx, image.Pt(col*(cw+gap), y), image.Pt(cw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.themeCard(gtx, cw, t)
			})
			rowH = max(rowH, d.Size.Y)
		}
		return layout.Dimensions{Size: image.Pt(f.w, y+rowH)}
	})
	f.gap(12)
	p.hint(f, p.themeStatus)
	p.hint(f, "Match Slack’s Light or Dark mode (Preferences > Appearance) to your theme for the best result.")
	if bundled == 0 { // Clack doesn't ship Slick's theme files
		f.gap(8)
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			label := "Download Slick’s themes"
			busy := p.downloadState == "busy"
			if busy {
				label = "Downloading…"
			}
			d, c := p.btn(gtx, "dlthemes", label, popups.BtnOutline, popups.BtnSmall, busy)
			if c {
				p.downloadThemes()
			}
			at(gtx, image.Pt(d.Size.X+gtx.Dp(10), 0), image.Pt(f.w-d.Size.X-gtx.Dp(10), d.Size.Y), func(gtx layout.Context) layout.Dimensions {
				msg := "Slick’s built-in themes (AMOLED, Catppuccin, Nord…) aren’t installed."
				if p.downloadState != "" && p.downloadState != "busy" {
					msg = p.downloadState
				}
				return vcenter(gtx, d.Size.Y, func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 13, theme.Regular, cl.sec, msg) })
			})
			return d
		})
	}
	f.gap(24)
	p.card(f, "Custom CSS", "", nil, []func(gtx layout.Context, w int) layout.Dimensions{
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "Your CSS", "Raw CSS applied on top of the selected theme. For a JSON theme file, use Import theme JSON above.", "", false,
				func(gtx layout.Context, _ int) layout.Dimensions {
					d, c := p.btn(gtx, "csseditor", "Open CSS editor", popups.BtnOutline, popups.BtnMedium, false)
					if c {
						p.openCSSEditor(st.UserCSS)
					}
					return d
				})
		},
	})
}

func (p *slickPage) themeCard(gtx layout.Context, w int, t slick.ThemeCard) layout.Dimensions {
	cl := p.colors()
	b := p.k.btn("theme." + t.ID)
	stripH := gtx.Dp(88)
	m := op.Record(gtx.Ops)
	ty := stripH + gtx.Dp(1) + gtx.Dp(10)
	nameH := gtx.Dp(22)
	at(gtx, image.Pt(gtx.Dp(12), ty), image.Pt(w-gtx.Dp(24)-gtx.Dp(22), nameH), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, nameH, func(gtx layout.Context) layout.Dimensions { return p.k.label(gtx, 15, theme.Bold, cl.pry, t.Name) })
	})
	if t.Selected {
		at(gtx, image.Pt(w-gtx.Dp(12)-gtx.Dp(16), ty+(nameH-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("check")(gtx, 16, cl.hgl)
		})
	}
	desc := t.Description // slick already appends " · Only in <mode> mode"
	if suf := " · Only in " + t.Mode + " mode"; t.Mode != "" && !strings.Contains(desc, suf) {
		desc += suf
	}
	at(gtx, image.Pt(gtx.Dp(12), ty+nameH), image.Pt(w-gtx.Dp(24), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
		return p.a.th.Layout(gtx, theme.TextStyle{Size: 12, LineHeight: 16, Color: cl.sec, MaxLines: 1}, desc)
	})
	h := ty + nameH + gtx.Dp(16) + gtx.Dp(12)
	tcall := m.Stop()
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(12)
	d := clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
		st := clip.UniformRRect(r, rad).Push(gtx.Ops)
		p.themePreview(gtx, w, stripH, t)
		theme.Fill(gtx, image.Rect(0, stripH, w, stripH+gtx.Dp(1)), cl.otlTer)
		tcall.Add(gtx.Ops)
		st.Pop()
		bc := cl.otlTer
		switch {
		case t.Selected:
			bc = cl.otlHgl
			theme.Border(gtx, r.Inset(-gtx.Dp(1)), rad+gtx.Dp(1), gtx.Dp(1), cl.otlHgl)
		case b.Hovered():
			bc = cl.otlPry
		}
		theme.Border(gtx, r, rad, gtx.Dp(1), bc)
		return layout.Dimensions{Size: r.Max}
	})
	removing := false
	if t.Imported && b.Hovered() {
		rb := p.k.btn("rmtheme." + t.ID)
		at(gtx, image.Pt(w-gtx.Dp(6)-gtx.Dp(24), gtx.Dp(6)), image.Pt(gtx.Dp(24), gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, rb, nil, func(gtx layout.Context) layout.Dimensions {
				s := gtx.Dp(24)
				theme.RRect(gtx, image.Rect(0, 0, s, s), gtx.Dp(6), color.NRGBA{A: 0x8c})
				at(gtx, image.Pt(gtx.Dp(4), gtx.Dp(4)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("trash")(gtx, 16, rgbT(0xffffff))
				})
				return layout.Dimensions{Size: image.Pt(s, s)}
			})
		})
		if clicked(rb) {
			removing = true
			id := t.ID
			p.write(func(ctx context.Context, s *slick.Slick) error {
				err := s.RemoveTheme(id)
				p.a.Post(func() {
					if err != nil {
						p.themeStatus = "Could not remove the theme."
					} else {
						p.themeStatus = "Removed imported theme."
					}
				})
				return nil
			})
		}
	}
	if clicked(b) && !removing && !t.Selected {
		id := t.ID
		p.write(func(ctx context.Context, s *slick.Slick) error { return s.SetTheme(id) })
	}
	return d
}

// themePreview draws Slick's preview strip: a 32% sidebar pane with three
// bars (text, accent, text) and a main pane with 70/45/30% bars.
func (p *slickPage) themePreview(gtx layout.Context, w, h int, t slick.ThemeCard) {
	cl := p.colors()
	if t.Preview == nil { // "Custom CSS only"
		theme.Fill(gtx, image.Rect(0, 0, w, h), cl.surf)
		s := gtx.Dp(24)
		at(gtx, image.Pt((w-s)/2, (h-s)/2), image.Pt(s, s), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("code")(gtx, 24, cl.ter)
		})
		return
	}
	pv := t.Preview
	bg, side, fg, acc := parseHexColor(pv.Background), parseHexColor(pv.Sidebar), parseHexColor(pv.Text), parseHexColor(pv.Accent)
	if pv.Background == "" {
		bg = rgbT(0x1a1d21)
	}
	theme.Fill(gtx, image.Rect(0, 0, w, h), bg)
	sw := w * 32 / 100
	theme.Fill(gtx, image.Rect(0, 0, sw, h), side)
	bar := func(x, y, bw int, c color.NRGBA, a uint8) {
		bh := gtx.Dp(6)
		theme.RRect(gtx, image.Rect(x, y, x+bw, y+bh), bh/2, theme.Over(theme.WithAlpha(c, a), bg))
	}
	sideBar := func(x, y, bw int, c color.NRGBA, a uint8) {
		bh := gtx.Dp(6)
		theme.RRect(gtx, image.Rect(x, y, x+bw, y+bh), bh/2, theme.Over(theme.WithAlpha(c, a), side))
	}
	padX, padY, step := gtx.Dp(10), gtx.Dp(14), gtx.Dp(13)
	iw := sw - 2*padX
	sideBar(padX, padY, iw, fg, 0x8c)
	sideBar(padX, padY+step, iw, acc, 0xff)
	sideBar(padX, padY+2*step, iw, fg, 0x8c)
	mx := sw + padX
	mw := w - sw - 2*padX
	bar(mx, padY, mw*70/100, fg, 0x8c)
	bar(mx, padY+step, mw*45/100, fg, 0x8c)
	bar(mx, padY+2*step, mw*30/100, acc, 0xff)
}

func (p *slickPage) importTheme() {
	a := p.a
	p.importing = true
	go func() {
		files, err := a.pickFiles(".json", false)
		a.Post(func() {
			defer func() { p.importing = false; a.Invalidate() }()
			if err != nil || len(files) == 0 {
				if err != nil {
					p.themeStatus = "Could not import the theme."
				}
				return
			}
			fl := files[0]
			if len(fl.Data) > 256<<10 {
				p.themeStatus = "Theme files must be smaller than 256 KiB."
				return
			}
			var probe any
			if json.Unmarshal(fl.Data, &probe) != nil {
				p.themeStatus = "The theme file is not valid JSON."
				return
			}
			s := a.Slick()
			if s == nil {
				return
			}
			id, err := s.ImportTheme(fl.Data, fl.Name)
			if err != nil {
				msg := err.Error()
				switch {
				case strings.Contains(msg, "palette") || strings.Contains(msg, "expected"):
					p.themeStatus = "Expected a Slick theme with palette, sidebar, vars, or css fields."
				case strings.Contains(msg, "save") || strings.Contains(msg, "write"):
					p.themeStatus = "Could not save the theme."
				default:
					p.themeStatus = "Could not import the theme."
				}
				return
			}
			name := id
			for _, t := range s.Settings().Themes {
				if t.ID == id {
					name = t.Name
				}
			}
			p.themeStatus = "Imported " + name + "."
		})
	}()
}

// downloadThemes fetches Slick's themes/*.json from its repository into
// <slick dir>/themes (Clack doesn't ship them). Slick reads bundled themes
// at start, so they appear after Clack restarts.
func (p *slickPage) downloadThemes() {
	s := p.a.Slick()
	if s == nil {
		return
	}
	dir := filepath.Join(s.Dir(), "themes")
	p.downloadState = "busy"
	a := p.a
	go func() {
		n, err := fetchSlickThemes(dir)
		a.Post(func() {
			switch {
			case err != nil:
				p.downloadState = "Couldn’t download the themes: " + err.Error()
			default:
				p.downloadState = fmt.Sprintf("Downloaded %d themes. Restart %s to use them.", n, AppName)
			}
			a.Invalidate()
		})
	}()
}

func fetchSlickThemes(dir string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	get := func(u string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", res.StatusCode)
		}
		return io.ReadAll(io.LimitReader(res.Body, 1<<20))
	}
	b, err := get("https://api.github.com/repos/3kh0/slick/contents/themes")
	if err != nil {
		return 0, err
	}
	var list []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"download_url"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	n := 0
	for _, f := range list {
		if !strings.HasSuffix(f.Name, ".json") || strings.ContainsAny(f.Name, `/\`) || !strings.HasPrefix(f.DownloadURL, "https://") {
			continue
		}
		data, err := get(f.DownloadURL)
		if err != nil {
			return n, err
		}
		var probe map[string]any
		if json.Unmarshal(data, &probe) != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, f.Name), data, 0o600); err != nil {
			return n, err
		}
		n++
	}
	if n == 0 {
		return 0, errors.New("no themes found")
	}
	return n, nil
}

// openCSSEditor is the host's CSS editor (Slick opens a separate editor
// window): a modal with a code box that saves as you type.
func (p *slickPage) openCSSEditor(css string) {
	if mkModal == nil {
		return
	}
	e := &widget.Editor{}
	e.SetText(css)
	saved := css
	save := func() {
		if t := e.Text(); t != saved {
			saved = t
			p.write(func(ctx context.Context, s *slick.Slick) error { return s.SetUserCSS(t) })
		}
	}
	var last time.Time
	body := func(gtx layout.Context) layout.Dimensions {
		w := gtx.Constraints.Max.X
		y := p.k.para(gtx, 13, 18, p.colors().sec, "Applied on top of the selected theme as you type. Clack applies Slack's class names, like Slick.").Size.Y + gtx.Dp(10)
		at(gtx, image.Pt(0, y), image.Pt(w, gtx.Dp(420)), func(gtx layout.Context) layout.Dimensions {
			return p.k.input(gtx, e, w, gtx.Dp(420), "/* your CSS */", true)
		})
		if e.Text() != saved {
			if last.IsZero() {
				last = gtx.Now
			}
			if gtx.Now.Sub(last) > 600*time.Millisecond {
				save()
				last = time.Time{}
			} else {
				gtx.Execute(op.InvalidateCmd{At: last.Add(650 * time.Millisecond)})
			}
		}
		return layout.Dimensions{Size: image.Pt(w, y+gtx.Dp(420))}
	}
	p.a.ShowOverlay(mkModal(p.a, "Custom CSS", body, save))
}

// ---- Install ----

func (p *slickPage) install(f *flow, st slick.Settings) {
	installURL := func() {
		u := strings.TrimSpace(p.urlEd.Text())
		if u == "" || p.installing {
			return
		}
		p.installing, p.installStatus = true, ""
		a := p.a
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			s := a.Slick()
			if s == nil {
				return
			}
			id, err := s.InstallURL(ctx, u)
			a.Post(func() {
				p.installing = false
				if err != nil {
					p.installStatus = installErr(err)
					return
				}
				p.urlEd.SetText("")
				p.installStatus = "Installed " + p.pluginName(id) + ". It will keep itself up to date."
			})
		}()
	}
	for {
		ev, ok := p.urlEd.Update(p.k.gtxNow)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			installURL()
		}
	}
	p.card(f, "Install plugins", "", nil, []func(gtx layout.Context, w int) layout.Dimensions{
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "From a URL", "Plugins installed from a URL are re-downloaded in the background and updated when the file changes.", "", true,
				func(gtx layout.Context, iw int) layout.Dimensions {
					h := gtx.Dp(36)
					bm := op.Record(gtx.Ops)
					bd, c := p.btn(gtx, "installurl", "Install", popups.BtnPrimary, popups.BtnMedium, p.installing || strings.TrimSpace(p.urlEd.Text()) == "")
					bcall := bm.Stop()
					if c {
						installURL()
					}
					inW := iw - bd.Size.X - gtx.Dp(8)
					p.sinput(gtx, &p.urlEd, inW, h, gtx.Dp(12), "https://example.com/MyPlugin.js", p.installing)
					st := op.Offset(image.Pt(inW+gtx.Dp(8), (h-bd.Size.Y)/2)).Push(gtx.Ops)
					bcall.Add(gtx.Ops)
					st.Pop()
					return layout.Dimensions{Size: image.Pt(iw, h)}
				})
		},
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "From a file", "A .js file built with bun run build:plugin.", "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				d, c := p.btn(gtx, "installfile", "Choose file…", popups.BtnOutline, popups.BtnMedium, p.installing)
				if c {
					p.installFile()
				}
				return d
			})
		},
	})
	p.card(f, "Updates", "", nil, []func(gtx layout.Context, w int) layout.Dimensions{
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "Update URL-installed plugins automatically", "", "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				d, c := p.sw(gtx, "autoupd", st.AutoUpdates, false)
				if c {
					on := !st.AutoUpdates
					p.write(func(ctx context.Context, s *slick.Slick) error { _ = s.SetAutoUpdates(on); return nil })
				}
				return d
			})
		},
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "Check now", "Fetch the latest version of every plugin installed from a URL.", "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				d, c := p.btn(gtx, "checkupd", "Check for updates", popups.BtnOutline, popups.BtnMedium, p.installing)
				if c {
					p.checkUpdates()
				}
				return d
			})
		},
	})
	p.hint(f, p.installStatus)
	if p.installStatus != "" {
		f.gap(12)
	}
	p.notice(f, "Plugins you install will have FULL ACCESS to your Slack data. Only install code you trust!", false)
}

func installErr(err error) string {
	if err == nil {
		return ""
	}
	if msg := err.Error(); msg != "" {
		return msg
	}
	return "Could not install the plugin."
}

func (p *slickPage) pluginName(id string) string {
	if s := p.a.Slick(); s != nil {
		for _, pl := range s.Settings().Plugins {
			if pl.ID == id {
				return pl.Name
			}
		}
	}
	return id
}

func (p *slickPage) installFile() {
	a := p.a
	p.installing, p.installStatus = true, ""
	go func() {
		files, err := a.pickFiles(".js", false)
		if err != nil || len(files) == 0 {
			a.Post(func() {
				p.installing = false
				if err != nil {
					p.installStatus = installErr(err)
				}
			})
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		s := a.Slick()
		if s == nil {
			return
		}
		id, err := s.Import(ctx, files[0].Data)
		a.Post(func() {
			p.installing = false
			if err != nil {
				p.installStatus = installErr(err)
				return
			}
			p.installStatus = "Installed " + p.pluginName(id) + "."
		})
	}()
}

func (p *slickPage) checkUpdates() {
	a := p.a
	p.installing, p.installStatus = true, ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s := a.Slick()
		if s == nil {
			return
		}
		res := s.UpdateUserPlugins(ctx)
		a.Post(func() {
			p.installing = false
			msg := "Everything is up to date."
			if len(res.Updated) > 0 {
				names := make([]string, len(res.Updated))
				for i, id := range res.Updated {
					names[i] = p.pluginName(id)
				}
				msg = "Updated " + strings.Join(names, ", ") + "."
			}
			if res.Failed > 0 {
				msg += fmt.Sprintf(" %d could not be checked.", res.Failed)
			}
			p.installStatus = msg
		})
	}()
}
