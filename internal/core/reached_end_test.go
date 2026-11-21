package core

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// After a socket gap the open conversation holds every message, like
// Slack's, and the others' unread state catches up: Karim's #clack on
// α.112 (2026-11-20) drew 6:54 PM and then 9:53 PM with the three hours
// between missing and nothing to say so. The socket had gone away, and
// either nothing noticed (Clack pinged and never looked for the pong:
// Slack closes a socket 90 s after the last one, client-boot Kot3), or it
// came back and nothing read what it missed: the list reloaded core's
// cached window, and the socket's next message went right after it.
// Slack's onConnected resets every channel's reachedEnd at a slow
// connect (boot-shared erkq, client-boot 6Yed), and the open view reads
// newer messages from its slice's end (AEjl requestNewer), or the newest
// page when more than a page is missing. Each case: a socket gap while
// messages arrive in the open channel and another, then a message after
// it; the open channel's window must hold every message Slack has from
// its oldest to the newest, and the other channel must be unread.
// Synthetic data only.

type gapEnv struct {
	t     *testing.T
	a     *App
	fs    *fakeslack.Server
	log   *eventLog
	clk   *clock2 // the test's clock, when it runs on one
	ch    string  // the open conversation
	other string  // another, read when the test starts, nobody looks at
	user  string  // who posts
	n     int     // messages posted
}

func newGapEnv(t *testing.T, clock bool) *gapEnv {
	t.Helper()
	e := &gapEnv{t: t}
	if clock {
		e.clk = &clock2{wall: time.Date(2026, 11, 20, 0, 54, 0, 0, time.UTC)}
		t.Cleanup(slack.SetClock(e.clk.read))
	}
	// Slack's 90 s without a pong, but a ping (and so a check) every
	// 25 ms instead of 10 s, so the test's clock steps are noticed at once
	pingTimers(t, 25*time.Millisecond, 90*time.Second)
	oldPoll, oldProbe, oldFP := clientOnlinePoll, clientOnlineProbe, networkFingerprint
	clientOnlinePoll, clientOnlineProbe = 20*time.Millisecond, func() bool { return true }
	networkFingerprint = func() string { return "en0=10.20.0.7" }
	t.Cleanup(func() { clientOnlinePoll, clientOnlineProbe, networkFingerprint = oldPoll, oldProbe, oldFP })

	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.CountsFollowPosts(true) // client.counts sees what the test posts, as Slack's does
	e.fs = fs
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	evs, stop := a.Subscribe()
	t.Cleanup(stop)
	e.log = &eventLog{}
	go func() {
		for ev := range evs {
			e.log.add(ev)
		}
	}()
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.a = a
	ids := fs.ChannelIDs()
	e.ch = ids[1]
	for _, id := range ids[2:] {
		if c := a.Count(id); !c.Unread && c.Mentions == 0 {
			e.other = id
			break
		}
	}
	if e.other == "" {
		t.Fatal("no read channel to post into")
	}
	e.user = fs.UserIDs()[3]
	if _, err := a.History(context.Background(), e.ch, ""); err != nil { // the conversation on screen
		t.Fatal(err)
	}
	a.SetRealtime(true)
	e.waitUp(1)
	e.live("before the gap")
	return e
}

// ups is how many times the socket has connected.
func (e *gapEnv) ups() int {
	n := 0
	for _, ev := range e.log.all() {
		if ev.Type == "connection" && ev.Connected {
			n++
		}
	}
	return n
}

