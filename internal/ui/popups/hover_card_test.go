package popups

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/theme"
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
	a.EnsureUsers(ctx, []string{fakeslack.SelfID, fakeslack.ProfileID, fakeslack.AdminID, fakeslack.DeactivatedID, fakeslack.SlackbotID,
		fakeslack.AppUserID, fakeslack.ExternalID, fakeslack.ExternalAppUserID})
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

// hcLoad lays p out until cond (its async loads land through Pump).
func hcLoad(t *testing.T, p *ProfileCard, what string, cond func() bool) image.Rectangle {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r := hcLayout(t, p)
		Pump()
		if cond() {
			return hcLayout(t, p)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
		_ = r
	}
}

// An app's bot user gets Slack's AppHoverCard (320 wide, above the
// trigger) from apps.profile.get: name, tagline, the menu items that
// apply, Slack's installation summary and Go to App; a message's bot_id
// alone opens the same card.
func TestHoverCardApp(t *testing.T) {
	h := hcEnv(t)
	anchor := image.Rect(120, 600, 200, 622)
	p := NewHoverCard(h, anchor, fakeslack.AppUserID, "")
	r := hcLoad(t, p, "the app card", func() bool { return p.app.card != nil && p.app.team != "" })
	if !p.IsApp() {
		t.Fatal("a bot user got the member card")
	}
	if r.Dx() != 320 || r.Max.Y != anchor.Min.Y-hovercard.OffsetY || r.Min.X != anchor.Min.X {
		t.Errorf("app card %v, want 320 wide with its bottom-left 12px above %v", r, anchor)
	}
	c := *p.app.card
	if c.Name != "Deploy Bot" || c.Tagline == "" || c.External {
		t.Errorf("card %+v", c)
	}
	var labels []string
	for _, it := range p.appMenu(c, h.a.Self()) {
		labels = append(labels, it.label)
	}
	if got := strings.Join(labels, "|"); got != "About this app|Configuration|Add this app to a channel…|Add to VIP" {
		t.Errorf("menu %q", got)
	}
	if !p.appShowDM(c) {
		t.Error("no Go to App for the app's bot user")
	}
	sum := p.appSummary(c, h.a.Self())
	if len(sum) != 1 || sum[0].s != "Deploy Bot is installed on Fake Club." {
		t.Errorf("summary %+v", sum)
	}
	// installer wording when the home team isn't known
	p.app.team = ""
	if sum := p.appSummary(c, h.a.Self()); len(sum) != 2 || !sum[0].mention || sum[0].s != "@avery.admin" || sum[1].s != " added Deploy Bot to this workspace." {
		t.Errorf("installer summary %+v", sum)
	}

	b := NewHoverCard(h, anchor, "", fakeslack.AppBotID)
	hcLoad(t, b, "the bot's app card", func() bool { return b.app.card != nil })
	if !b.IsApp() || b.app.card.Name != "Deploy Bot" {
		t.Errorf("bot-only card: app=%v %+v", b.IsApp(), b.app.card)
	}

	// another org's app: the external banner, no Configuration
	x := NewHoverCard(h, anchor, fakeslack.ExternalAppUserID, "")
	xr := hcLoad(t, x, "the external app card", func() bool { return x.app.card != nil && x.app.team != "" })
	if !x.app.card.External || x.app.team != "Partner Co" {
		t.Errorf("external app %+v team %q", x.app.card, x.app.team)
	}
	for _, it := range x.appMenu(*x.app.card, h.a.Self()) {
		if it.label == "Configuration" {
			t.Error("another org's app offers Configuration")
		}
	}
	if xr.Dy() <= r.Dy()-200 {
		t.Errorf("external card %v suspiciously short next to %v", xr, r)
	}
}

