package huddleui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Every way into a 1:1 huddle, against the fake Chime: the DM header's
// button, Cmd/Ctrl+Shift+H (and again to leave), /huddle and the Start a
// Huddle picker (all Begin). Each start is one rooms.join in the DM with
// Slack's joinRoom shape, and nothing that rings: Slack's server rings
// the other member on rooms.join (the profile card's path, with
// conversations.open first, is TestStartHuddleFromCard).
func TestStartPaths(t *testing.T) {
	e := newMenuEnv(t, false, false)
	e.c.Tweak = func(o *huddle.Options) { o.Device = huddle.NewToneDevice(440) }
	start := func(name string, do func()) {
		t.Helper()
		fk := huddle.NewFake(testClip(t), t.Errorf)
		defer fk.Close()
		e.c.Client = fk.Caller()
		do()
		waitFor(t, name+": the huddle", func() bool { return e.c.Session() != nil })
		s := e.c.Session()
		if s.Channel() != dmChannel {
			t.Fatalf("%s: huddle in %q", name, s.Channel())
		}
		calls := fk.Calls()
		if len(calls) != 1 || calls[0].Method != "rooms.join" || calls[0].Params.Get("channel_id") != dmChannel ||
			calls[0].Params.Get("multidevice") != "false" || calls[0].Params.Get("regions") == "" {
			t.Fatalf("%s: Slack calls %+v", name, calls)
		}
		if name == "shortcut" {
			e.c.ShortcutToggle("") // Cmd/Ctrl+Shift+H again: leave
		} else {
			e.c.Leave()
		}
		waitFor(t, name+": left", func() bool { return !e.c.Active() })
	}
	start("header button", func() { e.c.Begin(dmChannel, EntryHeaderButton) })
	start("live header pill", func() { e.c.Toggle(dmChannel) })
	start("shortcut", func() { e.c.ShortcutToggle(dmChannel) })
	start("/huddle", func() { e.c.Begin(dmChannel, "") })
	start("Start a Huddle picker", func() { e.c.Begin(dmChannel, EntryHuddlesPage) })

	// the shortcut without a conversation on screen does nothing
	e.c.ShortcutToggle("")
	time.Sleep(50 * time.Millisecond)
	if e.c.Active() {
		t.Fatal("shortcut with no conversation started a huddle")
	}
}

// tileEnv is a joined fake huddle (for its video) and a window to draw.
func tileEnv(t *testing.T) (*shotEnv, *huddle.Session, string) {
	t.Helper()
	e := newShotEnv(t, false)
	e.h.app.S.PutUsers([]slack.User{{ID: "U0CAMERA01", Name: "camera", RealName: "Camera Person"},
		{ID: "U0CALLEE01", Name: "callee", RealName: "Callee Person"}})
	s := e.join(nil, nil)
	st := waitState(t, s, "video", func(st huddle.State) bool {
		return st.Phase == huddle.Connected && len(st.Videos) > 0 && s.Frame(st.Videos[0].Key).Img != nil
	})
	return e, s, st.Videos[0].Key
}

