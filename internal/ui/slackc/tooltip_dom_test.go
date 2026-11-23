package slackc

import (
	"image"
	"slices"
	"testing"

	"gioui.org/layout"
)

// Slack's BaseTooltip (client-boot F2XN) renders the child it clones, with
// data-sk="tooltip_parent", and then span[hidden][data-sk=tooltip]. In a
// row of three items whose middle one a Tooltip holds, the row's children
// in Slack's DOM are the first item, the middle one, the hidden span and
// the last item, and selectors match them there: the middle item is the
// row's 2nd child (not the Tooltip's only one) and 3rd from last, the
// last item its 4th child and 4th span, no item its 3rd; "+" from the
// middle item reaches the hidden span, not the last item, and "~" goes on
// past it (ChannelListTyping's `.slick-channel-typing +
// .p-channel_sidebar__channel_suffix` doesn't reach past its Tooltip in
// Slack); the row :has() the span. The same tree laid out apart, as a
// host lays out an Element prop, numbers its roots among themselves.
func TestTooltipChildAsSlacksDOM(t *testing.T) {
	windowSize = image.Pt(1000, 800)
	RegisterHost(func(gtx layout.Context, h *Host) layout.Dimensions { return h.Children(gtx) }, "TestNoElementTip")
	RegisterHostNoElement("TestNoElementTip")
	var inProp *View
	RegisterHost(func(gtx layout.Context, h *Host) layout.Dimensions {
		inProp = h.view
		return h.Element(gtx, "content")
	}, "TestTipDomHost")
	SetStyle("test:tip-dom", `
      .item:first-child { --first: y; }
      .item:last-child { --last: y; }
      .item:only-child { --only: y; }
      .item:nth-child(2) { --nth2: y; }
      .item:nth-child(3) { --nth3: y; }
      .item:nth-child(4) { --nth4: y; }
      .item:nth-of-type(4) { --type4: y; }
      .item:nth-last-child(3) { --last3: y; }
      #b + .item, #pb + .item { --after-b: y; }
      [data-sk=tooltip] + .item { --after-tip: y; }
      #b ~ .item, #pb ~ .item { --sib-b: y; }
      [data-sk=tooltip_parent] { --clone: y; }
      .row:has(> span[hidden][data-sk=tooltip]) { --has-tip: y; }`)
	defer RemoveStyle("test:tip-dom")
	items := func(p string) string {
		return `{"t":"span","k":2,"p":{"id":"` + p + `a","className":"item"},"c":[{"t":"#","v":"a"}]},` +
			`{"t":"@","n":"TestNoElementTip","k":3,"p":{"tip":"hi"},"c":[` +
			`{"t":"span","k":4,"p":{"id":"` + p + `b","className":"item"},"c":[{"t":"#","v":"b"}]}]},` +
			`{"t":"span","k":5,"p":{"id":"` + p + `c","className":"item"},"c":[{"t":"#","v":"c"}]}`
	}
	all := []string{"--first", "--last", "--only", "--nth2", "--nth3", "--nth4", "--type4", "--last3", "--after-b", "--after-tip", "--sib-b", "--clone"}
	want := map[string][]string{
		"a": {"--first"},
		"b": {"--nth2", "--last3", "--clone"},
		"c": {"--last", "--nth4", "--type4", "--after-tip", "--sib-b"},
	}
	check := func(what string, find func(id string) *box) {
		t.Helper()
		for _, id := range []string{"a", "b", "c"} {
			b := find(id)
			if b == nil {
				t.Fatalf("%s: no box for item %s", what, id)
			}
			for _, k := range all {
				if got, exp := b.st.Vars[k] == "y", slices.Contains(want[id], k); got != exp {
					t.Errorf("%s: item %s %s matched %v, want %v (Slack's DOM: a, b, the hidden span, c)", what, id, k, got, exp)
				}
			}
		}
	}

	v := NewView(9002)
	v.Layout(testGtx(400, 100), mustTree(t, `[{"t":"div","k":1,"p":{"id":"row","className":"row"},"c":[`+items("")+`]}]`), "", nil, nil)
	check("in a row", func(id string) *box { return byID(v, id) })
	if row := byID(v, "row"); row == nil || row.st.Vars["--has-tip"] != "y" {
		t.Error(".row:has(> span[hidden][data-sk=tooltip]) didn't match the row")
	}

	// the items as a host's Element prop: the roots of a tree laid out
	// apart, each other's siblings
	v2 := NewView(9003)
	v2.Layout(testGtx(400, 100), mustTree(t, `[{"t":"@","n":"TestTipDomHost","k":1,"p":{"content":{"$el":[`+items("p")+`]}}}]`), "", nil, nil)
	if inProp != v2 || len(v2.extra) == 0 {
		t.Fatalf("the host laid out no Element prop (%d extra trees)", len(v2.extra))
	}
	check("as a host's Element prop", func(id string) *box {
		for _, x := range v2.extra {
			var find func([]*box) *box
			find = func(bs []*box) *box {
				for _, b := range bs {
					if b.n != nil && b.n.ID == "p"+id {
						return b
					}
					if r := find(b.kids); r != nil {
						return r
					}
				}
				return nil
			}
			if b := find(x.boxes); b != nil {
				return b
			}
		}
		return nil
	})
}
