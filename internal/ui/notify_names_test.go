package ui

// A toast names the members its message mentions, those the store didn't
// hold when the message came too (α.110's review: "@U07DANA can you
// join?"). Core announces a socket message before fetching anyone it
// names (onMessage; Slack's messageReceived stores it at once), so its
// conversion writes a mention of a member the store doesn't hold as the
// id. The toast waits for the people it names (noteMembers, noteResolve:
// showNotification's ensureModelsArePresent) and must name them then,
// also where the store keeps no copy of the message to convert again: a
// group DM whose history it never loaded. Synthetic data only.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

func TestNotifyNamesAMentionCoreDidNotHold(t *testing.T) {
	a, ca, fs, rec := privNoteAppFS(t)
	g := fakeslack.ThreadGroupDMID
	conv := fixtureConv(t, ca, g)
	ca.S.PutConv(conv) // the group DM, its history never loaded
	var sender string
	for _, id := range conv.Members {
		if id != fakeslack.SelfID {
			sender = id
			break
		}
	}
	// someone outside the group the store doesn't hold
	var mentioned string
	for _, id := range fs.UserIDs() {
		in := false
		for _, m := range conv.Members {
			in = in || m == id
		}
		if _, held := ca.S.RawUser(id); !in && !held && id != fakeslack.SelfID {
			mentioned = id
			break
		}
	}
	if sender == "" || mentioned == "" {
		t.Fatalf("the fixture has no sender (%q) or no member to mention (%q)", sender, mentioned)
	}
	// users/info answers after a second, as on a busy Grid
	fs.SetEdgeHook(func(method string, _ map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			time.Sleep(time.Second)
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetEdgeHook(nil) })

	evs, stop := ca.Subscribe()
	defer stop()
	const ts = "1762520400.000100"
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": g, "user": sender, "ts": ts,
		"text": "<@" + mentioned + "> can you join?"})
	cj, _ := json.Marshal(g)
	ca.Inject(slack.Event{Type: "message", Channel: cj, Raw: raw})
	var ev core.Event
	dl := time.After(10 * time.Second)
	for ev.Msg == nil {
		select {
		case e := <-evs:
			if e.Type == "message" && e.Channel == g && e.TS == ts {
				ev = e
			}
		case <-dl:
			t.Fatal("core never announced the message")
		}
	}
	a.data.handle(a, ev)
	n := rec.wait(t, a, 1)
	name, ok := ca.S.MemberName(mentioned)
	if !ok || name == "" {
		t.Fatal("users/info never brought the mentioned member")
	}
	want := "@" + name
	if strings.Contains(n.Body, mentioned) || !strings.Contains(n.Body, want) {
		t.Errorf("the toast's body is %q: want the mention named %q, never the id %s", n.Body, want, mentioned)
	}
	for _, id := range []string{mentioned, sender} {
		if strings.Contains(n.Title, id) {
			t.Errorf("the toast's title %q carries the id %s", n.Title, id)
		}
	}
	// the title names the group's members, whom the toast waited for too
	// (ensureModelsArePresent adds a group DM's members)
	for _, id := range conv.Members {
		if id == fakeslack.SelfID {
			continue
		}
		u, ok := ca.RawUser(id)
		if !ok || u.Name == "" {
			t.Errorf("the group's member %s wasn't fetched for the toast's title %q", id, n.Title)
			continue
		}
		if !strings.Contains(n.Title, u.Name) {
			t.Errorf("the toast's title %q doesn't name the group's member %q", n.Title, u.Name)
		}
	}
}

// A toast whose message was deleted while it waited for the people it
// names isn't shown: core knows the message is gone (its change log), and
// clicking the toast would open nothing.
func TestNotifyDropsAMessageDeletedWhileItWaits(t *testing.T) {
	a, ca, fs, rec := privNoteAppFS(t)
	g := fakeslack.ThreadGroupDMID
	conv := fixtureConv(t, ca, g)
	ca.S.PutConv(conv)
	var sender string
	for _, id := range conv.Members {
		if id != fakeslack.SelfID {
			sender = id
			break
		}
	}
	fs.SetEdgeHook(func(method string, _ map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			time.Sleep(time.Second)
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetEdgeHook(nil) })
	evs, stop := ca.Subscribe()
	defer stop()
	const ts = "1762520500.000100"
	cj, _ := json.Marshal(g)
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": g, "user": sender, "ts": ts, "text": "a synthetic ping"})
	ca.Inject(slack.Event{Type: "message", Channel: cj, Raw: raw})
	var ev core.Event
	dl := time.After(10 * time.Second)
	for ev.Msg == nil {
		select {
		case e := <-evs:
			if e.Type == "message" && e.Channel == g && e.TS == ts {
				ev = e
			}
		case <-dl:
			t.Fatal("core never announced the message")
		}
	}
	a.data.handle(a, ev) // waits for its sender, whom the store doesn't hold
	now := fmt.Sprintf("%d.000200", time.Now().Unix())
	del, _ := json.Marshal(map[string]any{"type": "message", "subtype": "message_deleted", "channel": g, "hidden": true,
		"deleted_ts": ts, "ts": now, "event_ts": now,
		"previous_message": map[string]any{"type": "message", "user": sender, "ts": ts, "text": "a synthetic ping"}})
	ca.Inject(slack.Event{Type: "message", Subtype: "message_deleted", Channel: cj, Raw: del})
	quiet(a, rec) // past the second users/info takes
	if n := quiet(a, rec); n != 0 {
		t.Errorf("%d toasts for a message deleted while its toast waited for its sender", n)
	}
}
