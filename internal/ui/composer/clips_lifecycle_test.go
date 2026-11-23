package composer

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	vclip "github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/media/mediatest"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// The recorder's life with its composer and session: a stop before any
// audio, the composer going (another conversation, the session ending),
// and a draft already holding 10 files.

// silentMic is a microphone that opens and never delivers a frame.
type silentMic struct{}

func (silentMic) Start(func([]int16), func([]int16)) error { return nil }
func (silentMic) Close()                                   {}

// noPanicC runs f, failing the test with what panicked instead of ending
// the test binary.
func noPanicC(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("%s panicked: %v", what, p)
		}
	}()
	f()
}

// clipEnv is a composer on fakeslack with a recorder host, its draft and
// attachments forgotten after the test.
type clipEnv struct {
	t   *testing.T
	fs  *fakeslack.Server
	app *core.App
	ch  string
	h   *clipHost
	c   *Composer
	ops op.Ops
}

func newClipEnv(t *testing.T, dev func() huddle.Device) *clipEnv {
	t.Helper()
	app, fs, ch := clipFake(t, map[string]any{"allow_audio_clips": true})
	setClipRecorder(t, true)
	stubTyping(t)
	oldDev, oldSound := ClipDevice, ClipStartSound
	ClipDevice, ClipStartSound = dev, func() {}
	t.Cleanup(func() { ClipDevice, ClipStartSound = oldDev, oldSound })
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
	e := &clipEnv{t: t, fs: fs, app: app, ch: ch}
	e.h = &clipHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	e.c = New(e.h, ch, "")
	t.Cleanup(func() { noPanicC(t, "the composer closing", e.c.Close) })
	e.c.Origin = image.Pt(0, 300) // the composer at the window's bottom, room above it
	e.frame()
	e.frame()
	return e
}

// frame lays out the composer, then the overlays, as the ui root does.
func (e *clipEnv) frame() { e.h.layoutOverlays(composerFrame(e.c, &e.ops)) }

// overlaysOnly is a frame that doesn't draw the composer (another
// conversation's view is up), then the overlays.
func (e *clipEnv) overlaysOnly() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
	e.h.layoutOverlays(gtx)
}

func (e *clipEnv) until(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
}

// record opens the recorder from the microphone button and waits until it
// records.
func (e *clipEnv) record() *vclip.Recorder {
	e.t.Helper()
	e.c.ui.btn[crAudio].Click()
	e.frame()
	rec := e.c.ClipRecorder()
	if rec == nil {
		e.t.Fatal("the button opened no recorder")
	}
	e.until("recording", func() bool { return rec.State() == vclip.Recording })
	return rec
}

// sawDialog reports whether a MicrophoneAccessErrorDialog (a Modal) was
// ever shown.
func (e *clipEnv) sawDialog() bool {
	e.h.mu.Lock()
	defer e.h.mu.Unlock()
	for _, o := range e.h.ovs {
		if _, ok := o.(*popups.Modal); ok {
			return true
		}
	}
	return false
}

// ✓ (Return, Space) as the timer starts, or a microphone that delivers
// nothing, even at the 5:00 stop: no audio reached the encoder, and the
// stop makes Slack's empty audio_message.m4a, which attaches and uploads
// like any clip (StreamManager's recordingend Blob of no chunks; the
// hook's `if (a)` holds for it) as the popover closes. It isn't a
// microphone error: MicrophoneAccessErrorDialog said "Can't access
// microphone" and left the popover up, and then any of its close paths
// (×, Escape, the composer closing: Cancel) closed the recorder's ticker
// channel a second time and panicked the UI goroutine.
func TestAudioClipStopBeforeFirstPacket(t *testing.T) {
	e := newClipEnv(t, func() huddle.Device { return silentMic{} })
	rec := e.record()
	e.c.StopClip()
	e.until("the empty clip attached and the popover closed", func() bool {
		return e.c.att.Len() == 1 && len(e.h.open()) == 0
	})
	if rec.State() != vclip.Stopped {
		t.Errorf("state %s", rec.State())
	}
	if e.sawDialog() {
		t.Error("a stop with nothing recorded showed MicrophoneAccessErrorDialog")
	}
	e.c.att.mu.Lock()
	n, m := len(e.c.att.chips[0].data), e.c.att.chips[0].clip
	e.c.att.mu.Unlock()
	if n != 0 || m == nil || m.subtype != "slack_audio" || m.duration > 1 || len(m.samples) != vclip.WaveDots {
		t.Errorf("the clip: %d bytes, %+v", n, m)
	}
	// Slack's getUploadURL for what MediaRecorder gave it: length 0
	e.until("its upload slot", func() bool { return len(e.fs.Uploads()) == 1 })
	if a := e.fs.Uploads()[0].URLArgs; a.Get("length") != "0" || a.Get("subtype") != "slack_audio" || a.Get("filename") != "audio_message.m4a" {
		t.Errorf("files.getUploadURL %v", a)
	}
	// the close paths after it: Escape, a late Cancel, the composer closing
	// (the cleanup)
	noPanicC(t, "Escape", func() { edPress(e.c.ed, key.NameEscape, 0); e.frame() })
	noPanicC(t, "Cancel after the stop", rec.Cancel)
}

