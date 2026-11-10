package messages

import (
	"image"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

type profileHost struct {
	coreHost
	mu     sync.Mutex
	opened []string
}

func (h *profileHost) OpenProfile(_ image.Point, id string) {
	h.mu.Lock()
	h.opened = append(h.opened, id)
	h.mu.Unlock()
}

func (h *profileHost) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.opened)
}

// senderClickEnv is a channel view over a router, and click, which clicks
// the first header row's name and reports whether a profile opened. The
// first click (no plugins yet) checks the coordinates.
func senderClickEnv(t *testing.T) (tt *taut.Taut, u *pluginUI, frame func(), click func() bool) {
	t.Helper()
	_, _, tt, u, ch := pluginEnv(t)
	h := &profileHost{coreHost: ch}
	v := NewChannelView(h, tGen)
	v.Preview = true
	var r input.Router
	ops := new(op.Ops)
	now := time.Now()
	frame = func() {
		ops.Reset()
		now = now.Add(20 * time.Millisecond)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 700)), Now: now, Source: r.Source()}
		v.Layout(gtx)
		r.Frame(ops)
	}
	until(t, 5*time.Second, "rows", frame, func() bool { return len(v.s.rows) >= 2 })
	var ts time.Duration
	// click is pressed on each candidate point of the first header row's name
	click = func() bool {
		before := h.count()
		for i := v.s.visible[0]; i < v.s.visible[1] && i < len(v.s.items); i++ {
			it := v.s.items[i]
			if it.kind != itMsg || !it.header {
				continue
			}
			if row := v.s.rows[it.key]; row == nil || row.m.User == "" {
				continue
			}
			for _, dy := range []int{8, 12, 16, 20} {
				p := f32.Pt(70, float32(v.s.pos[i]+dy))
				ts += 30 * time.Millisecond
				r.Queue(
					pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: ts},
					pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: ts + time.Millisecond},
					pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: ts + 2*time.Millisecond},
				)
				for k := 0; k < 4; k++ {
					frame()
				}
				if h.count() > before {
					return true
				}
			}
			return false
		}
		return false
	}
	if !click() {
		t.Fatal("control: clicking the name without plugins didn't open a profile (test coordinates are off)")
	}
	return tt, u, frame, click
}

// Clicking a sender's name opens their profile even when plugins patch
// MessageSender and BaseMessageSender (ShowSendingBot + IdvStatus live: the
// name hovered but clicks did nothing, and its context menu never opened).
func TestSenderNameClickUnderPatches(t *testing.T) {
	tt, u, frame, click := senderClickEnv(t)
	code := []byte(`(() => {
const TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;
class WrapSenders extends TautPlugin {
  static id = 'WrapSenders'
  static pluginName = 'Wrap Senders'
  static description = 'patches both sender components'
  static authors = ['jeremy']
  static defaultConfig = { enabled: true }
  start() {
    const R = React
    this.api.patchComponent('MessageSender', (O) => (p) => R.createElement(O, p))
    this.api.patchComponent('BaseMessageSender', (O) => (p) => R.createElement(O, { ...p, className: (p.className ? p.className + ' ' : '') + 'x-wrapped' }))
  }
}
return WrapSenders
})()`)
	install(t, tt, code)
	until(t, 5*time.Second, "both senders patched", func() {}, func() bool {
		return slackc.Patched("MessageSender") && slackc.Patched("BaseMessageSender")
	})
	until(t, 8*time.Second, "the patched sender trees", frame, func() bool { return u.anyTree("x-wrapped") })
	for k := 0; k < 5; k++ {
		frame()
	}
	if !click() {
		t.Error("with MessageSender and BaseMessageSender patched, clicking the name didn't open the profile")
	}
}

// The same with the real plugins (the messages-taut workflow has them).
func TestPluginIdvStatusSenderClick(t *testing.T) {
	idv, ssb := pluginFile(t, "IdvStatus"), pluginFile(t, "ShowSendingBot")
	tt, u, frame, click := senderClickEnv(t)
	install(t, tt, ssb)
	install(t, tt, idv)
	until(t, 5*time.Second, "both senders patched", func() {}, func() bool {
		return slackc.Patched("MessageSender") && slackc.Patched("BaseMessageSender")
	})
	until(t, 10*time.Second, "IdvStatus' class", frame, func() bool { return u.anyTree("taut-idv-status") })
	for k := 0; k < 5; k++ {
		frame()
	}
	if !click() {
		t.Error("with ShowSendingBot and IdvStatus on, clicking the name didn't open the profile")
	}
}
