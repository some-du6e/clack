package popups

import "image"

// CommitSearch runs q as a search the way the search box's Enter does
// (SearchPopover.submit: the recent query, search.save, the results
// page): Slack's search handoff with commitSearch set, as the sidebar
// filter's "Search {teamName}" sends it (boot-render JS+i,
// openMethod "sidebar_filter_handoff").
func CommitSearch(h Host, q string) {
	NewSearchPopover(h, q, image.Rectangle{}).submit(q)
}

// OnConversationPicked, when set, runs when the switcher or the search
// box opens a conversation: the ui root clears the sidebar text filter
// then, as Slack's omnibox does under sidebar_text_filter (boot-render
// pqxE dispatches "sidebar-text-filter-reset").
var OnConversationPicked func()

func pickedConversation() {
	if OnConversationPicked != nil {
		OnConversationPicked()
	}
}
