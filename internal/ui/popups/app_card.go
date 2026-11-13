package popups

import (
	"context"
	"image"
	"image/color"
	"strings"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's AppHoverCard (boot-render SmYd AppHoverCard → f0+8
// AppProfileCard), opened by ProfileHoverTrigger instead of the member
// card when the member isn't human (useProfileHoverApp: a bot user, or a
// message's bot_id). From the web client and its CSS (2026-11):
//
//   - .p-app_profile_card__container: 320 wide, radius 5, box-shadow 0 5px
//     10px rgba(0,0,0,.12), overflow hidden; the menu inside has no
//     padding (noMargin) and .p-app_profile_card padding-bottom 8.
//   - BaseAppProfileCardHeader: padding 24 (60 on top with the external
//     banner), white text on the app's color (app_card_color, default
//     #2C2D30) under linear-gradient(136.53deg, white 5% → 20%); the 64px
//     icon (radius 4, white behind) at the top of the row, then a column
//     16px in (margin-bottom 8, centered): the bold name, the inverse APP
//     badge 8px after it (surf-pry behind white text), the tagline (15px,
//     two lines). "This app is unavailable" is italic. AppExternalTeamBanner across the top
//     for another org's app: #1d1c1d, padding 8, the 20px team icon (radius
//     3, 2px white ring, 8px each side), the team name 13px bold.
//   - a separator (no padding above), "This app is unavailable" when the
//     install isn't active, then createAppProfileCardMenu: About this app,
//     Configuration (/services/<bot>?settings=1), Add this app to a
//     channel…, Add to VIP. Then up to three slash commands (bold name +
//     usage, 13px description; a click types "/cmd " into the composer),
//     the installation summary (BaseAppProfileCardInstallationSummary,
//     15px, padding 4px 24px) and the outline "Go to App" button (16px in,
//     margin 8).
//
// The card's data is apps.profile.get (core.AppCardOf); until it arrives
// Slack shows the 380px loading card.

// OpenAppAbout opens an app's About tab (its App Home DM). The ui root
// sets it; nil opens the DM instead.
var OpenAppAbout func(appUserID string)

// ComposerInsert types text at the start of the open conversation's
// composer and focuses it (Slack's autoFillSlashCommand). The ui root
// sets it.
var ComposerInsert func(text string) bool

type appItem struct {
	label string
	do    func()
}

// appState is the app card's part of a ProfileCard.
type appState struct {
	asked, failed bool
	card          *core.AppCard
	team          string // the app's home team name (external banner, summary)
	teamIcon      string

	header, goApp widget.Clickable
	items         []widget.Clickable
	cmds          [3]widget.Clickable
}

// isApp is useProfileHoverApp (hover cards, ProfileHoverCard SmYd) and
// ProfileCardTrigger's ProfileCardContainer (click cards, boot-render
// D3FY): both open the app card for a member that isn't human and for a
// bot with no member, and the member card (with its LocalTime row) only
// for a human. Human is ztJE isMemberHuman: not isAppOrBot (qSRE:
// is_app_user, is_bot, or the USLACK id); Slackbot (USLACKBOT, is_bot
// false) is human there and keeps the member card's "Built by Slack".
func (p *ProfileCard) isApp(u core.User, ok bool) bool {
	if p.id == "" {
		return p.bot != ""
	}
	return ok && !pfHuman(u)
}

// pfHuman is isMemberHuman: not an app or bot user (qSRE isAppOrBot).
// Slackbot's member is is_bot false in Slack's data, so it's human there;
// core.User marks it IsBot (userOf), hence its id here.
func pfHuman(u core.User) bool {
	return pfIsSlackbot(u) || !(u.IsBot || u.AppUser || u.ID == "USLACK")
}

// loadApp fetches the card once.
func (p *ProfileCard) loadApp() {
	if p.app.asked {
		return
	}
	p.app.asked = true
	c := p.h.Core()
	if c == nil {
		p.app.failed = true
		return
	}
	if card, ok := c.CachedAppCard(p.id, p.bot); ok {
		p.app.card = &card
	}
	uid, bid := p.id, p.bot
	type res struct {
		card       core.AppCard
		team, icon string
	}
	run(p.h, func(ctx context.Context) (res, error) {
		card, err := c.AppCardOf(ctx, uid, bid)
		if err != nil {
			return res{}, err
		}
		c.EnsureTeam(ctx, card.TopTeam())
		if card.InstallerID != "" {
			c.EnsureUsers(ctx, []string{card.InstallerID})
		}
		name, icon, _ := c.Team(card.TopTeam(), 20)
		return res{card, name, icon}, nil
	}, func(r res, err error) {
		if err != nil {
			p.app.failed = p.app.card == nil
			return
		}
		p.app.card, p.app.team, p.app.teamIcon = &r.card, r.team, r.icon
	})
}

// appProps are AppHoverCard's props.
func (p *ProfileCard) appProps() slackc.Props {
	pp := slackc.Props{"$key": p.key + ":app", "memberId": p.id, "serviceId": p.bot}
	if c := p.app.card; c != nil {
		pp["appId"] = c.AppID
		pp["homeWorkspaceId"] = c.HomeTeamID
		if pp["serviceId"] == "" {
			pp["serviceId"] = c.BotID
		}
	}
	return pp
}

// layoutApp draws the app card where the hover card goes and returns its
// rect (window coordinates).
func (p *ProfileCard) layoutApp(gtx layout.Context, th *theme.Theme, cl *Colors, win image.Point) image.Rectangle {
	p.loadApp()
	if p.app.failed {
		return image.Rectangle{} // Slack's AppHoverCard renders null (Missing)
	}
	w := dp(gtx, 320)
	if mw := win.X - 2*dp(gtx, 8); w > mw {
		w = mw
	}
	native := func(gtx layout.Context, _ slackc.Props) layout.Dimensions { return p.appContent(gtx, th, cl, w) }
	call, d := record(loose(gtx, image.Pt(w, win.Y)), func(gtx layout.Context) layout.Dimensions {
		return slackc.RenderLazy(gtx, "ProfileHoverCard", func() slackc.Props {
			return slackc.Props{
				"$key":                     p.key + ":outer",
				"memberId":                 p.id,
				"serviceId":                p.bot,
				"shouldStopPropagation":    false,
				"hideButtons":              false,
				"disableMemberProfileLink": false,
				"children":                 slackc.Child("AppHoverCard", p.appProps(), native),
			}
		}, func(gtx layout.Context, pp slackc.Props) layout.Dimensions {
			return childrenOr(gtx, pp, "AppHoverCard", p.appProps, native)
		})
	})
	pos := p.place(gtx, d.Size, win)
	r := image.Rectangle{Min: pos, Max: pos.Add(d.Size)}
	rad := dp(gtx, 5)
	st := appCardClass.Enter(gtx, 0, 0)
	if !st.Hidden() {
		lk := cardLook{fill: cl.Bg, ring: color.NRGBA{}, shadow: func(gtx layout.Context, r image.Rectangle, rad int) {
			softShadow(gtx, r, rad, dp(gtx, 5), dp(gtx, 10), rgba(0x000000, 0.12))
		}}
		styledCardLook(gtx, st, lk, pos, d.Size, rad, func(gtx layout.Context) {
			local := image.Rectangle{Max: d.Size}
			p.block.Layout(gtx, local)
			cs := clipRRect(gtx, local, rad)
			call.Add(gtx.Ops)
			cs.Pop()
		})
	}
	st.Leave()
	return r
}

var appCardClass = slackc.Class("p-app_profile_card__container", "data-qa", "app_profile_card_container")

// appContent is AppProfileCard at the origin, w wide.
func (p *ProfileCard) appContent(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	c := p.app.card
	if c == nil { // .p-app_profile_card--loading: 380 tall, centered
		h := dp(gtx, 380)
		d := dp(gtx, 8)
		for i := -1; i <= 1; i++ {
			x := w/2 + i*2*d - d/2
			rrect(gtx, image.Rect(x, h/2-d/2, x+d, h/2+d/2), d/2, cl.Selected)
		}
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	h := p.h
	y := p.appHeader(gtx, th, cl, *c, w)
	sep := func(above bool) {
		if above {
			y += dp(gtx, 8)
		}
		fill(gtx, image.Rect(0, y, w, y+border1(gtx)), cl.Divider)
		y += border1(gtx) + dp(gtx, 8)
	}
	sep(false)
	self := core.User{}
	if h.Core() != nil {
		self = h.Core().Self()
	}
	inactive := !c.Active && !c.External
	if inactive {
		d := at(gtx, image.Pt(dp(gtx, 24), y+dp(gtx, 4)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 48), 1<<20)}
			// .p-app_profile_card__disabled_message: foreground_max .7, italic
			gtx.Constraints.Min = image.Point{}
			return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Regular, Italic: true, Color: theme.WithAlpha(cl.Fg, 0xb3)}, "This app is unavailable")
		})
		y += d.Size.Y + dp(gtx, 8)
	}
	items := p.appMenu(*c, self)
	if len(p.app.items) < len(items) {
		p.app.items = make([]widget.Clickable, len(items))
	}
	rowH := dp(gtx, 28)
	for i, it := range items {
		if p.app.items[i].Clicked(gtx) {
			it.do()
		}
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return pfMenuRow(gtx, th, cl, &p.app.items[i], image.Pt(w, rowH), func(gtx layout.Context, fg color.NRGBA) {
				at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 48), rowH)}
					return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, fg, it.label)
					})
				})
			})
		})
		y += rowH
	}
	var cmds []core.AppCommand
	if c.Active && !c.External {
		cmds = c.Commands
		if len(cmds) > 3 {
			cmds = cmds[:3]
		}
	}
	if len(cmds) > 0 {
		sep(true)
		for i, cm := range cmds {
			if p.app.cmds[i].Clicked(gtx) {
				p.Dismiss()
				if ComposerInsert == nil || !ComposerInsert(cm.Name+" ") {
					h.Toast("Open a conversation to use " + cm.Name)
				}
			}
			ch := rowH + dp(gtx, 18)
			if cm.Desc == "" {
				ch = rowH
			}
			at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				return pfMenuRow(gtx, th, cl, &p.app.cmds[i], image.Pt(w, ch), func(gtx layout.Context, fg color.NRGBA) {
					x := dp(gtx, 24)
					maxX := w - dp(gtx, 24)
					nd := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(maxX-x, rowH)}
						return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
							return text(gtx, th, 15, theme.Bold, fg, cm.Name)
						})
					})
					if cm.Usage != "" {
						ux := x + nd.Size.X + dp(gtx, 4)
						at(gtx, image.Pt(ux, 0), func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, maxX-ux), rowH)}
							return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
								return text(gtx, th, 15, theme.Regular, fg, cm.Usage)
							})
						})
					}
					if cm.Desc != "" {
						dc := cl.Fg2
						if fg == cl.HighText {
							dc = fg
						}
						at(gtx, image.Pt(x, rowH-dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints = layout.Constraints{Max: image.Pt(maxX-x, dp(gtx, 17))}
							return text(gtx, th, 13, theme.Regular, dc, cm.Desc)
						})
					}
				})
			})
			y += ch
		}
	}
	summary := p.appSummary(*c, self)
	if summary != nil && (inactive || len(items) > 0 || len(cmds) > 0) {
		sep(true)
	}
	if summary != nil {
		d := at(gtx, image.Pt(dp(gtx, 24), y+dp(gtx, 4)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 48), 1<<20)}
			return pfRuns(gtx, th, cl, summary, w-dp(gtx, 48))
		})
		y += dp(gtx, 4) + d.Size.Y + dp(gtx, 4)
	}
	if p.appShowDM(*c) {
		if p.app.goApp.Clicked(gtx) {
			p.Dismiss()
			pfMessage(h, c.AppUserID)
		}
		y += dp(gtx, 8)
		d := at(gtx, image.Pt(dp(gtx, 16), y), func(gtx layout.Context) layout.Dimensions {
			bw := float32(w-dp(gtx, 32)) / gtx.Metric.PxPerDp
			if gtx.Metric.PxPerDp == 0 {
				bw = float32(w - 32)
			}
			return Button(gtx, th, &p.app.goApp, BtnOutline, BtnMedium, "Go to App", "", bw)
		})
		y += d.Size.Y + dp(gtx, 8)
	}
	y += dp(gtx, 8) // .p-app_profile_card padding-bottom
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// appShowDM is shouldShowDirectMessageItem: the app's bot user can be
// messaged (another org's app only when it is a xoxp app).
func (p *ProfileCard) appShowDM(c core.AppCard) bool {
	if c.AppUserID == "" {
		return false
	}
	if u, ok := lookupUser(p.h, c.AppUserID); ok && u.Deleted {
		return false
	}
	return !c.External || c.AppType == "xoxp"
}

