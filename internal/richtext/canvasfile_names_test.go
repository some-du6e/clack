package richtext

import (
	"strings"
	"testing"
)

// A canvas preview's mentions are the thumbnail's Block Kit mentions,
// ids only: a message names them through its resolver on each
// conversion, as Slack's FileQuip renders the thumbnail's Block Kit with
// the message renderer (BaseMrkdwnChannel, MemberSlug, UserGroup).
func TestCanvasPreviewMentions(t *testing.T) {
	sec := func(els string) string {
		return `{"type":"rich_text_section","elements":[` + els + `]}`
	}
	thumb := `{"blocks":[{"type":"header","text":{"type":"plain_text","text":"Title"}},` +
		`{"type":"rich_text","elements":[` + sec(`{"type":"channel","channel_id":"C1"},{"type":"text","text":" Guidelines"}`) + `]},` +
		`{"type":"rich_text","elements":[{"type":"rich_text_list","style":"bullet","elements":[` +
		sec(`{"type":"text","text":"first"}`) + `,` +
		sec(`{"type":"user","user_id":"U1","style":{"bold":true}},{"type":"text","text":" and "},{"type":"usergroup","usergroup_id":"S1"}`) +
		`]}]}]}`
	r := &thumbRes{fakeRes: R, thumbs: map[string]string{"F1": thumb}}
	f := canvasFileOf(t, r, "F1")
	var spans []Span
	for _, b := range f.CanvasPreview {
		spans = append(spans, b.Spans...)
		for _, it := range b.Items {
			spans = append(spans, it...)
		}
	}
	find := func(k SpanKind, id string) *Span {
		for i := range spans {
			if spans[i].Kind == k && spans[i].ID == id {
				return &spans[i]
			}
		}
		t.Fatalf("no %v %s in the preview %+v", k, id, spans)
		return nil
	}
	if s := find(Channel, "C1"); !strings.Contains(s.Text, "general") {
		t.Errorf("channel: %+v", s)
	}
	if s := find(User, "U1"); !strings.Contains(s.Text, "alice") || s.Style&Bold == 0 {
		t.Errorf("user in a list item: %+v", s)
	}
	if s := find(Usergroup, "S1"); !strings.Contains(s.Text, "design") {
		t.Errorf("group: %+v", s)
	}
}
