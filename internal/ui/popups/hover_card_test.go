package popups

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/hovercard"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// hcEnv is a popups host over a core on the in-repo fake Slack, with the
// fixed profile variants fetched (users/info).
func hcEnv(t *testing.T) *tzHost {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.EnsureUsers(ctx, []string{fakeslack.SelfID, fakeslack.ProfileID, fakeslack.AdminID, fakeslack.DeactivatedID, fakeslack.SlackbotID})
	if u, ok := a.S.User(fakeslack.SelfID); ok {
		a.S.Self = *u
	}
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	return &tzHost{a: a, th: th, im: imgcache.New(noNet, "", nil)}
}

// hcLayout lays the card out in a 1000x800 window and returns its rect.
func hcLayout(t *testing.T, p *ProfileCard) image.Rectangle {
	t.Helper()
	ops := new(op.Ops)
	now := time.Date(2026, 11, 8, 15, 42, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1000, 800))}
		p.Layout(gtx)
	}
	return p.Rect()
}

// The hover card shows each variant from what core caches: Slack's
// MemberProfileHoverCard order (banner, entity, status, local time,
// buttons), above the trigger, flipped below near the top.
func TestHoverCardVariants(t *testing.T) {
	h := hcEnv(t)
	for _, id := range []string{fakeslack.ProfileID, fakeslack.AdminID, fakeslack.DeactivatedID, fakeslack.SlackbotID} {
		if _, ok := lookupUser(h, id); !ok {
			t.Fatalf("fake users/info didn't serve %s", id)
		}
	}
	anchor := image.Rect(120, 600, 200, 622)
	heights := map[string]int{}
	for _, id := range []string{fakeslack.ProfileID, fakeslack.AdminID, fakeslack.DeactivatedID, fakeslack.SlackbotID, fakeslack.SelfID} {
		p := NewHoverProfileCard(h, anchor, id)
		r := hcLayout(t, p)
		if r.Dx() != 340 {
			t.Errorf("%s: width %d, want Slack's 340", id, r.Dx())
		}
		if r.Max.Y != anchor.Min.Y-hovercard.OffsetY || r.Min.X != anchor.Min.X {
			t.Errorf("%s: card %v, want its bottom-left 12px above the trigger %v", id, r, anchor)
		}
		if p.Dismissed() || p.focus {
			t.Errorf("%s: the hover card took focus or closed itself", id)
		}
		heights[id] = r.Dy()
	}
	// the admin card carries a banner, a local time and buttons; Slackbot's
	// has only "Built by Slack" below its entity
	if heights[fakeslack.AdminID] <= heights[fakeslack.SlackbotID] {
		t.Errorf("admin card (%d) not taller than Slackbot's (%d)", heights[fakeslack.AdminID], heights[fakeslack.SlackbotID])
	}

	// near the window's top it opens below the trigger
	p := NewHoverProfileCard(h, image.Rect(120, 90, 200, 112), fakeslack.ProfileID)
	if r := hcLayout(t, p); r.Min.Y != 112+hovercard.OffsetY {
		t.Errorf("near the top: card %v, want it 12px below the trigger", r)
	}
}

func TestHoverCardDeactivatedButtons(t *testing.T) {
	h := hcEnv(t)
	u, _ := lookupUser(h, fakeslack.DeactivatedID)
	p := NewHoverProfileCard(h, image.Rect(120, 600, 200, 622), u.ID)
	hcLayout(t, p)
	if !u.Deleted {
		t.Fatal("fixture not deactivated")
	}
	if !p.moreLocal.Empty() {
		t.Error("a deactivated member's card has a ⋮ button")
	}
}

func TestLocalTimeLabel(t *testing.T) {
	h := hcEnv(t)
	h.a.S.Lock()
	h.a.S.Self.TZ = "America/New_York"
	h.a.S.Unlock()
	berlin := core.User{TZ: "Europe/Berlin"}
	tokyo := core.User{TZ: "Asia/Tokyo"}
	hawaii := core.User{TZ: "Pacific/Honolulu"}
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tc := range []struct {
		u    core.User
		now  string
		want string
	}{
		{berlin, "2026-11-08T14:42:00Z", "3:42 PM local time"},
		{tokyo, "2026-11-08T20:00:00Z", "5:00 AM (tomorrow) local time"},
		{hawaii, "2026-11-09T05:30:00Z", "7:30 PM (yesterday) local time"},
	} {
		if got := pfLocalTimeLabel(h, tc.u, at(tc.now)); got != tc.want {
			t.Errorf("%s at %s: %q, want %q", tc.u.TZ, tc.now, got, tc.want)
		}
	}
}
