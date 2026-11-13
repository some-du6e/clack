package blockkit

import (
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Measured from Slack's Block Kit renderer (app.slack.com Block Kit
// Builder, which renders with the client's own components, 2026-11):
//
// Messages (small): buttons 28px, 13px/700, radius 4, padding 0 8,
// outline 1px rgba(94,93,96,.45); elements 8px apart, rows 8px apart;
// the first 5 elements of an actions block show, then "+ N more"
// (padding 0 12). static/users/conversations/channels selects 190×28,
// radius 8, 1px #7c7a7f, 13px text at 8px, 20px chevron 8px from the
// right; datepicker 220×28 with the calendar icon at 8 and text at 28;
// timepicker 190×28 with a clock icon; overflow 30×28. Checkboxes 14px
// (radius 4) and radios 13px at y+3, labels 15px/22 at x+22, options 8px
// apart, a description adds a 22px line. checkboxes, radio_buttons and
// multi-selects stack under a section's text instead of sitting at its
// right. datetimepicker is two 208×36 fields 8px apart with a 13px "Time
// zone: …" caption (rgb 69,68,71) under the time.
//
// Modals (medium): 520 wide, header 68 (padding 16 24; app icon 36,
// radius 4; title 22/900 at 52px from the icon's left), body padding 24,
// controls 464 wide; input blocks are a 30px label row (15/700, a muted
// 400 "(optional)"), the 36px control (textarea 104), and a 13px/18
// hint (rgb 94,93,96) 4px below; blocks are 8px apart. Section
// accessory buttons in modals are at least 120 wide.

func dp(gtx C, v float32) int { return gtx.Dp(unit.Dp(v)) }

func at(gtx C, p image.Point, w layout.Widget) D {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	return w(gtx)
}

func fixed(gtx C, sz image.Point) C {
	gtx.Constraints = layout.Exact(sz)
	return gtx
}

func rrect(gtx C, r image.Rectangle, radius int, c color.NRGBA) { theme.RRect(gtx, r, radius, c) }

// outline draws a 1px (w) border inside r with fill bg.
func outline(gtx C, r image.Rectangle, radius, w int, border, bg color.NRGBA) {
	rrect(gtx, r, radius, border)
	rrect(gtx, r.Inset(w), max(radius-w, 0), bg)
}

func label(gtx C, th *theme.Theme, size float32, w font.Weight, c color.NRGBA, s string) D {
	gtx.Constraints.Min = image.Point{}
	return th.Label(gtx, unit.Sp(size), w, c, s)
}

func para(gtx C, th *theme.Theme, size, lh float32, w font.Weight, c color.NRGBA, s string) D {
	gtx.Constraints.Min = image.Point{}
	return th.Layout(gtx, theme.TextStyle{Size: unit.Sp(size), Weight: w, Color: c, LineHeight: unit.Sp(lh)}, s)
}

func measure(gtx C, th *theme.Theme, size float32, w font.Weight, s string) int {
	return th.Measure(gtx, unit.Sp(size), w, s)
}

func icon(gtx C, name string, size float32, c color.NRGBA) D {
	return icons.Get(name)(gtx, unit.Dp(size), c)
}

// vcenter draws w vertically centred in a row of height h.
func vcenter(gtx C, h int, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	call := m.Stop()
	defer op.Offset(image.Pt(0, (h-d.Size.Y)/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(d.Size.X, h)}
}

// colors is the palette blockkit draws with: popups' Slack tokens with
// the Block Kit renderer's own values on top, measured in both modes
// (Block Kit Builder, light and dark, 2026-11):
//
//	                light                   dark
//	surface bg      #ffffff                 #1a1d21 (modal, inputs, buttons)
//	text            #1d1c1d                 #d1d2d3 (labels, body)
//	input text      #1d1c1d                 #f8f8f8 (fields, outline buttons)
//	input border    #7c7a7f                 #797c81
//	button border   rgba(94,93,96,.45)      rgba(121,124,129,.5)
//	primary         #007a5a                 #177e56
//	danger          #e01e5a                 #d3305d
//	checked box     #1264a3                 #0a77a7
//	check border    rgba(94,93,96,.45)      rgba(121,124,129,.5)
//	hint            #5e5d60                 #9a9b9e
//	picker text     #616061                 #ababad
//	modal ring      rgba(29,28,29,.13)      rgba(232,232,232,.13)
//	footer border   rgba(94,93,96,.13)      #35373b
type colors struct {
	*popups.Colors
	Text, InputText, Text2, Hint, Placeholder color.NRGBA
	SelectBorder, CheckBorder, Check          color.NRGBA
	ErrorText, Hover, FooterBorder            color.NRGBA
}

var (
	paletteMu sync.Mutex
	palettes  [2]*colors
)

func colorsOf(th *theme.Theme) colors {
	if th.Tokens != nil && th.Tokens.Overridden() {
		return themedColors(th)
	}
	return measuredColors(th)
}

// bkTokens is the Slack token behind blockkit's own fields (Block Kit's
// elements are Slack's c-input_text, c-select, c-input_checkbox…):
// popups.ApplyTokens covers the embedded popup palette.
var bkTokens = []struct {
	f   func(*colors) *color.NRGBA
	tok theme.Tok
}{
	{func(c *colors) *color.NRGBA { return &c.Text }, theme.Both("var(--dt_color-content-pry)")},
	{func(c *colors) *color.NRGBA { return &c.InputText }, theme.Both("var(--dt_color-content-pry)")},
	{func(c *colors) *color.NRGBA { return &c.Text2 }, theme.Both("var(--dt_color-content-sec)")},
	{func(c *colors) *color.NRGBA { return &c.Hint }, theme.Both("var(--dt_color-content-ter)")},
	{func(c *colors) *color.NRGBA { return &c.Placeholder }, theme.Both("var(--dt_color-content-ter)")},
	{func(c *colors) *color.NRGBA { return &c.SelectBorder }, theme.Both("var(--dt_color-otl-pry)")},
	{func(c *colors) *color.NRGBA { return &c.CheckBorder }, theme.Both("var(--dt_color-otl-sec)")},
	{func(c *colors) *color.NRGBA { return &c.Check }, theme.Both("var(--dt_color-base-inv-hgl-1)")},
	{func(c *colors) *color.NRGBA { return &c.ErrorText }, theme.Both("var(--dt_color-content-imp)")},
	{func(c *colors) *color.NRGBA { return &c.Hover }, theme.Both("var(--dt_color-base-pry-hover)")},
	{func(c *colors) *color.NRGBA { return &c.FooterBorder }, theme.Both("var(--dt_color-otl-ter)")},
}

var (
	tokMu  sync.Mutex
	tokTh  *theme.Theme
	tokGen uint64
	tokCol colors
)

// themedColors is the measured palette with the stylesheets' overridden
// tokens laid over it, cached per theme generation. The Block Kit modal is
// Slack's .c-sk-modal (and its inputs and buttons sit on it), so its
// surface is --dt_color-base-pry, not the menus' --dt_color-ctr-pry.
func themedColors(th *theme.Theme) colors {
	tokMu.Lock()
	defer tokMu.Unlock()
	if tokTh == th && tokGen == th.Gen && tokCol.Colors != nil {
		return tokCol
	}
	c := measuredColors(th)
	pc := *c.Colors
	c.Colors = &pc
	popups.ApplyTokens(&pc, th)
	t := th.Tokens
	if theme.SetColor(t, &pc.Bg, "var(--dt_color-base-pry)") && pc.Bg.A != 0xff {
		pc.Bg = theme.Over(pc.Bg, th.C.Bg)
	}
	for _, f := range bkTokens {
		if theme.SetTok(t, f.f(&c), f.tok, th.Dark) {
			switch p := f.f(&c); p {
			case &c.CheckBorder, &c.FooterBorder:
				*p = theme.LinearAlpha(*p, pc.Bg)
			default:
				if p.A != 0xff {
					*p = theme.Over(*p, pc.Bg)
				}
			}
		}
	}
	tokCol, tokTh, tokGen = c, th, th.Gen
	return c
}

func measuredColors(th *theme.Theme) colors {
	i := 0
	if th.Dark {
		i = 1
	}
	paletteMu.Lock()
	defer paletteMu.Unlock()
	if p := palettes[i]; p != nil {
		return *p
	}
	pc := *popups.ColorsOf(th) // a copy: popups' palette is shared
	c := &colors{Colors: &pc}
	if th.Dark {
		bg := rgb(0x1a1d21)
		pc.Bg, pc.BtnBg, pc.BtnHover = bg, bg, rgb(0x222529)
		pc.Fg = rgb(0xd1d2d3)
		pc.BtnBorder = theme.LinearAlpha(rgba(0x797c81, .5), bg)
		pc.Primary, pc.PrimaryText = rgb(0x177e56), rgb(0xffffff)
		pc.DangerBg = rgb(0xd3305d)
		pc.Ring = theme.LinearAlpha(rgba(0xe8e8e8, .13), bg)
		c.Text, c.InputText, c.Text2, c.Hint, c.Placeholder = rgb(0xd1d2d3), rgb(0xf8f8f8), rgb(0xababad), rgb(0x9a9b9e), rgb(0xababad)
		c.SelectBorder, c.CheckBorder, c.Check = rgb(0x797c81), theme.LinearAlpha(rgba(0x797c81, .5), bg), rgb(0x0a77a7)
		c.ErrorText, c.Hover, c.FooterBorder = rgb(0xe8577e), theme.LinearAlpha(rgba(0xffffff, .04), bg), rgb(0x35373b)
	} else {
		bg := rgb(0xffffff)
		pc.Primary, pc.PrimaryText, pc.DangerBg = rgb(0x007a5a), rgb(0xffffff), rgb(0xe01e5a)
		c.Text, c.InputText, c.Text2, c.Hint, c.Placeholder = rgb(0x1d1c1d), rgb(0x1d1c1d), rgb(0x616061), rgb(0x5e5d60), rgb(0x616061)
		c.SelectBorder, c.CheckBorder, c.Check = rgb(0x7c7a7f), theme.LinearAlpha(rgba(0x5e5d60, .45), bg), rgb(0x1264a3)
		c.ErrorText, c.Hover, c.FooterBorder = rgb(0xe01e5a), rgb(0xf8f8f8), theme.LinearAlpha(rgba(0x5e5d60, .13), bg)
	}
	palettes[i] = c
	return *c
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func rgba(v uint32, a float32) color.NRGBA {
	c := rgb(v)
	c.A = uint8(a*255 + .5)
	return c
}

// spinner draws Slack's loading ring (a 3/4 arc turning once a second)
// centred in a box of size d, and keeps frames coming.
func spinner(gtx C, d int, c color.NRGBA) {
	t := float64(gtx.Now.UnixMilli()%1000) / 1000
	r := float32(d)/2 - float32(dp(gtx, 1.5))
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	a0 := float32(t * 2 * math.Pi)
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
	p.ArcTo(ctr, ctr, 1.5*math.Pi)
	st := clip.Stroke{Path: p.End(), Width: float32(dp(gtx, 2))}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}

// handCursor sets the pointer cursor over r.
func handCursor(gtx C, r image.Rectangle) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	pointer.CursorPointer.Add(gtx.Ops)
}

func unitSp(v float32) unit.Sp { return unit.Sp(v) }

// buttonColors are Block Kit button colors per style.
func buttonColors(cl colors, style string, hovered bool) (bg, fg, border color.NRGBA) {
	switch style {
	case "primary":
		bg, fg = cl.Primary, cl.PrimaryText
		if hovered {
			bg = theme.Mix(bg, color.NRGBA{A: 255}, 0.12)
		}
		return bg, fg, color.NRGBA{}
	case "danger":
		bg, fg = cl.DangerBg, cl.HighText
		if hovered {
			bg = theme.Mix(bg, color.NRGBA{A: 255}, 0.12)
		}
		return bg, fg, color.NRGBA{}
	}
	bg = cl.BtnBg
	if hovered {
		bg = cl.BtnHover
	}
	return bg, cl.InputText, cl.BtnBorder
}

// drawButton paints a Block Kit button of size sz (label centred; busy
// shows the spinner instead, keeping the width).
func drawButton(gtx C, th *theme.Theme, sz image.Point, style, text string, radius, fs float32, hovered, busy, disabled bool) {
	cl := colorsOf(th)
	bg, fg, border := buttonColors(cl, style, hovered && !busy)
	if disabled {
		bg, fg, border = cl.Disabled, cl.DisText, color.NRGBA{}
	}
	r := image.Rectangle{Max: sz}
	rad := dp(gtx, radius)
	if border.A > 0 {
		outline(gtx, r, rad, max(1, dp(gtx, 1)), border, bg)
	} else {
		rrect(gtx, r, rad, bg)
	}
	if busy {
		s := dp(gtx, 16)
		at(gtx, image.Pt((sz.X-s)/2, (sz.Y-s)/2), func(gtx C) D { spinner(gtx, s, fg); return D{} })
		return
	}
	tw := measure(gtx, th, fs, theme.Bold, text)
	pad := dp(gtx, 8)
	maxW := sz.X - 2*pad
	x := (sz.X - min(tw, maxW)) / 2
	at(gtx, image.Pt(x, 0), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, sz.Y)}
		return vcenter(gtx, sz.Y, func(gtx C) D { return label(gtx, th, fs, theme.Bold, fg, text) })
	})
}

