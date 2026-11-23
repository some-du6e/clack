package popups

// A message's time is a link: Slack's Timestamp with a link (boot-shared
// JO00, CDN build 36d315d11944) renders the label in an <a> (CFnj Link)
// to the message's permalink (getPermalinkForMessage, 1PIM), class
// c-timestamp: the hand cursor, the label underlined under the pointer,
// TimestampTooltip after 300 ms. MessageTimestamp (azta) makes its click
// handleTimestampClick (Gi27) instead of the browser's navigation: with
// Cmd (Ctrl off macOS: isCmdKey) on a reply, or on a message the view
// isn't showing, the message's thread opens in a tile with it
// highlighted; else the message list takes the focus at it and the
// permalink opens (openPermalink), which routes to the message and
// highlights it as an opened link does. A right-click is a link's: the
// desktop shell's link menu (Copy Link, Open Link), which the
// containers open (NewLinkMenu).

import (
	"image"
	"image/color"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TimeLinkTipDelay is Timestamp's Tooltip delay.
const TimeLinkTipDelay = 300 * time.Millisecond

// TimeLink is one message time's link.
type TimeLink struct {
	click gesture.Click
	menu  int // the right-click's target (Menu)
	tip   TipState
	shown bool // laid out this frame
}

// TimeLinkClick is a click on the time.
type TimeLinkClick struct {
	Clicked bool
	Cmd     bool // Cmd (Ctrl off macOS) held: isCmdKey, a tile
	// Menu is a right-click (ctxclick.Press) on the time: the link's
	// menu, for a container that has no context menu of its own
	Menu bool
}

// Hovered reports whether the pointer is on the time (it was laid out
// this frame).
func (t *TimeLink) Hovered() bool { return t.shown && t.click.Hovered() }

// Shown reports whether the time was laid out (and not skipped since).
func (t *TimeLink) Shown() bool { return t.shown }

// Tag is the link's pointer tag (the router's AreaBounds finds the time).
func (t *TimeLink) Tag() event.Tag { return &t.click }

// TipShowing reports whether its full-date tooltip is up at now.
func (t *TimeLink) TipShowing(now time.Time) bool { return t.tip.Showing(now) }

// Skip is a frame the time isn't drawn in: its hover and tooltip end.
func (t *TimeLink) Skip() {
	t.shown = false
	t.tip.Reset()
}

// Begin starts the time's owner's frame (a row's layout): until Layout
// runs again Hovered is false, so a time the row no longer draws (a row
// that became grouped, or a header again) can't hold a stale hover.
func (t *TimeLink) Begin() { t.shown = false }

// Layout makes the label laid out at the current offset (size, its
// baseline from the top, its colour) the link: it reads the click, takes
// the pointer over the label (the hand cursor), underlines the label while
// hovered and shows tip's text (the full date; nil for none) after
// 300 ms. tip is asked once the frame's events are read, so the frame the
// pointer comes in starts the tooltip's delay.
func (t *TimeLink) Layout(gtx layout.Context, th *theme.Theme, size image.Point, baseline int, col color.NRGBA, tip func() string) TimeLinkClick {
	var c TimeLinkClick
	for {
		e, ok := t.click.Update(gtx.Source)
		if !ok {
			break
		}
		// a Ctrl+click on macOS is a right-click (ctxclick.Context): the
		// menu, not the link
		if e.Kind == gesture.KindClick && !(ctxclick.Mac && e.Modifiers.Contain(key.ModCtrl)) {
			c.Clicked, c.Cmd = true, e.Modifiers.Contain(key.ModShortcut)
		}
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &t.menu, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			c.Menu = true
		}
	}
	t.shown = true
	r := image.Rectangle{Max: size}
	a := clip.Rect(r).Push(gtx.Ops)
	pointer.CursorPointer.Add(gtx.Ops)
	t.click.Add(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &t.menu)
	pass.Pop()
	a.Pop()
	hov := t.click.Hovered()
	if hov && baseline > 0 {
		// text-decoration: underline, in the label's colour: 1px under
		// the baseline
		lw := max(1, gtx.Dp(1))
		y := min(size.Y-lw, baseline+gtx.Dp(1))
		paint.FillShape(gtx.Ops, col, clip.Rect{Min: image.Pt(0, y), Max: image.Pt(size.X, y+lw)}.Op())
	}
	txt := ""
	if hov && tip != nil {
		txt = tip()
	}
	t.tip.Delay = TimeLinkTipDelay
	Tooltip(gtx, th, &t.tip, hov && txt != "", size, Above, txt, "")
	return c
}

// TimestampOpener is optional on a Host: handleTimestampClick for the
// message at ch/ts (thread its thread_ts, "" for none); tile is a Cmd
// click (the ui root's App.OpenMessageTime).
type TimestampOpener interface {
	OpenMessageTime(ch, ts, thread string, tile bool)
}

// OpenMessageTime is a click on a message's time: the Host's
// handleTimestampClick, else its permalink as a link.
func OpenMessageTime(h Host, ch, ts, thread string, tile bool) {
	if o, ok := h.(TimestampOpener); ok {
		o.OpenMessageTime(ch, ts, thread, tile)
		return
	}
	h.Navigate("c/" + ch)
}

// TimeLinkMenu is the time's right-click: the shell's link menu for the
// message's permalink.
func TimeLinkMenu(h Host, at image.Point, ch, ts, thread string) *Menu {
	link := Permalink(h, ch, ts, thread)
	return NewLinkMenu(h, at, LinkMenu{URL: link, Open: func() { OpenMessageTime(h, ch, ts, thread, false) }})
}
