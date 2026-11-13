package ui

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// An agent conversation's ⋮ on the Agents & tools peek (web client build
// f132f0e78468, boot-shared XsVf AgentSessionMenuButton):
//
//   - PeekSessionEntityRow's .menu__knBGu: a 24x24 slot 12px after the
//     row's content, opacity 0 until the row is hovered (or has the focus)
//     or its menu is open (.menuOpen__Pd8Jg). In it the small IconButton
//     (28px, radius 8, a 16px ellipsis-vertical-filled in content-sec;
//     content-pry on base-ter when hovered), "Conversation options". A
//     session row has no context menu.
//   - The menu (+RDp useAgentSessionMenuTemplate, surface "peek"; 220px
//     wide at the button's bottom-right): "Pin session" (a checkbox, on
//     while pinned; "Pin conversation" without
//     agent_ux_august2026_release_translations, as Mute and Rename), for a
//     thread that isn't in Slackbot's DM "Mute session" (on while the
//     thread isn't followed), "Rename session" (agent_channel_thread_naming,
//     a thread with an agent_session, not a DM with an agent app), a
//     separator, "Open in split view", "Copy link", a separator and "Close
//     conversation" (danger).
//   - Pin (useSessionActions): the row moves at once (setAgentSessionPinned)
//     and agents.threads.pin / unpin follows; a failure puts it back and
//     toasts "Failed to pin conversation" / "Failed to unpin conversation".
//     Close: agents.threads.archive, the row gone at once, back with "Failed
//     to close conversation" on a failure. Mute: the thread's subscription
//     (toggleThreadSubscription). Rename: EditAgentThreadNameModal
//     (agents.threads.rename). Copy link: the thread's (or the channel's)
//     permalink.
//
// Not ported: "Open in new window" (Clack has one window), and Slackbot
// DM threads' own copy (copyThreadLink for Slackbot's DM).

const agMenuWidth = 220

// agentsPeekMenu is the ⋮ state (UI goroutine only).
type agentsPeekMenu struct {
	btns map[string]*widget.Clickable
	key  string // the row whose menu is open
	menu *popups.Menu
}

func (v *agentsPeek) menuBtn(key string) *widget.Clickable {
	if v.mn.btns == nil {
		v.mn.btns = map[string]*widget.Clickable{}
	}
	b := v.mn.btns[key]
	if b == nil {
		b = new(widget.Clickable)
		v.mn.btns[key] = b
	}
	return b
}

// menuOpenFor reports whether key's menu is showing (menuOpen).
func (v *agentsPeek) menuOpenFor(key string) bool {
	return v.mn.key == key && v.mn.menu != nil && !v.mn.menu.Dismissed()
}

// sessionMenuButton draws the row's .menu__knBGu slot at its right (w is
// the row's width, h its height) and handles its clicks; rowHov is the
// row's hover.
func (v *agentsPeek) sessionMenuButton(gtx layout.Context, s core.AgentSession, key string, w, h int, rowHov bool, p agPal) {
	b := v.menuBtn(key)
	if b.Clicked(gtx) {
		v.openSessionMenu(gtx, s, key)
	}
	slot := gtx.Dp(24)
	bs := gtx.Dp(28)
	org := image.Pt(w-gtx.Dp(16)-slot-(bs-slot)/2, (h-bs)/2)
	show := rowHov || b.Hovered() || v.menuOpenFor(key)
	at(gtx, org, image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(bs, bs)
			if !show { // opacity 0: still there for the pointer
				return layout.Dimensions{Size: sz}
			}
			col := p.sec
			if b.Hovered() {
				theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(8), v.a.peekBaseTer())
				col = p.fg
			}
			d := gtx.Dp(16)
			at(gtx, image.Pt((bs-d)/2, (bs-d)/2), image.Pt(d, d), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("ellipsis-vertical-filled")(gtx, 16, col)
			})
			return layout.Dimensions{Size: sz}
		})
	})
}

// peekBaseTer is --dt_color-base-ter (the IconButton's hover fill).
func (a *App) peekBaseTer() (c color.NRGBA) {
	c = color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}
	if a.th.Dark {
		c = color.NRGBA{R: 0x21, G: 0x24, B: 0x28, A: 0xff}
	}
	theme.SetTok(a.th.Tokens, &c, theme.Both("var(--dt_color-base-ter)"), a.th.Dark)
	return c
}

// openSessionMenu opens s's menu under its ⋮ (bottom-right).
func (v *agentsPeek) openSessionMenu(gtx layout.Context, s core.AgentSession, key string) {
	a := v.a
	b := v.menuBtn(key)
	at := a.ptrWin
	if hs := b.History(); len(hs) > 0 {
		at = at.Sub(hs[len(hs)-1].Position)
	}
	bs := gtx.Dp(28)
	m := popups.NewMenuOpts(a, v.sessionMenu(s), popups.MenuOpts{Width: agMenuWidth,
		Anchor: image.Rectangle{Min: at, Max: at.Add(image.Pt(bs, bs))}, Popover: "bottom-right"})
	v.mn.key, v.mn.menu = key, m
	a.ShowOverlay(m)
}

// agentsT is the agent_tasks namespace (useAgentSessionMenuTemplate).
var agentsT = i18n.NS("agent_tasks")

