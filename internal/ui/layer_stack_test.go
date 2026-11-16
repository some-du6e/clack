package ui

// Escape and the focus in the main window's overlay stack (slackc
// layers.go), the way the DOM does a modal: the focused element gets the
// keydown first, then the innermost layer holding the focus, else the
// topmost; the shell's "Close / mark as read" only with no layer open;
// and a layer that closes gives the focus back to what had it. Plugin
// trees come from a fake bridge (no JS). Synthetic data on
// internal/fakeslack.

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// escBridge is the plugin side: it records renders, the JS functions Go
// calls and the events it dispatches.
type escBridge struct {
	mu      sync.Mutex
	renders map[string]uint64 // component → its latest root
	calls   []int64
	events  []string // "<target>:<event JSON>"
}

func (b *escBridge) Render(root uint64, name string, props []byte) {
	b.mu.Lock()
	b.renders[name] = root
	b.mu.Unlock()
}
func (b *escBridge) RenderFn(root uint64, fn int64, args []byte) {}
func (b *escBridge) Unmount(root uint64)                         {}
func (b *escBridge) Dispatch(target int64, event []byte) {
	b.mu.Lock()
	b.events = append(b.events, fmt.Sprint(target)+":"+string(event))
	b.mu.Unlock()
}
func (b *escBridge) Call(fn int64, args []byte, done func([]byte, error)) {
	b.mu.Lock()
	b.calls = append(b.calls, fn)
	b.mu.Unlock()
}
func (b *escBridge) SetRects(rects map[int64][4]float32)      {}
func (b *escBridge) SetViewport(w, h, dpr float32, dark bool) {}

func (b *escBridge) root(name string) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.renders[name]
}

// called reports whether JS function fn was called, and forgets the calls.
func (b *escBridge) called(fn int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	ok := slices.Contains(b.calls, fn)
	b.calls = nil
	return ok
}

// escKeydowns lists the targets of the Escape keydowns dispatched so far.
func (b *escBridge) escKeydowns() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, e := range b.events {
		if strings.Contains(e, `"type":"keydown"`) && strings.Contains(e, `"key":"Escape"`) {
			out = append(out, e[:strings.IndexByte(e, ':')])
		}
	}
	return out
}

// layerEnv is the main window on a channel with names patched by the fake
// bridge.
type layerEnv struct {
	*perfEnv
	fb *escBridge
	ch string
}

func newLayerEnv(t *testing.T, names ...string) *layerEnv {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	pe := startEnv(t, fs, t.TempDir())
	pe.waitReady()
	ch := fs.ChannelIDs()[1]
	pe.openChannel(ch)
	pe.until("history", 10*time.Second, func() bool { return pe.loadedMessages(ch) > 0 })
	pe.settle(300 * time.Millisecond)
	if pe.a.overlayOpen() {
		t.Fatal("an overlay is open after boot")
	}
	fb := &escBridge{renders: map[string]uint64{}}
	slackc.SetBridge(fb)
	slackc.SetPatched(names)
	t.Cleanup(func() { slackc.Reset(); slackc.SetBridge(nil); slackc.SetPatched(nil) })
	return &layerEnv{pe, fb, ch}
}

// rootOf waits for the plugin root of component name.
func (e *layerEnv) rootOf(name string) uint64 {
	e.tb.Helper()
	e.until(name+"'s plugin root", 5*time.Second, func() bool { return e.fb.root(name) != 0 })
	return e.fb.root(name)
}

func (e *layerEnv) deliver(root uint64, tree string) {
	e.tb.Helper()
	if err := slackc.Deliver(root, []byte(tree)); err != nil {
		e.tb.Fatal(err)
	}
	e.settle(300 * time.Millisecond)
}

// fakeLatest is the channel's newest message as unread() makes it:
// conversations.mark at this ts is the shell's Escape marking it read.
const fakeLatest = "9999999999.000100"

func (e *layerEnv) unread() {
	e.tb.Helper()
	c := e.a.data.m.conv(e.ch)
	if c == nil {
		e.tb.Fatal("no conversation in the model")
	}
	c.Unread, c.Latest = true, fakeLatest
}

// escape presses Escape and lets it settle.
func (e *layerEnv) escape() {
	e.key(key.NameEscape, 0)
	e.settle(400 * time.Millisecond)
}

func (e *layerEnv) composerFocused() bool {
	cv := e.a.convos[e.ch+"/"]
	f, ok := cv.comp.(interface{ Focused() bool })
	return ok && f.Focused()
}

const onPopClose = 901

