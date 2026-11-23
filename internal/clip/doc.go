// Package clip records Slack's voice clips: the audio clip the composer's
// microphone button records (AudioComposerButton → AudioPopover, boot-shared
// VIJ/; the recorder hook useAudioWaveformRecorder, 3xSY; the + menu's
// AudioRecorderModal, wul1).
//
// What Slack attaches. useAudioWaveformRecorder makes File([blob],
// "audio_message.m4a", {type: blob.type}) with subtype "slack_audio",
// audio_wave_samples (the waveform's 24 dots × 100) and duration (the
// timer's seconds). The blob is what StreamManager's MediaRecorder wrote
// (KwNj: getAudioMIMEType picks the first of "audio/mp4",
// "audio/webm;codecs=opus", "audio/webm" that MediaRecorder supports, with
// audioBitsPerSecond 128000 and a 5 s timeslice). Slack's desktop app is
// Chromium, which supports "audio/mp4" and records it as Opus in MP4: no
// codec in the type means AudioTrackRecorder::GetPreferredCodec, and that
// is Opus for every container since Chromium's 8b97014da4 ("Make Opus
// audio codec as default for MP4 container", 2024-03-26); AAC only when
// the type says mp4a.40.2. So the clip is Opus in MP4 on macOS, Windows and
// Linux alike, and so is Clack's: libopus (the huddle's, internal/huddle)
// with AudioTrackOpusEncoder's settings (48 kHz, 60 ms frames,
// OPUS_APPLICATION_AUDIO, the bitrate asked for, VBR), in the MP4 that
// Chromium's Mp4Muxer writes (mp4.go).
//
// The upload is any pending file's (UploadQueue, boot-shared HnCu):
// files.getUploadURL {filename, length, subtype: "slack_audio"}, the
// multipart POST, files.completeUpload {files: [{id, title, subtype}]}.
// audio_wave_samples and duration stay in the client: the composer draws
// the pending clip from them (PendingAudioMessageUpload, cu6M).
//
// Nothing here writes audio to disk: the recording lives in memory until
// it's uploaded or discarded.
package clip

import "time"

// The clip Slack attaches (AudioPopover's useAudioWaveformRecorder call and
// StreamManager's MediaRecorder).
const (
	FileName = "audio_message.m4a"
	Subtype  = "slack_audio"
	Mime     = "audio/mp4"
	// BitRate is MediaRecorder's audioBitsPerSecond.
	BitRate = 128000
	// Timeslice is MediaRecorder.start(audioTimeslice): Mp4Muxer flushes a
	// fragment once a packet is this far past the last flush.
	Timeslice = 5 * time.Second
	// TimeLimit is timeLimitSeconds: the recording stops and attaches
	// itself at 5:00.
	TimeLimit = 300 * time.Second
	// TimeWarning is timeWarningSeconds: the timer and the waveform turn
	// red this long before the limit.
	TimeWarning = 10 * time.Second
	// StartDelay is the wait after the start sound before recording
	// starts (the microphonestart handler's await c(300)).
	StartDelay = 300 * time.Millisecond
	// StartSound is the sound microphonestart plays (client-boot cfKp uY),
	// a file on Slack's CDN.
	StartSound = "pop_v2-86c5be3.mp3"
)

// Rate is the clip's sample rate (Opus's; Chromium resamples to it).
const Rate = 48000

// FrameSamples is one Opus packet's samples per channel:
// kOpusPreferredBufferDurationMs, 60 ms.
const FrameSamples = Rate * 60 / 1000
