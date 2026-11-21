package views

// Threads, Activity and Later draw MessageKit bodies, labels included
// (insertMessageLabelIntoBlocks): an agent reply whose stream was stopped
// shows StoppedLabel's "(stopped)" there as in the channel,
// "(stopped by {name})" for someone else's stop under
// agents_streaming_stop_button_fe, and an edited message "(edited)" alone
// (the open MINOR: the views drew no stopped label). Synthetic only.

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestViewsStoppedLabel(t *testing.T) {
	app, _ := vaFake(t)
	h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	b := newBase(h)
	raw, _ := json.Marshal(fakeslack.AgentStoppedReply("1763500000.000100"))
	var sm slack.Message
	if err := json.Unmarshal(raw, &sm); err != nil {
		t.Fatal(err)
	}
	m := app.MessageFromSlack(fakeslack.AgentChannelID, &sm)
	if !m.Stopped {
		t.Fatal("the stopped fixture reads as not stopped")
	}
	label := func() richview.Options {
		var o richview.Options
		b.messageLabel(&m, &o)
		return o
	}
	if o := label(); !o.Stopped || o.Edited || o.StoppedBy != "" {
		t.Errorf("stopped by the viewer: stopped %v edited %v by %q, want (stopped)", o.Stopped, o.Edited, o.StoppedBy)
	}
	app.SetExperimentGroup("agents_streaming_stop_button_fe", "on")
	t.Cleanup(func() { app.SetExperimentGroup("agents_streaming_stop_button_fe", "") })
	app.S.PutUsers([]slack.User{{ID: "U0STOPPER1", Name: "stopper", Profile: slack.Profile{DisplayName: "stopper"}}})
	m.StoppedBy = "U0STOPPER1"
	if o := label(); !o.Stopped || o.StoppedBy != "stopper" {
		t.Errorf("stopped by another member, experiment on: stopped %v by %q, want (stopped by stopper)", o.Stopped, o.StoppedBy)
	}
	m.Edited, m.BotID = true, "" // a person's edited message: (edited) and nothing else
	if o := label(); !o.Edited || o.Stopped {
		t.Errorf("an edited message: edited %v stopped %v, want (edited) alone", o.Edited, o.Stopped)
	}
}
