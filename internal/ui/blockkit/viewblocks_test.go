package blockkit

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// A modal's body is Slack's ViewBlocks with its props: the view (as Slack's
// client keeps it: id, type, appId, botId, blocks…), containerContext
// "modal". OwlMode reads view.appId and view.blocks to add Save in Taut.
func TestViewBlocksProps(t *testing.T) {
	v := &core.View{ID: "V0OWL", Type: "modal", AppID: "A0C5L27PB9P", BotID: "B0OWL", TeamID: "T0266FRGM",
		AppInstalledTeamID: "T0266FRGM", CallbackID: "confess", Blocks: json.RawMessage(`[{"type":"section","text":{"type":"mrkdwn","text":"hi"}}]`)}
	p := modalView{v: v}.props()
	if p["containerContext"] != "modal" || p["containerId"] != "V0OWL" || p["viewContextTeamId"] != "T0266FRGM" {
		t.Errorf("props %v", p)
	}
	view := p.Map("view")
	if view.StrOr("appId", "") != "A0C5L27PB9P" || view.StrOr("id", "") != "V0OWL" || view.StrOr("botId", "") != "B0OWL" || view.StrOr("type", "") != "modal" {
		t.Errorf("view %v", view)
	}
	b, _ := json.Marshal(p)
	var back struct {
		View struct {
			Blocks []map[string]any `json:"blocks"`
		} `json:"view"`
	}
	if err := json.Unmarshal(b, &back); err != nil || len(back.View.Blocks) != 1 || back.View.Blocks[0]["type"] != "section" {
		t.Errorf("view.blocks don't reach JS as Slack's blocks: %s (%v)", b, err)
	}
	if empty := (modalView{v: &core.View{ID: "V2"}}).props().Map("view"); empty["blocks"] == nil {
		t.Error("a view with no blocks has no blocks array")
	}
}
