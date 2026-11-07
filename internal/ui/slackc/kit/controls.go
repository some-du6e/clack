package kit

// Form controls and buttons: Button, IconButtonBase, FilterPill, Tag,
// Checkbox, Label, Legend, Hint, FieldSet, InlineAlert, FormTextInput.
// Metrics follow Slack's 2026 SlackKit (c-button, c-input_text,
// c-input_checkbox, c-label, c-legend, c-inline_alert, c-tag).

import (
	"image"
	"image/color"
	"strconv"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

func init() {
	add(buttonComp, "Button")
	add(iconButtonBase, "IconButtonBase")
	add(filterPill, "FilterPill")
	add(tagComp, "Tag")
	add(checkboxComp, "Checkbox")
	add(labelComp, "Label")
	add(legendComp, "Legend")
	add(hintComp, "Hint")
	add(fieldSet, "FieldSet")
	add(inlineAlert, "InlineAlert")
	add(formTextInput, "FormTextInput")
}

// ---- shared bits ----

// ctlPress is a clickable with onMouseDown edge detection. It is
// keyboard-operable like Slack's buttons: Tab focuses it (Gio's focus
// order), Enter or Space activates it, and focus shows Slack's ring.
type ctlPress struct {
	click   widget.Clickable
	down    bool
	focused bool
}

// layout lays w out as the clickable and draws the focus ring
// (box-shadow 0 0 0 4px FocusRing, like Slack's c-button:focus-visible)
// around it while it has keyboard focus. onFocus/onBlur fire on changes.
func (s *ctlPress) layout(gtx layout.Context, h *slackc.Host, radius int, w layout.Widget) layout.Dimensions {
	d := s.click.Layout(gtx, w)
	f := gtx.Focused(&s.click)
	if f != s.focused {
		s.focused = f
		if f {
			h.Call("onFocus", focusEvent("focus", ""))
		} else {
			h.Call("onBlur", focusEvent("blur", ""))
		}
	}
	if f {
		g := dp(gtx, 2)
		theme.Border(gtx, image.Rectangle{Max: d.Size}.Inset(-g), radius+g, 2*g, colorsOf(thm()).FocusRing)
	}
	return d
}

// update drains the clickable: it calls onMouseDown when a press starts
// and reports a click (with its modifiers). Disabled controls drop input.
func (s *ctlPress) update(gtx layout.Context, h *slackc.Host, enabled bool) (bool, key.Modifiers) {
	clicked := false
	var mods key.Modifiers
	for {
		c, ok := s.click.Update(gtx)
		if !ok {
			break
		}
		if enabled {
			clicked, mods = true, c.Modifiers
		}
	}
	pressed := s.click.Pressed()
	if enabled && pressed && !s.down {
		p, _ := pointerWin()
		h.Call("onMouseDown", mouseEvent(gtx, "mousedown", 0, p, 0))
	}
	s.down = pressed
	return clicked, mods
}

// ctlFireClick calls onClick with a DOM-like click event.
func ctlFireClick(gtx layout.Context, h *slackc.Host, mods key.Modifiers) {
	p, _ := pointerWin()
	h.Call("onClick", mouseEvent(gtx, "click", 0, p, mods))
}

// ctlAria reads an aria-* boolean ("true" or true).
func ctlAria(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

func ctlDisabled(p slackc.Props) bool {
	return p.Bool("disabled") || p.Bool("isDisabled") || ctlAria(p["aria-disabled"])
}

// ariaLabel attaches the accessible name.
func ctlAriaLabel(gtx layout.Context, p slackc.Props) {
	if s := p.StrOr("aria-label", ""); s != "" {
		semantic.LabelOp(s).Add(gtx.Ops)
	}
}

// iconLabel lays out an optional icon then an optional label in a row,
// vertically centered, gap px apart.
func ctlIconLabel(gtx layout.Context, iconName string, isz float32, fg color.NRGBA, gap int, lbl layout.Widget) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	var ic, lc op.CallOp
	var id, ld layout.Dimensions
	if iconName != "" {
		ic, id = record(gtx, func(gtx layout.Context) layout.Dimensions { return drawIcon(gtx, iconName, isz, fg) })
	}
	hasL := false
	if lbl != nil {
		lc, ld = record(gtx, lbl)
		hasL = ld.Size.X > 0 || ld.Size.Y > 0
	}
	h := id.Size.Y
	if ld.Size.Y > h {
		h = ld.Size.Y
	}
	x := 0
	if iconName != "" {
		callAt(gtx, ic, image.Pt(0, (h-id.Size.Y)/2))
		x = id.Size.X
		if hasL {
			x += gap
		}
	}
	if hasL {
		callAt(gtx, lc, image.Pt(x, (h-ld.Size.Y)/2))
		x += ld.Size.X
	}
	return layout.Dimensions{Size: image.Pt(x, h), Baseline: 0}
}

// childLabel draws the host's children as a one-line label: plain text
// with the theme, anything else through the host tree in the same style.
func ctlChildLabel(h *slackc.Host, th *theme.Theme, size float32, bold bool, fg color.NRGBA) layout.Widget {
	if !h.HasChildren() {
		return nil
	}
	return func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		w, cw := theme.Regular, 400
		if bold {
			w, cw = theme.Bold, 700
		}
		if s, ok := plainChildren(h); ok {
			return label(gtx, th, size, w, fg, s)
		}
		return children(gtx, h, fg, size, cw)
	}
}

func ctlMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- Button ----

// btnMetrics are Slack's c-button sizes: height, font, radius, padding,
// icon (dp/px).
func btnMetrics(size int) (h, fs, rad, pad, isz float32) {
	switch size {
	case 0:
		return 28, 13, 4, 12, 16
	case 2:
		return 44, 15, 8, 20, 20
	}
	return 36, 15, 8, 16, 18
}

// btnColors returns background, text and border for a button kind.
func btnColors(cl *colors, kind string, disabled, hover bool) (bg, fg, border color.NRGBA) {
	if disabled {
		if kind == "ghost" {
			return color.NRGBA{}, cl.DisText, color.NRGBA{}
		}
		return cl.Disabled, cl.DisText, color.NRGBA{}
	}
	switch kind {
	case "primary":
		bg, fg = cl.Primary, cl.PrimaryText
		if hover {
			bg = theme.Mix(bg, rgb(0x000000), .1)
		}
	case "danger":
		bg, fg = cl.DangerBg, cl.HighText
		if hover {
			bg = theme.Mix(bg, rgb(0x000000), .1)
		}
	case "ghost":
		fg = cl.Fg
		if hover {
			bg = cl.GhostHover
		}
	default: // outline
		bg, fg, border = cl.BtnBg, cl.Fg, cl.BtnBorder
		if hover {
			bg = cl.BtnHover
		}
	}
	return bg, fg, border
}

func buttonComp(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	p := h.Props
	st := stateOf[ctlPress](h)
	disabled := ctlDisabled(p)
	kind := p.StrOr("type", "outline")
	hh, fs, rad, pad, isz := btnMetrics(sizeOf(p, 1))

	if ok, mods := st.update(gtx, h, !disabled); ok {
		ctlFireClick(gtx, h, mods)
		if href := p.StrOr("href", ""); href != "" {
			openURL(href)
		}
	}
	hover := (st.click.Hovered() || st.click.Pressed()) && !disabled
	bg, fg, border := btnColors(cl, kind, disabled, hover)

	icon := p.StrOr("icon", "")
	lbl := ctlChildLabel(h, th, fs, true, fg)
	cg := gtx
	cg.Constraints.Max.X -= 2 * dp(gtx, pad)
	if cg.Constraints.Max.X < 0 {
		cg.Constraints.Max.X = 0
	}
	call, cd := record(cg, func(gtx layout.Context) layout.Dimensions {
		return ctlIconLabel(gtx, icon, isz, fg, dp(gtx, 4), lbl)
	})
	H := dp(gtx, hh)
	w := cd.Size.X + 2*dp(gtx, pad)
	if lbl == nil {
		w = ctlMax(H, cd.Size.X+dp(gtx, 8)) // icon-only: square
	}
	if gtx.Constraints.Min.X > w {
		w = gtx.Constraints.Min.X
	}
	if cd.Size.Y+dp(gtx, 4) > H { // wrapped element children grow the button
		H = cd.Size.Y + dp(gtx, 4)
	}
	size := image.Pt(w, H)
	draw := func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: size}
		radius := dp(gtx, rad)
		switch {
		case border.A > 0:
			ring(gtx, r, radius, 1, border, bg)
		case bg.A > 0:
			theme.RRect(gtx, r, radius, bg)
		}
		callAt(gtx, call, image.Pt((w-cd.Size.X)/2, (H-cd.Size.Y)/2))
		if !disabled {
			cursor(gtx, size, pointer.CursorPointer)
		}
		ctlAriaLabel(gtx, p)
		return layout.Dimensions{Size: size}
	}
	if disabled {
		return draw(gtx)
	}
	return st.layout(gtx, h, dp(gtx, rad), draw)
}

