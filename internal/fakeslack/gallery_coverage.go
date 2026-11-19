package fakeslack

// The gallery's coverage part (docs/BLOCKKIT-COVERAGE.md): the blocks the
// coverage round added, each in the shapes Slack's API sends, and how a
// block Slack can't draw falls back. Synthetic only.

import (
	"strconv"
	"strings"
	"time"
)

func init() { RegisterGallery("12-coverage", galleryCoverage) }

func galleryCoverage(e GalleryEnv) []GalleryMessage {
	u1, u2, u3 := e.Users[0], e.Users[1], e.Users[min(2, len(e.Users)-1)]
	now := time.Now().Unix()
	at := func(sec int64) string { return strconv.FormatInt(sec, 10) }
	r := strings.NewReplacer("{U1}", u1, "{U2}", u2, "{U3}", u3, "{ICON72}", e.Img(72, 72, "callapp"),
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
		{Name: "coverage/loading", Text: "loading", Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"A loading block:"}},{"type":"loading"}]`},
		{Name: "coverage/rich_text header and divider", Text: "rich text headers", Blocks: `[{"type":"rich_text","elements":[
			{"type":"rich_text_header","level":1,"elements":[{"type":"text","text":"rich_text_header level 1"}]},
			{"type":"rich_text_section","elements":[{"type":"text","text":"Its first header has no top margin; later ones keep 1em."}]},
			{"type":"rich_text_divider"},
			{"type":"rich_text_header","level":2,"elements":[{"type":"text","text":"Level 2 (the default)"}]},
			{"type":"rich_text_section","elements":[{"type":"text","text":"under level 2"}]},
			{"type":"rich_text_header","level":3,"elements":[{"type":"text","text":"Level 3 is bold, not black"}]},
			{"type":"rich_text_section","elements":[{"type":"text","text":"under level 3"}]}]}]`},
		{Name: "coverage/fallbacks", Text: "fallbacks", Blocks: `[
			{"type":"section","text":{"type":"mrkdwn","text":"Next: a fallback_canary (its fallback list draws), an unknown type with an empty fallback (nothing), and expandable (no renderable: the card)."}},
			{"type":"fallback_canary","fallback":[{"type":"section","text":{"type":"mrkdwn","text":"*fallback_canary* drew its fallback"}},{"type":"divider"}]},
			{"type":"future_widget","fallback":[]},
			{"type":"expandable"}]`},
		{Name: "coverage/condition", Text: "condition", Blocks: `[{"type":"condition","cases":[
			{"conditions":[{"type":"client","op":"=","value":"ios"}],"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"ios only"}}]},
			{"conditions":[{"type":"client","op":"=","value":"desktop"}],"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"*condition*: the desktop case drew"}}]}],
			"default":[{"type":"section","text":{"type":"mrkdwn","text":"default"}}]}]`},
	}
}
