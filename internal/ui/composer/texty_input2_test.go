package composer

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Cutting a pill and pasting it back keeps the pill (Texty reads its own
// slack/texty clipboard first, with the input's formats: slackmention and
// slackemoji), so the saved topic keeps the mention.
func TestTextyPillCutPaste(t *testing.T) {
	in := newTestTexty("Ask <@U0FAKE00003> now")
	e := in.ed
	base := in.Value()
	// what copySel keeps for the editor's own paste
	sl := e.Doc().Slice(4, 5)
	e.clipText, e.clipDoc = sl.copyText(), sl
	e.SetSelection(4, 5)
	e.deleteSel()
	e.SetSelection(e.Doc().Len(), e.Doc().Len())
	e.paste(e.clipText)
	if a := e.Doc().Attrs[e.Doc().Len()-1].Atom; a == nil || a.Kind != AtomUser || a.ID != "U0FAKE00003" {
		t.Fatalf("the pasted pill is %+v (text %q)", a, in.Text())
	}
	if got := in.Value(); got != "Ask  now<@U0FAKE00003|@U0FAKE00003>" || got == base {
		t.Errorf("value %q", got)
	}
	if got := in.Submit(); got != "Ask  now<@U0FAKE00003>" {
		t.Errorf("Save sends %q", got)
	}
}

func textyFrame(in *TextyInput, maxH int) layout.Dimensions {
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Now(),
		Constraints: layout.Constraints{Max: image.Pt(400, maxH)}}
	return in.Layout(gtx, theme.New(false), theme.New(false).C.Fg)
}

// BasicContainer's five lines (numMinLines = numMaxLines = 5): the box's
// text area is 110px tall whatever the text, and longer text scrolls.
func TestTextyFiveLines(t *testing.T) {
	for _, text := range []string{"one line", strings.Repeat("line\n", 20) + "end"} {
		in := newTestTexty(text)
		if d := textyFrame(in, 1000); d.Size.Y != 5*22 {
			t.Errorf("%q: %dpx tall, want %d", text[:8], d.Size.Y, 5*22)
		}
	}
}

// overlayHost records the overlays it is asked to show.
type overlayHost struct {
	*slickHost
	shown []popups.Overlay
}

func (h *overlayHost) ShowOverlay(o popups.Overlay) { h.shown = append(h.shown, o) }

// A paste over 50,000 characters is refused and PasteLimitDialog (Jv6G)
// opens: TextyInput's onPasteLimit without a host handler.
func TestTextyPasteLimitDialog(t *testing.T) {
	h := &overlayHost{slickHost: &slickHost{th: theme.New(false)}}
	in := NewTextyInput(h, "topic", "", 250)
	in.ed.paste(strings.Repeat("x", 50001))
	textyFrame(in, 1000)
	if in.Text() != "topic" {
		t.Errorf("the refused paste went in: %d characters", len(in.Text()))
	}
	if len(h.shown) != 1 {
		t.Fatalf("%d overlays, want the paste-limit dialog", len(h.shown))
	}
	if m, ok := h.shown[0].(*popups.Modal); !ok || m.Title() != "That’s a bit too long to paste" {
		t.Errorf("overlay %T, want PasteLimitDialog", h.shown[0])
	}
}
