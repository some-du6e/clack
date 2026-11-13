package huddleui

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A sidebar row with a huddle on: Slack's BaseChannelSuffixHuddleIcon
// (render PZNh) in the row's suffix, client-boot-styles' rules:
//
//   - HuddleAvatarStack (client-boot qgIY) with excludeIcon and
//     excludeMemberOverflowCount: the first two participants (maxMembers
//     defaults to 2; no "+N", no headphones), x_small 20px faces
//     (qSyV) with 25% corners (--huddles-radius), 8px after the name.
//     Every face after the first sits margin-left -2.235% over the one
//     before (a percentage of the stack's own width, so about 0.87px for
//     two) and is clipped by the stack's clipPath (--huddles-mask: the
//     concave notch on its left). Slack hides the stack only while your
//     huddle here is starting or ending (isHuddleTransitionState: STARTING,
//     PENDING, COMPLETING_PRE_JOIN, ENDING), not while you're in it. A face
//     that hasn't loaded is the base-ter tile (.c-base_icon).
//   - HuddlesBadgeFakeMask: a 2×20 sliver in the badge's colour whose
//     concave left edge meets the faces.
//   - HuddleSidebarLinkPeerCountBadge (lw/3): the headphones and the head
//     count (2px apart, 13px bold), min-width 36px, 20px tall, padding 2px
//     6px, corners 0 4px 4px 0, 4px before what follows; filled
//     theme-base-inv-hgl-1 with content-inv-pry ink, and on the selected
//     row surf-inv with theme-content-hgl-1 (your huddle) or base-pry with
//     content-pry (one you're not in).
//   - The stack (withTooltip) and the badge (lw/3's withTooltip, default
//     true) each show HuddleChannelTooltip without the channel's name
//     (below). The stack, the badge's count and the tooltip read
//     getActiveMembersByChannelId: your own huddle lists the others and
//     then you.

// sbBadge is the badge's colours by theme (Slack's default theme tables:
// the inverted sidebar scope in light mode, dark mode).
var sbBadge = [2]struct{ bg, ink, selBg, selInk, selOtherBg, selOtherInk color.NRGBA }{
	{bg: rgba(97, 31, 105, 1), ink: rgba(255, 255, 255, 1), selBg: rgba(255, 255, 255, .18), selInk: rgba(57, 6, 58, 1),
		selOtherBg: rgba(255, 255, 255, 1), selOtherInk: rgba(29, 28, 29, 1)},
	{bg: rgba(125, 57, 134, 1), ink: rgba(248, 248, 248, 1), selBg: rgba(255, 255, 255, .18), selInk: rgba(255, 255, 255, 1),
		selOtherBg: rgba(26, 29, 33, 1), selOtherInk: rgba(248, 248, 248, 1)},
}

// The stack's clipPath (qgIY, clipPathUnits objectBoundingBox) and the
// fake mask's path (PZNh HuddlesBadgeFakeMask, in px of its 2×20 box).
const (
	sbFaceMask = "M0.03,1 h0.742 a0.258,0.2,0,0,0,0.258,-0.2 V0.2 a0.258,0.2,0,0,0,-0.258,-0.2 H0.03 a0.385,0.299,0,0,1,0.098,0.2 v0.6 A0.385,0.299,0,0,1,0.03,1 Z"
	sbFakeMask = "M2.98805e-07 20H2.52782V0H0C0.95008 1.06151 1.52779 2.46329 1.52779 4V16C1.52779 17.5367 0.95008 18.9385 2.98805e-07 20Z"
	// sbFaceOffset is --huddles-offset, a fraction of the stack's width.
	sbFaceOffset = 0.02235
	sbMaxFaces   = 2 // HuddleAvatarStackContent's maxMembers default
)

// transitioning is Slack's hide rule for the stack: your huddle is in ch
// and starting or ending (isHuddleTransitionState of the optimistic huddle
// state). Joining before the session exists is STARTING/PENDING; a session
// still connecting is COMPLETING_PRE_JOIN; one that ended is ENDING.
func (c *Controller) transitioning(ch string) bool {
	c.mu.Lock()
	joining, s := c.joining, c.s
	c.mu.Unlock()
	if joining == ch {
		return true
	}
	if s == nil || s.Channel() != ch {
		return false
	}
	switch s.State().Phase {
	case huddle.Connecting, huddle.Ended:
		return true
	}
	return false
}

