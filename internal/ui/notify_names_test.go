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
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
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
	noteSend(t, a, ca, fs, ch, user, ts, text, slow)
	return rec.wait(t, a, 1)
}

// noteSend is noteToast up to the backend taking core's announcement.
func noteSend(t *testing.T, a *App, ca *core.App, fs *fakeslack.Server, ch, user, ts, text string, slow time.Duration) {
	t.Helper()
	noteSendHook(t, a, ca, fs, ch, user, ts, text, func(method string, _ map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			time.Sleep(slow)
		}
		return nil, false
	})
}

// usersInfoAs answers edge/users/info with us after slow, every other id
// asked for as failed (users/info has no such member).
func usersInfoAs(us map[string]slack.User, slow time.Duration) func(string, map[string]json.RawMessage) (any, bool) {
	return func(method string, body map[string]json.RawMessage) (any, bool) {
		if method != "edge/users/info" {
			return nil, false
		}
		time.Sleep(slow)
		var asked map[string]int
		_ = json.Unmarshal(body["updated_ids"], &asked)
		res, failed := []slack.User{}, []string{}
		for id := range asked {
			if u, ok := us[id]; ok {
				res = append(res, u)
			} else {
				failed = append(failed, id)
			}
		}
		return map[string]any{"ok": true, "results": res, "failed_ids": failed}, true
	}
}

// noteSendHook is noteSend with hook answering the fake's edge calls.
func noteSendHook(t *testing.T, a *App, ca *core.App, fs *fakeslack.Server, ch, user, ts, text string, hook func(string, map[string]json.RawMessage) (any, bool)) {
	t.Helper()
	noteSendRaw(t, a, ca, fs, map[string]any{"type": "message", "channel": ch, "user": user, "ts": ts, "text": text}, hook)
}

