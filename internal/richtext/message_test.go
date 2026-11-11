package richtext

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func doc(t *testing.T, js string) Doc {
	t.Helper()
	d := FromMessage(R, msg(t, js))
	checkInvariants(t, js, d.Blocks)
	return d
}

func TestFromMessageText(t *testing.T) {
	cases := []struct {
		name, js string
		want     Doc
	}{
		{"nil-safe empty", `{"ts":"1.0","text":""}`, Doc{}},
		{"mrkdwn", `{"ts":"1.0","text":"hi *there*"}`, Doc{Blocks: []Block{para(tx("hi "), st("there", Bold))}}},
		{"jumbo", `{"ts":"1.0","text":":smile: :wave:"}`, Doc{Jumbo: true, Blocks: []Block{para(em("smile"), tx(" "), em("wave"))}}},
		{"not jumbo", `{"ts":"1.0","text":":smile: ok"}`, Doc{Blocks: []Block{para(em("smile"), tx(" ok"))}}},
		{"deleted", `{"ts":"1.0","subtype":"message_deleted","text":"gone"}`, Doc{Blocks: []Block{tombstone("This message was deleted.")}}},
		{"tombstone", `{"ts":"1.0","subtype":"tombstone","text":"This message was deleted."}`, Doc{Blocks: []Block{tombstone("This message was deleted.")}}},
		{"system join", `{"ts":"1.0","subtype":"channel_join","user":"U1","text":"<@U1> has joined the channel"}`,
			Doc{Blocks: []Block{para(user("U1", "@alice"), tx(" has joined the channel"))}}},
		{"topic", `{"ts":"1.0","subtype":"channel_topic","text":"set the channel topic: Ship *it*"}`,
			Doc{Blocks: []Block{para(tx("set the channel topic: Ship "), st("it", Bold))}}},
	}
	for _, c := range cases {
		eq(t, c.name, doc(t, c.js), c.want)
	}
	if d := FromMessage(R, nil); d.Blocks != nil || d.Jumbo {
		t.Error("nil message should give an empty Doc")
	}
}

