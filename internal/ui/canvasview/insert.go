package canvasview

// The insertion toolbar's inserts that go beyond text (docs/CANVASES.md):
// each is one canvases.edit with canvas markdown, placed where Quip
// places a new section: after the section being edited (Quip's
// createNewSection with the focused section as its base, which it
// replaces when that section is an empty line), else at the end.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/composer"
)

// chooseFile asks the user for a file (tests replace it).
var chooseFile = composer.ChooseFile

// insertPoint ends the session (saving it) and returns the section to
// insert after: the one being edited, or the one before it when the save
// deleted it (it was emptied), or the document's last; "" for the end of
// an empty document. ok is false when the save was refused.
func (e *editState) insertPoint(v *View) (after string, ok bool) {
	cur, prev := "", ""
	if s := e.cur; s != nil {
		cur = s.id
		if s.target == nil {
			cur = s.afterID
		}
		for _, b := range v.blocks {
			if b.b.ID == cur {
				break
			}
			if b.b.ID != "" {
				prev = b.b.ID
			}
		}
	}
	if !e.finish(v) {
		return "", false
	}
	if cur != "" {
		if v.findBlock(cur) != nil {
			return cur, true
		}
		if prev != "" { // the edit emptied its block and deleted it
			return prev, true
		}
	}
	for i := len(v.blocks) - 1; i >= 0; i-- {
		if v.blocks[i].b.ID != "" {
			return v.blocks[i].b.ID, true
		}
	}
	return "", true
}

// findBlock finds a section by id at any depth.
func (v *View) findBlock(id string) *blk {
	var found *blk
	var walk func(bs []*blk)
	walk = func(bs []*blk) {
		for _, b := range bs {
			if found != nil {
				return
			}
			if b.b != nil && b.b.ID == id {
				found = b
				return
			}
			walk(b.kids)
			for _, c := range b.cols {
				walk(c)
			}
		}
	}
	walk(v.blocks)
	return found
}

func insertChange(after, md string) core.CanvasChange {
	if after == "" {
		return core.CanvasChange{Operation: "insert_at_end", Markdown: md}
	}
	return core.CanvasChange{Operation: "insert_after", SectionID: after, Markdown: md}
}

// insertBlocks inserts md (shown at once as local) at the insert point.
func (e *editState) insertBlocks(v *View, md string, local []canvas.Block) {
	after, ok := e.insertPoint(v)
	if !ok {
		return
	}
	ch := insertChange(after, md)
	e.insertLocal(v, after, local)
	e.save(v, func(ctx context.Context, a *core.App) error { return a.EditCanvas(ctx, v.id, ch) })
	v.h.Invalidate()
}

// newTable is Quip's new table (TableLayout.initializeContent:
// insertRows({count: 3}), insertColumns({count: 2})), empty and without a
// header row.
func newTable() canvas.Block {
	rows := make([][]canvas.Cell, 3)
	for i := range rows {
		rows[i] = []canvas.Cell{{Blocks: []canvas.Block{{Kind: canvas.Paragraph}}}, {Blocks: []canvas.Block{{Kind: canvas.Paragraph}}}}
	}
	return canvas.Block{Kind: canvas.Table, Rows: rows}
}

// insertTable is Insert table (FORMAT TABLE): Quip's new 3×2 table as a
// canvas markdown table (a headerless table is written with an empty
// header row, which canvas markdown reads as no header).
func (e *editState) insertTable(v *View) {
	t := newTable()
	e.insertBlocks(v, canvas.Markdown(t), []canvas.Block{t})
}

// insertProfile is Insert > Profile: a new line after the insert point
// with "@" typed, so the mention picker opens; the person picked saves as
// "![](@U…)" alone in its block, which Slack unfurls as their profile
// card.
func (e *editState) insertProfile(v *View) {
	after, ok := e.insertPoint(v)
	if !ok {
		return
	}
	v.startNewAfter(after)
	if s := e.cur; s != nil {
		s.ed.InsertText("@")
	}
}

// insertQuote is Insert > Blockquote (canvas_blockquote_layout's
// BLOCKQUOTE_LAYOUT, Quip's insertSection("blockquote-layout")): a new
// quote line after the insert point, saved as canvas markdown's "> ".
func (e *editState) insertQuote(v *View) {
	after, ok := e.insertPoint(v)
	if !ok {
		return
	}
	v.startNewAfter(after)
	if s := e.cur; s != nil && s.ed.ActiveBlock() != composer.Quote {
		s.ed.ToggleBlock(composer.Quote)
	}
}

// startNewAfter begins typing a new paragraph after section after ("" or
// unknown: after the document's last block).
func (v *View) startNewAfter(after string) {
	e := v.edit
	if e == nil || !e.canEdit(v) || !e.finish(v) {
		return
	}
	i := len(v.blocks)
	for j, b := range v.blocks {
		if after != "" && b.b.ID == after {
			i = j + 1
		}
	}
	if i == len(v.blocks) {
		after = ""
		for j := len(v.blocks) - 1; j >= 0; j-- {
			if v.blocks[j].b.ID != "" {
				after = v.blocks[j].b.ID
				break
			}
		}
	}
	place := &blk{b: &canvas.Block{Kind: canvas.Paragraph}}
	out := append([]*blk(nil), v.blocks[:i]...)
	out = append(out, place)
	v.blocks = append(out, v.blocks[i:]...)
	v.list.ScrollTo(i + 1) // the header row comes first
	s := &session{afterID: after, place: place, kind: canvas.Paragraph, ed: composer.NewEditor(), focus: true}
	s.ed.SetDoc(composer.NewDoc())
	e.cur = s
	v.h.Invalidate()
}

// insertUpload is Insert > Image / Insert file: the file the user picks
// is uploaded the documented way (files.getUploadURL, the bytes,
// files.completeUpload; no channel, so it isn't shared anywhere), then
// placed with its permalink: "![name](permalink)" for an image, as
// canvases.edit takes images, and "![](permalink)" for any other file, a
// file unfurl. The canvas reloads once Slack has it.
func (e *editState) insertUpload(v *View, image bool) {
	a := v.h.Core()
	if a == nil {
		return
	}
	after, ok := e.insertPoint(v)
	if !ok {
		return
	}
	e.saving++
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err := func() error {
			name, data, err := chooseFile(ctx)
			if err != nil {
				return err
			}
			if image && !strings.HasPrefix(http.DetectContentType(data), "image/") {
				return errors.New(name + " isn't an image")
			}
			pu, err := a.PrepareUpload(ctx, core.UploadFile{Name: name, Title: name, Data: data}, nil)
			if err != nil {
				return err
			}
			if pu.File == nil || pu.File.Permalink == "" {
				return errors.New("Slack didn't return the file's link")
			}
			md := "![](" + pu.File.Permalink + ")"
			if image {
				md = "![" + mdEscape(name) + "](" + pu.File.Permalink + ")"
			}
			return a.EditCanvas(ctx, v.id, insertChange(after, md))
		}()
		v.post(func() {
			e.saving--
			switch {
			case errors.Is(err, composer.ErrPickCancelled):
			case err != nil:
				v.h.Toast("Couldn't add the file to the canvas: " + friendly(err))
			default:
				v.load(true)
			}
		})
	}()
}

// mdEscape escapes an image's alt text for canvas markdown.
func mdEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`)
	return r.Replace(s)
}
