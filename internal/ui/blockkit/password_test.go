package blockkit

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// Synthetic fixtures only.

// Slack's PasswordElement (mm++; CDN build 9f450b8ad3db) is an <input
// type="password">: its text draws as bullets, and its placeholder is the
// element's or "Write something". What it holds goes in the state as
// {type: password, value}, trimmed, and null when empty.
func TestPasswordElement(t *testing.T) {
	e := newBKEnv(t, false, false)
	pt := func(s string) map[string]any { return map[string]any{"type": "plain_text", "text": s} }
	in := func(bid string, el map[string]any) map[string]any {
		el["type"], el["action_id"] = "password", "v"
		return map[string]any{"type": "input", "block_id": bid, "optional": true, "label": pt("Token"), "element": el}
	}
	v, err := core.ParseView(mustJSON(map[string]any{"id": "VSURF0PW01", "type": "modal", "callback_id": "pw", "hash": "p1",
		"title": pt("Connect"), "submit": pt("Save"), "close": pt("Cancel"), "state": map[string]any{"values": map[string]any{}},
		"blocks": []any{in("plain", map[string]any{}), in("hinted", map[string]any{"placeholder": pt("Paste your token")})}}))
	if err != nil {
		t.Fatal(err)
	}
	s := newStack(e.h, v)
	e.h.ShowOverlay(s)
	e.frame()
	e.frame()
	widget := func(bid string) *ew {
		t.Helper()
		sf := s.top()
		for _, b := range allBlocks(sf.blocks) {
			if b.BlockID == bid && b.Element != nil {
				if w := sf.ws[surfKey(b, b.Element)]; w != nil {
					return w
				}
			}
		}
		t.Fatalf("no widget for %s", bid)
		return nil
	}
	field := func(bid string) FieldInfo {
		t.Helper()
		for _, f := range s.fields() {
			if f.BlockID == bid {
				return f
			}
		}
		t.Fatalf("no field %s (fields %+v)", bid, s.fields())
		return FieldInfo{}
	}
	for _, c := range []struct{ bid, ph string }{{"plain", "Write something"}, {"hinted", "Paste your token"}} {
		w := widget(c.bid)
		if w.ed == nil {
			t.Fatalf("%s: no text editor", c.bid)
		}
		if w.ed.Mask != '•' || w.textPlaceholder() != c.ph {
			t.Errorf("%s: mask %q placeholder %q, want bullets and %q", c.bid, w.ed.Mask, w.textPlaceholder(), c.ph)
		}
	}

	f := field("plain")
	if f.R.Empty() {
		t.Fatalf("the password field wasn't drawn: %+v", f)
	}
	e.click(center(f.R))
	e.frame()
	e.frame()
	if !field("plain").Focused {
		t.Fatalf("a click at %v didn't focus the password field", center(f.R))
	}
	typeIn(e, "  hunter2 ")
	if f := field("plain"); f.Text != "  hunter2 " || f.Value != "hunter2" {
		t.Errorf("field holds %q with state value %q, want %q and the trimmed %q", f.Text, f.Value, "  hunter2 ", "hunter2")
	}
	n := len(submittedState(e))
	s.doSubmit()
	e.until("views.submit", 10*time.Second, func() bool { return len(submittedState(e)) > n })
	st := submittedState(e)[n]
	if !strings.Contains(st, `"plain":{"v":{"type":"password","value":"hunter2"}}`) {
		t.Errorf("views.submit state %s, want plain.v = {type: password, value: hunter2}", st)
	}
	if strings.Contains(st, `"hinted"`) && !strings.Contains(st, `"hinted":{"v":{"type":"password","value":null}}`) {
		t.Errorf("views.submit state %s, want the empty password's value null", st)
	}
	e.closeOverlays()
}

// Slack's PasswordElement (mm++) calls onAction(e, !1) and has no
// onKeyDown: in an input block nothing dispatches it, whatever
// dispatch_action and dispatch_action_config say (useInputBlockValidation
// dispatches only with shouldDispatch), so a password never reaches the
// app in blocks.actions from there. Outside one, wrappedOnAction sends
// each change, 500 ms after the last, and Enter sends nothing. The editor
// tells the input method it holds a password.
func TestPasswordNeverDispatchesInInputBlocks(t *testing.T) {
	e := newBKEnv(t, false, false)
	pt := func(s string) map[string]any { return map[string]any{"type": "plain_text", "text": s} }
	// the gallery app's, so an action would go out (BlockActionSend needs
	// the view's service id): the check below is that none does
	v, err := core.ParseView(mustJSON(map[string]any{"id": "VSURF0PW02", "type": "modal", "callback_id": "pw", "hash": "p2",
		"bot_id": fakeslack.GalleryBotID, "app_id": fakeslack.GalleryAppID,
		"title": pt("Connect"), "submit": pt("Save"), "close": pt("Cancel"), "state": map[string]any{"values": map[string]any{}},
		"blocks": []any{map[string]any{"type": "input", "block_id": "pw", "dispatch_action": true, "label": pt("Token"),
			"element": map[string]any{"type": "password", "action_id": "v",
				"dispatch_action_config": map[string]any{"trigger_actions_on": []string{"on_enter_pressed", "on_character_entered"}}}}}}))
	if err != nil {
		t.Fatal(err)
	}
	s := newStack(e.h, v)
	e.h.ShowOverlay(s)
	e.frame()
	e.frame()
	var f FieldInfo
	for _, x := range s.fields() {
		if x.BlockID == "pw" {
			f = x
		}
	}
	if f.R.Empty() {
		t.Fatalf("the password field wasn't drawn: %+v", s.fields())
	}
	e.click(center(f.R))
	e.frame()
	e.frame()
	n := len(e.fs.BlockActionCalls())
	typeIn(e, "hunter2")
	e.settle(700 * time.Millisecond) // past on_character_entered's debounce
	e.key(key.NameReturn)
	e.noNewCall(n, 700*time.Millisecond, "a password typed and entered in a dispatching input block")
	var w *ew
	for _, b := range allBlocks(s.top().blocks) {
		if b.BlockID == "pw" {
			w = s.top().ws[surfKey(b, b.Element)]
		}
	}
	if w == nil || w.ed == nil || w.ed.InputHint != key.HintPassword {
		t.Errorf("the password editor's input hint isn't HintPassword: %+v", w)
	}
	// outside an input block: each change (debounced), never Enter
	for _, c := range []struct {
		input       bool
		enter, char bool
	}{{true, false, false}, {false, false, true}} {
		x := &ew{e: &Element{Type: "password"}, b: &Block{DispatchAction: true}, input: c.input}
		if x.dispatchOn("on_enter_pressed") != c.enter || x.dispatchOn("on_character_entered") != c.char {
			t.Errorf("input %v: Enter dispatches %v, a change %v; want %v, %v", c.input, x.dispatchOn("on_enter_pressed"), x.dispatchOn("on_character_entered"), c.enter, c.char)
		}
	}
	// once the modal closes, the typed password is gone from memory
	if w != nil && w.ed != nil && w.ed.Text() == "" {
		t.Fatal("nothing was typed")
	}
	s.Close()
	e.frame()
	if w != nil && w.ed != nil && w.ed.Text() != "" {
		t.Error("the password stayed in its editor after the modal closed")
	}
	if v := s.top().vals.get("pw", "v"); v != nil && v.Text != "" {
		t.Error("the password stayed in the view's state after the modal closed")
	}
	e.closeOverlays()
}

