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
// A channel has its own card through the same controller: Slack's
// BaseMrkdwnChannel wraps a message's #channel in HoverCardTrigger with
// its defaults (position "top", the same delays) around ChannelHoverCard,
// and Texty's TextyTooltip opens that card over a composer channel slug
// (ReportChannel, ReportTextyChannel; popups.ChannelHoverCard).
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
//   - A click on the trigger closes the card (handleClick → close), and
//     the click goes on to the trigger's own handler: Slack's member
//     links, names and avatars open the profile in the secondary pane
//     (openProfile, showMemberProfile; Clack's App.OpenProfile), never a
//     card (openOnClick is off).
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
//
// Where the card is once open (build 35b42de8b870, read from the public
// CDN without a cookie): HoverCardTrigger (boot-shared pws8) measures the
// trigger once, when the show delay ends (its open callback U:
// F(T.current.getBoundingClientRect())), and hands that rect to the
// Popover (client-boot TxLv) as targetBounds. The Popover places the card
// from it in onOpen and again only when the rect prop changes
// (componentDidUpdate's areBoundsNearlyEqual) or the card's own content
// resizes (a ResizeObserver on popoverContent, getResizeObserverForWindow
// yN2M); it listens for no scroll or window resize. So a card never
// follows its trigger: the row scrolling, the list laying out again, a
// pane opening and the window resizing all leave it where it opened
// (Keep). What closes it is the pointer: Chromium recomputes the hover
// after a scroll or a layout, so a trigger that moves out from under a
// resting pointer gets its mouseleave (handleLeave: the 250ms close, or
// a pending open dropped), which Probe makes Gio's hit test tell the
// trigger. And the card is the trigger's child (a portal in its React
// tree): a trigger that unmounts (a recycled row, another Activity tab,
// another page) takes it down at once, and its pending open with it
// (VmK6's timer is cleared on unmount). Mounted is how a trigger says it
// is still there, and Seq how it learns it wasn't for a frame: Gio
// forgets a handler that misses a frame, and with it the Leave the
// handler was owed.
//
// A canvas's channel card is not that Popover. Quip's HovercardImpl
// (quip-collab-webapp) shows a bubble whose anchorBoundsFn is measured
// by the window's positioner registry (qsRV), which measures and places
// every bubble again on resize, scroll, transitionend, visibilitychange,
// any DOM mutation and font loads: that card follows its mention.
// HovercardImpl hands the bubble its mention's element (an Anchor whose
// boundsInViewport is measured each time), and BubbleImpl's measure
// dismisses the card only when that element has left the document, or
// has measured 0x0 for 30 tries in a row. A mention that moves or scrolls
// out of the canvas's view keeps its card, placed by it (shifted into the
// window only when no side fits): the card goes with it, and once it has
// slid out from under a resting pointer, the bubble's mouseleave starts
// dismissDelayMs (250). So Frame takes a canvas trigger's rect as it
// reports it while the pointer is on the mention, and as its layout puts
// it (Follow) while the pointer rests on the card. A wheel that begins
// on the card scrolls nothing: the bubble is in the document's body,
// outside the canvas's scroller; one already scrolling the canvas keeps
// scrolling it when the card slides under the pointer, as Chromium keeps
// a wheel gesture on the scroller it began on (CardArea). Clack's canvas lays out only the rows it
// shows, so a mention whose row scrolls out entirely is unmounted, and
// closes its card at once as any trigger's does.
package hovercard

