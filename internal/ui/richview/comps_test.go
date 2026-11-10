package richview

import (
	"testing"

	"gioui.org/op"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

func compDoc() rt.Doc {
	return doc(
		para(txt("hi "), rt.Span{Kind: rt.User, ID: "U1", Text: "@alice"}, txt(" see "), rt.Span{Kind: rt.Channel, ID: "C1", Text: "#general"}, txt(" ok")),
		rt.Block{Kind: rt.AttachmentBlock, Att: &rt.Attachment{AuthorName: "bob", Title: []rt.Span{txt("A shared message")}, IsShare: true, ShareChannel: "random", ShareChannelID: "C2"}},
	)
}

// No plugin, no CSS: nothing is a component.
func TestNoComponentsByDefault(t *testing.T) {
	if compMode() != 0 {
		t.Fatalf("mode %b without plugins or CSS", compMode())
	}
	v := layoutAt(newTestStyle(), compDoc(), Options{}, 400)
	if len(v.d.comps) != 0 {
		t.Fatalf("%d components", len(v.d.comps))
	}
}

// patchAll makes every pill and card a component (as a plugin patching
// them, or installed CSS, does).
func patchAll(t *testing.T) {
	slackc.SetPatched([]string{"MemberLink", "BaseMrkdwnChannel", "MessageAttachment"})
	t.Cleanup(func() { slackc.SetPatched(nil) })
	if compMode() != modePills|modeAtts {
		t.Fatalf("mode %b with patches", compMode())
	}
}

// Patched, pills and cards become components drawn per frame; the layout
// itself must not move.
func TestComponentsKeepLayout(t *testing.T) {
	st := newTestStyle()
	plain := layoutAt(st, compDoc(), Options{}, 400)
	patchAll(t)
	v := layoutAt(st, compDoc(), Options{TS: "1.2", Channel: "C9"}, 400)
	if v.d.size != plain.d.size || len(v.d.frags) != len(plain.d.frags) {
		t.Fatalf("size %v / %d frags, want %v / %d", v.d.size, len(v.d.frags), plain.d.size, len(plain.d.frags))
	}
	for i := range v.d.frags {
		a, b := v.d.frags[i], plain.d.frags[i]
		if a.x != b.x || a.y != b.y || a.p0 != b.p0 || a.p1 != b.p1 {
			t.Fatalf("frag %d moved: %+v vs %+v", i, a, b)
		}
	}
	if len(v.d.comps) != 3 || v.d.comps[0].kind != compUser || v.d.comps[1].kind != compChannel || v.d.comps[2].kind != compAtt {
		t.Fatalf("components %+v", v.d.comps)
	}
	for i, c := range v.d.comps {
		if c.r.Empty() {
			t.Errorf("component %d has no box", i)
		}
	}
	if string(v.d.plain) != string(plain.d.plain) {
		t.Errorf("plain text %q, want %q", v.d.plain, plain.d.plain)
	}
	// Drawing goes through the natives (no plugin runtime: they get our
	// own props) and settles: no extra space is fed back.
	ops := new(op.Ops)
	for i := 0; i < 3; i++ {
		ops.Reset()
		v.Layout(testGtx(ops, 400), st, nil)
	}
	for i := range v.cs {
		if v.cs[i].extra != 0 {
			t.Errorf("component %d: extra %d", i, v.cs[i].extra)
		}
	}
	if v.dirty {
		t.Error("layout keeps invalidating")
	}
	ref := compRef{v: v, i: 2, st: st}
	if p := ref.shareProps(); p.StrOr("channelName", "") != "random" || p.StrOr("channelId", "") != "C2" || p.StrOr("messageTs", "") != "1.2" {
		t.Errorf("share props %v", p)
	}
	if p := (&compRef{v: v, i: 1}).channelProps(); p.StrOr("channelName", "") != "general" || p.StrOr("id", "") != "C1" {
		t.Errorf("channel props %v", p)
	}
}

// A plugin's channelName for a shared message shows in "Posted in".
func TestShareChannelOverride(t *testing.T) {
	st := newTestStyle()
	patchAll(t)
	v := layoutAt(st, compDoc(), Options{}, 400)
	before := string(v.d.plain)
	ref := compRef{v: v, i: 2, st: st}
	ops := new(op.Ops)
	ref.layoutShareNative(testGtx(ops, 400), slackc.Props{"channelName": "C2-stub", "isPrivate": true})
	if !v.dirty {
		t.Fatal("override did not ask for a relayout")
	}
	v.relayout(st, 400, 1, 1)
	if after := string(v.d.plain); after == before || !contains(after, "C2-stub") {
		t.Errorf("plain %q (before %q)", after, before)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
