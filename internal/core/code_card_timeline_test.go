package core

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Synthetic fixtures only.

// A code channel's summary message that holds only a timeline reads as
// its points' titles (getCodeChannelPreviewText's
// convertMessageToPlainText), not as no summary.
func TestCodeSummaryTimeline(t *testing.T) {
	a := New(slack.New(slack.Creds{}), store.New(""))
	m := &slack.Message{TS: "1.0", Text: "fallback", Blocks: json.RawMessage(`[{"type":"timeline","points":[
		{"id":"a","status":"complete","text":"Read the issue"},{"id":"b","status":"in_progress","text":"Opening a PR"}]}]`)}
	if got := codeSummaryText(a, m); got != "Read the issue In progress: Opening a PR" {
		t.Errorf("summary %q", got)
	}
}
