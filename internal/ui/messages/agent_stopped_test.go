package messages

// A stopped agent reply's row asks richview for StoppedLabel (J825):
// "(stopped)", with the stopper's name only under
// agents_streaming_stop_button_fe and when it wasn't the viewer, and none
// on a message that shows "(edited)" (insertMessageLabelIntoBlocks).

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestAgentStoppedRow(t *testing.T) {
	app, _, ts := shotsApp(t)
	host := coreHost{app: app, th: theme.New(false)}
	mk := func(by string, edited bool) *row {
		m := core.Message{TS: ts["b1"], Channel: tShots, User: "UANA00001", Edited: edited}
		if edited {
			m.EditedTS = ts["b2"]
		}
		e := entry{rev: 1, m: withStopped(m, by)}
		r := newRow(&e)
		r.rc = &rowCtx{h: host, selfID: "USELF"}
		return r
	}
	if s, by := rowStopped(mk("UBEN00001", false)); !s || by != "" {
		t.Errorf("stopped by ben, experiment off: label %v by %q, want (stopped)", s, by)
	}
	app.SetExperimentGroup("agents_streaming_stop_button_fe", "on")
	if s, by := rowStopped(mk("UBEN00001", false)); !s || by != "ben" {
		t.Errorf("stopped by ben, experiment on: label %v by %q, want (stopped by ben)", s, by)
	}
	if s, by := rowStopped(mk("USELF", false)); !s || by != "" {
		t.Errorf("stopped by the viewer: label %v by %q, want (stopped)", s, by)
	}
	if s, _ := rowStopped(mk("UBEN00001", true)); s {
		t.Errorf("an edited message: (stopped) drawn too")
	}
	app.SetExperimentGroup("agents_streaming_stop_button_fe", "")
}