func TestFiles(t *testing.T) {
	d := doc(t, `{"ts":"1.0","text":"","files":[
		{"id":"F1","name":"a.png","title":"a.png","mimetype":"image/png","filetype":"png","pretty_type":"PNG","size":1000,
		 "url_private":"https://files.slack.com/files-pri/T1-F1/a.png","url_private_download":"https://files.slack.com/files-pri/T1-F1/download/a.png",
		 "permalink":"https://ws.slack.com/files/U1/F1/a.png",
		 "thumb_360":"https://files.slack.com/files-tmb/T1-F1-x/a_360.png","thumb_360_w":360,"thumb_360_h":180,
		 "thumb_720":"https://files.slack.com/files-tmb/T1-F1-x/a_720.png","thumb_720_w":720,"thumb_720_h":360,
		 "original_w":1440,"original_h":720},
		{"id":"F2","name":"b.jpg","title":"Holiday","mimetype":"image/jpeg","url_private":"https://files.slack.com/files-pri/T1-F2/b.jpg",
		 "thumb_480":"https://files.slack.com/files-tmb/T1-F2-x/b_480.jpg","original_w":2000,"original_h":1000},
		{"id":"F3","name":"c.gif","mimetype":"image/gif","size":5000,"url_private":"https://files.slack.com/files-pri/T1-F3/c.gif",
		 "thumb_360":"https://files.slack.com/files-tmb/T1-F3-x/c_360.png","original_w":200,"original_h":100},
		{"id":"F4","name":"v.mp4","mimetype":"video/mp4","url_private":"https://files.slack.com/files-pri/T1-F4/v.mp4",
		 "mp4":"https://files.slack.com/files-tmb/T1-F4-x/v.mp4","thumb_video":"https://files.slack.com/files-tmb/T1-F4-x/v_thumb.jpg",
		 "original_w":1280,"original_h":720},
		{"id":"F5","name":"clip","mimetype":"audio/webm","subtype":"slack_audio","duration_ms":5000,"url_private":"https://files.slack.com/files-pri/T1-F5/clip"},
		{"id":"F6","name":"s.txt","title":"Snippet","mimetype":"text/plain","filetype":"text","mode":"snippet","preview":"line1\nline2","lines":2,
		 "url_private":"https://files.slack.com/files-pri/T1-F6/s.txt"},
		{"id":"F7","name":"doc.pdf","mimetype":"application/pdf","filetype":"pdf","pretty_type":"PDF","size":2048,"url_private":"https://files.slack.com/files-pri/T1-F7/doc.pdf"},
		{"id":"F8","mode":"tombstone"},
		{"id":"F9","mode":"hidden_by_limit"},
		{"id":"F10","name":"x","mimetype":"application/octet-stream","url_private":"javascript:alert(1)"}
	]}`)
	want := []Block{
		{Kind: FileBlock, File: &File{ID: "F1", Name: "a.png", Mime: "image/png", Type: "png", PrettyType: "PNG", Size: 1000,
			URL: "https://files.slack.com/files-pri/T1-F1/a.png", Download: "https://files.slack.com/files-pri/T1-F1/download/a.png",
			Permalink: "https://ws.slack.com/files/U1/F1/a.png", IsImage: true, OrigW: 1440, OrigH: 720,
			Thumb: &Image{URL: "https://files.slack.com/files-tmb/T1-F1-x/a_720.png", Alt: "a.png", W: 720, H: 360, Proxy: true}}},
		{Kind: FileBlock, File: &File{ID: "F2", Name: "b.jpg", Title: "Holiday", Mime: "image/jpeg", URL: "https://files.slack.com/files-pri/T1-F2/b.jpg", IsImage: true, OrigW: 2000, OrigH: 1000,
			Thumb: &Image{URL: "https://files.slack.com/files-tmb/T1-F2-x/b_480.jpg", Alt: "b.jpg", W: 480, H: 240, Proxy: true}}},
		{Kind: FileBlock, File: &File{ID: "F3", Name: "c.gif", Mime: "image/gif", Size: 5000, URL: "https://files.slack.com/files-pri/T1-F3/c.gif", IsImage: true, OrigW: 200, OrigH: 100,
			Thumb: &Image{URL: "https://files.slack.com/files-pri/T1-F3/c.gif", Alt: "c.gif", W: 200, H: 100, Proxy: true}}},
		{Kind: FileBlock, File: &File{ID: "F4", Name: "v.mp4", Mime: "video/mp4", URL: "https://files.slack.com/files-tmb/T1-F4-x/v.mp4", IsVideo: true, OrigW: 1280, OrigH: 720,
			Thumb: &Image{URL: "https://files.slack.com/files-tmb/T1-F4-x/v_thumb.jpg", Alt: "v.mp4", W: 1280, H: 720, Proxy: true}}},
		{Kind: FileBlock, File: &File{ID: "F5", Name: "clip", Mime: "audio/webm", URL: "https://files.slack.com/files-pri/T1-F5/clip", IsAudio: true, DurationMS: 5000, Subtype: "slack_audio"}},
		{Kind: FileBlock, File: &File{ID: "F6", Name: "s.txt", Title: "Snippet", Mime: "text/plain", Type: "text", URL: "https://files.slack.com/files-pri/T1-F6/s.txt",
			IsSnippet: true, Preview: "line1\nline2", Lines: 2}},
		{Kind: FileBlock, File: &File{ID: "F7", Name: "doc.pdf", Mime: "application/pdf", Type: "pdf", PrettyType: "PDF", Size: 2048, URL: "https://files.slack.com/files-pri/T1-F7/doc.pdf"}},
		tombstone("This file was deleted."),
		tombstone("This file is hidden because it is older than the workspace's visible history."),
		{Kind: FileBlock, File: &File{ID: "F10", Name: "x", Mime: "application/octet-stream"}},
	}
	if len(d.Blocks) != len(want) {
		t.Fatalf("got %d blocks, want %d:\n%s", len(d.Blocks), len(want), dump(d.Blocks))
	}
	for i := range want {
		eq(t, "file "+want[i].kindName(), d.Blocks[i], want[i])
	}
	eq(t, "files plain", PlainText(Doc{Blocks: d.Blocks[:2]}), "a.png\nHoliday")
}

