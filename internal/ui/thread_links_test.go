package ui

// The rest of Slack's jumpToMessage and openPermalink (boot-shared wc0P,
// 0S61, build 2c0d02ca130f) beyond a reply's link (thread_open_root_test.go):
//
//   - a link to a thread's parent opens its thread (BaseMrkdwnLink's
//     openThreadForRootMessage), but in a DM with a bot the conversation
//     at it; a message_mention pill opens the thread its thread_ts names;
//   - a link to an old message loads the window around it once (Slack's
//     message pane opened at startTs: two conversations.history calls),
//     never a walk back from the newest page;
//   - in the split view a link's jump goes on the view's back stack and
//     starts the thread at the reply, highlighted;
//   - a route naming a reply as its thread leaves no history entry, and a
//     pane opened with a reply's ts fetches the thread once;
//   - links with only a thread (and message=latest) take toThread, a
//     slack:// link in a message goes to handleDeepLink, a link into
//     another workspace opens in the browser;
//   - clack.log tags every id a navigation writes.
//
// The checks read what was drawn: the rows a pane laid out in the last
// frame and the jump highlight each painted (messages.SenderView), the
// split view's through its own pane. Synthetic fakeslack fixtures;
// nothing talks to Slack.

import (
	"encoding/json"
	"image"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// paneRows is what a pane drew in the last frame: the rows of thread
// ch/ts (ts "": of conversation ch), in the main window (pane "") or the
// split view (paneSplit).
func (e *perfEnv) paneRows(pane, ch, ts string) []messages.SenderView {
	var rows []messages.SenderView
	e.a.inPane(pane, func() {
		c := e.a.convos[ch+"/"+ts]
		if c == nil {
			return
		}
		if l, ok := c.list.(interface{ Senders() []messages.SenderView }); ok {
			rows = l.Senders()
		}
	})
	return rows
}

// drawnState lists rows for a failure, "*" after a row that drew the
// jump highlight.
func drawnState(rows []messages.SenderView) string {
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(r.TS)
		if r.Highlight > 0 {
			b.WriteByte('*')
		}
	}
	return "[" + b.String() + "]"
}

// lit reports that row ts drew the jump highlight
// (c-message_kit__background--highlight).
func lit(rows []messages.SenderView, ts string) bool {
	for _, r := range rows {
		if r.TS == ts && r.Highlight > 0 {
			return true
		}
	}
	return false
}

// threadDrawn reports a pane that drew a fixture thread whole, from its
// root: the root, then its two replies.
func threadDrawn(rows []messages.SenderView, root string) bool {
	return len(rows) == 3 && rows[0].TS == root
}

// waitDrawn runs frames until cond holds for what the pane drew, and
// fails with those rows, the route and the panes after d.
func (e *perfEnv) waitDrawn(t *testing.T, what string, d time.Duration, pane, ch, ts string, cond func([]messages.SenderView) bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		rows := e.paneRows(pane, ch, ts)
		if cond(rows) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %s/%s drew %s; %s, split view %q", what, ch, ts, drawnState(rows), e.openState(), e.a.splitTop())
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
}

// linkPointIn is linkPoint for a conversation in pane (paneSplit: the
// split view's).
func (e *perfEnv) linkPointIn(t *testing.T, pane, ch, url string) image.Point {
	t.Helper()
	var r image.Rectangle
	e.until("the link to "+url, 15*time.Second, func() bool {
		found := false
		e.a.inPane(pane, func() {
			for _, lr := range e.mcLinks(ch, "") {
				if lr.Kind == richview.HitLink && lr.URL == url {
					r, found = lr.R, true
					return
				}
			}
		})
		return found
	})
	return image.Pt(r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2)
}

// apiCalls is what the fake was asked, per method (recordCalls).
type apiCalls struct {
	mu    sync.Mutex
	forms map[string][]url.Values
}

