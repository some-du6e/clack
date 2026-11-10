package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"

	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/huddleui"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// channelPane holds per-channel shell state (the views are cached in
// a.views so switching back is instant).
type channelPane struct {
	tab int // 0 = Messages, then bookmarks/pins/canvases
}

func (a *App) layoutChannel(gtx layout.Context, ch string) layout.Dimensions {
	sz := gtx.Constraints.Max
	c := &a.th.C
	conv := a.model().conv(ch)
	if conv == nil && a.data != nil {
		conv = a.data.resolved(ch) // closed DM / channel outside the sidebar
	}
	hh := gtx.Dp(theme.HeaderH)
	th := gtx.Dp(theme.TabsH)

	atExact(gtx, image.Pt(0, 0), image.Pt(sz.X, hh), func(gtx layout.Context) layout.Dimensions {
		return a.layoutChannelHeader(gtx, ch, conv)
	})
	dm := a.appDM(ch) // app DMs: Home / Messages / About (blockkit)
	atExact(gtx, image.Pt(0, hh), image.Pt(sz.X, th), func(gtx layout.Context) layout.Dimensions {
		if dm != nil {
			return dm.LayoutTabs(gtx)
		}
		return a.layoutChannelTabs(gtx, ch)
	})
	theme.Fill(gtx, image.Rect(0, hh+th-gtx.Dp(1), sz.X, hh+th), c.Border)

	body := image.Rect(0, hh+th, sz.X, sz.Y)
	if a.chBodyW > 0 && a.chBodyW < sz.X {
		body.Max.X = a.chBodyW
	}
	if dm != nil && dm.Tab() != "messages" {
		atExact(gtx, body.Min, body.Size(), dm.LayoutBody)
		return layout.Dimensions{Size: sz}
	}
	if v := a.canvasTabView(ch); v != nil { // a canvas tab (mount_features_canvas.go)
		atExact(gtx, body.Min, body.Size(), v.Layout)
		return layout.Dimensions{Size: sz}
	}
	if v := a.listTabView(ch); v != nil { // a list tab (mount_features_lists.go)
		atExact(gtx, body.Min, body.Size(), v.Layout)
		return layout.Dimensions{Size: sz}
	}
	cv := a.convoFor(ch, "")
	atExact(gtx, body.Min, body.Size(), func(gtx layout.Context) layout.Dimensions {
		mp := clsMessagePane.Enter(gtx, 0, 0)
		defer mp.Leave()
		return a.layoutConvo(gtx, cv, a.primaryOrigin.Add(body.Min))
	})
	return layout.Dimensions{Size: sz}
}

