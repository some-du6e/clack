package ui

// Karim, 2026-11-19 (α.111/α.112): "unknown author for dms", in "dm's
// messages". A DM's rows name their senders the way Slack's do: the
// connected MessageSender (boot-shared JA+h) reads the member through
// getMemberById (client-boot DOS4 nv), whose UnknownFetchManager (0mwJ,
// made in DOS4 H) fetches a member the store doesn't hold from the first
// lookup on (its isEnabled is on outside Slack's test runners), and
// BaseMessageSender (AC4g) draws BaseMissingMember (MJkf) for the stub
// ("unknown") and for a member users/info didn't return ("non-existent",
// unless something names it). A message with no user, no username and no
// bot is isUnknown too (JA+h's z stays true): the bar, never a word.
//
// Each case opens a DM the way the viewer can (its route, as the sidebar
// and the switcher do; a notification's jump; a launch that restores it),
// with its partner a member nothing has fetched yet. Synthetic people and
// text only.

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// dmSenders are the senders the DM's list drew in the last frame, by ts.
func dmSenders(e *perfEnv, ch string) map[string]messages.SenderView {
	out := map[string]messages.SenderView{}
	cv := e.a.convos[ch+"/"]
	if cv == nil {
		return out
	}
	l, ok := cv.list.(channelList)
	if !ok {
		return out
	}
	for _, sv := range l.Senders() {
		out[sv.TS] = sv
	}
	return out
}

// dmSendersDump is what the DM's rows drew, for a failure.
func dmSendersDump(e *perfEnv, ch string, tss ...string) string {
	s := dmSenders(e, ch)
	var out []string
	for _, ts := range tss {
		sv, ok := s[ts]
		if !ok {
			out = append(out, ts+": not drawn")
			continue
		}
		out = append(out, fmt.Sprintf("%s: user %q name %q missing %d", ts, sv.User, sv.Name, sv.Missing))
	}
	return strings.Join(out, "; ")
}

// memberNameOf is the name a row draws for member id as the store holds
// them ("" when it doesn't).
func memberNameOf(c *core.App, id string) string {
	u, ok := c.S.RawUser(id)
	if !ok {
		return ""
	}
	return store.DisplayName(u)
}

// usersInfoLog records the ids edge users/info is asked for; hold, when
// set, makes a request that asks for it wait until release.
type usersInfoLog struct {
	mu    sync.Mutex
	asked map[string]int
	hold  map[string]bool
	gate  chan struct{}
	once  sync.Once
}

func watchUsersInfo(t *testing.T, fs *fakeslack.Server, hold ...string) *usersInfoLog {
	l := &usersInfoLog{asked: map[string]int{}, hold: map[string]bool{}, gate: make(chan struct{})}
	for _, id := range hold {
		l.hold[id] = true
	}
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method != "edge/users/info" {
			return nil, false
		}
		var up map[string]int
		_ = json.Unmarshal(body["updated_ids"], &up)
		held := false
		l.mu.Lock()
		for id := range up {
			l.asked[id]++
			held = held || l.hold[id]
		}
		l.mu.Unlock()
		if held {
			<-l.gate
		}
		return nil, false
	})
	t.Cleanup(func() { l.release(); fs.SetEdgeHook(nil) }) // before fs.Close: a held request has to answer
	return l
}

func (l *usersInfoLog) release() { l.once.Do(func() { close(l.gate) }) }

func (l *usersInfoLog) wasAsked(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.asked[id] > 0
}

// dmFixture is a closed DM (no sidebar row, so boot fetches nobody for
// it) with partner, holding a message from the partner and one from the
// viewer, newest last.
type dmFixture struct {
	ch, partner      string
	fromPartner, own string
}

func addDMFixture(fs *fakeslack.Server, ch, partner string) dmFixture {
	fs.AddClosedDM(ch, partner)
	f := dmFixture{ch: ch, partner: partner}
	f.fromPartner, _ = fs.PostQuiet(ch, partner, "a synthetic line from the partner")
	f.own, _ = fs.PostQuiet(ch, fakeslack.SelfID, "the viewer's synthetic answer")
	return f
}