func TestFileBlockUsesMessageFile(t *testing.T) {
	d := doc(t, `{"ts":"1.0","text":"see","blocks":[
		{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"see file"}]}]},
		{"type":"file","file_id":"F1","source":"remote"},
		{"type":"file","external_id":"X9","source":"remote"}],
		"files":[{"id":"F1","name":"r.doc","mimetype":"application/msword","url_private":"https://files.slack.com/files-pri/T1-F1/r.doc"}]}`)
	want := []Block{
		para(tx("see file")),
		{Kind: FileBlock, File: &File{ID: "F1", Name: "r.doc", Mime: "application/msword", URL: "https://files.slack.com/files-pri/T1-F1/r.doc"}},
		{Kind: FileBlock, File: &File{ID: "X9", Type: "remote"}},
	}
	eq(t, "file block", d.Blocks, want)
}

func TestAttachments(t *testing.T) {
	d := doc(t, `{"ts":"1.0","text":"","attachments":[
		{"color":"good","pretext":"pre *b*","author_name":"Bot","author_icon":"https://i.test/a.png","author_link":"https://a.test",
		 "title":"T &amp; :smile:","title_link":"https://t.test","text":"body _i_",
		 "fields":[{"title":"F1","value":"v1","short":true},{"title":"F2","value":"*v2*","short":false}],
		 "image_url":"https://files.slack.com/files-pri/T1-F1/x.png","image_width":800,"image_height":400,
		 "thumb_url":"https://t.test/ignored.png",
		 "footer":"foot <@U1>","footer_icon":"https://i.test/f.png","ts":"1700000000.000100"},
		{"service_name":"Example","service_icon":"https://ex.test/fav.ico","from_url":"https://ex.test/page","title":"Page",
		 "title_link":"https://ex.test/page","text":"desc","thumb_url":"https://ex.test/t.png","thumb_width":100,"thumb_height":80,
		 "author_name":"Writer","author_subname":"@writer"},
		{"color":"3AA3E3","title":"Bad link","title_link":"javascript:alert(1)","author_icon":"data:image/png;base64,xx"},
		{"fallback":"only *fb*"},
		{},
		{"text":"pick one","actions":[{"name":"ok","text":"OK","type":"button","style":"primary","value":"1"},{"id":2,"text":"Pick","type":"select"}]},
		{"text":"plain *not bold* <@U1>","mrkdwn_in":["pretext"]},
		{"title":"App","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"app *block*"}}],
		 "files":[{"id":"F1","name":"n.txt","mimetype":"text/plain","url_private":"https://files.slack.com/files-pri/T1-F1/n.txt"}]}
	]}`)
	want := []Block{
		{Kind: AttachmentBlock, Att: &Attachment{Legacy: true, Color: "#2EB886", Pretext: []Span{tx("pre "), st("b", Bold)},
			AuthorName: "Bot", AuthorIcon: "https://i.test/a.png", AuthorLink: "https://a.test",
			Title: []Span{tx("T & "), em("smile")}, TitleLink: "https://t.test", Text: []Block{para(tx("body "), st("i", Italic))},
			Image:  &Image{URL: "https://files.slack.com/files-pri/T1-F1/x.png", W: 800, H: 400, Proxy: true},
			Footer: []Span{tx("foot "), user("U1", "@alice")}, FooterIcon: "https://i.test/f.png", TS: 1700000000},
			Fields:     [][]Span{{st("F1", Bold), br(), tx("v1")}, {st("F2", Bold), br(), st("v2", Bold)}},
			FieldShort: []bool{true, false}},
		{Kind: AttachmentBlock, Att: &Attachment{Legacy: true, ServiceName: "Example", ServiceIcon: "https://ex.test/fav.ico", FromURL: "https://ex.test/page",
			IsUnfurl: true, Title: []Span{tx("Page")}, TitleLink: "https://ex.test/page", Text: []Block{para(tx("desc"))},
			Thumb: &Image{URL: "https://ex.test/t.png", W: 100, H: 80}, AuthorName: "Writer", AuthorSubname: "@writer"}},
		{Kind: AttachmentBlock, Att: &Attachment{Legacy: true, Color: "#3AA3E3", Title: []Span{tx("Bad link")}}},
		{Kind: AttachmentBlock, Att: &Attachment{Legacy: true, Text: []Block{para(tx("only "), st("fb", Bold))}}},
		{Kind: AttachmentBlock, Att: &Attachment{Legacy: true, Text: []Block{para(tx("pick one"))}}, Buttons: []Button{
			{Kind: "button", Text: "OK", Style: "primary", ActionID: "ok", Value: "1"},
			{Kind: "select", Text: "Pick", ActionID: "2"},
		}},
		{Kind: AttachmentBlock, Att: &Attachment{Legacy: true, Text: []Block{para(tx("plain *not bold* "), user("U1", "@alice"))}}},
		{Kind: AttachmentBlock, Att: &Attachment{Legacy: true, Kit: true, Title: []Span{tx("App")}, Text: []Block{{Kind: Section, Kit: true, Spans: []Span{tx("app "), st("block", Bold)}}}},
			Children: []Block{{Kind: FileBlock, File: &File{ID: "F1", Name: "n.txt", Mime: "text/plain", URL: "https://files.slack.com/files-pri/T1-F1/n.txt"}}}},
	}
	if len(d.Blocks) != len(want) {
		t.Fatalf("got %d blocks, want %d:\n%s", len(d.Blocks), len(want), dump(d.Blocks))
	}
	for i := range want {
		eq(t, "attachment", d.Blocks[i], want[i])
	}
	eq(t, "attachment plain", PlainText(Doc{Blocks: d.Blocks[:1]}), "pre b\nT & 😄\nbody i\nF1\nv1\nF2\nv2")
}