// noteSendRaw is noteSendHook for the socket message msg as given (its
// channel and ts in it).
func noteSendRaw(t *testing.T, a *App, ca *core.App, fs *fakeslack.Server, msg map[string]any, hook func(string, map[string]json.RawMessage) (any, bool)) {
	t.Helper()
	fs.SetEdgeHook(hook)
	t.Cleanup(func() { fs.SetEdgeHook(nil) })
	evs, stop := ca.Subscribe()
	defer stop()
	ch, _ := msg["channel"].(string)
	ts, _ := msg["ts"].(string)
	sub, _ := msg["subtype"].(string)
	raw, _ := json.Marshal(msg)
	cj, _ := json.Marshal(ch)
	ca.Inject(slack.Event{Type: "message", Subtype: sub, Channel: cj, Raw: raw})
	dl := time.After(10 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "message" && ev.Channel == ch && ev.TS == ts {
				a.data.handle(a, ev)
				return
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

// A typed message mentions people as rich_text user elements, and its
// toast's body comes from its blocks (richtext.NotificationMessageText). A
// member the store didn't hold is named once the toast's wait brought
// them, after one users/info ask. One users/info answers too late for
// (past noteResolveWait) reads as nothing, and the body's conversion
// doesn't ask for them again: getContentForNotification runs after
// ensureModelsArePresent and fetches no one.
func TestNotifyNamesABlockMention(t *testing.T) {
	for _, slow := range []bool{false, true} {
		a, ca, fs, rec := privNoteAppFS(t)
		conv, sender := groupDMFixture(t, ca)
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
		if mentioned == "" {
			t.Fatal("the fixture has no member to mention")
		}
		var mu sync.Mutex
		asks := 0
		fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
			if method == "edge/users/info" {
				var up map[string]int
				_ = json.Unmarshal(body["updated_ids"], &up)
				if _, ok := up[mentioned]; ok {
					mu.Lock()
					asks++
					mu.Unlock()
					if slow {
						time.Sleep(noteResolveWait + time.Second)
					}
				}
			}
			return nil, false
		})
		t.Cleanup(func() { fs.SetEdgeHook(nil) })
		evs, stop := ca.Subscribe()
		ts := "1762521000.000101"
		if slow {
			ts = "1762521000.000102"
		}
		raw, _ := json.Marshal(map[string]any{"type": "message", "channel": conv.ID, "user": sender, "ts": ts,
			"text": "ping <@" + mentioned + "> please",
			"blocks": []any{map[string]any{"type": "rich_text", "block_id": "b1", "elements": []any{map[string]any{"type": "rich_text_section",
				"elements": []any{map[string]any{"type": "text", "text": "ping "}, map[string]any{"type": "user", "user_id": mentioned},
					map[string]any{"type": "text", "text": " please"}}}}}}})
		cj, _ := json.Marshal(conv.ID)
		ca.Inject(slack.Event{Type: "message", Channel: cj, Raw: raw})
		var n *desktopNote
		for dl := time.After(10 * time.Second); n == nil; {
			select {
			case ev := <-evs:
				if ev.Type == "message" && ev.Channel == conv.ID && ev.TS == ts {
					a.data.handle(a, ev)
					n = rec.wait(t, a, 1)
				}
			case <-dl:
				t.Fatal("core never announced the message")
			}
		}
		stop()
		time.Sleep(1500 * time.Millisecond) // time for an ask the conversion might start
		mu.Lock()
		got := asks
		mu.Unlock()
		if strings.Contains(n.Body, mentioned) {
			t.Errorf("slow %v: the toast's body %q carries the id %s", slow, n.Body, mentioned)
		}
		if !slow {
			name, ok := ca.S.MemberName(mentioned)
			if !ok || !strings.Contains(n.Body, "@"+name) {
				t.Errorf("the toast's body %q doesn't name the mention (@%s)", n.Body, name)
			}
		}
		if got != 1 {
			t.Errorf("slow %v: users/info asked for the mentioned member %d times, want once (the toast's wait)", slow, got)
		}
	}
}

// A member the viewer hid (hidden_users) reads "Hidden user" in a toast's
// body, with no @ and never their name or id, as Slack's userToText
// writes them.
func TestNotifyNamesAHiddenMember(t *testing.T) {
	a, ca, fs, rec := privNoteAppFS(t)
	conv, sender := groupDMFixture(t, ca)
	hidden := strangers(t, ca, fs, conv, 1)[0]
	fs.SetPrefs(map[string]any{"mute_sounds": true, "hidden_users": hidden})
	if _, err := ca.RefreshPrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := noteToast(t, a, ca, fs, rec, conv.ID, sender, "1762521000.000100", "<@"+hidden+"> joined the synthetic standup", 200*time.Millisecond)
	if !strings.HasSuffix(n.Body, ": Hidden user joined the synthetic standup") || strings.Contains(n.Body, "Name hidden") || strings.Contains(n.Body, hidden) {
		t.Errorf("the toast's body %q: want the hidden member as \"Hidden user\", no @, no id", n.Body)
	}
}

// A message from a member the viewer hid (hidden_users) shows no toast, in
// a group DM or a DM, as Slack's maybeShowNotification gives up for a
// hidden sender (isUserHidden, unless the org locked hide_person_opt_out):
// Clack toasted their name and their text.
func TestNotifyNamesNoToastFromAHiddenSender(t *testing.T) {
	for _, dm := range []bool{false, true} {
		name := map[bool]string{false: "group DM", true: "DM"}[dm]
		t.Run(name, func(t *testing.T) {
			a, ca, fs, rec := privNoteAppFS(t)
			var ch, sender string
			if dm {
				d := fixtureConv(t, ca, fakeslack.ThreadDMID)
				ca.S.PutConv(d)
				ch, sender = d.ID, d.User
			} else {
				conv, s := groupDMFixture(t, ca)
				ch, sender = conv.ID, s
			}
			fs.SetPrefs(map[string]any{"mute_sounds": true, "hidden_users": sender})
			if _, err := ca.RefreshPrefs(context.Background()); err != nil {
				t.Fatal(err)
			}
			noteSend(t, a, ca, fs, ch, sender, "1762521100.000100", "a synthetic message from someone hidden", 200*time.Millisecond)
			quiet(a, rec) // past users/info
			if n := quiet(a, rec); n != 0 {
				t.Errorf("%s: %d toasts for a message from a member the viewer hid", name, n)
			}
		})
	}
}

// namedUser is a synthetic member with a display name and a real name.
func namedUser(id, handle, display, real string) slack.User {
	return slack.User{ID: id, Name: handle, RealName: real, Profile: slack.Profile{DisplayName: display, RealName: real}}
}

// A group DM's toast is titled as getMpimDisplayNameAlphabetical names it:
// its members' names sorted as Slack's sorter does (en-US, numeric), not
// in the API's order, under the viewer's display_real_names, and "Name
// hidden" for one the viewer hid. The body's "{sender}: " follows
// display_real_names too (getPrefCompliantMemberNameById); a mention
// doesn't (userToText's getMemberMentionName: the display name) unless
// the viewer's mentions show real names (shouldDisplayRealNamesInMentions:
// the mentions_full_names_only_pref experiment, then
// mentions_real_names_override, then the team's
// display_real_names_in_mentions). Clack titled it in the API's order
// with display names and the hidden member's own name (the reviews).
func TestNotifyNamesTheGroupDMAsSlackDoes(t *testing.T) {
	const plain, real = "New message in adam, Name hidden, zoe", "New message in Adam Apple, Name hidden, Zoe Zimmer"
	const noted, notedReal, mentionReal = "zoe: @adam took the synthetic notes", "Zoe Zimmer: @adam took the synthetic notes", "zoe: @Adam Apple took the synthetic notes"
	inMentions := map[string]json.RawMessage{"display_real_names_in_mentions": json.RawMessage("true")}
	for _, c := range []struct {
		name        string
		prefs       map[string]any
		team        map[string]json.RawMessage
		experiment  string // mentions_full_names_only_pref's group
		title, body string
	}{
		{"display names", nil, nil, "", plain, noted},
		{"display_real_names", map[string]any{"display_real_names": true}, nil, "", real, notedReal},
		{"display_real_names_override", map[string]any{"display_real_names_override": 1}, nil, "", real, notedReal},
		{"real names in mentions", map[string]any{"mentions_real_names_override": 1}, nil, "on", plain, mentionReal},
		{"the team's real names in mentions", nil, inMentions, "on", plain, mentionReal},
		{"display names in mentions over the team's", map[string]any{"mentions_real_names_override": -1}, inMentions, "on", plain, noted},
		{"real names in mentions, out of the experiment", map[string]any{"mentions_real_names_override": 1}, inMentions, "off", plain, noted},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, ca, fs, rec := privNoteAppFS(t)
			a.data.selfID = fakeslack.SelfID // a session's snapshot sets it
			conv, _ := groupDMFixture(t, ca)
			var others []string
			for _, id := range conv.Members {
				if id != fakeslack.SelfID {
					others = append(others, id)
				}
			}
			if len(others) < 3 {
				t.Fatalf("the group DM has %d members besides the viewer, want 3", len(others))
			}
			// in the API's order: zoe, the member the viewer hid, adam
			zoe, hidden, adam := others[0], others[1], others[2]
			us := map[string]slack.User{
				zoe:    namedUser(zoe, "zoe.z", "zoe", "Zoe Zimmer"),
				hidden: namedUser(hidden, "mia.m", "mia", "Mia Moss"),
				adam:   namedUser(adam, "adam.a", "adam", "Adam Apple"),
			}
			prefs := map[string]any{"hidden_users": hidden}
			for k, v := range c.prefs {
				prefs[k] = v
			}
			fs.SetPrefs(prefs)
			if _, err := ca.RefreshPrefs(context.Background()); err != nil {
				t.Fatal(err)
			}
			if c.team != nil {
				ca.SetTeamPrefs(c.team)
			}
			ca.SetExperimentGroup("mentions_full_names_only_pref", c.experiment)
			noteSendHook(t, a, ca, fs, conv.ID, zoe, "1762521200.000100", "<@"+adam+"> took the synthetic notes", usersInfoAs(us, 200*time.Millisecond))
			n := rec.wait(t, a, 1)
			if n.Title != c.title || n.Body != c.body {
				t.Errorf("notified %q / %q, Slack %q / %q", n.Title, n.Body, c.title, c.body)
			}
		})
	}
}

