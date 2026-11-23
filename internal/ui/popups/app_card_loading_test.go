package popups

// Karim (2026-11-22, α.137/.138 on Windows): "when i over a bot profile
// it becomes tall and then comes back to the thing. it also shows that
// its a bit sloppy".
//
// Slack's hover card over a bot or an app (ProfileHoverTrigger →
// ProfileHoverCard JM3T → AppHoverCard → AppProfileCard 4JWQ) opens 350ms
// after the pointer rests, renders nothing while the member isn't in the
// store (isMemberHuman, Emf2, needs a loaded member; with no bot there's
// no app), a bot found by its id goes straight to its app's card, the
// app's card is the 320 × 380 .p-app_profile_card--loading with the jumbo
// blue InfiniteSpinner until apps.profile.get puts the app in the store,
// and then the card, once: the home team and the installer are what the
// store holds, filled in as their reads land. The popover follows the
// content's size (TxLv's ResizeObserver), so the only change of height is
// the loading card's 380 to the card's own.
//
// Clack drew a bot user it didn't hold yet as the 340-wide member card's
// loading state, then the app card's loading state, then the app; and it
// kept the loading card up until team.info and the installer's users.info
// had answered too. Synthetic: the in-repo fake Slack's app fixtures.

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// acEnv is hcEnv holding only the users in ensure (and the viewer), and
// the fake to hook.
func acEnv(t *testing.T, ensure ...string) (*tzHost, *fakeslack.Server) {
	t.Helper()
	return acEnvSize(t, fakeslack.Small, ensure...)
}

// acEnvSize is acEnv on a fake of size sz.
func acEnvSize(t *testing.T, sz fakeslack.Size, ensure ...string) (*tzHost, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.SetHook(nil); fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.EnsureUsers(ctx, append([]string{fakeslack.SelfID}, ensure...))
	// the viewer's own team and org: the store holds them from boot
	a.EnsureTeam(ctx, fakeslack.TeamID)
	a.EnsureTeam(ctx, fakeslack.EnterpriseID)
	if u, ok := a.S.User(fakeslack.SelfID); ok {
		a.S.Self = *u
	}
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	return &tzHost{a: a, th: th, im: imgcache.New(noNet, "", nil)}, fs
}

// acFrame lays p out once in a 1000x800 window at wall-clock time into
// ops and returns its rect.
func acFrame(p *ProfileCard, ops *op.Ops) image.Rectangle {
	ops.Reset()
	gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1000, 800))}
	p.Layout(gtx)
	return p.Rect()
}

// acFrames runs frames ~10ms apart until done holds (and 300ms more) or
// d passes, and returns every frame's rect.
func acFrames(t *testing.T, p *ProfileCard, d time.Duration, done func() bool) []image.Rectangle {
	t.Helper()
	ops := new(op.Ops)
	var out []image.Rectangle
	var doneAt time.Time
	for dl := time.Now().Add(d); time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
		out = append(out, acFrame(p, ops))
		if doneAt.IsZero() && done() {
			doneAt = time.Now()
		}
		if !doneAt.IsZero() && time.Since(doneAt) > 300*time.Millisecond {
			return out
		}
	}
	t.Fatalf("not loaded after %v", d)
	return out
}

// wantSlackHeights checks frames against Slack's: nothing, or the 320
// wide card; first the 380 loading card, then one height, kept.
func wantSlackHeights(t *testing.T, what string, rs []image.Rectangle) {
	t.Helper()
	final := -1
	for i, r := range rs {
		if r.Empty() {
			if final >= 0 {
				t.Errorf("%s: frame %d drew no card after the loaded one", what, i)
				return
			}
			continue
		}
		if r.Dx() != 320 {
			t.Errorf("%s: frame %d drew a %dx%d card; Slack's app card is 320 wide from its first frame (the member card's loading state is 340)", what, i, r.Dx(), r.Dy())
			return
		}
		switch {
		case final < 0 && r.Dy() == 380: // .p-app_profile_card--loading
		case final < 0:
			final = r.Dy()
		case r.Dy() != final:
			t.Errorf("%s: frame %d is %d tall after the loaded card's %d; Slack's changes height once, from the loading card's 380", what, i, r.Dy(), final)
			return
		}
	}
	if final < 0 {
		t.Errorf("%s: the card never left its loading state", what)
	}
}

