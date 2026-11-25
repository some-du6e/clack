package slick

import (
	"strings"
	"testing"
	"time"
)

// preambleBoth patches the channel message header under both of Slack's
// names for it, as Slick's User Pronouns (#507) and Local Time (#508) do,
// each patch adding a .preamble-mark span, without their guard against a
// second one.
const preambleBoth = `// slick-plugin {"id":"PreambleBoth","name":"Preamble Both","description":"test"}
(globalThis.__slickPlugins ??= []).push((require) => {
var module = { exports: {} }, exports = module.exports;
var import_slick = require("$slick");
class PreambleBoth extends import_slick.SlickPlugin {
  static id = "PreambleBoth";
  static pluginName = "Preamble Both";
  static description = "test: patches the channel message header under both of its names";
  static category = "appearance";
  static authors = "<@UALICE0001>";
  static settings = {};
  static requiredAPIs = [];
  start() {
    for (const name of ["BroadcastPreamble", "BaseBroadcastPreamble"]) {
      this.api.patchComponent(name, (Original) => (props) => {
        const kids = React.Children.toArray(props.children);
        kids.push(React.createElement("span", { key: "mark-" + name, className: "preamble-mark" }, name));
        return React.createElement(Original, props, kids);
      });
    }
  }
}
module.exports.default = PreambleBoth;
return module.exports.default;
});
`

// A Slick plugin that patches the channel message header under both of
// Slack's names (BroadcastPreamble since build 889ac449a32d,
// BaseBroadcastPreamble before) draws in it once, by either name: its
// patch of the old name gives way to the new one's, as Slack has only one
// of the two. Slick's API tells Taut's which plugin patched.
func TestPreambleBothNamesPatchOnce(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, preambleBoth)
	if err := h.s.SetEnabled(h.ctx(), "PreambleBoth", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the header patched", func() bool { return h.patched("BroadcastPreamble") })
	if h.patched("BaseBroadcastPreamble") {
		t.Error("the old name is reported as a component of its own")
	}
	props := map[string]any{"msg": map[string]any{"user": "UALICE0001", "ts": "1700000001.000100"}, "children": []any{}}
	for i, name := range []string{"BroadcastPreamble", "BaseBroadcastPreamble"} {
		nodes := h.render(uint64(40+i), name, props)
		if n := findNative(nodes, "BroadcastPreamble"); n == nil {
			t.Errorf("%s: no BroadcastPreamble native in %v", name, nodes)
		}
		if got := strings.Count(treeText(nodes), "Preamble"); got != 1 {
			t.Errorf("%s: %d marks, want 1: %q", name, got, treeText(nodes))
		}
	}
}
