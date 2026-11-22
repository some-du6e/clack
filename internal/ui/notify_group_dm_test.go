package ui

// A group DM has a level, as any conversation but a 1:1 DM does. Slack's
// shouldShowNotificationForChannelMessage (Fpyc rr) reads it through
// getCalculatedDesktopNotifySetting: the group DM's own desktop override,
// else global_mpdm_desktop (getGlobalDmNotifySetting, everything when
// unset), "nothing" read as mentions_dms either way. At mentions only a
// mention notifies; only the 1:1 DM rule (Fpyc RM) reads no level
// (notify_dm.go). Synthetic: fakeslack's thread-header group DM.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestNotifyGroupDMLevelLikeSlack(t *testing.T) {
	a, ca, _, rec := privNoteAppFS(t)
	g := fakeslack.ThreadGroupDMID
	ca.S.PutConv(fixtureConv(t, ca, g))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	posts, shown := 0, 0
	// post delivers a message in the group DM, one that mentions the
	// viewer or not, and checks whether it toasts
	post := func(what string, mention, want bool) {
		t.Helper()
		posts++
		ts := fmt.Sprintf("1762520500.%06d", 100+posts)
		a.data.handle(a, core.Event{Type: "message", Channel: g, TS: ts, ConvKind: "mpim",
			Msg: noteMsg(ts, "", fmt.Sprintf("a synthetic line %d", posts)), Mention: mention})
		if want {
			shown++
			if n := rec.wait(t, a, shown); n.Msg != ts {
				t.Errorf("%s: the toast is for %s, want %s", what, n.Msg, ts)
			}
			return
		}
		if got := quiet(a, rec); got != shown {
			t.Errorf("%s: a toast, Slack holds it back (no mention at the mentions level)", what)
			shown = got
		}
	}

	// unset global_mpdm_desktop: everything
	post("the default level, no mention", false, true)

	// the group DM's own "nothing": mentions_dms, so a mention only
	must("the group DM's level nothing", ca.SetNotifyPrefs(ctx, g, core.ChannelNotify{Desktop: "nothing"}))
	post(`the group DM's own desktop "nothing", no mention`, false, false)
	post(`the group DM's own desktop "nothing", a mention`, true, true)

	// global_mpdm_desktop decides without an override of its own
	must("global_mpdm_desktop mentions", ca.SetGlobalNotifyPref(ctx, "global_mpdm_desktop", "mentions_dms"))
	must("the group DM's level everything", ca.SetNotifyPrefs(ctx, g, core.ChannelNotify{Desktop: "everything"}))
	post(`global_mpdm_desktop mentions, the group DM's own everything`, false, true)
	must("the group DM's level default", ca.SetNotifyPrefs(ctx, g, core.ChannelNotify{Desktop: "default"}))
	post(`global_mpdm_desktop mentions, no mention`, false, false)
	post(`global_mpdm_desktop mentions, a mention`, true, true)
	must("global_mpdm_desktop nothing", ca.SetGlobalNotifyPref(ctx, "global_mpdm_desktop", "nothing"))
	post(`global_mpdm_desktop "nothing", no mention`, false, false)
	post(`global_mpdm_desktop "nothing", a mention`, true, true)
	must("global_mpdm_desktop everything", ca.SetGlobalNotifyPref(ctx, "global_mpdm_desktop", "everything"))
	post(`global_mpdm_desktop everything, no mention`, false, true)
}
