package views

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// A draft's badge quotes the draft's text as it is: core.Draft.Text is
// already the plain text of the draft's document, so reading it as mrkdwn
// again would eat its asterisks and angle brackets (the badges' mrkdwn
// is for the messages' text).
func TestActivityDraftBadgeQuotesPlainText(t *testing.T) {
	v, _, _, fs := vaFakeActivity(t, false)
	const plain = "Tom & Jerry: *literal* stars <here>"
	blocks, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section",
		"elements": []any{map[string]any{"type": "text", "text": plain}}}}}})
	dest, _ := json.Marshal([]any{map[string]any{"channel_id": fakeslack.ViewsChannel}})
	fs.AddDraft(fakeslack.Draft{Blocks: blocks, Destinations: dest})
	if _, err := v.h.Core().Drafts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d, ok := v.h.Core().DraftFor(fakeslack.ViewsChannel, ""); !ok || d.Text != plain {
		t.Fatalf("the channel's draft is %q (%v), want %q", d.Text, ok, plain)
	}
	mk, ok := v.readMark(actItem(t, v, "fa-channel"))
	if want := `Draft: "` + plain + `"`; !ok || mk.tip != want {
		t.Errorf("the draft badge says %q (%v), want %q", mk.tip, ok, want)
	}
}
