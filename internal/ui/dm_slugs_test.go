package ui

// Karim's capture (α.109, 2026-11-19): a DM's message link drawn as the
// pill "Unknown author". Slack labels a message slug for the linked
// message itself (getMessageSlugAttributes, client-boot N1tw): the
// message from the messages store (wClv getMessageByTimestamp), fetched
// by id when the store doesn't hold it (Za0J: messages.list), and its
// sender (hVKV getMsgSenderEntity) named as the members store names them
// (xbeB, getMemberById: users/info for one it doesn't hold). The
// element's author_id plays no part. While messages.list is out the
// pill is the Unavailable variant, "Private message"; "Unknown author"
// only while the sender is the members fetch's unknown stub.
//
// The DM holds two links: one whose element has no author_id, to a
// message of the same DM from the partner, and one whose author_id is a
// member nobody has fetched or asked for, to that member's message in a
// channel whose history the client never loaded. Synthetic people and
// text only.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// slugPermalink is a synthetic permalink to ts of ch.
func slugPermalink(ch, ts string) string {
	return "https://fakeslack.slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", "")
}

// slugBlocks is a rich_text message "see " and a message_mention of ts
// in ch, with author_id when author is set (Slack's composer sends one
// when it knew the linked message's sender).
func slugBlocks(ch, ts, author string) json.RawMessage {
	el := map[string]any{"type": "message_mention", "channel_id": ch, "message_ts": ts, "url": slugPermalink(ch, ts),
		"text": slugPermalink(ch, ts)}
	if author != "" {
		el["author_id"] = author
	}
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "s" + strings.ReplaceAll(ts, ".", ""),
		"elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
			map[string]any{"type": "text", "text": "see "}, el}}}}})
	return b
}

// slugShownIn is the label the row of ts drew for its message slug (the
// body's text after "see "), and whether the row was drawn.
func slugShownIn(rows []messages.SenderView, ts string) (string, bool) {
	for _, sv := range rows {
		if sv.TS == ts {
			return strings.TrimSpace(strings.TrimPrefix(sv.Text, "see ")), true
		}
	}
	return "", false
}

// dmRows are the rows the DM's list drew in the last frame.
func dmRows(e *perfEnv, ch string) []messages.SenderView {
	var out []messages.SenderView
	for _, sv := range dmSenders(e, ch) {
		out = append(out, sv)
	}
	return out
}

// holdID makes a users/info request that asks for id wait until release.
func (l *usersInfoLog) holdID(id string) {
	l.mu.Lock()
	l.hold[id] = true
	l.mu.Unlock()
}

// waitSlug runs frames until rows() draws ts's slug as want, reporting
// (not stopping the test) what it drew instead when d runs out.
func waitSlug(t *testing.T, e *perfEnv, what string, rows func() []messages.SenderView, ts, want string, d time.Duration) bool {
	t.Helper()
	got, drawn := "", false
	for dl := time.Now().Add(d); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
		e.frame()
		if got, drawn = slugShownIn(rows(), ts); drawn && got == want {
			return true
		}
	}
	if !drawn {
		t.Errorf("%s: the row of %s wasn't drawn", what, ts)
	} else {
		t.Errorf("%s: the pill reads %q, want %q (Slack's)", what, got, want)
	}
	return false
}

// keepSlugs runs frames for d, reporting each frame that draws a slug of
// want otherwise.
func keepSlugs(t *testing.T, e *perfEnv, what string, rows func() []messages.SenderView, want map[string]string, d time.Duration) {
	t.Helper()
	bad := 0
	for dl := time.Now().Add(d); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
		e.frame()
		r := rows()
		for ts, w := range want {
			if got, drawn := slugShownIn(r, ts); drawn && got != w {
				if bad++; bad <= 3 {
					t.Errorf("%s: a frame draws %s's pill as %q, want %q (Slack's)", what, ts, got, w)
				}
			}
		}
	}
}