// recordCalls records the params of every call to methods from now on;
// the calls get the fake's answers as before.
func (e *perfEnv) recordCalls(methods ...string) *apiCalls {
	c := &apiCalls{forms: map[string][]url.Values{}}
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if slices.Contains(methods, method) {
			cp := url.Values{}
			for k, v := range f {
				cp[k] = append([]string(nil), v...)
			}
			c.mu.Lock()
			c.forms[method] = append(c.forms[method], cp)
			c.mu.Unlock()
		}
		return nil, false
	})
	e.tb.Cleanup(func() { e.fs.SetHook(nil) })
	return c
}

// of lists the calls to method (of channel ch, "" any) as "key=value"
// lines of keys.
func (c *apiCalls) of(method, ch string, keys ...string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, f := range c.forms[method] {
		if ch != "" && f.Get("channel") != ch {
			continue
		}
		kv := make([]string, 0, len(keys))
		for _, k := range keys {
			kv = append(kv, k+"="+f.Get(k))
		}
		out = append(out, strings.Join(kv, " "))
	}
	return out
}

// noRawIDs fails on every captured log line that names one of ids
// as is: ids go in as their tags (docs/INSPECT.md).
func noRawIDs(t *testing.T, l *logLines, ids ...string) {
	t.Helper()
	for _, line := range l.all() {
		for _, id := range ids {
			if id != "" && strings.Contains(line, id) {
				t.Errorf("clack.log line %q names %s: ids go in as tags", line, id)
			}
		}
	}
}

// TestParentLinkOpensItsThread: a mrkdwn or rich_text link renders
// through Slack's BaseMrkdwnLink, which passes openThreadForRootMessage,
// so a link to a thread's parent (Copy link on a parent carries no
// thread_ts) opens its thread: toThread({threadTs: root, replyTs: root,
// highlightRoot}), the root highlighted in the conversation beside it.
// In a DM with a bot (DUy0 G: isBotUser) the flag doesn't apply and the
// link opens the conversation at the message. Clack opened the
// conversation at the parent with no thread, wherever it was.
func TestParentLinkOpensItsThread(t *testing.T) {
	root, digest := fakeslack.ThreadRootTS, fakeslack.ThreadPrivateID
	t.Run("in a channel", func(t *testing.T) {
		e := threadHeaderEnv(t, false)
		pub := fakeslack.ThreadPublicID
		link := digestLink(pub, root, "")
		postDigest(e.fs, digest, link)
		e.openChannel(digest)
		e.settle(800 * time.Millisecond)
		e.clickAt(e.linkPoint(t, digest, link), time.Second)
		e.waitDrawn(t, "a click on a thread parent's link", 20*time.Second, "", pub, root, func(rows []messages.SenderView) bool {
			return threadDrawn(rows, root)
		})
		if r := e.a.router.Current(); r != Route("c/"+pub+"/t/"+root) {
			t.Errorf("route %q, want the thread's c/%s/t/%s (toThread)", r, pub, root)
		}
		e.waitDrawn(t, "the root highlighted in the conversation (highlightRoot)", 10*time.Second, "", pub, "",
			func(rows []messages.SenderView) bool { return lit(rows, root) })
	})
	t.Run("in a DM with a bot", func(t *testing.T) {
		e := threadHeaderEnv(t, false)
		dm := fakeslack.ThreadAppDMID
		e.openChannel(dm) // its header names the app: users.info has the bot user
		e.until("the app's bot user", 15*time.Second, func() bool {
			u, ok := e.a.Core().S.User(fakeslack.ThreadAppUserID)
			return ok && u != nil && u.IsBot
		})
		link := digestLink(dm, root, "")
		postDigest(e.fs, digest, link)
		e.openChannel(digest)
		e.settle(800 * time.Millisecond)
		e.clickAt(e.linkPoint(t, digest, link), time.Second)
		e.waitDrawn(t, "a click on the link to a parent in a bot's DM", 20*time.Second, "", dm, "",
			func(rows []messages.SenderView) bool { return lit(rows, root) })
		if r := e.a.router.Current(); r != Route("c/"+dm+"/m/"+root) {
			t.Errorf("route %q, want the DM at the message (c/%s/m/%s): a bot's DM opens no thread", r, dm, root)
		}
		if s := e.a.secondary; s.kind == secThread {
			t.Errorf("a thread opened beside the bot's DM: %s", e.openState())
		}
	})
}

