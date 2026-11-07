package kit

import (
	"image"
	"sync"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// ---- MenuTemplateItem[] → popups menu rows ----

// asProps returns v as Props when it is an object.
func asProps(v any) (slackc.Props, bool) {
	switch m := v.(type) {
	case slackc.Props:
		return m, true
	case map[string]any:
		return slackc.Props(m), true
	}
	return nil, false
}

// asList returns v as a list.
func asList(v any) []any {
	switch l := v.(type) {
	case []any:
		return l
	case []slackc.Props:
		out := make([]any, len(l))
		for i, p := range l {
			out[i] = p
		}
		return out
	case []map[string]any:
		out := make([]any, len(l))
		for i, p := range l {
			out[i] = p
		}
		return out
	}
	return nil
}

// fireFunc invokes a template item's click (a JSFunc, or a Go func that
// crossed through JS) with the frame's context for the event object.
type fireFunc func(gtx layout.Context, fn any)

// menuItems converts Slack's MenuTemplateItem[] (key, label, description,
// type submenu/separator/header/custom/radio/checkbox, checked, template,
// icon, click, disabled, danger) into popups menu rows. fire runs a
// clicked item's click function.
func menuItems(tpl []any, fire fireFunc) []popups.MenuItem {
	out := make([]popups.MenuItem, 0, len(tpl))
	for _, v := range tpl {
		it, ok := asProps(v)
		if !ok {
			continue
		}
		typ := it.StrOr("type", "")
		switch typ {
		case "separator", "divider":
			// no leading, trailing or doubled separators (Slack drops them)
			if len(out) > 0 && !out[len(out)-1].Separator {
				out = append(out, popups.Sep())
			}
			continue
		case "header":
			if s := valueText(it["label"]); s != "" {
				out = append(out, popups.MenuItem{Header: s})
			}
			continue
		}
		mi := popups.MenuItem{
			Label:    valueText(it["label"]),
			Disabled: it.Bool("disabled"),
			Danger:   it.Bool("danger"),
		}
		if ic := it.StrOr("icon", ""); ic != "" {
			mi.Icon = clackIcon(ic)
		}
		if typ == "radio" || typ == "checkbox" {
			mi.Checked = it.Bool("checked")
		}
		if typ == "submenu" || it.Has("template") {
			mi.Sub = menuItems(asList(it["template"]), fire)
		}
		if fn, ok := it.Func("click"); ok && fire != nil {
			mi.DoCtx = func(gtx layout.Context) { fire(gtx, fn) }
		}
		if desc := valueText(it["description"]); desc != "" {
			mi.Widget = describedRow(mi, desc)
		}
		out = append(out, mi)
	}
	for len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// describedRow draws a two-line menu row (label + description), Slack's
// c-menu_item with a description, highlighted like popups' rows.
func describedRow(mi popups.MenuItem, desc string) func(gtx layout.Context, hl bool) layout.Dimensions {
	return func(gtx layout.Context, hl bool) layout.Dimensions {
		th := thm()
		cl := colorsOf(th)
		w := gtx.Constraints.Max.X
		padX, padY := dp(gtx, 24), dp(gtx, 6)
		fg, sub := cl.Fg, cl.Fg3
		if mi.Danger {
			fg = cl.Danger
		}
		if mi.Disabled {
			fg, sub = cl.DisText, cl.DisText
		}
		x := padX
		if mi.Icon != "" {
			x += dp(gtx, 28)
		}
		right := w - padX
		if mi.Checked || len(mi.Sub) > 0 {
			right -= dp(gtx, 24)
		}
		tw := right - x
		if tw < 1 {
			tw = 1
		}
		lc, ld := record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, fg, mi.Label, 0)
		})
		dc, dd := record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 13, 18, theme.Regular, sub, desc, 0)
		})
		h := padY*2 + ld.Size.Y + dd.Size.Y
		sz := image.Pt(w, h)
		if hl {
			bg := cl.Highlight
			if mi.Danger {
				bg = cl.DangerBg
			}
			theme.Fill(gtx, image.Rectangle{Max: sz}, bg)
			lc, _ = record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 15, 22, theme.Regular, cl.HighText, mi.Label, 0)
			})
			dc, _ = record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 13, 18, theme.Regular, cl.HighText, desc, 0)
			})
			fg = cl.HighText
		}
		if mi.Icon != "" {
			at(gtx, image.Pt(padX, padY+(dp(gtx, 22)-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions {
				return drawIcon(gtx, mi.Icon, 18, fg)
			})
		}
		callAt(gtx, lc, image.Pt(x, padY))
		callAt(gtx, dc, image.Pt(x, padY+ld.Size.Y))
		switch {
		case len(mi.Sub) > 0:
			at(gtx, image.Pt(w-padX-dp(gtx, 15), padY+(dp(gtx, 22)-dp(gtx, 15))/2), func(gtx layout.Context) layout.Dimensions {
				return drawIcon(gtx, "chevron-right", 15, fg)
			})
		case mi.Checked:
			at(gtx, image.Pt(w-padX-dp(gtx, 16), padY+(dp(gtx, 22)-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return drawIcon(gtx, "check", 16, fg)
			})
		}
		if !mi.Disabled {
			cursor(gtx, sz, pointer.CursorPointer)
		}
		return layout.Dimensions{Size: sz}
	}
}

// fireClick calls a template item's click with a click event.
func fireClick(gtx layout.Context, fn any) {
	ev := clickEvent(gtx)
	switch f := fn.(type) {
	case slackc.JSFunc:
		f.Call(ev)
	case func():
		f()
	case func(any):
		f(ev)
	default:
		slackc.Props{"f": fn}.Invoke("f", ev)
	}
}

// menuWidth parses a width prop ("240px", 240) into dp (0 = default).
func menuWidth(v any) float32 {
	f, pct, ok := parseLen(v)
	if !ok || pct || f <= 0 {
		return 0
	}
	return f
}

// ---- pointer helpers ----

// clickCatcher detects primary clicks and secondary presses over an area
// without taking them from the widgets drawn under it (pass-through).
type clickCatcher struct {
	tag     int
	pressed bool
}

type catch struct {
	click     bool // primary press + release inside
	secondary bool // secondary-button press
	pos       image.Point
}

func (c *clickCatcher) layout(gtx layout.Context, sz image.Point) catch {
	var r catch
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &c.tag, Kinds: pointer.Press | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		p := image.Pt(int(pe.Position.X+.5), int(pe.Position.Y+.5))
		switch pe.Kind {
		case pointer.Press:
			if pe.Buttons.Contain(pointer.ButtonSecondary) {
				r.secondary, r.pos = true, p
				c.pressed = false
			} else if pe.Buttons.Contain(pointer.ButtonPrimary) || pe.Buttons == 0 {
				c.pressed = true
			}
		case pointer.Release:
			if c.pressed && p.In(image.Rectangle{Max: sz}) {
				r.click, r.pos = true, p
			}
			c.pressed = false
		case pointer.Cancel:
			c.pressed = false
		}
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &c.tag)
	return r
}