// sbFaceSteps is where each face starts (px from the stack's left) for n
// faces of s px: margin-left -2.235% resolves against the stack's final
// width, W = n·s − (n−1)·0.02235·W.
func sbFaceSteps(n, s int) []int {
	if n <= 0 {
		return nil
	}
	w := float64(n*s) / (1 + float64(n-1)*sbFaceOffset)
	step := float64(s) - sbFaceOffset*w
	out := make([]int, n)
	for i := range out {
		out[i] = int(float64(i)*step + .5)
	}
	return out
}

// SidebarSuffix draws a conversation row's huddle suffix ending at right
// (row coordinates, rowH tall) when a huddle is on in ch, and returns the
// new right edge (right itself when there's none). sel is the selected
// row. The faces and the badge each carry HuddleChannelTooltip. Main
// window goroutine.
func (c *Controller) SidebarSuffix(gtx C, th *theme.Theme, ch string, right, rowH int, sel bool) int {
	// BaseChannelSuffixHuddleIcon: myHuddleChannelId is your huddle's
	// channel (isMessagingClient), the stack withTooltip, the badge's
	// tooltip on (lw/3's default), huddleFakeMaskSvg 2×20
	return c.Badge(gtx, th, ch, right, rowH, BadgeLook{Selected: sel, Mine: c.In(ch), MaskH: 20, Tips: true})
}

// BadgeLook is where a huddle badge sits and how that place draws it.
type BadgeLook struct {
	// Selected: on the selected sidebar row (.p-channel_sidebar__channel
	// --selected recolours the badge and the fake mask).
	Selected bool
	// Mine: the badge without --other, myHuddleChannelId being this
	// channel (the sidebar passes your huddle's channel; the channel hover
	// card passes none, so its badge is always --other).
	Mine bool
	// MaskH is the fake mask svg's height: 20 (the sidebar's
	// huddleFakeMaskSvg) or 19 (p-search_autocomplete__huddle_fake_mask_svg,
	// the channel card's); its 20px path is cut off below that.
	MaskH float32
	// Tips: the stack and the badge show HuddleChannelTooltip (the
	// sidebar's withTooltip; the card turns both off).
	Tips bool
}

