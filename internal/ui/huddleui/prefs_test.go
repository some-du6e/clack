package huddleui

import (
	"encoding/json"
	"image"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

// overlayHost is a popups.Host that keeps what ShowOverlay got.
type overlayHost struct {
	hcHost
	shown []popups.Overlay
}

func (h *overlayHost) ShowOverlay(o popups.Overlay)              { h.shown = append(h.shown, o) }
func (h *overlayHost) OpenEmojiPicker(image.Point, func(string)) {}
func (h *overlayHost) OpenProfile(image.Point, string)           {}
func (h *overlayHost) OpenMessageMenu(image.Point, core.Message) {}
func (h *overlayHost) OpenLightbox(richtext.Image, core.Message) {}
func (h *overlayHost) CloseSecondary()                           {}

func setPrefs(app *core.App, kv map[string]any) {
	raw := map[string]json.RawMessage{}
	for k, v := range kv {
		b, _ := json.Marshal(v)
		raw[k] = b
	}
	app.S.SetRawPrefs(raw)
}

// Audio & video > "Automatically turn on captions when you’re in a huddle"
// (huddles_cc_by_default) starts the session with captions shown; "Send a
// warning if you’re starting a huddle in a channel with more than 150
// members" (huddles_show_shouty_rooster) asks before starting one there.
func TestHuddleJoinPrefs(t *testing.T) {
	e := newShotEnv(t, false)
	app := e.h.app
	setPrefs(app, map[string]any{"huddles_cc_by_default": true})
	if !e.c.options(app).Captions {
		t.Fatal("huddles_cc_by_default didn't turn captions on")
	}
	setPrefs(app, map[string]any{"huddles_cc_by_default": false})
	if e.c.options(app).Captions {
		t.Fatal("captions on with huddles_cc_by_default off")
	}

	oh := &overlayHost{hcHost: *e.h}
	e.c.h = oh
	app.S.PutConv(slack.Conversation{ID: "C0BIG", Name: "announcements", IsChannel: true, IsMember: true, NumMembers: 151})
	ran := false
	if !e.c.shoutyRooster("C0BIG", func() { ran = true }) || len(oh.shown) != 1 || ran {
		t.Fatalf("no warning starting a huddle in a 151-member channel (shown %d)", len(oh.shown))
	}
	oh.shown[0].(*popups.Modal).PressDefault()
	if !ran {
		t.Fatal("OK didn't start the huddle")
	}
	if e.c.shoutyRooster(hcChannel, func() {}) {
		t.Fatal("warned in a 2-member channel")
	}
	setPrefs(app, map[string]any{"huddles_show_shouty_rooster": false})
	if e.c.shoutyRooster("C0BIG", func() {}) {
		t.Fatal("warned with huddles_show_shouty_rooster off")
	}
}
