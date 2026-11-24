package messages

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/richclip"
)

// A focused message's keys are Slack's MessageActions key commands
// (popups/actions_keys.go): each runs its action where the action's rule
// gives the message one, and a letter the message doesn't take goes on to
// the conversation's input, as any other letter does.

// keysHost is ctxHost that records the threads it opens.
type keysHost struct {
	*ctxHost
	threads []string
}

func (h *keysHost) OpenThread(ch, ts string) { h.threads = append(h.threads, ch+"/"+ts) }

// keysEnv is newEphEnv's #eph in a list over keysHost (the channel's or
// the thread pane's), with what its input steals gathered.
type keysEnv struct {
	*ctxEnv
	kh     *keysHost
	f      *fakeSlack
	s      *listState
	lay    func(gtx layout.Context) layout.Dimensions
	stolen string
}

func newKeysEnv(t *testing.T, thread bool) *keysEnv {
	t.Helper()
	e, f := newEphEnv(t)
	k := &keysEnv{ctxEnv: e, kh: &keysHost{ctxHost: e.h}, f: f}
	steal := func(gtx layout.Context, text string, keys []key.Event) event.Tag {
		k.stolen += text
		return nil
	}
	want := []string{tsEphPlain, tsEph, tsEphMine}
	if thread {
		tv := NewThreadView(k.kh, tEph, tsEphRoot)
		tv.SetSteal(steal)
		k.s, k.lay = tv.s, tv.Layout
		want = []string{tsEphR1, tsEphR2}
	} else {
		cv := NewChannelView(k.kh, tEph)
		cv.Preview = true
		cv.SetSteal(steal)
		k.s, k.lay = cv.s, cv.Layout
	}
	until(t, 5*time.Second, "the rows", k.frame, func() bool {
		for _, ts := range want {
			if k.s.rows[ts] == nil {
				return false
			}
		}
		return true
	})
	return k
}

// frame lays the list out over the env's router.
func (k *keysEnv) frame() {
	k.ops.Reset()
	k.now = k.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &k.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 700)), Now: k.now, Source: k.r.Source()}
	k.lay(gtx)
	k.r.Frame(&k.ops)
}

// effects is what the actions did so far: overlays shown, threads opened,
// API calls made, the clipboard's last write.
type effects struct {
	overlays, threads, calls int
	clip                     string
}

func (k *keysEnv) effects() effects {
	k.f.mu.Lock()
	n := len(k.f.calls)
	k.f.mu.Unlock()
	return effects{overlays: len(k.kh.overlays), threads: len(k.kh.threads), calls: n, clip: k.clip()}
}

// selectMsg gives the list the keyboard focus at ts.
func (k *keysEnv) selectMsg(t *testing.T, ts string) {
	t.Helper()
	k.s.kb.want, k.s.kb.ts, k.s.kb.focusReq = "", ts, true
	until(t, 5*time.Second, "the list's keyboard focus at "+ts, k.frame, func() bool { return k.s.kb.on && k.s.kb.ts == ts })
	k.frame()
}

// press presses key name with mods, typing text (the key's character, ""
// for none), as the OS sends them.
func (k *keysEnv) press(name key.Name, mods key.Modifiers, text string) {
	evs := []event.Event{key.Event{Name: name, Modifiers: mods, State: key.Press}}
	if text != "" {
		evs = append(evs, key.EditEvent{Text: text})
	}
	evs = append(evs, key.Event{Name: name, Modifiers: mods, State: key.Release})
	k.r.Queue(evs...)
	k.frame()
	k.frame()
}

// fkey is one of a focused message's keys and what it does when bound.
type fkey struct {
	name  string
	key   key.Name
	mods  key.Modifiers
	text  string
	did   func(before, after effects) bool
	what  string
	copyC bool // Mod+C: its copy is the OS clipboard's where richclip is
}

var (
	overlayShown = func(b, a effects) bool { return a.overlays > b.overlays }
	threadOpened = func(b, a effects) bool { return a.threads > b.threads }
	apiCalled    = func(b, a effects) bool { return a.calls > b.calls }
	clipWritten  = func(b, a effects) bool { return a.clip != "" && a.clip != b.clip }
)

var focusedKeys = []fkey{
	{name: "R add reaction", key: "R", text: "r", did: overlayShown, what: "the reaction picker"},
	{name: "T reply in thread", key: "T", text: "t", did: threadOpened, what: "the thread"},
	{name: "S share", key: "S", text: "s", did: overlayShown, what: "the share dialog"},
	{name: "F forward", key: "F", text: "f", did: overlayShown, what: "the share dialog"},
	{name: "A add to-do", key: "A", text: "a", did: apiCalled, what: "saved.add"},
	{name: "U mark unread", key: "U", text: "u", did: apiCalled, what: "the mark"},
	{name: "P pin", key: "P", text: "p", did: apiCalled, what: "pins.add"},
	{name: "M remind", key: "M", text: "m", did: overlayShown, what: "the custom reminder dialog"},
	{name: "L copy link", key: "L", text: "l", did: clipWritten, what: "the link on the clipboard"},
	{name: "Mod+Shift+S save", key: "S", mods: key.ModShortcut | key.ModShift, did: apiCalled, what: "saved.add"},
	{name: "Mod+C copy message", key: "C", mods: key.ModShortcut, did: clipWritten, what: "the text on the clipboard", copyC: true},
	{name: "Delete", key: key.NameDeleteForward, did: overlayShown, what: "the delete dialog"},
}

