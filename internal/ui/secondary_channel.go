package ui

// A conversation in the secondary column: where Slack opens an Activity
// item that isn't a thread while the inbox fills the page
// (activity_inbox_remove_persistent_detail_panel; gantry boot-shared GMkG
// handleOnClickActivityItem navigates with forceRenderInSecondary and
// forceTab ActivityInbox, so the inbox stays the Primary view and the
// conversation is a secondary view beside it). On the Activity tab the
// column takes half the region (secondaryDefault, aTtQ / wrZz
// isActivityTab). Its header is the conversation's own, ended by the view
// header's close button (p-ia__view_header__button--close: 28px); Esc
// and ⌘. on the Activity tab close it (N8Zu ClientKeyboardShortcutsActivity).

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

// secChannel is the column's kind for a conversation, outside
// secondary.go's iota block (as secShortcuts).
const secChannel secKind = 65

// secConvPane draws conversation ch in the secondary column.
type secConvPane struct {
	a        *App
	ch       string
	drawing  bool // its header is being drawn (layoutChannelHeader's close)
	close    widget.Clickable
	closeTip popups.TipState
}

func (p *secConvPane) Layout(gtx layout.Context) layout.Dimensions {
	p.drawing = true
	defer func() { p.drawing = false }()
	return p.a.layoutChannel(gtx, p.ch)
}

// OpenChannelSecondary shows conversation ch in the secondary column
// (views.ActivitySecondary). Opened from inside the split view it goes
// there instead, as OpenThread does.
func (a *App) OpenChannelSecondary(ch string) {
	if ch == "" {
		return
	}
	if a.pane == paneSplit {
		a.OpenSplit("c/" + ch)
		return
	}
	s := &a.secondary
	if s.kind == secChannel && s.channel == ch && !s.hidden {
		return
	}
	a.secondary = secondaryState{kind: secChannel, channel: ch, width: s.width}
	a.secondary.view = &secConvPane{a: a, ch: ch}
	a.Invalidate()
}

// SecondaryShows reports whether the secondary column shows conversation
// ch: its thread ts, or with ts "" the conversation itself
// (views.ActivitySecondary).
func (a *App) SecondaryShows(ch, ts string) bool {
	s := &a.secondary
	if !s.open() || s.channel != ch {
		return false
	}
	if ts == "" {
		return s.kind == secChannel
	}
	return s.kind == secThread && s.ts == ts
}

// secChannelPane is the conversation pane whose header is being drawn,
// if the header layoutChannelHeader draws is that one's.
func (a *App) secChannelPane(ch string) *secConvPane {
	if p, ok := a.secondary.view.(*secConvPane); ok && p.drawing && p.ch == ch && a.secondary.kind == secChannel {
		return p
	}
	return nil
}

// closeButton is the view header's close at the header's right end; it
// returns the x the other controls end at.
func (p *secConvPane) closeButton(gtx layout.Context, right, h int) int {
	a := p.a
	btn := gtx.Dp(28)
	c := &a.th.C
	x := right - btn
	at(gtx, image.Pt(x, (h-btn)/2), image.Pt(btn, btn), func(gtx layout.Context) layout.Dimensions {
		d := iconButton(gtx, &p.close, "close", 28, 20, c.FgMuted, c.BgHover, 8)
		p.closeTip.Delay = 300 * time.Millisecond
		popups.Tooltip(gtx, a.th, &p.closeTip, p.close.Hovered(), d.Size, popups.Below, "Close", "")
		return d
	})
	if clicked(&p.close) {
		a.CloseSecondary()
	}
	return x - gtx.Dp(4)
}

// secChannelFocused reports whether the key focus is in the column's
// conversation composer (its own Escape, as the thread pane's).
func (a *App) secChannelFocused(gtx layout.Context) bool {
	s := &a.secondary
	if s.kind != secChannel || !s.open() {
		return false
	}
	c := a.convos[s.channel+"/"]
	if c == nil || c.comp == nil {
		return false
	}
	ft, ok := c.comp.(interface{ FocusTag() any })
	return ok && gtx.Focused(ft.FocusTag())
}

// activityTab reports whether the Activity tab is shown (Slack's
// ClientKeyboardShortcutsActivity keymap).
func (a *App) activityTab() bool { return a.router.Current().Page() == "activity" }

var _ views.ActivitySecondary = (*App)(nil)
