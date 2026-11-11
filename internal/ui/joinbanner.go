package ui

// The channel join banner over the composer: Slack's ChannelJoinBanner
// (chunk megaphone-message-pane-input-channel-join-banner 7BTH, its
// buttons in the shared …message-pane-input-channel-join-banner_channel
// chunk mzyA / QwCw, CSS megaphone-message-pane-input-channel-join-banner,
// CDN build bv1-13).
//
//   - When (Slack's megaphone flow, core/megaphone.go): the message list's
//     megaphone container exists only while message_input holds one of
//     its notifications (GyIg G5), the channel's own (channelSpaces) or,
//     with none of them for the channel, the global one the boot load and
//     the RTM pushes keep (bU1n isGloballyScoped/
//     isChannelScopedMegaphoneNotificationVisible); MegaphoneContainer
//     (J3pY) draws ChannelJoinBanner when that notification is
//     education_channel_join_banner. Only then does the banner ask
//     megaphone.notifications.list for the channel's message_input spaces
//     (its REQUEST_SPACES on mount), and it shows while the channel's
//     message_input notification is education_channel_join_banner. So
//     Clack no longer asks for every channel it opens: a channel the
//     server never put the banner in the boot load or a push for gets
//     none. It never shows for a preview (isPreview), an agent record
//     channel or a meeting channel, and the pref hides it: with
//     isAutoSortRolloutActive (suggested_sections_auto_sort_v2 on, its
//     killswitch off) disable_channel_join_banner true, otherwise unless
//     channel_join_experience_enabled is true.
//   - What (the default variant; the auto-sort and Slackbot AI variants
//     need server state and AI features Clack doesn't have): the card
//     (1px otl-ter, base-sec, radius 12, margin 8 20 0, padding 20), a
//     close button at top 20 right 40, ":wave: Welcome to #name" (15px
//     900), "Group this channel with similar conversations in your
//     sidebar and choose when you want to be notified about new
//     messages." (13px/18px) and the two outline buttons 4px apart
//     (jbButton): the section selector ("★ Move to Section" for a channel
//     in no section, else the section's emoji or icon and name, with a
//     chevron) opening the Move to… menu, and the notifications control
//     (its level's icon and "All new posts" / "Just mentions" / "Nothing"
//     / "Muted", a caret) opening ChannelNotificationPrefMenu above.
//   - Dismissal: the close button or Delete on the banner slides it down
//     (.3s: opacity 0, translateY(100%)) and records
//     megaphone.setNotificationAsSeen dismiss; the slid-away banner keeps
//     its place until the channel is left (the transform doesn't change
//     layout). The container doesn't pass ChannelJoinBanner an onClose, so
//     its useHasSectionOrNotificationChanged close does nothing. Leaving a
//     channel whose message_input holds the banner records an impression
//     and asks the space again (maybeHideChannelJoinBanner): the server
//     remembers the dismissal and answers null from then on.

