package richtext

import (
	"strings"
	"testing"
)

// botStateRes is the fake resolver with members' state: U0BOT00001 and
// U0BOT00002 loaded, U0BOTDELE1 deleted, U0GONE0001 not found by
// users.info (Slack's non-existent member: known, not deleted); any other
// member-shaped id is still being fetched.
type botStateRes struct{ fakeRes }

func (botStateRes) MemberDeleted(id string) (bool, bool) {
	switch id {
	case "U0BOT00001", "U0BOT00002", "U0GONE0001":
		return false, true
	case "U0BOTDELE1":
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
		return `[{"type":"share_shortcut","block_id":"s1","function_trigger_id":"Ft0SHORT01","app_id":"A0001","owning_team_id":"T1",
			"title":"Request a review","description":"Asks the team for a review","share_url":"https://slack.com/shortcuts/Ft0SHORT01/abc",
			"workflow_id":"Wf0001","button_label":"Start","app_collaborators":["U2"],"is_workflow_app":true` + extra + `}]`
	}
	cases := []struct {
		name, blocks string
		want         []string
	}{
		{"a link trigger: Start Workflow trips its url, no byline",
			sc(`,"trigger_type":"shortcut","url":"https://slack.com/shortcuts/Ft0SHORT01/def","bot_user_id":"U0BOT00001"`),
			[]string{"enabled shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0SHORT01/def|byline=|bicon=|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
		{"no trigger_type is a link trigger; contains_ai_steps is the AI badge",
			sc(`,"url":"https://slack.com/shortcuts/Ft0SHORT01/def","bot_user_id":"U0BOT00002","contains_ai_steps":true`),
			[]string{"enabled shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0SHORT01/def|byline=|bicon=|icon=https://wf.test/app.png|ai=true|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
		{"an event trigger words its event and has no Start button",
			sc(`,"trigger_type":"event","trigger_subtype":"reaction_added","url":"https://slack.com/shortcuts/x","bot_user_id":"U0BOT00001"`),
			[]string{"enabled event Request a review|Asks the team for a review|url=|byline=Starts when a reaction is used|bicon=add-reaction|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
		{"an event subtype Slack doesn't know has no byline; a slack#/events/ prefix is stripped",
			"", nil},
		{"scheduled",
			sc(`,"trigger_type":"scheduled","bot_user_id":"U0BOT00001"`),
			[]string{"enabled scheduled Request a review|Asks the team for a review|url=|byline=Starts on a schedule|bicon=clock|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
		{"a member-shaped bot still being fetched is the loading card (isUnknownObject)",
			sc(`,"url":"https://slack.com/shortcuts/Ft0SHORT01/def","bot_user_id":"U0BOT00009"`),
			[]string{"loading shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0SHORT01/def|byline=|bicon=|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
		// getMemberById: no id, an id that isn't member-shaped and a member
		// users.info didn't find are non-existent members, not unknown
		{"no bot at all is the enabled card",
			sc(`,"url":"https://slack.com/shortcuts/Ft0SHORT01/def"`),
			[]string{"enabled shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0SHORT01/def|byline=|bicon=|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
		{"a bot id that isn't member-shaped is the enabled card",
			sc(`,"url":"https://slack.com/shortcuts/Ft0SHORT01/def","bot_user_id":"U9"`),
			[]string{"enabled shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0SHORT01/def|byline=|bicon=|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
		{"a bot users.info didn't find is the enabled card",
			sc(`,"url":"https://slack.com/shortcuts/Ft0SHORT01/def","bot_user_id":"U0GONE0001"`),
			[]string{"enabled shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0SHORT01/def|byline=|bicon=|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
		{"a service trigger names its service",
			sc(`,"trigger_type":"service","service_name":"GitHub","bot_user_id":"U0BOT00001"`),
			[]string{"enabled service Request a review|Asks the team for a review|url=|byline=Starts with a GitHub event|bicon=code|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
		{"a service trigger without a service_name: a third party",
			sc(`,"trigger_type":"service","bot_user_id":"U0BOT00001"`),
			[]string{"enabled service Request a review|Asks the team for a review|url=|byline=Starts with a third party event|bicon=code|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
		{"a deleted bot: the workflow no longer exists",
			sc(`,"url":"https://slack.com/shortcuts/Ft0SHORT01/def","bot_user_id":"U0BOTDELE1"`),
			[]string{"deleted shortcut Request a review|Asks the team for a review|url=https://slack.com/shortcuts/Ft0SHORT01/def|byline=|bicon=|icon=https://wf.test/app.png|ai=false|share=https://slack.com/shortcuts/Ft0SHORT01/abc|wf=Wf0001"}},
	}
	for _, c := range cases {
		if c.want == nil { // the prefixed subtype
			d := FromMessage(botStateRes{}, msg(t, `{"ts":"1.0","text":"x","blocks":`+sc(`,"trigger_type":"event","trigger_subtype":"slack#/events/user_joined_channel/C1","bot_user_id":"U0BOT00001"`)+`}`))
			got := shortcutOf(d)
			if len(got) != 1 || !strings.Contains(got[0], "byline=Starts when someone joins a channel|bicon=user-add") {
				t.Errorf("%s: %q", c.name, got)
			}
			d = FromMessage(botStateRes{}, msg(t, `{"ts":"1.0","text":"x","blocks":`+sc(`,"trigger_type":"event","trigger_subtype":"weather_changed","bot_user_id":"U0BOT00001"`)+`}`))
			if got := shortcutOf(d); len(got) != 1 || !strings.Contains(got[0], "byline=|bicon=|") {
				t.Errorf("%s: unknown subtype %q", c.name, got)
			}
			continue
		}
		d := FromMessage(botStateRes{}, msg(t, `{"ts":"1.0","text":"x","blocks":`+c.blocks+`}`))
		eq(t, c.name, shortcutOf(d), c.want)
	}
	// KrN7: a function_trigger_id that isn't a trigger id
	// (looksLikeTriggerId, ^Ft[A-Z0-9]{7,}$) is the text, no card
	for _, id := range []string{"Ft0001", "Ft0abcdefgh", "Wf0SHORT01", ""} {
		d := FromMessage(botStateRes{}, msg(t, `{"ts":"1.0","text":"x","blocks":[{"type":"share_shortcut","function_trigger_id":"`+id+`","title":"T"}]}`))
		if got := shortcutOf(d); len(got) != 0 {
			t.Errorf("trigger id %q: a card %q", id, got)
		}
		eq(t, "trigger id "+id, d.Blocks, []Block{{Kind: Paragraph, Kit: true, Spans: []Span{tx("UNKNOWN SHORTCUT TYPE")}}})
	}
	// in an attachment, as the real one came (a shared workflow link's unfurl)
	d := FromMessage(botStateRes{}, msg(t, `{"ts":"1.0","text":"<https://slack.com/shortcuts/Ft0SHORT01/abc>","attachments":[{"id":1,"from_url":"https://slack.com/shortcuts/Ft0SHORT01/abc","blocks":`+
		sc(`,"url":"https://slack.com/shortcuts/Ft0SHORT01/def","bot_user_id":"U0BOT00001"`)+`}]}`))
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
