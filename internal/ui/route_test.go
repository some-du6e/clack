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
	n, m := parseCombo("Mod+Shift+A")
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

func TestDeepLinkRouteOnlyNavigates(t *testing.T) {
	for _, uri := range []string{
		"slack://channel?team=T1&id=C1/t/1.2",           // route segment smuggled in the id
		"slack://channel?team=T1&id=C123%3Fq%3Dsecret",  // search?q=… smuggled
		"slack://channel?team=T1&id=../../x",            //
		"slack://channel?team=T1&id=C123&thread_ts=1/x", // bad ts
		"slack://channel?team=T1&id=U123",               // a user id isn't a channel
		"slack://user?team=T1&id=C123",                  // nor the other way round
		"slack://user?team=T1&id=",                      //
		"slack://file?team=T1&id=F123",                  // downloads aren't deep-linkable
		"slack://signout", "slack://join?id=C1", "slack://leave?id=C1", "slack://send?channel=C1&text=x",
		"slack://open/../../signout",
	} {
		if r, u, ok := deepLinkRoute(uri); ok && (u != "" || r != "home") {
			t.Errorf("%s: handled as %q %q", uri, r, u)
		}
	}
	if r, _, ok := deepLinkRoute("slack://channel?team=T1&id=G0ABC&thread_ts=1700000000.000100"); !ok || r != "c/G0ABC/t/1700000000.000100" {
		t.Errorf("valid link: %q %v", r, ok)
	}
}
