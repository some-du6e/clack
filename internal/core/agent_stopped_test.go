package core

// getMessageLabelValues (vshb): an agent reply is stopped when its
// ai_context.result_status is "stopped_by_user", stopped by
// ai_context.stopped_by_user_id; any other status, or none, isn't.

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func TestAgentStoppedLabelValues(t *testing.T) {
	a := New(slack.New(slack.Creds{Cookie: "xoxd-test", UserID: fakeslack.SelfID}), store.New(""))
	conv := func(raw map[string]any) Message {
		b, _ := json.Marshal(raw)
		var sm slack.Message
		if err := json.Unmarshal(b, &sm); err != nil {
			t.Fatal(err)
		}
		return a.message(fakeslack.AgentChannelID, &sm)
	}
	stopped := fakeslack.AgentStoppedReply("1762520200.000100")
	if s, by := stoppedOf(conv(stopped)); !s || by != fakeslack.SelfID {
		t.Errorf("stopped reply: stopped %v by %q, want true by %s", s, by, fakeslack.SelfID)
	}
	stopped["ai_context"] = map[string]any{"result_status": "completed"}
	if s, _ := stoppedOf(conv(stopped)); s {
		t.Errorf("result_status completed: stopped")
	}
	delete(stopped, "ai_context")
	if s, _ := stoppedOf(conv(stopped)); s {
		t.Errorf("no ai_context: stopped")
	}
}
