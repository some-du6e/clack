package popups

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// AboutPage is Slack's About page (about_page_AboutPage in module 2Fle,
// build c07ab895506f): an app DM's About tab while agent_about_page is
// on (core.AgentAboutPage; BaseAppSpace draws the older layout without
// it). The model is core.AboutPageOf over apps.about.info; the CSS is
// client-boot-styles' about page modules:
//
//   - .wrapper (scrolls; the tab's list does that here) around
//     .contentRow: padding 24 40 16 40, flex-wrap, gap 8px 32px, an
//     inline-size container. .mainColumn (flex 7, 256…968 wide, column,
//     gap 8, padding-bottom 32) and .sidebarColumn (flex 3, 256…360). At
//     544px or narrower (@container about_page_content) both take the whole
//     line: the sidebar first, then .contentDivider (a 1px otl-ter rule,
//     margin 20 0) when there's main content, then the main column.
//   - Main column: AboutPageEmptyState ("This app has no description." or
//     the internal app's text, content-sec), AboutPageDescription (the
//     heading 22px black, the description's mrkdwn clamped at six lines
//     with Show more / Show less), FeaturesSection (+4jS "tab" variant:
//     Assistant, Shortcuts, Message shortcuts, Slash commands, Calls,
//     Org-wide deployment, MCP servers), AboutPagePermissions
//     ("Configurations": Required and Optional authorizations, a tag over
//     a card of accordion rows per scope group) and
//     AboutPageSecurityCompliance (intro, the General / Privacy & data
//     governance / Certification & compliance / Security accordions, the
//     Scopes accordions). Each section after the first is ruled off (hr
//     otl-ter, margin 20 0).
//   - Sidebar (aside, column, gap 24): the buttons (Add to VIP / Remove
//     from VIP for the app's bot user; Configuration to the app's settings
//     page), Mention (an informative tag "@username"), Categories,
//     Supported languages, Pricing, Learn more & support (support, website,
//     email, privacy policy links with their 20px icons, content-hgl-1),
//     and for App Directory apps the disclaimer and "Report this app"
//     (content-imp). Headings 15px bold content-sec.
//   - Until apps.about.info answers: the 52px spinner, centred
//     (loadingContainer); a failed fetch: AppNotFound (gpb+), "Agent not
//     found" for an AI app with revamp_agent_app_profile.
//
// Not drawn (Clack has no data or surface for them): the app connection
// status, its banners and Connect / Manage (useAppConnectionStatus reads
// apps_connection_status's per-app state, which Clack doesn't fetch, so
// Slack's loading state hides them), the feature examples' modals (their
// /img/app-directory pictures; the links aren't drawn either), a workflow
// app's Configuration (Workflow Builder) and "Created with Workflow
// Builder." note, and the Assistant feature's agent pane. The
// description's clamp cuts the sixth line without -webkit-line-clamp's
// ellipsis.
type AboutPage struct {
	h          Host
	appID, uid string
	list       layout.List

	asked, loading bool
	info           *core.AppAboutInfo
	card           *core.AppCard

	clicks map[string]*widget.Clickable
	open   map[string]bool // accordions and expanded descriptions
	descV  map[string]*richview.View
	descK  map[string]string
}

// NewAboutPage is the About page of the app appID, whose bot user is
// userID (the DM's member).
func NewAboutPage(h Host, appID, userID string) *AboutPage {
	p := &AboutPage{h: h, appID: appID, uid: userID, clicks: map[string]*widget.Clickable{},
		open: map[string]bool{}, descV: map[string]*richview.View{}, descK: map[string]string{}}
	p.list.Axis = layout.Vertical
	return p
}

// AppID is the page's app.
func (p *AboutPage) AppID() string { return p.appID }

// Model is what the page draws from now.
func (p *AboutPage) Model() core.AboutPage {
	c := p.h.Core()
	if c == nil {
		return core.AboutPage{}
	}
	return c.AboutPageOf(p.info, p.loading, p.card)
}

func (p *AboutPage) load() {
	if p.asked {
		return
	}
	p.asked = true
	c := p.h.Core()
	if c == nil {
		return
	}
	if info, ok := c.CachedAppAboutInfo(p.appID); ok {
		p.info = info
	} else {
		p.loading = true
	}
	if card, ok := c.CachedAppCard(p.uid, ""); ok {
		p.card = &card
	}
	appID, uid := p.appID, p.uid
	type res struct {
		info *core.AppAboutInfo
		card *core.AppCard
	}
	run(p.h, func(ctx context.Context) (res, error) {
		var r res
		info, err := c.AppAboutInfoOf(ctx, appID)
		if err != nil {
			return r, err
		}
		r.info = info
		if uid != "" {
			if card, err := c.AppCardOf(ctx, uid, ""); err == nil {
				r.card = &card
				c.EnsureTeam(ctx, card.TopTeam())
			}
		}
		return r, nil
	}, func(r res, err error) {
		p.loading = false
		if err != nil {
			return
		}
		p.info = r.info
		if r.card != nil {
			p.card = r.card
		}
	})
}

func (p *AboutPage) clk(k string) *widget.Clickable {
	c := p.clicks[k]
	if c == nil {
		c = new(widget.Clickable)
		p.clicks[k] = c
	}
	return c
}

// Layout fills the constraints with the page, scrolling.
func (p *AboutPage) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	p.load()
	th := p.h.Theme()
	cl := colorsOf(th)
	sz := gtx.Constraints.Max
	m := p.Model()
	p.handleClicks(gtx, m)
	gtx.Constraints = layout.Exact(sz)
	pal := aboutColors(th)
	switch {
	case p.loading || !m.Loaded && p.h.Core() == nil:
		// loadingContainer: the jumbo (52px) blue spinner, centred
		d := dp(gtx, 52)
		at(gtx, image.Pt((sz.X-d)/2, (sz.Y-d)/2), func(gtx layout.Context) layout.Dimensions {
			return abSpinner(gtx, d, pal.link)
		})
		return layout.Dimensions{Size: sz}
	case !m.Loaded:
		agent := false
		if c := p.h.Core(); c != nil && p.card != nil {
			agent = c.RevampAgentAppProfile() && (p.card.AgentApp || p.card.ActiveAgentApp)
		}
		return appNotFound(gtx, th, cl, sz.X, sz.Y, agent)
	}
	return p.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		return p.content(gtx, th, cl, pal, m, sz.X)
	})
}

