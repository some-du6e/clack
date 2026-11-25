package taut

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// patchOnFocus is a plugin's part that patches Slack's composer clipboard
// the way Slick's ClearURLs does: on a composer's focusin, through the
// clipboard module's class prototype, once; __restore puts it back.
const patchOnFocus = `globalThis.__seen = []
document.addEventListener('focusin', (e) => {
  const c = e.target.closest && e.target.closest('.ql-container')
  __seen.push(c ? c.getAttribute('data-composer') : 'none')
  const cb = c && c.__quill && c.__quill.getModule('clipboard')
  const proto = cb && Object.getPrototypeOf(cb)
  if (!proto || typeof proto.preparePastedDelta !== 'function' || globalThis.__restore) return
  const prepare = proto.preparePastedDelta
  proto.preparePastedDelta = function (args) {
    const ops = args.pastedDelta.ops.map((op) => typeof op.insert === 'string' ? { ...op, insert: op.insert.replace('utm_source=x&', '') } : op)
    return prepare.call(this, { ...args, pastedDelta: new args.pastedDelta.constructor(ops) })
  }
  globalThis.__restore = () => { proto.preparePastedDelta = prepare }
}, true)
return 0`

// Slack's composer as Quill (05_slack.js): each native composer kind has a
// stand-in .ql-container whose editor is document.activeElement while the
// composer has the keyboard focus (focusin on it, as Slack's), and whose
// __quill's clipboard a plugin patches by its class's prototype. A paste
// goes through the clipboard's preparePastedDelta only while a plugin has
// replaced it (Go is told), with the pasted delta's ops and the caret's
// formats, and what it returns goes in; the plugin's restore turns that
// off again.
func TestQuillClipboardStandIn(t *testing.T) {
	h := newHarness(t)
	ctx := h.ctx()
	ops := json.RawMessage(`[{"insert":"see https://example.com/?utm_source=x&id=7\n"}]`)
	if pasteClipboardOn(h.tt) {
		t.Fatal("pastes go through JS before a plugin patched the clipboard")
	}
	if out, changed := preparePaste(ctx, h.tt, "channel", ops, nil); changed || string(out) != string(ops) {
		t.Fatalf("an unpatched clipboard changed the paste: %s", out)
	}
	h.js(patchOnFocus)
	composerFocus(h.tt, "thread", true)
	eventually(t, 5*time.Second, "the plugin's patch told to Go", func() bool { return pasteClipboardOn(h.tt) })
	var st struct {
		Seen   []string
		Active string
	}
	h.jsInto(`const a = document.activeElement, c = a.closest && a.closest('.ql-container')
return { Seen: __seen, Active: c ? c.getAttribute('data-composer') + ' ' + a.className : a.tagName }`, &st)
	if !slices.Equal(st.Seen, []string{"thread"}) || st.Active != "thread ql-editor" {
		t.Errorf("focusin seen on %q, the active element %q; want the thread composer's .ql-editor", st.Seen, st.Active)
	}
	out, changed := preparePaste(ctx, h.tt, "channel", ops, map[string]any{"blockquote": true})
	if !changed || strings.Contains(string(out), "utm_source") || !strings.Contains(string(out), "id=7") {
		t.Errorf("the patched clipboard's paste: %s (changed %v)", out, changed)
	}
	// the composer's blur leaves the focus nowhere
	composerFocus(h.tt, "thread", false)
	eventually(t, 5*time.Second, "the blur", func() bool {
		var a string
		h.jsInto(`return document.activeElement.tagName`, &a)
		return a == "BODY"
	})
	// the plugin puts the clipboard back: pastes stop going through it
	h.js(`__restore(); return 0`)
	eventually(t, 5*time.Second, "the restore told to Go", func() bool { return !pasteClipboardOn(h.tt) })
	if out, changed := preparePaste(ctx, h.tt, "channel", ops, nil); changed {
		t.Errorf("after the restore the paste changed: %s", out)
	}
}

// slowOnPaste patches the composer clipboard as patchOnFocus does, with a
// preparePastedDelta that takes 3 s and then changes the paste.
const slowOnPaste = `globalThis.__ran = 0
document.addEventListener('focusin', (e) => {
  const c = e.target.closest && e.target.closest('.ql-container')
  const cb = c && c.__quill && c.__quill.getModule('clipboard')
  const proto = cb && Object.getPrototypeOf(cb)
  if (!proto || typeof proto.preparePastedDelta !== 'function' || globalThis.__slow) return
  const prepare = proto.preparePastedDelta
  globalThis.__slow = true
  proto.preparePastedDelta = function (args) {
    const end = Date.now() + 3000
    while (Date.now() < end) {}
    __ran++
    return prepare.call(this, { ...args, pastedDelta: new args.pastedDelta.constructor([{ insert: 'late\n' }]) })
  }
}, true)
return 0`

// The composer's deadline bounds a plugin's preparePastedDelta while it
// runs, not only while it waits for the JS thread: the paste goes in as
// it came at the deadline, and the call runs on to its end, its answer
// dropped. It held the UI goroutine until the plugin returned, up to
// jsrt's task limit.
func TestPreparePasteBoundedWhileRunning(t *testing.T) {
	h := newHarness(t)
	h.js(slowOnPaste)
	composerFocus(h.tt, "channel", true)
	eventually(t, 5*time.Second, "the plugin's patch told to Go", func() bool { return pasteClipboardOn(h.tt) })
	h.js(`return 0`) // nothing queued ahead of the paste: its call runs at once
	ops := json.RawMessage(`[{"insert":"see https://example.com/\n"}]`)
	// the composer's is 150 ms; 500 so a slow runner surely starts the call first
	ctx, cancel := context.WithTimeout(UserAction(context.Background()), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, changed := preparePaste(ctx, h.tt, "channel", ops, nil)
	took := time.Since(start)
	if changed || string(out) != string(ops) {
		t.Errorf("a paste whose plugin call ran past the deadline came back %s (changed %v); want it as it came", out, changed)
	}
	if took > 2*time.Second {
		t.Errorf("the paste waited %v for a 3 s plugin call, past its 500 ms", took)
	}
	var ran int
	h.jsInto(`return __ran`, &ran) // after the call: it holds the JS thread
	if ran != 1 {
		t.Errorf("the plugin's call ran %d times, want once, to its end", ran)
	}
}

// A composer focused before the runtime starts is focused in it once it
// has: a plugin that starts then sees the stand-in editor as
// document.activeElement.
func TestQuillFocusBeforeStart(t *testing.T) {
	h := newHarness(t)
	composerFocus(h.tt, "edit", true)
	eventually(t, 5*time.Second, "the edit composer's editor active", func() bool {
		var a string
		h.jsInto(`const a = document.activeElement, c = a.closest && a.closest('.ql-container')
return c ? c.getAttribute('data-composer') : a.tagName`, &a)
		return a == "edit"
	})
}