// ---- IconButtonBase ----

func iconButtonBase(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	p := h.Props
	st := stateOf[ctlPress](h)
	disabled := ctlDisabled(p)
	s := float32(32)
	switch p.StrOr("size", "") {
	case "small", "x-small", "tiny":
		s = 28
	case "large":
		s = 36
	}
	if ok, mods := st.update(gtx, h, !disabled); ok {
		ctlFireClick(gtx, h, mods)
	}
	pressed := ctlAria(p["aria-pressed"]) || ctlAria(p["aria-expanded"])
	hover := (st.click.Hovered() || st.click.Pressed()) && !disabled
	fg := cl.Fg2
	if hover || pressed {
		fg = cl.Fg
	}
	if disabled {
		fg = cl.DisText
	}
	var call op.CallOp
	var cd layout.Dimensions
	if h.HasChildren() {
		call, cd = record(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			if txt, ok := plainChildren(h); ok {
				return label(gtx, th, 13, theme.Bold, fg, txt)
			}
			return children(gtx, h, fg, 0, 0)
		})
	} else if ic := p.StrOr("icon", ""); ic != "" {
		call, cd = record(gtx, func(gtx layout.Context) layout.Dimensions { return drawIcon(gtx, ic, 18, fg) })
	}
	side := dp(gtx, s)
	size := image.Pt(ctlMax(side, cd.Size.X+dp(gtx, 8)), ctlMax(side, cd.Size.Y))
	draw := func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: size}
		switch {
		case pressed && hover:
			theme.RRect(gtx, r, dp(gtx, 6), theme.Mix(cl.GhostHover, cl.Fg, .08))
		case pressed || hover:
			theme.RRect(gtx, r, dp(gtx, 6), cl.GhostHover)
		}
		callAt(gtx, call, image.Pt((size.X-cd.Size.X)/2, (size.Y-cd.Size.Y)/2))
		if !disabled {
			cursor(gtx, size, pointer.CursorPointer)
		}
		ctlAriaLabel(gtx, p)
		return layout.Dimensions{Size: size}
	}
	if disabled {
		return draw(gtx)
	}
	return st.layout(gtx, h, dp(gtx, 6), draw)
}

// ---- FilterPill ----

func filterPill(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	p := h.Props
	st := stateOf[ctlPress](h)
	disabled := ctlDisabled(p)
	active := p.Bool("isActive") || ctlAria(p["aria-pressed"])
	if ok, mods := st.update(gtx, h, !disabled); ok {
		ctlFireClick(gtx, h, mods)
	}
	hover := st.click.Hovered() && !disabled
	bg, fg, border := cl.Bg, cl.Fg, cl.BtnBorder
	switch {
	case active:
		bg, fg, border = cl.PillActiveBg, cl.PillActiveFg, cl.Focus
		if hover {
			bg = theme.Mix(bg, cl.Focus, .08)
		}
	case hover:
		bg = cl.BtnHover
	}
	if disabled {
		fg, border = cl.DisText, cl.Divider
	}
	lbl := ctlChildLabel(h, th, 13, true, fg)
	call, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		return ctlIconLabel(gtx, "", 0, fg, 0, lbl)
	})
	H := ctlMax(dp(gtx, 28), cd.Size.Y+dp(gtx, 4))
	size := image.Pt(cd.Size.X+2*dp(gtx, 12), H)
	draw := func(gtx layout.Context) layout.Dimensions {
		ring(gtx, image.Rectangle{Max: size}, H/2, 1, border, bg)
		callAt(gtx, call, image.Pt(dp(gtx, 12), (H-cd.Size.Y)/2))
		if !disabled {
			cursor(gtx, size, pointer.CursorPointer)
		}
		ctlAriaLabel(gtx, p)
		return layout.Dimensions{Size: size}
	}
	if disabled {
		return draw(gtx)
	}
	return st.layout(gtx, h, H/2, draw)
}

