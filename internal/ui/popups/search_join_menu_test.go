package popups

// The Channels tab's "Join…" caret menu joins the channel it was opened
// on (review of thread-header-hover-4): its rows captured the row's index,
// and a page landing while the menu was open moved another channel to it,
// which "Just for today" then joined. Synthetic: fakeslack's channels.

import (
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestSearchJoinMenuJoinsItsChannel(t *testing.T) {
	e := srNewEnv(t, false)
	ch := e.fs.ChannelIDs()[1]
	word := strings.Split(e.h.a.ConvRef(ch).Name, "-")[0]
	v, d := srView(t, e, word, false)
	v.setTab(srTabChannels)
	d.until(t, "channels", func() bool { return !v.loading && len(v.chans) > 0 })
	d.frame()
	target := v.chans[0]
	v.chans[0].IsMember = false // a channel to join
	var mu sync.Mutex
	var joined []string
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if strings.Contains(method, "join") {
			mu.Lock()
			joined = append(joined, f.Get("channel"))
			mu.Unlock()
		}
		return nil, false
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	v.chanUIOf(target.ID).caretR = image.Rect(900, 10, 924, 34) // where the caret was drawn
	e.h.mu.Lock()
	e.h.shown = nil
	e.h.mu.Unlock()
	v.joinMenu(srTestGtx(), 0)
	e.h.mu.Lock()
	var m *Menu
	for _, o := range e.h.shown {
		if mm, ok := o.(*Menu); ok {
			m = mm
		}
	}
	e.h.mu.Unlock()
	if m == nil {
		t.Fatal("no Join… menu")
	}
	// a page lands while the menu is open: another channel at index 0
	v.chans = append(srSynthChans("PAGE", 1), v.chans...)
	v.growRows(len(v.chans))
	for _, it := range m.items {
		if it.Label == "Just for today" && it.Do != nil {
			it.Do()
		}
	}
	d.until(t, "the join", func() bool { mu.Lock(); defer mu.Unlock(); return len(joined) > 0 })
	mu.Lock()
	defer mu.Unlock()
	if joined[0] != target.ID {
		t.Errorf("Just for today joined %s, want %s, the channel the menu was opened on", joined[0], target.ID)
	}
}