// aboutPal is the page's tokens.
type aboutPal struct {
	pry, sec, line, surf, ctr, ctrSec, link, imp color.NRGBA
}

func aboutColors(th *theme.Theme) aboutPal {
	return aboutPal{
		pry:    chipTok{"var(--dt_color-content-pry)", rgb(0x1d1c1d), rgb(0xf8f8f8)}.of(th),
		sec:    chipTok{"var(--dt_color-content-sec)", rgb(0x454447), rgb(0xb9babd)}.of(th),
		line:   chipTok{"var(--dt_color-otl-ter)", rgba8(0x5e5d60, 0x21), rgba8(0x797c81, 0x4d)}.of(th),
		surf:   chipTok{"var(--dt_color-surf-pry)", rgba8(0x1d1c1d, 0x0f), rgba8(0xf8f8f8, 0x0f)}.of(th),
		ctr:    chipTok{"var(--dt_color-ctr-pry)", rgb(0xffffff), rgb(0x1a1d21)}.of(th),
		ctrSec: chipTok{"var(--dt_color-ctr-sec)", rgb(0xf8f8f8), rgb(0x222529)}.of(th),
		link:   chipTok{"var(--dt_color-content-hgl-1)", rgb(0x1264a3), rgb(0x1d9bd1)}.of(th),
		imp:    chipTok{"var(--dt_color-content-imp)", rgb(0xc01343), rgb(0xe8577e)}.of(th),
	}
}

// handleClicks runs the clicks of the last frame's buttons.
func (p *AboutPage) handleClicks(gtx layout.Context, m core.AboutPage) {
	for k, c := range p.clicks {
		if !c.Clicked(gtx) {
			continue
		}
		switch {
		case k == "vip":
			if m.AppUserID != "" {
				u, ok := lookupUser(p.h, m.AppUserID)
				if !ok {
					u = core.User{ID: m.AppUserID, Name: m.Username, IsBot: true}
				}
				pfToggleVIP(p.h, u)
			}
		case k == "settings":
			p.openLink(m.SettingsURL)
		case k == "report":
			if c := p.h.Core(); c != nil {
				p.openLink(c.ReportAppURL(p.appID))
			}
		case strings.HasPrefix(k, "link:"):
			p.openLink(strings.TrimPrefix(k, "link:"))
		case strings.HasPrefix(k, "shot:"):
			i, _ := strconv.Atoi(strings.TrimPrefix(k, "shot:"))
			if i >= 0 && i < len(m.Screenshots) {
				s := m.Screenshots[i]
				p.h.OpenLightbox(richtext.Image{URL: s.Image1600, Alt: m.AppName, Title: m.AppName, W: 1600, H: 1000}, core.Message{})
			}
		case k == "youtube":
			p.openLink(m.YoutubeURL)
		default:
			p.open[k] = !p.open[k]
		}
		p.h.Invalidate()
	}
}

func (p *AboutPage) openLink(u string) {
	if u == "" {
		return
	}
	openurl.OpenExternal(u)
}

// content is the contentRow; it returns the page's height.
func (p *AboutPage) content(gtx layout.Context, th *theme.Theme, cl *Colors, pal aboutPal, m core.AboutPage, w int) layout.Dimensions {
	top, right, bottom, left := dp(gtx, 24), dp(gtx, 40), dp(gtx, 16), dp(gtx, 40)
	inner := maxInt(1, w-left-right)
	on := p.h.Core() != nil && p.h.Core().AgentAboutPage()
	em := on && m.Loaded && !m.HasDescription && !m.HasFeatures && !m.ShowPermissions && !m.SecurityCompliance &&
		m.TokenRotation == nil && !m.HasScopeInfo
	ep := m.HasDescription || m.ShowPermissions || m.SecurityCompliance || m.HasScopeInfo
	divider := ep || em // eb outside an agent DM pane

	y := top
	if m.ShowScreenshots {
		y += p.screenshots(gtx, pal, m, 0, 0, w) - top
		y = maxInt(y, top)
	}
	if inner <= dp(gtx, 544) {
		// stacked: sidebar, divider, main
		h := p.sidebar(gtx, th, cl, pal, m, left, y, inner)
		y += h
		if divider {
			y += dp(gtx, 8) + dp(gtx, 20)
			fill(gtx, image.Rect(left, y, left+inner, y+border1(gtx)), pal.line)
			y += border1(gtx) + dp(gtx, 20)
		}
		y += dp(gtx, 8)
		y += p.main(gtx, th, cl, pal, m, em, on, left, y, inner)
		return layout.Dimensions{Size: image.Pt(w, y+bottom)}
	}
	mainW, sideW := aboutColumns(gtx, inner)
	mh := p.main(gtx, th, cl, pal, m, em, on, left, y, mainW)
	sh := p.sidebar(gtx, th, cl, pal, m, left+mainW+dp(gtx, 32), y, sideW)
	return layout.Dimensions{Size: image.Pt(w, y+maxInt(mh, sh)+bottom)}
}

// aboutColumns resolves the row's flex: flex 7 (256…968) and flex 3
// (256…360) from a zero basis, 32px apart.
func aboutColumns(gtx layout.Context, w int) (mainW, sideW int) {
	free := w - dp(gtx, 32)
	mn, mainMax, sideMax := dp(gtx, 256), dp(gtx, 968), dp(gtx, 360)
	mainW, sideW = free*7/10, free-free*7/10
	switch {
	case sideW < mn:
		sideW, mainW = mn, free-mn
	case sideW > sideMax:
		sideW, mainW = sideMax, free-sideMax
	}
	if mainW > mainMax {
		mainW = mainMax
		sideW = min(max(free-mainMax, mn), sideMax)
	}
	return mainW, sideW
}