// A group DM whose members users/info never brought (none held after the
// wait) is titled as Slack titles it: getMpimDisplayNameAlphabetical
// leaves out every member it can't name, and getNotificationTitle reads
// "New message in " with nothing after it. Never the sidebar's name for
// it, the handles in its mpdm slug or formatChannelName's
// "unknown-channel" (none of them Slack's), and no conversations.info for
// it: its name is its members', the people the toast waited for. Clack
// titled it from the sidebar's row or the slug (the reviews).
func TestNotifyNamesTheGroupDMWhoseMembersNeverCame(t *testing.T) {
	for _, named := range []bool{true, false} {
		t.Run(map[bool]string{true: "the sidebar named it", false: "the sidebar has its slug"}[named], func(t *testing.T) {
			a, ca, fs, rec := privNoteAppFS(t)
			a.data.selfID = fakeslack.SelfID
			conv, sender := groupDMFixture(t, ca)
			row := "alex"
			if !named {
				row = strings.TrimPrefix(conv.Name, "mpdm-")
			}
			a.data.m.Convs[conv.ID] = &convInfo{ID: conv.ID, Name: row, Kind: "mpim", Members: conv.Members}
			var mu sync.Mutex
			lookups := 0
			fs.SetHook(func(method string, form url.Values) (any, bool) {
				if method == "conversations.info" && form.Get("channel") == conv.ID {
					mu.Lock()
					lookups++
					mu.Unlock()
				}
				return nil, false
			})
			t.Cleanup(func() { fs.SetHook(nil) })
			noteSendHook(t, a, ca, fs, conv.ID, sender, "1762521300.000100", "a synthetic hello", usersInfoAs(nil, 0))
			n := rec.wait(t, a, 1)
			if n.Title != "New message in " {
				t.Errorf("titled %q, Slack \"New message in \" (no member it can name)", n.Title)
			}
			mu.Lock()
			defer mu.Unlock()
			if lookups > 0 {
				t.Errorf("%d conversations.info lookups of the group DM for its title: its name is its members'", lookups)
			}
		})
	}
}

