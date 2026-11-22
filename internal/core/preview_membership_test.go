package core

import (
	"strings"
	"testing"
)

// infoCalls counts the conversations.info calls f has seen.
func (f *fakeSlack) infoCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.calls {
		if m == "conversations.info" {
			n++
		}
	}
	return n
}

// A channel someone else made or renamed reaches every member of the
// team (channel_created, channel_rename), and refreshConv looks it up.
// conversations.info's is_member:false keeps it out of the member set:
// a preview (Opi6 isPreview), no sidebar row, not a channel the composer
// posts in (Karim, α.90: #僕のゴミ showed the composer and no Join).
func TestRefreshedNonMemberChannelStaysAPreview(t *testing.T) {
	a, f := previewApp(t)
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C5","name":"僕のゴミ","is_channel":true,"is_member":false,"creator":"U2"}}`
	a.handleNow(t, rtm("channel_created", map[string]any{"channel": map[string]any{"id": "C5", "name": "僕のゴミ", "creator": "U2"}}))
	if _, member := a.S.Conv("C5"); member {
		t.Fatal("channel_created by someone else put the channel in the member set")
	}
	c, ok := a.Conv("C5")
	if !ok || !c.Preview || c.Member || c.Name != "僕のゴミ" {
		t.Fatalf("created channel: ok=%v %+v", ok, c)
	}
	if strings.Contains(sectionsByType(a)["channels"], "C5") {
		t.Error("a channel the viewer isn't in got a sidebar row")
	}
	a.handleNow(t, rtm("channel_rename", map[string]any{"channel": map[string]any{"id": "C5", "name": "gomi"}}))
	if _, member := a.S.Conv("C5"); member {
		t.Fatal("channel_rename put the channel in the member set")
	}
	if c, _ := a.Conv("C5"); !c.Preview {
		t.Errorf("renamed channel not a preview: %+v", c)
	}

	// its messages (a previewed channel's) don't look it up again each
	n := f.infoCalls()
	a.handleNow(t, rtm("message", map[string]any{"channel": "C5", "ts": "1762516900.000100", "user": "U2", "text": "hi"}))
	a.handleNow(t, rtm("message", map[string]any{"channel": "C5", "ts": "1762516901.000100", "user": "U2", "text": "again"}))
	if got := f.infoCalls(); got != n {
		t.Errorf("messages in a known non-member channel made %d conversations.info calls", got-n)
	}
	if _, member := a.S.Conv("C5"); member {
		t.Fatal("a message put the channel in the member set")
	}

	// joining (from any client) makes it a member's
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C5","name":"gomi","is_channel":true,"is_member":true}}`
	a.handleNow(t, rtm("channel_joined", map[string]any{"channel": map[string]any{"id": "C5", "name": "gomi"}}))
	if _, member := a.S.Conv("C5"); !member {
		t.Fatal("channel_joined didn't make the channel a member's")
	}
	if c, _ := a.Conv("C5"); c.Preview {
		t.Errorf("joined channel still a preview: %+v", c)
	}
}

// A message in a channel outside the member set that conversations.info
// says the viewer is in (joined elsewhere, before channel_joined) is a
// member's; one it says they aren't in stays a preview.
func TestMessageInUnknownChannelUsesIsMember(t *testing.T) {
	a, f := previewApp(t)
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C6","name":"elsewhere","is_channel":true,"is_member":true}}`
	a.handleNow(t, rtm("message", map[string]any{"channel": "C6", "ts": "1762516900.000100", "user": "U2", "text": "hi"}))
	if _, member := a.S.Conv("C6"); !member {
		t.Error("is_member:true channel left out of the member set")
	}
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C4","name":"public","is_channel":true,"is_member":false}}`
	a.handleNow(t, rtm("message", map[string]any{"channel": "C4", "ts": "1762516900.000200", "user": "U2", "text": "hi"}))
	if _, member := a.S.Conv("C4"); member {
		t.Error("is_member:false channel put in the member set")
	}
	if c, ok := a.Conv("C4"); !ok || !c.Preview {
		t.Errorf("C4: ok=%v %+v", ok, c)
	}
	// a member channel's own refresh (a payload without is_member) keeps it
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C1","name":"general","is_channel":true}}`
	a.handleNow(t, rtm("channel_unarchive", map[string]any{"channel": "C1"}))
	if _, member := a.S.Conv("C1"); !member {
		t.Error("a member channel's refresh dropped it from the member set")
	}
}
