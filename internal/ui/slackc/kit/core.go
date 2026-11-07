// Package kit draws Taut's api.elements natively. In Taut,
// this.api.elements.Button and friends are Slack's own React components;
// in Clack the JS side renders each as a host node
// {"t":"@","n":"<SlackComponentName>","p":props,"c":children} and kit
// draws a native look-alike with Clack's theme, icons and popups, so plugin
// UI matches the rest of the client.
//
// Call Register once (idempotent) before plugins render. Component
// coverage, honoured props and gaps are listed in README.md.
package kit

import (
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// ---- registration ----

type entry struct {
	fn    slackc.HostComponent
	names []string
}

var (
	entries []entry
	regOnce sync.Once
)

// add queues a component for Register; component files call it from init.
func add(fn slackc.HostComponent, names ...string) {
	entries = append(entries, entry{fn: fn, names: names})
}

// Register registers every kit component with slackc under the names the
// JS side uses (Slack's component names plus the api.elements aliases).
// Safe to call more than once.
func Register() {
	regOnce.Do(func() {
		for _, e := range entries {
			slackc.RegisterHost(e.fn, e.names...)
		}
	})
}

// Names lists every name kit registers, sorted.
func Names() []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.names...)
	}
	sort.Strings(out)
	return out
}

// ---- environment ----

func envOf() slackc.Env { return slackc.CurrentEnv() }

var (
	fallbackOnce sync.Once
	fallbackTh   *theme.Theme
)

// thm is the app theme (a light default before Init, e.g. in tests).
func thm() *theme.Theme {
	if th := envOf().Theme; th != nil {
		return th
	}
	fallbackOnce.Do(func() { fallbackTh = theme.New(false) })
	return fallbackTh
}

func invalidate() {
	if f := envOf().Invalidate; f != nil {
		f()
	}
}

func showOverlay(o popups.Overlay) {
	if f := envOf().ShowOverlay; f != nil {
		f(o)
	}
}

func toast(s string) {
	if f := envOf().Toast; f != nil {
		f(s)
	}
}

func openURL(u string) {
	if f := envOf().OpenURL; f != nil && u != "" {
		f(u)
	}
}

func navigate(route string) {
	if f := envOf().Navigate; f != nil {
		f(route)
	}
}

// pointerWin is the last pointer position in window coordinates.
func pointerWin() (image.Point, bool) {
	if f := envOf().PointerWin; f != nil {
		return f(), true
	}
	return image.Point{}, false
}

// popHost adapts slackc.Env to popups.Host, so kit can open popups'
// menus, modals and popovers. One pointer: popups keys maps by Host.
type popHost struct{}

var ph = &popHost{}

func (*popHost) Core() *core.App              { return envOf().Core }
func (*popHost) Theme() *theme.Theme          { return thm() }
func (*popHost) Images() *imgcache.Cache      { return envOf().Images }
func (*popHost) Invalidate()                  { invalidate() }
func (*popHost) Navigate(route string)        { navigate(route) }
func (*popHost) OpenThread(ch, ts string)     { navigate("c/" + ch + "/t/" + ts) }
func (*popHost) CloseSecondary()              {}
func (*popHost) ShowOverlay(o popups.Overlay) { showOverlay(o) }
func (*popHost) OpenEmojiPicker(anchor image.Point, onPick func(name string)) {
	if f := envOf().OpenEmojiPicker; f != nil {
		f(anchor, onPick)
	}
}
func (*popHost) OpenProfile(anchor image.Point, userID string) {
	if f := envOf().OpenProfile; f != nil {
		f(anchor, userID)
	}
}
func (*popHost) OpenMessageMenu(anchor image.Point, m core.Message) {}
func (*popHost) OpenLightbox(img richtext.Image, m core.Message)    { openURL(img.URL) }
func (*popHost) Toast(text string)                                  { toast(text) }

// ---- colors (Slack 2026 tokens; the popups set plus form controls) ----

