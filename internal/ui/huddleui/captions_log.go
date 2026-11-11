package huddleui

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/taut"
)

// Turning captions on and off, as the ⋯ menu's CaptionsSubmenu does it
// (gantry-v2-async-client-boot-shared a+g1, toggleClosedCaptions):
//
//	(allowHuddleTranscriptions || closed captions on) && the huddle is
//	starting or started (zq6z sx, isHuddleActiveState) →
//	    side-by-side off and closed captions on ? stopEventLog : startEventLog,
//	    then setClosedCaptionsEnabled(!on)
//
// startEventLog and stopEventLog are Slack's thunks (client-boot B3Qp,
// YmpG), so a plugin wrapping them sees the same calls; their natives
// are the session's rooms.startTranscription / rooms.stopTranscription
// (internal/huddle transcribe.go). Joining with Audio & video's
// "Automatically turn on captions" (huddles_cc_by_default) runs
// startEventLog and turns them on once your audio connects
// (enableCaptionsIfOnByDefault). Clack has no side-by-side captions (the
// huddle window has no side panel), so that half is always off.

// allowTranscriptions is allowHuddleTranscriptions (zq6z UA / R8): the
// allow_huddles_transcriptions team pref, false when the loaded prefs
// don't set it. Before team.prefs.get answers (or on Grid, where Slack
// also reads org-level prefs Clack isn't sent) it allows them.
func (c *Controller) allowTranscriptions() bool {
	app := c.h.Core()
	if app == nil {
		return false
	}
	if !app.TeamPrefsLoaded() {
		return true
	}
	v, ok := app.TeamPref("allow_huddles_transcriptions")
	if !ok {
		return false
	}
	var b bool
	if json.Unmarshal(v, &b) == nil {
		return b
	}
	var str string
	return json.Unmarshal(v, &str) == nil && str == "true"
}

// ToggleClosedCaptions is the menu's Closed captions row. Any goroutine.
func (c *Controller) ToggleClosedCaptions(s *huddle.Session) {
	if s == nil {
		return
	}
	st := s.State()
	on := st.CaptionsOn
	if !c.allowTranscriptions() && !on {
		return
	}
	if st.Phase == huddle.Ended {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if on {
			c.stopEventLog(ctx, s)
		} else {
			c.startEventLog(ctx, s)
		}
	}()
	s.SetCaptions(!on)
	c.changed()
}

// startEventLog dispatches Slack's startEventLog; its native starts the
// transcription (and, like Slack's, stops the lounge music first).
func (c *Controller) startEventLog(ctx context.Context, s *huddle.Session) {
	_, err := taut.Dispatch(ctx, c.taut(), "startEventLog", huddleArgs{HuddleID: s.RoomID(), ChannelID: s.Channel()},
		func(ctx context.Context, _ huddleArgs) (any, error) {
			c.stopMusic(s) // stopMusic({logContext: "huddles-captions-turned-on"})
			return nil, s.StartEventLog(ctx)
		})
	if err != nil {
		log.Printf("huddle: startEventLog: %v", err)
	}
	c.changed()
}

// stopEventLog dispatches Slack's stopEventLog (a no-op unless the
// transcription is on).
func (c *Controller) stopEventLog(ctx context.Context, s *huddle.Session) {
	if on, loading := s.EventLog(); !on || loading {
		return
	}
	_, err := taut.Dispatch(ctx, c.taut(), "stopEventLog", huddleArgs{HuddleID: s.RoomID(), ChannelID: s.Channel()},
		func(ctx context.Context, _ huddleArgs) (any, error) { return nil, s.StopEventLog(ctx) })
	if err != nil {
		log.Printf("huddle: stopEventLog: %v", err)
	}
	c.changed()
}

// captionsByDefault is enableCaptionsIfOnByDefault: with
// huddles_cc_by_default and transcriptions allowed, captions turn on as
// your audio connects.
func (c *Controller) captionsByDefault(s *huddle.Session) {
	app := c.h.Core()
	if app == nil || !app.PrefBool("huddles_cc_by_default", false) || !c.allowTranscriptions() {
		return
	}
	s.SetCaptions(true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c.startEventLog(ctx, s)
	}()
}
