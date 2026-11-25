package popups

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// A search card draws its result's attachments without
// MessageKitAttachments' 20 (SearchResult's renderAttachments, boot-shared
// IT2K, draws each one itself): the card's views carry NoAttachmentLimit
// (review of expand-more: B3 gave search cards the message's limit).
func TestSearchCardsDrawEveryAttachment(t *testing.T) {
	e := srNewEnv(t, false)
	const word, ts = "zqattlimit", "1762516300.000100"
	ch := e.fs.ChannelIDs()[0]
	var atts []any
	for i := 1; i <= 22; i++ {
		atts = append(atts, map[string]any{"id": i, "text": fmt.Sprintf("%s att-%02d", word, i)})
	}
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "search.modules.messages" || !strings.Contains(f.Get("query"), word) {
			return nil, false
		}
		m := map[string]any{"ts": ts, "user": fakeslack.SelfID, "iid": "I" + ts, "text": word + " the weekly digest",
			"blocks": []any{sbRichText(sbSection(word + " the weekly digest"))}, "attachments": atts}
		return map[string]any{"ok": true, "items": []any{map[string]any{"channel": map[string]any{"id": ch, "name": "general", "is_channel": true},
			"messages": []any{m}}}, "pagination": map[string]any{"total_count": 1, "page": 1, "page_count": 1}}, true
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	v, d := srView(t, e, word, false)
	d.until(t, "the card", func() bool { return len(v.cards) == 1 })
	scBuild(v, 0)
	if !scHasRich(v.cards[0]) || !scNoAttachmentLimit(v.cards[0]) {
		t.Errorf("the card's views: rich %v, no attachment limit %v", scHasRich(v.cards[0]), scNoAttachmentLimit(v.cards[0]))
	}
}
