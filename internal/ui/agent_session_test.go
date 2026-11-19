package ui

// An agent session request in a channel (fakeslack Size.AgentSessionRequests,
// the shapes of the message Karim saw drawn wrong, 2026-11-20): its
// agent_channel_unfurl draws the code channel's card (CodeChannelEntityCard,
// surface MessageUnfurl), never a legacy attachment of its fallback text.
// BK_AGENT_SHOTS_DIR keeps the window (bk-agent-shots). Synthetic only.

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

func TestAgentSessionRequestCard(t *testing.T) {
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
	v := e.ownSendList(ch)
	var req messages.SenderView
	e.until("the request's row", 10*time.Second, func() bool {
		for _, sv := range v.Senders() {
			if sv.TS == fakeslack.AgentSessRootTS {
				req = sv
				return len(sv.Slots) > 0
			}
		}
		return false
	})
	t.Logf("request: body %v slots %v text %q", req.Body, req.Slots, req.Text)
	if len(req.Slots) != 1 || req.Slots[0].Dy() < 60 {
		t.Errorf("the request's agent_channel_unfurl: slots %v, want the code channel's card", req.Slots)
	}
	if strings.Contains(req.Text, "Code channel") {
		t.Errorf("the request drew its attachment's fallback text: %q", req.Text)
	}
	e.appPlanShot(t, "agent-session-request")
}