func (a *App) layoutChannelHeader(gtx layout.Context, ch string, conv *convInfo) layout.Dimensions {
	sz := gtx.Constraints.Max
	vh := clsViewHeader.Enter(gtx, 0, 0)
	defer vh.Leave()
	if vh.Hidden() {
		return layout.Dimensions{Size: sz}
	}
	c := &a.th.C
	if vh.Has("background-color") { // a theme's .p-view_header background
		theme.Fill(gtx, image.Rectangle{Max: sz}, theme.Over(vh.Background(c.Bg), c.Bg))
	}
	h := sz.Y
	x := gtx.Dp(18) // Slack: 28px star button at +18, title icon at +54
	// Slack's header has no star (ViewHeaderEntitySectionSelector) and no
	// notifications bell for a channel the viewer isn't in: the preview's
	// title sits at +18 (C08E083LZ9A, 2026-11-08). A code channel shows
	// ViewHeaderCodeIcon where the star goes, member or not (#test).
	preview := conv != nil && conv.Preview
	switch {
	case conv != nil && conv.Code != nil:
		at(gtx, image.Pt(x, (h-gtx.Dp(28))/2), image.Pt(gtx.Dp(28), gtx.Dp(28)), a.codeHeaderTile)
		x += gtx.Dp(34) // title button at +52
	case !preview:
		a.layoutHeaderStar(gtx, ch, conv, image.Pt(x, (h-gtx.Dp(28))/2))
		x += gtx.Dp(34) // title button at +52
	}

	// right-side controls, laid out right-to-left first so the title can
	// ellipsize into the remaining space.
	right := sz.X - gtx.Dp(14)
	btn := gtx.Dp(28)
	y := (h - btn) / 2
	kebabX := 0
	// Slack's app DMs keep only the bell and the kebab: no search, Huddle
	// or member count.
	appDM := conv != nil && conv.Kind == "app"
	hdrBtns := []string{"kebab", "search", "bell"}
	switch {
	case appDM:
		hdrBtns = []string{"kebab", "bell"}
	case preview:
		hdrBtns = []string{"kebab", "search"}
	}
	for _, k := range hdrBtns {
		k := k
		right -= btn
		if k == "kebab" {
			kebabX = right
		}
		at(gtx, image.Pt(right, y), image.Pt(btn, btn), func(gtx layout.Context) layout.Dimensions {
			name := k
			if k == "bell" {
				name = a.headerBellIcon(ch, conv)
			}
			return iconButton(gtx, a.clicks.get("hdr."+k), name, 28, 20, c.FgMuted, c.BgHover, 8)
		})
		right -= gtx.Dp(6)
	}
	if clicked(a.clicks.get("hdr.kebab")) {
		// popups take window coordinates; the pointer is on the kebab (this
		// header is also drawn inside views' panes, where offsets differ)
		_ = kebabX
		// Slack opens it right-aligned 6px inside the kebab's right edge, its
		// top at y 74 (the header is always 40px down and its kebab sits 14px
		// from the panel's right inset, here and in a split pane).
		a.openChannelMenu(ch, image.Pt(a.winSize.X-gtx.Dp(theme.PanelInset)-gtx.Dp(14)-gtx.Dp(6), gtx.Dp(theme.TopNavH)+gtx.Dp(34)))
	}
	if clicked(a.clicks.get("hdr.search")) {
		a.searchInChannel(ch) // Slack's searchInChannel: the ⌘F pane
	}
	if clicked(a.clicks.get("hdr.bell")) {
		a.openNotifPrefs(ch)
	}

	// doesChannelSupportHuddles: no button in a channel you're previewing
	// without huddle_join_from_preview (huddleui.SupportsHuddles)
	if hud := a.huddles(); !appDM && hud.SupportsHuddles(ch) {
		// Huddle split button (94×28: 74 + 20); Slack 2026's --polish variant
		// has no outline, only hover fills. While a huddle is on, huddleui
		// draws Slack's live button: mint with faces, headphones and a bare
		// count, or solid jade with the leave headphones when you're in it.
		hud.Seed()
		hw := gtx.Dp(94)
		live := hud.HeaderLive(ch)
		if live {
			hw = hud.HeaderPillWidth(gtx, a.th, ch)
		}
		right -= hw
		hudAt := image.Pt(right, y)
		hudOrg := a.hudOrigin[ch]
		if hudOrg == nil {
			if a.hudOrigin == nil {
				a.hudOrigin = map[string]*originTracker{}
			}
			hudOrg = &originTracker{}
			a.hudOrigin[ch] = hudOrg
		}
		caretX := gtx.Dp(74) // the caret's x in the button
		if live {
			caretX = hud.HeaderCaretX(gtx, a.th, ch)
			at(gtx, image.Pt(right, y), image.Pt(hw, btn), func(gtx layout.Context) layout.Dimensions {
				d := hud.HeaderPill(gtx, a.th, ch, btn)
				hudOrg.track(gtx, a)
				return d
			})
		} else {
			at(gtx, image.Pt(right, y), image.Pt(hw, btn), func(gtx layout.Context) layout.Dimensions {
				r := image.Rect(0, 0, hw, btn)
				at(gtx, image.Pt(0, 0), image.Pt(gtx.Dp(74), btn), func(gtx layout.Context) layout.Dimensions {
					return clickArea(gtx, a.clicks.get("hdr.huddle"), func(gtx layout.Context, s image.Point, hov bool) {
						if hov {
							theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), c.BgHover)
						}
					}, func(gtx layout.Context) layout.Dimensions {
						at(gtx, image.Pt(gtx.Dp(8), (btn-gtx.Dp(18))/2), image.Pt(gtx.Dp(18), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
							return icons.Get("huddle")(gtx, 18, c.FgMuted)
						})
						at(gtx, image.Pt(gtx.Dp(30), 0), image.Pt(gtx.Dp(44), btn), func(gtx layout.Context) layout.Dimensions {
							return vcenter(gtx, btn, func(gtx layout.Context) layout.Dimensions {
								return a.th.Label(gtx, 13, theme.Bold, c.FgMuted, i18n.T("huddles", "Huddle"))
							})
						})
						return layout.Dimensions{Size: image.Pt(gtx.Dp(74), btn)}
					})
				})
				hud.StartTip(gtx, a.th, ch, a.clicks.get("hdr.huddle").Hovered(), image.Pt(gtx.Dp(74), btn))
				at(gtx, image.Pt(gtx.Dp(74), 0), image.Pt(gtx.Dp(20), btn), func(gtx layout.Context) layout.Dimensions {
					return clickArea(gtx, a.clicks.get("hdr.huddlemore"), nil, func(gtx layout.Context) layout.Dimensions {
						at(gtx, image.Pt(gtx.Dp(1), (btn-gtx.Dp(14))/2), image.Pt(gtx.Dp(14), gtx.Dp(14)), func(gtx layout.Context) layout.Dimensions {
							return icons.Get("chevron-down")(gtx, 14, c.FgMuted)
						})
						return layout.Dimensions{Size: image.Pt(gtx.Dp(20), btn)}
					})
				})
				at(gtx, image.Pt(gtx.Dp(74), 0), image.Pt(gtx.Dp(20), btn), func(gtx layout.Context) layout.Dimensions {
					hud.CaretTip(gtx, a.th, ch, a.clicks.get("hdr.huddlemore").Hovered(), image.Pt(gtx.Dp(20), btn))
					return layout.Dimensions{}
				})
				hudOrg.track(gtx, a)
				return layout.Dimensions{Size: r.Size()}
			})
		}
		if clicked(a.clicks.get("hdr.huddle")) {
			hud.Begin(ch, huddleui.EntryHeaderButton)
		}
		if clicked(a.clicks.get("hdr.huddlemore")) || hud.TakeCaret(ch) {
			// the pointer is on the caret, so the tracker knows the button's
			// window position (fallback: where it sits in the main pane)
			fb := image.Pt(a.winSize.X-gtx.Dp(theme.PanelInset)-(sz.X-hudAt.X), gtx.Dp(theme.TopNavH)+hudAt.Y)
			org := hudOrg.get(fb)
			caret := image.Rect(caretX, 0, caretX+gtx.Dp(20), btn).Add(org)
			a.openHuddleMenu(gtx, ch, caret)
		}
		right -= gtx.Dp(6)
	}

	// member facepile (up to 3 avatars, 20px, overlapped) + count: Slack's
	// BaseAvatarStack, whose width a plugin may change (HumanCount).
	if conv != nil && conv.Kind != "im" && !appDM && conv.NumMembers > 0 {
		call, d := measured(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(right-x, 0), btn)}
			return slackc.Component(gtx, "BaseAvatarStack", hdrStack{a: a, ch: ch, conv: conv, btn: btn}, hdrStack.props, hdrStack.layout)
		})
		right -= d.Size.X
		place(gtx, call, image.Pt(right, y+(btn-d.Size.Y)/2))
		if clicked(a.clicks.get("hdr.members")) {
			a.OpenDetails(ch)
		}
		if d.Size.X > 0 {
			right -= gtx.Dp(6)
		}
	}

	// title: icon + name 18px/900 + chevron, clickable → details.
	name := a.convTitle(conv, ch)
	icon := "hash"
	if conv != nil {
		switch conv.Kind {
		case "private":
			icon = "lock"
		case "im", "app":
			icon = ""
		case "mpim":
			icon = "people"
		}
	}
	avail := right - x - gtx.Dp(16)
	nw := a.th.Measure(gtx, 18, theme.Black, name)
	// Slack (measured): button padding 2px 4px, 18px icon, name 20px after
	// it, no chevron, title min-width 88px; the topic starts 16px after.
	iconW := 0
	if icon != "" {
		iconW = gtx.Dp(20)
	} else if conv != nil {
		iconW = gtx.Dp(28)
	}
	titleW := max(iconW+nw, gtx.Dp(88)) + gtx.Dp(8)
	if titleW > avail {
		nw -= titleW - avail
		titleW = avail
	}
	if nw < 0 {
		nw = 0
	}
	at(gtx, image.Pt(x, (h-gtx.Dp(32))/2), image.Pt(titleW, gtx.Dp(32)), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("hdr.title"), func(gtx layout.Context, s image.Point, hov bool) {
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.BgHover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			th := gtx.Dp(32)
			ix := gtx.Dp(4)
			if icon != "" {
				at(gtx, image.Pt(ix, (th-gtx.Dp(18))/2), image.Pt(gtx.Dp(18), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icon)(gtx, 18, c.Fg)
				})
			} else if conv != nil {
				at(gtx, image.Pt(ix, (th-gtx.Dp(24))/2), image.Pt(gtx.Dp(24), gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
					return a.avatar(gtx, conv.Avatar, conv.Name, gtx.Dp(24), gtx.Dp(6))
				})
			}
			at(gtx, image.Pt(ix+iconW, 0), image.Pt(nw, th), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, th, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 18, theme.Black, c.Fg, name)
				})
			})
			return layout.Dimensions{Size: image.Pt(titleW, th)}
		})
	})
	if clicked(a.clicks.get("hdr.title")) {
		a.OpenDetails(ch)
	}
	a.headerTitleMenu(gtx, ch, image.Rect(x, (h-gtx.Dp(32))/2, x+titleW, (h+gtx.Dp(32))/2))
	x += titleW + gtx.Dp(16)

	// topic (13px muted) in the remaining space.
	if conv != nil && conv.Topic != "" && right-x > gtx.Dp(40) {
		a.layoutHeaderTopic(gtx, ch, conv, image.Rect(x, 0, right-gtx.Dp(12), h))
	}
	return layout.Dimensions{Size: sz}
}

