package huddleui

import (
	"image"
	"testing"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Pictures of the incoming invite (CLACK_SHOTS: invite-*.png): the invite
// window (a DM, a channel, after Be there soon), the sidebar card, and the
// stickers on tiles and in the toolbar.
func TestHuddleShotsInvite(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		e := newShotEnv(t, dark)
		e.h.app.S.PutConv(slack.Conversation{ID: dmChannel, IsIM: true, User: huddle.FakeRemoteUser})
		c := e.c
		th := theme.New(dark)
		k := invInkFor(th)
		var dec, soon, join widget.Clickable

		win := func(name string, inv *incoming) *image.RGBA {
			c.mu.Lock()
			c.inv = inv
			c.mu.Unlock()
			return render(t, name+"-"+mode, 380, 280, func(gtx C) {
				layoutInviteWindow(gtx, th, c, c.view(inv), &dec, &soon, &join)
			})
		}
		img := win("invite-window-dm", &incoming{Channel: dmChannel, Call: "R1", Sender: huddle.FakeRemoteUser, status: "pending"})
		// the window's base-inv-pry background, and the Join circle in base-pry
		if n := pixeltest.Count(img, image.Rect(4, 70*shotScale, 40*shotScale, 100*shotScale), k.bg, 6); n < 30*30*shotScale*shotScale/2 {
			t.Fatalf("%s: window background (%d px)", mode, n)
		}
		if n := pixeltest.Count(img, image.Rect(0, 180*shotScale, 380*shotScale, 260*shotScale), k.join, 10); n < 600 {
			t.Fatalf("%s: no Join button (%d px)", mode, n)
		}
		win("invite-window-channel", &incoming{Channel: hcChannel, Call: "R1", Sender: huddle.FakeRemoteUser, status: "pending"})
		win("invite-window-soon", &incoming{Channel: dmChannel, Call: "R1", Sender: huddle.FakeRemoteUser, status: "joining_soon",
			note: "Join when you’re ready!", noteAt: time.Now()})

		// the sidebar card, on the sidebar's aubergine
		var st cardState
		for _, cs := range []struct {
			name string
			inv  *incoming
		}{
			{"invite-card-dm", &incoming{Channel: dmChannel, Call: "R1", Sender: huddle.FakeRemoteUser, status: "pending", card: true}},
			{"invite-card-channel", &incoming{Channel: hcChannel, Call: "R1", Sender: huddle.FakeRemoteUser, status: "pending", card: true}},
			{"invite-card-soon", &incoming{Channel: dmChannel, Call: "R1", Sender: huddle.FakeRemoteUser, status: "joining_soon", card: true,
				note: "Join when you’re ready!", noteAt: time.Now()}},
		} {
			inv := cs.inv
			img := render(t, cs.name+"-"+mode, 260, 240, func(gtx C) {
				theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
				layoutCard(gtx, th, c, c.view(inv), &st, gtx.Constraints.Max.X, false)
			})
			if n := pixeltest.Count(img, image.Rect(14*shotScale, 14*shotScale, 246*shotScale, 200*shotScale), k.bg, 6); n < 2000 {
				t.Fatalf("%s %s: no card (%d px)", cs.name, mode, n)
			}
			if n := pixeltest.Count(img, image.Rect(14*shotScale, 40*shotScale, 246*shotScale, 200*shotScale), k.join, 10); n < 400 {
				t.Fatalf("%s %s: no Join button (%d px)", cs.name, mode, n)
			}
		}
		c.mu.Lock()
		c.inv = nil
		c.mu.Unlock()
	}

	// stickers on the tiles: their raised hand, your Be right back, and the
	// small-tile and Audio-only forms
	e, s, _ := tileEnv(t)
	now := time.Now()
	self := huddle.Participant{UserID: huddle.FakeSelfUser, AttendeeID: "att-self", InMedia: true, Self: true, Sticker: "coffee"}
	remote := huddle.Participant{UserID: huddle.FakeRemoteUser, AttendeeID: "att-remote", InMedia: true, Sticker: "raised_hand", StickerAt: now}
	st := huddle.State{Phase: huddle.Connected, RoomID: huddle.FakeRoomID, Channel: hcChannel, Participants: []huddle.Participant{self, remote}}
	img := render(t, "invite-tiles-stickers", 620, 320, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.w.stage(gtx, s, st, image.Rectangle{Max: gtx.Constraints.Max}, now)
	})
	// the right tile's pill at its top left: white, wider than an emoji
	g := shotCtx(nil, 620, 320)
	area := image.Rectangle{Max: g.Constraints.Max}.Inset(g.Dp(12))
	side := min(g.Dp(220), (area.Dx()-g.Dp(8))/2, area.Dy())
	x0 := area.Min.X + (area.Dx()-(2*side+g.Dp(8)))/2 + side + g.Dp(8)
	y0 := area.Min.Y + (area.Dy()-side)/2
	pill := image.Rect(x0+10*shotScale, y0+10*shotScale, x0+90*shotScale, y0+38*shotScale)
	if n := pixeltest.Count(img, pill, hudWhite, 8); n < pill.Dx()*pill.Dy()/3 {
		t.Fatalf("no sticker pill with its label (%d px)", n)
	}
	audio := remote
	audio.Sticker = "speaker"
	st.Participants = []huddle.Participant{self, audio}
	render(t, "invite-tiles-audio-only", 620, 320, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.w.stage(gtx, s, st, image.Rectangle{Max: gtx.Constraints.Max}, now)
	})
	render(t, "invite-tiles-small", 240, 120, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.w.tile(gtx, s, remote, "", image.Rect(gtx.Dp(10), gtx.Dp(10), gtx.Dp(100), gtx.Dp(100)), now)
	})

	// the toolbar with your hand up (the sticker button lit), and the
	// sticker picker open
	if err := s.SetSticker("raised_hand"); err != nil {
		t.Fatal(err)
	}
	st = s.State()
	render(t, "invite-toolbar-hand", 620, 60, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.w.toolbar(gtx, st, image.Rectangle{Max: gtx.Constraints.Max})
	})
	e.w.stick = true
	render(t, "invite-sticker-picker", 620, 320, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.w.stage(gtx, s, st, image.Rectangle{Max: gtx.Constraints.Max}, now)
		e.w.stickerPicker(gtx, image.Rectangle{Max: gtx.Constraints.Max})
	})
	e.w.stick = false
}
