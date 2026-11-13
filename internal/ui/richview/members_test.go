package richview

import (
	"strings"
	"testing"

	"gioui.org/op"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// A user mention names the member as plugins present it (Nicknames'
// patched members slice, through the ui root's MemberLabel): the label
// replaces the parsed "@name", the doc is laid out again when the label
// changes, and the parsed name comes back when the plugin stops changing
// the member. Synthetic names.
func TestMentionLabelFromPlugins(t *testing.T) {
	labels := map[string]string{}
	calls := 0
	SetMemberLabel(func(id string) (string, bool) {
		calls++
		n, ok := labels[id]
		return n, ok
	})
	t.Cleanup(func() { SetMemberLabel(nil) })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	doc := rt.Doc{Blocks: []rt.Block{{Kind: rt.Paragraph, Spans: []rt.Span{
		{Kind: rt.Text, Text: "ping "},
		{Kind: rt.User, Text: "@alice", ID: "UALICE0001"},
		{Kind: rt.Text, Text: " about the "},
		{Kind: rt.Channel, Text: "#general", ID: "C0GENERAL1"},
	}}}}
	const w = 480
	v := layoutAt(st, doc, Options{}, w)
	text := func() string {
		var ops op.Ops
		v.Layout(testGtx(&ops, w), st, &recHandler{})
		return v.TextRange(0, v.PlainLen())
	}
	if got := text(); !strings.Contains(got, "@alice") {
		t.Fatalf("unpatched: %q, want the parsed @alice", got)
	}
	labels["UALICE0001"] = "Ally the Nicknamed"
	if got := text(); !strings.Contains(got, "@Ally the Nicknamed") || strings.Contains(got, "@alice") {
		t.Errorf("with a nickname: %q", got)
	}
	if got := text(); !strings.Contains(got, "#general") {
		t.Errorf("the channel mention changed: %q", got)
	}
	delete(labels, "UALICE0001")
	if got := text(); !strings.Contains(got, "@alice") || strings.Contains(got, "Ally") {
		t.Errorf("the nickname removed: %q", got)
	}
	// no hook (no plugin runtime): nothing is asked or recorded
	SetMemberLabel(nil)
	calls = 0
	v2 := layoutAt(st, doc, Options{}, w)
	if len(v2.d.names) != 0 || calls != 0 {
		t.Errorf("without a hook the layout recorded %d mentions and asked %d times", len(v2.d.names), calls)
	}
}

// A mention whose member the store didn't hold when its message loaded
// (core builds message docs without asking: core/lazy_users.go) reads the
// member as it draws (MemberRead, Slack's getMemberById in MemberSlug):
// each layout reads it, the parsed id stays until the member is known,
// then the doc is laid out again with the name; a plugin's label still
// wins. Synthetic names.
func TestMentionReadsMemberWhenDrawn(t *testing.T) {
	known := map[string]string{}
	reads := 0
	SetMemberRead(func(id string) (string, bool) {
		reads++
		n, ok := known[id]
		return n, ok
	})
	t.Cleanup(func() { SetMemberRead(nil) })
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	doc := rt.Doc{Blocks: []rt.Block{{Kind: rt.Paragraph, Spans: []rt.Span{
		{Kind: rt.Text, Text: "ping "},
		{Kind: rt.User, Text: "@UBOB000001", ID: "UBOB000001"},
	}}}}
	const w = 480
	v := layoutAt(st, doc, Options{}, w)
	text := func() string {
		var ops op.Ops
		v.Layout(testGtx(&ops, w), st, &recHandler{})
		return v.TextRange(0, v.PlainLen())
	}
	if got := text(); !strings.Contains(got, "@UBOB000001") {
		t.Fatalf("unknown member: %q, want the parsed id", got)
	}
	n := reads
	text()
	if reads <= n {
		t.Errorf("a drawn mention didn't read its member again (%d reads, then %d)", n, reads)
	}
	known["UBOB000001"] = "bob"
	if got := text(); !strings.Contains(got, "@bob") || strings.Contains(got, "UBOB000001") {
		t.Errorf("the member arrived: %q, want @bob", got)
	}
	SetMemberLabel(func(id string) (string, bool) { return "Bobby", id == "UBOB000001" })
	t.Cleanup(func() { SetMemberLabel(nil) })
	if got := text(); !strings.Contains(got, "@Bobby") {
		t.Errorf("a plugin's label: %q, want @Bobby", got)
	}
}
