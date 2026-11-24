package blockkit

import (
	"context"
	"image"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// shortcutsGen bumps when app_actions_updated drops the list.
var shortcutsGen atomic.Uint32

var (
	recentMu sync.Mutex
	recent   []core.AppShortcut // most recent first (memory only)
)

func noteRecent(s core.AppShortcut) {
	recentMu.Lock()
	defer recentMu.Unlock()
	out := []core.AppShortcut{s}
	for _, r := range recent {
		if r.ActionID != s.ActionID && len(out) < 8 {
			out = append(out, r)
		}
	}
	recent = out
}

// RunShortcut runs an app shortcut (message shortcuts pass the
// message's channel and ts). The app's modal opens when Slack sends it.
func RunShortcut(h Host, s core.AppShortcut, ch, ts string) {
	appFor(h) // make sure view events are flowing
	noteRecent(s)
	run(h, 15*time.Second, func(ctx context.Context) (string, error) {
		return dispatchExecute(ctx, h, s, ch, ts)
	}, func(_ string, err error) {
		if err != nil {
			h.Toast(errMessage(err))
		}
	})
}

// The message menu's Connect to apps submenu, as Slack's (JuvA e8 over
// 2LNc selectMessageActions and qGxq, build 83aff18a1847b8e9):
//
//   - its actions are the team's app actions of type message_action (the
//     apps.actions.v2.list answer, core Interactive.CachedShortcuts) for
//     the apps installed where the channel is (teams), in the frecency
//     store's order: the ten message actions used most (tyx1 rM,
//     core.App.MessageActionFrecency, the "frecency" pref every device
//     writes, so the order follows the account), then the rest in the
//     list's order. The first three are rows (appActionToMessageAction,
//     WzzR): the action's name, its app's name beside it, data-qa
//     app_action_<id>, frecencyId "MA<id>", which a pick records.
//   - then the browse row: "More message shortcuts…" while the list loads
//     or once it has any action, "Add a message shortcut…" without one. It
//     opens the shortcuts dialog (app_message_actions_dialog) while the list
//     loads or when there are more than three, else it is a link to the
//     app directory's shortcuts collection (app_directory_link).
//   - no shortcut rows for a viewer who can't run app actions
//     (canUserRunAppAction, RS5d: a single-channel guest; a guest when the
//     team's commands_only_regular is on), nor on a message they don't run
//     on (canRunAppAction, iub5 N$: actionable, no subtype or bot_message
//     or thread_broadcast); the browse row is there for everyone (e8 builds
//     it outside RS5d's check), so the submenu is never empty.
//   - every row is disabled offline (e8: disabled = !Pb, isClientOnline).

// appDirectoryShortcuts is the browse row's href without enough actions.
const appDirectoryShortcuts = "apps/collection/shortcuts?utm_source=in-prod&utm_medium=inprod-get_more_app_actions-message_action_dialog-click"

// canUserRunAppAction is RS5d for the viewer.
func canUserRunAppAction(a *core.App) bool {
	u, ok := a.S.User(a.S.SelfID())
	if !ok || u == nil {
		return true
	}
	if u.IsUltraRestricted {
		return false
	}
	v, _ := a.TeamPref("commands_only_regular")
	return string(v) != "true" || !u.IsRestricted
}

// canRunAppAction is iub5 N$ for m.
func canRunAppAction(a *core.App, m *core.Message) bool {
	if m.Subtype == "file_comment" || !a.Actionable(m) {
		return false
	}
	return m.Subtype == "" || m.Subtype == "bot_message" || m.Subtype == "thread_broadcast"
}

// MessageActions is selectMessageActions for channel ch: the message
// shortcuts in Slack's order, and whether the list is loaded.
func MessageActions(a *core.App, ch string) (acts []core.AppShortcut, loaded bool) {
	all, loaded := a.Interactive().CachedShortcuts()
	teams := channelTeams(a, ch)
	byKey := map[string]core.AppShortcut{}
	var order []string
	for _, s := range all {
		if s.Type != "" && s.Type != "message_action" {
			continue
		}
		if len(teams) > 0 && len(s.Teams) > 0 && !anyIn(s.Teams, teams) {
			continue
		}
		k := core.MessageActionFrecencyPrefix + s.ActionID
		if _, dup := byKey[k]; !dup {
			order = append(order, k)
		}
		byKey[k] = s
	}
	for _, id := range a.MessageActionFrecency() {
		if s, ok := byKey[id]; ok {
			acts = append(acts, s)
			delete(byKey, id)
		}
	}
	for _, k := range order {
		if s, ok := byKey[k]; ok {
			acts = append(acts, s)
			delete(byKey, k)
		}
	}
	return acts, loaded
}

// channelTeams are the workspaces whose apps a channel's message actions
// come from (UaAK getRelevantWorkspaceIdsForChannel): an org-wide
// channel's workspaces (internal_team_ids), a DM's every workspace of the
// viewer's (nil: no filter, Clack doesn't list them), else the team.
func channelTeams(a *core.App, ch string) []string {
	if c, ok := a.S.Conv(ch); ok {
		if len(c.InternalTeamIDs) > 0 {
			return c.InternalTeamIDs
		}
		if c.IsIM || c.IsMPIM {
			return nil
		}
	}
	if t, _ := a.S.TeamIDs(); t != "" {
		return []string{t}
	}
	return nil
}

func anyIn(xs, set []string) bool {
	for _, x := range xs {
		for _, y := range set {
			if x == y {
				return true
			}
		}
	}
	return false
}

// MessageShortcutItems are the Connect to apps submenu's rows for m.
func MessageShortcutItems(h Host, m core.Message) []popups.MenuItem {
	a := h.Core()
	if a == nil {
		return nil
	}
	acts, loaded := MessageActions(a, m.Channel)
	var items []popups.MenuItem
	if canUserRunAppAction(a) && canRunAppAction(a, &m) { // RS5d and N$ gate the shortcut rows only
		for i, s := range acts {
			if i == 3 {
				break
			}
			s := s
			it := popups.MenuItem{Action: s.ActionID, Label: s.Name, Right: s.AppName, Frecency: core.MessageActionFrecencyPrefix + s.ActionID,
				Do: func() { RunShortcut(h, s, m.Channel, m.TS) }}
			if s.ActionID != "" {
				it.QA = "app_action_" + s.ActionID
			}
			items = append(items, it)
		}
	}
	ch, ts := m.Channel, m.TS
	label := "Add a message shortcut…"
	if !loaded || len(acts) > 0 {
		label = "More message shortcuts…"
	}
	browse := popups.MenuItem{Action: "browse_apps", Label: label}
	if !loaded || len(acts) > 3 {
		browse.QA = "app_message_actions_dialog"
		browse.Do = func() { OpenShortcuts(h, pointerAt(h, image.Point{}), "message_action", ch, ts) }
	} else {
		browse.QA = "app_directory_link"
		u := strings.TrimSuffix(a.TeamURL(), "/") + "/" + appDirectoryShortcuts
		browse.Do = func() { openURL(u) }
	}
	items = append(items, browse)
	if !a.ClientOnline() { // e8: every row disabled offline (Pb isClientOnline)
		for i := range items {
			items[i].Disabled = true
		}
	}
	return items
}

// OpenShortcuts opens the shortcuts picker at anchor (window coords):
// kind "global_action" (the composer's shortcuts button) or
// "message_action" (a message's More message shortcuts).
func OpenShortcuts(h Host, anchor image.Point, kind, ch, ts string) {
	p := &picker{h: h, anchor: anchor, kind: kind, ch: ch, ts: ts, loading: true}
	p.ed.SingleLine = true
	p.list.Axis = layout.Vertical
	h.ShowOverlay(p)
	run(h, 20*time.Second, func(ctx context.Context) ([]core.AppShortcut, error) {
		return h.Core().Interactive().Shortcuts(ctx)
	}, func(all []core.AppShortcut, err error) {
		p.loading = false
		if err != nil {
			p.err = "Couldn't load shortcuts"
			return
		}
		if kind == "message_action" {
			// the dialog's actions are selectMessageActions' (2LNc), in
			// its order: the store has the list this fetch brought
			p.all, _ = MessageActions(h.Core(), ch)
			p.filter()
			return
		}
		for _, s := range all {
			if s.Type == kind {
				p.all = append(p.all, s)
			}
		}
		sort.SliceStable(p.all, func(i, j int) bool {
			if p.all[i].AppName != p.all[j].AppName {
				return strings.ToLower(p.all[i].AppName) < strings.ToLower(p.all[j].AppName)
			}
			return strings.ToLower(p.all[i].Name) < strings.ToLower(p.all[j].Name)
		})
		p.filter()
	})
}

// picker is the shortcuts list (search + rows: app icon, name, app).
type picker struct {
	h       Host
	anchor  image.Point
	card    image.Rectangle // the card as last laid out, window coords (Rect)
	kind    string
	ch, ts  string
	ed      widget.Editor
	all     []core.AppShortcut
	rows    []core.AppShortcut
	taps    []tap
	list    layout.List
	sel     int
	loading bool
	err     string
	done    bool
	scrim   int
	tag     int
	focus   bool
}

func (p *picker) Dismissed() bool { return p.done }
func (p *picker) Dismiss()        { p.done = true; p.h.Invalidate() }

func (p *picker) filter() {
	if p.kind == "message_action" {
		p.filterMessageActions()
		return
	}
	q := strings.ToLower(strings.TrimSpace(p.ed.Text()))
	p.rows = p.rows[:0]
	if q == "" && p.kind == "global_action" {
		recentMu.Lock()
		for _, r := range recent {
			if r.Type == p.kind {
				p.rows = append(p.rows, r)
			}
		}
		recentMu.Unlock()
	}
	seen := map[string]bool{}
	for _, r := range p.rows {
		seen[r.ActionID] = true
	}
	for _, s := range p.all {
		if seen[s.ActionID] {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(s.Name), q) || strings.Contains(strings.ToLower(s.AppName), q) ||
			strings.Contains(strings.ToLower(s.Desc), q) {
			p.rows = append(p.rows, s)
		}
	}
	if p.sel >= len(p.rows) {
		p.sel = 0
	}
	p.h.Invalidate()
}

// filterMessageActions is BaseAppActionsDialog's search
// (searchLocalMessageActions): the actions whose name or app name
// (appActionToMessageAction's description) holds every word of the query,
// sorted by frecency for it (sortByFrecency, each known by "MA<id>").
func (p *picker) filterMessageActions() {
	words := strings.Fields(strings.ToLower(p.ed.Text()))
	p.rows = p.rows[:0]
	for _, s := range p.all {
		name, app := strings.ToLower(s.Name), strings.ToLower(s.AppName)
		ok := name != ""
		for _, w := range words {
			if !strings.Contains(name, w) && !strings.Contains(app, w) {
				ok = false
				break
			}
		}
		if ok {
			p.rows = append(p.rows, s)
		}
	}
	if a := p.h.Core(); a != nil {
		p.rows = core.FrecencySortBy(a.FrecencyCache(), p.ed.Text(), p.rows, func(s core.AppShortcut) string {
			return core.MessageActionFrecencyPrefix + s.ActionID
		})
	}
	if p.sel >= len(p.rows) {
		p.sel = 0
	}
	p.h.Invalidate()
}

func (p *picker) pick(i int) {
	if i < 0 || i >= len(p.rows) {
		return
	}
	s := p.rows[i]
	if p.kind == "message_action" {
		// onSelectAction's recordFrecencyEvent {frecencyId, query: the
		// filter's text}
		if a := p.h.Core(); a != nil {
			a.RecordFrecency(core.MessageActionFrecencyPrefix+s.ActionID, p.ed.Text())
		}
	}
	p.Dismiss()
	RunShortcut(p.h, s, p.ch, p.ts)
}

// Rect is the card as last laid out (window coords).
func (p *picker) Rect() image.Rectangle { return p.card }

// InspectRole names the overlay for the inspect tool's report.
func (p *picker) InspectRole() string { return "shortcuts picker" }

func (p *picker) Layout(gtx C) D {
	drain()
	if p.done {
		return D{}
	}
	th := p.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &p.scrim, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			p.Dismiss()
			return D{}
		}
	}
	func() {
		defer clip.Rect{Max: win}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &p.scrim)
	}()
	if !p.focus {
		p.focus = true
		gtx.Execute(key.FocusCmd{Tag: &p.ed})
	}
	for {
		e, ok := gtx.Event(key.Filter{Focus: &p.ed, Name: key.NameUpArrow}, key.Filter{Focus: &p.ed, Name: key.NameDownArrow},
			key.Filter{Focus: &p.ed, Name: key.NameReturn}, key.Filter{Focus: &p.ed, Name: key.NameEnter},
			key.Filter{Focus: &p.ed, Name: key.NameEscape})
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameEscape:
			p.Dismiss()
			return D{}
		case key.NameUpArrow:
			if p.sel > 0 {
				p.sel--
			}
		case key.NameDownArrow:
			if p.sel < len(p.rows)-1 {
				p.sel++
			}
		default:
			p.pick(p.sel)
			return D{}
		}
		if p.sel < p.list.Position.First {
			p.list.Position.First = p.sel
		} else if p.sel > p.list.Position.First+8 {
			p.list.Position.First = p.sel - 8
		}
	}
	for {
		e, ok := p.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.ChangeEvent); ok {
			p.filter()
		}
	}
	if len(p.taps) < len(p.rows) {
		p.taps = append(p.taps, make([]tap, len(p.rows)-len(p.taps))...)
	}
	for i := range p.rows {
		if p.taps[i].clicked(gtx, p.h) {
			p.pick(i)
			return D{}
		}
	}

	w := min(dp(gtx, 420), win.X-dp(gtx, 16))
	rh := dp(gtx, 44)
	head := dp(gtx, 56)
	n := len(p.rows)
	listH := min(max(n, 1), 9) * rh
	h := head + listH + dp(gtx, 8)
	pos := p.anchor.Sub(image.Pt(0, h+dp(gtx, 8)))
	if pos.Y < dp(gtx, 8) {
		pos.Y = p.anchor.Y + dp(gtx, 8)
	}
	pos.X = max(dp(gtx, 8), min(pos.X, win.X-w-dp(gtx, 8)))
	pos.Y = max(dp(gtx, 8), min(pos.Y, win.Y-h-dp(gtx, 8)))
	p.card = image.Rectangle{Min: pos, Max: pos.Add(image.Pt(w, h))}
	defer slackc.InspectCard(gtx, p.card) // the inspect tool: the card is the picker's box
	at(gtx, pos, func(gtx C) D {
		r := image.Rect(0, 0, w, h)
		theme.Shadow(gtx, r, dp(gtx, 8), dp(gtx, 12), cl.Shadow)
		popups.CardRing(gtx, r, dp(gtx, 8), cl.Ring, cl.Bg) // a box-shadow spread
		rrect(gtx, r, dp(gtx, 8), cl.Bg)
		func() {
			defer clip.Rect(r).Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, &p.tag)
		}()
		defer clip.UniformRRect(r, dp(gtx, 8)).Push(gtx.Ops).Pop()
		at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 10)), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(w-dp(gtx, 24), dp(gtx, 36)))
			ph := "Search shortcuts"
			if p.kind == "message_action" {
				ph = "Search message shortcuts"
			}
			return popups.Field(gtx, th, &p.ed, ph, "search", 36, 15)
		})
		at(gtx, image.Pt(0, head), func(gtx C) D {
			if n == 0 {
				msg := "No shortcuts found"
				if p.loading {
					msg = "Loading…"
				} else if p.err != "" {
					msg = p.err
				}
				return at(gtx, image.Pt(dp(gtx, 20), 0), func(gtx C) D {
					return vcenter(gtx, rh, func(gtx C) D { return label(gtx, th, 15, theme.Regular, cl.Text2, msg) })
				})
			}
			gtx.Constraints = layout.Exact(image.Pt(w, listH))
			return p.list.Layout(gtx, n, func(gtx C, i int) D {
				s := &p.rows[i]
				t := &p.taps[i]
				if t.g.Hovered() {
					p.sel = i
				}
				sz := image.Pt(w, rh)
				fg, sub := cl.Text, cl.Text2
				if i == p.sel {
					rrect(gtx, image.Rectangle{Max: sz}, 0, cl.Highlight)
					fg, sub = cl.HighText, cl.HighText
				}
				is := dp(gtx, 24)
				x := dp(gtx, 20)
				if img, ok := p.h.Images().Get(s.Icon, is); ok && s.Icon != "" {
					at(gtx, image.Pt(x, (rh-is)/2), func(gtx C) D { return theme.Image(gtx, img, image.Pt(is, is), dp(gtx, 4)) })
				} else {
					at(gtx, image.Pt(x+dp(gtx, 2), (rh-dp(gtx, 20))/2), func(gtx C) D { return icon(gtx, "lightning", 20, fg) })
				}
				x += is + dp(gtx, 12)
				at(gtx, image.Pt(x, dp(gtx, 4)), func(gtx C) D {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-x-dp(gtx, 16), dp(gtx, 20))}
					return label(gtx, th, 15, theme.Bold, fg, s.Name)
				})
				at(gtx, image.Pt(x, dp(gtx, 24)), func(gtx C) D {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-x-dp(gtx, 16), dp(gtx, 18))}
					txt := s.AppName
					if s.Desc != "" {
						txt += " · " + s.Desc
					}
					return label(gtx, th, 13, theme.Regular, sub, txt)
				})
				t.add(gtx, sz)
				return D{Size: sz}
			})
		})
		return D{Size: r.Max}
	})
	return D{Size: win}
}

// ComposerShortcutItems is the app shortcuts section of the composer's
// "+" menu: recent global shortcuts, then "Browse all shortcuts".
func ComposerShortcutItems(h Host, ch string, anchor image.Rectangle) []popups.MenuItem {
	var items []popups.MenuItem
	recentMu.Lock()
	rs := append([]core.AppShortcut(nil), recent...)
	recentMu.Unlock()
	n := 0
	for _, s := range rs {
		if s.Type != "global_action" || n >= 3 {
			continue
		}
		s := s
		n++
		items = append(items, popups.MenuItem{Label: s.Name, Right: s.AppName, Icon: "lightning", Do: func() { RunShortcut(h, s, ch, "") }})
	}
	items = append(items, popups.MenuItem{Label: "Browse all shortcuts", Icon: "shortcut", Do: func() {
		OpenShortcuts(h, anchor.Min, "global_action", ch, "")
	}})
	return items
}
