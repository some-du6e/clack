package messages

import "github.com/some-du6e/clack/internal/richtext"

// RowText is the text the thread's row for ts draws now (its message as
// plugins present it), and whether that row was laid out; Edited reports
// whether it draws "(edited)".
func (v *ThreadView) RowText(ts string) (text string, edited, ok bool) { return v.s.rowText(ts) }

// RowText is ThreadView.RowText for a channel's list.
func (v *ChannelView) RowText(ts string) (text string, edited, ok bool) { return v.s.rowText(ts) }

func (s *listState) rowText(ts string) (text string, edited, ok bool) {
	r := s.rows[ts]
	if r == nil || r.rv == nil {
		return "", false, false
	}
	return richtext.PlainText(r.m.Doc), r.rv.Options().Edited, true
}
