package popups

import (
	"bytes"
	"context"
	_ "embed"
	"image"
	"image/png"
	"strconv"
	"strings"
	"sync"

	"gioui.org/layout"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// AppDetailsView is Slack's App details flexpane (gantry-v2-async-client-
// app-details, build 3889f0c93d30: Qsn3 AppDetails with the view model
// w9Gc useAppDetailsViewModel), the view toAppDetails opens in the
// secondary pane: MemberSlug's click on an app's mention (boot-shared
// ld4x, when getBotIdFromMember finds a bot id) and openProfile for a
// sender with a bot (client-boot rLTI). The test account is in both
// flexpane_content_parity and revamp_agent_app_profile
// (docs/EXPERIMENTS.md), so this is the parity layout:
//
//   - The flexpane header says "App" (hideBottomBorder).
//   - The first section (.appProfileContentSection: margin-inline 20,
//     padding 20 0 28 in .parityDividers) is AppDetailsHeaderSection's
//     parity header (column, gap 12): a row of the 36px avatar (the app's
//     icon, or Slack's default_new_app_icon) and, 12px on, the name (h2:
//     plastic's 1.75rem bold, line-height 1.2, one line) with its APP
//     badge (c-app_badge, gap 8) and the VIP badge, over "by {developer}"
//     (13px, content-sec); then the app's desc; then "Active in N channels
//     and DMs" (installation.memberships_count) beside the 20px channel
//     icon (15px content-pry, padding-top 8, gap 8). Then, 20px down, the
//     buttons (flex, gap 8): the main action (getAppPrimaryActions,
//     8oqt TW: Message when the app supportsDmInteraction, else Open App;
//     flex 4) and AppDetailsOverflow's 36px ⋮.
//   - AppDetailsSection: the Description section (sectionHeader 15/900,
//     margin-bottom 8; long_desc clamped at 16 lines), each section ruled
//     off by 1px otl-ter.
//   - No apps.profile.get answer: AppNotFound (gpb+): the 128px magnifying
//     glass, "App not found" (18/900) and its message (15px), centered.
//
// Not drawn: what needs data Clack doesn't fetch (apps.info's categories,
// shortcuts and MCP servers for the Features section, scopes for
// Permissions, screenshots, suggested prompts, the connection status and
// approval banners, the top-bar items).
type AppDetailsView struct {
	h        Host
	uid, bot string
	list     layout.List

	asked, failed bool
	card          *core.AppCard
	team          string // the app's home team (an external app's installed-on row)

	closeBtn, backBtn, mainBtn, moreBtn widget.Clickable
	moreBox                             image.Rectangle // the ⋮, pane coords
	paneW                               int
}

// OpenAppTab opens the app's DM on a tab ("home", "about" or "messages"):
// AppDetails' main action and its overflow's Message and App home (Slack's
// openAppSpace +/mf FL with tabId). The ui root sets it; nil opens the DM.
var OpenAppTab func(appUserID, tab string)

// NewAppDetailsView is toAppDetails for the bot user userID (memberId)
// or the bot botID (serviceId).
func NewAppDetailsView(h Host, userID, botID string) *AppDetailsView {
	v := &AppDetailsView{h: h, uid: userID, bot: botID}
	v.list.Axis = layout.Vertical
	return v
}

// UserID is the app's bot user the view was opened for.
func (v *AppDetailsView) UserID() string { return v.uid }

// Card is the app's profile once loaded.
func (v *AppDetailsView) Card() (core.AppCard, bool) {
	if v.card == nil {
		return core.AppCard{}, false
	}
	return *v.card, true
}

func (v *AppDetailsView) load() {
	if v.asked {
		return
	}
	v.asked = true
	c := v.h.Core()
	if c == nil {
		v.failed = true
		return
	}
	if card, ok := c.CachedAppCard(v.uid, v.bot); ok {
		v.card = &card
	}
	uid, bid := v.uid, v.bot
	type res struct {
		card core.AppCard
		team string
	}
	run(v.h, func(ctx context.Context) (res, error) {
		card, err := c.AppCardOf(ctx, uid, bid)
		if err != nil {
			return res{}, err
		}
		if card.External {
			c.EnsureTeam(ctx, card.TopTeam())
		}
		name, _, _ := c.Team(card.TopTeam(), 20)
		return res{card, name}, nil
	}, func(r res, err error) {
		if err != nil {
			v.failed = v.card == nil
			return
		}
		v.card, v.team = &r.card, r.team
	})
}

// Layout fills the constraints with the pane.
func (v *AppDetailsView) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	th := v.h.Theme()
	cl := colorsOf(th)
	sz := gtx.Constraints.Max
	v.paneW = sz.X
	v.load()
	v.clicks(gtx)

	// FlexpaneHeader: "App", the back button while the pane has a back
	// stack, close
	hh := dp(gtx, 49)
	pad := dp(gtx, 16)
	tx := pad
	if b, ok := v.h.(SecondaryBacker); ok && b.SecondaryBack() {
		at(gtx, image.Pt(pad, (hh-dp(gtx, 28))/2), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, &v.backBtn, "caret-left-full", 28, 20, cl.Fg2, cl.BgHover, 8)
		})
		tx += dp(gtx, 28) + dp(gtx, 6) + dp(gtx, 6)
	}
	at(gtx, image.Pt(tx, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, sz.X-tx-dp(gtx, 52)), hh)}
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Black, cl.Fg, "App")
		})
	})
	at(gtx, image.Pt(sz.X-dp(gtx, 12)-dp(gtx, 28), (hh-dp(gtx, 28))/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &v.closeBtn, "close", 28, 20, cl.Fg2, cl.BgHover, 8)
	})

	bodyH := maxInt(0, sz.Y-hh)
	at(gtx, image.Pt(0, hh), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(sz.X, bodyH))
		defer clipRRect(gtx, image.Rect(0, 0, sz.X, bodyH), 0).Pop()
		switch {
		case v.card == nil && v.failed:
			return v.notFound(gtx, th, cl, sz.X, bodyH)
		case v.card == nil: // isFetchingApp: the loading spinner
			d := dp(gtx, 24)
			at(gtx, image.Pt((sz.X-d)/2, (bodyH-d)/2), func(gtx layout.Context) layout.Dimensions {
				return abSpinner(gtx, d, cl.Fg2)
			})
			pfTick(gtx)
			return layout.Dimensions{Size: image.Pt(sz.X, bodyH)}
		}
		return v.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			return v.body(gtx, th, cl, *v.card, sz.X)
		})
	})
	return layout.Dimensions{Size: sz}
}

