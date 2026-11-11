package fakeslack

// accountExperiments are more of the test account's groups
// (experiments.getByUser, 2026-11-10, docs/EXPERIMENTS.md): the canvas
// and list experiments Clack follows. The fake's answer gets them on top
// of its sidebar set, so tests and shots run as the account does; tests
// flip one with core's SetExperimentGroup.
var accountExperiments = map[string]string{
	"canvas_table_of_contents":    "on",
	"canvas_blockquote_layout":    "on",
	"canvas_comment_only_mode":    "on",
	"slackified_checkbox":         "on",
	"lists_file_backed_templates": "on",
	"lists_export_enrichment":     "on",
	"lists_grid_todos":            "on",
}

// withAccountExperiments adds accountExperiments to an
// experiments.getByUser answer (keeping any group it already has).
func withAccountExperiments(out any) any {
	m, ok := out.(map[string]any)
	if !ok {
		return out
	}
	as, ok := m["assignments"].(map[string]any)
	if !ok {
		return out
	}
	for name, group := range accountExperiments {
		if _, has := as[name]; !has {
			as[name] = map[string]any{"group": group}
		}
	}
	return out
}