// TestMessageMentionPillOpensThread: a message_mention pill (a pasted
// permalink the composer made a slug of) carries the message's
// thread_ts, its own ts for a thread's parent (KwXc
// hydrateMessageSlugData), and its click is jumpToMessage({channelId,
// ts: message_ts, threadTs: thread_ts}) (p9lB MessageMention): a
// parent's pill opens its thread, a reply's the thread at the reply,
// highlighted, from the pill's own ids (its url, here another message's,
// is never followed) with no lookup, and with no suspicious-link check
// (the pill isn't a link the user typed). Clack treated the pill as a
// plain link: the label isn't the URL, so the click went no further than
// the link check (on 1c8ea7ba it navigated nowhere), and past it a
// parent's link opened the conversation, no thread.
func TestMessageMentionPillOpensThread(t *testing.T) {
	pub, root := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS
	for _, c := range []struct{ name, ts, at string }{
		{"to a parent", root, root},
		{"to a reply", fakeslack.ThreadReplyTS(1), fakeslack.ThreadReplyTS(1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := threadHeaderEnv(t, false)
			digest := fakeslack.ThreadPrivateID
			// the pill's url names another message: only its ids are followed
			link := digestLink(fakeslack.ThreadDMID, root, "")
			b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "pill", "elements": []any{
				map[string]any{"type": "rich_text_section", "elements": []any{
					map[string]any{"type": "text", "text": "synthetic: see "},
					map[string]any{"type": "message_mention", "channel_id": pub, "message_ts": c.ts, "thread_ts": root,
						"url": link, "author_id": e.fs.UserIDs()[2]}}}}}})
			e.fs.PostBlocksQuiet(digest, e.fs.UserIDs()[3], "synthetic: see "+link, b)
			e.openChannel(digest)
			e.settle(800 * time.Millisecond)
			calls := e.recordCalls("messages.list")
			e.clickAt(e.linkPoint(t, digest, link), time.Second)
			e.waitDrawn(t, "a click on the pill", 20*time.Second, "", pub, root, func(rows []messages.SenderView) bool {
				return threadDrawn(rows, root) && lit(rows, c.at)
			})
			if r := e.a.router.Current(); r != Route("c/"+pub+"/t/"+root) {
				t.Errorf("route %q, want the pill's thread c/%s/t/%s", r, pub, root)
			}
			if asked := calls.of("messages.list", "", "message_ids"); len(asked) > 0 {
				t.Errorf("the pill's message was looked up: %q (its thread_ts is given)", asked)
			}
		})
	}
}

// TestPermalinkLoadsAroundOldMessage: a link to a message far back in a
// conversation the session has open opens it at the message, loaded the
// way Slack's message pane opened at startTs loads it: the window around
// the message, two conversations.history calls (latest and oldest the
// message, inclusive, 28 each), once. The message is drawn highlighted
// and clack.log tags the conversation. Clack paged back from the newest
// page, up to 20 pages of history, and gave up 2,000 messages back.
func TestPermalinkLoadsAroundOldMessage(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	sz.LongChannels, sz.LongHistory = 1, 3000
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	long, digest := fs.LongChannelIDs()[0], fakeslack.ThreadPrivateID
	target := ""
	for i := 2500; i < 2600 && target == ""; i++ { // a message that isn't a thread's parent
		if m, ok := fs.HistoryAt(long, i); ok && m.ThreadTS == "" {
			target = m.TS
		}
	}
	if target == "" {
		t.Fatalf("no plain message 2,500 back in %s", long)
	}
	link := digestLink(long, target, "")
	postDigest(fs, digest, link)
	e.openChannel(long) // seen this session: its newest page loaded
	e.waitDrawn(t, "the long conversation's newest messages", 20*time.Second, "", long, "",
		func(rows []messages.SenderView) bool { return len(rows) > 0 })
	e.openChannel(digest)
	e.settle(800 * time.Millisecond)
	logs := captureLog(t)
	calls := e.recordCalls("conversations.history")
	e.clickAt(e.linkPoint(t, digest, link), time.Second)
	e.waitDrawn(t, "the old message, highlighted", 30*time.Second, "", long, "",
		func(rows []messages.SenderView) bool { return lit(rows, target) })
	if r := e.a.router.Current(); r != Route("c/"+long+"/m/"+target) {
		t.Errorf("route %q, want the conversation at the message (c/%s/m/%s)", r, long, target)
	}
	var got []string
	for _, c := range calls.of("conversations.history", long, "latest", "oldest", "inclusive", "limit") {
		if strings.HasPrefix(c, "latest= ") && !strings.Contains(c, "oldest="+target+" ") {
			continue // core's poll of the newest page (no socket in tests), not the jump's
		}
		got = append(got, c)
	}
	slices.Sort(got)
	want := []string{
		"latest= oldest=" + target + " inclusive=true limit=28",
		"latest=" + target + " oldest= inclusive=true limit=28",
	}
	if !slices.Equal(got, want) {
		t.Errorf("conversations.history for the jump: %d calls %q\nwant the window around the message: %q", len(got), got, want)
	}
	noRawIDs(t, logs, long, target, digest)
}