func (v *AppDetailsView) clicks(gtx layout.Context) {
	h := v.h
	if v.closeBtn.Clicked(gtx) {
		h.CloseSecondary()
	}
	if v.backBtn.Clicked(gtx) {
		if b, ok := h.(SecondaryBacker); ok && b.SecondaryBack() {
			b.GoSecondaryBack()
		}
	}
	if v.card == nil {
		return
	}
	c := *v.card
	if v.mainBtn.Clicked(gtx) {
		v.mainAction(c)
	}
	if v.moreBtn.Clicked(gtx) {
		if items := v.overflow(c); len(items) > 0 {
			h.ShowOverlay(&pfPaneMenu{h: h, items: items, box: v.moreBox, paneW: v.paneW, app: c.AppID})
		}
	}
}

// adOffersMessage is useAppPrimaryActionFacts' offersMessage: the app
// has a bot user and supportsDmInteraction (app_home_metadata's
// messages_tab_enabled, on unless the app turns it off).
func adOffersMessage(c core.AppCard) bool { return c.AppUserID != "" && c.MessagesTab }

// mainAction is the main button's onClick (AppDetails aT): Message opens
// the DM's Messages tab; Open App the App Home's Home tab, or About when
// the app has no home tab (flexpane_content_parity).
func (v *AppDetailsView) mainAction(c core.AppCard) {
	if c.AppUserID == "" {
		return
	}
	tab := "home"
	switch {
	case adOffersMessage(c):
		tab = "messages"
	case !c.HomeTab:
		tab = "about"
	}
	v.openTab(c, tab)
}

func (v *AppDetailsView) openTab(c core.AppCard, tab string) {
	if OpenAppTab != nil {
		OpenAppTab(c.AppUserID, tab)
		return
	}
	pfMessage(v.h, c.AppUserID)
}

