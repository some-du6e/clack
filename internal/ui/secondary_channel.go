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

	"github.com/some-du6e/clack/internal/ui/composer"
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
		s.at = "" // the conversation itself again (OpenMessageSecondary sets it after)
		return
	}
	a.secondary = secondaryState{kind: secChannel, channel: ch, width: s.width, page: a.router.Current().Page()}
	a.secondary.view = &secConvPane{a: a, ch: ch}
	a.Invalidate()
}

// ViewTodo is LaterLabel's "View to-dos" (boot-shared wRj5 f) and the
// rail's Later peek's rows: the To-dos tab (setActiveTab todo, the to-do
// highlighted: setHighlightId, which only the old SavedItem rows read)
// and, without agentic IA (swdb TT), the message opened with forceTab
// Later: with a thread_ts (MessageLabels passes the message's; the peek's
// jumpToSavedMessage its getMessageThreadTs) in its thread at the
// message (jpad UX: replyTs, highlightTs, highlightRoot), else in its
// conversation at the message (R9). The To-dos page is the primary view
// here, so the message goes in the secondary column beside it, as Palc
// puts a record there when the LaterList is the primary view; the page
// selects the row while the column shows it (SecondaryAt). Slack's tab
// switch is async, so the navigation still starts from the page the
// label was clicked on (jumpToThreadReply's prevViewSet: openThreadAt).
func (a *App) ViewTodo(ch, ts, thread string) {
	prev := a.router.Current()
	a.Navigate("later")
	switch {
	case ch == "" || ts == "":
	case thread != "":
		a.openThreadAt(ch, thread, ts, prev)
	default:
		a.OpenMessageSecondary(ch, ts)
	}
}

// secActivity is the column's kind for an Activity item without a
// conversation (a reminder, a system notification).
const secActivity secKind = 66

// secDetailPane draws an Activity item's own detail in the column: Slack
// opens those there too (ActivityLaterReminderDetailsView,
// ActivitySystemNotificationView, with forceRenderInSecondary).
type secDetailPane struct {
	key  string
	draw layout.Widget
}

func (p *secDetailPane) Layout(gtx layout.Context) layout.Dimensions { return p.draw(gtx) }

// OpenDetailSecondary shows draw, the Activity view's detail for item
// key, in the secondary column (views.ActivitySecondary).
func (a *App) OpenDetailSecondary(key string, draw layout.Widget) {
	if key == "" || draw == nil {
		return
	}
	a.secondary = secondaryState{kind: secActivity, width: a.secondary.width, page: a.router.Current().Page()}
	a.secondary.view = &secDetailPane{key: key, draw: draw}
	a.Invalidate()
}

// SecondaryShowsDetail reports whether the column shows item key's detail
// (views.ActivitySecondary).
func (a *App) SecondaryShowsDetail(key string) bool {
	p, ok := a.secondary.view.(*secDetailPane)
	return ok && a.secondary.kind == secActivity && a.secondary.open() && p.key == key
}

// leavePageSecondary closes the conversation or Activity item a page
// opened beside itself when the route leaves that page: the Activity
// tab's (GMkG opens them with forceTab ActivityInbox), To-dos' (the Later
// tab's view set: L1Ms, View to-dos' forceTab Later) and the Saved page's
// (the Home tab's Primary changes) are the tab's, not the next page's. A
// thread stays as any thread pane does (afterRoute).
func (a *App) leavePageSecondary(r Route) {
	if k := a.secondary.kind; (k == secChannel || k == secActivity) && r.Page() != a.secondary.page {
		a.secondary = secondaryState{width: a.secondary.width}
	}
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

// activityStep is the Activity tab's option+up/down (N8Zu
// ClientKeyboardShortcutsActivity; Slack's global keymap PG1Q has none):
// the inbox's previous / next item, unread with shift, wherever the focus
// is in the tab. It reports whether the tab is shown.
func (a *App) activityStep(dir int, unread bool) bool {
	if !a.activityTab() {
		return false
	}
	if v, ok := a.views[a.router.Current()].(interface{ StepItem(dir int, unread bool) }); ok {
		v.StepItem(dir, unread)
	}
	return true
}

// secComposerEd is the editor of the secondary column's conversation or
// thread composer when it has the key focus, else nil.
func (a *App) secComposerEd(gtx layout.Context) *composer.Editor {
	s := &a.secondary
	if !s.open() || (s.kind != secThread && s.kind != secChannel) {
		return nil
	}
	k := s.channel + "/"
	if s.kind == secThread {
		k += s.ts
	}
	c := a.convos[k]
	if c == nil {
		return nil
	}
	for _, cv := range []composerView{c.edit, c.comp} {
		if cv == nil {
			continue
		}
		if ft, ok := cv.(interface{ FocusTag() any }); ok {
			if ed, ok := ft.FocusTag().(*composer.Editor); ok && gtx.Focused(ed) {
				return ed
			}
		}
	}
	return nil
}

// composerEdgeOK is ddmY's rule for the Activity tab's option+up/down over
// a focused composer: through an empty one, over one with text only from
// its start (up) or its end (down).
func composerEdgeOK(ed *composer.Editor, up bool) bool {
	if ed == nil || ed.Empty() {
		return true
	}
	_, caret := ed.Selection()
	if up {
		return caret == 0
	}
	return caret >= ed.Doc().Len()
}

var _ views.ActivitySecondary = (*App)(nil)
