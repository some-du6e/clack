package richview

import "testing"

// TestAlertFillsViews: in a modal or App Home a short alert spans the
// view: WSwB's block wrappers (wrapViewBlock, wrapAppHomeBlock) aren't
// flex, so .c-alert{display:flex} fills them, unlike a message's block
// wrapper, where it shrinks to its text (TestAlertWidth).
func TestAlertFillsViews(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"alert","text":{"type":"plain_text","text":"Pick a date"},"level":"warning"}]`)
	v := layoutAt(st, d, fillBlocksOpts(), 480)
	for _, b := range v.d.boxes {
		if b.radius == 8 && b.kind == bFill {
			if b.r.Min.X != 0 || b.r.Max.X != 480 {
				t.Errorf("alert box %v in a view, want the view's 0 to 480", b.r)
			}
			return
		}
	}
	t.Fatal("no alert box")
}
