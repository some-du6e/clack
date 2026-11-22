package taut

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// TestMirrorFileInitialComment: a mirrored message's file carries Slack's
// own initial_comment, the object as Slack sent it (its empty and zero
// fields too), not whether there is one; a file without one has none.
// Synthetic message.
func TestMirrorFileInitialComment(t *testing.T) {
	h := newHarness(t)
	var sm slack.Message
	raw := `{"type":"message","ts":"1700000800.000100","user":"UALICE","text":"","upload":true,"files":[` +
		`{"id":"F0SYNTH0001","name":"a.txt","mimetype":"text/plain","user":"UALICE",` +
		`"initial_comment":{"id":"Fc0SYNTH0001","created":0,"timestamp":1700000800,"user":"UALICE","comment":"","is_intro":true}},` +
		`{"id":"F0SYNTH0002","name":"b.txt","mimetype":"text/plain","user":"UALICE"}]}`
	if err := json.Unmarshal([]byte(raw), &sm); err != nil {
		t.Fatal(err)
	}
	h.app.S.SetLatest("C0FILES", []slack.Message{sm}, false)
	var got map[string]any
	h.jsInto(`
		const m = T.redux.getRawState().messages.C0FILES['1700000800.000100']
		return { comment: m.files[0].initial_comment, none: 'initial_comment' in m.files[1] }`, &got)
	want := map[string]any{"id": "Fc0SYNTH0001", "created": float64(0), "timestamp": float64(1700000800), "user": "UALICE", "comment": "", "is_intro": true}
	if !reflect.DeepEqual(got["comment"], want) {
		t.Errorf("files[0].initial_comment = %#v, Slack's %#v", got["comment"], want)
	}
	if got["none"] != false {
		t.Errorf("a file without an initial_comment has one: %v", got["none"])
	}
}