// popoverTree is a plugin tree for component name: its own draw (unless
// noOriginal) and, after it, an open popover around kids.
func popoverTree(name, kids string, noOriginal bool) string {
	pop := `{"t":"@","n":"Popover","k":2,"p":{"isOpen":true,"targetBounds":{"x":600,"y":200,"width":28,"height":28},` +
		`"onClose":{"$fn":901}},"c":[{"t":"div","k":3,"c":[` + kids + `]}]}`
	if noOriginal {
		return `[` + pop + `]`
	}
	return `[{"t":"@","n":"` + name + `","k":1},` + pop + `]`
}

// In the main window Escape is an open plugin Popover's, as in Slack
// (react-modal closes on an Escape from anything inside it and stops it
// there): the shell's "Close / mark as read", read first in the frame,
// took it, so Escape marked the channel read and the popover stayed open.
// It closes the popover with the popover focused and with the field in it
// focused; the focus goes back to the composer, which had it when the
// popover opened; and with no layer open the binding has Escape again.
func TestEscapeClosesPluginPopoverFirst(t *testing.T) {
	e := newLayerEnv(t, "HelpButton")
	root := e.rootOf("HelpButton")
	cv := e.a.convos[e.ch+"/"]
	cv.comp.Focus()
	e.settle(300 * time.Millisecond)
	if !e.composerFocused() {
		t.Fatal("the composer didn't take the focus")
	}
	for _, c := range []struct{ what, kids string }{
		{"the popover focused", `"Filter"`},
		{"the autoFocus field in it focused", `{"t":"@","n":"FormTextInput","k":4,"p":{"autoFocus":true,"value":""}}`},
	} {
		e.deliver(root, popoverTree("HelpButton", c.kids, false))
		e.unread()
		e.escape()
		if !e.fb.called(onPopClose) {
			t.Errorf("%s: Escape didn't close the plugin's popover (no onClose)", c.what)
		}
		if e.fs.Marked(e.ch) == fakeLatest {
			t.Fatalf("%s: Escape marked the channel read under the open popover", c.what)
		}
	}
	e.deliver(root, `[{"t":"@","n":"HelpButton","k":1}]`) // the plugin closes it
	if !e.composerFocused() {
		t.Error("the popover closed and the composer, which had the focus when it opened, didn't get it back")
	}
	e.unread()
	e.escape()
	e.until("Escape's conversations.mark with no popover open", 10*time.Second, func() bool { return e.fs.Marked(e.ch) == fakeLatest })
}

// While an async action is in flight its pump (an invisible overlay) is
// in the shell's stack. It isn't a layer: Escape still closes the
// popover, and with no layer open it marks the channel read (the pump
// took every Escape and passed it on to no popup).
func TestEscapeWithThePumpUp(t *testing.T) {
	e := newLayerEnv(t, "HelpButton")
	root := e.rootOf("HelpButton")
	e.fs.SetFault("saved.add", fakeslack.Fault{Delay: 12 * time.Second})
	tl := e.a.data.core.S.Timeline(e.ch)
	if tl == nil || len(tl.Msgs) == 0 {
		t.Fatal("no messages in the channel")
	}
	popups.RunSaveAction(e.a, core.Message{Channel: e.ch, TS: tl.Msgs[len(tl.Msgs)-1].TS})
	e.settle(200 * time.Millisecond)
	pump := func() bool {
		for _, o := range e.a.overlays {
			if invisible(o) && !o.Dismissed() {
				return true
			}
		}
		return false
	}
	if !pump() {
		t.Fatal("no pump up while saved.add is in flight")
	}
	e.deliver(root, popoverTree("HelpButton", `"Filter"`, false))
	e.unread()
	e.escape()
	if !e.fb.called(onPopClose) {
		t.Error("with the pump up, Escape didn't close the plugin's popover")
	}
	if e.fs.Marked(e.ch) == fakeLatest {
		t.Fatal("Escape marked the channel read under the open popover")
	}
	e.deliver(root, `[{"t":"@","n":"HelpButton","k":1}]`)
	if !pump() {
		t.Fatal("the pump went before the test's last Escape")
	}
	e.unread()
	e.escape()
	e.until("Escape's conversations.mark with only the pump up", 5*time.Second, func() bool { return e.fs.Marked(e.ch) == fakeLatest })
}

