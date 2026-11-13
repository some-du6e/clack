package popups

// Slack's apps browser, as channel details open it: openAppsBrowserModal
// (chunk shared-js_modern_redux_actions_open-apps-browser-modal_ts,
// module wrSx; gantry-v2 build d3de3a487b1c, 2026-11-12) with
// addAppsVariant, from the Integrations tab's "Add an App"
// (about-channel-modal IntegrationsSection) and from Agents & apps' "Add
// agents and apps" while agents_in_channels_improvements is off
// (agents-apps-tab +xuY; on, Slack opens the channel invite modal scoped
// to agents and apps instead, which Clack doesn't have). The reads are
// core/apps_index.go.
//
//   - AppsBrowserModal: a FullScreenModal (.c-fullscreen_modal: the
//     window in sk_primary_background; the close button 64×64 at top 48,
//     right 48, the × 32px over "esc" 13px; body padding 15vh 32px 0; the
//     content 640 wide). AppsBrowserHeaderLinks sit at top 32, left 48:
//     "Manage apps…" (settings) and "View Slack Marketplace" (apps), 24
//     apart, sk_foreground_max_solid, underlined on hover.
//   - AppsBrowser: the h2 (34/41 bold, margin-bottom 32) "Add apps to
//     {channelName}", or "Add agents and apps to {channelName}" with the
//     #, under channel_details_agents_apps_tab; then AppsBrowserFilter (a
//     search input, "Search by app name or category", 300 ms debounce:
//     searchAppsIfNeeded).
//   - AppsBrowserResults: category headers (12.8/19.2, padding-bottom 8,
//     sk_foreground_max_solid) and app rows (padding 11px 12px, a 1px
//     #ddd top border; the 36px icon at radius 3.2, 10 before the name
//     15/20.8 bold and the short description sk_foreground_max_solid, one
//     line each; the outline button "Add" / "View" / "Install" at the
//     right). The hovered row is .p-apps_browser__app--selected (#f8f8f8,
//     a #ddd border, radius 6). No results: "There are no apps matching
//     <b>query</b>" centred in 100px.
//   - selectApp: an app that can be added (AfRX vS) goes through
//     addAppToChannelByMemberId (an app user: addAppToChannel's
//     AppPermissionDialog; any other bot: conversations.invite) and the
//     modal closes; an installed one opens its profile (openAboutApp; Clack
//     shows the app card); else the Marketplace page
//     /apps/<id>?utm_source=in-prod&utm_medium=inprod-btn_app_install-index-click.
//
// Not drawn: the free plan's app limit meter (z: not on a paid plan), the
// native channel expert row, the team picker of an org-level install and
// the roadblock for channels apps can't join (8wiL).

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
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	nsAppsBrowser = i18n.NS("apps_browser")     // AppsBrowser and its parts
	nsFullscreen  = i18n.NS("fullscreen_modal") // FullScreenModal's close button (client-boot r0qR)
)

// abDebounce is AppsBrowserFilter's debounce.
const abDebounce = 300 * time.Millisecond

// AppsBrowser is AppsBrowserModal with addAppsVariant for a channel.
type AppsBrowser struct {
	base
	h      Host
	ch     string
	agents bool // channel_details_agents_apps_tab

	q       widget.Editor
	last    string
	debQ    string
	debAt   time.Time
	focused bool

	res     core.AppsIndex
	rows    []core.AppsBrowserRow
	query   string // the query rows answer
	loading bool
	seq     int
	addable map[string]bool

	shield         int
	center         image.Point // where the app card opens (window coords)
	close, manage  widget.Clickable
	market         widget.Clickable
	rowClk, btnClk map[string]*widget.Clickable
	list           widget.List
}

// OpenAppsBrowser opens the browser over everything for channel ch.
func OpenAppsBrowser(h Host, ch string) *AppsBrowser {
	b := &AppsBrowser{h: h, ch: ch, rowClk: map[string]*widget.Clickable{}, btnClk: map[string]*widget.Clickable{}}
	b.q.SingleLine = true
	b.list.Axis = layout.Vertical
	if c := h.Core(); c != nil {
		b.agents = c.Experiment("channel_details_agents_apps_tab") == "on"
	}
	b.fetch("") // componentDidMount: fetchDefaultApps
	h.ShowOverlay(b)
	return b
}

