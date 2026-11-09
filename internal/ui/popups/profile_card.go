package popups

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/hovercard"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Measured from app.slack.com (2026-11, profile hover card): 340 wide,
// radius 8, popover shadow (ring 1px rgba(29,28,29,.13) + 0 4px 12px
// rgba(0,0,0,.12)). Primary block padding 16: avatar 72 (radius 12), 12px
// gap, name 15/900 followed by the presence indicator, pronouns and title
// 15px #454447 (title wraps), status emoji + text. Secondary block: border
// top 1px rgba(29,28,29,.13), padding 0 16 16; clock icon 20 + "12:39 PM
// local time" 15px (margin-top 16); buttons row (margin-top 12): [Message]
// [Huddle ▾] [VIP] ⋮, outline, 28 tall, radius 4, 13/700, 16px icons.

// OpenFullProfile opens userID's full profile in the secondary pane. The ui
// root sets it (popups cannot reach the pane itself); while it is nil the
// card and its menu toast instead.
var OpenFullProfile func(userID string)

// ProfileCard is the profile hover/click card.
type ProfileCard struct {
	base
	h      Host
	id     string
	anchor image.Point
	scrim  scrim
	block  blocker
	rect   image.Rectangle
	focus  bool

	// hover: the card opened by resting on a trigger (hovercard). It has no
	// scrim, never takes focus and sits above anchorR (hovercard.Place).
	hover   bool
	anchorR image.Rectangle
	sub     Overlay // a menu the card opened (holds a hover card open)

	asked   bool // EnsureUsers started
	missing bool // EnsureUsers finished without the user

	bot string   // a bot with no user (a message's bot_id): AppHoverCard
	app appState // the app card (app_card.go)

	// Slack Connect: the external member's team (AppExternalTeamBanner)
	teamAsked          bool
	teamName, teamIcon string

	avatarBtn, nameBtn                 widget.Clickable
	msgBtn, huddleBtn, vipBtn, moreBtn widget.Clickable
	moreLocal                          image.Rectangle // kebab rect, card coords
	huddleLocal                        image.Rectangle // Huddle button rect, card coords
	key                                string          // slackc instance key
}

// NewProfileCard opens the card with its top-left corner at anchor (window
// coords), flipping/clamping at the window edges.
func NewProfileCard(h Host, anchor image.Point, userID string) *ProfileCard {
	p := &ProfileCard{h: h, id: userID, anchor: anchor, key: nextKey("card")}
	pfSubscribe(h, userID)
	return p
}

// NewHoverProfileCard is the card the ui root draws while the pointer
// rests on a profile trigger whose window bounds are anchor (Slack's
// ProfileHoverTrigger: the card's bottom-left corner 12px above the
// trigger's top-left, flipped below it when there's no room). The root
// draws it under the overlays and closes it through hovercard.
func NewHoverProfileCard(h Host, anchor image.Rectangle, userID string) *ProfileCard {
	return NewHoverCard(h, anchor, userID, "")
}

// NewHoverCard is NewHoverProfileCard for a trigger that names a user, a
// bot (botID: a message's bot_id with no user), or both: an app or bot
// gets Slack's AppHoverCard (app_card.go), a person the member card.
func NewHoverCard(h Host, anchor image.Rectangle, userID, botID string) *ProfileCard {
	p := &ProfileCard{h: h, id: userID, bot: botID, anchorR: anchor, anchor: anchor.Min, hover: true, key: nextKey("hovercard")}
	pfSubscribe(h, userID)
	return p
}

// IsApp reports whether the card shows Slack's AppHoverCard (after a
// Layout).
func (p *ProfileCard) IsApp() bool {
	u, ok := lookupUser(p.h, p.id)
	return p.isApp(u, ok)
}

// place is the card's top-left for a card of size sz: hover cards above
// the trigger (hovercard.Place), click cards at the click.
func (p *ProfileCard) place(gtx layout.Context, sz, win image.Point) image.Point {
	if p.hover {
		return hovercard.Place(p.anchorR, sz, win, dp(gtx, hovercard.OffsetY), dp(gtx, hovercard.AllowanceX), dp(gtx, hovercard.AllowanceY))
	}
	return place(pointRect(p.anchor), sz, win, dp(gtx, 8), Below)
}

// ensureTeam reads an external member's team once (team.info).
func (p *ProfileCard) ensureTeam(u core.User) {
	c := p.h.Core()
	if p.teamAsked || c == nil || !u.External || u.TeamID == "" {
		return
	}
	p.teamAsked = true
	team := u.TeamID
	if name, icon, ok := c.Team(team, 20); ok {
		p.teamName, p.teamIcon = name, icon
		return
	}
	type res struct{ name, icon string }
	run(p.h, func(ctx context.Context) (res, error) {
		c.EnsureTeam(ctx, team)
		name, icon, _ := c.Team(team, 20)
		return res{name, icon}, nil
	}, func(r res, _ error) { p.teamName, p.teamIcon = r.name, r.icon })
}

// Rect is the card's bounds in window coordinates (after a Layout).
func (p *ProfileCard) Rect() image.Rectangle { return p.rect }

// Holding reports whether a menu the card opened is still showing (a hover
// card stays open under it, like Slack's: the menu is inside the trigger).
func (p *ProfileCard) Holding() bool { return p.sub != nil && !p.sub.Dismissed() }

// UserID is the member the card shows.
func (p *ProfileCard) UserID() string { return p.id }

