package slackc

import (
	"image"
	"strings"
	"testing"

	"gioui.org/layout"
)

// recBridge records renders and unmounts by root.
type recBridge struct {
	renders   []recRender
	unmounted []uint64
}

type recRender struct {
	root uint64
	name string
}

func (b *recBridge) Render(root uint64, name string, props []byte) {
	b.renders = append(b.renders, recRender{root, name})
}
func (b *recBridge) RenderFn(root uint64, fn int64, args []byte)          {}
func (b *recBridge) Unmount(root uint64)                                  { b.unmounted = append(b.unmounted, root) }
func (b *recBridge) Dispatch(target int64, event []byte)                  {}
func (b *recBridge) Call(fn int64, args []byte, done func([]byte, error)) {}
func (b *recBridge) SetRects(rects map[int64][4]float32)                  {}
func (b *recBridge) SetViewport(w, h, dpr float32, dark bool)             {}

func (b *recBridge) last(name string) uint64 {
	for i := len(b.renders) - 1; i >= 0; i-- {
		if b.renders[i].name == name {
			return b.renders[i].root
		}
	}
	return 0
}

// boxNative records the props it was drawn with.
type boxNative struct {
	calls int
	props []Props
}

func (n *boxNative) props0() Props {
	return Props{"$key": "k1", "children": []any{Child("Part", Props{"x": 1}, nativeFn)}}
}
func (n *boxNative) draw(gtx layout.Context, p Props) layout.Dimensions {
	n.calls++
	n.props = append(n.props, p)
	return layout.Dimensions{Size: image.Pt(100, 40)}
}

func guardEnv(t *testing.T, names ...string) *recBridge {
	t.Helper()
	b := &recBridge{}
	SetBridge(b)
	SetPatched(names)
	t.Cleanup(func() { Reset(); SetBridge(nil) })
	return b
}

func guardCtx() layout.Context {
	gtx := ctx()
	gtx.Constraints.Max = image.Pt(400, 400)
	return gtx
}

// Until a plugin's first tree arrives a Component native draws as it does
// unpatched, with nil props: given the props built for JS it took them for
// <Original>'s, and the composer drew nothing until JS answered. A Render
// native keeps getting its own props, as unpatched.
func TestPreTreeNativeDrawsUnpatched(t *testing.T) {
	guardEnv(t, "Box", "Row", "Lazy")
	gtx := guardCtx()
	n := &boxNative{}
	if d := Component(gtx, "Box", n, (*boxNative).props0, func(n *boxNative, gtx layout.Context, p Props) layout.Dimensions { return n.draw(gtx, p) }); d.Size.Y != 40 {
		t.Fatalf("pre-tree Component drew %v", d.Size)
	}
	if n.calls != 1 || n.props[0] != nil {
		t.Errorf("pre-tree Component native: %d calls, props %v; want nil props (as unpatched)", n.calls, n.props)
	}
	var got Props
	Render(gtx, "Row", Props{"$key": "r1", "a": 1}, func(gtx layout.Context, p Props) layout.Dimensions { got = p; return layout.Dimensions{} })
	if got.IntOr("a", 0) != 1 {
		t.Errorf("pre-tree Render native got %v; want its own props", got)
	}
	got = Props{"stale": true}
	RenderLazy(gtx, "Lazy", func() Props { return Props{"$key": "l1", "b": 2} }, func(gtx layout.Context, p Props) layout.Dimensions { got = p; return layout.Dimensions{} })
	if got != nil {
		t.Errorf("pre-tree RenderLazy native got %v; want nil", got)
	}
	// with a bridge but no tree, or no bridge at all: the same
	SetBridge(nil)
	n.props = nil
	Component(gtx, "Box", n, (*boxNative).props0, func(n *boxNative, gtx layout.Context, p Props) layout.Dimensions { return n.draw(gtx, p) })
	if len(n.props) != 1 || n.props[0] != nil {
		t.Errorf("no bridge: Component native props %v; want nil", n.props)
	}
}

// Unpatched draws every boundary by its native, whatever the plugin's tree
// holds; KeyedTrees says what that was; RemountKeyed drops the instance so
// the next draw mounts a fresh root.
func TestUnpatchedRemountKeyed(t *testing.T) {
	b := guardEnv(t, "Box")
	gtx := guardCtx()
	n := &boxNative{}
	draw := func() layout.Dimensions {
		return Component(gtx, "Box", n, (*boxNative).props0, func(n *boxNative, gtx layout.Context, p Props) layout.Dimensions { return n.draw(gtx, p) })
	}
	draw()
	root := b.last("Box")
	if root == 0 {
		t.Fatal("no render reached the bridge")
	}
	if s := KeyedTrees("k1"); s != "Box: no tree yet" {
		t.Errorf("KeyedTrees before a tree = %q", s)
	}
	// a tree that dropped the original: the native isn't called
	if err := Deliver(root, []byte(`[{"t":"div","k":1,"c":[{"t":"#","v":"plugin"}]}]`)); err != nil {
		t.Fatal(err)
	}
	n.calls = 0
	draw()
	if n.calls != 0 {
		t.Fatalf("a tree without <Original> called the native %d times", n.calls)
	}
	if s := KeyedTrees("k1"); s != "Box: 2 nodes without its <Original>" {
		t.Errorf("KeyedTrees = %q", s)
	}
	// Unpatched: the native draws, with nil props
	n.props = nil
	if d := Unpatched(draw); d.Size.Y != 40 || n.calls != 1 || n.props[0] != nil {
		t.Errorf("Unpatched: drew %v, native called %d times with %v", d.Size, n.calls, n.props)
	}
	if unpatched != 0 {
		t.Errorf("unpatched left at %d", unpatched)
	}
	// an empty tree
	if err := Deliver(root, []byte(`[]`)); err != nil {
		t.Fatal(err)
	}
	draw()
	if s := KeyedTrees("k1"); s != "Box: an empty tree" {
		t.Errorf("KeyedTrees = %q", s)
	}
	if got := RemountKeyed("k1"); got != 1 {
		t.Fatalf("RemountKeyed = %d", got)
	}
	if len(b.unmounted) != 1 || b.unmounted[0] != root {
		t.Errorf("unmounted %v, want [%d]", b.unmounted, root)
	}
	if s := KeyedTrees("k1"); s != "" {
		t.Errorf("KeyedTrees after the remount = %q", s)
	}
	n.calls = 0
	draw()
	if fresh := b.last("Box"); fresh == root || fresh == 0 {
		t.Errorf("the next draw rendered root %d (was %d): no fresh root", fresh, root)
	}
	if n.calls != 1 {
		t.Errorf("the fresh root's native drew %d times before its tree", n.calls)
	}
	if RemountKeyed("") != 0 || RemountKeyed("nobody") != 0 || !strings.Contains(KeyedTrees("k1"), "no tree yet") {
		t.Error("RemountKeyed of no key dropped something, or the fresh root is missing")
	}
}