// fetch is searchAppsIfNeeded: the list for "", else the search.
func (b *AppsBrowser) fetch(q string) {
	c := b.h.Core()
	if c == nil {
		return
	}
	b.seq++
	seq, ch := b.seq, b.ch
	b.loading = true
	type answer struct {
		ix      core.AppsIndex
		addable map[string]bool
	}
	run(b.h, func(ctx context.Context) (answer, error) {
		var ix core.AppsIndex
		var err error
		if q == "" {
			ix, err = c.AppsIndexList(ctx)
		} else {
			ix, err = c.AppsIndexSearch(ctx, q)
		}
		if err != nil {
			return answer{}, err
		}
		// componentDidUpdate: getChannelMembershipInfoForUsers for the rows' bots
		return answer{ix, c.AppsAddable(ctx, ch, core.AppsResultRows(ix.Categories))}, nil
	}, func(a answer, err error) {
		if seq != b.seq {
			return // receiveResults keeps only the current query's answer
		}
		b.loading = false
		if err != nil {
			a = answer{}
		}
		b.res, b.rows, b.query, b.addable = a.ix, core.AppsResultRows(a.ix.Categories), q, a.addable
		b.h.Invalidate()
	})
}

// Title is the h2.
func (b *AppsBrowser) Title() string {
	name := ""
	if c := b.h.Core(); c != nil {
		if cv, ok := c.Conv(b.ch); ok {
			name = cv.Name
			if b.agents && cv.Kind == "channel" {
				name = "#" + name // includeChannelPrefix
			}
		}
	}
	if b.agents {
		return nsAppsBrowser.T("Add agents and apps to {channelName}", "channelName", name)
	}
	return nsAppsBrowser.T("Add apps to {channelName}", "channelName", name)
}

// Rows are the rows drawn (tests).
func (b *AppsBrowser) Rows() []core.AppsBrowserRow { return b.rows }

// Loading reports a fetch in flight (tests).
func (b *AppsBrowser) Loading() bool { return b.loading }

// SetQuery types q into the filter (tests).
func (b *AppsBrowser) SetQuery(q string) {
	b.q.SetText(q)
	b.h.Invalidate()
}

// ActionLabel is AppsBrowserActionText's button for an app: Add when it
// can be added, View when installed, else Install.
func (b *AppsBrowser) ActionLabel(a core.AppIndexEntry) string {
	switch {
	case b.addable[a.BotUserID] && a.BotUserID != "":
		return nsAppsBrowser.T("Add")
	case a.Installed:
		return nsAppsBrowser.T("View")
	}
	return nsAppsBrowser.T("Install")
}

// Select is selectApp for app id (tests click through it too).
func (b *AppsBrowser) Select(appID string) {
	for _, r := range b.rows {
		if r.Kind == "app" && r.App.AppID == appID {
			b.selectApp(r.App)
			return
		}
	}
}

func (b *AppsBrowser) selectApp(a core.AppIndexEntry) {
	c := b.h.Core()
	if c == nil {
		return
	}
	switch {
	case b.addable[a.BotUserID] && a.BotUserID != "":
		bot, ch := a.BotUserID, b.ch
		if u, ok := c.User(bot); ok && u.AppUser {
			pfAddAppToChannel(b.h, bot, ch, nil) // addAppToChannel (XcD2)
		} else {
			run(b.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.Invite(ctx, ch, bot) },
				func(struct{}, error) {}) // a failed invite is only logged (eg.A error)
		}
		b.Dismiss()
	case a.Installed:
		b.h.OpenProfile(b.center, a.BotUserID) // openAboutApp
	default:
		openTeamPage(b.h, "apps/"+a.AppID+"?utm_source=in-prod&utm_medium=inprod-btn_app_install-index-click")
	}
}

// openTeamPage opens a page of the team's site in the browser.
func openTeamPage(h Host, path string) {
	c := h.Core()
	if c == nil || c.TeamURL() == "" {
		return
	}
	srOpenLink(h, c.TeamURL()+strings.TrimPrefix(path, "/"))
}