// noteMembers puts the people a toast's title names, its sender and its
// group DM's members, ahead of the members its text mentions, so a
// message mentioning more people than one users/info batch
// (noteMembersMax) still waits for them; never the viewer.
func TestNotifyMembersPutsTheTitlesPeopleFirst(t *testing.T) {
	a, ca, _, _ := privNoteAppFS(t)
	a.data.selfID = fakeslack.SelfID
	conv, sender := groupDMFixture(t, ca)
	text := ""
	for i := 0; i < noteMembersMax+20; i++ {
		text += fmt.Sprintf("<@U0NOTE%05d> ", i)
	}
	m := ca.MessageFromSlack(conv.ID, &slack.Message{Type: "message", User: sender, TS: "1762521400.000100", Text: text + "all hands"})
	got := a.data.noteMembers(&m)
	if len(got) != noteMembersMax {
		t.Errorf("%d people to wait for, want one users/info batch of %d", len(got), noteMembersMax)
	}
	in := map[string]bool{}
	for _, id := range got {
		in[id] = true
	}
	for _, id := range conv.Members {
		switch {
		case id == fakeslack.SelfID && in[id]:
			t.Error("the toast waits for the viewer")
		case id != fakeslack.SelfID && !in[id]:
			t.Errorf("the group's member %s, whom the title names, was cut for the mentions", id)
		}
	}
}

// A DM's toast names its partner under the viewer's display_real_names
// (getPrefCompliantMemberNameById), whether the sidebar lists the DM (its
// row has their display name: a followed thread's reply is titled from
// it) or only core knows it; and a socket message in it is titled "New
// message from" that name.
func TestNotifyNamesTheDMPartnerUnderThePref(t *testing.T) {
	a, ca, fs, rec := privNoteAppFS(t)
	a.data.selfID = fakeslack.SelfID
	dm := fixtureConv(t, ca, fakeslack.ThreadDMID)
	ca.S.PutConv(dm)
	ca.S.PutUsers([]slack.User{namedUser(dm.User, "zoe.z", "zoe", "Zoe Zimmer")})
	for i, real := range []bool{false, true} {
		fs.SetPrefs(map[string]any{"display_real_names": real})
		if _, err := ca.RefreshPrefs(context.Background()); err != nil {
			t.Fatal(err)
		}
		want := map[bool]string{false: "zoe", true: "Zoe Zimmer"}[real]
		a.data.m.Convs[dm.ID] = &convInfo{ID: dm.ID, Name: "zoe", Kind: "im", User: dm.User}
		if c, named := a.data.noteConv(dm.ID); c == nil || !named || c.Name != want {
			t.Errorf("display_real_names %v, the sidebar's row: named %+v, want %q", real, c, want)
		}
		delete(a.data.m.Convs, dm.ID)
		if c, named := a.data.noteConv(dm.ID); c == nil || !named || c.Name != want {
			t.Errorf("display_real_names %v, core's DM: named %+v, want %q", real, c, want)
		}
		noteSend(t, a, ca, fs, dm.ID, dm.User, fmt.Sprintf("1762521500.00010%d", i), "a synthetic question", 0)
		n := rec.wait(t, a, i+1)
		if n.Title != "New message from "+want || n.Body != "a synthetic question" {
			t.Errorf("display_real_names %v: notified %q / %q, Slack %q / %q", real, n.Title, n.Body, "New message from "+want, "a synthetic question")
		}
	}
}