// ---- Tag ----

// tagColors maps Tag's style prop to background and text.
func tagColors(cl *colors, style string) (bg, fg color.NRGBA) {
	tint := func(c color.NRGBA) (color.NRGBA, color.NRGBA) {
		return theme.Over(theme.WithAlpha(c, 0x1f), cl.Bg), c
	}
	switch style {
	case "informative", "info", "highlight", "new":
		return cl.InfoBg, cl.InfoFg
	case "warning", "attention":
		return tint(cl.Warning)
	case "danger", "error", "critical", "important":
		return tint(cl.Error)
	case "success", "positive":
		return tint(cl.Primary)
	}
	return cl.TagBg, cl.TagFg
}

func tagComp(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	p := h.Props
	bg, fg := tagColors(cl, p.StrOr("style", ""))
	hh, fs, pad := float32(20), float32(12), float32(6)
	if p.Bool("isMicro") {
		hh, fs, pad = 16, 11, 4
	}
	lbl := ctlChildLabel(h, th, fs, true, fg)
	if lbl == nil {
		return layout.Dimensions{}
	}
	call, cd := record(gtx, lbl)
	H := ctlMax(dp(gtx, hh), cd.Size.Y)
	size := image.Pt(cd.Size.X+2*dp(gtx, pad), H)
	theme.RRect(gtx, image.Rectangle{Max: size}, dp(gtx, 4), bg)
	callAt(gtx, call, image.Pt(dp(gtx, pad), (H-cd.Size.Y)/2))
	return layout.Dimensions{Size: size}
}

// ---- Checkbox ----

// ctlCheckMark strokes a check in an s×s box.
func ctlCheckMark(gtx layout.Context, s int, c color.NRGBA) {
	f := float32(s) / 16
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(3.8*f, 8.3*f))
	path.LineTo(f32.Pt(6.7*f, 11.1*f))
	path.LineTo(f32.Pt(12.2*f, 5*f))
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: path.End(), Width: 2 * f}.Op())
}

// toggleEvent is the change event a click on a checkbox produces.
func toggleEvent(p slackc.Props) slackc.Props {
	return changeEvent(p.StrOr("value", "on"), !p.Bool("checked"), p.StrOr("id", ""))
}

func checkboxComp(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	p := h.Props
	st := stateOf[ctlPress](h)
	disabled := ctlDisabled(p)
	checked, ind := p.Bool("checked"), p.Bool("indeterminate")
	if ok, _ := st.update(gtx, h, !disabled); ok {
		h.Call("onChange", toggleEvent(p))
		invalidate()
	}
	hover := st.click.Hovered() && !disabled
	s := dp(gtx, 16)
	size := image.Pt(s, s)
	draw := func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: size}
		rad := dp(gtx, 4)
		on := checked || ind
		switch {
		case on && disabled:
			theme.RRect(gtx, r, rad, theme.Mix(cl.Check, cl.Bg, .55))
		case on:
			fill := cl.Check
			if hover {
				fill = theme.Mix(fill, rgb(0x000000), .1)
			}
			theme.RRect(gtx, r, rad, fill)
		default:
			b := cl.CheckBorder
			if hover {
				b = cl.Fg2
			}
			bg := cl.Bg
			if disabled {
				b, bg = cl.Divider, cl.BgAlt
			}
			ring(gtx, r, rad, ctlMax(1, dp(gtx, 1)), b, bg)
		}
		switch {
		case ind:
			bar := image.Rect(dp(gtx, 4), s/2-dp(gtx, 1), s-dp(gtx, 4), s/2+dp(gtx, 1))
			theme.RRect(gtx, bar, dp(gtx, 1), cl.CheckText)
		case checked:
			ctlCheckMark(gtx, s, cl.CheckText)
		}
		if !disabled {
			cursor(gtx, size, pointer.CursorPointer)
		}
		ctlAriaLabel(gtx, p)
		return layout.Dimensions{Size: size}
	}
	if disabled {
		return draw(gtx)
	}
	return st.layout(gtx, h, dp(gtx, 4), draw)
}

