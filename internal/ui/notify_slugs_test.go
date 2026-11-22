package ui

// A toast writes a message slug as Slack's messageMentionToText (client-
// boot DRDu) does: showNotification first waits for
// ensureModelsArePresent (Eley te, DHDI), which fetches the linked
// messages by id (x0EU), the elements' author_ids and channels, then the
// label is getMessageSlugAttributes' for the linked message's own sender
// (fetchIfMissing false), or the link itself when the slug is disabled
// (convertUnknownSlugToUrl: Slack has no such message). Never "Unknown
// author" where Slack names the sender. Synthetic data only.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

func TestNotifyWritesMessageSlugsLikeSlack(t *testing.T) {
	a, ca, fs, rec := privNoteAppFS(t, func(sz *fakeslack.Size) { sz.MessagesByID = true }) // messages.list by ts
	g := fakeslack.ThreadGroupDMID
	conv := fixtureConv(t, ca, g)
	ca.S.PutConv(conv) // the group DM, its history never loaded
	ch := fs.ChannelIDs()[2]
	chConv := fixtureConv(t, ca, ch)
	ca.S.PutConv(chConv) // the channel the links point into, its history never loaded either
	var sender string
	for _, id := range conv.Members {
		if id != fakeslack.SelfID {
			sender = id
			break
		}
	}
	// the first link's sender: someone outside the group the store
	// doesn't hold, whom only the element's author_id names
	var other string
	for _, id := range fs.UserIDs() {
		in := false
		for _, m := range conv.Members {
			in = in || m == id
		}
		if _, held := ca.S.RawUser(id); !in && !held && id != fakeslack.SelfID {
			other = id
			break
		}
	}
	if sender == "" || other == "" {
		t.Fatalf("the fixture has no sender (%q) or no member to link (%q)", sender, other)
	}
	byOther, _ := fs.PostQuiet(ch, other, "a synthetic line the first link points at")
	bySender, _ := fs.PostQuiet(ch, sender, "a synthetic line the second link points at")
	const missing = "1762520000.000999" // no such message
	for _, ts := range []string{byOther, bySender, missing} {
		if _, ok := ca.S.FindHeld(ch, ts); ok {
			t.Fatalf("%s is held before the toast: the case checks nothing", ts)
		}
	}
	link := func(ts, author string) map[string]any {
		el := map[string]any{"type": "message_mention", "channel_id": ch, "message_ts": ts, "url": slugPermalink(ch, ts),
			"text": slugPermalink(ch, ts)}
		if author != "" {
			el["author_id"] = author
		}
		return el
	}
	txt := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	blocks := []any{map[string]any{"type": "rich_text", "block_id": "n1", "elements": []any{map[string]any{
		"type": "rich_text_section", "elements": []any{txt("look "), link(byOther, other), txt(" and "), link(bySender, ""),
			txt(" and "), link(missing, "")}}}}}

	evs, stop := ca.Subscribe()
	defer stop()
	const ts = "1762520400.000200"
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": g, "user": sender, "ts": ts,
		"text": "look " + slugPermalink(ch, byOther), "blocks": blocks})
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

	in := " in #" + strings.TrimPrefix(chConv.Name, "#") // the plain label: "#" for a private channel too
	on, _ := ca.S.MemberNameHeld(other)
	sn, _ := ca.S.MemberNameHeld(sender)
	if on == "" || sn == "" {
		t.Errorf("the toast didn't wait for the links' senders: %q (the element's author_id) %q (the message's sender)", on, sn)
	}
	for _, want := range []string{on + in, sn + in, slugPermalink(ch, missing)} {
		if !strings.Contains(n.Body, want) {
			t.Errorf("the toast's body is %q: want %q in it (Slack's messageMentionToText)", n.Body, want)
		}
	}
	for _, never := range []string{"Unknown author", "Private message"} {
		if strings.Contains(n.Body, never) {
			t.Errorf("the toast's body %q writes %q where Slack names the sender or writes the link", n.Body, never)
		}
	}
}
