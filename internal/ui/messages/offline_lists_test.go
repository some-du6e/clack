package messages

// The offline list states (offline_indicators.go): while !isClientOnline
// a channel window short of its start draws InlineOfflineIndicator at the
// top ("No connection." and "Go online to load more messages.") where the
// history loader's skeleton was, and a conversation with no messages yet
// draws LargeOfflineIndicator ("No connection" and "Go online to get the
// latest messages."). Synthetic history on the fake.

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/slack"
)

// labelsIn runs f with labelHook recording every label drawn.
func labelsIn(f func()) []string {
	var mu sync.Mutex
	var out []string
	prev := labelHook
	labelHook = func(txt string, _ textStyle) { mu.Lock(); out = append(out, txt); mu.Unlock() }
	f()
	labelHook = prev
	mu.Lock()
	defer mu.Unlock()
	return out
}

func hasLabelTrim(ls []string, s string) bool {
	for _, l := range ls {
		if strings.TrimSpace(l) == s {
			return true
		}
	}
	return false
}

func TestHistoryLoaderOfflineLikeSlack(t *testing.T) {
	var online atomic.Bool
	online.Store(true)
	prev := clientOnline
	clientOnline = func(Host) bool { return online.Load() }
	t.Cleanup(func() { clientOnline = prev })

	e, _ := guardEnv(t, "C0OFFLINE1", nil, nil)
	if ls := labelsIn(e.rt.frame); hasLabelTrim(ls, "No connection.") {
		t.Fatalf("control: online, the list draws the offline indicator: %q", ls)
	}
	// offline, then at the window's top: the history loader's row, in the
	// first frame there (before a page older could land)
	online.Store(false)
	if len(e.v.s.items) == 0 || e.v.s.items[0].kind != itLoading {
		t.Fatal("control: the window's first row isn't the history loader (the fake's history is one page?)")
	}
	e.v.s.list.Position = layout.Position{BeforeEnd: true, First: 0} // the oldest row at the top ({} is the newest end)
	e.v.s.stopScroll()
	ls := labelsIn(e.rt.frame)
	if !hasLabelTrim(ls, "No connection.") || !hasLabelTrim(ls, "Go online to load more messages.") {
		t.Errorf("offline, the loader row draws %q; want InlineOfflineIndicator's \"No connection.\" and \"Go online to load more messages.\"", ls)
	}

	// a conversation with no messages yet, its load held
	const empty = "C0OFFLINE2"
	release := HoldLoad(empty, "")
	t.Cleanup(release)
	e.h.app.S.PutConv(slack.Conversation{ID: empty, Name: "offline-empty", IsChannel: true, IsMember: true, NumMembers: 3})
	v := NewChannelView(e.h, empty)
	rt := newRouted(v, 900, 700)
	rt.frame()
	ls = labelsIn(rt.frame)
	if !hasLabelTrim(ls, "No connection") || !hasLabelTrim(ls, "Go online to get the latest messages.") {
		t.Errorf("offline, an empty conversation draws %q; want LargeOfflineIndicator", ls)
	}
}