// Badge draws the huddle badge Slack's sidebar row suffix and channel
// hover card share when a huddle is on in ch, ending at right (current
// coordinates, rowH tall): HuddleAvatarStack (excludeIcon,
// excludeMemberOverflowCount: two x_small faces, margin-left 8px), the
// 2px fake mask, then HuddleSidebarLinkPeerCountBadge. The badge's
// margin-left -1px and left 1px (relative) paint it flush with the mask
// while its flow box overlaps the mask by 1px, and its margin-right 4px
// ends the run: the painted badge ends 3px before right. It returns the
// left edge (the stack's margin included), right itself when there's no
// huddle. Main window goroutine.
func (c *Controller) Badge(gtx C, th *theme.Theme, ch string, right, rowH int, look BadgeLook) int {
	people := c.activeMembers(ch)
	if _, live := c.Live(ch); !live && !c.In(ch) || len(people) == 0 {
		return right
	}
	k := sbBadge[mode(th)]
	bg, ink := k.bg, k.ink
	theme.SetColor(th.Tokens, &bg, "var(--dt_color-theme-base-inv-hgl-1)")
	theme.SetColor(th.Tokens, &ink, "var(--dt_color-content-inv-pry)")
	if look.Selected {
		bg, ink = k.selOtherBg, k.selOtherInk
		if look.Mine {
			bg, ink = k.selBg, k.selInk
		}
	}
	hs := c.hdrState(ch)
	num := strconv.Itoa(len(people))
	nw := th.Measure(gtx, 13, theme.Bold, num)
	is := gtx.Dp(16)
	h := gtx.Dp(20)
	w := max(gtx.Dp(36), gtx.Dp(6)+is+gtx.Dp(2)+nw+gtx.Dp(6))
	// flow: margin-right 4px, the box w wide with margin-left -1px; paint:
	// left 1px
	x1 := right - gtx.Dp(4) + gtx.Dp(1)
	x0 := x1 - w
	y0 := (rowH - h) / 2
	r := image.Rect(x0, y0, x1, y0+h)
	segFill(gtx, r, segCaret, gtx.Dp(4), bg) // corners 0 4px 4px 0
	cx := r.Min.X + (w-(is+gtx.Dp(2)+nw))/2
	at(gtx, image.Pt(cx, y0+(h-is)/2), image.Pt(is, is), func(gtx C) D { return icons.Get("headphones")(gtx, 16, ink) })
	at(gtx, image.Pt(cx+is+gtx.Dp(2), y0), image.Pt(nw, h), func(gtx C) D {
		m := labelSize(gtx, th, 13, theme.Bold, num)
		at(gtx, image.Pt(0, (h-m.Y)/2), m, func(gtx C) D { return th.Label(gtx, 13, theme.Bold, ink, num) })
		return D{Size: image.Pt(nw, h)}
	})
	if look.Tips {
		c.sbTipArea(gtx, th, &hs.sbBadge, r, ch, false) // lw/3's withTooltip
	} else {
		hs.sbBadge = sbTip{}
	}
	right = x0
	// the fake mask: its path clipped to its 2×MaskH box, at the top of
	// the run (the flex row stretches; the svg has its own height)
	mw := gtx.Dp(2)
	mh := gtx.Dp(unit.Dp(look.MaskH))
	func() {
		defer op.Offset(image.Pt(right-mw, y0)).Push(gtx.Ops).Pop()
		defer clip.Rect{Max: image.Pt(mw, mh)}.Push(gtx.Ops).Pop()
		sc := float32(gtx.Dp(20)) / 20
		defer clip.Outline{Path: icons.ClipPath(gtx, sbFakeMask, sc)}.Op().Push(gtx.Ops).Pop()
		theme.Fill(gtx, image.Rect(0, 0, mw, mh), bg)
	}()
	right -= mw
	if c.transitioning(ch) {
		hs.sbFaces = sbTip{} // the stack is gone, and its tooltip with it
		return right
	}
	t := &hs.sbFaces
	if !look.Tips {
		*t = sbTip{}
		t = nil
	}
	return c.sbStack(gtx, th, t, ch, people, right, rowH, false) - gtx.Dp(8) // margin-left 8px
}

// CardBadge is ChannelHoverCardHuddleBadge (boot-shared 7+JF) ending at
// right in a row rowH tall: when a huddle is on in ch (isMessagingClient
// and getChannelHasActiveHuddle), Badge without tooltips, never
// selected, always --other, with the 2×19 fake mask. It returns the left
// edge, right when there's no huddle.
func (c *Controller) CardBadge(gtx C, th *theme.Theme, ch string, right, rowH int) int {
	return c.Badge(gtx, th, ch, right, rowH, BadgeLook{MaskH: 19})
}

// sbStack draws HuddleAvatarStackContent (excludeIcon,
// excludeMemberOverflowCount, x_small faces) for ch ending at right with
// its tooltip (withTooltip, nil t: none; includeChannelNameInTooltip is
// withChannel), and returns its left edge.
func (c *Controller) sbStack(gtx C, th *theme.Theme, t *sbTip, ch string, people []string, right, rowH int, withChannel bool) int {
	fs := gtx.Dp(hdrFace)
	n := min(len(people), sbMaxFaces)
	steps := sbFaceSteps(n, fs)
	fw := steps[n-1] + fs
	fx := right - fw
	for i, u := range people[:n] {
		p := image.Pt(fx+steps[i], (rowH-fs)/2)
		c.sbFace(gtx, th, u, p, fs, i > 0)
	}
	if t != nil { // withTooltip
		c.sbTipArea(gtx, th, t, image.Rect(fx, (rowH-fs)/2, right, (rowH+fs)/2), ch, withChannel)
	}
	return fx
}

