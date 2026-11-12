package richtext

import "testing"

// MessageKitAttachments hands an attachment with a work object to
// MessageAttachmentWorkObject: one WorkObject block, its rich text
// fields as blocks, the members and channels its fields name resolved;
// a Tableau work object (no Tableau client) draws nothing, never an
// unfurl.
func TestWorkObjectAttachment(t *testing.T) {
	d := FromMessage(R, msg(t, `{"type":"message","ts":"1.0","user":"U1","text":"<https://tracker.example.com/T-1>",
		"attachments":[
		 {"id":1,"from_url":"https://tracker.example.com/T-1","app_unfurl_url":"https://tracker.example.com/T-1?from=slack","is_app_unfurl":true,
		  "title":"must not draw","service_name":"must not draw",
		  "work_object_entity":{"work_object_type":"external","entity_id":"W:A1:T-1","app_id":"A1","display_type":"Task",
		   "layouts":{"expanded":{"title":{"text":"Fix it"},"fields":{"elements":[
		    {"field_type":"rich_text","label":"Body","rich_text":{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"hello "},{"type":"user","user_id":"U1"}]}]}},
		    {"field_type":"user","label":"Who","user":[{"user_type":"slack","user_id":"U2"}]},
		    {"field_type":"channel","label":"Where","channel":[{"channel_id":"C1"}]}]}}}}},
		 {"id":2,"from_url":"https://tableau.example.com/v/1","work_object":{"work_object_type":"tableau","entity_id":"W:A2:v1","layouts":{"compact":{"title":{"text":"Chart"}}}}}]}`))
	var wos []*WorkObject
	for _, b := range d.Blocks {
		if b.Kind == AttachmentBlock && b.Att != nil {
			if b.Att.WorkObject == nil {
				t.Fatalf("an unfurl from a work object attachment: %+v", b.Att)
			}
			wos = append(wos, b.Att.WorkObject)
		}
	}
	if len(wos) != 1 {
		t.Fatalf("%d work objects, want 1 (the Tableau one draws nothing)", len(wos))
	}
	wo := wos[0]
	if wo.E.EntityID != "W:A1:T-1" || wo.AppUnfurlURL != "https://tracker.example.com/T-1?from=slack" {
		t.Errorf("entity %q, app_unfurl_url %q", wo.E.EntityID, wo.AppUnfurlURL)
	}
	body := wo.RichField("expanded", 0)
	if got := PlainText(Doc{Blocks: body}); got != "hello @alice" {
		t.Errorf("rich text field %q", got)
	}
	if m := wo.Member("U2"); m.Name != "@bob" {
		t.Errorf("member %+v", m)
	}
	if c := wo.Channel("C1"); c != "general" {
		t.Errorf("channel %q", c)
	}
}