import (
	"context"
	"image"
	"image/color"
	"slices"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's banner copy (custom_emoji_list namespace).
const (
	jbBody  = "Group this channel with similar conversations in your sidebar and choose when you want to be notified about new messages."
	jbMove  = "Move to Section"
	jbSlide = 300 * time.Millisecond
)

// clsJoinBanner is the banner's wrapper (CSS module classes from
// client-boot-styles: channelJoinBannerOuterWrapper, animateSlideDown
// while it closes).
var clsJoinBanner = slackc.Class("channelJoinBannerOuterWrapper__TpBQA").Mods("animateSlideDown__Bj5IF")

// jbSpaces are the spaces ChannelJoinBanner requests on mount.
var jbSpaces = []string{core.SpaceMessageInput, "message_input_invited_glow", "message_input_channel_join_summary_glow"}

type joinBanner struct {
	mounted   bool // the megaphone container holds it (its REQUEST_SPACES went out)
	closing   time.Time
	gone      bool // slid away: invisible, still taking its height (h)
	h         int
	closeBtn  widget.Clickable
	moveBtn   widget.Clickable
	notifyBtn widget.Clickable
	moveR     image.Rectangle // window coordinates, for the menus
	notifyR   image.Rectangle
}

// joinBannerHidden is the pref gate E.
func joinBannerHidden(c *core.App) bool {
	if c.Experiment("suggested_sections_auto_sort_v2") == "on" && c.Experiment("suggested_sections_auto_sort_killswitch") != "on" {
		return c.PrefBool("disable_channel_join_banner", false)
	}
	return !c.PrefBool("channel_join_experience_enabled", false)
}

// joinBannerEligible: a channel the viewer is in, not a DM, an agent
// record channel ($n) or a meeting channel (Dh).
func joinBannerEligible(conv *convInfo, cv core.Conv) bool {
	if conv == nil || conv.Preview || conv.Archived {
		return false
	}
	if conv.Kind != "channel" && conv.Kind != "private" {
		return false
	}
	return cv.Code == nil && !strings.HasPrefix(cv.Name, "mc:")
}

// joinBannerProTrial is the one G5 notification the global space only
// holds the container with five unread messages or more
// (isGloballyScopedMegaphoneNotificationVisible).
const joinBannerProTrial = "pro_trial_slack_ai_offer_in_channel_summary_banner"

// joinBannerMounted is the message list's megaphone container for ch
// holding ChannelJoinBanner (bU1n, J3pY): the global message_input
// notification is one of the container's (with the pro-trial banner, only
// at five unread or more), else the channel's is; whichever it is must be
// education_channel_join_banner. unread is getUnreadBannerDisplayCount.
func joinBannerMounted(c *core.App, ch string, unread func() int) bool {
	inG5 := func(n string) bool { return n != "" && slices.Contains(core.MegaphoneMessageInputNotifications, n) }
	global := c.MegaphoneNotification(core.SpaceMessageInput, "")
	if inG5(global) && (global != joinBannerProTrial || unread() >= 5) {
		return global == core.NotificationChannelJoinBanner
	}
	scoped := c.MegaphoneNotification(core.SpaceMessageInput, ch)
	return inG5(scoped) && scoped == core.NotificationChannelJoinBanner
}

// joinBannerUnread is getUnreadBannerDisplayCount for ch: the loaded
// messages after its read cursor.
func joinBannerUnread(c *core.App, ch string) int {
	lr := c.Count(ch).LastRead
	tl := c.S.Timeline(ch)
	if tl == nil || lr == "" {
		return 0
	}
	n := 0
	for i := range tl.Msgs {
		if tl.Msgs[i].TS > lr {
			n++
		}
	}
	return n
}

// layoutJoinBanner draws c's banner (when Slack's megaphone says so) and
// returns its size; origin is its window position.
func (a *App) layoutJoinBanner(gtx layout.Context, c *convo, origin image.Point) layout.Dimensions {
	ca := a.Core()
	if c.ts != "" || ca == nil {
		return layout.Dimensions{}
	}
	conv := a.model().conv(c.ch)
	cv, _ := ca.Conv(c.ch)
	if !joinBannerEligible(conv, cv) {
		return layout.Dimensions{}
	}
	a.megaphoneBoot(c.ch)
	if a.joinBannerCh != c.ch { // a channel view opened: channelOpenedEvent
		a.joinBannerCh = c.ch
		ch := c.ch
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = ca.MegaphoneChannelOpened(ctx, ch)
		}()
	}
	b := c.banner
	if b == nil {
		b = &joinBanner{}
		c.banner = b
	}
	if !joinBannerMounted(ca, c.ch, func() int { return joinBannerUnread(ca, c.ch) }) {
		b.mounted = false
		return layout.Dimensions{}
	}
	if !b.mounted { // mounted: a fresh ChannelJoinBanner asks for the channel's spaces
		*b = joinBanner{mounted: true}
		a.askJoinBanner(c.ch)
	}
	show := strings.Contains(ca.MegaphoneNotification(core.SpaceMessageInput, c.ch), core.NotificationChannelJoinBanner)
	if !show || joinBannerHidden(ca) {
		return layout.Dimensions{}
	}
	if b.gone {
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, b.h)}
	}
	if b.closeBtn.Clicked(gtx) {
		a.dismissJoinBanner(c.ch, b)
	}
	if b.moveBtn.Clicked(gtx) {
		st := a.channelSections(c.ch)
		a.menuAt(b.moveR, int(popups.Above), 280, a.moveToItems(c.ch, st))
	}
	if b.notifyBtn.Clicked(gtx) {
		a.ShowOverlay(popups.NewMenuOpts(a, popups.ChannelNotifyItems(a, c.ch), popups.MenuOpts{Anchor: b.notifyR, Side: popups.Above, Width: 300}))
	}
	// Delete on the focused banner dismisses it; a click focuses it (the
	// wrapper's tabIndex 0)
	for {
		ev, ok := gtx.Event(key.FocusFilter{Target: b}, key.Filter{Focus: b, Name: key.NameDeleteForward})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			a.dismissJoinBanner(c.ch, b)
		}
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: b, Kinds: pointer.Press})
		if !ok {
			break
		}
		if _, ok := ev.(pointer.Event); ok {
			gtx.Execute(key.FocusCmd{Tag: b})
		}
	}

	th := a.th
	tc := &th.C
	w := gtx.Constraints.Max.X
	// the card: margin 8 20 0, a 1px border, padding 20 inside it
	mx, mt, bw := gtx.Dp(20), gtx.Dp(8), max(1, gtx.Dp(1))
	pad := gtx.Dp(20) + bw
	inner := max(0, w-2*mx-2*pad)
	rec := op.Record(gtx.Ops)
	y := 0
	// title: :wave: Welcome to #name (margin-right 48)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(0, inner-gtx.Dp(48)), gtx.Dp(200))}
	td := a.jbTitle(g, c.ch, conv)
	y += td.Size.Y + gtx.Dp(2)
	func() {
		defer op.Offset(image.Pt(0, y)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(inner, gtx.Dp(400))}
		// channelJoinSectionLabelWrapper's line-height 18px holds the
		// 15px strut and the 13px span (its margin-bottom "12px 0" is
		// invalid, so none): a line box of 15px's ascent over 13px's
		// half-leading and descent
		a15, d15 := a.jbMetrics(gtx, 15, theme.Regular)
		a13, d13 := a.jbMetrics(gtx, 13, theme.Regular)
		l18 := float32(gtx.Dp(18))
		hl15, hl13 := (l18-float32(a15+d15))/2, (l18-float32(a13+d13))/2
		lh := (max(float32(a15)+hl15, float32(a13)+hl13) + max(float32(d15)+hl15, float32(d13)+hl13)) / gtx.Metric.PxPerSp
		d := th.Layout(g, theme.TextStyle{Size: 13, LineHeight: unit.Sp(lh), Color: tc.Fg}, jbBody)
		y += d.Size.Y
	}()
	y += gtx.Dp(8)
	bh := 0
	func() {
		defer op.Offset(image.Pt(0, y)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(inner, gtx.Dp(28))}
		bx := origin.Add(image.Pt(mx+pad, mt+pad+y))
		d := layout.Flex{Alignment: layout.Middle}.Layout(g,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				d := a.jbSectionButton(gtx, c.ch, b)
				b.moveR = image.Rectangle{Min: bx, Max: bx.Add(d.Size)}
				return d
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(gtx.Dp(4), 0)} }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				d := a.jbNotifyButton(gtx, c.ch, b)
				x := b.moveR.Dx() + gtx.Dp(4)
				b.notifyR = image.Rectangle{Min: bx.Add(image.Pt(x, 0)), Max: bx.Add(image.Pt(x, 0)).Add(d.Size)}
				return d
			}),
		)
		bh = d.Size.Y
	}()
	y += bh
	content := rec.Stop()

	cardH := y + 2*pad
	card := image.Rect(mx, mt, w-mx, mt+cardH)
	total := image.Pt(w, mt+cardH)
	// the slide-down: opacity 0 and translateY(100%) (.animateSlideDown)
	// with the wrapper's transition: transform .3s, opacity .3s, through
	// the cascade (slackc.NativeTransitionsCSS), so a rule over it (Slick's
	// Snappy) shortens it
	var opac paint.OpacityStack
	var shift int
	if !b.closing.IsZero() {
		ts := slackc.CurrentScope().Child(clsJoinBanner, 1)
		td, tdl := ts.Transition("transform", jbSlide, 0)
		od, odl := ts.Transition("opacity", jbSlide, 0)
		pt, doneT := slackc.TransitionProgress(b.closing, gtx.Now, td, tdl)
		po, doneO := slackc.TransitionProgress(b.closing, gtx.Now, od, odl)
		if doneT && doneO {
			b.gone, b.h = true, total.Y // slid away; its place stays until the channel is left
			a.Invalidate()
			return layout.Dimensions{Size: total}
		}
		shift = int(pt * float32(total.Y))
		opac = paint.PushOpacity(gtx.Ops, 1-po)
		gtx.Execute(op.InvalidateCmd{})
	}
	st := op.Offset(image.Pt(0, shift)).Push(gtx.Ops)
	rad := gtx.Dp(12)
	theme.RRect(gtx, card, rad, jbCardBg(th))
	theme.Border(gtx, card, rad, bw, jbCardLine(th))
	func() {
		defer op.Offset(card.Min.Add(image.Pt(pad, pad))).Push(gtx.Ops).Pop()
		content.Add(gtx.Ops)
	}()
	// close: an UnstyledButton (c-button-unstyled: no padding, no hover
	// fill) around the 16px close SvgIcon, top 20 right 40 of the outer
	// wrapper, content-sec
	cs := gtx.Dp(16)
	at(gtx, image.Pt(w-gtx.Dp(40)-cs, gtx.Dp(20)), image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
		return b.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			pointer.CursorPointer.Add(gtx.Ops)
			return icons.Get("close")(gtx, 16, jbSec(th))
		})
	})
	area := clip.Rect(card).Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, b)
	pass.Pop()
	area.Pop()
	st.Pop()
	if !b.closing.IsZero() {
		opac.Pop()
	}
	return layout.Dimensions{Size: total}
}

