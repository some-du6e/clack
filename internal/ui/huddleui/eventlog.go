package huddleui

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/taut"
)

// The huddle's event log for plugins, as Slack's client keeps it
// (client-boot 4xZR huddleEvents, mvRv's selectors, the call handlers that
// push into it): the session owns the log (internal/huddle transcribe.go:
// on while captions are, its START_TRANSCRIPTION and TRANSCRIBE events,
// and the JOIN, LEAVE, SCREENSHARE_START, SCREENSHARE_STOP and
// SEND_REACTION events pushed while it's on), and taut's huddleEvents
// slice is that log. Slack's enableEventLog / disableEventLog actions and
// its transcription fetchers reach the session through taut.HuddleHooks,
// so a plugin that keeps the log open (HuddleActivity) gets the log Slack
// would give it. The Captions panel (captions_panel.go) draws the log
// as plugins present it.

// bindTaut tells plugins about the huddle you're in: its reaction sender,
// the active huddle, and the event log's hooks.
func (c *Controller) bindTaut(tt *taut.Taut, s *huddle.Session, channel string) {
	if tt == nil || s == nil {
		return
	}
	tt.SetHuddleReaction(func(emoji, kind, url string) bool { return s.React(emoji, url) == nil })
	tt.SetHuddleHooks(taut.HuddleHooks{
		EventLog: s.SetEventLog,
		Transcription: func(ctx context.Context, callID string, on bool) error {
			if callID != s.RoomID() {
				return nil
			}
			return s.Transcription(ctx, on)
		},
		Backgrounds: func(ctx context.Context) ([]taut.HuddleBackground, error) {
			ts, err := c.themes(ctx, s.RoomID())
			out := make([]taut.HuddleBackground, 0, len(ts))
			for _, t := range ts {
				out = append(out, taut.HuddleBackground{ID: t.ID, Name: t.Name, Category: t.Category})
			}
			return out, err
		},
	})
	tt.SetActiveHuddle(s.RoomID(), channel, s.SelfAttendee(), c.selfID())
	c.feedLog(tt, s, s.State(), true)
}

// logFP is what feedLog last sent, to skip unchanged session updates
// (they come at the volume meters' rate).
type logFP struct {
	on, loading, cc bool
	captions, log   int
	last            string
	room            string
}

// feedLog hands taut the session's event log (force: even if unchanged).
func (c *Controller) feedLog(tt *taut.Taut, s *huddle.Session, st huddle.State, force bool) {
	on, loading := s.EventLog()
	fp := logFP{on: on, loading: loading, cc: st.CaptionsOn, captions: len(st.Captions), log: len(st.Log), room: s.RoomID()}
	if n := len(st.Captions); n > 0 {
		l := st.Captions[n-1]
		fp.last = l.ID + "\x00" + l.Text
	}
	c.mu.Lock()
	same := c.logFP == fp
	c.logFP = fp
	c.mu.Unlock()
	if same && !force {
		return
	}
	tt.SetHuddleLog(taut.HuddleLog{Enabled: on, Loading: loading, ClosedCaptions: st.CaptionsOn, Events: logEvents(st)})
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// logEvents is the log as Slack's huddleEvents.events holds it: each
// event {type, memberId, timestampMs, …}, by time (updateTranscriptions
// sorts the log by timestampMs; START_TRANSCRIPTION, at 0, comes first).
func logEvents(st huddle.State) []map[string]any {
	out := make([]map[string]any, 0, len(st.Captions)+len(st.Log))
	for _, l := range st.Captions {
		if l.Start {
			out = append(out, map[string]any{"type": "START_TRANSCRIPTION", "memberId": "USLACKBOT", "timestampMs": int64(0)})
			continue
		}
		at := ms(l.At)
		out = append(out, map[string]any{"type": "TRANSCRIBE", "resultId": l.ID, "memberId": l.UserID, "content": l.Text,
			"timestampMs": at, "endTimeMs": at})
	}
	for _, e := range st.Log {
		ev := map[string]any{"type": e.Type, "memberId": e.MemberID, "timestampMs": ms(e.At)}
		if e.Emoji != "" {
			ev["emoji"] = e.Emoji
		}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["timestampMs"].(int64) < out[j]["timestampMs"].(int64) })
	return out
}

