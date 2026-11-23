package pluginlib

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// docs/PLUGIN-COMPAT.md (getclack.club/compat) lists every plugin in the
// published Library under how far it works in Clack: compat.go's broken
// and partial rows with their reasons, and every other plugin as working.
// tools/compatdoc/compatdoc.py writes it from compat.go and
// tools/compatdoc/index.json (the Library's index); this keeps the page in
// step with both, so a compat.go change that skips the generator fails.
func TestCompatDocInStep(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "tools", "compatdoc", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ix Index
	if err := json.Unmarshal(b, &ix); err != nil {
		t.Fatal(err)
	}
	if len(ix.Plugins) < 90 {
		t.Fatalf("tools/compatdoc/index.json has %d plugins", len(ix.Plugins))
	}
	page, err := os.ReadFile(filepath.Join("..", "..", "docs", "PLUGIN-COMPAT.md"))
	if err != nil {
		t.Fatal(err)
	}
	const (
		secBroken  = "❌ Doesn't work yet"
		secPartial = "⚠️ Partly works"
		secOK      = "✅ Works fully"
	)
	type row struct{ section, note string }
	sys := map[string]string{Taut: "Taut", Slick: "Slick"}

	want, wantN := map[string]row{}, map[string]int{}
	taut, slick := 0, 0
	for _, e := range ix.Plugins {
		c := CompatOf(e.System, e.ID)
		r := row{secOK, ""}
		switch c.Status {
		case CompatBroken:
			r = row{secBroken, strings.TrimPrefix(c.Line, "Doesn't work in Clack: ")}
		case CompatPartial:
			r = row{secPartial, strings.TrimPrefix(c.Line, "Partly works: ")}
		}
		want[e.Name+" ("+sys[e.System]+")"] = r
		wantN[r.section]++
		if e.System == Taut {
			taut++
		} else {
			slick++
		}
	}

	got, gotN := map[string]row{}, map[string]int{}
	section := ""
	cell := func(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), `\|`, "|") }
	for _, l := range strings.Split(string(page), "\n") {
		if strings.HasPrefix(l, "## ") {
			section = strings.TrimPrefix(l, "## ")
			continue
		}
		if !strings.HasPrefix(l, "| ") || strings.HasPrefix(l, "| Plugin |") || strings.HasPrefix(l, "| | ") {
			continue
		}
		cols := strings.Split(strings.TrimSuffix(strings.TrimPrefix(l, "| "), " |"), " | ")
		if section == "" { // the counts table: "| ✅ Works fully | 72 |"
			if len(cols) == 2 {
				var n int
				if _, err := fmt.Sscan(cols[1], &n); err == nil {
					gotN[cols[0]] = n
				}
			}
			continue
		}
		if len(cols) < 3 {
			t.Errorf("a row the test can't read under %q: %q", section, l)
			continue
		}
		note := ""
		if len(cols) >= 4 {
			note = cell(cols[3])
		}
		k := cell(cols[0]) + " (" + cell(cols[1]) + ")"
		if _, dup := got[k]; dup {
			t.Errorf("%s is listed twice", k)
		}
		got[k] = row{section, note}
	}

	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		w, inWant := want[k]
		g, inGot := got[k]
		switch {
		case !inGot:
			t.Errorf("%s isn't on the page; compat.go says %s %q", k, w.section, w.note)
		case !inWant:
			t.Errorf("%s is on the page (%s) but not in tools/compatdoc/index.json", k, g.section)
		case g != w:
			t.Errorf("%s: the page says %s %q, compat.go %s %q", k, g.section, g.note, w.section, w.note)
		}
	}
	for _, s := range []string{secOK, secPartial, secBroken} {
		if gotN[s] != wantN[s] {
			t.Errorf("the page counts %d %q, want %d", gotN[s], s, wantN[s])
		}
	}
	if intro := fmt.Sprintf("(%d plugins: %d from Taut, %d from Slick)", len(ix.Plugins), taut, slick); !strings.Contains(string(page), intro) {
		t.Errorf("the page's intro doesn't say %s", intro)
	}
	if t.Failed() {
		t.Log("regenerate it: python3 tools/compatdoc/compatdoc.py (from the repository root)")
	}
}
