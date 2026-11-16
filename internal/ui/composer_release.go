package ui

import "gioui.org/layout"

// Composers taken off the screen let go of the focus.
//
// composer.KeepFocus holds a focused editor that missed a frame (a
// plugin's tree skipping the conversation once). It can't tell that from
// a composer unmounted on purpose, so after a switch to Activity, another
// channel, a permalink, a closed thread pane or another thread in it, the
// hidden box kept the focus for KeepFocus's whole window, took what was
// typed, replayed it into whichever box drew that editor next, and then
// left nothing focused (Karim's log, 2026-11-15: 3078 held frames across
// 132 lines, 40 after "switch to activity", 93 on thread routes).
//
// Slack's components unmount with their view, and its
// TRANSITION_FOCUS_NAVIGATE_LISTENER (client-boot 1zp8, build
// 2c0d02ca130f) moves the focus to the view that changed: when a
// container's view closed (onViewClosed) to the first of Secondary,
// Primary, Sidebar that still has a view, so closing the thread pane
// focuses the channel's message input (CloseSecondary here); a primary
// view that changed takes it itself (focusAfterNavigate).
//
// Here, at the end of every frame (releaseHidden):
//
//   - the composers of a conversation the state stopped showing since the
//     last frame are released (composer.Release): the primary view's
//     channel when the route left it; the secondary column's thread or
//     conversation when it closed, hid or shows something else; the split
//     view's when it moved on or closed. That also drops a focus request
//     made just before (CloseSecondary, then a navigation away).
//   - a focused composer this frame didn't draw is released too, wherever
//     it lives (the DMs tab's or Activity's conversation, any pane), unless
//     the state still shows its conversation: only there is a missing
//     draw a glitch frame, for KeepFocus to bridge.
//
// The state, not what was drawn, decides what a glitch is, so a frame a
// plugin's tree skipped doesn't release anything it still shows, and
// Release keeps the focus of an editor drawn that frame anyway. Editors
// outside a conversation (an inline message edit, Block Kit inputs) are
// KeepFocus's alone: a hold that ends without them drops what was typed
// meanwhile.

// shownConvos are the conversations the state shows, this frame's and the
// last one's: keys of a.convos (main) and of the split view's (split).
// The two frames' maps take turns (no new maps per frame).
type shownConvos struct {
	main, split         map[string]bool
	prevMain, prevSplit map[string]bool
}

// next makes this frame's sets the last frame's and empties the other
// pair for this frame.
func (s *shownConvos) next() {
	s.main, s.prevMain = s.prevMain, s.main
	s.split, s.prevSplit = s.prevSplit, s.split
	if s.main == nil {
		s.main, s.split = map[string]bool{}, map[string]bool{}
	}
	clear(s.main)
	clear(s.split)
}

// fillShown reads them from the route, the secondary column and the split
// view.
func (a *App) fillShown(s *shownConvos) {
	if ch := a.router.Current().Channel(); ch != "" {
		s.main[ch+"/"] = true
	}
	if sec := &a.secondary; sec.open() {
		switch sec.kind {
		case secThread:
			s.main[sec.channel+"/"+sec.ts] = true
		case secChannel:
			s.main[sec.channel+"/"] = true
		}
	}
	if a.splitOpen() {
		if r := a.splitTop(); r.Channel() != "" {
			s.split[r.Channel()+"/"+r.Thread()] = true
		}
	}
}

// releaseHidden runs once per frame, after the layout and before
// composer.KeepFocus.
func (a *App) releaseHidden(gtx layout.Context) {
	s := &a.shownConvos
	s.next()
	a.fillShown(s)
	for k := range s.prevMain {
		if !s.main[k] {
			a.releaseConvo(a.convos[k])
		}
	}
	for k := range s.prevSplit {
		if !s.split[k] {
			a.releaseConvo(a.split.pane.convos[k])
		}
	}
	a.releaseUndrawn(gtx, a.convos, s.main)
	a.releaseUndrawn(gtx, a.split.pane.convos, s.split)
}

// focusReleaser is what releaseHidden needs of a composer
// (composer.Composer, through composerBox).
type focusReleaser interface {
	FocusTag() any
	Release()
	DrawnLive(gtx layout.Context) bool
}

// releaseUndrawn releases a focused composer this frame didn't draw in a
// conversation the state doesn't show.
func (a *App) releaseUndrawn(gtx layout.Context, convos map[string]*convo, shown map[string]bool) {
	for k, c := range convos {
		if shown[k] {
			continue
		}
		for _, cv := range []composerView{c.comp, c.edit} {
			if r, ok := cv.(focusReleaser); ok && gtx.Focused(r.FocusTag()) && !r.DrawnLive(gtx) {
				r.Release()
			}
		}
	}
}

// releaseConvo releases c's composer and its edit composer.
func (a *App) releaseConvo(c *convo) {
	if c == nil {
		return
	}
	for _, cv := range []composerView{c.comp, c.edit} {
		if r, ok := cv.(interface{ Release() }); ok {
			r.Release()
		}
	}
}
