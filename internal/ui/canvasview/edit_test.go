package canvasview

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/canvas"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
)

func findBlk(v *View, f func(b *canvas.Block) bool) *blk {
	for _, b := range v.blocks {
		if f(b.b) {
			return b
		}
	}
	return nil
}

func fakeBlock(t *testing.T, fs *fakeslack.Server, id string) *canvas.Block {
	t.Helper()
	fc, _ := fs.Canvas(fakeslack.CanvasChannel)
	return fc.Doc.Find(id)
}

func lastChange(t *testing.T, fs *fakeslack.Server) map[string]any {
	t.Helper()
	es := fs.CanvasEdits()
	if len(es) == 0 {
		t.Fatal("no canvases.edit call")
	}
	var cs []map[string]any
	if err := json.Unmarshal([]byte(es[len(es)-1].Get("changes")), &cs); err != nil || len(cs) != 1 {
		t.Fatalf("changes %s", es[len(es)-1].Get("changes"))
	}
	return cs[0]
}

// Edit a paragraph, type with markdown, tick a checklist item, add list
// items with Enter, insert a channel mention, rename: each is one
// canvases.edit the fake applies, and the view shows the result.
func TestEditRoundTrip(t *testing.T) {
	h, fs := newEnv(t, false)
	v := New(h, fakeslack.CanvasChannel, Full, Actions{})
	s := newShooter(t, image.Pt(1100, 900))
	frame := func() { s.frame(func(gtx layout.Context) { v.Layout(gtx) }) }
	settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil })
	frame()
	if !v.edit.canEdit(v) {
		t.Fatal("fixture canvas not editable")
	}
	edits := func() int { return len(fs.CanvasEdits()) }

	// 1. a paragraph: replace its text (bold via the toolbar's style)
	p := findBlk(v, func(b *canvas.Block) bool { return b.Kind == canvas.Paragraph && strings.HasPrefix(b.Text(), "A ") })
	if p == nil {
		t.Fatal("no paragraph")
	}
	pid := p.b.ID
	v.edit.start(v, p)
	frame()
	ed := v.edit.cur.ed
	ed.SetSelection(0, ed.Doc().Len())
	ed.InsertText("Plain then ")
	ed.ToggleStyle(composer.Bold)
	ed.InsertText("bold")
	frame()
	s.shot("canvas-editing", s.frame(func(gtx layout.Context) { v.Layout(gtx) }))
	v.edit.finish(v)
	settle(t, s, v, "the paragraph edit", func() bool {
		return edits() == 1 && fakeBlock(t, fs, pid) != nil && fakeBlock(t, fs, pid).Text() == "Plain then bold"
	})
	if c := lastChange(t, fs); c["operation"] != "replace" || c["section_id"] != pid ||
		c["document_content"].(map[string]any)["markdown"] != "Plain then **bold**" {
		t.Errorf("change %v", c)
	}
	if b := findBlk(v, func(b *canvas.Block) bool { return b.ID == pid }); b == nil || b.b.Text() != "Plain then bold" {
		t.Error("view didn't update optimistically")
	}

	// 2. tick a checklist item
	ck := findBlk(v, func(b *canvas.Block) bool { return b.Kind == canvas.Check && !b.Checked })
	cid := ck.b.ID
	v.edit.toggleCheck(v, ck)
	settle(t, s, v, "the tick", func() bool { b := fakeBlock(t, fs, cid); return b != nil && b.Checked })

	// 3. a bullet: Enter adds a second item in the same edit
	bl := findBlk(v, func(b *canvas.Block) bool { return b.Kind == canvas.Bullet && b.Text() == "Back to top" })
	v.edit.start(v, bl)
	frame()
	ed = v.edit.cur.ed
	ed.SetSelection(ed.Doc().Len(), ed.Doc().Len())
	ed.InsertText("\nAnother item")
	frame()
	v.edit.finish(v)
	settle(t, s, v, "the new list item", func() bool { return edits() == 3 })
	if md := lastChange(t, fs)["document_content"].(map[string]any)["markdown"]; md != "- Back to top\n- Another item" {
		t.Errorf("two items: %q", md)
	}
	settle(t, s, v, "the fake's list", func() bool {
		fc, _ := fs.Canvas(fakeslack.CanvasChannel)
		for i, b := range fc.Doc.Blocks {
			if b.Text() == "Back to top" && i+1 < len(fc.Doc.Blocks) && fc.Doc.Blocks[i+1].Kind == canvas.Bullet && fc.Doc.Blocks[i+1].Text() == "Another item" {
				return true
			}
		}
		return false
	})

	// 4. markdown shortcut: a new block typed "## " becomes a heading
	v.StartNewBlock()
	frame()
	v.edit.cur.ed.InsertText("## Next steps")
	for i := 0; i < 3; i++ {
		frame()
	}
	if v.edit.cur.kind != canvas.Heading2 {
		t.Fatalf("shortcut kind %v", v.edit.cur.kind)
	}
	v.edit.finish(v)
	settle(t, s, v, "the heading insert", func() bool { return edits() == 4 })
	if c := lastChange(t, fs); c["operation"] != "insert_after" || c["document_content"].(map[string]any)["markdown"] != "## Next steps" {
		t.Errorf("heading change %v", c)
	}
	settle(t, s, v, "the fake's heading", func() bool {
		fc, _ := fs.Canvas(fakeslack.CanvasChannel)
		b := fc.Doc.Blocks[len(fc.Doc.Blocks)-1]
		return b.Kind == canvas.Heading2 && b.Text() == "Next steps"
	})

	// 5. "[] " makes a checklist; "#chan" picks a channel mention
	v.StartNewBlock()
	frame()
	ed = v.edit.cur.ed
	ed.InsertText("[] Ask ")
	frame()
	if v.edit.cur.kind != canvas.Check {
		t.Fatalf("checklist shortcut kind %v", v.edit.cur.kind)
	}
	snap := h.a.Snapshot()
	var chName, chID string
	for _, c := range snap.Convs {
		if c.Kind == "channel" {
			chName, chID = c.Name, c.ID
			break
		}
	}
	ed.InsertText("#" + chName[:3])
	settle(t, s, v, "channel suggestions", func() bool { return v.edit.pick.active() })
	var pickIt pickItem
	for _, it := range v.edit.pick.items {
		if it.id == chID {
			pickIt = it
		}
	}
	if pickIt.id == "" {
		pickIt = v.edit.pick.items[0]
		chID = pickIt.id
	}
	v.edit.pick.insert(v, v.edit.cur, pickIt)
	frame()
	v.edit.finish(v)
	settle(t, s, v, "the checklist insert", func() bool { return edits() == 5 })
	if md, _ := lastChange(t, fs)["document_content"].(map[string]any)["markdown"].(string); md != "- [ ] Ask ![](#"+chID+")" {
		t.Errorf("checklist + mention markdown %q", md)
	}

	// 6. emptying a block deletes it
	q := findBlk(v, func(b *canvas.Block) bool { return b.Kind == canvas.Heading3 })
	qid := q.b.ID
	v.edit.start(v, q)
	frame()
	ed = v.edit.cur.ed
	ed.SetSelection(0, ed.Doc().Len())
	ed.InsertText("")
	v.edit.finish(v)
	settle(t, s, v, "the delete", func() bool { return edits() == 6 && fakeBlock(t, fs, qid) == nil })

	// 7. rename from the title
	v.startRename()
	frame()
	v.edit.rename.SetText("Renamed plan")
	v.edit.commitRename(v)
	settle(t, s, v, "the rename", func() bool { fc, _ := fs.Canvas(fakeslack.CanvasChannel); return fc.Title == "Renamed plan" })
	if v.Title() != "Renamed plan" {
		t.Errorf("view title %q", v.Title())
	}
	// unchanged text sends nothing
	n := edits()
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == pid }))
	frame()
	v.edit.finish(v)
	time.Sleep(100 * time.Millisecond)
	if edits() != n {
		t.Error("an unchanged block was saved")
	}
}

