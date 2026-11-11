package composer

import (
	"encoding/json"
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// firstChip is the composer's first attached file once its eager upload
// has its Slack id.
func firstChip(t *testing.T, c *Composer) *attChip {
	t.Helper()
	var ch *attChip
	waitUntil(t, "the attached image's upload", func() bool {
		c.att.mu.Lock()
		defer c.att.mu.Unlock()
		if len(c.att.chips) == 0 || c.att.chips[0].prepared == nil {
			return false
		}
		ch = c.att.chips[0]
		return true
	})
	return ch
}

// layEditFile lays the open Edit file dialog out once (its body reads
// the fields).
func layEditFile(d *editFileDialog) {
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800))}
	d.m.Layout(gtx)
}

// The pending viewer (W9QH, isPendingView) and EditFileDialog (e4hN):
// the attached image opens in the viewer; Edit file details shows the
// name and the Description; Done sends editFileApi's files.edit {file,
// title, alt_txt, is_canvas_template: false} for the uploaded file; a
// name is required.
func TestEditFileDetailsLikeSlack(t *testing.T) {
	app, fs := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := freshComposer(t, h)
	c.att.Add("cat.png", imgSynthPNG(t, 64, 48))
	chip := firstChip(t, c)

	c.att.openPreview(chip)
	if _, ok := h.last().(*popups.Lightbox); !ok {
		t.Fatalf("the attached image opened %T, want the pending file viewer", h.last())
	}

	d := c.att.openEditFile([]*attChip{chip}, 0, editFileOpts{entry: "media_viewer_button"})
	if d == nil || h.last() != d.m {
		t.Fatal("no Edit file details dialog")
	}
	layEditFile(d)
	if d.titles[0].Text() != chip.title || !d.init[0].image {
		t.Fatalf("fields: name %q, image %v", d.titles[0].Text(), d.init[0].image)
	}
	d.titles[0].SetText("")
	if d.valid() {
		t.Error("an empty name passed validation (EditFileDialogTitle is required)")
	}
	d.titles[0].SetText("a cat.png")
	d.alts[0].SetText("A grey cat asleep on a keyboard")
	if !d.valid() || !d.submit() {
		t.Fatal("Done didn't submit")
	}
	waitUntil(t, "files.edit", func() bool { return len(fs.called("files.edit")) == 1 })
	e := fs.called("files.edit")[0]
	if e.Get("file") != chip.prepared.ID || e.Get("title") != "a cat.png" || e.Get("alt_txt") != "A grey cat asleep on a keyboard" || e.Get("is_canvas_template") != "false" {
		t.Errorf("files.edit %v", e)
	}
	c.att.mu.Lock()
	title, alt := chip.title, chip.alt
	c.att.mu.Unlock()
	if title != "a cat.png" || alt != "A grey cat asleep on a keyboard" {
		t.Errorf("chip now %q / %q", title, alt)
	}
}

// maybeOpenEditFileDialogForImages (6Lt3): with alt text reminders on, a
// send with an image lacking alt text opens "Edit alt text" with "Send";
// submitting it saves the alt text and sends the message.
func TestAltTextSpeedBumpLikeSlack(t *testing.T) {
	app, fs := syntheticApp(t, func(*store.Store) {})
	app.SetTeamPrefs(map[string]json.RawMessage{"alt_text_reminders": json.RawMessage("true")})
	stubTyping(t)
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := freshComposer(t, h)
	c.att.Add("dog.png", imgSynthPNG(t, 40, 40))
	firstChip(t, c)
	d0 := NewDoc()
	d0.Insert(0, "look", Attr{})
	c.ed.SetDoc(d0)

	c.submit()
	d := lastEditFile
	if d == nil || h.last() != d.m || !d.opts.altOnly || d.opts.submitLabel != "Send" || d.opts.entry != "alt_text_speedbump" {
		t.Fatalf("speed bump %+v", d)
	}
	if c.att.Len() != 1 || len(fs.called("files.share")) != 0 || c.ed.Doc().Text() != "look" {
		t.Fatal("the message went before the alt text")
	}
	layEditFile(d)
	d.alts[0].SetText("A dog")
	if !d.submit() {
		t.Fatal("Send didn't submit")
	}
	waitUntil(t, "the share after the speed bump", func() bool { return len(fs.called("files.share")) == 1 })
	if c.att.Len() != 0 {
		t.Error("the image stayed in the composer")
	}

	// alt text there now: no speed bump the next time
	c.att.Add("dog2.png", imgSynthPNG(t, 40, 40))
	chip := firstChip(t, c)
	c.att.setDetails(chip, "dog2.png", "Another dog", true)
	c.ed.SetDoc(d0.Clone())
	before := lastEditFile
	c.submit()
	if lastEditFile != before {
		t.Error("the speed bump opened for an image with alt text")
	}
}
