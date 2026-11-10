package ui

// The channel join banner over the composer: Slack's ChannelJoinBanner
// (chunk megaphone-message-pane-input-channel-join-banner 7BTH, its
// buttons in the shared …message-pane-input-channel-join-banner_channel
// chunk mzyA / QwCw, CSS megaphone-message-pane-input-channel-join-banner,
// CDN build bv1-13).
//
//   - When: Slack's megaphone service decides. Opening a channel (not a
//     DM or group DM) asks megaphone.notifications.list for the
//     message_input spaces; the banner shows while message_input's
//     notification is education_channel_join_banner. It never shows for
//     a preview (isPreview), an agent record channel or a meeting
//     channel, and the pref hides it: with isAutoSortRolloutActive
//     (suggested_sections_auto_sort_v2 on, its killswitch off)
//     disable_channel_join_banner true, otherwise unless
//     channel_join_experience_enabled is true.
//   - What (the default variant; the auto-sort and Slackbot AI variants
//     need server state and AI features Clack doesn't have): the card
//     (1px otl-ter, base-sec, radius 12, margin 8 20 0, padding 20), a
//     close button at top 20 right 40, ":wave: Welcome to #name" (15px
//     900), "Group this channel with similar conversations in your
//     sidebar and choose when you want to be notified about new
//     messages." (13px/18px) and two 28px outline buttons 4px apart: the
//     section selector ("★ Move to Section" for a channel in no section,
//     else the section's emoji or icon and name, with a chevron) opening
//     the Move to… menu, and the notifications control (its level's icon
//     and "All new posts" / "Just mentions" / "Nothing" / "Muted", a
//     chevron) opening ChannelNotificationPrefMenu above.
//   - Dismissal: the close button or Delete on the banner slides it down
//     (.3s) and records megaphone.setNotificationAsSeen dismiss. Moving
//     the channel to another section or changing its notifications from
//     elsewhere (not through the banner) closes it without the record.
//     Leaving the channel records an impression and asks the spaces
//     again (maybeHideChannelJoinBanner).

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's banner copy (custom_emoji_list namespace).
const (
	jbBody  = "Group this channel with similar conversations in your sidebar and choose when you want to be notified about new messages."
	jbMove  = "Move to Section"
	jbSlide = 300 * time.Millisecond
)

// jbSpaces are the spaces ChannelJoinBanner requests on mount.
var jbSpaces = []string{core.SpaceMessageInput, "message_input_invited_glow", "message_input_channel_join_summary_glow"}

