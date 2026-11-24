package richview

import (
	"fmt"
	"strings"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// A search result draws every attachment (SearchResult's
// renderAttachments, boot-shared IT2K, never MessageKitAttachments): with
// NoAttachmentLimit the 22 attachments that collapse past 20 in a message
// all draw, and no "+ 2 more attachments" button follows them.
func TestAttachmentsNoLimitInSearch(t *testing.T) {
	st := newTestStyle()
	m := slack.Message{TS: "1700000000.000100"}
	for i := 1; i <= 22; i++ {
		m.Attachments = append(m.Attachments, slack.Attachment{ID: i, Text: fmt.Sprintf("att-%02d", i)})
	}
	d := rt.FromMessage(codeRes{}, &m)
	v := layoutAt(st, d, noAttachmentLimit(Options{Channel: "C1", TS: m.TS}), 700)
	plain := string(v.d.plain)
	if !strings.Contains(plain, "att-22") || strings.Contains(plain, "more attachments") || strings.Contains(plain, "Show fewer") {
		t.Errorf("a search result's attachments: plain %q", plain)
	}
	if w := layoutAt(st, d, Options{Channel: "C1", TS: m.TS}, 700); !strings.Contains(string(w.d.plain), "+ 2 more attachments") {
		t.Errorf("a message keeps the limit: plain %q", string(w.d.plain))
	}
}
