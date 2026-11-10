package ui

import (
	"context"
	"log"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

// Workflow wiring (docs/WORKFLOWS.md). Running a workflow is blockkit's
// (TripWorkflow: Slack's workflowsTriggersTrip, its toasts and speedbump);
// this file hands it the shell's workflow entry points.

func init() {
	composer.WorkflowItem = composerWorkflowItem
	views.TripWorkflow = func(h views.Host, link string, tc core.TripContext, uiPage string) {
		if bh, ok := h.(blockkit.Host); ok { // channel details > Automations
			blockkit.TripWorkflow(bh, link, tc, uiPage)
		}
	}
	popups.AddWorkflow = addWorkflow
}

// addWorkflow is the channel menu's "Add a workflow" (getWorkflowTemplate,
// w7ek): the workflow picker with source channel_workflows and automated
// triggers; the picked one is bookmarked in the channel (bookmarks.add,
// type shortcut) and the channel opens.
func addWorkflow(h popups.Host, ch string) {
	a, _ := h.(*App)
	blockkit.OpenWorkflowPicker(h, blockkit.WorkflowPickerOpts{Source: blockkit.PickerChannelWorkflows, ChannelID: ch, IncludeAutomated: true,
		OnInsert: func(t core.WorkflowTrigger) {
			if a != nil {
				a.addWorkflowBookmark(ch, t, "")
			}
		}})
}

// addTabWorkflowItems is the tab bar "+" menu's "Workflow" (Slack's
// AddTabMenu, boot-shared /wV5: the bolt icon), listed while the channel
// has no Workflows tab and the viewer may post in it. Its picker is
// useCreateWorkflow's (OAZD): the same as the channel menu's, but the
// bookmark carries the trigger's shortcut URL and a workflow already
// bookmarked isn't added again (addWorkflowBookmarkForChannel).
func (a *App) addTabWorkflowItems(ch string) []MenuItem {
	c := a.Core()
	if c == nil || !c.PostingIn(ch).CanPost() {
		return nil
	}
	for _, t := range a.channelTabs(ch) {
		if t.icon == "workflow" {
			return nil
		}
	}
	return []MenuItem{{Label: "Workflow", Icon: "lightning", Do: func() {
		blockkit.OpenWorkflowPicker(a, blockkit.WorkflowPickerOpts{Source: blockkit.PickerChannelWorkflows, ChannelID: ch, IncludeAutomated: true,
			OnInsert: func(t core.WorkflowTrigger) {
				if a.data != nil {
					for _, bm := range a.data.bookmarks[ch] {
						if bm.ShortcutID == t.ID {
							return // "Cannot add already bookmarked workflow to bookmarks"
						}
					}
				}
				a.addWorkflowBookmark(ch, t, t.ShortcutURL)
			}})
	}}}
}

// addWorkflowBookmark bookmarks t in ch (link: the tab bar's shortcut URL,
// "" for the channel menu's), then opens the channel's Workflows tab with
// the tabs reloaded (Slack's toChannel with tabId workflows). Slack shows
// nothing when bookmarks.add fails (the promise has no catch); Clack logs
// it.
func (a *App) addWorkflowBookmark(ch string, t core.WorkflowTrigger, link string) {
	if a.data == nil || a.data.core == nil {
		return
	}
	c := a.data.core
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := c.AddWorkflowBookmark(ctx, ch, t, link)
		a.Post(func() {
			if err != nil {
				log.Printf("workflows: add %s to %s: %v", t.ID, ch, err)
				return
			}
			a.Navigate("c/" + ch)
			delete(a.data.fetched, ch)
			a.data.opened(ch)
			a.openWorkflowsTab(ch) // workflows_tab.go: selected once the tab bar has it
		})
	}()
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
