package ui

// Agent session requests in a channel (fakeslack Size.AgentSessionRequests,
// the shapes of the message Karim saw drawn wrong, 2026-11-20): an
// agent_channel_unfurl draws the code channel's card (CodeChannelEntityCard,
// surface MessageUnfurl), never a legacy attachment of its fallback text,
// and a private code channel the viewer isn't in draws the joinable card,
// whose Join joins it from the message's channel and opens it.
// BK_AGENT_SHOTS_DIR keeps the window (bk-agent-shots). Synthetic only.

import (
	"image"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// agentSessEnv boots fakeslack with the requests (and the code channels),
// session_channels on, and #agent-requests open at its newest message.
func agentSessEnv(t *testing.T) (*fakeslack.Server, *perfEnv) {
	t.Helper()
	sz := fakeslack.Small
	sz.CodeChannels, sz.AgentSessionRequests = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.a.data.core.SetExperimentGroup("session_channels", "on")
	ch := fakeslack.AgentSessChannelID
	e.openChannel(ch)
	e.until("#agent-requests' history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(800 * time.Millisecond)
	e.toLatest()
	return fs, e
}

// agentSessRow is message ts's row in ch as drawn: the row once it is on
// screen, then (2 s at most) its element slot, which the frame after
// measures. A row without one is what the caller judges.
func (e *perfEnv) agentSessRow(ch, ts string) messages.SenderView {
	e.tb.Helper()
	v := e.ownSendList(ch)
	var row messages.SenderView
	find := func() bool {
		for _, sv := range v.Senders() {
			if sv.TS == ts {
				row = sv
				return true
			}
		}
		return false
	}
	e.until("the row of "+ts, 10*time.Second, find)
	for dl := time.Now().Add(2 * time.Second); len(row.Slots) == 0 && time.Now().Before(dl); {
		e.settle(50 * time.Millisecond)
		find()
	}
	return row
}

// agentSlot is message ts's element slot in ch on screen (window
// coordinates).
func (e *perfEnv) agentSlot(ch, ts string) image.Rectangle {
	e.tb.Helper()
	for _, s := range e.ownSendList(ch).ElementSlotRects() {
		if s.TS == ts {
			return s.R
		}
	}
	e.tb.Fatalf("no element slot of %s on screen", ts)
	return image.Rectangle{}
}

// joinPoint is a point inside the joinable card's Join, from its slot
// (the card and its 8px margin-bottom): the 28px button at the footer's
// right, inside the 1px border and the footer's padding 8px 12px, 8px in
// from its right edge.
func joinPoint(slot image.Rectangle, s float32) image.Point {
	dp := func(v float32) int { return int(math.Round(float64(v * s))) }
	b := max(1, int(s)) // border1
	bottom := slot.Max.Y - dp(8)
	return image.Pt(slot.Max.X-b-dp(12)-dp(8), bottom-b-dp(8)-dp(28)/2)
}

func TestAgentSessionRequestCard(t *testing.T) {
	_, e := agentSessEnv(t)
	req := e.agentSessRow(fakeslack.AgentSessChannelID, fakeslack.AgentSessRootTS)
	t.Logf("request: body %v slots %v text %q", req.Body, req.Slots, req.Text)
	if len(req.Slots) != 1 || req.Slots[0].Dy() < 60 {
		t.Errorf("the request's agent_channel_unfurl: slots %v, want the code channel's card", req.Slots)
	}
	if strings.Contains(req.Text, "Code channel") {
		t.Errorf("the request drew its attachment's fallback text: %q", req.Text)
	}
	e.appPlanShot(t, "agent-session-request")
}

// The private request's card is the joinable one (kR8w's eE branch): the
// lock tile, the channel's name, "Code channel", the agent's avatar and
// Join. A refused join toasts and leaves the viewer where they were; Join
// again (sent again, past the failure backoff) loads, joins the channel
// from #agent-requests (client.codeChannels.join) and opens it.
func TestAgentSessionJoinableCard(t *testing.T) {
	fs, e := agentSessEnv(t)
	ch, priv := fakeslack.AgentSessChannelID, fakeslack.AgentSessPrivateID
	if cv, ok := e.a.data.core.Conv(priv); ok && cv.Member {
		t.Fatal("the viewer is in the private code channel before any join")
	}
	req := e.agentSessRow(ch, fakeslack.AgentSessPrivTS)
	t.Logf("private request: body %v slots %v text %q", req.Body, req.Slots, req.Text)
	s := e.pxPerDp()
	// the header (8 + 41 + 8) over the footer (1 + 8 + 28 + 8) in the 1px
	// border, and the 8px margin
	if len(req.Slots) != 1 || req.Slots[0].Dy() < int(100*s) {
		t.Fatalf("the private request's agent_channel_unfurl: slots %v, want the joinable card", req.Slots)
	}
	if strings.Contains(req.Text, "Code channel") {
		t.Errorf("the private request drew its attachment's fallback text: %q", req.Text)
	}
	drawn := e.drawnFrame()
	for _, want := range []string{fakeslack.AgentSessPrivName, "Code channel", "Join"} {
		if !slices.Contains(drawn, want) {
			t.Errorf("the joinable card didn't draw %q (labels %q)", want, drawn)
		}
	}
	e.appPlanShot(t, "agent-session-joinable")
	jp := joinPoint(e.agentSlot(ch, fakeslack.AgentSessPrivTS), s)

	// a refused join: Slack's toast, and the viewer stays
	fs.SetFault("client.codeChannels.join", fakeslack.Fault{Error: "not_allowed", Times: 1})
	e.click(jp)
	const failed = "Couldn’t join the channel. Please try again."
	e.until("the refused join's toast", 10*time.Second, func() bool { return e.a.toast.text == failed })
	if got := e.a.router.Current().Channel(); got != ch {
		t.Errorf("after a refused join the view is %q, want #agent-requests (%s)", got, ch)
	}
	if cv, ok := e.a.data.core.Conv(priv); ok && cv.Member {
		t.Error("a refused join left the viewer in the channel")
	}
	e.settle(300 * time.Millisecond)
	if !slices.Contains(e.drawnFrame(), "Join") {
		t.Error("after the refusal Join isn't back")
	}
	e.appPlanShot(t, "agent-session-join-refused")

	// Join again, the answer held: the button loads, then the channel opens
	fs.SetFault("client.codeChannels.join", fakeslack.Fault{Delay: 2 * time.Second, Times: 1})
	e.click(jp)
	e.settle(200 * time.Millisecond)
	if slices.Contains(e.drawnFrame(), "Join") {
		t.Error("Join drew its label while the join ran, want LoadingButton's spinner alone")
	}
	e.appPlanShot(t, "agent-session-joining")
	e.until("the joined channel open", 15*time.Second, func() bool { return e.a.router.Current().Channel() == priv })
	if cv, ok := e.a.data.core.Conv(priv); !ok || !cv.Member {
		t.Errorf("after the join the store has %+v (ok %v), want the viewer in it", cv, ok)
	}
	var joins []fakeslack.CodeCall
	for _, c := range fs.CodeCalls() {
		if c.Method == "client.codeChannels.join" {
			joins = append(joins, c)
		}
	}
	if len(joins) != 1 || joins[0].Args.Get("channel_id") != priv || joins[0].Args.Get("origin_channel_id") != ch {
		t.Errorf("client.codeChannels.join calls that took effect: %+v, want one {channel_id %s, origin_channel_id %s}", joins, priv, ch)
	}
	if n := fs.CallCount("client.codeChannels.join"); n != 2 {
		t.Errorf("client.codeChannels.join sent %d times, want 2 (the refused one, then Join again)", n)
	}
}