type colors struct {
	Bg, BgAlt, BgHover   color.NRGBA
	Fg, Fg2, Fg3, Muted  color.NRGBA
	Ring, Divider        color.NRGBA
	Shadow, ModalShadow  color.NRGBA
	Backdrop             color.NRGBA
	Highlight, HighText  color.NRGBA // menu row highlight
	Danger, DangerBg     color.NRGBA
	Link                 color.NRGBA
	Primary, PrimaryText color.NRGBA
	BtnBorder            color.NRGBA
	BtnBg, BtnHover      color.NRGBA
	GhostHover           color.NRGBA
	Disabled, DisText    color.NRGBA
	Tooltip, TooltipText color.NRGBA
	Focus, FocusRing     color.NRGBA
	InputBorder          color.NRGBA
	InputBorderHover     color.NRGBA
	Placeholder          color.NRGBA
	Error                color.NRGBA // InlineAlert, invalid inputs
	Warning              color.NRGBA
	Check, CheckText     color.NRGBA // checked checkbox fill / mark
	CheckBorder          color.NRGBA
	PillActiveBg         color.NRGBA // FilterPill isActive
	PillActiveFg         color.NRGBA
	TagBg, TagFg         color.NRGBA
	InfoBg, InfoFg       color.NRGBA // Tag style="informative"
	Skeleton             color.NRGBA
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func rgba(v uint32, a float32) color.NRGBA {
	c := rgb(v)
	c.A = uint8(a*255 + .5)
	return c
}

var lightColors = colors{
	Bg: rgb(0xffffff), BgAlt: rgb(0xf8f8f8), BgHover: rgb(0xf8f8f8),
	Fg: rgb(0x1d1c1d), Fg2: rgb(0x454447), Fg3: rgb(0x5e5d60), Muted: rgb(0x616061),
	Ring: rgba(0x1d1c1d, .13), Divider: rgba(0x5e5d60, .13),
	Shadow: rgba(0x000000, .12), ModalShadow: rgba(0x000000, .35),
	Backdrop:  rgba(0x1d1c1d, .70),
	Highlight: rgb(0x1264a3), HighText: rgb(0xffffff),
	Danger: rgb(0xc01343), DangerBg: rgb(0xe01e5a),
	Link:    rgb(0x1264a3),
	Primary: rgb(0x007a5a), PrimaryText: rgb(0xffffff),
	BtnBorder: rgba(0x5e5d60, .45), BtnBg: rgb(0xffffff), BtnHover: rgb(0xf8f8f8),
	GhostHover: rgba(0x1d1c1d, .06),
	Disabled:   rgb(0xeaeaea), DisText: rgb(0x868686),
	Tooltip: rgb(0x1d1c1d), TooltipText: rgb(0xffffff),
	Focus: rgb(0x1264a3), FocusRing: rgba(0x1264a3, .3),
	InputBorder: rgba(0x1d1c1d, .3), InputBorderHover: rgba(0x1d1c1d, .5),
	Placeholder: rgb(0x868686),
	Error:       rgb(0xe01e5a), Warning: rgb(0xe8912d),
	Check: rgb(0x1264a3), CheckText: rgb(0xffffff), CheckBorder: rgba(0x1d1c1d, .5),
	PillActiveBg: rgba(0x1d9bd1, .1), PillActiveFg: rgb(0x1264a3),
	TagBg: rgba(0x1d1c1d, .06), TagFg: rgb(0x454447),
	InfoBg: rgba(0x1d9bd1, .1), InfoFg: rgb(0x1264a3),
	Skeleton: rgb(0xeaeaea),
}

var darkColors = colors{
	Bg: rgb(0x222529), BgAlt: rgb(0x1a1d21), BgHover: rgb(0x27292d),
	Fg: rgb(0xd1d2d3), Fg2: rgb(0xababad), Fg3: rgb(0x9a9b9e), Muted: rgb(0xababad),
	Ring: rgba(0xd1d2d3, .13), Divider: rgba(0xd1d2d3, .13),
	Shadow: rgba(0x000000, .35), ModalShadow: rgba(0x000000, .6),
	Backdrop:  rgba(0x000000, .70),
	Highlight: rgb(0x1164a3), HighText: rgb(0xffffff),
	Danger: rgb(0xe8577e), DangerBg: rgb(0xe01e5a),
	Link:    rgb(0x1d9bd1),
	Primary: rgb(0x007a5a), PrimaryText: rgb(0xffffff),
	BtnBorder: rgba(0xd1d2d3, .3), BtnBg: rgb(0x222529), BtnHover: rgb(0x2c2d30),
	GhostHover: rgba(0xd1d2d3, .08),
	Disabled:   rgb(0x2c2d30), DisText: rgb(0x9a9b9e),
	Tooltip: rgb(0xf8f8f8), TooltipText: rgb(0x1d1c1d),
	Focus: rgb(0x1d9bd1), FocusRing: rgba(0x1d9bd1, .3),
	InputBorder: rgba(0xd1d2d3, .3), InputBorderHover: rgba(0xd1d2d3, .5),
	Placeholder: rgb(0x9a9b9e),
	Error:       rgb(0xe8577e), Warning: rgb(0xe8912d),
	Check: rgb(0x1d9bd1), CheckText: rgb(0x1a1d21), CheckBorder: rgba(0xd1d2d3, .5),
	PillActiveBg: rgba(0x1d9bd1, .15), PillActiveFg: rgb(0x1d9bd1),
	TagBg: rgba(0xd1d2d3, .1), TagFg: rgb(0xababad),
	InfoBg: rgba(0x1d9bd1, .15), InfoFg: rgb(0x1d9bd1),
	Skeleton: rgb(0x2c2d30),
}

func init() {
	// Gio blends in linear light; pre-adjust translucent tokens so they
	// read like CSS over the card background (as popups does).
	for _, p := range []struct {
		c  *colors
		bg color.NRGBA
	}{{&lightColors, rgb(0xffffff)}, {&darkColors, rgb(0x222529)}} {
		c := p.c
		for _, f := range []*color.NRGBA{&c.Ring, &c.Divider, &c.Shadow, &c.ModalShadow, &c.Backdrop,
			&c.BtnBorder, &c.GhostHover, &c.FocusRing, &c.InputBorder, &c.InputBorderHover, &c.CheckBorder,
			&c.PillActiveBg, &c.TagBg, &c.InfoBg} {
			*f = theme.LinearAlpha(*f, p.bg)
		}
	}
}

// colorsOf returns the token set for th.
func colorsOf(th *theme.Theme) *colors {
	if th != nil && th.Dark {
		return &darkColors
	}
	return &lightColors
}

// ---- layout helpers ----

func dp(gtx layout.Context, v float32) int { return gtx.Dp(unit.Dp(v)) }

// at lays out w with its origin at p.
func at(gtx layout.Context, p image.Point, w layout.Widget) layout.Dimensions {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	return w(gtx)
}

// record lays out w into a macro.
func record(gtx layout.Context, w layout.Widget) (op.CallOp, layout.Dimensions) {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	return m.Stop(), d
}

// callAt replays a macro at p.
func callAt(gtx layout.Context, c op.CallOp, p image.Point) {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	c.Add(gtx.Ops)
}

// loose returns gtx with Min zero and Max sz.
func loose(gtx layout.Context, sz image.Point) layout.Context {
	gtx.Constraints = layout.Constraints{Max: sz}
	return gtx
}

// exact returns gtx constrained to exactly sz.
func exact(gtx layout.Context, sz image.Point) layout.Context {
	gtx.Constraints = layout.Exact(sz)
	return gtx
}

// vcenter lays out w vertically centered in a band of height h.
func vcenter(gtx layout.Context, h int, w layout.Widget) layout.Dimensions {
	call, d := record(gtx, w)
	callAt(gtx, call, image.Pt(0, (h-d.Size.Y)/2))
	return layout.Dimensions{Size: image.Pt(d.Size.X, h)}
}

// label draws one ellipsized line at its natural size.
func label(gtx layout.Context, th *theme.Theme, size float32, w font.Weight, c color.NRGBA, s string) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	return th.Label(gtx, unit.Sp(size), w, c, s)
}