// overflow is AppDetailsOverflow's parity template: Message and App home
// unless the main button is that action, the app's slash commands (three
// at most, typed into the open conversation's composer), Add to channel,
// Add to VIP / Remove from VIP and Configuration, separated by group.
func (v *AppDetailsView) overflow(c core.AppCard) []MenuItem {
	h := v.h
	self := core.User{}
	if h.Core() != nil {
		self = h.Core().Self()
	}
	msg := adOffersMessage(c)
	var out []MenuItem
	sep := func() {
		if n := len(out); n > 0 && !out[n-1].Separator {
			out = append(out, MenuItem{Separator: true})
		}
	}
	// navigation: the main action is Message or Open App, so only App home
	// is left over (offersAppHome: home_tab_enabled)
	if msg && c.HomeTab {
		out = append(out, MenuItem{Label: "App home", Icon: "home", Do: func() { v.openTab(c, "home") }})
	}
	sep()
	if c.Active && !c.External && len(c.Commands) > 0 && ComposerInsert != nil {
		cmds := c.Commands
		if len(cmds) > 3 {
			cmds = cmds[:3]
		}
		var sub []MenuItem
		for _, cm := range cmds {
			name := cm.Name
			sub = append(sub, MenuItem{Label: name, Subtitle: cm.Desc, Do: func() {
				if !ComposerInsert(name + " ") {
					h.Toast("Open a conversation to use " + name)
				}
			}})
		}
		out = append(out, MenuItem{Label: "Slash commands", Icon: "slash", Sub: sub})
	}
	sep()
	if c.AppUserID != "" && c.Active && !c.External && !self.Guest && !c.Workflow {
		app := c.AppUserID
		out = append(out, MenuItem{Label: "Add to channel", Icon: "channel", Do: func() { pfOpenInviteToChannel(h, app) }})
	}
	if c.AppUserID != "" {
		u, ok := lookupUser(h, c.AppUserID)
		if !ok {
			u = core.User{ID: c.AppUserID, Name: c.Name, IsBot: true}
		}
		label, ic := "Add to VIP", "user-add"
		if pfIsVIP(h, u.ID) {
			label, ic = "Remove from VIP", "user-hide"
		}
		out = append(out, MenuItem{Label: label, Icon: ic, Do: func() { pfToggleVIP(h, u) }})
	}
	sep()
	if c.Active && !c.External && !self.Guest && c.BotID != "" && h.Core() != nil {
		if base := strings.TrimSuffix(h.Core().C.Creds.TeamURL, "/"); base != "" {
			link := base + "/services/" + c.BotID + "?settings=1"
			out = append(out, MenuItem{Label: "Configuration", Icon: "new-window", Do: func() {
				if err := openurl.Open(link); err != nil {
					h.Toast("Couldn't open the link: " + errText(err))
				}
			}})
		}
	}
	// stripTerminalSeparators
	for len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	for len(out) > 0 && out[0].Separator {
		out = out[1:]
	}
	return out
}

// adSection is .appProfileContentSection in .parityDividers: 20px in on
// each side, padding 28 0 (20 on top for the first, 32 at the bottom for
// the last), a 1px otl-ter rule under all but the last.
func adSection(gtx layout.Context, cl *Colors, y, w int, first, last bool, body func(gtx layout.Context, w int) int) int {
	mx := dp(gtx, 20)
	top, bot := dp(gtx, 28), dp(gtx, 28)
	if first {
		top = dp(gtx, 20)
	}
	if last {
		bot = dp(gtx, 32)
	}
	inner := maxInt(1, w-2*mx)
	var h int
	at(gtx, image.Pt(mx, y+top), func(gtx layout.Context) layout.Dimensions {
		h = body(gtx, inner)
		return layout.Dimensions{Size: image.Pt(inner, h)}
	})
	y += top + h + bot
	if !last {
		fill(gtx, image.Rect(mx, y-border1(gtx), mx+inner, y), cl.Divider)
	}
	return y
}

