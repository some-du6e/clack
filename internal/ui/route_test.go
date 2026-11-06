package ui

import (
	"testing"

	"gioui.org/io/key"
)

func TestRouteParts(t *testing.T) {
	r := Route("c/C123/t/1700000000.000100")
	if r.Channel() != "C123" || r.Thread() != "1700000000.000100" || r.Page() != "c" {
		t.Fatalf("got %q %q %q", r.Channel(), r.Thread(), r.Page())
	}
	if q := Route("search?q=hello world").Query(); q != "hello world" {
		t.Fatalf("query %q", q)
	}
	if Route("activity").Channel() != "" || Route("activity").Page() != "activity" {
		t.Fatal("page route")
	}
}

func TestRouterHistory(t *testing.T) {
	var h router
	for _, r := range []Route{"c/A", "c/B", "c/B", "activity"} {
		h.Push(r)
	}
	if len(h.stack) != 3 {
		t.Fatalf("dedupe: %v", h.stack)
	}
	if r, _ := h.Back(); r != "c/B" {
		t.Fatalf("back = %q", r)
	}
	h.Push("c/C") // truncates forward history
	if h.CanForward() {
		t.Fatal("forward after push")
	}
	if got := h.Recent(5); len(got) != 3 || got[0] != "c/C" {
		t.Fatalf("recent = %v", got)
	}
}

func TestParseCombo(t *testing.T) {
	n, m := parseCombo("Ctrl+Shift+A")
	if n != "A" || m != key.ModShortcut|key.ModShift {
		t.Fatalf("%q %v", n, m)
	}
	if n, m := parseCombo("Alt+Up"); n != key.NameUpArrow || m != key.ModAlt {
		t.Fatalf("%q %v", n, m)
	}
	if n, _ := parseCombo("Ctrl+["); n != "[" {
		t.Fatalf("%q", n)
	}
}
