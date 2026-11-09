package composer

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/refocus"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// keysHost records the overlays the dialog shows.
type keysHost struct {
	*slickHost
	shown []popups.Overlay
}

func (h *keysHost) ShowOverlay(o popups.Overlay) { h.shown = append(h.shown, o) }

// "Notify N people?" through Gio's router, with a real composer editor
// under it: "Edit Message" has the focus and the ring when the dialog
// opens (MessagePaneInputBroadcastWarning's autoFocus), so Enter or Space
// goes back to editing; Tab reaches Send; Escape cancels; every way out
// gives the composer its focus back (useRestoreFocus).
func TestBroadcastWarningKeys(t *testing.T) {
	refocus.Reset()
	t.Cleanup(refocus.Reset)
	app, fs := broadcastApp(t, nil)
	people(fs, 30)
	th := theme.New(false)
	h := &keysHost{slickHost: &slickHost{app: app, th: th}}
	st := DefaultEditorStyle(theme.NewShaper())
	ed := emojiEditor(nil)
	var r input.Router
	var ops op.Ops
	now := time.Now()
	focusEd := false
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1200, 900))}
		slackc.Frame(gtx)
		if focusEd {
			ed.Focus(gtx)
			focusEd = false
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(600, 40))
		ed.Layout(g, st)
		for _, o := range h.shown {
			if !o.Dismissed() {
				g := gtx
				g.Constraints = layout.Constraints{Max: gtx.Constraints.Max}
				o.Layout(g)
			}
		}
		r.Frame(&ops)
		now = now.Add(16 * time.Millisecond)
	}
	settle := func() {
		for range 4 {
			frame()
		}
	}
	press := func(n key.Name, m key.Modifiers) {
		r.Queue(key.Event{Name: n, Modifiers: m, State: key.Press})
		frame()
		r.Queue(key.Event{Name: n, Modifiers: m, State: key.Release})
		settle()
	}
	edFocused := func() bool { return r.Source().Focused(ed) }

	focusEd = true
	settle()
	if !edFocused() {
		t.Fatal("the composer editor didn't take the focus")
	}
	answers := []bool{}
	open := func() *broadcastWarning {
		t.Helper()
		var d *broadcastWarning
		onBroadcastWarning = func(w *broadcastWarning) { d = w }
		defer func() { onBroadcastWarning = nil }()
		openBroadcastWarning(h, coreAPI{app}, broadcastAsk{Channel: tChOpen, Used: []string{"channel"}}, func(ok bool) { answers = append(answers, ok) })
		if d == nil {
			t.Fatal("no dialog")
		}
		settle()
		if edFocused() {
			t.Fatal("the composer kept the focus under the dialog")
		}
		return d
	}
	focus := func(d *broadcastWarning, want string) {
		t.Helper()
		got, ring := d.modal.FocusedButton(layout.Context{Source: r.Source()})
		if got != want || !ring {
			t.Fatalf("focused %q (ring %v), want %q with the ring", got, ring, want)
		}
	}
	closed := func(d *broadcastWarning, sent bool) {
		t.Helper()
		settle()
		if !d.modal.Dismissed() {
			t.Fatal("the dialog is still open")
		}
		if len(answers) == 0 || answers[len(answers)-1] != sent {
			t.Fatalf("answers %v, want the last one %v", answers, sent)
		}
		if !edFocused() {
			t.Fatal("the composer didn't get the focus back")
		}
	}

	d := open()
	focus(d, "Edit Message")
	press(key.NameReturn, 0)
	closed(d, false)

	d = open()
	press(key.NameSpace, 0)
	closed(d, false)

	d = open()
	press(key.NameTab, 0)
	focus(d, "Send")
	press(key.NameTab, 0)
	focus(d, "Close")
	press(key.NameTab, key.ModShift)
	focus(d, "Send")
	press(key.NameReturn, 0)
	closed(d, true)

	d = open()
	press(key.NameEscape, 0)
	closed(d, false)
}