// para draws wrapping text (maxLines 0 = unlimited).
func para(gtx layout.Context, th *theme.Theme, size, lh float32, w font.Weight, c color.NRGBA, s string, maxLines int) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	return th.Layout(gtx, theme.TextStyle{Size: unit.Sp(size), Weight: w, Color: c, LineHeight: unit.Sp(lh), MaxLines: maxLines}, s)
}

// textW measures one line.
func textW(gtx layout.Context, th *theme.Theme, size float32, w font.Weight, s string) int {
	return th.Measure(gtx, unit.Sp(size), w, s)
}

// drawIcon draws a Slack-named icon (see clackIcon) at size dp.
func drawIcon(gtx layout.Context, slackName string, size float32, c color.NRGBA) layout.Dimensions {
	return icons.Get(clackIcon(slackName))(gtx, unit.Dp(size), c)
}

// cursor sets a cursor over sz.
func cursor(gtx layout.Context, sz image.Point, c pointer.Cursor) {
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	c.Add(gtx.Ops)
}

// ring paints a w-px border inside r (outer rrect minus inner).
func ring(gtx layout.Context, r image.Rectangle, radius, w int, c color.NRGBA, fill color.NRGBA) {
	theme.RRect(gtx, r, radius, c)
	ir := radius - w
	if ir < 0 {
		ir = 0
	}
	theme.RRect(gtx, r.Inset(w), ir, fill)
}

