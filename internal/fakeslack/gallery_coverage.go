package fakeslack

// The gallery's coverage part (docs/BLOCKKIT-COVERAGE.md): the blocks the
// coverage round added, each in the shapes Slack's API sends, and how a
// block Slack can't draw falls back. Synthetic only.

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

func init() { RegisterGallery("12-coverage", galleryCoverage) }

// GalleryHTTPImage is coverage/http image's image_url: one of the fake's
// pictures on a third-party plain-http host, which only the image proxy
// serves (ProxiedImageURL).
var GalleryHTTPImage = ProxiedImageURL("http://icons.example.net", 64, 64, "weather-sunny")

func galleryCoverage(e GalleryEnv) []GalleryMessage {
	u1, u2, u3 := e.Users[0], e.Users[1], e.Users[min(2, len(e.Users)-1)]
	now := time.Now().Unix()
	at := func(sec int64) string { return strconv.FormatInt(sec, 10) }
	r := strings.NewReplacer("{U1}", u1, "{U2}", u2, "{U3}", u3, "{C1}", e.Channel, "{HTTPICON}", GalleryHTTPImage, "{ICON72}", e.Img(72, 72, "callapp"),
		"{HERO}", e.Img(400, 300, "cardhero"), "{CARDICON}", e.Img(72, 72, "cardicon"),
		"{RUN}", at(now-25*60), "{END}", at(1762506000), "{ENDS}", at(1762506000-47*60))
	b := func(s string) string { return r.Replace(s) }
	icons := `"app_icon_urls":{"image_32":"{ICON72}","image_72":"{ICON72}"}`
	return []GalleryMessage{
		{Name: "coverage/call ended", Text: "Call ended", Blocks: b(`[{"type":"call","block_id":"cl1","call_id":"R0GALLERY1","api_decoration_available":false,
			"call":{"media_backend_type":"platform_call","v1":{"id":"R0GALLERY1","app_id":"A0GALCALL","name":"Weekly sync",` + icons + `,
			"date_start":{ENDS},"date_end":{END},"has_ended":true,"display_id":"812-3344-5566","join_url":"https://call.example.com/j/812",
			"desktop_app_join_url":"callexample://join/812","created_by":"{U1}","channels":[],"is_dm_call":false,"was_accepted":false,
			"was_missed":false,"was_rejected":false,"active_participants":[],"all_participants":[{"slack_id":"{U1}"},{"slack_id":"{U2}"}]}}}]`)},
		{Name: "coverage/call running", Text: "Call started", Blocks: b(`[{"type":"call","block_id":"cl2","call_id":"R0GALLERY2",
			"call":{"media_backend_type":"platform_call","v1":{"id":"R0GALLERY2","app_id":"A0GALCALL","name":"Design review",` + icons + `,
			"date_start":{RUN},"date_end":0,"join_url":"https://call.example.com/j/913","created_by":"{U2}",
			"active_participants":[{"slack_id":"{U1}"},{"slack_id":"{U2}"},{"slack_id":"{U3}"},
				{"external_id":"guest-1","display_name":"Guest One"},{"external_id":"guest-2","display_name":"Guest Two"},
				{"external_id":"guest-3","display_name":"Guest Three"},{"external_id":"guest-4","display_name":"Guest Four"}],
			"all_participants":[]}}}]`)},
		{Name: "coverage/call waiting", Text: "Call started", Blocks: b(`[{"type":"call","block_id":"cl3","call_id":"R0GALLERY3",
			"call":{"media_backend_type":"platform_call","v1":{"id":"R0GALLERY3","date_start":{RUN},"date_end":0,
			"join_url":"https://call.example.com/j/914","active_participants":[],"all_participants":[]}}}]`)},
		{Name: "coverage/alert", Text: "alerts", Blocks: `[
			{"type":"alert","text":{"type":"mrkdwn","text":"*default*: no level set"}},
			{"type":"alert","level":"info","text":{"type":"mrkdwn","text":"*info*: the deploy window opens at 5pm"}},
			{"type":"alert","level":"success","text":{"type":"plain_text","text":"success: the build passed"}},
			{"type":"alert","level":"warning","text":{"type":"mrkdwn","text":"*warning*: 2 checks are flaky"}},
			{"type":"alert","level":"error","text":{"type":"mrkdwn","text":"*error*: the release failed, <https://example.com|see the log>"}}]`},
		{Name: "coverage/callout", Text: "callouts", Blocks: `[
			{"type":"callout","background_color":"indigo","child_blocks":[{"type":"header","text":{"type":"plain_text","text":"Callout :sparkles:","emoji":true}},
				{"type":"section","text":{"type":"mrkdwn","text":"Child blocks render bare inside: their margins collapse, 12px padding all round."}},
				{"type":"divider"},{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"rich_text child"}]}]}]},
			{"type":"callout","background_color":"jade","child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"jade"}}]},
			{"type":"callout","background_color":"orange","child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"orange"}}]},
			{"type":"callout","background_color":"gray","child_blocks":[]},
			{"type":"callout","child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"no colour: green"}}]}]`},
		{Name: "coverage/container", Text: "containers", Blocks: b(`[
			{"type":"container","title":{"type":"plain_text","text":"Standard container"},"subtitle":{"type":"mrkdwn","text":"a *mrkdwn* subtitle"},
			 "icon":{"type":"image","image_url":"{ICON72}","alt_text":"icon"},"has_header_divider":true,
			 "child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Child blocks sit 12px in; the first has no top margin."}},
				{"type":"context","elements":[{"type":"mrkdwn","text":"a context child"}]}]},
			{"type":"container","rich_text_title":{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Collapsible ","style":{"bold":true}},{"type":"emoji","name":"package"}]}]},
			 "subtitle":{"type":"plain_text","text":"click the header"},"is_collapsible":true,"width":"wide",
			 "child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Shown until the header is clicked."}}]},
			{"type":"container","title":{"type":"plain_text","text":"Narrow, collapsed by default"},"is_collapsible":true,"default_collapsed":true,"width":"narrow",
			 "child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"hidden"}}]}]`)},
		// the loading block's spinner turns for ever (as in Slack), so it
		// lives in a thread: the channel itself must idle (TestIdleFrames)
		{Name: "coverage/loading", Text: "A loading block lives in this thread",
			Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"A loading block lives in this thread."}}]`,
			Replies: []GalleryMessage{{Name: "coverage/loading reply", Text: "loading",
				Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"A loading block:"}},{"type":"loading"}]`}}},
		{Name: "coverage/rich_text header and divider", Text: "rich text headers", Blocks: `[{"type":"rich_text","elements":[
			{"type":"rich_text_header","level":1,"elements":[{"type":"text","text":"rich_text_header level 1"}]},
			{"type":"rich_text_section","elements":[{"type":"text","text":"Its first header has no top margin; later ones keep 1em."}]},
			{"type":"rich_text_divider"},
			{"type":"rich_text_header","level":2,"elements":[{"type":"text","text":"Level 2 (the default)"}]},
			{"type":"rich_text_section","elements":[{"type":"text","text":"under level 2"}]},
			{"type":"rich_text_header","level":3,"elements":[{"type":"text","text":"Level 3 is bold, not black"}]},
			{"type":"rich_text_section","elements":[{"type":"text","text":"under level 3"}]}]}]`},
		{Name: "coverage/share_shortcut", Text: "A workflow link", Blocks: b(`[{"type":"share_shortcut","block_id":"ss1","function_trigger_id":"Ft0GALLERY1",
			"app_id":"A0GALWF","owning_team_id":"T0FAKE","title":"Request time off","description":"Pick your dates and a reason; your manager gets a request to approve.",
			"share_url":"https://slack.com/shortcuts/Ft0GALLERY1/abc","url":"https://slack.com/shortcuts/Ft0GALLERY1/def","workflow_id":"Wf0GALLERY1",
			"trigger_type":"shortcut","bot_user_id":"{U1}","is_workflow_app":true,"app_collaborators":["{U2}"],"button_label":"Start"}]`)},
		{Name: "coverage/share_shortcut attachment", Text: "<https://slack.com/shortcuts/Ft0GALLERY2/abc>", From: u2,
			Attachments: b(`[{"id":1,"from_url":"https://slack.com/shortcuts/Ft0GALLERY2/abc","fallback":"Workflow: Weekly standup",
			"blocks":[{"type":"share_shortcut","block_id":"ss2","function_trigger_id":"Ft0GALLERY2","app_id":"A0GALWF","title":"Weekly standup",
			"description":"Answers three questions and posts them to the channel.","share_url":"https://slack.com/shortcuts/Ft0GALLERY2/abc",
			"url":"https://slack.com/shortcuts/Ft0GALLERY2/def","workflow_id":"Wf0GALLERY2","trigger_type":"shortcut","bot_user_id":"{U1}","contains_ai_steps":true}]}]`)},
		{Name: "coverage/share_shortcut event", Text: "An event workflow", Blocks: b(`[{"type":"share_shortcut","block_id":"ss3","function_trigger_id":"Ft0GALLERY3",
			"title":"Thank you note","description":"Sends a thank-you when someone reacts with :pray:.","share_url":"https://slack.com/shortcuts/Ft0GALLERY3/abc",
			"workflow_id":"Wf0GALLERY3","trigger_type":"event","trigger_subtype":"reaction_added","bot_user_id":"{U1}"}]`)},
		{Name: "coverage/fallbacks", Text: "fallbacks", Blocks: `[
			{"type":"section","text":{"type":"mrkdwn","text":"Next: a fallback_canary (its fallback list draws), an unknown type with an empty fallback (nothing), and expandable (no renderable: the card)."}},
			{"type":"fallback_canary","fallback":[{"type":"section","text":{"type":"mrkdwn","text":"*fallback_canary* drew its fallback"}},{"type":"divider"}]},
			{"type":"future_widget","fallback":[]},
			{"type":"expandable"}]`},
		// cards (Card, 7Xkb): every part; the slack_icon glyph with three
		// actions (the danger one left, the primary last); a title alone
		{Name: "coverage/card", Text: "card", Blocks: b(`[{"type":"card","block_id":"card1",
			"hero_image":{"type":"image","image_url":"{HERO}","alt_text":"a harbour at dusk"},
			"icon":{"type":"image","image_url":"{CARDICON}","alt_text":"app"},
			"title":{"type":"mrkdwn","text":"*card*: Quarterly planning"},"subtitle":{"type":"mrkdwn","text":"Due Friday"},
			"body":{"type":"mrkdwn","text":"Review the roadmap and pick the _three_ bets for next quarter."},
			"subtext":{"type":"mrkdwn","text":"Updated 2 hours ago"},
			"actions":[{"type":"button","action_id":"card_open","text":{"type":"plain_text","text":"Open"},"style":"primary","value":"open"},
				{"type":"button","action_id":"card_later","text":{"type":"plain_text","text":"Later"},"value":"later"}]}]`)},
		{Name: "coverage/card slack icon", Text: "card with a slack icon", Blocks: `[{"type":"card","block_id":"card2",
			"slack_icon":{"name":"calendar"},"title":{"type":"plain_text","text":"Design review"},"subtitle":{"type":"plain_text","text":"Tomorrow at 10:00"},
			"actions":[{"type":"button","action_id":"card_decline","text":{"type":"plain_text","text":"Decline"},"style":"danger","value":"no"},
				{"type":"button","action_id":"card_accept","text":{"type":"plain_text","text":"Accept"},"style":"primary","value":"yes"},
				{"type":"button","action_id":"card_maybe","text":{"type":"plain_text","text":"Maybe"},"value":"maybe"}]}]`},
		{Name: "coverage/card title only", Text: "card with a title", Blocks: `[{"type":"card","block_id":"card3","title":{"type":"plain_text","text":"Just a title"}}]`},
		// a check list: the 16px checkbox-filled / square icons, an item's
		// own checked over the list's, a nested list 28px further in
		{Name: "coverage/check list", Text: "check list", Blocks: `[{"type":"rich_text","elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"check list:","style":{"bold":true}}]},
			{"type":"rich_text_list","style":"check","checked":true,"elements":[
				{"type":"rich_text_section","elements":[{"type":"text","text":"Book the room"}]},
				{"type":"rich_text_section","checked":false,"elements":[{"type":"text","text":"Send the agenda, a long item that wraps back under its checkbox when the column is narrow enough"}]}]},
			{"type":"rich_text_list","style":"check","indent":1,"elements":[
				{"type":"rich_text_section","elements":[{"type":"text","text":"Draft it first"}]}]}]}]`},
		// a message unfurl whose image block is a 64×64 icon on a plain-http
		// third-party host, with the sizes Slack's API adds (a weather
		// bot's forecast, shared by link): Slack's client loads it through
		// its image proxy (getImgProxyUrl), as Clack must
		{Name: "coverage/http image", Text: "<https://fakeclub.slack.com/archives/{C1}/p1762500000000100>", From: u2,
			Attachments: b(`[{"id":1,"is_msg_unfurl":true,"author_id":"{U1}","channel_id":"{C1}","ts":"1762500000.000100",
			"from_url":"https://fakeclub.slack.com/archives/{C1}/p1762500000000100","original_url":"https://fakeclub.slack.com/archives/{C1}/p1762500000000100",
			"fallback":"Forecast: sunny, 24°C","blocks":[
			{"type":"image","block_id":"wx_icon","image_url":"{HTTPICON}","alt_text":"Sunny","title":{"type":"plain_text","text":"Sunny"},
			 "image_width":64,"image_height":64,"image_bytes":1613,"is_animated":true},
			{"type":"header","text":{"type":"plain_text","text":"Forecast"}},
			{"type":"section","text":{"type":"mrkdwn","text":"*24°C*, clear all day"}}]}]`)},
		{Name: "coverage/condition", Text: "condition", Blocks: `[{"type":"condition","cases":[
			{"conditions":[{"type":"client","op":"=","value":"ios"}],"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"ios only"}}]},
			{"conditions":[{"type":"client","op":"=","value":"desktop"}],"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"*condition*: the desktop case drew"}}]}],
			"default":[{"type":"section","text":{"type":"mrkdwn","text":"default"}}]}]`},
	}
}

