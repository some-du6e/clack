package core

import "testing"

// SearchResult's attachmentHasSearchMatch and fileHasSearchMatch
// (boot-shared IT2K, build a09b625d90aa) on an answer still holding its
// highlight markers: the card draws only what matched.
func TestSearchMatches(t *testing.T) {
	raw := []byte(`{"items":[{"messages":[{"ts":"1","text":"x","attachments":[` +
		`{"id":1,"title":"X post","text":"nothing here"},` +
		`{"id":2,"text":"the zephyr rollout"},` +
		`{"id":3,"title":"t","extracts":{"text":[{"text":"x"}]}},` +
		`{"text":"no id but zephyr"},` +
		`{"id":5,"author_name":"Zephyr bot"}],` +
		`"files":[{"id":"F1","name":"notes.txt"},{"id":"F2","title":"zephyr.pdf"},` +
		`{"id":"F3","name":"a.txt","file_contents_hl":{"lines":[{"line":"x"}]}}]}]}]}`)
	got := smOf(raw)[[2]int{0, 0}]
	atts, files := got[0], got[1]
	for _, k := range []string{"2", "3", "idx:3", "5"} {
		if !atts[k] {
			t.Errorf("attachment %s not matched (%v)", k, atts)
		}
	}
	if atts["1"] {
		t.Error("an attachment without a marker matched")
	}
	if files["F1"] || !files["F2"] || !files["F3"] {
		t.Errorf("files %v, want F2 and F3", files)
	}
}
