package core

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A message slug's label is the linked message's (getMessageSlugAttributes,
// client-boot N1tw): its sender as the store names them (a member, a bot
// message's username, else the bot), "Unknown author" for a sender nothing
// names, "Private message" while the store doesn't hold it. A draw's read
// asks messages.list for a message the store doesn't hold, and the answer
// is a "names" naming it ("channel/ts"); one Slack says doesn't exist
// stays "Private message" and isn't asked for again.
func TestMessageSlugLabelReadsTheLinkedMessage(t *testing.T) {
	a, f := testApp(t)
	const (
		alices, own, bobs = "1762795200.000100", "1762795201.000100", "1762795202.000100"
		botName, botOnly  = "1762795203.000100", "1762795204.000100"
		nobodys, far      = "1762795205.000100", "1762795206.000100"
		gone              = "1762795207.000100"
	)
	a.S.PutBot("B0SLUGBOT2", store.BotInfo{Name: "Helper"})
	a.S.PutFetched("D1", []slack.Message{
		{Type: "message", TS: alices, User: "U1", Text: "a synthetic line from alice"},
		{Type: "message", TS: own, User: "USELF", Text: "a synthetic line from the viewer"},
	}, a.S.LiveSeq())
	a.S.PutFetched("C1", []slack.Message{
		{Type: "message", TS: bobs, User: "U2", Text: "a synthetic line from bob"},
		{Type: "message", TS: botName, BotID: "B0SLUGBOT1", Username: "deploys", Subtype: "bot_message", Text: "a synthetic bot line"},
		{Type: "message", TS: botOnly, BotID: "B0SLUGBOT2", Subtype: "bot_message", Text: "another synthetic bot line"},
		{Type: "message", TS: nobodys, User: "U0NOBODY01", Text: "a synthetic line from someone nobody fetched"},
	}, a.S.LiveSeq())
	alice, _ := a.S.MemberNameHeld("U1")
	me, _ := a.S.MemberNameHeld("USELF")
	bob, _ := a.S.MemberNameHeld("U2")
	if alice == "" || me == "" || bob == "" {
		t.Fatalf("the fixture's names: %q %q %q", alice, me, bob)
	}
	for _, c := range []struct {
		ch, ts, want string
		off          bool
	}{
		{"D1", alices, alice, false},
		{"D1", own, me + " with " + strings.TrimPrefix(a.S.ChannelName("D1"), "#"), false},
		{"C1", bobs, bob + " in #general", false},
		{"C1", botName, "deploys in #general", false},
		{"C1", botOnly, "Helper in #general", false},
		{"C1", nobodys, "Unknown author in #general", false},
		{"C1", far, "Private message", true},
	} {
		if label, _, _, off := a.MessageSlugLabel(c.ch, c.ts, false); label != c.want || off != c.off {
			t.Errorf("%s/%s: %q off=%v, want %q off=%v", c.ch, c.ts, label, off, c.want, c.off)
		}
	}
	if n := f.count("messages.list"); n != 0 {
		t.Fatalf("a read without ask asked messages.list %d times", n)
	}

	// a draw's read: messages.list by id, then "names" naming the message
	f.mu.Lock()
	f.reply["messages.list"] = `{"ok":true,"messages":{"C1":[{"type":"message","ts":"` + far + `","user":"U2","text":"a synthetic line fetched by id"}]}}`
	f.mu.Unlock()
	evs, stop := a.Subscribe()
	defer stop()
	if label, _, _, off := a.MessageSlugLabel("C1", far, true); label != "Private message" || !off {
		t.Errorf("while messages.list is out: %q off=%v, want Slack's Private message", label, off)
	}
	namesFor(t, evs, "C1/"+far)
	if label, _, _, off := a.MessageSlugLabel("C1", far, false); label != bob+" in #general" || off {
		t.Errorf("once messages.list answered: %q off=%v, want %q", label, off, bob+" in #general")
	}

	// a message messages.list doesn't return: the non-existent stub, not
	// asked for again
	f.mu.Lock()
	f.reply["messages.list"] = `{"ok":true,"messages":{}}`
	f.mu.Unlock()
	a.MessageSlugLabel("C1", gone, true)
	namesFor(t, evs, "C1/"+gone)
	n := f.count("messages.list")
	for range 3 {
		if label, _, _, off := a.MessageSlugLabel("C1", gone, true); label != "Private message" || !off {
			t.Errorf("a message Slack doesn't have: %q off=%v, want Private message", label, off)
		}
	}
	time.Sleep(refetchGap + 100*time.Millisecond)
	if m := f.count("messages.list"); m != n {
		t.Errorf("a message Slack said doesn't exist was asked for again (%d more)", m-n)
	}
	if got := a.MessageSlugText("C1", gone, "https://x.slack.com/archives/C1/p1762795207000100"); got != "https://x.slack.com/archives/C1/p1762795207000100" {
		t.Errorf("a notification's text for it: %q, want the link (convertUnknownSlugToUrl)", got)
	}
	if got := a.MessageSlugText("C1", bobs, "https://x.slack.com/archives/C1/p1762795202000100"); got != bob+" in #general" {
		t.Errorf("a notification's text for bob's: %q", got)
	}
}

// A notification's message names its slugs' authors (getMessageMentions'
// userIds), the elements' author_ids, nested anywhere in its blocks.
func TestSlugAuthors(t *testing.T) {
	m := &Message{Blocks: []byte(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
		{"type":"text","text":"see "},
		{"type":"message_mention","channel_id":"C1","message_ts":"1.000100","author_id":"U1"},
		{"type":"message_mention","channel_id":"C1","message_ts":"1.000200"},
		{"type":"message_mention","channel_id":"C1","message_ts":"1.000300","author_id":"U1"}]},
		{"type":"rich_text_quote","elements":[{"type":"message_mention","channel_id":"D1","message_ts":"1.000400","author_id":"U2"}]}]}]`)}
	got := SlugAuthors(m)
	slices.Sort(got)
	if !slices.Equal(got, []string{"U1", "U2"}) {
		t.Errorf("SlugAuthors = %v, want U1 and U2 once each", got)
	}
	if SlugAuthors(&Message{}) != nil || SlugAuthors(nil) != nil {
		t.Error("a message without blocks has no slug authors")
	}
}
