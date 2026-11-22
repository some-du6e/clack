package core

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// refsMsg is Ana's message: a file Cara owns, a slug to a canvas tab of
// #ops that its canvas file titles, a slug to a message in Self's DM with
// Bob ("… with bob"), a channel mention of #ops and a mention of Dan.
func refsMsg(a *App) slack.Message {
	a.S.PutUsers([]slack.User{
		{ID: "U0CARA001", Name: "cara", Profile: slack.Profile{DisplayName: "cara"}},
		{ID: "U0BOB0001", Name: "bob", Profile: slack.Profile{DisplayName: "bob"}},
		{ID: "U0DAN0001", Name: "dan", Profile: slack.Profile{DisplayName: "dan"}},
	})
	props, _ := json.Marshal(map[string]any{"tabz": []any{map[string]any{"id": "Ct0OPS001", "type": "canvas", "data": map[string]any{"file_id": "F0RUNBOOK"}}}})
	a.S.PutConv(slack.Conversation{ID: "C0OPS0001", Name: "ops", IsChannel: true, IsMember: true, Properties: props})
	a.S.PutConv(slack.Conversation{ID: "D0BOB0001", IsIM: true, User: "U0BOB0001", IsMember: true})
	a.S.PutCanvasRef("F0RUNBOOK", richtext.CanvasRef{Title: "Runbook", Status: "normal"})
	// the message the slug links to, held (fetched by id): its label reads
	// it, and the DM it's in
	a.S.PutFetched("D0BOB0001", []slack.Message{{Type: "message", TS: "1700000000.000050", User: "USELF", Text: "an earlier line"}}, 0)
	el := func(m map[string]any) map[string]any { return m }
	blocks, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "r", "elements": []any{map[string]any{"type": "rich_text_section",
		"elements": []any{
			el(map[string]any{"type": "text", "text": "see "}),
			el(map[string]any{"type": "channel", "channel_id": "C0OPS0001", "tab_id": "Ct0OPS001"}),
			el(map[string]any{"type": "message_mention", "message_ts": "1700000000.000050", "channel_id": "D0BOB0001", "author_id": "USELF",
				"text": "https://x.slack.com/archives/D0BOB0001/p1700000000000050", "url": "https://x.slack.com/archives/D0BOB0001/p1700000000000050"}),
			el(map[string]any{"type": "channel", "channel_id": "C0OPS0001"}),
			el(map[string]any{"type": "user", "user_id": "U0DAN0001"}),
		}}}}})
	return slack.Message{Type: "message", TS: "1700000000.000100", User: "U0ANA0001", Text: "see <#C0OPS0001>", Blocks: blocks,
		Files: []slack.File{{ID: "F0PLAN001", Name: "plan.pdf", Title: "Plan", Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF", Mode: "hosted", User: "U0CARA001"}}}
}

// A conversion records the members, conversations and files its Doc read
// (a file's owner, a DM's partner, a tab slug's channel and the canvas
// that titles it, a channel mention), not the sender or a user mention
// (rows read those as they draw).
func TestMessageRefs(t *testing.T) {
	a, _ := testApp(t)
	m := refsMsg(a)
	v := a.MessageFromSlack("C0GEN0001", &m)
	for _, id := range []string{"U0CARA001", "U0BOB0001"} {
		if !slices.Contains(v.Refs.Members, id) {
			t.Errorf("members read %v; want %s", v.Refs.Members, id)
		}
	}
	for _, id := range []string{"U0ANA0001", "U0DAN0001"} {
		if slices.Contains(v.Refs.Members, id) {
			t.Errorf("members read %v; %s is the sender or a mention, which rows read as they draw", v.Refs.Members, id)
		}
	}
	for _, id := range []string{"C0OPS0001", "D0BOB0001"} {
		if !slices.Contains(v.Refs.Channels, id) {
			t.Errorf("conversations read %v; want %s", v.Refs.Channels, id)
		}
	}
	if !slices.Contains(v.Refs.Files, "F0RUNBOOK") {
		t.Errorf("files read %v; want the canvas that titles the tab", v.Refs.Files)
	}
	if !slices.Contains(v.Refs.Messages, "D0BOB0001/1700000000.000050") || !v.Refs.Reads(RefChange{Messages: []string{"D0BOB0001/1700000000.000050"}}) {
		t.Errorf("messages read %v; want the one the slug links to", v.Refs.Messages)
	}
	if !v.Refs.Reads(RefChange{Members: []string{"U0CARA001"}}) || !v.Refs.Reads(RefChange{Channels: []string{"C0OPS0001"}}) ||
		!v.Refs.Reads(RefChange{Files: []string{"F0RUNBOOK"}}) || !v.Refs.Reads(RefChange{AnyMember: true}) ||
		v.Refs.Reads(RefChange{Members: []string{"U0DAN0001"}, Channels: []string{"C0GEN0001"}}) {
		t.Errorf("Reads disagrees with %+v", v.Refs)
	}

	for _, c := range []struct {
		ev   Event
		want RefChange
	}{
		{Event{Type: "user", UserID: "U0CARA001"}, RefChange{Members: []string{"U0CARA001"}}},
		{Event{Type: "user", User: &User{ID: "U0CARA001"}}, RefChange{Members: []string{"U0CARA001"}}},
		{Event{Type: "user", UserID: "U0CARA001", SameNames: true}, RefChange{}},
		{Event{Type: "conv", Channel: "C0OPS0001"}, RefChange{Channels: []string{"C0OPS0001"}}},
		{Event{Type: "conv", Conv: &Conv{ID: "C0OPS0001"}}, RefChange{Channels: []string{"C0OPS0001"}}},
		{Event{Type: "tabs", Channel: "C0OPS0001"}, RefChange{Channels: []string{"C0OPS0001"}}},
		{Event{Type: "tabs", Channel: "C0OPS0001", SameNames: true}, RefChange{}},
		{Event{Type: "names", Users: []string{"U0CARA001", "F0RUNBOOK"}}, RefChange{Members: []string{"U0CARA001", "F0RUNBOOK"},
			Channels: []string{"U0CARA001", "F0RUNBOOK"}, Files: []string{"U0CARA001", "F0RUNBOOK"}, Bots: []string{"U0CARA001", "F0RUNBOOK"},
			Messages: []string{"U0CARA001", "F0RUNBOOK"}}},
		// a message slug's message answered (msg_slug.go), and one deleted
		{Event{Type: "names", Users: []string{"D0BOB0001/1700000000.000050"}}, RefChange{Members: []string{"D0BOB0001/1700000000.000050"},
			Channels: []string{"D0BOB0001/1700000000.000050"}, Files: []string{"D0BOB0001/1700000000.000050"},
			Bots: []string{"D0BOB0001/1700000000.000050"}, Messages: []string{"D0BOB0001/1700000000.000050"}}},
		{Event{Type: "delete", Channel: "D0BOB0001", TS: "1700000000.000050"}, RefChange{Messages: []string{"D0BOB0001/1700000000.000050"}}},
		{Event{Type: "names"}, RefChange{All: true}}, // the viewer's user groups
		{Event{Type: "emoji_changed", Name: "partyparrot"}, RefChange{Emoji: []string{"partyparrot"}}},
		{Event{Type: "emoji_changed"}, RefChange{All: true}},
		// a removal names what it removed (Slack's names): the messages
		// that drew those, not every loaded one
		{Event{Type: "emoji_changed", Users: []string{"partyparrot", "blobdance"}}, RefChange{Emoji: []string{"partyparrot", "blobdance"}}},
		{Event{Type: "canvas", ID: "F0RUNBOOK"}, RefChange{Files: []string{"F0RUNBOOK"}}},
		{Event{Type: "list", ID: "F0TODO001"}, RefChange{Files: []string{"F0TODO001"}}},
		{Event{Type: "experiments"}, RefChange{All: true}},
		{Event{Type: "prefs", NamePrefs: true}, RefChange{AnyMember: true}},
		{Event{Type: "prefs", DocPrefs: true}, RefChange{All: true}},
		{Event{Type: "prefs", NamePrefs: true, DocPrefs: true}, RefChange{All: true}},
		{Event{Type: "prefs"}, RefChange{}},
		{Event{Type: "message", Channel: "C0OPS0001"}, RefChange{}},
		{Event{Type: "badge_counts_updated"}, RefChange{}},
	} {
		got := RefsChanged(c.ev)
		if !slices.Equal(got.Members, c.want.Members) || !slices.Equal(got.Channels, c.want.Channels) ||
			!slices.Equal(got.Files, c.want.Files) || !slices.Equal(got.Bots, c.want.Bots) || !slices.Equal(got.Emoji, c.want.Emoji) ||
			!slices.Equal(got.Messages, c.want.Messages) || got.AnyMember != c.want.AnyMember || got.All != c.want.All {
			t.Errorf("RefsChanged(%+v) = %+v; want %+v", c.ev, got, c.want)
		}
	}
}

// A conversion records the bot that sent the message (senderOf's name and
// picture), a bot an attachment names, and every emoji it used, a
// reaction's too, known or not: an emoji added later draws in them.
func TestMessageRefsBotsAndEmoji(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutBot("B0HELPER1", store.BotInfo{Name: "Helper"})
	m := slack.Message{Type: "message", TS: "1700000000.000200", BotID: "B0HELPER1", Subtype: "bot_message", Text: "deploy :rocket: done :notyet:",
		Reactions: []slack.Reaction{{Name: "tada", Count: 1, Users: []string{"U1"}}}}
	v := a.MessageFromSlack("C1", &m)
	if !slices.Contains(v.Refs.Bots, "B0HELPER1") {
		t.Errorf("bots read %v; want the sender", v.Refs.Bots)
	}
	for _, name := range []string{"rocket", "notyet", "tada"} {
		if !slices.Contains(v.Refs.Emoji, name) {
			t.Errorf("emoji read %v; want %s", v.Refs.Emoji, name)
		}
	}
	if !v.Refs.Reads(RefChange{Emoji: []string{"notyet"}}) || !v.Refs.Reads(RefChange{Bots: []string{"B0HELPER1"}}) ||
		v.Refs.Reads(RefChange{Emoji: []string{"partyparrot"}}) || !v.Refs.Reads(RefChange{All: true}) {
		t.Errorf("Reads disagrees with %+v", v.Refs)
	}
	if ids := v.Refs.RefIDs(); !slices.Contains(ids, ":tada") || !slices.Contains(ids, "B0HELPER1") {
		t.Errorf("RefIDs %v: an emoji goes as :name, a bot as its id", ids)
	}
}

// What a conversion reads of a member and a conversation, field by field:
// each changes the fingerprint; a status, a topic, a purpose don't.
func TestDocFingerprints(t *testing.T) {
	base := slack.User{ID: "U0CARA001", Name: "cara", RealName: "Cara Lindqvist", TeamID: "T1", Profile: slack.Profile{DisplayName: "cara",
		RealName: "Cara Lindqvist", Image48: "i48", Image72: "i72", Image192: "i192", Image512: "i512", AvatarHash: "h1", StatusText: "here"}}
	fp := userDocFP(&base)
	for name, edit := range map[string]func(u *slack.User){
		"name": func(u *slack.User) { u.Name = "kit" }, "real name": func(u *slack.User) { u.RealName = "Kit L" },
		"display name": func(u *slack.User) { u.Profile.DisplayName = "kit" }, "profile real name": func(u *slack.User) { u.Profile.RealName = "Kit L" },
		"image_48": func(u *slack.User) { u.Profile.Image48 = "x" }, "image_72": func(u *slack.User) { u.Profile.Image72 = "x" },
		"image_192": func(u *slack.User) { u.Profile.Image192 = "x" }, "image_512": func(u *slack.User) { u.Profile.Image512 = "x" },
		"avatar hash": func(u *slack.User) { u.Profile.AvatarHash = "h2" }, "team": func(u *slack.User) { u.TeamID = "T2" },
	} {
		u := base
		edit(&u)
		if userDocFP(&u) == fp {
			t.Errorf("a member's %s changed and the fingerprint didn't", name)
		}
	}
	for name, edit := range map[string]func(u *slack.User){
		"status": func(u *slack.User) { u.Profile.StatusText = "away" }, "status emoji": func(u *slack.User) { u.Profile.StatusEmoji = ":palm_tree:" },
		"huddle": func(u *slack.User) { u.Profile.HuddleState = "in_a_huddle" },
	} {
		u := base
		edit(&u)
		if userDocFP(&u) != fp {
			t.Errorf("a member's %s changed the fingerprint", name)
		}
	}

	a, _ := testApp(t)
	props, _ := json.Marshal(map[string]any{"tabz": []any{map[string]any{"id": "Ct1", "type": "canvas"}}})
	conv := slack.Conversation{ID: "C0OPS0001", Name: "ops", IsChannel: true, IsMember: true, Properties: props,
		Topic: &slack.Purpose{Value: "on call"}, Purpose: &slack.Purpose{Value: "ops things"}}
	a.S.PutConv(conv)
	cfp := a.convDocFP(conv.ID)
	props2, _ := json.Marshal(map[string]any{"tabz": []any{map[string]any{"id": "Ct2", "type": "canvas"}}})
	for name, edit := range map[string]func(c *slack.Conversation){
		"name": func(c *slack.Conversation) { c.Name = "ops-2" }, "private": func(c *slack.Conversation) { c.IsPrivate = true },
		"archived": func(c *slack.Conversation) { c.IsArchived = true }, "properties": func(c *slack.Conversation) { c.Properties = props2 },
		"kind": func(c *slack.Conversation) { c.IsChannel, c.IsGroup = false, true },
	} {
		c := conv
		edit(&c)
		a.S.PutConv(c)
		if a.convDocFP(conv.ID) == cfp {
			t.Errorf("a conversation's %s changed and the fingerprint didn't", name)
		}
		a.S.PutConv(conv)
	}
	for name, edit := range map[string]func(c *slack.Conversation){
		"topic":   func(c *slack.Conversation) { c.Topic = &slack.Purpose{Value: "off call"} },
		"purpose": func(c *slack.Conversation) { c.Purpose = &slack.Purpose{Value: "other things"} },
	} {
		c := conv
		edit(&c)
		a.S.PutConv(c)
		if a.convDocFP(conv.ID) != cfp {
			t.Errorf("a conversation's %s changed the fingerprint", name)
		}
		a.S.PutConv(conv)
	}
}

// A member stored anew without an event (a search, the composer's @ list,
// channel details) and then changed only in status: the event compares
// with what the member's last event saw, so the silent rename shows now
// (SameNames false), and Docs converted before it name them anew. It was
// compared with the store's copy just before, which already had the new
// name.
func TestUserEventAfterSilentRename(t *testing.T) {
	a, _ := testApp(t)
	refsMsg(a)
	evs, stop := a.Subscribe()
	defer stop()
	userChange := func(display, status string) Event {
		t.Helper()
		u := map[string]any{"id": "U0CARA001", "name": "cara", "profile": map[string]any{"display_name": display, "status_text": status}}
		raw, _ := json.Marshal(map[string]any{"type": "user_change", "user": u})
		a.Inject(slack.Event{Type: "user_change", Raw: raw})
		for deadline := time.After(5 * time.Second); ; {
			select {
			case ev := <-evs:
				if ev.Type == "user" {
					return ev
				}
			case <-deadline:
				t.Fatal("no user event")
			}
		}
	}
	userChange("cara", "here") // the member's first event: the baseline
	a.S.PutUsers([]slack.User{{ID: "U0CARA001", Name: "cara", Profile: slack.Profile{DisplayName: "kit", StatusText: "here"}}})
	if ev := userChange("kit", "out to lunch"); ev.SameNames {
		t.Error("the status change after a silent rename was SameNames: Docs converted before keep the old name")
	}
	if ev := userChange("kit", "back"); !ev.SameNames {
		t.Error("a status change with names as the last event left them was not SameNames")
	}
	// the other order: an older answer (an autocomplete asked before the
	// rename) stored after the last event puts the old name back, and the
	// next event, with the new name the baseline has, isn't SameNames
	a.S.PutUsers([]slack.User{{ID: "U0CARA001", Name: "cara", Profile: slack.Profile{DisplayName: "cara", StatusText: "back"}}})
	if ev := userChange("kit", "in a meeting"); ev.SameNames {
		t.Error("the event after an older profile was stored silently was SameNames: Docs converted with the old name keep it")
	}
}

// Prefs installed without an event (users.prefs.get on a cold start, the
// cache) move the baseline; the next prefs event reports what they moved,
// and a change back to a default after them is a change. The baseline
// was New's, from before any prefs: turning full names off after a boot
// that installed them reported nothing.
func TestPrefsBaselineFollowsInstalls(t *testing.T) {
	a, f := testApp(t)
	f.mu.Lock()
	f.reply["users.prefs.set"] = `{"ok":true}`
	f.mu.Unlock()
	evs, stop := a.Subscribe()
	defer stop()
	next := func() Event {
		t.Helper()
		for deadline := time.After(5 * time.Second); ; {
			select {
			case ev := <-evs:
				if ev.Type == "prefs" {
					return ev
				}
			case <-deadline:
				t.Fatal("no prefs event")
			}
		}
	}
	a.installPrefs(map[string]json.RawMessage{"display_real_names": json.RawMessage("true")}) // a boot's, no event
	if !a.prefsMoved() {
		t.Error("an install that turned full names on moved nothing")
	}
	a.emit(Event{Type: "prefs"}) // the boot's event, whenever it comes
	if ev := next(); !ev.NamePrefs {
		t.Error("the prefs event after the install didn't report the names pref it moved")
	}
	if err := a.SetMultiplePrefs(context.Background(), map[string]any{"display_real_names": false}); err != nil {
		t.Fatal(err)
	}
	if ev := next(); !ev.NamePrefs {
		t.Error("full names turned off after the install (back to the default) reported no change")
	}
	if a.prefsMoved() {
		t.Error("the prefs event left the install's change pending")
	}
}

// A canvas that titles tab slugs is renamed: file_change refreshes its
// title (files.info) and names it (a "names" event), so the messages whose
// conversion read it convert again. Its first title stayed for the
// session.
func TestRenamedCanvasTitle(t *testing.T) {
	a, f := testApp(t)
	a.S.PutCanvasRef("F0RUNBOOK", richtext.CanvasRef{Title: "Runbook", Status: "normal", Kind: "canvas"})
	f.mu.Lock()
	f.reply["files.info"] = `{"ok":true,"file":{"id":"F0RUNBOOK","title":"Playbook","filetype":"quip","mode":"canvas","pretty_type":"Canvas"}}`
	f.mu.Unlock()
	evs, stop := a.Subscribe()
	defer stop()
	raw, _ := json.Marshal(map[string]any{"type": "file_change", "file_id": "F0RUNBOOK", "file": map[string]any{"id": "F0RUNBOOK"}})
	a.Inject(slack.Event{Type: "file_change", Raw: raw})
	for deadline := time.After(5 * time.Second); ; {
		select {
		case ev := <-evs:
			if ev.Type == "names" && slices.Contains(ev.Users, "F0RUNBOOK") {
				if r, _ := a.S.KnownCanvasRef("F0RUNBOOK"); r.Title != "Playbook" {
					t.Errorf("the canvas's title after file_change: %q", r.Title)
				}
				return
			}
		case <-deadline:
			r, _ := a.S.KnownCanvasRef("F0RUNBOOK")
			t.Fatalf("no names event for the renamed canvas (its title: %q)", r.Title)
		}
	}
}

// StoredMessage converts what the store holds again without its reads
// counting as a view's: the members it names keep their last hit, so
// their next profile change doesn't look drawn.
func TestStoredMessageReadsNoMember(t *testing.T) {
	a, _ := testApp(t)
	m := refsMsg(a)
	a.S.SetLatest("C0GEN0001", []slack.Message{m}, false)
	got, ok := a.StoredMessage("C0GEN0001", m.TS)
	if !ok || !slices.Contains(got.Refs.Members, "U0CARA001") {
		t.Fatalf("StoredMessage: %v, refs %+v", ok, got.Refs)
	}
	for _, id := range []string{"U0CARA001", "U0BOB0001"} {
		if at := a.S.MemberAccessed(id); !at.IsZero() {
			t.Errorf("StoredMessage counted a read of %s (%v)", id, at)
		}
	}
	if _, ok := a.StoredMessage("C0GEN0001", "1.0"); ok {
		t.Error("StoredMessage found a message the store doesn't hold")
	}
	a.MessageFromSlack("C0GEN0001", &m) // a view's conversion does count
	if a.S.MemberAccessed("U0CARA001").IsZero() {
		t.Error("a view's conversion didn't count its read of the file's owner")
	}
}

// Events say whether they changed what message Docs read: a status-only
// profile change, a topic, a bookmark don't; a name, a renamed channel,
// its tabs do; a names pref changes the Docs that name a member, and
// emoji_mode every Doc.
func TestEventsMarkDocChanges(t *testing.T) {
	a, f := testApp(t)
	f.mu.Lock()
	f.reply["users.prefs.set"] = `{"ok":true}`
	f.mu.Unlock()
	refsMsg(a)
	evs, stop := a.Subscribe()
	defer stop()
	next := func(typ string) Event {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case ev := <-evs:
				if ev.Type == typ {
					return ev
				}
			case <-deadline:
				t.Fatalf("no %s event", typ)
			}
		}
	}
	userChange := func(display, status string) Event {
		u := map[string]any{"id": "U0CARA001", "name": "cara", "profile": map[string]any{"display_name": display, "status_text": status}}
		raw, _ := json.Marshal(map[string]any{"type": "user_change", "user": u})
		a.Inject(slack.Event{Type: "user_change", Raw: raw})
		return next("user")
	}
	userChange("cara", "here") // the member's first event: the baseline
	if ev := userChange("cara", "out to lunch"); !ev.SameNames {
		t.Error("a status change marked the member's names changed")
	}
	if ev := userChange("kit", "out to lunch"); ev.SameNames {
		t.Error("a display name change marked the member's names the same")
	}
	// a picture or a real name alone: Docs read them too (huddle faces,
	// work objects' people)
	for name, extra := range map[string]map[string]any{"image_72": {"image_72": "https://avatars.invalid/kit-72.png"},
		"real_name": {"real_name": "Kit Lindqvist"}} {
		p := map[string]any{"display_name": "kit", "status_text": "out to lunch"}
		for k, v := range extra {
			p[k] = v
		}
		raw, _ := json.Marshal(map[string]any{"type": "user_change", "user": map[string]any{"id": "U0CARA001", "name": "cara", "profile": p}})
		a.Inject(slack.Event{Type: "user_change", Raw: raw})
		if ev := next("user"); ev.SameNames {
			t.Errorf("a change of the member's %s alone was SameNames", name)
		}
	}

	conv := func() Event { a.emit(Event{Type: "conv", Channel: "C0OPS0001"}); return next("conv") }
	conv() // the first one only learns the channel
	if ev := conv(); !ev.SameNames {
		t.Error("a conv event that changed nothing marked the channel changed")
	}
	c, _ := a.S.Conv("C0OPS0001")
	renamed := *c
	renamed.Name = "ops-2"
	a.S.PutConv(renamed)
	if ev := conv(); ev.SameNames {
		t.Error("a rename marked the channel the same")
	}
	a.emit(Event{Type: "tabs", Channel: "C0OPS0001", Name: "bookmark_added"})
	if ev := next("tabs"); !ev.SameNames {
		t.Error("a bookmark's tabs event marked the channel changed")
	}

	set := func(k string, v any) Event {
		t.Helper()
		if err := a.SetMultiplePrefs(context.Background(), map[string]any{k: v}); err != nil {
			t.Fatal(err)
		}
		return next("prefs")
	}
	if ev := set("color_names_in_list", false); ev.NamePrefs || ev.DocPrefs {
		t.Errorf("a pref no Doc reads marked NamePrefs %v, DocPrefs %v", ev.NamePrefs, ev.DocPrefs)
	}
	if ev := set("display_real_names", true); !ev.NamePrefs || ev.DocPrefs {
		t.Errorf("display_real_names marked NamePrefs %v, DocPrefs %v; want only NamePrefs", ev.NamePrefs, ev.DocPrefs)
	}
	if ev := set("emoji_mode", "as_text"); ev.NamePrefs || !ev.DocPrefs {
		t.Errorf("emoji_mode marked NamePrefs %v, DocPrefs %v; want only DocPrefs (every Doc's emoji)", ev.NamePrefs, ev.DocPrefs)
	}
	if ev := set("all_notifications_prefs", `{"global":{"global_keywords":"launch"}}`); ev.NamePrefs || !ev.DocPrefs {
		t.Errorf("a new highlight word marked NamePrefs %v, DocPrefs %v; want only DocPrefs (every Doc's highlights)", ev.NamePrefs, ev.DocPrefs)
	}
}

