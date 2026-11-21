package popups

import (
	"image"
	"slices"
	"testing"
)

// rfHost records the comment row's setFocusInput and navigation, in order.
type rfHost struct {
	*tzHost
	calls []string
}

func (h *rfHost) OpenThread(ch, ts string)          { h.calls = append(h.calls, "open "+ch+"/"+ts) }
func (h *rfHost) SetThreadFocusInput(ch, ts string) { h.calls = append(h.calls, "focus "+ch+"/"+ts) }
func (h *rfHost) ClearThreadFocusInput()            { h.calls = append(h.calls, "clear") }

var _ ThreadFocusInput = (*rfHost)(nil)

// The context menu's Reply in thread row is Slack's comment thunk (0yz+):
// setFocusInput for channel-(threadTs || ts), with the threadTs the
// menu's container passes (none from a channel's list, AEjl; the row's
// thread from the Threads view, aQNV), then the thread opens at its root.
// So a broadcast reply's key in a channel is its own ts: its parent's box
// stays unfocused, as in Slack.
func TestMenuCommentSetsThreadFocusInput(t *testing.T) {
	base := mkHost(t)
	const ch, root, reply = "C0FAKE00001", "1700000000.000100", "1700000000.000200"
	cases := []struct {
		name    string
		ts, thr string
		replies int
		where   MessageContext
		onReply bool
		want    []string
	}{
		{"channel root", root, "", 0, InChannel, false, []string{"focus " + ch + "/" + root, "open " + ch + "/" + root}},
		{"channel root with replies", root, root, 2, InChannel, false, []string{"focus " + ch + "/" + root, "open " + ch + "/" + root}},
		{"channel broadcast reply", reply, root, 0, InChannel, false, []string{"focus " + ch + "/" + reply, "open " + ch + "/" + root}},
		{"Threads view reply", reply, root, 0, InThreadsView, false, []string{"focus " + ch + "/" + root, "open " + ch + "/" + root}},
		{"Threads view root", root, root, 2, InThreadsView, false, []string{"focus " + ch + "/" + root, "open " + ch + "/" + root}},
		{"channel root, OnReply", root, "", 0, InChannel, true, []string{"focus " + ch + "/" + root, "onReply"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &rfHost{tzHost: base}
			m := mkMsg(false)
			m.Channel, m.TS, m.ThreadTS, m.ReplyCount = ch, c.ts, c.thr, c.replies
			var o MessageMenuOpts
			if c.onReply {
				o.OnReply = func() { h.calls = append(h.calls, "onReply") }
			}
			menu := NewMessageContextMenu(h, image.Pt(200, 200), m, c.where, o)
			i := slices.IndexFunc(menu.Items(), func(it MenuItem) bool { return it.Action == "comment" })
			if i < 0 {
				t.Fatal("no Reply in thread row")
			}
			menu.Items()[i].Do()
			if !slices.Equal(h.calls, c.want) {
				t.Errorf("calls %q, want %q", h.calls, c.want)
			}
		})
	}
}
