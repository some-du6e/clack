package slack

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPackEmojiJSONMatchesEncodingJSON(t *testing.T) {
	body := `{"ok":true,"cache_ts":"1.0","meta":{"x":[1,{"emoji":"no"}]},"emoji":{` +
		`"party":"https:\/\/emoji.slack-edge.com\/T0\/party\/a.gif",` +
		`"pp":"alias:party", "q\"uote":"https:\/\/e\/q.png",` +
		`"u":"é😀", "skip":null, "num":3, "obj":{"a":"b"},` +
		` "last" : "v"},"categories_version":"5"}`
	var want struct {
		Emoji map[string]any `json:"emoji"`
	}
	if err := json.Unmarshal([]byte(body), &want); err != nil {
		t.Fatal(err)
	}
	packed, n, err := PackEmojiJSON([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	parts := strings.Split(strings.TrimSuffix(packed, "\n"), "\n")
	for i := 0; i+1 < len(parts); i += 2 {
		got[parts[i]] = parts[i+1]
	}
	strs := 0
	for k, v := range want.Emoji {
		s, ok := v.(string)
		if !ok {
			if _, has := got[k]; has {
				t.Errorf("%q: a non-string value was packed", k)
			}
			continue
		}
		strs++
		if got[k] != s {
			t.Errorf("%q = %q, want %q", k, got[k], s)
		}
	}
	if n != strs || len(got) != strs {
		t.Errorf("packed %d (%d), want %d", n, len(got), strs)
	}
	if _, _, err := PackEmojiJSON([]byte(`{"ok":true,"emoji":{"a":"b"`)); err == nil {
		t.Error("truncated JSON accepted")
	}
	if p, n, err := PackEmojiJSON([]byte(`{"ok":true}`)); err != nil || n != 0 || p != "" {
		t.Errorf("no emoji key: %q %d %v", p, n, err)
	}
}