// TestSplitViewLinkKeepsBackStack: a click on a reply's link (no
// thread_ts) in the split view, looked up with messages.list, goes on
// the view's back stack (OpenSplit from inside it), and the split view's
// thread starts at the reply, highlighted (toThread's replyTs in the
// Split container); the main window stays as it was. Clack ran the
// lookup's navigation outside the split view: the main window navigated
// and the split view's thread opened at its top.
func TestSplitViewLinkKeepsBackStack(t *testing.T) {
	e := threadHeaderEnv(t, false)
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)
	digest, main := fakeslack.ThreadPrivateID, fakeslack.ThreadDMID
	link := digestLink(pub, reply, "")
	postDigest(e.fs, digest, link)
	e.openChannel(main)
	e.settle(300 * time.Millisecond)
	e.a.OpenSplit("c/" + digest)
	e.settle(800 * time.Millisecond)
	e.clickAt(e.linkPointIn(t, paneSplit, digest, link), time.Second)
	want := []Route{Route("c/" + digest), Route("c/" + pub + "/t/" + root)}
	deadline := time.Now().Add(20 * time.Second)
	for !slices.Equal(e.a.split.stack, want) {
		if time.Now().After(deadline) {
			t.Fatalf("the split view's stack %q, want %q (the link's thread on its back stack); %s", e.a.split.stack, want, e.openState())
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
	e.waitDrawn(t, "the split view's thread at the reply", 10*time.Second, paneSplit, pub, root, func(rows []messages.SenderView) bool {
		return threadDrawn(rows, root) && lit(rows, reply)
	})
	if rows := e.paneRows(paneSplit, pub, reply); len(rows) > 0 {
		t.Errorf("the split view drew the reply as a thread's root: %s", drawnState(rows))
	}
	if r := e.a.router.Current(); r != Route("c/"+main) {
		t.Errorf("the main window navigated to %q, want it left at c/%s", r, main)
	}
	if s := e.a.secondary; s.kind == secThread {
		t.Errorf("the thread opened in the main window's pane too: %s", e.openState())
	}
}

// TestReplyRouteLeavesNoHistory: a route naming a reply as its thread
// (a slack://…thread_ts=<reply> link, Back to one) while its parent's
// thread is open shows the thread at the reply, highlighted, and the
// route becomes the thread's: no history entry names the reply as a
// thread. Clack's pane stayed as it was and kept c/<ch>/t/<reply> as the
// route.
func TestReplyRouteLeavesNoHistory(t *testing.T) {
	e := threadHeaderEnv(t, false)
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)
	e.openChannel(pub)
	e.a.OpenThread(pub, root)
	e.waitDrawn(t, "the thread", 15*time.Second, "", pub, root, func(rows []messages.SenderView) bool { return threadDrawn(rows, root) })
	before := slices.Clone(e.a.router.stack)
	e.a.Navigate("c/" + pub + "/t/" + reply)
	e.frame()
	if r := e.a.router.Current(); r != Route("c/"+pub+"/t/"+root) {
		t.Errorf("route %q, want the reply's thread c/%s/t/%s", r, pub, root)
	}
	if !slices.Equal(e.a.router.stack, before) {
		t.Errorf("history %q, want it as it was (%q): the route naming the reply leaves no entry, and Back doesn't step onto the thread again",
			e.a.router.stack, before)
	}
	e.waitDrawn(t, "the reply highlighted in its thread", 10*time.Second, "", pub, root, func(rows []messages.SenderView) bool {
		return threadDrawn(rows, root) && lit(rows, reply)
	})
	if c := e.a.convos[pub+"/"+reply]; c != nil {
		t.Errorf("a thread pane was made for the reply %s", reply)
	}
}

