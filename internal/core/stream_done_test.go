package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The message_changed that ends a chat stream is marked StreamDone, with
// what a message event carries for its notification, even when the
// stream's thread isn't cached (fakeslack's AgentStream replies in a
// thread Clack never opened); the in-progress updates aren't.
func TestStreamDoneEvent(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	defer a.Shutdown()
	evs, stop := a.Subscribe()
	defer stop()
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	const ts = "1762520099.000200"
	go fs.PlayAgentStream(ts, 20*time.Millisecond)
	var got []string
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.TS != ts || ev.Msg == nil || (ev.Type != "message" && ev.Type != "update") {
				continue
			}
			s := ev.Type + ":" + ev.Msg.StreamingState
			if ev.StreamDone {
				s += ":done"
				if ev.UserID != fakeslack.AgentUserID || !ev.Reply || ev.ConvKind != a.kindOf(ev.Channel) {
					t.Errorf("done event %+v", ev)
				}
			}
			got = append(got, s)
			if ev.StreamDone {
				want := []string{"message:in_progress", "update:in_progress", "update:in_progress", "update:completed:done"}
				if len(got) != len(want) {
					t.Fatalf("events %v, want %v", got, want)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("events %v, want %v", got, want)
					}
				}
				return
			}
		case <-deadline:
			t.Fatalf("timed out: %v", got)
		}
	}
}

// openStreams forgets settled streams and stays bounded.
func TestOpenStreams(t *testing.T) {
	var o openStreams
	if o.seen("C1", "1", true) {
		t.Error("new stream reported open before")
	}
	if !o.seen("C1", "1", true) || !o.seen("C1", "1", false) || o.seen("C1", "1", false) {
		t.Error("open → settled")
	}
	for i := 0; i < maxOpenStreams+10; i++ {
		o.seen("C2", time.Unix(int64(i), 0).String(), true)
	}
	if len(o.open) != maxOpenStreams || len(o.q) != maxOpenStreams {
		t.Errorf("open %d q %d", len(o.open), len(o.q))
	}
}

// file_deleted / file_undeleted reach subscribers as file_trash events
// with the raw event (the Files view's deleted list follows them).
func TestFileTrashEvents(t *testing.T) {
	a := New(slack.New(slack.Creds{}), store.New(""))
	defer a.Shutdown()
	evs, stop := a.Subscribe()
	defer stop()
	a.handle(rtm("file_deleted", map[string]any{"file_id": "F0DEL1", "event_ts": "1.2"}))
	ev := next(t, evs, "file_trash")
	if ev.Name != "file_deleted" || !strings.Contains(string(ev.Raw), "F0DEL1") {
		t.Errorf("deleted: %+v", ev)
	}
	a.handle(rtm("file_undeleted", map[string]any{"file_ids": []string{"F0DEL1", "F0DEL2"}}))
	if ev := next(t, evs, "file_trash"); ev.Name != "file_undeleted" || !strings.Contains(string(ev.Raw), "F0DEL2") {
		t.Errorf("undeleted: %+v", ev)
	}
}
