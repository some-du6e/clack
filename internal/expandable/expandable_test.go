package expandable

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// The expected keys are Slack's: icC3 (getExpandableKey and
// getMessageExpandableKeys, build 9f450b8ad3db) run verbatim in node over
// these cases (scratch slash-commands-2/oracle/expand: oracle.cjs,
// cases.json, gen.py). Slack's message store keeps a message's files as
// ids and a call's room id as _call_id; the messages here are the API's
// shape Clack keeps (files as objects, the room's id).

func TestKeyLikeSlack(t *testing.T) {
	for _, c := range []struct {
		name, kind, ts string
		o              Opts
		want           string
	}{
		{"key file", "file", "1700000000.000100", Opts{ID: "F0AAA", ViewContext: "Channel"}, "inline_file_msg_1700000000_000100F0AAAChannel"},
		{"key attachmentText", "attachmentText", "1700000000.000100", Opts{ID: "3", ViewContext: "Thread"}, "attach_text_31700000000.000100Thread"},
		{"key attachmentText whisper", "attachmentText", "1700000000.000100", Opts{ID: "3", ViewContext: "Thread", Whisper: true}, "attach_text_31700000000.000100Thread_agent_whisper"},
		{"key overLimit", "attachmentsOverLimit", "1700000000.000100", Opts{Limit: 20, ViewContext: "Channel"}, "attach_over_20_1700000000.000100Channel"},
		{"key blockText", "blockText", "1700000000.000100", Opts{BlockID: "abc", ViewContext: "Channel"}, "block_text_1700000000.000100_abc"},
		{"key blockText whisper", "blockText", "1700000000.000100", Opts{BlockID: "abc", Whisper: true}, "block_text_1700000000.000100_abc_agent_whisper"},
		{"key blockVideo", "blockVideo", "1700000000.000100", Opts{BlockID: "v1"}, "block_video_1700000000.000100_v1"},
		{"key blockImage no block id", "blockImage", "1700000000.000100", Opts{}, "block_image_1700000000.000100_undefined"},
		{"key canvasRecap", "canvasRecap", "1700000000.000100", Opts{ViewContext: "Channel"}, "canvas_recap_1700000000.000100Channel"},
		{"key unknown", "bogus", "1700000000.000100", Opts{ID: "x"}, ""},
		{"key no ts", "files", "", Opts{FileIDs: []string{"F1"}}, ""},
		{"key files no ids", "files", "1700000000.000100", Opts{ViewContext: "Channel"}, ""},
		{"key ts two dots", "files", "1.2.3", Opts{FileIDs: []string{"F1"}, ViewContext: "Channel"}, "inline_files_msg_1_2.3F1Channel"},
	} {
		if got := Key(Kind(c.kind), c.ts, c.o); got != c.want {
			t.Errorf("%s: Key(%s, %q, %+v) = %q, want %q", c.name, c.kind, c.ts, c.o, got, c.want)
		}
	}
}

