package composer

import (
	"fmt"
	"image"
	"image/color"
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
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Karim (α.113, Windows, a public channel's thread): "i swear i just saw
// the [/] icon flicker like crazy", with an inspect capture of the thread
// composer under OwlMode's patches (InputContainer, MessageInput,
// WysiwygContainer, ShortcutsMenuTriggerButton, TautComposerButtons): the
// toolbar folded behind "…", and the overflow row's [/] floating over the
// "More options" tooltip. Under those patches the composer is drawn from
// the plugin's tree, and the footer handed TextyButtonOverflow its own
// minButtonsForOverflow back (overflowProps): JS rendered the composer's
// tree every frame and the toolbar folded and unfolded every frame
// between 220px and 580px wide. Slack (build 1f2d42be66b2) folds only when
// the buttons don't fit (inlineButtonsWidth), and under Taut's
// composer.addButton patch counts only the toolbar's real children.

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
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	k := 0
	for _, v := range b.n {
		k += v
	}
	return k
}

func (b *tbBridge) counts() string {
	if b == nil {
		return "no plugins"
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return fmt.Sprint(b.n)
}

const tbThreadTS = "1700000000.000100"

// tbComposerButton stands in for slackc/kit's ComposerButton, which a
// composer test can't link (kit imports this package): kit draws each
// composer.addButton entry as a "small" IconButtonBase, 28dp square.
// Without a host the node draws nothing, and plugin buttons that render
// would take no room in these tests.
func tbComposerButton(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	s := gtx.Dp(28)
	return layout.Dimensions{Size: image.Pt(s, s)}
}

func tbNoHost(layout.Context, *slackc.Host) layout.Dimensions { return layout.Dimensions{} }

// tbCase is a composer and the plugins it runs under.
type tbCase struct {
	name    string
	thread  bool
	real    string   // a Taut plugin built from Taut (TAUT_PLUGINS_DIR)
	plugins []string // inline plugins; none (and no real one): no Taut at all
	patched []string // what they patch (waited for)
	tree    bool     // the composer is drawn from a plugin's tree
	plug    int      // Taut's composer.addButton buttons for this composer
	plugW   int      // the width of the ones that render (dp)
	step    int      // the width sweep's step
	free    []int    // widths also watched with JS answering when it does
}

var tbCases = []tbCase{
	{name: "thread under OwlMode", thread: true, plugins: []string{owlShaped},
		patched: []string{"InputContainer", "MessageInput", "WysiwygContainer", "ShortcutsMenuTriggerButton", tautButtons},
		tree:    true, plug: 1, step: 16, free: []int{230, 380, 633, 900}},
	{name: "thread under OwlMode and SilentTyping", thread: true, plugins: []string{owlShaped, silentShaped},
		patched: []string{"InputContainer", "MessageInput", "WysiwygContainer", tautButtons},
		tree:    true, plug: 2, plugW: 28, step: 32},
	{name: "channel under Better Typing Indicator", plugins: []string{typingShaped},
		patched: []string{"MessagePaneInput", "TypingNames"}, tree: true, step: 32},
	{name: "channel with SilentTyping's button", plugins: []string{silentShaped},
		patched: []string{tautButtons}, plug: 1, plugW: 28, step: 32},
	{name: "channel without plugins", step: 32},
}

// tbMargin is the composer's side margin (20px channel, 16px thread).
func (tc tbCase) margin() int {
	if tc.thread {
		return 16
	}
	return 20
}

// tbSlack is where Slack puts the footer's buttons for a composer w px
// wide at 1x: TextyButtons and TextyButtonOverflow (build 1f2d42be66b2),
// with Taut 3.7.2's composer.addButton buttons after Slack's. It returns
// the toolbar's width, whether it folds, and the chrome-local left edges
// of Run shortcut (unfolded) and of ⋯ (folded). The toolbar folds when
// its buttons don't fit, plugin buttons or not: Taut's own patch counts
// only the slots that render, so with one or two plugin buttons Slack
// leaves the row unfolded at these widths and runs it under the send
// group, where [/] and the plugin buttons can't be clicked (overflow.go).
func tbSlack(tc tbCase, w int) (eq int, fold bool, slash, more int) {
	m := tc.margin()
	left := m + 1 // the box's border
	// the box's content less the footer's padding (6 + 4), the + (28),
	// the suffix's padding (4 + 2) and the send group (60)
	eq = w - 2*m - 2 - 104
	moved := 0
	for _, at := range []int{160, 105, 65} { // mention, emoji, Aa
		if eq < at {
			moved++
		}
	}
	// inlineButtonsWidth: from .c-texty_buttons' left edge, Aa 2+28+2,
	// emoji 2+28+2, mention 2+28, the divider 6+1+6, [/] 2+28, then the
	// plugin buttons that render (2 + their width)
	inline := 137
	if tc.plugW > 0 {
		inline += 2 + tc.plugW
	}
	fold = eq > 0 && eq < inline // 15 slots and the plugin buttons: over 5
	slash = left + 6 + 28 + 109
	more = left + [...]int{132, 102, 70, 38}[moved]
	return
}

// tbRig is a composer laid out the way the conversation lays it out
// (exactly w wide), through a real input.Router; with plugins, each frame
// waits for JS to answer what it sent (lockstep) unless free is set.
type tbRig struct {
	t    *testing.T
	tc   tbCase
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

func newTBRig(t *testing.T, tc tbCase, w int) *tbRig {
	t.Helper()
	g := &tbRig{t: t, tc: tc, now: time.Now(), w: w}
	if tc.real != "" || len(tc.plugins) > 0 {
		var e *slickEnv
		extra := tc.plugins
		if tc.real != "" {
			e = startTautUI(t, tc.real, nil)
		} else {
			e, extra = startTautCode(t, []byte(tc.plugins[0]), nil, nil), tc.plugins[1:]
		}
		for _, code := range extra {
			id, err := e.tt.Import(e.ctx(), []byte(code), "")
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			if err := e.tt.SetEnabled(e.ctx(), id, true); err != nil {
				t.Fatalf("enable %s: %v", id, err)
			}
		}
		e.patched(tc.patched...)
		if tc.plugW > 0 {
			slackc.RegisterHost(tbComposerButton, "ComposerButton")
			t.Cleanup(func() { slackc.RegisterHost(tbNoHost, "ComposerButton") })
		}
		g.rec = &tbBridge{Bridge: e.tt.Bridge()}
		slackc.SetBridge(g.rec)
		rt, err := e.tt.JS()
		if err != nil {
			t.Fatal(err)
		}
		g.e, g.rt, g.h = e, rt, &ovHost{slickHost: e.h}
	} else {
		app, _ := syntheticApp(t, func(*store.Store) {})
		g.h = &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	}
	stubTyping(t)
	ts := ""
	if tc.thread {
		ts = tbThreadTS
	}
	clearDraft(tChGen, ts)
	g.c = New(g.h, tChGen, ts)
	t.Cleanup(func() {
		g.c.Close()
		clearDraft(tChGen, ts)
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		g.frame()
		drawn := g.c.ui.f.boxOK && !g.c.ui.rect[crSend].Empty()
		if drawn && g.e != nil && tc.tree {
			_, drawn = g.e.treeWith(`"n":"MessageInputRoadblock"`)
		}
		if drawn {
			break
		}
		if time.Now().After(deadline) {
			if g.e != nil {
				g.e.dumpTrees()
			}
			t.Fatalf("%s: the composer never drew (%s)", tc.name, g.state())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if g.c.BoxVanished() {
		t.Fatalf("%s: the composer vanished under the plugins", tc.name)
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
	if g.rt == nil {
		return
	}
	if g.free {
		time.Sleep(2 * time.Millisecond)
		return
	}
	if err := g.rt.Do(func(*jsrt.Ctx) error { return nil }); err != nil {
		g.t.Errorf("waiting for JS: %v", err)
	}
}

// settle runs frames until for 3 frames in a row the footer drew the same
// and nothing went to JS (at most 80).
func (g *tbRig) settle() {
	quiet, last := 0, g.sig()
	for i := 0; i < 80 && quiet < 3; i++ {
		n := g.rec.total()
		g.frame()
		if s := g.sig(); s == last && g.rec.total() == n {
			quiet++
		} else {
			quiet, last = 0, s
		}
	}
}

// tbSig is what the footer drew in a frame.
type tbSig struct {
	fold, open bool
	eq, suf    int
	shown      uint32
	rects      [len(tbIDs)]image.Rectangle
	plug       image.Rectangle // Taut's buttons in the toolbar
}

var tbIDs = [...]crBtn{crAttach, crAa, crEmoji, crMention, crShortcut, crFootMore, crSend, crSchedule}

var tbNames = [len(tbIDs)]string{"+", "Aa", "emoji", "@", "[/]", "…", "send", "caret"}

func (g *tbRig) sig() tbSig {
	u := &g.c.ui
	s := tbSig{fold: u.f.fold, open: u.more.open, eq: u.f.eq, suf: u.sufW, plug: tbPlugRect(g.c)}
	for i, id := range tbIDs {
		if u.liveShown&(1<<id) != 0 {
			s.shown |= 1 << id
			s.rects[i] = u.rect[id]
		}
	}
	return s
}

func (s tbSig) has(id crBtn) bool { return s.shown&(1<<id) != 0 }

func (s tbSig) rect(id crBtn) image.Rectangle {
	for i, x := range tbIDs {
		if x == id {
			return s.rects[i]
		}
	}
	return image.Rectangle{}
}

func (s tbSig) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "fold %v open %v toolbar %d suffix %d:", s.fold, s.open, s.eq, s.suf)
	for i, id := range tbIDs {
		if s.has(id) {
			fmt.Fprintf(&b, " %s%v", tbNames[i], s.rects[i])
		}
	}
	if !s.plug.Empty() {
		fmt.Fprintf(&b, " plugins%v", s.plug)
	}
	return b.String()
}

func (g *tbRig) state() string {
	return fmt.Sprintf("w %d %v; renders %s", g.w, g.sig(), g.rec.counts())
}

// watch runs n frames: every change of the footer's signature, and the
// renders sent to JS meanwhile.
func (g *tbRig) watch(n int) (first tbSig, changes []string, renders int) {
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

// check watches the footer at width w for n frames: it must draw the same
// in every frame, send JS nothing once settled, put Run shortcut and ⋯
// where Slack does, and keep every toolbar button out from under the send
// group (drawn after the toolbar, it covers them and takes their clicks).
func (g *tbRig) check(w, n int) (failed bool) {
	g.t.Helper()
	g.w = w
	g.settle()
	first, changes, renders := g.watch(n)
	eq, fold, slash, more := tbSlack(g.tc, w)
	mode := "lockstep"
	if g.free {
		mode = "free"
	}
	var errs []string
	if len(changes) > 0 {
		errs = append(errs, fmt.Sprintf("the footer changed %d times in %d frames (the first: %s)", len(changes), n, changes[0]))
	}
	if renders > 0 {
		errs = append(errs, fmt.Sprintf("%d renders went to JS in %d settled frames (%s)", renders, n, g.rec.counts()))
	}
	switch {
	case first.eq != eq:
		errs = append(errs, fmt.Sprintf("toolbar %d px, Slack's %d", first.eq, eq))
	case fold && (!first.has(crFootMore) || first.has(crShortcut)):
		errs = append(errs, "Slack folds the toolbar behind … here")
	case !fold && (first.has(crFootMore) || !first.has(crShortcut)):
		errs = append(errs, "Slack has Run shortcut in the toolbar here, not behind …")
	case fold && first.rect(crFootMore).Min.X != more:
		errs = append(errs, fmt.Sprintf("… at x %d, Slack's %d", first.rect(crFootMore).Min.X, more))
	case !fold && first.rect(crShortcut).Min.X != slash:
		errs = append(errs, fmt.Sprintf("Run shortcut at x %d, Slack's %d", first.rect(crShortcut).Min.X, slash))
	}
	send := first.rect(crSend).Union(first.rect(crSchedule))
	if first.has(crShortcut) && first.rect(crShortcut).Overlaps(send) {
		errs = append(errs, fmt.Sprintf("Run shortcut %v is under the send group %v", first.rect(crShortcut), send))
	}
	if first.plug.Overlaps(send) {
		errs = append(errs, fmt.Sprintf("the plugin buttons %v are under the send group %v", first.plug, send))
	}
	if len(errs) > 0 {
		g.t.Errorf("%s, %d px composer (%s): %s; drew %v", g.tc.name, w, mode, strings.Join(errs, "; "), first)
		return true
	}
	return false
}

// The footer's toolbar under each plugin set, at every width from a
// narrow pane to a wide one: the same layout frame after frame, no render
// loop with JS, and Run shortcut where Slack puts it.
func TestComposerToolbarStableUnderPlugins(t *testing.T) {
	for _, tc := range tbCases {
		t.Run(strings.ReplaceAll(tc.name, " ", "_"), func(t *testing.T) {
			g := newTBRig(t, tc, 633)
			bad := 0
			for w := 200; w <= 1000 && bad < 8; w += tc.step {
				if g.check(w, 24) {
					bad++
				}
			}
			if g.rt != nil {
				g.free = true
				for _, w := range tc.free {
					if g.check(w, 40) {
						bad++
					}
				}
			}
			t.Logf("renders by component over the sweep: %s", g.rec.counts())
		})
	}
}

// The same under Taut's own OwlMode, built from Taut (the composer-taut
// workflow), in the thread composer Karim's capture is of.
func TestTautOwlModeToolbarStable(t *testing.T) {
	tc := tbCases[0]
	tc.name, tc.real, tc.plugins, tc.step = "thread under Taut's OwlMode", "OwlMode", nil, 32
	g := newTBRig(t, tc, 633)
	bad := 0
	for w := 200; w <= 1000 && bad < 8; w += tc.step {
		if g.check(w, 24) {
			bad++
		}
	}
	g.free = true
	for _, w := range tc.free {
		if g.check(w, 40) {
			bad++
		}
	}
	t.Logf("renders by component over the sweep: %s", g.rec.counts())
}

// The overflow row's tooltips, in a thread composer under OwlMode narrow
// enough that Slack folds (a 230px pane: emoji and mention move in, five
// children): with the row open, "More options" shows no tooltip
// (tipClassName display_none), and the toolbar's tooltips are above the
// row (Slack's are in the top layer): Aa's, which reaches over it.
func TestComposerToolbarOverflowTooltips(t *testing.T) {
	g := newTBRig(t, tbCases[0], 633)
	g.w = 230
	g.settle()
	if s := g.sig(); !s.has(crFootMore) {
		t.Fatalf("a 230px thread composer under OwlMode didn't fold: %v", s)
	}
	more := g.at(crFootMore)
	g.mouse(pointer.Move, 0, more)
	g.mouse(pointer.Press, pointer.ButtonPrimary, more)
	g.mouse(pointer.Release, 0, more)
	g.settle()
	if !g.c.ui.more.open {
		t.Fatalf("a click on … didn't open the overflow row (%s)", g.state())
	}
	row := tbMoreRow(g.c)
	for range 40 { // longer than the tooltips' delay, the pointer resting on …
		g.frame()
	}
	if tip := &g.c.ui.tip[crFootMore]; tip.Showing(g.now) {
		t.Errorf("the More options tooltip shows at %v over the open row %v (Slack hides it while the row is open)", tip.Placed().Box.Add(g.c.ui.rect[crFootMore].Min), row)
	}
	g.mouse(pointer.Move, 0, g.at(crAa))
	for range 40 {
		g.frame()
	}
	if !g.c.ui.more.open {
		t.Fatalf("resting on Aa closed the row (%s)", g.state())
	}
	tip := &g.c.ui.tip[crAa]
	if !tip.Showing(g.now) {
		t.Fatalf("Aa's tooltip didn't show (%s)", g.state())
	}
	box := tip.Placed().Box.Add(g.c.ui.rect[crAa].Min)
	over := box.Intersect(row)
	if over.Empty() || box.Min.X >= row.Min.X {
		t.Fatalf("Aa's tooltip %v doesn't reach over the row %v from its left: the check needs that", box, row)
	}
	// the pixels (CI's GPU steps; skipped without one): the tooltip's
	// colour, sampled in its padding left of the row, must cover the part
	// of the row under it
	top := image.Pt(0, g.c.top)
	img := pixeltest.Render(t, g.w, 600, &g.ops)
	at := box.Min.Add(top).Add(image.Pt(3, box.Dy()/2))
	col := img.RGBAAt(at.X, at.Y)
	in := over.Add(top)
	n := pixeltest.Count(img, in, color.NRGBA{R: col.R, G: col.G, B: col.B, A: 255}, 24)
	if area := in.Dx() * in.Dy(); n*10 < area*6 {
		t.Errorf("Aa's tooltip is under the open row: %d of the %d pixels where they meet (%v) are the tooltip's %v", n, area, in, col)
	}
}
