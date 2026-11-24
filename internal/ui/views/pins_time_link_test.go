package views

import (
	"fmt"
	"image"
	"os"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// pinTimeHost records the message times clicked in the pins view.
type pinTimeHost struct {
	*pfHost
	mu     sync.Mutex
	opened []string
}

func (h *pinTimeHost) OpenMessageTime(ch, ts, thread string, tile bool) {
	h.mu.Lock()
	h.opened = append(h.opened, fmt.Sprintf("%s/%s/%v", ch, ts, tile))
	h.mu.Unlock()
}

// A pin's time is BasePinnedMessage's MessageTimestamp (clickable, full):
// the message's permalink as a link, whose click is handleTimestampClick
// (not the row's open); a right-click on it is the pin's own menu.
func TestPinsTimeIsALink(t *testing.T) {
	app, fs := pfCore(t)
	if os.Getenv("PINS_LATE_GRANT") != "" {
		// the tab grant answering after the pins, as on a slow runner
		// (bk-file-icons-2's full-test 45448790432)
		fs.SetFault("edge/permissions/info", fakeslack.Fault{Delay: 500 * time.Millisecond})
	}
	ph := pfHostFor(t, app, fs, false)
	h := &pinTimeHost{pfHost: ph}
	v := NewChannelPins(h, fakeslack.PinsChannelID)
	e := &vaEnv{size: image.Pt(1100, 900)}
	pfDraw(e, ph, v.Layout)
	e.until(t, "the pins", func() bool { return v.Loaded() && len(v.Items()) == 4 })
	// useCanEditTabs on Grid: the pin's menu has Remove pin once the
	// MANAGE_CHANNEL_TABS grant (edge permissions/info, asked for the first
	// time anything reads it) is in. Read before it, the menu below was Copy
	// link alone, and the right-click's, built after it, had Remove pin
	loaded := time.Now()
	e.until(t, "the tab grant", func() bool { return app.CanEditTabs(fakeslack.PinsChannelID) })
	t.Logf("the tab grant came %v after the pins (edge permissions/info calls: %d)", time.Since(loaded).Round(time.Millisecond), fs.CallCount("edge/permissions/info"))
	e.frame()
	key := fakeslack.PinnedTS[0]
	r := v.rows[key]
	if r == nil {
		t.Fatal("no row for the first pin")
	}
	tl := pinTimeT(r)
	full, _, ok := e.r.AreaBounds(tl.Tag())
	if !ok || full.Empty() {
		t.Fatalf("the first pin's time has no area (%v, %v)", full, ok)
	}
	c := full.Min.Add(full.Size().Div(2))
	p := f32.Pt(float32(c.X)+.5, float32(c.Y)+.5)
	at := time.Second
	press := func(b pointer.Buttons) {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: at},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: b, Position: p, Time: at + time.Millisecond},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: at + 2*time.Millisecond})
		at += time.Second
		for k := 0; k < 3; k++ {
			e.frame()
		}
	}
	press(pointer.ButtonPrimary)
	if !tl.Hovered() {
		t.Error("the pointer on the pin's time doesn't hover its link")
	}
	h.mu.Lock()
	got := fmt.Sprint(h.opened)
	h.mu.Unlock()
	if want := fmt.Sprint([]string{fakeslack.PinsChannelID + "/" + key + "/false"}); got != want {
		t.Errorf("a click on the pin's time opened %s, want %s (handleTimestampClick)", got, want)
	}
	// a right-click on the time is the pin's own menu, as on the rest of
	// the row (BasePinnedMessage's row takes it before the <a>)
	var want []string
	for _, it := range v.Items() {
		if it.Key() == key {
			for _, mi := range v.menuItems(it) {
				want = append(want, mi.Label)
			}
		}
	}
	if fmt.Sprint(want) != "[Copy link Remove pin]" {
		t.Fatalf("the pin's menu is %v, want [Copy link Remove pin] (PinsListMessageContextMenu with the tab grant)", want)
	}
	n := len(ph.open())
	press(pointer.ButtonSecondary)
	os := ph.open()
	if len(os) <= n {
		t.Fatal("a right-click on the pin's time showed no menu")
	}
	m, ok := os[len(os)-1].(*popups.Menu)
	var labels []string
	if ok {
		for _, mi := range m.Items() {
			labels = append(labels, mi.Label)
		}
	}
	if !ok || len(want) == 0 || fmt.Sprint(labels) != fmt.Sprint(want) {
		t.Errorf("the pin time's right-click showed %T %v, want the pin's menu %v", os[len(os)-1], labels, want)
	}
}
