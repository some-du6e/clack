package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// users.conversations answers the viewer's conversations without
// is_member (fakeslack leaves it out, as Slack does); boot marks its
// channels as the viewer's, or every public channel reads as one you
// only preview and the hover toolbar has no Reply in thread (Karim,
// 2026-11-13: "i cant reply to threads anymore").
func TestBootChannelsAreMembers(t *testing.T) {
	a, _, _ := rtApp(t)
	var raw json.RawMessage
	if err := a.C.Call(context.Background(), "users.conversations", nil, &raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"is_member"`) {
		t.Fatal("fakeslack's users.conversations carries is_member; Slack's doesn't")
	}
	a.S.Lock()
	var convs []slack.Conversation
	for _, c := range a.S.Convs {
		convs = append(convs, *c)
	}
	a.S.Unlock()
	n := 0
	for _, c := range convs {
		if c.IsIM || c.IsMPIM {
			continue
		}
		n++
		if !c.IsMember {
			t.Errorf("%s (%s): not a member after boot", c.ID, c.Name)
		}
	}
	if n == 0 {
		t.Fatal("no channels after boot")
	}
	// a later payload without is_member (a write's channel echo) keeps it
	for _, c := range convs {
		if c.IsIM || c.IsMPIM {
			continue
		}
		cp := c
		cp.IsMember = false
		a.S.PutConv(cp)
		if got, _ := a.S.Conv(c.ID); !got.IsMember {
			t.Errorf("%s: PutConv without is_member made it a preview", c.ID)
		}
		break
	}
}
