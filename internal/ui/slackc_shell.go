package ui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The shell's Slack component boundaries and class tags for Taut plugins
// (docs/PLUGINS-components.md §1.6, §1.11, §2.3, §5): BaseAvatarStack in
// the channel header, HelpButton in the top nav, ListChannelEntity and
// BasePresence in the sidebar, Tabs in Preferences, plus the ancestor
// classes descendant selectors need. Unpatched and with no CSS active,
// every boundary is an atomic load and a direct call.

// Ancestor classes (§5.2): the chain .p-client_container > .p-ia… must be
// on the element stack for plugin selectors to match.
var (
	clsClient      = slackc.Class("p-client_container")
	clsIA          = slackc.Class("p-ia4_client_container p-ia4_client p-ia")
	clsTopNav      = slackc.Class("p-ia4_top_nav p-top_nav").Mods(invertedScope)
	clsTopNavLeft  = slackc.Class("p-ia4_top_nav__left_container")
	clsTopNavRight = slackc.Class("p-ia4_top_nav__right_container p-top_nav__right").FlexItems() // a flex row: a HelpButton patch's buttons sit beside it
	clsHelp        = slackc.Class("c-button-unstyled p-top_nav__button p-top_nav__help", "data-qa", "top-nav-help-button", "aria-label", "Help").Tag("button")
	clsPrimaryView = slackc.Class("p-workspace__primary_view p-view_contents p-view_contents--primary")
	clsSecondary   = slackc.Class("p-workspace__secondary_view p-view_contents p-view_contents--secondary")
	clsViewHeader  = slackc.Class("p-view_header p-view_header--tiles")
	clsMessagePane = slackc.Class("p-message_pane p-message_pane--classic-nav")
	clsAvatarStack = slackc.Class("c-button-unstyled p-avatar_stack--details", "data-qa", "avatar_stack").Tag("button")
	clsAvatarCount = slackc.Class("p-avatar_stack--details__count").Tag("span")

	clsSidebar = slackc.Class("p-channel_sidebar p-channel_sidebar--classic-nav").Mods(invertedScope)
	clsSbList  = slackc.Class("p-channel_sidebar__list p-channel_sidebar__static_list", "role", "tree")
	// every sidebar row; data-item-key is the dynamic attribute.
	clsSbItem = slackc.Class("p-channel_sidebar__static_list__item", "role", "treeitem")
	// the top links (Threads, Huddles, …): Slack's seasonal hashed class
	// prefix, so SlackFixes' [class*="spookySidebarLinkContainer__"] rule
	// reaches them.
	clsSbLink    = slackc.Class("p-channel_sidebar__link spookySidebarLinkContainer__clack").Tag("a")
	clsSbHeading = slackc.Class("p-channel_sidebar__section_heading", "data-qa", "section_heading")
	clsSbName    = slackc.Class("p-channel_sidebar__name").Tag("span")
	clsSbIcon    = slackc.Class("p-channel_sidebar__channel_icon_prefix c-channel_icon")
	clsPresence  = slackc.Class("c-presence c-avatar__presence").Tag("span").
			Mods("c-presence--active", "c-presence--away")
)

// clsSbChannel is .p-channel_sidebar__channel per channel type and
// selection (static attributes); the channel id is EnterAttr's.
var clsSbChannel = func() (t [5][2]*slackc.ClassDef) {
	for k, typ := range sbChanTypes {
		for s, sel := range [2]string{"false", "true"} {
			cls := "p-channel_sidebar__channel"
			if s == 1 {
				cls += " p-channel_sidebar__channel--selected"
			}
			t[k][s] = slackc.Class(cls,
				"data-qa", "channel-sidebar-channel",
				"data-qa-channel-sidebar-channel-type", typ,
				"data-qa-channel-sidebar-channel-is-selected", sel,
			).Tag("div").Mods("p-channel_sidebar__channel--unread", "p-channel_sidebar__channel--muted")
		}
	}
	return
}()

var sbChanTypes = [5]string{"channel", "private", "im", "mpim", "app"}

func sbChanType(kind string) int {
	for i, t := range sbChanTypes {
		if t == kind {
			return i
		}
	}
	return 0
}

// hoverState is css.StateHover when hov.
func hoverState(hov bool) css.State {
	if hov {
		return css.StateHover
	}
	return 0
}

// measured lays out w and returns its recorded ops with its size, for
// right-aligned boundaries whose width a plugin may change.
func measured(gtx layout.Context, w layout.Widget) (op.CallOp, layout.Dimensions) {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	return m.Stop(), d
}

