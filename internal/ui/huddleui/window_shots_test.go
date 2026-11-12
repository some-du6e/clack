package huddleui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// shotPictures fetches the shots' pictures: Slack's public CDN files
// (the huddle theme, emoji; static, no cookie) when CI can reach them,
// else a stand-in; avatars.test avatars are drawn here.
func shotPictures(ctx context.Context, url string) ([]byte, error) {
	if strings.HasPrefix(url, "https://avatars.test/") {
		return avatarPNG(url), nil
	}
	if strings.HasPrefix(url, "https://a.slack-edge.com/") {
		cl := http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			if resp, err := cl.Do(req); err == nil {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
				resp.Body.Close()
				if resp.StatusCode == 200 && len(b) > 0 {
					return b, nil
				}
			}
		}
		if strings.Contains(url, "/img/huddles/") {
			return desertPNG(), nil // offline: a painted-desert stand-in
		}
		return discPNG(color.NRGBA{R: 0xf5, G: 0xc0, B: 0x2a, A: 0xff}), nil
	}
	return nil, errors.New("shot pictures: " + url)
}

// avatarPNG is a 192px avatar: a colour per user with a lighter head.
func avatarPNG(url string) []byte {
	h := 0
	for _, c := range url {
		h = h*31 + int(c)
	}
	base := []color.NRGBA{{0x2b, 0x6c, 0xb0, 0xff}, {0xc2, 0x4e, 0x3a, 0xff}, {0x3c, 0x8d, 0x5a, 0xff}, {0x8a, 0x4f, 0xa8, 0xff}}[(h&0x7fffffff)%4]
	const n = 192
	m := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			c := base
			if dx, dy := x-n/2, y-n*2/5; dx*dx+dy*dy < (n/5)*(n/5) || (y > n*7/10 && (x-n/2)*(x-n/2) < (n/3)*(n/3)) {
				c = color.NRGBA{R: 0xf3, G: 0xd9, B: 0xc4, A: 0xff}
			}
			m.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, m)
	return b.Bytes()
}

// desertPNG stands in for a theme picture when the CDN can't be reached.
func desertPNG() []byte {
	const w, h = 640, 400
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBA{R: uint8(250 - y/4), G: uint8(190 - y/5), B: uint8(150 - y/6), A: 0xff}
			if y > h/2+int(30*float64(x%160)/160) {
				c = color.NRGBA{R: 0xc8, G: 0x7a, B: 0x4a, A: 0xff}
			}
			m.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, m)
	return b.Bytes()
}

func discPNG(c color.NRGBA) []byte {
	const n = 64
	m := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if dx, dy := x-n/2, y-n/2; dx*dx+dy*dy < n*n/4 {
				m.SetNRGBA(x, y, c)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, m)
	return b.Bytes()
}

// windowShotEnv is a joined fake huddle whose window has pictures, the
// fake's themes and Clack's main theme in the given mode.
func windowShotEnv(t *testing.T, dark bool, people int) (*shotEnv, *huddle.Session) {
	t.Helper()
	e := newShotEnv(t, dark)
	users := []slack.User{
		{ID: huddle.FakeSelfUser, Name: "me", RealName: "Test Self"},
		{ID: huddle.FakeRemoteUser, Name: "remote", RealName: "Remote Example"},
		{ID: "U0THIRD001", Name: "third", RealName: "Third Person"},
		{ID: "U0FOURTH01", Name: "fourth", RealName: "Fourth Person"},
	}
	for i := range users {
		u := "https://avatars.test/" + users[i].ID + ".png"
		users[i].Profile.Image72, users[i].Profile.Image192, users[i].Profile.Image512 = u, u, u
	}
	e.h.app.S.PutUsers(users)
	e.h.imgs = imgcache.New(shotPictures, "", nil)
	e.w.th.Images = e.h.imgs
	e.noShare = true // the people, not a screen share, on the stage
	s := e.join(nil, nil)
	e.c.Client = e.fk.Caller() // huddles.listBackgrounds
	waitState(t, s, "connected", func(st huddle.State) bool {
		return st.Phase == huddle.Connected && len(st.Participants) > 1
	})
	// someone picked ⋯ › Choose a huddle theme › Arizona
	s.HandleRoomEvent("sh_room_update", huddle.FakeBackgroundUpdate(hcChannel, "ARIZONA"))
	if got := s.State().Background; got != "ARIZONA" {
		t.Fatalf("theme %q after the room update", got)
	}
	extra := []huddle.Participant{
		{UserID: "U0THIRD001", AttendeeID: "att-third", InMedia: true},
		{UserID: "U0FOURTH01", AttendeeID: "att-fourth", InMedia: true, Muted: true},
	}
	e.w.tweak = func(st *huddle.State) {
		for i := range st.Participants {
			if !st.Participants[i].Self {
				st.Participants[i].Muted = true
				st.Participants[i].Sticker = "coffee" // Be right back
			}
		}
		if people > 2 {
			st.Participants = append(st.Participants, extra[:people-2]...)
		}
	}
	return e, s
}