type joinBanner struct {
	asked, show bool
	closing     time.Time
	gone        bool
	touched     bool // the banner was clicked (eK): changes don't auto-close it
	initSection string
	initNotify  string
	initMuted   bool
	seeded      bool
	closeBtn    widget.Clickable
	moveBtn     widget.Clickable
	notifyBtn   widget.Clickable
	moveR       image.Rectangle // window coordinates, for the menus
	notifyR     image.Rectangle
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

// layoutJoinBanner draws c's banner (when Slack's megaphone says so) and
// returns its size; origin is its window position.
func (a *App) layoutJoinBanner(gtx layout.Context, c *convo, origin image.Point) layout.Dimensions {
	core := a.Core()
	if c.ts != "" || core == nil {
		return layout.Dimensions{}
	}
	conv := a.model().conv(c.ch)
	cv, _ := core.Conv(c.ch)
	if !joinBannerEligible(conv, cv) {
		return layout.Dimensions{}
	}
	if a.joinBannerCh != c.ch { // maybeHideChannelJoinBanner for the one left
		a.leaveJoinBanner()
		a.joinBannerCh = c.ch
	}
	b := c.banner
	if b == nil {
		b = &joinBanner{}
		c.banner = b
	}
	if !b.asked {
		b.asked = true
		a.askJoinBanner(c.ch, b)
	}
	if !b.show || b.gone || joinBannerHidden(core) {
		return layout.Dimensions{}
	}
	a.joinBannerAutoClose(c.ch, b)
	if b.gone {
		return layout.Dimensions{}
	}
	if b.closeBtn.Clicked(gtx) {
		a.dismissJoinBanner(c.ch, b)
	}
	if b.moveBtn.Clicked(gtx) {
		b.touched = true
		st := a.channelSections(c.ch)
		a.menuAt(b.moveR, int(popups.Above), 280, a.moveToItems(c.ch, st))
	}
	if b.notifyBtn.Clicked(gtx) {
		b.touched = true
		a.ShowOverlay(popups.NewMenuOpts(a, popups.ChannelNotifyItems(a, c.ch), popups.MenuOpts{Anchor: b.notifyR, Side: popups.Above, Width: 300}))
	}
	// Delete on the focused banner dismisses it; a click focuses it (and
	// marks it touched)
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
			b.touched = true
			gtx.Execute(key.FocusCmd{Tag: b})
		}
	}

	th := a.th
	tc := &th.C
	w := gtx.Constraints.Max.X
	mx, mt, pad := gtx.Dp(20), gtx.Dp(8), gtx.Dp(20)
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
		d := th.Layout(g, theme.TextStyle{Size: 13, LineHeight: 18, Color: tc.Fg}, jbBody)
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
	// the slide-down: opacity 0 and translateY(100%) over .3s
	var opac paint.OpacityStack
	var shift int
	if !b.closing.IsZero() {
		p := float32(gtx.Now.Sub(b.closing)) / float32(jbSlide)
		if p >= 1 {
			b.gone = true
			a.Invalidate()
			return layout.Dimensions{}
		}
		shift = int(p * float32(total.Y))
		opac = paint.PushOpacity(gtx.Ops, 1-p)
		gtx.Execute(op.InvalidateCmd{})
	}
	st := op.Offset(image.Pt(0, shift)).Push(gtx.Ops)
	rad := gtx.Dp(12)
	theme.RRect(gtx, card, rad, jbCardBg(th))
	theme.Border(gtx, card, rad, max(1, gtx.Dp(1)), jbCardLine(th))
	func() {
		defer op.Offset(card.Min.Add(image.Pt(pad, pad))).Push(gtx.Ops).Pop()
		content.Add(gtx.Ops)
	}()
	// close: top 20, right 40 of the wrapper (16px icon, content-sec)
	cs := gtx.Dp(28)
	cx, cy := w-gtx.Dp(40)-gtx.Dp(16)-(cs-gtx.Dp(16))/2, gtx.Dp(20)-(cs-gtx.Dp(16))/2
	at(gtx, image.Pt(cx, cy), image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
		return b.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			pointer.CursorPointer.Add(gtx.Ops)
			if b.closeBtn.Hovered() {
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(cs, cs)}, gtx.Dp(6), tc.BgHover)
			}
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return icons.Get("close")(gtx, 16, jbSec(th))
			})
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

// jbTitle is ":wave: Welcome to #name" in 15px black.
func (a *App) jbTitle(gtx layout.Context, ch string, conv *convInfo) layout.Dimensions {
	th := a.th
	name := ch
	if conv != nil {
		name = conv.Name
	}
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
			return layout.Dimensions{Size: image.Pt(s+gtx.Dp(4), s)}
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Black, Color: th.C.Fg}, "Welcome to #"+name)
		}),
	)
}

// jbButton is .channelJoinBannerActionButtons: an outline button 28 tall,
// padding 0 8px, 13px, a 1px otl-ter border.
func (a *App) jbButton(gtx layout.Context, c *widget.Clickable, lead func(gtx layout.Context) layout.Dimensions, label string, chevron bool) layout.Dimensions {
	th := a.th
	tc := &th.C
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		pointer.CursorPointer.Add(gtx.Ops)
		h := gtx.Dp(28)
		rec := op.Record(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		d := layout.Inset{Left: 8, Right: 8}.Layout(g, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = h
			return layout.W.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if lead == nil {
							return layout.Dimensions{}
						}
						return layout.Inset{Right: 4}.Layout(gtx, lead)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.Label(gtx, 13, theme.Bold, tc.Fg, label)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !chevron {
							return layout.Dimensions{}
						}
						return layout.Inset{Left: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return icons.Get("chevron-down")(gtx, 13, tc.Fg)
						})
					}),
				)
			})
		})
		call := rec.Stop()
		r := image.Rectangle{Max: image.Pt(d.Size.X, h)}
		rad := gtx.Dp(8)
		theme.RRect(gtx, r, rad, tc.Bg)
		if c.Hovered() {
			theme.RRect(gtx, r, rad, tc.BgHover)
		}
		theme.Border(gtx, r, rad, max(1, gtx.Dp(1)), jbCardLine(th))
		call.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Size()}
	})
}

