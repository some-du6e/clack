package fakeslack

// The gallery's layout blocks (bk-layout): section (text, fields, every
// accessory position), header, divider, context, image, video, file,
// markdown, rich_text (every element and style), table, and the limits
// (Show more in threads, invalid and unknown blocks). Synthetic only.

import (
	"strconv"
	"strings"
)

func init() { RegisterGallery("10-layout", galleryLayout) }

func galleryLayout(e GalleryEnv) []GalleryMessage {
	u1, u2 := e.Users[0], e.Users[1]
	emo := "smile"
	if len(e.CustomEmoji) > 0 {
		emo = e.CustomEmoji[0]
	}
	r := strings.NewReplacer(
		"{U1}", u1, "{U2}", u2, "{C1}", e.Channel, "{S1}", e.Usergroup, "{EMO}", emo,
		"{IMG88}", e.Img(176, 176, "acc"),
		"{IMG20}", e.Img(40, 40, "ctx"),
		"{IMGW}", e.Img(1200, 600, "wide"),
		"{IMGT}", e.Img(300, 900, "tall"),
		"{IMGS}", e.Img(160, 120, "small"),
		"{IMGN}", e.Img(480, 320, "nodims"),
		"{THUMB}", e.Img(640, 360, "vthumb"),
		"{ICON}", e.Img(32, 32, "vicon"),
	)
	b := func(s string) string { return r.Replace(s) }
	long := "Hiya! To join, say 'circle' or 'square' along with an image, a mention of someone, or 'me' to use your own picture. " +
		"Or just use the dropdown below! Say 'ring' (or tick it) for a ring around the picture, tick auto-update and the bot " +
		"re-makes your emoji whenever you change your profile photo, and pick as many effects as you like from the list underneath."
	return []GalleryMessage{
		{Name: "layout/section mrkdwn", Blocks: b(`[{"type":"section","text":{"type":"mrkdwn","text":"*Section mrkdwn*: *bold* _italic_ ~strike~ ` + "`code`" + ` <https://example.com|a link> <@{U1}> <#{C1}> <!subteam^{S1}> <!here> :tada: :{EMO}:\n> a quoted line\n` + "```" + `preformatted\n  block` + "```" + `"}}]`)},
		{Name: "layout/section plain_text", Blocks: b(`[
			{"type":"section","text":{"type":"plain_text","text":"plain_text, emoji true :wave: *not bold*","emoji":true}},
			{"type":"section","text":{"type":"plain_text","text":"plain_text, emoji false :wave: stays as typed","emoji":false}}]`)},
		{Name: "layout/section fields", Blocks: b(`[
			{"type":"section","text":{"type":"mrkdwn","text":"*Fields* under text (12px above, 16px gutter, max 600 wide)"},"fields":[
				{"type":"mrkdwn","text":"*Owner*\n<@{U1}>"},{"type":"mrkdwn","text":"*Status*\n:large_green_circle: Live"},
				{"type":"plain_text","text":"Plain field","emoji":true},{"type":"mrkdwn","text":"*Long field*\nWraps inside its half of the grid when the text is longer than the column"},
				{"type":"mrkdwn","text":"*Odd one out*\nfifth field"}]},
			{"type":"section","fields":[{"type":"mrkdwn","text":"*Fields only*"},{"type":"mrkdwn","text":"no text above"}]}]`)},
		{Name: "layout/section accessory image", Blocks: b(`[{"type":"section","text":{"type":"mrkdwn","text":"*Image accessory* 88×88, radius 8, at the right of a 600px block. The text wraps next to it and the block is as tall as the image plus its 4px margin."},
			"accessory":{"type":"image","image_url":"{IMG88}","alt_text":"accessory"}}]`)},
		{Name: "layout/section accessory elements", Blocks: b(`[
			{"type":"section","text":{"type":"mrkdwn","text":"Button accessory"},"accessory":{"type":"button","text":{"type":"plain_text","text":"Click"},"action_id":"g_btn","value":"1"}},
			{"type":"section","text":{"type":"mrkdwn","text":"Overflow accessory"},"accessory":{"type":"overflow","action_id":"g_ov","options":[{"text":{"type":"plain_text","text":"Edit"},"value":"e"},{"text":{"type":"plain_text","text":"Delete"},"value":"d"}]}},
			{"type":"section","text":{"type":"mrkdwn","text":"Select accessory"},"accessory":{"type":"static_select","action_id":"g_sel","placeholder":{"type":"plain_text","text":"Pick one"},"options":[{"text":{"type":"plain_text","text":"One"},"value":"1"},{"text":{"type":"plain_text","text":"Two"},"value":"2"}]}},
			{"type":"section","text":{"type":"mrkdwn","text":"Datepicker accessory"},"accessory":{"type":"datepicker","action_id":"g_dp","initial_date":"2026-11-07"}},
			{"type":"section","text":{"type":"mrkdwn","text":"Checkboxes stack under the text (4px)"},"accessory":{"type":"checkboxes","action_id":"g_cb","options":[{"text":{"type":"plain_text","text":"First"},"value":"1"},{"text":{"type":"plain_text","text":"Second"},"value":"2"}]}},
			{"type":"section","text":{"type":"mrkdwn","text":"Radio buttons stack too"},"accessory":{"type":"radio_buttons","action_id":"g_rb","options":[{"text":{"type":"plain_text","text":"Yes"},"value":"y"},{"text":{"type":"plain_text","text":"No"},"value":"n"}]}}]`)},
		{Name: "layout/header and divider", Blocks: b(`[
			{"type":"header","text":{"type":"plain_text","text":"Header block :rocket:","emoji":true}},
			{"type":"section","text":{"type":"mrkdwn","text":"Under a header (15px 900, 1em above, 4px below)"}},
			{"type":"divider"},
			{"type":"section","text":{"type":"mrkdwn","text":"After a divider (1px otl-sec, 8px each side)"}},
			{"type":"header","text":{"type":"plain_text","text":"A second header after text"}}]`)},
		{Name: "layout/context", Blocks: b(`[
			{"type":"context","elements":[{"type":"image","image_url":"{IMG20}","alt_text":"icon"},{"type":"mrkdwn","text":"*Context*: image then mrkdwn <@{U1}>"},{"type":"plain_text","text":"then plain :wave:","emoji":true}]},
			{"type":"context","elements":[{"type":"image","image_url":"{IMG20}","alt_text":"a"},{"type":"image","image_url":"{IMG88}","alt_text":"b"},{"type":"image","image_url":"{IMGS}","alt_text":"c"},{"type":"mrkdwn","text":"three images, 4px apart"}]},
			{"type":"context","elements":[
				{"type":"mrkdwn","text":"one"},{"type":"mrkdwn","text":"two"},{"type":"mrkdwn","text":"three"},{"type":"mrkdwn","text":"four"},{"type":"mrkdwn","text":"five"},
				{"type":"mrkdwn","text":"six"},{"type":"mrkdwn","text":"seven"},{"type":"mrkdwn","text":"eight"},{"type":"mrkdwn","text":"nine"},
				{"type":"mrkdwn","text":"ten: the maximum, a longer element that wraps onto its own flex line when the row is full"}]}]`)},
		{Name: "layout/image", Blocks: b(`[
			{"type":"image","title":{"type":"plain_text","text":"Wide image, title and size"},"image_url":"{IMGW}","alt_text":"wide","image_width":1200,"image_height":600,"image_bytes":24576},
			{"type":"image","image_url":"{IMGT}","alt_text":"tall","image_width":300,"image_height":900},
			{"type":"image","title":{"type":"plain_text","text":"Small image stays small"},"image_url":"{IMGS}","alt_text":"small","image_width":160,"image_height":120},
			{"type":"image","image_url":"{IMGN}","alt_text":"no dimensions"}]`)},
		{Name: "layout/video and file", Blocks: b(`[
			{"type":"video","title":{"type":"plain_text","text":"Synthetic demo video"},"title_url":"https://video.example.com/watch/1","video_url":"https://video.example.com/embed/1",
			 "thumbnail_url":"{THUMB}","alt_text":"demo","author_name":"Fixture Studio","provider_name":"ExampleTube","provider_icon_url":"{ICON}",
			 "description":{"type":"plain_text","text":"A made-up video for the gallery."}},
			{"type":"file","external_id":"gallery-remote-1","source":"remote"}]`)},
		{Name: "layout/markdown", Blocks: `[{"type":"markdown","text":"# Markdown block\n**bold**, *italic*, ~~strike~~, ` + "`code`" + ` and [a link](https://example.com)\n\n- bullet one\n- bullet two\n  - nested\n1. first\n2. second\n\n> a quote\n\n` + "```" + `\nfenced code\n` + "```" + `"}]`},
		{Name: "layout/rich_text inline", Blocks: b(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
			{"type":"text","text":"Styles: "},{"type":"text","text":"bold","style":{"bold":true}},{"type":"text","text":" "},
			{"type":"text","text":"italic","style":{"italic":true}},{"type":"text","text":" "},{"type":"text","text":"strike","style":{"strike":true}},
			{"type":"text","text":" "},{"type":"text","text":"code","style":{"code":true}},{"type":"text","text":" "},
			{"type":"text","text":"underline","style":{"underline":true}},{"type":"text","text":" "},
			{"type":"text","text":"all","style":{"bold":true,"italic":true,"strike":true}},{"type":"text","text":"\nLinks: "},
			{"type":"link","url":"https://example.com"},{"type":"text","text":" "},{"type":"link","url":"https://example.com/x","text":"labelled","style":{"bold":true}},
			{"type":"text","text":"\nMentions: "},{"type":"user","user_id":"{U1}"},{"type":"text","text":" "},{"type":"usergroup","usergroup_id":"{S1}"},
			{"type":"text","text":" "},{"type":"channel","channel_id":"{C1}"},{"type":"text","text":" "},{"type":"broadcast","range":"here"},
			{"type":"text","text":" "},{"type":"broadcast","range":"channel"},{"type":"text","text":" "},{"type":"broadcast","range":"everyone"},
			{"type":"text","text":"\nEmoji: "},{"type":"emoji","name":"wave","unicode":"1f44b"},{"type":"emoji","name":"wave","unicode":"1f44b-1f3fd","skin_tone":4},
			{"type":"emoji","name":"{EMO}"},{"type":"text","text":"\nDate: "},
			{"type":"date","timestamp":1762506000,"format":"{date_long_pretty} at {time}","fallback":"Nov 7, 2025"},{"type":"text","text":" · fallback only: "},
			{"type":"date","timestamp":0,"format":"","fallback":"sometime"},
			{"type":"text","text":"\nColor: "},{"type":"color","value":"#1D9BD1"},{"type":"text","text":" "},{"type":"color","value":"not-a-color"}]}]}]`)},
		{Name: "layout/rich_text lists", Blocks: b(`[{"type":"rich_text","elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"Lists:"}]},
			{"type":"rich_text_list","style":"bullet","indent":0,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"bullet"}]},{"type":"rich_text_section","elements":[{"type":"text","text":"bullet two"}]}]},
			{"type":"rich_text_list","style":"bullet","indent":1,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"nested ring"}]}]},
			{"type":"rich_text_list","style":"bullet","indent":2,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"nested square"}]}]},
			{"type":"rich_text_list","style":"ordered","indent":0,"offset":0,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"one"}]},{"type":"rich_text_section","elements":[{"type":"text","text":"two"}]}]},
			{"type":"rich_text_list","style":"ordered","indent":1,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"alpha"}]},{"type":"rich_text_section","elements":[{"type":"text","text":"beta"}]}]},
			{"type":"rich_text_list","style":"ordered","indent":2,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"roman"}]}]},
			{"type":"rich_text_list","style":"ordered","indent":0,"offset":9,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"offset 9 starts at ten"}]}]},
			{"type":"rich_text_list","style":"bullet","indent":0,"border":1,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"bordered list"}]}]}]}]`)},
		{Name: "layout/rich_text quote and pre", Blocks: b(`[{"type":"rich_text","elements":[
			{"type":"rich_text_quote","elements":[{"type":"text","text":"A quote with "},{"type":"text","text":"style","style":{"bold":true}},{"type":"text","text":" and "},{"type":"user","user_id":"{U2}"}]},
			{"type":"rich_text_quote","border":1,"elements":[{"type":"text","text":"A bordered quote"}]},
			{"type":"rich_text_preformatted","elements":[{"type":"text","text":"preformatted := true\n  keeps   spacing"},{"type":"link","url":"https://example.com"}]},
			{"type":"rich_text_preformatted","border":1,"elements":[{"type":"text","text":"bordered pre"}]}]}]`)},
		{Name: "layout/table", Blocks: `[{"type":"table","column_settings":[{"align":"left"},{"align":"center"},{"align":"right"}],"rows":[
			[{"type":"raw_text","text":"Name"},{"type":"raw_text","text":"Kind"},{"type":"raw_text","text":"Score"}],
			[{"type":"raw_text","text":"Ada"},{"type":"raw_text","text":"fixture"},{"type":"raw_text","text":"42"}],
			[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Rich","style":{"bold":true}},{"type":"text","text":" cell"}]}]},{"type":"raw_text","text":"rich_text"},{"type":"raw_text","text":"7"}]]}]`},
		{Name: "layout/table preview", Blocks: tablePreview()},
		{Name: "layout/invalid and unknown", Blocks: `[
			{"type":"section","text":{"type":"mrkdwn","text":"Next: a block that doesn't decode, an image without a URL, then an unknown type with text."}},
			{"type":"section","fields":"not an array"},
			{"type":"image","alt_text":"no url"},
			{"type":"future_block","text":{"type":"mrkdwn","text":"future block _text_"}}]`},
		{Name: "layout/show more", Text: "Show more lives in the thread", Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"Long sections truncate in threads: open the thread."}}]`,
			Replies: []GalleryMessage{{Name: "layout/show more reply", Blocks: b(`[
				{"type":"section","block_id":"g_long","text":{"type":"mrkdwn","text":"<@{U1}> ` + long + `"}},
				{"type":"actions","block_id":"g_long_act","elements":[{"type":"static_select","action_id":"g_shape","initial_option":{"text":{"type":"plain_text","text":"circle"},"value":"circle"},
					"options":[{"text":{"type":"plain_text","text":"circle"},"value":"circle"},{"text":{"type":"plain_text","text":"square"},"value":"square"}]}]},
				{"type":"actions","block_id":"g_long_cb","elements":[{"type":"checkboxes","action_id":"g_fx","initial_options":[{"text":{"type":"plain_text","text":"checkmark badge"},"value":"check"}],
					"options":[{"text":{"type":"plain_text","text":"checkmark badge"},"value":"check"},{"text":{"type":"plain_text","text":"as a picture"},"value":"pfp"},{"text":{"type":"plain_text","text":"ring around it"},"value":"ring"}]}]},
				{"type":"actions","block_id":"g_long_btn","elements":[{"type":"button","text":{"type":"plain_text","text":"put me in a box!"},"style":"primary","action_id":"g_box"},
					{"type":"button","text":{"type":"plain_text","text":"my emojis"},"style":"danger","action_id":"g_mine"}]},
				{"type":"section","block_id":"g_expand","expand":true,"text":{"type":"mrkdwn","text":"expand: true never truncates. ` + long + `"}}]`)}}},
	}
}

func tablePreview() string {
	var sb strings.Builder
	sb.WriteString(`[{"type":"table","rows":[[{"type":"raw_text","text":"Row"},{"type":"raw_text","text":"Value"}]`)
	for i := 1; i <= 12; i++ {
		sb.WriteString(`,[{"type":"raw_text","text":"row ` + strconv.Itoa(i) + `"},{"type":"raw_text","text":"` + strconv.Itoa(i*i) + `"}]`)
	}
	sb.WriteString(`]}]`)
	return sb.String()
}