// Layout draws the card.
func (p *ProfileCard) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if p.done {
		return layout.Dimensions{}
	}
	th := p.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	if !p.hover {
		stacked(gtx, p)
		if !p.focus {
			gtx.Execute(key.FocusCmd{Tag: p})
			p.focus = true
		}
		if isTop(p) && escape(gtx, nil) {
			p.Dismiss()
			return layout.Dimensions{}
		}
		if p.scrim.Layout(gtx) {
			p.Dismiss()
			return layout.Dimensions{}
		}
	}
	u, ok := lookupUser(p.h, p.id)
	if ok && u.Deleted {
		dc := *cl
		dc.Bg = cl.BgAlt // .p-member_profile_hover_card__container_deleted: #f8f8f8
		dc.Fg = theme.WithAlpha(cl.Fg, 0xb3)
		cl = &dc
	}
	if !ok && !p.asked && p.h.Core() != nil && p.id != "" {
		p.asked = true
		pfEnsure(p.h, p.id, func(found bool) { p.missing = !found })
	}
	if p.hover && p.isApp(u, ok) {
		p.rect = p.layoutApp(gtx, th, cl, win)
		return layout.Dimensions{Size: win}
	}
	if ok {
		p.ensureTeam(u)
		p.clicks(gtx, u)
		if p.done {
			return layout.Dimensions{}
		}
		pfTick(gtx)
	}

	w := dp(gtx, 340)
	if mw := win.X - 2*dp(gtx, 8); w > mw {
		w = mw
	}
	call, d := record(loose(gtx, image.Pt(w, win.Y)), func(gtx layout.Context) layout.Dimensions {
		if !ok {
			return p.content(gtx, th, cl, u, ok, w, nil)
		}
		hover := func() slackc.Props { return p.hoverProps(th, cl, u, w) }
		content := func(gtx layout.Context, pp slackc.Props) layout.Dimensions {
			return p.content(gtx, th, cl, u, ok, w, pp)
		}
		// Slack's ProfileHoverCard (the trigger's card) renders
		// MemberProfileHoverCard: the inner card is its children slot, so
		// context the outer one provides (LastSeen) reaches the rows.
		return slackc.RenderLazy(gtx, "ProfileHoverCard", func() slackc.Props {
			return slackc.Props{
				"$key":                     p.key + ":outer",
				"memberId":                 u.ID,
				"shouldStopPropagation":    false,
				"hideButtons":              false,
				"disableMemberProfileLink": false,
				"children":                 slackc.Child("MemberProfileHoverCard", hover(), content),
			}
		}, func(gtx layout.Context, pp slackc.Props) layout.Dimensions {
			return childrenOr(gtx, pp, "MemberProfileHoverCard", hover, content)
		})
	})
	pos := p.place(gtx, d.Size, win)
	p.rect = image.Rectangle{Min: pos, Max: pos.Add(d.Size)}
	rad := dp(gtx, 8)
	st := pfCardClass.Enter(gtx, 0, 0)
	if !st.Hidden() {
		styledCard(gtx, st, cl, pos, d.Size, rad, false, func(gtx layout.Context) {
			local := image.Rectangle{Max: d.Size}
			p.block.Layout(gtx, local)
			cs := clipRRect(gtx, local, rad)
			call.Add(gtx.Ops)
			cs.Pop()
		})
	}
	st.Leave()
	if !p.hover {
		keyTarget(gtx, p, p.rect)
	}
	return layout.Dimensions{Size: win}
}

func (p *ProfileCard) clicks(gtx layout.Context, u core.User) {
	h := p.h
	if p.avatarBtn.Clicked(gtx) || p.nameBtn.Clicked(gtx) {
		p.Dismiss()
		pfOpenFull(h, u.ID)
		return
	}
	if p.msgBtn.Clicked(gtx) {
		p.Dismiss()
		if pfIsSlackbot(u) {
			return
		}
		if pfIsSelf(h, u.ID) {
			pfSetStatus(h)
		} else {
			pfMessage(h, u.ID)
		}
		return
	}
	if p.huddleBtn.Clicked(gtx) && !p.huddleLocal.Empty() {
		if m := pfHuddleMenuAt(gtx, h, u.ID, p.huddleLocal.Add(p.rect.Min), p.Dismiss); m != nil {
			p.sub = m
			h.ShowOverlay(m)
		}
	}
	if p.vipBtn.Clicked(gtx) {
		h.Toast("VIPs aren't supported yet")
	}
	if p.moreBtn.Clicked(gtx) {
		items := pfMoreItems(h, u, true, p.Dismiss)
		m := NewMenuOpts(h, items, pfMenuOpts(u.ID, MenuOpts{Anchor: p.moreLocal.Add(p.rect.Min), Side: Below}))
		p.sub = m
		h.ShowOverlay(m)
	}
}

// hoverProps are MemberProfileHoverCard's props: the primary block is the
// header slot, Slack's MemberProfileBaseEntity (ShinigamiEyes appends to
// the header, LocalTime appends the card's time line to the entity), and
// the clock line the LocalTime slot (LastSeen appends "Last seen" in the
// card's context).
func (p *ProfileCard) hoverProps(th *theme.Theme, cl *Colors, u core.User, w int) slackc.Props {
	return slackc.Props{
		"$key":                     p.key,
		"memberId":                 u.ID,
		"closeProfileCard":         p.Dismiss,
		"hideButtons":              false,
		"disableMemberProfileLink": false,
		"shouldStopPropagation":    false,
		"header": slackc.Child("MemberProfileBaseEntity", p.entityProps(u), func(gtx layout.Context, _ slackc.Props) layout.Dimensions {
			return p.header(gtx, th, cl, u, true, w)
		}),
		"localTime": slackc.Child("LocalTime", pfLocalTimeProps(u), func(gtx layout.Context, _ slackc.Props) layout.Dimensions {
			return pfIconLine(gtx, th, cl, "clock", pfLocalTimeLabel(p.h, u, gtx.Now), 15, 22)
		}),
	}
}

// entityProps are MemberProfileBaseEntity's props (the card's primary
// block: avatar, name, presence, pronouns, title, status).
func (p *ProfileCard) entityProps(u core.User) slackc.Props {
	h := p.h
	return slackc.Props{
		"$key":                     p.key + ":entity",
		"memberId":                 u.ID,
		"channelId":                "",
		"deletedHoverCard":         u.Deleted,
		"disableMemberProfileLink": false,
		"onNavigate":               func() { p.Dismiss(); pfOpenFull(h, u.ID) },
	}
}

func pfLocalTimeProps(u core.User) slackc.Props {
	return slackc.Props{"member": slackMember(u), "$key": "localtime:" + u.ID}
}

