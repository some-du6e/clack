package huddleui

import (
	"image"
	"image/color"
	"strconv"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// tipEnv is a shot env with a second channel, #other, whose huddle you're
// not in.
func tipEnv(t *testing.T, dark bool, people ...string) *shotEnv {
	t.Helper()
	e := newShotEnv(t, dark)
	e.h.app.S.PutConv(slack.Conversation{ID: "C0OTHER", Name: "other", IsChannel: true, IsMember: true, NumMembers: 3})
	e.c.mu.Lock()
	e.c.live["C0OTHER"] = liveRoom{ID: "R2", Channel: "C0OTHER", Participants: people, Start: 1000}
	e.c.mu.Unlock()
	return e
}

func setLive(e *shotEnv, ch string, r liveRoom) {
	e.c.mu.Lock()
	r.Channel = ch
	e.c.live[ch] = r
	e.c.mu.Unlock()
}

// HuddleChannelTooltip's lines (client-boot Soit): the member text with
// and without the channel (includeChannelNameInTooltip), the topic, and
// the participant limit (getParticipantLimit: 50, 250 under
// increase_huddles_max_participants).
func TestHuddleChannelTooltipText(t *testing.T) {
	e := tipEnv(t, false, huddle.FakeRemoteUser, "U0THIRD")
	for _, c := range []struct {
		withChannel  bool
		action, memb string
	}{
		{false, "", "2 people in the huddle"},
		{true, "", "2 people in the huddle #other"},
	} {
		if a, m := e.c.tipText("C0OTHER", c.withChannel); a != c.action || m != c.memb {
			t.Errorf("withChannel %v: %q / %q, want %q / %q", c.withChannel, a, m, c.action, c.memb)
		}
	}
	setLive(e, "C0OTHER", liveRoom{ID: "R2", Participants: []string{huddle.FakeRemoteUser}, Name: "Standup :coffee:"})
	who := e.c.userName(huddle.FakeRemoteUser) // vUCu sE: the name the viewer's display pref picks
	if a, m := e.c.tipText("C0OTHER", false); a != "Standup :coffee:" || m != who+" is in the huddle" {
		t.Errorf("one other with a topic: %q / %q", a, m)
	}
	if _, m := e.c.tipText("C0OTHER", true); m != who+" is in the huddle #other" {
		t.Errorf("one other, with the channel: %q", m)
	}
	setLive(e, "C0OTHER", liveRoom{ID: "R2", Participants: []string{huddle.FakeSelfUser}, Name: "   "})
	if a, m := e.c.tipText("C0OTHER", false); a != "" || m != "Just you in the huddle" {
		t.Errorf("just you (blank topic): %q / %q", a, m)
	}
	if _, m := e.c.tipText("C0OTHER", true); m != "Just you in the huddle #other" {
		t.Errorf("just you, with the channel: %q", m)
	}
	// a DM never names the channel
	e.h.app.S.PutConv(slack.Conversation{ID: "D0DM", IsIM: true, User: huddle.FakeRemoteUser})
	setLive(e, "D0DM", liveRoom{ID: "R3", Participants: []string{huddle.FakeRemoteUser, huddle.FakeSelfUser}})
	if _, m := e.c.tipText("D0DM", true); m != "2 people in the huddle" {
		t.Errorf("DM with the channel asked for: %q", m)
	}
	// the limit
	full := make([]string, sbLimit)
	for i := range full {
		full[i] = "U0P" + strconv.Itoa(i)
	}
	setLive(e, "C0OTHER", liveRoom{ID: "R2", Participants: full, Name: "Town hall"})
	if a, m := e.c.tipText("C0OTHER", false); a != "Only 50 people can join a huddle" || m != "50 people in the huddle" {
		t.Errorf("50 people: %q / %q", a, m)
	}
	e.h.app.SetExperimentGroup(ExpMaxParticipants, "on")
	if a, _ := e.c.tipText("C0OTHER", false); a != "Town hall" {
		t.Errorf("50 people under %s: %q, want the topic", ExpMaxParticipants, a)
	}
}

// getActiveMembersByChannelId: in your own huddle the others come first
// and you last (the stack's two faces and the tooltip's five read it).
func TestSidebarActiveMembersSelfLast(t *testing.T) {
	e := tipEnv(t, false)
	s := e.join(nil, nil)
	waitState(t, s, "connected with the remote", func(st huddle.State) bool {
		return st.Phase == huddle.Connected && len(st.Participants) == 2
	})
	got := e.c.activeMembers(hcChannel)
	if len(got) != 2 || got[0] != huddle.FakeRemoteUser || got[1] != huddle.FakeSelfUser {
		t.Fatalf("your huddle's members %v, want the remote then you", got)
	}
}

// The faces and the badge each open HuddleChannelTooltip after F2XN's
// 150 ms, hovered through a real router; the pointer passes through to the
// row; moving off hides it.
func TestSidebarSuffixTooltipHover(t *testing.T) {
	e := tipEnv(t, false, huddle.FakeRemoteUser, "U0THIRD")
	th := theme.New(false)
	var rt input.Router
	var ops op.Ops
	left := 0
	frame := func(now time.Time) {
		ops.Reset()
		gtx := shotCtx(&ops, 220, 28)
		gtx.Now, gtx.Source = now, rt.Source()
		left = e.c.SidebarSuffix(gtx, th, "C0OTHER", gtx.Constraints.Max.X-gtx.Dp(6), gtx.Constraints.Max.Y, false)
		rt.Frame(&ops)
	}
	move := func(x int) {
		rt.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), 14*shotScale)})
	}
	t0 := time.Now()
	frame(t0)
	hs := e.c.hdrState("C0OTHER")
	facesX := left + 8*shotScale + 10*shotScale // margin-left 8, inside the first face
	move(facesX)
	frame(t0)
	frame(t0.Add(100 * time.Millisecond))
	if hs.sbFaces.tip.Showing(t0.Add(100 * time.Millisecond)) {
		t.Error("the faces' tooltip showed before 150 ms")
	}
	frame(t0.Add(160 * time.Millisecond))
	if !hs.sbFaces.tip.Showing(t0.Add(160 * time.Millisecond)) {
		t.Error("no tooltip on the faces 160 ms after hovering them")
	}
	if hs.sbBadge.tip.Showing(t0.Add(160 * time.Millisecond)) {
		t.Error("the badge's tooltip showed while the faces were hovered")
	}
	// the badge: 4px in from the right edge, 36px wide
	badgeX := (220 - 6 - 4 - 18) * shotScale
	move(badgeX)
	t1 := t0.Add(time.Second)
	frame(t1)
	frame(t1.Add(10 * time.Millisecond))
	frame(t1.Add(200 * time.Millisecond))
	if !hs.sbBadge.tip.Showing(t1.Add(200 * time.Millisecond)) {
		t.Error("no tooltip on the headphones badge")
	}
	if hs.sbFaces.tip.Showing(t1.Add(200 * time.Millisecond)) {
		t.Error("the faces' tooltip stayed after the pointer left them")
	}
}