// drawField paints a select-like field: border (focus ring when active),
// optional leading icon, text (placeholder color when ph), chevron.
func drawField(gtx C, th *theme.Theme, sz image.Point, lead, text string, ph, active, chevron bool, fs float32, errored bool) {
	cl := colorsOf(th)
	r := image.Rectangle{Max: sz}
	rad := dp(gtx, 8)
	bw := max(1, dp(gtx, 1))
	switch {
	case active:
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		outline(gtx, r, rad, bw, cl.Focus, cl.Bg)
	case errored:
		outline(gtx, r, rad, bw, cl.ErrorText, cl.Bg)
	default:
		outline(gtx, r, rad, bw, cl.SelectBorder, cl.Bg)
	}
	small := sz.Y < dp(gtx, 32)
	x := dp(gtx, 12)
	if small {
		x = dp(gtx, 8)
	}
	if lead != "" {
		is := float32(16)
		if !small {
			is = 18
		}
		at(gtx, image.Pt(x, (sz.Y-dp(gtx, is))/2), func(gtx C) D { return icon(gtx, lead, is, cl.Text2) })
		x += dp(gtx, is) + dp(gtx, 4)
		if small {
			x = dp(gtx, 28)
		} else {
			x = dp(gtx, 32)
		}
	}
	right := sz.X - dp(gtx, 8)
	if chevron {
		cs := dp(gtx, 20)
		right = sz.X - dp(gtx, 8) - cs
		if !small {
			right = sz.X - dp(gtx, 12) - cs
		}
		at(gtx, image.Pt(right, (sz.Y-cs)/2), func(gtx C) D { return icon(gtx, "chevron-down", 20, cl.InputText) })
		right -= dp(gtx, 4)
	}
	col := cl.InputText
	if ph {
		col = cl.Placeholder
	}
	at(gtx, image.Pt(x, 0), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(max(right-x, 0), sz.Y)}
		return vcenter(gtx, sz.Y, func(gtx C) D { return label(gtx, th, fs, theme.Regular, col, text) })
	})
}