// content draws the card body at the origin (card coords). pp holds the
// plugin's MemberProfileHoverCard props (nil when unpatched).
//
// Slack's MemberProfileHoverCard (client-boot-shared VoM7): the header
// slot, MemberProfileBaseEntity (banner, avatar, name, presence, title,
// pronouns), then .p-member_profile_hover_card__secondary (border-top,
// padding 0 16 16, a 16px ::before spacer): the custom status, then
// Slackbot's "Built by Slack" row or LocalTime (margin-top 4), then
// MemberProfileButtons (margin-top 12, hidden for Slackbot). The spacer's
// margin collapses with the first child's.
func (p *ProfileCard) content(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, ok bool, w int, pp slackc.Props) layout.Dimensions {
	pad := dp(gtx, 16)
	var hd layout.Dimensions
	if e, has := pp.Elem("header"); has {
		hd = slackc.LayoutElement(loose(gtx, image.Pt(w, 1<<20)), e)
	} else if ok {
		hd = slackc.RenderLazy(gtx, "MemberProfileBaseEntity", func() slackc.Props { return p.entityProps(u) },
			func(gtx layout.Context, _ slackc.Props) layout.Dimensions { return p.header(gtx, th, cl, u, ok, w) })
	} else {
		hd = p.header(gtx, th, cl, u, ok, w)
	}
	y := hd.Size.Y

	// secondary block
	fill(gtx, image.Rect(0, y, w, y+1), cl.Ring)
	y++
	y += pad // the ::before spacer
	inner := w - 2*pad
	first := true
	margin := func(m int) {
		if !first {
			y += m
		}
		first = false
	}
	if !ok {
		rrect(gtx, image.Rect(pad, y, pad+dp(gtx, 120), y+dp(gtx, 28)), dp(gtx, 4), cl.Selected)
		y += dp(gtx, 28) + pad
		return layout.Dimensions{Size: image.Pt(w, y)}
	}
	if pfHasStatus(u, gtx.Now) {
		margin(0)
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return pfCardStatus(gtx, p.h, th, cl, u)
		})
		y += d.Size.Y
	}
	slackbot := pfIsSlackbot(u)
	switch {
	case slackbot:
		margin(0)
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return pfIconLine(gtx, th, cl, "slack-logo", "Built by Slack", 15, 22)
		})
		y += d.Size.Y
	case pfHasClock(u):
		margin(dp(gtx, 4))
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return slotOr(gtx, pp, "localTime", "LocalTime", func() slackc.Props { return pfLocalTimeProps(u) },
				func(gtx layout.Context, _ slackc.Props) layout.Dimensions {
					return pfIconLine(gtx, th, cl, "clock", pfLocalTimeLabel(p.h, u, gtx.Now), 15, 22)
				})
		})
		y += d.Size.Y
	}
	if !slackbot {
		margin(dp(gtx, 12))
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return p.buttons(gtx, th, u, inner)
		})
		if !p.moreLocal.Empty() {
			p.moreLocal = p.moreLocal.Add(image.Pt(pad, y))
		}
		if !p.huddleLocal.Empty() {
			p.huddleLocal = p.huddleLocal.Add(image.Pt(pad, y))
		}
		y += d.Size.Y
	}
	y += pad
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// header is MemberProfileBaseEntity's primary block: Slack's
// MemberProfileHoverBanner (Workspace Owner / Workspace Admin /
// Deactivated account) across the top, then avatar, name, presence,
// pronouns and title, full card width, paddings included.
func (p *ProfileCard) header(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, ok bool, w int) layout.Dimensions {
	bh := 0
	if ok {
		// MemberProfileBaseEntity's banners: AppExternalTeamBanner for a
		// Slack Connect member, then MemberProfileHoverBanner
		if u.External {
			bh = pfExternalBanner(gtx, p.h, th, cl, p.teamName, p.teamIcon, w).Size.Y
		}
		func() {
			defer op.Offset(image.Pt(0, bh)).Push(gtx.Ops).Pop()
			bh += pfBanner(gtx, th, cl, u, w).Size.Y
		}()
	}
	defer op.Offset(image.Pt(0, bh)).Push(gtx.Ops).Pop()
	pad := dp(gtx, 16)
	av := dp(gtx, 72)
	colX := pad + av + dp(gtx, 12)
	colW := w - colX - pad
	if colW < 1 {
		colW = 1
	}
	// primary block
	if ok {
		at(gtx, image.Pt(pad, pad), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &p.avatarBtn, nil, func(gtx layout.Context) layout.Dimensions {
				return avatar(gtx, p.h, pfAvatarURL(u, false), 72)
			})
		})
	} else {
		rrect(gtx, image.Rect(pad, pad, pad+av, pad+av), dp(gtx, 12), cl.Selected)
	}
	// Slack's text column has margin: -4px 0 (it starts 4px above the
	// avatar and may hang 4px below it).
	m4 := dp(gtx, 4)
	cd := at(gtx, image.Pt(colX, pad-m4), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(colW, 1<<20)}
		if !ok {
			return pfSkeleton(gtx, th, cl, p.missing, p.id, colW)
		}
		return p.column(gtx, th, cl, u, colW)
	})
	y := bh + pad + maxInt(av, cd.Size.Y-2*m4) + pad
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// column is the text column beside the avatar.
func (p *ProfileCard) column(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, colW int) layout.Dimensions {
	lineH := dp(gtx, 22)
	y := 0
	// name + presence
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return pfNameLine(gtx, p.h, th, &p.nameBtn, u, 15, 22, 10, colW)
	})
	y += lineH
	if !pfBlank(u.Pronouns) {
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(colW, lineH)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, u.Pronouns, 1)
		})
		y += lineH
	}
	if !pfBlank(u.Title) {
		col := cl.Fg2
		if u.Deleted { // .p-member_profile_base_entity__title_deleted: color inherit
			col = cl.Fg
		}
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(colW, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, col, u.Title, 0)
		})
		y += maxInt(d.Size.Y, lineH)
	}
	return layout.Dimensions{Size: image.Pt(colW, maxInt(y, lineH))}
}