// jbTitle is channelJoinBannerTitle (15px, 900, line-height 22): the
// c-emoji__small :wave: img (16px, titleEmoji's vertical-align text-top,
// so 2px down the line) and " Welcome to #name", the space a 15px black
// one.
func (a *App) jbTitle(gtx layout.Context, ch string, conv *convInfo) layout.Dimensions {
	th := a.th
	name := ch
	if conv != nil {
		name = conv.Name
	}
	sp := th.Measure(gtx, 15, theme.Black, "x x") - th.Measure(gtx, 15, theme.Black, "xx")
	return layout.Flex{Alignment: layout.Start}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			s := gtx.Dp(16)
			if c := a.Core(); c != nil {
				if u, _, ok := c.Emoji("wave"); ok && u != "" {
					if img, ok := a.images.Get(u, s); ok {
						defer op.Offset(image.Pt(0, gtx.Dp(2))).Push(gtx.Ops).Pop()
						theme.Image(gtx, img, image.Pt(s, s), 0)
					}
				}
			}
			return layout.Dimensions{Size: image.Pt(s+sp, s)}
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Black, Color: th.C.Fg}, "Welcome to #"+name)
		}),
	)
}

// jbPart is one flex item of a banner chip: its width, its CSS box's
// height (the chip centres it: align-items center), the margin before it,
// and how to draw it with the box's top-left at the origin.
type jbPart struct {
	w, h, gap int
	draw      func(gtx layout.Context)
}