// The recorder is the composer's: when another conversation opens (Mod+K,
// Alt+↑/↓, the Switch to Channel menu) or the pane closes, the composer
// isn't drawn, and the popover goes with it as Slack's AudioPopover
// unmounts with MessagePaneInput: the recording is dropped, ✓ can't
// attach it to the conversation that was, nothing uploads, and the old
// composer doesn't take the focus.
func TestAudioClipGoesWithItsComposer(t *testing.T) {
	e := newClipEnv(t, func() huddle.Device { return huddle.NewToneDevice(440) })
	rec := e.record()
	time.Sleep(300 * time.Millisecond) // some audio in the encoder
	e.c.focusReq = false
	e.overlaysOnly()
	if rec.State() != vclip.Cancelled || len(e.h.open()) != 0 || e.c.ClipRecorder() != nil {
		t.Fatalf("a frame without its composer: state %s, %d overlays open", rec.State(), len(e.h.open()))
	}
	if e.c.focusReq {
		t.Error("the composer that went asked for the focus")
	}
	// back to the conversation: no recorder to stop, nothing attached
	e.frame()
	e.c.StopClip()
	for i := 0; i < 20; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if e.c.att.Len() != 0 || e.fs.CallCount("files.getUploadURL") != 0 {
		t.Errorf("the dropped recording attached (%d) or uploaded (%d)", e.c.att.Len(), e.fs.CallCount("files.getUploadURL"))
	}
}

// The session ending (ui's showLogin, switchAccount, signOut) closes every
// recorder: CloseClips drops the recording, so nothing attaches and
// nothing uploads, in this session or through the next. And a recording
// that stops after its composer's session changed (CloseClips missed it)
// drops its clip: it never attaches, or uploads, in another account.
func TestAudioClipNeverCrossesSessions(t *testing.T) {
	e := newClipEnv(t, func() huddle.Device { return huddle.NewToneDevice(440) })
	rec := e.record()
	e.c.focusReq = false
	CloseClips()
	e.frame()
	if rec.State() != vclip.Cancelled || len(e.h.open()) != 0 || e.c.focusReq {
		t.Fatalf("after CloseClips: state %s, %d overlays open, focus asked %v", rec.State(), len(e.h.open()), e.c.focusReq)
	}

	rec = e.record()
	time.Sleep(300 * time.Millisecond)
	other, otherFS, _ := clipFake(t, map[string]any{"allow_audio_clips": true})
	e.h.app = other // the composer's host now serves another session
	e.c.StopClip()
	e.until("the popover closed", func() bool { return len(e.h.open()) == 0 })
	if rec.State() != vclip.Stopped {
		t.Errorf("state %s", rec.State())
	}
	for i := 0; i < 20; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if e.c.att.Len() != 0 || otherFS.CallCount("files.getUploadURL") != 0 || e.fs.CallCount("files.getUploadURL") != 0 {
		t.Errorf("a clip recorded in one session attached (%d) or uploaded (%d, %d)", e.c.att.Len(),
			otherFS.CallCount("files.getUploadURL"), e.fs.CallCount("files.getUploadURL"))
	}
}