// drawCheckbox paints Slack's 14px checkbox.
func drawCheckbox(gtx C, th *theme.Theme, on, hovered bool) {
	cl := colorsOf(th)
	s := dp(gtx, 14)
	r := image.Rect(0, 0, s, s)
	rad := dp(gtx, 4)
	if on {
		rrect(gtx, r, rad, cl.Check)
		at(gtx, image.Pt(0, 0), func(gtx C) D { return icon(gtx, "check", 14, cl.HighText) })
		return
	}
	border := cl.CheckBorder
	if hovered {
		border = cl.Text2
	}
	outline(gtx, r, rad, max(1, dp(gtx, 1)), border, cl.Bg)
}

// drawRadio paints Slack's 13px radio button.
func drawRadio(gtx C, th *theme.Theme, on, hovered bool) {
	cl := colorsOf(th)
	s := dp(gtx, 13)
	ctr := image.Pt(s/2, s/2)
	if on {
		theme.Circle(gtx, ctr, s, cl.Check)
		theme.Circle(gtx, ctr, dp(gtx, 5), cl.HighText)
		return
	}
	border := cl.CheckBorder
	if hovered {
		border = cl.Text2
	}
	theme.Circle(gtx, ctr, s, border)
	theme.Circle(gtx, ctr, s-2*max(1, dp(gtx, 1)), cl.Bg)
}

