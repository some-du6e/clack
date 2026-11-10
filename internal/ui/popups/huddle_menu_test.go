package popups

import (
	"image"
	"net/url"
	"strconv"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// hudHost is tzHost with the ui root's huddle call menu.
type hudHost struct {
	*tzHost
	started []string
}

func (h *hudHost) HuddleMemberMenu(user string) []MenuItem {
	return []MenuItem{{Label: "Start Huddle", Icon: "headphones", Do: func() { h.started = append(h.started, user) }}}
}

// The profile card's Huddle button opens the huddle call menu (Slack's
// WithCallMenu → CallMenu: 305 wide, centered 10px under the button) and
// an item closes the card after it runs.
func TestCardHuddleMenu(t *testing.T) {
	h := &hudHost{tzHost: hcEnv(t)}
	p := NewHoverProfileCard(h, image.Rect(120, 600, 200, 622), fakeslack.ProfileID)
	card := hcLayout(t, p)
	btn := p.huddleLocal.Add(p.rect.Min)
	if p.huddleLocal.Empty() || !btn.In(card) {
		t.Fatalf("Huddle button %v not in the card %v", btn, card)
	}
	m := pfHuddleMenuAt(hcGtx(), h, fakeslack.ProfileID, btn, p.Dismiss)
	if m == nil {
		t.Fatal("no menu")
	}
	if m.opts.Width != 305 || m.opts.Side != Below {
		t.Errorf("menu opts %+v", m.opts)
	}
	if a := m.opts.Anchor; a.Max.Y != btn.Max.Y+10 || (a.Min.X+a.Max.X)/2 != (btn.Min.X+btn.Max.X)/2 {
		t.Errorf("anchor %v for button %v: want centered, 10px below", a, btn)
	}
	m.items[0].Do()
	if len(h.started) != 1 || h.started[0] != fakeslack.ProfileID || !p.Dismissed() {
		t.Errorf("started %v, card dismissed %v", h.started, p.Dismissed())
	}
	// no huddle side in the host: no menu
	if pfHuddleMenuAt(hcGtx(), h.tzHost, fakeslack.ProfileID, btn, nil) != nil {
		t.Error("a menu without huddles")
	}
}

// hcGtx is a 1px-per-dp frame context.
func hcGtx() layout.Context {
	return layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1000, 800))}
}

// hsPump runs posted callbacks until cond or the deadline.
func hsPump(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		Pump()
		time.Sleep(5 * time.Millisecond)
	}
}

func hsEnv(t *testing.T) (*tzHost, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	h := hcEnv(t)
	h.a = core.New(slack.New(fs.Creds()), h.a.S)
	h.a.S.PutConv(slack.Conversation{ID: "C0SCHED001", Name: "general", IsChannel: true, IsMember: true})
	h.a.S.PutConv(slack.Conversation{ID: "D0SCHED001", IsIM: true, User: fakeslack.ProfileID})
	return h, fs
}

// Schedule Huddle against fakeslack: the calendar check, Slack's defaults
// (title "#general huddle", the next half hour, 30 minutes), and
// huddles.scheduled.new's arguments.
func TestHuddleScheduleModal(t *testing.T) {
	h, fs := hsEnv(t)
	s := NewHuddleScheduleModal(h, "C0SCHED001")
	hsPump(t, "the calendar check", func() bool { return s.calLoaded })
	if !s.CalendarReady() {
		t.Fatal("no writable calendar on the fake")
	}
	if got := s.titleEd.Text(); got != "#general huddle" {
		t.Errorf("title %q", got)
	}
	if w := s.when(); w.Minute()%30 != 0 || !w.After(time.Now()) || s.dur != 30 {
		t.Errorf("default %v for %d min", w, s.dur)
	}
	s.SetInviteAll(true)
	s.Submit()
	hsPump(t, "the schedule call", func() bool { return s.Dismissed() })
	got := fs.ScheduledHuddles()
	if len(got) != 1 {
		t.Fatalf("%d huddles.scheduled.new calls", len(got))
	}
	f := got[0].Form
	if f.Get("channel_id") != "C0SCHED001" || f.Get("invite_all_channel_members") != "true" || f.Get("duration") != "1800" ||
		f.Get("topic") != "#general huddle" || f.Get("reason") != "schedule-new" || f.Get("invitees") != "" {
		t.Errorf("args %v", f)
	}
	if ts, _ := strconv.ParseInt(f.Get("scheduled_time"), 10, 64); ts != s.when().Unix() {
		t.Errorf("scheduled_time %s, want %d", f.Get("scheduled_time"), s.when().Unix())
	}
	if ts := h.toastList(); len(ts) == 0 || ts[len(ts)-1] != "#general huddle scheduled" {
		t.Errorf("toasts %q", ts)
	}

	// people picked instead of everyone
	s = NewHuddleScheduleModal(h, "C0SCHED001")
	hsPump(t, "the calendar check", func() bool { return s.calLoaded })
	s.Pick(core.User{ID: "U0PICK0001"})
	s.Pick(core.User{ID: "U0PICK0002"})
	s.Submit()
	hsPump(t, "the second call", func() bool { return s.Dismissed() })
	if f := fs.ScheduledHuddles()[1].Form; f.Get("invitees") != "U0PICK0001,U0PICK0002" || f.Has("invite_all_channel_members") {
		t.Errorf("picked: %v", f)
	}

	// a DM invites everyone and has no default title
	s = NewHuddleScheduleModal(h, "D0SCHED001")
	hsPump(t, "the calendar check", func() bool { return s.calLoaded })
	if !s.dm || s.titleEd.Text() != "" {
		t.Errorf("DM: dm %v title %q", s.dm, s.titleEd.Text())
	}
	s.Submit()
	hsPump(t, "the DM call", func() bool { return s.Dismissed() })
	if f := fs.ScheduledHuddles()[2].Form; f.Get("invite_all_channel_members") != "true" || f.Has("topic") {
		t.Errorf("DM: %v", f)
	}
	if ts := h.toastList(); ts[len(ts)-1] != "Huddle scheduled" {
		t.Errorf("untitled toast %q", ts[len(ts)-1])
	}

	// an API error shows Slack's message and keeps the modal open
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "huddles.scheduled.new" {
			return map[string]any{"ok": false, "error": "too_many_invitees"}, true
		}
		return nil, false
	})
	s = NewHuddleScheduleModal(h, "C0SCHED001")
	hsPump(t, "the calendar check", func() bool { return s.calLoaded })
	s.Submit()
	hsPump(t, "the error", func() bool { return s.Error() != "" })
	if s.Error() != "Too many people to invite to one huddle. Remove some and try again." || s.Dismissed() {
		t.Errorf("error %q, dismissed %v", s.Error(), s.Dismissed())
	}
	fs.SetHook(nil)

	// without a calendar: Slack's notice, and Schedule does nothing
	fs.SetCalendarConnected(false)
	n := len(fs.ScheduledHuddles())
	s = NewHuddleScheduleModal(h, "C0SCHED001")
	hsPump(t, "the calendar check", func() bool { return s.calLoaded })
	if s.CalendarReady() || s.btn.enabled() {
		t.Error("Schedule enabled without a calendar")
	}
	s.Submit()
	time.Sleep(50 * time.Millisecond)
	Pump()
	if len(fs.ScheduledHuddles()) != n || s.Dismissed() {
		t.Error("scheduled without a calendar")
	}
}