// TestReplyRootedPaneFetchesThreadOnce: a pane opened with a reply's ts
// core doesn't hold (slack://channel?thread_ts=<reply>) asks
// conversations.replies for it once; the answer, its parent's thread from
// the reply on, goes under the parent's ts, so the parent's pane it hands
// on to starts at the reply with that page (threadStartAt) and reads it
// no more: what it reads after is a page before the reply (loadPre, the
// replies between the root and the reply, as a thread reads a page at a
// time), and nothing is kept under the reply's ts. Clack fetched the
// thread twice and kept a copy under the reply.
func TestReplyRootedPaneFetchesThreadOnce(t *testing.T) {
	e := threadHeaderEnv(t, false)
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)
	e.openChannel(fakeslack.ThreadPrivateID)
	e.settle(300 * time.Millisecond)
	calls := e.recordCalls("conversations.replies", "client.counts")
	// start right after one of core's polls (no socket in tests: every
	// PollInterval it refetches the threads opened in the last 15
	// minutes), so the replies requests that follow are the panes'
	polls := len(calls.of("client.counts", ""))
	e.until("one of core's polls", core.PollInterval+10*time.Second, func() bool { return len(calls.of("client.counts", "")) > polls })
	e.settle(time.Second) // the rest of that poll
	e.a.OpenURL("slack://channel?team=" + fakeslack.TeamID + "&id=" + pub + "&thread_ts=" + reply)
	e.waitParentThread(t, "slack://channel?thread_ts=<a reply>", pub, root, reply)
	e.settle(500 * time.Millisecond)
	got := calls.of("conversations.replies", pub, "ts", "oldest", "latest")
	if len(got) == 0 || !strings.HasPrefix(got[0], "ts="+reply+" ") {
		t.Errorf("conversations.replies for %s: %q, want the first for the reply's ts (it answers its parent's thread)", pub, got)
	}
	for _, c := range got[min(1, len(got)):] {
		latest, ok := strings.CutPrefix(c, "ts="+root+" oldest= latest=")
		if !ok || latest == "" || tsBefore(reply, latest) {
			t.Errorf("conversations.replies for %s: %q; after the hand-on only a page before the reply (ts=%s, latest at or before %s)", pub, got, root, reply)
			break
		}
	}
	if th, ok := e.a.Core().S.Thread(pub, reply); ok {
		t.Errorf("core keeps %d messages under the reply's ts as a thread of its own", len(th))
	}
}