// sbFace draws one face of the stack at p (s px), clipped by the stack's
// mask when it isn't the first.
func (c *Controller) sbFace(gtx C, th *theme.Theme, id string, p image.Point, s int, masked bool) {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	if masked {
		defer clip.Outline{Path: icons.ClipPath(gtx, sbFaceMask, float32(s))}.Op().Push(gtx.Ops).Pop()
	}
	sz := image.Pt(s, s)
	rad := s / 4 // --huddles-radius
	if url := c.avatarURL(id, s); url != "" && c.h.Images() != nil {
		if img, ok := c.h.Images().Get(url, s); ok {
			theme.Image(gtx, img, sz, rad)
			return
		}
	}
	theme.RRect(gtx, image.Rectangle{Max: sz}, rad, th.C.Skeleton) // .c-base_icon: base-ter
}

// activeMembers is getActiveMembersByChannelId (client-boot b0mi ft / A7
// with excludeSelf false) for the stack, the badge's count and the
// tooltip: in your own huddle the connected peers and then you (the
// client store's peers, then the viewer pushed last), elsewhere the room's
// participants in its order.
func (c *Controller) activeMembers(ch string) []string {
	s := c.Session()
	if s == nil || s.Channel() != ch {
		return c.headerPeople(ch)
	}
	st := s.State()
	if st.Phase == huddle.Ended {
		return c.headerPeople(ch)
	}
	self := c.selfID()
	var out []string
	for _, p := range st.Participants {
		if !p.Self && p.UserID != self && !contains(out, p.UserID) {
			out = append(out, p.UserID)
		}
	}
	return append(out, self)
}

// ---- HuddleChannelTooltip ----
//
// Every trigger is Slack's Tooltip (F2XN: position top, delay 150) around
// HuddleChannelTooltip (client-boot Soit), modern.vendor's rules:
//
//   - .c-tooltip__tip: base-inv-pry, max-width 200px, padding 8px 12px,
//     13px bold, line-height 1.25, centred; .p-huddle_channel_tooltip
//     inside it pads another 8px.
//   - HuddleTooltipAvatarStack (MemberTooltipAvatarStack): the first five
//     members (A7), medium 36px avatars in inline-block boxes 28px wide
//     (medium - 8; the last one auto), each with border-radius 8px and a
//     2px #1d1c1d ring (box-shadow 0 0 0 2px), margin 4px above and
//     below. With more than five the fifth carries c-avatar__additional
//     _overlay: "+N" (N = count - 4) or "9+", 15px bold white on #00000080.
//   - the action text (margin_bottom_25: 4px under it, empty or not): the
//     huddle's topic (U4Fz: the optimistic topic, else the call's name),
//     or "Only {maxParticipants} people can join a huddle" once the count
//     reaches getParticipantLimit (zq6z df: 250 with
//     increase_huddles_max_participants, else 50).
//   - the member text: "Just you in the huddle", "{name} is in the
//     huddle", or "{memberCount} people in the huddle", each with
//     " #{channelName}" when the trigger asks for the channel
//     (includeChannelNameInTooltip: SectionHuddleIndicator, never a row's
//     own suffix) and the conversation is neither a DM nor a group DM.

// sbTip is one HuddleChannelTooltip trigger's hover and timing.
type sbTip struct {
	hover gesture.Hover
	tip   popups.TipState
}

// sbTipDelay is F2XN Tooltip's default delay.
const sbTipDelay = 150 * time.Millisecond

// sbTipArea makes r (current coordinates) t's hover target, passing
// pointer events through to the row under it, and shows HuddleChannelTooltip
// for ch above it once hovered.
func (c *Controller) sbTipArea(gtx C, th *theme.Theme, t *sbTip, r image.Rectangle, ch string, withChannel bool) {
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	func() {
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		defer clip.Rect{Max: r.Size()}.Push(gtx.Ops).Pop()
		t.hover.Add(gtx.Ops)
	}()
	t.tip.Delay = sbTipDelay
	popups.TooltipBox(gtx, th, &t.tip, t.hover.Update(gtx.Source), r.Size(), popups.Above, func(gtx C) D {
		return c.channelTip(gtx, th, ch, withChannel)
	})
}

// Huddle participant limits (zq6z getParticipantLimit).
const (
	sbLimit         = 50
	sbLimitIncrease = 250
)

// participantLimit is getParticipantLimit.
func (c *Controller) participantLimit() int {
	if c.on(ExpMaxParticipants) {
		return sbLimitIncrease
	}
	return sbLimit
}