// The heights a bot's or an app's hover card goes through from its first
// frame: a bot user the store holds, one it doesn't yet, and a bot known
// only by its id (a message's bot_id).
func TestAppHoverCardHeightsLikeSlack(t *testing.T) {
	anchor := image.Rect(300, 600, 400, 620)
	for _, c := range []struct {
		name     string
		ensure   []string
		user, bt string
	}{
		{"bot user held", []string{fakeslack.AppUserID, fakeslack.AdminID}, fakeslack.AppUserID, ""},
		{"bot user not held yet", []string{fakeslack.AdminID}, fakeslack.AppUserID, ""},
		{"bot by its id", []string{fakeslack.AdminID}, "", fakeslack.AppBotID},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, _ := acEnv(t, c.ensure...)
			p := NewHoverCard(h, anchor, c.user, c.bt)
			rs := acFrames(t, p, 10*time.Second, func() bool { return p.app.card != nil })
			wantSlackHeights(t, c.name, rs)
		})
	}
}

// The card draws once apps.profile.get answers, while the installer's
// users.info is still out (Slack's AppProfileCard renders from the store
// then), and keeps its height when that read lands.
func TestAppHoverCardShowsWithTheAppProfile(t *testing.T) {
	h, fs := acEnv(t, fakeslack.AppUserID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// the viewer's own team and org: the store holds them from boot
	h.a.EnsureTeam(ctx, fakeslack.TeamID)
	h.a.EnsureTeam(ctx, fakeslack.EnterpriseID)
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "users.info" || strings.HasSuffix(method, "users/info") { // the edge read EnsureUsers makes
			<-release
		}
		return nil, false
	})
	p := NewHoverCard(h, image.Rect(300, 600, 400, 620), fakeslack.AppUserID, "")
	ops := new(op.Ops)
	var r image.Rectangle
	loaded := false
	for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl) && !loaded; time.Sleep(10 * time.Millisecond) {
		r = acFrame(p, ops)
		loaded = p.app.card != nil && !r.Empty() && r.Dy() != 380
	}
	if !loaded {
		free()
		t.Fatalf("the card was still loading (%v) 5s after the open with only users.info out; Slack's draws once apps.profile.get answers", r)
	}
	free()
	rs := acFrames(t, p, 5*time.Second, func() bool { _, ok := h.a.User(fakeslack.AdminID); return ok })
	for _, q := range rs {
		if q.Dy() != r.Dy() {
			t.Errorf("the card went from %d to %d tall when the installer's read landed", r.Dy(), q.Dy())
			break
		}
	}
}

// The loading card is Slack's: 320 × 380 with the jumbo (52px) blue
// InfiniteSpinner in its middle (content-hgl-1 ink there), held while
// apps.profile.get is out; then the card. CLACK_SHOTS saves both
// (app-card-loading.png, app-card-loaded.png).
func TestAppHoverCardLoadingSpinner(t *testing.T) {
	h, fs := acEnv(t, fakeslack.AppUserID, fakeslack.AdminID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.a.EnsureTeam(ctx, fakeslack.TeamID)
	h.a.EnsureTeam(ctx, fakeslack.EnterpriseID)
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "apps.profile.get" {
			<-release
		}
		return nil, false
	})
	p := NewHoverCard(h, image.Rect(300, 600, 400, 620), fakeslack.AppUserID, "")
	ops := new(op.Ops)
	var r image.Rectangle
	for i := 0; i < 5; i++ {
		r = acFrame(p, ops)
		time.Sleep(10 * time.Millisecond)
	}
	if r.Dx() != 320 || r.Dy() != 380 {
		t.Fatalf("the loading card is %v, want Slack's 320 × 380", r)
	}
	img := pixeltest.Render(t, 1000, 800, ops)
	acShot(t, "app-card-loading", img, r)
	cl := colorsOf(h.th)
	mid := image.Rect(r.Min.X+(320-52)/2, r.Min.Y+(380-52)/2, r.Min.X+(320+52)/2, r.Min.Y+(380+52)/2)
	if n := pixeltest.Ink(img, mid, cl.Link, cl.Bg); n < 40 {
		t.Errorf("%d pixels of content-hgl-1 in the loading card's middle 52px (%v); Slack draws its blue InfiniteSpinner there", n, mid)
	}
	free()
	acFrames(t, p, 10*time.Second, func() bool { return p.app.card != nil })
	r = acFrame(p, ops)
	acShot(t, "app-card-loaded", pixeltest.Render(t, 1000, 800, ops), r)
}

