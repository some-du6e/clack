package views

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// The "You replied" and draft badges quote their text the way Slack's
// RepliedTooltipContent does (useReplyMessagePreview through Mrkdwn):
// mentions and channel links as @name and #name, never the raw <@U…> and
// <#C…> tokens (the 2026-11-22 relatives of the search title's fix).
func TestActivityBadgeTextResolvesMentions(t *testing.T) {
	app, _ := vaFake(t)
	got := vbBadgeText(app, "ask <@"+fakeslack.SelfID+"> in <#"+fakeslack.ViewsChannel+">")
	if strings.Contains(got, "<@") || strings.Contains(got, "<#") {
		t.Fatalf("raw tokens in %q", got)
	}
	if !strings.Contains(got, "#views-fixtures") || !strings.Contains(got, "@") {
		t.Errorf("badge text %q, want the mention's name and #views-fixtures", got)
	}
	if plain := vbBadgeText(app, "just words"); plain != "just words" {
		t.Errorf("plain text changed: %q", plain)
	}
}
