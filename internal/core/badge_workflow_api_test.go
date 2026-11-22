package core

import "github.com/some-du6e/clack/internal/slack"

// senderBadgeOf and the workflow bot_profile (badge_workflow_test.go).
func senderBadgeOf(a *App, user, bot string, bp *BotProfile) string {
	return a.SenderBadgeBP(user, bot, bp)
}
func workflowProfile(id string) *BotProfile { return &BotProfile{ID: id, Workflow: true} }
func convertedWorkflow(a *App, ch string, m slack.Message) bool {
	v := a.message(ch, &m)
	return v.BotProfile != nil && v.BotProfile.Workflow
}
