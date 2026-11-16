package composer

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// ComposerLinkModal is a boundary plugins can patch, and then the dialog's
// fields are a native slackc also lays out in measure passes
// (gtx.Disabled()), where a FocusCmd goes nowhere. The dialog's first
// focus waits for a pass that delivers input: spent in a measure pass
// before the first draw, neither field ever got the caret.
func TestLinkDialogFocusAfterMeasurePass(t *testing.T) {
	app, _ := fakeslackApp(t)
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := New(h, "C0W10LINK", "")
	t.Cleanup(c.Close)
	var dlg *crLinkDlg
	linkDialogTap = func(d *crLinkDlg) { dlg = d }
	t.Cleanup(func() { linkDialogTap = nil })
	d := NewDoc()
	d.Insert(0, "plain words", Attr{})
	c.ed.SetDoc(d)
	c.ed.SetSelection(0, 5)
	c.openLink()
	if dlg == nil || h.last() == nil {
		t.Fatal("no link dialog")
	}
	var r input.Router
	ops := new(op.Ops)
	gtx := func() layout.Context {
		ops.Reset()
		return layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800)),
			Now: time.Now(), Source: r.Source()}
	}
	// what slackc's measure pass of the patched native does first
	m := gtx()
	m.Constraints = layout.Constraints{Max: image.Pt(472, 400)}
	dlg.layoutBody(m.Disabled(), nil)
	for i := 0; i < 3; i++ {
		g := gtx()
		h.last().Layout(g)
		r.Frame(ops)
	}
	// the Text field holds the selection, so the caret goes to Link
	if !r.Source().Focused(&dlg.link) {
		t.Fatalf("the Link field doesn't have the focus after a measure pass (Text focused %v)", r.Source().Focused(&dlg.text))
	}
}