func (p *ProfileCard) buttons(gtx layout.Context, th *theme.Theme, u core.User, w int) layout.Dimensions {
	p.huddleLocal = image.Rectangle{}
	var specs []pfBtnSpec
	switch {
	case u.Deleted:
		// MemberProfileButtons for a deactivated member: the message button
		// reads "View Message History"; no huddle (renderSecondaryButton
		// returns null) and no VIP.
		specs = []pfBtnSpec{{c: &p.msgBtn, label: "View Message History", lead: "threads"}}
	case pfIsSelf(p.h, u.ID):
		specs = []pfBtnSpec{{c: &p.msgBtn, label: "Set a status", lead: "emoji"}}
	default:
		specs = []pfBtnSpec{
			{c: &p.msgBtn, label: "Message", lead: "threads"},
			{c: &p.huddleBtn, label: "Huddle", lead: "headphones", caret: true, rect: &p.huddleLocal},
			{c: &p.vipBtn, label: "VIP", lead: "user-plus", vip: true},
		}
	}
	kebab := &p.moreBtn
	if len(specs) > 1 || u.Deleted {
		kebab = nil // Slack's card shows only [Message] [Huddle ▾] [VIP]
	}
	d, kr := pfBtnRow(gtx, th, BtnSmall, specs, kebab, w, false)
	p.moreLocal = kr
	return d
}

// pfIsSlackbot reports whether u is Slackbot (Slack's isSlackbot by id:
// its card says "Built by Slack" and has no buttons).
func pfIsSlackbot(u core.User) bool { return u.ID == "USLACKBOT" }

// pfBanner is Slack's MemberProfileHoverBanner (client-boot-shared B0lO)
// across the card's top: "Workspace Owner", else "Workspace Admin", else
// (deactivated) an archive icon and "Deactivated account". Padding 8px
// 16px, 13px bold, one line. Light: rgba(29,28,29,.04) (deactivated .13,
// text .7); dark: #ddd behind the same dark text.
func pfBanner(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, w int) layout.Dimensions {
	var label, ic string
	switch {
	case u.IsOwner && !u.Deleted:
		label = "Workspace Owner"
	case u.IsAdmin && !u.Deleted:
		label = "Workspace Admin"
	case u.Deleted:
		label, ic = "Deactivated account", "archive-filled"
	default:
		return layout.Dimensions{}
	}
	fg := rgb(0x1d1c1d)
	bg := rgba(0x1d1c1d, 0.04)
	if u.Deleted {
		fg = rgba(0x1d1c1d, 0.7)
		bg = rgba(0x1d1c1d, 0.13)
	}
	if th != nil && th.Dark {
		bg = rgb(0xdddddd)
	}
	padX, padY := dp(gtx, 16), dp(gtx, 8)
	lh := dp(gtx, 18)
	h := padY + lh + padY
	fill(gtx, image.Rect(0, 0, w, h), bg)
	x := padX
	if ic != "" {
		at(gtx, image.Pt(x, padY+(lh-dp(gtx, 12))/2-dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, ic, 12, fg)
		})
		x += dp(gtx, 12) + dp(gtx, 8)
	}
	at(gtx, image.Pt(x, padY), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x-padX), lh)}
		return para(gtx, th, 13, 18, theme.Bold, fg, label, 1)
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// pfExternalBanner is AppExternalTeamBanner inside the member card
// (.p-member_profile_base_entity__primary .p-app_external_team_banner):
// in the flow, rgba(29,28,29,.04), padding 8, the 20px team icon (radius
// 3, 2px white ring, 8px each side), the team name 13px bold, one line.
// Slack renders it only once the team is known (externalTeam && name).
func pfExternalBanner(gtx layout.Context, h Host, th *theme.Theme, cl *Colors, name, iconURL string, w int) layout.Dimensions {
	if name == "" {
		return layout.Dimensions{}
	}
	pad, ic := dp(gtx, 8), dp(gtx, 20)
	hh := pad + ic + pad
	bg := rgba(0x1d1c1d, 0.04)
	if th != nil && th.Dark {
		bg = rgba(0xe8e8e8, 0.06)
	}
	fill(gtx, image.Rect(0, 0, w, hh), bg)
	x := pad + dp(gtx, 8)
	pfTeamIcon(gtx, h, th, name, iconURL, image.Pt(x, pad), 20, true)
	x += ic + dp(gtx, 8)
	at(gtx, image.Pt(x, pad), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x-pad), ic)}
		return vcenter(gtx, ic, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Bold, cl.Fg, name)
		})
	})
	return layout.Dimensions{Size: image.Pt(w, hh)}
}

// pfCardStatus is the card's custom status (BaseCustomStatus with
// .p-member_profile_hover_card__status): a 20px emoji, a space, and the
// text on one line, cut with an ellipsis.
func pfCardStatus(gtx layout.Context, h Host, th *theme.Theme, cl *Colors, u core.User) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	lineH := dp(gtx, 22)
	x := 0
	if u.StatusEmoji != "" || u.StatusEmojiURL != "" {
		at(gtx, image.Pt(0, (lineH-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
			return pfStatusEmoji(gtx, h, u, 20)
		})
		x = dp(gtx, 20) + dp(gtx, 4)
	}
	if !pfBlank(u.StatusText) {
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, maxW-x), lineH)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, u.StatusText, 1)
		})
	}
	return layout.Dimensions{Size: image.Pt(maxW, lineH)}
}

// pfLocalTimeLabel is Slack's LocalTime text (useProfileMemberLocalTime
// with useAtlasI18nLocalTimeLabels): "3:42 PM local time", or "3:42 PM
// (tomorrow) local time" / "(yesterday)" when the member's date differs
// from the viewer's (the viewer's profile time zone, not the machine's).
func pfLocalTimeLabel(h Host, u core.User, now time.Time) string {
	t := pfInZone(u, now)
	s := clock(t)
	if h != nil && h.Core() != nil {
		me := h.Core().Self()
		if me.TZ != "" || me.TZOffset != 0 {
			mine := pfInZone(me, now)
			ty, tm, td := t.Date()
			my, mm, md := mine.Date()
			a := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
			b := time.Date(my, mm, md, 0, 0, 0, 0, time.UTC)
			switch {
			case a.After(b):
				return s + " (tomorrow) local time"
			case a.Before(b):
				return s + " (yesterday) local time"
			}
		}
	}
	return s + " local time"
}