// softShadow approximates CSS box-shadow 0 dy blur c (Gio has no blur).
func softShadow(gtx layout.Context, r image.Rectangle, radius, dy, blur int, c color.NRGBA) {
	if blur <= 0 || c.A == 0 {
		return
	}
	steps := blur / 2
	if steps < 3 {
		steps = 3
	}
	if steps > 10 {
		steps = 10
	}
	base := r.Add(image.Pt(0, dy))
	for i := steps; i >= 1; i-- {
		grow := blur * i / (steps * 2)
		a := float32(c.A) * 2 / float32(steps+1) * float32(steps-i+1) / float32(steps)
		col := c
		col.A = uint8(a + .5)
		if col.A == 0 {
			continue
		}
		theme.RRect(gtx, base.Inset(-grow), radius+grow, col)
	}
}

// card paints a popover card: shadow, 1px ring, background.
func card(gtx layout.Context, cl *colors, r image.Rectangle, radius int) {
	softShadow(gtx, r, radius, dp(gtx, 4), dp(gtx, 12), cl.Shadow)
	theme.RRect(gtx, r.Inset(-1), radius+1, cl.Ring)
	theme.RRect(gtx, r, radius, cl.Bg)
}

// ---- per-instance state ----

// stateOf returns the host instance's *T (one state type per component).
func stateOf[T any](h *slackc.Host) *T {
	v := h.State(func() any { return new(T) })
	if s, ok := v.(*T); ok {
		return s
	}
	return new(T)
}

// ---- window origin ----

// origin learns a widget's window position from pointer events, like
// ui's originTracker: Gio has no global transform, but a pass-through
// handler sees the same event in local coordinates that the root sees in
// window coordinates, so origin = window − local.
type origin struct {
	tag int
	pt  image.Point
	ok  bool
}

// track reads last frame's events and registers a pass-through handler
// over r (local coordinates). Call it while laying out the widget.
func (o *origin) track(gtx layout.Context, r image.Rectangle) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &o.tag, Kinds: pointer.Move | pointer.Enter | pointer.Press | pointer.Drag | pointer.Release})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok {
			o.learn(pe.Position.X, pe.Position.Y)
		}
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &o.tag)
}

// learn pairs a local pointer position with the window pointer.
func (o *origin) learn(x, y float32) {
	if w, ok := pointerWin(); ok {
		o.pt = w.Sub(image.Pt(int(x+.5), int(y+.5)))
		o.ok = true
	}
}