// jbButton is the banner's c-button c-button--outline c-button--medium
// channelJoinBannerActionButtons (client-boot-styles): 28px tall
// border-box (everything is, Slack Kit's preflight), a 1px otl-ter
// border, radius 8 (--medium's --sk_button-styles-radius), padding 0 8px,
// min-width 80 (--medium's), inline-flex with align-items and
// justify-content center, 13px 700 text at line-height normal (the UA's,
// buttons don't inherit it), base-pry; hovered base-sec with 0 1px 3px
// and 0 1px 2px -1px #00000019.
func (a *App) jbButton(gtx layout.Context, c *widget.Clickable, parts []jbPart) layout.Dimensions {
	th := a.th
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		pointer.CursorPointer.Add(gtx.Ops)
		h, pad, bw := gtx.Dp(28), gtx.Dp(8), max(1, gtx.Dp(1))
		cw := 0
		for _, p := range parts {
			cw += p.gap + p.w
		}
		w := max(cw+2*pad, gtx.Dp(80))
		r := image.Rectangle{Max: image.Pt(w, h)}
		rad := gtx.Dp(8)
		bg := jbVar(th, "--dt_color-base-pry", th.C.Bg)
		if c.Hovered() {
			sh := color.NRGBA{A: 0x19}
			theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(1))), rad, gtx.Dp(3), sh)
			theme.Shadow(gtx, r.Inset(gtx.Dp(1)).Add(image.Pt(0, gtx.Dp(1))), rad, gtx.Dp(2), sh)
			bg = jbCardBg(th)
		}
		theme.RRect(gtx, r, rad, bg)
		theme.Border(gtx, r, rad, bw, jbCardLine(th))
		x := float32(w-cw) / 2
		for _, p := range parts {
			x += float32(p.gap)
			y := float32(bw) + float32(h-2*bw-p.h)/2
			t := op.Offset(image.Pt(int(x+.5), int(y+.5))).Push(gtx.Ops)
			p.draw(gtx)
			t.Pop()
			x += float32(p.w)
		}
		return layout.Dimensions{Size: r.Size()}
	})
}

