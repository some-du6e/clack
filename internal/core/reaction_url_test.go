package core

// A live reaction_added keeps the event's url: an emoji the workspace
// hasn't got, a Slack Connect partner's custom one, which the pill draws
// from it (boot-render REACTIONS-SHARED-RTM passes the event's url to
// boot-shared cBAo addReaction, which sets m&&(h.url=m)). And a malformed
// add, by nobody, changes nothing, as cBAo refuses an empty userId.
// Synthetic data only.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

const reactURLChan, reactURLTS = "C0FAKE00001", "1762701000.000100"

func reactURLApp(t *testing.T) *App {
	t.Helper()
	a, _ := actFake(t)
	a.S.SetLatest(reactURLChan, []slack.Message{{Type: "message", TS: reactURLTS, User: "U0FAKE00002", Text: "a synthetic message"}}, false)
	return a
}

func reactionAdded(user, name, url string) slack.Event {
	ev := map[string]any{"type": "reaction_added", "user": user, "reaction": name, "event_ts": "1762701001.000100",
		"item": map[string]any{"type": "message", "channel": reactURLChan, "ts": reactURLTS}}
	if url != "" {
		ev["url"] = url
	}
	raw, _ := json.Marshal(ev)
	return slack.Event{Type: "reaction_added", Raw: raw}
}

func TestReactionAddedKeepsItsURL(t *testing.T) {
	a := reactURLApp(t)
	evs, stop := a.Subscribe()
	defer stop()
	const url = "https://emoji.slack-edge.com/T0PARTNER/partner-wave/0123abcd.png"
	a.handle(reactionAdded("U0PARTNER1", "partner-wave", url))
	m, ok := a.S.Find(reactURLChan, reactURLTS)
	if !ok || len(m.Reactions) != 1 || m.Reactions[0].URL != url {
		t.Fatalf("the stored reactions %+v, want partner-wave with the event's url", m.Reactions)
	}
	for dl := time.After(5 * time.Second); ; {
		select {
		case ev := <-evs:
			if ev.Type != "update" || ev.Msg == nil || ev.TS != reactURLTS {
				continue
			}
			if len(ev.Msg.Reactions) != 1 || ev.Msg.Reactions[0].URL != url {
				t.Fatalf("the update the rows draw has %+v, want the event's url", ev.Msg.Reactions)
			}
			// someone else's, with none: the url stays
			a.handle(reactionAdded("U0PARTNER2", "partner-wave", ""))
			m, _ = a.S.Find(reactURLChan, reactURLTS)
			if len(m.Reactions) != 1 || m.Reactions[0].Count != 2 || m.Reactions[0].URL != url {
				t.Errorf("after a second person's: %+v", m.Reactions)
			}
			return
		case <-dl:
			t.Fatal("no update for the reaction")
		}
	}
}

func TestMalformedReactionChangesNothing(t *testing.T) {
	a := reactURLApp(t)
	a.handle(reactionAdded("", "tada", ""))
	a.handle(reactionAdded("U0FAKE00003", "", ""))
	m, _ := a.S.Find(reactURLChan, reactURLTS)
	if len(m.Reactions) != 0 {
		t.Errorf("an add by nobody, or of no reaction, made %+v", m.Reactions)
	}
}