// get returns the learned origin ((0,0) until the pointer visits).
func (o *origin) get() image.Point { return o.pt }

// toWin converts a local point to window coordinates.
func (o *origin) toWin(p image.Point) image.Point { return p.Add(o.pt) }

// ---- floating layers ----

// layer draws content above everything (op.Defer) at a window position,
// for Popover, Tooltip-like and menu content that stays part of the host
// tree. With modal it catches presses outside the content and Escape, and
// learns the host's window origin from pointer moves anywhere in the
// window (the host may never be hovered).
type layer struct {
	scrim, block, focus int
	focused             bool
}

const far = 1 << 16

// float draws call (laid out at the origin, size sz) with its top-left at
// window position win. It reports a press outside the content and an
// Escape press since the last frame (modal only).
func (l *layer) float(gtx layout.Context, org *origin, win image.Point, call op.CallOp, sz image.Point, modal bool) (outside, esc bool) {
	if modal {
		for {
			e, ok := gtx.Event(pointer.Filter{Target: &l.scrim, Kinds: pointer.Press | pointer.Move | pointer.Enter | pointer.Drag | pointer.Scroll,
				ScrollY: pointer.ScrollRange{Min: -far, Max: far}, ScrollX: pointer.ScrollRange{Min: -far, Max: far}})
			if !ok {
				break
			}
			if pe, ok := e.(pointer.Event); ok {
				org.learn(pe.Position.X, pe.Position.Y)
				if pe.Kind == pointer.Press {
					outside = true
				}
			}
		}
		for {
			e, ok := gtx.Event(pointer.Filter{Target: &l.block, Kinds: pointer.Press | pointer.Release | pointer.Scroll | pointer.Move | pointer.Enter,
				ScrollY: pointer.ScrollRange{Min: -far, Max: far}})
			if !ok {
				break
			}
			if pe, ok := e.(pointer.Event); ok {
				// block coordinates are content-local: content = win − org.
				off := win.Sub(org.get())
				org.learn(pe.Position.X+float32(off.X), pe.Position.Y+float32(off.Y))
			}
		}
		for {
			e, ok := gtx.Event(key.FocusFilter{Target: &l.focus}, key.Filter{Focus: &l.focus, Name: key.NameEscape})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press && ke.Name == key.NameEscape {
				esc = true
			}
		}
	}
	m := op.Record(gtx.Ops)
	if modal {
		st := clip.Rect{Min: image.Pt(-far, -far), Max: image.Pt(far, far)}.Push(gtx.Ops)
		event.Op(gtx.Ops, &l.scrim)
		st.Pop()
	}
	off := op.Offset(win.Sub(org.get())).Push(gtx.Ops)
	if modal {
		bs := clip.Rect{Max: sz}.Push(gtx.Ops)
		event.Op(gtx.Ops, &l.block)
		bs.Pop()
		ps := pointer.PassOp{}.Push(gtx.Ops)
		ks := clip.Rect{Max: sz}.Push(gtx.Ops)
		event.Op(gtx.Ops, &l.focus)
		ks.Pop()
		ps.Pop()
	}
	call.Add(gtx.Ops)
	off.Pop()
	op.Defer(gtx.Ops, m.Stop())
	if modal && !l.focused {
		gtx.Execute(key.FocusCmd{Tag: &l.focus})
		l.focused = true
	}
	if !modal {
		l.focused = false
	}
	return outside, esc
}

// reset forgets focus so the next open grabs it again.
func (l *layer) reset() { l.focused = false }