// jbMetrics is the UI font's ascent and descent at size (px): the
// content area of a line at line-height normal.
func (a *App) jbMetrics(gtx layout.Context, size unit.Sp, weight font.Weight) (asc, desc int) {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<16, 1<<16)}
	d := a.th.Layout(g, theme.TextStyle{Size: size, Weight: weight, MaxLines: 1}, "x")
	m.Stop()
	return d.Size.Y - d.Baseline, d.Baseline
}

// jbText is a chip's 13px bold text (an anonymous flex item, or the
// section name span): a box of its line at line-height normal, at most
// maxW wide (text-overflow ellipsis).
func (a *App) jbText(gtx layout.Context, label string, maxW int) jbPart {
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(maxW, 1), 1<<16)}
	m := op.Record(gtx.Ops)
	d := a.th.Layout(g, theme.TextStyle{Size: 13, Weight: theme.Bold, Color: a.th.C.Fg, MaxLines: 1}, label)
	call := m.Stop()
	return jbPart{w: d.Size.X, h: d.Size.Y, draw: func(gtx layout.Context) { call.Add(gtx.Ops) }}
}

// jbIcon is NotificationsButton's p-about_modal__action_icon_prefix (and
// the suffix span): 15px / line-height 1.46668 (22px) around a 1em
// SvgIcon at vertical-align -.125em.
func (a *App) jbIcon(gtx layout.Context, name string) jbPart {
	lh, s := gtx.Dp(22), gtx.Dp(15)
	asc, desc := a.jbMetrics(gtx, 15, theme.Regular)
	top := (lh-asc-desc)/2 + asc + gtx.Dp(1.875) - s
	col := a.th.C.Fg
	return jbPart{w: s, h: lh, draw: func(gtx layout.Context) {
		defer op.Offset(image.Pt(0, top)).Push(gtx.Ops).Pop()
		icons.Get(name)(gtx, 15, col)
	}}
}

// jbSectionChevron is the section button's c-icon--chevron-medium-down
// (an inline-flex i, 1em of the suffix's 15px wide, its zero-width :after
// giving it the 22px line, the font glyph centred in it).
func (a *App) jbSectionChevron(gtx layout.Context) jbPart {
	lh, s := gtx.Dp(22), gtx.Dp(15)
	col := a.th.C.Fg
	return jbPart{w: s, h: lh, gap: gtx.Dp(4), draw: func(gtx layout.Context) {
		defer op.Offset(image.Pt(0, (lh-s)/2)).Push(gtx.Ops).Pop()
		icons.Get("chevron-medium-down")(gtx, 15, col)
	}}
}

// jbSectionButton is ViewHeaderChannelSectionSelector in the banner
// (source channel_join_banner): the section's emoji and name, the
// Starred star, the custom section's fallback icon, or "★ Move to
// Section" (showMoveSection: not starred and in no section), then the
// chevron.
func (a *App) jbSectionButton(gtx layout.Context, ch string, b *joinBanner) layout.Dimensions {
	st := a.channelSections(ch)
	var lead jbPart
	switch {
	case !st.starred && st.custom == nil:
		lead = a.jbMoveToSection(gtx)
	case st.custom == nil && st.stars != nil && st.stars.EmojiURL != "":
		lead = a.jbEmojiName(gtx, st.stars.EmojiURL, "")
	case st.custom == nil:
		lead = a.jbIconName(gtx, "star-font", "Starred", true)
	case st.custom.EmojiURL != "":
		lead = a.jbEmojiName(gtx, st.custom.EmojiURL, st.custom.Name)
	default:
		lead = a.jbIconName(gtx, "section", st.custom.Name, false)
	}
	return a.jbButton(gtx, &b.moveBtn, []jbPart{lead, a.jbSectionChevron(gtx)})
}

