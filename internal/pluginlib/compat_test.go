package pluginlib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// docStatuses reads a PLUGINS-live doc's plugin table (the first one with
// a "| Plugin |" header): each row's plugin id and its Result column.
func docStatuses(t *testing.T, name string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	in := false
	for _, l := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(l, "| Plugin |"):
			in = true
			continue
		case in && !strings.HasPrefix(l, "|"):
			return out
		case !in || strings.HasPrefix(l, "|---"):
			continue
		}
		cols := strings.Split(strings.Trim(strings.TrimSpace(l), "|"), "|")
		if len(cols) < 2 {
			continue
		}
		out[strings.TrimSpace(cols[0])] = strings.TrimSpace(cols[1])
	}
	return out
}

// The table holds exactly the docs' ❌ and ⚠️ plugins, with their status,
// and each line is one short line that says which it is.
func TestCompatMatchesDocs(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range []struct{ system, doc string }{{Slick, "PLUGINS-live-slick.md"}, {Taut, "PLUGINS-live-taut.md"}} {
		rows := docStatuses(t, d.doc)
		if len(rows) < 20 {
			t.Fatalf("%s: only %d plugin rows read", d.doc, len(rows))
		}
		for id, res := range rows {
			want := CompatOK
			switch {
			case strings.Contains(res, "❌"):
				want = CompatBroken
			case strings.Contains(res, "⚠️"):
				want = CompatPartial
			}
			c := CompatOf(d.system, id)
			if c.Status != want {
				t.Errorf("%s %s: the doc says %q, the table %v", d.system, id, res, c.Status)
			}
			seen[d.system+"/"+id] = true
		}
	}
	for k, c := range compat {
		if !seen[k] {
			t.Errorf("%s is in the table but in neither doc's plugin table", k)
		}
		prefix := map[CompatStatus]string{CompatBroken: "Doesn't work in Clack: ", CompatPartial: "Partly works: "}[c.Status]
		if prefix == "" || !strings.HasPrefix(c.Line, prefix) {
			t.Errorf("%s: %q doesn't say how it works", k, c.Line)
		}
		if n := utf8.RuneCountInString(c.Line); n > 72 || strings.ContainsAny(c.Line, "\n`") {
			t.Errorf("%s: %q (%d characters) isn't one short line", k, c.Line, n)
		}
	}
	if c := CompatOf(Slick, "MentionProfiles"); c.Status != CompatOK || c.Line != "" {
		t.Errorf("a working plugin has %+v", c)
	}
}