// place positions a box of size sz next to anchor in a window of size win
// (margin m), preferring side and flipping when there is no room.
// align: 0 start, 1 center, 2 end along the cross axis.
func place(anchor image.Rectangle, sz, win image.Point, m int, side popups.Side, align int) image.Point {
	var p image.Point
	switch side {
	case popups.Below, popups.Above:
		switch align {
		case 1:
			p.X = anchor.Min.X + (anchor.Dx()-sz.X)/2
		case 2:
			p.X = anchor.Max.X - sz.X
		default:
			p.X = anchor.Min.X
		}
		below, above := anchor.Max.Y, anchor.Min.Y-sz.Y
		if side == popups.Below {
			p.Y = below
			if win.Y > 0 && below+sz.Y > win.Y-m && above >= m {
				p.Y = above
			}
		} else {
			p.Y = above
			if above < m && (win.Y == 0 || below+sz.Y <= win.Y-m) {
				p.Y = below
			}
		}
	default:
		switch align {
		case 1:
			p.Y = anchor.Min.Y + (anchor.Dy()-sz.Y)/2
		case 2:
			p.Y = anchor.Max.Y - sz.Y
		default:
			p.Y = anchor.Min.Y
		}
		right, left := anchor.Max.X, anchor.Min.X-sz.X
		if side == popups.Right {
			p.X = right
			if win.X > 0 && right+sz.X > win.X-m && left >= m {
				p.X = left
			}
		} else {
			p.X = left
			if left < m && (win.X == 0 || right+sz.X <= win.X-m) {
				p.X = right
			}
		}
	}
	if win.X > 0 && p.X+sz.X > win.X-m {
		p.X = win.X - m - sz.X
	}
	if win.Y > 0 && p.Y+sz.Y > win.Y-m {
		p.Y = win.Y - m - sz.Y
	}
	if p.X < m {
		p.X = m
	}
	if p.Y < m {
		p.Y = m
	}
	return p
}

// position parses Slack's position strings ("top", "bottom-left",
// "right", "top-right"…) into a side and a cross-axis alignment.
func position(s string, def popups.Side) (popups.Side, int) {
	side, align := def, 0
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(s)), "-", 2)
	switch parts[0] {
	case "top", "above":
		side = popups.Above
	case "bottom", "below":
		side = popups.Below
	case "left":
		side = popups.Left
	case "right":
		side = popups.Right
	case "":
		return def, 0
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "left", "top", "start":
			align = 0
		case "right", "bottom", "end":
			align = 2
		case "center", "middle":
			align = 1
		}
	} else if side == popups.Above || side == popups.Below || side == popups.Left || side == popups.Right {
		align = 1
	}
	return side, align
}

// ---- DOM-like events ----

func cssPx(gtx layout.Context, v int) float64 {
	s := gtx.Metric.PxPerDp
	if s == 0 {
		s = 1
	}
	return float64(float32(v) / s)
}

// mouseEvent is the event object React passes to onClick/onMouseDown…:
// {type, button, buttons, clientX, clientY, …}. win is in window px.
func mouseEvent(gtx layout.Context, typ string, button int, win image.Point, mods key.Modifiers) slackc.Props {
	x, y := cssPx(gtx, win.X), cssPx(gtx, win.Y)
	return slackc.Props{
		"type": typ, "button": button, "buttons": 1 << uint(button), "detail": 1,
		"clientX": x, "clientY": y, "pageX": x, "pageY": y, "screenX": x, "screenY": y,
		"altKey": mods.Contain(key.ModAlt), "ctrlKey": mods.Contain(key.ModCtrl),
		"shiftKey": mods.Contain(key.ModShift), "metaKey": mods.Contain(key.ModSuper),
		"bubbles": true, "cancelable": true, "defaultPrevented": false,
	}
}

// clickEvent is mouseEvent "click" at the last window pointer position.
func clickEvent(gtx layout.Context) slackc.Props {
	p, _ := pointerWin()
	return mouseEvent(gtx, "click", 0, p, 0)
}

// changeEvent is React's onChange event for inputs: target carries
// value and checked.
func changeEvent(value string, checked bool, id string) slackc.Props {
	t := slackc.Props{"value": value, "checked": checked, "id": id}
	return slackc.Props{"type": "change", "target": t, "currentTarget": t, "bubbles": true}
}

