package slackc

import (
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
)

type row struct{ n int }

func (r *row) props() Props { return Props{"n": r.n} }
func (r *row) native(gtx layout.Context, p Props) layout.Dimensions {
	r.n++
	return layout.Dimensions{}
}

func nativeFn(gtx layout.Context, p Props) layout.Dimensions { return layout.Dimensions{} }

func ctx() layout.Context { return layout.Context{Ops: new(op.Ops)} }

// The unpatched paths must not allocate: owners call them for every
// widget, every frame.
func TestUnpatchedNoAlloc(t *testing.T) {
	gtx := ctx()
	r := &row{}
	if n := testing.AllocsPerRun(1000, func() { Render(gtx, "MessageSender", nil, nativeFn) }); n != 0 {
		t.Errorf("Render: %v allocs", n)
	}
	if n := testing.AllocsPerRun(1000, func() { Component(gtx, "MessageSender", r, (*row).props, (*row).native) }); n != 0 {
		t.Errorf("Component: %v allocs", n)
	}
	x := 3
	if n := testing.AllocsPerRun(1000, func() {
		RenderLazy(gtx, "MessageSender", func() Props { return Props{"x": x} }, nativeFn)
	}); n != 0 {
		t.Errorf("RenderLazy: %v allocs", n)
	}
	// Some other component patched: still free for this one.
	SetPatched([]string{"Reaction"})
	defer SetPatched(nil)
	if n := testing.AllocsPerRun(1000, func() { Component(gtx, "MessageSender", r, (*row).props, (*row).native) }); n != 0 {
		t.Errorf("Component, other name patched: %v allocs", n)
	}
	if Patched("MessageSender") || !Patched("Reaction") {
		t.Error("Patched")
	}
}

func TestPropsRoundTrip(t *testing.T) {
	called := false
	p := Props{"channelId": "C1", "memberCount": 3, "onClick": func() { called = true }, "nested": Props{"a": []any{1.0, "b"}}, "$key": "k"}
	slots := map[string]int64{}
	js := encodeProps(p, slots)
	got, err := DecodeProps(js)
	if err != nil {
		t.Fatal(err)
	}
	if got.StrOr("channelId", "") != "C1" || got.IntOr("memberCount", 0) != 3 || got.Has("$key") {
		t.Fatalf("decoded %s → %#v", js, got)
	}
	got.Invoke("onClick")
	if !called {
		t.Fatal("go func did not survive the round trip")
	}
	if hashProps(p) != hashProps(Props{"$key": "other", "channelId": "C1", "memberCount": 3, "onClick": func() {}, "nested": Props{"a": []any{1.0, "b"}}}) {
		t.Fatal("hash should ignore $key and closure identity")
	}
	releaseSlots(slots)
}

func TestParseTree(t *testing.T) {
	nodes, err := ParseTree([]byte(`[{"t":"div","k":1,"p":{"className":"a b","style":{"marginLeft":4},"data-qa":"x"},"on":{"click":1},"c":["hi",{"t":"#","v":"there"},[{"t":"span","k":2}],{"t":"@","n":"Original","k":3,"p":{"fn":{"$fn":9},"tip":{"$el":[{"t":"#","v":"tip"}]}}}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	d := nodes[0]
	if d.T != "div" || len(d.Class) != 2 || d.On["click"] != 1 || len(d.C) != 4 {
		t.Fatalf("%+v", d)
	}
	if v, _ := d.AttrString("data-qa"); v != "x" {
		t.Fatal("attr")
	}
	at := d.C[3]
	if _, ok := at.P["fn"].(JSFunc); !ok {
		t.Fatalf("fn: %#v", at.P["fn"])
	}
	if e, ok := at.P.Elem("tip"); !ok || len(e.Nodes) != 1 || e.Nodes[0].V != "tip" {
		t.Fatalf("tip: %#v", at.P["tip"])
	}
	if d.C[2].parent != d || d.C[2].index != 0 || d.C[3].index != 1 || d.C[3].nsib != 2 {
		t.Fatal("sibling index")
	}
}

func BenchmarkRenderUnpatched(b *testing.B) {
	gtx := ctx()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Render(gtx, "MessageSender", nil, nativeFn)
	}
}

func BenchmarkComponentUnpatched(b *testing.B) {
	gtx := ctx()
	r := &row{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Component(gtx, "MessageSender", r, (*row).props, (*row).native)
	}
}

func BenchmarkComponentOtherPatched(b *testing.B) {
	gtx := ctx()
	r := &row{}
	SetPatched([]string{"Reaction"})
	defer SetPatched(nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Component(gtx, "MessageSender", r, (*row).props, (*row).native)
	}
}

// BenchmarkNativeDirect is the baseline: calling the widget directly.
func BenchmarkNativeDirect(b *testing.B) {
	gtx := ctx()
	r := &row{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.native(gtx, nil)
	}
}

func BenchmarkStyledUnstyled(b *testing.B) {
	gtx := ctx()
	d := Class("c-message_kit__message")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := d.Enter(gtx, 0, 0)
		s.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} })
		s.Leave()
	}
}
