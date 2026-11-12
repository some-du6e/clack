package huddleui

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The toolbar's emoji button (HuddleToolbarEmojiPickerButton, dltD) opens
// HuddleEmojiMenuTrigger's popover (gantry-v2-async-client-boot-shared
// Vw0B, build d3de3a487b1c): position "top", offsetY -5, holding
// HuddleQuickEmojiPicker, a c-menu card (.p-huddle_quick_emoji_picker:
// 360px wide, sk_primary_background) with
//
//   - Slack Kit tabs (.p-huddle_quick_emoji_picker__tab_menu: padding 2px
//     8px 0; c-tabs__tab 13px bold, content-sec, content-pry and a 2px
//     inset theme-base-pry underline when active; the menu's 1px otl-ter
//     line) over the tab panel: Reactions, Effects, then GIFs where GIPHY
//     is on. Reactions is the default (lastActiveTab "reactions");
//   - the close button at the top right (.p-huddle_quick_emoji_picker__
//     right_controls: top 2px, right 0; a medium icon button, `close`);
//   - the Reactions panel: the bare EmojiPicker (hideTabs,
//     isHuddlesEmojiPicker, forceHideEmojiPackCTA), 260px tall, 200px in
//     compact mode (isHuddleWindowOpen && getIsCompactMode: the huddle
//     window).
//
// A pick sends a standard reaction (useSendHuddleReaction, reactionType
// "standard") and closes the popover. Clack draws the Reactions tab only:
// Effects are Slack's Lottie packs and GIFs need GIPHY
// (docs/HUDDLES-AUDIT.md "Emoji picker"). The sticker tray is the same
// component with onlyShowStickers (hand.go).

const (
	qpW          = 360 // .p-huddle_quick_emoji_picker width
	qpTabMenuPad = 2   // .p-huddle_quick_emoji_picker__tab_menu padding-top
	qpTabPad     = 8   // its padding-left; .c-tabs__tab margin 0 8px
	qpTabH       = 32  // .c-tabs__tab: padding 8px 0 around 13px × 1.25
	qpPanelH     = 260 // the Reactions panel (.c-menu.p-emoji_picker)
	qpPanelHC    = 200 // …--compact
	qpCloseW     = 36  // c-icon_button--size_medium
	qpOffsetY    = 5   // the popover's offsetY -5
)

// quickPicker is HuddleQuickEmojiPicker in its popover: an overlay over
// the window it opens in (the huddle window, or the main window for the
// docked bar's emoji button).
type quickPicker struct {
	h       popups.Host
	anchor  image.Rectangle // the emoji button, window px
	compact bool
	react   func(name string)

	picker *popups.EmojiPicker
	tab    widget.Clickable
	close  widget.Clickable
	scrim  int
	card   image.Rectangle // last frame's card, window px
	done   bool
}

// newQuickPicker opens the quick picker above anchor; react sends a pick.
func newQuickPicker(h popups.Host, anchor image.Rectangle, compact bool, react func(name string)) *quickPicker {
	q := &quickPicker{h: h, anchor: anchor, compact: compact, react: react}
	q.picker = popups.NewEmbeddedEmojiPicker(h, func(name string) {
		if q.react != nil {
			q.react(name)
		}
	})
	return q
}

func (q *quickPicker) Dismiss() {
	if q.done {
		return
	}
	q.done = true
	q.picker.Dismiss()
}

func (q *quickPicker) Dismissed() bool { return q.done }

// size is the card's size in px.
func (q *quickPicker) size(gtx C) image.Point {
	ph := qpPanelH
	if q.compact {
		ph = qpPanelHC
	}
	return image.Pt(gtx.Dp(qpW), gtx.Dp(qpTabMenuPad+qpTabH)+gtx.Dp(unit.Dp(ph)))
}

// place puts the card above the anchor (offsetY -5), centred on it and
// kept in the window; below it when there's no room above.
func (q *quickPicker) place(gtx C, sz, win image.Point) image.Point {
	m := gtx.Dp(8)
	x := (q.anchor.Min.X+q.anchor.Max.X)/2 - sz.X/2
	x = max(m, min(x, win.X-m-sz.X))
	y := q.anchor.Min.Y - gtx.Dp(qpOffsetY) - sz.Y
	if y < m {
		y = q.anchor.Max.Y + gtx.Dp(qpOffsetY)
		if y+sz.Y > win.Y-m {
			y = max(0, win.Y-m-sz.Y)
		}
	}
	return image.Pt(x, y)
}