// body is the scrolling content, w the pane width.
func (v *AppDetailsView) body(gtx layout.Context, th *theme.Theme, cl *Colors, c core.AppCard, w int) layout.Dimensions {
	desc := strings.TrimSpace(c.LongDesc)
	y := adSection(gtx, cl, 0, w, true, desc == "", func(gtx layout.Context, iw int) int {
		return v.header(gtx, th, cl, c, iw)
	})
	if desc != "" {
		y = adSection(gtx, cl, y, w, false, true, func(gtx layout.Context, iw int) int {
			at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(iw, dp(gtx, 22))}
				return para(gtx, th, 15, 22, theme.Black, cl.Fg, "Description", 1)
			})
			hy := dp(gtx, 22) + dp(gtx, 8)
			d := at(gtx, image.Pt(0, hy), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(iw, 1<<20)}
				return para(gtx, th, 15, 22, theme.Regular, cl.Fg, desc, 16) // contractedLines 16
			})
			return hy + d.Size.Y
		})
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// header is the parity header and the buttons under it; it returns the
// height.
func (v *AppDetailsView) header(gtx layout.Context, th *theme.Theme, cl *Colors, c core.AppCard, w int) int {
	av := dp(gtx, 36)
	gap := dp(gtx, 12)
	tx := av + gap
	tw := maxInt(1, w-tx)
	name := firstNonEmptyStr(c.Name, "App")
	nameLH := dp(gtx, 33.6) // 28px × 1.2
	// the name line: name, APP badge, VIP badge (wrapping as a flex-wrap
	// row would; one line here, the name ellipsized)
	badgeW := textW(gtx, th, 10, theme.Bold, "APP") + dp(gtx, 6)
	vip := c.AppUserID != "" && pfIsVIP(v.h, c.AppUserID)
	reserve := dp(gtx, 8) + badgeW
	if vip {
		reserve += dp(gtx, 8) + dp(gtx, 20)
	}
	devH := 0
	if c.Developer != "" {
		devH = dp(gtx, 18)
	}
	rowH := maxInt(av, nameLH+devH)
	// headerRowWithDeveloper aligns to the last baseline: the avatar sits
	// at the row's bottom then, else it's centered
	avY := (rowH - av) / 2
	if devH > 0 {
		avY = rowH - av
	}
	at(gtx, image.Pt(0, avY), func(gtx layout.Context) layout.Dimensions {
		rad := avatarRadius(gtx, 36)
		if !appIcon(gtx, v.h, c, av, rad) {
			rrect(gtx, image.Rect(0, 0, av, av), rad, cl.Selected)
		}
		return layout.Dimensions{Size: image.Pt(av, av)}
	})
	ty := (rowH - nameLH - devH) / 2
	nd := at(gtx, image.Pt(tx, ty), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, tw-reserve), nameLH)}
		return para(gtx, th, 28, 33.6, theme.Bold, cl.Fg, name, 1)
	})
	bx := tx + nd.Size.X + dp(gtx, 8)
	bh := dp(gtx, 14)
	by := ty + (nameLH-bh)/2
	// .c-app_badge: content-sec on surf-pry, 10px bold, padding 1px 3px,
	// radius 2
	badgeBg := rgba(0x1d1c1d, 0x0f/255.0) // --dt_color-surf-pry
	if th.Dark {
		badgeBg = rgba(0xf8f8f8, 0x0f/255.0)
	}
	rrect(gtx, image.Rect(bx, by, bx+badgeW, by+bh), dp(gtx, 2), badgeBg)
	at(gtx, image.Pt(bx+dp(gtx, 3), by), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 10, theme.Bold, cl.Fg2, "APP")
		})
	})
	if vip {
		vx := bx + badgeW + dp(gtx, 8)
		at(gtx, image.Pt(vx, ty+(nameLH-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "vip", 20, cl.Fg)
		})
	}
	if devH > 0 {
		at(gtx, image.Pt(tx, ty+nameLH), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, devH)}
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, "by "+c.Developer, 1)
		})
	}
	y := rowH
	if d := strings.TrimSpace(c.Desc); d != "" {
		y += gap
		dd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, d, 0)
		})
		y += dd.Size.Y
	}
	if c.External && v.team != "" {
		y += gap
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			icon(gtx, "buildings", 20, cl.Fg2)
			at(gtx, image.Pt(dp(gtx, 28), 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 28)), dp(gtx, 20))}
				return vcenter(gtx, dp(gtx, 20), func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg2, name+" is installed on "+v.team)
				})
			})
			return layout.Dimensions{Size: image.Pt(w, dp(gtx, 20))}
		})
		y += dp(gtx, 20)
	}
	if n := c.Memberships; n > 0 {
		// .parityStatusRows .connectedLabel: padding-top 8, gap 8, 15px
		y += gap + dp(gtx, 8)
		lh := dp(gtx, 22)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(0, (lh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "channel", 20, cl.Fg)
			})
			at(gtx, image.Pt(dp(gtx, 28), 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 28)), lh)}
				return para(gtx, th, 15, 22, theme.Regular, cl.Fg, adActiveIn(n), 1)
			})
			return layout.Dimensions{Size: image.Pt(w, lh)}
		})
		y += lh
	}
	// the buttons, 20px under the header (.parityHeader:not(:last-child))
	y += dp(gtx, 20)
	bh36 := dp(gtx, 36)
	items := v.overflow(c)
	moreW := 0
	if len(items) > 0 {
		moreW = bh36 + dp(gtx, 8)
	}
	label, ic := "Open App", "apps"
	if adOffersMessage(c) {
		label, ic = "Message", "message"
	}
	if c.AppUserID != "" {
		mw := maxInt(bh36, w-moreW)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return pfBtn(gtx, th, BtnMedium, pfBtnSpec{c: &v.mainBtn, label: label, lead: ic}, mw)
		})
	}
	if len(items) > 0 {
		kr := image.Rect(w-bh36, y, w, y+bh36)
		at(gtx, kr.Min, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(bh36, bh36)}
			return Button(gtx, th, &v.moreBtn, BtnOutline, BtnMedium, "", "kebab", 0)
		})
		// pane coords: the section's 20px margin and the header's 49
		v.moreBox = kr.Add(image.Pt(dp(gtx, 20), dp(gtx, 49)+dp(gtx, 20)-v.list.Position.Offset))
	}
	if c.AppUserID != "" || len(items) > 0 {
		y += bh36
	}
	return y
}

