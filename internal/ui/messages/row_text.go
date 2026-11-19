package messages

import (
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// RowBody is the body the thread's row for ts paints (its richview), nil
// before the row was laid out (tests: richview.PaintedHook tells which
// surface painted a message).
func (v *ThreadView) RowBody(ts string) *richview.View {
	if r := v.s.rows[ts]; r != nil {
		return r.rv
	}
	return nil
}

// RowText is the text the thread's row for ts draws now (its message as
// plugins present it), and whether that row was laid out; Edited reports
// whether it draws "(edited)".
func (v *ThreadView) RowText(ts string) (text string, edited, ok bool) {
	r := v.s.rows[ts]
	if r == nil || r.rv == nil {
		return "", false, false
	}
	return richtext.PlainText(r.m.Doc), r.rv.Options().Edited, true
}