// headerBellIcon is the bell's glyph (NotificationsControl, source
// channel_header, boot-shared vpVS): n9iP Nl of the calculated desktop
// setting, the ringing bell for All new posts and notifications-off when
// muted; 1:1 DMs show notifications / notifications-off.
func (a *App) headerBellIcon(ch string, conv *convInfo) string {
	muted := conv != nil && conv.Muted
	if conv != nil && (conv.Kind == "im" || conv.Kind == "app") {
		return popups.NotifyIcon("", muted)
	}
	return popups.ChannelNotifyIcon(a, ch, muted)
}

// canSetTopic is canUserSetTopicForChannelWithReasonById (boot-shared
// xjwB) on what Clack knows: not archived, a member, allowed to post, not
// a guest. (Slack's SET_CHANNEL_TOPIC role grant and the
// channel_topic_purpose_setting_restrict_to_channel_managers experiment
// need permission data Clack doesn't load.)
func (a *App) canSetTopic(ch string, conv *convInfo) bool {
	if conv == nil || conv.Archived || conv.Preview || conv.Code != nil {
		return false
	}
	c := a.Core()
	if c == nil {
		return false
	}
	if conv.Kind == "im" || conv.Kind == "mpim" || conv.Kind == "app" {
		return true
	}
	return c.PostingIn(ch).CanPost() && !c.Self().Guest
}

