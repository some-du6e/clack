package core

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
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
			Channels: []string{"U0CARA001", "F0RUNBOOK"}, Files: []string{"U0CARA001", "F0RUNBOOK"}}},
		{Event{Type: "prefs", NamePrefs: true}, RefChange{AnyMember: true}},
		{Event{Type: "prefs"}, RefChange{}},
		{Event{Type: "message", Channel: "C0OPS0001"}, RefChange{}},
	} {
		got := RefsChanged(c.ev)
		if !slices.Equal(got.Members, c.want.Members) || !slices.Equal(got.Channels, c.want.Channels) ||
			!slices.Equal(got.Files, c.want.Files) || got.AnyMember != c.want.AnyMember {
			t.Errorf("RefsChanged(%+v) = %+v; want %+v", c.ev, got, c.want)
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
	if ev := userChange("cara", "out to lunch"); !ev.SameNames {
		t.Error("a status change marked the member's names changed")
	}
	if ev := userChange("kit", "out to lunch"); ev.SameNames {
		t.Error("a display name change marked the member's names the same")
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
}
