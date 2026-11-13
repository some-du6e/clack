package views

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// An Activity row's body is Slack's SimpleMessagePreview (ActivityMessage
// /ad9 → cjey): an agent's footer message (a context line and feedback
// buttons) previews as its text, where Clack drew the context line (Karim,
// 2026-11-12: "done in under a", cut off); its edited answer as the
// answer, without "(edited)".
func TestActivityPreviewAgentMessages(t *testing.T) {
	app, fs := vaFake(t)
	h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	b := newBase(h)
	ft := fs.AgentDMFooter2()
	footer := app.MessageFromSlack(fakeslack.AgentDMID, &ft)
	if got := richtext.PlainText(b.actPreviewDoc(&footer)); got != "done in under a second" {
		t.Errorf("the footer previews %q, want its text", got)
	}
	ans := fs.AgentDMAnswer(time.Now())
	answer := app.MessageFromSlack(fakeslack.AgentDMID, &ans)
	if got := richtext.PlainText(b.actPreviewDoc(&answer)); got != "pong" {
		t.Errorf("the answer previews %q, want pong", got)
	}
	if answer.DisplaysEdited() {
		t.Error("the app's edited answer would show (edited)")
	}
}