// A Slack Connect member's card carries AppExternalTeamBanner with the
// partner team's name; members of the workspace don't.
func TestHoverCardExternal(t *testing.T) {
	h := hcEnv(t)
	anchor := image.Rect(120, 600, 200, 622)
	in := NewHoverCard(h, anchor, fakeslack.ProfileID, "")
	inR := hcLayout(t, in)
	p := NewHoverCard(h, anchor, fakeslack.ExternalID, "")
	hcLoad(t, p, "the partner team", func() bool { return p.teamName != "" })
	if p.IsApp() || p.teamName != "Partner Co" {
		t.Fatalf("external member: app=%v team %q", p.IsApp(), p.teamName)
	}
	if in.teamName != "" || in.teamAsked {
		t.Error("a workspace member's card looked up a team")
	}
	u, _ := lookupUser(h, fakeslack.ExternalID)
	if !u.External {
		t.Fatal("fixture not external")
	}
	if !pfShowsPresence(u) {
		t.Error("an external member has no presence")
	}
	_ = inR
}

// Slackbot's card shows its presence (Slack's heart), a deactivated
// member's none, a bot's none (on the click card).
func TestHoverCardPresenceRule(t *testing.T) {
	h := hcEnv(t)
	sb, _ := lookupUser(h, fakeslack.SlackbotID)
	gone, _ := lookupUser(h, fakeslack.DeactivatedID)
	bot, _ := lookupUser(h, fakeslack.AppUserID)
	if !sb.IsBot || sb.Presence != "active" {
		t.Fatalf("fixture: Slackbot %+v", sb)
	}
	if !pfShowsPresence(sb) {
		t.Error("Slackbot's card hides its presence")
	}
	if pfShowsPresence(gone) || pfShowsPresence(bot) {
		t.Error("presence on a deactivated member or a bot")
	}
}

// A bot or app user's card has no local time row: Slack's hover card
// (ProfileHoverCard, useProfileHoverApp) and click card
// (ProfileCardTrigger's ProfileCardContainer) both open the app card for
// a member that isn't human (qSRE isAppOrBot: is_app_user, is_bot, the
// USLACK id), and LocalTime (A05L via dD/2) draws nothing for a member
// with neither a tz nor a tz_offset.
func TestBotCardNoLocalTime(t *testing.T) {
	h := hcEnv(t)
	if u, ok := lookupUser(h, fakeslack.AppUserID); !ok || !u.IsBot {
		t.Fatalf("fixture %s: %+v", fakeslack.AppUserID, u)
	}
	click := NewProfileCard(h, image.Pt(300, 300), fakeslack.AppUserID)
	r := hcLoad(t, click, "the click card's app card", func() bool { return click.app.card != nil })
	if !click.IsApp() || r.Dx() != 320 {
		t.Errorf("a bot user's click card: app=%v %v, want Slack's 320-wide app card", click.IsApp(), r)
	}
	b := NewBotProfileCard(h, image.Pt(300, 300), fakeslack.AppBotID)
	hcLoad(t, b, "the bot-only click card", func() bool { return b.app.card != nil })
	if !b.IsApp() {
		t.Error("a bot_id-only click card isn't the app card")
	}
	person := NewProfileCard(h, image.Pt(300, 300), fakeslack.ProfileID)
	if hcLayout(t, person); person.IsApp() {
		t.Error("a person's click card is the app card")
	}

	for _, c := range []struct {
		u     core.User
		human bool
	}{
		{core.User{ID: "U1", IsBot: true}, false},
		{core.User{ID: "U2", AppUser: true}, false}, // is_app_user without is_bot
		{core.User{ID: "USLACK"}, false},
		{core.User{ID: fakeslack.SlackbotID}, true}, // USLACKBOT: the member card's "Built by Slack"
		{core.User{ID: "U3"}, true},
	} {
		if got := pfHuman(c.u); got != c.human {
			t.Errorf("pfHuman(%+v) = %v, want %v", c.u, got, c.human)
		}
	}
	for _, c := range []struct {
		u     core.User
		clock bool
	}{
		{core.User{TZ: "Europe/Berlin", TZOffset: 3600}, true},
		{core.User{TZOffset: -18000}, true},
		{core.User{TZ: "Europe/London"}, true}, // offset 0, but a tz
		{core.User{}, false},                   // no tz, offset 0: getMemberTimezoneOffset 0 is falsy
		{core.User{TZ: "undefined"}, false},
	} {
		if got := pfHasClock(c.u); got != c.clock {
			t.Errorf("pfHasClock(%+v) = %v, want %v", c.u, got, c.clock)
		}
	}
}