// adActiveIn is socialProofActiveChannelIndicator (PsgJ).
func adActiveIn(n int) string {
	if n == 1 {
		return "Active in 1 channel or DM"
	}
	return "Active in " + strconv.Itoa(n) + " channels and DMs"
}

//go:embed assets/magnifying-glass-d8a6430@512.png
var adGlassPNG []byte

var (
	adGlassOnce sync.Once
	adGlassImg  paint.ImageOp
	adGlassOK   bool
)

// adGlass is AppNotFound's magnifying-glass-d8a6430.svg (100×68, from
// Slack's CDN), rasterized at 512px wide.
func adGlass() (paint.ImageOp, bool) {
	adGlassOnce.Do(func() {
		img, err := png.Decode(bytes.NewReader(adGlassPNG))
		if err != nil {
			return
		}
		adGlassImg, adGlassOK = paint.NewImageOp(img), true
	})
	return adGlassImg, adGlassOK
}

// notFound is AppNotFound (gpb+): a centered column (gap 8, padding 16)
// of the 128px image (the 100×68 SVG fit into it), the title and the
// message.
func (v *AppDetailsView) notFound(gtx layout.Context, th *theme.Theme, cl *Colors, w, h int) layout.Dimensions {
	pad := dp(gtx, 16)
	iw := maxInt(1, w-2*pad)
	box := dp(gtx, 128)
	title := "App not found"
	msg := "The app may have been uninstalled or you do not have permission to view it."
	tc, td := record(loose(gtx, image.Pt(iw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return paraCenteredW(gtx, th, 18, 24, theme.Black, cl.Fg, title, iw)
	})
	mc, md := record(loose(gtx, image.Pt(iw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return paraCenteredW(gtx, th, 15, 22, theme.Regular, cl.Fg, msg, iw)
	})
	gap := dp(gtx, 8)
	total := box + gap + td.Size.Y + gap + md.Size.Y
	y := maxInt(pad, (h-total)/2)
	if img, ok := adGlass(); ok {
		gh := box * 68 / 100
		at(gtx, image.Pt((w-box)/2, y+(box-gh)/2), func(gtx layout.Context) layout.Dimensions {
			return theme.Image(gtx, img, image.Pt(box, gh), 0)
		})
	}
	y += box + gap
	macroAt(gtx, tc, image.Pt(pad, y))
	y += td.Size.Y + gap
	macroAt(gtx, mc, image.Pt(pad, y))
	return layout.Dimensions{Size: image.Pt(w, h)}
}
