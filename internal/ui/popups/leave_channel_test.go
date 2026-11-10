package popups

import (
	"testing"

	"gioui.org/io/key"
)

// MaybeLeaveChannel (the channel browser's Leave): a public channel is
// left at once; a private one asks with LeaveChannel and leaves on
// "Leave Channel" (its autoFocus: Enter).
func TestMaybeLeaveChannel(t *testing.T) {
	h := hcEnv(t)
	e := newMdEnv(t, h)
	left := 0
	MaybeLeaveChannel(h, "lounge", "channel", false, func() { left++ })
	e.settle()
	if left != 1 || e.top() != nil {
		t.Fatalf("public: left %d, modal %v", left, e.top() != nil)
	}
	m := e.open(func() { MaybeLeaveChannel(h, "plans", "private", false, func() { left++ }) })
	if m.o.Title != "Leave plans" || left != 1 {
		t.Fatalf("private: title %q, left %d before confirming", m.o.Title, left)
	}
	e.focus("Leave Channel", true)
	e.press(key.NameReturn, 0)
	if left != 2 {
		t.Errorf("Leave Channel didn't leave (%d)", left)
	}
}