// pfInZone is now in u's time zone (tz, else its offset).
func pfInZone(u core.User, now time.Time) time.Time {
	if u.TZ != "" {
		if loc, err := time.LoadLocation(u.TZ); err == nil {
			return now.In(loc)
		}
	}
	return now.UTC().Add(time.Duration(u.TZOffset) * time.Second)
}

// ---- Slack boundaries (BasePresence, overflow menu, classes) ----

var (
	pfCardClass     = slackc.Class("p-member_profile_hover_card", "role", "dialog")
	pfPresenceClass = slackc.Class("c-presence", "data-qa", "presence").Tag("span").
			Mods("c-presence--active", "c-presence--away")
	pfVipIconClass = slackc.Class("c-base_icon c-icon", "data-qa", "vip").Tag("svg")
)

// pfMenuOpts puts a member's ⋮ menu inside RimetoMemberProfileOverflowMenu
// (AdminBackend and Nicknames mark that subtree, then append items to its
// MenuFromTemplate).
func pfMenuOpts(memberID string, o MenuOpts) MenuOpts {
	o.Within = "RimetoMemberProfileOverflowMenu"
	o.WithinProps = slackc.Props{"memberId": memberID}
	return o
}

// pfPresenceProps are BasePresence's props for u.
func pfPresenceProps(h Host, u core.User, showText bool) slackc.Props {
	k := "presence:" + u.ID
	if showText {
		k += ":text"
	}
	presence := "away"
	if u.Presence == "active" {
		presence = "active"
	}
	return slackc.Props{
		"$key":      k,
		"userId":    u.ID,
		"presence":  presence,
		"isActive":  presence == "active",
		"isSelf":    pfIsSelf(h, u.ID),
		"showText":  showText,
		"className": "c-presence",
	}
}

// pfPresence draws u's presence (Slack's Presence around BasePresence):
// the dot, plus "Active"/"Away" when showText. rowH > 0 centers it in a
// 20dp column of that height, text 28dp in (the full profile's presence
// row).
func pfPresence(gtx layout.Context, h Host, th *theme.Theme, u core.User, dot float32, showText bool, rowH int) layout.Dimensions {
	props, native := pfPresenceParts(h, th, u, dot, showText, rowH)
	return slackc.RenderLazy(gtx, "Presence", props, native)
}

// pfPresenceParts are Presence's props and native: Presence{showText,
// isActive, isSelf, userId} whose children slot is BasePresence, so a
// context from an ancestor (RimetoProfilePresence) reaches both. Slick's
// LastSeen renders Presence with showText=false plus "Last seen …"; Taut's
// patches BasePresence the same way. Either override hides the text.
func pfPresenceParts(h Host, th *theme.Theme, u core.User, dot float32, showText bool, rowH int) (func() slackc.Props, slackc.Native) {
	show := showText // Presence's showText this frame, after its override
	bpNative := func(gtx layout.Context, p slackc.Props) layout.Dimensions {
		s := show
		if p.Has("showText") {
			s = s && p.Bool("showText")
		}
		return pfPresenceNative(gtx, th, u, dot, s, rowH, p)
	}
	bpProps := func() slackc.Props { return pfPresenceProps(h, u, show) }
	props := func() slackc.Props {
		p := pfPresenceProps(h, u, showText)
		p["$key"] = "presence-outer:" + p.StrOr("$key", "")
		p["children"] = slackc.Child("BasePresence", pfPresenceProps(h, u, showText), bpNative)
		return p
	}
	native := func(gtx layout.Context, p slackc.Props) layout.Dimensions {
		show = showText
		if p.Has("showText") {
			show = p.Bool("showText")
		}
		return childrenOr(gtx, p, "BasePresence", bpProps, bpNative)
	}
	return props, native
}

// pfPresenceNative draws the presence with showText already resolved, and
// honours the isActive override.
func pfPresenceNative(gtx layout.Context, th *theme.Theme, u core.User, dot float32, showText bool, rowH int, p slackc.Props) layout.Dimensions {
	active := u.Presence == "active"
	if p.Has("isActive") {
		active = p.Bool("isActive")
	}
	var mods uint64 = 2
	if active {
		mods = 1
	}
	st := pfPresenceClass.Enter(gtx, 0, mods)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		if rowH <= 0 {
			if !showText {
				return pfPresenceGlyph(gtx, th, u, active, dot)
			}
			rowH = dp(gtx, 22)
		}
		cl := colorsOf(th)
		col := dp(gtx, 20)
		d := dp(gtx, dot)
		at(gtx, image.Pt((col-d)/2, (rowH-d)/2), func(gtx layout.Context) layout.Dimensions {
			return pfPresenceGlyph(gtx, th, u, active, dot)
		})
		if !showText {
			return layout.Dimensions{Size: image.Pt(col, rowH)}
		}
		label := "Away"
		if active {
			label = "Active"
		}
		x := dp(gtx, 28)
		td := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, gtx.Constraints.Max.X-x), rowH)}
			return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, st.Color(cl.Fg), label)
			})
		})
		return layout.Dimensions{Size: image.Pt(x+td.Size.X, rowH)}
	})
}

// pfShowsPresence is MemberProfileBaseEntity's presence rule: everyone
// but a deactivated member. Clack's member card also reaches bots (the
// click card); they have none, but Slackbot does: core marks it a bot,
// while Slack's ConnectedPresence draws it (isSystemUser for USLACKBOT:
// the status-slackbot-filled heart, colored by its presence).
func pfShowsPresence(u core.User) bool {
	return !u.Deleted && (!u.IsBot || pfIsSlackbot(u))
}