// waitNamed waits for the DM's partner's and the viewer's messages to be
// drawn with their names (want: the partner's), never a bar, an id or
// "Unknown".
func waitNamed(t *testing.T, e *perfEnv, f dmFixture, what string, d time.Duration) {
	t.Helper()
	c := e.a.Core()
	dl := time.Now().Add(d)
	for {
		e.frame()
		s := dmSenders(e, f.ch)
		p, own := s[f.fromPartner], s[f.own]
		pn, sn := memberNameOf(c, f.partner), memberNameOf(c, fakeslack.SelfID)
		if pn != "" && sn != "" && p.Name == pn && p.Missing == core.MemberKnown && own.Name == sn && own.Missing == core.MemberKnown {
			return
		}
		if time.Now().After(dl) {
			t.Fatalf("%s: the DM's senders after %v: %s (the store names the partner %q, the viewer %q)", what, d,
				dmSendersDump(e, f.ch, f.fromPartner, f.own), pn, sn)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// noUnknownWord fails when a frame draws Clack's old "Unknown" sender or
// one of ids: Slack draws neither (BaseMissingMember adds no text).
func noUnknownWord(t *testing.T, e *perfEnv, f dmFixture, what string, ids ...string) {
	t.Helper()
	for ts, sv := range dmSenders(e, f.ch) {
		if sv.Name == "Unknown" {
			t.Errorf("%s: message %s's sender is drawn as %q (user %q, missing %d)", what, ts, sv.Name, sv.User, sv.Missing)
		}
		for _, id := range ids {
			if id != "" && strings.Contains(sv.Name, id) {
				t.Errorf("%s: message %s's sender draws the id %s (%q)", what, ts, id, sv.Name)
			}
		}
	}
}

// dmToLatest scrolls the open DM to its newest message and waits for the
// fixture's rows.
func dmToLatest(t *testing.T, e *perfEnv, f dmFixture, what string) {
	t.Helper()
	e.until(what+": the DM's history", 20*time.Second, func() bool { return e.loadedMessages(f.ch) > 0 })
	e.toLatest()
	e.until(what+": the fixture's rows", 20*time.Second, func() bool {
		s := dmSenders(e, f.ch)
		_, a := s[f.fromPartner]
		_, b := s[f.own]
		return a && b
	})
}

func TestDMSendersLikeSlack(t *testing.T) {
	// Opened once the client is up (the sidebar's and the switcher's
	// route): the partner's row is the unknown stub while users/info is
	// out, the viewer's is named, and the name follows the answer with
	// no other input.
	t.Run("opened", func(t *testing.T) {
		fs, err := fakeslack.Start(fakeslack.Small)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
		partner := fs.UserIDs()[61]
		f := addDMFixture(fs, "D0DMUAOPEN1", partner)
		l := watchUsersInfo(t, fs, partner)
		e := startEnv(t, fs, t.TempDir())
		e.waitReady()
		c := e.a.Core()
		if _, ok := c.S.RawUser(partner); ok {
			t.Fatal("the partner was fetched before the DM opened: the case checks nothing")
		}
		e.openChannel(f.ch)
		dmToLatest(t, e, f, "opened")
		e.until("users/info asked for the partner", 10*time.Second, func() bool { return l.wasAsked(partner) })
		e.settle(200 * time.Millisecond)
		if sv := dmSenders(e, f.ch)[f.fromPartner]; sv.Missing != core.MemberUnknown || sv.Name != "" {
			t.Errorf("held: the partner's row %+v, want BaseMissingMember's loading bar", sv)
		}
		if sv, n := dmSenders(e, f.ch)[f.own], memberNameOf(c, fakeslack.SelfID); n == "" || sv.Name != n {
			t.Errorf("held: the viewer's row %+v, want %q", sv, n)
		}
		noUnknownWord(t, e, f, "held", partner)
		l.release()
		waitNamed(t, e, f, "after users/info", 10*time.Second)
	})

	// A notification's jump (notificationClicked → jumpToMessage) to the
	// partner's message.
	t.Run("notification", func(t *testing.T) {
		fs, err := fakeslack.Start(fakeslack.Small)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
		f := addDMFixture(fs, "D0DMUANOTE1", fs.UserIDs()[62])
		e := startEnv(t, fs, t.TempDir())
		e.waitReady()
		e.a.notificationClicked(f.ch, f.fromPartner, "", "")
		e.frame()
		if got := e.a.router.Current().Channel(); got != f.ch {
			t.Fatalf("the notification opened %q, want %s", got, f.ch)
		}
		dmToLatest(t, e, f, "notification")
		waitNamed(t, e, f, "notification", 10*time.Second)
		noUnknownWord(t, e, f, "notification", f.partner)
	})

	// A launch that restores the DM (ui.json's route, a warm start) while
	// boot's calls are still out: Slack's UnknownFetchManager asks for a
	// member the first frame misses, whatever the boot is doing. The
	// partner isn't in the metadata cache (the last session never drew
	// the DM), and the header's conversations.info, whose EnsureUsers
	// names the partner on its own, is held too: the rows name the
	// partner through the lazy fetcher alone, as Slack's do.
	t.Run("restored", func(t *testing.T) {
		fs, err := fakeslack.Start(fakeslack.Small)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
		f := addDMFixture(fs, "D0DMUAREST1", fs.UserIDs()[63])
		home := freezeHome(t)
		e1 := startEnv(t, fs, home)
		e1.waitReady()
		e1.settle(300 * time.Millisecond)
		<-e1.a.data.stop() // meta.json and the boot data
		e1.shutdown()
		setUIRoute(t, home, "c/"+f.ch)
		release, held := holdCalls(t, fs, "users.conversations", "conversations.info") // boot can't finish, nor the header read
		e := startEnv(t, fs, home)
		c := e.a.Core()
		if _, ok := c.S.RawUser(f.partner); ok {
			t.Fatal("the partner is in the metadata cache: the case checks nothing")
		}
		dmToLatest(t, e, f, "restored")
		if held() == 0 {
			t.Fatal("users.conversations wasn't held: the case checks nothing")
		}
		waitNamed(t, e, f, "restored, boot still out", 10*time.Second)
		if !e.a.data.booted.IsZero() {
			t.Fatal("core finished booting while users.conversations was held")
		}
		noUnknownWord(t, e, f, "restored", f.partner)
		release()
		e.waitReady()
		waitNamed(t, e, f, "restored, booted", 10*time.Second)
	})

	// A partner users/info doesn't return: BaseMissingMember's
	// non-existent bar ("Private user info"), never the id or a word.
	t.Run("nonexistent", func(t *testing.T) {
		fs, err := fakeslack.Start(fakeslack.Small)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
		const ghost = "U0DMUAGHOST1" // no such member
		f := addDMFixture(fs, "D0DMUAGONE1", ghost)
		e := startEnv(t, fs, t.TempDir())
		e.waitReady()
		c := e.a.Core()
		e.openChannel(f.ch)
		dmToLatest(t, e, f, "nonexistent")
		e.until("the ghost's non-existent bar", 10*time.Second, func() bool {
			sv := dmSenders(e, f.ch)[f.fromPartner]
			return c.S.Nonexistent(ghost) && sv.Missing == core.MemberNonExistent && sv.Name == ""
		})
		if sv, n := dmSenders(e, f.ch)[f.own], memberNameOf(c, fakeslack.SelfID); sv.Name != n || n == "" {
			t.Errorf("the viewer's row %+v, want %q", sv, n)
		}
		noUnknownWord(t, e, f, "nonexistent", ghost)
	})

	// A DM message with no user, no username and no bot (JA+h: isUnknown
	// stays true): BaseMissingMember's bar, never "Unknown".
	t.Run("userless", func(t *testing.T) {
		fs, err := fakeslack.Start(fakeslack.Small)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
		f := addDMFixture(fs, "D0DMUANONE1", fs.UserIDs()[64])
		// a bot nothing returns (no username, no bot_profile): getBotById's
		// stub while bots.info is out, non-existent once it said nothing
		const ghostBot = "B0DMUAGHOST1"
		botTS := fs.PostBotQuiet(f.ch, ghostBot, "", "a synthetic line from a bot nobody knows")
		bare, _ := fs.PostQuiet(f.ch, "", "a synthetic line nobody signed")
		e := startEnv(t, fs, t.TempDir())
		e.waitReady()
		c := e.a.Core()
		e.openChannel(f.ch)
		dmToLatest(t, e, f, "userless")
		waitNamed(t, e, f, "userless", 10*time.Second)
		e.until("the bare message's row", 10*time.Second, func() bool { _, ok := dmSenders(e, f.ch)[bare]; return ok })
		if sv := dmSenders(e, f.ch)[bare]; sv.Missing != core.MemberUnknown || sv.Name != "" {
			t.Errorf("a message with no user, username or bot: %+v, want BaseMissingMember's unknown bar", sv)
		}
		e.until("the unknown bot's non-existent bar", 10*time.Second, func() bool {
			sv, ok := dmSenders(e, f.ch)[botTS]
			return ok && sv.Missing == core.MemberNonExistent && sv.Name == ""
		})
		if _, ok := c.S.Bot(ghostBot); ok || !botGone(c, ghostBot) {
			t.Errorf("the unknown bot: stored %v, non-existent %v", ok, botGone(c, ghostBot))
		}
		noUnknownWord(t, e, f, "userless", ghostBot)
	})
}