// topicFor is U4Fz: your huddle's topic (Clack applies a rename to the
// session at once, Slack's optimistic topic), else the call's name.
func (c *Controller) topicFor(ch string) string {
	if s := c.Session(); s != nil && s.Channel() == ch {
		return s.State().Name
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live[ch].Name
}

// tipText is HuddleChannelTooltip's action and member lines for ch.
func (c *Controller) tipText(ch string, withChannel bool) (action, member string) {
	members := c.activeMembers(ch) // ft
	n := len(members)
	if t := c.topicFor(ch); strings.TrimSpace(t) != "" {
		action = t // FormattedHuddleTopic
	}
	if n >= c.participantLimit() {
		action = nsHuddles.T("Only {maxParticipants} people can join a huddle", "maxParticipants", c.participantLimit())
	}
	var chName string
	if app := c.h.Core(); withChannel && app != nil {
		if conv, ok := app.Conv(ch); ok && conv.Kind != "im" && conv.Kind != "mpim" { // !isHuddleChannel && !JM
			chName = conv.Name // deprecatedUnsafeGetDisplayName: no prefix
		}
	}
	withChannel = chName != ""
	self := c.selfID()
	switch {
	case n == 1 && members[0] == self && withChannel:
		member = nsHuddles.T("Just you in the huddle #{channelName}", "channelName", chName)
	case n == 1 && withChannel:
		member = nsHuddles.T("{name} is in the huddle #{channelName}", "name", c.userName(members[0]), "channelName", chName)
	case withChannel:
		member = nsHuddles.T("{memberCount} people in the huddle #{channelName}", "memberCount", n, "channelName", chName)
	case n == 1 && members[0] == self:
		member = nsHuddles.T("Just you in the huddle")
	case n == 1:
		member = nsHuddles.T("{name} is in the huddle", "name", c.userName(members[0]))
	default:
		member = nsHuddles.T("{memberCount} people in the huddle", "memberCount", n)
	}
	return action, member
}

// Tooltip geometry (px at 1x).
const (
	tipMaxW    = 200 // .c-tooltip__tip max-width (border-box)
	tipPadX    = 12 + 8
	tipPadY    = 8 + 8
	tipFace    = 36 // medium
	tipFaceBox = tipFace - 8
	tipFaceRad = 8
	tipRing    = 2
	tipMaxFace = 5
)

// tipRingColor is the avatars' box-shadow: #1d1c1d in both modes.
var tipRingColor = rgba(0x1d, 0x1c, 0x1d, 1)

// channelTip draws HuddleChannelTooltip's body at the origin (its
// padding included) and returns the tip's size.
func (c *Controller) channelTip(gtx C, th *theme.Theme, ch string, withChannel bool) D {
	members := c.activeMembers(ch)
	action, member := c.tipText(ch, withChannel)
	ink := th.C.TooltipText
	padX, padY := gtx.Dp(tipPadX), gtx.Dp(tipPadY)
	textW := gtx.Dp(tipMaxW) - 2*padX
	para := func(txt string) (op.CallOp, D) {
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(textW, 1<<20)}
		d := th.Layout(g, theme.TextStyle{Size: 13, Weight: theme.Bold, Color: ink, LineHeight: 13 * 1.25, Align: text.Middle}, txt)
		return m.Stop(), d
	}
	var actCall, memCall op.CallOp
	var actD, memD D
	if action != "" {
		actCall, actD = para(action)
	}
	memCall, memD = para(member)
	shown := members[:min(len(members), tipMaxFace)]
	fs, fb := gtx.Dp(tipFace), gtx.Dp(tipFaceBox)
	stackW := 0
	if len(shown) > 0 {
		stackW = (len(shown)-1)*fb + fs
	}
	w := max(stackW, actD.Size.X, memD.Size.X)
	y := padY
	stackH := 0
	if len(shown) > 0 {
		stackH = gtx.Dp(4) + fs + gtx.Dp(4)
	}
	h := padY + stackH + actD.Size.Y + gtx.Dp(4) + memD.Size.Y + padY
	sz := image.Pt(w+2*padX, h)
	if len(shown) > 0 {
		x0 := (sz.X - stackW) / 2
		top := y + gtx.Dp(4)
		ring, rad := gtx.Dp(tipRing), gtx.Dp(tipFaceRad)
		for i, u := range shown {
			p := image.Pt(x0+i*fb, top)
			theme.RRect(gtx, image.Rectangle{Min: p.Sub(image.Pt(ring, ring)), Max: p.Add(image.Pt(fs+ring, fs+ring))}, rad+ring, tipRingColor)
			c.tipFace(gtx, th, u, p, fs, rad)
			if i == len(shown)-1 && len(members) > tipMaxFace {
				more := len(members) - len(shown) + 1
				label := "+" + strconv.Itoa(more)
				if more > 9 {
					label = "9+"
				}
				r := image.Rectangle{Min: p, Max: p.Add(image.Pt(fs, fs))}
				theme.RRect(gtx, r, rad, rgba(0, 0, 0, .5))
				at(gtx, p, image.Pt(fs, fs), func(gtx C) D {
					centerLabel(gtx, th, image.Pt(fs, fs), 15, theme.Bold, rgba(255, 255, 255, 1), label)
					return D{Size: image.Pt(fs, fs)}
				})
			}
		}
		y += stackH
	}
	if action != "" {
		at(gtx, image.Pt((sz.X-actD.Size.X)/2, y), actD.Size, func(gtx C) D { actCall.Add(gtx.Ops); return actD })
	}
	y += actD.Size.Y + gtx.Dp(4) // margin_bottom_25
	at(gtx, image.Pt((sz.X-memD.Size.X)/2, y), memD.Size, func(gtx C) D { memCall.Add(gtx.Ops); return memD })
	return D{Size: sz}
}

