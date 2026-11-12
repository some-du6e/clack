package messages

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// HcaStatus: its main half asks auth.hackclub.com (answered here) and its
// BaseMessageSender patch puts slick-hca-unverified / slick-hca-over-18 on
// the sender, whose rule (text-decoration: underline wavy <color>
// !important on the class and its .c-message__sender_button) Clack's
// native sender name draws: the setting's colour under an unverified
// person's name, its over-18 colour under an over-18 one's, nothing under
// a verified person's. Pixels from the GPU renderer; a screenshot goes to
// SLICK_SHOTS_DIR.
func TestSlickViewHcaStatus(t *testing.T) {
	const ch = "C0HCASTAT1"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "hca", IsChannel: true, IsMember: true, NumMembers: 3})
		s.SetLatest(ch, []slack.Message{
			{Type: "message", TS: "1700000401.000100", User: "UALICE", Text: "a synthetic hello from someone unverified"},
			{Type: "message", TS: "1700000501.000100", User: "UBOB", Text: "a synthetic reply from someone over 18"},
			{Type: "message", TS: "1700000601.000100", User: "UCAROL001", Text: "and one from someone verified"},
		}, false)
		s.PutUsers([]slack.User{{ID: "UCAROL001", Name: "carol", RealName: "Carol Example", Profile: slack.Profile{DisplayName: "carol"}}})
	})
	base := "https://auth.hackclub.com/api/external/check?slack_id="
	e.fetches[base+"UALICE"] = `{"result":"not_verified"}`
	e.fetches[base+"UBOB"] = `{"result":"verified_but_over_18"}`
	e.fetches[base+"UCAROL001"] = `{"result":"verified_eligible"}`
	v := NewChannelView(e.h, ch)
	v.Preview = true
	size := image.Pt(900, 700)
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		paint.FillShape(ops, e.h.th.C.Bg, clip.Rect{Max: size}.Op())
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(size), Now: time.Now()}
		v.Layout(gtx)
	}
	red := color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}   // unverifiedColor's default
	amber := color.NRGBA{R: 0xd9, G: 0x77, B: 0x06, A: 0xff} // over18Color's default
	count := func() (int, int, *image.RGBA) {
		for i := 0; i < 5; i++ {
			frame()
		}
		img := pixeltest.Render(t, size.X, size.Y, ops)
		// a 1px wavy line is antialiased: count the pixels at least half
		// its colour over the list's background
		bg := e.h.th.C.Bg
		return pixeltest.Ink(img, img.Bounds(), red, bg), pixeltest.Ink(img, img.Bounds(), amber, bg), img
	}
	red0, amber0, _ := count() // the list before the plugin: its own reds (if any)

	e.install(t, "HcaStatus", nil)
	patched(t, "BaseMessageSender")
	e.waitFor(t, "both markers on the senders", frame, func() bool {
		return e.ui.anyTree("slick-hca-unverified") && e.ui.anyTree("slick-hca-over-18")
	})
	nRed, nAmber, img := count()
	if dir := os.Getenv("SLICK_SHOTS_DIR"); dir != "" {
		if f, err := os.Create(filepath.Join(dir, "hca-status.png")); err == nil {
			_ = png.Encode(f, img)
			f.Close()
		}
	}
	if nRed-red0 < 12 {
		t.Errorf("%d new pixels of the unverified underline (#e01e5a; %d before), want the wavy line under alice's name", nRed-red0, red0)
	}
	if nAmber-amber0 < 12 {
		t.Errorf("%d new pixels of the over-18 underline (#d97706; %d before), want the wavy line under bob's name", nAmber-amber0, amber0)
	}
	t.Logf("underline pixels: unverified %d (was %d), over 18 %d (was %d)", nRed, red0, nAmber, amber0)
}
