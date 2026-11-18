package ui

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// svReactionPlugin patches Reaction (as Who Reacted does) and draws it
// unchanged.
const svReactionPlugin = `// slick-plugin {"id":"ReactionSlick"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class P extends import_slick.SlickPlugin {
  static id = "ReactionSlick";
  static pluginName = "ReactionSlick";
  static description = "test";
  static requiredAPIs = [];
  start() {
    this.api.patchComponent("Reaction", (Original) => (props) => React.createElement(Original, props));
  }
}
return P;
});
`

// The plugin bench and the perf plugins job wrap slackc's bridge once the
// runtime is up (countingBridge), then enable the plugins they measure.
// Their patches must still reach slackc: whether a runtime may publish
// them is the UI's attachment (tautOwner), not which bridge value slackc
// holds. 1f17ed91 compared the two, so every patch registered after the
// wrap was dropped: none of the bench's 50 variants patched a component,
// and twelve plugins lost their checks in Taut and Slick.
func TestSlickViewWrappedBridgeStillPatches(t *testing.T) {
	e := newSVIn(t, "")
	e.installCode("PassSlick", svPassPlugin)
	// a patch reached slackc, so Attached's post has bound the runtime
	e.until(5*time.Second, "MAMenu patched", func() bool { return slackc.Patched("MAMenu") })
	var calls benchCalls
	slackc.SetBridge(countingBridge{e.a.Taut().Bridge(), &calls}) // as plugin_bench_test.go does

	e.installCode("ReactionSlick", svReactionPlugin)
	e.until(5*time.Second, "Reaction patched with the bridge wrapped", func() bool { return slackc.Patched("Reaction") })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := e.s.SetEnabled(ctx, "PassSlick", false); err != nil {
		t.Fatal(err)
	}
	e.until(5*time.Second, "MAMenu unpatched once PassSlick stopped", func() bool { return !slackc.Patched("MAMenu") })
	if !slackc.Patched("Reaction") {
		t.Fatal("Reaction lost its patch when PassSlick stopped")
	}
}