// TestThreadOnlyLinksOpenTheThread: a link naming only a thread is
// toThread: /archives/<C>?thread_ts= (openPermalink's `if (ed || !i)`
// branch) and slack://channel?thread_ts= open the thread with its root
// highlighted in the conversation (highlightRoot). message=latest (and
// /archives/<C>/latest?thread_ts=) is the thread's newest reply under
// thread_latest_reply_permalink (cdTA Mz: conversations.replies limit 1,
// the root's latest_reply), else the thread at its root; and a slack://
// link in a message goes to handleDeepLink (Link, jRB6). Clack opened
// /archives/<C>?thread_ts= as the conversation, the deep links without
// highlightRoot, message=latest not at all.
func TestThreadOnlyLinksOpenTheThread(t *testing.T) {
	pub, root, latest := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)
	deep := "slack://channel?team=" + fakeslack.TeamID + "&id=" + pub
	for _, c := range []struct {
		name, link string
		inMessage  bool   // clicked in a message (else the shell hands Clack the link)
		experiment bool   // thread_latest_reply_permalink on
		at         string // the reply the thread starts at, highlighted
	}{
		{name: "/archives/C?thread_ts=", link: "https://fakeclub.slack.com/archives/" + pub + "?thread_ts=" + root + "&cid=" + pub, inMessage: true},
		{name: "slack://channel?thread_ts=", link: deep + "&thread_ts=" + root},
		{name: "message=latest, experiment unassigned", link: deep + "&thread_ts=" + root + "&message=latest"},
		{name: "message=latest", link: deep + "&thread_ts=" + root + "&message=latest", experiment: true, at: latest},
		{name: "/archives/C/latest", link: "https://fakeclub.slack.com/archives/" + pub + "/latest?thread_ts=" + root, inMessage: true,
			experiment: true, at: latest},
		{name: "slack:// in a message", link: deep + "&message=" + latest, inMessage: true, at: latest},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := threadHeaderEnv(t, false)
			if c.experiment {
				e.a.Core().SetExperimentGroup("thread_latest_reply_permalink", "on")
			}
			digest := fakeslack.ThreadPrivateID
			if c.inMessage {
				postDigest(e.fs, digest, c.link)
			}
			e.openChannel(digest)
			e.settle(800 * time.Millisecond)
			if c.inMessage {
				e.clickAt(e.linkPoint(t, digest, c.link), time.Second)
			} else {
				e.a.OpenURL(c.link)
			}
			e.waitDrawn(t, c.name, 20*time.Second, "", pub, root, func(rows []messages.SenderView) bool {
				return threadDrawn(rows, root) && (c.at == "" || lit(rows, c.at))
			})
			if r := e.a.router.Current(); r != Route("c/"+pub+"/t/"+root) {
				t.Errorf("route %q, want the thread's c/%s/t/%s", r, pub, root)
			}
			e.waitDrawn(t, c.name+": the root highlighted in the conversation (highlightRoot)", 10*time.Second, "", pub, "",
				func(rows []messages.SenderView) bool { return lit(rows, root) })
		})
	}
}

// TestForeignWorkspaceLinkOpensBrowser: a permalink into another
// workspace is jumpToMessage's "Could not find suitable target
// workspace", which openPermalink opens in a browser tab, and the window
// stays. The fake is an Enterprise Grid workspace, where a link with
// another of the org's subdomains can name any of the org's conversations
// (wc0P finds the team by the channel): it's looked up first
// (conversations.info), and only one the lookup can't find goes to the
// browser; one it finds opens in the client, as does a conversation
// Clack knows whatever the subdomain (a Slack Connect channel's link from
// the partner's workspace). Clack asked messages.list for the foreign
// message and navigated to a conversation it didn't have (round 1), then
// sent every link with another subdomain to the browser (round 2).
func TestForeignWorkspaceLinkOpensBrowser(t *testing.T) {
	e := threadHeaderEnv(t, false)
	b := catchBrowser(t)
	digest, shared, root := fakeslack.ThreadPrivateID, fakeslack.ThreadSharedID, fakeslack.ThreadRootTS
	foreign := "https://otherco.slack.com/archives/C0FOREIGN01/p1762516060000100"
	known := "https://partnerco.slack.com/archives/" + shared + "/p" + strings.ReplaceAll(root, ".", "")
	org := "https://orgsibling.slack.com/archives/" + fakeslack.ThreadElsewhereID + "/p" + strings.ReplaceAll(root, ".", "")
	postDigest(e.fs, digest, foreign, known, org)
	e.openChannel(digest)
	e.settle(800 * time.Millisecond)
	calls := e.recordCalls("messages.list", "conversations.info")
	e.clickAt(e.linkPoint(t, digest, foreign), time.Second)
	e.until("the browser tab", 10*time.Second, func() bool { return len(b.got()) > 0 })
	e.settle(300 * time.Millisecond)
	if got := b.got(); !slices.Equal(got, []string{foreign}) {
		t.Errorf("opened in the browser: %q, want the foreign link", got)
	}
	if r := e.a.router.Current(); r != Route("c/"+digest) {
		t.Errorf("the foreign link navigated to %q", r)
	}
	if asked := calls.of("messages.list", "", "message_ids"); len(asked) > 0 {
		t.Errorf("the foreign message was looked up: %q", asked)
	}
	if asked := calls.of("conversations.info", "", "channel"); !slices.Contains(asked, "channel=C0FOREIGN01") {
		t.Errorf("conversations.info %q, want the foreign conversation looked up first (Grid)", asked)
	}
	// the shared channel's link from the partner's workspace: Clack's
	e.clickAt(e.linkPoint(t, digest, known), 2*time.Second)
	e.waitDrawn(t, "the Slack Connect channel's thread", 20*time.Second, "", shared, root, func(rows []messages.SenderView) bool {
		return threadDrawn(rows, root)
	})
	// a conversation of the org's another workspace has, found by the lookup
	e.openChannel(digest)
	e.settle(500 * time.Millisecond)
	e.clickAt(e.linkPoint(t, digest, org), 3*time.Second)
	e.waitDrawn(t, "the org's conversation's thread", 20*time.Second, "", fakeslack.ThreadElsewhereID, root, func(rows []messages.SenderView) bool {
		return threadDrawn(rows, root)
	})
	if got := b.got(); len(got) != 1 {
		t.Errorf("Clack's conversations' links opened in the browser too: %q", got)
	}
}

