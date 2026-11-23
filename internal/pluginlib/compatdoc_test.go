package pluginlib

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var updateCompatDoc = flag.Bool("update", false, "write docs/PLUGIN-COMPAT.md from compat.go and tools/compatdoc/index.json (TestCompatDocInStep)")

// docs/PLUGIN-COMPAT.md (getclack.club/compat) lists every plugin in the
// published Library under how far it works in Clack: compat.go's broken
// and partial rows with their reasons, every other plugin as working.
// compatPage renders it from compat.go and tools/compatdoc/index.json (a
// copy of the Library's index, refreshed when the Library is
// republished). This test fails while the page differs, and writes it
// with -update:
//
//	go test ./internal/pluginlib -run TestCompatDocInStep -update
//
// tools/compatdoc/compatdoc.py writes the same page where go test can't
// run; this test decides.
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
	want := compatPage(&ix)
	path := filepath.Join("..", "..", "docs", "PLUGIN-COMPAT.md")
	if *updateCompatDoc {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// a Windows checkout may have made the file's line ends CRLF
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if string(got) != want {
		t.Errorf("docs/PLUGIN-COMPAT.md is out of step with compat.go and the Library's index:\n%s\n"+
			"regenerate it: go test ./internal/pluginlib -run TestCompatDocInStep -update (or, from the repository root, python3 tools/compatdoc/compatdoc.py)",
			lineDiff(string(got), want, 12))
	}
}

// compatPage is docs/PLUGIN-COMPAT.md for the Library's index ix.
func compatPage(ix *Index) string {
	type item struct{ name, sys, desc, note string }
	sysName := map[string]string{Taut: "Taut", Slick: "Slick"}
	plugins := append([]Entry(nil), ix.Plugins...)
	sort.SliceStable(plugins, func(i, j int) bool {
		a, b := strings.ToLower(plugins[i].Name), strings.ToLower(plugins[j].Name)
		if a != b {
			return a < b
		}
		return plugins[i].System < plugins[j].System
	})
	var broken, partial, ok []item
	taut, slick := 0, 0
	for _, e := range plugins {
		if e.System == Taut {
			taut++
		} else {
			slick++
		}
		it := item{e.Name, sysName[e.System], strings.TrimSpace(e.Description), ""}
		switch c := CompatOf(e.System, e.ID); c.Status {
		case CompatBroken:
			it.note = strings.TrimPrefix(c.Line, "Doesn't work in Clack: ")
			broken = append(broken, it)
		case CompatPartial:
			it.note = strings.TrimPrefix(c.Line, "Partly works: ")
			partial = append(partial, it)
		default:
			ok = append(ok, it)
		}
	}
	esc := func(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
	l := []string{
		"# Plugin compatibility in Clack",
		"",
		fmt.Sprintf("Clack runs Taut's and Slick's plugins natively, from the published plugin Library (%d plugins: %d from Taut, %d from Slick). This page is how far each one works in Clack.", len(plugins), taut, slick),
		"",
		"| | Plugins |",
		"|---|---:|",
		fmt.Sprintf("| ✅ Works fully | %d |", len(ok)),
		fmt.Sprintf("| ⚠️ Partly works | %d |", len(partial)),
		fmt.Sprintf("| ❌ Doesn't work yet | %d |", len(broken)),
		"",
		"A plugin both systems ship (under the same name or not) is one row in Preferences › Plugins › Browse; here each version has its own line.",
		"Compatibility follows the newest Clack release. Each row comes from `internal/pluginlib/compat.go`, which `TestCompatMatchesDocs` keeps in step with the per-system pages, " +
			"[PLUGINS-live-taut.md](PLUGINS-live-taut.md) and [PLUGINS-live-slick.md](PLUGINS-live-slick.md). " +
			"`go test ./internal/pluginlib -run TestCompatDocInStep -update` writes this page from it and the Library's index (`tools/compatdoc/index.json`), and the test fails while the page is out of step.",
	}
	for _, sec := range []struct {
		title string
		items []item
		notes bool
	}{{"❌ Doesn't work yet", broken, true}, {"⚠️ Partly works", partial, true}, {"✅ Works fully", ok, false}} {
		l = append(l, "", "## "+sec.title, "")
		if sec.notes {
			l = append(l, "| Plugin | From | What it does | In Clack |", "|---|---|---|---|")
			for _, it := range sec.items {
				l = append(l, fmt.Sprintf("| %s | %s | %s | %s |", esc(it.name), it.sys, esc(it.desc), esc(it.note)))
			}
		} else {
			l = append(l, "| Plugin | From | What it does |", "|---|---|---|")
			for _, it := range sec.items {
				l = append(l, fmt.Sprintf("| %s | %s | %s |", esc(it.name), it.sys, esc(it.desc)))
			}
		}
	}
	l = append(l, "")
	return strings.Join(l, "\n")
}

// lineDiff lists the lines only want has (+) and only got has (-), at
// most n of each.
func lineDiff(got, want string, n int) string {
	count := map[string]int{}
	for _, l := range strings.Split(got, "\n") {
		count[l]++
	}
	var plus, minus []string
	for _, l := range strings.Split(want, "\n") {
		if count[l] > 0 {
			count[l]--
			continue
		}
		if len(plus) < n {
			plus = append(plus, "+ "+l)
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if count[l] > 0 {
			count[l]--
			if len(minus) < n {
				minus = append(minus, "- "+l)
			}
		}
	}
	return strings.Join(append(plus, minus...), "\n")
}
