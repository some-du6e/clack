package taut

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// TestBucketJSONSameAsBucket: the messages slice's JSON the host hands JS
// (bucketJSON, written directly) decodes to exactly what encoding/json
// made of the bucket: every message, text with quotes, backslashes,
// control characters, line and paragraph separators and invalid UTF-8,
// blocks as Slack sent them, and a long text of many lines.
func TestBucketJSONSameAsBucket(t *testing.T) {
	h := newHarness(t)
	m := h.tt.mirror
	long := strings.Repeat("abcdefgh\n", 20000)
	blocks := json.RawMessage(`[{"type":"rich_text","block_id":"b1","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"x \"y\" <z>"}]}]}]`)
	extra := []slack.Message{
		{Type: "message", TS: "1700000100.000100", User: "UALICE", Text: "quote \" backslash \\ tab \t nul \x00 bell \x07 ls \u2028 ps \u2029 bad \xff\xfe end <&>", Blocks: blocks},
		{Type: "message", TS: "1700000200.000100", User: "UBOB", Text: long, Blocks: json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":` + mustJSON(long) + `}]}]}]`)},
		{Type: "message", TS: "1700000300.000100", User: "UBOB", Text: ""},
	}
	tl := h.app.S.Timeline(chGen)
	if tl == nil {
		t.Fatalf("no timeline for %s", chGen)
	}
	h.app.S.SetLatest(chGen, append(append([]slack.Message(nil), tl.Msgs...), extra...), true)
	want, err := json.Marshal(m.bucket(chGen))
	if err != nil {
		t.Fatal(err)
	}
	got := m.bucketJSON(chGen)
	if !json.Valid(got) {
		t.Fatalf("bucketJSON isn't valid JSON (%d bytes)", len(got))
	}
	var a, b map[string]any
	if err := json.Unmarshal(want, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) || len(b) < len(extra) {
		t.Fatalf("bucketJSON has %d messages, the bucket %d", len(b), len(a))
	}
	for ts, x := range a {
		if !reflect.DeepEqual(x, b[ts]) {
			xj, _ := json.Marshal(x)
			yj, _ := json.Marshal(b[ts])
			t.Errorf("message %s:\n bucket     %.400s\n bucketJSON %.400s", ts, xj, yj)
		}
	}
	if m.bucketJSON("C0NOSUCHCHAN") != nil {
		t.Error("a channel with no messages has a bucket")
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
