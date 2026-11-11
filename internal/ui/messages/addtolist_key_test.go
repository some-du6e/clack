package messages

import (
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
)

// listAddHost is ctxHost with the ui root's "Add to list…" (ListAdder).
type listAddHost struct {
	*ctxHost
	added []core.Message
}

func (h *listAddHost) AddMessageToList(m core.Message) { h.added = append(h.added, m) }

// V on the keyboard-selected message is its "Add to list…" (Slack's
// add_to_list action, shortcut P2.ADD_TO_LIST: "v").
func TestAddToListShortcut(t *testing.T) {
	e := newCtxEnv(t)
	lh := &listAddHost{ctxHost: e.h}
	e.v = NewChannelView(lh, tGen)
	e.v.Preview = true
	until(t, 5*time.Second, "the rows laid out", e.frame, func() bool { return e.row(tsCtxFile) != nil })
	if !e.v.FocusNewest(func() {}) {
		t.Fatal("no message to select")
	}
	for k := 0; k < 3; k++ { // the focus frame holds that frame's events
		e.frame()
	}
	e.r.Queue(key.Event{Name: "V", State: key.Press}, key.Event{Name: "V", State: key.Release})
	e.frame()
	e.frame()
	if len(lh.added) != 1 || lh.added[0].TS != tsCtxFile {
		t.Fatalf("Add to list for %v", lh.added)
	}
}