import (
	"image"
	"math"
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
	// Preview is a link preview's hover card (MessageAttachmentDefaultV3's
	// HoverCardTrigger, position "top"): what the trigger hands the root
	// to draw instead of a profile card (richview.Preview).
	Preview any
	// Texty is a person's slug in the composer: TextyTooltip's
	// renderHoverCard (boot-shared, build 32bb3cc8205b) opens
	// renderMemberProfileHoverCard({hideButtons, hideCoveragePlanLink})
	// from Texty's onMouseOver, so with no show delay, in a Popover at
	// "top" with offsetY -12 and allowance 0 (no tryOppositePosition).
	// Leaving the slug for the card keeps it (onHoverCardMouseEnter
	// cancels the hide); leaving both closes it after hideTooltipDelayed's
	// 250ms, as HideDelay.
	Texty bool
	// ChannelID is a channel's hover card (ChannelHoverCard, boot-shared
	// 7+JF): BaseMrkdwnChannel wraps a message's #channel in
	// HoverCardTrigger with its defaults (position "top", offsetY -12,
	// the same 350ms open and 250ms close), and Texty's TextyTooltip
	// opens it over a composer's channel slug (Texty set: at once,
	// allowance 0). Buttons is enableButtons: a message's card has View
	// Channel / Join Channel, the composer's none.
	ChannelID string
	Buttons   bool
	// Tab is a channel-tab slug's peek (D3FY ChannelTabSlugPeekTrigger):
	// with session_channels_slug_peek on and a tab type that has a peek,
	// ResolvedChannelTabSlug wraps the slug in HoverCardTrigger (0oEe:
	// position "top", offsetY -12, the same 350ms open and 250ms close)
	// whose card is useTabPeek's view of ChannelID's tab Tab.
	Tab string
	// Canvas is a channel mention inside a canvas (ChannelID): the
	// canvas editor's CardViewHovercardTrigger opens Slack's embedded
	// ChannelHovercard (boot-shared UyZX) after HovercardImpl's 250ms
	// anchorMouseEnter delay (Delay), not ChannelHoverCard.
	Canvas bool
	// CanvasFile and CanvasSection are the canvas and the section holding
	// that mention (the embedded menu's section rows).
	CanvasFile, CanvasSection string
	// URL is a link's UrlHoverCard (boot-shared cXYm): with
	// truncated_url_hover_card on, BaseMrkdwnLink's renderDefaultLink
	// wraps an external link that shows a tooltip in UrlHoverCardTrigger
	// (kQRT), HoverCardTrigger (0oEe) at position "top" with its
	// defaults: offsetY -12, the 350ms open and 250ms close.
	URL string
	// Opposite: the trigger's Popover sits on the opposite side
	// (usingOppositePosition). The Popover is the trigger's child and
	// keeps its state while the trigger is mounted, open or not, so with
	// stickyPosition the next card starts there (Keep.Start); the trigger
	// keeps it (ReportSide, Placed).
	Opposite bool
	// Delay replaces ShowDelay when set.
	Delay time.Duration
}

