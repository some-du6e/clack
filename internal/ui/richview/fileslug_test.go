package richview

import (
	"encoding/json"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestFileSlugLabel: an inline rich_text file element draws Slack's File
// slug with its label (the file's title) beside the type icon, on the
// element's line; the element's own text isn't drawn.
func TestFileSlugLabel(t *testing.T) {
	st := newTestStyle()
	m := slack.Message{TS: "1.0", Text: "x", Files: []slack.File{{ID: "F1", Name: "q3.pdf", Title: "Quarterly report", Filetype: "pdf"}},
		Blocks: json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"See "},` +
			`{"type":"file","file_id":"F1","url":"https://x.slack.com/files/U1/F1/q3.pdf","text":"ignored"}]}]}]`)}
	v := layoutAt(st, rt.FromMessage(codeRes{}, &m), Options{}, 600)
	see, label := findFrag(t, &v.d, "See"), findFrag(t, &v.d, "Quarterly")
	if label.top != see.top || label.x <= see.x {
		t.Errorf("label at (%.0f, %d), want after See on its line (%d)", label.x, label.top, see.top)
	}
	if hasFrag(&v.d, "ignored") {
		t.Error("the element's own text drew")
	}
}
