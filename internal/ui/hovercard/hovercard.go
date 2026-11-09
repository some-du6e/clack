// Package hovercard is the timing and placement of Slack's profile hover
// card: the card that opens when the pointer rests on a message author's
// name or avatar, an @mention, or a member in a list.
//
// Where Slack has the trigger (2026-11 web client): ProfileHoverTrigger
// is registered as renderProfileHoverCard (client-boot QvaD yN, boot-render
// 26v2 CZ.yN.register), and these call it: the message sender's name
// (/OeL MessageSender, also in search results) and avatar (xc3R
// BaseAvatar with showCard, isInteractive and size >= 36: message
// avatars, search result avatars, the DM foreword's 56px avatars), an
// @mention (ld4x MemberSlug), member lists that pass showHoverCard
// (LGvk MediumMemberEntity wraps the primary name only: the channel
// details Members tab, channel managers, the apps tab), the Activity
// feed's reaction headers (U5s4 ActivityItemMember: "X reacted to your
// message"), system-message rollups (Nwo1 "X joined", c-message__rollup_
// member) and file/canvas/list owners. Not: reaction pills and their
// tooltips, the sidebar's DMs, thread reply-bar facepiles (24px,
// isInteractive false), huddle participants.
//
// Triggers anywhere in the UI report themselves during layout (Trigger, or
// Report for views that hit-test their own content, like richview's
// mentions). The ui root owns one Controller: after the content is laid
// out it runs Slack's timers (Frame), draws the card (popups'
// NewHoverProfileCard) where Place puts it, and closes it on Escape.
//
// Slack's behaviour, from the web client on the public CDN (2026-11,
// gantry-v2-async-client-boot-shared module 0oEe HoverCardTrigger,
// gantry-v2-async-client-boot-render module SmYd ProfileHoverTrigger,
// client-boot module L1PI Popover):
//
//   - The card opens after the pointer has rested on the trigger for
//     showDelay = 350ms (useDelayedCallback(open)(350) on mouseover).
//   - Leaving the trigger before then cancels it. Leaving it while the card
//     is open closes the card after 250ms (q(250) in handleLeave). The card
//     is a portal inside the trigger's React tree, so moving onto the card
//     counts as staying on the trigger and cancels the close; leaving the
//     card starts the same 250ms close.
//   - Moving from one trigger to another schedules the first card's close
//     (250ms) and the second card's open (350ms) independently.
//   - A click on the trigger closes the card (handleClick → close). Clack's
//     click then opens the sticky click card.
//   - Escape closes it from a document-level capture listener, before any
//     focused element sees the key. The card never takes focus
//     (shouldFocusAfterRender: false) and focusing a trigger doesn't open
//     it (revealOnFocus only applies with openOnActivation, which
//     ProfileHoverTrigger doesn't set). Blurring the trigger closes it.
//   - Placement: ProfileHoverTrigger's position is "top-left" and
//     HoverCardTrigger's offsetY is -12, so the card's bottom-left corner
//     sits 12px above the trigger's top-left corner. The Popover keeps the
//     card 16px (allowanceX) from the window's sides and 10px (allowanceY)
//     from its top and bottom. When the card has to move vertically to fit
//     and tryOppositePosition is set (it is), it tries "bottom-left" with
//     the offset negated (12px below the trigger's bottom edge) and uses it
//     if it fits entirely. Otherwise it stays clamped.
//   - The popover fades in over 80ms (shouldFade: "80ms").
package hovercard