// waitUp waits for the socket's n-th connect, and for the fake to have
// the socket (Push reaches only sockets it holds).
func (e *gapEnv) waitUp(n int) {
	e.t.Helper()
	dl := time.Now().Add(15 * time.Second)
	for e.ups() < n || e.fs.Sockets() == 0 {
		if time.Now().After(dl) {
			e.t.Fatalf("the socket never came back (%d connects, want %d; %d sockets); events: %s", e.ups(), n, e.fs.Sockets(), evTypes(e.log.all()))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitDown waits until the client knows its socket is gone.
func (e *gapEnv) waitDown(downs int) {
	e.t.Helper()
	dl := time.Now().Add(15 * time.Second)
	for {
		n := 0
		for _, ev := range e.log.all() {
			if ev.Type == "connection" && !ev.Connected {
				n++
			}
		}
		if n >= downs {
			return
		}
		if time.Now().After(dl) {
			e.t.Fatalf("the client never noticed its socket was gone; events: %s", evTypes(e.log.all()))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// post puts a message into ch's history and sends its event to whatever
// sockets are there to hear it, as Slack's gateway does.
func (e *gapEnv) post(ch, what string) string {
	e.n++
	ts, ev := e.fs.PostQuiet(ch, e.user, fmt.Sprintf("synthetic message %d: %s", e.n, what))
	e.fs.Push(ev)
	return ts
}

// live posts into the open conversation and waits for the window to hold
// the message.
func (e *gapEnv) live(what string) string {
	e.t.Helper()
	ts := e.post(e.ch, what)
	dl := time.Now().Add(10 * time.Second)
	for !e.holds(ts) {
		if time.Now().After(dl) {
			e.t.Fatalf("the window never took the live message %s (%q)", ts, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return ts
}

func (e *gapEnv) holds(ts string) bool {
	if tl := e.a.S.Timeline(e.ch); tl != nil {
		for i := range tl.Msgs {
			if tl.Msgs[i].TS == ts {
				return true
			}
		}
	}
	return false
}

// gap posts n messages into the open conversation and two into the other
// while the socket is away, and returns the open one's.
func (e *gapEnv) gap(n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, e.post(e.ch, "while the socket was away"))
	}
	e.post(e.other, "in another channel while the socket was away")
	e.post(e.other, "and another")
	return out
}

// missing is what the open conversation's window lacks of the messages
// Slack's history has from the window's oldest message to the newest.
func (e *gapEnv) missing() (miss []string, held int) {
	tl := e.a.S.Timeline(e.ch)
	if tl == nil {
		return []string{"(no window)"}, 0
	}
	have := map[string]bool{}
	for i := range tl.Msgs {
		have[tl.Msgs[i].TS] = true
	}
	all := e.fs.HistoryTS(e.ch)
	from := len(all)
	for i, ts := range all {
		if have[ts] {
			from = i
			break
		}
	}
	for _, ts := range all[from:] {
		if !have[ts] {
			miss = append(miss, ts)
		}
	}
	return miss, len(tl.Msgs)
}

// whole waits for the open conversation's window to hold every message
// from its oldest to the newest, the must ones among them, and for the
// other conversation to be unread.
func (e *gapEnv) whole(must ...string) {
	e.t.Helper()
	dl := time.Now().Add(10 * time.Second)
	for {
		miss, held := e.missing()
		lacks := []string{}
		for _, ts := range must {
			if !e.holds(ts) {
				lacks = append(lacks, ts)
			}
		}
		if len(miss) == 0 && len(lacks) == 0 {
			break
		}
		if time.Now().After(dl) {
			e.t.Fatalf("after the gap the open channel's window (%d messages) has a hole: %d of Slack's messages from its oldest to the newest missing %s, %d of the gap's and later ones %s",
				held, len(miss), strings.Join(miss, " "), len(lacks), strings.Join(lacks, " "))
		}
		time.Sleep(10 * time.Millisecond)
	}
	for dl := time.Now().Add(10 * time.Second); !e.a.Count(e.other).Unread; {
		if time.Now().After(dl) {
			e.t.Fatalf("the other channel %s isn't unread after the gap (%+v)", e.other, e.a.Count(e.other))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A socket that goes silent for minutes (no pong, no close: a lost NAT
// mapping, a Wi-Fi network gone) while messages arrive is closed once 90 s
// pass without a pong, and the client connects again and reads what it
// missed.
func TestSocketGapSilent(t *testing.T) {
	e := newGapEnv(t, true)
	ups := e.ups()
	e.fs.StallSockets()
	missed := e.gap(6)
	e.clk.tick(4 * time.Minute) // four minutes awake, and not a word from the socket
	e.waitUp(ups + 1)
	after := e.live("after the socket came back")
	e.whole(append(missed, after)...)
}

// A socket cut without a close handshake (a network drop): the client
// connects again at once, slow (no reconnect_url), and the messages sent
// while it was away aren't lost.
func TestSocketGapDropped(t *testing.T) {
	e := newGapEnv(t, false)
	ups := e.ups()
	e.fs.DropSockets()
	e.waitDown(1)
	missed := e.gap(6)
	e.waitUp(ups + 1)
	after := e.live("after the reconnect")
	e.whole(append(missed, after)...)
}

// A laptop asleep for 25 minutes: the socket goes quiet with it, and on
// the wake the client connects again (online.go's wake check, and the
// pong timeout on the wall clock) and reads what was posted while it
// slept.
func TestSocketGapWake(t *testing.T) {
	e := newGapEnv(t, true)
	ups := e.ups()
	e.fs.StallSockets() // asleep: nothing comes, nothing is answered
	missed := e.gap(6)
	e.clk.sleep(25 * time.Minute)
	e.waitUp(ups + 1)
	after := e.live("after the wake")
	e.whole(append(missed, after)...)
}

// The window paged back to an old message (a jump from search, a link or
// Activity pages back until it holds it), then a gap and live messages:
// the window keeps the old messages and gains the gap's, with no hole
// between, and the message jumped to is still there.
func TestSocketGapJump(t *testing.T) {
	e := newGapEnv(t, false)
	ctx := context.Background()
	for i := 0; i < 3; i++ { // page back to the channel's first messages
		tl := e.a.S.Timeline(e.ch)
		if tl == nil || !tl.HasMore {
			break
		}
		if _, err := e.a.History(ctx, e.ch, tl.Msgs[0].TS); err != nil {
			t.Fatal(err)
		}
	}
	target := e.fs.HistoryTS(e.ch)[3]
	if !e.holds(target) {
		t.Fatalf("paging back didn't reach the message jumped to (%s)", target)
	}
	e.live("after the jump")
	ups := e.ups()
	e.fs.DropSockets()
	e.waitDown(1)
	missed := e.gap(5)
	e.waitUp(ups + 1)
	after := e.live("after the reconnect")
	e.whole(append(missed, after, target)...)
}

// More messages than a page while the socket was away: the newest page
// starts the window over (Slack's newest view does), with no hole in it
// either, and the message after the gap joins it.
func TestSocketGapMoreThanAPage(t *testing.T) {
	e := newGapEnv(t, false)
	ups := e.ups()
	e.fs.DropSockets()
	e.waitDown(1)
	missed := e.gap(60)
	e.waitUp(ups + 1)
	after := e.live("after the reconnect")
	// the newest page (50) holds the gap's newest; the window has no hole
	// from its oldest on
	e.whole(append(missed[len(missed)-40:], after)...)
}

// holdNewest holds the open conversation's next newest-page read
// (conversations.history with neither oldest nor latest) until release:
// held is closed once the read is in. The catch-up's forward read
// (oldest) goes through.
func (e *gapEnv) holdNewest() (held <-chan struct{}, release func()) {
	in, out := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "conversations.history" && form.Get("channel") == e.ch && form.Get("oldest") == "" && form.Get("latest") == "" {
			once.Do(func() {
				close(in)
				<-out
			})
		}
		return nil, false
	})
	var rel sync.Once
	release = func() { rel.Do(func() { close(out) }) }
	e.t.Cleanup(func() { release(); e.fs.SetHook(nil) })
	return in, release
}

// The viewer's own send, sent and confirmed while the catch-up's newest
// page is out (more than a page missed, so the window starts over at
// the newest page), stays in the window: a send goes into the window past
// whatever the socket missed, never waits, and no page read before it
// holds it. The window the page made used to drop it (the review's
// blocker): the message was gone from core and the list.
func TestSocketGapOwnSendDuringNewestRead(t *testing.T) {
	e := newGapEnv(t, false)
	ctx := context.Background()
	ups := e.ups()
	e.fs.DropSockets()
	e.waitDown(1)
	missed := e.gap(40) // more than the forward page (28): the newest page is read
	held, release := e.holdNewest()
	e.waitUp(ups + 1)
	select {
	case <-held:
	case <-time.After(15 * time.Second):
		t.Fatal("the catch-up never read the newest page")
	}
	m, err := e.a.Send(ctx, SendOpts{Channel: e.ch, Text: "synthetic: mine, sent while the newest page was out"})
	if err != nil {
		t.Fatal(err)
	}
	if !e.holds(m.TS) {
		t.Fatalf("the confirmed send %s isn't in the window before the page lands", m.TS)
	}
	release()
	after := e.live("after the catch-up")
	e.whole(append(missed[len(missed)-20:], m.TS, after)...)
}

// A send that failed before the gap (its Retry row) stays in the window
// through a catch-up that starts over at the newest page: no race needed.
func TestSocketGapFailedSendSurvives(t *testing.T) {
	e := newGapEnv(t, false)
	ctx := context.Background()
	e.fs.SetFault("chat.postMessage", fakeslack.Fault{Error: "internal_error"})
	if _, err := e.a.Send(ctx, SendOpts{Channel: e.ch, Text: "synthetic: mine, it failed"}); err == nil {
		t.Fatal("the send didn't fail")
	}
	e.fs.SetFault("chat.postMessage", fakeslack.Fault{})
	failed := ""
	if tl := e.a.S.Timeline(e.ch); tl != nil {
		for i := range tl.Msgs {
			if tl.Msgs[i].Text == "synthetic: mine, it failed" {
				failed = tl.Msgs[i].TS
			}
		}
	}
	if failed == "" {
		t.Fatal("no failed copy in the window")
	}
	ups := e.ups()
	e.fs.DropSockets()
	e.waitDown(1)
	missed := e.gap(40)
	e.waitUp(ups + 1)
	after := e.live("after the catch-up")
	e.whole(append(missed[len(missed)-20:], failed, after)...)
}
