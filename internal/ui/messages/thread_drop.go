package messages

// DropTop is where the thread's body starts under the pane's header
// (last frame): Slack's threads FileDragDrop (location THREAD) wraps the
// flexpane body, not its header, so the drop overlay and drop zone start
// here (the ui root's drop_overlay.go).
func (v *ThreadView) DropTop() int { return v.top }
