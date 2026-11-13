package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The rail's DMs, Activity, Later and Files peeks (web client build
// f132f0e78468), each a TabPeekTrigger (rail_tab_peek.go) whose content is
// a PeekView (boot-shared 9eDG) around the tab's own list:
//
//   - DMs: Zxh6 DmsPeekView ("Direct messages", the Unreads toggle,
//     DmsListAll compact in peek mode; views/dms_peek.go).
//   - Activity: gOQ8 ActivityPeekTrigger on the Activity tab, and on Home
//     while Activity lives in Home (activity_in_home_prototype) and Home
//     isn't active ("Activity", the Unreads toggle, the Activity page;
//     views/activity_peek.go).
//   - Later: s1p9 LaterPeekView (the tab's name and "{count} in progress",
//     LaterPeekContent; views/later_peek.go).
//   - Files: j33M UnifiedFilesPeekView ("Files", the Starred toggle,
//     UnifiedFilesPeekContent; views/files_peek.go).
//
// PeekView (client-boot-styles): .p-peek_card--as-card is ctr-pry, 360px
// wide, 65vh tall (at most 100vh - 8px; 100vh - 20px when the window is at
// most 584px tall), radius 12, a column; .p-peek_card__header is 44px,
// padding 0 8px 0 16px, 15px black, with the 1px
// rgba(--sk_foreground_low,.13) shadow under it, and the buttons at its
// right (.p-peek_card__buttons, flex); .p-peek_card__header_subtitle is
// 13px regular foreground_max_solid, 8px after the title;
// .p-peek_card__content takes the rest (flex 1) and its list scrolls.
//
// The views are the pages' own instances (a.view by route): the filters
// the toggles change are the pages' (Slack's peeks read the same redux
// state as the tabs), and each peek keeps its own scroll position.

// pagePeekView is a page view's peek-mode drawing.
type pagePeekView interface {
	LayoutPeek(gtx layout.Context) layout.Dimensions
}

// pageTabPeek is one of these peeks.
type pageTabPeek struct {
	a     *App
	key   string // rail key: dms, activity, later, files, home
	route Route  // the page whose list it shows
}

func (a *App) pagePeek(key string) *pageTabPeek {
	if a.pgPeeks == nil {
		a.pgPeeks = map[string]*pageTabPeek{}
	}
	p := a.pgPeeks[key]
	if p == nil {
		r := Route(key)
		if key == "home" {
			r = "activity"
		}
		p = &pageTabPeek{a: a, key: key, route: r}
		a.pgPeeks[key] = p
	}
	return p
}

// activityInHome is 6rqO j (isActivityInHomePrototypeEnabled): Home gets
// Activity's peek.
func (a *App) activityInHome() bool { return a.exp("activity_in_home_prototype") == "on" }

// view is the page's instance (built if the tab hasn't been opened yet).
func (p *pageTabPeek) view() View {
	a := p.a
	if mkPage == nil {
		return nil
	}
	return a.view(p.route, func() View {
		if v := mkPage(a, p.route); v != nil {
			return v
		}
		return &paneStub{a: a, title: pageTitle(p.route)}
	})
}

// opened is the PeekView's mount.
func (p *pageTabPeek) opened() {
	closePeek := func() { p.a.closeTabPeek(p.key) }
	switch v := p.view().(type) {
	case interface {
		PeekOpened(func(), string)
	}:
		v.PeekOpened(closePeek, string(p.route))
	case interface{ PeekOpened(func()) }:
		v.PeekOpened(closePeek)
	}
}

// title is the header's text and subtitle.
func (p *pageTabPeek) title() (title, sub string) {
	v := p.view()
	if p.key == "later" { // LaterPeekView: "To-dos" with the to-dos experiment, else "Later"
		title = p.a.railTabFor(core.TabLater).label
	} else if t, ok := v.(interface{ PeekTitle() string }); ok {
		title = t.PeekTitle()
	}
	if s, ok := v.(interface{ PeekSubtitle() string }); ok {
		sub = s.PeekSubtitle()
	}
	return title, sub
}

// peekCardSize is .p-peek_card--as-card's box in the window.
func (a *App) peekCardSize(gtx layout.Context, w int) image.Point {
	win := a.winSize
	h := min(int(float32(win.Y)*.65), win.Y-gtx.Dp(8))
	if win.Y <= gtx.Dp(584) { // @media (height <= 584px)
		h = win.Y - gtx.Dp(20)
	}
	return image.Pt(w, max(h, gtx.Dp(120)))
}

// layout draws the PeekView.
func (p *pageTabPeek) layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	pl := a.agentsPal()
	sz := a.peekCardSize(gtx, gtx.Dp(360))
	gtx.Constraints = layout.Exact(sz)
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	theme.Fill(gtx, image.Rectangle{Max: sz}, pl.bg)
	v := p.view()
	title, sub := p.title()
	hh := gtx.Dp(44)
	// .p-peek_card__buttons at the header's right (padding-right 8)
	bw := 0
	if b, ok := v.(interface {
		LayoutPeekButtons(gtx layout.Context) layout.Dimensions
	}); ok {
		call, d := measured(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(sz.X/2, hh)}
			return b.LayoutPeekButtons(gtx)
		})
		bw = d.Size.X
		place(gtx, call, image.Pt(sz.X-gtx.Dp(8)-bw, (hh-d.Size.Y)/2))
	}
	tw := max(1, sz.X-gtx.Dp(16+8)-bw-gtx.Dp(8))
	at(gtx, image.Pt(gtx.Dp(16), 0), image.Pt(tw, hh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			ts := theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Black, Color: pl.fg, MaxLines: 1}
			d := a.th.Layout(gtx, ts, title)
			if sub != "" {
				x := d.Size.X + gtx.Dp(8)
				at(gtx, image.Pt(x, gtx.Dp(2)), image.Pt(max(1, tw-x), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
					return a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: theme.Regular, Color: pl.maxSolid, MaxLines: 1}, sub)
				})
			}
			return layout.Dimensions{Size: image.Pt(tw, d.Size.Y)}
		})
	})
	body := image.Rect(0, hh, sz.X, sz.Y)
	if c, ok := v.(pagePeekView); ok {
		at(gtx, body.Min, body.Size(), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(body.Size())
			defer clip.Rect{Max: body.Size()}.Push(gtx.Ops).Pop()
			return c.LayoutPeek(gtx)
		})
	}
	// the header's box-shadow: 0 1px 0 0, over the content's first pixel
	theme.Fill(gtx, image.Rect(0, hh, sz.X, hh+max(1, gtx.Dp(1))), pl.rule)
	return layout.Dimensions{Size: sz}
}