// pfPresenceGlyph is BasePresence's icon (getPresenceIconName) at about
// dot dp: Slackbot's heart (isSystemUser: status-slackbot-filled, drawn
// whatever its presence), the rounded square of an external member or
// guest (status-guest / -filled), else the member circle. Active is
// #2bac76 (c-presence--active), away the hollow gray.
func pfPresenceGlyph(gtx layout.Context, th *theme.Theme, u core.User, active bool, dot float32) layout.Dimensions {
	cl := colorsOf(th)
	col := cl.Away
	if active {
		col = cl.Presence
	}
	switch {
	case pfIsSlackbot(u):
		px := dp(gtx, dot*10/9) // the glyph is 10 wide where the circle is 9
		pfHeart(gtx, px, col)
		return layout.Dimensions{Size: image.Pt(px, px)}
	case u.External || u.Guest:
		px := dp(gtx, dot*8/9) // an 8-unit square where the circle is 9
		off := (dp(gtx, dot) - px) / 2
		r := image.Rect(off, off, off+px, off+px)
		rad := maxInt(1, px*7/32) // 1.75 of 8
		if active {
			rrect(gtx, r, rad, col)
		} else {
			stroke(gtx, r, rad, maxInt(1, dp(gtx, dot)/6), col)
		}
		return layout.Dimensions{Size: image.Pt(dp(gtx, dot), dp(gtx, dot))}
	}
	return presenceDot(gtx, th, active, dot)
}

// pfHeart fills a heart in a px square (redrawn after Slack's
// status-slackbot-filled: two round lobes meeting at a point).
func pfHeart(gtx layout.Context, px int, col color.NRGBA) {
	s := float32(px)
	pt := func(x, y float32) f32.Point { return f32.Pt(x*s, y*s) }
	var pa clip.Path
	pa.Begin(gtx.Ops)
	pa.MoveTo(pt(0.5, 0.92))
	pa.CubeTo(pt(0.2, 0.72), pt(0.0, 0.52), pt(0.0, 0.32))
	pa.CubeTo(pt(0.0, 0.14), pt(0.12, 0.04), pt(0.27, 0.04))
	pa.CubeTo(pt(0.38, 0.04), pt(0.46, 0.1), pt(0.5, 0.2))
	pa.CubeTo(pt(0.54, 0.1), pt(0.62, 0.04), pt(0.73, 0.04))
	pa.CubeTo(pt(0.88, 0.04), pt(1.0, 0.14), pt(1.0, 0.32))
	pa.CubeTo(pt(1.0, 0.52), pt(0.8, 0.72), pt(0.5, 0.92))
	pa.Close()
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: pa.End()}.Op())
}

// ---- shared profile helpers (card + full profile) ----

// pfBlankCut is spaces plus zero-width characters (ZWSP, ZWNJ, ZWJ, word
// joiner, BOM).
const pfBlankCut = " \t\n\u200b\u200c\u200d\u2060\ufeff"

// pfBlank reports whether s is empty once spaces and zero-width characters
// are trimmed (Hack Club users may have a display name of a lone zero-width space).
func pfBlank(s string) bool {
	return strings.Trim(s, pfBlankCut) == ""
}

// pfName is the name Slack shows for u, skipping blank display names.
func pfName(u core.User) string {
	switch {
	case !pfBlank(u.Name):
		return u.Name
	case !pfBlank(u.RealName):
		return u.RealName
	case !pfBlank(u.Handle):
		return u.Handle
	}
	return u.ID
}

func pfAvatarURL(u core.User, big bool) string {
	if big && u.Avatar512 != "" {
		return u.Avatar512
	}
	if u.Avatar192 != "" {
		return u.Avatar192
	}
	if u.Avatar512 != "" {
		return u.Avatar512
	}
	return u.Avatar
}

func pfIsSelf(h Host, id string) bool {
	return h.Core() != nil && id != "" && h.Core().Self().ID == id
}

// pfHasClock reports whether u has a meaningful local time.
func pfHasClock(u core.User) bool {
	return u.TZ != "" || u.TZOffset != 0 || !u.IsBot
}

func pfHasStatus(u core.User, now time.Time) bool {
	if pfBlank(u.StatusText) && u.StatusEmoji == "" && u.StatusEmojiURL == "" {
		return false
	}
	return u.StatusExp <= 0 || now.Unix() < u.StatusExp
}

// pfTick repaints at the next minute so local times stay current.
func pfTick(gtx layout.Context) {
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Truncate(time.Minute).Add(time.Minute)})
}

// pfSubscribe asks core for realtime presence of id (off the UI goroutine;
// it may write to the websocket).
func pfSubscribe(h Host, id string) {
	c := h.Core()
	if c == nil || id == "" {
		return
	}
	run(h, func(context.Context) (struct{}, error) {
		c.SubscribePresence([]string{id})
		return struct{}{}, nil
	}, nil)
}

// pfEnsure fetches an unknown user and reports whether it arrived.
func pfEnsure(h Host, id string, done func(found bool)) {
	c := h.Core()
	if c == nil {
		return
	}
	run(h, func(ctx context.Context) (struct{}, error) {
		c.EnsureUsers(ctx, []string{id})
		return struct{}{}, nil
	}, func(struct{}, error) {
		_, ok := lookupUser(h, id)
		if done != nil {
			done(ok)
		}
		h.Invalidate()
	})
}

// pfOpenFull opens the full profile pane (or toasts while unwired).
func pfOpenFull(h Host, id string) {
	if OpenFullProfile != nil {
		OpenFullProfile(id)
		return
	}
	h.Toast("Full profiles aren't available yet")
}

// pfMessage opens (or creates) the DM with id and navigates to it.
func pfMessage(h Host, id string) {
	c := h.Core()
	if c == nil {
		return
	}
	run(h, func(ctx context.Context) (core.Conv, error) {
		return c.OpenDM(ctx, id)
	}, func(cv core.Conv, err error) {
		if err != nil {
			h.Toast("Couldn't open the conversation: " + errText(err))
			return
		}
		if cv.ID != "" {
			h.Navigate("c/" + cv.ID)
		}
	})
}

// pfSetStatus is the self "Set a status" action.
func pfSetStatus(h Host) {
	h.ShowOverlay(NewSetStatusModal(h))
}