// ---- ContextMenuTrigger ----

type ctxMenuState struct {
	org     origin
	catch   clickCatcher
	menu    *popups.Menu
	pending bool
}

// contextMenuTrigger: getTemplate (called on right-click; its
// MenuTemplateItem[] opens as a Slack menu at the pointer), desktopAppOnly
// and showSlackKitContextMenuOnDesktop (Clack always draws Slack's menu),
// width, menuAriaLabel, isDisabled, children.
func contextMenuTrigger(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[ctxMenuState](h)
	d := h.Children(gtx)
	st.org.track(gtx, image.Rectangle{Max: d.Size})
	c := st.catch.layout(gtx, d.Size)
	if c.secondary && !h.Props.Bool("isDisabled") && !st.pending && (st.menu == nil || st.menu.Dismissed()) {
		anchor := st.org.toWin(c.pos)
		if w, ok := pointerWin(); ok {
			anchor = w
		}
		width := menuWidth(h.Props["width"])
		open := func(tpl []any) {
			st.pending = false
			items := menuItems(tpl, fireClick)
			if len(items) == 0 {
				return
			}
			st.menu = popups.NewMenuOpts(ph, items, popups.MenuOpts{
				Anchor: image.Rectangle{Min: anchor, Max: anchor},
				Width:  width,
			})
			showOverlay(st.menu)
		}
		switch f := h.Props["getTemplate"].(type) {
		case slackc.JSFunc:
			st.pending = true
			f.CallResult(func(res any, err error) {
				if err != nil {
					st.pending = false
					return
				}
				open(asList(res))
			})
		case func() []any:
			open(f())
		case func() any:
			open(asList(f()))
		}
	}
	return d
}