// the inline rich_text file slugs: a message whose rich text links its own
// files (Slack's File slug: the file's name and type icon) and a deleted
// one (a tombstone in the message's files: the disabled trash slug)
func init() { RegisterGalleryMessages("13-coverage-files", galleryFileSlugs) }

func galleryFileSlugs(s *Server) []slack.Message {
	u := ""
	if len(s.users) > 1 {
		u = s.users[1].ID
	}
	el := func(id, name string) string {
		return `{"type":"file","file_id":"` + id + `","url":"` + s.URL + `files/` + u + `/` + id + `/` + name + `","text":"` + name + `"}`
	}
	txt := func(t string) string { return `{"type":"text","text":"` + t + `"}` }
	blocks := `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` + strings.Join([]string{
		txt("Inline file slugs: "), el("F0GALSLUG1", "q3-report.pdf"), txt(", "), el("F0GALSLUG2", "standup.m4a"),
		txt(" and a deleted one: "), el("F0GALSLUG9", "gone.txt")}, ",") + `]}]}]`
	return []slack.Message{{
		Type: "message", User: u, Text: "Inline file slugs", Blocks: json.RawMessage(blocks),
		Files: []slack.File{
			{ID: "F0GALSLUG1", Name: "q3-report.pdf", Title: "Q3 report", Filetype: "pdf", Mimetype: "application/pdf", PrettyType: "PDF", Size: 245000, Mode: "hosted"},
			{ID: "F0GALSLUG2", Name: "standup.m4a", Title: "", Filetype: "m4a", Mimetype: "audio/mp4", Subtype: "slack_audio", Size: 52000, Mode: "hosted"},
			{ID: "F0GALSLUG9", Mode: "tombstone"},
		},
	}}
}
