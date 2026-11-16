package inputidle

import (
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
)

// Since follows what a Gio router is queued, wherever it goes.
func TestSince(t *testing.T) {
	var r input.Router
	time.Sleep(30 * time.Millisecond)
	if d := Since(); d < 25*time.Millisecond {
		t.Fatalf("input %v ago with none queued", d)
	}
	r.Queue(key.Event{Name: key.NameDownArrow, State: key.Press})
	if d := Since(); d >= 25*time.Millisecond {
		t.Fatalf("a key queued, but the last input is %v ago", d)
	}
}