// pfMoreItems is the ⋮ menu of the card (withFull adds "View full
// profile") and of the full profile.
func pfMoreItems(h Host, u core.User, withFull bool, closeCard func()) []MenuItem {
	id := u.ID
	name := pfName(u)
	var items []MenuItem
	if withFull {
		items = append(items, MenuItem{Label: "View full profile", Do: func() {
			if closeCard != nil {
				closeCard()
			}
			pfOpenFull(h, id)
		}}, Sep())
	}
	items = append(items,
		MenuItem{Label: "Copy display name", DoCtx: func(gtx layout.Context) {
			copyText(gtx, name)
			h.Toast("Display name copied")
		}},
		MenuItem{Label: "Copy member ID", DoCtx: func(gtx layout.Context) {
			copyText(gtx, id)
			h.Toast("Member ID copied")
		}},
		MenuItem{Label: "Copy link to profile", DoCtx: func(gtx layout.Context) {
			copyText(gtx, "https://"+teamDomain(h)+".slack.com/team/"+id)
			h.Toast("Link copied")
		}},
	)
	return items
}

// pfImage draws url as a size-dp square with corner radius rad dp.
func pfImage(gtx layout.Context, h Host, url string, size, rad float32) layout.Dimensions {
	px := dp(gtx, size)
	sz := image.Pt(px, px)
	r := dp(gtx, rad)
	if url != "" {
		if img, ok := h.Images().Get(url, px); ok {
			return theme.Image(gtx, img, sz, r)
		}
	}
	rrect(gtx, image.Rectangle{Max: sz}, r, colorsOf(h.Theme()).Selected)
	return layout.Dimensions{Size: sz}
}

// pfNameLine draws the bold name (clickable via c) and the presence
// indicator after it, within maxW.
func pfNameLine(gtx layout.Context, h Host, th *theme.Theme, c *widget.Clickable, u core.User, size, lh, dot float32, maxW int) layout.Dimensions {
	cl := colorsOf(th)
	lineH := dp(gtx, lh)
	name := pfName(u)
	// unknown presence draws the hollow ring, like Slack; a deactivated
	// member has none (MemberProfileBaseEntity: !isDeactivated)
	hasDot := pfShowsPresence(u)
	reserve := 0
	if hasDot {
		reserve = dp(gtx, dot) + dp(gtx, 6)
	}
	nameMax := maxInt(1, maxW-reserve)
	var nd layout.Dimensions
	if c != nil {
		nd = clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(nameMax, lineH)}
			return para(gtx, th, size, lh, theme.Black, cl.Fg, name, 1)
		})
	} else {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(nameMax, lineH)}
		nd = para(g, th, size, lh, theme.Black, cl.Fg, name, 1)
	}
	x := nd.Size.X
	if hasDot {
		if x > 0 {
			x += dp(gtx, 6)
		}
		d := at(gtx, image.Pt(x, (lineH-dp(gtx, dot))/2), func(gtx layout.Context) layout.Dimensions {
			return pfPresence(gtx, h, th, u, dot, false, 0)
		})
		x += maxInt(d.Size.X, dp(gtx, dot))
	}
	return layout.Dimensions{Size: image.Pt(x, lineH)}
}

// pfStatusEmoji draws u's status emoji (custom URL first) at size dp.
func pfStatusEmoji(gtx layout.Context, h Host, u core.User, size float32) layout.Dimensions {
	if u.StatusEmojiURL != "" {
		px := dp(gtx, size)
		sz := image.Pt(px, px)
		if img, ok := h.Images().Get(u.StatusEmojiURL, px); ok {
			return containImage(gtx, img, sz)
		}
		return layout.Dimensions{Size: sz}
	}
	return emojiImage(gtx, h, u.StatusEmoji, size)
}

// pfStatusLine draws status emoji + wrapping status text.
func pfStatusLine(gtx layout.Context, h Host, th *theme.Theme, cl *Colors, u core.User, size, lh, emoji float32) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	lineH := dp(gtx, lh)
	x := 0
	if u.StatusEmoji != "" || u.StatusEmojiURL != "" {
		at(gtx, image.Pt(0, (lineH-dp(gtx, emoji))/2), func(gtx layout.Context) layout.Dimensions {
			return pfStatusEmoji(gtx, h, u, emoji)
		})
		x = dp(gtx, emoji) + dp(gtx, 6)
	}
	hgt := lineH
	if !pfBlank(u.StatusText) {
		d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, maxW-x), 1<<20)}
			return para(gtx, th, size, lh, theme.Regular, cl.Fg, u.StatusText, 0)
		})
		hgt = maxInt(hgt, d.Size.Y)
	}
	return layout.Dimensions{Size: image.Pt(maxW, hgt)}
}

// pfIconLine draws a 20px icon (#454447) and text 8px after it.
func pfIconLine(gtx layout.Context, th *theme.Theme, cl *Colors, name, s string, size, lh float32) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	lineH := dp(gtx, lh)
	at(gtx, image.Pt(0, (lineH-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, name, 20, cl.Fg2)
	})
	x := dp(gtx, 28)
	d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, maxW-x), lineH)}
		return para(gtx, th, size, lh, theme.Regular, cl.Fg, s, 1)
	})
	return layout.Dimensions{Size: image.Pt(x+d.Size.X, lineH)}
}

// pfSkeleton is the loading state of the text column.
func pfSkeleton(gtx layout.Context, th *theme.Theme, cl *Colors, missing bool, id string, w int) layout.Dimensions {
	if missing {
		para(gtx, th, 15, 22, theme.Black, cl.Fg, "Unknown user", 1)
		at(gtx, image.Pt(0, dp(gtx, 22)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, dp(gtx, 22))}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, id, 1)
		})
		return layout.Dimensions{Size: image.Pt(w, dp(gtx, 44))}
	}
	r := dp(gtx, 4)
	rrect(gtx, image.Rect(0, dp(gtx, 4), w*3/5, dp(gtx, 18)), r, cl.Selected)
	rrect(gtx, image.Rect(0, dp(gtx, 30), w*9/10, dp(gtx, 42)), r, cl.Selected)
	rrect(gtx, image.Rect(0, dp(gtx, 50), w*2/3, dp(gtx, 62)), r, cl.Selected)
	return layout.Dimensions{Size: image.Pt(w, dp(gtx, 72))}
}

