package ui

// The BBM tab's three views (client-bbm-main-view, -more-view and
// -workspace-switcher-view chunks; their styles are module WOop in
// client-boot-styles): a header and a ListViewer of big rows.

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// bbmRow is one ListViewer row: "named-tab" (a tab: its icon on a
// 36px tile, its name at 22px), "control-strip-item" (Preferences) or
// "workspace".
type bbmRow struct {
	kind  string
	key   string // rail key, "preferences", or the team id
	label string
	icon  string
	badge int
	dot   bool
}

// bbmColors are the WOop tokens on the list surface (the sidebar's
// scope), from Clack's sidebar palette (theme/tokens.go maps each field
// to the same token): content-inv-pry text, otl-inv-pry row borders,
// surf-inv-pry icon tiles, surf-inv-ter hovers, base-inv-hgl-1 and
// content-inv-pry for the current workspace. The rows' own fill,
// theme-surf-pry-pressed, has no sidebar-scope field in the palette: it
// is drawn as the dark ramp's value (aubergine-100 at .18) on the
// sidebar's lilac.
type bbmColors struct {
	text, border, tile, hover, fill, cur, curText, sec color.NRGBA
}

func (a *App) bbmColors() bbmColors {
	p := &a.th.C
	base := a.bbmListBase
	lilac := p.ChromeHover
	return bbmColors{
		text:    p.SidebarTextUnread,
		border:  theme.Over(p.SidebarDivider, base),
		tile:    theme.Over(p.SidebarButton, base),
		hover:   theme.Over(p.SidebarHover, theme.Over(theme.WithAlpha(lilac, 0x2e), base)),
		fill:    theme.Over(theme.WithAlpha(lilac, 0x2e), base),
		cur:     color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff}, // --dt_color-base-inv-hgl-1
		curText: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, // --dt_color-content-inv-pry
		sec:     theme.Over(p.SidebarText, base),
	}
}

// layoutBBMView draws the BBM tab's current view in the panel.
func (a *App) layoutBBMView(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	r := a.router.Current()
	var rows []bbmRow
	var head func(gtx layout.Context) int
	switch string(r) {
	case "bbm/more":
		rows = a.bbmMoreRows()
		head = func(gtx layout.Context) int { return a.bbmSecondaryHeader(gtx, "More") }
	case "bbm/workspaces":
		rows = a.bbmWorkspaceRows()
		head = func(gtx layout.Context) int { return a.bbmSecondaryHeader(gtx, "Workspaces") }
	default:
		rows = a.bbmMainRows(gtx)
		head = a.bbmMainHeader
	}
	hh := head(gtx)
	lst := &a.bbm.list
	lst.Axis = layout.Vertical
	if p := lst.Position; p.First > 0 || p.Offset > 0 {
		// the header's inset 0 -1px 0 0 otl-inv-pry once the list scrolls
		theme.Fill(gtx, image.Rect(0, hh-gtx.Dp(1), sz.X, hh), a.bbmColors().border)
	}
	// .listViewerContainer{margin-bottom:8px}; .listViewerList{width:
	// calc(100% - 40px);margin:0 auto;padding:8px 0}
	at(gtx, image.Pt(0, hh), image.Pt(sz.X, sz.Y-hh-gtx.Dp(8)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = gtx.Constraints.Max
		return lst.Layout(gtx, len(rows)+2, func(gtx layout.Context, i int) layout.Dimensions {
			if i == 0 || i == len(rows)+1 {
				return layout.Dimensions{Size: image.Pt(sz.X, gtx.Dp(8))}
			}
			return a.bbmRowLayout(gtx, rows[i-1])
		})
	})
	return layout.Dimensions{Size: sz}
}

// bbmMainRows is useListForMain: the tabs the rail would show
// (useTabRailOrganizer, the window's height), More among them; picking
// More opens BBMMore.
func (a *App) bbmMainRows(gtx layout.Context) []bbmRow {
	height := a.winSize.Y - gtx.Dp(theme.TopNavH)
	show, overflow := a.railFit(gtx, height-gtx.Dp(61)-gtx.Dp(164))
	a.railOverflow = overflow
	return a.bbmTabRows(show)
}

// bbmMoreRows is useListForMore: the rail's overflow tabs, then
// Preferences.
func (a *App) bbmMoreRows() []bbmRow {
	rows := a.bbmTabRows(a.railOverflow)
	if len(rows) > 0 { // 0===t.length || t.push(preferences)
		rows = append(rows, bbmRow{kind: "control-strip-item", key: "preferences", label: "Preferences", icon: "settings"})
	}
	return rows
}

