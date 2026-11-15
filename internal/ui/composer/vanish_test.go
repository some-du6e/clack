package composer

import (
	"encoding/json"
	"image"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The composer's box is always there (vanish.go). Karim's composer went
// away from #clack with Better Typing Indicator patching MessagePaneInput:
// a plugin root an uncaught JS error emptied sends the tree "[]", and the
// box laid out to nothing until a restart. These drive the patched
// boundary into each tree that leaves the box out, through a fake bridge
// (no JS), and check the box is drawn anyway, the roots are mounted again
// and the line is logged; and that a good tree, CSS that hides the box
// and a roadblock notice are left as they are.

// vanishBridge records what slackc asks of the JS side.
type vanishBridge struct {
	mu        sync.Mutex
	renders   []vanishRender
	unmounted []uint64
}

type vanishRender struct {
	root  uint64
	name  string
	props []byte
}

func (b *vanishBridge) Render(root uint64, name string, props []byte) {
	b.mu.Lock()
	b.renders = append(b.renders, vanishRender{root, name, append([]byte(nil), props...)})
	b.mu.Unlock()
}
func (b *vanishBridge) RenderFn(root uint64, fn int64, args []byte) {}
func (b *vanishBridge) Unmount(root uint64) {
	b.mu.Lock()
	b.unmounted = append(b.unmounted, root)
	b.mu.Unlock()
}
func (b *vanishBridge) Dispatch(target int64, event []byte)                  {}
func (b *vanishBridge) Call(fn int64, args []byte, done func([]byte, error)) {}
func (b *vanishBridge) SetRects(rects map[int64][4]float32)                  {}
func (b *vanishBridge) SetViewport(w, h, dpr float32, dark bool)             {}

// last is the latest render of component name.
func (b *vanishBridge) last(name string) (vanishRender, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.renders) - 1; i >= 0; i-- {
		if b.renders[i].name == name {
			return b.renders[i], true
		}
	}
	return vanishRender{}, false
}

func (b *vanishBridge) wasUnmounted(root uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.unmounted {
		if r == root {
			return true
		}
	}
	return false
}

// pluginTree is the host tree Taut sends back for a component whose patch
// renders <Original {...props}/>: an "@" node for it and one for each
// native slot in its props, nested as the slots are (the props as Go sent
// them, so the slot ids are the ones Go registered).
func pluginTree(t *testing.T, name string, props []byte) []byte {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(props, &p); err != nil {
		t.Fatal(err)
	}
	k := 0
	var node func(name string, p map[string]any, slot any) map[string]any
	node = func(name string, p map[string]any, slot any) map[string]any {
		k++
		np := map[string]any{}
		for key, v := range p {
			if key != "children" {
				np[key] = v
			}
		}
		if slot != nil {
			np["$slot"] = slot
		}
		n := map[string]any{"t": "@", "n": name, "k": k, "p": np}
		if kids, ok := p["children"].([]any); ok {
			var c []any
			for _, x := range kids {
				s, ok := x.(map[string]any)
				if !ok || s["$slot"] == nil {
					continue
				}
				sp, _ := s["p"].(map[string]any)
				nm, _ := s["n"].(string)
				c = append(c, node(nm, sp, s["$slot"]))
			}
			n["c"] = c
		}
		return n
	}
	b, err := json.Marshal([]any{node(name, p, nil)})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type vanishEnv struct {
	t     *testing.T
	b     *vanishBridge
	c     *Composer
	ops   *op.Ops
	r     *input.Router
	now   time.Time
	lines []string
}

// newVanishEnv is a channel composer in tChGen with patched (nil: nothing)
// patched, over the fake bridge.
func newVanishEnv(t *testing.T, patched []string) *vanishEnv {
	t.Helper()
	app, _ := syntheticApp(t)
	h := &slickHost{app: app, th: theme.New(false)}
	e := &vanishEnv{t: t, b: &vanishBridge{}, ops: new(op.Ops), r: new(input.Router), now: time.Now()}
	slackc.Init(slackc.Env{Theme: h.th, Core: app, Invalidate: func() {}})
	slackc.SetBridge(e.b)
	slackc.SetPatched(patched)
	OnVanish = func(l string) { e.lines = append(e.lines, l) }
	clearDraft(tChGen, "")
	e.c = New(h, tChGen, "")
	t.Cleanup(func() {
		e.c.Close()
		clearDraft(tChGen, "")
		OnVanish = nil
		slackc.RemoveStyle("vanish-test")
		slackc.Reset()
		slackc.SetBridge(nil)
		slackc.Init(slackc.Env{})
	})
	return e
}

// frame lays the composer out once, live (a real input source), dt after
// the last frame.
func (e *vanishEnv) frame(dt time.Duration) layout.Dimensions {
	e.now = e.now.Add(dt)
	e.ops.Reset()
	gtx := layout.Context{Ops: e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Min: image.Pt(900, 0), Max: image.Pt(900, 500)}, Now: e.now, Source: e.r.Source()}
	slackc.Frame(gtx)
	d := e.c.Layout(gtx)
	e.r.Frame(e.ops)
	return d
}

