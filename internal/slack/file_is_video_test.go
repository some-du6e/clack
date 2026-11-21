package slack

import (
	"encoding/json"
	"testing"
)

// FileIsPlayableVideo follows Slack's fileIsPlayableVideo case by case.
func TestFileIsPlayableVideo(t *testing.T) {
	for _, c := range []struct {
		name string
		f    File
		want bool
	}{
		{"mp4", File{Mimetype: "video/mp4", Filetype: "mp4"}, true},
		{"mov", File{Mimetype: "video/quicktime", Filetype: "mov"}, true},
		{"webm", File{Mimetype: "video/webm", Filetype: "webm"}, true},
		{"MP4 upper", File{Mimetype: "video/mp4", Filetype: "MP4"}, true},
		{"mkv without its mp4", File{Mimetype: "video/x-matroska", Filetype: "mkv"}, false},
		{"mkv with its mp4", File{Mimetype: "video/x-matroska", Filetype: "mkv", MP4: "https://files.slack.com/x.mp4"}, true},
		{"avi without", File{Mimetype: "video/x-msvideo", Filetype: "avi"}, false},
		{"a clip", File{Subtype: "slack_video", Filetype: "mkv"}, true},
		{"a video story", File{Subtype: "video_story"}, true},
		{"an audio clip", File{Subtype: "slack_audio", Mimetype: "video/mp4", Filetype: "mp4"}, false},
		{"an audio story", File{Subtype: "audio_story", Filetype: "mp4"}, false},
		{"a name pronunciation", File{Subtype: "slack_name_pronunciation", Filetype: "mp4"}, false},
		{"a tombstone", File{Mode: "tombstone", Filetype: "mp4"}, false},
		{"external", File{IsExternal: true, Filetype: "mp4", Mimetype: "video/mp4"}, false},
		{"external with outputs", File{IsExternal: true, Filetype: "mp4", Video: &FileVideo{Outputs: json.RawMessage(`[{"url":"x"}]`)}}, true},
		{"external with no outputs", File{IsExternal: true, Filetype: "mp4", Video: &FileVideo{Outputs: json.RawMessage(`[]`)}}, false},
		{"salesforce", File{IsExternal: true, ExternalType: "salesforce", Filetype: "mov"}, true},
		{"an image", File{Mimetype: "image/png", Filetype: "png"}, false},
		{"mp4 audio", File{Mimetype: "audio/mp4", Filetype: "m4a"}, false},
	} {
		if got := FileIsPlayableVideo(&c.f); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if FileIsPlayableVideo(nil) {
		t.Error("nil plays")
	}
	var f File
	if err := json.Unmarshal([]byte(`{"filetype":"mp4","thumb_video_ts":"https://x/y.jpg","hls_embed":"data:x","vtt":"v","aac":"a","media_progress":{"offset_ms":1500,"media_watched":true},"video":{"outputs":[1]}}`), &f); err != nil {
		t.Fatal(err)
	}
	if f.ThumbVideoTS != "https://x/y.jpg" || f.HLSEmbed != "data:x" || f.VTT != "v" || f.AAC != "a" || f.MediaProgress == nil || f.MediaProgress.OffsetMS != 1500 || !f.MediaProgress.MediaWatched || f.Video == nil {
		t.Errorf("decoded %+v", f)
	}
}
