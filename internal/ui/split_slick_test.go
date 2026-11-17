package ui

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Plugins work in the split view as in the main one (split.go): with
// BetterTypingIndicator on, someone typing in the split view's channel
// (not the main view's) shows through the plugin's TypingNames in the
// split view's composer.
func TestSlickViewSplitView(t *testing.T) {
	e := newSV(t)
	e.install("BetterTypingIndicator", map[string]any{"showAvatars": false})
	e.until(5*time.Second, "TypingNames patched", func() bool { return slackc.Patched("TypingNames") })
	e.a.OpenSplit("c/" + svRandom)
	e.until(8*time.Second, "#random's composer in the split view", func() bool {
		cv := e.a.split.pane.convos[svRandom+"/"]
		return e.a.splitTop() == Route("c/"+svRandom) && cv != nil && !cv.compRect.Empty()
	})
	if cv := e.a.convos[svRandom+"/"]; cv != nil {
		t.Error("the split view's channel drew in the main view's conversations")
	}
	raw, _ := json.Marshal(map[string]any{"type": "user_typing", "channel": svRandom, "user": svAlice})
	e.ca.Inject(slack.Event{Type: "user_typing", Channel: json.RawMessage(`"` + svRandom + `"`), Raw: raw})
	e.until(6*time.Second, "the typing line through the plugin in the split view", func() bool {
		return e.tree("slick-typing-indicator") != ""
	})
	// with a plugin runtime up the sidebar's row menu keeps its split row
	e.rightClick(ttGen)
	e.until(5*time.Second, "#general's row menu", func() bool { return e.lastMenu(ttGen) != nil })
	if _, ok := menuItem(e.lastMenu(ttGen), "channel_ctx_open_split"); !ok {
		t.Errorf("no Open in split view in the sidebar menu: %v", e.lastMenu(ttGen))
	}
}

// svPassPlugin, in the shape Slick's build:plugin writes, replaces the
// root message menu's onSelectAction with one that records each pick and
// hands it on to the menu's own (falsy), so Clack's row runs.
const svPassPlugin = `// slick-plugin {"id":"PassSlick"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class P extends import_slick.SlickPlugin {
  static id = "PassSlick";
  static pluginName = "PassSlick";
  static description = "test";
  static requiredAPIs = [];
  start() {
    globalThis.__passed = [];
    this.api.patchComponent("MAMenu", (Original) => (props) => {
      if (props.autoClogProps?.uiComponentName !== "message_actions_menu" || typeof props.onSelectAction !== "function") return React.createElement(Original, props);
      return React.createElement(Original, { ...props, onSelectAction: (event, action, context) => {
        globalThis.__passed.push(action?.id);
        return props.onSelectAction(event, action, context);
      } });
    });
  }
}
return P;
});
`

// A row a plugin's onSelectAction passes on runs in the view container
// its menu opened in, where Slack's dispatchMessageAction runs it: Reply
// in thread on a message's menu in the split view opens the thread there,
// and the main view's thread pane stays as it was. (No built plugins:
// this runs anywhere.)
func TestMessageMenuPickInSplitView(t *testing.T) {
	e := newSVIn(t, "")
	e.installCode("PassSlick", svPassPlugin)
	e.until(5*time.Second, "MAMenu patched", func() bool { return slackc.Patched("MAMenu") })
	e.a.OpenSplit("c/" + svRandom)
	e.until(8*time.Second, "#random in the split view", func() bool { return e.a.splitTop() == Route("c/"+svRandom) })
	const ts = "1700000006.000600"
	msg := core.Message{Channel: svRandom, TS: ts, User: svBob, Text: "a message in the split view"}
	// opened from the split view, as its message list's right-click does
	e.a.inPane(paneSplit, func() {
		e.a.ShowOverlay(popups.NewMessageContextMenu(e.a, image.Pt(900, 300), msg, popups.InChannel, popups.MessageMenuOpts{}))
	})
	var m *popups.Menu
	i := -1
	e.until(8*time.Second, "the menu as the plugin laid it out", func() bool {
		if m = e.topMenu(); m == nil {
			return false
		}
		i = menuIndex(m.Items(), "comment")
		return i >= 0 && len(m.RowRects()) == len(m.Items()) && !m.RowRects()[i].Empty() &&
			e.tree(`"onSelectAction":{"$fn"`) != ""
	})
	for k := 0; k < 3; k++ {
		e.frame()
	}
	main := e.a.secondary
	e.clickRect(m.RowRects()[i])
	e.until(8*time.Second, "the thread in the split view", func() bool { return e.a.splitTop() == Route("c/"+svRandom+"/t/"+ts) })
	if got := e.jsString("globalThis.__passed"); got != "comment" {
		t.Errorf("the plugin was asked about %q, want comment", got)
	}
	if s := e.a.secondary; s.kind != main.kind || s.channel != main.channel || s.ts != main.ts {
		t.Errorf("the main view's secondary changed: %v %s/%s, was %v %s/%s", s.kind, s.channel, s.ts, main.kind, main.channel, main.ts)
	}
}

// A row a plugin's onSelectAction passes on runs with the modifiers of
// the click that picked it, as Slack's dispatchMessageAction runs in the
// click's handler: a Ctrl+Alt (⌘⌥ on macOS) click on Reply in thread in
// the main view opens the thread in the split view (Slack's shouldSplit),
// and the main view's thread pane stays as it was.
func TestMessageMenuPickSplitClick(t *testing.T) {
	e := newSVIn(t, "")
	e.installCode("PassSlick", svPassPlugin)
	e.until(5*time.Second, "MAMenu patched", func() bool { return slackc.Patched("MAMenu") })
	const ts = "1700000008.000800"
	msg := core.Message{Channel: ttGen, TS: ts, User: svBob, Text: "a message in the main view"}
	e.a.ShowOverlay(popups.NewMessageContextMenu(e.a, image.Pt(900, 300), msg, popups.InChannel, popups.MessageMenuOpts{}))
	var m *popups.Menu
	i := -1
	e.until(8*time.Second, "the menu as the plugin laid it out", func() bool {
		if m = e.topMenu(); m == nil {
			return false
		}
		i = menuIndex(m.Items(), "comment")
		return i >= 0 && len(m.RowRects()) == len(m.Items()) && !m.RowRects()[i].Empty() &&
			e.tree(`"onSelectAction":{"$fn"`) != ""
	})
	for k := 0; k < 3; k++ {
		e.frame()
	}
	main := e.a.secondary
	e.clickRectMods(m.RowRects()[i], shortcut.Mod()|key.ModAlt)
	e.until(8*time.Second, "the thread in the split view", func() bool { return e.a.splitTop() == Route("c/"+ttGen+"/t/"+ts) })
	if got := e.jsString("globalThis.__passed"); got != "comment" {
		t.Errorf("the plugin was asked about %q, want comment", got)
	}
	if s := e.a.secondary; s.kind != main.kind || s.channel != main.channel || s.ts != main.ts {
		t.Errorf("the main view's secondary changed: %v %s/%s, was %v %s/%s", s.kind, s.channel, s.ts, main.kind, main.channel, main.ts)
	}
}
