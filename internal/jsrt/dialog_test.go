package jsrt

import (
	"context"
	"strings"
	"testing"
	"time"
)

// <dialog>: showModal() puts it in the top layer (serialized as "modal",
// no attribute a plugin could see), focuses its autofocus control, and
// Escape fires cancel then close, as browsers do (HaikuWarning listens
// for close). show() opens it in place.
func TestDialogShowModal(t *testing.T) {
	r := newTest(t)
	bodies := make(chan string, 64)
	r.OnDocument(func(body []byte, css []string) { bodies <- string(body) })
	waitBody := func(what string, ok func(string) bool) string {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case b := <-bodies:
				if ok(b) {
					return b
				}
			case <-deadline:
				t.Fatalf("timed out waiting for a document with %s", what)
			}
		}
	}
	got := evalString(t, r, `(() => {
  const d = document.createElement('dialog'); d.id = 'd'
  const one = document.createElement('button'); one.textContent = 'one'
  const two = document.createElement('button'); two.autofocus = true; two.textContent = 'two'
  d.append(one, two); document.body.append(d)
  globalThis.events = []
  d.addEventListener('cancel', () => events.push('cancel'))
  d.addEventListener('close', () => events.push('close'))
  d.showModal()
  let threw = ''
  try { d.show() } catch (e) { threw = e.name }
  d.showModal() // already modal: nothing happens
  return JSON.stringify([d.open, d.getAttributeNames().join(','), document.activeElement === two, threw, events.length])
})()`)
	if got != `[true,"id,open",true,"InvalidStateError",0]` {
		t.Fatalf("after showModal(): %s", got)
	}
	b := waitBody(`a modal dialog`, func(b string) bool { return strings.Contains(b, `"modal":true`) })
	if !strings.Contains(b, `"t":"dialog"`) || !strings.Contains(b, `"focus":true`) {
		t.Errorf("document: %s", b)
	}

	// Escape (Go sends keys for the document to the focused element)
	if _, err := r.DispatchEventSync(context.Background(), 0, []byte(`{"type":"keydown","key":"Escape","code":"Escape"}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		got = evalString(t, r, `JSON.stringify([events, document.getElementById('d').open, document.getElementById('d').returnValue, document.activeElement === document.body])`)
		if strings.Contains(got, `"close"`) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got != `[["cancel","close"],false,"",true]` {
		t.Errorf("after Escape: %s", got)
	}
	waitBody(`the dialog closed`, func(b string) bool { return strings.Contains(b, `"t":"dialog"`) && !strings.Contains(b, `"modal"`) })

	// a cancel listener that prevents it keeps the dialog open; close()
	// on a closed dialog fires nothing; show() is never modal
	got = evalString(t, r, `(() => {
  const d = document.getElementById('d')
  events.length = 0
  d.addEventListener('cancel', (e) => e.preventDefault())
  d.showModal()
  return String(d.open)
})()`)
	if got != "true" {
		t.Fatalf("reopen: %s", got)
	}
	if _, err := r.DispatchEventSync(context.Background(), 0, []byte(`{"type":"keydown","key":"Escape","code":"Escape"}`)); err != nil {
		t.Fatal(err)
	}
	got = evalString(t, r, `(() => {
  const d = document.getElementById('d')
  const kept = d.open
  d.close('done')
  const rv = d.returnValue
  d.close()
  d.show()
  return JSON.stringify([kept, events, rv, d.open])
})()`)
	if got != `[true,["cancel"],"done",true]` {
		t.Errorf("prevented cancel / close / show: %s", got)
	}
	time.Sleep(50 * time.Millisecond)
	if got = evalString(t, r, `JSON.stringify(events)`); got != `["cancel","close"]` {
		t.Errorf("events after close(), close() again: %s (one close)", got)
	}
	waitBody(`a non-modal open dialog`, func(b string) bool {
		return strings.Contains(b, `"open":""`) && !strings.Contains(b, `"modal"`)
	})

	// showModal() on a dialog outside the document throws
	if got = evalString(t, r, `(() => { try { document.createElement('dialog').showModal(); return 'no' } catch (e) { return e.name } })()`); got != "InvalidStateError" {
		t.Errorf("showModal() on a disconnected dialog: %s", got)
	}
}
