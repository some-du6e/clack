package ui

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"math"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// Preferences sections Slack has beyond Clack's original five: each row
// either reads and writes the users.prefs key Slack's dialog uses (and
// drives Clack's behaviour where Clack has it), or shows the way Slack
// shows a disabled row. Keys and calls are from Slack's
// gantry-v2-async-prefs-dialog chunk (2026-11).

var errNoCore = errors.New("not signed in")

// ---- writes (the functions the rows call) ----

// setSlackPref writes one users.prefs key through Slack's setUserPrefByApi
// thunk, so plugins see (and may change) it; the native is core.SetPref
// (users.prefs.set name/value). done runs on the UI goroutine.
func (a *App) setSlackPref(name string, value any, done func(error)) {
	a.dispatch("setUserPrefByApi", map[string]any{"pref": name, "value": value},
		func(ctx context.Context, args map[string]any) (any, error) {
			n := argStr(args, "pref")
			if n == "" {
				n = name
			}
			err := errNoCore
			if c := a.Core(); c != nil {
				err = c.SetPref(ctx, n, prefValue(args["value"]))
			}
			a.prefWritten(err, done)
			return nil, err
		})
}

// setSlackPrefs writes several keys in one users.prefs.set (Slack's
// setMultipleUserPrefsByApi: Mark as read's two read-state prefs).
func (a *App) setSlackPrefs(kv map[string]any, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := errNoCore
		if c := a.Core(); c != nil {
			err = c.SetMultiplePrefs(ctx, kv)
		}
		a.prefWritten(err, done)
	}()
}

// setNotifyPref writes a global notification pref
// (users.prefs.setNotifications global=true, like Slack's
// NotificationsGlobalPrefCheckbox).
func (a *App) setNotifyPref(name string, value any, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := errNoCore
		if c := a.Core(); c != nil {
			err = c.SetGlobalNotifyPref(ctx, name, value)
		}
		a.prefWritten(err, done)
	}()
}

// setNotifyPrefs writes several global notification prefs in one call
// (Slack's setNotificationsMultiGlobalPrefsByApi).
func (a *App) setNotifyPrefs(kv map[string]any, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := errNoCore
		if c := a.Core(); c != nil {
			err = c.SetGlobalNotifyPrefs(ctx, kv)
		}
		a.prefWritten(err, done)
	}()
}

// removeVIP takes a user off the VIP list (users.priority.remove).
func (a *App) removeVIP(user string, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := errNoCore
		if c := a.Core(); c != nil {
			err = c.RemoveVIP(ctx, user)
		}
		a.prefWritten(err, done)
	}()
}

func (a *App) prefWritten(err error, done func(error)) {
	a.Post(func() {
		if err != nil {
			a.Toast("Couldn't save that preference: " + err.Error())
		}
		if done != nil {
			done(err)
		}
		a.Invalidate()
	})
}

// prefValue is a thunk's value as core.SetPref takes it (plugins hand
// numbers back as json.Number or float64).
func prefValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return int(i)
		}
		return x.String()
	case float64:
		if x == math.Trunc(x) {
			return int(x)
		}
	}
	return v
}

// ---- reads, with the value just written shown until the write lands ----

func (p *prefsModal) pending(k string) (any, bool) {
	v, ok := p.pend[k]
	return v, ok
}

func (p *prefsModal) prefBool(name string, def bool) bool {
	if v, ok := p.pending(name); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	if c := p.a.Core(); c != nil {
		return c.PrefBool(name, def)
	}
	return def
}

func (p *prefsModal) prefStr(name string) string {
	if v, ok := p.pending(name); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if c := p.a.Core(); c != nil {
		return c.PrefString(name)
	}
	return ""
}

func (p *prefsModal) prefInt(name string, def int) int {
	if v, ok := p.pending(name); ok {
		if n, ok := v.(int); ok {
			return n
		}
	}
	if c := p.a.Core(); c != nil {
		return c.PrefInt(name, def)
	}
	return def
}

func (p *prefsModal) notifyBool(name string, def bool) bool {
	if v, ok := p.pending("notify:" + name); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	if c := p.a.Core(); c != nil {
		return c.GlobalNotifyBool(name, def)
	}
	return def
}

func (p *prefsModal) notifyStr(name string) string {
	if v, ok := p.pending("notify:" + name); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if c := p.a.Core(); c != nil {
		return c.GlobalNotifyString(name)
	}
	return ""
}

func (p *prefsModal) hold(k string, v any) func(error) {
	if p.pend == nil {
		p.pend = map[string]any{}
	}
	p.pend[k] = v
	return func(error) { delete(p.pend, k) }
}

// setPref is what a pref row calls (setUserPrefByApi).
func (p *prefsModal) setPref(name string, v any) { p.a.setSlackPref(name, v, p.hold(name, v)) }

// setPrefs writes several prefs at once.
func (p *prefsModal) setPrefs(kv map[string]any) {
	var dones []func(error)
	for k, v := range kv {
		dones = append(dones, p.hold(k, v))
	}
	p.a.setSlackPrefs(kv, func(err error) {
		for _, d := range dones {
			d(err)
		}
	})
}

// setNotify writes a global notification pref.
func (p *prefsModal) setNotify(name string, v any) {
	p.a.setNotifyPref(name, v, p.hold("notify:"+name, v))
}

