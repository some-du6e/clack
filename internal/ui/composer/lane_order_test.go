package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The user's input reaches the plugins' JS in the order the user made it,
// a send included, however much rendering the runtime has queued: a
// browser handles input in order, and its user-blocking priority puts
// input ahead of rendering, never ahead of earlier input. A send
// overtook the click before it (OwlMode's "Reply anonymously" toggle: its
// send check and its prepareAndSendMessage patch read the mode the click
// sets, and a confession reply went out under the user's own name), and
// the typing report the keystrokes before it queued ("is typing…" under
// the sent message for 6 s). Synthetic plugins only.

// laneTaut is pluginTaut with the trees the plugins render kept.
func laneTaut(t *testing.T, app *core.App, body string) (*taut.Taut, func(root uint64) []byte) {
	t.Helper()
	var mu sync.Mutex
	trees := map[uint64][]byte{}
	dir := t.TempDir()
	tt, err := taut.New(taut.Options{App: app, Dir: dir, CredsPath: dir + "/creds.json", CacheDir: t.TempDir(), NoAutoStart: true,
		UI: taut.UIHooks{Deliver: func(root uint64, tree []byte) {
			mu.Lock()
			trees[root] = append([]byte(nil), tree...)
			mu.Unlock()
		}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	id, err := tt.Import(ctx, []byte(`(() => {
const TautPlugin = globalThis.TautPlugin
class LaneOrder extends TautPlugin {
  static id = 'LaneOrder'
  static pluginName = 'Lane order'
  static description = 'Records what its send check and send patch saw'
  static category = 'messageBox'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  `+body+`
}
return LaneOrder
})()`), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	withTaut(t, tt)
	t.Cleanup(func() {
		SetTaut(nil)
		syncHooks()
	})
	return tt, func(root uint64) []byte {
		mu.Lock()
		defer mu.Unlock()
		return trees[root]
	}
}

// jsBacklog queues d of JS work in 10 ms tasks: what a channel's patched
// rows leave the runtime when messages and profile changes arrive.
func jsBacklog(rt *jsrt.Runtime, d time.Duration) {
	for i := time.Duration(0); i < d; i += 10 * time.Millisecond {
		rt.Post(func(*jsrt.Ctx) { time.Sleep(10 * time.Millisecond) })
	}
}

func TestSendSeesTheClickBeforeIt(t *testing.T) {
	c, fs, _ := composerEnv(t, nil)
	tt, tree := laneTaut(t, c.api.a, `start() {
    this.mode = 'as yourself'
    globalThis.__laneSeen = []
    globalThis.__laneSeenNow = () => globalThis.__laneSeen
    this.offs = [
      this.api.composer.addButton({ id: 'lane-toggle', placement: 'end', render: () => ({ icon: 'eye', label: 'Reply anonymously', pressed: this.mode === 'anonymously', onClick: () => { this.mode = 'anonymously' } }) }),
      this.api.composer.addSendCheck(() => { globalThis.__laneSeen.push('check ' + this.mode); return true }),
      this.api.redux.patchThunk('prepareAndSendMessage', (orig) => (...a) => { globalThis.__laneSeen.push('send ' + this.mode); return orig(...a) }),
    ]
  }
  stop() { for (const off of this.offs ?? []) off() }`)
	waitFor(t, 10*time.Second, "the plugin's send check and send patch", func() bool {
		return tt.HasSendChecks() && tt.ThunkPatched("prepareAndSendMessage")
	})
	syncHooks()
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	// the composer's toolbar, as the composer renders it, and its button's onClick
	props, _ := json.Marshal(map[string]any{"placement": "end", "context": map[string]any{"kind": "channel", "channelId": tChGen}})
	tt.Bridge().Render(9, "TautComposerButtons", props)
	var fn float64
	waitFor(t, 5*time.Second, "the plugin's composer button", func() bool {
		var nodes []any
		if json.Unmarshal(tree(9), &nodes) != nil {
			return false
		}
		btn := findNode(nodes, "ComposerButton")
		if btn == nil {
			return false
		}
		p, _ := btn["p"].(map[string]any)
		click, _ := p["onClick"].(map[string]any)
		fn, _ = click["$fn"].(float64)
		return fn != 0
	})
	send := func(text string) {
		t.Helper()
		n := len(fs.called("chat.postMessage"))
		d := NewDoc()
		d.Insert(0, text, Attr{})
		c.ed.SetDoc(d)
		c.submit()
		waitFor(t, 15*time.Second, text+" posted", func() bool { return len(fs.called("chat.postMessage")) > n })
	}
	seen := func() []string {
		t.Helper()
		raw, err := rt.Call(context.Background(), "__laneSeenNow")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		_ = json.Unmarshal(raw, &out)
		return out
	}
	send("warm")
	before := len(seen())
	jsBacklog(rt, 400*time.Millisecond)
	tt.Bridge().Call(int64(fn), []byte("[]"), nil) // the click, queued behind the renders
	send("thanks all")                             // then Enter
	got := strings.Join(seen()[before:], ", ")
	if want := "check anonymously, send anonymously"; got != want {
		t.Errorf("the send's check and patch saw %q, want %q: the send overtook the click before it", got, want)
	}
	_ = rt.Do(func(*jsrt.Ctx) error { return nil })
}

func TestTypingGoesOutBeforeTheMessage(t *testing.T) {
	c, fs, _ := composerEnv(t, nil)
	tt := pluginTaut(t, c.api.a, `start() {
    this.offs = [
      this.api.composer.addSendCheck(() => true),
      this.api.redux.patchThunk('currentUserStartedTyping', (orig) => (...a) => orig(...a)),
    ]
  }
  stop() { for (const off of this.offs ?? []) off() }`)
	waitFor(t, 10*time.Second, "the plugin's typing patch", func() bool {
		return tt.HasSendChecks() && tt.ThunkPatched(thTyping)
	})
	syncHooks()
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	var mu sync.Mutex
	var typed []string
	old := typingNative
	typingNative = func(coreAPI, string, string) error {
		mu.Lock()
		typed = append(typed, fmt.Sprint(len(fs.called("chat.postMessage"))))
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { typingNative = old })
	api := coreAPI{c.api.a}
	sendTyping(api, tChGen, "") // warm
	waitFor(t, 10*time.Second, "the warm typing report", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(typed) > 0
	})
	mu.Lock()
	typed = nil
	mu.Unlock()
	posts := len(fs.called("chat.postMessage"))
	jsBacklog(rt, 400*time.Millisecond)
	startTyping(api, tChGen, "") // the first keystroke, as composer.go starts it on the UI goroutine, then Enter at once
	d := NewDoc()
	d.Insert(0, "ok", Attr{})
	c.ed.SetDoc(d)
	c.submit() // Enter
	waitFor(t, 15*time.Second, "the message and its typing", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(typed) > 0 && len(fs.called("chat.postMessage")) > posts
	})
	mu.Lock()
	got := typed[0]
	mu.Unlock()
	if want := fmt.Sprint(posts); got != want {
		t.Errorf("the typing report went out after %s posts, want %s: after the message typed with it", got, want)
	}
	_ = rt.Do(func(*jsrt.Ctx) error { return nil })
}

// A dispatch the native UI makes (the emoji picker's pick, a reaction
// pill, a draft sync) stays ahead of the user's later input, as every
// action of the user's does in a browser: a plugin click after it can't
// overtake it. OwlMode decides a reaction's identity from its mode when
// the reaction's patch runs, and a pick queued from run's goroutine ran
// after a later toggle click: one meant for Owl went out under the user's
// own name. Driven through popups.React, the picker's own call, which
// queues its JS before it returns.
func TestNativeDispatchBeforeLaterClick(t *testing.T) {
	c, _, _ := composerEnv(t, nil)
	tt, tree := laneTaut(t, c.api.a, `start() {
    this.mode = 'as Owl'
    globalThis.__laneSeen = []
    globalThis.__laneSeenNow = () => globalThis.__laneSeen
    this.offs = [
      this.api.composer.addButton({ id: 'lane-toggle', placement: 'end', render: () => ({ icon: 'eye', label: 'Reply as yourself', pressed: this.mode === 'as yourself', onClick: () => { this.mode = 'as yourself' } }) }),
      this.api.redux.patchThunk('submitAddOrRemoveReaction', (orig) => (...a) => { globalThis.__laneSeen.push('reaction ' + this.mode); return orig(...a) }),
    ]
  }
  stop() { for (const off of this.offs ?? []) off() }`)
	waitFor(t, 10*time.Second, "the plugin's reaction patch", func() bool { return tt.ThunkPatched("submitAddOrRemoveReaction") })
	popups.SetTaut(tt)
	t.Cleanup(func() { popups.SetTaut(nil) })
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	props, _ := json.Marshal(map[string]any{"placement": "end", "context": map[string]any{"kind": "channel", "channelId": tChGen}})
	tt.Bridge().Render(9, "TautComposerButtons", props)
	var fn float64
	waitFor(t, 5*time.Second, "the plugin's composer button", func() bool {
		var nodes []any
		if json.Unmarshal(tree(9), &nodes) != nil {
			return false
		}
		btn := findNode(nodes, "ComposerButton")
		if btn == nil {
			return false
		}
		p, _ := btn["p"].(map[string]any)
		click, _ := p["onClick"].(map[string]any)
		fn, _ = click["$fn"].(float64)
		return fn != 0
	})
	seen := func() []string {
		t.Helper()
		raw, err := rt.Call(context.Background(), "__laneSeenNow")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		_ = json.Unmarshal(raw, &out)
		return out
	}
	h := &slickHost{app: c.api.a, th: theme.New(false)}
	// the picker's onPick, on the UI goroutine
	pick := func() { popups.React(h, tChGen, "1700000000.000100", "eyes", true) }
	pick() // warm
	waitFor(t, 10*time.Second, "the warm-up reaction's patch", func() bool { return len(seen()) > 0 })
	before := len(seen())
	jsBacklog(rt, 400*time.Millisecond)
	pick()                                         // the pick, behind the renders
	tt.Bridge().Call(int64(fn), []byte("[]"), nil) // then the toggle
	waitFor(t, 10*time.Second, "the reaction's patch", func() bool { return len(seen()) > before })
	got := strings.Join(seen()[before:], ", ")
	if want := "reaction as Owl"; got != want {
		t.Errorf("the reaction's patch saw %q, want %q: the click after it overtook it", got, want)
	}
	_ = rt.Do(func(*jsrt.Ctx) error { return nil })
}