// tipFace draws one of the tooltip's avatars at p (s px, rad corners); one
// that hasn't loaded is the base-ter tile.
func (c *Controller) tipFace(gtx C, th *theme.Theme, id string, p image.Point, s, rad int) {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	sz := image.Pt(s, s)
	if url := c.avatarURL(id, s); url != "" && c.h.Images() != nil {
		if img, ok := c.h.Images().Get(url, s); ok {
			theme.Image(gtx, img, sz, rad)
			return
		}
	}
	theme.RRect(gtx, image.Rectangle{Max: sz}, rad, th.C.Skeleton)
}

// ---- SectionHuddleIndicator ----
//
// A collapsed section's heading (render SectionMenuNewInternal, under
// huddle_section_indicator: pSxm I6) ends its buttonsContainer (flex,
// gap 4px, margin-left auto, after the ⋯ button) with
// SectionHuddleIndicator (render 2LNF) for the section's
// channelsAssociatedOrdered: HuddleAvatarStackContent (the row suffix's
// stack: excludeIcon, excludeMemberOverflowCount, two x_small faces, the
// crescent mask) with class huddleAvatarStack (margin-left 0),
// withTooltip and includeChannelNameInTooltip. Which huddle it shows is
// getChannelIdWithActiveHuddle (b0mi nf) over the section's channels that
// have one (jg: an active huddle with participants, yours read from your
// session):
//
//  1. the channel of the huddle inviting you, if it's in the section
//     (getActiveInviteForCurrentTeam);
//  2. otherwise only channels you're a member of (JUrK isMemberOfChannel),
//     not muted (pTqQ), not your self-DM (N9WL), not a DM with Slackbot
//     or a bot (DUy0), that you can post in (o/IU canUserPostInChannel;
//     is_huddle_channel, which Clack doesn't read, would also do), not a
//     preview, archived, frozen or pending Slack Connect channel, and not
//     a DM with an invited user (Opi6 dq, which Clack can't tell);
//  3. of those, the one with your huddle, else the earliest started
//     (date_start; the first in section order on a tie).
//
// The stack hides, as on a row, while your huddle there is starting or
// ending. Its own click does nothing: it isn't inside toggleAndLabel, so
// the heading doesn't collapse or expand.

const (
	// ExpSectionIndicator is pSxm I6.
	ExpSectionIndicator = "huddle_section_indicator"
	// ExpMaxParticipants is pSxm nC (getParticipantLimit 250, else 50).
	ExpMaxParticipants = "increase_huddles_max_participants"
)