// sessionMenu is useAgentSessionMenuTemplate's template for s.
func (v *agentsPeek) sessionMenu(s core.AgentSession) []popups.MenuItem {
	a := v.a
	c := a.Core()
	if c == nil {
		return nil
	}
	newStr := c.Experiment("agent_ux_august2026_release_translations") == "on" // M/qt Kw
	pin, mute, rename := agentsT.T("Pin conversation"), agentsT.T("Mute conversation"), agentsT.T("Rename")
	if newStr {
		pin, mute, rename = agentsT.T("Pin session"), agentsT.T("Mute session"), agentsT.T("Rename session")
	}
	thread := ""
	if s.Kind == "thread" {
		thread = s.TS
	}
	slackbotDM := false // D: the session's channel is Slackbot's DM
	if cv, ok := c.Conv(s.Channel); ok && cv.User == "USLACKBOT" {
		slackbotDM = true
	}
	items := []popups.MenuItem{{Key: "pin", Label: pin, Checked: s.Pinned, Do: func() { v.togglePin(s) }}}
	if thread != "" && !slackbotDM {
		followed := s.Root != nil && s.Root.FollowingThread // R1 isThreadSubscribed
		items = append(items, popups.MenuItem{Key: "mute", Label: mute, Checked: !followed, Do: func() {
			ch := s.Channel
			popups.ActDo(a, "", func(ctx context.Context, c *core.App) error { return c.SubscribeThread(ctx, ch, thread, !followed) })
		}})
	}
	// U: agent_channel_thread_naming, a thread with an agent_session, not
	// a DM with an agent app
	if thread != "" && c.Experiment("agent_channel_thread_naming") == "on" && s.Root != nil && s.Root.AgentSession != nil && !c.SessionWithAgentApp(s.Channel) {
		items = append(items, popups.MenuItem{Key: "rename", Label: rename, Do: func() { v.renameSession(s) }})
	}
	items = append(items, popups.Sep())
	route := "c/" + s.Channel
	if thread != "" {
		route += "/t/" + thread
	}
	if split, ok := popups.SplitViewItem(a, route, false); ok {
		split.Label = agentsT.T("Open in split view")
		items = append(items, split)
	}
	var link string
	if thread != "" {
		link = popups.MessageLink(a, s.Channel, thread, thread, "")
	} else {
		link = popups.ChannelLink(a, s.Channel)
	}
	items = append(items, popups.MenuItem{Key: "copy_link", Label: agentsT.T("Copy link"), Icon: "link", DoCtx: func(gtx layout.Context) { popups.CopyText(gtx, link) }},
		popups.Sep(),
		popups.MenuItem{Key: "close", Label: agentsT.T("Close conversation"), Danger: true, Do: func() { v.archiveSession(s) }})
	return items
}

// sessionIndex is s's place in the list (-1 for none).
func (v *agentsPeek) sessionIndex(s core.AgentSession) int {
	for i, x := range v.sessions {
		if x.Kind == s.Kind && x.Channel == s.Channel && x.TS == s.TS {
			return i
		}
	}
	return -1
}

// togglePin is pinAgentSession: the row moves first, the write follows.
func (v *agentsPeek) togglePin(s core.AgentSession) {
	a := v.a
	i := v.sessionIndex(s)
	if i < 0 {
		return
	}
	was := v.sessions[i].Pinned
	v.sessions[i].Pinned = !was
	thread := ""
	if s.Kind == "thread" {
		thread = s.TS
	}
	ch := s.Channel
	failMsg := "Failed to pin conversation"
	if was {
		failMsg = "Failed to unpin conversation"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := a.Core().PinAgentSession(ctx, ch, thread, !was)
		a.Post(func() {
			if err != nil {
				if j := v.sessionIndex(s); j >= 0 {
					v.sessions[j].Pinned = was
				}
				a.Toast(nsSearchT(failMsg))
			}
			a.Invalidate()
		})
	}()
	a.Invalidate()
}

// archiveSession is archiveAgentSession (Close conversation): the row
// goes at once and comes back if the write fails.
func (v *agentsPeek) archiveSession(s core.AgentSession) {
	a := v.a
	i := v.sessionIndex(s)
	if i < 0 {
		return
	}
	prev := append([]core.AgentSession(nil), v.sessions...)
	v.sessions = append(v.sessions[:i:i], v.sessions[i+1:]...)
	thread := ""
	if s.Kind == "thread" {
		thread = s.TS
	}
	ch := s.Channel
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := a.Core().ArchiveAgentSession(ctx, ch, thread)
		a.Post(func() {
			if err != nil {
				v.sessions = prev
				a.Toast(nsSearchT("Failed to close conversation"))
			}
			a.Invalidate()
		})
	}()
	a.Invalidate()
}

// nsSearchT is useSessionActions' strings (the search namespace).
func nsSearchT(s string) string { return i18n.T("search", s) }

// renameSession opens EditAgentThreadNameModal for s.
func (v *agentsPeek) renameSession(s core.AgentSession) {
	a := v.a
	cur := ""
	if s.Root != nil && s.Root.AgentSession != nil {
		cur = s.Root.AgentSession.SessionTitle()
	}
	if cur == "" {
		cur = s.AssistantTitle
	}
	ch, thread := s.Channel, s.TS
	a.ShowOverlay(popups.NewAgentThreadNameModal(a, cur, func(ctx context.Context, name string) error {
		err := a.Core().RenameAgentThread(ctx, ch, thread, name)
		if err == nil {
			a.Post(func() {
				if j := v.sessionIndex(s); j >= 0 && v.sessions[j].Root != nil && v.sessions[j].Root.AgentSession != nil {
					as := *v.sessions[j].Root.AgentSession
					as.RawTitle, _ = json.Marshal(name)
					root := *v.sessions[j].Root
					root.AgentSession = &as
					v.sessions[j].Root = &root
				}
				a.Invalidate()
			})
		}
		return err
	}))
}