// ---- buttons with a trailing caret, laid out in rows ----

// pfBtnSpec is one labeled button of a row.
type pfBtnSpec struct {
	c     *widget.Clickable
	label string
	lead  string           // icon before the label
	caret bool             // chevron-down after the label
	vip   bool             // the VIP button (its icon is svg[data-qa="vip"])
	rect  *image.Rectangle // set to the button's rect in the row (pfBtnRow)
}

type pfBtnMetrics struct {
	h, fs, rad, isz, csz, pad float32
}

func pfMetrics(size BtnSize) pfBtnMetrics {
	if size == BtnMedium {
		return pfBtnMetrics{h: 36, fs: 15, rad: 8, isz: 18, csz: 14, pad: 12}
	}
	return pfBtnMetrics{h: 28, fs: 13, rad: 4, isz: 16, csz: 12, pad: 8}
}

// pfBtnParts returns content width (icon + label) and the caret reserve.
func pfBtnParts(gtx layout.Context, th *theme.Theme, m pfBtnMetrics, s pfBtnSpec) (iw, tw, cw int) {
	tw = textW(gtx, th, m.fs, theme.Bold, s.label)
	if s.lead != "" {
		iw = dp(gtx, m.isz) + dp(gtx, 4)
	}
	if s.caret {
		cw = dp(gtx, 6) + dp(gtx, m.csz)
	}
	return iw, tw, cw
}

func pfBtnNatural(gtx layout.Context, th *theme.Theme, m pfBtnMetrics, s pfBtnSpec) int {
	iw, tw, cw := pfBtnParts(gtx, th, m, s)
	return iw + tw + cw + 2*dp(gtx, m.pad)
}

// pfBtn draws an outline button of width w px: icon + label centered (in
// the space left of the caret when the button is at natural width), caret
// at the right edge.
func pfBtn(gtx layout.Context, th *theme.Theme, size BtnSize, s pfBtnSpec, w int) layout.Dimensions {
	cl := colorsOf(th)
	m := pfMetrics(size)
	iw, tw, cw := pfBtnParts(gtx, th, m, s)
	pad := dp(gtx, m.pad)
	hh := dp(gtx, m.h)
	sz := image.Pt(w, hh)
	return s.c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: sz}
		rad := dp(gtx, m.rad)
		bg := cl.BtnBg
		if s.c.Hovered() {
			bg = cl.BtnHover
		}
		rrect(gtx, r, rad, cl.BtnBorder)
		rrect(gtx, r.Inset(1), maxInt(0, rad-1), bg)
		nat := iw + tw + cw + 2*pad
		x := pad + (w-nat)/2 // natural: icon, label and caret centered as a group
		cx := x + iw + tw + dp(gtx, 6)
		if s.caret && w > nat {
			// stretched: label centered on the button, caret at the right
			x = (w - iw - tw) / 2
			cx = w - pad - dp(gtx, m.csz)
		}
		if x < pad {
			x = pad
		}
		if s.lead != "" {
			at(gtx, image.Pt(x, (hh-dp(gtx, m.isz))/2), func(gtx layout.Context) layout.Dimensions {
				if !s.vip {
					return icon(gtx, s.lead, m.isz, cl.Fg)
				}
				st := pfVipIconClass.Enter(gtx, 0, 0)
				defer st.Leave()
				if st.Hidden() {
					return layout.Dimensions{}
				}
				return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, s.lead, m.isz, st.Color(cl.Fg))
				})
			})
			x += iw
		}
		lw := minInt(tw, maxInt(1, w-x-pad-cw))
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(exact(gtx, image.Pt(lw, hh)), hh, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				return text(gtx, th, m.fs, theme.Bold, cl.Fg, s.label)
			})
		})
		if s.caret {
			at(gtx, image.Pt(cx, (hh-dp(gtx, m.csz))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "chevron-down", m.csz, cl.Fg)
			})
		}
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}

// pfBtnRow lays out specs then a square kebab button across width w (gap
// 8). equal gives every labeled button the same width (full profile);
// otherwise natural widths share the leftover space (card). It returns the
// row size and the kebab's rect in row coords.
func pfBtnRow(gtx layout.Context, th *theme.Theme, size BtnSize, specs []pfBtnSpec, kebab *widget.Clickable, w int, equal bool) (layout.Dimensions, image.Rectangle) {
	m := pfMetrics(size)
	hh := dp(gtx, m.h)
	gap := dp(gtx, 8)
	var widths [4]int
	n := minInt(len(specs), len(widths))
	avail := w - hh - gap*n
	if kebab == nil {
		avail = w - gap*(n-1)
	}
	sum := 0
	for i := 0; i < n; i++ {
		widths[i] = pfBtnNatural(gtx, th, m, specs[i])
		sum += widths[i]
	}
	if n > 0 {
		if equal || sum > avail {
			for i := 0; i < n; i++ {
				widths[i] = maxInt(hh, avail/n)
			}
		} else {
			extra := (avail - sum) / n
			for i := 0; i < n; i++ {
				widths[i] += extra
			}
		}
	}
	x := 0
	for i := 0; i < n; i++ {
		s := specs[i]
		bw := widths[i]
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return pfBtn(gtx, th, size, s, bw)
		})
		if s.rect != nil {
			*s.rect = image.Rect(x, 0, x+bw, hh)
		}
		x += bw + gap
	}
	if kebab == nil {
		return layout.Dimensions{Size: image.Pt(w, hh)}, image.Rectangle{}
	}
	kr := image.Rect(x, 0, x+hh, hh)
	at(gtx, kr.Min, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(hh, hh)}
		return Button(gtx, th, kebab, BtnOutline, size, "", "kebab", 0)
	})
	return layout.Dimensions{Size: image.Pt(maxInt(w, kr.Max.X), hh)}, kr
}