// SectionHuddle is getChannelIdWithActiveHuddle over ids (a section's
// channelsAssociatedOrdered): the channel whose huddle a collapsed heading
// shows, "" for none. "" too while huddle_section_indicator is off.
func (c *Controller) SectionHuddle(ids []string) string {
	if !c.on(ExpSectionIndicator) {
		return ""
	}
	var have []string // jg, in the section's order
	for _, ch := range ids {
		if _, live := c.Live(ch); (live || c.In(ch)) && len(c.activeMembers(ch)) > 0 {
			have = append(have, ch)
		}
	}
	if len(have) == 0 {
		return ""
	}
	app := c.h.Core()
	c.mu.Lock()
	inv := c.inv
	c.mu.Unlock()
	if inv != nil && contains(have, inv.Channel) && (app == nil || inv.Team == "" || app.S.TeamID == "" || inv.Team == app.S.TeamID) {
		return inv.Channel
	}
	var ok []string
	for _, ch := range have {
		if c.sectionEligible(ch) {
			ok = append(ok, ch)
		}
	}
	if len(ok) == 0 {
		return ""
	}
	if s := c.Session(); s != nil && contains(ok, s.Channel()) {
		return s.Channel()
	}
	best, start := ok[0], c.startOf(ok[0])
	for _, ch := range ok[1:] {
		if t := c.startOf(ch); t > 0 && (start <= 0 || t < start) {
			best, start = ch, t
		}
	}
	return best
}

// startOf is the huddle's date_start in ch (0 when unknown).
func (c *Controller) startOf(ch string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live[ch].Start
}

// sectionEligible is getChannelIdWithActiveHuddle's filter for ch.
func (c *Controller) sectionEligible(ch string) bool {
	app := c.h.Core()
	if app == nil {
		return false
	}
	conv, ok := app.Conv(ch)
	raw, rok := app.S.Conv(ch)
	if !ok || !rok {
		return false // isExistentObject
	}
	im, mpim := conv.Kind == "im", conv.Kind == "mpim"
	if !(im || mpim || raw.IsMember) || conv.Preview { // isMemberOfChannel, isPreview
		return false
	}
	if conv.Muted || conv.IsArchived || raw.IsFrozen || raw.IsPendingExtShared && im {
		return false
	}
	if im {
		if conv.User == c.selfID() || conv.User == "USLACKBOT" || conv.User == "USLACK" {
			return false // N9WL, isChannelSlackbotIm
		}
		if u, ok := c.user(conv.User); ok && u.IsBot {
			return false // isBotUser
		}
	}
	return app.PostingFor(raw).CanPost() // canUserPostInChannel (not archived, not frozen, not read-only)
}

// SectionIndicatorWidth is the room ch's SectionHuddleIndicator takes in a
// heading (0 when it's hidden), its 4px gap to the ⋯ button not included.
func (c *Controller) SectionIndicatorWidth(gtx C, ch string) int {
	if ch == "" || c.transitioning(ch) {
		return 0
	}
	n := min(len(c.activeMembers(ch)), sbMaxFaces)
	if n == 0 {
		return 0
	}
	fs := gtx.Dp(hdrFace)
	return sbFaceSteps(n, fs)[n-1] + fs
}

// SectionIndicator draws a collapsed heading's SectionHuddleIndicator for
// ch ending at right (row coordinates, rowH tall), with its tooltip, and
// returns its left edge (right when there's none). key is the section.
func (c *Controller) SectionIndicator(gtx C, th *theme.Theme, key, ch string, right, rowH int) int {
	t := &c.hdrState("\x00section:" + key).sbFaces
	if c.SectionIndicatorWidth(gtx, ch) == 0 {
		*t = sbTip{}
		return right
	}
	return c.sbStack(gtx, th, t, ch, c.activeMembers(ch), right, rowH, true)
}

// SectionIndicatorHovered reports whether the pointer is over section
// key's indicator (the heading then skips the click: the stack sits
// outside SectionMenuNew's toggleAndLabel).
func (c *Controller) SectionIndicatorHovered(gtx C, key string) bool {
	hs := c.hdr["\x00section:"+key]
	return hs != nil && hs.sbFaces.hover.Update(gtx.Source)
}