// ---- MenuTrigger + MenuFromTemplate ----

// menuCtx links a MenuTrigger's open menu to the MenuFromTemplate its
// renderMenu returns: Go funcs can't ride RenderFn's args, so the menu
// props carry clackMenuId and MenuFromTemplate looks the trigger up here.
type menuCtx struct {
	id      int64
	anchor  image.Rectangle // trigger, window coords
	side    popups.Side
	align   int
	sub     *slackc.Sub
	claimed bool // a MenuFromTemplate draws it as a popups menu
	menu    *popups.Menu
	closed  bool
	layer   *menuLayer
	onClose []func()
}

var menus struct {
	sync.Mutex
	seq int64
	m   map[int64]*menuCtx
}

func lookupMenu(id int64) *menuCtx {
	menus.Lock()
	defer menus.Unlock()
	return menus.m[id]
}

func (c *menuCtx) close() {
	if c.closed {
		return
	}
	c.closed = true
	menus.Lock()
	delete(menus.m, c.id)
	menus.Unlock()
	if c.menu != nil && !c.menu.Dismissed() {
		c.menu.Dismiss()
	}
	if c.sub != nil {
		c.sub.Close()
	}
	for _, f := range c.onClose {
		f()
	}
	invalidate()
}

// menuLayer is the overlay that keeps renderMenu's subtree mounted and,
// for custom menus (not MenuFromTemplate), draws it at the trigger with
// outside-click and Escape dismissal.
type menuLayer struct {
	ctx   *menuCtx
	scrim int
	block int
}

func (l *menuLayer) Dismissed() bool { return l.ctx.closed }

// Dismiss is what the ui root calls on Escape.
func (l *menuLayer) Dismiss() { l.ctx.close() }

func (l *menuLayer) Layout(gtx layout.Context) layout.Dimensions {
	c := l.ctx
	if c.closed {
		return layout.Dimensions{}
	}
	win := gtx.Constraints.Max
	g := loose(gtx, win)
	call, d := record(g, c.sub.Layout)
	if c.claimed {
		// MenuFromTemplate opened a popups menu; the subtree only needs to
		// stay laid out (it draws nothing itself).
		call.Add(gtx.Ops)
		return layout.Dimensions{Size: win}
	}
	if !c.sub.Ready() {
		return layout.Dimensions{Size: win}
	}
	outside := false
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &l.scrim, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			outside = true
		}
	}
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &l.block, Kinds: pointer.Press | pointer.Release}); !ok {
			break
		}
	}
	if outside {
		c.close()
		return layout.Dimensions{}
	}
	sc := clip.Rect{Max: win}.Push(gtx.Ops)
	event.Op(gtx.Ops, &l.scrim)
	sc.Pop()
	gap := dp(gtx, 4)
	a := c.anchor
	if c.side == popups.Below || c.side == popups.Above {
		a.Min.Y -= gap
		a.Max.Y += gap
	} else {
		a.Min.X -= gap
		a.Max.X += gap
	}
	p := place(a, d.Size, win, dp(gtx, 8), c.side, c.align)
	t := op.Offset(p).Push(gtx.Ops)
	bc := clip.Rect{Max: d.Size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &l.block)
	bc.Pop()
	call.Add(gtx.Ops)
	t.Pop()
	return layout.Dimensions{Size: win}
}

