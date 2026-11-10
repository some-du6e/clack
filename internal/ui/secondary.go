package ui

import (
	"image"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

type secKind uint8

const (
	secNone secKind = iota
	secThread
	secDetails
	secProfile
	secSplit
	secHelp   // the Help flexpane (update_ui.go)
	secSearch // search in channel (⌘F, popups.SearchInChannel)
	secCode   // a code channel's details pane (codechannel_pane.go)
)

// secondaryState is the resizable right-hand pane (thread, channel details,
// profile).
type secondaryState struct {
	kind    secKind
	channel string
	ts      string
	user    string
	view    View
	width   int  // dp; 0 is Slack's auto width (secondaryWidth)
	hidden  bool // Ctrl+. toggles without forgetting what was open
	// expanded is the code channel pane's "Expand view": Slack switches the
	// window to single-column mode, the pane taking the whole panel.
	expanded bool
	// keyFocus: the key focus is in the thread pane (set each frame
	// before the shortcuts run; threadEsc)
	keyFocus bool
	// drawnW is the pane's width last frame, px (screenshots crop it)
	drawnW int
	drag   gesture.Drag
	dragX  float32
	// lastPress is the resizer's last press (a second within 500 ms
	// resets the width); reset: this press did, the drag is ignored
	lastPress time.Time
	reset     bool
}

func (s *secondaryState) open() bool { return s.kind != secNone && !s.hidden }

// OpenThread shows a thread in the secondary pane.
func (a *App) OpenThread(channel, ts string) {
	if a.secondary.kind == secThread && a.secondary.channel == channel && a.secondary.ts == ts && !a.secondary.hidden {
		return
	}
	a.secondary = secondaryState{kind: secThread, channel: channel, ts: ts, width: a.secondary.width}
	a.secondary.view = a.mountThread(channel, ts)
	if c := a.convos[channel+"/"+ts]; c != nil { // a cached thread: Slack mounts a new pane
		if r, ok := c.list.(interface{ Reopen() }); ok {
			r.Reopen()
		}
	}
	if a.router.Current().Channel() == channel {
		a.router.Replace(Route("c/" + channel + "/t/" + ts))
	}
	a.Invalidate()
}

// FocusThreadComposer focuses a thread's reply composer
// (messages.ThreadComposerFocuser): the reply bar opens the thread with
// its input focused, as Slack's openThread sets focusInput.
func (a *App) FocusThreadComposer(channel, ts string) {
	if c := a.convoFor(channel, ts); c != nil && c.comp != nil {
		c.comp.Focus()
	}
}

// openDetails, when set by a feature package, shows channel details its own
// way (ui-views draws a modal dialog) and reports true.
var openDetails func(a *App, ch string) bool

// openDetailsTab is openDetails opening on a tab ("Settings").
var openDetailsTab func(a *App, ch, tab string) bool

// OpenDetailsTab shows channel details on the tab labelled tab (Slack's
// channel menu "Edit settings" opens the Settings tab).
func (a *App) OpenDetailsTab(channel, tab string) {
	if openDetailsTab != nil && openDetailsTab(a, channel, tab) {
		return
	}
	a.OpenDetails(channel)
}

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

// mkSearchInChannel builds the ⌘F pane (mount_features.go).
var mkSearchInChannel func(a *App, ch string) View

// OpenSearchInChannel shows Slack's search in channel pane for ch.
func (a *App) OpenSearchInChannel(ch string) {
	if mkSearchInChannel == nil || ch == "" {
		return
	}
	if a.secondary.kind == secSearch && a.secondary.channel == ch && !a.secondary.hidden {
		a.secondary.view = mkSearchInChannel(a, ch) // ⌘F again: a fresh box, focused
		a.Invalidate()
		return
	}
	a.secondary = secondaryState{kind: secSearch, channel: ch, width: a.secondary.width}
	a.secondary.view = mkSearchInChannel(a, ch)
	a.Invalidate()
}

// searchInChannel opens the search in channel pane, or (without the
// popups feature) the search box scoped to the channel.
func (a *App) searchInChannel(ch string) {
	if mkSearchInChannel != nil {
		a.OpenSearchInChannel(ch)
		return
	}
	a.openSearch("in:<#" + ch + ">")
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

// threadEsc is Escape in the thread pane: Slack's secondary view is a
// dialog whose keydown hands Escape to the registered handler first
// (ThreadsFlexpane onEsc marks the thread read when there is something
// to mark) and otherwise closes the view. Reports whether it took the
// key (only with the key focus in the pane).
func (a *App) threadEsc() bool {
	s := &a.secondary
	if s.kind != secThread || !s.open() || !s.keyFocus {
		return false
	}
	if c := a.convos[s.channel+"/"+s.ts]; c != nil {
		if e, ok := c.list.(interface{ Esc() bool }); ok && e.Esc() {
			return true
		}
	}
	a.CloseSecondary()
	return true
}

// threadPaneFocused reports whether the key focus is in the thread pane's
// reply composer with no popup of its own up. An edit composer, the
// message list's keyboard focus and the composer's autocomplete keep
// their own Escape (cancel the edit, back to the composer, close the
// popup), as Slack's input handles them before the view's keydown.
func (a *App) threadPaneFocused(gtx layout.Context) bool {
	s := &a.secondary
	if s.kind != secThread || !s.open() {
		return false
	}
	c := a.convos[s.channel+"/"+s.ts]
	if c == nil || c.comp == nil || c.edit != nil {
		return false
	}
	ft, ok := c.comp.(interface{ FocusTag() any })
	if !ok || !gtx.Focused(ft.FocusTag()) {
		return false
	}
	p, ok := c.comp.(interface{ PopupOpen() bool })
	return !ok || !p.PopupOpen()
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

// secondaryWidth is the pane's width in px for a primary panel panel px
// wide: Slack's secondary column (client-boot-render aTtQ, with the
// nested secondary view and secondary_view_default_width on, as the
// account has them) is 40% of the region by default ("auto", width 0),
// the dragged width once resized ("manual"), never under 300 and leaving
// the primary view its 300 (secondary maxWidth: region - 300).
func (a *App) secondaryWidth(gtx layout.Context, panel int) int {
	w := a.secondaryDefault(gtx, panel)
	if a.secondary.width > 0 {
		w = gtx.Dp(unit.Dp(a.secondary.width))
	}
	if hi := panel - gtx.Dp(300); w > hi {
		w = hi
	}
	return max(w, gtx.Dp(theme.SecondaryMin))
}

// secondaryDefault is getIdealSecondaryWidth: (region - sidebar) * .4
// under secondary_view_default_width (on for the account), the region
// minus the sidebar being the primary panel the pane shares; without it
// .3 of the region, the sidebar included.
func (a *App) secondaryDefault(gtx layout.Context, panel int) int {
	if c := a.Core(); c != nil && c.Experiment("secondary_view_default_width") != "on" {
		sb := 0
		if !a.sidebar.hidden {
			sb = gtx.Dp(unit.Dp(a.sidebar.width))
		}
		return int(float32(panel+sb)*.3 + .5)
	}
	return int(float32(panel)*.4 + .5)
}

// layoutSecondaryResize is the pane's BaseResizer (client-boot-shared
// 7M48): dragging its left edge sets a manual width within 300 and the
// region minus the primary's 300, snapping to the default width within
// 15px of it (mode "auto"); two presses within 500 ms reset it to the
// default. x is the pane's left edge in the panel, h its height.
func (a *App) layoutSecondaryResize(gtx layout.Context, x, h int) {
	s := &a.secondary
	panel := gtx.Constraints.Max.X
	px := gtx.Metric.PxPerDp
	cur := float32(panel-x) / px // the width drawn, dp
	def := float32(a.secondaryDefault(gtx, panel)) / px
	hi := float32(panel)/px - 300
	for {
		e, ok := s.drag.Update(gtx.Metric, gtx.Source, gesture.Horizontal)
		if !ok {
			break
		}
		switch e.Kind {
		case pointer.Press:
			if !s.lastPress.IsZero() && gtx.Now.Sub(s.lastPress) < 500*time.Millisecond {
				s.width, s.lastPress, s.reset = 0, time.Time{}, true // onReset: the default, "auto"
				a.savePrefs()
				continue
			}
			s.lastPress, s.reset = gtx.Now, false
			s.dragX = e.Position.X
		case pointer.Drag:
			if s.reset {
				continue
			}
			w := cur - (e.Position.X-s.dragX)/px
			switch {
			case w >= def-15 && w <= def+15: // calculateColumnWidth: snap to the default
				s.width = 0
			default:
				s.width = int(min(max(w, theme.SecondaryMin), max(hi, theme.SecondaryMin)) + .5)
			}
		case pointer.Release, pointer.Cancel:
			a.savePrefs()
		}
	}
	r := image.Rect(x-gtx.Dp(3), 0, x+gtx.Dp(3), h)
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	pointerArea(gtx, &s.drag, image.Rectangle{Max: r.Size()}, pointer.CursorColResize)
}