// appMenu is createAppProfileCardMenu's items that apply here.
func (p *ProfileCard) appMenu(c core.AppCard, self core.User) []appItem {
	h := p.h
	var out []appItem
	if c.AppID != "" && (!c.External || c.Published || p.appShowDM(c)) {
		out = append(out, appItem{"About this app", func() { p.Dismiss(); p.openAbout(c) }})
	}
	if c.Active && !c.External && !self.Guest && c.BotID != "" && h.Core() != nil {
		if base := strings.TrimSuffix(h.Core().C.Creds.TeamURL, "/"); base != "" {
			link := base + "/services/" + c.BotID + "?settings=1&utm_source=in-prod&utm_medium=inprod-link_app_settings-user_card-click"
			out = append(out, appItem{"Configuration", func() {
				p.Dismiss()
				if err := openurl.Open(link); err != nil {
					h.Toast("Couldn't open the link: " + errText(err))
				}
			}})
		}
	}
	if c.AppUserID != "" && c.Active && !self.Guest && !c.Workflow {
		// channel_invite: openInviteMemberToChannelDialog (profile_app_channel.go)
		app := c.AppUserID
		out = append(out, appItem{"Add this app to a channel…", func() {
			p.Dismiss()
			pfOpenInviteToChannel(h, app)
		}})
	}
	if c.AppUserID != "" && c.Active {
		// toggle_vip: togglePriorityUserById (profile_vip.go)
		u, ok := lookupUser(h, c.AppUserID)
		if !ok {
			u = core.User{ID: c.AppUserID, Name: c.Name, IsBot: true}
		}
		label := "Add to VIP"
		if pfIsVIP(h, u.ID) {
			label = "Remove from VIP"
		}
		out = append(out, appItem{label, func() { p.Dismiss(); pfToggleVIP(h, u) }})
	}
	return out
}

