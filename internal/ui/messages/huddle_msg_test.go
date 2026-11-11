package messages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Karim, 2026-11-10: a huddle message drew as "Slackbot: A huddle
// started" over a legacy "Huddle / Huddle in progress / Nov 10th" card.
// Slack draws a huddle_thread message without its poster (Slackbot) or its
// text: with system_messages_redesign_m1 on (the account's group) a
// redesigned system row, the headphones in the avatar's place, "{status}.
// {sentence}" and the time, the small Join Huddle under it while you can
// join; with the experiment off the status is the sender's name (a span,
// no APP badge) with the LIVE or MISSED badge, by HuddleMsgAvatar's tile.
// Synthetic rooms only.
const (
	tHuddleCh                   = "C0HUDDLE1"
	tsLive, tsDone, tsMissed    = "1700000100.000100", "1700000900.000100", "1700001900.000100"
	tsHuddleNext, tsHuddleLater = "1700000160.000100", "1700001960.000100"
)

// huddleTestMsgs are a live huddle (bob in it, you not), one you were in
// with bob, one you missed, and ordinary messages after two of them.
func huddleTestMsgs() []slack.Message {
	ch := tHuddleCh
	room := func(v map[string]any) json.RawMessage {
		v["channels"] = []string{ch}
		b, _ := json.Marshal(v)
		return b
	}
	return []slack.Message{
		{Type: "message", Subtype: "huddle_thread", TS: tsLive, User: tBot, Text: "A huddle started",
			Room: room(map[string]any{"id": "R0LIVE", "date_start": 1700000100, "date_end": 0, "has_ended": false,
				"participants": []string{"UBOB"}, "participant_history": []string{"UBOB"}})},
		{Type: "message", Subtype: "huddle_thread", TS: tsDone, User: tBot, Text: "A huddle started",
			Room: room(map[string]any{"id": "R0DONE", "date_start": 1700000300, "date_end": 1700000900, "has_ended": true,
				"participants": []string{}, "participant_history": []string{"UBOB", tSelf}})},
		{Type: "message", Subtype: "huddle_thread", TS: tsMissed, User: tBot, Text: "A huddle started",
			Room: room(map[string]any{"id": "R0MISS", "date_start": 1700001600, "date_end": 1700001900, "has_ended": true,
				"participants": []string{}, "participant_history": []string{"UBOB"}, "missed_participants": map[string]any{tSelf: map[string]any{}}})},
		{Type: "message", TS: tsHuddleNext, User: "UALICE", Text: "joining in a sec"},
		{Type: "message", TS: tsHuddleLater, User: "UBOB", Text: "notes are in the canvas"},
	}
}

func TestHuddleMessageRowLikeSlack(t *testing.T) {
	ch := tHuddleCh
	msgs := huddleTestMsgs()
	docText := func(d richtext.Doc) string {
		var b strings.Builder
		for _, bl := range d.Blocks {
			b.WriteString(richtext.SpansText(bl.Spans))
		}
		return b.String()
	}
	for _, redesign := range []bool{true, false} {
		group := map[bool]string{true: "on", false: "off"}[redesign]
		e := newCtxEnvSetup(t, ch, msgs, func(app *core.App) {
			app.SetExperimentGroup("system_messages_redesign_m1", group)
			app.SetExperimentGroup("huddles_missed_notification", "on")
		})
		var drawn []string
		labelHook = func(txt string, _ textStyle) { drawn = append(drawn, txt) }
		e.frame()
		e.frame()
		labelHook = nil
		for _, l := range drawn {
			if l == "Slackbot" || l == "A huddle started" {
				t.Errorf("redesign %s: %q drawn (the poster or the message text)", group, l)
			}
		}
		has := func(s string) bool {
			for _, l := range drawn {
				if l == s {
					return true
				}
			}
			return false
		}
		for _, c := range []struct {
			ts, status, line, badge string
			join                    bool
		}{
			{tsLive, "A huddle is happening", "A huddle is happening. bob is already there.", "LIVE", true},
			{tsDone, "A huddle happened", "A huddle happened. You and bob were in the huddle for 10m.", "", false},
			{tsMissed, "You missed a huddle", "You missed a huddle. bob was in the huddle for 5m.", "MISSED", false},
		} {
			r := e.row(c.ts)
			if r == nil {
				t.Fatalf("redesign %s: no row %s", group, c.ts)
			}
			if !r.hmOK || r.hm.status != c.status {
				t.Errorf("redesign %s, %s: status %q (ok %v), want %q", group, c.ts, r.hm.status, r.hmOK, c.status)
			}
			if r.hm.room.Joinable != c.join {
				t.Errorf("redesign %s, %s: joinable %v, want %v", group, c.ts, r.hm.room.Joinable, c.join)
			}
			if redesign {
				if !r.m.System || r.hmName {
					t.Errorf("redesign on, %s: System %v, sender line %v; want the redesigned system row", c.ts, r.m.System, r.hmName)
				}
				if got := docText(r.m.Doc); got != c.line {
					t.Errorf("redesign on, %s: the row reads %q, want %q", c.ts, got, c.line)
				}
				continue
			}
			if r.m.System || !r.hmName || r.name != c.status || r.badge != "" {
				t.Errorf("redesign off, %s: System %v, status sender %v, name %q, app badge %q; want the status as the sender, no badge",
					c.ts, r.m.System, r.hmName, r.name, r.badge)
			}
			if c.badge != "" && !has(c.badge) {
				t.Errorf("redesign off, %s: no %s badge drawn (labels %q)", c.ts, c.badge, drawn)
			}
		}
		if redesign && !has("Join Huddle") {
			t.Errorf("redesign on: no Join Huddle under the live huddle (labels %q)", drawn)
		}
		if !redesign && (has("LIVE") && !has("A huddle is happening")) {
			t.Errorf("redesign off: the status isn't drawn as the sender (labels %q)", drawn)
		}
	}
}

// TestHuddleMessageShots (MESSAGES_AUDIT_SHOTS_DIR, the
// messages-audit-shots workflow) draws the rows above, light and dark,
// with system_messages_redesign_m1 on and off.
func TestHuddleMessageShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("MESSAGES_AUDIT_SHOTS_DIR not set (the messages-audit-shots workflow)")
	}
	for _, redesign := range []string{"on", "off"} {
		for _, dark := range []bool{false, true} {
			e := newCtxEnvSetup(t, tHuddleCh, huddleTestMsgs(), func(app *core.App) {
				app.SetExperimentGroup("system_messages_redesign_m1", redesign)
				app.SetExperimentGroup("huddles_missed_notification", "on")
			})
			th := theme.New(dark)
			e.h.th = th
			const w, h = 900, 700
			ops := new(op.Ops)
			f := func() { shotFrame(e.v, th, w, h, ops) }
			loadList(e.v, f)
			for i := 0; i < 30; i++ {
				f()
				time.Sleep(10 * time.Millisecond)
			}
			f()
			img := pixeltest.Render(t, w, h, ops)
			y0, y1 := rowsSpan(e.v.s)
			mode := map[bool]string{false: "light", true: "dark"}[dark]
			savePNG(t, filepath.Join(dir, "huddle-rows-m1-"+redesign+"-"+mode+".png"), crop(img, max(y0-8, 0), min(y1+8, h)))
		}
	}
}