func (a *App) bbmTabRows(ids []string) []bbmRow {
	var rows []bbmRow
	for _, id := range ids {
		t := a.railTabFor(id)
		n, dot := a.railBadge(t.key)
		rows = append(rows, bbmRow{kind: "named-tab", key: t.key, label: strings.ReplaceAll(t.label, "\n", " "), icon: t.icon, badge: n, dot: dot && n == 0})
	}
	return rows
}

// bbmWorkspaceRows is useListforWorkspaceSwitcher: one row per signed-in
// workspace. Slack ends the list with "Add Workspace"; Clack signs in to
// one workspace, so the row is left out.
func (a *App) bbmWorkspaceRows() []bbmRow {
	return []bbmRow{{kind: "workspace", key: "team", label: a.teamName()}}
}

// bbmMainHeader is BBMWorkspaceView's header: the workspace switcher
// button (.workspaceSwitcherButton: margin 4px, margin-inline-start 30px,
// padding 8px, radius 8; the 34px team icon 12px before the name at 22px
// black; a mention badge 12px after it). It returns its height.
func (a *App) bbmMainHeader(gtx layout.Context) int {
	k := a.bbmColors()
	m := a.model()
	name := a.teamName()
	is := gtx.Dp(34)
	nw := a.th.Measure(gtx, 22, theme.Black, name)
	n := m.mentionTotal()
	bw := 0
	if n > 0 {
		bw = gtx.Dp(12) + max(gtx.Dp(20), a.th.Measure(gtx, 13, theme.Semi, itoa(n))+gtx.Dp(12))
	}
	pad := gtx.Dp(8)
	w := pad + is + gtx.Dp(12) + nw + bw + pad
	h := pad*2 + is
	org := image.Pt(gtx.Dp(4)+gtx.Dp(30), gtx.Dp(2)+gtx.Dp(4))
	btn := a.clicks.get("bbm.switcher")
	at(gtx, org, image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hovered bool) {
			if hovered {
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), k.hover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(pad, pad), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions { return a.teamIcon(gtx, is) })
			at(gtx, image.Pt(pad+is+gtx.Dp(12), 0), image.Pt(nw+2, h), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 22, theme.Black, k.text, name)
				})
			})
			if n > 0 {
				bx := pad + is + gtx.Dp(12) + nw + gtx.Dp(12)
				a.bbmBadge(gtx, image.Pt(bx, (h-gtx.Dp(20))/2), n, 20, 20)
			}
			return layout.Dimensions{Size: image.Pt(w, h)}
		})
	})
	if clicked(btn) {
		a.Navigate("bbm/workspaces") // toBBMWorkspaceSwitcher
	}
	return org.Y + h + gtx.Dp(4)
}

// bbmSecondaryHeader is SecondaryHeader (.secondaryHeader: padding 13px 0
// 13px 38px, gap 12px): the back button (caret-left-full 18, padding 8px,
// radius 4, "Back") and the title at 22px black. Back goes to BBMMain.
func (a *App) bbmSecondaryHeader(gtx layout.Context, title string) int {
	k := a.bbmColors()
	bs := gtx.Dp(34)
	org := image.Pt(gtx.Dp(38), gtx.Dp(13))
	btn := a.clicks.get("bbm.back")
	at(gtx, org, image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hovered bool) {
			if hovered {
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), k.hover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(gtx.Dp(8), gtx.Dp(8)), image.Pt(gtx.Dp(18), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("caret-left-full")(gtx, 18, k.text)
			})
			return layout.Dimensions{Size: image.Pt(bs, bs)}
		})
	})
	tw := a.th.Measure(gtx, 22, theme.Black, title)
	at(gtx, image.Pt(org.X+bs+gtx.Dp(12), org.Y), image.Pt(tw+2, bs), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, bs, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 22, theme.Black, k.text, title)
		})
	})
	if clicked(btn) {
		a.Navigate("bbm")
	}
	return org.Y + bs + gtx.Dp(13)
}

