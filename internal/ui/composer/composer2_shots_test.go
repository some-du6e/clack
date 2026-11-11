package composer

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// forgetDraft drops ch's in-memory draft: its text and its attachments
// (attStore keeps those across composers, with the first one's host).
func forgetDraft(ch string) {
	clearDraft(ch, "")
	attStore.Lock()
	delete(attStore.m, draftKey(ch, ""))
	attStore.Unlock()
}

// c2ShotHost is the GIF test host with a real image cache (the fake's
// images; the attached files' own bytes through localimg).
type c2ShotHost struct {
	*gifTestHost
	img *imgcache.Cache
}

func (h c2ShotHost) Images() *imgcache.Cache { return h.img }

// TestComposer2Shots draws the composer-2 branch's ports, light and dark
// at 2x, into COMPOSER_AUDIT_SHOTS_DIR (the composer-audit-shots
// workflow; skipped without it): the draft's GIF tiles after a file, the
// + menu's GIF row, Edit file details and the pending file viewer, the
// snippet dialog's Title counter, share warning and conversation picker,
// a message before and after Mod+Shift+F, and the floating bar past the
// composer's right edge. Synthetic data and internal/fakeslack only.
func TestComposer2Shots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	app, fs := fakeslackApp(t)
	app.SetTeamPrefs(map[string]json.RawMessage{"allow_native_gif_picker": json.RawMessage("true")})
	images := imgcache.New(fs.Fetch, "", nil)
	stubTyping(t)
	ch := fs.ChannelIDs()[2]
	app.S.PutConv(slack.Conversation{ID: ch, Name: "design-crit", IsChannel: true, IsMember: true})
	clearDraft(ch, "")
	t.Cleanup(func() { clearDraft(ch, "") })
	const scale = 2
	save := func(t *testing.T, name string, w, h int, ops *op.Ops) {
		img := pixeltest.Render(t, w, h, ops)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s.png", name)
	}
	frame := func(th *theme.Theme, ops *op.Ops, r *input.Router, w, h int, draw func(gtx layout.Context)) {
		ops.Reset()
		paint.FillShape(ops, th.C.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
			Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
		if r != nil {
			gtx.Source = r.Source()
		}
		slackc.Frame(gtx)
		draw(gtx)
		if r != nil {
			r.Frame(ops)
		}
	}
	overlay := func(t *testing.T, th *theme.Theme, name string, o popups.Overlay, focus any) {
		w, h := 1100*scale, 760*scale
		var ops op.Ops
		var r input.Router
		for i := 0; i < 6; i++ {
			frame(th, &ops, &r, w, h, func(gtx layout.Context) {
				if focus != nil && i == 1 {
					gtx.Execute(key.FocusCmd{Tag: focus})
				}
				o.Layout(gtx)
			})
			time.Sleep(60 * time.Millisecond) // the image cache decodes off the frame
		}
		save(t, name, w, h, &ops)
	}
	composerShot := func(t *testing.T, th *theme.Theme, name string, c *Composer, w, h int, setup func(i int)) {
		var ops op.Ops
		var r input.Router
		for i := 0; i < 5; i++ {
			if setup != nil {
				setup(i)
			}
			time.Sleep(40 * time.Millisecond)
			frame(th, &ops, &r, w*scale, h*scale, func(gtx layout.Context) {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w*scale, h*scale)}
				off := op.Offset(image.Pt(0, (h-200)*scale)).Push(gtx.Ops)
				c.Layout(gtx)
				off.Pop()
			})
		}
		save(t, name, w*scale, h*scale, &ops)
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		th := theme.New(dark)
		gh := &gifTestHost{ovHost: &ovHost{slickHost: &slickHost{app: app, th: th}}}
		h := c2ShotHost{gifTestHost: gh, img: images}

		t.Run("gif-tiles-"+mode, func(t *testing.T) {
			forgetDraft(ch) // no draft (files, GIFs) left by the shot before
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			c.att.Add("crit.png", imgSynthPNG(t, 120, 80))
			c.att.addGif(testGif("a").Selected(true))
			c.att.addGif(testGif("b").Selected(true))
			composerShot(t, th, "c2-gif-tiles-"+mode, c, 760, 300, nil)
		})
		t.Run("plus-menu-gif-"+mode, func(t *testing.T) {
			forgetDraft(ch) // no draft (files, GIFs) left by the shot before
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			c.att.Menu(image.Rect(40*scale, 600*scale, 68*scale, 628*scale), c.plusMenu())
			overlay(t, th, "c2-plus-menu-gif-"+mode, h.last(), nil)
		})
		t.Run("edit-file-"+mode, func(t *testing.T) {
			forgetDraft(ch) // no draft (files, GIFs) left by the shot before
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			c.att.Add("aerial.png", imgSynthPNG(t, 320, 200))
			chip := firstChip(t, c)
			d := c.att.openEditFile([]*attChip{chip}, 0, editFileOpts{entry: "media_viewer_button"})
			d.alts[0].SetText("Aerial view of a synthetic coastline")
			overlay(t, th, "c2-edit-file-"+mode, d.m, nil)
		})
		t.Run("edit-alt-text-"+mode, func(t *testing.T) {
			forgetDraft(ch) // no draft (files, GIFs) left by the shot before
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			c.att.Add("one.png", imgSynthPNG(t, 200, 200))
			c.att.Add("two.png", imgSynthPNG(t, 200, 120))
			firstChip(t, c)
			d := c.att.openEditFile(c.att.imageChips(), 0, editFileOpts{altOnly: true, submitLabel: "Send", entry: "alt_text_speedbump"})
			overlay(t, th, "c2-edit-alt-text-"+mode, d.m, &d.alts[0])
		})
		t.Run("pending-viewer-"+mode, func(t *testing.T) {
			forgetDraft(ch) // no draft (files, GIFs) left by the shot before
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			c.att.Add("crit.png", imgSynthPNG(t, 120, 80))
			chip := firstChip(t, c)
			c.att.openPreview(chip)
			overlay(t, th, "c2-pending-viewer-"+mode, h.last(), nil)
		})
		t.Run("snippet-counter-warning-"+mode, func(t *testing.T) {
			forgetDraft(ch) // no draft (files, GIFs) left by the shot before
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			d := openSnippetDialog(c, snippetSpec{text: "SELECT id, name\nFROM members\nWHERE team = 'hackclub' -- synthetic\n", ch: ch})
			d.title.SetText(strings.Repeat("long-title-", 20) + "query.sql") // 229 characters
			d.typ = "sql"
			d.ch = "C0SYNTHFAR"
			d.picked = core.Conv{ID: "C0SYNTHFAR", Name: "far-away", Kind: "channel", Preview: true}
			overlay(t, th, "c2-snippet-counter-warning-"+mode, d.m, &d.title)
		})
		t.Run("snippet-picker-"+mode, func(t *testing.T) {
			forgetDraft(ch) // no draft (files, GIFs) left by the shot before
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			d := openSnippetDialog(c, snippetSpec{text: "x", ch: ch})
			p := newConvPicker(d, image.Rect(300*scale, 640*scale, 560*scale, 676*scale))
			p.ed.SetText("de")
			p.query = "de"
			p.refilter()
			w, hh := 1100*scale, 760*scale
			var ops op.Ops
			var r input.Router
			for i := 0; i < 4; i++ {
				frame(th, &ops, &r, w, hh, func(gtx layout.Context) {
					d.m.Layout(gtx)
					p.Layout(gtx)
				})
			}
			save(t, "c2-snippet-picker-"+mode, w, hh, &ops)
		})
		t.Run("markdown-convert-"+mode, func(t *testing.T) {
			for _, after := range []bool{false, true} {
				forgetDraft(ch)
				c := New(h, ch, "")
				t.Cleanup(c.Close)
				d := NewDoc()
				d.Insert(0, "Ship *bold* and _italic_ with `code`\n- one item\n- another\n> a quote", Attr{})
				c.ed.SetDoc(d)
				name := fmt.Sprintf("c2-markdown-before-%s", mode)
				if after {
					c.ed.ConvertMarkdown()
					name = fmt.Sprintf("c2-markdown-after-%s", mode)
				}
				composerShot(t, th, name, c, 760, 320, nil)
			}
		})
	}
	// the floating bar near the window's right edge: the composer is a
	// narrow column at the right of a 1,400px window
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run("floating-bar-window-"+mode, func(t *testing.T) {
			th := theme.New(dark)
			h := winHost{ovHost: &ovHost{slickHost: &slickHost{app: app, th: th}}, w: 1000 * scale}
			forgetDraft(ch) // no draft (files, GIFs) left by the shot before
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			old := showFormatting
			formattingWrite.Lock()
			formattingWrite.pending, formattingWrite.on = 1, false
			formattingWrite.Unlock()
			t.Cleanup(func() {
				formattingWrite.Lock()
				formattingWrite.pending = 0
				formattingWrite.Unlock()
				showFormatting = old
			})
			d := NewDoc()
			d.Insert(0, "one two three four five six seven", Attr{})
			c.ed.SetDoc(d)
			c.Focus()
			cw := 420 * scale
			c.Origin = image.Pt(1000*scale-cw, 0)
			var ops op.Ops
			var r input.Router
			w, hh := 1000*scale, 260*scale
			for i := 0; i < 6; i++ {
				if i == 3 {
					c.ed.SetSelection(24, 27)
				}
				frame(th, &ops, &r, w, hh, func(gtx layout.Context) {
					gtx.Constraints = layout.Constraints{Max: image.Pt(cw, hh)}
					off := op.Offset(image.Pt(c.Origin.X, 110*scale)).Push(gtx.Ops)
					c.Layout(gtx)
					off.Pop()
				})
			}
			save(t, "c2-floating-bar-window-"+mode, w, hh, &ops)
		})
	}
}