// A post with no user, a webhook's or a legacy bot's (bot_id and a
// username), that mentions the viewer toasts its text alone: Slack's
// getContentForNotification writes "{sender}: " from the message's user
// (getPrefCompliantMemberNameById), and there is none. Clack wrote the
// username as the sender, "deploy-bot: ..." (the reviews).
func TestNotifyNamesABotPostHasNoSenderPrefix(t *testing.T) {
	a, ca, fs, rec := privNoteAppFS(t)
	a.data.selfID = fakeslack.SelfID
	pub := fixtureConv(t, ca, fakeslack.ThreadPublicID)
	ca.S.PutConv(pub)
	a.data.m.Convs[pub.ID] = &convInfo{ID: pub.ID, Name: pub.Name, Kind: "channel"}
	noteSendRaw(t, a, ca, fs, map[string]any{"type": "message", "subtype": "bot_message", "channel": pub.ID, "ts": "1762521600.000100",
		"bot_id": "B0DEPLOY01", "username": "deploy-bot", "text": "<@" + fakeslack.SelfID + "> the synthetic deploy finished"},
		usersInfoAs(nil, 0))
	n := rec.wait(t, a, 1)
	if n.Title != "New message in #"+pub.Name {
		t.Errorf("titled %q, Slack \"New message in #%s\"", n.Title, pub.Name)
	}
	if strings.Contains(n.Body, "deploy-bot") || !strings.HasSuffix(n.Body, " the synthetic deploy finished") {
		t.Errorf("the toast's body %q: want the text alone, no \"deploy-bot: \"", n.Body)
	}
}

// A followed thread's reply (the server's desktop_notification) from a
// member the viewer hid shows nothing (shouldShowNotificationForDesktop
// Notification's 29: "user" in it and isUserHidden), after the reply's
// own checks, and an invite from one still shows: Slack returns 0 for
// is_channel_invite first. Clack checked the hidden sender first, so an
// invite from a hidden member never showed (the reviews).
func TestNotifyNamesNoThreadToastFromAHiddenSender(t *testing.T) {
	const priv, root = fakeslack.ThreadPrivateID, "1762520000.000100"
	for _, c := range []struct {
		name           string
		hidden, invite bool
		toasts         bool
	}{
		{"a reply from someone hidden", true, false, false},
		{"a reply from someone else", false, false, true},
		{"an invite from someone hidden", true, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, ca, fs, rec := privNoteAppFS(t)
			ca.S.PutConv(fixtureConv(t, ca, priv))
			const hidden, other = "U0FAKE00002", "U0FAKE00003"
			fs.SetPrefs(map[string]any{"mute_sounds": true, "hidden_users": hidden})
			if _, err := ca.RefreshPrefs(context.Background()); err != nil {
				t.Fatal(err)
			}
			from := other
			if c.hidden {
				from = hidden
			}
			ev := map[string]any{"type": "desktop_notification", "channel": priv, "ts": "1762521700.000100", "msg": "1762521700.000100",
				"thread_ts": root, "user": from, "content": "Robin Example: a synthetic reply", "title": "Thread in th-secret-plans"}
			if c.invite {
				ev = map[string]any{"type": "desktop_notification", "channel": priv, "is_channel_invite": true, "user": from,
					"content": "Robin Example invited you", "title": "th-secret-plans"}
			}
			raw, _ := json.Marshal(ev)
			ca.S.NoteAdded("1762521700.000100") // the reply's own message came first (the notifications' maxTs, reason 19)
			a.data.handle(a, core.Event{Type: "desktop_notification", Channel: priv, Raw: raw})
			if n := quiet(a, rec); (n > 0) != c.toasts {
				t.Errorf("%d toasts, want %v", n, c.toasts)
			}
		})
	}
}

