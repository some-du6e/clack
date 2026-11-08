package slackc

import (
	"image"
	"os"
	"regexp"
	"testing"

	"gioui.org/layout"
)

// viewContentsCSS is the layered sheet taut's RenderedViewContents shape
// installs (Slack's own layout for .p-view_contents), read from the JS so
// the two can't drift.
func viewContentsCSS(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../../taut/js/04_react.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const VIEW_CONTENTS_CSS =\s*'([^']+)'`).FindSubmatch(src)
	if m == nil {
		t.Fatal("no VIEW_CONTENTS_CSS in 04_react.js")
	}
	return string(m[1])
}

// TestViewContentsTabBar: the primary view element as taut draws
// RenderedViewContents (a host div around Clack's body slot) with a bar a
// plugin put first inside it, styled the way Slick's ChatTabs does: the
// view pads its top by the bar's height only while it :has(> the bar),
// the bar is absolutely positioned over that padding, and the body slot
// gets exactly the height left (a native fills what it is given).
func TestViewContentsTabBar(t *testing.T) {
	SetStyle("test:view-contents", viewContentsCSS(t))
	SetStyle("test:tabs", `
		.tabs-layout { display:contents; }
		.tabs-layout .p-view_contents--primary:has(> .tabs) { padding-top:var(--tabs-h, 0px); }
		.tabs { position:absolute; top:0; left:0; right:0; z-index:1; height:40px; }`)
	t.Cleanup(func() { RemoveStyle("test:view-contents"); RemoveStyle("test:tabs") })
	windowSize = image.Pt(1000, 800)

	run := func(withBar bool) (*View, layout.Constraints) {
		t.Helper()
		bar := ""
		if withBar {
			bar = `{"t":"div","k":3,"p":{"className":"tabs","id":"bar"},"c":[{"t":"#","v":"tabs"}]},`
		}
		js := `[{"t":"div","k":1,"p":{"className":"tabs-layout","style":{"--tabs-h":"40px"}},"c":[
			{"t":"div","k":2,"p":{"className":"p-view_contents p-view_contents--primary p-workspace__primary_view","id":"pv"},"c":[` + bar + `
				{"t":"@","n":"Body","k":4,"p":{"id":"body","style":{"flex":"1 1 0","minHeight":0,"minWidth":0}}}]}]}]`
		var got layout.Constraints
		native := func(gtx layout.Context, p Props) layout.Dimensions {
			got = gtx.Constraints
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
		v := NewView(1)
		gtx := testGtx(600, 400)
		gtx.Constraints.Min = gtx.Constraints.Max
		d := v.Layout(gtx, mustTree(t, js), "Body", nil, native)
		if d.Size != image.Pt(600, 400) {
			t.Errorf("view dims %v, want the panel's 600x400", d.Size)
		}
		return v, got
	}

	v, cs := run(true)
	checkBox(t, v, "pv", 0, 0, 600, 400)
	checkBox(t, v, "bar", 0, 0, 600, 40)
	checkBox(t, v, "body", 0, 40, 600, 360)
	if cs.Min != image.Pt(600, 360) || cs.Max != image.Pt(600, 360) {
		t.Errorf("body native constraints %v, want exactly 600x360 (the panel less the bar)", cs)
	}

	// no bar: no padding, the body has the whole view
	v, cs = run(false)
	checkBox(t, v, "body", 0, 0, 600, 400)
	if cs.Max != image.Pt(600, 400) {
		t.Errorf("body native constraints without the bar %v", cs)
	}
}

// TestContentsAtRootIsTransparent: a display:contents element at a
// tree's top (ChatTabs' layout div around ClientPrimaryView) leaves its
// children laid out against the scope: a 100% height resolves against the
// scope's height instead of the wrapper's auto height.
func TestContentsAtRootIsTransparent(t *testing.T) {
	SetStyle("test:contents-root", `.full{height:100%;display:flex;flex-direction:column} .wrap{display:contents}`)
	t.Cleanup(func() { RemoveStyle("test:contents-root") })
	windowSize = image.Pt(1000, 800)
	for _, js := range []string{
		`[{"t":"div","k":2,"p":{"className":"full","id":"v"}}]`,
		`[{"t":"div","k":1,"p":{"className":"wrap"},"c":[{"t":"div","k":2,"p":{"className":"full","id":"v"}}]}]`,
		`[{"t":"div","k":1,"p":{"className":"wrap"},"c":[{"t":"div","k":3,"p":{"className":"wrap"},"c":[{"t":"div","k":2,"p":{"className":"full","id":"v"}}]}]}]`,
	} {
		v := NewView(1)
		gtx := testGtx(600, 400)
		gtx.Constraints.Min = gtx.Constraints.Max
		v.Layout(gtx, mustTree(t, js), "X", nil, nil)
		checkBox(t, v, "v", 0, 0, 600, 400)
	}
}