// place adds a recorded widget at p.
func place(gtx layout.Context, call op.CallOp, p image.Point) {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

// ---- BaseAvatarStack (#47, HumanCount) ----

// hdrStack is the channel header facepile + member count.
type hdrStack struct {
	a    *App
	ch   string
	conv *convInfo
	btn  int // px
}

func (h hdrStack) faces() []string {
	faces := h.conv.Members
	if len(faces) > 3 {
		faces = faces[:3]
	}
	return faces
}

// props: Slack's BaseAvatarStack shape (props.md): channelId, memberCount,
// member1..3 + memberName/IsActive1..5, display flags, callbacks.
func (h hdrStack) props() slackc.Props {
	a := h.a
	p := slackc.Props{
		"$key":                                 h.ch,
		"channelId":                            h.ch,
		"channelIsIm":                          h.conv.Kind == "im",
		"memberCount":                          h.conv.NumMembers,
		"showFiveAvatars":                      false,
		"showIconOnly":                         false,
		"showPresence":                         false,
		"hideOutline":                          false,
		"hasHuddleParticipants":                false,
		"isQuipDoc":                            false,
		"experimentChannelIaMembershipGroupOn": false,
		"openAboutChannelModal":                func() { a.OpenDetails(h.ch) },
	}
	c := a.Core()
	for i := 0; i < 5; i++ {
		n := itoa(i + 1)
		p["memberIsActive"+n] = false
		if i >= len(h.conv.Members) || i >= 3 {
			continue
		}
		id := h.conv.Members[i]
		m := slackc.Props{"id": id}
		name := ""
		if c != nil {
			if u, ok := c.User(id); ok {
				name = u.Name
				m["name"] = u.Handle
				m["real_name"] = u.RealName
				m["is_bot"] = u.IsBot
				m["deleted"] = u.Deleted
				m["is_restricted"] = u.Guest
				m["profile"] = slackc.Props{"display_name": u.Name, "real_name": u.RealName, "image_24": u.Avatar, "image_48": u.Avatar}
				p["memberIsActive"+n] = u.Presence == "active"
			}
		}
		p["member"+n] = m
		p["memberName"+n] = name
	}
	return p
}

// layout draws the facepile at the origin. Under a patch it honours
// memberCount (HumanCount), member1..3 ids, a string label replacing the
// count, and showIconOnly.
func (h hdrStack) layout(gtx layout.Context, p slackc.Props) layout.Dimensions {
	a := h.a
	c := &a.th.C
	btn := h.btn
	cnt := ""
	faces := h.faces()
	if p == nil {
		cnt = itoa(h.conv.NumMembers)
	} else {
		cnt = p.StrOr("label", itoa(p.IntOr("memberCount", h.conv.NumMembers)))
		if p.Has("member1") {
			var ids [3]string
			n := 0
			for i := 0; i < 3; i++ {
				if id := p.Map("member"+itoa(i+1)).StrOr("id", ""); id != "" {
					ids[n] = id
					n++
				}
			}
			faces = ids[:n]
		}
		if p.Bool("showIconOnly") {
			faces = nil
		}
	}
	if len(faces) == 0 && h.conv.NumMembers > 0 && (p == nil || !p.Bool("showIconOnly")) && len(h.conv.Members) == 0 {
		// members not fetched yet: gray rounded squares (avatar()'s
		// skeleton for an empty URL), never just the count
		var blank [3]string
		faces = blank[:min(3, h.conv.NumMembers)]
	}
	st := clsAvatarStack.Enter(gtx, hoverState(a.clicks.get("hdr.members").Hovered()), 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	cs := clsAvatarCount.Enter(gtx, 0, 0)
	fg := cs.Color(st.Color(c.FgMuted))
	fs := cs.FontSize(13)
	cs.Leave()
	cw := a.th.Measure(gtx, fs, theme.Bold, cnt)
	pw := gtx.Dp(8) + len(faces)*gtx.Dp(16) + gtx.Dp(6) + cw + gtx.Dp(8)
	if len(faces) > 0 {
		pw += gtx.Dp(4)
	}
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("hdr.members"), func(gtx layout.Context, s image.Point, hov bool) {
			// Slack 2026 (--polish): no outline, only the hover fill.
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), c.BgHover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			fx := gtx.Dp(6)
			for _, uid := range faces {
				url := a.userAvatar(uid)
				at(gtx, image.Pt(fx, (btn-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					s := gtx.Dp(20)
					theme.RRect(gtx, image.Rect(-gtx.Dp(2), -gtx.Dp(2), s+gtx.Dp(2), s+gtx.Dp(2)), gtx.Dp(6), c.Bg)
					return a.avatar(gtx, url, "", s, gtx.Dp(4))
				})
				fx += gtx.Dp(16)
			}
			if len(faces) > 0 {
				fx += gtx.Dp(8)
			}
			at(gtx, image.Pt(fx, 0), image.Pt(cw+2, btn), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, btn, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, fs, theme.Bold, fg, cnt)
				})
			})
			return layout.Dimensions{Size: image.Pt(pw, btn)}
		})
	})
}