// settle lays the window out until its pictures have loaded and the
// theme has faded in.
func settle(t *testing.T, e *shotEnv, w, h int) {
	t.Helper()
	var ops op.Ops
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		ops.Reset()
		e.w.layout(shotCtx(&ops, w, h))
		if e.w.theme.url != "" && e.h.imgs.Pending() == 0 {
			e.w.theme.at = time.Now().Add(-time.Second) // past the fade
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the theme never loaded (shown %q, %d pending)", e.w.theme.url, e.h.imgs.Pending())
}

// The huddle window next to Slack's compact huddle window (Karim's α.37
// comparison): 2 and 4 people, light and dark, the other people on "Be
// right back", the Arizona theme, and the sticker tray open. Pictures:
// huddles-audit-shots' window-compact-*.png.
func TestHuddleShotsWindowCompact(t *testing.T) {
	const W, H = 589, 351 // the window's size (Slack's compact window)
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, people := range []int{2, 4} {
			e, s := windowShotEnv(t, dark, people)
			st := s.State()
			e.w.tweak(&st)
			if title, sub := e.c.windowTitle(st); title != "Huddle in huddle-test" || sub == "" {
				t.Fatalf("title %q / %q", title, sub)
			}
			// the layout's logic, before any rendering (pixel tests skip
			// without a GPU)
			var ops op.Ops
			gtx := shotCtx(&ops, W, H)
			gallery := image.Rect(0, gtx.Dp(winTitleH)+gtx.Dp(16)+gtx.Dp(connBtnH), gtx.Dp(W), gtx.Dp(H)-gtx.Dp(winFooterH))
			g := compactTiles(gtx, gallery, people)
			if len(g.tiles) != people || g.more != 0 {
				t.Fatalf("%d people: %d tiles, +%d", people, len(g.tiles), g.more)
			}
			if side := g.tiles[0].Dx() / shotScale; side > 150 || side < 99 {
				t.Fatalf("%d people: %d dp tiles, want Slack's one row of 99-150", people, side)
			}
			settle(t, e, W, H)
			if e.c.themeURL(st.RoomID, "ARIZONA") != "https://a.slack-edge.com/f06881b/img/huddles/HaleyTippmann-Arizona.jpg" {
				t.Fatal("the theme's picture isn't huddles.listBackgrounds' url")
			}
			name := "window-compact-" + mode + "-" + itoa(people)
			img := render(t, name, W, H, func(gtx C) { e.w.layout(gtx) })
			// Leave ends the centred toolbar row, right after ⋯
			leave := image.Rect(e.w.moreBtn.Max.X, (H-winFooterH)*shotScale, e.w.moreBtn.Max.X+100*shotScale, (H-5)*shotScale)
			if n := pixeltest.Count(img, leave, hudLeave, 12); n < 3000 {
				t.Fatalf("%s: no Leave after ⋯ (%d px)", name, n)
			}
			// the connection-quality button, white, at the stage's top right
			cb := image.Rect((W-24-connBtnW)*shotScale, (winTitleH+16)*shotScale, (W-24)*shotScale, (winTitleH+16+connBtnH)*shotScale)
			if n := pixeltest.Count(img, cb, hudWhite, 8); n < cb.Dx()*cb.Dy()/2 {
				t.Fatalf("%s: no connection button (%d px)", name, n)
			}
			// the sticker pill follows the theme: dark in dark mode
			main := e.h.th
			t0 := g.tiles[1]
			pill := image.Rect(t0.Min.X+gtx.Dp(8), t0.Min.Y+gtx.Dp(8), t0.Min.X+gtx.Dp(40), t0.Min.Y+gtx.Dp(40))
			if n := pixeltest.Count(img, pill, main.C.Bg, 6); n < pill.Dx()*pill.Dy()/4 {
				t.Fatalf("%s: the sticker pill isn't the theme's background (%d px)", name, n)
			}
			if people == 2 {
				e.w.stick = true   // the sticker tray
				settle(t, e, W, H) // its emoji pictures
				render(t, name+"-tray", W, H, func(gtx C) { e.w.layout(gtx) })
				e.w.stick = false
			}
		}
	}
}

// The sticker pill's emoji is Slack's 20px image: laid out at 18sp in a
// 20px box the label had no room for the 1.2em image and drew "…" (Karim:
// "…Be right back").
func TestStickerEmojiFits(t *testing.T) {
	th := theme.Bare(true)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	imgs := &askImages{}
	th.Images = imgs
	var ops op.Ops
	gtx := shotCtx(&ops, 100, 100)
	es := gtx.Dp(20)
	d := emojiAt(gtx, th, image.Pt(es, es), es, "☕")
	if imgs.n == 0 {
		t.Fatal("the coffee image was never asked for: the label truncated it")
	}
	if d.Size.X < es {
		t.Fatalf("emoji %d px wide, want %d", d.Size.X, es)
	}
	// the old sizing, for the record: it can't fit
	imgs.n = 0
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(es, es)}
	m := op.Record(gtx.Ops)
	th.Label(g, 18, theme.Regular, hudInk, "☕")
	m.Stop()
	if imgs.n != 0 {
		t.Log("note: the theme now fits a 1.2em emoji in a 20px box at 18sp")
	}
}

// askImages counts emoji image requests.
type askImages struct {
	mu sync.Mutex
	n  int
}

func (a *askImages) Get(string, int) (paint.ImageOp, bool) {
	a.mu.Lock()
	a.n++
	a.mu.Unlock()
	return paint.NewImageOp(image.NewRGBA(image.Rect(0, 0, 4, 4))), true
}
