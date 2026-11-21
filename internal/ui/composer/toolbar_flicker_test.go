package composer

import (
	"fmt"
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Karim (α.113, Windows, a public channel's thread): "i swear i just saw
// the [/] icon flicker like crazy", with an inspect capture of the thread
// composer under OwlMode's patches (InputContainer, MessageInput,
// WysiwygContainer, ShortcutsMenuTriggerButton, TautComposerButtons): the
// toolbar folded behind "…", the overflow row's [/] floating over the
// "More options" tooltip.

// owlShaped is shaped like Taut's OwlMode (plugins/OwlMode.tsx) outside
// an owl thread: InputContainer, MessageInput, WysiwygContainer,
// BaseContextBarV2 and ShortcutsMenuTriggerButton patched to render their
// Original with the props they got, and a composer.addButton "end" button
// for threads whose render returns null (no owl post at the root).
const owlShaped = `(() => {
const TautPlugin = globalThis.TautPlugin
class OwlShaped extends TautPlugin {
  static id = 'OwlShaped'
  static pluginName = 'Owl Shaped'
  static description = 'an Owl Mode shaped plugin (test)'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = globalThis.React
    const Owl = R.createContext(null)
    this.api.composer.addButton({ id: 'taut-owl-mode', placement: 'end', kinds: ['thread'], render: () => null })
    const pass = (Original) => (props) => R.createElement(Original, { ...props })
    this.api.patchComponent('InputContainer', pass)
    this.api.patchComponent('MessageInput', pass)
    this.api.patchComponent('WysiwygContainer', (Original) => (props) => {
      const owl = R.useContext(Owl)
      return owl ? R.createElement(Original, { ...props, className: (props.className || '') + ' taut-owl-composer' }) : R.createElement(Original, { ...props })
    })
    this.api.patchComponent('BaseContextBarV2', pass)
    this.api.patchComponent('ShortcutsMenuTriggerButton', pass)
  }
  stop() {}
}
return OwlShaped
})()`

// silentShaped adds Taut's SilentTyping button (plugins/SilentTyping.tsx):
// an "end" button for channels and threads that renders.
const silentShaped = `(() => {
const TautPlugin = globalThis.TautPlugin
class SilentShaped extends TautPlugin {
  static id = 'SilentShaped'
  static pluginName = 'Silent Shaped'
  static description = 'a Silent Typing shaped plugin (test)'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    this.api.composer.addButton({ id: 'taut-silent-typing', placement: 'end',
      render: () => ({ icon: 'notifications', label: 'Hide typing in this chat', tooltip: 'Hide typing in this chat', pressed: false, onClick() {} }) })
  }
  stop() {}
}
return SilentShaped
})()`

// tbBridge counts the renders Go sends JS, by component.
type tbBridge struct {
	*taut.Bridge
	mu sync.Mutex
	n  map[string]int
}

func (b *tbBridge) Render(root uint64, name string, props []byte) {
	b.add(name)
	b.Bridge.Render(root, name, props)
}

func (b *tbBridge) RenderIn(root, parent uint64, parentK int64, name string, props []byte) {
	b.add(name)
	b.Bridge.RenderIn(root, parent, parentK, name, props)
}

func (b *tbBridge) add(name string) {
	b.mu.Lock()
	if b.n == nil {
		b.n = map[string]int{}
	}
	b.n[name]++
	b.mu.Unlock()
}

func (b *tbBridge) total() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := 0
	for _, v := range b.n {
		k += v
	}
	return k
}

func (b *tbBridge) counts() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return fmt.Sprint(b.n)
}

const tbThreadTS = "1700000000.000100"

// tbRig is the thread pane's composer drawn through a plugin's tree, laid
// out the way the conversation lays it out (exactly w wide), through a
// real input.Router. Each frame waits for JS to answer what it sent
// (lockstep) unless free is set.
type tbRig struct {
	t    *testing.T
	e    *slickEnv
	c    *Composer
	h    *ovHost
	rec  *tbBridge
	rt   *jsrt.Runtime
	r    input.Router
	ops  op.Ops
	now  time.Time
	w    int
	free bool
	ptrT time.Duration
}

