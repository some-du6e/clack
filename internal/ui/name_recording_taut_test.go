package ui

// Taut's CustomNameRecording (taut plugins/CustomNameRecording.tsx) in Edit
// your profile: it patches EditAudioButton, which the Name recording field
// renders as Slack's BaseEditProfileField does, with Slack's props
// (popups/profile_name_rec.go), and its Upload audio button follows the
// field's own control (<Original/>). Synthetic data only.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/media/mediatest"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestTautCustomNameRecordingUploadButton(t *testing.T) {
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the ui-taut workflow builds Taut's plugins and runs this")
	}
	code, err := os.ReadFile(filepath.Join(dir, "CustomNameRecording.js"))
	if err != nil {
		t.Fatal(err)
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	e := benchStart(t, fs)
	shots := os.Getenv("TAUT_SHOTS_DIR")
	if shots != "" {
		if w, err := pixeltest.NewWindow(e.size.X, e.size.Y); err == nil {
			e.gpu = w // perfEnv.shutdown releases it (a second Release crashes headless)
		} else {
			t.Logf("no GPU renderer (%v): no screenshot", err)
		}
	}
	e.waitReady()
	e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id, err := tt.Import(ctx, code, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	e.until("the plugin's patch", 30*time.Second, func() bool { return slackc.Patched("EditAudioButton") })

	popups.OpenEditProfile(e.a, popups.EditProfileOpts{})
	if !e.waitTree(30*time.Second, "Upload audio", "taut-name-recording__upload") {
		t.Fatal("no tree with CustomNameRecording's Upload audio: Edit profile renders no EditAudioButton")
	}
	var drawn map[string]bool
	e.until("the field's own control through <Original/>", 10*time.Second, func() bool {
		drawn = frameStrings(e.perfEnv)
		return drawn["Record Audio Clip"]
	})
	for i := 0; i < 5; i++ {
		e.frame()
	}
	e.shotTo(t, shots, "taut-customnamerecording-edit-profile")
}

// TestTautCustomNameRecordingConvertsAndUploads: Taut's CustomNameRecording
// uploads a name recording from a file, in the whole client. Its Upload
// audio button (in Edit profile's Name recording field, as above) opens a
// chooser for audio/*; a WAV isn't an MP3, so the plugin reads it with
// decodeAudioData and converts it with a MediaRecorder (audio/mp4: Opus in
// MP4), then uploads audio_name_pronunciation.m4a typed audio/mp4 with the
// name recording's subtype, slack_name_pronunciation, in files.getUploadURL
// and completeUpload's files entry, as Slack's UploadQueue sends a File's
// subtype. The field takes the new file (onChangeFile): Edit profile reads
// it (files.info) for its tile. Synthetic data only.
func TestTautCustomNameRecordingConvertsAndUploads(t *testing.T) {
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the ui-taut workflow builds Taut's plugins and runs this")
	}
	code, err := os.ReadFile(filepath.Join(dir, "CustomNameRecording.js"))
	if err != nil {
		t.Fatal(err)
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	var mu sync.Mutex
	var accepts []string
	pickFilesHook = func(accept string, multiple bool) ([]jsrt.PickedFile, error) {
		mu.Lock()
		accepts = append(accepts, accept)
		mu.Unlock()
		return []jsrt.PickedFile{{Name: "tones.wav", Type: "audio/wav", Data: mediatest.TonesWAV, LastModified: time.Unix(1700000000, 0)}}, nil
	}
	t.Cleanup(func() { pickFilesHook = nil })
	e := benchStart(t, fs)
	e.waitReady()
	e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id, err := tt.Import(ctx, code, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	e.until("the plugin's patch", 30*time.Second, func() bool { return slackc.Patched("EditAudioButton") })
	popups.OpenEditProfile(e.a, popups.EditProfileOpts{})
	if !e.waitTree(30*time.Second, "taut-name-recording__upload") {
		t.Fatal("no tree with CustomNameRecording's Upload audio")
	}
	click := onClickWithClass([]byte(e.treeWith("taut-name-recording__upload")), "taut-name-recording__upload")
	if click == 0 {
		t.Fatal("the Upload audio button has no onClick")
	}
	before, infos := len(fs.Uploads()), fs.CallCount("files.info")
	tt.Bridge().Call(click, []byte("[]"), nil)
	var up fakeslack.UploadedFile
	e.until("the converted recording uploaded", 60*time.Second, func() bool {
		all := fs.Uploads()
		if len(all) <= before || !all[len(all)-1].Completed {
			return false
		}
		up = all[len(all)-1]
		return true
	})
	mu.Lock()
	got := append([]string(nil), accepts...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "audio/*" {
		t.Errorf("the choosers opened: %q, want one for audio/*", got)
	}
	if up.URLArgs.Get("filename") != "audio_name_pronunciation.m4a" || up.PartType != "audio/mp4" || up.Subtype != "slack_name_pronunciation" ||
		up.CompleteSubtype != "slack_name_pronunciation" || up.Bytes == 0 {
		t.Errorf("the upload: %s typed %q, subtype %q/%q, %d bytes; want audio_name_pronunciation.m4a, audio/mp4, slack_name_pronunciation in both",
			up.URLArgs.Get("filename"), up.PartType, up.Subtype, up.CompleteSubtype, up.Bytes)
	}
	e.until("Edit profile reading the new recording", 20*time.Second, func() bool { return fs.CallCount("files.info") > infos })
}

// onClickWithClass is the onClick function id of the first node of a host
// tree (JSON) whose className has cls.
func onClickWithClass(tree []byte, cls string) int64 {
	var nodes []any
	if json.Unmarshal(tree, &nodes) != nil {
		return 0
	}
	var walk func([]any) int64
	walk = func(ns []any) int64 {
		for _, x := range ns {
			m, _ := x.(map[string]any)
			if m == nil {
				continue
			}
			p, _ := m["p"].(map[string]any)
			if c, _ := p["className"].(string); strings.Contains(" "+c+" ", " "+cls+" ") {
				if f, _ := p["onClick"].(map[string]any); f != nil {
					if id, ok := f["$fn"].(float64); ok {
						return int64(id)
					}
				}
			}
			if kids, _ := m["c"].([]any); kids != nil {
				if id := walk(kids); id != 0 {
					return id
				}
			}
		}
		return 0
	}
	return walk(nodes)
}