func TestDMMessageSlugsLikeSlack(t *testing.T) {
	sz := fakeslack.Small
	sz.MessagesByID = true // messages.list answers by ts, as Slack's does
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	partner := fs.UserIDs()[62]
	const dm = "D0DMSLUG0001" // a closed DM the test opens
	fs.AddClosedDM(dm, partner)
	users := watchUsersInfo(t, fs) // every users/info ask, from boot on
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.Core()
	// the second link's sender: a member nothing has fetched or asked
	// for, posting in a public channel whose history nothing loaded (not
	// the first channel, which a launch opens)
	var other, ch string
	for _, id := range fs.UserIDs()[1:] {
		if _, held := c.S.RawUser(id); id != fakeslack.SelfID && id != partner && !held && !users.wasAsked(id) && !c.S.Nonexistent(id) {
			other = id
			break
		}
	}
	chans := fs.ChannelIDs()
	for i := len(chans) - 1; i > 0; i-- {
		if k, ok := c.S.ConvKind(chans[i]); ok && k == "public" && e.loadedMessages(chans[i]) == 0 {
			ch = chans[i]
			break
		}
	}
	if other == "" || ch == "" {
		t.Fatalf("no member nothing asked for (%q), or no public channel nothing loaded (%q)", other, ch)
	}
	users.holdID(other) // its users/info waits for the test
	in := " in #" + strings.TrimPrefix(c.S.ChannelName(ch), "#")
	// the messages, once the client is up: nothing could read them at boot
	linked, _ := fs.PostQuiet(dm, partner, "a synthetic line the first link points at")
	farTS, _ := fs.PostQuiet(ch, other, "a synthetic line the second link points at")
	noAuthor := fs.PostBlocksQuiet(dm, fakeslack.SelfID, slugPermalink(dm, linked), slugBlocks(dm, linked, ""))
	withAuthor := fs.PostBlocksQuiet(dm, fakeslack.SelfID, slugPermalink(ch, farTS), slugBlocks(ch, farTS, other))
	fs.PostReplyQuiet(dm, noAuthor, partner, "a synthetic reply") // a thread to open on the first link
	if _, ok := c.S.FindHeld(ch, farTS); ok {
		t.Fatal("the second link's message is held before anything asked for it: the case checks nothing")
	}
	releaseList, listed := holdCalls(t, fs, "messages.list") // the messages fetch by id
	rows := func() []messages.SenderView { return dmRows(e, dm) }

	e.openChannel(dm)
	e.until("the DM's history", 20*time.Second, func() bool { return e.loadedMessages(dm) > 0 })
	e.toLatest()
	e.until("the links' rows", 20*time.Second, func() bool {
		r := rows()
		_, a := slugShownIn(r, noAuthor)
		_, b := slugShownIn(r, withAuthor)
		return a && b
	})
	e.until("the partner fetched", 10*time.Second, func() bool { _, ok := c.S.RawUser(partner); return ok })
	pn, _ := c.S.MemberNameHeld(partner)
	if pn == "" {
		t.Fatal("the partner has no name to show")
	}

	// messages.list held: the link without an author_id names the
	// partner, who sent the message it links to (the store holds it: the
	// DM's history); the other one's message isn't there yet, Slack's
	// Unavailable variant
	ok1 := waitSlug(t, e, "no author_id, the message held", rows, noAuthor, pn, 10*time.Second)
	ok2 := waitSlug(t, e, "messages.list out", rows, withAuthor, "Private message", 10*time.Second)
	if ok1 && ok2 {
		keepSlugs(t, e, "messages.list out", rows, map[string]string{noAuthor: pn, withAuthor: "Private message"}, 300*time.Millisecond)
	}
	if listed() == 0 {
		t.Error("nothing asked messages.list for the second link's message (getMessageByTimestamp's fetch)")
	}

	// messages.list answers, users/info for its sender still out: the
	// sender is the members fetch's unknown stub, "Unknown author"
	releaseList()
	if waitSlug(t, e, "the message in, its sender's users/info out", rows, withAuthor, "Unknown author"+in, 10*time.Second) {
		// the label's read asks for the sender (getMemberById), on the
		// member fetcher's next tick; the pill stays the unknown stub's
		// while that is out
		for dl := time.Now().Add(10 * time.Second); !users.wasAsked(other) && time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
			e.frame()
		}
		if !users.wasAsked(other) {
			t.Error("nothing asked users/info for the second link's sender")
		}
		keepSlugs(t, e, "its sender's users/info out", rows, map[string]string{noAuthor: pn, withAuthor: "Unknown author" + in}, 300*time.Millisecond)
	}

	// users/info answers: the pill names the sender, and from then on no
	// frame draws "Unknown author" or "Private message" where Slack names
	// them
	users.release()
	e.until("the second link's sender fetched", 10*time.Second, func() bool { _, ok := c.S.RawUser(other); return ok })
	on, _ := c.S.MemberNameHeld(other)
	if on == "" {
		t.Fatal("the second link's sender has no name to show")
	}
	if waitSlug(t, e, "both answered", rows, withAuthor, on+in, 10*time.Second) {
		keepSlugs(t, e, "both answered", rows, map[string]string{noAuthor: pn, withAuthor: on + in}, 300*time.Millisecond)
	}

	// the thread pane on the first link's message draws it as its root
	e.a.OpenThread(dm, noAuthor)
	e.until("the thread", 15*time.Second, func() bool {
		tv := e.threadView(dm, noAuthor)
		return tv != nil && tv.ContentHeight() > 0
	})
	thread := func() []messages.SenderView {
		if tv := e.threadView(dm, noAuthor); tv != nil {
			return tv.Senders()
		}
		return nil
	}
	if waitSlug(t, e, "the thread pane", thread, noAuthor, pn, 10*time.Second) {
		keepSlugs(t, e, "the thread pane", thread, map[string]string{noAuthor: pn}, 300*time.Millisecond)
	}
	for _, r := range [][]messages.SenderView{rows(), thread()} {
		for _, sv := range r {
			if strings.Contains(sv.Text, "Unknown author") {
				t.Errorf("row %s draws %q", sv.TS, sv.Text)
			}
		}
	}
}