// screenshots is ScreenshotRail (bKLX) above the row: .rail padding 16,
// gap 12, 300×176 cards (360×208 from 960px, 400×224 from 1280px),
// radius 8, the YouTube thumbnail first with base-modal and a play icon.
func (p *AboutPage) screenshots(gtx layout.Context, pal aboutPal, m core.AboutPage, x, y, w int) int {
	cw, ch := 300, 176
	if w >= dp(gtx, 1280) {
		cw, ch = 400, 224
	} else if w >= dp(gtx, 960) {
		cw, ch = 360, 208
	}
	cwp, chp := dp(gtx, float32(cw)), dp(gtx, float32(ch))
	pad, gap := dp(gtx, 16), dp(gtx, 12)
	cx := x + pad
	rad := dp(gtx, 8)
	card := func(key, url string, play bool) {
		c := p.clk(key)
		at(gtx, image.Pt(cx, y+pad), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(cwp, chp))
			return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				r := image.Rect(0, 0, cwp, chp)
				if img, ok := p.h.Images().Get(url, cwp); ok {
					theme.Image(gtx, img, r.Size(), rad)
				} else {
					rrect(gtx, r, rad, pal.surf)
				}
				if play {
					rrect(gtx, r, rad, theme.WithAlpha(rgb(0x000000), 0x99))
					is := dp(gtx, 24)
					at(gtx, image.Pt((cwp-is)/2, (chp-is)/2), func(gtx layout.Context) layout.Dimensions {
						return icon(gtx, "play", 24, rgb(0xffffff))
					})
				}
				stroke(gtx, r, rad, border1(gtx), pal.line)
				pointerCursor(gtx, r.Size())
				return layout.Dimensions{Size: r.Size()}
			})
		})
		cx += cwp + gap
	}
	if id := youtubeID(m.YoutubeURL); id != "" {
		card("youtube", "https://i.ytimg.com/vi/"+id+"/hqdefault.jpg", true)
	}
	for i, s := range m.Screenshots {
		card("shot:"+strconv.Itoa(i), s.Image440, false)
	}
	return pad + chp + pad
}

// youtubeID is SM2u getYoutubeIdFromUrl for watch?v=, youtu.be/ and
// /embed/ links.
func youtubeID(u string) string {
	for _, pre := range []string{"v=", "youtu.be/", "/embed/"} {
		if i := strings.Index(u, pre); i >= 0 {
			id := u[i+len(pre):]
			if j := strings.IndexAny(id, "&?#/"); j >= 0 {
				id = id[:j]
			}
			return id
		}
	}
	return ""
}

// ---- the main column ----

// aboutCol stacks a flex column's items, gap apart.
type aboutCol struct {
	gtx    layout.Context
	x, y0  int
	y, gap int
	n      int
}

func (c *aboutCol) item(h func(y int) int) {
	if c.n > 0 {
		c.y += c.gap
	}
	c.y += h(c.y)
	c.n++
}

func (c *aboutCol) height() int { return c.y - c.y0 }

// rule is a section's top hr: margin 20 0, 1px otl-ter.
func (p *AboutPage) rule(gtx layout.Context, pal aboutPal, x, y, w int) int {
	m := dp(gtx, 20)
	fill(gtx, image.Rect(x, y+m, x+w, y+m+border1(gtx)), pal.line)
	return 2*m + border1(gtx)
}

// aboutHeading is a section's h3: 22px black, line-height 30.
func aboutHeading(gtx layout.Context, th *theme.Theme, pal aboutPal, s string, x, y, w int) int {
	d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return para(loose(gtx, image.Pt(w, 1<<20)), th, 22, 30, theme.Black, pal.pry, s, 0)
	})
	return d.Size.Y
}

// aboutBody draws 15/22 text (or 13/18 when small) w wide.
func aboutBody(gtx layout.Context, th *theme.Theme, c color.NRGBA, wt font.Weight, small bool, s string, x, y, w int) int {
	sz, lh := float32(15), float32(22)
	if small {
		sz, lh = 13, 18
	}
	d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return para(loose(gtx, image.Pt(w, 1<<20)), th, sz, lh, wt, c, s, 0)
	})
	return d.Size.Y
}

func (p *AboutPage) main(gtx layout.Context, th *theme.Theme, cl *Colors, pal aboutPal, m core.AboutPage, em, on bool, x, y, w int) int {
	col := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 8)}
	// AboutPageEmptyState
	if on && em {
		msg := "This app has no description."
		if m.InternalApp {
			msg = "This is an internal app. It has no description."
			if m.HomeTeamName != "" {
				msg = "This is an internal app owned by " + m.HomeTeamName + ". It has no description."
			}
		}
		col.item(func(y int) int { return aboutBody(gtx, th, pal.sec, theme.Regular, false, msg, x, y, w) })
	}
	// AboutPageDescription
	name := m.AppName
	if m.AIApp {
		if m.AssistantDescription != "" {
			h := "What can this app do?"
			if name != "" {
				h = "What can " + name + " do?"
			}
			col.item(func(y int) int { return p.description(gtx, th, pal, "assistant", h, m.AssistantDescription, x, y, w) })
		}
		if m.Description != "" {
			h := "More about this app"
			if name != "" {
				h = "More about " + name
			}
			col.item(func(y int) int { return p.description(gtx, th, pal, "more", h, m.Description, x, y, w) })
		}
	} else if m.Description != "" {
		h := firstNonEmptyStr(name, "About this app")
		col.item(func(y int) int { return p.description(gtx, th, pal, "about", h, m.Description, x, y, w) })
	}
	// FeaturesSection
	if m.HasFeatures {
		col.item(func(y int) int { return p.features(gtx, th, cl, pal, m, !m.HasDescription, x, y, w) })
	}
	// AboutPagePermissions
	if m.ShowPermissions && on {
		col.item(func(y int) int { return p.permissions(gtx, th, pal, m, !m.HasDescription && !m.HasFeatures, x, y, w) })
	}
	// AboutPageSecurityCompliance
	if on && (len(m.Security) > 0 || len(m.ScopeGroups) > 0) {
		col.item(func(y int) int {
			return p.security(gtx, th, cl, pal, m, !m.HasDescription && !m.HasFeatures && !m.ShowPermissions, x, y, w)
		})
	}
	return col.height() + dp(gtx, 32) // padding-bottom
}