// A Popover a plugin draws inside the Edit profile modal (its
// EditProfileFormPane patch point) is a layer over the modal: Escape
// closes the popover, not the modal (dismissing the whole modal left the
// plugin's popover open for when it came back). The focus goes back into
// the modal, to the field that had it, never to the composer behind it;
// the next Escape closes the modal.
func TestPopoverInsideEditProfile(t *testing.T) {
	e := newLayerEnv(t, "EditProfileFormPane")
	m := popups.OpenEditProfile(e.a, popups.EditProfileOpts{})
	root := e.rootOf("EditProfileFormPane")
	e.settle(300 * time.Millisecond)
	e.deliver(root, popoverTree("EditProfileFormPane", `"Pick one"`, true))
	e.escape()
	if !e.fb.called(onPopClose) {
		t.Error("Escape didn't close the popover in the Edit profile modal")
	}
	if m.Dismissed() {
		t.Fatal("Escape closed the Edit profile modal under the plugin's popover")
	}
	e.deliver(root, `[]`) // the plugin closes it
	if e.composerFocused() {
		t.Error("the popover in the Edit profile modal closed and the composer behind the modal took the focus")
	}
	if e.r.Source().FocusTag() == nil {
		t.Error("the popover in the Edit profile modal closed and the focus went nowhere; want it back in the modal")
	}
	e.escape()
	if !m.Dismissed() {
		t.Error("Escape with the popover gone didn't close the Edit profile modal")
	}
}

// A Popover in a Block Kit views modal (its ViewBlocks patch point) closes
// on Escape and the modal stays; the focus goes back into the modal, not
// to the composer behind it.
func TestPopoverInViewsModal(t *testing.T) {
	e := newLayerEnv(t, "ViewBlocks")
	ix := e.a.data.core.Interactive()
	pt := func(s string) map[string]any { return map[string]any{"type": "plain_text", "text": s} }
	view := map[string]any{"id": "VLAYERS01", "type": "modal", "app_id": "A0LAYERS1", "title": pt("Layers"), "close": pt("Cancel"),
		"blocks": []any{map[string]any{"type": "section", "block_id": "s", "text": map[string]any{"type": "mrkdwn", "text": "a view"}}}}
	raw, _ := json.Marshal(map[string]any{"type": "view_opened", "view_id": "VLAYERS01", "view_type": "modal",
		"client_token": ix.ClientToken(), "timeout_range": 10000, "app_id": "A0LAYERS1", "view": view})
	e.a.data.core.Inject(slack.Event{Type: "view_opened", Raw: raw})
	root := e.rootOf("ViewBlocks")
	var st *blockkit.Stack
	for _, o := range e.a.overlays {
		if s, ok := o.(*blockkit.Stack); ok && !s.Dismissed() {
			st = s
		}
	}
	if st == nil {
		t.Fatal("the views modal isn't up")
	}
	e.deliver(root, popoverTree("ViewBlocks", `"Pick one"`, false))
	e.escape()
	if !e.fb.called(onPopClose) {
		t.Error("Escape didn't close the popover in the views modal")
	}
	if st.Dismissed() {
		t.Fatal("Escape closed the views modal under the plugin's popover")
	}
	e.deliver(root, `[{"t":"@","n":"ViewBlocks","k":1}]`)
	if e.composerFocused() {
		t.Error("the popover in the views modal closed and the composer behind the modal took the focus")
	}
	if e.r.Source().FocusTag() == nil {
		t.Error("the popover in the views modal closed and the focus went nowhere; want it back in the modal")
	}
}

// A plugin's modal <dialog> (an overlay root) opened over a Popover gets
// Escape: its focused input's keydown goes to JS, whose default action
// cancels the topmost modal dialog. The popover under it stays open.
func TestDialogOverPopover(t *testing.T) {
	e := newLayerEnv(t, "HelpButton")
	root := e.rootOf("HelpButton")
	e.deliver(root, popoverTree("HelpButton", `"Filter"`, false))
	const dialog = 1<<63 | 77
	t.Cleanup(func() { _ = slackc.Deliver(dialog, []byte(`[]`)) })
	e.deliver(dialog, `[{"t":"dialog","k":11,"p":{"open":""},"modal":true,"c":[{"t":"input","k":12,"focus":true,"p":{}}]}]`)
	if e.r.Source().Focused(nil) {
		t.Fatal("the dialog's input didn't take the focus")
	}
	e.fb.called(onPopClose) // forget
	e.escape()
	if e.fb.called(onPopClose) {
		t.Error("Escape in a plugin dialog over a popover closed the popover")
	}
	if k := e.fb.escKeydowns(); !slices.Contains(k, "12") {
		t.Errorf("Escape keydowns dispatched at %v; want one at the dialog's focused input (12)", k)
	}
}
