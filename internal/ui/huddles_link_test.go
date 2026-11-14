package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/ui/huddleui"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The sidebar's Huddles link on the whole client over the fake (Slack's
// HuddlesSidebarLink, YCI0), with every event coming over fakeslack's
// socket: a live huddle in a channel gives the link that huddle's faces
// (three at most, HuddleChannelTooltip with the channel's name on
// hover); an incoming huddle_invite makes the link bold and, under
// huddles_link_v2 only, puts the unread invites' count where the faces
// were; opening the Huddles page marks them seen 600 ms on (not with the
// experiment off); Decline and huddle_invite_cancel take an invite off.
// Nothing rings anyone: the invites are synthetic and no call is joined.
func TestHuddlesLinkInvites(t *testing.T) {
	e, fs := filterEnv(t, nil)
	a := e.a
	hud := a.huddles()
	hud.NoWindow = true        // the invite window is huddleui's to test
	hud.Ring = func(string) {} // and its sound
	a.notify = &recNotifier{}  // no desktop notification leaves the test
	a.Core().SetExperimentGroup(huddleui.ExpHuddlesLinkV2, "")
	var chans []string
	for _, s := range a.model().Sections {
		for _, id := range s.Rows {
			if c := a.model().conv(id); c != nil && c.Kind == "channel" && !c.Preview && len(chans) < 2 {
				chans = append(chans, id)
			}
		}
	}
	if len(chans) < 2 {
		t.Fatalf("the fake has %d member channels, want 2", len(chans))
	}
	ch, ch2 := chans[0], chans[1]
	users := fs.GroupDMUsers()
	// the huddle events come over the socket
	go a.Core().SetRealtime(true)
	e.until("the realtime socket", 15*time.Second, func() bool { return a.data.m.Connected && fs.Sockets() > 0 })
	now := time.Now().Unix()
	fs.PushHuddleRoom("sh_room_join", ch, "R0LINK0001", []string{users[0].ID, users[1].ID, users[2].ID, users[3].ID}, now-120, nil)
	fs.PushHuddleRoom("sh_room_join", ch2, "R0LINK0002", []string{users[4].ID}, now-60, nil)
	e.until("the live huddles", 10*time.Second, func() bool {
		_, on := hud.Live(ch)
		_, on2 := hud.Live(ch2)
		return on && on2
	})
	if got := hud.Link().Channel; got != ch {
		t.Fatalf("the link's facepile is %q's, want the earlier huddle's %s (%s started later)", got, ch, ch2)
	}
	e.settle(400 * time.Millisecond) // the faces' avatars

	link := e.sbRowPoint(t, "huddles")
	right := a.sidebar.origin.X + int(a.sidebar.width)
	// the faces: sweep the link's right end for HuddleChannelTooltip
	hit := image.Point{}
	for x := right - 4; x > right-100; x -= 2 {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(link.Y))})
		e.frame()
		if hud.LinkFacesHovered(e.context()) {
			hit = image.Pt(x-4, link.Y)
			break
		}
	}
	if hit == (image.Point{}) {
		t.Fatal("no faces on the Huddles link with a huddle on")
	}
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(hit.X), float32(hit.Y))})
	e.settle(300 * time.Millisecond) // past F2XN's 150 ms
	e.shotBoth("huddles-link-faces-tip")
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(right+400), float32(link.Y))})
	e.settle(200 * time.Millisecond)

	// rung in ch2: the link goes bold with ch2's faces; with the
	// experiment unassigned, no badge
	fs.PushHuddleInvite(ch2, "R0LINK0002", users[4].ID)
	e.until("the invite", 10*time.Second, func() bool { _, ok := hud.Incoming(); return ok && hud.UnreadInvites() == 1 })
	if l := hud.Link(); l != (huddleui.HuddleLink{Unread: true, Channel: ch2}) {
		t.Fatalf("invited, huddles_link_v2 off: %+v", l)
	}
	e.settle(300 * time.Millisecond)
	e.shotBoth("huddles-link-invite-off")
	if n := e.linkBadgeInk(t, link); n > 0 {
		t.Errorf("huddles_link_v2 off: %d badge pixels on the link", n)
	}
	// the Huddles page doesn't mark them with the experiment off
	a.Navigate("huddles")
	e.settle(time.Second)
	if n := hud.UnreadInvites(); n != 1 {
		t.Fatalf("huddles_link_v2 off: the Huddles page marked the invite seen (%d left)", n)
	}
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)

	// on: the count badge in place of the faces
	a.Core().SetExperimentGroup(huddleui.ExpHuddlesLinkV2, "on")
	e.settle(200 * time.Millisecond)
	if l := hud.Link(); l != (huddleui.HuddleLink{Unread: true, Channel: ch2, Badge: 1}) {
		t.Fatalf("invited, huddles_link_v2 on: %+v", l)
	}
	e.shotBoth("huddles-link-badge")
	if n := e.linkBadgeInk(t, link); e.gpu != nil && n < 60 {
		t.Errorf("huddles_link_v2 on: no badge on the link (%d px)", n)
	}
	// a second call ringing: the active invite moves to ch, the count to 2
	fs.PushHuddleInvite(ch, "R0LINK0001", users[0].ID)
	e.until("the second invite", 10*time.Second, func() bool { return hud.UnreadInvites() == 2 })
	if l := hud.Link(); l != (huddleui.HuddleLink{Unread: true, Channel: ch, Badge: 2}) {
		t.Fatalf("two invites: %+v", l)
	}
	e.shotBoth("huddles-link-badge-2")

	// the Huddles page: seen 600 ms after it notices them
	e.click(link)
	e.until("the Huddles page", 5*time.Second, func() bool { return a.router.Current() == "huddles" })
	opened := time.Now()
	e.until("the invites marked seen", 10*time.Second, func() bool { return hud.UnreadInvites() == 0 })
	if d := time.Since(opened); d < 550*time.Millisecond {
		t.Fatalf("marked %v after the page opened, before Slack's 600 ms", d)
	}
	if l := hud.Link(); l != (huddleui.HuddleLink{Unread: true, Channel: ch}) {
		t.Fatalf("seen: %+v", l)
	}
	e.settle(300 * time.Millisecond)
	e.shotBoth("huddles-link-seen")

	// Decline: the invite and its channel's unread go; the link shows the
	// earliest live huddle again, not bold
	hud.DeclineInvite()
	e.until("declined", 10*time.Second, func() bool { _, ok := hud.Incoming(); return !ok })
	if l := hud.Link(); l != (huddleui.HuddleLink{Channel: ch}) {
		t.Fatalf("declined: %+v", l)
	}
	found := false
	for _, f := range fs.InviteResponses() {
		if f.Get("response") == "decline" && f.Get("channel_id") == ch && f.Get("room_id") == "R0LINK0001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Decline's rooms.inviteResponse: %v", fs.InviteResponses())
	}

	// off the page an invite stays unread; huddle_invite_cancel drops it
	e.openChannel(ch)
	fs.PushHuddleInvite(ch2, "R0LINK0003", users[4].ID)
	e.until("a third invite", 10*time.Second, func() bool { return hud.UnreadInvites() == 1 })
	e.settle(time.Second)
	if n := hud.UnreadInvites(); n != 1 {
		t.Fatalf("off the Huddles page an invite was marked seen (%d left)", n)
	}
	fs.PushHuddleInviteCancel(ch2)
	e.until("cancelled", 10*time.Second, func() bool { _, ok := hud.Incoming(); return !ok && hud.UnreadInvites() == 0 })
	if l := hud.Link(); l != (huddleui.HuddleLink{Channel: ch}) {
		t.Fatalf("cancelled: %+v", l)
	}
}

// linkBadgeInk counts the sidebar badge colour's pixels in the Huddles
// link's facepile box (0 without a GPU).
func (e *perfEnv) linkBadgeInk(t *testing.T, link image.Point) int {
	t.Helper()
	if e.gpu == nil {
		return 0
	}
	e.frame()
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	right := e.a.sidebar.origin.X + int(e.a.sidebar.width)
	box := image.Rect(right-100, link.Y-12, right, link.Y+12)
	return pixeltest.Count(img, box, e.a.sbPal.Badge, 8)
}
