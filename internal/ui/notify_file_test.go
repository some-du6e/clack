package ui

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// heldNames is a resolver whose store holds these members' names
// (MemberName, getPrefCompliantMemberName of getMemberById).
type heldNames struct {
	noResolver
	names map[string]string
}

func (h heldNames) MemberName(id string) (string, bool) {
	n, ok := h.names[id]
	return n, ok
}

// MemberNameHeld is the read before notify-file-meta-2 (red branches).
func (h heldNames) MemberNameHeld(id string) (string, bool) { return h.MemberName(id) }

// TestNotifyFileTextLikeSlack: a notification's text for a message with
// files is extractTextFromMessage's (boot-shared Eley, notify_file.go): a
// clip's line ("sent an audio clip", "replied with a few clips and a
// message, …"), else for a message with no text or a file subtype
// (file_share, file_mention, file_comment, file_upload) the first file's
// title, else MXE2's meta line by the file's kind (getFileType: snippet,
// email, post, image, else file), whether the message uploaded it, its
// initial comment and its uploader ("shared Robin’s image"), each
// placeholder escaped as Slack's i18n escapes it (twice for the name).
// Expected values are Slack's own: Eley's extractTextFromMessage with
// MXE2, 2grX, vITP, Jg0n, Qre6, R3SR, ajnp and lodash running in node over
// these messages (CDN build 35b42de8b870; only the store, i18n's t, the
// text converter and Slackbot AI's entitlement, on, are stand-ins; the
// sender is hVKV's getMsgSenderEntity: a legacy file_comment's
// comment.user, a canvas comment's editor, a list edit's author, none
// for a huddle, a canvas update, a Slackbot response or Slackbot's own
// file, the file's uploader for Slackbot's email forward). Synthetic
// messages.
func TestNotifyFileTextLikeSlack(t *testing.T) {
	unknown := func(s richtext.Span) (string, bool) { return "", s.Kind == richtext.User }
	for _, c := range []struct {
		name, msg string
		members   map[string]string
		want      string
	}{
		{"no text: the title, unescaped and trimmed",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":" Q3 &amp; Q4 &lt;plan&gt; ","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}]}`,
			map[string]string{}, "Q3 & Q4 <plan>"},
		{"no text, a title of spaces: the meta line",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"   ","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}]}`,
			map[string]string{}, "shared this file"},
		{"own file, not uploaded with the message: shared this file",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}]}`,
			map[string]string{}, "shared this file"},
		{"uploaded with the message",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"upload":true}`,
			map[string]string{}, "uploaded this file"},
		{"uploaded, with an initial comment",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","initial_comment":{"comment":"c"}}],"upload":true}`,
			map[string]string{}, "uploaded and commented on this file"},
		{"someone else's file: their name's possessive",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}]}`,
			map[string]string{"U0SYNTH0002": "Robin"}, "shared Robin’s file"},
		{"someone else's file, a name with & and an apostrophe: escaped twice",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}]}`,
			map[string]string{"U0SYNTH0002": "Pat & Sam O'Neil"}, "shared Pat &amp;amp; Sam O&amp;#39;Neil’s file"},
		{"someone else's file, a member the store lacks: an empty name",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}]}`,
			map[string]string{}, "shared ’s file"},
		{"someone else's file uploaded with the message: uploaded",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"upload":true}`,
			map[string]string{"U0SYNTH0002": "Robin"}, "uploaded this file"},
		{"an image",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"image/png","filetype":"png","pretty_type":"PNG","mode":"hosted","thumb_360":"https://files.example.invalid/t360.png"}],"upload":true}`,
			map[string]string{}, "uploaded this image"},
		{"an image someone else shared",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"image/png","filetype":"png","pretty_type":"PNG","mode":"hosted","thumb_360":"https://files.example.invalid/t360.png"}]}`,
			map[string]string{"U0SYNTH0002": "Robin"}, "shared Robin’s image"},
		{"an image/* file without thumbs is a file",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"image/png","filetype":"png","pretty_type":"PNG","mode":"hosted"}],"upload":true}`,
			map[string]string{}, "uploaded this file"},
		{"an image hidden by the storage limit is a file",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"image/png","filetype":"png","pretty_type":"PNG","mode":"hidden_by_limit","thumb_360":"https://files.example.invalid/t360.png"}],"upload":true}`,
			map[string]string{}, "uploaded this file"},
		{"a snippet uploaded: added",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"Python","mode":"snippet"}],"upload":true}`,
			map[string]string{}, "added this Python snippet"},
		{"a snippet uploaded with a comment",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"Python","mode":"snippet","initial_comment":{"comment":"c"}}],"upload":true}`,
			map[string]string{}, "added and commented on this Python snippet"},
		{"a snippet shared",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"Python","mode":"snippet"}]}`,
			map[string]string{}, "shared this Python snippet"},
		{"someone else's snippet",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"Python","mode":"snippet"}]}`,
			map[string]string{"U0SYNTH0002": "Robin"}, "shared Robin’s Python snippet"},
		{"a snippet with no pretty_type: file",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"","mode":"snippet"}],"upload":true}`,
			map[string]string{}, "added this file snippet"},
		{"a pretty_type with & is escaped once",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"C&C","mode":"snippet"}],"upload":true}`,
			map[string]string{}, "added this C&amp;C snippet"},
		{"an email",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"email","pretty_type":"Email","mode":"email"}]}`,
			map[string]string{}, "shared this email"},
		{"an email uploaded with a comment",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"email","pretty_type":"Email","mode":"email","initial_comment":{"comment":"c"}}],"upload":true}`,
			map[string]string{}, "uploaded and commented on this email"},
		{"a post",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"post","pretty_type":"Post","mode":"post"}],"upload":true}`,
			map[string]string{}, "uploaded this post"},
		{"a docs file is a post",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"docs","pretty_type":"Doc","mode":"docs"}]}`,
			map[string]string{}, "shared this post"},
		{"someone else's post",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"space","pretty_type":"Post","mode":"space"}]}`,
			map[string]string{"U0SYNTH0002": "Robin"}, "shared Robin’s post"},
		{"a canvas is a file",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"quip","pretty_type":"Canvas","mode":"quip"}],"upload":true}`,
			map[string]string{}, "uploaded this file"},
		{"a quip filetype in post mode is not a post",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"quip","pretty_type":"Canvas","mode":"post"}],"upload":true}`,
			map[string]string{}, "uploaded this file"},
		{"audio in snippet mode is audio: a file",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"audio/mpeg","filetype":"mp3","pretty_type":"MP3","mode":"snippet"}],"upload":true}`,
			map[string]string{}, "uploaded this file"},
		{"a video is a file",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"video/mp4","filetype":"mp4","pretty_type":"MPEG 4 Video","mode":"hosted"}],"upload":true}`,
			map[string]string{}, "uploaded this file"},
		{"a list is a file",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"list","pretty_type":"List","mode":"list"}]}`,
			map[string]string{}, "shared this file"},
		{"file_share with text: the meta line, not the text",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"see this","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"Report","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"file_share","upload":true}`,
			map[string]string{}, "uploaded this file"},
		{"file_mention with text",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"look","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"file_mention"}`,
			map[string]string{"U0SYNTH0002": "Robin"}, "shared Robin’s file"},
		{"file_comment with no text: the title",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"Report","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"file_comment"}`,
			map[string]string{}, "Report"},
		{"text and a file, no file subtype: the text",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"see this","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"Report","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"upload":true}`,
			map[string]string{}, "see this"},
		{"the first file speaks",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"},{"id":"F0SYNTH0002","user":"U0SYNTH0001","name":"x.bin","title":"Second","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"upload":true}`,
			map[string]string{}, "uploaded this file"},
		{"one audio clip",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"Audio clip","mimetype":"audio/mp4","filetype":"m4a","pretty_type":"Binary","mode":"hosted","subtype":"slack_audio"}]}`,
			map[string]string{}, "sent an audio clip"},
		{"one video clip",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"Video clip","mimetype":"video/mp4","filetype":"mp4","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"}]}`,
			map[string]string{}, "sent a video clip"},
		{"one audio clip with text",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"Tom &amp; Jerry","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"audio/mp4","filetype":"m4a","pretty_type":"Binary","mode":"hosted","subtype":"slack_audio"}]}`,
			map[string]string{}, "sent an audio clip and a message, \"Tom &amp; Jerry\""},
		{"an audio clip in a reply",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"audio/mp4","filetype":"m4a","pretty_type":"Binary","mode":"hosted","subtype":"slack_audio"}],"thread_ts":"1762510000.000100"}`,
			map[string]string{}, "replied with an audio clip"},
		{"a video clip in a reply, with text",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"watch","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"video/mp4","filetype":"mp4","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"}],"thread_ts":"1762510000.000100"}`,
			map[string]string{}, "replied with a video clip and a message, \"watch\""},
		{"a thread's root is not a reply",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"video/mp4","filetype":"mp4","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"}],"thread_ts":"1762520000.000100"}`,
			map[string]string{}, "sent a video clip"},
		{"a few clips",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","subtype":"slack_audio"},{"id":"F0SYNTH0002","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"}]}`,
			map[string]string{}, "sent a few clips"},
		{"a few clips with text, in a reply",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"two","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"},{"id":"F0SYNTH0002","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"}],"thread_ts":"1762510000.000100"}`,
			map[string]string{}, "replied with a few clips and a message, \"two\""},
		{"a file then a clip: the clip's line",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"Report","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"},{"id":"F0SYNTH0002","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","subtype":"slack_audio"}]}`,
			map[string]string{}, "sent an audio clip"},
		{"a clip under file_share",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"}],"subtype":"file_share"}`,
			map[string]string{}, "sent a video clip"},
		{"a clip with text that converts to nothing",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"<@U0NOBODY01>","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"}]}`,
			map[string]string{}, "sent a video clip and a message, \"\""},
		// the sender the uploader is compared with is getMsgSenderEntity's
		// userId (hVKV running in node too: notify-file-meta-2)
		{"a legacy file_comment's sender is comment.user",
			`{"type":"message","ts":"1762520000.000100","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"file_comment","comment":{"user":"U0SYNTH0001","comment":"c"}}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared this file"},
		{"Slackbot forwarding an email: the file's uploader is the sender",
			`{"type":"message","ts":"1762520000.000100","user":"USLACKBOT","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"email","pretty_type":"Email","mode":"email","bot_id":"B0SYNTH0001"}]}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared this email"},
		{"Slackbot posting its own bot file: no member sender",
			`{"type":"message","ts":"1762520000.000100","user":"USLACKBOT","text":"","files":[{"id":"F0SYNTH0001","user":"USLACKBOT","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","bot_id":"B0SYNTH0001"}]}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared Slackbot’s file"},
		{"Slackbot's own file under Slackbot AI: no member sender",
			`{"type":"message","ts":"1762520000.000100","user":"USLACKBOT","text":"","files":[{"id":"F0SYNTH0001","user":"USLACKBOT","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}]}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared Slackbot’s file"},
		{"Slackbot AI's message: Slackbot is the sender",
			`{"type":"message","ts":"1762520000.000100","user":"USLACKBOT","text":"","files":[{"id":"F0SYNTH0001","user":"USLACKBOT","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"ai_context":{"type":"slackbot_ai"}}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared this file"},
		{"a Slackbot response: no member sender",
			`{"type":"message","ts":"1762520000.000100","user":"USLACKBOT","text":"","files":[{"id":"F0SYNTH0001","user":"USLACKBOT","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"slackbot_response"}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared Slackbot’s file"},
		{"a huddle message: no member sender",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"room":{"id":"R0SYNTH0001"},"room_id":"R0SYNTH0001"}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared Ada’s file"},
		{"a canvas comment root: its most recent editor",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"document_comment_root","document_comment":{"thread_id":"temp:C:x","most_recent_editor":"U0SYNTH0002","authors":["U0SYNTH0001"]}}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared this file"},
		{"a canvas comment root without an editor: its first author",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"document_comment_root","document_comment":{"thread_id":"temp:C:x","authors":["U0SYNTH0002","U0SYNTH0001"]}}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared this file"},
		{"a list edit: who made it",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"list_edit","list_edit":{"created_by":"U0SYNTH0002"}}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared this file"},
		{"a tab added: who added it",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"channel_tab_added","tab_added_user_id":"U0SYNTH0002"}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared this file"},
		{"a folder item added: who added it",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"folder_item_added_unread_ignore","item_added_user_id":"U0SYNTH0002"}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared this file"},
		{"a channel canvas update: no member sender",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"channel_canvas_updated"}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared Ada’s file"},
		{"an AI huddle summary: msg.user",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"ai_context":{"summary":{"type":"huddle"}}}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared this file"},
		{"a welcome party: no member sender",
			`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}],"subtype":"welcome_party"}`,
			map[string]string{"U0SYNTH0001": "Ada", "U0SYNTH0002": "Robin", "USLACKBOT": "Slackbot"}, "shared Ada’s file"},
	} {
		var m slack.Message
		if err := json.Unmarshal([]byte(c.msg), &m); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := notifyText(heldNames{names: c.members}, noteTestMessage(&m), unknown); got != c.want {
			t.Errorf("%s: %q, Slack %q", c.name, got, c.want)
		}
	}
}

// TestNotifyPossessiveLikeSlack: the affix after the uploader's name is
// ajnp's possessive for the locale (fr-FR's d’ before a vowel once
// i18n_deburr has folded the name, de-DE's ’ after s, ß, x or z); values
// from ajnp with J3F/ i18n_deburr and lodash deburr running in node.
func TestNotifyPossessiveLikeSlack(t *testing.T) {
	for _, c := range []struct{ name, loc, want string }{
		{"Robin", "en-US", "’s"},
		{"", "en-US", "’s"},
		{"Robin", "en-GB", "’s"},
		{"", "en-GB", "’s"},
		{"Robin", "fr-FR", "de "},
		{"Émile", "fr-FR", "d’"},
		{"Иван", "fr-FR", "d’"},
		{"Hans", "fr-FR", "de "},
		{"Strauß", "fr-FR", "de "},
		{"Max", "fr-FR", "de "},
		{"Fritz", "fr-FR", "de "},
		{"Ana", "fr-FR", "d’"},
		{"yan", "fr-FR", "d’"},
		{"Ørjan", "fr-FR", "d’"},
		{"", "fr-FR", "de "},
		{"Robin", "de-DE", "s"},
		{"Émile", "de-DE", "s"},
		{"Иван", "de-DE", "s"},
		{"Hans", "de-DE", "’"},
		{"Strauß", "de-DE", "’"},
		{"Max", "de-DE", "’"},
		{"Fritz", "de-DE", "’"},
		{"Ana", "de-DE", "s"},
		{"yan", "de-DE", "s"},
		{"Ørjan", "de-DE", "s"},
		{"", "de-DE", "s"},
		{"Robin", "es-ES", "de "},
		{"", "es-ES", "de "},
		{"Robin", "es-LA", "de "},
		{"", "es-LA", "de "},
		{"Robin", "pt-BR", "de "},
		{"", "pt-BR", "de "},
		{"Robin", "ja-JP", "の"},
		{"", "ja-JP", "の"},
		{"Robin", "ko-KR", "의"},
		{"", "ko-KR", "의"},
		{"Robin", "it-IT", "di "},
		{"", "it-IT", "di "},
		{"Robin", "zh-CN", " 的"},
		{"", "zh-CN", " 的"},
		{"Robin", "zh-TW", " 的"},
		{"", "zh-TW", " 的"},
		{"Robin", "xx-XX", ""},
		{"", "xx-XX", ""},
	} {
		if got := notePossessiveOf(c.name, c.loc); got != c.want {
			t.Errorf("%q in %s: %q, Slack %q", c.name, c.loc, got, c.want)
		}
	}
}
