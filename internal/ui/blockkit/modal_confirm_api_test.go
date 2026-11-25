package blockkit

import "gioui.org/io/event"

// The modal confirm test's window on the speed bump
// (modal_confirm_bump_test.go); a red run on the old code swaps this
// file for one written against its API.

type mcInfo struct {
	header, body, keepTxt, cont, style string
	mrkdwn                             bool
}

func mcBump(s *Stack) (mcInfo, bool) {
	b := s.bump
	if b == nil {
		return mcInfo{}, false
	}
	return mcInfo{b.header, b.body, b.keepTxt, b.cont, b.style, b.bodyView != nil}, true
}

func mcKeep(s *Stack) event.Tag {
	if s.bump == nil {
		return nil
	}
	return &s.bump.keep
}

func mcConfirmIn(sf *Surface, c *Confirm, ok, deny func()) { sf.confirmIn(c, ok, deny) }