// setNotifies writes several global notification prefs at once.
func (p *prefsModal) setNotifies(kv map[string]any) {
	var dones []func(error)
	for k, v := range kv {
		dones = append(dones, p.hold("notify:"+k, v))
	}
	p.a.setNotifyPrefs(kv, func(err error) {
		for _, d := range dones {
			d(err)
		}
	})
}

// ---- scrolling (pages and the nav outgrow the 624px dialog) ----

// The page's scroll container is Slack's .p-prefs_dialog__panel (padding
// 20px 28px), so it scrolls and clips at its padding box: what overflows
// a control (an input's focus ring, a box-shadow 5px outside it) shows in
// that padding, and a scrolled page runs up under the top padding to the
// dialog's line. The modal hands a page the panel's content box (its
// left edge at the controls'); panelPad is the padding round it (none
// below: the page already reaches the dialog's bottom edge).
func panelPad(gtx layout.Context) image.Point { return image.Pt(gtx.Dp(28), gtx.Dp(20)) }

// panelList lays a page's one-child list out over the panel's padding
// box, w drawing at the content box as before (gtx's max size; w's own
// height is what scrolls).
func panelList(gtx layout.Context, l *layout.List, w layout.Widget) layout.Dimensions {
	sz := gtx.Constraints.Max
	pad := panelPad(gtx)
	t := op.Offset(image.Pt(-pad.X, -pad.Y)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(sz.X+2*pad.X, sz.Y+pad.Y))
	l.Layout(g, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		c := gtx
		c.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: image.Pt(sz.X, 1<<24)}
		ct := op.Offset(pad).Push(gtx.Ops)
		d := w(c)
		ct.Pop()
		return layout.Dimensions{Size: image.Pt(sz.X+2*pad.X, d.Size.Y+pad.Y)}
	})
	t.Pop()
	return layout.Dimensions{Size: sz}
}

type prefsScroll struct {
	sc       gesture.Scroll
	y        int
	contentH int
}

// layout draws w (laid out with unbounded height) scrolled and clipped to
// gtx's max size.
func (s *prefsScroll) layout(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return s.scroll(gtx, image.Rectangle{Max: gtx.Constraints.Max}, w)
}

// panel is layout for a page: scrolled within, and clipped to, the
// panel's padding box round gtx's max size (panelPad).
func (s *prefsScroll) panel(gtx layout.Context, w layout.Widget) layout.Dimensions {
	pad := panelPad(gtx)
	size := gtx.Constraints.Max
	return s.scroll(gtx, image.Rect(-pad.X, -pad.Y, size.X+pad.X, size.Y), w)
}

// scroll draws w at gtx's box scrolled, clipped (and taking the wheel)
// within area.
func (s *prefsScroll) scroll(gtx layout.Context, area image.Rectangle, w layout.Widget) layout.Dimensions {
	vh := gtx.Constraints.Max.Y
	sr := pointer.ScrollRange{Min: -s.y, Max: max(0, s.contentH-vh-s.y)}
	if d := s.sc.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical, pointer.ScrollRange{}, sr); d != 0 {
		s.y += d
	}
	s.y = min(max(s.y, 0), max(0, s.contentH-vh))
	size := gtx.Constraints.Max
	cl := clip.Rect(area).Push(gtx.Ops)
	s.sc.Add(gtx.Ops)
	t := op.Offset(image.Pt(0, -s.y)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(size.X, 1<<20)}
	d := w(g)
	t.Pop()
	cl.Pop()
	s.contentH = d.Size.Y
	return layout.Dimensions{Size: size}
}

// ---- pages ----

// pageFns are Clack's own pages by id (Appearance lives in
// prefs_appearance.go; Availability is popups' page).
var pageFns = map[string]func(p *prefsModal, f *prefsForm){
	"notifications":      (*prefsModal).notificationsPage,
	"vip":                (*prefsModal).vipPage,
	"navigation":         (*prefsModal).navigationPage,
	"sidebar":            (*prefsModal).homePage,
	"messages_media":     (*prefsModal).messagesPage,
	"language_region":    (*prefsModal).languagePage,
	"accessibility":      (*prefsModal).accessibilityPage,
	"mark_as_read":       (*prefsModal).markReadPage,
	"video_audio":        (*prefsModal).audioVideoPage,
	"salesforce":         (*prefsModal).salesforcePage,
	"connected_accounts": (*prefsModal).connectedPage,
	"privacy_visibility": (*prefsModal).privacyPage,
	"advanced":           (*prefsModal).advancedPage,
	clackTabID:           (*prefsModal).clackPage, // clack_prefs.go
}

// ensureUserAsync loads a user Clack hasn't seen (VIP rows).
func ensureUserAsync(a *App, id string) {
	c := a.Core()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c.EnsureUsers(ctx, []string{id})
		a.Invalidate()
	}()
}

// confirmMarkAllRead is Shift+Esc: with confirm_clear_all_unreads (Mark
// as read > "Prompt to confirm", on by default) it asks first, like Slack.
func (a *App) confirmMarkAllRead() {
	if a.data == nil {
		return
	}
	if c := a.Core(); c != nil && !c.PrefBool("confirm_clear_all_unreads", true) {
		a.data.markAllRead()
		return
	}
	a.ShowOverlay(popups.Confirm(a, "Mark all messages as read?", "Every channel will be marked read. This can’t be undone.",
		"Mark as read", false, nil, func() { a.data.markAllRead() }))
}
