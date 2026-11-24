package views

import (
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
)

// The Threads view's message keys need keyboard focus, and a key an
// action takes goes no further:
//   - after a click on a row (no keyboard mode: FocusEventManager drops
//     hasKeyboardFocus on any mousedown), MessageActions binds no key, so
//     typing a reply types it: P pins nothing, its "p" is the reply box's;
//   - with keyboard focus P pins the message, and its letter goes nowhere;
//   - U marks the thread unread once (subscriptions.thread.mark read=0),
//     the focus stays on the message and nothing is typed, so the reply
//     box's focus doesn't mark it read again.
func TestThreadsKeysNeedKeyboardFocus(t *testing.T) {
	var mu sync.Mutex
	calls := map[string][]url.Values{}
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		switch method {
		case "pins.add", "subscriptions.thread.mark":
			mu.Lock()
			calls[method] = append(calls[method], f)
			mu.Unlock()
		}
		return nil, false
	})
	got := func(method string) []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return append([]url.Values(nil), calls[method]...)
	}
	h := newW26Host(app, false)
	v := NewThreads(h)
	e := &vaEnv{size: image.Pt(1000, 1100)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the followed threads", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 2 })
	e.frame()
	e.frame()
	p, _, _ := v.data.get()
	k := -1
	for i := range p.items {
		if v.unread(&p.items[i]) == 0 {
			k = i // a read thread: nothing marks it read on its own
		}
	}
	if k < 0 {
		t.Fatal("no read thread in the fixture")
	}
	key0 := p.items[k].key
	root := "root-" + key0
	settle := func() {
		for i := 0; i < 30; i++ {
			e.frame()
			time.Sleep(10 * time.Millisecond)
		}
	}

	// a click, then a letter: the reply box's
	kbClickRow(t, e, v.kb, root, 49)
	kbType(e, "P", 0, "p")
	settle()
	if n := len(got("pins.add")); n != 0 {
		t.Errorf("P after a click pinned the root (%d pins.add): typing a reply ran the letter's action", n)
	}
	c := v.cards[key0]
	if c == nil || c.comp == nil || !strings.HasSuffix(c.comp.DocText(), "p") {
		t.Fatalf("P after a click: the reply box didn't get the letter (card %v)", c != nil)
	}
	typed := c.comp.DocText()

	// keyboard focus: P pins it, and the letter goes nowhere
	kbClickRow(t, e, v.kb, root, 49)
	h.kbMode.Store(true) // as an arrow key would
	kbType(e, "P", 0, "p")
	e.until(t, "pins.add from P with keyboard focus", func() bool { return len(got("pins.add")) == 1 })
	settle()
	if c.comp.DocText() != typed {
		t.Errorf("P with keyboard focus also typed into the reply box: %q, was %q", c.comp.DocText(), typed)
	}

	// U: marked unread, and not read again by the reply box's focus
	before := len(got("subscriptions.thread.mark"))
	kbType(e, "U", 0, "u")
	e.until(t, "the thread marked unread", func() bool {
		for _, f := range got("subscriptions.thread.mark")[before:] {
			if f.Get("read") == "0" {
				return true
			}
		}
		return false
	})
	settle()
	time.Sleep(300 * time.Millisecond)
	settle()
	for _, f := range got("subscriptions.thread.mark")[before:] {
		if f.Get("read") == "1" {
			t.Errorf("U marked the thread unread, then read again (subscriptions.thread.mark %v)", f)
		}
	}
	if !v.kb.focused {
		t.Error("U moved the focus off the message")
	}
	if c.comp.DocText() != typed {
		t.Errorf("U typed into the reply box: %q, was %q", c.comp.DocText(), typed)
	}
	c.comp.Close()
}