// ---- Label ----

// toggleChild clicks the first Checkbox inside n (an inline Label's text
// toggles the control it wraps, like a <label>).
func toggleChild(n *slackc.Node) bool {
	if n == nil {
		return false
	}
	for _, c := range n.C {
		if c.IsComponent() && c.N == "Checkbox" {
			if !ctlDisabled(c.P) {
				c.P.Invoke("onChange", toggleEvent(c.P))
				invalidate()
			}
			return true
		}
		if toggleChild(c) {
			return true
		}
	}
	return false
}

type labelState struct{ click widget.Clickable }

func labelComp(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	p := h.Props
	inline := p.StrOr("type", "block") == "inline"
	disabled := p.Bool("isDisabled")
	fg, sub := cl.Fg, cl.Fg3
	if disabled {
		fg, sub = cl.DisText, cl.DisText
	}
	w := theme.Bold
	if inline {
		w = theme.Regular
	}
	textBlock := func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		y, maxW := 0, 0
		if h.HasElement("text") {
			g := gtx
			ow := 0
			const opt = " (optional)"
			if p.Bool("optional") {
				ow = textW(gtx, th, 15, theme.Regular, opt)
				g.Constraints.Max.X = ctlMax(0, g.Constraints.Max.X-ow)
			}
			d := textOrElement(g, h, "text", th, 15, 22, w, fg, 0)
			if ow > 0 {
				x := d.Size.X
				if x+ow > gtx.Constraints.Max.X {
					x = ctlMax(0, gtx.Constraints.Max.X-ow)
				}
				at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
					return para(gtx, th, 15, 22, theme.Regular, sub, opt, 1)
				})
				d.Size.X = x + ow
			}
			y, maxW = d.Size.Y, d.Size.X
		}
		if h.HasElement("subtext") {
			if y > 0 {
				y += dp(gtx, 2)
			}
			d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				return textOrElement(gtx, h, "subtext", th, 13, 18, theme.Regular, sub, 0)
			})
			y += d.Size.Y
			maxW = ctlMax(maxW, d.Size.X)
		}
		return layout.Dimensions{Size: image.Pt(maxW, y)}
	}

	if !inline {
		td := textBlock(gtx)
		y := td.Size.Y
		wd := td.Size.X
		if h.HasChildren() {
			if y > 0 {
				y += dp(gtx, 8)
			}
			cd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = 0
				return h.Children(gtx)
			})
			y += cd.Size.Y
			wd = ctlMax(wd, cd.Size.X)
		}
		return layout.Dimensions{Size: image.Pt(wd, y)}
	}

	// inline: the control, then its text beside it (c-label--inline).
	st := stateOf[labelState](h)
	if st.click.Clicked(gtx) && !disabled {
		toggleChild(h.Node)
	}
	lh := dp(gtx, 22)
	var cc op.CallOp
	var cd layout.Dimensions
	if h.HasChildren() {
		cc, cd = record(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			return h.Children(gtx)
		})
	}
	cy := 0
	if cd.Size.Y < lh {
		cy = (lh - cd.Size.Y) / 2
	}
	callAt(gtx, cc, image.Pt(0, cy))
	x := 0
	if cd.Size.X > 0 {
		x = cd.Size.X + dp(gtx, 8)
	}
	tg := gtx
	tg.Constraints.Min = image.Point{}
	tg.Constraints.Max.X = ctlMax(0, gtx.Constraints.Max.X-x)
	td := at(tg, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		return st.click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			d := textBlock(gtx)
			if !disabled {
				cursor(gtx, d.Size, pointer.CursorPointer)
			}
			return d
		})
	})
	return layout.Dimensions{Size: image.Pt(x+td.Size.X, ctlMax(cy+cd.Size.Y, td.Size.Y))}
}

// ---- Legend, Hint, FieldSet ----

func legendComp(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return childrenOrText(gtx, h, th, 15, 22, theme.Bold, cl.Fg, 0)
	})
}

func hintComp(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	return layout.Inset{Top: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return childrenOrText(gtx, h, th, 13, 18, theme.Regular, cl.Fg3, 0)
	})
}

func fieldSet(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	return layout.Inset{Bottom: 24}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.Y = 0
		return h.Children(gtx)
	})
}

