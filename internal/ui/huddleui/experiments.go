package huddleui

import (
	"github.com/some-du6e/clack/internal/huddle"
)

// Slack's huddle experiments, as its client reads them (client-boot pSxm,
// build 4d0c78085232; docs/EXPERIMENTS.md). Every selector compares one
// group ("on", or "treatment" for the two A/B ones), so an experiment the
// account isn't assigned to reads as off; the Taut and Slick Experiments
// plugins can force either group, and every check here reads core at the
// moment it runs, so a flip takes effect on the next frame or action.
const (
	ExpJoinFromPreview     = "huddle_join_from_preview"              // M1: huddles in channels you're previewing
	ExpPreviewV2           = "huddles_preview_v2"                    // ZN: the join preview
	ExpAloneTimeout        = "huddles_alone_timeout"                 // JQ: "Are you still there?"
	ExpAloneShortened      = "huddles_alone_timeout_shortened"       // p4: its 30 s / 15 s variant
	ExpAVButtonPermissions = "huddles_av_button_permission_handling" // yc: the mic button's OS permission
	ExpAwarenessTimestamp  = "huddles_awareness_timestamp"           // gG: awareness entries carry their time
	ExpMilesDavis          = "huddles_miles_davis"                   // fJ: the Miles Davis lounge songs
	ExpMissedNotification  = "huddles_missed_notification"           // Z0: "Huddle missed", MISSED badges
	ExpMultiDevice         = "huddles_multi_device"                  // pL (treatment): a huddle on two devices
	ExpScreensharePopout   = "huddles_screenshare_popout"            // fD: a shared screen in its own window
	ExpShareAudio          = "huddles_screenshare_share_audio"       // Jf: the screen picker's "Share system audio"
	ExpSwitchMicHelper     = "huddles_switch_microphone_helper"      // fs: "We are not detecting any audio…"
	ExpNoiseSuppAwareness  = "huddles_noise_suppression_awareness"   // nL: the Audio settings menu's NS row
	ExpNewGrids            = "huddles_new_grids"                     // KR: no troubleshooting button in the compact header
)

// on reports whether name is in group (Slack's "on"===getGroupForUser).
func (c *Controller) on(name string) bool { return c.group(name, "on") }

// group reports whether name is in g.
func (c *Controller) group(name, g string) bool {
	app := c.h.Core()
	return app != nil && app.Experiment(name) == g
}

// mediaExperiments are the experiments the media layer follows.
func (c *Controller) mediaExperiments() huddle.Experiments {
	return huddle.Experiments{
		AwarenessTimestamp: c.on(ExpAwarenessTimestamp),
		MultiDevice:        c.group(ExpMultiDevice, "treatment"),
	}
}

// syncExperiments hands the session the current groups (a plugin may
// have flipped one); cheap enough to run on every session change.
func (c *Controller) syncExperiments(s *huddle.Session) {
	if s != nil {
		s.SetExperiments(c.mediaExperiments())
	}
}

// SupportsHuddles is doesChannelSupportHuddles' preview rule (client-boot
// zq6z doesChannelSupportHuddlesWithReason: isPreview(t) &&
// !huddle_join_from_preview → unsupported): a channel you're previewing
// (Opi6 isPreview: not archived, public or private, not a member) takes
// no huddle button and no start without the experiment.
func (c *Controller) SupportsHuddles(ch string) bool {
	app := c.h.Core()
	if app == nil {
		return true
	}
	conv, ok := app.Conv(ch)
	if !ok {
		return true
	}
	preview := conv.Preview && !conv.IsArchived && (conv.Kind == "channel" || conv.Kind == "private")
	return !preview || c.on(ExpJoinFromPreview)
}
