package slackc

import (
	"fmt"
	"image"
	"reflect"
	"testing"
)

// What a patched row costs on a frame where nothing changed (Karim's
// freeze reports of 2026-11-15: the UI goroutine inside renderPatched for
// every row scrolling by): JS isn't asked again, the message's hash isn't
// recomputed, and the Go func slots are refreshed without walking the
// message.

// bigMsg is a message's props the way messages builds them: blocks,
// reactions, files.
func bigMsg(ts string) Props {
	var blocks []any
	for i := 0; i < 20; i++ {
		blocks = append(blocks, Props{"type": "rich_text", "block_id": fmt.Sprint(i), "elements": []any{
			Props{"type": "rich_text_section", "elements": []any{Props{"type": "text", "text": "some words in a section"}}},
		}})
	}
	var rx []any
	for i := 0; i < 8; i++ {
		rx = append(rx, Props{"name": fmt.Sprint("e", i), "count": 3, "users": []any{"U1", "U2", "U3"}})
	}
	return Props{"type": "message", "ts": ts, "user": "U1", "text": "hello", "blocks": blocks, "reactions": rx,
		"files": []any{Props{"id": "F1", "name": "a.png"}}}
}

func TestUnchangedRowNotRenderedAgain(t *testing.T) {
	nb := &nestBridge{}
	SetBridge(nb)
	SetPatched([]string{"MessageWrapper"})
	t.Cleanup(func() { Reset(); SetBridge(nil) })
	gtx := ctx()
	gtx.Constraints.Max = image.Pt(400, 400)
	msg := bigMsg("1.1")
	clicks := 0
	frame := func(m Props) {
		// new props every frame, as a row's props func builds them, around
		// the row's cached message props and a fresh closure
		p := Props{"$key": "1.1", "msg": m, "viewContext": "message_pane", "onClick": func() { clicks++ },
			"children": []any{Child("MessageText", Props{"$key": "1.1", "msg": m}, nativeFn)}}
		Render(gtx, "MessageWrapper", p, nativeFn)
	}
	for i := 0; i < 100; i++ {
		frame(msg)
	}
	if len(nb.rendered) != 1 {
		t.Fatalf("%d renders over 100 unchanged frames, want 1", len(nb.rendered))
	}
	// the row's message changed: its props are a new map
	frame(bigMsg("1.1"))
	if len(nb.rendered) != 1 {
		t.Fatalf("a new map with the same message rendered again (%d)", len(nb.rendered))
	}
	changed := bigMsg("1.1")
	changed["text"] = "edited"
	frame(changed)
	if len(nb.rendered) != 2 {
		t.Fatalf("an edited message didn't render again (%d renders)", len(nb.rendered))
	}
	// the slot ids JS holds call this frame's closure
	var in *inst
	for _, x := range insts {
		if x.name == "MessageWrapper" {
			in = x
		}
	}
	id, ok := in.slots[".onClick"]
	if !ok {
		t.Fatalf("no slot for onClick: %v", in.slots)
	}
	frame(changed)
	callGo(goFn(id), nil)
	if clicks != 1 {
		t.Fatalf("the slot called a stale closure (%d clicks)", clicks)
	}
	if _, ok := goFn(in.slots[".children.0"]).(Slot); !ok {
		t.Fatalf("the children slot isn't this frame's Slot: %T", goFn(in.slots[".children.0"]))
	}
}

// propsHash is hashProps; it only skips hashing a "msg" map it has seen.
func TestPropsHashIsHashProps(t *testing.T) {
	in := &inst{}
	m := bigMsg("1.1")
	cases := []Props{
		{"$key": "a", "msg": m, "x": 1},
		{"msg": m, "x": 2, "nested": Props{"msg": "not a map"}},
		{"msg": "a string msg"},
		{"msg": nil, "y": []any{1.0}},
		{"msg": m},
		{},
	}
	for i, p := range cases {
		if got, want := in.propsHash(p), hashProps(p); got != want {
			t.Errorf("case %d: propsHash %x, hashProps %x", i, got, want)
		}
	}
	m2 := bigMsg("1.1")
	m2["text"] = "other"
	if in.propsHash(Props{"msg": m}) == in.propsHash(Props{"msg": m2}) {
		t.Error("two messages hash the same")
	}
	if in.propsHash(Props{"msg": m}) != hashProps(Props{"msg": m}) {
		t.Error("going back to the first map")
	}
}

// refreshSlots reaches every slot the full walk did, and skips the rest
// of the props without allocating.
func TestRefreshSlotsSkipsTheMessage(t *testing.T) {
	type fns struct{ a, b, c func() }
	mk := func(f fns, m Props) Props {
		return Props{"msg": m, "onA": f.a, "list": []any{"x", Props{"onB": f.b}},
			"slot": Child("Inner", Props{"onC": f.c, "msg": m}, nativeFn)}
	}
	var hit []string
	f1 := fns{func() { hit = append(hit, "a1") }, func() { hit = append(hit, "b1") }, func() { hit = append(hit, "c1") }}
	m := bigMsg("1.1")
	in := &inst{slots: map[string]int64{}}
	encodeProps(mk(f1, m), in.slots)
	t.Cleanup(func() { releaseSlots(in.slots) })
	want := []string{".onA", ".list.1.onB", ".slot", ".slot.p.onC"}
	for _, p := range want {
		if _, ok := in.slots[p]; !ok {
			t.Fatalf("no slot %s in %v", p, in.slots)
		}
	}
	f2 := fns{func() { hit = append(hit, "a2") }, func() { hit = append(hit, "b2") }, func() { hit = append(hit, "c2") }}
	p2 := mk(f2, m)
	in.refreshSlots(p2)
	for _, path := range []string{".onA", ".list.1.onB", ".slot.p.onC"} {
		callGo(goFn(in.slots[path]), nil)
	}
	if !reflect.DeepEqual(hit, []string{"a2", "b2", "c2"}) {
		t.Fatalf("slots called %v, want this frame's closures", hit)
	}
	if s, ok := goFn(in.slots[".slot"]).(Slot); !ok || s.Name != "Inner" {
		t.Fatalf("the Slot slot holds %T", goFn(in.slots[".slot"]))
	}
	if n := testing.AllocsPerRun(200, func() { in.refreshSlots(p2) }); n != 0 {
		t.Errorf("refreshSlots: %v allocations (it walked the message)", n)
	}
}