// The participant tiles in each state, as Slack's HuddlePeerTile draws
// them (pictures under CLACK_SHOTS: tiles-*.png).
func TestHuddleShotsTiles(t *testing.T) {
	e, s, vid := tileEnv(t)
	now := time.Now()
	self := huddle.Participant{UserID: huddle.FakeSelfUser, AttendeeID: "att-self", InMedia: true, Self: true}
	remote := huddle.Participant{UserID: huddle.FakeRemoteUser, AttendeeID: "att-remote", InMedia: true}
	base := huddle.State{Phase: huddle.Connected, RoomID: huddle.FakeRoomID, Channel: hcChannel}
	stage := func(name string, st huddle.State) *image.RGBA {
		return render(t, name, 620, 320, func(gtx C) {
			theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
			e.w.stage(gtx, s, st, image.Rectangle{Max: gtx.Constraints.Max}, now)
		})
	}
	// two tiles side by side: the left one's box, for the checks
	twoTiles := func(gtx C) (image.Rectangle, image.Rectangle) {
		g := compactTiles(gtx, image.Rectangle{Max: gtx.Constraints.Max}, 2)
		return g.tiles[0], g.tiles[1]
	}
	var ops = shotCtx(nil, 620, 320)
	left, right := twoTiles(ops)

	// speaking: you sort first, so the remote person is the right tile
	speaking := remote
	speaking.SpokeAt = now
	st := base
	st.Participants = []huddle.Participant{self, speaking}
	img := stage("tiles-speaking", st)
	// Slack's active speaker: a white outline just outside the tile, not green
	ring := image.Rect(right.Min.X-3*shotScale, right.Min.Y+40*shotScale, right.Min.X, right.Max.Y-40*shotScale)
	if n := pixeltest.Count(img, ring, hudWhite, 10); n < ring.Dx()*ring.Dy()/2 {
		t.Fatalf("no white outline on the speaker (%d px of %d)", n, ring.Dx()*ring.Dy())
	}
	old := color.NRGBA{R: 0x2b, G: 0xac, B: 0x76, A: 0xff} // Clack's old green border
	if n := pixeltest.Count(img, right.Inset(-4*shotScale), old, 8); n > 20 {
		t.Fatalf("a green speaking border (%d px)", n)
	}
	// the mic pill: white, with the green bars
	pill := image.Rect(right.Min.X+8*shotScale, right.Max.Y-36*shotScale, right.Min.X+44*shotScale, right.Max.Y-8*shotScale)
	if n := pixeltest.Count(img, pill, speakerGreen, 40); n < 20 {
		t.Fatalf("no speaking bars (%d px)", n)
	}
	if n := pixeltest.Count(img, pill, hudWhite, 8); n < pill.Dx()*pill.Dy()/3 {
		t.Fatalf("speaking pill isn't white (%d px)", n)
	}
	darkSq := color.NRGBA{R: 0x1a, G: 0x3d, B: 0x2e, A: 0xff} // Clack's old dark chip
	if n := pixeltest.Count(img, pill.Inset(-4*shotScale), darkSq, 10); n > 20 {
		t.Fatalf("the old dark speaking chip (%d px)", n)
	}
	// nobody speaking on your tile: no outline there
	if n := pixeltest.Count(img, image.Rect(left.Min.X-3*shotScale, left.Min.Y+40*shotScale, left.Min.X, left.Max.Y-40*shotScale), hudWhite, 10); n > 10 {
		t.Fatalf("an outline on a quiet tile (%d px)", n)
	}

	// muted: the dark translucent pill with the struck-through mic
	muted := remote
	muted.Muted = true
	st.Participants = []huddle.Participant{self, muted}
	img = stage("tiles-muted", st)
	if n := pixeltest.Count(img, pill, hudWhite, 30); n < 15 {
		t.Fatalf("no mic-off glyph (%d px)", n)
	}
	if n := pixeltest.Count(img, pill, speakerGreen, 40); n > 5 {
		t.Fatalf("speaking bars while muted (%d px)", n)
	}

	// camera: the video fills the tile
	cam := huddle.Participant{UserID: "U0CAMERA01", AttendeeID: "att-cam", InMedia: true, Camera: true}
	st.Participants = []huddle.Participant{self, cam}
	st.Videos = []huddle.Video{{Key: vid, AttendeeID: "att-cam", UserID: "U0CAMERA01"}}
	img = stage("tiles-camera", st)
	avatarGreen := color.NRGBA{R: 0x00, G: 0x7a, B: 0x5a, A: 0xff}
	if n := pixeltest.Count(img, right.Inset(20*shotScale), avatarGreen, 6); n > right.Dx()*right.Dy()/4 {
		t.Fatalf("camera tile shows the avatar (%d px)", n)
	}

	// a screen share takes the stage; no "<name>'s screen" chip
	st.Participants = []huddle.Participant{self, remote}
	st.Videos = []huddle.Video{{Key: vid, AttendeeID: "att-remote", UserID: huddle.FakeRemoteUser, Content: true}}
	stage("tiles-share", st)

	// a reaction: the white pill at the top left
	react := remote
	react.Reaction, react.ReactedAt = "tada", now.Add(-time.Second)
	st.Videos = nil
	st.Participants = []huddle.Participant{self, react}
	img = stage("tiles-reaction", st)
	rp := image.Rect(right.Min.X+10*shotScale, right.Min.Y+10*shotScale, right.Min.X+46*shotScale, right.Min.Y+38*shotScale)
	if n := pixeltest.Count(img, rp, hudWhite, 8); n < rp.Dx()*rp.Dy()/3 {
		t.Fatalf("no reaction pill (%d px)", n)
	}

	// rung (a DM huddle the other person hasn't picked up): faded avatar
	// and the white invite pill
	st.Participants = []huddle.Participant{self}
	for _, inv := range []huddle.Invite{{UserID: "U0CALLEE01", Status: "pending"}, {UserID: "U0CALLEE01", Status: "declined"}} {
		st.Invites = []huddle.Invite{inv}
		img = stage("tiles-invite-"+inv.Status, st)
		ip := image.Rect(right.Min.X+9*shotScale, right.Min.Y+9*shotScale, right.Min.X+41*shotScale, right.Min.Y+33*shotScale)
		if n := pixeltest.Count(img, ip, hudWhite, 8); n < ip.Dx()*ip.Dy()/3 {
			t.Fatalf("%s: no invite pill (%d px)", inv.Status, n)
		}
		if n := pixeltest.Count(img, right.Inset(40*shotScale), avatarGreen, 4); n > 50 {
			t.Fatalf("%s: the invitee's avatar isn't faded (%d px)", inv.Status, n)
		}
	}

	// hover: the name pill (drawn directly; the shots can't hover)
	img = render(t, "tiles-name-hover", 240, 240, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		r := image.Rect(gtx.Dp(10), gtx.Dp(10), gtx.Dp(230), gtx.Dp(230))
		e.w.tile(gtx, s, speaking, "", r, now)
		at(gtx, r.Min, r.Size(), func(gtx C) D {
			e.w.namePill(gtx, r.Size(), "remote", false, true, e.w.ink(), now)
			return D{}
		})
	})
	np := image.Rect(18*shotScale, 194*shotScale, 100*shotScale, 222*shotScale)
	if n := pixeltest.Count(img, np, speakerInk, 40); n < 20 {
		t.Fatalf("no name on the speaking name pill (%d px)", n)
	}
}
