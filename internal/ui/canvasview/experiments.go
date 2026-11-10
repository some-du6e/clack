package canvasview

// The canvas experiments Clack follows (docs/EXPERIMENTS.md, Canvases).
// Slack's client reads them with getGroupForUser (HWuB `_Z`), which is
// null for an experiment the viewer isn't assigned to, so only "on"
// turns one on. The Experiments plugins flip them live: every check is
// read on the frame that needs it.
//
//   - canvas_table_of_contents: Quip's CANVAS_ALLOW_SLACK_OUTLINE (feature
//     188, client-boot-shared 65wK createInitConfig) and QuipEmbed's
//     TableOfContentsDrawer (tshn), toc.go.
//   - canvas_blockquote_layout: Quip's BLOCKQUOTE_LAYOUT (197) and the
//     toolbars' blockquote layout (client-boot M6vS zk, foOi
//     InsertionToolbarFormattingMenu / InsertionToolbarMenu), edit.go.
//   - slackified_checkbox: Quip's useSlackCheckboxes (178), render.go.

// exp: the viewer is in the experiment's "on" group.
func (v *View) exp(name string) bool {
	a := v.h.Core()
	return a != nil && a.Experiment(name) == "on"
}