// jbEmojiName is p-view_header_channel_section_selector__section_name_and_emoji
// (nowrap, ellipsis, max-width 175): the c-emoji c-emoji__small img with
// padding_right_25 (16x16 border-box, so the emoji is contained in the
// 12x16 left of its 4px padding) at vertical-align text-bottom, then the
// 13px section name; one line whose box the 16px img makes taller than the
// text's.
func (a *App) jbEmojiName(gtx layout.Context, url, name string) jbPart {
	asc, desc := a.jbMetrics(gtx, 13, theme.Bold)
	es, img := gtx.Dp(16), gtx.Dp(12)
	base := max(asc, es-desc)
	var txt jbPart
	if name != "" {
		txt = a.jbText(gtx, name, gtx.Dp(175)-es)
	}
	return jbPart{w: es + txt.w, h: base + desc, draw: func(gtx layout.Context) {
		if im, ok := a.images.Get(url, img); ok {
			t := op.Offset(image.Pt(0, base+desc-es+(es-img)/2)).Push(gtx.Ops)
			theme.Image(gtx, im, image.Pt(img, img), 0)
			t.Pop()
		}
		if txt.draw != nil {
			defer op.Offset(image.Pt(es, base-asc)).Push(gtx.Ops).Pop()
			txt.draw(gtx)
		}
	}}
}

// jbIconName is the Starred section's inline c-icon--star (font, 1em of
// the 13px text, centred in the i the text's line makes, margin-right 4)
// or the custom section's InlineChannelSectionFallbackIcon (an inline
// 1em "section" SvgIcon at -.125em in a margin_right_25 span), then the
// name, in one line at line-height normal.
func (a *App) jbIconName(gtx layout.Context, icon, name string, font bool) jbPart {
	asc, desc := a.jbMetrics(gtx, 13, theme.Bold)
	s, gap := gtx.Dp(13), gtx.Dp(4)
	top := (asc + desc - s) / 2
	if !font {
		top = asc + gtx.Dp(1.625) - s
	}
	txt := a.jbText(gtx, name, gtx.Dp(175)-s-gap)
	col := a.th.C.Fg
	return jbPart{w: s + gap + txt.w, h: asc + desc, draw: func(gtx layout.Context) {
		t := op.Offset(image.Pt(0, top)).Push(gtx.Ops)
		icons.Get(icon)(gtx, 13, col)
		t.Pop()
		defer op.Offset(image.Pt(s+gap, 0)).Push(gtx.Ops).Pop()
		txt.draw(gtx)
	}}
}

// jbMoveToSection is p-view_header_channel_section_selector__channel_join_banner
// (display flex, items stretched): the 16px star SvgIcon (display block)
// in a margin_right_25 span, then "Move to Section" at the top of the
// 16px line.
func (a *App) jbMoveToSection(gtx layout.Context) jbPart {
	s, gap := gtx.Dp(16), gtx.Dp(4)
	txt := a.jbText(gtx, jbMove, 1<<16)
	col := a.th.C.Fg
	return jbPart{w: s + gap + txt.w, h: max(s, txt.h), draw: func(gtx layout.Context) {
		icons.Get("star")(gtx, 16, col)
		defer op.Offset(image.Pt(s+gap, 0)).Push(gtx.Ops).Pop()
		txt.draw(gtx)
	}}
}

// jbNotify is NotificationsControl's level and label for the banner.
func (a *App) jbNotify(ch string) (level, label, icon string, muted bool) {
	c := a.Core()
	n := c.NotifyPrefs(ch)
	conv := a.model().conv(ch)
	muted = n.Muted || conv != nil && conv.Muted
	level = n.Desktop
	if level == "" || level == "default" {
		level = c.Prefs().GlobalDesktop
	}
	switch level { // pA
	case "everything", "nothing":
	default:
		level = "mentions_dms"
	}
	if muted {
		return level, "Muted", "notifications-off", true
	}
	switch level { // B0 and Km
	case "everything":
		return level, "All new posts", "notifications-all-new-posts", false
	case "nothing":
		return level, "Nothing", "notifications-off", false
	}
	return level, "Just mentions", "notifications", false
}