// Block <-> editor conversions keep styles, links and mentions.
func TestEditorConversions(t *testing.T) {
	b := &canvas.Block{Kind: canvas.Paragraph, Spans: []richtext.Span{
		{Kind: richtext.Text, Text: "a "}, {Kind: richtext.Text, Text: "b", Style: richtext.Bold | richtext.Italic},
		{Kind: richtext.Link, Text: " link", URL: "https://example.com"}, {Kind: richtext.User, ID: "U1", Text: "@x"},
		{Kind: richtext.Emoji, ID: "tada"}}}
	d := toDoc(b)
	out := fromDoc(d, &session{kind: canvas.Paragraph})
	if len(out) != 1 || canvas.Markdown(out[0]) != canvas.Markdown(*b) {
		t.Fatalf("round trip %q vs %q", canvas.Markdown(out[0]), canvas.Markdown(*b))
	}
	h := &canvas.Block{Kind: canvas.Heading1, Spans: []richtext.Span{{Kind: richtext.Text, Text: "T", Style: richtext.Bold}}}
	if got := blocksMarkdown(fromDoc(toDoc(h), &session{kind: canvas.Heading1})); got != "# T" {
		t.Errorf("heading %q", got)
	}
	code := &canvas.Block{Kind: canvas.Code, Code: "a\n  b"}
	if got := blocksMarkdown(fromDoc(toDoc(code), &session{kind: canvas.Code})); got != "```\na\n  b\n```" {
		t.Errorf("code %q", got)
	}
}