type menuTriggerState struct {
	org   origin
	catch clickCatcher
	ctx   *menuCtx
}

// menuTrigger: position (top/bottom/left/right and corners), isDisabled,
// renderMenu(menuProps) (rendered through Host.RenderFn when the
// children are clicked), children (the trigger).
func menuTrigger(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[menuTriggerState](h)
	d := h.Children(gtx)
	r := image.Rectangle{Max: d.Size}
	st.org.track(gtx, r)
	c := st.catch.layout(gtx, d.Size)
	open := st.ctx != nil && !st.ctx.closed
	if c.click && !h.Props.Bool("isDisabled") {
		if open {
			st.ctx.close()
		} else if fn, ok := funcProp(h.Props, "renderMenu"); ok {
			pos := h.Props.StrOr("position", "bottom-left")
			side, align := position(pos, popups.Below)
			menus.Lock()
			if menus.m == nil {
				menus.m = map[int64]*menuCtx{}
			}
			menus.seq++
			ctx := &menuCtx{id: menus.seq, anchor: r.Add(st.org.get()), side: side, align: align}
			menus.m[ctx.id] = ctx
			menus.Unlock()
			ctx.sub = h.RenderFn(fn, slackc.Props{
				"clackMenuId":  ctx.id,
				"isOpen":       true,
				"position":     pos,
				"targetBounds": rectProps(gtx, ctx.anchor),
			})
			ctx.layer = &menuLayer{ctx: ctx}
			st.ctx = ctx
			showOverlay(ctx.layer)
		}
	}
	return d
}

// rectProps is a DOMRect-shaped object (CSS px) for a window rectangle.
func rectProps(gtx layout.Context, r image.Rectangle) slackc.Props {
	x, y := cssPx(gtx, r.Min.X), cssPx(gtx, r.Min.Y)
	w, hh := cssPx(gtx, r.Dx()), cssPx(gtx, r.Dy())
	return slackc.Props{"x": x, "y": y, "width": w, "height": hh, "left": x, "top": y, "right": x + w, "bottom": y + hh}
}

type menuFromTemplateState struct {
	ctxID  int64
	inline inlineMenu
}

// menuFromTemplate: template (MenuTemplateItem[]) plus the menu props a
// MenuTrigger passed to renderMenu. Inside a MenuTrigger it opens as a
// popups menu anchored to the trigger (keyboard navigation, submenus);
// rendered on its own it draws an inline menu card.
func menuFromTemplate(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[menuFromTemplateState](h)
	tpl := asList(h.Props["template"])
	if id := int64(h.Props.IntOr("clackMenuId", 0)); id != 0 {
		ctx := lookupMenu(id)
		if ctx == nil || ctx.closed {
			return layout.Dimensions{}
		}
		if !ctx.claimed {
			ctx.claimed = true
			st.ctxID = id
			props := h.Props
			if _, ok := funcProp(props, "onClose"); ok {
				ctx.onClose = append(ctx.onClose, func() { props.Invoke("onClose") })
			}
			items := menuItems(tpl, fireClick)
			ctx.menu = popups.NewMenuOpts(ph, items, popups.MenuOpts{
				Raw:        true, // already a plugin-rendered MenuFromTemplate
				Anchor:     ctx.anchor,
				Side:       ctx.side,
				AlignRight: ctx.align == 2,
				Width:      menuWidth(h.Props["width"]),
				OnClose:    ctx.close,
			})
			showOverlay(ctx.menu)
		}
		return layout.Dimensions{}
	}
	return st.inline.layout(gtx, h, menuItems(tpl, fireClick))
}

// inlineMenu draws menu rows in a card in the host flow (a
// MenuFromTemplate outside a MenuTrigger).
type inlineMenu struct {
	clicks []widget.Clickable
}