// ---- InlineAlert ----

// ctlAlertRow draws Slack's form error line: a 16px warning icon and wrapping
// 13px text in the error color.
func ctlAlertRow(gtx layout.Context, th *theme.Theme, fg color.NRGBA, body layout.Widget) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	is, gap := dp(gtx, 16), dp(gtx, 6)
	at(gtx, image.Pt(0, dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
		return drawIcon(gtx, "warning", 16, fg)
	})
	g := gtx
	g.Constraints.Max.X = ctlMax(0, gtx.Constraints.Max.X-is-gap)
	d := at(g, image.Pt(is+gap, 0), body)
	return layout.Dimensions{Size: image.Pt(is+gap+d.Size.X, ctlMax(dp(gtx, 18), d.Size.Y))}
}

func inlineAlert(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	if !h.HasChildren() {
		return layout.Dimensions{}
	}
	return ctlAlertRow(gtx, th, cl.Error, func(gtx layout.Context) layout.Dimensions {
		return childrenOrText(gtx, h, th, 13, 18, theme.Regular, cl.Error, 0)
	})
}

// ---- FormTextInput ----

type ftiState struct {
	ed       widget.Editor
	init     bool
	last     string // the value prop last applied
	focused  bool
	autoDone bool
	area     int // pointer tag over the field's padding
}

// ftiMetrics are c-input_text heights and font sizes.
func ftiMetrics(size int) (h, fs float32) {
	switch size {
	case 0:
		return 28, 13
	case 2:
		return 44, 15
	}
	return 36, 15
}

// counterText is the character counter Slack shows with a limit.
func counterText(n, limit int) string {
	return strconv.Itoa(limit - n)
}

