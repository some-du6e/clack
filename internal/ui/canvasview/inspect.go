package canvasview

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// The canvas document's title as Clack's inspect tool (internal/ui/inspect)
// sees it: Quip's title section, recorded with slackc's ClassDef.Inspect
// for the picker alone. No element goes on slackc's element stack, so no
// selector matches these and nothing draws differently; outside an
// inspect session each costs one atomic load.
//
// Quip's markup (gantry-v2-async-quip-collab-webapp, createSectionDom):
// every section is <div class="section" data-click="section" …> holding
// <div class="content">, the section's style in data-section-style
// (setSectionStyleDataAttribute_). The title is TEXT_TITLE_STYLE, 48; the
// Quip editor's stylesheet (canvas-controller-*.css) sizes it with
// .document-content .section[data-section-style="48"]>.content (28px/34px
// at --doc-theme-header-weight). The section has no padding, so the
// content fills it; its 20px bottom margin is outside both.
var (
	insTitleSection = slackc.Class("section", "data-section-style", "48")
	insTitleContent = slackc.Class("content")
)

// inspectTitle records the title section and its content around draw,
// which lays out the title at the current op origin and returns its
// height; the boxes are w px wide (the column: a block div). It returns
// draw's height.
func inspectTitle(gtx layout.Context, w int, draw func() int) int {
	sec := insTitleSection.Inspect(0)
	cnt := insTitleContent.Inspect(0)
	h := draw()
	cnt.End(gtx, image.Pt(w, h))
	sec.End(gtx, image.Pt(w, h))
	return h
}
