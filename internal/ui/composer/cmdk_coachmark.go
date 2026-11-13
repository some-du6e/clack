package composer

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// cmdKCoachmark is MessageInputCmdKCoachmark (gantry-v2-async-client-
// boot-shared, module rs9k, build 2cd9270e786c): Mod+K with text selected
// in the message input (onCmdKWhenSelected, SlackKeyboard's binding
// {key:"K", shortKey:true}) sets cmdKCoachmarkVisible, and
// renderCmdKCoachmark draws
//
//	DeprecatedCoachmarkAnchor (slxn) orientation "top", no children, with
//	DeprecatedBaseCoachmark (A2rj) isOpen, offsetY 20, arrowPosition 10,
//	style {width: 360px, transform: translate(0, -104%)}, overlayStyle
//	{backgroundColor: rgba(0,0,0,.3)}, hideCloseButton, and the text
//	msg_input rt("Trying to add a link? Use {commandKey} + {vKey} instead")
//	with KeyboardKey (HgMH zG) slim chips "CmdOrCtrl" and "V".
//
// The binding returns true, so the key goes on and the jumper opens too
// (Clack's composer opens it itself; see Composer.cmdKSelected). The card
// is a ReactModal: Esc (shouldCloseOnEsc) and a click on the overlay
// (shouldCloseOnOverlayClick) close it (closeCmdKCoachmark); it goes away
// with its MessageInput, so switching conversations closes it as well. The
// 2 s screen reader announcement (useEphemeralState) has no Clack
// equivalent.
//
// Geometry (client-boot-styles): .c-coachmark is sk_primary_background,
// radius 8, padding 20px 24px 24px, box-shadow 0 0 0 1px
// rgba(sk_foreground_low,.13), 0 18px 48px rgba(0,0,0,.08); "top" puts it
// at the anchor's left and anchor.top − anchor.height + offsetY, and the
// inline transform lifts it by 104% of its height. Its :after arrow is a
// 16px border triangle at left 10% − 12px (the later margin-left rule)
// under the card in --arrow-color (sk_inverted_foreground, the card's own
// background), its :before outline 17px in rgba(sk_foreground_low,.13).
type cmdKCoachmark struct {
	c     *Composer
	done  bool
	scrim int // the overlay's press tag
	card  int // the card's (presses on it don't close)
}

func (m *cmdKCoachmark) Dismissed() bool { return m.done }

// Dismiss is closeCmdKCoachmark.
func (m *cmdKCoachmark) Dismiss() {
	if m.done {
		return
	}
	m.done = true
	if m.c != nil && m.c.cmdK == m {
		m.c.cmdK = nil
	}
}

// cmdKSelected is MessageInput.onCmdKWhenSelected: show the coachmark
// unless it's showing, then let the key go on to the jumper.
func (c *Composer) cmdKSelected() {
	if c.cmdK == nil || c.cmdK.done {
		c.cmdK = &cmdKCoachmark{c: c}
		c.h.ShowOverlay(c.cmdK)
	}
	if s, ok := c.h.(interface{ OpenQuickSwitcher() }); ok {
		s.OpenQuickSwitcher()
	}
	c.h.Invalidate()
}

// cmdKCopy is the coachmark's text with the two KeyboardKey chips as
// parts whose Arg is "commandKey" or "vKey".
func cmdKCopy() []i18n.Part {
	return msgInputNS.RT("Trying to add a link? Use {commandKey} + {vKey} instead", "commandKey", "\x00", "vKey", "\x00")
}

// cmdKKeyLabel is KeyboardKey's label for "CmdOrCtrl" (the command icon on
// macOS, "Ctrl" elsewhere) and "V".
func cmdKKeyLabel(arg string) string {
	if arg == "commandKey" {
		if shortcut.Mac() {
			return ""
		}
		return "Ctrl"
	}
	return "V"
}

// cmdK geometry, in dp.
const (
	cmdKW       = 360
	cmdKPadT    = 20
	cmdKPadX    = 24
	cmdKPadB    = 24
	cmdKRadius  = 8
	cmdKOffsetY = 20
	cmdKArrow   = 16 // :after border width
	cmdKArrowX  = 12 // margin-left −12px from left: 10%
	cmdKLineH   = 22 // 15px Lato at line-height 1.46668
	cmdKChipH   = 20 // KeyboardKey slim: height 20, min-width 20, 4px inside
	cmdKChipM   = 4  // its margin-inline and margin-block-end
)

type cmdKPal struct {
	bg, fg, ring, overlay, chip, chipLine, chipText color.NRGBA
	shadow                                          color.NRGBA
}

