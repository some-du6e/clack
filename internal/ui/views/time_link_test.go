package views

import (
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// timeHost records the message times clicked and the menus shown.
type timeHost struct {
	*kbHost
	mu     sync.Mutex
	opened []string
	shown  []popups.Overlay
}

func (h *timeHost) OpenMessageTime(ch, ts, thread string, tile bool) {
	h.mu.Lock()
	h.opened = append(h.opened, fmt.Sprintf("%s/%s/%s/%v", ch, ts, thread, tile))
	h.mu.Unlock()
}

func (h *timeHost) ShowOverlay(o popups.Overlay) {
	h.mu.Lock()
	h.shown = append(h.shown, o)
	h.mu.Unlock()
}

// The Threads view's times are MessageTimestamp's links (ThreadSender
// AndTimestampGeneric): a click is handleTimestampClick for the message
// (Cmd: a tile), a right-click the link's menu.
func TestThreadsTimeIsALink(t *testing.T) {
	ch := fakeslack.ViewsChannel
	app, fs := vaFake(t)
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "subscriptions.thread.getView" {
			return thrOrderAnswer(thrOrderFix(ch, thrA, thrAReply, "U0FAKE00002", true)), true
		}
		return nil, false
	})
	h := &timeHost{kbHost: newKBHost(app, false)}
	v := NewThreads(h)
	e := thrViewEnv(t, v, 1)
	link := func() *popups.TimeLink {
		for _, c := range v.cards {
			if s := c.msgs[thrAReply]; s != nil {
				return &s.time
			}
		}
		return nil
	}
	var p f32.Point
	e.until(t, "the reply's time", func() bool {
		l := link()
		if l == nil || !l.Shown() {
			return false
		}
		full, _, ok := e.r.AreaBounds(l.Tag())
		if !ok || full.Empty() {
			return false
		}
		c := full.Min.Add(full.Size().Div(2))
		p = f32.Pt(float32(c.X), float32(c.Y))
		return true
	})
	at := time.Second
	press := func(b pointer.Buttons, mods key.Modifiers) {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: at},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: b, Position: p, Modifiers: mods, Time: at + time.Millisecond},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Modifiers: mods, Time: at + 2*time.Millisecond})
		at += time.Second
		for k := 0; k < 3; k++ {
			e.frame()
		}
	}
	press(pointer.ButtonPrimary, 0)
	if !link().Hovered() {
		t.Error("the pointer on the reply's time doesn't hover it")
	}
	press(pointer.ButtonPrimary, key.ModShortcut)
	h.mu.Lock()
	got := fmt.Sprint(h.opened)
	h.mu.Unlock()
	if want := fmt.Sprint([]string{ch + "/" + thrAReply + "/" + thrA + "/false", ch + "/" + thrAReply + "/" + thrA + "/true"}); got != want {
		t.Errorf("clicks on the reply's time opened %s, want %s (a click, then a Cmd click)", got, want)
	}
	press(pointer.ButtonSecondary, 0)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.shown) == 0 {
		t.Fatal("a right-click on the time showed no menu")
	}
	m, ok := h.shown[len(h.shown)-1].(*popups.Menu)
	if !ok || len(m.Items()) < 2 || m.Items()[0].Label != "Copy Link" || m.Items()[1].Label != "Open Link" {
		t.Errorf("the time's right-click showed %T %v, want the link menu (Copy Link, Open Link)", h.shown[len(h.shown)-1], m)
	}
}