// acShot saves the card's part of img under CLACK_SHOTS, over white.
func acShot(t *testing.T, name string, img *image.RGBA, r image.Rectangle) {
	t.Helper()
	dir := os.Getenv("CLACK_SHOTS")
	if dir == "" {
		return
	}
	r = r.Inset(-16).Intersect(img.Bounds())
	out := image.NewRGBA(image.Rectangle{Max: r.Size()})
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			c := img.RGBAAt(r.Min.X+x, r.Min.Y+y) // premultiplied: over white
			w := 0xff - c.A
			out.SetRGBA(x, y, color.RGBA{c.R + w, c.G + w, c.B + w, 0xff})
		}
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		t.Fatal(err)
	}
}

// Hack Club's #new-bots (C0BKP49D58E, Karim: "about every bot in" it),
// read for ids and flags only: every poster and member bot there is an
// app's bot user, in three shapes (fakeslack Size.NewBots): a member
// whose profile.bot_id bots.info no longer finds (most of them), an app
// whose messages carry a newer bot than its member's profile, and a live
// bot. Each card goes from the loading card to the app once, and asks
// for the app as Slack's does: with bot_home_team, the homeWorkspaceId
// useProfileHoverApp works out (the member's workspace, else the
// viewer's), so a stale bot reads as the workspace's install, not the
// org's uninstalled app.
func TestAppHoverCardNewBotsShapes(t *testing.T) {
	anchor := image.Rect(300, 600, 400, 620)
	sz := fakeslack.Small
	sz.NewBots = true
	for _, c := range []struct {
		name      string
		user, bt  string
		wantBot   string // the bot apps.profile.get is asked for
		active    bool   // auth or config came back: no "This app is unavailable"
		installed bool
	}{
		{"stale profile bot", fakeslack.NewBotsStaleUserID, "", fakeslack.NewBotsStaleBotID, false, true},
		{"two bots, the member", fakeslack.NewBotsTwoUserID, "", fakeslack.NewBotsTwoOldBotID, false, true},
		// a message row's trigger: the member and the message's bot
		// (messages row.hoverBot, Slack's MessageSender botId), the app's
		// installed, active card, as Slack's useProfileHoverApp asks for
		// the message's bot
		{"two bots, a message row", fakeslack.NewBotsTwoUserID, fakeslack.NewBotsTwoBotID, fakeslack.NewBotsTwoBotID, true, true},
		{"two bots, a message's with no member", "", fakeslack.NewBotsTwoBotID, fakeslack.NewBotsTwoBotID, true, true},
		{"live bot", fakeslack.NewBotsLiveUserID, "", fakeslack.NewBotsLiveBotID, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			ensure := []string{fakeslack.AdminID}
			if c.user != "" {
				ensure = append(ensure, c.user)
			}
			h, fs := acEnvSize(t, sz, ensure...)
			var mu sync.Mutex
			var asked []url.Values
			fs.SetHook(func(method string, f url.Values) (any, bool) {
				if method == "apps.profile.get" {
					mu.Lock()
					asked = append(asked, f)
					mu.Unlock()
				}
				return nil, false
			})
			p := NewHoverCard(h, anchor, c.user, c.bt)
			rs := acFrames(t, p, 10*time.Second, func() bool { return p.app.card != nil })
			wantSlackHeights(t, c.name, rs)
			mu.Lock()
			defer mu.Unlock()
			if len(asked) == 0 {
				t.Fatal("no apps.profile.get")
			}
			f := asked[0]
			if f.Get("bot") != c.wantBot || f.Get("bot_home_team") != fakeslack.TeamID {
				t.Errorf("apps.profile.get bot=%q bot_home_team=%q; Slack asks bot=%s bot_home_team=%s (homeWorkspaceId)", f.Get("bot"), f.Get("bot_home_team"), c.wantBot, fakeslack.TeamID)
			}
			card := *p.app.card
			if card.Installed == nil || *card.Installed != c.installed || card.HomeTeamID != fakeslack.TeamID {
				t.Errorf("the card reads the app as installed=%v on %q; Slack's answer is the workspace's install (%v on %s)", card.Installed != nil && *card.Installed, card.HomeTeamID, c.installed, fakeslack.TeamID)
			}
			if card.Active != c.active {
				t.Errorf("active %v, want %v (installation.is_active: auth or config in the answer)", card.Active, c.active)
			}
		})
	}
}