func TestSharedMessage(t *testing.T) {
	d := doc(t, `{"ts":"1.0","text":"look","attachments":[
		{"is_share":true,"author_name":"alice","author_icon":"https://i.test/al.png","author_link":"https://ws.slack.com/team/U1",
		 "channel_id":"C1","from_url":"https://ws.slack.com/archives/C1/p1700000000000100","ts":"1700000000.000100",
		 "text":"ignored when blocks exist",
		 "message_blocks":[{"message":{"blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"shared text"}]}]}]}}]},
		{"is_msg_unfurl":true,"author_subname":"bob","channel_id":"C2","channel_name":"random","text":"unfurled *msg*",
		 "files":[{"id":"F1","mode":"tombstone"}]}
	]}`)
	want := []Block{
		para(tx("look")),
		{Kind: AttachmentBlock, Att: &Attachment{Legacy: true, IsShare: true, AuthorName: "alice", AuthorIcon: "https://i.test/al.png", AuthorLink: "https://ws.slack.com/team/U1",
			ShareChannel: "general", ShareChannelID: "C1", FromURL: "https://ws.slack.com/archives/C1/p1700000000000100", TS: 1700000000,
			Text: []Block{para(tx("shared text"))}}},
		{Kind: AttachmentBlock, Att: &Attachment{Legacy: true, IsShare: true, MsgCard: true, AuthorName: "bob", ShareChannel: "random", ShareChannelID: "C2",
			Text:    []Block{para(tx("unfurled "), st("msg", Bold))},
			Context: []Span{tx("Posted in "), {Kind: Channel, Text: "random", ID: "C2"}}},
			Children: []Block{tombstone("This file was deleted.")}},
	}
	eq(t, "share", d.Blocks, want)
	eq(t, "share plain", PlainText(d), "look\nalice\nshared text\nbob\nunfurled msg\nThis file was deleted.")
}