// description is a description section (.section, column gap 12): the
// heading, then DescriptionSection's mrkdwn clamped at six lines with
// "Show more" (column gap 8, the button flush left).
func (p *AboutPage) description(gtx layout.Context, th *theme.Theme, pal aboutPal, key, title, text string, x, y, w int) int {
	h := aboutHeading(gtx, th, pal, title, x, y, w)
	h += dp(gtx, 12)
	v := p.descV[key]
	if v == nil || p.descK[key] != text {
		blocks := []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: text}}}}
		if c := p.h.Core(); c != nil {
			blocks = c.Mrkdwn(text)
		}
		v = richview.New(richtext.Doc{Blocks: blocks}, richview.Options{NoHoverCards: true})
		p.descV[key], p.descK[key] = v, text
	}
	var st *richview.Style
	if RichStyle != nil {
		st = RichStyle(p.h)
	}
	if st == nil {
		return h + aboutBody(gtx, th, pal.pry, theme.Regular, false, text, x, y+h, w)
	}
	call, d := record(loose(gtx, image.Pt(w, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return v.Layout(gtx, st, topicHandler{p.h})
	})
	limit := 6 * dp(gtx, 22)
	expanded := p.open["desc:"+key]
	over := d.Size.Y > limit
	shown := d.Size.Y
	if over && !expanded {
		shown = limit
	}
	func() {
		defer clip.Rect{Min: image.Pt(x, y+h), Max: image.Pt(x+w, y+h+shown)}.Push(gtx.Ops).Pop()
		macroAt(gtx, call, image.Pt(x, y+h))
	}()
	h += shown
	if over {
		h += dp(gtx, 8)
		label := "Show more"
		if expanded {
			label = "Show less"
		}
		h += p.linkButton(gtx, th, pal.link, "desc:"+key, label, x, y+h)
	}
	return h
}

// linkButton is a ButtonLink (c-link--button): 15/22 link text.
func (p *AboutPage) linkButton(gtx layout.Context, th *theme.Theme, c color.NRGBA, key, label string, x, y int) int {
	b := p.clk(key)
	d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			d := para(loose(gtx, image.Pt(1<<20, 1<<20)), th, 15, 22, theme.Regular, c, label, 1)
			pointerCursor(gtx, d.Size)
			return d
		})
	})
	return d.Size.Y
}

// features is FeaturesSection's "tab" variant.
func (p *AboutPage) features(gtx layout.Context, th *theme.Theme, cl *Colors, pal aboutPal, m core.AboutPage, hideTop bool, x, y, w int) int {
	col := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 8)}
	if !hideTop {
		col.item(func(y int) int { return p.rule(gtx, pal, x, y, w) })
	}
	col.item(func(y int) int { return aboutHeading(gtx, th, pal, "Features", x, y, w) })
	app := firstNonEmptyStr(m.AppName, "App")
	col.item(func(y int) int {
		subs := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 20)}
		sub := func(title, desc string, more func(c *aboutCol)) {
			subs.item(func(y int) int {
				c := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 16)}
				c.item(func(y int) int { return aboutBody(gtx, th, pal.pry, theme.Bold, false, title, x, y, w) })
				c.item(func(y int) int { return aboutBody(gtx, th, pal.sec, theme.Regular, false, desc, x, y, w) })
				if more != nil {
					more(c)
				}
				return c.height()
			})
		}
		count := func(c *aboutCol, s string) {
			c.item(func(y int) int { return aboutBody(gtx, th, pal.sec, theme.Regular, false, s, x, y, w) })
		}
		if m.AIApp || m.AgentApp {
			sub("Assistant", "Chat with this app’s AI-powered assistant anywhere in Slack — proofread copy, summarize messages, generate content, find answers, and more.", nil)
		}
		if n := len(m.GlobalShortcuts); n > 0 {
			sub("Shortcuts", "Take action with this app without leaving Slack. Shortcuts are available from the shortcuts menu and search results.", func(c *aboutCol) {
				count(c, app+" has "+plural(n, "shortcut")+":")
				c.item(func(y int) int { return p.shortcutCard(gtx, th, pal, m.GlobalShortcuts, m.Icon32, x, y, w) })
			})
		}
		if n := len(m.MessageShortcuts); n > 0 {
			sub("Message shortcuts", "Create a task in another service straight from a message. Message shortcuts are available in the More actions menu on any message.", func(c *aboutCol) {
				count(c, app+" has "+plural(n, "message shortcut")+":")
				c.item(func(y int) int { return p.shortcutCard(gtx, th, pal, m.MessageShortcuts, m.Icon32, x, y, w) })
			})
		}
		if n := len(m.SlashCommands); n > 0 {
			sub("Slash commands", "Complete an action just by sending a message. Type / in any conversation to see the slash commands available to you.", func(c *aboutCol) {
				count(c, app+" has "+plural(n, "slash command")+":")
				c.item(func(y int) int { return p.commandCard(gtx, th, pal, m.SlashCommands, x, y, w) })
			})
		}
		if m.CallsApp {
			// the description's phone glyph (an inline 16px "call" icon in
			// Slack) isn't drawn inside the wrapped text; its link is
			// HelpLink article 216951758 (0xcee6bce)
			subs.item(func(y int) int {
				c := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 16)}
				c.item(func(y int) int { return aboutBody(gtx, th, pal.pry, theme.Bold, false, "Calls", x, y, w) })
				c.item(func(y int) int {
					c2 := *cl
					c2.Fg, c2.Link = pal.sec, pal.link
					d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
						return srLinkPara(gtx, th, &c2, []srLinkSeg{
							{text: "The phone icon starts voice and video calls from Slack. Some apps "},
							{text: "can be set as default calling apps", link: p.clk("link:https://slack.com/help/articles/216951758")},
							{text: ", so you can start calls with that service from the phone icon."},
						}, w)
					})
					return d.Size.Y
				})
				count(c, app+" can be set as a default calling app.")
				return c.height()
			})
		}
		if m.OrgReady {
			sub("Org-wide deployment", "Apps with org-wide deployment can be installed at the org level in an Enterprise Grid org, then deployed across workspaces by an Org Owner or Admin — no need to install the app on each workspace.", nil)
		}
		if n := len(m.MCPServers); n > 0 {
			sub("MCP servers", "MCP servers expose tools that Slackbot can use to work with data outside of Slack.", func(c *aboutCol) {
				count(c, app+" has "+plural(n, "MCP server")+":")
				c.item(func(y int) int { return p.mcpServers(gtx, th, pal, m.MCPServers, x, y, w) })
			})
		}
		return subs.height()
	})
	return col.height()
}

