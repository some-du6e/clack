package ui

import (
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

// The sidebar row menu runs popups' channel-menu actions, as Slack does:
// leaving #general shows Slack's "Membership is required" alert (no
// conversations.leave; public channels are left at once, private ones
// ask: channels_audit_test.go), closing a DM doesn't ask, and star/mute
// are popups' real actions; the other keys stay the shell's.
func TestSidebarMenuUsesPopupsActions(t *testing.T) {
	a, ca := ttApp(t, t.TempDir())
	ca.S.PutConv(slack.Conversation{ID: "D0ALICE01", IsIM: true, User: "UALICE01", IsMember: true})
	for _, key := range []string{"toggle_star", "toggle_mute", "leave", "close"} {
		if popups.ChannelMenuAction(a, ttGen, key) == nil {
			t.Errorf("%s isn't popups' action", key)
		}
		if a.sbMenuDo(ttGen, key) == nil {
			t.Errorf("%s does nothing in the sidebar", key)
		}
	}
	for _, key := range []string{"view_details", "copy_link", "copy_name", "mark_read"} {
		if popups.ChannelMenuAction(a, ttGen, key) != nil {
			t.Errorf("popups claims %s", key)
		}
		if a.sbMenuDo(ttGen, key) == nil {
			t.Errorf("the shell lost %s", key)
		}
	}
	n := len(a.overlays)
	a.sbMenuDo(ttGen, "leave")()
	if len(a.overlays) != n+1 {
		t.Fatalf("leaving #general showed %d overlays, want Slack's alert", len(a.overlays)-n)
	}
	if _, ok := a.overlays[len(a.overlays)-1].(*popups.Modal); !ok {
		t.Errorf("leaving #general showed %T, want the alert modal", a.overlays[len(a.overlays)-1])
	}
	n = len(a.overlays)
	a.sbMenuDo("D0ALICE01", "close")()
	for _, o := range a.overlays[n:] { // (popups' async pump is an overlay too)
		if _, ok := o.(*popups.Modal); ok {
			t.Errorf("closing a DM asked for confirmation")
		}
	}
}