func (m *inlineMenu) layout(gtx layout.Context, h *slackc.Host, items []popups.MenuItem) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	if len(m.clicks) != len(items) {
		m.clicks = make([]widget.Clickable, len(items))
	}
	w := dp(gtx, 300)
	if w > gtx.Constraints.Max.X {
		w = gtx.Constraints.Max.X
	}
	if mw := menuWidth(h.Props["width"]); mw > 0 {
		w = dp(gtx, mw)
	}
	pad := dp(gtx, 12)
	type row struct {
		call op.CallOp
		h    int
	}
	rows := make([]row, len(items))
	total := 2 * pad
	for i := range items {
		it := items[i]
		click := &m.clicks[i]
		if click.Clicked(gtx) && !it.Disabled && it.DoCtx != nil {
			it.DoCtx(gtx)
		}
		call, d := record(loose(gtx, image.Pt(w, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return inlineRow(gtx, th, cl, &it, click, w)
		})
		rows[i] = row{call, d.Size.Y}
		total += d.Size.Y
	}
	r := image.Rect(0, 0, w, total)
	card(gtx, cl, r, dp(gtx, 6))
	y := pad
	for _, rw := range rows {
		callAt(gtx, rw.call, image.Pt(0, y))
		y += rw.h
	}
	return layout.Dimensions{Size: r.Max}
}

func inlineRow(gtx layout.Context, th *theme.Theme, cl *colors, it *popups.MenuItem, click *widget.Clickable, w int) layout.Dimensions {
	switch {
	case it.Separator:
		h := dp(gtx, 17)
		theme.Fill(gtx, image.Rect(0, h/2, w, h/2+1), cl.Divider)
		return layout.Dimensions{Size: image.Pt(w, h)}
	case it.Header != "":
		h := dp(gtx, 28)
		return at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx layout.Context) layout.Dimensions {
			vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return label(gtx, th, 13, theme.Bold, cl.Fg3, it.Header)
			})
			return layout.Dimensions{Size: image.Pt(w, h)}
		})
	}
	draw := func(gtx layout.Context) layout.Dimensions {
		hl := click.Hovered() && !it.Disabled
		if it.Widget != nil {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Constraints.Max.Y)}
			return it.Widget(gtx, hl)
		}
		h := dp(gtx, 28)
		sz := image.Pt(w, h)
		fg := cl.Fg
		if it.Danger {
			fg = cl.Danger
		}
		if it.Disabled {
			fg = cl.DisText
		}
		if hl {
			bg := cl.Highlight
			if it.Danger {
				bg = cl.DangerBg
			}
			theme.Fill(gtx, image.Rectangle{Max: sz}, bg)
			fg = cl.HighText
		}
		x := dp(gtx, 24)
		if it.Icon != "" {
			at(gtx, image.Pt(x, (h-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions {
				return drawIcon(gtx, it.Icon, 18, fg)
			})
			x += dp(gtx, 28)
		}
		right := w - dp(gtx, 24)
		if len(it.Sub) > 0 || it.Checked {
			name := "check"
			if len(it.Sub) > 0 {
				name = "chevron-right"
			}
			right -= dp(gtx, 16)
			at(gtx, image.Pt(right, (h-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return drawIcon(gtx, name, 16, fg)
			})
			right -= dp(gtx, 8)
		}
		lw := right - x
		if lw < 1 {
			lw = 1
		}
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(loose(gtx, image.Pt(lw, h)), h, func(gtx layout.Context) layout.Dimensions {
				return label(gtx, th, 15, theme.Regular, fg, it.Label)
			})
		})
		if !it.Disabled {
			cursor(gtx, sz, pointer.CursorPointer)
		}
		return layout.Dimensions{Size: sz}
	}
	if it.Disabled {
		return draw(gtx)
	}
	return click.Layout(gtx, draw)
}

func init() {
	add(contextMenuTrigger, "withKeyboardFocusContext(ContextMenuTrigger)", "ContextMenuTrigger")
	add(menuTrigger, "MenuTrigger")
	add(menuFromTemplate, "MenuFromTemplate")
}