// cardFrame draws a .card's border (1px otl-ter, radius 8) around rows
// of the heights given, a 1px rule between them; it returns the height.
// The cards' ctr-pry fill is the tab's own background.
func cardFrame(gtx layout.Context, pal aboutPal, x, y, w int, rows []func(y int) int) int {
	b := border1(gtx)
	yy := y + b
	var tops []int
	for i, r := range rows {
		if i > 0 {
			tops = append(tops, yy)
			yy += b
		}
		yy += r(yy)
	}
	yy += b
	for _, t := range tops {
		fill(gtx, image.Rect(x+b, t, x+w-b, t+b), pal.line)
	}
	stroke(gtx, image.Rect(x, y, x+w, yy), dp(gtx, 8), b, pal.line)
	return yy - y
}

// shortcutCard is ShortcutList: a .card of rows (padding 16 20, gap 12)
// with the app's 32px icon (radius 4) and the shortcut's name.
func (p *AboutPage) shortcutCard(gtx layout.Context, th *theme.Theme, pal aboutPal, list []core.AppShortcutRef, iconURL string, x, y, w int) int {
	var rows []func(y int) int
	for _, s := range list {
		s := s
		rows = append(rows, func(y int) int {
			px, py := dp(gtx, 20), dp(gtx, 16)
			is := dp(gtx, 32)
			tx := x + px + is + dp(gtx, 12)
			th2 := aboutBody(gtx, th, pal.pry, theme.Regular, false, s.Name, tx, y+py+maxInt(0, (is-dp(gtx, 22))/2), maxInt(1, x+w-px-tx))
			rowH := maxInt(is, th2)
			at(gtx, image.Pt(x+px, y+py+(rowH-is)/2), func(gtx layout.Context) layout.Dimensions {
				if img, ok := p.h.Images().Get(iconURL, is); ok {
					return theme.Image(gtx, img, image.Pt(is, is), dp(gtx, 4))
				}
				rrect(gtx, image.Rect(0, 0, is, is), dp(gtx, 4), pal.surf)
				return layout.Dimensions{Size: image.Pt(is, is)}
			})
			return 2*py + rowH
		})
	}
	return cardFrame(gtx, pal, x, y, w, rows)
}

// commandCard is SlashCommandList: rows (padding 16 20, flex-wrap, gap
// 8 16) of the command pill (13px monospace, ctr-sec, 1px otl-ter,
// radius 4, padding 8 12, line-height 1) and its 15px content-sec
// description.
func (p *AboutPage) commandCard(gtx layout.Context, th *theme.Theme, pal aboutPal, list []core.AppSlashCommand, x, y, w int) int {
	var rows []func(y int) int
	for _, c := range list {
		c := c
		rows = append(rows, func(y int) int {
			px, py := dp(gtx, 20), dp(gtx, 16)
			label := c.Name
			if c.UsageHint != "" {
				label = c.Name + " " + c.UsageHint
			}
			iw := maxInt(1, w-2*px)
			pw := aboutMonoW(gtx, th, label) + 2*dp(gtx, 12)
			pw = min(pw, iw)
			ph := dp(gtx, 13) + 2*dp(gtx, 8)
			at(gtx, image.Pt(x+px, y+py), func(gtx layout.Context) layout.Dimensions {
				r := image.Rect(0, 0, pw, ph)
				rrect(gtx, r, dp(gtx, 4), pal.ctrSec)
				stroke(gtx, r, dp(gtx, 4), border1(gtx), pal.line)
				at(gtx, image.Pt(dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(exact(gtx, image.Pt(pw-2*dp(gtx, 12), ph)), ph, func(gtx layout.Context) layout.Dimensions {
						return aboutMono(gtx, th, pal.pry, label)
					})
				})
				return layout.Dimensions{Size: r.Size()}
			})
			if c.Desc == "" {
				return 2*py + ph
			}
			dw := textW(gtx, th, 15, theme.Regular, c.Desc)
			if pw+dp(gtx, 16)+dw <= iw {
				dy := y + py + (ph-dp(gtx, 22))/2
				aboutBody(gtx, th, pal.sec, theme.Regular, false, c.Desc, x+px+pw+dp(gtx, 16), dy, iw-pw-dp(gtx, 16))
				return 2*py + maxInt(ph, dp(gtx, 22))
			}
			dh := aboutBody(gtx, th, pal.sec, theme.Regular, false, c.Desc, x+px, y+py+ph+dp(gtx, 8), iw)
			return 2*py + ph + dp(gtx, 8) + dh
		})
	}
	return cardFrame(gtx, pal, x, y, w, rows)
}

