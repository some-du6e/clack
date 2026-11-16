package ui

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
// view that changed takes it itself (focusAfterNavigate). Here the
// composers of a conversation the screen stopped showing since the last
// frame are released (composer.Release) at the end of the frame: the
// primary view's channel when the route left it, the pane's thread when
// the pane closed or shows something else. The check reads the state, not
// what was drawn, so a glitch frame doesn't trip it, and Release keeps the
// focus of an editor that was drawn that frame anyway (the same
// conversation in another pane).

// shownConvos are the convos (a.convos keys) the last frame's state
// showed: the primary view's channel and the thread pane's thread.
type shownConvos struct {
	channel, thread string
}

// releaseLeftComposers runs once per frame, before composer.KeepFocus.
func (a *App) releaseLeftComposers() {
	now := shownConvos{}
	if ch := a.router.Current().Channel(); ch != "" {
		now.channel = ch + "/"
	}
	if s := &a.secondary; s.kind == secThread && s.open() {
		now.thread = s.channel + "/" + s.ts
	}
	prev := a.shownConvos
	a.shownConvos = now
	if prev.channel != "" && prev.channel != now.channel {
		a.releaseConvo(a.convos[prev.channel])
	}
	if prev.thread != "" && prev.thread != now.thread {
		a.releaseConvo(a.convos[prev.thread])
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
