package taut

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

// Bridge.CallTruthy answers the way Slack's MAMenu reads onSelectAction's
// return in dispatchMessageAction (`onSelectAction(…) || native(…)`): a
// truthy value, a Promise included (it isn't awaited), takes the pick; a
// falsy one passes it on; a throw takes it (Slack's own handler never
// runs); a function JS no longer has passes it on (no handler is there).
// The pick's event arrives as React's synthetic event.
func TestCallTruthy(t *testing.T) {
	h := newHarness(t)
	var ids map[string]float64
	h.jsInto(`
		globalThis.__truthyArgs = []
		const fns = {
			yes: () => "taken",
			no: () => 0,
			nothing: () => {},
			promise: () => Promise.resolve(false),
			boom: () => { throw new Error("the plugin's handler failed") },
			event: (e, action, context) => {
				globalThis.__truthyArgs.push([e?.type, typeof e?.preventDefault, action?.id, context?.ts])
				return null
			},
		}
		const out = {}
		for (const [k, f] of Object.entries(fns)) out[k] = T.encode(f).$fn
		return out`, &ids)
	call := func(name, args string) (truthy, threw bool, err error) {
		t.Helper()
		done := make(chan struct{})
		h.tt.Bridge().CallTruthy(int64(ids[name]), []byte(args), func(tr, th bool, e error) {
			truthy, threw, err = tr, th, e
			close(done)
		})
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: no answer", name)
		}
		return truthy, threw, err
	}
	for _, c := range []struct {
		name          string
		truthy, threw bool
	}{
		{"yes", true, false},
		{"no", false, false},
		{"nothing", false, false},
		{"promise", true, false},
		{"boom", false, true},
	} {
		if tr, th, err := call(c.name, "[]"); err != nil || tr != c.truthy || th != c.threw {
			t.Errorf("%s: truthy %v threw %v err %v, want truthy %v threw %v", c.name, tr, th, err, c.truthy, c.threw)
		}
	}

	if tr, th, err := call("event", `[{"$event":{"type":"click","button":0}},{"id":"copy_link"},{"ts":"1700000000.000100"}]`); err != nil || tr || th {
		t.Errorf("event: truthy %v threw %v err %v", tr, th, err)
	}
	var seen string
	h.jsInto(`return JSON.stringify(globalThis.__truthyArgs)`, &seen)
	if seen != `[["click","function","copy_link","1700000000.000100"]]` {
		t.Errorf("the handler saw %s", seen)
	}

	// released with its tree: no handler to decide
	var none any
	h.jsInto(`T.releaseFns([`+strconv.FormatInt(int64(ids["no"]), 10)+`]); return null`, &none)
	if _, _, err := call("no", "[]"); !errors.Is(err, ErrFnGone) {
		t.Errorf("a released function: err %v, want ErrFnGone", err)
	}
}
