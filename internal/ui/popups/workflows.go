package popups

// The channel menu's "Add a workflow" (Slack's getWorkflowTemplate,
// gantry-v2-async-client-boot-render module w7ek, build a84662d2d135;
// docs/WORKFLOWS.md): Slack's workflow picker (source channel_workflows,
// automated triggers included); the picked workflow is bookmarked in the
// channel and the channel's Workflows tab opens.

// AddWorkflow, set by the ui root (workflows_wire.go), opens the picker
// for convID.
var AddWorkflow func(h Host, convID string)

// addWorkflowItems is the row, left out where Slack leaves it out: DMs
// and group DMs, and private channels the viewer isn't in.
func addWorkflowItems(h Host, convID string, dm, private, preview bool) []MenuItem {
	if dm || private && preview || AddWorkflow == nil {
		return nil
	}
	return []MenuItem{MenuItem{Label: "Add a workflow", Do: func() { AddWorkflow(h, convID) }}.In(nsMenu)} // getWorkflowTemplate
}