// mcpServers is the MCP servers list (.list, column gap 8): per server a
// card (padding 20, gap 16) of the header (the name bold, the URL 13px
// monospace, "N tools" in a gray micro tag) and its tool chips.
func (p *AboutPage) mcpServers(gtx layout.Context, th *theme.Theme, pal aboutPal, list []core.AppMCPServer, x, y, w int) int {
	col := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 8)}
	for _, s := range list {
		s := s
		col.item(func(y int) int {
			pad := dp(gtx, 20)
			iw := maxInt(1, w-2*pad)
			cx, cy := x+pad, y+pad
			// header: icon, name, url, count (flex-wrap gap 12)
			hx := cx
			at(gtx, image.Pt(hx, cy+dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "plug", 20, pal.sec) })
			hx += dp(gtx, 20) + dp(gtx, 12)
			nd := at(gtx, image.Pt(hx, cy), func(gtx layout.Context) layout.Dimensions {
				return para(loose(gtx, image.Pt(maxInt(1, cx+iw-hx), 22)), th, 15, 22, theme.Bold, pal.pry, s.Name, 1)
			})
			hx += nd.Size.X + dp(gtx, 12)
			if s.URL != "" {
				ud := at(gtx, image.Pt(hx, cy+dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
					return aboutMono(gtx, th, pal.sec, s.URL)
				})
				hx += ud.Size.X + dp(gtx, 12)
			}
			at(gtx, image.Pt(hx, cy+dp(gtx, 3)), func(gtx layout.Context) layout.Dimensions {
				return Tag(gtx, th, plural(len(s.Tools), "tool"), false, true)
			})
			cy += dp(gtx, 22) + dp(gtx, 16)
			// tools grid: chips (flex-wrap gap 8)
			tx, ty := cx, cy
			chipH := dp(gtx, 13) + 2*dp(gtx, 8)
			for _, t := range s.Tools {
				label := firstNonEmptyStr(t.Title, t.Name)
				cw := min(aboutMonoW(gtx, th, label)+2*dp(gtx, 12), iw)
				if tx > cx && tx+cw > cx+iw {
					tx, ty = cx, ty+chipH+dp(gtx, 8)
				}
				at(gtx, image.Pt(tx, ty), func(gtx layout.Context) layout.Dimensions {
					r := image.Rect(0, 0, cw, chipH)
					rrect(gtx, r, dp(gtx, 4), pal.ctrSec)
					stroke(gtx, r, dp(gtx, 4), border1(gtx), pal.line)
					at(gtx, image.Pt(dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
						return vcenter(exact(gtx, image.Pt(cw-2*dp(gtx, 12), chipH)), chipH, func(gtx layout.Context) layout.Dimensions {
							return aboutMono(gtx, th, pal.pry, label)
						})
					})
					return layout.Dimensions{Size: r.Size()}
				})
				tx += cw + dp(gtx, 8)
			}
			h := ty + chipH - y + pad
			stroke(gtx, image.Rect(x, y, x+w, y+h), dp(gtx, 8), border1(gtx), pal.line)
			return h
		})
	}
	return col.height()
}

// accordion is an accordion row (padding 16 20, gap 12): the optional
// 20px content-sec icon, the 15px heading, the 16px caret (pointing
// down while open); open, panel draws under it.
func (p *AboutPage) accordion(gtx layout.Context, th *theme.Theme, pal aboutPal, key, iconName, title string, x, y, w int, panel func(y int) int) int {
	c := p.clk(key)
	px, py := dp(gtx, 20), dp(gtx, 16)
	open := p.open[key]
	tx := x + px
	if iconName != "" {
		tx += dp(gtx, 20) + dp(gtx, 12)
	}
	cw := dp(gtx, 16)
	hw := maxInt(1, x+w-px-cw-dp(gtx, 12)-tx)
	_, td := record(loose(gtx, image.Pt(hw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, pal.pry, title, 0)
	})
	rowH := 2*py + td.Size.Y
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, rowH))
		return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if iconName != "" {
				at(gtx, image.Pt(px, (rowH-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, iconName, 20, pal.sec)
				})
			}
			at(gtx, image.Pt(tx-x, py), func(gtx layout.Context) layout.Dimensions {
				return para(loose(gtx, image.Pt(hw, 1<<20)), th, 15, 22, theme.Regular, pal.pry, title, 0)
			})
			caret := "caret-right"
			if open {
				caret = "caret-down"
			}
			at(gtx, image.Pt(w-px-cw, (rowH-cw)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, caret, 16, pal.sec)
			})
			pointerCursor(gtx, image.Pt(w, rowH))
			return layout.Dimensions{Size: image.Pt(w, rowH)}
		})
	})
	if !open || panel == nil {
		return rowH
	}
	return rowH + panel(y+rowH)
}

// permissions is AboutPagePermissions' tab variant.
func (p *AboutPage) permissions(gtx layout.Context, th *theme.Theme, pal aboutPal, m core.AboutPage, hideTop bool, x, y, w int) int {
	perm := m.Permissions
	req := perm.RequiredContent != nil || perm.RequiredActions != nil
	opt := perm.OptionalContent != nil || perm.OptionalActions != nil
	if !req && !opt {
		return 0
	}
	col := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 8)}
	if !hideTop {
		col.item(func(y int) int { return p.rule(gtx, pal, x, y, w) })
	}
	col.item(func(y int) int { return aboutHeading(gtx, th, pal, "Configurations", x, y, w) })
	app := firstNonEmptyStr(m.AppName, "App")
	view, do := app+" will be able to view", app+" will be able to do"
	sub := func(kind, title string, content, actions *core.AboutPermissionBlock) {
		col.item(func(y int) int {
			c := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 16)}
			c.item(func(y int) int { return aboutBody(gtx, th, pal.pry, theme.Bold, false, title, x, y, w) })
			if content != nil {
				c.item(func(y int) int { return p.permBlock(gtx, th, pal, kind+":content", content, view, x, y, w) })
			}
			if actions != nil {
				c.item(func(y int) int { return p.permBlock(gtx, th, pal, kind+":actions", actions, do, x, y, w) })
			}
			return c.height()
		})
	}
	if req {
		sub("req", "Required authorizations", perm.RequiredContent, perm.RequiredActions)
	}
	if opt {
		sub("opt", "Optional authorizations", perm.OptionalContent, perm.OptionalActions)
	}
	return col.height()
}