// A toast's body names a mention the same whether it comes from the
// message's blocks (richtext.NotificationMessageText with noteNames: a rich_text
// user element, a Block Kit section's mrkdwn) or from its text: userToText
// (noteMentionName), the display name unless the viewer's mentions show
// real names (the team's display_real_names_in_mentions under
// mentions_full_names_only_pref), and "Hidden user", no @ and never their
// name or id, for a member the viewer hid (hidden_users). Synthetic
// people.
func TestNotifyNamesABlockMentionLikeItsText(t *testing.T) {
	inMentions := map[string]json.RawMessage{"display_real_names_in_mentions": json.RawMessage("true")}
	const tail = " took the synthetic notes"
	for _, c := range []struct {
		name       string
		team       map[string]json.RawMessage
		experiment string
		adam, body string // noteNames' name for adam, the toast's body
	}{
		{"display names", nil, "", "adam", "zoe: @adam and Hidden user" + tail},
		{"the team's real names in mentions", inMentions, "on", "Adam Apple", "zoe: @Adam Apple and Hidden user" + tail},
	} {
		for _, src := range []string{"rich_text", "section", "text"} {
			t.Run(c.name+"/"+src, func(t *testing.T) {
				a, ca, fs, rec := privNoteAppFS(t)
				a.data.selfID = fakeslack.SelfID // a session's snapshot sets it
				conv, _ := groupDMFixture(t, ca)
				var others []string
				for _, id := range conv.Members {
					if id != fakeslack.SelfID {
						others = append(others, id)
					}
				}
				if len(others) < 3 {
					t.Fatalf("the group DM has %d members besides the viewer, want 3", len(others))
				}
				zoe, hidden, adam := others[0], others[1], others[2]
				us := map[string]slack.User{
					zoe:    namedUser(zoe, "zoe.z", "zoe", "Zoe Zimmer"),
					hidden: namedUser(hidden, "mia.m", "mia", "Mia Moss"),
					adam:   namedUser(adam, "adam.a", "adam", "Adam Apple"),
				}
				fs.SetPrefs(map[string]any{"hidden_users": hidden})
				if _, err := ca.RefreshPrefs(context.Background()); err != nil {
					t.Fatal(err)
				}
				if c.team != nil {
					ca.SetTeamPrefs(c.team)
				}
				ca.SetExperimentGroup("mentions_full_names_only_pref", c.experiment)
				text := "<@" + adam + "> and <@" + hidden + ">" + tail
				msg := map[string]any{"type": "message", "channel": conv.ID, "user": zoe, "ts": "1762521300.000100", "text": text}
				switch src {
				case "rich_text":
					msg["blocks"] = []any{map[string]any{"type": "rich_text", "block_id": "b1", "elements": []any{map[string]any{
						"type": "rich_text_section", "elements": []any{
							map[string]any{"type": "user", "user_id": adam}, map[string]any{"type": "text", "text": " and "},
							map[string]any{"type": "user", "user_id": hidden}, map[string]any{"type": "text", "text": tail}}}}}}
				case "section":
					msg["blocks"] = []any{map[string]any{"type": "section", "block_id": "s1",
						"text": map[string]any{"type": "mrkdwn", "text": text}}}
				}
				noteSendRaw(t, a, ca, fs, msg, usersInfoAs(us, 200*time.Millisecond))
				n := rec.wait(t, a, 1)
				if n.Body != c.body || strings.Contains(n.Body, hidden) || strings.Contains(n.Body, "Mia") || strings.Contains(n.Body, "mia") {
					t.Errorf("the toast's body %q, Slack %q", n.Body, c.body)
				}
				r := noteNames{ca.S, a.data}
				if got := r.MentionName(adam); got != c.adam {
					t.Errorf("noteNames names adam %q, want %q (noteMentionName's)", got, c.adam)
				}
				if got := r.MentionName(hidden); got != "" {
					t.Errorf("noteNames names the hidden member %q, want nothing", got)
				}
			})
		}
	}
}