// huddle_thread messages are HuddleMsgText: getHuddleMsgText's sentence
// (dbqw) in place of the text, the room for the Join Huddle button.
func TestHuddle(t *testing.T) {
	d := doc(t, `{"ts":"1.0","subtype":"huddle_thread","text":"A huddle happened","room":{"id":"R1","date_start":1700000000,"date_end":1700000600,
		"has_ended":true,"participants":[],"participant_history":["U1","USELF"],"channels":["C1"]}}`)
	if len(d.Blocks) != 1 || d.Blocks[0].Att == nil || d.Blocks[0].Att.Huddle == nil {
		t.Fatalf("blocks %+v", d.Blocks)
	}
	a := d.Blocks[0].Att
	eq(t, "ended, you were in it", a.Text, []Block{para(tx("You and alice were in the huddle for 10m."))})
	if h := a.Huddle; h.Live || h.Joinable || h.ID != "R1" || h.Channel != "C1" {
		t.Fatalf("room %+v", h)
	}

	live := doc(t, `{"ts":"1.0","subtype":"huddle_thread","text":"","room":{"id":"R1","date_start":1700000000,"participants":["U1"],"participant_history":["U1"]}}`)
	eq(t, "live, not in it", live.Blocks[0].Att.Text, []Block{para(tx("alice is already there."))})
	if h := live.Blocks[0].Att.Huddle; !h.Live || !h.Joinable {
		t.Fatalf("live room %+v", h)
	}
}

// The sentence for each of getHuddleMsgText's cases, and both groups of
// system_messages_redesign_m1.
func TestHuddleMsgTextCases(t *testing.T) {
	now := time.Unix(1700003900, 0)
	room := func(js string) *HuddleRoom {
		h, ok := ParseHuddleRoom(json.RawMessage(js))
		if !ok {
			t.Fatalf("parse %s", js)
		}
		return h
	}
	text := func(h *HuddleRoom, o HuddleTextOpts) string {
		var b strings.Builder
		for _, s := range HuddleMsgText(R, h, o, now) {
			b.WriteString(s.Text)
		}
		return b.String()
	}
	five := room(`{"id":"R","date_start":1700000000,"participants":["U1","U2","U3","U4","U5"],"participant_history":["U1","U2","U3","U4","U5"]}`)
	for _, c := range []struct {
		h    *HuddleRoom
		o    HuddleTextOpts
		want string
	}{
		{five, HuddleTextOpts{}, "alice and 4 others are already there."},
		{five, HuddleTextOpts{Redesign: true}, "alice, bob, and 3 others are already there."},
		{room(`{"id":"R","participants":["U1","U2"],"participant_history":["U1","U2"]}`), HuddleTextOpts{}, "alice and bob are already there."},
		{room(`{"id":"R","participants":["U1"]}`), HuddleTextOpts{IM: true}, ""}, // a DM: no "already there"
		{room(`{"id":"R","participants":["USELF"],"participant_history":["USELF"]}`), HuddleTextOpts{}, "You’re the only one here. Enjoy the tranquility, or invite someone."},
		{room(`{"id":"R","participants":["USELF"],"participant_history":["USELF"]}`), HuddleTextOpts{Redesign: true}, "Enjoy the tranquility, or invite someone."},
		{room(`{"id":"R","participants":["USELF"],"participant_history":["USELF"]}`), HuddleTextOpts{IM: true, Partner: "U1"}, "alice has received a notification to join"},
		{room(`{"id":"R","participants":["USELF","U1"],"participant_history":["USELF","U1"]}`), HuddleTextOpts{}, "alice is here too."},
		{room(`{"id":"R","date_start":1700000000,"date_end":1700003900,"has_ended":true,"participant_history":["USELF"]}`), HuddleTextOpts{}, "You were in the huddle by yourself for 1h 5m. A nice, meditative session :tea:"},
		{room(`{"id":"R","date_start":1700000000,"date_end":1700000120,"has_ended":true,"participant_history":["USELF","U1","U2","U3"]}`), HuddleTextOpts{}, "You, alice, and 2 others were in the huddle for 2m."},
		{room(`{"id":"R","date_start":1700000000,"date_end":1700000120,"has_ended":true,"participant_history":["USELF","U1","U2","U3"]}`), HuddleTextOpts{Redesign: true}, "You, alice, bob, and 1 other were in the huddle for 2m."},
		{room(`{"id":"R","date_start":1700000000,"date_end":1700000060,"has_ended":true,"participant_history":["U1","U2"]}`), HuddleTextOpts{}, "alice and bob were in the huddle for 1m."},
	} {
		if got := text(c.h, c.o); got != c.want {
			t.Errorf("%+v %+v:\n got %q\nwant %q", c.h, c.o, got, c.want)
		}
	}
}