// ---- HelpButton (#48, StreamerMode, whatsNew) ----

type navHelp struct {
	a   *App
	org image.Point // window position (chrome hover composite)
	b   int         // px
}

// layout is Clack's help button. Plugins render siblings before it
// (<>{button}<Original/></>), so the caller right-aligns whatever width
// comes back.
func (h navHelp) layout(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	a := h.a
	btn := a.clicks.get("nav.help")
	st := clsHelp.Enter(gtx, hoverState(btn.Hovered()), 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	col := st.Color(a.th.C.ChromeText)
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(h.b, h.b)}
		d := a.chromeButton(gtx, h.org, btn, "help", 26, 20, col, 4)
		a.helpBadge(gtx, d.Size.X) // a downloaded update (update_ui.go)
		return d
	})
}

// ---- ListChannelEntity (#49, PrivateChannel) ----

// sbEntity is a sidebar conversation's icon (or avatar + presence) and
// name, drawn from the row's icon column. Under a patch it honours name
// (the label) and isPrivate/isMpim (the icon).
type sbEntity struct {
	a      *App
	conv   *convInfo
	label  string
	suffix string
	fg     color.NRGBA
	weight font.Weight
	sel    bool
	rowH   int
	width  int // px from the icon column to the badge
}

// cssWeight is the CSS font-weight (100..900) when a rule set one, as a
// Gio weight (relative to 400), else def.
func cssWeight(st slackc.Styled, def font.Weight) font.Weight {
	return font.Weight(st.FontWeight(int(def)+400) - 400)
}

func (e sbEntity) props() slackc.Props {
	k := e.conv.Kind
	return slackc.Props{
		"$key":         e.conv.ID,
		"id":           e.conv.ID,
		"name":         e.label,
		"isPrivate":    k == "private",
		"isMpim":       k == "mpim",
		"isIm":         k == "im" || k == "app",
		"isMember":     true,
		"isMuted":      e.conv.Muted,
		"isArchived":   e.conv.Archived,
		"isUnread":     e.conv.Unread,
		"isSelected":   e.sel,
		"mentionCount": e.conv.Mentions,
	}
}