func newTBRig(t *testing.T, plugins ...string) *tbRig {
	t.Helper()
	e := startTautCode(t, []byte(plugins[0]), nil, nil)
	for _, code := range plugins[1:] {
		id, err := e.tt.Import(e.ctx(), []byte(code), "")
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if err := e.tt.SetEnabled(e.ctx(), id, true); err != nil {
			t.Fatalf("enable %s: %v", id, err)
		}
	}
	e.patched("InputContainer", "MessageInput", "WysiwygContainer", "ShortcutsMenuTriggerButton", tautButtons)
	rec := &tbBridge{Bridge: e.tt.Bridge()}
	slackc.SetBridge(rec)
	rt, err := e.tt.JS()
	if err != nil {
		t.Fatal(err)
	}
	stubTyping(t)
	g := &tbRig{t: t, e: e, rec: rec, rt: rt, h: &ovHost{slickHost: e.h}, now: time.Now(), w: 633}
	clearDraft(tChGen, tbThreadTS)
	g.c = New(g.h, tChGen, tbThreadTS)
	t.Cleanup(func() {
		g.c.Close()
		clearDraft(tChGen, tbThreadTS)
	})
	e.why = func() string { return g.state() }
	e.until(10*time.Second, "the thread composer through the plugin's tree", g.frame, func() bool {
		_, ok := e.treeWith(`"n":"MessageInputRoadblock"`)
		return ok && g.c.ui.f.boxOK && !g.c.ui.rect[crSend].Empty()
	})
	if g.c.BoxVanished() {
		t.Fatal("the composer vanished under the plugin")
	}
	return g
}

func (g *tbRig) frame() {
	g.ops.Reset()
	gtx := layout.Context{Ops: &g.ops, Source: g.r.Source(), Now: g.now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Min: image.Pt(g.w, 0), Max: image.Pt(g.w, 600)}}
	slackc.Frame(gtx)
	g.c.Layout(gtx)
	g.r.Frame(&g.ops)
	g.now = g.now.Add(16 * time.Millisecond)
	if g.free {
		time.Sleep(2 * time.Millisecond)
		return
	}
	if err := g.rt.Do(func(*jsrt.Ctx) error { return nil }); err != nil {
		g.t.Errorf("waiting for JS: %v", err)
	}
}

// settle runs frames until nothing was sent to JS for 3 frames in a row.
func (g *tbRig) settle() {
	g.t.Helper()
	quiet := 0
	for i := 0; i < 60 && quiet < 3; i++ {
		n := g.rec.total()
		g.frame()
		if g.rec.total() == n {
			quiet++
		} else {
			quiet = 0
		}
	}
}

// tbSig is what the footer showed in a frame.
type tbSig struct {
	fold, open bool
	eq, suf    int
	shown      uint32
	rects      [len(tbIDs)]image.Rectangle
}

var tbIDs = [...]crBtn{crAttach, crAa, crEmoji, crMention, crShortcut, crFootMore, crSend}

func (g *tbRig) sig() tbSig {
	u := &g.c.ui
	s := tbSig{fold: u.f.fold, open: u.more.open, eq: u.f.eq, suf: u.sufW}
	for i, id := range tbIDs {
		if u.liveShown&(1<<id) != 0 {
			s.shown |= 1 << id
			s.rects[i] = u.rect[id]
		}
	}
	return s
}

func (s tbSig) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "fold %v open %v eq %d suf %d:", s.fold, s.open, s.eq, s.suf)
	names := [...]string{"+", "Aa", "emoji", "@", "[/]", "…", "send"}
	for i, id := range tbIDs {
		if s.shown&(1<<id) != 0 {
			fmt.Fprintf(&b, " %s%v", names[i], s.rects[i])
		}
	}
	return b.String()
}

func (g *tbRig) state() string {
	return fmt.Sprintf("w %d %v; renders %s", g.w, g.sig(), g.rec.counts())
}