// jbSectionButton is ViewHeaderChannelSectionSelector for the banner:
// "★ Move to Section" (showMoveSection: not starred and in no custom
// section), else where the channel is.
func (a *App) jbSectionButton(gtx layout.Context, ch string, b *joinBanner) layout.Dimensions {
	tc := &a.th.C
	st := a.channelSections(ch)
	icon := func(name string) func(gtx layout.Context) layout.Dimensions {
		return func(gtx layout.Context) layout.Dimensions { return icons.Get(name)(gtx, 16, tc.Fg) }
	}
	emoji := func(url string) func(gtx layout.Context) layout.Dimensions {
		return func(gtx layout.Context) layout.Dimensions {
			s := gtx.Dp(16)
			if img, ok := a.images.Get(url, s); ok {
				theme.Image(gtx, img, image.Pt(s, s), 0)
			}
			return layout.Dimensions{Size: image.Pt(s, s)}
		}
	}
	switch {
	case !st.starred && st.custom == nil:
		return a.jbButton(gtx, &b.moveBtn, icon("star"), jbMove, true)
	case st.starred && st.custom == nil:
		if st.stars != nil && st.stars.EmojiURL != "" {
			return a.jbButton(gtx, &b.moveBtn, emoji(st.stars.EmojiURL), "Starred", true)
		}
		return a.jbButton(gtx, &b.moveBtn, icon("star"), "Starred", true)
	}
	if st.custom.EmojiURL != "" {
		return a.jbButton(gtx, &b.moveBtn, emoji(st.custom.EmojiURL), st.custom.Name, true)
	}
	return a.jbButton(gtx, &b.moveBtn, icon(sectionIcon("standard")), st.custom.Name, true)
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

func (a *App) jbNotifyButton(gtx layout.Context, ch string, b *joinBanner) layout.Dimensions {
	_, label, icon, _ := a.jbNotify(ch)
	tc := &a.th.C
	return a.jbButton(gtx, &b.notifyBtn, func(gtx layout.Context) layout.Dimensions {
		return icons.Get(icon)(gtx, 16, tc.Fg)
	}, label, true)
}

// joinBannerAutoClose is useHasSectionOrNotificationChanged: a change
// made elsewhere closes the banner (without recording a dismissal).
func (a *App) joinBannerAutoClose(ch string, b *joinBanner) {
	st := a.channelSections(ch)
	sec := ""
	switch {
	case st.custom != nil:
		sec = st.custom.ID
	case st.starred && st.stars != nil:
		sec = st.stars.ID
	case st.starred:
		sec = "stars"
	}
	level, _, _, muted := a.jbNotify(ch)
	if !b.seeded {
		b.seeded = true
		b.initSection, b.initNotify, b.initMuted = sec, level, muted
		return
	}
	if b.touched {
		return
	}
	if sec != b.initSection || level != b.initNotify || muted != b.initMuted {
		b.gone = true
	}
}

// askJoinBanner is the REQUEST_SPACES load for ch.
func (a *App) askJoinBanner(ch string, b *joinBanner) {
	c := a.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		sp, err := c.MegaphoneSpaces(ctx, ch, jbSpaces)
		if err != nil {
			return
		}
		show := sp[core.SpaceMessageInput] == core.NotificationChannelJoinBanner
		a.Post(func() {
			b.show = show
			a.Invalidate()
		})
	}()
}

// dismissJoinBanner is the close button: slide down, record the dismissal.
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

// leaveJoinBanner is maybeHideChannelJoinBanner: leaving a channel whose
// banner showed records an impression and asks its spaces again.
func (a *App) leaveJoinBanner() {
	ch := a.joinBannerCh
	if ch == "" || a.convos == nil {
		return
	}
	cv := a.convos[ch+"/"]
	if cv == nil || cv.banner == nil || !cv.banner.show || cv.banner.gone {
		return
	}
	b := cv.banner
	c := a.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = c.RecordMegaphone(ctx, core.NotificationChannelJoinBanner, core.MegaphoneImpression, ch)
		sp, err := c.MegaphoneSpaces(ctx, ch, []string{core.SpaceMessageInput})
		if err != nil {
			return
		}
		show := sp[core.SpaceMessageInput] == core.NotificationChannelJoinBanner
		a.Post(func() { b.show = show })
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
