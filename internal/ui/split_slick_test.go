package ui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
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