// watch runs n frames and returns every change of the footer's signature.
func (g *tbRig) watch(n int) (first tbSig, changes []string, renders int) {
	g.t.Helper()
	first = g.sig()
	last := first
	r0 := g.rec.total()
	for i := 0; i < n; i++ {
		g.frame()
		if s := g.sig(); s != last {
			changes = append(changes, fmt.Sprintf("frame %d: %v", i, s))
			last = s
		}
	}
	return first, changes, g.rec.total() - r0
}

// mouse queues a mouse event at p (composer coordinates) and runs a frame.
func (g *tbRig) mouse(k pointer.Kind, b pointer.Buttons, p image.Point) {
	g.ptrT += 20 * time.Millisecond
	NotePointer(p)
	if k == pointer.Press {
		NotePress()
	}
	g.r.Queue(pointer.Event{Kind: k, Source: pointer.Mouse, Buttons: b, Position: f32.Pt(float32(p.X)+.5, float32(p.Y)+.5), Time: g.ptrT})
	g.frame()
}

// at is button id's centre in composer coordinates.
func (g *tbRig) at(id crBtn) image.Point {
	r := g.c.ui.rect[id]
	return r.Min.Add(r.Size().Div(2)).Add(image.Pt(0, g.c.top))
}

// The probe: the footer's layout at each width under the plugin, frame
// after frame, and with the overflow row open under a resting pointer.
func TestComposerToolbarStableUnderPlugins(t *testing.T) {
	for _, tc := range []struct {
		name    string
		plugins []string
	}{
		{"owl", []string{owlShaped}},
		{"owl+silent", []string{owlShaped, silentShaped}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newTBRig(t, tc.plugins...)
			for _, free := range []bool{false, true} {
				g.free = free
				for w := 240; w <= 1000; w += 9 {
					g.w = w
					g.settle()
					first, changes, renders := g.watch(30)
					if len(changes) > 0 || renders > 0 {
						t.Errorf("free %v w %d: %d changes, %d renders sent in 30 frames; first %v", free, w, len(changes), renders, first)
						for i, c := range changes {
							if i == 8 {
								break
							}
							t.Logf("  %s", c)
						}
					} else if w%45 == 0 {
						t.Logf("free %v w %d: %v", free, w, first)
					}
				}
			}
			t.Logf("renders by component: %s", g.rec.counts())

			// Karim's width: hover "…", open the row, rest on "…"
			g.free = false
			g.w = 633
			g.settle()
			s := g.sig()
			t.Logf("w 633: %v", s)
			if s.shown&(1<<crFootMore) == 0 {
				t.Logf("w 633: no … drawn")
				return
			}
			more := g.at(crFootMore)
			g.mouse(pointer.Move, 0, more)
			for range 40 {
				g.frame()
			}
			tip := &g.c.ui.tip[crFootMore]
			t.Logf("… hovered %v, tip showing %v placed %v", g.c.ui.btn[crFootMore].Hovered(), tip.Showing(g.now), tip.Placed())
			g.mouse(pointer.Press, pointer.ButtonPrimary, more)
			g.mouse(pointer.Release, 0, more)
			g.settle()
			first, changes, renders := g.watch(60)
			t.Logf("row open %v: first %v, %d changes, %d renders", g.c.ui.more.open, first, len(changes), renders)
			for i, c := range changes {
				if i == 12 {
					break
				}
				t.Logf("  %s", c)
			}
			if len(changes) > 0 {
				t.Errorf("the row open under a resting pointer: %d changes in 60 frames", len(changes))
			}
			mr := g.c.ui.rect[crFootMore]
			row := crMoreRowRect(layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}, mr, g.c.ui.more.n)
			box := tip.Placed().Box.Add(mr.Min)
			t.Logf("tip showing %v box %v, row %v, overlap %v", tip.Showing(g.now), box, row, box.Overlaps(row))
			if tip.Showing(g.now) && box.Overlaps(row) {
				t.Errorf("the More options tooltip %v under the open row %v", box, row)
			}
		})
	}
}
