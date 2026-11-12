package huddleui

import (
	"context"
	"image"
	"os"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/idleprobe"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// TestHuddleWindowIdleFrames is internal/ui TestIdleFrames for the huddle
// window (CLACK_IDLE_FRAMES=1): you alone in a huddle against the fake
// Chime, microphone silent, camera off, nothing moving, for 10 s. Slack's
// huddle window draws nothing then (no caret, and it shows no timer:
// window.go). Allowed: idleOtherFrames, for a clock tick. (With someone
// else in it, their tile's spinner until their media connects, their
// speaking bars and a shared screen animate, as in Slack.)
func TestHuddleWindowIdleFrames(t *testing.T) {
	if os.Getenv("CLACK_IDLE_FRAMES") == "" {
		t.Skip("CLACK_IDLE_FRAMES not set")
	}
	p := idleprobe.New()
	defer p.Close()
	e := newShotEnv(t, true)
	invalidateHook = func(w *window) {
		if w == e.w {
			p.Invalidated()
		}
	}
	defer func() { invalidateHook = nil }()
	e.c.mu.Lock()
	e.c.win = e.w // the open huddle window: Controller.changed redraws it
	e.c.mu.Unlock()

	// A new DM huddle: rooms.join lists only you (the fake's answer for a
	// D… channel), SoloRoom has nobody on the media, and NewFake(nil)
	// shares no screen. Nothing on screen moves.
	const dm = "D0IDLE00001"
	e.h.app.S.PutConv(slack.Conversation{ID: dm, IsIM: true, User: huddle.FakeRemoteUser})
	e.fk = huddle.NewFake(nil, t.Errorf)
	t.Cleanup(e.fk.Close)
	e.fk.SoloRoom()
	granted := func() <-chan avperm.Status {
		ch := make(chan avperm.Status, 1)
		ch <- avperm.Authorized
		return ch
	}
	o := huddle.Options{Client: e.fk.Caller(), SelfID: huddle.FakeSelfUser, Logf: t.Logf, OnChange: e.c.onSession,
		MicAccess: granted, OpenAudio: nullAudio}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := huddle.Join(ctx, o, dm)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Leave)
	e.c.mu.Lock()
	e.c.s = s
	e.c.mu.Unlock()
	waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected })

	stop := e.w.startTick() // window.run's
	defer stop()
	var (
		r   input.Router
		ops op.Ops
	)
	frame := func(now time.Time) {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(620, 400)), Source: r.Source()}
		slackc.LockFrame()
		restore := slackc.SubFrame(gtx, "p-multi_view_window p-huddle_window", hudWhite)
		e.w.layout(gtx)
		restore()
		slackc.UnlockFrame()
		r.Frame(&ops)
	}
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); { // joining: the spinner, the join sound's banner
		frame(time.Now())
		time.Sleep(16 * time.Millisecond)
	}
	res := p.Run("huddle window, alone", 10*time.Second, &r, frame)
	res.Log(t)
	if n := res.Count(); n > idleOtherFrames {
		t.Errorf("huddle window: %d frames in 10s idle, want at most %d", n, idleOtherFrames)
	}
}

// idleOtherFrames is internal/ui's: a clock tick and room for two more.
const idleOtherFrames = 3
