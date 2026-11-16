package ui

// Switching around the app: a channel's page doesn't wait for its
// header's member count, a press on a sidebar row starts the channel's
// history read before the release (Slack's prefetchPointerdownHistory),
// and the view caches keep the views used most recently. Synthetic data
// (internal/fakeslack) only.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// The channel's messages show while edge users/counts is still out (core
// History's side read), and the header's count draws when it lands: the
// "member_count" event. conversations.info fails here, so the header's own
// read (backend.opened's ChannelInfo) can't be what brings it.
func TestHeaderCountLandsAfterPage(t *testing.T) {
	e, fs := organizeEnv(t, nil)
	// a member channel nothing opened yet (boot opens one; its header read
	// its count)
	ch := ""
	for _, s := range e.a.model().Sections {
		for _, id := range s.Rows {
			c := e.a.model().conv(id)
			if c == nil || c.Kind != "channel" || c.Preview || c.Archived || c.File || id == e.a.router.Current().Channel() {
				continue
			}
			if _, ok := e.a.data.core.MemberCountCached(id); !ok && ch == "" {
				ch = id
			}
		}
	}
	if ch == "" {
		t.Fatal("no member channel without a member count in the sidebar")
	}
	const people = 4321
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		var c string
		_ = json.Unmarshal(body["channel"], &c)
		if method != "edge/users/counts" || c != ch {
			return nil, false
		}
		<-hold
		return map[string]any{"ok": true, "counts": map[string]int{"people": people, "members": people, "everyone": people}}, true
	})
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "conversations.info" && form.Get("channel") == ch {
			return map[string]any{"ok": false, "error": "fatal_error"}, true
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil); fs.SetEdgeHook(nil) })

	e.openChannel(ch)
	e.until("the channel's messages while users/counts is out", 10*time.Second, func() bool {
		l, ok := e.a.convoFor(ch, "").list.(channelList)
		if !ok {
			return false
		}
		msgs, loaded, _ := l.LayoutSig()
		return loaded && msgs > 0
	})
	if c := e.a.model().conv(ch); c == nil || c.NumMembers == people {
		t.Fatalf("the header has users/counts' count before it answered: %+v", c)
	}
	release()
	e.until("the header's member count", 10*time.Second, func() bool {
		c := e.a.model().conv(ch)
		return c != nil && c.NumMembers == people
	})
	if !frameStrings(e)[itoa(people)] {
		t.Errorf("the header doesn't draw the count %d", people)
	}
}

