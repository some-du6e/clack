package listview

import (
	"image"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// TestItemCommentsPageLikeSlack (threads-ship findings 1 and 9): an item's
// comments are its thread, read a page at a time from the root as the List
// record thread view's Comments tab reads it (ThreadsFlexpane:
// client-list-record-thread f2Yt hands the record's thread to boot-shared
// l6n6): with 40 comments the first read is the root and 28 of them, and
// scrolled to their end the pane reads the rest; the comment button says
// the thread's 40 from the start (the root's reply_count, as
// BaseListRecordDetailAddCommentButton reads it); the composer, Slack's
// input row, shows once the comments reach the thread's end, and a
// comment posted there shows. The code before drew the one page core's
// Replies read, said "28 Comments", and a comment posted went into a
// slice of its own and never showed. Synthetic data only.
func TestItemCommentsPageLikeSlack(t *testing.T) {
	h, fs := newEnv(t, false)
	const rec = "Rec0ITEM004" // "Ship the beta": no comments in the fixture
	us := fs.UserIDs()
	cs := fs.AddListRecordComments(fakeslack.ListKitchen, rec, 40, us[1], us[2])
	if len(cs) != 40 {
		t.Fatalf("the fake added %d comments", len(cs))
	}
	it := NewItem(h, fakeslack.ListKitchen, rec, Actions{Close: func() {}})
	s := newShooter(t, image.Pt(480, 900))
	draw := func(gtx layout.Context) { it.Layout(gtx) }
	comments := func() []core.Message {
		it.mu.Lock()
		defer it.mu.Unlock()
		return append([]core.Message(nil), it.comments...)
	}
	has := func(ts string) bool {
		for _, m := range comments() {
			if m.TS == ts {
				return true
			}
		}
		return false
	}
	settle(t, s, draw, "the item's first comments", func() bool { return has(cs[0]) })
	if l := itemCommentsLabel(it); l != "40 Comments" {
		t.Errorf("with the first page in, the comment button says %q, want the thread's \"40 Comments\"", l)
	}
	// scrolled to the comments' end: the pages after the first come in
	toEnd := func(gtx layout.Context) {
		it.scroll = it.maxScroll
		it.Layout(gtx)
	}
	settle(t, s, toEnd, "all 40 comments", func() bool {
		for _, ts := range cs {
			if !has(ts) {
				return false
			}
		}
		return true
	})
	settle(t, s, toEnd, "the composer at the thread's end", func() bool { return itemComposerShown(it) })
	const text = "synthetic: comment 41"
	it.composer.SetText(text)
	it.send()
	settle(t, s, toEnd, "the comment posted", func() bool {
		for _, m := range comments() {
			if m.Text == text {
				return true
			}
		}
		return false
	})
}
