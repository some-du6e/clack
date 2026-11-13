package ui

import (
	"image"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// The rail tabs' hover peeks, as Slack's web client wires them (build
// f132f0e78468): boot-render Zxh6 gives a tab button a renderWrapper,
// DmsPeekTrigger, ActivityPeekTrigger (boot-shared gOQ8),
// LaterPeekTrigger (s1p9), UnifiedFilesPeekTrigger (j33M) or
// AutomationsPeekTrigger for Agents & tools (Platform), each a
// TabPeekTrigger (boot-shared v5c+) around the PeekTrigger (20Sq) that
// More's and focus mode's peeks use too:
//
//   - TabPeekTrigger: position right-bottom, offsetX 6, offsetY -44, the
//     Popover's own allowances, no forceOpenOnClick. When the tab is the
//     active one (TjOl useActiveTabMatchesName) it renders the button
//     alone: no peek on the tab you're on.
//   - PeekTrigger: coming onto the button starts the 300 ms trigger delay
//     (peeksEnabledOnMouseEnter, off in the simplified layout); leaving
//     before it's up cancels it. The hover peek has no scrim and doesn't
//     take the keyboard focus. While it's open a document mousemove,
//     throttled to one per 30 ms (lodash throttle, leading and trailing),
//     closes it once the pointer is outside the safe area (railPeekSafe).
//     A press on the button closes it (onMouseDown: without
//     forceOpenOnClick every press does) and the click goes on to the tab.
//     Escape closes it; with the pointer still on the button it opens
//     again only after the pointer moves on it twice (ep / eh).
//   - activePeekId: one peek at a time; opening one closes the others.
//   - The button keeps its hover tile while its peek shows
//     (.p-peek_trigger--is-peek-visible).
//   - A row in the card closes it (PeekContext maybeClosePeek) before it
//     navigates.

const (
	tabPeekDelay    = 300 * time.Millisecond // PeekTrigger's default triggerDelay
	tabPeekThrottle = 30 * time.Millisecond  // its throttled document mousemove
)

// tabPeekContent is one tab's PeekView.
type tabPeekContent interface {
	// layout draws the card (its size is the card's).
	layout(gtx layout.Context) layout.Dimensions
	// opened runs when the card opens (the view mounts: its fetches).
	opened()
}

// tabPeekState is one tab's PeekTrigger (UI goroutine only).
type tabPeekState struct {
	key       string
	trig      image.Rectangle // the tab button, window coords (last frame)
	mounted   bool            // the trigger was drawn last frame (not the active tab)
	seen      bool            // and this frame, so far
	hov       bool
	enterAt   time.Time
	pop       *popups.Popover
	prev      image.Point
	hasPrev   bool
	dismissed bool // ep: closed by Escape with the pointer on the button
	rearm     bool // eh
	pressed   bool // a press on the button closed it: nothing until the pointer leaves
	lastPtr   image.Point

	checkedAt  time.Time
	pending    image.Point
	hasPending bool

	content tabPeekContent
}

// tabPeeks are the rail's tab peeks by rail key.
type tabPeeks struct {
	m map[string]*tabPeekState
}

func (a *App) tabPeek(key string) *tabPeekState {
	if a.tpeeks.m == nil {
		a.tpeeks.m = map[string]*tabPeekState{}
	}
	p := a.tpeeks.m[key]
	if p == nil {
		p = &tabPeekState{key: key}
		a.tpeeks.m[key] = p
	}
	return p
}

// tabPeekContentFor is the tab's PeekView, nil for a tab without one.
func (a *App) tabPeekContentFor(key string) tabPeekContent {
	switch key {
	case "platform":
		return a.agentsPeekView()
	case "dms", "activity", "later", "files", "home": // rail_peek_pages.go
		if a.hasTabPeek(key) {
			return a.pagePeek(key)
		}
	}
	return nil
}

// hasTabPeek reports whether the rail tab key has a hover peek.
func (a *App) hasTabPeek(key string) bool {
	switch key {
	case "platform": // AutomationsPeekTrigger: canUserViewWorkflows && the payment tier (B4i3 MA)
		return true
	case "dms", "activity", "later", "files": // DmsPeekTrigger, ActivityPeekTrigger, LaterPeekTrigger, UnifiedFilesPeekTrigger
		return true
	case "home": // Zxh6's Home renderWrapper: ActivityPeekTrigger while Activity lives in Home
		return a.activityInHome()
	}
	return false
}

// tabPeekOpen reports whether key's peek is showing.
func (a *App) tabPeekOpen(key string) bool {
	p, ok := a.tpeeks.m[key]
	return ok && p.pop != nil && !p.pop.Dismissed()
}

// anyTabPeekOpen is the open tab peek's key ("" for none).
func (a *App) anyTabPeekOpen() string {
	for k := range a.tpeeks.m {
		if a.tabPeekOpen(k) {
			return k
		}
	}
	return ""
}

// closeTabPeeks closes every tab peek but keep's (activePeekId).
func (a *App) closeTabPeeks(keep string) {
	for k, p := range a.tpeeks.m {
		if k != keep && a.tabPeekOpen(k) {
			p.pop.Dismiss()
		}
	}
}

// closeOtherPeeks closes the rail's other peeks before one opens: the tab
// peeks but keep, More's and focus mode's (one activePeekId).
func (a *App) closeOtherPeeks(keep string) {
	a.closeTabPeeks(keep)
	if keep != "browse" && a.railPeekOpen() {
		a.peek.menu.Dismiss()
	}
	if keep != "focus" && a.focusPeekOpen() {
		a.fm().pop.Dismiss()
	}
}

// tabPeekFrame runs key's PeekTrigger for this frame: trig is the button
// (window coords), hov whether the pointer is on it, active whether it's
// the active tab (no trigger then).
func (a *App) tabPeekFrame(gtx layout.Context, key string, trig image.Rectangle, hov, active bool) {
	p := a.tabPeek(key)
	p.trig = trig
	if active { // the tab's own page: TabPeekTrigger renders the button alone
		if a.tabPeekOpen(key) {
			p.pop.Dismiss()
		}
		*p = tabPeekState{key: key, content: p.content}
		return
	}
	p.seen = true
	open := a.tabPeekOpen(key)
	switch {
	case hov && !p.hov: // onMouseEnter
		if !p.dismissed && !open && a.peeksOnHover() {
			p.enterAt = gtx.Now
		}
	case !hov && p.hov: // onMouseLeave: a pending open is cancelled
		p.dismissed, p.rearm, p.pressed = false, false, false
		if !open {
			p.enterAt = time.Time{}
		}
	}
	p.hov = hov
	if open && p.hasPending { // the throttle's trailing call
		if due := p.checkedAt.Add(tabPeekThrottle); gtx.Now.Before(due) {
			gtx.Execute(op.InvalidateCmd{At: due})
		} else {
			p.hasPending = false
			p.checkedAt = gtx.Now
			a.tabPeekCheck(p, p.pending)
		}
	}
	if open || p.enterAt.IsZero() || p.pressed {
		return
	}
	if due := p.enterAt.Add(tabPeekDelay); gtx.Now.Before(due) {
		gtx.Execute(op.InvalidateCmd{At: due})
		return
	}
	p.enterAt = time.Time{}
	a.openTabPeek(key)
}

// openTabPeek opens key's PeekView by its button as a hover peek.
func (a *App) openTabPeek(key string) {
	p := a.tabPeek(key)
	c := a.tabPeekContentFor(key)
	if c == nil {
		return
	}
	a.closeOtherPeeks(key)
	p.content = c
	var pop *popups.Popover
	pop = popups.NewPopover(a, c.layout, popups.PopoverOpts{
		Anchor: p.trig, Popover: "right-bottom", OffsetX: 6, OffsetY: -44, Radius: 12,
		Peek: true, AnimateIn: true, NoMotion: a.reduceMotion(), // PeekView: p-peek_card--animate-in
		OnClose: func() {
			if p.pop != pop {
				return
			}
			p.hasPrev, p.hasPending = false, false
			if p.hov && !p.pressed { // eI: Escape with the pointer still on the button
				p.dismissed = true
			}
			a.Invalidate()
		},
	})
	p.pop = pop
	p.hasPrev, p.hasPending, p.checkedAt = false, false, time.Time{}
	c.opened()
	a.ShowOverlay(pop)
}

// closeTabPeek is a row's maybeClosePeek.
func (a *App) closeTabPeek(key string) {
	if a.tabPeekOpen(key) {
		a.tpeeks.m[key].pop.Dismiss()
		a.Invalidate()
	}
}

// tabPeekPointer is the open tab peek's document mousemove and its
// button's onMouseMove / onMouseDown (pe in window coordinates).
func (a *App) tabPeekPointer(pe pointer.Event) {
	pt := image.Pt(int(pe.Position.X+.5), int(pe.Position.Y+.5))
	for _, p := range a.tpeeks.m {
		if !p.mounted {
			continue
		}
		open := a.tabPeekOpen(p.key)
		if pe.Kind == pointer.Press && pt.In(p.trig) {
			// onMouseDown: without forceOpenOnClick any press closes it
			// (and cancels a pending open); the click goes to the tab
			p.enterAt, p.pressed = time.Time{}, true
			if open {
				p.pop.Dismiss()
				a.Invalidate()
			}
			continue
		}
		if pe.Kind != pointer.Move && pe.Kind != pointer.Drag {
			continue
		}
		moved := pt != p.lastPtr
		p.lastPtr = pt
		if moved && pt.In(p.trig) && !open && !p.pressed { // eF after an Escape
			switch {
			case p.dismissed:
				p.dismissed, p.rearm = false, true
			case p.rearm:
				p.rearm = false
				if a.peeksOnHover() {
					p.enterAt = time.Now()
					a.Invalidate()
				}
			}
		}
		if !open {
			continue
		}
		now := time.Now()
		if !p.checkedAt.IsZero() && now.Sub(p.checkedAt) < tabPeekThrottle {
			p.pending, p.hasPending = pt, true
			a.Invalidate() // tabPeekFrame waits for the window's end
			continue
		}
		p.checkedAt, p.hasPending = now, false
		a.tabPeekCheck(p, pt)
	}
}

// tabPeekCheck is the throttled handler: close the hover peek when pt is
// outside the safe area (the card itself counts: isMouseInsidePeek),
// and remember pt as the previous position.
func (a *App) tabPeekCheck(p *tabPeekState, pt image.Point) {
	if !a.tabPeekOpen(p.key) {
		return
	}
	if !(p.pop.InsideCard(pt) || railPeekSafe(pt, p.trig, p.pop.Rect(), p.prev, p.hasPrev)) {
		p.pop.Dismiss()
		a.Invalidate()
		return
	}
	p.prev, p.hasPrev = pt, true
}

// tabPeekEndFrame runs after the rail: a trigger no tab drew this frame
// (the tab left the rail, or became the active one) unmounts and its peek
// closes.
func (a *App) tabPeekEndFrame() {
	for _, p := range a.tpeeks.m {
		p.mounted, p.seen = p.seen, false
		if !p.mounted && a.tabPeekOpen(p.key) {
			p.pop.Dismiss()
		}
	}
}
