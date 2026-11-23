package huddleui

import (
	"testing"

	"github.com/some-du6e/clack/internal/store"
)

// A huddle invite in a conversation set to Nothing notifies, as Slack's
// shouldShowNotificationForHuddle does: getCalculatedDesktopNotifySetting
// reads "nothing" as mentions_dms, so its level check (14) never holds
// one (docs/NOTIFY-RULES.md §6). Clack held it. The prefs are installed
// as core installs users.prefs.get's (ParsePrefs), so the levels read
// Nothing.
func TestIncomingInviteNotifiesAtNothing(t *testing.T) {
	e := newInviteEnv(t, map[string]any{"huddle_invite_sound_v2": ring,
		"all_notifications_prefs": `{"channels":{"` + hcChannel + `":{"desktop":"nothing"}},"global":{"global_desktop":"nothing"}}`})
	e.app.S.RLock()
	raw := e.app.S.RawPrefs
	e.app.S.RUnlock()
	p, muted := store.ParsePrefs(raw)
	e.app.S.SetPrefs(p, muted)
	if got := e.app.Prefs(); got.GlobalDesktop != "nothing" || got.ChannelNotify[hcChannel].Desktop != "nothing" {
		t.Fatalf("setup: global_desktop %q, the channel's desktop %q; want both nothing", got.GlobalDesktop, got.ChannelNotify[hcChannel].Desktop)
	}
	e.invite(hcChannel)
	e.shown("the invite")
	waitFor(t, "the invite's notification", func() bool { return len(e.h.notifications()) == 1 })
}