// permBlock is PermissionsBlockView: the tag (surf-pry, radius 4,
// padding 0 4) over a .card of PermissionsAccordionRows; an open row
// lists its descriptions (padding 0 20 16 48; 13px content-sec items,
// padding 4 0 4 16, a bullet in front).
func (p *AboutPage) permBlock(gtx layout.Context, th *theme.Theme, pal aboutPal, key string, b *core.AboutPermissionBlock, tag string, x, y, w int) int {
	tw := textW(gtx, th, 15, theme.Regular, tag) + 2*dp(gtx, 4)
	tw = min(tw, w)
	th1 := dp(gtx, 22)
	rrect(gtx, image.Rect(x, y, x+tw, y+th1), dp(gtx, 4), pal.surf)
	at(gtx, image.Pt(x+dp(gtx, 4), y), func(gtx layout.Context) layout.Dimensions {
		return para(loose(gtx, image.Pt(tw-2*dp(gtx, 4), th1)), th, 15, 22, theme.Regular, pal.pry, tag, 1)
	})
	cy := y + th1 + dp(gtx, 12)
	var rows []func(y int) int
	for _, g := range b.Groups {
		g := g
		k := "perm:" + key + ":" + g.Key
		rows = append(rows, func(y int) int {
			bw := w - 2*border1(gtx)
			return p.accordion(gtx, th, pal, k, core.AboutPermissionIcons[g.Key], g.Heading, x+border1(gtx), y, bw, func(y int) int {
				lx, lw := x+border1(gtx)+dp(gtx, 48), maxInt(1, bw-dp(gtx, 48)-dp(gtx, 20))
				yy := y
				for _, d := range g.Descriptions {
					yy += dp(gtx, 4)
					at(gtx, image.Pt(lx, yy), func(gtx layout.Context) layout.Dimensions {
						return para(loose(gtx, image.Pt(dp(gtx, 16), 18)), th, 15, 18, theme.Regular, pal.sec, "•", 1)
					})
					yy += aboutBody(gtx, th, pal.sec, theme.Regular, true, d, lx+dp(gtx, 16), yy, maxInt(1, lw-dp(gtx, 16)))
					yy += dp(gtx, 4)
				}
				return yy - y + dp(gtx, 16)
			})
		})
	}
	return cy - y + cardFrame(gtx, pal, x, cy, w, rows)
}

// security is AboutPageSecurityCompliance's tab variant.
func (p *AboutPage) security(gtx layout.Context, th *theme.Theme, cl *Colors, pal aboutPal, m core.AboutPage, hideTop bool, x, y, w int) int {
	col := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 8)}
	if !hideTop {
		col.item(func(y int) int { return p.rule(gtx, pal, x, y, w) })
	}
	col.item(func(y int) int { return aboutHeading(gtx, th, pal, "Security & compliance", x, y, w) })
	col.item(func(y int) int {
		// the intro (15px content-sec); "Help Center" is a HelpLink to
		// article 360001670528 (0x53d1c58d80)
		c2 := *cl
		c2.Fg, c2.Link = pal.sec, pal.link
		d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return srLinkPara(gtx, th, &c2, []srLinkSeg{
				{text: "Review the details to better understand this app’s security practices. To learn more about assessing apps for your workspace visit our "},
				{text: "Help Center", link: p.clk("link:https://slack.com/help/articles/360001670528")},
			}, w)
		})
		return d.Size.Y
	})
	if len(m.Security) > 0 {
		col.item(func(y int) int {
			var rows []func(y int) int
			for _, c := range m.Security {
				c := c
				rows = append(rows, func(y int) int {
					bw := w - 2*border1(gtx)
					return p.accordion(gtx, th, pal, "sec:"+c.Title, "", c.Title, x+border1(gtx), y, bw, func(y int) int {
						return p.detailGrid(gtx, th, pal, c.Rows, x+border1(gtx), y, bw)
					})
				})
			}
			return cardFrame(gtx, pal, x, y, w, rows)
		})
	}
	if len(m.ScopeGroups) > 0 {
		col.item(func(y int) int {
			h := dp(gtx, 8)
			h += aboutBody(gtx, th, pal.pry, theme.Bold, false, "Scopes", x, y+h, w)
			h += dp(gtx, 12)
			var rows []func(y int) int
			for _, g := range m.ScopeGroups {
				g := g
				rows = append(rows, func(y int) int {
					bw := w - 2*border1(gtx)
					return p.accordion(gtx, th, pal, "scopes:"+g.Icon, g.Icon, g.Title, x+border1(gtx), y, bw, func(y int) int {
						px := dp(gtx, 20)
						return dp(gtx, 12) + aboutBody(gtx, th, pal.sec, theme.Regular, true, strings.Join(g.Scopes, ", "),
							x+border1(gtx)+px, y+dp(gtx, 12), maxInt(1, bw-2*px)) + dp(gtx, 20)
					})
				})
			}
			return h + cardFrame(gtx, pal, x, y+h, w, rows)
		})
	}
	return col.height()
}

// detailGrid is the open category's panel (padding 12 20 20): a dl grid
// of 2fr 3fr columns, gap 8 16: the label 13px bold content-pry, the
// value 13px content-sec (a link for URLs).
func (p *AboutPage) detailGrid(gtx layout.Context, th *theme.Theme, pal aboutPal, rows []core.AboutDetailRow, x, y, w int) int {
	px := dp(gtx, 20)
	iw := maxInt(1, w-2*px)
	gx := dp(gtx, 16)
	lw := (iw - gx) * 2 / 5
	vw := iw - gx - lw
	yy := y + dp(gtx, 12)
	for i, r := range rows {
		if i > 0 {
			yy += dp(gtx, 8)
		}
		lh := aboutBody(gtx, th, pal.pry, theme.Bold, true, r.Label, x+px, yy, lw)
		var vh int
		if r.Type == "url" {
			k := "link:" + r.Value
			b := p.clk(k)
			d := at(gtx, image.Pt(x+px+lw+gx, yy), func(gtx layout.Context) layout.Dimensions {
				return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					d := para(loose(gtx, image.Pt(vw, 1<<20)), th, 13, 18, theme.Regular, pal.link, r.Value, 0)
					pointerCursor(gtx, d.Size)
					return d
				})
			})
			vh = d.Size.Y
		} else {
			vh = aboutBody(gtx, th, pal.sec, theme.Regular, true, r.Value, x+px+lw+gx, yy, vw)
		}
		yy += maxInt(lh, vh)
	}
	return yy - y + dp(gtx, 20)
}

// ---- the sidebar ----

