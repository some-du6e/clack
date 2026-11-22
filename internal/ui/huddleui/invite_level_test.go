package huddleui

import "testing"

// A huddle invite in a conversation set to Nothing notifies, as Slack's
// shouldShowNotificationForHuddle does: getCalculatedDesktopNotifySetting
// reads "nothing" as mentions_dms, so its level check (14) never holds
// one (docs/NOTIFY-RULES.md §6). Clack held it.
func TestIncomingInviteNotifiesAtNothing(t *testing.T) {
	e := newInviteEnv(t, map[string]any{"huddle_invite_sound_v2": ring,
		"all_notifications_prefs": `{"channels":{"` + hcChannel + `":{"desktop":"nothing"}},"global":{"global_desktop":"nothing"}}`})
	e.invite(hcChannel)
	e.shown("the invite")
	waitFor(t, "the invite's notification", func() bool { return len(e.h.notifications()) == 1 })
}
