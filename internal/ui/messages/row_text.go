package messages

import "github.com/some-du6e/clack/internal/richtext"

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
