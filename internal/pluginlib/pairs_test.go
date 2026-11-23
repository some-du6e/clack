package pluginlib

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// docPairTable reads docs/PLUGINS.md's table of plugins both systems ship
// (the one with a "| Plugin (Taut / Slick) |" header): each row's Taut and
// Slick ids and what pairs them ("name" or "sources").
func docPairTable(t *testing.T) map[Pair]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "PLUGINS.md"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[Pair]string{}
	in := false
	for _, l := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(l, "| Plugin (Taut / Slick) |"):
			in = true
			continue
		case in && !strings.HasPrefix(l, "|"):
			return out
		case !in || strings.HasPrefix(l, "|---"):
			continue
		}
		cols := strings.Split(strings.Trim(strings.TrimSpace(l), "|"), "|")
		if len(cols) < 4 {
			continue
		}
		id := func(s string) string { return strings.Trim(strings.TrimSpace(s), "`") }
		out[Pair{Taut: id(cols[1]), Slick: id(cols[2])}] = strings.TrimSpace(cols[3])
	}
	return out
}

// The pairs read from the sources are exactly the doc table's "sources"
// rows, and a pair is never both one of those and one paired by name.
func TestPairsMatchDocs(t *testing.T) {
	rows := docPairTable(t)
	if len(rows) < 25 {
		t.Fatalf("only %d rows read from docs/PLUGINS.md's pair table", len(rows))
	}
	var fromDoc []Pair
	for p, by := range rows {
		switch by {
		case "sources":
			fromDoc = append(fromDoc, p)
		case "name":
		default:
			t.Errorf("%v: paired by %q, want name or sources", p, by)
		}
		if p.Taut == "" || p.Slick == "" {
			t.Errorf("a row without both ids: %+v", p)
		}
	}
	less := func(a, b Pair) int { return strings.Compare(a.Taut+"/"+a.Slick, b.Taut+"/"+b.Slick) }
	got := Pairs()
	slices.SortFunc(got, less)
	slices.SortFunc(fromDoc, less)
	if !slices.Equal(got, fromDoc) {
		t.Errorf("pluginlib.Pairs %v\nthe doc's \"sources\" rows %v", got, fromDoc)
	}
	seen := map[string]bool{}
	for _, p := range got {
		for _, k := range []string{Taut + "/" + p.Taut, Slick + "/" + p.Slick} {
			if seen[k] {
				t.Errorf("%s is in two pairs", k)
			}
			seen[k] = true
		}
	}
}
