package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// A socket message from someone core never fetched goes to the views at
// once (core onMessage asks nobody first: the rows ask for the people
// they draw), and its desktop notification waits for them, as Slack's
// showNotification awaits ensureModelsArePresent for the sender and the
// people the text mentions (at most 5 s): it names both, never the id or
// nobody (msg-latency).
func TestNotificationWaitsForNewPeople(t *testing.T) {
	a, ca, fs, rec := noteApp(t)
	fs.SetFault("edge/users/info", fakeslack.Fault{Delay: 300 * time.Millisecond})
	sender, mentioned := fakeslack.SynthMemberID(7), fakeslack.SynthMemberID(8)
	const ch, ts = "D0NOTE0007", "1762520199.000300"
	// in core's store as the socket stored it, converted before anyone
	// it names was fetched (the event's copy)
	m := slack.Message{Type: "message", TS: ts, User: sender, Text: "hi <@" + mentioned + ">, here"}
	ca.S.SetLatest(ch, []slack.Message{m}, false)
	msg := ca.MessageFromSlack(ch, &m)
	if msg.Name != "" {
		t.Fatalf("the sender is named %q before anyone fetched them", msg.Name)
	}
	a.data.handle(a, core.Event{Type: "message", Channel: ch, TS: ts, ConvKind: "im", Msg: &msg})
	n := rec.wait(t, a, 1)
	senderName := strings.ToLower(strings.TrimPrefix(sender, "U0")) // synthNNNNNN, the synthetic member's display name
	mentionName := strings.ToLower(strings.TrimPrefix(mentioned, "U0"))
	if n.Title != "New message from "+senderName {
		t.Errorf("the title is %q, want the sender named (%q)", n.Title, "New message from "+senderName)
	}
	if !strings.Contains(n.Body, mentionName) || strings.Contains(n.Body, mentioned) {
		t.Errorf("the body is %q, want the mention named %s (never its id)", n.Body, mentionName)
	}
}