func TestPlainText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hi *b* <@U1>\n&gt; q\n```code```", "hi b @alice\n> q\ncode"},
		{"<https://x.test|label> :smile: :parrot:", "label 😄 :parrot:"},
		{"<!here> <#C1> <!subteam^S1>", "@here #general @design"},
		{"a\nb", "a\nb"},
	}
	for _, c := range cases {
		eq(t, c.in, PlainText(Doc{Blocks: FromMrkdwn(R, c.in)}), c.want)
	}
	eq(t, "empty", PlainText(Doc{}), "")
	eq(t, "tombstone", PlainText(Doc{Blocks: []Block{tombstone("This message was deleted.")}}), "This message was deleted.")
}

// Images Slack draws inline whatever thumbs they carry (fileIsImage:
// image/* plus an image filetype or thumb_360): a small screenshot with
// thumbs only up to thumb_360 (the shape of a real 223px paste), a wide
// short one whose thumb_360 is 44px tall, one with only the small thumbs,
// and one with no thumbs at all (the original). An upload Slack hasn't
// processed yet (no mimetype or filetype) isn't an image, as in Slack.
func TestImageFileThumbs(t *testing.T) {
	cases := []struct {
		name, js   string
		url        string
		w, h       int
		notAnImage bool
	}{
		{"small paste", `{"id":"F1","name":"image.png","mimetype":"image/png","filetype":"png","mode":"hosted","file_access":"visible",
			"media_display_type":"unknown","has_rich_preview":false,"is_modified_by_ai":false,"original_w":223,"original_h":150,
			"url_private":"https://files.slack.com/F1/image.png","thumb_64":"https://files.slack.com/F1/64.png","thumb_80":"https://files.slack.com/F1/80.png",
			"thumb_160":"https://files.slack.com/F1/160.png","thumb_360":"https://files.slack.com/F1/360.png","thumb_360_w":223,"thumb_360_h":150,
			"thumb_tiny":"AwAe"}`, "https://files.slack.com/F1/360.png", 223, 150, false},
		{"wide short", `{"id":"F2","name":"image.png","mimetype":"image/png","filetype":"png","mode":"hosted","original_w":1022,"original_h":125,
			"url_private":"https://files.slack.com/F2/image.png","thumb_160":"https://files.slack.com/F2/160.png",
			"thumb_360":"https://files.slack.com/F2/360.png","thumb_360_w":360,"thumb_360_h":44}`, "https://files.slack.com/F2/360.png", 360, 44, false},
		{"only small thumbs", `{"id":"F3","name":"image.png","mimetype":"image/png","filetype":"png","mode":"hosted","original_w":1022,"original_h":125,
			"thumb_64":"https://files.slack.com/F3/64.png","thumb_160":"https://files.slack.com/F3/160.png"}`, "https://files.slack.com/F3/160.png", 160, 19, false},
		{"no thumbs", `{"id":"F4","name":"image.png","mimetype":"image/png","filetype":"png","mode":"hosted","original_w":90,"original_h":40,
			"url_private":"https://files.slack.com/F4/image.png"}`, "https://files.slack.com/F4/image.png", 90, 40, false},
		{"unprocessed upload", `{"id":"F5","name":"image.png","title":"image.png","mimetype":"","filetype":"","pretty_type":"","size":0,"mode":"hosted",
			"media_display_type":"unknown","file_access":"visible","url_private":"https://files.slack.com/F5/image.png"}`, "", 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := doc(t, `{"ts":"1.0","text":"","upload":true,"files":[`+tc.js+`]}`)
			if len(d.Blocks) != 1 || d.Blocks[0].File == nil {
				t.Fatalf("blocks %+v", d.Blocks)
			}
			f := d.Blocks[0].File
			if tc.notAnImage {
				if f.IsImage {
					t.Errorf("an unprocessed upload is an image: %+v", f)
				}
				return
			}
			if !f.IsImage || f.Thumb == nil {
				t.Fatalf("not an inline image: %+v", f)
			}
			if f.Thumb.URL != tc.url || f.Thumb.W != tc.w || f.Thumb.H != tc.h {
				t.Errorf("thumb %s %dx%d, want %s %dx%d", f.Thumb.URL, f.Thumb.W, f.Thumb.H, tc.url, tc.w, tc.h)
			}
		})
	}
}
