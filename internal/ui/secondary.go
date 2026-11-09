package ui

import (
	"image"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type secKind uint8

const (
	secNone secKind = iota
	secThread
	secDetails
	secProfile
	secSplit
	secHelp // the Help flexpane (update_ui.go)
)

// secondaryState is the resizable right-hand pane (thread, channel details,
// profile).
type secondaryState struct {
	kind    secKind
	channel string
	ts      string
	user    string
	view    View
	width   int  // dp
	hidden  bool // Ctrl+. toggles without forgetting what was open
	drag    gesture.Drag
	dragX   float32
	dragW   float32
}

func (s *secondaryState) open() bool { return s.kind != secNone && !s.hidden }

// OpenThread shows a thread in the secondary pane.
func (a *App) OpenThread(channel, ts string) {
	if a.secondary.kind == secThread && a.secondary.channel == channel && a.secondary.ts == ts && !a.secondary.hidden {
		return
	}
	a.secondary = secondaryState{kind: secThread, channel: channel, ts: ts, width: a.secondary.width}
	a.secondary.view = a.mountThread(channel, ts)
	if a.router.Current().Channel() == channel {
		a.router.Replace(Route("c/" + channel + "/t/" + ts))
	}
	a.Invalidate()
}

// openDetails, when set by a feature package, shows channel details its own
// way (ui-views draws a modal dialog) and reports true.
var openDetails func(a *App, ch string) bool

// OpenDetails shows channel details (modal from ui-views, else the pane).
func (a *App) OpenDetails(channel string) {
	if openDetails != nil && openDetails(a, channel) {
		return
	}
	a.secondary = secondaryState{kind: secDetails, channel: channel, width: a.secondary.width}
	a.secondary.view = a.mountDetails(channel)
	a.Invalidate()
}

// OpenUserProfile shows a full profile in the secondary pane.
func (a *App) OpenUserProfile(userID string) {
	a.secondary = secondaryState{kind: secProfile, user: userID, width: a.secondary.width}
	a.secondary.view = a.mountProfile(userID)
	a.Invalidate()
}

// OpenSplit opens a conversation route ("c/<id>" or "c/<id>/t/<ts>") in the
// right-hand pane next to the current one (Slack's "Open in split view").
func (a *App) OpenSplit(route string) {
	r := Route(route)
	ch := r.Channel()
	if ch == "" {
		a.Navigate(route)
		return
	}
	if ts := r.Thread(); ts != "" {
		a.OpenThread(ch, ts)
		return
	}
	a.secondary = secondaryState{kind: secSplit, channel: ch, width: a.secondary.width}
	a.secondary.view = &splitPane{a: a, ch: ch}
	a.Invalidate()
}

type splitPane struct {
	a  *App
	ch string
}

func (p *splitPane) Layout(gtx layout.Context) layout.Dimensions { return p.a.layoutChannel(gtx, p.ch) }

// CloseSecondary closes the secondary pane.
func (a *App) CloseSecondary() {
	if a.secondary.kind == secThread {
		if ch := a.router.Current().Channel(); ch != "" {
			a.router.Replace(Route("c/" + ch))
		}
	}
	w := a.secondary.width
	a.secondary = secondaryState{width: w}
	a.Invalidate()
}

func (a *App) toggleSecondary() {
	if a.secondary.kind == secNone {
		if ch := a.router.Current().Channel(); ch != "" {
			a.OpenDetails(ch)
		}
		return
	}
	a.secondary.hidden = !a.secondary.hidden
}

func (a *App) layoutSecondary(gtx layout.Context) layout.Dimensions {
	if v := a.secondary.view; v != nil {
		return v.Layout(gtx)
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

func (a *App) layoutSecondaryResize(gtx layout.Context, x, h int) {
	s := &a.secondary
	base := float32(s.width)
	for {
		e, ok := s.drag.Update(gtx.Metric, gtx.Source, gesture.Horizontal)
		if !ok {
			break
		}
		switch e.Kind {
		case pointer.Press:
			s.dragX = e.Position.X
		case pointer.Drag:
			s.dragW = base - (e.Position.X-s.dragX)/gtx.Metric.PxPerDp
			if s.dragW < theme.SecondaryMin {
				s.dragW = theme.SecondaryMin
			}
			s.width = int(s.dragW + .5)
		case pointer.Release, pointer.Cancel:
			a.savePrefs()
		}
	}
	r := image.Rect(x-gtx.Dp(3), 0, x+gtx.Dp(3), h)
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	pointerArea(gtx, &s.drag, image.Rectangle{Max: r.Size()}, pointer.CursorColResize)
}