// settleFor lays frames out until did holds or 2s pass (the writes run off
// the UI goroutine), and reports whether it held.
func (k *keysEnv) settleFor(before effects, did func(b, a effects) bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if did(before, k.effects()) {
			return true
		}
		k.frame()
		time.Sleep(20 * time.Millisecond)
	}
	return did(before, k.effects())
}

// Each key on an ordinary message runs its action (Delete on one of the
// viewer's own) and its letter stays out of the input; on an ephemeral
// message only Copy message and Delete are bound (actions_rules.go), and
// every other letter goes to the input instead.
func TestFocusedMessageKeysLikeSlack(t *testing.T) {
	for _, c := range focusedKeys {
		t.Run(strings.ReplaceAll(c.name, " ", "_"), func(t *testing.T) {
			if c.copyC && richclip.Supported() {
				t.Skip("Copy message writes the OS clipboard here (richclip)")
			}
			k := newKeysEnv(t, false)
			normal := tsEphPlain
			if c.key == key.NameDeleteForward {
				normal = tsEphMine
			}
			k.selectMsg(t, normal)
			before := k.effects()
			k.press(c.key, c.mods, c.text)
			if !k.settleFor(before, c.did) {
				t.Errorf("%s on an ordinary message: no %s", c.name, c.what)
			}
			if k.stolen != "" {
				t.Errorf("%s on an ordinary message: the input got %q too", c.name, k.stolen)
			}

			// the ephemeral message: Copy message and Delete still, the rest
			// unbound
			onEph := c.copyC || c.key == key.NameDeleteForward
			k.stolen = ""
			k.selectMsg(t, tsEph)
			before = k.effects()
			k.press(c.key, c.mods, c.text)
			did := k.settleFor(before, c.did)
			switch {
			case onEph && !did:
				t.Errorf("%s on the ephemeral message: no %s (Slack keeps it)", c.name, c.what)
			case !onEph && did:
				t.Errorf("%s on the ephemeral message: %s, which Slack has no action for", c.name, c.what)
			}
			if !onEph && c.text != "" && k.stolen != c.text {
				t.Errorf("%s on the ephemeral message: the input got %q, want the unbound letter %q", c.name, k.stolen, c.text)
			}
		})
	}
}

// A key with a modifier it doesn't have is no key of the message's
// (Mousetrap matches modifiers exactly): Shift+R or Alt+R opens no picker,
// and a temporary message (a notice this client made) binds no keys at
// all. A letter no action has goes to the input.
func TestFocusedMessageKeysExactly(t *testing.T) {
	k := newKeysEnv(t, false)
	k.selectMsg(t, tsEphPlain)
	for _, mods := range []key.Modifiers{key.ModShift, key.ModAlt} {
		before := k.effects()
		k.press("R", mods, "")
		if k.settleFor(before, overlayShown) {
			t.Errorf("R with %v opened %d overlays", mods, k.effects().overlays-before.overlays)
		}
	}
	k.stolen = ""
	k.press("X", 0, "x")
	if k.stolen != "x" {
		t.Errorf("x on a selected message: the input got %q", k.stolen)
	}

	ts := k.h.app.LocalEphemeral(tEph, "", "a synthetic local notice")
	until(t, 5*time.Second, "the notice's row", k.frame, func() bool { return k.s.rows[ts] != nil })
	k.selectMsg(t, ts)
	k.stolen = ""
	before := k.effects()
	k.press("T", 0, "t")
	if k.settleFor(before, threadOpened) {
		t.Error("T on the local notice opened a thread")
	}
	if k.stolen != "t" {
		t.Errorf("t on the local notice: the input got %q", k.stolen)
	}
}

// In the thread pane: no thread action (T goes to the reply box), U marks
// the thread unread from the reply, R reacts.
func TestFocusedReplyKeysLikeSlack(t *testing.T) {
	k := newKeysEnv(t, true)
	k.selectMsg(t, tsEphR1)
	before := k.effects()
	k.press("T", 0, "t")
	if k.settleFor(before, threadOpened) {
		t.Error("T on a reply in the thread pane opened a thread")
	}
	if k.stolen != "t" {
		t.Errorf("t on a reply: the reply box got %q, want t", k.stolen)
	}
	for _, c := range []struct {
		key  key.Name
		text string
		did  func(b, a effects) bool
		what string
	}{{"U", "u", apiCalled, "the thread's mark"}, {"R", "r", overlayShown, "the reaction picker"}} {
		k.stolen = ""
		before := k.effects()
		k.press(c.key, 0, c.text)
		if !k.settleFor(before, c.did) {
			t.Errorf("%s on a reply: no %s", c.key, c.what)
		}
		if k.stolen != "" {
			t.Errorf("%s on a reply: the reply box got %q too", c.key, k.stolen)
		}
	}
	// its ephemeral reply: neither
	k.selectMsg(t, tsEphR2)
	for _, c := range []struct {
		key  key.Name
		text string
	}{{"U", "u"}, {"R", "r"}} {
		k.stolen = ""
		before := k.effects()
		k.press(c.key, 0, c.text)
		if k.settleFor(before, func(b, a effects) bool { return a.overlays > b.overlays || a.calls > b.calls }) {
			t.Errorf("%s on the ephemeral reply did something", c.key)
		}
		if k.stolen != c.text {
			t.Errorf("%s on the ephemeral reply: the reply box got %q", c.key, k.stolen)
		}
	}
}
