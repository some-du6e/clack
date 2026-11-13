package popups

// Slack's ChannelHoverCard (channel_card.go) over core on the in-repo fake
// Slack (synthetic data only), and the composer's app tooltip
// (member_tip.go).

import (
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// chcEnv is a popups host over core on the fake with the thread-header
// channels (public, private, Slack Connect, archived, one the viewer
// isn't in), not booted: the card resolves what it shows itself.
func chcEnv(t *testing.T, dark bool) (*tzHost, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.EnsureUsers(ctx, []string{fakeslack.SelfID, fakeslack.ProfileID, fakeslack.AppUserID, fakeslack.SlackbotID})
	if u, ok := a.S.User(fakeslack.SelfID); ok {
		a.S.Self = *u
	}
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	th := theme.New(dark)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	return &tzHost{a: a, th: th, im: imgcache.New(noNet, "", nil)}, fs
}

// chcFrames lays the card out in a 1000x800 window until done holds (or
// 5s pass) and returns the ops of the last frame.
func chcFrames(t *testing.T, p *ChannelHoverCard, done func() bool) *op.Ops {
	t.Helper()
	ops := new(op.Ops)
	deadline := time.Now().Add(5 * time.Second)
	for {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1000, 800))}
		p.Layout(gtx)
		if done() {
			return ops
		}
		if time.Now().After(deadline) {
			t.Fatalf("card for %s: state %v, items %q, button %q", p.id, p.State(), p.Items(), p.Button())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func chcShot(t *testing.T, ops *op.Ops, name string) {
	t.Helper()
	dir := os.Getenv("HOVER_SHOTS_DIR")
	if dir == "" {
		return
	}
	img := pixeltest.Render(t, 1000, 800, ops)
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// Each of Slack's states from the fixtures: the Slack Connect channel
// (count, last message, the partner's row, View Channel), one the viewer
// isn't in (Join Channel), an archived one (no items), a private one the
// viewer is in, and one conversations.info doesn't know (the no-access
// text); the composer's card has no buttons.
func TestChannelHoverCardStates(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		h, fs := chcEnv(t, dark)
		anchor := image.Rect(400, 500, 480, 522)
		cases := []struct {
			name, id string
			state    ChannelCardState
			button   string
			items    int
			team     string
		}{
			{"shared", fakeslack.ThreadSharedID, ChannelCardFull, "View Channel", 3, "Partner Co is in this channel"},
			{"not-member", fakeslack.ThreadElsewhereID, ChannelCardFull, "Join Channel", 2, ""},
			{"archived", fakeslack.ThreadArchivedID, ChannelCardFull, "View Channel", 0, ""},
			{"private", fakeslack.ThreadPrivateID, ChannelCardFull, "View Channel", 2, ""},
			{"no-access", "C0NOPE00001", ChannelCardNoAccess, "", 0, ""},
			{"topic", fs.ChannelIDs()[2], ChannelCardFull, "View Channel", 2, ""}, // a generated channel: its topic under the name
		}
		for _, c := range cases {
			p := NewChannelHoverCard(h, anchor, c.id, true, false)
			ops := chcFrames(t, p, func() bool {
				if p.State() != c.state {
					return false
				}
				it := p.Items()
				if len(it) != c.items {
					return false
				}
				for _, s := range it {
					if s == "" || strings.Contains(s, "is in this channel") && strings.HasPrefix(s, " ") {
						return false // still loading (the team's name too)
					}
				}
				return true
			})
			if got := p.Button(); got != c.button {
				t.Errorf("%s: button %q, want %q", c.name, got, c.button)
			}
			it := p.Items()
			if c.items >= 2 {
				if it[0] != "1,100 people in this channel" {
					t.Errorf("%s: count %q", c.name, it[0])
				}
				if !strings.HasPrefix(it[1], "Last message ") {
					t.Errorf("%s: last message %q", c.name, it[1])
				}
			}
			if c.team != "" && (len(it) < 3 || it[2] != c.team) {
				t.Errorf("%s: teams %q, want %q", c.name, it, c.team)
			}
			r := p.Rect()
			if r.Dx() != 340 {
				t.Errorf("%s: width %d, want HoverCard's 340", c.name, r.Dx())
			}
			// position "top": centred over the trigger, 12px above it
			if r.Max.Y != anchor.Min.Y-hovercard.OffsetY || r.Min.X+r.Dx()/2 != anchor.Min.X+anchor.Dx()/2 {
				t.Errorf("%s: card %v, want it centred 12px above %v", c.name, r, anchor)
			}
			chcShot(t, ops, "popups-channel-card-"+c.name+"-"+mode)
		}
		// the composer's (TextyTooltip): no buttons, the same body
		p := NewChannelHoverCard(h, anchor, fakeslack.ThreadSharedID, false, true)
		chcFrames(t, p, func() bool { return p.State() == ChannelCardFull })
		if p.Button() != "" {
			t.Errorf("composer card has %q", p.Button())
		}
	}
}

// getExternalTeamsTextElement for one, two and more teams.
func TestChannelHoverCardTeamsText(t *testing.T) {
	h, _ := chcEnv(t, false)
	p := NewChannelHoverCard(h, image.Rectangle{}, "C1", true, false)
	for _, c := range []struct {
		ids  []string
		want string
	}{
		{[]string{"T1"}, " is in this channel"},
		{[]string{"T1", "T2"}, " and  are in this channel"},
		{[]string{"T1", "T2", "T3"}, " and 2 other teams are in this channel"},
		{[]string{"T1", "T2", "T3", "T4"}[:2], " and  are in this channel"},
	} {
		if got := i18n.Plain(p.teamsParts(c.ids)); got != c.want {
			t.Errorf("%v: %q, want %q (unknown team names are blank)", c.ids, got, c.want)
		}
	}
	// the <t1> run is its own part (strong, with the tooltip)
	t1 := false
	for _, pt := range p.teamsParts([]string{"T1", "T2"}) {
		t1 = t1 || pt.Tag == "t1"
	}
	if t1 {
		t.Error("two teams have no <t1> run")
	}
	for _, pt := range p.teamsParts([]string{"T1", "T2", "T3"}) {
		t1 = t1 || pt.Tag == "t1" && pt.Text == "2 other teams"
	}
	if !t1 {
		t.Errorf("three teams: no <t1>2 other teams</t1> in %+v", p.teamsParts([]string{"T1", "T2", "T3"}))
	}
	if got := memberCountText(1); got != "1 person in this channel" {
		t.Errorf("one member: %q", got)
	}
}

// An app's slug in the composer gets SmallMemberEntity in the member tip
// (padding 6 12 6 6: 32px tall for a 20px line); a person's never does.
func TestComposerMemberTip(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, _ := chcEnv(t, dark)
		app, ok := h.a.User(fakeslack.AppUserID)
		if !ok || !MemberTipApplies(app) {
			t.Fatalf("the app fixture %+v %v isn't an app", app, ok)
		}
		if u, ok := h.a.User(fakeslack.ProfileID); !ok || MemberTipApplies(u) {
			t.Fatalf("a person gets the app tip: %+v", u)
		}
		if memberBadge(app) != "APP" {
			t.Errorf("badge %q", memberBadge(app))
		}
		ops := new(op.Ops)
		gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(600, 200))}
		var st TipState
		st.Texty = true
		var d layout.Dimensions
		at(gtx, image.Pt(200, 120), func(gtx layout.Context) layout.Dimensions {
			TooltipBox(gtx, h.th, &st, true, image.Pt(60, 20), Above, func(gtx layout.Context) layout.Dimensions {
				d = MemberTip(gtx, h, fakeslack.AppUserID)
				return d
			})
			return layout.Dimensions{}
		})
		if d.Size.Y != 32 || d.Size.X < 6+20+8+12 {
			t.Errorf("tip %v, want 32 tall (6 + 20 + 6) and the avatar, name and badge across", d.Size)
		}
		if pl := st.Placed(); pl.Box.Empty() {
			t.Errorf("placed %+v: no tip drawn", pl)
		}
		if dir := os.Getenv("HOVER_SHOTS_DIR"); dir != "" {
			img := pixeltest.Render(t, 600, 200, ops)
			f, err := os.Create(filepath.Join(dir, "composer-app-tip-"+map[bool]string{false: "light", true: "dark"}[dark]+".png"))
			if err != nil {
				t.Fatal(err)
			}
			_ = png.Encode(f, img)
			f.Close()
		}
	}
}