// With 10 files attached a recording still attaches, as Slack's eager
// upload does (use_eager_upload has no count check): the draft holds 11
// and can't be sent (isComposerDraftSendable's tooManyFiles: the send
// button is off, Enter does nothing) until one goes. It was rejected
// after it was made, and the recording lost.
func TestAudioClipPastTenFiles(t *testing.T) {
	e := newClipEnv(t, func() huddle.Device { return huddle.NewToneDevice(440) })
	for i := 0; i < attMaxFiles; i++ {
		e.c.AttachFile("notes"+strconv.Itoa(i)+".txt", []byte("file "+strconv.Itoa(i)))
	}
	e.until("10 files", func() bool { return e.c.att.Len() == attMaxFiles })
	rec := e.record()
	time.Sleep(400 * time.Millisecond)
	e.c.StopClip()
	e.until("the clip, an 11th", func() bool { return e.c.att.Len() == attMaxFiles+1 })
	if rec.State() != vclip.Stopped {
		t.Errorf("state %s", rec.State())
	}
	e.frame()
	if !e.c.tooManyFiles() || !e.c.ui.cur.sendOff() {
		t.Errorf("11 files: tooManyFiles %v, send off %v", e.c.tooManyFiles(), e.c.ui.cur.sendOff())
	}
	shared := len(e.fs.Shared())
	edPress(e.c.ed, key.NameReturn, 0)
	for i := 0; i < 20; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(e.fs.Shared()); n != shared || e.fs.CallCount("chat.postMessage") != 0 {
		t.Errorf("Enter sent a draft of 11 files (files.share %d, chat.postMessage %d)", n-shared, e.fs.CallCount("chat.postMessage"))
	}
	e.c.att.mu.Lock()
	first := e.c.att.chips[0]
	e.c.att.mu.Unlock()
	e.c.att.remove(first)
	e.frame()
	if e.c.tooManyFiles() || e.c.ui.cur.sendOff() {
		t.Errorf("10 files again: tooManyFiles %v, send off %v", e.c.tooManyFiles(), e.c.ui.cur.sendOff())
	}
}

// Once uploaded the pending clip plays, as Slack's PendingFile draws
// FileAudio for the processed file (fileIsPlayableAudio) where
// PendingAudioMessageUpload drew the recording's bytes with its play
// button disabled: the window's player in its mini style, with no speed
// control or More actions, at the natural rate whatever
// media_playback_speed says (ignoresPlaybackSpeedPref), reading the
// uploaded file.
func TestAudioClipPendingCardPlays(t *testing.T) {
	e := newClipEnv(t, func() huddle.Device { return huddle.NewToneDevice(440) })
	var mu sync.Mutex
	var clip []byte
	var opened []string
	noCard := func(func([]int16)) (io.Closer, error) { return nil, errors.New("no sound card (the test's)") }
	ctl := videoplayer.New(videoplayer.Env{
		Manager: media.NewManager(media.NewChain(nil, &goaudio.Backend{Output: noCard, Logf: t.Logf})),
		Open: func(ctx context.Context, u string, off int64) (io.ReadCloser, media.Info, error) {
			mu.Lock()
			opened = append(opened, u)
			b := clip
			mu.Unlock()
			_, o := mediatest.Source("FCLIP", b, false)
			return o.Open(ctx, off)
		},
		Pref: func(k string) (json.RawMessage, bool) {
			if k == "media_playback_speed" {
				return json.RawMessage("2"), true
			}
			return nil, false
		},
		Repaint: func() {},
		Theme:   func() *theme.Theme { return e.h.th },
	})
	t.Cleanup(ctl.CloseAll)
	e.h.vids = ctl

	e.record()
	time.Sleep(1200 * time.Millisecond)
	e.c.StopClip()
	var fid string
	e.until("the clip uploaded and processed", func() bool {
		e.c.att.mu.Lock()
		defer e.c.att.mu.Unlock()
		if len(e.c.att.chips) != 1 || e.c.att.chips[0].prepared == nil || e.c.att.chips[0].prepared.File == nil {
			return false
		}
		fid = e.c.att.chips[0].prepared.ID
		mu.Lock()
		clip = e.c.att.chips[0].data
		mu.Unlock()
		return true
	})
	e.frame()
	st, ok := ctl.TileState("composer", "", "", fid)
	if !ok || st.AudioPlay.Empty() || st.AudioScrub.Empty() {
		t.Fatalf("the uploaded clip's card draws no player: %v %+v", ok, st)
	}
	if !st.AudioSpeed.Empty() || !st.AudioMore.Empty() {
		t.Errorf("the mini player drew the speed control %v or More %v", st.AudioSpeed, st.AudioMore)
	}
	if !ctl.Click("composer", "", "", fid) {
		t.Fatal("no tile to click")
	}
	e.until("the clip playing", func() bool {
		st, _ := ctl.TileState("composer", "", "", fid)
		return st.Playing && st.Elapsed > 300*time.Millisecond
	})
	st, _ = ctl.TileState("composer", "", "", fid)
	if st.Rate != 1 {
		t.Errorf("rate %v with media_playback_speed 2, want 1 (ignoresPlaybackSpeedPref)", st.Rate)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(opened) == 0 || !strings.Contains(opened[0], fid) && !strings.Contains(opened[0], "audio_message") {
		t.Errorf("the player read %v, not the uploaded file", opened)
	}
}
