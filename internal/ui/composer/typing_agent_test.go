package composer

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

func statusEvent(status, statusType string) core.Event {
	raw, _ := json.Marshal(map[string]any{"channel_id": "C1", "thread_ts": "1.0", "bot_user_id": "UBOT", "status": status, "status_type": statusType})
	return core.Event{Type: "assistant_status", Channel: "C1", ThreadTS: "1.0", UserID: "UBOT", Name: "kyto", Raw: raw}
}

// An app's assistant.threads.setStatus is the thread's typing line,
// "<app> <status>" (Slack's AiAppStatusIndicator), until the app posts,
// clears it, or someone else types too.
func TestAssistantStatusLine(t *testing.T) {
	s := &typingState{m: map[string]map[string]typer{}}
	s.apply(statusEvent("is thinking…", "typing"))
	if name, st := s.assistantLine("C1", "1.0", "USELF"); name != "kyto" || st != "is thinking…" {
		t.Fatalf("line %q %q", name, st)
	}
	if _, st := s.assistantLine("C1", "", "USELF"); st != "" {
		t.Error("status leaked into the channel")
	}
	// someone else typing: the normal typing line
	s.apply(core.Event{Type: "typing", Channel: "C1", ThreadTS: "1.0", UserID: "U2", Name: "Ada"})
	if _, st := s.assistantLine("C1", "1.0", "USELF"); st != "" {
		t.Error("status shown with two typers")
	}
	if got := typingLine(s.names("C1", "1.0", "USELF")); got != "Ada and kyto are typing…" {
		t.Errorf("two typers: %q", got)
	}
	s.apply(core.Event{Type: "message", Channel: "C1", ThreadTS: "1.0", UserID: "U2"})
	// the app's reply clears its status
	s.apply(core.Event{Type: "message", Channel: "C1", ThreadTS: "1.0", UserID: "UBOT"})
	if _, st := s.assistantLine("C1", "1.0", "USELF"); st != "" {
		t.Error("status kept after the app's message")
	}
	// an empty status clears it; a banner status isn't a typing line
	s.apply(statusEvent("is thinking…", "typing"))
	s.apply(statusEvent("", "typing"))
	if _, st := s.assistantLine("C1", "1.0", "USELF"); st != "" {
		t.Error("empty status didn't clear")
	}
	s.apply(statusEvent("Searching…", "banner"))
	if _, st := s.assistantLine("C1", "1.0", "USELF"); st != "" {
		t.Error("banner status drawn as the typing line")
	}
}