func (p *AboutPage) sidebar(gtx layout.Context, th *theme.Theme, cl *Colors, pal aboutPal, m core.AboutPage, x, y, w int) int {
	col := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 24)}
	// statusAndActions (the connection status isn't drawn: see the type)
	settings := m.ShowSettingsButton && !m.WorkflowApp && m.SettingsURL != ""
	if m.AppUserID != "" || settings {
		col.item(func(y int) int {
			g := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 12)}
			minW := float32(w) / gtx.Metric.PxPerDp
			if m.AppUserID != "" {
				label, ic := "Add to VIP", "user-add"
				if pfIsVIP(p.h, m.AppUserID) {
					label, ic = "Remove from VIP", "user-hide"
				}
				g.item(func(y int) int {
					d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(w, dp(gtx, 36))}
						return Button(gtx, th, p.clk("vip"), BtnOutline, BtnMedium, label, ic, minW)
					})
					return d.Size.Y
				})
			}
			if settings {
				g.item(func(y int) int {
					d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(w, dp(gtx, 36))}
						return Button(gtx, th, p.clk("settings"), BtnOutline, BtnMedium, "Configuration", "new-window", minW)
					})
					return d.Size.Y
				})
			}
			return g.height()
		})
	}
	section := func(title string, content func(y int) int) {
		col.item(func(y int) int {
			h := aboutBody(gtx, th, pal.sec, theme.Bold, false, title, x, y, w)
			h += dp(gtx, 12)
			return h + content(y+h)
		})
	}
	if m.Username != "" {
		section("Mention", func(y int) int {
			d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				return Tag(gtx, th, "@"+m.Username, true, false)
			})
			return d.Size.Y
		})
	}
	if len(m.Categories) > 0 {
		section("Categories", func(y int) int {
			cx, cy := x, y
			th20 := dp(gtx, 20)
			for _, c := range m.Categories {
				cw := textW(gtx, th, 12, theme.Bold, c.Name) + 2*dp(gtx, 6)
				if cx > x && cx+cw > x+w {
					cx, cy = x, cy+th20+dp(gtx, 8)
				}
				at(gtx, image.Pt(cx, cy), func(gtx layout.Context) layout.Dimensions { return Tag(gtx, th, c.Name, true, false) })
				cx += cw + dp(gtx, 8)
			}
			return cy + th20 - y
		})
	}
	if len(m.Languages) > 0 {
		section("Supported languages", func(y int) int {
			return aboutBody(gtx, th, pal.pry, theme.Regular, false, strings.Join(m.Languages, ", "), x, y, w)
		})
	}
	if m.PricingLabel != "" {
		section("Pricing", func(y int) int { return aboutBody(gtx, th, pal.pry, theme.Regular, false, m.PricingLabel, x, y, w) })
	}
	type link struct{ icon, label, url string }
	var links []link
	if m.SupportURL != "" {
		links = append(links, link{"life-ring", "Get app support", m.SupportURL})
	}
	if m.HomeURL != "" {
		links = append(links, link{"new-window", "Visit developer website", m.HomeURL})
	}
	if m.SupportEmail != "" {
		links = append(links, link{"email", m.SupportEmail, "mailto:" + m.SupportEmail})
	}
	if m.PrivacyURL != "" {
		links = append(links, link{"lock", "Privacy policy", m.PrivacyURL})
	}
	if len(links) > 0 {
		section("Learn more & support", func(y int) int {
			l := &aboutCol{gtx: gtx, x: x, y0: y, y: y, gap: dp(gtx, 12)}
			for _, k := range links {
				k := k
				l.item(func(y int) int { return p.iconLink(gtx, th, pal.link, "link:"+k.url, k.icon, k.label, x, y, w) })
			}
			return l.height()
		})
	}
	if m.ShowReportApp {
		col.item(func(y int) int {
			h := aboutBody(gtx, th, pal.sec, theme.Regular, true, "Slack conducts a brief review of apps in Slack Marketplace and does not endorse or certify these apps. If you encounter inappropriate content or behavior please report it.", x, y, w)
			h += dp(gtx, 8)
			return h + p.iconLink(gtx, th, pal.imp, "report", "flag", "Report this app", x, y+h, w)
		})
	}
	return col.height()
}

// iconLink is a sidebar link row (gap 8): the 20px icon and the link, in
// c.
func (p *AboutPage) iconLink(gtx layout.Context, th *theme.Theme, c color.NRGBA, key, iconName, label string, x, y, w int) int {
	b := p.clk(key)
	is := dp(gtx, 20)
	tx := is + dp(gtx, 8)
	d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			td := at(gtx, image.Pt(tx, 0), func(gtx layout.Context) layout.Dimensions {
				return para(loose(gtx, image.Pt(maxInt(1, w-tx), 1<<20)), th, 15, 22, theme.Regular, c, label, 0)
			})
			at(gtx, image.Pt(0, (dp(gtx, 22)-is)/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, iconName, 20, c) })
			sz := image.Pt(tx+td.Size.X, maxInt(is, td.Size.Y))
			pointerCursor(gtx, sz)
			return layout.Dimensions{Size: sz}
		})
	})
	return d.Size.Y
}

// aboutMonoFont is --font-family-monospace (Monaco, Menlo, Consolas,
// Courier New, monospace) with Clack's own mono face first.
var aboutMonoFont = font.Font{Typeface: font.Typeface(theme.MonoTypeface + ", Monaco, Menlo, Consolas, \"Courier New\", monospace")}

// aboutMono draws one 13px monospace line.
func aboutMono(gtx layout.Context, th *theme.Theme, c color.NRGBA, s string) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	return th.Text(gtx, theme.LabelOpts{Font: aboutMonoFont, Size: unit.Sp(13), MaxLines: 1, Color: c}, s)
}

// aboutMonoW measures aboutMono's line.
func aboutMonoW(gtx layout.Context, th *theme.Theme, s string) int {
	_, d := record(loose(gtx, image.Pt(1<<20, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return aboutMono(gtx, th, color.NRGBA{}, s)
	})
	return d.Size.X
}

// appNotFound is AppNotFound (gpb+) w×h: "Agent not found" and its
// message for an AI app with revamp_agent_app_profile, else the app's.
func appNotFound(gtx layout.Context, th *theme.Theme, cl *Colors, w, h int, agent bool) layout.Dimensions {
	title := "App not found"
	msg := "The app may have been uninstalled or you do not have permission to view it."
	if agent {
		title = "Agent not found"
		msg = "The agent may have been uninstalled or you do not have permission to view it."
	}
	return notFoundBox(gtx, th, cl, w, h, title, msg)
}