// boxDrawn fails unless the frame drew the composer's box.
func (e *vanishEnv) boxDrawn(what string, d layout.Dimensions) {
	e.t.Helper()
	if e.c.BoxRect().Empty() || d.Size.Y < 60 {
		e.t.Fatalf("%s: no message box (box %v, composer %v)", what, e.c.BoxRect(), d.Size)
	}
}

func (e *vanishEnv) render(name string) vanishRender {
	e.t.Helper()
	r, ok := e.b.last(name)
	if !ok {
		e.t.Fatalf("no render of %s reached the bridge", name)
	}
	return r
}

func (e *vanishEnv) deliver(root uint64, tree []byte) {
	e.t.Helper()
	if err := slackc.Deliver(root, tree); err != nil {
		e.t.Fatal(err)
	}
}

// The plugin root's tree comes back empty (an uncaught JS error): the box
// is drawn without plugins, the root is mounted again, the line says
// what the tree held; the fresh root's good tree is drawn through the
// plugin again with no further lines until the run is over.
func TestComposerBoxSurvivesEmptyPluginTree(t *testing.T) {
	e := newVanishEnv(t, []string{"MessagePaneInput"})
	e.boxDrawn("before the plugin's tree (its native draws)", e.frame(0))
	first := e.render("MessagePaneInput")

	e.deliver(first.root, []byte("[]"))
	d := e.frame(16 * time.Millisecond)
	e.boxDrawn("the plugin's tree empty", d)
	if !e.c.BoxVanished() {
		t.Error("BoxVanished: the guard didn't step in")
	}
	if len(e.lines) != 1 || !strings.Contains(e.lines[0], "MessagePaneInput for "+tChGen+" drew no message box") ||
		!strings.Contains(e.lines[0], "MessagePaneInput: an empty tree") || !strings.Contains(e.lines[0], "remounted 1 plugin root") {
		t.Fatalf("log lines %q", e.lines)
	}
	if !e.b.wasUnmounted(first.root) {
		t.Error("the empty root wasn't unmounted")
	}

	// the next frame mounts a fresh root (its native draws until it renders)
	e.boxDrawn("the frame after", e.frame(16*time.Millisecond))
	fresh := e.render("MessagePaneInput")
	if fresh.root == first.root {
		t.Fatal("no fresh root mounted")
	}
	e.deliver(fresh.root, pluginTree(t, "MessagePaneInput", fresh.props))
	d = e.frame(16 * time.Millisecond)
	e.boxDrawn("the fresh root's tree", d)
	if e.c.BoxVanished() {
		t.Error("a good tree still went through the guard")
	}
	if len(e.lines) != 1 {
		t.Errorf("more lines while the box was drawn: %q", e.lines[1:])
	}
	// drawn for vanishSettle: the run is over, said once
	e.frame(vanishSettle + time.Second)
	if len(e.lines) != 2 || !strings.Contains(e.lines[1], "draws its message box through its plugins again") {
		t.Errorf("end of the run: lines %q", e.lines)
	}
}

