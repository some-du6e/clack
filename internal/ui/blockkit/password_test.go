package blockkit

import (
	"strings"
	"testing"
	"time"

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
