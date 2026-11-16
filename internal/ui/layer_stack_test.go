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
	"image"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"

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
	e := newNativeEnv(t)
	fb := &escBridge{renders: map[string]uint64{}}
	slackc.SetBridge(fb)
	slackc.SetPatched(names)
	t.Cleanup(func() { slackc.Reset(); slackc.SetBridge(nil); slackc.SetPatched(nil) })
	e.fb = fb
	return e
}

// newNativeEnv is the main window on a channel with no plugins at all.
func newNativeEnv(t *testing.T) *layerEnv {
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
	return &layerEnv{pe, nil, ch}
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

// Without plugins Escape is as it always was: the shell's binding, read
// first in the frame, closes the topmost overlay (dismissTop). A menu or
// a dialog opened from inside Channel details is its own overlay on top,
// and Escape closes it, not the details dialog under it (whose own
// Escape reader, given the key, closed itself and left the menu floating
// over the page); the next Escape closes Channel details.
func TestEscapeClosesWhatChannelDetailsOpenedFirst(t *testing.T) {
	e := newNativeEnv(t)
	var prefs map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"allow_calls":true,"calls_apps":{"video":[{"id":"A5GE9BMQC","name":"Zoom"},{"id":"AMEET0001","name":"Google Meet"}]}}`), &prefs)
	e.a.Core().SetTeamPrefs(prefs)
	e.a.OpenDetails(e.ch)
	e.settle(500 * time.Millisecond)
	d := topDetails(t, e.perfEnv)
	e.until("the call button's menu", 10*time.Second, func() bool { vis, _, caret, _ := d.CallButton(); return vis && caret })
	d.PressCall()
	e.until("the call menu", 5*time.Second, func() bool { return d.CallMenu() != nil })
	m := d.CallMenu()
	e.settle(300 * time.Millisecond)
	e.escape()
	if d.Closed() {
		t.Fatal("Escape over the call menu closed Channel details under it")
	}
	if !m.Dismissed() {
		t.Error("Escape didn't close the call menu over Channel details")
	}

	// the "Start a call?" confirm it opens
	d.PressCall()
	e.until("the call menu again", 5*time.Second, func() bool { return d.CallMenu() != nil })
	var meet func()
	for _, it := range d.CallMenu().Items() {
		if it.Label == "Google Meet" {
			meet = it.Do
		}
	}
	if meet == nil {
		t.Fatal("no Google Meet row in the call menu")
	}
	d.CallMenu().Dismiss()
	e.settle(100 * time.Millisecond)
	meet()
	e.until("Start a call?", 5*time.Second, func() bool { return d.CallConfirm() != nil })
	e.settle(300 * time.Millisecond)
	e.escape()
	if d.Closed() {
		t.Fatal("Escape over \"Start a call?\" closed Channel details under it")
	}
	if d.CallConfirm() != nil {
		t.Error("Escape didn't close \"Start a call?\" over Channel details")
	}
	if len(d.CallsOpened()) != 0 {
		t.Fatalf("a call was opened: %v", d.CallsOpened())
	}
	e.escape()
	if !d.Closed() {
		t.Error("Escape with nothing over Channel details didn't close it")
	}
}

// The colour popover Preferences' Appearance opens (a custom theme
// colour's edit button) takes no focus; the focus stays on the dialog.
// Escape closes the popover, not Preferences (its own reader, Focus-bound
// on the dialog, read it first and left the popover over the page,
// still applying colours); the next Escape closes Preferences.
func TestEscapeClosesPreferencesColourPopoverFirst(t *testing.T) {
	e := newNativeEnv(t)
	e.a.showPrefs("appearance")
	e.settle(500 * time.Millisecond)
	prefs, ok := topOverlay[*popups.Modal](e.a)
	if !ok || prefs.Title() != "Preferences" {
		t.Fatal("Preferences didn't open")
	}
	p := newColorPopover(e.a, image.Pt(400, 300), "#611F69", func(string) {}) // as "Edit … color" does
	e.a.ShowOverlay(p)
	e.settle(300 * time.Millisecond)
	e.escape()
	if prefs.Dismissed() {
		t.Fatal("Escape on the colour popover closed Preferences under it")
	}
	if slices.Contains(e.a.overlays, Overlay(p)) && !p.Dismissed() {
		t.Error("Escape didn't close the colour popover over Preferences")
	}
	e.escape()
	if !prefs.Dismissed() {
		t.Error("Escape with the popover gone didn't close Preferences")
	}
}

// A menu opened over a modal (Edit profile's) closes on Escape first; the
// modal stays for the next one.
func TestEscapeClosesMenuOverEditProfileFirst(t *testing.T) {
	e := newNativeEnv(t)
	ep := popups.OpenEditProfile(e.a, popups.EditProfileOpts{})
	e.settle(400 * time.Millisecond)
	m := popups.NewMenu(e.a, image.Pt(500, 300), []popups.MenuItem{{Label: "One"}, {Label: "Two"}})
	e.a.ShowOverlay(m)
	e.settle(300 * time.Millisecond)
	e.escape()
	if ep.Dismissed() {
		t.Fatal("Escape over a menu closed the Edit profile modal under it")
	}
	if !m.Dismissed() {
		t.Error("Escape didn't close the menu over Edit profile")
	}
	e.escape()
	if !ep.Dismissed() {
		t.Error("Escape with the menu gone didn't close Edit profile")
	}
}

// A modal whose CloseGuard keeps it open (unsaved edits: its speed bump
// goes up over it) stays in the shell's stack on Escape: dropped, it
// went behind its own "discard?" confirm, and cancelling that left no
// dialog.
func TestEscapeKeepsGuardedModal(t *testing.T) {
	e := newNativeEnv(t)
	asked := 0
	m := popups.NewModal(e.a, popups.ModalOpts{Title: "Guarded", Width: 400,
		Body:       func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(300, 80)} },
		CloseGuard: func() bool { asked++; return false }})
	e.a.ShowOverlay(m)
	e.settle(300 * time.Millisecond)
	e.escape()
	if asked != 1 {
		t.Fatalf("Escape asked the guard %d times; want once", asked)
	}
	if m.Dismissed() || !slices.Contains(e.a.overlays, Overlay(m)) {
		t.Error("the guard kept the modal open and Escape dropped it from the window's overlays anyway")
	}
	m.Close()
	e.settle(200 * time.Millisecond)
}

// A hover peek never takes the focus, so it has none to give back: one
// that opens and closes with the focus nowhere leaves it nowhere (it was
// an overlay layer, and its close focused the channel's composer).
func TestHoverPeekGivesNoFocus(t *testing.T) {
	e := newNativeEnv(t)
	e.r.Source().Execute(key.FocusCmd{})
	e.settle(200 * time.Millisecond)
	if !e.r.Source().Focused(nil) {
		t.Fatal("something took the focus back")
	}
	pop := popups.NewPopover(e.a, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(200, 100)} },
		popups.PopoverOpts{Anchor: image.Rect(100, 100, 130, 130), Peek: true})
	e.a.ShowOverlay(pop)
	e.settle(300 * time.Millisecond)
	pop.Dismiss()
	e.settle(300 * time.Millisecond)
	if e.composerFocused() || !e.r.Source().Focused(nil) {
		t.Errorf("a hover peek came and went: the focus is on %v (composer %v); want nowhere", e.r.Source().FocusTag(), e.composerFocused())
	}
}

// A modal opened from a menu item gives the focus back, when it closes,
// to what had it before the menu opened: the thread pane's reply
// composer here, not the channel's (the menu, which had the focus when
// the modal opened, was gone).
func TestModalFromMenuGivesFocusBackToThreadComposer(t *testing.T) {
	e := threadsAuditEnv(t, false)
	ch, root := fakeslack.ThreadsAuditChannel, fakeslack.ThreadsAuditLong
	e.openChannel(ch)
	e.a.OpenThread(ch, root)
	e.until("the thread", 15*time.Second, func() bool {
		tv := e.threadView(ch, root)
		return tv != nil && tv.ContentHeight() >= 0 && len(tv.Channel()) > 0
	})
	focused := func(k string) bool {
		c := e.a.convos[k]
		if c == nil {
			return false
		}
		f, ok := c.comp.(interface{ Focused() bool })
		return ok && f.Focused()
	}
	e.a.FocusThreadComposer(ch, root)
	e.settle(300 * time.Millisecond)
	if !focused(ch + "/" + root) {
		t.Fatal("the thread composer didn't take the focus")
	}
	var modal *popups.Modal
	m := popups.NewMenu(e.a, image.Pt(600, 400), []popups.MenuItem{{Label: "Custom time", Do: func() {
		modal = popups.NewModal(e.a, popups.ModalOpts{Title: "Custom time", Width: 400,
			Body:    func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(300, 80)} },
			Buttons: []*popups.ModalButton{{Label: "Cancel", Do: func() bool { return true }}}})
		e.a.ShowOverlay(modal)
	}}})
	e.a.ShowOverlay(m)
	e.settle(300 * time.Millisecond)
	e.key(key.NameDownArrow, 0)
	e.settle(100 * time.Millisecond)
	e.key(key.NameReturn, 0)
	e.until("the modal the menu item opened", 5*time.Second, func() bool { return modal != nil })
	e.settle(300 * time.Millisecond)
	if focused(ch + "/" + root) {
		t.Fatal("the modal didn't take the focus from the thread composer")
	}
	e.key(key.NameEscape, 0)
	e.settle(400 * time.Millisecond)
	if !modal.Dismissed() {
		t.Fatal("Escape didn't close the modal")
	}
	if !focused(ch + "/" + root) {
		t.Errorf("the modal opened from the menu closed: the focus went to %v (channel composer %v); want the thread composer",
			e.r.Source().FocusTag(), focused(ch+"/"))
	}
}

// Replay introduction closes everything over the app, past a CloseGuard.
// With a dirty dialog under Preferences (its guard puts a speed bump up
// and refuses, every time it's asked) the close loop never ended: each
// pass closed the speed bump the last one put up, and the UI froze.
func TestReplayIntroClosesGuardedModal(t *testing.T) {
	e := newNativeEnv(t)
	asked := 0
	m := popups.NewModal(e.a, popups.ModalOpts{Title: "Dirty", Width: 400,
		Body: func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(300, 80)} },
		CloseGuard: func() bool { // as the snippet dialog's: a speed bump each time, never yes
			asked++
			popups.Confirm(e.a, "Discard it?", "", "Discard", true, nil, func() {})
			return false
		}})
	e.a.ShowOverlay(m)
	e.settle(200 * time.Millisecond)
	e.a.showPrefs("notifications")
	e.settle(300 * time.Millisecond)
	if _, ok := topOverlay[*popups.Modal](e.a); !ok || len(e.a.overlays) < 2 {
		t.Fatalf("Preferences didn't open over the dirty dialog (%d overlays)", len(e.a.overlays))
	}
	e.a.motionOverride = -1 // no OS motion watcher (as the intro tests)
	hang := time.AfterFunc(20*time.Second, func() { panic("replayIntro never returned: its close loop spins on the guarded modal") })
	e.a.replayIntro()
	hang.Stop()
	t.Cleanup(func() { e.a.intro.close(); e.a.intro = nil })
	if !m.Dismissed() {
		t.Error("Replay introduction left the dirty dialog open")
	}
	for _, o := range e.a.overlays {
		if !o.Dismissed() && !invisible(o) {
			t.Errorf("Replay introduction left %T open", o)
		}
	}
	if asked != 0 {
		t.Errorf("the dialog's guard was asked %d times; the replay closes past it", asked)
	}
	if e.a.intro == nil {
		t.Error("the replay didn't start")
	}
}

// Escape on Edit profile with unsaved edits puts its speed bump up and
// keeps the dialog: the shell's stack held the EditProfileModal wrapper,
// which said nothing of its modal's guard, so Escape dropped the dialog
// behind its own "Keep Editing / Close & Discard" and the edits went.
func TestEscapeKeepsDirtyEditProfile(t *testing.T) {
	e := newNativeEnv(t)
	ep := popups.OpenEditProfile(e.a, popups.EditProfileOpts{})
	e.settle(400 * time.Millisecond)
	f := ep.Form()
	field := ""
	for _, n := range f.Fields() {
		if !f.Locked(n) {
			field = n
			break
		}
	}
	if field == "" {
		t.Fatal("no editable field on Edit profile")
	}
	f.SetField(field, "Changed by the test")
	e.settle(200 * time.Millisecond)
	e.escape()
	if ep.Dismissed() || !slices.Contains(e.a.overlays, Overlay(ep)) {
		t.Fatal("Escape on dirty Edit profile closed it (its speed bump never showed, the edits went)")
	}
	if !f.SpeedBump() {
		t.Error("Escape on dirty Edit profile didn't put its speed bump up")
	}
	e.escape() // on the speed bump: it goes, the dialog stays
	if ep.Dismissed() || f.SpeedBump() {
		t.Errorf("Escape on the speed bump: dialog closed %v, bump up %v; want the bump gone and the dialog kept", ep.Dismissed(), f.SpeedBump())
	}
	ep.Close()
	e.settle(200 * time.Millisecond)
}