// keyEvent is a keydown/keyup event object.
func keyEvent(typ, name string, mods key.Modifiers, value string) slackc.Props {
	k := keyName(name)
	t := slackc.Props{"value": value, "tagName": "INPUT", "type": "text"}
	return slackc.Props{
		"type": typ, "key": k, "code": keyCode(k),
		"altKey": mods.Contain(key.ModAlt), "ctrlKey": mods.Contain(key.ModCtrl),
		"shiftKey": mods.Contain(key.ModShift), "metaKey": mods.Contain(key.ModSuper),
		"repeat": false, "isComposing": false, "target": t, "currentTarget": t,
		"nativeEvent": slackc.Props{"isComposing": false}, "defaultPrevented": false,
	}
}

// focusEvent is a focus/blur event object.
func focusEvent(typ, value string) slackc.Props {
	t := slackc.Props{"value": value}
	return slackc.Props{"type": typ, "target": t, "currentTarget": t}
}

// keyName maps Gio key names to DOM KeyboardEvent.key.
func keyName(n string) string {
	switch n {
	case key.NameReturn, key.NameEnter:
		return "Enter"
	case key.NameEscape:
		return "Escape"
	case key.NameTab:
		return "Tab"
	case key.NameSpace:
		return " "
	case key.NameDeleteBackward:
		return "Backspace"
	case key.NameDeleteForward:
		return "Delete"
	case key.NameUpArrow:
		return "ArrowUp"
	case key.NameDownArrow:
		return "ArrowDown"
	case key.NameLeftArrow:
		return "ArrowLeft"
	case key.NameRightArrow:
		return "ArrowRight"
	case key.NameHome:
		return "Home"
	case key.NameEnd:
		return "End"
	case key.NamePageUp:
		return "PageUp"
	case key.NamePageDown:
		return "PageDown"
	}
	if len(n) == 1 {
		return strings.ToLower(n)
	}
	return n
}

func keyCode(k string) string {
	switch {
	case k == " ":
		return "Space"
	case len(k) == 1 && k[0] >= 'a' && k[0] <= 'z':
		return "Key" + strings.ToUpper(k)
	case len(k) == 1 && k[0] >= '0' && k[0] <= '9':
		return "Digit" + k
	}
	return k
}

// ---- prop helpers ----

// nodesText is the text content of host nodes (React children or an
// element prop): text nodes concatenated, components' text props
// (MrkdwnElement text, Label text) included.
func nodesText(ns []*slackc.Node) string {
	var b strings.Builder
	var walk func(ns []*slackc.Node)
	walk = func(ns []*slackc.Node) {
		for _, n := range ns {
			if n == nil {
				continue
			}
			switch {
			case n.IsText():
				b.WriteString(n.V)
			case n.T == "br":
				b.WriteByte('\n')
			default:
				if n.IsComponent() {
					if s, ok := n.P["text"].(string); ok {
						b.WriteString(s)
					}
				}
				walk(n.C)
			}
		}
	}
	walk(ns)
	return b.String()
}

// valueText is the text of a React-node-ish value: a string, number or
// Element (anything else is "").
func valueText(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case slackc.Element:
		return nodesText(v.Nodes)
	case []any:
		var b strings.Builder
		for _, x := range v {
			b.WriteString(valueText(x))
		}
		return b.String()
	}
	return ""
}

// plainChildren returns the children's text when every child is text (so
// the component can draw it with the theme like Clack's own widgets).
func plainChildren(h *slackc.Host) (string, bool) {
	if h.Node == nil || len(h.Node.C) == 0 {
		return "", false
	}
	for _, c := range h.Node.C {
		if !c.IsText() {
			return "", false
		}
	}
	return nodesText(h.Node.C), true
}

// funcProp returns a function prop's JSFunc, if it is one.
func funcProp(p slackc.Props, k string) (slackc.JSFunc, bool) {
	f, ok := p[k].(slackc.JSFunc)
	return f, ok
}

// hasFunc reports a JS or Go function prop.
func hasFunc(p slackc.Props, k string) bool {
	_, ok := p.Func(k)
	return ok
}

