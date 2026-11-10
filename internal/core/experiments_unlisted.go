package core

// Experiment checks other packages already make that experimentReads
// didn't list yet (TestExperimentReadsListed), with the groups each
// compares. Their owners may list them in experiments.go too: setting a
// name twice is harmless.
var unlistedExperimentReads = map[string][]string{
	"activity_bulk_unread_tool":                         on, // ui/views/activity_bulk.go
	"ai_channel_exclusions":                             on, // ui/views/details_more.go
	"draft_management_in_composer":                      {"treatment"},
	"enable_new_channel_settings":                       on,
	"lists_grid_todos":                                  on, // ui/listview/picker.go
	"mcp_ai_channel_exclusions":                         on,
	"move_channel_tabs_settings":                        on,
	"mpdm_default_exclude_history_clients_webapp":       on,
	"rich_text_headers_and_dividers":                    on, // ui/composer/composer.go
	"sidebar_draft_indicator_menu_and_thread_inclusion": on, // ui/backend.go
	"updated_scheduled_sends_desktop":                   on,
}

func init() {
	for name, groups := range unlistedExperimentReads {
		if _, ok := experimentReads[name]; !ok {
			experimentReads[name] = groups
		}
	}
}
