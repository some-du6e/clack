package richtext

import (
	"strings"
	"testing"
)

// botStateRes is the fake resolver with members' deleted state (U1 and
// U2 known, UDEL deleted; anyone else not loaded yet).
type botStateRes struct{ fakeRes }

func (botStateRes) MemberDeleted(id string) (bool, bool) {
	switch id {
	case "U1", "U2":
		return false, true
	case "UDEL":
		return true, true
	}
	return false, false
}

// share_shortcut: Slack's ShareShortcut (boot-shared KrN7) takes the
// block's own fields (createShortcutFromBlock 1BgL) and draws
// WorkflowEntity, its state from the workflow's bot member; the byline is
// WorkflowTriggerTypeLabel (b2Ud). Synthetic blocks in the shape the API
// sends (the one real one, 2026-11-18: app_collaborators, app_id,
// bot_user_id, button_label, description, function_trigger_id,
// is_workflow_app, owning_team_id, share_url, title, trigger_type,
// trigger_subtype, url, workflow_id…).
func TestShareShortcutBlock(t *testing.T) {
	defer withTriggerIcons("https://wf.test/app.png")()
	sc := func(extra string) string {
		return `[{"type":"share_shortcut","block_id":"s1","function_trigger_id":"Ft0001","app_id":"A0001","owning_team_id":"T1",
			"title":"Request a review","description":"Asks the team for a review","share_url":"https://slack.com/shortcuts/Ft0001/abc",
			"workflow_id":"Wf0001","button_label":"Start","app_collaborators":["U2"],"is_workflow_app":true` + extra + `}]`
	}
	cases := []struct {
		name, blocks string
		want         []string
	}{
		{"a link trigger: Start Workflow trips its url, no byline",
			sc(`,"trigger_type":"shortcut","url":"https://slack.com/shortcuts/Ft0001/def","bot_user_id":"U1"`),
			[]string{"enabled shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0001/def|byline=|bicon=|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0001/abc|wf=Wf0001"}},
		{"no trigger_type is a link trigger; contains_ai_steps is the AI badge",
			sc(`,"url":"https://slack.com/shortcuts/Ft0001/def","bot_user_id":"U2","contains_ai_steps":true`),
			[]string{"enabled shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0001/def|byline=|bicon=|icon=https://wf.test/app.png|ai=true|share=https://slack.com/shortcuts/Ft0001/abc|wf=Wf0001"}},
		{"an event trigger words its event and has no Start button",
			sc(`,"trigger_type":"event","trigger_subtype":"reaction_added","url":"https://slack.com/shortcuts/x","bot_user_id":"U1"`),
			[]string{"enabled event Request a review|Asks the team for a review|url=|byline=Starts when a reaction is used|bicon=add-reaction|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0001/abc|wf=Wf0001"}},
		{"an event subtype Slack doesn't know has no byline; a slack#/events/ prefix is stripped",
			"", nil},
		{"scheduled",
			sc(`,"trigger_type":"scheduled","bot_user_id":"U1"`),
			[]string{"enabled scheduled Request a review|Asks the team for a review|url=|byline=Starts on a schedule|bicon=clock|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0001/abc|wf=Wf0001"}},
		{"the bot not loaded yet is the loading card; no bot at all too",
			sc(`,"url":"https://slack.com/shortcuts/Ft0001/def","bot_user_id":"U9"`),
			[]string{"loading shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0001/def|byline=|bicon=|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0001/abc|wf=Wf0001"}},
		{"a deleted bot: the workflow no longer exists",
			sc(`,"url":"https://slack.com/shortcuts/Ft0001/def","bot_user_id":"UDEL"`),
			[]string{"deleted shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0001/def|byline=|bicon=|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0001/abc|wf=Wf0001"}},
	}
	for _, c := range cases {
		if c.want == nil { // the prefixed subtype
			d := FromMessage(botStateRes{}, msg(t, `{"ts":"1.0","text":"x","blocks":`+sc(`,"trigger_type":"event","trigger_subtype":"slack#/events/user_joined_channel/C1","bot_user_id":"U1"`)+`}`))
			got := shortcutOf(d)
			if len(got) != 1 || !strings.Contains(got[0], "byline=Starts when someone joins a channel|bicon=user-add") {
				t.Errorf("%s: %q", c.name, got)
			}
			d = FromMessage(botStateRes{}, msg(t, `{"ts":"1.0","text":"x","blocks":`+sc(`,"trigger_type":"event","trigger_subtype":"weather_changed","bot_user_id":"U1"`)+`}`))
			if got := shortcutOf(d); len(got) != 1 || !strings.Contains(got[0], "byline=|bicon=|") {
				t.Errorf("%s: unknown subtype %q", c.name, got)
			}
			continue
		}
		d := FromMessage(botStateRes{}, msg(t, `{"ts":"1.0","text":"x","blocks":`+c.blocks+`}`))
		eq(t, c.name, shortcutOf(d), c.want)
	}
	// in an attachment, as the real one came (a shared workflow link's unfurl)
	d := FromMessage(botStateRes{}, msg(t, `{"ts":"1.0","text":"<https://slack.com/shortcuts/Ft0001/abc>","attachments":[{"id":1,"from_url":"https://slack.com/shortcuts/Ft0001/abc","blocks":`+
		sc(`,"url":"https://slack.com/shortcuts/Ft0001/def","bot_user_id":"U1"`)+`}]}`))
	found := false
	for _, b := range d.Blocks {
		if b.Att != nil {
			found = len(shortcutOf(Doc{Blocks: b.Att.Text})) == 1
		}
	}
	if !found {
		t.Errorf("attachment: no card in %s", dump(d.Blocks))
	}
}
