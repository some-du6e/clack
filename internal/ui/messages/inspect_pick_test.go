package messages

// The inspect tool (internal/ui/inspect) picks a plugin's inline element
// in a message header: UserPronouns renders BaseBroadcastPreamble's
// <Original> with its children, the pronouns span inserted among them,
// so the span is in the tree the native lays its children out in
// (LayoutElement), painted before the tree around the native, whose box
// under the pointer is only the native's. The picker outlines the span,
// not the whole preamble, and the sender's name still picks its own
// component.

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// pronounsPlugin is UserPronouns' channel half (P/UserPronouns.tsx
// 82-104): a span after the header's children, in <Original>.
var pronounsPlugin = pluginJS(`
class PickPronouns extends TautPlugin {
  static id = 'PickPronouns'
  static pluginName = 'Pick Pronouns'
  static description = 'test: UserPronouns-shaped header span'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = React
    this.api.setStyle('.taut-pronouns { margin-left: 6px; font-size: 12px; white-space: nowrap; cursor: default }')
    this.api.patchComponent('BaseBroadcastPreamble', (O) => (p) => {
      const kids = R.Children.toArray(p.children)
      kids.push(R.createElement('span', { key: 'taut-pronouns', className: 'taut-pronouns' }, 'they/them'))
      return R.createElement(O, p, kids)
    })
  }
}
return PickPronouns`)

// pickHost is the window as the tool sees it, for a view on its own.
type pickHost struct{ th *theme.Theme }

func (h pickHost) Theme() *theme.Theme                         { return h.th }
func (h pickHost) Describe(*inspect.Report, *inspect.Redactor) {}
func (h pickHost) ConvKind(string) string                      { return "" }
func (h pickHost) Regions() []inspect.Region                   { return nil }
func (h pickHost) Toast(string)                                {}
func (h pickHost) CopyText(string)                             {}
func (h pickHost) Post(f func())                               {}
func (h pickHost) Invalidate()                                 {}
func (h pickHost) ConfigDir() string                           { return "" }

func TestInspectPicksPluginPronouns(t *testing.T) {
	_, _, tt, u, h := pluginEnv(t)
	install(t, tt, pronounsPlugin)
	until(t, 5*time.Second, "BaseBroadcastPreamble patched", func() {}, func() bool { return slackc.Patched("BaseBroadcastPreamble") })
	v := NewChannelView(h, tGen)
	v.Preview = true
	until(t, 8*time.Second, "the pronouns in a header", frameFn(v), func() bool { return u.anyTree("taut-pronouns") })

	var r input.Router
	ops := new(op.Ops)
	tool := inspect.New(pickHost{th: h.th}, key.Filter{Name: "D", Required: key.ModShortcut | key.ModAlt | key.ModShift})
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 700)), Now: time.Now(), Source: r.Source()}
		g := tool.Begin(gtx)
		v.Layout(g)
		tool.End(gtx)
		r.Frame(ops)
	}
	for i := 0; i < 20; i++ {
		frame()
	}
	tool.Toggle()
	frame()
	frame()
	if !tool.Active() {
		t.Fatal("no inspect session")
	}

	// along each visible header's sender line
	s := v.s
	var pron, sender bool
	var seen []string
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items) && !(pron && sender); i++ {
		if s.items[i].kind != itMsg || i >= len(s.pos) {
			continue
		}
		row := s.rows[s.items[i].key]
		if row == nil {
			continue
		}
		y := s.origin.Y + s.pos[i] + row.col.Y + int(mt.nameLine)/2
		for x := s.origin.X + row.col.X + 2; x < s.origin.X+row.col.X+420 && !(pron && sender); x += 3 {
			r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(y))})
			frame()
			label, rect, ok := tool.Hovered()
			if !ok {
				continue
			}
			if len(seen) == 0 || seen[len(seen)-1] != label {
				seen = append(seen, label)
			}
			switch {
			case strings.HasPrefix(label, "span.taut-pronouns"):
				pron = true
				if rect.Dx() > 120 || rect.Dy() > 40 || !image.Pt(x, y).In(rect.Inset(-1)) {
					t.Errorf("the pronouns outlined as %v (pointer %d,%d): not the span", rect, x, y)
				}
			case strings.HasPrefix(label, "BaseMessageSender"):
				sender = true
			}
		}
	}
	if !pron {
		t.Errorf("no header's pronouns could be picked; outlined along the sender lines: %q", seen)
	}
	if !sender {
		t.Errorf("the sender's name no longer picks BaseMessageSender; outlined: %q", seen)
	}
}

// The hover toolbar's buttons (.c-message_actions__button, entered with
// their data-qa) have boxes for the picker: pointing at Reply in thread
// outlines that button, not the whole MessageActions card, and ↑ walks
// out to the card.
func TestInspectPicksToolbarButton(t *testing.T) {
	e := newCtxEnv(t)
	th := e.h.coreHost.th
	s := e.v.s
	tool := inspect.New(pickHost{th: th}, key.Filter{Name: "D", Required: key.ModShortcut | key.ModAlt | key.ModShift})
	const w, h = 900, 700
	frame := func() {
		e.ops.Reset()
		e.now = e.now.Add(16 * time.Millisecond)
		gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h)), Now: e.now, Source: e.r.Source()}
		g := tool.Begin(gtx)
		e.v.Layout(g)
		tool.End(gtx)
		e.r.Frame(&e.ops)
	}
	move := func(p f32.Point) {
		e.clock += 10 * time.Millisecond
		e.r.Queue(pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: e.clock})
		frame()
		frame()
	}
	for range 3 {
		frame()
	}
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].key == tsCtxLink && s.heights[i] > 0 {
			move(f32.Pt(300, float32(s.pos[i]+s.heights[i]-4+s.origin.Y)))
		}
	}
	if s.tbFor < 0 {
		t.Fatal("no toolbar over the hovered message")
	}
	tool.Toggle()
	frame()
	frame()
	var at f32.Point
	for k, i := range s.tbBtns {
		if i == tbReply {
			bs, pad := int(mt.toolbarBtn), int(mt.toolbarPad)
			at = f32.Pt(float32(s.tbRect.Min.X+pad+(len(s.quick.names)+k)*bs+bs/2+s.origin.X), float32(s.tbRect.Min.Y+pad+bs/2+s.origin.Y))
		}
	}
	if at == (f32.Point{}) {
		t.Fatalf("no Reply in thread button (%v)", s.tbBtns)
	}
	move(at)
	label, r, ok := tool.Hovered()
	if !ok || !strings.HasPrefix(label, "button.c-button-unstyled.c-icon_button") {
		t.Fatalf("over Reply in thread: outline %q (%v), want the toolbar button", label, ok)
	}
	if bs := int(mt.toolbarBtn); r.Dx() != bs || r.Dy() != bs || !image.Pt(int(at.X), int(at.Y)).In(r) {
		t.Errorf("the button's outline %v, want %dx%d around %v", r, bs, bs, at)
	}
	e.r.Queue(key.Event{Name: key.NameUpArrow, State: key.Press})
	frame()
	if label, _, _ := tool.Hovered(); !strings.HasPrefix(label, "MessageActions") {
		t.Errorf("↑ from the button: outline %q, want MessageActions", label)
	}
}
