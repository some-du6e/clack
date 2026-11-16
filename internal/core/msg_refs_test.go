package core

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// A conversion records the members and conversations its Doc read (a
// file's owner, a tab slug's channel), not the sender (the row reads it as
// it draws), and RefsChanged names what an event changed of them.
func TestMessageRefs(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "U0CARA001", Name: "cara", Profile: slack.Profile{DisplayName: "cara"}}})
	props, _ := json.Marshal(map[string]any{"tabz": []any{map[string]any{"id": "Ct0OPS001", "type": "canvas", "label": "Runbook"}}})
	a.S.PutConv(slack.Conversation{ID: "C0OPS0001", Name: "ops", IsChannel: true, IsMember: true, Properties: props})
	blocks, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "r", "elements": []any{map[string]any{"type": "rich_text_section",
		"elements": []any{map[string]any{"type": "text", "text": "see "}, map[string]any{"type": "channel", "channel_id": "C0OPS0001", "tab_id": "Ct0OPS001"}}}}}})
	m := slack.Message{Type: "message", TS: "1700000000.000100", User: "U0ANA0001", Text: "see <#C0OPS0001>", Blocks: blocks,
		Files: []slack.File{{ID: "F0PLAN001", Name: "plan.pdf", Title: "Plan", Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF", Mode: "hosted", User: "U0CARA001"}}}
	v := a.MessageFromSlack("C0GEN0001", &m)
	if !slices.Contains(v.Refs.Members, "U0CARA001") || slices.Contains(v.Refs.Members, "U0ANA0001") {
		t.Errorf("members read %v; want the file's owner and not the sender", v.Refs.Members)
	}
	if !slices.Contains(v.Refs.Channels, "C0OPS0001") {
		t.Errorf("conversations read %v; want the tab slug's", v.Refs.Channels)
	}
	if !v.Refs.Reads([]string{"U0CARA001"}, nil) || !v.Refs.Reads(nil, []string{"C0OPS0001"}) || v.Refs.Reads([]string{"U0BEN0001"}, []string{"C0GEN0001"}) {
		t.Errorf("Reads disagrees with %+v", v.Refs)
	}

	for _, c := range []struct {
		ev       Event
		members  []string
		channels []string
	}{
		{Event{Type: "user", UserID: "U0CARA001"}, []string{"U0CARA001"}, nil},
		{Event{Type: "user", User: &User{ID: "U0CARA001"}}, []string{"U0CARA001"}, nil},
		{Event{Type: "conv", Channel: "C0OPS0001"}, nil, []string{"C0OPS0001"}},
		{Event{Type: "conv", Conv: &Conv{ID: "C0OPS0001"}}, nil, []string{"C0OPS0001"}},
		{Event{Type: "tabs", Channel: "C0OPS0001"}, nil, []string{"C0OPS0001"}},
		{Event{Type: "names", Users: []string{"U0CARA001", "C0OPS0001"}}, []string{"U0CARA001", "C0OPS0001"}, []string{"U0CARA001", "C0OPS0001"}},
		{Event{Type: "message", Channel: "C0OPS0001"}, nil, nil},
		{Event{Type: "tabs"}, nil, nil},
	} {
		ms, cs := RefsChanged(c.ev)
		if !slices.Equal(ms, c.members) || !slices.Equal(cs, c.channels) {
			t.Errorf("RefsChanged(%s) = %v, %v; want %v, %v", c.ev.Type, ms, cs, c.members, c.channels)
		}
	}

	a.S.SetLatest("C0GEN0001", []slack.Message{m}, false)
	got, ok := a.StoredMessage("C0GEN0001", m.TS)
	if !ok || !slices.Contains(got.Refs.Members, "U0CARA001") {
		t.Errorf("StoredMessage: %v, refs %+v", ok, got.Refs)
	}
	if _, ok := a.StoredMessage("C0GEN0001", "1.0"); ok {
		t.Error("StoredMessage found a message the store doesn't hold")
	}
}