// ---- button labels ----

// btnRun is one run of a button label: text, or a workspace emoji's image.
type btnRun struct{ text, url string }

// btnEmoji is the label's emoji size (emojiSize small: c-emoji__small, an
// inline 16px image centred on the line).
const btnEmoji = 16

// btnMaxChars is ButtonElement's maxCharacters.
const btnMaxChars = 40

// buttonRuns is ButtonElement's label as PlainText draws it (boot-shared
// iY3m through the formatter in NOMRKDWN mode, emoji on unless the text
// says emoji: false): runs of white space are one space (c-button's
// white-space: nowrap), a standard :shortcode: is its emoji, a workspace
// one its image, and the label stops at 40 characters with "…".
func buttonRuns(h Host, t *Text) []btnRun {
	if t == nil {
		return nil
	}
	s := strings.Join(strings.Fields(unescape(t.Text)), " ")
	if t.Type == "mrkdwn" {
		s = stripMrkdwn(s)
	}
	var app *core.App
	if h != nil {
		app = h.Core()
	}
	var out []btnRun
	n := 0 // characters so far; an emoji is one
	text := func(x string) bool {
		if x == "" {
			return true
		}
		rs := []rune(x)
		if n+len(rs) > btnMaxChars {
			x = string(rs[:max(btnMaxChars-n, 0)]) + "…"
			out = append(out, btnRun{text: x})
			return false
		}
		n += len(rs)
		if k := len(out); k > 0 && out[k-1].url == "" {
			out[k-1].text += x
		} else {
			out = append(out, btnRun{text: x})
		}
		return true
	}
	for s != "" {
		i := strings.IndexByte(s, ':')
		j := -1
		if i >= 0 {
			j = strings.IndexByte(s[i+1:], ':')
		}
		if i < 0 || j < 0 || !t.emojiOn() {
			text(s)
			break
		}
		name := s[i+1 : i+1+j]
		url, uni, ok := "", "", false
		if name != "" && !strings.ContainsAny(name, " \t") {
			if app != nil {
				url, uni, ok = app.S.Emoji(name)
			} else if _, u, std := emoji.Standard(name); std {
				uni, ok = u, true
			}
		}
		if !ok {
			if !text(s[:i+1]) {
				return out
			}
			s = s[i+1:]
			continue
		}
		if !text(s[:i]) {
			return out
		}
		switch {
		case uni != "": // a standard emoji: its character
			if !text(uni) {
				return out
			}
		case n+1 > btnMaxChars:
			text("…")
			return out
		default:
			n++
			out = append(out, btnRun{url: url})
		}
		s = s[i+j+2:]
	}
	return out
}

