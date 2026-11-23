package messages

// The hover toolbar follows its message, not a row index (hover-toolbar-
// pos's hover by key): Slack's MessageActions is inside the message's row,
// so with the pointer resting on the toolbar, a message above it that goes
// (deleted) leaves the toolbar over the same message, now one row up, and
// a row the list didn't hover never takes it. Synthetic data.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/uipref"
)

func TestToolbarFollowsItsMessageLikeSlack(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Reset()
	e := newCtxEnv(t)
	s := e.v.s
	now := e.now.Add(time.Second)
	e.moveTo(rowPoint(t, s, tsCtxFile), now)
	e.frameAt(now.Add(20 * time.Millisecond))
	if s.tbKey != tsCtxFile {
		t.Fatalf("the toolbar is over %q, not the hovered %q", s.tbKey, tsCtxFile)
	}
	b := toolbarButtonAt(t, s, tbAddReact)
	now = e.now.Add(time.Second)
	e.moveTo(b, now) // resting on its button
	e.frameAt(now.Add(20 * time.Millisecond))
	before := s.sel.keyIdx[tsCtxFile]
	injectEvent(e.h.app, tGen, "message", "message_deleted", map[string]any{"deleted_ts": tsCtxLink, "hidden": true})
	moved := false
	for i := 0; i < 50 && !moved; i++ {
		now = now.Add(20 * time.Millisecond)
		e.frameAt(now)
		idx, ok := s.sel.keyIdx[tsCtxFile]
		moved = ok && idx != before
		if !moved {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !moved {
		t.Fatal("the message above was never taken out: the case tests nothing")
	}
	if s.tbFor >= 0 && s.tbKey != tsCtxFile {
		t.Errorf("after the row above went, the toolbar is over %q (row %d); Slack's stays in %q's row", s.tbKey, s.tbFor, tsCtxFile)
	}
	if s.tbFor >= 0 && s.tbFor != s.sel.keyIdx[tsCtxFile] {
		t.Errorf("the toolbar is drawn for row %d, %q is row %d now", s.tbFor, tsCtxFile, s.sel.keyIdx[tsCtxFile])
	}
}