func formTextInput(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	p := h.Props
	st := stateOf[ftiState](h)
	ed := &st.ed
	ed.SingleLine = true
	ed.Submit = true
	if p.StrOr("type", "text") == "password" {
		ed.Mask = '•'
	} else {
		ed.Mask = 0
	}
	disabled := ctlDisabled(p)
	ed.ReadOnly = disabled

	// controlled value: apply prop changes, keep user edits in flight.
	value := valueText(p["value"])
	if !st.init || value != st.last {
		st.init, st.last = true, value
		if ed.Text() != value {
			ed.SetText(value)
		}
	}
	if p.Bool("autoFocus") && !st.autoDone && !disabled {
		gtx.Execute(key.FocusCmd{Tag: ed})
		st.autoDone = true
	}

	// clicks on the padding focus the editor.
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &st.area, Kinds: pointer.Press})
		if !ok {
			break
		}
		if _, ok := e.(pointer.Event); ok && !disabled {
			gtx.Execute(key.FocusCmd{Tag: ed})
		}
	}

	// Enter and the arrows reach the plugin before the editor, as in a
	// browser input: the onKeyDown prop, then a DOM keydown/keyup at the
	// component's node that bubbles through the plugin's elements around
	// it (api.modal's wrapper, a form's onKeyDown). Enter without an
	// onKeyDown of its own also submits an enclosing <form> (implicit
	// submission).
	if gtx.Focused(ed) {
		mods := key.ModShift | key.ModCtrl | key.ModAlt | key.ModSuper
		own := hasFunc(p, "onKeyDown")
		for {
			e, ok := gtx.Event(
				key.Filter{Focus: ed, Name: key.NameReturn, Optional: mods},
				key.Filter{Focus: ed, Name: key.NameEnter, Optional: mods},
				key.Filter{Focus: ed, Name: key.NameUpArrow, Optional: mods},
				key.Filter{Focus: ed, Name: key.NameDownArrow, Optional: mods},
			)
			if !ok {
				break
			}
			ke, ok := e.(key.Event)
			if !ok {
				continue
			}
			if ke.State == key.Press && own {
				h.Call("onKeyDown", keyEvent("keydown", string(ke.Name), ke.Modifiers, ed.Text()))
			}
			h.DispatchKey(ke)
			if ke.State == key.Press && !own && (ke.Name == key.NameReturn || ke.Name == key.NameEnter) && ke.Modifiers == 0 {
				h.SubmitForm()
			}
		}
	}
	for {
		e, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.ChangeEvent); ok {
			h.Call("onChange", ed.Text())
		}
	}
	if f := gtx.Focused(ed); f != st.focused {
		st.focused = f
		if f {
			h.Call("onFocus", focusEvent("focus", ed.Text()))
		} else {
			h.Call("onBlur", focusEvent("blur", ed.Text()))
		}
	}

	hh, fs := ftiMetrics(sizeOf(p, 1))
	errText := p.StrOr("errorText", "")
	invalid := p.Bool("isInvalid") || errText != ""
	limit := p.IntOr("maxCharacterLimit", 0)
	over := limit > 0 && ed.Len() > limit
	if over {
		invalid = true
	}

	w := gtx.Constraints.Max.X
	if w <= 0 || w >= 1<<20 {
		w = ctlMax(gtx.Constraints.Min.X, dp(gtx, 320))
	}
	H := dp(gtx, hh)
	r := image.Rect(0, 0, w, H)
	rad := dp(gtx, 8)
	focused := gtx.Focused(ed)
	bg := cl.Bg
	if disabled {
		bg = cl.BgAlt
	}
	switch {
	case invalid:
		if focused {
			ringW := dp(gtx, 4)
			theme.RRect(gtx, r.Inset(-ringW), rad+ringW, theme.LinearAlpha(theme.WithAlpha(cl.Error, 0x4d), cl.Bg))
		}
		ring(gtx, r, rad, 1, cl.Error, bg)
	case focused:
		ringW := dp(gtx, 4)
		theme.RRect(gtx, r.Inset(-ringW), rad+ringW, cl.FocusRing)
		ring(gtx, r, rad, 1, cl.Focus, bg)
	default:
		ring(gtx, r, rad, 1, cl.InputBorder, bg)
	}
	func() {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &st.area)
		pointer.CursorText.Add(gtx.Ops)
	}()

	padX := dp(gtx, 12)
	tw := ctlMax(1, w-2*padX)
	fg := cl.Fg
	if disabled {
		fg = cl.DisText
	}
	at(gtx, image.Pt(padX, 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, H, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, H)}
			if ed.Len() == 0 {
				if hold := p.StrOr("placeholder", ""); hold != "" {
					call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
						return label(gtx, th, fs, theme.Regular, cl.Placeholder, hold)
					})
					call.Add(gtx.Ops)
				}
			}
			return th.Editor(gtx, ed, unit.Sp(fs), fg, theme.WithAlpha(cl.Focus, 0x55))
		})
	})
	ctlAriaLabel(gtx, p)

	y := H
	under := func(wd layout.Widget) {
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return wd(gtx)
		})
		y += d.Size.Y
	}
	if errText != "" {
		under(func(gtx layout.Context) layout.Dimensions {
			return ctlAlertRow(gtx, th, cl.Error, func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 13, 18, theme.Regular, cl.Error, errText, 0)
			})
		})
	}
	if ht := p.StrOr("hintText", ""); ht != "" {
		under(func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg3, ht, 0)
		})
	}
	if limit > 0 && ed.Len() > limit-20 {
		under(func(gtx layout.Context) layout.Dimensions {
			c := cl.Fg3
			if over {
				c = cl.Error
			}
			s := counterText(ed.Len(), limit)
			tw := textW(gtx, th, 13, theme.Regular, s)
			return at(gtx, image.Pt(ctlMax(0, w-tw), 0), func(gtx layout.Context) layout.Dimensions {
				d := label(gtx, th, 13, theme.Regular, c, s)
				d.Size.X = w
				return d
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(w, y), Baseline: 0}
}

// ---- ComposerButton ----

// composerButton draws one composer.addButton entry (taut-api renders
// each as "@ComposerButton" with {id, icon, label, pressed, onClick,
// tooltip}) as Slack's composer icon button.
func composerButton(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	p := h.Props
	q := make(slackc.Props, len(p)+3)
	for k, v := range p {
		q[k] = v
	}
	q["size"] = "small"
	if p.Bool("pressed") {
		q["aria-pressed"] = "true"
	}
	if l, ok := p.Str("label"); ok && !p.Has("aria-label") {
		q["aria-label"] = l
	}
	h.Props = q
	defer func() { h.Props = p }()
	return iconButtonBase(gtx, h)
}

func init() { add(composerButton, "ComposerButton") }
