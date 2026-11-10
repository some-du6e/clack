package core

// More of Clack's experiment checks, registered here rather than in
// experimentReads' literal so branches that add checks in parallel don't
// collide on it (a key repeated in a map literal doesn't compile; one
// assigned twice here is harmless). TestExperimentReadsListed covers
// these like the rest.
func init() {
	for name, groups := range map[string][]string{
		// canvases (ui/canvasview; docs/EXPERIMENTS.md, Canvases)
		"canvas_table_of_contents": on, // TableOfContentsDrawer and Quip's 64px outline gutter
		"canvas_blockquote_layout": on, // the toolbars' blockquote layout (M6vS zk, foOi)
		"slackified_checkbox":      on, // Quip's useSlackCheckboxes
		"canvas_comment_only_mode": on, // QuipAccessIndicator's "Comment only" (CQK3)
		// lists (core/lists.go, ui/listview)
		"lists_file_backed_templates": on, // lists.templates' template_files (o9il fetchTemplates)
		"lists_export_enrichment":     on, // More actions' Download modal (57SI MoreActionsMenu)
		"lists_grid_todos":            on, // to-do lists from the dialogs (fuYI, wr4u)
		// checks merged into ui (3a5a0373) without a listing
		"activity_bulk_unread_tool":                         on,
		"ai_channel_exclusions":                             on,
		"draft_management_in_composer":                      {"treatment"},
		"enable_new_channel_settings":                       on,
		"mcp_ai_channel_exclusions":                         on,
		"move_channel_tabs_settings":                        on,
		"mpdm_default_exclude_history_clients_webapp":       on,
		"rich_text_headers_and_dividers":                    on,
		"sidebar_draft_indicator_menu_and_thread_inclusion": on,
		"updated_scheduled_sends_desktop":                   on,
	} {
		experimentReads[name] = groups
	}
}
