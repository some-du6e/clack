package messages

// thread_root_selected_state (client-boot O2By Gl, build 4d0c78085232):
// MessageWrapper (boot-shared EXGU) gives the message whose thread the
// side pane shows (activeThreadKeyFromSecondaryView) the class
// p-message_pane_message__active_thread, background
// var(--dt_color-base-hgl-3), in the channel's message pane only.

// ActiveThreadHost is optional: the thread open in the side pane, if any.
type ActiveThreadHost interface {
	ActiveThread() (channel, ts string)
}

// activeThreadIn is the ts of the root in ch whose thread is open beside
// it, "" for none or without the experiment (or in a thread pane).
func activeThreadIn(h Host, ch string, thread bool) string {
	if thread || ch == "" {
		return ""
	}
	at, ok := h.(ActiveThreadHost)
	if !ok {
		return ""
	}
	c, ts := at.ActiveThread()
	if c != ch || ts == "" {
		return ""
	}
	if a := h.Core(); a == nil || a.Experiment("thread_root_selected_state") != "on" {
		return ""
	}
	return ts
}
