package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/op"
)

// svPanelPlugin registers a settings panel (api.settings.panel) that
// counts its mounts.
const svPanelPlugin = `// slick-plugin {"id":"PanelSlick"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class P extends import_slick.SlickPlugin {
  static id = "PanelSlick";
  static pluginName = "PanelSlick";
  static description = "test";
  static requiredAPIs = [];
  start() {
    globalThis.__panelMounts = globalThis.__panelMounts ?? 0;
    function Panel() {
      React.useEffect(() => { globalThis.__panelMounts++; }, []);
      return React.createElement("div", { className: "panel-slick" }, "a synthetic panel");
    }
    this.api.settings.panel(() => React.createElement(Panel));
  }
}
return P;
});
`

// A plugin's settings panel unmounts when its Preferences page goes, as
// Slick's PluginDetail, which renders it, unmounts it with the page: back
// on the plugin list its root is gone, and opened again it mounts afresh.
func TestSlickPanelUnmountsWithItsPage(t *testing.T) {
	e := newSVIn(t, "")
	e.installCode("PanelSlick", svPanelPlugin)
	p := newSlickPage(e.a)
	defer p.close()
	draw := func() {
		gtx := ttContext(new(op.Ops), image.Pt(800, 1200))
		e.a.slackcFrame(gtx)
		p.layout(gtx, "slick_plugins")
	}
	mounted := func() bool {
		sub := p.k.subs["slickpanel.PanelSlick"]
		return sub != nil && sub.Ready()
	}
	p.layoutTab("slick_plugins")
	p.open = "PanelSlick"
	e.until(10*time.Second, "the panel on its page", func() bool { draw(); return mounted() })
	e.until(5*time.Second, "the panel's mount", func() bool { return e.jsString("globalThis.__panelMounts") == "1" })

	p.open = "" // back to the plugin list
	draw()
	if p.k.subs["slickpanel.PanelSlick"] != nil {
		t.Fatal("the panel's root outlived its page")
	}
	p.open = "PanelSlick"
	e.until(10*time.Second, "the panel on its page again", func() bool { draw(); return mounted() })
	e.until(5*time.Second, "a second mount", func() bool { return e.jsString("globalThis.__panelMounts") == "2" })
}
