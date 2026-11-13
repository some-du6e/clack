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

// MessageShortcutItems are the message menu's app shortcuts: the recent
// message shortcuts, then "More message shortcuts…" (Slack's message
// actions menu).
func MessageShortcutItems(h Host, m core.Message) []popups.MenuItem {
	var items []popups.MenuItem
	recentMu.Lock()
	rs := append([]core.AppShortcut(nil), recent...)
	recentMu.Unlock()
	n := 0
	for _, s := range rs {
		if s.Type != "message_action" || n >= 3 {
			continue
		}
		s := s
		n++
		items = append(items, popups.MenuItem{Label: s.Name, Right: s.AppName, Do: func() { RunShortcut(h, s, m.Channel, m.TS) }})
	}
	ch, ts := m.Channel, m.TS
	items = append(items, popups.MenuItem{Label: "More message shortcuts…", Icon: "lightning", Do: func() {
		OpenShortcuts(h, pointerAt(h, image.Point{}), "message_action", ch, ts)
	}})
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

func (p *picker) pick(i int) {
	if i < 0 || i >= len(p.rows) {
		return
	}
	s := p.rows[i]
	p.Dismiss()
	RunShortcut(p.h, s, p.ch, p.ts)
}

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