// jbNotifyButton is NotificationsControl in the banner (vs9A
// NotificationsButton, size medium): the level's icon in
// p-about_modal__action_icon_prefix (margin-right 4), the label, and the
// caret-down SvgIcon (Clack's chevron-down) in
// p-about_modal__action_icon_suffix (margin-left 4).
func (a *App) jbNotifyButton(gtx layout.Context, ch string, b *joinBanner) layout.Dimensions {
	_, label, icon, _ := a.jbNotify(ch)
	txt := a.jbText(gtx, label, 1<<16)
	txt.gap = gtx.Dp(4)
	caret := a.jbIcon(gtx, "chevron-down")
	caret.gap = gtx.Dp(4)
	return a.jbButton(gtx, &b.notifyBtn, []jbPart{a.jbIcon(gtx, icon), txt, caret})
}

// jbVar is a slack-kit token's color in th's mode, else fallback.
func jbVar(th *theme.Theme, name string, fallback color.NRGBA) color.NRGBA {
	if v, ok := css.SlackVars(th.Dark)[name]; ok {
		if c, ok := css.ParseColor(v); ok {
			return c
		}
	}
	return fallback
}

// megaphoneBoot starts the boot load (maybeFetchMegaphoneOnBootEvent)
// once, with the channel on screen as event.channel; core repeats it at
// most hourly.
func (a *App) megaphoneBoot(ch string) {
	if a.megaBooted {
		return
	}
	a.megaBooted = true
	c := a.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = c.MegaphoneBoot(ctx, ch)
	}()
}

// askJoinBanner is ChannelJoinBanner's REQUEST_SPACES on mount: the
// answers land in core's spaces for ch (a "megaphone" event redraws).
func (a *App) askJoinBanner(ch string) {
	c := a.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = c.MegaphoneSpaces(ctx, ch, jbSpaces)
	}()
}

// dismissJoinBanner is the close button (and Delete): slide down, record
// the dismissal.
func (a *App) dismissJoinBanner(ch string, b *joinBanner) {
	if !b.closing.IsZero() {
		return
	}
	b.closing = time.Now()
	c := a.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = c.RecordMegaphone(ctx, core.NotificationChannelJoinBanner, core.MegaphoneDismiss, ch)
	}()
	a.Invalidate()
}

// leaveJoinBanner is maybeHideChannelJoinBanner, on every navigation
// (afterRoute): leaving a channel view unmounts its banner, and when the
// channel's message_input holds the banner it records an impression and
// asks that space again.
func (a *App) leaveJoinBanner(prev, next Route) {
	ch := prev.Channel()
	if ch == "" || ch == next.Channel() {
		return
	}
	if cv := a.convos[ch+"/"]; cv != nil && cv.banner != nil {
		cv.banner.mounted = false
	}
	c := a.Core()
	if c == nil || !core.LooksLikeChannelID(ch) || c.MegaphoneNotification(core.SpaceMessageInput, ch) != core.NotificationChannelJoinBanner {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = c.RecordMegaphone(ctx, core.NotificationChannelJoinBanner, core.MegaphoneImpression, ch)
		_, _ = c.MegaphoneSpaces(ctx, ch, []string{core.SpaceMessageInput})
	}()
}

// jbCardBg is --dt_color-base-sec, jbCardLine --dt_color-otl-ter and
// jbSec --dt_color-content-sec (slack-kit tokens, light / dark).
func jbCardBg(th *theme.Theme) color.NRGBA {
	if th.Dark {
		return color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 0xff}
	}
	return color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}
}

func jbCardLine(th *theme.Theme) color.NRGBA {
	if th.Dark {
		return color.NRGBA{R: 0x79, G: 0x7c, B: 0x81, A: 0x4d}
	}
	return color.NRGBA{R: 0x5e, G: 0x5d, B: 0x60, A: 0x21}
}

func jbSec(th *theme.Theme) color.NRGBA {
	if th.Dark {
		return color.NRGBA{R: 0xb9, G: 0xba, B: 0xbd, A: 0xff}
	}
	return color.NRGBA{R: 0x45, G: 0x44, B: 0x47, A: 0xff}
}
