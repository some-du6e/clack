package huddleui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
)

// A huddle_recommend, the server's suggestion to join the huddle going on
// in a conversation, shows as Slack's showHuddleRecommendNotification
// shows it (recommend.go): no title, the conversation as the subtitle,
// "{A and B} are talking in {conversation}."; nothing under Do Not
// Disturb (13) or in a muted conversation (12), each logged with its
// reason. Clack showed nothing. Synthetic people only.
func TestHuddleRecommendLikeSlack(t *testing.T) {
	recommend := func(e *inviteEnv, users ...string) {
		raw, _ := json.Marshal(map[string]any{"type": "huddle_recommend", "channel_id": hcChannel, "talking_users": users})
		e.c.Handle(core.Event{Type: "huddle", Name: "huddle_recommend", Raw: raw})
	}
	e := newInviteEnv(t, nil)
	// Do Not Disturb first: core asks dnd.info once
	e.fs.SetDND(map[string]any{"snooze_enabled": true, "snooze_endtime": time.Now().Add(time.Hour).Unix()})
	recommend(e, huddle.FakeRemoteUser)
	time.Sleep(500 * time.Millisecond)
	if n := e.h.notifications(); len(n) != 0 {
		t.Errorf("a suggestion under Do Not Disturb showed: %+v", n)
	}
	e.app.SetDNDStatus(core.DNDStatus{})

	recommend(e, huddle.FakeRemoteUser, huddle.FakeSelfUser)
	waitFor(t, "the suggestion", func() bool { return len(e.h.notifications()) == 1 })
	n := e.h.notifications()[0]
	who, other := e.c.displayName(huddle.FakeRemoteUser), e.c.displayName(huddle.FakeSelfUser)
	want := who + " and " + other + " are talking in #huddle-test."
	if n.Body != want || n.PlainBody != want || n.Title != "" || n.Subtitle != "#huddle-test" || !isRecommend(n) || n.Channel != hcChannel || len(n.Buttons) != 0 {
		t.Errorf("the suggestion: title %q, subtitle %q, body %q (plain %q), recommend %v, channel %q, %d buttons; want no title, #huddle-test and %q",
			n.Title, n.Subtitle, n.Body, n.PlainBody, isRecommend(n), n.Channel, len(n.Buttons), want)
	}

	e.app.S.Lock()
	e.app.S.Muted = map[string]bool{hcChannel: true}
	e.app.S.Unlock()
	recommend(e, huddle.FakeRemoteUser)
	time.Sleep(500 * time.Millisecond)
	if n := e.h.notifications(); len(n) != 1 {
		t.Errorf("a suggestion in a muted conversation showed: %d notifications", len(n))
	}
}