func (p *ProfileCard) openAbout(c core.AppCard) {
	if c.AppUserID == "" {
		return
	}
	if OpenAppAbout != nil {
		OpenAppAbout(c.AppUserID)
		return
	}
	pfMessage(p.h, c.AppUserID)
}

// pfRun is one styled run of a summary line.
type pfRun struct {
	s       string
	mention bool // MemberSlug: @name in link blue on the mention tint
}

// appSummary is BaseAppProfileCardInstallationSummary's text (nil: none).
// Slack shows it when the install is active with an installer, or the
// app's home team is known (getAppHomeTeamDisplayName).
func (p *ProfileCard) appSummary(c core.AppCard, self core.User) []pfRun {
	name := firstNonEmptyStr(c.Name, "this app")
	if !(c.Active && c.InstallerID != "") && p.app.team == "" {
		return nil
	}
	if c.InstallerID != "" && c.InstallerID == self.ID {
		return []pfRun{{s: "You added " + name + " to this workspace."}}
	}
	if p.app.team != "" {
		return []pfRun{{s: name + " is installed on " + p.app.team + "."}}
	}
	who := pfRun{s: "Unknown user"}
	if u, ok := lookupUser(p.h, c.InstallerID); ok {
		who = pfRun{s: "@" + pfName(u), mention: true}
	}
	switch c.AppType {
	case "custom_integration":
		return []pfRun{who, {s: " added this custom integration to this workspace."}}
	case "slack":
		return []pfRun{who, {s: " configured " + name + " to post in this channel."}}
	}
	return []pfRun{who, {s: " added " + name + " to this workspace."}}
}