import (
	"image"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Slack's constants (see the package comment).
const (
	ShowDelay = 350 * time.Millisecond
	HideDelay = 250 * time.Millisecond
	Fade      = 80 * time.Millisecond

	OffsetY    = 12 // dp between the trigger and the card
	AllowanceX = 16 // dp from the window's left and right edges
	AllowanceY = 10 // dp from the window's top and bottom edges
)

// Request is one trigger under the pointer this frame.
type Request struct {
	Key    any             // the trigger's identity (stable across frames)
	UserID string          // the member to show
	BotID  string          // a bot with no user (Slack's serviceId): AppHoverCard
	Anchor image.Rectangle // the trigger's bounds in window coordinates
}

// Controller runs the hover card's timers. It belongs to the UI goroutine.
type Controller struct {
	// Pointer returns the last pointer position in window coordinates. It
	// converts a trigger's local bounds to window bounds.
	Pointer func() image.Point

	rep    Request // the trigger reported this frame
	hasRep bool

	pending Request // waiting for showDelay
	hasPend bool
	showAt  time.Time

	open     Request
	isOpen   bool
	openedAt time.Time
	hideAt   time.Time // zero: no close scheduled
	gen      int       // bumps each time a card opens

	card     image.Rectangle // the drawn card, window coordinates
	overCard bool
	cardTag  int

	spent any // a trigger pressed while hovered: ignored until it is left
}

var current *Controller

// SetDefault makes c the controller that Report and Trigger feed. The ui
// root calls it once.
func SetDefault(c *Controller) { current = c }

// Default returns the controller set by SetDefault (nil before).
func Default() *Controller { return current }

// Report says the pointer is on a profile trigger this frame. local is the
// trigger's bounds and ptr the pointer, both in the caller's coordinates
// (from the same pointer event), so the window bounds are local moved by
// window pointer - ptr.
func Report(key any, userID string, local image.Rectangle, ptr f32.Point) {
	c := current
	if c == nil || userID == "" {
		return
	}
	c.Report(key, userID, local, ptr)
}

// Report is the package Report for this controller.
func (c *Controller) Report(key any, userID string, local image.Rectangle, ptr f32.Point) {
	c.ReportBot(key, userID, "", local, ptr)
}

// ReportBot is Report for a trigger that may name a bot instead of (or
// with) a user: Slack's ProfileHoverTrigger takes memberId and serviceId.
func (c *Controller) ReportBot(key any, userID, botID string, local image.Rectangle, ptr f32.Point) {
	if key == c.spent || userID == "" && botID == "" {
		return
	}
	off := image.Point{}
	if c.Pointer != nil {
		off = c.Pointer().Sub(image.Pt(int(ptr.X+.5), int(ptr.Y+.5)))
	}
	c.rep = Request{Key: key, UserID: userID, BotID: botID, Anchor: local.Add(off)}
	c.hasRep = true
}

// Pressed is a click on a trigger: Slack's handleClick closes the card,
// and the trigger stays quiet until the pointer leaves it.
func (c *Controller) Pressed(key any) {
	c.spent = key
	c.hasPend = false
	if c.isOpen && c.open.Key == key {
		c.Close()
	}
}

// Left is the pointer leaving a trigger.
func (c *Controller) Left(key any) {
	if c.spent == key {
		c.spent = nil
	}
}

// Open returns the card to draw, if any, and a generation that changes
// whenever a new card opens (so the caller can rebuild it).
func (c *Controller) Open() (Request, int, bool) {
	return c.open, c.gen, c.isOpen
}

// Show opens r's card now, without the delay (tests and screenshots).
func (c *Controller) Show(r Request, now time.Time) {
	c.open, c.isOpen = r, true
	c.hasPend = false
	c.hideAt = time.Time{}
	c.openedAt = now.Add(-Fade)
	c.overCard = false
	c.gen++
}

// Pending returns the trigger waiting for its show delay, if any.
func (c *Controller) Pending() (Request, bool) { return c.pending, c.hasPend }

// OpenedAt is when the current card opened (for its fade).
func (c *Controller) OpenedAt() time.Time { return c.openedAt }

// Close closes the card and forgets any pending one.
func (c *Controller) Close() {
	c.closeCard()
	c.hasPend = false
}

// closeCard closes the open card, keeping a pending one (another trigger's
// card still opens on its own timer).
func (c *Controller) closeCard() {
	c.isOpen = false
	c.hideAt = time.Time{}
	c.overCard = false
	c.card = image.Rectangle{}
}

// Frame runs the timers once the frame's triggers have reported. blocked
// (a menu, modal or other popover is open) closes everything unless hold
// (the card's own menu is open) keeps the card as it is. It returns the
// next time Frame must run again (zero: none).
func (c *Controller) Frame(now time.Time, blocked, hold bool) time.Time {
	rep, has := c.rep, c.hasRep
	c.hasRep = false
	if hold && c.isOpen {
		c.hasPend = false
		c.hideAt = time.Time{}
		return time.Time{}
	}
	if blocked {
		c.Close()
		return time.Time{}
	}
	onOpen := c.isOpen && (c.overCard || has && rep.Key == c.open.Key)
	switch {
	case onOpen:
		c.hideAt = time.Time{}
		c.hasPend = false
	case has:
		if !c.hasPend || c.pending.Key != rep.Key {
			c.pending, c.hasPend = rep, true
			c.showAt = now.Add(ShowDelay)
		} else {
			c.pending.Anchor = rep.Anchor
			c.pending.UserID, c.pending.BotID = rep.UserID, rep.BotID
		}
		if c.isOpen && c.hideAt.IsZero() {
			c.hideAt = now.Add(HideDelay)
		}
	default:
		c.hasPend = false
		if c.isOpen && c.hideAt.IsZero() {
			c.hideAt = now.Add(HideDelay)
		}
	}
	if c.isOpen && !c.hideAt.IsZero() && !now.Before(c.hideAt) {
		c.closeCard()
	}
	if c.hasPend && !now.Before(c.showAt) {
		c.open, c.isOpen = c.pending, true
		c.hasPend = false
		c.hideAt = time.Time{}
		c.openedAt = now
		c.overCard = false
		c.gen++
	}
	next := time.Time{}
	if c.hasPend {
		next = c.showAt
	}
	if c.isOpen && !c.hideAt.IsZero() && (next.IsZero() || c.hideAt.Before(next)) {
		next = c.hideAt
	}
	if c.isOpen && now.Sub(c.openedAt) < Fade {
		if f := c.openedAt.Add(Fade); next.IsZero() || f.Before(next) {
			next = f
		}
	}
	return next
}

// Opacity is the card's fade-in at now (0..1).
func (c *Controller) Opacity(now time.Time) float32 {
	d := now.Sub(c.openedAt)
	if d >= Fade || d < 0 {
		return 1
	}
	return float32(d) / float32(Fade)
}

// CardArea registers the drawn card's bounds r (window coordinates, gtx at
// the window origin) so the pointer resting on the card keeps it open. It
// is pass-through: the card's own buttons sit under it.
func (c *Controller) CardArea(gtx layout.Context, r image.Rectangle) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &c.cardTag, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok {
			switch pe.Kind {
			case pointer.Enter, pointer.Move:
				c.overCard = true
			case pointer.Leave, pointer.Cancel:
				c.overCard = false
			}
		}
	}
	c.card = r
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &c.cardTag)
}

