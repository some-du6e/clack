package slick

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Acceptance: every plugin in Slick's src/plugins/, built with Slick's own
// build (the slick workflow runs internal/slick/build/build.mjs and sets
// SLICK_PLUGINS_DIR), imported unmodified the way a user imports it, must
// start, register its hooks, do its thing against the synthetic workspace,
// and leave nothing behind when it stops.

func pluginsDir(t *testing.T) string {
	dir := os.Getenv("SLICK_PLUGINS_DIR")
	if dir == "" {
		t.Skip("SLICK_PLUGINS_DIR not set: the slick workflow builds Slick's plugins and runs this")
	}
	return dir
}

func builtPlugins(t *testing.T, dir string) []string {
	files, _ := filepath.Glob(filepath.Join(dir, "*.js"))
	var ids []string
	for _, f := range files {
		b := filepath.Base(f)
		if strings.HasSuffix(b, ".main.js") || strings.HasSuffix(b, ".user.js") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(b, ".js"))
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		t.Fatalf("no plugins in %s", dir)
	}
	return ids
}

// enableWith are settings a plugin needs on to do something observable.
var enableWith = map[string]map[string]any{}

// before runs before the plugin is imported (seed storage, answers for fetch).
var before = map[string]func(h *harness){}

// pluginCheck does a plugin's thing after it started.
type pluginCheck func(t *testing.T, h *harness)

var checks = map[string]pluginCheck{}

// quiet are plugins that, by design, register nothing observable in the
// renderer in Clack's synthetic workspace at start (they only act on a
// setting, a main-half event or an incoming event); their check proves
// what they do instead.
var quiet = map[string]string{}

func TestAcceptance(t *testing.T) {
	dir := pluginsDir(t)
	ids := builtPlugins(t, dir)
	var pass, fail []string
	for _, id := range ids {
		ok := t.Run(id, func(t *testing.T) { acceptOne(t, dir, id) })
		if ok {
			pass = append(pass, id)
		} else {
			fail = append(fail, id)
		}
	}
	t.Logf("ACCEPT %d/%d pass: %s", len(pass), len(ids), strings.Join(pass, " "))
	if len(fail) > 0 {
		t.Logf("ACCEPT failing: %s", strings.Join(fail, " "))
	}
}

func acceptOne(t *testing.T, dir, id string) {
	h := newHarness(t)
	if f := before[id]; f != nil {
		f(h)
	}
	base := h.state().registrations()
	h.importFile(filepath.Join(dir, id+".js"))
	if _, err := os.Stat(filepath.Join(dir, id+".main.js")); err == nil {
		h.importFile(filepath.Join(dir, id+".main.js"))
		if !h.plugin(id).HasMain {
			t.Fatal("main half not recorded")
		}
	}
	for k, v := range enableWith[id] {
		if err := h.s.SetOption(h.ctx(), id, k, v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
	if err := h.s.SetEnabled(h.ctx(), id, true); err != nil {
		t.Fatal(err)
	}
	info := h.plugin(id)
	if !info.Running || info.StartError != "" {
		t.Fatalf("didn't start: running=%v error=%q", info.Running, info.StartError)
	}
	// let start's background work settle (lazy discovery, storage loads)
	time.Sleep(150 * time.Millisecond)
	started := h.state()
	added := diff(started.registrations(), base)
	t.Logf("registered: %s", strings.Join(added, " "))
	if len(added) == 0 && quiet[id] == "" && !has(started.Mains, id) {
		t.Errorf("registered nothing")
	}
	if c := checks[id]; c != nil {
		c(t, h)
	} else {
		t.Logf("no behaviour check yet (start + hooks only)")
	}
	if err := h.s.SetEnabled(h.ctx(), id, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	stopped := h.state()
	if left := diff(stopped.registrations(), base); len(left) > 0 {
		t.Errorf("left behind after stop: %s", strings.Join(left, " "))
	}
	if has(stopped.Running, id) || has(stopped.Mains, id) {
		t.Errorf("still running after stop")
	}
	// enable again: a second start must work (stale state, double registration)
	if err := h.s.SetEnabled(h.ctx(), id, true); err != nil {
		t.Fatal(err)
	}
	if info := h.plugin(id); !info.Running {
		t.Errorf("didn't restart: %q", info.StartError)
	}
	if again := diff(h.state().registrations(), base); len(again) != len(added) {
		t.Logf("second start registered %d (first %d): %s", len(again), len(added), strings.Join(again, " "))
	}
}

// diff is a minus b, as multisets.
func diff(a, b []string) []string {
	count := map[string]int{}
	for _, x := range b {
		count[x]++
	}
	var out []string
	for _, x := range a {
		if count[x] > 0 {
			count[x]--
			continue
		}
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

// ---- helpers for checks ----

func (h *harness) jsString(body string) string {
	var s string
	h.jsInto(body, &s)
	return s
}

func (h *harness) jsBool(body string) bool {
	var b bool
	h.jsInto(body, &b)
	return b
}

// renderText renders a patched component and returns its text.
func (h *harness) renderText(root uint64, name string, props any) string {
	return treeText(h.render(root, name, props))
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

var _ = fmt.Sprint