// onTranscriptionStatus is a status of the call's transcription, which
// Slack hands its processAwsTranscription thunk ({type, eventTimeMs}): a
// plugin wrapping it (HuddleActivity's "Transcription started.") sees it.
// Clack's own captions come from the session, so the native does nothing.
func (c *Controller) onTranscriptionStatus(status string) {
	tt := c.taut()
	if tt == nil {
		return
	}
	type statusArgs struct {
		Type        string `json:"type"`
		EventTimeMs int64  `json:"eventTimeMs"`
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = taut.Dispatch(ctx, tt, "processAwsTranscription", statusArgs{Type: status, EventTimeMs: time.Now().UnixMilli()},
			func(ctx context.Context, _ statusArgs) (any, error) { return nil, nil })
	}()
}

// ---- alerts ----

// Slack's displayAlertMessage thunk (client-boot EeW/): some alerts
// (peerMuteRequest among them) show 3 s after they're asked for, and an
// alert already waiting drops the next of its kind; then addAlert puts
// makeAlertObject's entry in alert.alerts and the banner shows it.
// onPeerMuteRequest dispatches it with {alert: "peerMuteRequest", peerId}
// when someone mutes you, after muting your microphone.

const alertDelay = 3 * time.Second

// delayedAlerts are displayAlertMessage's 3 s alerts (EeW/ o).
var delayedAlerts = map[string]bool{"noUserAudioStream": true, "noUserVideoStream": true, "noAudioOutputAudioElementError": true,
	"noAudioOutputAudioLevelError": true, "peerMuteRequest": true, "NoAudioDetected": true}

type alertArgs struct {
	Alert  string `json:"alert"`
	PeerID string `json:"peerId,omitempty"`
}

type huddleAlerts struct {
	mu      sync.Mutex
	pending map[string]bool // EeW/ l: an alert waiting its 3 s
	shown   []map[string]any
}

// displayAlert dispatches Slack's displayAlertMessage, so a plugin that
// wraps it (HuddleActivity, HuddleMuteDetector) sees the alert.
func (c *Controller) displayAlert(alert, peer string) {
	tt := c.taut()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = taut.Dispatch(ctx, tt, "displayAlertMessage", alertArgs{Alert: alert, PeerID: peer},
			func(ctx context.Context, a alertArgs) (any, error) {
				c.queueAlert(a)
				return nil, nil
			})
	}()
}

func (c *Controller) queueAlert(a alertArgs) {
	as := &c.alerts
	as.mu.Lock()
	if as.pending == nil {
		as.pending = map[string]bool{}
	}
	if as.pending[a.Alert] {
		as.mu.Unlock()
		return
	}
	if !delayedAlerts[a.Alert] {
		as.mu.Unlock()
		c.addAlert(a)
		return
	}
	as.pending[a.Alert] = true
	as.mu.Unlock()
	time.AfterFunc(alertDelay, func() {
		as.mu.Lock()
		delete(as.pending, a.Alert)
		as.mu.Unlock()
		c.addAlert(a)
	})
}

// addAlert is addAlert with makeAlertObject's entry, and its banner.
func (c *Controller) addAlert(a alertArgs) {
	if c.Session() == nil {
		return
	}
	switch a.Alert {
	case "peerMuteRequest":
		entry := map[string]any{"priority": 802, "type": a.Alert, "id": a.PeerID, "dismissible": true, "isWarning": true}
		c.alerts.mu.Lock()
		c.alerts.shown = []map[string]any{entry}
		shown := c.alerts.shown
		c.alerts.mu.Unlock()
		if tt := c.taut(); tt != nil {
			tt.SetHuddleAlerts(shown)
		}
		// HuddlesInformationalBanner "user_was_muted" (microphone-slash)
		c.showNamedBanner("informational_user_was_muted", nsHuddles.T("Someone muted you."), "mic-off")
	}
}

// clearAlerts drops the shown alerts (their banner closed or was replaced).
func (c *Controller) clearAlerts() {
	c.alerts.mu.Lock()
	had := len(c.alerts.shown) > 0
	c.alerts.shown = nil
	c.alerts.mu.Unlock()
	if had {
		if tt := c.taut(); tt != nil {
			tt.SetHuddleAlerts(nil)
		}
	}
}
