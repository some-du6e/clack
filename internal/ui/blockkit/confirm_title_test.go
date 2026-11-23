package blockkit

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// confirmHost keeps the dialogs showConfirm opens.
type confirmHost struct {
	Host
	th    *theme.Theme
	shown []popups.Overlay
}

func (h *confirmHost) Theme() *theme.Theme          { return h.th }
func (h *confirmHost) Core() *core.App              { return nil }
func (h *confirmHost) Images() *imgcache.Cache      { return nil }
func (h *confirmHost) Invalidate()                  {}
func (h *confirmHost) ShowOverlay(o popups.Overlay) { h.shown = append(h.shown, o) }

// TestConfirmObjectTitle: a confirm object's dialog (a Slackbot prompt's
// "Add" in Karim's private channel, alpha.157) is Slack's
// AttachmentActionConfirmDialog, a DeprecatedDialog: 520px wide
// (.c-dialog__content 32.5rem), a 68px header (padding 16px 24px), and,
// with no truncateTitle, a long title kept on one line and cut with an
// ellipsis. Its buttons are the confirm's deny and confirm texts; a
// confirm with no title has none ("Are you sure?" is the body's default).
func TestConfirmObjectTitle(t *testing.T) {
	h := &confirmHost{th: theme.New(false)}
	var labels []string
	old := modalShown
	modalShown = func(_ popups.Overlay, bs []*popups.ModalButton) {
		for _, b := range bs {
			labels = append(labels, b.Label)
		}
	}
	t.Cleanup(func() { modalShown = old })
	long := "Are you sure you want to add these people to #a-very-long-private-channel-name?"
	showConfirm(h, &Confirm{
		Title:   &Text{Type: "plain_text", Text: long},
		Text:    &Text{Type: "mrkdwn", Text: "New members will be able to see all of the channel’s history, including any files that have been shared in the channel."},
		Confirm: &Text{Type: "plain_text", Text: "Add"},
		Deny:    &Text{Type: "plain_text", Text: "Cancel"},
	}, func() {}, nil)
	if len(h.shown) != 1 {
		t.Fatalf("showConfirm showed %d overlays, want 1", len(h.shown))
	}
	m := h.shown[0].(*popups.Modal)
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800))}
	m.Layout(gtx)
	if w := m.Rect().Dx(); w != 520 {
		t.Errorf("the dialog is %dpx wide, want .c-dialog__content's 520px", w)
	}
	if bar := m.TitleBar().Dy(); bar != 68 {
		t.Errorf("the header is %dpx, want .c-dialog__header's 68px (16px + the 36px close button + 16px)", bar)
	}
	if lines := m.TitleLines(); len(lines) != 1 || lines[0] != long {
		t.Errorf("the title drew %q, want it whole on one line (cut there with an ellipsis)", lines)
	}
	if len(labels) != 2 || labels[0] != "Cancel" || labels[1] != "Add" {
		t.Errorf("buttons %q, want the deny then the confirm text: [Cancel Add]", labels)
	}

	labels = nil
	showConfirm(h, &Confirm{Text: &Text{Type: "mrkdwn", Text: "Sure?"}}, func() {}, nil)
	m = h.shown[1].(*popups.Modal)
	if m.Title() != "" {
		t.Errorf("a confirm with no title is titled %q; AttachmentActionConfirmDialog's title has no default", m.Title())
	}
	if len(labels) != 2 || labels[0] != "Cancel" || labels[1] != "OK" {
		t.Errorf("default buttons %q, want openConfirm's [Cancel OK]", labels)
	}
}

// TestConfirmObjectTitleShots saves the confirm dialog Karim hit
// (alpha.157: a private channel's add prompt), light and dark, under
// DIALOG_KEYS_SHOTS_DIR (dialog-keys.yml): 520px, its long title on one
// line with an ellipsis, as Slack's AttachmentActionConfirmDialog draws it.
func TestConfirmObjectTitleShots(t *testing.T) {
	dir := os.Getenv("DIALOG_KEYS_SHOTS_DIR")
	if dir == "" {
		t.Skip("DIALOG_KEYS_SHOTS_DIR unset (dialog-keys.yml sets it)")
	}
	const w, hgt = 900, 560
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		h := &confirmHost{th: theme.New(dark)}
		showConfirm(h, &Confirm{
			Title:   &Text{Type: "plain_text", Text: "Are you sure you want to add these people to #a-very-long-private-channel-name?"},
			Text:    &Text{Type: "mrkdwn", Text: "New members will be able to see all of the channel’s history, including any files that have been shared in the channel."},
			Confirm: &Text{Type: "plain_text", Text: "Add"},
			Deny:    &Text{Type: "plain_text", Text: "Cancel"},
		}, func() {}, nil)
		m := h.shown[len(h.shown)-1].(*popups.Modal)
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, hgt))}
		bg := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		if dark {
			bg = color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 255}
		}
		theme.Fill(gtx, image.Rect(0, 0, w, hgt), bg)
		m.Layout(gtx)
		img := pixeltest.Render(t, w, hgt, &ops)
		f, err := os.Create(filepath.Join(dir, "confirm-object-title-"+mode+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
