package views

// Slack's ProfileHoverTrigger in the views (internal/ui/hovercard lists
// where Slack has it): the channel details Members tab's names and the
// Activity feed's reaction rows. Each test sweeps the pointer over the
// real widget through Gio's router and checks which member reported.

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/hovercard"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type hvSweep struct {
	r   input.Router
	ops op.Ops
	ctl hovercard.Controller
	ptr image.Point
}

func newHvSweep(t *testing.T) *hvSweep {
	s := &hvSweep{}
	s.ctl.Pointer = func() image.Point { return s.ptr }
	hovercard.SetDefault(&s.ctl)
	t.Cleanup(func() { hovercard.SetDefault(nil) })
	return s
}

func (s *hvSweep) frame(size image.Point, draw func(gtx layout.Context)) {
	s.ops.Reset()
	gtx := layout.Context{Ops: &s.ops, Source: s.r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(size), Now: time.Now()}
	draw(gtx)
	s.r.Frame(&s.ops)
	s.ctl.Frame(gtx.Now, false, false)
}

// users moves the pointer over size in steps and returns every member a
// trigger reported, with a point inside each one's trigger.
func (s *hvSweep) users(size image.Point, draw func(gtx layout.Context)) map[string]image.Rectangle {
	out := map[string]image.Rectangle{}
	s.frame(size, draw)
	for y := 2; y < size.Y; y += 5 {
		for x := 2; x < size.X; x += 9 {
			s.ptr = image.Pt(x, y)
			s.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(y))})
			s.frame(size, draw)
			if req, ok := s.ctl.Pending(); ok {
				if _, seen := out[req.UserID]; !seen {
					out[req.UserID] = req.Anchor
				}
			}
		}
	}
	return out
}

// The Activity feed's reaction row: the reactor's name opens their card
// (Slack's ActivityItemMember in "X reacted to your message"); a mention
// row's name doesn't.
func TestHoverTriggerActivityReaction(t *testing.T) {
	app := actFeedApp(t)
	h := &slickHost{app: app, th: theme.New(false)}
	v := NewActivity(h)
	var ops op.Ops
	deadline := time.Now().Add(10 * time.Second)
	for len(actPage(v).Items) < 31 {
		if time.Now().After(deadline) {
			t.Fatal("feed never loaded")
		}
		v.Layout(actFrame(&ops, image.Pt(1200, 800)))
		time.Sleep(10 * time.Millisecond)
	}
	page := actPage(v)
	var react, mention int = -1, -1
	for i := range page.Items {
		switch page.Items[i].Type {
		case "message_reaction":
			if react < 0 && page.Items[i].Reaction != nil {
				react = i
			}
		case "at_user":
			if mention < 0 {
				mention = i
			}
		}
	}
	if react < 0 || mention < 0 {
		t.Fatalf("feed lacks a reaction (%d) or a mention (%d)", react, mention)
	}
	s := newHvSweep(t)
	size := image.Pt(800, 140)
	row := func(i int) func(gtx layout.Context) {
		return func(gtx layout.Context) {
			gtx.Constraints = layout.Constraints{Min: image.Pt(size.X, 0), Max: size}
			v.row(gtx, &page.Items[i], 1)
		}
	}
	for _, i := range []int{react, mention} { // the rows' text, as Layout builds it for visible rows
		r := v.rowFor(actKey(&page.Items[i]))
		r.d, r.built = v.describe(&page.Items[i], page.Convs), true
	}
	got := s.users(size, row(react))
	want := page.Items[react].Reaction.User
	if _, ok := got[want]; !ok || len(got) != 1 {
		t.Errorf("reaction row reported %v, want only the reactor %s", got, want)
	}
	// a mention row's sender name isn't a trigger (only its body's
	// @mention is, through richview)
	sender := page.Items[mention].Msg.User
	if got := newHvSweep(t).users(size, row(mention)); got[sender] != (image.Rectangle{}) {
		t.Errorf("a mention row's sender name opened a card: %v", got)
	}
}

// The channel details Members tab: each member's name is a trigger (Slack's
// MediumMemberEntity showHoverCard wraps the primary name only); the
// avatar isn't.
func TestHoverTriggerChannelMembers(t *testing.T) {
	app := actFeedApp(t)
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	h := &slickHost{app: app, th: th}
	d := NewChannelDetails(h, "C1")
	u, ok := app.User("U2")
	if !ok {
		t.Fatal("no fixture user")
	}
	s := newHvSweep(t)
	size := image.Pt(700, 60)
	draw := func(gtx layout.Context) {
		gtx.Constraints = layout.Constraints{Min: image.Pt(size.X, 0), Max: size}
		d.frame()
		d.memberRow(gtx, u, "USELF")
	}
	got := s.users(size, draw)
	r, ok := got["U2"]
	if !ok || len(got) != 1 {
		t.Fatalf("member row reported %v, want U2", got)
	}
	// the name starts after the 28px padding, the 36px avatar and 12px gap
	if r.Min.X < 28+36+12-1 {
		t.Errorf("trigger %v covers the avatar", r)
	}
}
