package ui

// A custom app's plan posts in a channel, through the whole window
// (booted against internal/fakeslack, Size.AppPlans): from history, and
// live (the post, then chat.update's message_changed), light and dark.
// No plan post may draw blank under its sender. BK_AGENT_SHOTS_DIR keeps
// the frames (bk-agent-shots.yml). Synthetic data only.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestAppPlanWindow(t *testing.T) {
	sz := fakeslack.Small
	sz.AppPlans = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.size = image.Pt(1280, 900)
	if os.Getenv("BK_AGENT_SHOTS_DIR") != "" {
		if e.gpu != nil {
			e.gpu.Release()
		}
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	ch := fakeslack.AppPlanChannelID
	e.openChannel(ch)
	e.until("#plans' history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(800 * time.Millisecond)
	e.toLatest()
	msgs, names := fakeslack.AppPlanMessages()
	want := map[string]string{}
	for i, n := range names {
		if n == "plan" || n == "mixed" {
			want[msgs[i].TS] = n
		}
	}
	check := func(stage string, want map[string]string) {
		t.Helper()
		v := e.ownSendList(ch)
		seen := map[string]messages.SenderView{}
		e.until(stage+": the plan rows drawn", 10*time.Second, func() bool {
			for _, sv := range v.Senders() {
				seen[sv.TS] = sv
			}
			for ts := range want {
				if _, ok := seen[ts]; !ok {
					return false
				}
			}
			return true
		})
		for ts, name := range want {
			sv := seen[ts]
			t.Logf("%s %s: name %q body %v slots %v text %q", stage, name, sv.Name, sv.Body, sv.Slots, sv.Text)
			if len(sv.Slots) != 1 || sv.Slots[0].Dy() < 19 || sv.Body.Dy() < 19 {
				t.Errorf("%s %s: blank under the sender: body %v slots %v", stage, name, sv.Body, sv.Slots)
			}
		}
	}
	check("history", want)
	e.appPlanShot(t, "app-plan-window-history")

	// live: the post, then its edit
	go e.a.Core().SetRealtime(true)
	e.until("the socket", 15*time.Second, func() bool { return e.a.data.m.Connected })
	ts := fs.PushAppPlanLive()
	e.settle(600 * time.Millisecond)
	e.toLatest()
	check("live", map[string]string{ts: "live"})
	e.appPlanShot(t, "app-plan-window-live")
}

// appPlanShot saves the window light and dark (BK_AGENT_SHOTS_DIR).
func (e *perfEnv) appPlanShot(t *testing.T, name string) {
	dir := os.Getenv("BK_AGENT_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.settle(400 * time.Millisecond)
		e.frame()
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		f, err := os.Create(filepath.Join(dir, name+"-"+map[bool]string{false: "light", true: "dark"}[dark]+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	e.setDark(false)
	e.settle(200 * time.Millisecond)
}