// Controller runs the hover card's timers. It belongs to the UI goroutine.
type Controller struct {
	// Pointer returns the last pointer position in window coordinates. It
	// converts a trigger's local bounds to window bounds.
	Pointer func() image.Point
	// Origin returns the frame's origin in the router's coordinates (the
	// App's FrameOrigin): the rectangles Gio's AreaBounds and FollowArea
	// answer include Gio's own title bar (Wayland without server-side
	// decorations), the FrameEvent coordinates Pointer, the card and its
	// area use don't. nil: none.
	Origin func() image.Point

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

	fol    image.Rectangle // the open canvas card's mention, laid out this frame (Follow)
	hasFol bool

	// the wheel gesture a canvas card sees (CardArea): when its last event
	// came, and whether it began on the card
	wheelTag  int
	wheelAt   time.Time
	wheelCard bool

	spent any // a trigger pressed while hovered: ignored until it is left

	seq int // Frame calls so far (Seq)
	// mounted: the open card's trigger said it is laid out since the last
	// Frame; tracks: it has said so (a trigger that never calls Mounted
	// keeps the 250ms close), for the open card and the pending one
	mounted, tracks, pendTracks bool
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

// ReportTexty is Report for a person's slug in the composer (Request.Texty).
func (c *Controller) ReportTexty(key any, userID string, local image.Rectangle, ptr f32.Point) {
	c.ReportBot(key, userID, "", local, ptr)
	if c.hasRep && c.rep.Key == key {
		c.rep.Texty = true
	}
}

// ReportChannel says the pointer is on a message's #channel this frame
// (BaseMrkdwnChannel's renderChannelHoverCardTrigger: the card with
// enableButtons, position "top").
func (c *Controller) ReportChannel(key any, channelID string, local image.Rectangle, ptr f32.Point) {
	if key == c.spent || channelID == "" {
		return
	}
	off := image.Point{}
	if c.Pointer != nil {
		off = c.Pointer().Sub(image.Pt(int(ptr.X+.5), int(ptr.Y+.5)))
	}
	c.rep = Request{Key: key, ChannelID: channelID, Buttons: true, Anchor: local.Add(off)}
	c.hasRep = true
}

// ReportTextyChannel is ReportChannel for a channel slug in the composer:
// TextyTooltip's renderHoverCard(ChannelHoverCard) with no buttons,
// opened from Texty's onMouseOver (Request.Texty).
func (c *Controller) ReportTextyChannel(key any, channelID string, local image.Rectangle, ptr f32.Point) {
	c.ReportChannel(key, channelID, local, ptr)
	if c.hasRep && c.rep.Key == key {
		c.rep.Texty, c.rep.Buttons = true, false
	}
}

// ReportTabPeek says the pointer is on a channel-tab slug whose tab has
// a peek (Request.Tab).
func (c *Controller) ReportTabPeek(key any, channelID, tab string, local image.Rectangle, ptr f32.Point) {
	c.ReportChannel(key, channelID, local, ptr)
	if c.hasRep && c.rep.Key == key {
		c.rep.Tab, c.rep.Buttons = tab, false
	}
}

// CanvasShowDelay is the canvas hovercard's open delay (HovercardImpl's
// anchorMouseEnter: wg(show, 250)).
const CanvasShowDelay = 250 * time.Millisecond

// ReportCanvasChannel says the pointer is on a canvas's channel mention
// (Request.Canvas).
func (c *Controller) ReportCanvasChannel(key any, channelID string, local image.Rectangle, ptr f32.Point) {
	c.ReportCanvasMention(key, channelID, "", "", local, ptr)
}

// ReportCanvasMention is ReportCanvasChannel for a mention in canvas
// file's section.
func (c *Controller) ReportCanvasMention(key any, channelID, file, section string, local image.Rectangle, ptr f32.Point) {
	c.ReportChannel(key, channelID, local, ptr)
	if c.hasRep && c.rep.Key == key {
		c.rep.Canvas, c.rep.Buttons, c.rep.Delay = true, false, CanvasShowDelay
		c.rep.CanvasFile, c.rep.CanvasSection = file, section
	}
}

// FrameOrigin is Origin's answer (zero without one).
func (c *Controller) FrameOrigin() image.Point {
	if c.Origin == nil {
		return image.Point{}
	}
	return c.Origin()
}

// Follows reports whether key's card is open and follows its mention
// while the pointer rests on the card (Request.Canvas: Quip's
// positioners; see the package comment). Its trigger then says where it
// is each frame it is laid out (Follow).
func (c *Controller) Follows(key any) bool {
	return key != nil && c.isOpen && c.open.Canvas && c.open.Key == key
}

// Follow says where key's mention is this frame, in window coordinates,
// as its layout put it (not as a pointer event paired it): a
// collaborator's edit moves a mention while the pointer rests on its
// card, with no pointer event on the mention. A report (the pointer on
// the mention) wins over it.
func (c *Controller) Follow(key any, r image.Rectangle) {
	if c.Follows(key) {
		c.fol, c.hasFol = r, true
	}
}

// ReportPreview says the pointer is on a link preview's hover trigger
// this frame (MessageAttachmentDefaultV3 wraps the row card in
// HoverCardTrigger: the same 350ms open and 250ms close as the profile
// card, position "top"). card is what the root draws (Request.Preview).
func (c *Controller) ReportPreview(key any, card any, local image.Rectangle, ptr f32.Point) {
	if key == c.spent || card == nil {
		return
	}
	off := image.Point{}
	if c.Pointer != nil {
		off = c.Pointer().Sub(image.Pt(int(ptr.X+.5), int(ptr.Y+.5)))
	}
	c.rep = Request{Key: key, Preview: card, Anchor: local.Add(off)}
	c.hasRep = true
}

// ReportURL says the pointer is on a message link whose hover opens
// UrlHoverCard this frame (Request.URL).
func (c *Controller) ReportURL(key any, url string, local image.Rectangle, ptr f32.Point) {
	if key == c.spent || url == "" {
		return
	}
	off := image.Point{}
	if c.Pointer != nil {
		off = c.Pointer().Sub(image.Pt(int(ptr.X+.5), int(ptr.Y+.5)))
	}
	c.rep = Request{Key: key, URL: url, Anchor: local.Add(off)}
	c.hasRep = true
}

// Seq counts the frames the controller ran (Frame calls). A trigger laid
// out with Seq more than one past the Seq it last saw missed a frame:
// Gio dropped its handler then, and with it any Leave (Probe.Missed).
func (c *Controller) Seq() int { return c.seq }

// OpenKey is the open card's trigger key, PendingKey the one waiting for
// its show delay (nil: none), so a trigger can tell Mounted it is still
// laid out.
func (c *Controller) OpenKey() any {
	if !c.isOpen {
		return nil
	}
	return c.open.Key
}

// PendingKey: see OpenKey.
func (c *Controller) PendingKey() any {
	if !c.hasPend {
		return nil
	}
	return c.pending.Key
}

// Mounted says key's trigger is laid out this frame. Once the open
// card's trigger has said so, a frame without it closes the card at once
// (Slack's card is the trigger's child: it unmounts with it).
func (c *Controller) Mounted(key any) {
	if key == nil {
		return
	}
	if c.isOpen && c.open.Key == key {
		c.mounted, c.tracks = true, true
	}
	if c.hasPend && c.pending.Key == key {
		c.pendTracks = true
	}
}

// ReportSide adds the trigger's Popover side (Request.Opposite) to its
// report this frame.
func (c *Controller) ReportSide(key any, opp bool) {
	if c.hasRep && c.rep.Key == key {
		c.rep.Opposite = opp
	}
}

// SetPlaced is the open card's side once placed (Keep.Opp).
func (c *Controller) SetPlaced(opp bool) {
	if c.isOpen {
		c.open.Opposite = opp
	}
}

// Placed returns the side key's open card was placed on, for its trigger
// to keep (ok: key's card is open).
func (c *Controller) Placed(key any) (opp, ok bool) {
	if key == nil || !c.isOpen || c.open.Key != key {
		return false, false
	}
	return c.open.Opposite, true
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
	c.mounted, c.tracks = false, false
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
	c.mounted, c.tracks = false, false
}

// Frame runs the timers once the frame's triggers have reported. blocked
// (a menu, modal or other popover is open) closes everything unless hold
// (the card's own menu is open) keeps the card as it is. It returns the
// next time Frame must run again (zero: none).
func (c *Controller) Frame(now time.Time, blocked, hold bool) time.Time {
	rep, has := c.rep, c.hasRep
	c.hasRep = false
	fol, hasFol := c.fol, c.hasFol
	c.hasFol = false
	c.seq++
	mounted := c.mounted
	c.mounted = false
	if hold && c.isOpen {
		c.hasPend = false
		c.hideAt = time.Time{}
		return time.Time{}
	}
	if blocked {
		c.Close()
		return time.Time{}
	}
	if c.isOpen && c.tracks && !mounted && !(has && rep.Key == c.open.Key) {
		c.closeCard() // its trigger unmounted (a recycled row, another tab or page)
	}
	if c.isOpen && c.open.Canvas {
		// Quip's positioners: the bubble follows its mention
		if has && rep.Key == c.open.Key {
			c.open.Anchor = rep.Anchor
		} else if hasFol {
			c.open.Anchor = fol
		}
	}
	onOpen := c.isOpen && (c.overCard || has && rep.Key == c.open.Key)
	switch {
	case onOpen:
		c.hideAt = time.Time{}
		c.hasPend = false
	case has:
		if !c.hasPend || c.pending.Key != rep.Key {
			c.pending, c.hasPend = rep, true
			c.pendTracks = false
			c.showAt = now.Add(ShowDelay)
			if rep.Delay > 0 {
				c.showAt = now.Add(rep.Delay)
			}
			if rep.Texty {
				c.showAt = now // a Popover from Texty's onMouseOver
			}
		} else {
			c.pending = rep // the same trigger: its bounds and what it names now
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
		c.mounted, c.tracks = false, c.pendTracks
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

// WheelLatch is how long a wheel gesture stays on the scroller it began
// on after its last event: Chromium's MouseWheelPhaseHandler ends a
// phaseless wheel's scroll sequence after kDefaultMouseWheelLatchingTransaction
// (500ms) without one, and until then sends every event of it, momentum
// included, to the scroller it latched at its first.
const WheelLatch = 500 * time.Millisecond

// CardArea registers the drawn card's bounds r (window coordinates, gtx at
// the window origin) so the pointer resting on the card keeps it open. It
// is pass-through: the card's own buttons sit under it.
//
// A canvas card takes a wheel gesture that begins on it: Quip's bubble is
// in the document's body, outside the canvas's scroller, so such a wheel
// scrolls nothing (and the canvas scrolling under the card would carry
// it off with its mention). A gesture that began elsewhere stays where
// Chromium latched it (WheelLatch): a canvas scrolled with the pointer on
// a mention keeps scrolling when the card, following the mention, slides
// under the resting pointer. A pass-through area over the window that
// takes none of the scroll sees each wheel event first, to tell.
func (c *Controller) CardArea(gtx layout.Context, r image.Rectangle) {
	f := pointer.Filter{Target: &c.cardTag, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel}
	canvas := c.isOpen && c.open.Canvas
	if canvas {
		for {
			e, ok := gtx.Event(pointer.Filter{Target: &c.wheelTag, Kinds: pointer.Scroll}) // no range: it takes none
			if !ok {
				break
			}
			if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Scroll {
				if gtx.Now.Sub(c.wheelAt) >= WheelLatch {
					c.wheelCard = image.Pt(int(pe.Position.X), int(pe.Position.Y)).In(c.card) // a new gesture
				}
				c.wheelAt = gtx.Now
			}
		}
		latched := gtx.Now.Sub(c.wheelAt) < WheelLatch
		if !latched || c.wheelCard {
			all := pointer.ScrollRange{Min: math.MinInt32, Max: math.MaxInt32}
			f.Kinds |= pointer.Scroll
			f.ScrollX, f.ScrollY = all, all
		} else {
			// a gesture on the canvas: the card takes the next one, from
			// when this one ends
			gtx.Execute(op.InvalidateCmd{At: c.wheelAt.Add(WheelLatch)})
		}
	}
	for {
		e, ok := gtx.Event(f)
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok {
			switch pe.Kind {
			case pointer.Enter, pointer.Move:
				c.overCard = true
			case pointer.Leave, pointer.Cancel:
				if c.overCard && c.isOpen && c.hideAt.IsZero() {
					// leaving the card starts the 250ms close (Frame ran
					// before this frame's card events, so it can't), and
					// asks for the frame that closes it
					c.hideAt = gtx.Now.Add(HideDelay)
					gtx.Execute(op.InvalidateCmd{At: c.hideAt})
				}
				c.overCard = false
			}
		}
	}
	c.card = r
	func() {
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &c.cardTag)
	}()
	if canvas {
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &c.wheelTag)
	}
}

// OverCard reports whether the pointer is on the drawn card.
func (c *Controller) OverCard() bool { return c.isOpen && c.overCard }

// Place is Slack's Popover.keepInBounds for position "top-left",
// offsetY -12, tryOppositePosition: the card's top-left corner for a card
// of size sz over anchor, in a window of size win. Distances are pixels:
// gap is OffsetY, ax and ay the allowances, already scaled.
func Place(anchor image.Rectangle, sz, win image.Point, gap, ax, ay int) image.Point {
	p, _ := keepInBounds(false, anchor, sz, win, gap, ax, ay, false)
	return p
}

// PlaceTop is keepInBounds for position "top" (a link preview's hover
// card): the card centered over the trigger, its bottom offsetY (gap)
// above the trigger's top; clamped into the window, and when it has to
// move vertically, "bottom" with the offset negated (gap below the
// trigger) if that fits entirely.
func PlaceTop(anchor image.Rectangle, sz, win image.Point, gap, ax, ay int) image.Point {
	p, _ := keepInBounds(true, anchor, sz, win, gap, ax, ay, false)
	return p
}

// Trigger is a profile trigger drawn by a widget: a pass-through area over
// the widget that reports while the pointer is on it. Lay it out after the
// widget, at the widget's origin.
type Trigger struct {
	tag   int
	in    bool
	pos   f32.Point
	probe Probe // where the pointer is on it, kept honest (anchor.go)
	opp   bool  // its Popover's side (Request.Opposite), while it stays mounted
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
	if t.probe.Missed() {
		// not laid out for a frame: Gio forgot the handler, and the Leave
		// it was owed (Slack's trigger unmounted and mounts unhovered,
		// with a new Popover)
		t.opp = false
		if t.in {
			t.in = false
			if c != nil {
				c.Left(t)
			}
		}
	}
	// the layout moved it under a resting pointer (the probe's Leave):
	// first, so the trigger's own newer events win (probe.Moved)
	if pos, ok := t.probe.Moved(gtx); ok && t.in {
		t.pos = pos
	}
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
	if c != nil {
		c.Mounted(t)
		if o, ok := c.Placed(t); ok {
			t.opp = o
		}
	}
	if t.in && c != nil && (userID != "" || botID != "") {
		c.ReportBot(t, userID, botID, image.Rectangle{Max: size}, t.pos)
		c.ReportSide(t, t.opp)
	}
	func() {
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &t.tag)
	}()
	if t.in {
		t.probe.Layout(gtx, t.pos)
	}
}

// Hovered reports whether the pointer is on the trigger.
func (t *Trigger) Hovered() bool { return t.in }

// Invalidate asks for a frame at next (zero: none).
func Invalidate(gtx layout.Context, next time.Time) {
	if !next.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: next})
	}
}