// TestNavigationLogsTagIDs: clack.log names no conversation or message
// as is (docs/INSPECT.md's tags). A reply's link whose lookup fails
// (messages.list errors: Slack navigates without a thread) logs the
// failure with tags, and the request's own line names its channels by
// tag; a deep link Clack doesn't handle logs its command only. Clack
// logged the request's raw channel ids next to the tagged jump line and
// the unhandled link's path.
func TestNavigationLogsTagIDs(t *testing.T) {
	e := threadHeaderEnv(t, false)
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(0)
	digest := fakeslack.ThreadPrivateID
	link := digestLink(pub, reply, "")
	postDigest(e.fs, digest, link)
	e.openChannel(digest)
	e.settle(800 * time.Millisecond)
	logs := captureLog(t)
	e.fs.SetHook(func(method string, _ url.Values) (any, bool) {
		switch method {
		case "messages.list", "bookmarks.list", "pins.list": // the lookup's, the bookmark bar's, the pins tab's
			return map[string]any{"ok": false, "error": "fatal_error"}, true
		}
		return nil, false
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	e.clickAt(e.linkPoint(t, digest, link), time.Second)
	e.until("the jump without its thread", 20*time.Second, func() bool {
		return e.a.router.Current() == Route("c/"+pub+"/m/"+reply)
	})
	if lines := logs.matching("ui: jumpToMessage: " + logTag(reply) + "'s thread_ts wasn't found"); len(lines) != 1 {
		t.Errorf("clack.log's lookup failures: %q, want one, tagged", logs.matching("ui: jumpToMessage: "))
	}
	if lines := logs.matching("messages.list: "); len(lines) == 0 || !strings.Contains(lines[0], logTag(pub)) {
		t.Errorf("clack.log's messages.list failure %q, want its channel's tag", lines)
	}
	e.a.OpenURL("slack://bogus/" + pub + "?id=" + pub + "&message=" + reply)
	e.until("the unhandled deep link's line", 10*time.Second, func() bool {
		return len(logs.matching("ui: deep link not handled: ")) > 0
	})
	if got := logs.matching("ui: deep link not handled: "); len(got) != 1 || got[0] != "ui: deep link not handled: slack://bogus" {
		t.Errorf("clack.log's unhandled deep link: %q, want its command only", got)
	}
	// a team-scoped link names the workspace in its host: by tag
	e.a.OpenURL("slack://" + fakeslack.TeamID + "/magic-login/synthetic")
	e.until("the team-scoped link's line", 10*time.Second, func() bool {
		return len(logs.matching("ui: deep link not handled: ")) > 1
	})
	if got := logs.matching("ui: deep link not handled: "); got[1] != "ui: deep link not handled: slack://"+logTag(fakeslack.TeamID) {
		t.Errorf("clack.log's team-scoped link: %q, want its workspace's tag", got[1])
	}
	noRawIDs(t, logs, pub, root, reply, digest, fakeslack.TeamID)
}
