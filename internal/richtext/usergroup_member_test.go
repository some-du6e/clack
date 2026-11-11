package richtext

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// groupRes is fakeRes in user group S1 (design) and not S2.
type groupRes struct{ fakeRes }

func (groupRes) InUsergroup(id string) bool { return id == "S1" }

// UserGroup (boot-shared fg3x) adds c-mrkdwn__user_group--mention, the
// self highlight, when the viewer is in the group (isUserInGroup over
// usergroups.membership): Karim's "pingroups are not highlighted"
// (2026-11-10). In mrkdwn and in rich_text blocks alike.
func TestUsergroupMentionOfYourGroup(t *testing.T) {
	var r groupRes
	for _, c := range []struct {
		name string
		d    Doc
	}{
		{"mrkdwn", Doc{Blocks: FromMrkdwn(r, "ping <!subteam^S1> and <!subteam^S2|@ops>")}},
		{"rich_text", FromMessage(r, &slack.Message{Blocks: json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
			`{"type":"usergroup","usergroup_id":"S1"},{"type":"text","text":" and "},{"type":"usergroup","usergroup_id":"S2"}]}]}]`)})},
	} {
		var mine, other *Span
		for i := range c.d.Blocks {
			for j := range c.d.Blocks[i].Spans {
				sp := &c.d.Blocks[i].Spans[j]
				if sp.Kind != Usergroup {
					continue
				}
				switch sp.ID {
				case "S1":
					mine = sp
				case "S2":
					other = sp
				}
			}
		}
		if mine == nil || other == nil {
			t.Fatalf("%s: user group spans missing: %+v", c.name, c.d.Blocks)
		}
		if !mine.Self || other.Self {
			t.Errorf("%s: your group Self %v, another group Self %v; want true, false", c.name, mine.Self, other.Self)
		}
	}
	// without the resolver method nothing is yours
	for _, b := range FromMrkdwn(R, "<!subteam^S1>") {
		for _, sp := range b.Spans {
			if sp.Kind == Usergroup && sp.Self {
				t.Errorf("Self without membership data: %+v", sp)
			}
		}
	}
}
