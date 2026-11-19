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

// noteToast injects a socket message into ch from user and hands core's
// announcement to the backend, users/info answering after slow; it returns
// the toast the backend showed.
func noteToast(t *testing.T, a *App, ca *core.App, fs *fakeslack.Server, rec *noteRec, ch, user, ts, text string, slow time.Duration) *desktopNote {
	t.Helper()
	fs.SetEdgeHook(func(method string, _ map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			time.Sleep(slow)
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetEdgeHook(nil) })
	evs, stop := ca.Subscribe()
	defer stop()
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ch, "user": user, "ts": ts, "text": text})
	cj, _ := json.Marshal(ch)
	ca.Inject(slack.Event{Type: "message", Channel: cj, Raw: raw})
	dl := time.After(10 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "message" && ev.Channel == ch && ev.TS == ts {
				a.data.handle(a, ev)
				return rec.wait(t, a, 1)
			}
		case <-dl:
			t.Fatal("core never announced the message")
		}
	}
}

// groupDMFixture is the thread-header fixture's group DM, stored in core
// with its history never loaded, and its first member besides the viewer.
func groupDMFixture(t *testing.T, ca *core.App) (slack.Conversation, string) {
	t.Helper()
	conv := fixtureConv(t, ca, fakeslack.ThreadGroupDMID)
	ca.S.PutConv(conv)
	for _, id := range conv.Members {
		if id != fakeslack.SelfID {
			return conv, id
		}
	}
	t.Fatal("the group DM has no member besides the viewer")
	return conv, ""
}

// strangers are n of the fake's people outside conv whom the store
// doesn't hold.
func strangers(t *testing.T, ca *core.App, fs *fakeslack.Server, conv slack.Conversation, n int) []string {
	t.Helper()
	in := map[string]bool{fakeslack.SelfID: true}
	for _, m := range conv.Members {
		in[m] = true
	}
	var out []string
	for _, id := range fs.UserIDs() {
		if _, held := ca.S.RawUser(id); !in[id] && !held && len(out) < n {
			out = append(out, id)
		}
	}
	if len(out) < n {
		t.Fatalf("%d people to mention, want %d", len(out), n)
	}
	return out
}

// A group DM the sidebar already lists, named from the members the store
// held at its snapshot: the toast, which waited for every member, titles
// it with all of them (the conversation as core has it after the wait),
// not the snapshot's name.
func TestNotifyNamesTheGroupDMTheSidebarHalfNamed(t *testing.T) {
	a, ca, fs, rec := privNoteAppFS(t)
	conv, sender := groupDMFixture(t, ca)
	g := conv.ID
	// named at the snapshot from the one member the store held then
	a.data.m.Convs[g] = &convInfo{ID: g, Name: "alex", Kind: "mpim", Members: conv.Members}
	n := noteToast(t, a, ca, fs, rec, g, sender, "1762520600.000100", "a synthetic hello", time.Second)
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
	// and its title asked for nobody: the toast waited for the members
	// itself (an ask here was a second users/info, and its "names" a pass
	// over everything on screen)
	_, asked, _ := ca.S.TakeMissing()
	for _, id := range asked {
		for _, m := range conv.Members {
			if id == m {
				t.Errorf("the toast's title asked for the group's member %s", id)
			}
		}
	}
}

// Ten people the store didn't hold, mentioned at once: the toast waits for
// all of them (one users/info batch) and names every one.
func TestNotifyNamesTenMentions(t *testing.T) {
	a, ca, fs, rec := privNoteAppFS(t)
	conv, sender := groupDMFixture(t, ca)
	who := strangers(t, ca, fs, conv, 10)
	text := ""
	for _, id := range who {
		text += "<@" + id + "> "
	}
	n := noteToast(t, a, ca, fs, rec, conv.ID, sender, "1762520700.000100", text+"standup in 5", time.Second)
	for _, id := range who {
		name, ok := ca.S.MemberName(id)
		if !ok || name == "" {
			t.Errorf("%s wasn't fetched for the toast", id)
			continue
		}
		if !strings.Contains(n.Body, "@"+name) || strings.Contains(n.Body, id) {
			t.Errorf("the toast's body %q doesn't name %s as %q", n.Body, id, "@"+name)
		}
	}
}

// Members still unknown after the wait (users/info has no such member):
// a bare mention reads as nothing, never the id; a labelled one keeps its
// label, as the conversion wrote it.
func TestNotifyNamesMembersStillUnknown(t *testing.T) {
	a, ca, fs, rec := privNoteAppFS(t)
	conv, sender := groupDMFixture(t, ca)
	const bare, labelled = "U0NOBODY097", "U0NOBODY098"
	n := noteToast(t, a, ca, fs, rec, conv.ID, sender, "1762520800.000100",
		"<@"+bare+"> and <@"+labelled+"|dana> please review", 200*time.Millisecond)
	for _, id := range []string{bare, labelled} {
		if strings.Contains(n.Body, id) {
			t.Errorf("the toast's body %q carries the id %s", n.Body, id)
		}
	}
	if !strings.Contains(n.Body, "@dana please review") {
		t.Errorf("the toast's body %q lost the labelled mention's label (want \"@dana please review\")", n.Body)
	}
}

// A 1:1 DM whose partner the store doesn't hold: the toast waits for them
// and is titled with their name.
func TestNotifyNamesTheDMPartner(t *testing.T) {
	a, ca, fs, rec := privNoteAppFS(t)
	dm := fixtureConv(t, ca, fakeslack.ThreadDMID)
	ca.S.PutConv(dm)
	if _, held := ca.S.RawUser(dm.User); held {
		t.Fatal("the store holds the DM's partner already")
	}
	n := noteToast(t, a, ca, fs, rec, dm.ID, dm.User, "1762520900.000100", "a synthetic question", time.Second)
	u, ok := ca.RawUser(dm.User)
	if !ok || u.Name == "" {
		t.Fatalf("the DM's partner wasn't fetched for the toast %q", n.Title)
	}
	if !strings.Contains(n.Title, u.Name) || strings.Contains(n.Title, dm.User) {
		t.Errorf("the toast's title %q doesn't name the DM's partner %q", n.Title, u.Name)
	}
}