func firstNonEmptyStr(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// pfRuns lays runs out as one wrapping 15/22 paragraph (word by word).
func pfRuns(gtx layout.Context, th *theme.Theme, cl *Colors, runs []pfRun, w int) layout.Dimensions {
	lh := dp(gtx, 22)
	x, y := 0, 0
	space := textW(gtx, th, 15, theme.Regular, " ")
	for _, r := range runs {
		words := []string{r.s}
		if !r.mention {
			words = strings.SplitAfter(r.s, " ")
		}
		for _, wd := range words {
			if wd == "" {
				continue
			}
			wt := strings.TrimRight(wd, " ")
			ww := textW(gtx, th, 15, theme.Regular, wt)
			if r.mention {
				ww += dp(gtx, 4)
			}
			if x > 0 && x+ww > w {
				x, y = 0, y+lh
			}
			if r.mention {
				rrect(gtx, image.Rect(x, y+dp(gtx, 1), x+ww, y+lh-dp(gtx, 1)), dp(gtx, 3), theme.WithAlpha(cl.Link, 0x1a))
				at(gtx, image.Pt(x+dp(gtx, 2), y), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Link, wt)
					})
				})
			} else {
				at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Fg, wt)
					})
				})
			}
			x += ww
			if strings.HasSuffix(wd, " ") {
				x += space
			}
		}
	}
	return layout.Dimensions{Size: image.Pt(w, y+lh)}
}