// --huddles-mask clips every face after the first (client-boot qgIY's
// clipPath on .p-huddle_avatar_stack__avatar:not(--first) [class^=
// c-base_icon]): its concave left edge leaves a crescent of the row
// between the two faces, about 0.128 of a face in at mid-height.
func TestSidebarFaceCrescent(t *testing.T) {
	e := tipEnv(t, false, huddle.FakeRemoteUser, "U0THIRD")
	th := theme.New(false)
	bg := color.NRGBA{R: 255, A: 255}
	var ops op.Ops
	gtx := shotCtx(&ops, 220, 28)
	theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, bg)
	left := e.c.SidebarSuffix(gtx, th, "C0OTHER", gtx.Constraints.Max.X-gtx.Dp(6), gtx.Constraints.Max.Y, false)
	img := pixeltest.Render(t, 220*shotScale, 28*shotScale, &ops)
	fs := gtx.Dp(hdrFace)
	fx := left + gtx.Dp(8)
	second := fx + sbFaceSteps(2, fs)[1]
	mid := 14 * shotScale
	at := func(x int) color.NRGBA {
		c := img.RGBAAt(x, mid)
		return color.NRGBA{R: c.R, G: c.G, B: c.B, A: c.A}
	}
	near := func(a, b color.NRGBA) bool {
		d := func(x, y uint8) int {
			if x > y {
				return int(x - y)
			}
			return int(y - x)
		}
		return d(a.R, b.R) < 12 && d(a.G, b.G) < 12 && d(a.B, b.B) < 12
	}
	if c := at(fx + fs/2); near(c, bg) {
		t.Fatalf("no first face at x=%d (%v)", fx+fs/2, c)
	}
	// first face ends at fx+fs; the second's mask edge is 0.128·fs in
	gap := fx + fs + (second+fs*128/1000-(fx+fs))/2
	if c := at(gap); !near(c, bg) {
		t.Errorf("x=%d between the faces is %v, want the row (the crescent)", gap, c)
	}
	if c := at(second + fs/2); near(c, bg) {
		t.Errorf("no second face at x=%d", second+fs/2)
	}
	render(t, "sidebar-face-crescent", 220, 28, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, th.C.SidebarBg)
		e.c.SidebarSuffix(gtx, th, "C0OTHER", gtx.Constraints.Max.X-gtx.Dp(6), gtx.Constraints.Max.Y, false)
	})
}

