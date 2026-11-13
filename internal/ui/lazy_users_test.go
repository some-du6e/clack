package ui

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// Slack fetches the members a loaded channel shows as its rows render
// (core/lazy_users.go), and a reaction's users only when its tooltip
// shows (ReactionTip's TooltipMemberName). Here a message's reaction is
// from people nothing else in the channel names: opening the channel asks
// users/info for none of them; resting the pointer on the pill asks for
// them once the tooltip is due (not before), and the tooltip then names
// them instead of drawing their ids.
func TestReactorsFetchedOnTooltip(t *testing.T) {
	sz := fakeslack.Small
	sz.Authors = 20 // the generated messages' people: the first 20 users
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ids := fs.UserIDs()
	reactors := append([]string(nil), ids[len(ids)-6:]...)
	ch := fs.ChannelIDs()[0]
	ts, _ := fs.PostQuiet(ch, ids[1], "lazy reactors")
	if !fs.ReactQuiet(ch, ts, "tada", reactors...) {
		t.Fatal("no message to react to")
	}
	var mu sync.Mutex
	asked := map[string]time.Time{}
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			var up map[string]int
			_ = json.Unmarshal(body["updated_ids"], &up)
			mu.Lock()
			for id := range up {
				if _, ok := asked[id]; !ok {
					asked[id] = time.Now()
				}
			}
			mu.Unlock()
		}
		return nil, false
	})
	askedOf := func(want []string) []string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for _, id := range want {
			if _, ok := asked[id]; ok {
				out = append(out, id)
			}
		}
		return out
	}

	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.Core()
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.toLatest() // the channel opens at its first unread; the message is the newest
	var pill messages.ReactionRect
	e.until("the reaction pill", 20*time.Second, func() bool {
		cv := e.a.convos[ch+"/"]
		if cv == nil {
			return false
		}
		l, ok := cv.list.(channelList)
		if !ok {
			return false
		}
		for _, r := range l.ReactionRects() {
			if r.TS == ts && r.Name == "tada" {
				pill = r
				return true
			}
		}
		return false
	})
	e.settle(2 * time.Second) // the rows' own lookups are answered by now
	// the sender is drawn by name: the row asked for it
	if _, ok := c.S.RawUser(ids[1]); !ok {
		t.Errorf("the sender %s wasn't fetched for its row", ids[1])
	}
	if got := askedOf(reactors); len(got) > 0 {
		t.Fatalf("opening the channel asked users/info for reactors %v", got)
	}
	for _, id := range reactors {
		if _, ok := c.S.RawUser(id); ok {
			t.Fatalf("the store holds reactor %s before its tooltip showed", id)
		}
	}

	// rest on the pill: nothing is asked before the tooltip is due
	at := f32.Pt(float32(pill.R.Min.X+pill.R.Dx()/2), float32(pill.R.Min.Y+pill.R.Dy()/2))
	t0 := time.Now()
	e.move(at)
	for time.Since(t0) < 200*time.Millisecond {
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	if got := askedOf(reactors); len(got) > 0 {
		t.Fatalf("reactors %v asked for before the tooltip showed", got)
	}
	e.until("the reactors fetched", 10*time.Second, func() bool { return len(askedOf(reactors)) == len(reactors) })
	mu.Lock()
	for _, id := range reactors {
		if d := asked[id].Sub(t0); d < 250*time.Millisecond {
			t.Errorf("%s asked for %s after the pointer arrived, before the tooltip's 300ms", id, d)
		}
	}
	mu.Unlock()
	var names []string
	e.until("the reactors in the store", 10*time.Second, func() bool {
		names = names[:0]
		for _, id := range reactors {
			u, ok := c.S.RawUser(id)
			if !ok {
				return false
			}
			names = append(names, store.DisplayName(u))
		}
		return true
	})
	var missing, asIDs []string
	dl := time.Now().Add(10 * time.Second)
	for {
		drawn := frameStrings(e)
		missing, asIDs = missing[:0], asIDs[:0]
		for i, id := range reactors {
			if !drawnContains(drawn, names[i]) {
				missing = append(missing, names[i])
			}
			if drawnContains(drawn, id) {
				asIDs = append(asIDs, id)
			}
		}
		if len(missing) == 0 && len(asIDs) == 0 {
			break
		}
		if time.Now().After(dl) {
			sort.Strings(missing)
			t.Fatalf("the tooltip doesn't name %s; draws ids %v", strings.Join(missing, ", "), asIDs)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// drawnContains reports whether any drawn string contains s.
func drawnContains(drawn map[string]bool, s string) bool {
	for d := range drawn {
		if strings.Contains(d, s) {
			return true
		}
	}
	return false
}
