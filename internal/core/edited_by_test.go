package core

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// EditedLabel's "{dateTime} by {name}" needs edited.user apart from the
// author (getMessageLabelValues, XSZB: a workflow message's editor).
func TestMessageKeepsEditor(t *testing.T) {
	a := New(slack.New(slack.Creds{Cookie: "xoxd-test", UserID: "USELF"}), store.New(""))
	m := a.message("C1", &slack.Message{TS: "1700000000.000100", User: "U1", Text: "x", WorkflowID: "Wf1",
		Edited: &slack.Edited{User: "U2", TS: "1700000001.000000"}})
	if !m.Edited || m.EditedTS != "1700000001.000000" || m.EditedBy != "U2" || m.WorkflowID != "Wf1" {
		t.Errorf("edited %v at %q by %q (workflow %q), want by U2", m.Edited, m.EditedTS, m.EditedBy, m.WorkflowID)
	}
	if m := a.message("C1", &slack.Message{TS: "1700000000.000200", User: "U1", Text: "y"}); m.Edited || m.EditedBy != "" {
		t.Errorf("an unedited message: edited %v by %q", m.Edited, m.EditedBy)
	}
}