// runsWidth is the label's width at fs sp bold.
func runsWidth(gtx C, th *theme.Theme, fs float32, runs []btnRun) int {
	w := 0
	for _, r := range runs {
		if r.url != "" {
			w += dp(gtx, btnEmoji)
			continue
		}
		w += measure(gtx, th, fs, theme.Bold, r.text)
	}
	return w
}

// drawRuns draws the label in a line of height lh at x (text vertically
// centred, emoji images centred on the line).
func drawRuns(gtx C, th *theme.Theme, h Host, fs float32, col color.NRGBA, runs []btnRun, x, lh, maxW int) {
	for _, r := range runs {
		if x >= maxW {
			return
		}
		if r.url != "" {
			es := dp(gtx, btnEmoji)
			if h != nil && h.Images() != nil {
				if img, ok := h.Images().Get(r.url, es); ok {
					at(gtx, image.Pt(x, (lh-es)/2), func(gtx C) D { return theme.Image(gtx, img, image.Pt(es, es), 0) })
				}
			}
			x += es
			continue
		}
		t := r.text
		at(gtx, image.Pt(x, 0), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(maxW-x, 0), lh)}
			return vcenter(gtx, lh, func(gtx C) D { return label(gtx, th, fs, theme.Bold, col, t) })
		})
		x += measure(gtx, th, fs, theme.Bold, t)
	}
}

// drawButtonRuns is drawButton with a label of runs (Block Kit buttons).
func drawButtonRuns(gtx C, th *theme.Theme, h Host, sz image.Point, style string, runs []btnRun, radius, fs float32, hovered, busy, disabled bool) {
	drawButton(gtx, th, sz, style, "", radius, fs, hovered, busy, disabled)
	if busy {
		return
	}
	cl := colorsOf(th)
	_, fg, _ := buttonColors(cl, style, hovered)
	if disabled {
		fg = cl.DisText
	}
	pad := dp(gtx, 8)
	tw := runsWidth(gtx, th, fs, runs)
	x := (sz.X - min(tw, sz.X-2*pad)) / 2
	// .p-block_kit_button_element { padding: 0 8px 1px }: the line is 1px
	// shorter than the button
	drawRuns(gtx, th, h, fs, fg, runs, x, sz.Y-dp(gtx, 1), sz.X-pad)
}
