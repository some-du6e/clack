package core

// A Workflow Builder post from a bot not yet cached badges WORKFLOW
// (review of thread-header-hover-4): a user-less sender's bot is the
// store's, else its bot_profile (Emf2: c = botProfile while getBotById
// hasn't it), whose is_workflow_bot the badge reads; since round 4 such
// bots aren't asked for (senderOf leaves bot_profile senders alone).

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func TestWorkflowBadgeFromBotProfile(t *testing.T) {
	a := New(slack.New(slack.Creds{Token: "xoxc-test", Team: "T0VIEWER01", UserID: "USELF"}), store.New(""))
	if !convertedWorkflow(a, "C0WORKFLOW", slack.Message{TS: "1700000000.000100", BotID: "B0WORKFLOW1",
		BotProfile: &slack.BotProfile{ID: "B0WORKFLOW1", Name: "Standup", AppID: "A0WF", IsWorkflowBot: true}}) {
		t.Error("the message's bot_profile lost is_workflow_bot")
	}
	if got := senderBadgeOf(a, "", "B0WORKFLOW1", workflowProfile("B0WORKFLOW1")); got != BadgeWorkflow {
		t.Errorf("a Workflow Builder post from an uncached bot badges %q, want %q (its bot_profile's is_workflow_bot)", got, BadgeWorkflow)
	}
}