func (b *AppsBrowser) Layout(gtx layout.Context) layout.Dimensions {
	if b.Dismissed() {
		return layout.Dimensions{}
	}
	th := b.h.Theme()
	cl := colorsOf(th)
	full := gtx.Constraints.Max
	b.center = image.Pt(full.X/2, full.Y/3)
	// the window behind takes no input while the modal is up
	func() {
		defer clip.Rect{Max: full}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &b.shield)
		pointer.CursorDefault.Add(gtx.Ops)
	}()
	fill(gtx, image.Rectangle{Max: full}, cl.Bg)

	if !b.focused {
		gtx.Execute(key.FocusCmd{Tag: &b.q})
		b.focused = true
	}
	for {
		if _, ok := b.q.Update(gtx); !ok {
			break
		}
	}
	if t := b.q.Text(); t != b.last {
		b.last = t
		b.debQ, b.debAt = strings.TrimSpace(t), gtx.Now
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(abDebounce)})
	}
	if !b.debAt.IsZero() && gtx.Now.Sub(b.debAt) >= abDebounce {
		b.debAt = time.Time{}
		if b.debQ != b.query || b.loading {
			b.fetch(b.debQ)
		}
	} else if !b.debAt.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: b.debAt.Add(abDebounce)})
	}
	if b.close.Clicked(gtx) {
		b.Dismiss()
		return layout.Dimensions{Size: full}
	}
	if b.manage.Clicked(gtx) {
		openTeamPage(b.h, "apps/manage?utm_source=in-prod&utm_medium=inprod-link_manage_apps-index-click")
	}
	if b.market.Clicked(gtx) {
		openTeamPage(b.h, "apps")
	}
	for _, r := range b.rows {
		if r.Kind != "app" {
			continue
		}
		k := r.App.AppID + "/" + r.App.BotID
		if c := b.rowClk[k]; c != nil && c.Clicked(gtx) {
			b.selectApp(r.App)
		}
		if c := b.btnClk[k]; c != nil && c.Clicked(gtx) {
			b.selectApp(r.App)
		}
	}
	if b.Dismissed() {
		return layout.Dimensions{Size: full}
	}

	// AppsBrowserHeaderLinks
	func() {
		defer op.Offset(image.Pt(dp(gtx, 48), dp(gtx, 32))).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints.Min = image.Point{}
		layout.Flex{Alignment: layout.Middle}.Layout(g,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return b.headerLink(gtx, th, cl, &b.manage, "settings", nsAppsBrowser.T("Manage apps…"))
			}),
			layout.Rigid(layout.Spacer{Width: 24}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return b.headerLink(gtx, th, cl, &b.market, "apps", nsAppsBrowser.T("View Slack Marketplace"))
			}),
		)
	}()
	// the close button
	func() {
		s := dp(gtx, 64)
		defer op.Offset(image.Pt(full.X-dp(gtx, 48)-s, dp(gtx, 48))).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		b.close.Layout(g, func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(s, s)
			fg := cl.Fg2
			if b.close.Hovered() {
				theme.Circle(gtx, image.Pt(s/2, s/2), s, cl.Divider) // sk_foreground_low_solid
				fg = cl.Fg
			}
			pointerCursor(gtx, sz)
			ic := dp(gtx, 32)
			lh := dp(gtx, 16)
			top := (s - ic - lh + dp(gtx, 4)) / 2
			func() {
				defer op.Offset(image.Pt((s-ic)/2, top)).Push(gtx.Ops).Pop()
				icon(gtx, "close", 32, fg)
			}()
			func() {
				lbl := nsFullscreen.T("esc")
				w := textW(gtx, th, 13, theme.Regular, lbl)
				defer op.Offset(image.Pt((s-w)/2, top+ic-dp(gtx, 4))).Push(gtx.Ops).Pop()
				text(gtx, th, 13, theme.Regular, fg, lbl)
			}()
			return layout.Dimensions{Size: sz}
		})
	}()

	// the body: 15vh down, 640 wide, to the bottom
	top := full.Y * 15 / 100
	w := min(dp(gtx, 640), full.X-2*dp(gtx, 32))
	x := (full.X - w) / 2
	func() {
		defer op.Offset(image.Pt(x, top)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, max(0, full.Y-top-dp(gtx, 16))))
		layout.Flex{Axis: layout.Vertical}.Layout(g,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(10).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: 32}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return para(gtx, th, 34, 41, theme.Bold, cl.Fg, b.Title(), 0) // overflow_hidden: wraps, no ellipsis
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return b.filter(gtx, th, cl) }),
					)
				})
			}),
			layout.Rigid(layout.Spacer{Height: 20}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return b.results(gtx, th, cl) }),
		)
	}()
	return layout.Dimensions{Size: full}
}

func (b *AppsBrowser) headerLink(gtx layout.Context, th *theme.Theme, cl *Colors, c *widget.Clickable, ic, lbl string) layout.Dimensions {
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		dims := layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return icon(gtx, ic, 16, cl.Fg2) })
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, cl.Fg2, lbl) }),
		)
		if c.Hovered() {
			fill(gtx, image.Rect(dp(gtx, 20), dims.Size.Y-dp(gtx, 2), dims.Size.X, dims.Size.Y-dp(gtx, 1)), cl.Fg2)
		}
		pointerCursor(gtx, dims.Size)
		return dims
	})
}

// filter is AppsBrowserFilter: the search input (36 high, radius 8, the
// magnifier; a spinner while a search runs).
func (b *AppsBrowser) filter(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	h := dp(gtx, 36)
	r := image.Rect(0, 0, w, h)
	rad := dp(gtx, 8)
	focused := gtx.Focused(&b.q)
	if focused {
		focusRing(gtx, cl, r, rad)
	}
	rrect(gtx, r, rad, cl.Bg)
	if !focused {
		stroke(gtx, r, rad, border1(gtx), cl.InputBorder)
	}
	func() {
		defer op.Offset(image.Pt(dp(gtx, 10), (h-dp(gtx, 20))/2)).Push(gtx.Ops).Pop()
		icon(gtx, "search", 20, cl.Fg2)
	}()
	func() {
		x := dp(gtx, 38)
		defer op.Offset(image.Pt(x, (h-dp(gtx, 22))/2)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w-x-dp(gtx, 10), dp(gtx, 22)))
		if b.q.Len() == 0 {
			para(g, th, 15, 22, theme.Regular, cl.Placeholder, nsAppsBrowser.T("Search by app name or category"), 1)
		}
		th.Editor(g, &b.q, unit.Sp(15), cl.Fg, cl.FocusRing)
	}()
	return layout.Dimensions{Size: r.Max}
}

