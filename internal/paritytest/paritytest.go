// Package paritytest runs a Slack-parity snapshot: Slack's own output for
// a list of inputs, committed as testdata/parity/<dimension>.json, against
// what Clack computes from the same inputs now. Slack's side comes from
// Slack's JavaScript run in node or from live Slack, never from someone
// reading the code; `tools/parity.py --refresh` captures it again.
//
// Only tests import this package. tools/parity.py runs them and reads
// each case's result (docs/PARITY.md).
package paritytest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// KnownDivergence starts the skip message of a case where Clack still
// differs from Slack on purpose or for now; tools/parity.py counts it as
// not matching.
const KnownDivergence = "parity: known divergence"

// Snapshot is one dimension's committed copy of Slack's output.
type Snapshot[In, Out any] struct {
	Dimension string `json:"dimension"`
	// Slack says where the values come from (for tools/parity.py).
	Slack json.RawMessage `json:"slack"`
	// Base, when there is one, is the inputs every case shares (the
	// viewer, their workspace, its experiments and prefs): each case's
	// own inputs go over it, the same way on Slack's side and on Clack's.
	Base  json.RawMessage `json:"base,omitempty"`
	Cases []Case[In, Out] `json:"cases"`
}

// Case is one input and what Slack made of it.
type Case[In, Out any] struct {
	Name string `json:"name"`
	In   In     `json:"in"`
	// Slack is Slack's output; nil until --refresh captures it.
	Slack *Out `json:"slack"`
	// Known says why Clack differs from Slack on this case and isn't
	// fixed yet. The case skips while it still differs and fails once it
	// matches, so the mark can't outlive the difference.
	Known string `json:"known,omitempty"`
	// Clack, with Known, is the output Clack is known to give instead:
	// any other output fails, so the rest of the case stays checked.
	Clack *Out `json:"clack,omitempty"`
}

// Check says how to get Clack's output for a case and compare it.
type Check[In, Out any] struct {
	// Clack is Clack's output for in, computed now.
	Clack func(t *testing.T, in In) Out
	// Same compares Slack's output with Clack's; nil compares their JSON.
	Same func(slack, clack Out) bool
	// Explain, if set, logs more about a case that differs.
	Explain func(t *testing.T, in In, clack Out)
}

// Load reads a snapshot, a path relative to the test's package.
func Load[In, Out any](t testing.TB, path string) *Snapshot[In, Out] {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s Snapshot[In, Out]
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(s.Cases) == 0 {
		t.Fatalf("%s: no cases", path)
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		// a "/" would read as a nested subtest, and a repeated name gets
		// a #01 suffix: either way tools/parity.py couldn't find the case
		if c.Name == "" || strings.Contains(c.Name, "/") || seen[c.Name] {
			t.Fatalf("%s: case name %q is empty, holds a /, or repeats", path, c.Name)
		}
		seen[c.Name] = true
	}
	return &s
}

// Run runs each case as a subtest named after it.
func (s *Snapshot[In, Out]) Run(t *testing.T, c Check[In, Out]) {
	t.Helper()
	same := c.Same
	if same == nil {
		same = func(a, b Out) bool { return js(a) == js(b) }
	}
	for _, k := range s.Cases {
		t.Run(k.Name, func(t *testing.T) {
			if k.Slack == nil {
				t.Skipf("parity: no Slack output for this case yet (tools/parity.py --refresh %s)", s.Dimension)
			}
			got := c.Clack(t, k.In)
			ok := same(*k.Slack, got)
			if k.Known != "" {
				switch {
				case ok:
					t.Errorf("parity: matches Slack now; drop the case's known divergence (%s)", k.Known)
				case k.Clack != nil && !same(*k.Clack, got):
					t.Errorf("parity: differs from Slack, and from the output its known divergence recorded\nslack: %s\nclack: %s\nrecorded: %s",
						js(*k.Slack), js(got), js(*k.Clack))
				default:
					t.Skipf("%s: %s\nslack: %s\nclack: %s", KnownDivergence, k.Known, js(*k.Slack), js(got))
				}
				return
			}
			if !ok {
				t.Errorf("parity: differs from Slack\nslack: %s\nclack: %s", js(*k.Slack), js(got))
				if c.Explain != nil {
					c.Explain(t, k.In, got)
				}
			}
		})
	}
}

// js is v as one line of JSON, with <, > and & left as they are.
func js(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return strings.TrimSuffix(b.String(), "\n")
}