func cmdKColors(th *theme.Theme) cmdKPal {
	v := css.SlackVars(th.Dark)
	sk := func(name string, a float64) color.NRGBA {
		c, ok := css.ParseColor("rgb(" + v["--"+name] + ")")
		if !ok {
			c = th.C.Fg
		}
		c.A = uint8(a*255 + .5)
		return c
	}
	dt := func(name string) color.NRGBA {
		c, _ := css.ParseColor(v["--"+name])
		theme.SetColor(th.Tokens, &c, "var(--"+name+")")
		return c
	}
	p := cmdKPal{
		bg:       sk("sk_primary_background", 1),
		fg:       sk("sk_primary_foreground", 1),
		ring:     sk("sk_foreground_low", .13),
		overlay:  color.NRGBA{A: 77}, // rgba(0, 0, 0, 0.3)
		chip:     dt("dt_color-base-ter"),
		chipLine: dt("dt_color-otl-ter"),
		chipText: dt("dt_color-content-pry"),
		shadow:   color.NRGBA{A: 20}, // rgba(0, 0, 0, .08)
	}
	theme.SetColor(th.Tokens, &p.bg, "rgb(var(--sk_primary_background))")
	theme.SetColor(th.Tokens, &p.fg, "rgb(var(--sk_primary_foreground))")
	return p
}

// cmdKRun is one piece of the card's inline flow: a word (with its
// trailing space) or a chip.
type cmdKRun struct {
	text string
	chip bool
	w    int
}

// cmdKRuns cuts the copy into words and chips, measured.
func cmdKRuns(gtx layout.Context, th *theme.Theme) []cmdKRun {
	var runs []cmdKRun
	for _, p := range cmdKCopy() {
		if p.Arg == "commandKey" || p.Arg == "vKey" {
			lbl := cmdKKeyLabel(p.Arg)
			w := gtx.Dp(cmdKChipH)
			if lbl != "" {
				w = max(w, th.Measure(gtx, 13, theme.Regular, lbl)+2*gtx.Dp(4))
			}
			runs = append(runs, cmdKRun{text: lbl, chip: true, w: w + 2*gtx.Dp(cmdKChipM)})
			continue
		}
		s := p.Text
		for s != "" {
			i := strings.IndexByte(s, ' ')
			word := s
			if i >= 0 {
				word, s = s[:i+1], s[i+1:]
			} else {
				s = ""
			}
			runs = append(runs, cmdKRun{text: word, w: th.Measure(gtx, 15, theme.Regular, word)})
		}
	}
	return runs
}

// cmdKLines wraps runs into lines at most w wide (CSS normal wrapping: a
// trailing space may hang).
func cmdKLines(gtx layout.Context, th *theme.Theme, runs []cmdKRun, w int) [][]cmdKRun {
	var lines [][]cmdKRun
	var cur []cmdKRun
	x := 0
	for _, r := range runs {
		rw := r.w
		if !r.chip && strings.HasSuffix(r.text, " ") {
			rw = th.Measure(gtx, 15, theme.Regular, strings.TrimRight(r.text, " "))
		}
		if len(cur) > 0 && x+rw > w {
			lines = append(lines, cur)
			cur, x = nil, 0
		}
		cur = append(cur, r)
		x += r.w
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// lineH is a line's height: a chip's margin box (20 + 4 below, centred on
// the text) stands 1px out of the 22px line at each end.
func cmdKLineHeight(gtx layout.Context, ln []cmdKRun) int {
	h := gtx.Dp(cmdKLineH)
	for _, r := range ln {
		if r.chip {
			return max(h, gtx.Dp(cmdKChipH+cmdKChipM))
		}
	}
	return h
}

// cardSize is the card's size for runs.
func cmdKCardSize(gtx layout.Context, lines [][]cmdKRun) image.Point {
	h := gtx.Dp(cmdKPadT) + gtx.Dp(cmdKPadB)
	for _, ln := range lines {
		h += cmdKLineHeight(gtx, ln)
	}
	return image.Pt(gtx.Dp(cmdKW), h)
}

// anchor is the coachmark anchor's window position: the top-left of the
// composer box, where MessageInput renders the anchor div among the
// container's children (above the input).
func (m *cmdKCoachmark) anchor() (image.Point, bool) {
	c := m.c
	if c == nil || !c.ui.f.boxOK {
		return image.Point{}, false
	}
	return c.Origin.Add(image.Pt(0, c.top)).Add(c.ui.f.box.Min), true
}

// cardRect is the card's window rectangle for a card of size sz.
func cmdKCardRect(gtx layout.Context, at image.Point, sz image.Point) image.Rectangle {
	top := at.Y + gtx.Dp(cmdKOffsetY) - int(float32(sz.Y)*1.04+.5)
	return image.Rectangle{Min: image.Pt(at.X, top), Max: image.Pt(at.X+sz.X, top+sz.Y)}
}

func (m *cmdKCoachmark) Layout(gtx layout.Context) layout.Dimensions {
	if m.done {
		return layout.Dimensions{}
	}
	c := m.c
	// gone with its MessageInput: the composer wasn't drawn this frame
	// (another conversation opened, the pane closed)
	if c == nil || c.done || !c.drawnAt.Equal(gtx.Now) {
		m.Dismiss()
		return layout.Dimensions{}
	}
	at, ok := m.anchor()
	if !ok {
		return layout.Dimensions{}
	}
	th := c.h.Theme()
	p := cmdKColors(th)
	win := gtx.Constraints.Max

	runs := cmdKRuns(gtx, th)
	lines := cmdKLines(gtx, th, runs, gtx.Dp(cmdKW-2*cmdKPadX))
	sz := cmdKCardSize(gtx, lines)
	card := cmdKCardRect(gtx, at, sz)

	// input: a press on the overlay closes; the card swallows its own
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &m.scrim, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			m.Dismiss()
		}
	}
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &m.card, Kinds: pointer.Press | pointer.Release}); !ok {
			break
		}
	}
	if m.done {
		return layout.Dimensions{}
	}

	// the overlay: position fixed, inset 0, rgba(0,0,0,.3)
	func() {
		defer clip.Rect{Max: win}.Push(gtx.Ops).Pop()
		paint.ColorOp{Color: p.overlay}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		event.Op(gtx.Ops, &m.scrim)
	}()

	rad := gtx.Dp(cmdKRadius)
	// 0 18px 48px rgba(0,0,0,.08), then the 1px ring, then the card
	theme.Shadow(gtx, card.Add(image.Pt(0, gtx.Dp(18))), rad, gtx.Dp(24), p.shadow)
	ring := max(1, gtx.Dp(1))
	theme.RRect(gtx, card.Inset(-ring), rad+ring, p.ring)
	theme.RRect(gtx, card, rad, p.bg)
	m.arrow(gtx, card, p)
	func() {
		defer clip.Rect(card).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &m.card)
	}()

	// the text
	y := card.Min.Y + gtx.Dp(cmdKPadT)
	for _, ln := range lines {
		lh := cmdKLineHeight(gtx, ln)
		x := card.Min.X + gtx.Dp(cmdKPadX)
		for _, r := range ln {
			if r.chip {
				m.chip(gtx, th, p, image.Pt(x+gtx.Dp(cmdKChipM), y+(lh-gtx.Dp(cmdKChipH+cmdKChipM))/2), r)
			} else {
				func() {
					defer op.Offset(image.Pt(x, y+(lh-gtx.Dp(cmdKLineH))/2)).Push(gtx.Ops).Pop()
					g := gtx
					g.Constraints = layout.Constraints{Max: image.Pt(r.w+gtx.Dp(2), gtx.Dp(cmdKLineH))}
					th.Label(g, 15, theme.Regular, p.fg, r.text)
				}()
			}
			x += r.w
		}
		y += lh
	}
	return layout.Dimensions{Size: win}
}

