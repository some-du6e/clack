package ui

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Mount points for the feature packages. Each returns a placeholder until the
// owning package is merged into the ui branch; mount_features.go (added at
// merge time) replaces these via the factory variables.
var (
	mkChannelView func(a *App, ch string) View
	mkThreadView  func(a *App, ch, ts string) View
	mkComposer    func(a *App, ch, ts string) View
	mkDetails     func(a *App, ch string) View
	mkProfile     func(a *App, user string) View
	mkPage        func(a *App, r Route) View
)

func (a *App) mountChannel(ch string) View {
	if mkChannelView != nil {
		return mkChannelView(a, ch)
	}
	return &placeholder{a: a, text: "Messages (ui-messages)"}
}

func (a *App) mountThread(ch, ts string) View {
	if mkThreadView != nil {
		return mkThreadView(a, ch, ts)
	}
	return &paneStub{a: a, title: "Thread", text: "Thread view (ui-messages)"}
}

func (a *App) mountComposer(ch, ts string) View {
	if mkComposer != nil {
		return mkComposer(a, ch, ts)
	}
	return &composerStub{a: a}
}

func (a *App) mountDetails(ch string) View {
	if mkDetails != nil {
		return mkDetails(a, ch)
	}
	return &paneStub{a: a, title: "Details", text: "Channel details (ui-views)"}
}

func (a *App) mountProfile(user string) View {
	if mkProfile != nil {
		return mkProfile(a, user)
	}
	return &paneStub{a: a, title: "Profile", text: "Profile (ui-popups)"}
}

func (a *App) layoutPage(gtx layout.Context, r Route) layout.Dimensions {
	if r == "" {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	v := a.view(r, func() View {
		if mkPage != nil {
			if v := mkPage(a, r); v != nil {
				return v
			}
		}
		return &paneStub{a: a, title: pageTitle(r), text: "(ui-views)"}
	})
	return v.Layout(gtx)
}

// ---- placeholders ----

type placeholder struct {
	a    *App
	text string
}

func (p *placeholder) Layout(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	c := &p.a.th.C
	tw := p.a.th.Measure(gtx, 15, theme.Regular, p.text)
	at(gtx, image.Pt((sz.X-tw)/2, sz.Y/2-gtx.Dp(11)), image.Pt(tw+2, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return p.a.th.Label(gtx, 15, theme.Regular, c.FgSubtle, p.text)
	})
	return layout.Dimensions{Size: sz}
}

// paneStub is a titled pane (secondary pane or page) placeholder.
type paneStub struct {
	a           *App
	title, text string
}

func (p *paneStub) Layout(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	c := &p.a.th.C
	hh := gtx.Dp(theme.HeaderH)
	at(gtx, image.Pt(gtx.Dp(20), 0), image.Pt(sz.X-gtx.Dp(60), hh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return p.a.th.Label(gtx, 18, theme.Black, c.Fg, p.title)
		})
	})
	theme.Fill(gtx, image.Rect(0, hh-gtx.Dp(1), sz.X, hh), c.Border)
	ph := &placeholder{a: p.a, text: p.text}
	atExact(gtx, image.Pt(0, hh), image.Pt(sz.X, sz.Y-hh), ph.Layout)
	return layout.Dimensions{Size: sz}
}

// composerStub reserves the composer's space with Slack's outline.
type composerStub struct{ a *App }

func (s *composerStub) Layout(gtx layout.Context) layout.Dimensions {
	w := gtx.Constraints.Max.X
	c := &s.a.th.C
	h := gtx.Dp(118) + gtx.Dp(20)
	r := image.Rect(gtx.Dp(20), 0, w-gtx.Dp(20), gtx.Dp(118))
	theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), c.BorderStrong)
	at(gtx, image.Pt(r.Min.X+gtx.Dp(12), gtx.Dp(48)), image.Pt(r.Dx(), gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return s.a.th.Label(gtx, 15, theme.Regular, c.FgSubtle, "Message")
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// ---- actions that open popups (wired to ui-popups when merged) ----

var (
	mkSwitcher    func(a *App) Overlay
	mkSearch      func(a *App, query string) Overlay
	mkMenu        func(a *App, anchor image.Point, items []MenuItem) Overlay
	mkEmojiPicker func(a *App, anchor image.Point, onPick func(string)) Overlay
)

// MenuItem is a generic menu entry the shell builds (team, channel, more,
// history menus).
type MenuItem struct {
	Label    string
	Icon     string
	Hint     string // shortcut text on the right
	Danger   bool
	Divider  bool // draws a separator instead of an item
	Disabled bool
	Do       func()
}

func (a *App) menu(anchor image.Point, items []MenuItem) {
	if mkMenu != nil {
		a.ShowOverlay(mkMenu(a, anchor, items))
		return
	}
	a.ShowOverlay(newShellMenu(a, anchor, items))
}

func (a *App) openSwitcher() {
	if mkSwitcher != nil {
		a.ShowOverlay(mkSwitcher(a))
		return
	}
	a.Toast("Quick switcher arrives with ui-popups")
}

func (a *App) openSearch(q string) {
	if mkSearch != nil {
		a.ShowOverlay(mkSearch(a, q))
		return
	}
	a.Toast("Search arrives with ui-popups")
}