// bbmRowLayout is one row (.row: border 1px otl-inv-pry, radius 12,
// margin 1px 0 8px, padding 12px 16px; hover surf-inv-ter): 62px for a
// tab (36px tile), 66px for a workspace (its name and address).
func (a *App) bbmRowLayout(gtx layout.Context, row bbmRow) layout.Dimensions {
	k := a.bbmColors()
	w := gtx.Constraints.Max.X
	x0 := gtx.Dp(20)
	rw := w - 2*x0
	in := gtx.Dp(1)
	padX, padY := gtx.Dp(16), gtx.Dp(12)
	ch := gtx.Dp(36)
	if row.kind == "workspace" {
		ch = gtx.Dp(40) // 22px name + 18px address
	}
	rh := 2*in + 2*padY + ch
	btn := a.clicks.get("bbm.row." + row.kind + "." + row.key)
	cur := row.kind == "workspace" // the signed-in workspace is the current one
	org := image.Pt(x0, gtx.Dp(1))
	at(gtx, org, image.Pt(rw, rh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hovered bool) {
			r := image.Rectangle{Max: s}
			bg := k.fill
			switch {
			case cur:
				bg = k.cur
			case hovered:
				bg = k.hover
			}
			theme.RRect(gtx, r, gtx.Dp(12), bg)
			theme.Border(gtx, r, gtx.Dp(12), in, k.border)
		}, func(gtx layout.Context) layout.Dimensions {
			cx, cy := in+padX, in+padY
			text := k.text
			if cur {
				text = k.curText
			}
			switch row.kind {
			case "workspace":
				is := gtx.Dp(36)
				iy := cy + (ch-is)/2
				at(gtx, image.Pt(cx, iy), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions { return a.teamIcon(gtx, is) })
				// .currentWorkspaceRow:after: a 44px ring, 2px content-inv-pry,
				// radius 12, at left 12px
				ring := image.Rect(gtx.Dp(12), iy-gtx.Dp(4), gtx.Dp(12)+gtx.Dp(44), iy-gtx.Dp(4)+gtx.Dp(44))
				theme.Border(gtx, ring, gtx.Dp(12), gtx.Dp(2), k.curText)
				tx := cx + is + gtx.Dp(12) // column-gap 12px
				at(gtx, image.Pt(tx, cy), image.Pt(rw-tx-padX, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 15, theme.Bold, text, row.label)
				})
				if u := a.bbmTeamURL(); u != "" {
					at(gtx, image.Pt(tx, cy+gtx.Dp(22)), image.Pt(rw-tx-padX, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Regular, text, u)
					})
				}
				// AccountSwitcherRowShortcut: ⌘1 / "Ctrl 1" at 13px (color: inherit)
				sc := "Ctrl 1"
				if shortcut.Mac() {
					sc = "⌘1"
				}
				sw := a.th.Measure(gtx, 13, theme.Regular, sc)
				at(gtx, image.Pt(rw-in-padX-sw, cy), image.Pt(sw+2, ch), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Regular, text, sc)
					})
				})
			default:
				// .icon{margin-right:12px}; NamedTab's 36px tile (radius 8,
				// surf-inv-pry); a control strip item's icon has no tile
				if row.kind == "named-tab" {
					theme.RRect(gtx, image.Rect(cx, cy, cx+ch, cy+ch), gtx.Dp(8), k.tile)
				}
				at(gtx, image.Pt(cx+(ch-gtx.Dp(20))/2, cy+(ch-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(row.icon)(gtx, 20, text)
				})
				switch { // ViewIcon: the badge (top -4px, right -4px) or the dot
				case row.badge > 0:
					a.bbmBadge(gtx, image.Pt(cx+ch-gtx.Dp(14), cy-gtx.Dp(4)), row.badge, 18, 18)
				case row.dot:
					theme.Circle(gtx, image.Pt(cx+ch-gtx.Dp(4), cy+gtx.Dp(4)), gtx.Dp(8), text)
				}
				// .title{font-size:22px}, black with mentions (.hasMentions)
				weight := theme.Regular
				if row.badge > 0 || row.dot {
					weight = theme.Black
				}
				tx := cx + ch + gtx.Dp(12)
				at(gtx, image.Pt(tx, cy), image.Pt(rw-tx-padX, ch), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 22, weight, text, row.label)
					})
				})
			}
			return layout.Dimensions{Size: image.Pt(rw, rh)}
		})
	})
	if clicked(btn) {
		a.bbmPick(row, image.Pt(org.X, 0))
	}
	return layout.Dimensions{Size: image.Pt(w, gtx.Dp(1)+rh+gtx.Dp(8))}
}

// bbmPick is a row's onListItemSelect: a tab opens it (More: BBMMore),
// Preferences opens Preferences (wk3G), the workspace row switches to
// that workspace (Clack's one workspace is already current).
func (a *App) bbmPick(row bbmRow, org image.Point) {
	switch row.kind {
	case "named-tab":
		if row.key == "browse" { // t.id===k6.Browse → toBBMMore
			a.Navigate("bbm/more")
			return
		}
		a.bbm.tabSwitch = true
		a.railClick(row.key, org)
		a.bbm.tabSwitch = false
	case "control-strip-item":
		a.openPrefs()
	}
}

// bbmTeamURL is the workspace row's address (mAu1 Zl without the scheme).
func (a *App) bbmTeamURL() string {
	c := a.Core()
	if c == nil {
		return ""
	}
	u := strings.TrimSuffix(strings.TrimPrefix(c.TeamURL(), "https://"), "/")
	return u
}