// A removal of custom emoji goes out with the names removed (Slack's
// names), so the messages that drew them convert again and no other; and
// a message drawing an alias reads its target too, so a change to the
// target converts it again.
func TestEmojiRemovalNamesAndAliases(t *testing.T) {
	a, _ := testApp(t)
	a.S.CustomEmoji.Put("partyparrot", "https://emoji.invalid/pp.gif")
	a.S.CustomEmoji.Put("pp", "alias:partyparrot")
	evs, stop := a.Subscribe()
	defer stop()
	raw, _ := json.Marshal(map[string]any{"type": "emoji_changed", "subtype": "remove", "names": []string{"partyparrot", "pp"}, "event_ts": "1700000000.000100"})
	a.Inject(slack.Event{Type: "emoji_changed", Raw: raw})
	for deadline := time.After(5 * time.Second); ; {
		select {
		case ev := <-evs:
			if ev.Type != "emoji_changed" {
				continue
			}
			if !slices.Equal(ev.Users, []string{"partyparrot", "pp"}) {
				t.Errorf("the removal's event names %v; want the names Slack removed", ev.Users)
			}
			if c := RefsChanged(ev); c.All || !slices.Equal(c.Emoji, []string{"partyparrot", "pp"}) {
				t.Errorf("RefsChanged(the removal) = %+v; want the two names, not every message", c)
			}
			goto aliases
		case <-deadline:
			t.Fatal("no emoji_changed event")
		}
	}
aliases:
	a.S.CustomEmoji.Put("partyparrot", "https://emoji.invalid/pp.gif")
	a.S.CustomEmoji.Put("pp", "alias:partyparrot")
	rn := refNames{msgNames: msgNames{a.S}, app: a}
	rn.Emoji("pp")
	if !slices.Contains(rn.refs.Emoji, "pp") || !slices.Contains(rn.refs.Emoji, "partyparrot") {
		t.Errorf("an alias's refs %v; want it and its target", rn.refs.Emoji)
	}
}
