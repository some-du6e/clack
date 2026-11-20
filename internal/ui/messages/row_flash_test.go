package messages

import (
	"encoding/json"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// Every row is MessageBackground's (Message's first child, boot-shared
// b5pm; ThreadRootGeneric and ThreadRootCanvas wrap theirs too, JM3a), so
// a jump flashes a tombstone (MessageTombstoneContent, sQ5n), a system
// message and a huddle message the way it does an ordinary one, in the
// conversation and, as a thread's root, in the thread pane. Clack painted
// the highlight only in ordinary message rows: Karim's deleted root never
// flashed. Synthetic messages.

const (
	tFlashCh                                  = "C0FLASH01"
	tsFlashTomb, tsFlashJoin, tsFlashHuddle   = "1700000100.000100", "1700000200.000100", "1700000300.000100"
	tsFlashPlain, tsFlashReply, tsFlashReply2 = "1700000400.000100", "1700000110.000100", "1700000310.000100"
)

func flashMsgs() []slack.Message {
	room, _ := json.Marshal(map[string]any{"id": "R0FLASH", "channels": []string{tFlashCh}, "date_start": 1700000300,
		"date_end": 1700000600, "has_ended": true, "participants": []string{}, "participant_history": []string{"UBOB"}})
	return []slack.Message{
		{Type: "message", Subtype: "tombstone", TS: tsFlashTomb, User: tBot, Hidden: true, Text: "This message was deleted.",
			ThreadTS: tsFlashTomb, ReplyCount: 1, LatestReply: tsFlashReply},
		{Type: "message", Subtype: "channel_join", TS: tsFlashJoin, User: "UBOB", Text: "<@UBOB> has joined the channel"},
		{Type: "message", Subtype: "huddle_thread", TS: tsFlashHuddle, User: tBot, Text: "A huddle started", Room: room,
			ThreadTS: tsFlashHuddle, ReplyCount: 1, LatestReply: tsFlashReply2},
		{Type: "message", TS: tsFlashPlain, User: "UALICE", Text: "after the huddle"},
	}
}

// lights runs frames until row ts of l draws the jump highlight.
func lights(t *testing.T, frame func(), l *listState, ts, what string) {
	t.Helper()
	for dl := time.Now().Add(5 * time.Second); ; {
		frame()
		if r := l.rows[ts]; r != nil && r.hlDrawn > 0 {
			return
		}
		if time.Now().After(dl) {
			t.Fatalf("%s: row %s never drew the jump highlight", what, ts)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEveryRowFlashes(t *testing.T) {
	msgs := flashMsgs()
	e := newCtxEnvSetup(t, tFlashCh, msgs, func(app *core.App) { app.SetExperimentGroup("system_messages_redesign_m1", "on") })
	for _, c := range []struct{ ts, what string }{
		{tsFlashTomb, "a tombstone"}, {tsFlashJoin, "a join"}, {tsFlashHuddle, "a huddle message"}, {tsFlashPlain, "a message"},
	} {
		e.v.Highlight(c.ts)
		lights(t, e.frame, e.v.s, c.ts, c.what)
	}
	time.Sleep(highlightFor + 200*time.Millisecond)
	e.frame()
	if r := e.row(tsFlashPlain); r == nil || r.hlDrawn != 0 {
		t.Errorf("the last flash didn't end at 3 s")
	}
	// the thread pane's roots: the huddle's and the deleted one
	app := e.h.app
	app.S.SetThread(tFlashCh, tsFlashHuddle, []slack.Message{msgs[2],
		{Type: "message", TS: tsFlashReply2, User: "UALICE", ThreadTS: tsFlashHuddle, Text: "notes in the canvas"}})
	app.S.SetThread(tFlashCh, tsFlashTomb, []slack.Message{msgs[0],
		{Type: "message", TS: tsFlashReply, User: "UBOB", ThreadTS: tsFlashTomb, Text: "a reply to it"}})
	for _, c := range []struct{ root, what string }{{tsFlashHuddle, "a huddle's root"}, {tsFlashTomb, "a deleted root"}} {
		tv := NewThreadView(e.h, tFlashCh, c.root)
		frame := frameFn(tv)
		tv.JumpTo(c.root, true)
		lights(t, frame, tv.s, c.root, c.what+" in the thread pane")
		time.Sleep(highlightFor + 200*time.Millisecond)
		frame()
		if r := tv.s.rows[c.root]; r == nil || r.hlDrawn != 0 {
			t.Errorf("%s in the thread pane: the flash didn't end at 3 s", c.what)
		}
	}
}

// TestRepeatJumpKeepsFlash: a second jump to a message still flashing
// doesn't start its flash over (its highlight prop doesn't turn from false
// to true: MessageBackground's componentDidUpdate, rFp4), so the flash ends
// 3 s after the first. Clack restarted it.
func TestRepeatJumpKeepsFlash(t *testing.T) {
	e := newCtxEnvSetup(t, tFlashCh, flashMsgs(), nil)
	e.v.Highlight(tsFlashPlain)
	lights(t, e.frame, e.v.s, tsFlashPlain, "the first jump")
	start := time.Now()
	time.Sleep(1500 * time.Millisecond)
	e.v.Highlight(tsFlashPlain)
	e.frame()
	e.frame()
	time.Sleep(time.Until(start.Add(highlightFor + 300*time.Millisecond)))
	e.frame()
	r := e.row(tsFlashPlain)
	if r == nil {
		t.Fatalf("the row isn't on screen")
	}
	if r.hlDrawn != 0 {
		t.Errorf("3.3 s after the first jump the row still flashes (%.2f): the second jump restarted it", r.hlDrawn)
	}
}

// TestFlashTintsSeeThroughFills: a reaction pill is rgba
// (.c-reaction: --dt_color-surf-pry), so in a row holding the jump
// highlight it shows the yellow through it, as code and mentions do
// (richview's SetUnder). Clack pre-blended it over the pane, so it stayed
// grey on the yellow row. Synthetic messages.
func TestFlashTintsSeeThroughFills(t *testing.T) {
	msgs := []slack.Message{
		{Type: "message", TS: tsFlashPlain, User: "UALICE", Text: "a message with a reaction",
			Reactions: []slack.Reaction{{Name: "tada", Count: 1, Users: []string{"UBOB"}}}},
	}
	e := newCtxEnvSetup(t, tFlashCh, msgs, nil)
	e.v.Highlight(tsFlashPlain)
	lights(t, e.frame, e.v.s, tsFlashPlain, "the jump")
	for dl := time.Now().Add(3 * time.Second); ; { // the hold: 0.6 s to 1.8 s
		e.frame()
		if r := e.row(tsFlashPlain); r != nil && r.hlDrawn == 1 {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("the highlight never held")
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.frame() // the pills follow the fill a frame later
	var fills []color.NRGBA
	rrectHook = func(_ image.Rectangle, c color.NRGBA) { fills = append(fills, c) }
	e.frame()
	rrectHook = nil
	c := colorsFor(e.h.th)
	want := richview.Over(lightColors.ReactBg, c.Highlight)
	for _, f := range fills {
		if f == want {
			return
		}
		if f == c.ReactBg {
			t.Fatalf("the reaction pill drew %v, its blend over the pane, on the yellow row; want %v (its rgba over the highlight)", f, want)
		}
	}
	t.Errorf("no reaction pill drawn in %v over the highlight %v; fills %v", want, c.Highlight, fills)
}

// TestTombstoneSquareLikeSlack: the deleted message's square is
// .c-message_kit__tombstone__icon's rgba(fg_low,.13) in both modes, about
// (226,226,226) over the light pane. Clack drew it in BadgeBg, 6% in light
// mode: (241,241,241).
func TestTombstoneSquareLikeSlack(t *testing.T) {
	e := newCtxEnvSetup(t, tFlashCh, flashMsgs()[:1], nil)
	until(t, 5*time.Second, "the deleted message's row", e.frame, func() bool { return e.row(tsFlashTomb) != nil })
	type fill struct {
		r image.Rectangle
		c color.NRGBA
	}
	var fills []fill
	rrectHook = func(r image.Rectangle, c color.NRGBA) { fills = append(fills, fill{r, c}) }
	e.frame()
	rrectHook = nil
	want := richview.Over(rgba(0x1d1c1d, 0.13), rgb(0xffffff))
	var squares []color.NRGBA
	for _, f := range fills {
		if f.r.Dx() == 36 && f.r.Dy() == 36 {
			squares = append(squares, f.c)
		}
	}
	if len(squares) != 1 || squares[0] != want {
		t.Errorf("the deleted message's square drew %v; want rgba(fg_low,.13) over the pane, %v", squares, want)
	}
}