var qpClass = slackc.Class("c-menu p-huddle_quick_emoji_picker")

// Layout draws the popover over the whole window (an outside press
// closes it, as Popover's overlay does).
func (q *quickPicker) Layout(gtx C) D {
	if q.done {
		return D{}
	}
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	th := q.h.Theme()
	cl := popups.ColorsOf(th)

	if q.close.Clicked(gtx) || q.picker.Dismissed() {
		q.Dismiss()
		return D{}
	}
	// the overlay: a press outside the card closes it
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &q.scrim, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok && pe.Kind == pointer.Press && !q.card.Empty() && !image.Pt(int(pe.Position.X), int(pe.Position.Y)).In(q.card) {
			q.Dismiss()
			return D{}
		}
	}
	sc := clip.Rect{Max: win}.Push(gtx.Ops)
	event.Op(gtx.Ops, &q.scrim)
	sc.Pop()

	sz := q.size(gtx)
	if sz.X > win.X-gtx.Dp(16) {
		sz.X = max(gtx.Dp(200), win.X-gtx.Dp(16))
	}
	pos := q.place(gtx, sz, win)
	q.card = image.Rectangle{Min: pos, Max: pos.Add(sz)}
	st := qpClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return D{Size: win}
	}
	shadow := func(gtx C, r image.Rectangle, rad int) { theme.Shadow(gtx, r, rad, gtx.Dp(12), cl.Shadow) }
	popups.StyledCard(gtx, st, cl.EmojiBg, cl.Ring, shadow, pos, sz, gtx.Dp(6), func(gtx C) {
		defer clip.UniformRRect(image.Rectangle{Max: sz}, gtx.Dp(6)).Push(gtx.Ops).Pop()
		// the card takes its own presses (the overlay's are outside it)
		cc := clip.Rect{Max: sz}.Push(gtx.Ops)
		event.Op(gtx.Ops, &q.card)
		cc.Pop()
		q.layoutTabs(gtx, th, cl, sz.X)
		top := gtx.Dp(qpTabMenuPad + qpTabH)
		off := op.Offset(image.Pt(0, top)).Push(gtx.Ops)
		pg := gtx
		pg.Constraints = layout.Exact(image.Pt(sz.X, sz.Y-top))
		q.picker.LayoutEmbedded(pg)
		off.Pop()
	})
	if q.picker.Dismissed() { // a pick (or Escape) this frame
		q.Dismiss()
	}
	return D{Size: win}
}

// layoutTabs is the tab menu (Reactions, active) and the close button.
func (q *quickPicker) layoutTabs(gtx C, th *theme.Theme, cl *popups.Colors, w int) {
	rowH := gtx.Dp(qpTabMenuPad + qpTabH)
	theme.Fill(gtx, image.Rect(0, rowH-max(1, gtx.Dp(1)), w, rowH), theme.Over(cl.TabLine, cl.EmojiBg)) // inset 0 -1px otl-ter
	label := nsHuddles.T("Reactions")
	ls := labelSize(gtx, th, 13, theme.Bold, label)
	tx := gtx.Dp(qpTabPad + qpTabPad)
	ty := gtx.Dp(qpTabMenuPad)
	th2 := gtx.Dp(qpTabH)
	at(gtx, image.Pt(tx, ty), image.Pt(ls.X, th2), func(gtx C) D {
		return button(gtx, &q.tab, image.Pt(ls.X, th2), nil, func(gtx C) D {
			at(gtx, image.Pt(0, (th2-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 13, theme.Bold, cl.TabFg, label) })
			// .c-tabs__tab--active: inset 0 -2px 0 0 theme-base-pry
			theme.Fill(gtx, image.Rect(0, th2-gtx.Dp(2), ls.X, th2), cl.Aubergine)
			return D{}
		})
	})
	cw := gtx.Dp(qpCloseW)
	at(gtx, image.Pt(w-cw, gtx.Dp(2)), image.Pt(cw, cw), func(gtx C) D {
		return button(gtx, &q.close, image.Pt(cw, cw), func(gtx C, hov bool) {
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(cw, cw)}, gtx.Dp(8), cl.BtnHover)
			}
		}, func(gtx C) D {
			col := cl.Fg2
			if q.close.Hovered() {
				col = cl.Fg
			}
			iconAt(gtx, image.Pt(cw, cw), "close", 20, col)
			return D{}
		})
	})
}
