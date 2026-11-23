package slack

import (
	"bytes"
	"strings"
)

// FileIsPlayableVideo is Slack's fileIsPlayableVideo (client-boot 2HRR,
// build 2c0d02ca130f): the files a message, a gallery and the media
// viewer play, all others being drawn as plain files. Not a tombstone, not
// an audio clip, and either a video clip (subtype slack_video or
// video_story) or a hosted file (not external, or external with
// transcoded video.outputs, or a Salesforce one) whose filetype is mp4,
// mov or webm, or whose mimetype is video/* with an mp4 Slack made. An mkv
// or avi upload plays once the server adds its mp4.
func FileIsPlayableVideo(f *File) bool {
	if f == nil || f.Mode == "tombstone" || fileIsAudioClip(f) {
		return false
	}
	if f.Subtype == "slack_video" || f.Subtype == "video_story" {
		return true
	}
	if !FileIsHosted(f) {
		return false
	}
	switch strings.ToLower(f.Filetype) {
	case "mp4", "mov", "webm":
		return true
	}
	return strings.HasPrefix(f.Mimetype, "video") && f.MP4 != ""
}

// fileIsAudioClip is Slack's isAudioClip (OiPr): a voice clip, an audio
// story or a name pronunciation.
func fileIsAudioClip(f *File) bool {
	switch f.Subtype {
	case "audio_story", "slack_audio", "slack_name_pronunciation":
		return true
	}
	return false
}

// FileIsHosted is Slack's fileIsHosted: not external, or external with
// non-empty video.outputs, or a Salesforce file.
func FileIsHosted(f *File) bool {
	if !f.IsExternal || f.ExternalType == "salesforce" {
		return true
	}
	if f.Video == nil {
		return false
	}
	o := bytes.TrimSpace(f.Video.Outputs)
	return len(o) > 0 && !bytes.Equal(o, []byte("null")) && !bytes.Equal(o, []byte("[]")) && !bytes.Equal(o, []byte("{}"))
}

// FileIsPlayableAudio is Slack's fileIsPlayableAudio (client-boot vITP,
// build 35b42de8b870): the files a message plays in its AudioPlayer, all
// others being drawn as plain files (a webm upload that isn't a clip is
// a video). Not a tombstone, hosted supported media (fileIsSupportedAudio:
// filetype m4a, mp3 or wav, or an audio_story; or fileIsSupportedVideo's
// mp4, mov or webm), and an audio clip or a supported audio file. An ogg,
// flac or aac upload is a plain file, as in Slack.
func FileIsPlayableAudio(f *File) bool {
	if f == nil || f.Mode == "tombstone" {
		return false
	}
	audio := fileIsSupportedAudio(f)
	if !audio && !fileIsSupportedVideo(f) || !FileIsHosted(f) {
		return false
	}
	return fileIsAudioClip(f) || audio
}

// fileIsSupportedAudio is /^(m4a|mp3|wav)$/ on the filetype, or an
// audio_story.
func fileIsSupportedAudio(f *File) bool {
	switch f.Filetype {
	case "m4a", "mp3", "wav":
		return true
	}
	return f.Subtype == "audio_story"
}

// fileIsSupportedVideo is /^(mp4|mov|webm)$/ on the filetype.
func fileIsSupportedVideo(f *File) bool {
	switch f.Filetype {
	case "mp4", "mov", "webm":
		return true
	}
	return false
}