// A primary press on a sidebar row starts the channel's history read
// before the release; the click's view takes that load (one
// conversations.history). A Shift press, the open conversation's row, and
// an account outside pointerdown_history_prefetch's treatment prefetch
// nothing.
func TestSidebarPressPrefetchesHistory(t *testing.T) {
	e, fs := organizeEnv(t, nil)
	c := e.a.Core()
	var mu sync.Mutex
	reads := map[string]int{}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "conversations.history" && form.Get("latest") == "" {
			mu.Lock()
			reads[form.Get("channel")]++
			mu.Unlock()
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	readsOf := func(ch string) int { mu.Lock(); defer mu.Unlock(); return reads[ch] }

	// read channel rows nothing loaded yet (unread ones may be prefetched
	// at boot), not the open one
	var cold []string
	for _, r := range e.a.sidebar.rows {
		if r.kind != sbConv {
			continue
		}
		cv := e.a.model().conv(r.key)
		if cv == nil || cv.Kind != "channel" || cv.Unread || cv.Preview || cv.Archived || cv.File ||
			r.key == e.a.router.Current().Channel() || c.S.Timeline(r.key) != nil || readsOf(r.key) > 0 {
			continue
		}
		cold = append(cold, r.key)
	}
	if len(cold) < 3 {
		t.Fatalf("%d cold channel rows, want 3", len(cold))
	}
	at := func(ch string) f32.Point {
		p := e.sbRowPoint(t, ch)
		return f32.Pt(float32(p.X), float32(p.Y))
	}
	press := func(pt f32.Point, mods key.Modifiers) {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Modifiers: mods})
		e.frame()
		e.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Modifiers: mods})
		e.frame()
	}
	release := func(pt f32.Point, mods key.Modifiers) {
		e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Modifiers: mods})
		e.frame()
		e.frame()
	}
	waitRead := func(ch string) bool {
		for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
			if readsOf(ch) > 0 {
				return true
			}
		}
		return false
	}
	noRead := func(ch, why string) {
		t.Helper()
		for dl := time.Now().Add(300 * time.Millisecond); time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
			e.frame()
		}
		if n := readsOf(ch); n > 0 {
			t.Errorf("%s: %d history reads for %s before the release", why, n, ch)
		}
	}

	// the account's group, then the press
	c.SetExperimentGroup("pointerdown_history_prefetch", "treatment")
	ch := cold[0]
	pt := at(ch)
	press(pt, 0)
	if !waitRead(ch) {
		t.Fatalf("the press on %s's row started no history read", ch)
	}
	if cur := e.a.router.Current().Channel(); cur == ch {
		t.Fatal("the press navigated before the release")
	}
	release(pt, 0)
	if cur := e.a.router.Current().Channel(); cur != ch {
		t.Fatalf("the click opened %q, want %s", cur, ch)
	}
	e.until("the channel's messages", 10*time.Second, func() bool {
		l, ok := e.a.convoFor(ch, "").list.(channelList)
		if !ok {
			return false
		}
		msgs, loaded, _ := l.LayoutSig()
		return loaded && msgs > 0
	})
	if n := readsOf(ch); n != 1 {
		t.Errorf("%d history reads for %s, want the press's one (the view takes it)", n, ch)
	}

	// Shift: no prefetch (Slack's handlePointerDown ignores modified presses)
	shift := cold[1]
	pt = at(shift)
	press(pt, key.ModShift)
	noRead(shift, "Shift press")
	release(pt, key.ModShift)

	// the open conversation's row: nothing to prefetch
	e.openChannel(cold[0])
	pt = at(cold[0])
	before := readsOf(cold[0])
	press(pt, 0)
	if n := readsOf(cold[0]); n != before {
		t.Errorf("a press on the open conversation's row read its history again (%d → %d)", before, n)
	}
	release(pt, 0)

	// not in the treatment: Slack doesn't prefetch
	c.SetExperimentGroup("pointerdown_history_prefetch", "control")
	off := cold[2]
	pt = at(off)
	press(pt, 0)
	noRead(off, "control group")
	release(pt, 0)

	// d7's app_dm: a DM only with a person the member store holds
	person := ""
	for _, cv := range e.a.model().Convs {
		if cv.Kind == "im" && cv.User != "" && cv.User != "USLACKBOT" {
			if u, ok := c.User(cv.User); ok && !u.IsBot {
				person = cv.User
				break
			}
		}
	}
	if person == "" {
		t.Fatal("no DM with a person in the sidebar")
	}
	for _, x := range []struct {
		cv   convInfo
		want bool
	}{
		{convInfo{Kind: "channel"}, true},
		{convInfo{Kind: "im", User: person}, true},
		{convInfo{Kind: "im", User: "U0NOTLOADED1"}, false}, // a partner not loaded yet
		{convInfo{Kind: "im", User: "USLACKBOT"}, false},
		{convInfo{Kind: "im", User: "USLACK"}, false},
		{convInfo{Kind: "app", User: person}, false},
	} {
		if got := e.a.sbPrefetchDM(&x.cv); got != x.want {
			t.Errorf("sbPrefetchDM(%s %s) = %v, want %v", x.cv.Kind, x.cv.User, got, x.want)
		}
	}
}

// The conversation views kept are the ones used most recently: a channel
// open all along survives 40 other conversations opened after it. The
// cache used to let views go in the order they were made, so the channel
// opened first went first however often it was used.
func TestConvoCacheKeepsTheChannelInUse(t *testing.T) {
	sz := fakeslack.Small
	sz.Channels = 45
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ids := fs.ChannelIDs()
	freq := ids[0]
	keep := e.a.convoFor(freq, "")
	for _, id := range ids[1:41] {
		e.a.convoFor(id, "")
		e.a.convoFor(freq, "") // drawn every frame while it's open
	}
	if got := e.a.convos[freq+"/"]; got != keep {
		t.Errorf("the channel used all along was let go (%v)", got)
	}
	if n := len(e.a.convos); n > maxConvos {
		t.Errorf("%d conversation views kept, want at most %d", n, maxConvos)
	}
	if e.a.convos[ids[1]+"/"] != nil {
		t.Error("the least recently used conversation is still kept")
	}

	// the page views (App.view) the same way
	mk := func() View { return &paneStub{a: e.a, title: "synthetic"} }
	page := e.a.view("lru-test/frequent", mk)
	for i := 0; i < 40; i++ {
		e.a.view(Route(fmt.Sprintf("lru-test/other-%d", i)), mk)
		e.a.view("lru-test/frequent", mk)
	}
	if got := e.a.views["lru-test/frequent"]; got != page {
		t.Error("the page view used all along was let go")
	}
	if _, ok := e.a.views["lru-test/other-0"]; ok {
		t.Error("the least recently used page view is still kept")
	}
	var calls atomic.Int32
	e.a.view("lru-test/frequent", func() View { calls.Add(1); return mk() })
	if calls.Load() != 0 {
		t.Error("the kept page view was built again")
	}
}