// layoutHeaderTopic is Slack's ViewHeaderTopic / ModelInfoTopic
// (view-header chunk W58+): the topic, one 13px line ellipsized in r,
// is a button that opens openChannelTopicDialog unless the topic is read
// only, and an "Edit" link (aria-label "Edit channel topic", "Edit topic"
// in DMs; .p-classic_nav__model__title__info__topic__edit: link blue,
// 8px after the topic, underlined on its own hover) shows while the
// pointer is over the topic container. When the topic doesn't fit,
// Slack caps it at min(70%, 540px) so Edit stays beside it.
func (a *App) layoutHeaderTopic(gtx layout.Context, ch string, conv *convInfo, r image.Rectangle) {
	hdrTopicRect = r
	c := &a.th.C
	h := r.Dy()
	editable := a.canSetTopic(ch, conv)
	editW := 0
	if editable {
		editW = a.th.Measure(gtx, 13, theme.Regular, "Edit")
	}
	avail := r.Dx()
	full := a.headerTopicWidth(gtx, conv)
	tw := min(full, avail)
	if editable && full+gtx.Dp(8)+editW > avail { // --truncated
		tw = min(avail*7/10, gtx.Dp(540), avail-gtx.Dp(8)-editW)
	}
	if tw <= 0 {
		return
	}
	hov := a.clicks.get("hdr.topicbox")
	at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
		return hov.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Point{}, image.Pt(tw, h), func(gtx layout.Context) layout.Dimensions {
				draw := func(gtx layout.Context) layout.Dimensions {
					if len(conv.TopicSpans) == 0 {
						return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
							return a.th.Layout(gtx, theme.TextStyle{Size: 13, Weight: theme.Regular, Color: c.FgMuted, MaxLines: 1}, oneLine(conv.Topic))
						})
					}
					return a.inlineSpans(gtx, conv.TopicSpans, 13, c.FgMuted, h)
				}
				if !editable {
					return draw(gtx)
				}
				return clickArea(gtx, a.clicks.get("hdr.topic"), nil, func(gtx layout.Context) layout.Dimensions {
					draw(gtx)
					return layout.Dimensions{Size: image.Pt(tw, h)}
				})
			})
			if editable && hov.Hovered() {
				at(gtx, image.Pt(tw+gtx.Dp(8), 0), image.Pt(editW, h), func(gtx layout.Context) layout.Dimensions {
					return clickArea(gtx, a.clicks.get("hdr.topicedit"), func(gtx layout.Context, s image.Point, over bool) {
						if over { // :hover text-decoration underline
							y := h/2 + gtx.Dp(7)
							theme.Fill(gtx, image.Rect(0, y, s.X, y+max(1, gtx.Dp(1))), c.Link)
						}
					}, func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
							return a.th.Label(gtx, 13, theme.Regular, c.Link, "Edit")
						})
					})
				})
			}
			return layout.Dimensions{Size: r.Size()}
		})
	})
	if clicked(a.clicks.get("hdr.topic")) || clicked(a.clicks.get("hdr.topicedit")) {
		a.openTopicDialog(ch, conv)
	}
}

