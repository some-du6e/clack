package canvasview

// Remove mention, the canvas channel card's last row (Kda0
// getRemoveSectionMenuTemplate: getRemoveLabel says "Remove mention" for
// a user or channel object; shown when the embedded control isRemovable;
// the click is performAction({action: 1}), Quip deleting the control from
// its section). Clack edits a section as a whole (canvases.edit replace),
// so the mention goes by saving its section without it, and only a
// section that passes the fidelity check (canvas.Editable) before and
// after is offered the row.

import (
	"context"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// sectionBlk is the block of section id, at any depth.
func sectionBlk(bs []*blk, id string) *blk {
	for _, b := range bs {
		if b.b != nil && b.b.ID == id {
			return b
		}
		if k := sectionBlk(b.kids, id); k != nil {
			return k
		}
		for _, c := range b.cols {
			if k := sectionBlk(c, id); k != nil {
				return k
			}
		}
	}
	return nil
}

// withoutMention is b without its first mention of channel ch.
func withoutMention(b canvas.Block, ch string) (canvas.Block, bool) {
	for i, s := range b.Spans {
		if s.Kind == richtext.Channel && s.ID == ch {
			nb := b
			nb.Spans = append(append([]richtext.Span(nil), b.Spans[:i]...), b.Spans[i+1:]...)
			return nb, true
		}
	}
	return b, false
}

// MentionRemovable reports whether the viewer can remove section's
// mention of channel ch: an editable canvas, a top-level section (not
// in a table, a quote or a callout) Clack can save with and without it.
func (v *View) MentionRemovable(section, ch string) bool {
	if !v.edit.canEdit(v) || section == "" {
		return false
	}
	b := sectionBlk(v.blocks, section)
	if b == nil || b.inCell || b.parent != nil || !b.edOK { // a quote's or callout's paragraph saves through its quote (edit.go)
		return false
	}
	nb, ok := withoutMention(*b.b, ch)
	if !ok {
		return false
	}
	ok, _ = canvas.Editable(nb)
	return ok
}

// RemoveMention removes section's mention of channel ch (canvases.edit
// replace), as Remove mention does.
func (v *View) RemoveMention(section, ch string) {
	if !v.MentionRemovable(section, ch) {
		return
	}
	b := sectionBlk(v.blocks, section)
	nb, _ := withoutMention(*b.b, ch)
	md := canvas.Markdown(nb)
	v.edit.replaceLocal(v, b, []canvas.Block{nb})
	id, e := nb.ID, v.edit
	e.save(v, func(ctx context.Context, a *core.App) error {
		return a.EditCanvas(ctx, v.id, core.CanvasChange{Operation: "replace", SectionID: id, Markdown: md})
	})
	v.h.Invalidate()
}