func (e sbEntity) layout(gtx layout.Context, p slackc.Props) layout.Dimensions {
	a := e.a
	c := &a.sbPal
	conv := e.conv
	rowH := e.rowH
	label := e.label
	kind := conv.Kind
	if p != nil {
		label = p.StrOr("name", label)
		switch {
		case p.Bool("isPrivate"):
			kind = "private"
		case p.Bool("isMpim"):
			kind = "mpim"
		case kind == "private" && p.Has("isPrivate"):
			kind = "channel"
		}
	}
	fg := e.fg
	iy := (rowH - gtx.Dp(16)) / 2
	if (kind == "im" || kind == "app") && !uipref.Get().SidebarAvatars {
		// show_sidebar_avatars off: BaseChannelPrefix draws the presence
		// badge in the icon's place (BaseChannelPrefixPresence)
		s := gtx.Dp(10)
		at(gtx, image.Pt((gtx.Dp(16)-s)/2, (rowH-s)/2), image.Pt(gtx.Dp(120), s), func(gtx layout.Context) layout.Dimensions {
			return slackc.Component(gtx, "BasePresence", sbPresence{a: a, active: conv.Active, sel: e.sel, self: conv.Self, key: conv.ID}, sbPresence.props, sbPresence.layout)
		})
	} else if kind == "im" || kind == "app" {
		at(gtx, image.Pt(0, iy), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return a.avatar(gtx, conv.Avatar, conv.Name, gtx.Dp(16), gtx.Dp(4))
		})
		if kind == "im" && !conv.Bot {
			s := gtx.Dp(10)
			ctr := image.Pt(gtx.Dp(15), iy+gtx.Dp(14))
			at(gtx, ctr.Sub(image.Pt(s/2, s/2)), image.Pt(gtx.Dp(120), s), func(gtx layout.Context) layout.Dimensions {
				return slackc.Component(gtx, "BasePresence", sbPresence{a: a, active: conv.Active, sel: e.sel, self: conv.Self, key: conv.ID}, sbPresence.props, sbPresence.layout)
			})
		}
	} else if conv.Code != nil {
		// a code channel: AgentChannelStatusDot (codechannel.go) in place of the #
		at(gtx, image.Pt(0, iy), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return a.codeStatusDot(gtx, conv, fg, conv.Muted)
		})
	} else {
		icon := "hash"
		switch kind {
		case "private":
			icon = "lock"
		case "mpim":
			icon = "people"
		default:
			// BaseChannelPrefixIcon: an unread channel that isn't muted
			// (the row's --unread, bold) gets channel-filled
			if e.weight == theme.Black && conv.Unread && !conv.Muted {
				icon = "hash-filled"
			}
		}
		is := clsSbIcon.Enter(gtx, 0, 0)
		if !is.Hidden() {
			ic := is.Color(fg)
			at(gtx, image.Pt(0, iy), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return is.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icon)(gtx, 16, ic)
				})
			})
		}
		is.Leave()
	}
	tx := gtx.Dp(24)
	tw := e.width - tx
	ns := clsSbName.Enter(gtx, 0, 0)
	size := ns.FontSize(15)
	nw, trunc := 0, false
	if !ns.Hidden() {
		nfg := ns.Color(fg)
		weight := cssWeight(ns, e.weight)
		nw = a.th.Measure(gtx, size, weight, label)
		trunc = nw > tw
		if nw > tw {
			nw = tw
		}
		at(gtx, image.Pt(tx, gtx.Dp(1)), image.Pt(nw, rowH) /* Slack: text 1px below center */, func(gtx layout.Context) layout.Dimensions {
			return ns.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, size, weight, nfg, label)
				})
			})
		})
	}
	ns.Leave()
	if conv.Code != nil { // BaseChannelName's code channel tooltip (codechannel.go)
		a.codeRowTooltip(gtx, conv, label, image.Rect(tx, 0, tx+nw, rowH), trunc)
	}
	if conv.StatusEmoji != "" && nw+gtx.Dp(26) < tw {
		// status emoji after the name (Slack: 16px, 6px gap)
		es := gtx.Dp(16)
		at(gtx, image.Pt(tx+nw+gtx.Dp(6), (rowH-es)/2), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
			if img, ok := a.images.Get(conv.StatusEmoji, es*2); ok {
				return theme.Image(gtx, img, image.Pt(es, es), 0)
			}
			return layout.Dimensions{Size: image.Pt(es, es)}
		})
		nw += es + gtx.Dp(6)
	}
	if e.suffix != "" && nw+gtx.Dp(8) < tw {
		sc := theme.Over(theme.WithAlpha(a.th.C.SidebarText, 0xa3), a.sbBase) // rgba(249,237,255,.6)
		if e.sel {
			sc = theme.Over(theme.WithAlpha(c.SidebarActiveText, 0x99), c.SidebarActive)
		}
		at(gtx, image.Pt(tx+nw+gtx.Dp(6), gtx.Dp(1)), image.Pt(tw-nw-gtx.Dp(6), rowH), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, size, theme.Regular, sc, e.suffix)
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(e.width, rowH)}
}

// ---- BasePresence (#57, LastSeen) in the sidebar ----

type sbPresence struct {
	a                 *App
	active, sel, self bool
	key               string
}

// props: Slack's BasePresence shape (props.md).
func (s sbPresence) props() slackc.Props {
	return slackc.Props{
		"$key":            "sb:" + s.key,
		"className":       "c-presence c-avatar__presence",
		"isActive":        s.active,
		"isDnd":           false,
		"isSelf":          s.self,
		"isSystemUser":    false,
		"userType":        "member",
		"inline":          false,
		"inheritIconSize": false,
		"showText":        false,
	}
}

// layout draws the 10dp presence badge at the origin; under a patch it
// honours isActive and showText ("Active"/"Away" after the dot).
func (s sbPresence) layout(gtx layout.Context, p slackc.Props) layout.Dimensions {
	a := s.a
	active := s.active
	text := false
	if p != nil {
		if p.Has("isActive") {
			active = p.Bool("isActive")
		}
		text = p.Bool("showText")
	}
	var mods uint64 = 2
	if active {
		mods = 1
	}
	st := clsPresence.Enter(gtx, 0, mods)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	d := gtx.Dp(10)
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		a.presenceDot(gtx, image.Pt(d/2, d/2), active, s.sel)
		if !text {
			return layout.Dimensions{Size: image.Pt(d, d)}
		}
		t := "Away"
		if active {
			t = "Active"
		}
		fg := st.Color(a.sbPal.SidebarText)
		ld := at(gtx, image.Pt(d+gtx.Dp(4), 0), image.Pt(gtx.Constraints.Max.X, d), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, d, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, st.FontSize(13), theme.Regular, fg, t)
			})
		})
		return layout.Dimensions{Size: image.Pt(d+gtx.Dp(4)+ld.Size.X, d)}
	})
}