// arrow draws :before (the outline) and :after (the fill) under card.
func (m *cmdKCoachmark) arrow(gtx layout.Context, card image.Rectangle, p cmdKPal) {
	cx := card.Min.X + int(float32(card.Dx())*.10+.5) // left: var(--arrow-position) = 10%
	tri := func(half int, top int, col color.NRGBA) {
		// margin-left −12 / −13 with border widths 16 / 17: the box is
		// 2×border wide, its tip at margin + border
		x0 := cx - gtx.Dp(cmdKArrowX) - (half - gtx.Dp(cmdKArrow))
		tip := image.Pt(x0+half, top+half)
		var path clip.Path
		path.Begin(gtx.Ops)
		path.MoveTo(layout.FPt(image.Pt(x0, top)))
		path.LineTo(layout.FPt(image.Pt(x0+2*half, top)))
		path.LineTo(layout.FPt(tip))
		path.Close()
		paint.FillShape(gtx.Ops, col, clip.Outline{Path: path.End()}.Op())
	}
	tri(gtx.Dp(cmdKArrow+1), card.Max.Y, p.ring)
	tri(gtx.Dp(cmdKArrow), card.Max.Y, p.bg)
}

// chip draws KeyboardKey slim at at (its margin box's top-left inside the
// 4px inline margin): base-ter, radius 4, a 1px otl-ter bottom edge, the
// label in 13px content-pry, or the command icon on macOS.
func (m *cmdKCoachmark) chip(gtx layout.Context, th *theme.Theme, p cmdKPal, at image.Point, r cmdKRun) {
	w := r.w - 2*gtx.Dp(cmdKChipM)
	kr := image.Rectangle{Min: at, Max: at.Add(image.Pt(w, gtx.Dp(cmdKChipH)))}
	rad := gtx.Dp(4)
	theme.RRect(gtx, kr, rad, p.chipLine)
	inner := kr
	inner.Max.Y -= max(1, gtx.Dp(1))
	theme.RRect(gtx, inner, rad, p.chip)
	if r.text == "" {
		is := gtx.Dp(14)
		defer op.Offset(image.Pt(kr.Min.X+(w-is)/2, inner.Min.Y+(inner.Dy()-is)/2)).Push(gtx.Ops).Pop()
		icons.Get("command")(gtx, unit.Dp(14), p.chipText)
		return
	}
	lw := th.Measure(gtx, 13, theme.Regular, r.text)
	defer op.Offset(image.Pt(kr.Min.X+(w-lw)/2, inner.Min.Y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(lw+gtx.Dp(2), inner.Dy())}
	rec := op.Record(gtx.Ops)
	d := th.Label(g, 13, theme.Regular, p.chipText, r.text)
	call := rec.Stop()
	defer op.Offset(image.Pt(0, (inner.Dy()-d.Size.Y)/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}
