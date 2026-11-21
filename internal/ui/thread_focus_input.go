package ui

// A thread's pending reply-box focus: Slack's
// threadMetadata.focus[windowId].focusInput (JhVT, build 35b42de8b870).
//
// Slack focuses a thread's reply box at one moment only: when the thread
// pane's input row mounts (ThreadsFlexpane.renderRow → ThreadFooter →
// InputContainer.componentDidMount → TextyInput.focus) while focusInput
// equals that thread's channel-threadTs. setFocusInput writes it: the
// comment thunk 0yz+ (the context menu's Reply in thread, by click or T,
// and the hover toolbar's Reply), the reply bar's openThread (r0Yq) and
// showThreadOrRefresh without requestFocus (mDw2). Activity, notification
// clicks, permalinks and ArrowRight never set it, so a thread they open
// draws its box unfocused.
//
// The key outlives the focus it gives (Slack keeps it), and goes when:
//   - a thread pane unmounts showing that thread (cancelFocusInput in
//     ThreadsFlexpane.componentWillUnmount): threadFocusUnmount, for the
//     side pane and the split view's thread alike (client-thread-view
//     renders the same ThreadsFlexpane in the split container, with the
//     window's windowId);
//   - thread search opens (openThreadSearch): ClearThreadFocusInput.
//
// Clack has one main window, so one key; the split view's thread reads the
// same one, as a Slack tile shares its window's.

// threadFocusState is the App's focusInput and the threads its two
// ThreadsFlexpanes showed last frame (their unmounts).
type threadFocusState struct {
	key   string // ch+"/"+ts, as a.convos; "" none
	pane  string // the side pane's thread last frame ("" none shown)
	split string // the split view's thread last frame ("" none shown)
}

// SetThreadFocusInput is setFocusInput (JhVT.js:229-244): the thread's box
// is to take the focus when it next mounts. It only stores the key: the
// convo's layout gives the focus (layoutConvo, inputShown).
func (a *App) SetThreadFocusInput(channel, ts string) {
	if channel == "" || ts == "" {
		return
	}
	a.threadFocus.key = channel + "/" + ts
}

// cancelThreadFocusInput is cancelFocusInput (JhVT.js:245-261): the key goes
// only if it is this thread's.
func (a *App) cancelThreadFocusInput(k string) {
	if k != "" && a.threadFocus.key == k {
		a.threadFocus.key = ""
	}
}

// ClearThreadFocusInput is the reset in openThreadSearch (JhVT.js:295-322):
// whatever thread the key named.
func (a *App) ClearThreadFocusInput() { a.threadFocus.key = "" }

// threadFocusWanted reports whether c is the thread focusInput names.
func (a *App) threadFocusWanted(c *convo) bool {
	return c.ts != "" && a.threadFocus.key != "" && a.threadFocus.key == c.ch+"/"+c.ts
}

// threadFocusUnmount runs once a frame: when the side pane stops showing a
// thread (closed, hidden, or another kind of pane), or the split view does
// (closed, back to a channel), that ThreadsFlexpane unmounts and cancels
// the key for the thread it showed. A switch from one thread to another
// remounts only the list, so it cancels nothing.
func (a *App) threadFocusUnmount() {
	side := ""
	if a.secondary.kind == secThread && a.secondary.open() {
		side = a.secondary.channel + "/" + a.secondary.ts
	}
	split := ""
	if r := a.splitTop(); a.splitOpen() && r.Thread() != "" {
		split = r.Channel() + "/" + r.Thread()
	}
	f := &a.threadFocus
	a.threadPaneLeft(f.pane, side, split) // its history (thread_close.go)
	a.threadPaneLeft(f.split, split, side)
	if f.pane != "" && side == "" {
		a.cancelThreadFocusInput(f.pane)
	}
	if f.split != "" && split == "" {
		a.cancelThreadFocusInput(f.split)
	}
	f.pane, f.split = side, split
}