// results is AppsBrowserResults.
func (b *AppsBrowser) results(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	sz := gtx.Constraints.Max
	if b.loading && len(b.rows) == 0 {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return abSpinner(gtx, dp(gtx, 32), cl.Fg3) })
	}
	if len(b.rows) == 0 {
		if b.query == "" {
			return layout.Dimensions{Size: sz}
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(sz.X, dp(gtx, 100)))
		return layout.Center.Layout(g, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, cl.Fg, nsAppsBrowser.T("There are no apps matching")+" ")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Bold, cl.Fg, b.query) }),
			)
		})
	}
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	return b.list.Layout(gtx, len(b.rows), func(gtx layout.Context, i int) layout.Dimensions {
		r := b.rows[i]
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		switch r.Kind {
		case "category":
			top := float32(10)
			if i > 0 {
				top = 35 // the section's padding and its 25px margin
			}
			return layout.Inset{Top: unit.Dp(top), Left: 10, Right: 10, Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 12.8, 19.2, theme.Regular, cl.Fg2, r.Category.DisplayName, 1)
			})
		case "loading":
			return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return abSpinner(gtx, dp(gtx, 20), cl.Fg3) })
			})
		}
		return layout.Inset{Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return b.appRow(gtx, th, cl, r.App) })
	})
}

func abClick(m map[string]*widget.Clickable, k string) *widget.Clickable {
	c := m[k]
	if c == nil {
		c = &widget.Clickable{}
		m[k] = c
	}
	return c
}

// appRow is AppsBrowserActionText.
func (b *AppsBrowser) appRow(gtx layout.Context, th *theme.Theme, cl *Colors, a core.AppIndexEntry) layout.Dimensions {
	k := a.AppID + "/" + a.BotID
	rc, bc := abClick(b.rowClk, k), abClick(b.btnClk, k)
	return rc.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 11, Bottom: 11, Left: 12, Right: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return b.appIcon(gtx, cl, a) })
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Right: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return para(gtx, th, 15, 20.8, theme.Bold, cl.Fg, a.Name, 1)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return para(gtx, th, 15, 20.8, theme.Regular, cl.Fg2, a.Description, 1)
								}),
							)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return button(gtx, th, bc, BtnOutline, BtnMedium, b.ActionLabel(a), "", 0, false)
					}),
				)
			})
		})
		r := image.Rectangle{Max: d.Size}
		one := border1(gtx)
		if rc.Hovered() || bc.Hovered() { // .p-apps_browser__app--selected
			rad := dp(gtx, 6)
			rrect(gtx, r, rad, abLowSolid(cl))
			rrect(gtx, r.Inset(one), max(0, rad-one), abMinSolid(th, cl))
		} else {
			fill(gtx, image.Rect(0, 0, d.Size.X, one), abLowSolid(cl))
		}
		call.Add(gtx.Ops)
		pointerCursor(gtx, d.Size)
		return d
	})
}

// abLowSolid is sk_foreground_low_solid (#ddd; dark #35373b).
func abLowSolid(cl *Colors) color.NRGBA { return cl.Tab }

// abMinSolid is sk_foreground_min_solid (#f8f8f8; dark #1a1d21).
func abMinSolid(th *theme.Theme, cl *Colors) color.NRGBA {
	if th != nil && th.Dark {
		return cl.BgAlt
	}
	return cl.BgHover
}

// appIcon is the 36px icon at radius .2rem.
func (b *AppsBrowser) appIcon(gtx layout.Context, cl *Colors, a core.AppIndexEntry) layout.Dimensions {
	px := dp(gtx, 36)
	sz := image.Pt(px, px)
	rad := dp(gtx, 3.2)
	if u := a.Icon(72); u != "" {
		if img, ok := b.h.Images().Get(u, px); ok {
			return theme.Image(gtx, img, sz, rad)
		}
	}
	rrect(gtx, image.Rectangle{Max: sz}, rad, cl.Selected)
	return layout.Dimensions{Size: sz}
}

// abSpinner is the loading ring (epSpinner); it asks for frames only
// while drawn.
func abSpinner(gtx layout.Context, d int, c color.NRGBA) layout.Dimensions {
	epSpinner(gtx, d, c)
	return layout.Dimensions{Size: image.Pt(d, d)}
}