// hdrTopicRect is the last header topic box, in the pane's coordinates
// (tests hover it).
var hdrTopicRect image.Rectangle

// headerTopicWidth is the topic line's natural width.
func (a *App) headerTopicWidth(gtx layout.Context, conv *convInfo) int {
	if len(conv.TopicSpans) == 0 {
		return a.th.Measure(gtx, 13, theme.Regular, oneLine(conv.Topic))
	}
	w := 0
	es := gtx.Sp(13 * 18 / 13)
	for _, s := range conv.TopicSpans {
		if s.Kind == richtext.Break {
			break
		}
		if s.Kind == richtext.Emoji && s.URL != "" {
			w += es + gtx.Dp(1)
			continue
		}
		wt := theme.Bold
		if s.Style&richtext.Bold == 0 {
			wt = theme.Regular
		}
		w += a.th.Measure(gtx, 13, wt, strings.ReplaceAll(s.Text, "\n", " "))
	}
	return w
}

// openTopicDialog is openChannelTopicDialog: Slack's "Edit topic".
func (a *App) openTopicDialog(ch string, conv *convInfo) {
	if a.Core() == nil || conv == nil {
		return
	}
	name := a.convTitle(conv, ch)
	if conv.Kind == "channel" || conv.Kind == "private" {
		name = "#" + conv.Name
	}
	a.ShowOverlay(popups.NewTopicDialog(a, ch, conv.Kind, name, conv.Topic))
}

