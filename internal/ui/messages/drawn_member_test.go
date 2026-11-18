package messages

// The hub redraws the lists for a member's change only when a list drew
// the member in its latest frame (drawsMember: the store's last access
// against the frame's stamp). A frame stamps itself before its rows read
// their senders, so a check that ran while the latest frame laid out its
// rows found the sender's read in the frame before, and a rename then
// never redrew the row: an idle window kept the old name. Here the
// rename lands while a frame draws the row above the sender's, and only
// the frames the hub asks for come after. Synthetic data on
// internal/fakeslack.

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestSenderRenameMidFrameDraws(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch = "C0RENAMMF1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "lunch", IsChannel: true, IsMember: true, NumMembers: 12})
	// Cara's row between two of Ana's: whichever way the list lays its
	// rows out, an "ana" label is drawn before Cara's row in a frame
	base := time.Now().Add(-time.Hour).Unix()
	a, b, c := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+600), fmt.Sprintf("%d.000300", base+1200)
	app.S.SetLatest(ch, []slack.Message{
		{Type: "message", TS: a, User: "UANA00001", Text: "lunch at noon?", Blocks: shotRich(shotSection("lunch at noon?"))},
		{Type: "message", TS: b, User: "UCARA0001", Text: "sure", Blocks: shotRich(shotSection("sure"))},
		{Type: "message", TS: c, User: "UANA00001", Text: "booked", Blocks: shotRich(shotSection("booked"))},
	}, false)
	f := newInvFrames(t)
	v := NewChannelView(invHost{coreHost{app: app, th: theme.New(false)}, f.invalidate}, ch)
	frame := frameFn(v)
	var mu sync.Mutex
	var drawn []string
	drew := func(name string) bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(drawn, name)
	}
	var armed atomic.Bool
	renamed := make(chan struct{})
	var once sync.Once
	hb := v.s.hb
	labelHook = func(txt string, _ textStyle) {
		mu.Lock()
		drawn = append(drawn, txt)
		mu.Unlock()
		if !armed.Load() {
			return
		}
		if txt == "cara" {
			armed.Store(false) // her row came first: no rename in this frame
			return
		}
		if txt != "ana" {
			return
		}
		once.Do(func() {
			// Cara's rename, heard by the hub while this frame lays out the
			// rows: hers is drawn after this one
			rv := hb.usersRev()
			refsUserChange(app, "kit.l")
			for dl := time.Now().Add(300 * time.Millisecond); time.Now().Before(dl) && hb.usersRev() == rv; {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(20 * time.Millisecond)
			close(renamed)
		})
	}
	t.Cleanup(func() { labelHook = nil })
	if !f.run(frame, 5*time.Second, func() bool { return itemHeight(v.s, c) > 0 && itemHeight(v.s, a) > 0 && drew("ana") && drew("cara") }) {
		t.Fatal("the rows and their senders never drew")
	}
	for i := 0; i < 3; i++ {
		time.Sleep(5 * time.Millisecond)
		frame()
	}
	time.Sleep(5 * time.Millisecond) // the next frame starts ms after the last one read her
	armed.Store(true)
	frame()
	select {
	case <-renamed:
	default:
		t.Fatal("the frame drew no row of Ana's before Cara's: the rename didn't land mid-frame")
	}
	mu.Lock()
	drawn = nil
	mu.Unlock()
	// what the hub asks for, nothing else
	if !f.wait(frame, 3*time.Second, func() bool { return drew("kit.l") }) {
		t.Errorf("Cara's rename landed while a frame drew the list, and no frame since drew her new name (drawn: %q)", drawn)
	}
}