func TestMessageKeysLikeSlack(t *testing.T) {
	for _, c := range []struct {
		name, msg, vc string
		want          []string
	}{
		{"files one", `{"ts":"1700000000.000100","files":[{"id":"F0AAA"}]}`, "Channel", []string{"inline_files_msg_1700000000_000100F0AAAChannel"}},
		{"files two thread", `{"ts":"1700000000.000200","files":[{"id":"F0AAA"},{"id":"F0BBB"}]}`, "Thread", []string{"inline_files_msg_1700000000_000200F0AAA_F0BBBThread"}},
		{"files no view", `{"ts":"1700000000.000300","files":[{"id":"F0CCC"}]}`, "", []string{"inline_files_msg_1700000000_000300F0CCC"}},
		{"files empty list", `{"ts":"1700000000.000310","files":[]}`, "Channel", []string{"inline_files_msg_1700000000_000310Channel"}},
		{"call shared", `{"ts":"1700000000.000500","subtype":"sh_room_shared","attachments":[{"id":1,"image_url":"https://x/y.png"}],"room":{"id":"R0CALL1"}}`, "Channel", []string{"call_msg_R0CALL11700000000.000500Channel"}},
		{"call created no id", `{"ts":"1700000000.000600","subtype":"sh_room_created"}`, "Threads", []string{"call_msg_undefined1700000000.000600Threads"}},
		{"attachments media kinds", `{"ts":"1700000000.000700","attachments":[{"id":1,"image_url":"https://e.com/a.png"},{"id":2,"audio_html":"<iframe src=a></iframe>"},{"id":3,"video_html":"<iframe src=v></iframe>","thumb_url":"https://e.com/t.jpg"},{"id":4,"thumb_url":"https://e.com/only-thumb.jpg"},{"id":5,"text":"no media"}]}`, "Channel", []string{"img_msg_1700000000.0007001Channel", "aud_msg_1700000000.0007002Channel", "vid_msg_1700000000.0007003Channel"}},
		{"attachments no ids", `{"ts":"1700000000.000800","attachments":[{"image_url":"https://e.com/b.png"},{"audio_html":"<audio></audio>"},{"video_html":"<v></v>","thumb_url":"https://e.com/vt.jpg"},{"video_html":"<v></v>"}]}`, "Thread", []string{"img_msg_1700000000.000800https://e.com/b.pngThread", "aud_msg_1700000000.000800<audio></audio>Thread", "vid_msg_1700000000.000800https://e.com/vt.jpgThread", "vid_msg_1700000000.000800undefinedThread"}},
		{"attachment image beats video", `{"ts":"1700000000.000900","attachments":[{"id":7,"image_url":"https://e.com/i.png","video_html":"<v></v>","audio_html":"<a></a>"}]}`, "Channel", []string{"img_msg_1700000000.0009007Channel"}},
		{"attachment blocks", `{"ts":"1700000000.001000","attachments":[{"id":1,"blocks":[{"type":"image","block_id":"img1","image_url":"https://e.com/1.png"},{"type":"section","block_id":"sec1","accessory":{"type":"image","image_url":"https://e.com/2.png"}},{"type":"section","block_id":"sec2","accessory":{"type":"button"}},{"type":"image"}]}]}`, "Channel", []string{"block_image_1700000000.001000_img1", "block_image_1700000000.001000_sec1", "block_image_1700000000.001000_undefined"}},
		{"blocks only", `{"ts":"1700000000.001100","blocks":[{"type":"rich_text","block_id":"rt"},{"type":"image","block_id":"pic"},{"type":"section","block_id":"s","accessory":{"type":"image"}},{"type":"video","block_id":"vid"}]}`, "Thread", []string{"block_image_1700000000.001100_pic", "block_image_1700000000.001100_s"}},
		{"files beat blocks", `{"ts":"1700000000.001200","files":[{"id":"F0FFF"}],"blocks":[{"type":"image","block_id":"pic"}]}`, "Channel", []string{"inline_files_msg_1700000000_001200F0FFFChannel"}},
		{"attachments beat blocks", `{"ts":"1700000000.001300","attachments":[{"id":1,"text":"t"}],"blocks":[{"type":"image","block_id":"pic"}]}`, "Channel", nil},
		{"nothing", `{"ts":"1700000000.001400","text":"hi"}`, "Channel", nil},
		{"whisper off", `{"ts":"1700000000.001600","whisper_blocks":[{"type":"image","block_id":"wpic"}]}`, "Channel", nil},
	} {
		var m slack.Message
		if err := json.Unmarshal([]byte(c.msg), &m); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := MessageKeys(&m, c.vc); !slices.Equal(got, c.want) {
			t.Errorf("%s: MessageKeys = %q, want %q", c.name, got, c.want)
		}
	}
}

// Keys built from an attachment without an id carry its media's URL or
// embed HTML: those never go to disk.
func TestOnDisk(t *testing.T) {
	for k, want := range map[string]bool{
		"inline_files_msg_1700000000_000100F0AAAChannel":     true,
		"img_msg_1700000000.0007001Channel":                  true,
		"block_image_1700000000.001100_pic":                  true,
		"img_msg_1700000000.000800https://e.com/b.pngThread": false,
		"aud_msg_1700000000.000800<audio></audio>Thread":     false,
		"": false,
	} {
		if got := OnDisk(k); got != want {
			t.Errorf("OnDisk(%q) = %v", k, got)
		}
	}
}