// inlineSpans draws rich spans on one line (topic, previews): text, links
// and mentions as labels, emoji as images; ellipsized at the max width.
func (a *App) inlineSpans(gtx layout.Context, spans []richtext.Span, size unit.Sp, col color.NRGBA, h int) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	x := 0
	es := gtx.Sp(size * 18 / 13)
	ell := a.th.Measure(gtx, size, theme.Regular, "…")
	for _, s := range spans {
		if s.Kind == richtext.Break {
			break
		}
		if s.Kind == richtext.Emoji && s.URL != "" {
			if x+es > maxW {
				break
			}
			at(gtx, image.Pt(x, (h-es)/2), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
				if img, ok := a.images.Get(s.URL, es*2); ok {
					return theme.Image(gtx, img, image.Pt(es, es), 0)
				}
				return layout.Dimensions{Size: image.Pt(es, es)}
			})
			x += es + gtx.Dp(1)
			continue
		}
		txt := strings.ReplaceAll(s.Text, "\n", " ")
		if i := strings.IndexByte(txt, '\n'); i >= 0 {
			txt = txt[:i]
		}
		if txt == "" {
			continue
		}
		w := theme.Bold
		if s.Style&richtext.Bold == 0 {
			w = theme.Regular
		}
		fg := col
		switch s.Kind {
		case richtext.Link, richtext.User, richtext.Channel, richtext.Usergroup, richtext.Broadcast:
			fg = a.th.C.Link
		}
		tw := a.th.Measure(gtx, size, w, txt)
		trunc := x+tw > maxW
		if trunc {
			tw = maxW - x - ell
			if tw <= 0 {
				break
			}
		}
		at(gtx, image.Pt(x, 0), image.Pt(tw+ell, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return a.th.Layout(gtx, theme.TextStyle{Size: size, Weight: w, Italic: s.Style&richtext.Italic != 0, Color: fg, MaxLines: 1}, txt)
			})
		})
		x += tw
		if trunc {
			break
		}
	}
	return layout.Dimensions{Size: image.Pt(maxW, h)}
}

func oneLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

type chTab struct {
	key, label, icon string
	fileID           string
	unread           bool // Slack's blue dot on the tab icon (unopened canvas)
	// spans is the label with its :emoji: codes resolved (Slack draws a
	// canvas tab's title through its Emoji text renderer), bold; nil for a
	// label without emoji.
	spans []richtext.Span
}

// tabLabelSpans resolves a tab label's :emoji: codes to emoji spans, as
// Slack's tab label does; nil when the label has none that resolve.
func tabLabelSpans(label string, emoji func(name string) (url, text string, ok bool)) []richtext.Span {
	if strings.Count(label, ":") < 2 || emoji == nil {
		return nil
	}
	var out []richtext.Span
	found := false
	text := func(t string) {
		if t == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Kind == richtext.Text {
			out[n-1].Text += t
			return
		}
		out = append(out, richtext.Span{Kind: richtext.Text, Text: t, Style: richtext.Bold})
	}
	rest := label
	for {
		i := strings.IndexByte(rest, ':')
		if i < 0 {
			break
		}
		j := strings.IndexByte(rest[i+1:], ':')
		if j < 0 {
			break
		}
		name := rest[i+1 : i+1+j]
		if name == "" || strings.ContainsAny(name, " \t") {
			text(rest[:i+1])
			rest = rest[i+1:]
			continue
		}
		url, uni, ok := emoji(name)
		if !ok || (url == "" && uni == "") {
			text(rest[:i+1])
			rest = rest[i+1:]
			continue
		}
		text(rest[:i])
		found = true
		if url != "" {
			out = append(out, richtext.Span{Kind: richtext.Emoji, Text: ":" + name + ":", URL: url})
		} else {
			text(uni)
		}
		rest = rest[i+2+j:]
	}
	text(rest)
	if !found {
		return nil
	}
	return out
}

// spansWidth is inlineSpans' width for spans at size (emoji at 18/13 of
// the text size plus a 1px gap).
func (a *App) spansWidth(gtx layout.Context, spans []richtext.Span, size unit.Sp) int {
	w := 0
	es := gtx.Sp(size * 18 / 13)
	for _, s := range spans {
		if s.Kind == richtext.Emoji && s.URL != "" {
			w += es + gtx.Dp(1)
			continue
		}
		wt := theme.Regular
		if s.Style&richtext.Bold != 0 {
			wt = theme.Bold
		}
		w += a.th.Measure(gtx, size, wt, s.Text)
	}
	return w
}

func (a *App) channelTabs(ch string) []chTab {
	tabs := []chTab{{key: "messages", label: "Messages", icon: "threads"}}
	if a.data != nil {
		tabs = append(tabs, a.data.tabs(ch)...)
	}
	return tabs
}

