package ui

import (
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The channel tab bar's hover peeks, as Slack's web client wires them
// (build 889ac449a32d):
//
//   - The bar's labels are ChannelTabLabelWithPeek (the bookmarks-store
//     chunk, next to "ChannelTabBarBase"); its renderPeek is useTabPeek's
//     view of the tab (tabPeekBodyFor, the slugs' too).
//   - StyledChannelTabLabel wraps the label in PeekTrigger (boot-shared):
//     triggerDelay 300, position "bottom-left", offsetX -8, offsetY -6,
//     peekId "<channel id>-<tab id>". The rail's PeekTrigger port
//     (rail_tab_peek.go) runs it: the delay, a press closes it and the
//     click goes on to the tab, Escape, the safe area, one peek at a time,
//     peeksEnabledOnMouseEnter. animateIn is off: the Popover doesn't fade,
//     the card (ChannelTabPeek in ChannelHeaderPeekContainer,
//     popups.PeekContainer) slides and fades in itself.
//   - No trigger on the selected tab, on a label in a menu row (the More
//     menu), or for a type isPreviewEnabled (client-boot) leaves out; the
//     channel type renders an empty fragment, so Messages has none.
//   - The trigger is the label: .channel_tab_label__WjErI's flex box,
//     padding 8px around the icon box, the 4px gap and the text, centred in
//     the 38px tab (.channel_tab__B1pww is an inline-flex, align-items
//     centre); its line is the 13px text's 1.25 (16.25px), so the box is
//     32.25px tall.
//   - While the card shows, the tab keeps its hover look
//     (.channel_tab__B1pww:has(.p-peek_trigger--is-peek-visible)).

// barPeekTypes is isPreviewEnabled's list.
var barPeekTypes = map[string]bool{
	"canvas": true, "list": true, "files": true, "folder": true, "bookmarks": true, "pins": true, "workflows": true,
	"channel": true, "code": true, "code_channel_pull_request": true, "native_file": true, "external_files": true,
	"agent_view": true, "agent_sessions": true, "automations": true, "salesforce_list_view": true,
	"salesforce_record": true, "record": true, "salesforce_record_related_list": true, "record_related_list": true,
}

// barPeek is one channel tab's peek source: the tab it was last drawn for.
type barPeek struct {
	ch  string
	tab core.SlugTab
}

// barPeekState is the bar's pointer offset (local to window coordinates,
// from its last pointer event: the More tab's trick) and its tabs' peeks.
type barPeekState struct {
	tag   int
	off   image.Point
	known bool
	peeks map[string]barPeek // by peekId
}

// barTabPeekKey is PeekTrigger's peekId for the tab.
func barTabPeekKey(ch, tabID string) string { return ch + "-" + tabID }

// chTabID is the tab's tabz id.
func chTabID(t chTab) string { return strings.TrimPrefix(t.key, "tab:") }

// slugTabOf is the tab as useTabPeek reads it (the tabz entry).
func slugTabOf(t chTab) core.SlugTab {
	return core.SlugTab{ID: chTabID(t), Type: t.typ, Label: t.rawLabel, FileID: t.fileID, DefaultView: t.defaultView, FolderID: t.folder}
}

// barPeekEvents reads the bar's pointer events for its window offset.
func (a *App) barPeekEvents(gtx layout.Context) {
	st := &a.barPeeks
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &st.tag, Kinds: pointer.Move | pointer.Enter | pointer.Press | pointer.Drag})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && a.ptrOK {
			st.off = a.ptrWin.Sub(image.Pt(int(e.Position.X+.5), int(e.Position.Y+.5)))
			st.known = true
		}
	}
}

// barPeekArea registers the bar for barPeekEvents (sz: the bar).
func (a *App) barPeekArea(gtx layout.Context, sz image.Point) {
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &a.barPeeks.tag)
}

// barTabPeekFrame runs a tab's PeekTrigger: box is the tab (bar
// coordinates, sz.Y tall), selected whether it's the bar's current tab.
func (a *App) barTabPeekFrame(gtx layout.Context, ch string, t chTab, box image.Rectangle, selected bool) {
	if t.key == "messages" {
		return // the channel type: an empty fragment, no trigger
	}
	st := &a.barPeeks
	key := barTabPeekKey(ch, chTabID(t))
	if st.peeks == nil {
		st.peeks = map[string]barPeek{}
	}
	st.peeks[key] = barPeek{ch: ch, tab: slugTabOf(t)}
	lh := gtx.Dp(unit.Dp(32.25))
	trig := image.Rect(box.Min.X, box.Min.Y+(box.Dy()-lh)/2, box.Max.X, box.Min.Y+(box.Dy()-lh)/2+lh).Add(st.off)
	hov := st.known && a.ptrOK && a.ptrWin.In(trig)
	a.tabPeekFrame(gtx, key, trig, hov, selected || !barPeekTypes[t.typ] || !st.known)
	a.tabPeek(key).bar = true // after: a reset trigger keeps being the bar's
}

// barPeekEndFrame is tabPeekEndFrame for the bar's triggers, after the
// views that draw bars: a tab no bar drew this frame (it scrolled into the
// More menu, the bar went away) unmounts and its peek closes.
func (a *App) barPeekEndFrame() {
	for _, p := range a.tpeeks.m {
		if !p.bar {
			continue
		}
		p.mounted, p.seen = p.seen, false
		if !p.mounted && a.tabPeekOpen(p.key) {
			p.pop.Dismiss()
		}
	}
}

