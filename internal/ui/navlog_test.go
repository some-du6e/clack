package ui

import (
	"strings"
	"testing"
)

// deepLinkMessage takes slack://channel links naming a message, a thread
// or a thread's latest reply for jumpToMessage, in
// formatMessageTimestamp's forms; the channel alone stays deepLinkRoute's,
// and nothing that doesn't look like Slack's ids gets through.
func TestDeepLinkMessage(t *testing.T) {
	for _, c := range []struct {
		uri, ch, ts, thread string
		latest, ok          bool
	}{
		{"slack://channel?team=T1&id=C123&message=1700000060.000200", "C123", "1700000060.000200", "", false, true},
		{"slack://channel?team=T1&id=C123&message=p1700000060000200&thread_ts=1700000000000100", "C123", "1700000060.000200", "1700000000.000100", false, true},
		{"slack://channel?team=T1&id=D123&message=1700000060.000200&thread_ts=1700000000.000100", "D123", "1700000060.000200", "1700000000.000100", false, true},
		{"slack://channel?team=T1&id=C123", "", "", "", false, false},
		// a thread alone is toThread with highlightRoot; message=latest its latest reply
		{"slack://channel?team=T1&id=C123&thread_ts=1700000000.000100", "C123", "", "1700000000.000100", false, true},
		{"slack://channel?team=T1&id=C123&message=latest&thread_ts=1700000000.000100", "C123", "", "1700000000.000100", true, true},
		{"slack://channel?team=T1&id=C123&message=latest", "", "", "", false, false},
		{"slack://channel?team=T1&id=C123&message=1/x", "", "", "", false, false},
		{"slack://channel?team=T1&id=C123&message=1700000060.000200&thread_ts=1/x", "", "", "", false, false},
		{"slack://channel?team=T1&id=U123&message=1700000060.000200", "", "", "", false, false},
		{"slack://user?team=T1&id=C123&message=1700000060.000200", "", "", "", false, false},
		{"https://x.slack.com/archives/C123/p1700000060000200", "", "", "", false, false},
	} {
		ch, ts, thread, latest, ok := deepLinkMessage(c.uri)
		if ok != c.ok || ok && (ch != c.ch || ts != c.ts || thread != c.thread || latest != c.latest) {
			t.Errorf("%s: %q %q %q %v %v, want %q %q %q %v %v", c.uri, ch, ts, thread, latest, ok, c.ch, c.ts, c.thread, c.latest, c.ok)
		}
	}
	for in, want := range map[string]string{"1700000060.000200": "1700000060.000200", "p1700000060000200": "1700000060.000200",
		"1700000060000200": "1700000060.000200", "170000006.000200": "", "1700000060.00020": "", "": "", "x1700000060000200": ""} {
		if got := slackTimestamp(in); got != want {
			t.Errorf("slackTimestamp(%q) = %q, want %q", in, got, want)
		}
	}
}

// The log's tags are INSPECT.md's (kind and six hex digits, one salt per
// run), and a navigation's source is the first caller outside the router.
func TestNavLogTags(t *testing.T) {
	ch, ts := logTag("C0THPUBLIC1"), logTag("1762516060.000100")
	if !strings.HasPrefix(ch, "ch:") || len(ch) != 9 || !strings.HasPrefix(ts, "ts:") || len(ts) != 9 || logTag("") != "-" {
		t.Errorf("tags %q %q %q", ch, ts, logTag(""))
	}
	if logTag("C0THPUBLIC1") != ch || logTag("C0THPRIVAT1") == ch {
		t.Errorf("one id, one tag within a run; two ids, two")
	}
	if via := func() string { return callerOutsideRouter() }(); !strings.HasPrefix(via, "ui.TestNavLogTags") || !strings.Contains(via, "navlog_test.go:") {
		t.Errorf("callerOutsideRouter from a test: %q", via)
	}
	for name, want := range map[string]bool{"ui.(*App).Navigate": true, "ui.(*App).jumpToMessage.func1.1": true, "ui.(*App).OpenThread": true,
		"ui/messages.(*richHandler).OpenURL": false, "ui.(*App).Back": false, "ui.(*App).OpenThreadReplyX": false} {
		if got := isRouterFunc(name); got != want {
			t.Errorf("isRouterFunc(%q) = %v, want %v", name, got, want)
		}
	}
}
