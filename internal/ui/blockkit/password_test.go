package blockkit

import (
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
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
	v, err := core.ParseView(mustJSON(map[string]any{"id": "VSURF0PW02", "type": "modal", "callback_id": "pw", "hash": "p2",
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
