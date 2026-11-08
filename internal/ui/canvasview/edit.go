package canvasview

import (
	"image"

	"gioui.org/layout"
)

// editState is the editing session (stage 3). A nil *editState is a
// read-only view: every method is safe on nil.
type editState struct {
	renaming bool
}

func (e *editState) events(gtx layout.Context, v *View)                       {}
func (e *editState) canEdit(v *View) bool                                     { return false }
func (e *editState) titleArea(gtx layout.Context, v *View, r image.Rectangle) {}
func (e *editState) layoutRename(gtx layout.Context, v *View, p image.Point, w int) int {
	return 0
}
func (e *editState) toolbarShown() bool { return false }
func (e *editState) layoutToolbar(gtx layout.Context, v *View, p image.Point, w int) int {
	return 0
}
func (e *editState) docChanged(v *View)  {}
func (e *editState) editing(b *blk) bool { return false }
func (e *editState) layoutEditor(gtx layout.Context, v *View, b *blk, top, w int) int {
	return top
}
func (e *editState) textArea(gtx layout.Context, v *View, b *blk, r image.Rectangle) {}
func (e *editState) toggleCheck(v *View, b *blk)                                     {}

// startRename is "Rename canvas" (stage 3).
func (v *View) startRename() { v.h.Toast("Renaming canvases isn't supported yet") }
