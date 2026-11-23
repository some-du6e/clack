package composer

import (
	"context"
	"encoding/json"
	"image"
	"math"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"

	vclip "github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// clipHost is slickHost keeping the overlays the composer shows, so a
// test lays them out each frame as the ui root does.
type clipHost struct {
	slickHost
	mu   sync.Mutex
	ovs  []popups.Overlay
	vids *videoplayer.Controller // the window's players (Videos), nil: none
}

// Videos is the ui root's: the window's players.
func (h *clipHost) Videos() *videoplayer.Controller { return h.vids }

func (h *clipHost) ShowOverlay(o popups.Overlay) {
	h.mu.Lock()
	h.ovs = append(h.ovs, o)
	h.mu.Unlock()
}

// layoutOverlays draws every overlay not dismissed, oldest first.
func (h *clipHost) layoutOverlays(gtx layout.Context) {
	h.mu.Lock()
	ovs := append([]popups.Overlay(nil), h.ovs...)
	h.mu.Unlock()
	for _, o := range ovs {
		if !o.Dismissed() {
			o.Layout(gtx)
		}
	}
}

func (h *clipHost) open() []popups.Overlay {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []popups.Overlay
	for _, o := range h.ovs {
		if !o.Dismissed() {
			out = append(out, o)
		}
	}
	return out
}

// useToneMic points the recorder at a synthetic microphone (the huddle's
// tone device) and counts the start sounds.
func useToneMic(t *testing.T) *atomic.Int32 {
	t.Helper()
	var plays atomic.Int32
	oldDev, oldSound := ClipDevice, ClipStartSound
	ClipDevice = func() huddle.Device { return huddle.NewToneDevice(440) }
	ClipStartSound = func() { plays.Add(1) }
	t.Cleanup(func() { ClipDevice, ClipStartSound = oldDev, oldSound })
	return &plays
}

// Recording a clip and sending it, against fakeslack, the way Slack's
// AudioPopover and UploadQueue do it:
//
//  1. the microphone button opens the recorder above it: the start sound,
//     then recording, the timer and the waveform moving;
//  2. Stop recording makes audio_message.m4a (Opus in MP4) and attaches it:
//     the popover closes and the clip is a pending file with its waveform
//     and duration, its remove tooltip "Remove audio clip";
//  3. it uploads at once: files.getUploadURL {filename, length, subtype
//     slack_audio}, the bytes, files.completeUpload {files: [{id, title,
//     subtype}]};
//  4. Enter sends it: files.share with the clip's file id.
func TestAudioClipRecordStopSend(t *testing.T) {
	app, fs, ch := clipFake(t, map[string]any{"allow_audio_clips": true})
	fs.ProcessDelay = 50 * time.Millisecond
	setClipRecorder(t, true)
	plays := useToneMic(t)
	stubTyping(t)
	old := draftSyncDelay
	draftSyncDelay = 30 * time.Millisecond
	t.Cleanup(func() { draftSyncDelay = old })
	key0 := draftKey(ch, "")
	forget := func() {
		clearDraft(ch, "")
		serverDraftIDs.Delete(key0)
		attStore.Lock()
		delete(attStore.m, key0)
		attStore.Unlock()
	}
	forget()
	t.Cleanup(forget)

	h := &clipHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	c := New(h, ch, "")
	t.Cleanup(c.Close)
	c.Origin = image.Pt(0, 300) // the composer at the window's bottom, room above it
	var ops op.Ops
	frame := func() {
		gtx := composerFrame(c, &ops)
		h.layoutOverlays(gtx)
	}
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			frame()
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	frame()
	frame()
	if !c.ui.itemVisible(crAudio) {
		t.Fatal("no audio clip button")
	}

	// 1. the button
	c.ui.btn[crAudio].Click()
	frame()
	rec := c.ClipRecorder()
	if rec == nil || len(h.open()) != 1 {
		t.Fatalf("the button opened %d overlays, recorder %v", len(h.open()), rec)
	}
	until("recording", func() bool { return rec.State() == vclip.Recording })
	if plays.Load() != 1 {
		t.Errorf("start sound played %d times", plays.Load())
	}
	// the popover's footer sits on the button: top-left, offsetX -64,
	// offsetY 28, the attach button 64 in
	pop := h.open()[0].(*popups.Popover)
	btn := c.ui.rect[crAudio].Add(c.Origin).Add(image.Pt(0, c.top))
	if r := pop.Rect(); r.Min.X != btn.Min.X-64 || r.Max.Y != btn.Min.Y+28 {
		t.Errorf("popover at %v for the button at %v", r, btn)
	}
	until("1.2 s of recording", func() bool { return rec.Elapsed() >= 1200*time.Millisecond })
	drawn := 0
	for _, d := range rec.Dots() {
		if d >= 0 {
			drawn++
		}
	}
	if drawn < 6 {
		t.Errorf("%d waveform dots after 1.2 s", drawn)
	}
	// an outside click is the speedbump, not a close
	c.clipPop.speedbump()
	frame()
	if len(h.open()) != 1 || c.ClipRecorder() == nil {
		t.Fatal("the overlay click closed the recorder")
	}

	// 2. Stop recording attaches the clip
	c.StopClip()
	until("the clip attached", func() bool { return c.att.Len() == 1 })
	if len(h.open()) != 0 || c.ClipRecorder() != nil {
		t.Error("the popover stayed open after attaching")
	}
	c.att.mu.Lock()
	chip := c.att.chips[0]
	name, cm := chip.name, chip.clip
	data := chip.data
	c.att.mu.Unlock()
	if name != "audio_message.m4a" || cm == nil || cm.subtype != "slack_audio" || cm.mime != "audio/mp4" || len(cm.samples) != 24 {
		t.Fatalf("attached %q %+v", name, cm)
	}
	if cm.duration < 1.1 || cm.duration > 3 {
		t.Errorf("duration %.2f s", cm.duration)
	}
	if tip := attRemoveTip(chip.subtype()); tip != "Remove audio clip" {
		t.Errorf("remove tooltip %q", tip)
	}
	info, err := vclip.Probe(data)
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec != "Opus" || info.Channels != 1 || info.SampleRate != 48000 || math.Abs(info.Duration().Seconds()-cm.duration) > 0.3 {
		t.Errorf("clip %s × %d at %d, %v long (timer %.2f s)", info.Codec, info.Channels, info.SampleRate, info.Duration(), cm.duration)
	}

	// 3. the eager upload, with the clip's subtype
	until("the upload", func() bool {
		ups := fs.Uploads()
		return len(ups) == 1 && ups[0].Completed
	})
	up := fs.Uploads()[0]
	a := up.URLArgs
	if up.Name != "audio_message.m4a" || up.Subtype != "slack_audio" || a.Get("length") != strconv.Itoa(len(data)) ||
		a.Has("snippet_type") || a.Has("alt_txt") {
		t.Errorf("files.getUploadURL %v", a)
	}
	if up.CompleteSubtype != "slack_audio" || up.Title != "audio_message.m4a" {
		t.Errorf("files.completeUpload title %q subtype %q", up.Title, up.CompleteSubtype)
	}
	if got := up.Data(); string(got) != string(data) {
		t.Errorf("the POST carried %d bytes, the clip is %d", len(got), len(data))
	}

	// 4. Enter sends it
	until("the clip processed", func() bool {
		c.att.mu.Lock()
		defer c.att.mu.Unlock()
		return len(c.att.chips) == 1 && c.att.chips[0].prepared != nil
	})
	edPress(c.ed, key.NameReturn, 0)
	until("the share", func() bool { return len(fs.Shared()) == 1 })
	sh := fs.Shared()[0]
	if len(sh.IDs) != 1 || sh.IDs[0] != up.ID || sh.Channel != ch {
		t.Errorf("files.share %+v", sh.Args)
	}
	if fs.CallCount("files.getUploadURL") != 1 {
		t.Errorf("uploaded %d times", fs.CallCount("files.getUploadURL"))
	}
	// the processed file is Slack's voice clip
	var fi struct {
		File struct {
			Subtype    string `json:"subtype"`
			Mimetype   string `json:"mimetype"`
			DurationMS int64  `json:"duration_ms"`
		} `json:"file"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.C.Call(ctx, "files.info", url.Values{"file": {up.ID}}, &fi); err != nil {
		t.Fatal(err)
	}
	if fo := fi.File; fo.Subtype != "slack_audio" || fo.Mimetype != "audio/mp4" || fo.DurationMS < 1000 {
		t.Errorf("the shared clip: %+v", fo)
	}
}

// Escape or the × closes the recorder and drops the recording; nothing is
// attached or uploaded. A microphone that won't open shows
// MicrophoneAccessErrorDialog and leaves the popover up.
func TestAudioClipCancelAndMicError(t *testing.T) {
	app, fs, ch := clipFake(t, map[string]any{"allow_audio_clips": true})
	setClipRecorder(t, true)
	useToneMic(t)
	stubTyping(t)
	h := &clipHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	c := New(h, ch, "")
	t.Cleanup(c.Close)
	c.Origin = image.Pt(0, 300)
	var ops op.Ops
	frame := func() { h.layoutOverlays(composerFrame(c, &ops)) }
	frame()
	frame()
	c.ui.btn[crAudio].Click()
	frame()
	rec := c.ClipRecorder()
	if rec == nil {
		t.Fatal("no recorder")
	}
	deadline := time.Now().Add(5 * time.Second)
	for rec.State() != vclip.Recording && time.Now().Before(deadline) {
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	c.clipPop.cancel.Click()
	frame()
	if rec.State() != vclip.Cancelled || c.ClipRecorder() != nil || len(h.open()) != 0 {
		t.Fatalf("after ×: state %s, open overlays %d", rec.State(), len(h.open()))
	}
	frame()
	if c.att.Len() != 0 || fs.CallCount("files.getUploadURL") != 0 {
		t.Errorf("a cancelled clip attached (%d) or uploaded (%d)", c.att.Len(), fs.CallCount("files.getUploadURL"))
	}

	ClipDevice = func() huddle.Device { return deadMic{} }
	c.ui.btn[crAudio].Click()
	frame()
	deadline = time.Now().Add(5 * time.Second)
	for len(h.open()) < 2 && time.Now().Before(deadline) {
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	ovs := h.open()
	if len(ovs) != 2 {
		t.Fatalf("%d overlays after the microphone failed; want the popover and the dialog", len(ovs))
	}
	m, ok := ovs[1].(*popups.Modal)
	if !ok {
		t.Fatalf("second overlay %T", ovs[1])
	}
	m.Dismiss()
	frame()
	if c.ClipRecorder() == nil {
		t.Error("closing the dialog closed the recorder")
	}
}

// deadMic is a microphone that won't open (getUserMedia's error).
type deadMic struct{}

func (deadMic) Start(func([]int16), func([]int16)) error { return errDeadMic }
func (deadMic) Close()                                   {}

var errDeadMic = &deadMicErr{}

type deadMicErr struct{}

func (*deadMicErr) Error() string { return "no microphone" }

// The gate's other terms (Qre6): a Slack Connect channel also needs
// allow_audio_clip_sharing_slack_connect; the edit composer has no clip
// buttons.
func TestAudioClipGate(t *testing.T) {
	app, _, ch := clipFake(t, map[string]any{"allow_audio_clips": true})
	setClipRecorder(t, true)
	stubTyping(t)
	c := New(&slickHost{app: app, th: theme.New(false)}, ch, "")
	t.Cleanup(c.Close)
	if !c.audioClipOn() {
		t.Fatal("off with the pref on")
	}
	setClipRecorder(t, false)
	if c.audioClipOn() {
		t.Error("on without a recorder (window.MediaRecorder)")
	}
	setClipRecorder(t, true)
	// a Slack Connect channel (is_ext_shared)
	app.S.PutConv(slack.Conversation{ID: ch, Name: "shared-clips", IsChannel: true, IsMember: true, IsExtShared: true})
	if c.audioClipOn() {
		t.Error("on in a Slack Connect channel without allow_audio_clip_sharing_slack_connect")
	}
	app.SetTeamPrefs(map[string]json.RawMessage{"allow_audio_clips": json.RawMessage("true"),
		"allow_audio_clip_sharing_slack_connect": json.RawMessage("true")})
	if !c.audioClipOn() {
		t.Error("off in a Slack Connect channel with both prefs")
	}
	for _, c := range []struct {
		in   float64
		want string
	}{{0, "0:00"}, {59.9, "0:59"}, {61, "1:01"}, {300, "5:00"}, {3725, "1:02:05"}} {
		if got := audioTime(math.Floor(c.in)); got != c.want {
			t.Errorf("secondsToAudioTime(%v) = %q, want %q", c.in, got, c.want)
		}
	}
	for sub, want := range map[string]string{"slack_audio": "Remove audio clip", "audio_story": "Remove audio clip",
		"slack_video": "Remove video clip", "": "Remove file"} {
		if got := attRemoveTip(sub); got != want {
			t.Errorf("attRemoveTip(%q) = %q", sub, got)
		}
	}
}