// The password's other paths: multiline doesn't make it more than the one
// line of an <input>; outside an input block a change goes 500 ms after
// the last (wrappedOnAction's trailing debounce) and Enter sends nothing;
// once its view goes, the editor is a fresh one (no undo history holds the
// text) and the view's initial state, which a views.update's snapshot
// holds the typed text in, is dropped.
func TestPasswordPaths(t *testing.T) {
	e := newBKEnv(t, false, false)
	pt := func(s string) map[string]any { return map[string]any{"type": "plain_text", "text": s} }
	v, err := core.ParseView(mustJSON(map[string]any{"id": "VSURF0PW03", "type": "modal", "callback_id": "pw", "hash": "p3",
		"bot_id": fakeslack.GalleryBotID, "app_id": fakeslack.GalleryAppID,
		"title": pt("Connect"), "close": pt("Cancel"), "state": map[string]any{"values": map[string]any{}},
		"blocks": []any{
			map[string]any{"type": "input", "block_id": "multi", "optional": true, "label": pt("Token"),
				"element": map[string]any{"type": "password", "action_id": "v", "multiline": true}},
			map[string]any{"type": "actions", "block_id": "act",
				"elements": []any{map[string]any{"type": "password", "action_id": "p"}}},
		}}))
	if err != nil {
		t.Fatal(err)
	}
	s := newStack(e.h, v)
	e.h.ShowOverlay(s)
	e.frame()
	e.frame()
	sf := s.top()
	var multi, act *ew
	for _, b := range allBlocks(sf.blocks) {
		switch {
		case b.BlockID == "multi" && b.Element != nil:
			multi = sf.ws[surfKey(b, b.Element)]
		case b.BlockID == "act" && len(b.Elements) == 1:
			act = sf.ws[surfKey(b, &b.Elements[0])]
		}
	}
	if multi == nil || multi.ed == nil || act == nil || act.ed == nil {
		t.Fatalf("the password widgets: %+v, %+v", multi, act)
	}
	if !multi.ed.SingleLine || multi.ed.Mask != '•' {
		t.Errorf("a multiline password: single line %v, mask %q; want one masked line", multi.ed.SingleLine, multi.ed.Mask)
	}

	// outside an input block: nothing while typing, the value once 500 ms
	// after the last change, nothing on Enter (the modal's first focus is
	// its button: a click focuses the password)
	e.click(sf.org.Add(act.pos).Add(image.Pt(20, 18)))
	e.frame()
	if !act.focusedD {
		t.Fatalf("a click at %v didn't focus the actions block's password (at %v)", sf.org.Add(act.pos), act.pos)
	}
	n := len(e.fs.BlockActionCalls())
	typeIn(e, "hunter2")
	e.noNewCall(n, 300*time.Millisecond, "a password as it's typed")
	e.until("the debounced change", 5*time.Second, func() bool { return len(e.fs.BlockActionCalls()) > n })
	calls := e.fs.BlockActionCalls()
	if len(calls) != n+1 || len(calls[n].Actions) != 1 || calls[n].Actions[0]["value"] != "hunter2" {
		t.Errorf("the change sent %+v, want one action with the value", calls[n:])
	}
	e.key(key.NameReturn)
	e.noNewCall(n+1, 700*time.Millisecond, "Enter in a password outside an input block")

	// a views.update keeps the values: its snapshot holds the typed text
	sf.set(sf.view, true)
	if !strings.Contains(sf.initial, "hunter2") {
		t.Fatalf("the update's snapshot %q doesn't hold the typed text: nothing to wipe", sf.initial)
	}
	old := act.ed
	s.Close()
	e.frame()
	if act.ed == old || act.ed.Text() != "" || old.Text() != "" {
		t.Error("the closed view's password editor is the one typed in (its undo history holds the text)")
	}
	if strings.Contains(sf.initial, "hunter2") {
		t.Error("the closed view's initial state still holds the typed password")
	}
	e.closeOverlays()
}
