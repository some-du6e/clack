package ui

import (
	"context"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// Workflow wiring (docs/WORKFLOWS.md). Running a workflow is blockkit's
// (TripWorkflow: Slack's workflowsTriggersTrip, its toasts and speedbump);
// this file hands it the shell's workflow entry points.

func init() {
	composer.WorkflowItem = composerWorkflowItem
}

// composerWorkflowItem is the + menu's "Workflow" row (Slack's
// useComposerIAFormatter: attach-workflow, the play icon): the workflow
// picker titled "Attach a workflow", whose Insert puts the workflow's
// name linked to its share URL at the caret (attachWorkflowToMessage).
func composerWorkflowItem(h popups.Host, ch string, insert func(text, url string)) *popups.MenuItem {
	return &popups.MenuItem{Label: "Workflow", Icon: "play", Do: func() {
		blockkit.OpenWorkflowPicker(h, blockkit.WorkflowPickerOpts{Source: blockkit.PickerShortcutsMenu, ChannelID: ch,
			OnInsert: func(t core.WorkflowTrigger) {
				if t.Name == "" || t.ShareURL == "" {
					return // Slack inserts nothing without both
				}
				insert(t.Name, t.ShareURL)
			}})
	}}
}

// tripBookmark runs a workflow bookmark (type shortcut, or any bookmark
// whose link is a workflow link) the way Slack's bookmarked workflows do:
// its trigger with location bookmark. It reports whether bm was one.
func (a *App) tripBookmark(ch string, bm core.Bookmark) bool {
	if blockkit.IsWorkflowLink(bm.Link) {
		blockkit.TripWorkflow(a, bm.Link, core.BookmarkTripContext(ch, bm.ID), "channel_workflows")
		return true
	}
	if bm.Type != "shortcut" || bm.ShortcutID == "" || a.data == nil || a.data.core == nil {
		return false
	}
	// a shortcut bookmark without its link: the trigger has it
	c := a.data.core
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		t, err := c.Interactive().TriggerInfo(ctx, bm.ShortcutID)
		a.Post(func() {
			if err != nil || t.ShortcutURL == "" {
				a.Toast(core.TripErrorText(core.ErrCode(err), ""))
				return
			}
			blockkit.TripWorkflow(a, t.ShortcutURL, core.BookmarkTripContext(ch, bm.ID), "channel_workflows")
		})
	}()
	return true
}
