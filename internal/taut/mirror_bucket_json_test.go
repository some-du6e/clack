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
	// the order JS sees: the messages by ts, each message's keys sorted,
	// text and blocks among them, as encoding/json writes a map
	if g, w := jsonKeyOrder(t, got), jsonKeyOrder(t, want); !reflect.DeepEqual(g, w) {
		t.Errorf("bucketJSON's keys come in another order than encoding/json's:\n got  %.300v\n want %.300v", g, w)
	}
	if m.bucketJSON("C0NOSUCHCHAN") != nil {
		t.Error("a channel with no messages has a bucket")
	}
}

// jsonKeyOrder lists an object of objects' keys in the order the JSON
// writes them: each top-level key, then its object's keys.
func jsonKeyOrder(t *testing.T, raw []byte) []string {
	t.Helper()
	var top []string
	var byKey map[string]json.RawMessage
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if _, err := dec.Token(); err != nil { // {
		t.Fatal(err)
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		top = append(top, k.(string))
		if byKey == nil {
			byKey = map[string]json.RawMessage{}
		}
		byKey[k.(string)] = v
	}
	var out []string
	for _, k := range top {
		out = append(out, "@"+k)
		d := json.NewDecoder(strings.NewReader(string(byKey[k])))
		if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
			continue
		}
		for d.More() {
			mk, err := d.Token()
			if err != nil {
				t.Fatal(err)
			}
			var skip json.RawMessage
			if err := d.Decode(&skip); err != nil {
				t.Fatal(err)
			}
			out = append(out, mk.(string))
		}
	}
	return out
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestWholeJSONSameAsMarshal: a chunked bucket written whole (wholeJSON)
// decodes to what encoding/json made of it, and the same entries always
// make the same bytes (the same-bytes check's premise).
func TestWholeJSONSameAsMarshal(t *testing.T) {
	b := map[string]json.RawMessage{
		"1700000300.000100": json.RawMessage(`{"ts":"1700000300.000100","text":"a \"quoted\" <b> & line\nbreak"}`),
		"1700000100.000100": json.RawMessage(`{"ts":"1700000100.000100","blocks":[{"type":"rich_text"}],"text":"` + strings.Repeat("x", 100000) + `"}`),
		"1700000200.000100": json.RawMessage(`{"ts":"1700000200.000100","reactions":[{"name":"tada","count":2}]}`),
	}
	want, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	got := wholeJSON(b)
	var x, y any
	if json.Unmarshal(want, &x) != nil || json.Unmarshal(got, &y) != nil {
		t.Fatalf("wholeJSON %.200s isn't JSON", got)
	}
	if !reflect.DeepEqual(x, y) {
		t.Errorf("wholeJSON decodes to something else than encoding/json's:\n%.300s\n%.300s", got, want)
	}
	if string(wholeJSON(b)) != string(got) {
		t.Error("the same entries made different bytes")
	}
	if string(wholeJSON(nil)) != "null" {
		t.Errorf("a nil bucket is %s", wholeJSON(nil))
	}
}
