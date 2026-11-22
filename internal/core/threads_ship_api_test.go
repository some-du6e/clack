package core

// The threads-ship tests' calls into what the fix added
// (threads_ship_test.go calls only these, so an evidence run on the code
// before can swap them).

// noteThreadRows is the thread list telling core which replies its pane
// has on screen.
func noteThreadRows(a *App, ch, ts, first, last string) { a.NoteThreadView(ch, ts, first, last) }