// pfMenuRow is a c-menu_item__button row: hover highlights it (#1264a3,
// white text) and body draws its content with the row's text color.
func pfMenuRow(gtx layout.Context, th *theme.Theme, cl *Colors, c *widget.Clickable, sz image.Point, body func(gtx layout.Context, fg color.NRGBA)) layout.Dimensions {
	g := gtx
	g.Constraints = layout.Exact(sz)
	return c.Layout(g, func(gtx layout.Context) layout.Dimensions {
		fg := cl.Fg
		if c.Hovered() {
			fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
			fg = cl.HighText
		}
		pointerCursor(gtx, sz)
		body(gtx, fg)
		return layout.Dimensions{Size: sz}
	})
}

// appHeader is BaseAppProfileCardHeader (+ AppExternalTeamBanner); it
// returns its height.
func (p *ProfileCard) appHeader(gtx layout.Context, th *theme.Theme, cl *Colors, c core.AppCard, w int) int {
	pad := dp(gtx, 24)
	top := pad
	if c.External {
		top = dp(gtx, 60)
	}
	av := dp(gtx, 64)
	colX := pad + av + dp(gtx, 16)
	colW := maxInt(1, w-colX-pad)
	white := rgb(0xffffff)
	// the title column, measured first (it may be taller than the icon)
	nameW := textW(gtx, th, 15, theme.Bold, c.Name)
	badgeW := textW(gtx, th, 10, theme.Bold, "APP") + dp(gtx, 6)
	tagCall, tagD := record(loose(gtx, image.Pt(colW, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		if c.Tagline == "" {
			return layout.Dimensions{}
		}
		return para(gtx, th, 15, 22, theme.Regular, white, c.Tagline, 2)
	})
	lineH := dp(gtx, 22)
	colH := lineH + dp(gtx, 4) + tagD.Size.Y + dp(gtx, 8) // padding_bottom_25, margin-bottom 8
	inner := maxInt(av, colH)
	hh := top + inner + pad
	r := image.Rect(0, 0, w, hh)

	// background: the app's color under the white 5% → 20% gradient
	base := rgb(0x2c2d30)
	if v, ok := parseHex(c.Color); ok {
		base = v
	}
	fill(gtx, r, base)
	func() {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		paint.LinearGradientOp{Stop1: f32.Pt(0, 0), Color1: rgba(0xffffff, 0.05),
			Stop2: f32.Pt(float32(w), float32(hh)), Color2: rgba(0xffffff, 0.2)}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}()
	if c.Active && !c.External {
		at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &p.app.header, nil, func(gtx layout.Context) layout.Dimensions {
				pointerCursor(gtx, r.Size())
				return layout.Dimensions{Size: r.Size()}
			})
		})
		if p.app.header.Clicked(gtx) {
			p.Dismiss()
			p.openAbout(c)
		}
	}
	if c.External {
		p.appExternalBanner(gtx, th, w)
	}
	// icon: 64px, radius 4, white behind, at the top of the header's
	// flex row (a 64px img in a stretch row: a taller title column
	// doesn't move it)
	ar := image.Rect(pad, top, pad+av, top+av)
	rrect(gtx, ar, dp(gtx, 4), white)
	at(gtx, ar.Min, func(gtx layout.Context) layout.Dimensions {
		appIcon(gtx, p.h, c, av, dp(gtx, 4)) // Slack's bundled default when the app has none
		return layout.Dimensions{Size: ar.Size()}
	})
	cy := top + (inner-colH)/2
	nmax := maxInt(1, colW-badgeW-dp(gtx, 8))
	nd := at(gtx, image.Pt(colX, cy), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(minInt(nameW, nmax)+1, lineH)}
		return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, white, c.Name)
		})
	})
	// .c-app_badge--inverse: 14 tall, 10px bold, padding 1px 3px, radius 2
	bx, bh := colX+nd.Size.X+dp(gtx, 8), dp(gtx, 14)
	by := cy + (lineH-bh)/2
	// .c-app_badge's --dt_color-surf-pry (#1d1c1d0f light, #f8f8f80f
	// dark); --inverse only makes the text white
	badgeBg := rgba(0x1d1c1d, 0x0f/255.0)
	if th.Dark {
		badgeBg = rgba(0xf8f8f8, 0x0f/255.0)
	}
	rrect(gtx, image.Rect(bx, by, bx+badgeW, by+bh), dp(gtx, 2), badgeBg)
	at(gtx, image.Pt(bx+dp(gtx, 3), by), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 10, theme.Bold, white, "APP")
		})
	})
	macroAt(gtx, tagCall, image.Pt(colX, cy+lineH+dp(gtx, 4)))
	return hh
}

