package slack

import (
	"encoding/json"
	"testing"
)

// TestTruthy: a JSON value as JavaScript's !!value reads it.
func TestTruthy(t *testing.T) {
	for _, c := range []struct {
		v    string
		want bool
	}{
		{"", false}, {"null", false}, {"false", false}, {`""`, false}, {"0", false}, {"-0", false}, {"0.0", false}, {" 0 ", false},
		{"true", true}, {"{}", true}, {"[]", true}, {`"0"`, true}, {`" "`, true}, {"1", true}, {"-1.5", true}, {`{"comment":""}`, true},
	} {
		if got := truthy(json.RawMessage(c.v)); got != c.want {
			t.Errorf("%q: %v, JavaScript %v", c.v, got, c.want)
		}
	}
}

// TestFileInitialCommentKept: a file's initial_comment stays Slack's own
// value, which encodes back as it came.
func TestFileInitialCommentKept(t *testing.T) {
	var f File
	in := `{"id":"F1","initial_comment":{"id":"Fc1","comment":"","created":0,"user":"U1"}}`
	if err := json.Unmarshal([]byte(in), &f); err != nil {
		t.Fatal(err)
	}
	if got := initialComment(&f); got != `{"id":"Fc1","comment":"","created":0,"user":"U1"}` {
		t.Errorf("initial_comment kept as %s", got)
	}
	var none File
	if err := json.Unmarshal([]byte(`{"id":"F1"}`), &none); err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(none); json.Valid(b) && containsKey(b, "initial_comment") {
		t.Errorf("a file without one encodes it: %s", b)
	}
}

func containsKey(b []byte, k string) bool {
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	_, ok := m[k]
	return ok
}