// A tree that keeps the patch's <Original> but drops what Go passed as its
// children (<Original channelId={…}/> without the props): the roadblock
// and everything inside it are missing, the box is drawn anyway. While the
// plugin keeps sending it, every frame is drawn without plugins, the roots
// are remounted 5 s apart and then further, and a line comes once a
// minute.
func TestComposerBoxSurvivesDroppedChildren(t *testing.T) {
	e := newVanishEnv(t, []string{"MessagePaneInput"})
	e.frame(0)
	bad := []byte(`[{"t":"div","k":1,"p":{"className":"plugin-wrap"},"c":[{"t":"@","n":"MessagePaneInput","k":2,"p":{"channelId":"` + tChGen + `"}}]}]`)
	// the plugin answers every root it gets, the current one included
	// (a dropped root's late tree is ignored, as from JS)
	answer := func() { e.deliver(e.render("MessagePaneInput").root, bad) }
	answer()
	e.boxDrawn("<Original> without its children", e.frame(16*time.Millisecond))
	if len(e.lines) != 1 || !strings.Contains(e.lines[0], "MessagePaneInput: 2 nodes;") || !strings.Contains(e.lines[0], "patched: MessagePaneInput") {
		t.Fatalf("log lines %q", e.lines)
	}
	roots := map[uint64]bool{}
	for i := 0; i < 32; i++ { // 64 s of frames 2 s apart (under the 5 s sweep)
		answer()
		e.boxDrawn("frame "+strconv.Itoa(i), e.frame(2*time.Second))
		roots[e.render("MessagePaneInput").root] = true
	}
	// remounted at 5, 15, 35 s and so on: a few roots, not one a frame
	if n := len(roots); n < 3 || n > 6 {
		t.Errorf("%d roots mounted over a minute of a bad tree; want a few (backing off)", n)
	}
	if len(e.lines) != 2 || !strings.Contains(e.lines[1], "still draws no message box through its plugins") {
		t.Errorf("after a minute: lines %q", e.lines)
	}
}

// A tree in which the plugin draws the composer whole: drawn through the
// plugin, the guard stays out of it, nothing logged.
func TestComposerGoodPluginTree(t *testing.T) {
	e := newVanishEnv(t, []string{"MessagePaneInput"})
	e.frame(0)
	r := e.render("MessagePaneInput")
	e.deliver(r.root, pluginTree(t, "MessagePaneInput", r.props))
	for i := 0; i < 3; i++ {
		e.boxDrawn("a plugin tree with every part", e.frame(16*time.Millisecond))
		if e.c.BoxVanished() {
			t.Fatal("the guard stepped in on a good tree")
		}
	}
	if len(e.lines) != 0 {
		t.Errorf("lines for a good tree: %q", e.lines)
	}
	if e.b.wasUnmounted(r.root) {
		t.Error("a good root was remounted")
	}
}

// No plugin at all: the composer draws as it always did.
func TestComposerUnpatchedNoGuard(t *testing.T) {
	e := newVanishEnv(t, nil)
	for i := 0; i < 3; i++ {
		e.boxDrawn("unpatched", e.frame(16*time.Millisecond))
	}
	if e.c.BoxVanished() || len(e.lines) != 0 {
		t.Errorf("unpatched: vanished %v, lines %q", e.c.BoxVanished(), e.lines)
	}
}

// CSS that hides the composer (a theme or plugin rule) is what the
// stylesheet asked for: no box, no guard, no line.
func TestComposerHiddenByCSS(t *testing.T) {
	e := newVanishEnv(t, nil)
	e.boxDrawn("before the rule", e.frame(0))
	slackc.SetStyle("vanish-test", ".p-message_pane_input { display: none }")
	d := e.frame(16 * time.Millisecond)
	if d.Size.Y != 0 || !e.c.BoxRect().Empty() {
		t.Errorf("display: none drew %v (box %v)", d.Size, e.c.BoxRect())
	}
	if !e.c.HiddenByCSS() || e.c.BoxVanished() || len(e.lines) != 0 {
		t.Errorf("hidden by CSS: HiddenByCSS %v, vanished %v, lines %q", e.c.HiddenByCSS(), e.c.BoxVanished(), e.lines)
	}
	slackc.RemoveStyle("vanish-test")
	e.boxDrawn("after the rule went", e.frame(16*time.Millisecond))
}

// A roadblock notice (an announcement-only channel) stands in for the
// box, patched or not: no guard.
func TestComposerRoadblockNoGuard(t *testing.T) {
	for _, patched := range [][]string{nil, {"MessagePaneInput"}} {
		t.Run(strings.Join(append([]string{"patched"}, patched...), "-"), func(t *testing.T) {
			e := newVanishEnv(t, patched)
			e.c.SetRoadblock("Only admins can post in this channel.")
			if patched != nil {
				e.frame(0)
				r := e.render("MessagePaneInput")
				e.deliver(r.root, pluginTree(t, "MessagePaneInput", r.props))
			}
			d := e.frame(16 * time.Millisecond)
			if d.Size.Y < 56 || !e.c.drewRoadblock {
				t.Errorf("no roadblock drawn (%v)", d.Size)
			}
			if e.c.BoxVanished() || len(e.lines) != 0 {
				t.Errorf("the guard stepped in for a roadblock: lines %q", e.lines)
			}
		})
	}
}