// Light and dark pictures of the row's tooltip and a collapsed section's
// indicator with its tooltip (the channel named), five faces with "+3" on
// the last for eight people, and a topic.
func TestSidebarTooltipShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		th := theme.New(dark)
		e := tipEnv(t, dark, huddle.FakeRemoteUser, "U0THIRD")
		shot := func(name, ch string, withChannel bool, draw func(gtx C, right, rowH int) int) {
			var st popups.TipState
			st.Delay = time.Nanosecond // drawn on render's second frame
			img := render(t, name+"-"+suffix, 300, 190, func(gtx C) {
				theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, th.C.ChromeA)
				theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, th.C.SidebarBg)
				rowH := gtx.Dp(28)
				defer op.Offset(image.Pt(0, gtx.Constraints.Max.Y-rowH-gtx.Dp(8))).Push(gtx.Ops).Pop()
				right := gtx.Constraints.Max.X/2 + gtx.Dp(60) // room for the tip on both sides
				l := draw(gtx, right, rowH)
				fs := gtx.Dp(hdrFace)
				anchor := image.Rect(l, (rowH-fs)/2, l+fs+fs*19/20, (rowH+fs)/2)
				defer op.Offset(anchor.Min).Push(gtx.Ops).Pop()
				popups.TooltipBox(gtx, th, &st, true, anchor.Size(), popups.Above, func(gtx C) D {
					return e.c.channelTip(gtx, th, ch, withChannel)
				})
			})
			if n := pixeltest.Count(img, img.Bounds(), th.C.Tooltip, 6); n < 2000 {
				t.Errorf("%s-%s: no tooltip drawn (%d px)", name, suffix, n)
			}
		}
		shot("sidebar-suffix-tip", "C0OTHER", false, func(gtx C, right, rowH int) int {
			return e.c.SidebarSuffix(gtx, th, "C0OTHER", right, rowH, false) + gtx.Dp(8)
		})
		eight := []string{huddle.FakeRemoteUser, "U0THIRD", "U0P2", "U0P3", "U0P4", "U0P5", "U0P6", "U0P7"}
		setLive(e, "C0OTHER", liveRoom{ID: "R2", Participants: eight, Name: "Design review"})
		e.h.app.SetExperimentGroup(ExpSectionIndicator, "on")
		shot("sidebar-section-indicator-tip", "C0OTHER", true, func(gtx C, right, rowH int) int {
			ch := e.c.SectionHuddle([]string{hcChannel, "C0OTHER"})
			if ch != "C0OTHER" {
				t.Fatalf("SectionHuddle picked %q", ch)
			}
			return e.c.SectionIndicator(gtx, th, "S1", ch, right, rowH)
		})
	}
}

// getChannelIdWithActiveHuddle: off without huddle_section_indicator; the
// earliest-started eligible huddle; a muted or archived channel, or one
// you're previewing, doesn't count; the huddle you're in wins; the
// inviting huddle wins over that.
func TestSectionHuddlePick(t *testing.T) {
	e := tipEnv(t, false, huddle.FakeRemoteUser)
	st := e.h.app.S
	for _, c := range []slack.Conversation{
		{ID: "C0EARLY", Name: "early", IsChannel: true, IsMember: true},
		{ID: "C0ARCH", Name: "arch", IsChannel: true, IsMember: true, IsArchived: true},
		{ID: "C0PREV", Name: "prev", IsChannel: true},
	} {
		st.PutConv(c)
	}
	setLive(e, "C0OTHER", liveRoom{ID: "R2", Participants: []string{huddle.FakeRemoteUser}, Start: 2000})
	setLive(e, "C0EARLY", liveRoom{ID: "R4", Participants: []string{"U0THIRD"}, Start: 1500})
	setLive(e, "C0ARCH", liveRoom{ID: "R5", Participants: []string{"U0THIRD"}, Start: 100})
	setLive(e, "C0PREV", liveRoom{ID: "R6", Participants: []string{"U0THIRD"}, Start: 200})
	ids := []string{"C0PREV", "C0ARCH", "C0OTHER", "C0EARLY", "C0NOHUDDLE"}
	if got := e.c.SectionHuddle(ids); got != "" {
		t.Errorf("experiment off: %q", got)
	}
	e.h.app.SetExperimentGroup(ExpSectionIndicator, "on")
	if got := e.c.SectionHuddle(ids); got != "C0EARLY" {
		t.Errorf("picked %q, want the earliest eligible (C0EARLY)", got)
	}
	if got := e.c.SectionHuddle([]string{"C0PREV", "C0ARCH"}); got != "" {
		t.Errorf("only a preview and an archived channel: %q", got)
	}
	// the inviting huddle's channel comes first
	e.c.mu.Lock()
	e.c.inv = &incoming{Channel: "C0OTHER"}
	e.c.mu.Unlock()
	if got := e.c.SectionHuddle(ids); got != "C0OTHER" {
		t.Errorf("with an invite to #other: %q", got)
	}
	e.c.mu.Lock()
	e.c.inv = nil
	e.c.mu.Unlock()
	// the huddle you're in comes before an earlier one
	st.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true})
	s := e.join(nil, nil)
	waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected })
	if got := e.c.SectionHuddle(append(ids, hcChannel)); got != hcChannel {
		t.Errorf("in your huddle: %q", got)
	}
	var ops op.Ops
	if w := e.c.SectionIndicatorWidth(shotCtx(&ops, 220, 28), hcChannel); w != sbFaceSteps(2, 2*hdrFace)[1]+2*hdrFace {
		t.Errorf("indicator width %d", w)
	}
}