// appExternalBanner is AppExternalTeamBanner over the app header.
func (p *ProfileCard) appExternalBanner(gtx layout.Context, th *theme.Theme, w int) {
	pad := dp(gtx, 8)
	ic := dp(gtx, 20)
	h := pad + ic + pad
	fill(gtx, image.Rect(0, 0, w, h), rgb(0x1d1c1d))
	x := pad + dp(gtx, 8)
	pfTeamIcon(gtx, p.h, th, p.app.team, p.app.teamIcon, image.Pt(x, pad), 20, true)
	x += ic + dp(gtx, 8)
	at(gtx, image.Pt(x, pad), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x-pad), ic)}
		return vcenter(gtx, ic, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Bold, rgb(0xffffff), pfTeamName(p.app.team))
		})
	})
}

// pfTeamName is getTeamDisplayName: "Unknown Workspace" until known.
func pfTeamName(name string) string {
	if name == "" {
		return "Unknown Workspace"
	}
	return name
}

// pfTeamIcon is BaseTeamIcon at pt, size dp: the image (radius 3), or the
// team's initials on gray when it has the default icon; ring adds the
// banner's 2px white ring.
func pfTeamIcon(gtx layout.Context, h Host, th *theme.Theme, name, url string, pt image.Point, size float32, ring bool) {
	px := dp(gtx, size)
	r := image.Rectangle{Min: pt, Max: pt.Add(image.Pt(px, px))}
	rad := dp(gtx, 3)
	if ring {
		rrect(gtx, r.Inset(-dp(gtx, 2)), rad+dp(gtx, 2), rgb(0xffffff))
	}
	if url != "" {
		if img, ok := h.Images().Get(url, px); ok {
			at(gtx, pt, func(gtx layout.Context) layout.Dimensions { return theme.Image(gtx, img, r.Size(), rad) })
			return
		}
	}
	rrect(gtx, r, rad, rgb(0xdddddd))
	in := pfInitials(name)
	if in == "" {
		return
	}
	at(gtx, pt, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(r.Size())
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, size/1.8, theme.Bold, rgb(0x1d1c1d), in)
		})
	})
}

// pfInitials is the first letters of a team's first two words.
func pfInitials(name string) string {
	out := ""
	for _, f := range strings.Fields(name) {
		for _, r := range f {
			out += strings.ToUpper(string(r))
			break
		}
		if len([]rune(out)) == 2 {
			break
		}
	}
	return out
}

func parseHex(s string) (color.NRGBA, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return color.NRGBA{}, false
	}
	var v uint32
	for _, ch := range s {
		var d uint32
		switch {
		case ch >= '0' && ch <= '9':
			d = uint32(ch - '0')
		case ch >= 'a' && ch <= 'f':
			d = uint32(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			d = uint32(ch-'A') + 10
		default:
			return color.NRGBA{}, false
		}
		v = v<<4 | d
	}
	return rgb(v), true
}
