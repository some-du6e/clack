package composer

import (
	"context"
	"encoding/json"
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// clipFake is a fakeslack session whose team allows audio clips (team
// prefs loaded), and a channel the composer can post in.
func clipFake(t *testing.T, prefs map[string]any) (*core.App, *fakeslack.Server, string) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.TeamPrefs = prefs
	app := core.New(slack.New(fs.Creds()), store.New(""))
	app.ServeOffline()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.LoadTeamPrefs(ctx); err != nil {
		t.Fatal(err)
	}
	return app, fs, fs.ChannelIDs()[2]
}

// composerFrame lays c out at its Origin in a 900×600 window and returns
// the window's frame context (for the overlays).
func composerFrame(c *Composer, ops *op.Ops) layout.Context {
	ops.Reset()
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
	slackc.Frame(gtx)
	g := gtx
	g.Constraints.Max = image.Pt(900, 600-c.Origin.Y)
	t := op.Offset(c.Origin).Push(ops)
	c.Layout(g)
	t.Pop()
	return gtx
}

// With a recorder and the team's allow_audio_clips, InputContainer's
// canPostAudioMessage (Qre6 hP) holds: MessagePaneInput passes
// enableAudioButton and TextyButtons draws the audio clip button. The
// video button stays out (Clack doesn't record video clips yet), and the
// pref turned off hides the audio one again.
func TestAudioClipButtonWithARecorder(t *testing.T) {
	app, _, ch := clipFake(t, map[string]any{"allow_audio_clips": true, "allow_video_clips": true})
	setClipRecorder(t, true)
	stubTyping(t)
	c := New(&slickHost{app: app, th: theme.New(false)}, ch, "")
	t.Cleanup(c.Close)
	p := c.inputProps()
	if !p.Bool("enableAudioButton") || p.Bool("enableStoryButton") {
		t.Fatalf("enableAudioButton %v, enableStoryButton %v; want the audio button only", p.Bool("enableAudioButton"), p.Bool("enableStoryButton"))
	}
	var ops op.Ops
	composerFrame(c, &ops)
	composerFrame(c, &ops)
	if c.cs.Hide&(1<<crAudio) != 0 || c.cs.Hide&(1<<crVideo) == 0 {
		t.Fatalf("Hide %b: want the video button hidden and the audio one shown", c.cs.Hide)
	}
	if !c.ui.itemVisible(crAudio) || c.ui.itemVisible(crVideo) || c.ui.rect[crAudio].Empty() {
		t.Fatalf("audio visible %v at %v, video visible %v", c.ui.itemVisible(crAudio), c.ui.rect[crAudio], c.ui.itemVisible(crVideo))
	}
	app.SetTeamPrefs(map[string]json.RawMessage{"allow_audio_clips": json.RawMessage("false")})
	composerFrame(c, &ops)
	if c.cs.Hide&(1<<crAudio) == 0 || c.inputProps().Bool("enableAudioButton") {
		t.Error("the audio button stayed with allow_audio_clips off")
	}
}
