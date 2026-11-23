package ui

// Taut's CustomNameRecording (taut plugins/CustomNameRecording.tsx) in Edit
// your profile: it patches EditAudioButton, which the Name recording field
// renders as Slack's BaseEditProfileField does, with Slack's props
// (popups/profile_name_rec.go), and its Upload audio button follows the
// field's own control (<Original/>). Synthetic data only.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
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
			e.gpu = w
			t.Cleanup(w.Release)
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