func (a *App) layoutChannelTabs(gtx layout.Context, ch string) layout.Dimensions {
	sz := gtx.Constraints.Max
	c := &a.th.C
	x := gtx.Dp(16)
	cur := a.chTab[ch]
	for i, t := range a.channelTabs(ch) {
		i, t := i, t
		t.label = i18n.T("tabbed-channel", t.label) // the standard tabs are Slack's; a bookmark's own name misses and stays
		lw := a.th.Measure(gtx, 13, theme.Bold, t.label)
		if t.spans != nil {
			lw = a.spansWidth(gtx, t.spans, 13)
		}
		w := gtx.Dp(8) + gtx.Dp(16) + gtx.Dp(4) + lw + gtx.Dp(8)
		if x+w > sz.X-gtx.Dp(48) {
			break
		}
		btn := a.clicks.get("tab." + ch + "." + t.key)
		at(gtx, image.Pt(x, 0), image.Pt(w, sz.Y), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hov bool) {
				if hov && i != cur {
					theme.RRect(gtx, image.Rect(0, gtx.Dp(5), s.X, s.Y-gtx.Dp(5)), gtx.Dp(6), c.BgHover)
				}
				if i == cur {
					theme.Fill(gtx, image.Rect(0, s.Y-gtx.Dp(2), s.X, s.Y), c.Accent)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				col := c.FgMuted
				if i == cur {
					col = c.Fg
					if a.th.Dark {
						col = color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff} // Slack: --dt_color-content-pry
					}
				}
				icon := t.icon
				if t.key == "messages" && i == cur {
					icon = "messages-filled" // Slack fills the active Messages bubble
				} else if i == cur && icons.Has(icon+"-filled") {
					icon += "-filled" // and the active Home (app DMs) etc.
				}
				at(gtx, image.Pt(gtx.Dp(8), (sz.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icon)(gtx, 16, col)
				})
				if t.unread {
					// Slack's p-channel_tab_unread_badge: 12px, 2px ring in the
					// header background around the blue dot, on the icon's corner.
					ctr := image.Pt(gtx.Dp(8+16-2), (sz.Y-gtx.Dp(16))/2+gtx.Dp(2))
					theme.Circle(gtx, ctr, gtx.Dp(12), c.Bg)
					theme.Circle(gtx, ctr, gtx.Dp(8), c.Link)
				}
				at(gtx, image.Pt(gtx.Dp(28), 0), image.Pt(lw+2, sz.Y), func(gtx layout.Context) layout.Dimensions {
					if t.spans != nil {
						return a.inlineSpans(gtx, t.spans, 13, col, sz.Y)
					}
					return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Bold, col, t.label)
					})
				})
				return layout.Dimensions{Size: image.Pt(w, sz.Y)}
			})
		})
		if clicked(btn) {
			if a.chTab == nil {
				a.chTab = map[string]int{}
			}
			if t.key == "messages" {
				a.chTab[ch] = 0
			} else if (t.icon == "canvas" || t.icon == "list-tab") && t.fileID != "" {
				a.chTab[ch] = i // the canvas or list opens in the channel's body
			} else {
				a.chTab[ch] = 0
				a.openTab(ch, t.key)
			}
		}
		x += w + gtx.Dp(4)
	}
	if ci := a.model().conv(ch); ci != nil && ci.Preview {
		return layout.Dimensions{Size: sz} // Slack: no "+" (add a tab) for a channel you aren't in
	}
	at(gtx, image.Pt(x+gtx.Dp(4), (sz.Y-gtx.Dp(28))/2), image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, a.clicks.get("tab.add"), "plus", 28, 18, c.FgMuted, c.BgHover, 6)
	})
	if clicked(a.clicks.get("tab.add")) {
		a.addTabMenu(ch) // mount_features_canvas.go
	}
	return layout.Dimensions{Size: sz}
}

// convTitle is the header name: channel name, DM partner, or group list.
func (a *App) convTitle(conv *convInfo, ch string) string {
	if conv == nil {
		return ch
	}
	return conv.displayName()
}

// headerTitleMenu opens the channel menu at the pointer on a right-click
// (or macOS Ctrl+click) of the header's title (r, header coordinates):
// Slack wraps the title in ChannelContextMenu{id}.
func (a *App) headerTitleMenu(gtx layout.Context, ch string, r image.Rectangle) {
	tag := a.hdrCtx[ch]
	if tag == nil {
		if a.hdrCtx == nil {
			a.hdrCtx = map[string]*int{}
		}
		tag = new(int)
		a.hdrCtx[ch] = tag
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			a.ShowOverlay(popups.NewChannelContextMenu(a, a.ptrWin, ch))
		}
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
}