// barPeekContent is the card for peekId key, nil for none.
func (a *App) barPeekContent(key string) tabPeekContent {
	bp, ok := a.barPeeks.peeks[key]
	if !ok {
		return nil
	}
	sp := &slugPeek{a: a, ch: bp.ch, tab: bp.tab}
	body := a.tabPeekBodyFor(sp, bp.ch, bp.tab)
	if body == nil {
		return nil
	}
	sp.body = body
	return &barTabPeek{a: a, key: key, sp: sp}
}

// barTabPeek is ChannelTabPeek on the bar: the slug peek's card, placed by
// the Popover (Bare: the container brings its own chrome).
type barTabPeek struct {
	a     *App
	key   string
	sp    *slugPeek
	start time.Time
}

func (b *barTabPeek) popover(o *popups.PopoverOpts) {
	o.Popover, o.OffsetX, o.OffsetY = "bottom-left", -8, -6
	o.AnimateIn, o.Bare = false, true
}

func (b *barTabPeek) opened() {}

// closed is the card's unmount (a canvas peek's 6 s mark-read).
func (b *barTabPeek) closed() { b.sp.closed() }

func (b *barTabPeek) layout(gtx layout.Context) layout.Dimensions {
	a, p := b.a, b.sp
	if p.done { // a list peek's click opened the tab
		a.closeTabPeek(b.key)
	}
	win := a.winSize
	h, ok := p.body.height(gtx)
	if !ok {
		return layout.Dimensions{}
	}
	w := gtx.Dp(360)
	maxH := int(float32(win.Y) * .75) // .content__p5cYb max-height: 75vh
	if h == peekDefault {             // .content__r_uvR: calc(85vh - 300px)
		h = max(0, int(float32(win.Y)*.85)-gtx.Dp(300))
	}
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(w, maxH)}
	if h != peekAuto {
		h = min(h, maxH)
		g.Constraints = layout.Exact(image.Pt(w, h))
	}
	m := op.Record(gtx.Ops)
	d := func() layout.Dimensions {
		defer clip.Rect{Max: g.Constraints.Max}.Push(g.Ops).Pop()
		return p.body.layout(g)
	}()
	call := m.Stop()
	if h == peekAuto {
		h = min(d.Size.Y, maxH)
	}
	sz := image.Pt(w, h)
	if b.start.IsZero() {
		b.start = gtx.Now
	}
	var draw image.Point
	if !a.reduceMotion() { // .containerAnimateIn__p1IDs (off with a11y_animations off)
		if el := gtx.Now.Sub(b.start); el < popups.PeekAnimateIn {
			x := float64(el) / float64(popups.PeekAnimateIn)
			draw.Y += int(float64(gtx.Dp(-8)) * (1 - ccBezier(.25, 1, .5, 1, x)))
			defer paint.PushOpacity(gtx.Ops, float32(ccBezier(.33, 1, .68, 1, x))).Pop()
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	popups.PeekContainer(gtx, a.th, image.Rectangle{Min: draw, Max: draw.Add(sz)})
	func() {
		defer op.Offset(draw).Push(gtx.Ops).Pop()
		defer clip.UniformRRect(image.Rectangle{Max: sz}, gtx.Dp(12)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	return layout.Dimensions{Size: sz}
}

// tabLookHook, when set (tests), sees each tab key drawn with its hover
// look.
var tabLookHook func(key string)

// tabHoverLook is a channel tab's :hover (and peek-visible, and :active)
// look, client-boot-styles: the whole 38px tab under a
// linear-gradient(var(--dt_color-base-pry-hover)) background-image
// (base-pry-pressed while pressed) blended with
// --dt_blend-mode-interaction, its top corners 8px, and on a tab that
// isn't the selected one box-shadow: inset 0 -1px 0 0
// rgb(var(--sk_foreground_low_solid)).
func (a *App) tabHoverLook(gtx layout.Context, s image.Point, pressed, selected bool) {
	th := a.th
	layer := color.NRGBA{R: 0x45, G: 0x44, B: 0x47, A: 0x0f} // #4544470f
	if pressed {
		layer.A = 0x21 // base-pry-pressed #45444721
	}
	line := color.NRGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff}
	if th.Dark {
		layer = color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0x14} // #f8f8f814
		if pressed {
			layer.A = 0x2e // #f8f8f82e
		}
		line = color.NRGBA{R: 0x35, G: 0x37, B: 0x3b, A: 0xff}
	}
	if t := th.Tokens; t != nil && t.Overridden() {
		expr := "var(--dt_color-base-pry-hover)"
		if pressed {
			expr = "var(--dt_color-base-pry-pressed)"
		}
		theme.SetColor(t, &layer, expr)
		theme.SetColor(t, &line, "rgb(var(--sk_foreground_low_solid))")
	}
	// multiply (lighten in dark mode) over the bar's base-pry: over white
	// (black) that is the layer itself at its alpha
	r := image.Rectangle{Max: s}
	rad := gtx.Dp(8)
	func() {
		defer clip.RRect{Rect: r, NW: rad, NE: rad}.Push(gtx.Ops).Pop()
		paint.ColorOp{Color: layer}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}()
	if !selected {
		theme.Fill(gtx, image.Rect(0, s.Y-max(1, gtx.Dp(1)), s.X, s.Y), line)
	}
}