// parseLen parses a CSS length prop: a number (px) or "12px", "50%",
// "1.5rem"/"em" (16px). pct reports a percentage.
func parseLen(v any) (val float32, pct, ok bool) {
	switch v := v.(type) {
	case float64:
		return float32(v), false, true
	case int:
		return float32(v), false, true
	case int64:
		return float32(v), false, true
	case string:
		s := strings.TrimSpace(strings.ToLower(v))
		mul := float32(1)
		switch {
		case strings.HasSuffix(s, "%"):
			s, pct = strings.TrimSuffix(s, "%"), true
		case strings.HasSuffix(s, "px"):
			s = strings.TrimSuffix(s, "px")
		case strings.HasSuffix(s, "rem"):
			s, mul = strings.TrimSuffix(s, "rem"), 16
		case strings.HasSuffix(s, "em"):
			s, mul = strings.TrimSuffix(s, "em"), 16
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
		if err != nil {
			return 0, false, false
		}
		return float32(f) * mul, pct, true
	}
	return 0, false, false
}

// cssLen resolves a width prop against full px; ok=false when unset.
func cssLen(gtx layout.Context, v any, full int) (int, bool) {
	f, pct, ok := parseLen(v)
	if !ok {
		return 0, false
	}
	if pct {
		return int(float32(full)*f/100 + .5), true
	}
	return dp(gtx, f), true
}

// sizeOf maps Slack's size prop to small/medium/large (0/1/2).
func sizeOf(p slackc.Props, def int) int {
	switch p.StrOr("size", "") {
	case "small", "sm", "tiny":
		return 0
	case "medium", "md", "default":
		return 1
	case "large", "lg":
		return 2
	}
	return def
}

// ---- children in the component's text style ----

// styled runs fn with h.Style's inherited text replaced, so children and
// element props laid out by fn draw in the component's own text style
// (white on a primary button, 13px in a tooltip…). size is px, weight
// CSS (400/700); zero keeps the inherited value.
func styled(h *slackc.Host, c color.NRGBA, size float32, weight int, fn func() layout.Dimensions) layout.Dimensions {
	old := h.Style
	var s css.Style
	if old != nil {
		s = *old
	} else {
		s = css.Initial(c)
	}
	s.Color = c
	if size > 0 {
		s.FontSize = size
	}
	if weight > 0 {
		s.FontWeight = weight
	}
	h.Style = &s
	defer func() { h.Style = old }()
	return fn()
}

// children lays out the host's children in a text style.
func children(gtx layout.Context, h *slackc.Host, c color.NRGBA, size float32, weight int) layout.Dimensions {
	return styled(h, c, size, weight, func() layout.Dimensions { return h.Children(gtx) })
}

// element lays out an element-or-text prop in a text style.
func element(gtx layout.Context, h *slackc.Host, prop string, c color.NRGBA, size float32, weight int) layout.Dimensions {
	return styled(h, c, size, weight, func() layout.Dimensions { return h.Element(gtx, prop) })
}

// textOrElement draws a string prop with the theme (Clack's exact text
// look) and anything else (an Element) through the host tree.
func textOrElement(gtx layout.Context, h *slackc.Host, prop string, th *theme.Theme, size, lh float32, w font.Weight, c color.NRGBA, maxLines int) layout.Dimensions {
	switch v := h.Props[prop].(type) {
	case string:
		return para(gtx, th, size, lh, w, c, v, maxLines)
	case float64:
		return para(gtx, th, size, lh, w, c, valueText(v), maxLines)
	}
	cw := 400
	if w >= theme.Bold {
		cw = 700
	}
	return element(gtx, h, prop, c, size, cw)
}

// childrenOrText draws plain-text children with the theme and anything
// richer through the host tree.
func childrenOrText(gtx layout.Context, h *slackc.Host, th *theme.Theme, size, lh float32, w font.Weight, c color.NRGBA, maxLines int) layout.Dimensions {
	if s, ok := plainChildren(h); ok {
		return para(gtx, th, size, lh, w, c, s, maxLines)
	}
	cw := 400
	if w >= theme.Bold {
		cw = 700
	}
	return children(gtx, h, c, size, cw)
}
