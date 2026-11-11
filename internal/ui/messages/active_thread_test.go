package messages

import (
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme"
)

type activeThreadHost struct {
	coreHost
	ch, ts string
}

func (h activeThreadHost) ActiveThread() (string, string) { return h.ch, h.ts }

// thread_root_selected_state: the root of the thread open beside its
// channel is MessageWrapper's p-message_pane_message__active_thread
// (base-hgl-3): only under the experiment, only in the channel's pane.
func TestActiveThreadRoot(t *testing.T) {
	app, _ := newTestApp(t)
	h := activeThreadHost{coreHost: coreHost{app: app, th: theme.Bare(false)}, ch: tGen, ts: tsFair}
	app.SetExperimentGroup("thread_root_selected_state", "on")
	if got := activeThreadIn(h, tGen, false); got != tsFair {
		t.Errorf("the open thread's root: %q, want %q", got, tsFair)
	}
	if got := activeThreadIn(h, tGen, true); got != "" {
		t.Errorf("a thread pane marked %q", got)
	}
	if got := activeThreadIn(h, tEmpty, false); got != "" {
		t.Errorf("another channel marked %q", got)
	}
	if got := activeThreadIn(coreHost{app: app}, tGen, false); got != "" {
		t.Errorf("a host without a side pane marked %q", got)
	}
	app.SetExperimentGroup("thread_root_selected_state", "off")
	if got := activeThreadIn(h, tGen, false); got != "" {
		t.Errorf("off: marked %q", got)
	}
	app.SetExperimentGroup("thread_root_selected_state", "") // not assigned: Slack's null
	if got := activeThreadIn(h, tGen, false); got != "" {
		t.Errorf("unassigned: marked %q", got)
	}
	if lightColors.ActiveThread != rgb(0xfffae0) || darkColors.ActiveThread != rgb(0x2f1e00) {
		t.Errorf("base-hgl-3: %v / %v", lightColors.ActiveThread, darkColors.ActiveThread)
	}
}