// OverCard reports whether the pointer is on the drawn card.
func (c *Controller) OverCard() bool { return c.isOpen && c.overCard }

// Place is Slack's Popover.keepInBounds for position "top-left",
// offsetY -12, tryOppositePosition: the card's top-left corner for a card
// of size sz over anchor, in a window of size win. Distances are pixels:
// gap is OffsetY, ax and ay the allowances, already scaled.
func Place(anchor image.Rectangle, sz, win image.Point, gap, ax, ay int) image.Point {
	left, right := ax, win.X-ax
	top, bottom := ay, win.Y-ay
	x := anchor.Min.X
	if x < left {
		x = left
	} else if x+sz.X > right {
		x = max(left, right-sz.X)
	}
	y0 := anchor.Min.Y - gap - sz.Y // top-left: above the trigger
	y := y0
	if y < top {
		y = top
	} else if y+sz.Y > bottom {
		y = max(top, bottom-sz.Y)
	}
	if y != y0 {
		// bottom-left with the offset negated: below the trigger
		if oy := anchor.Max.Y + gap; oy >= top && oy+sz.Y <= bottom {
			y = oy
		}
	}
	return image.Pt(x, y)
}

// Trigger is a profile trigger drawn by a widget: a pass-through area over
// the widget that reports while the pointer is on it. Lay it out after the
// widget, at the widget's origin.
type Trigger struct {
	tag int
	in  bool
	pos f32.Point
}

// Layout registers the trigger over size and reports it while hovered.
// userID "" disables it.
func (t *Trigger) Layout(gtx layout.Context, userID string, size image.Point) {
	t.LayoutBot(gtx, userID, "", size)
}

// LayoutBot is Layout for a sender that may be a bot without a user
// (botID: the message's bot_id). Both "" disables it.
func (t *Trigger) LayoutBot(gtx layout.Context, userID, botID string, size image.Point) {
	c := current
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &t.tag, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel | pointer.Press})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Enter, pointer.Move:
			t.in, t.pos = true, pe.Position
		case pointer.Leave, pointer.Cancel:
			t.in = false
			if c != nil {
				c.Left(t)
			}
		case pointer.Press:
			t.pos = pe.Position
			if c != nil {
				c.Pressed(t)
			}
		}
	}
	if t.in && c != nil && (userID != "" || botID != "") {
		c.ReportBot(t, userID, botID, image.Rectangle{Max: size}, t.pos)
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &t.tag)
}

// Hovered reports whether the pointer is on the trigger.
func (t *Trigger) Hovered() bool { return t.in }

// Invalidate asks for a frame at next (zero: none).
func Invalidate(gtx layout.Context, next time.Time) {
	if !next.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: next})
	}
}
