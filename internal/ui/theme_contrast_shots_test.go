package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestThemeContrastShots screenshots the whole window, the top nav, the
// tab rail, the sidebar (a selected and a hovered row), the Direct
// messages list and Activity, under railShotThemes' account themes and
// Slack's default one, light and dark (theme-<theme>-<mode>-<view>.png),
// over fakeslack's Unreads fixtures (synthetic data). For each it logs
// what the inspect tool reports for the tab rail (its most common colour
// and the most common one clearly unlike it: Karim's "bg #f0c1bd, ink
// #ffffff") and the palette's rail ink, for the theme-contrast report.
// The prefs-colors workflow runs it (PREFS_COLOR_SHOTS_DIR).
func TestThemeContrastShots(t *testing.T) {
	dir := os.Getenv("PREFS_COLOR_SHOTS_DIR")
	if dir == "" {
		t.Skip("PREFS_COLOR_SHOTS_DIR unset (the prefs-colors workflow)")
	}
	sz := fakeslack.Small
	sz.Unreads = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	e := startEnv(t, fs, home)
	if e.gpu == nil {
		w, err := headless.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	shot := func(name string) *image.RGBA {
		e.settle(400 * time.Millisecond)
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	type variant struct {
		name, ia string
		modes    []bool
	}
	vs := []variant{{"default", "", []bool{false, true}}}
	for _, th := range railShotThemes {
		vs = append(vs, variant{th.name, th.ia, []bool{false, true}})
	}
	for _, v := range vs {
		for _, dark := range v.modes {
			e.useAccountTheme(v.ia, dark)
			mode := "light"
			if dark {
				mode = "dark"
			}
			pre := "theme-" + strings.ReplaceAll(v.name, " ", "-") + "-" + mode
			e.openChannel(fakeslack.JoinBannerID) // a selected sidebar row
			e.settle(300 * time.Millisecond)
			if e.a.sidebar.sbPos(fakeslack.JoinBannerOldID) >= 0 {
				e.movePtr(e.sbRowPoint(t, fakeslack.JoinBannerOldID)) // and a hovered one
			}
			img := shot(pre + "-home")
			gtx := e.context()
			rail := image.Rect(0, gtx.Dp(theme.TopNavH), gtx.Dp(theme.RailW), e.size.Y)
			bg, ink := contrastSample(img, rail)
			c := &e.a.th.C
			t.Logf("%s: rail (the inspect tool's sample) bg %s ink %s; palette rail ink %s, search pill %s + %s, sidebar text %s / unread %s",
				pre, bg, ink, contrastHex(c.ChromeText), contrastHex(c.SearchBg), contrastHex(c.ChromeHoverTint),
				contrastHex(c.SidebarText), contrastHex(c.SidebarTextUnread))
			e.movePtr(e.listPoint())
			e.a.Navigate("dms")
			shot(pre + "-dms")
			e.a.Navigate("activity")
			shot(pre + "-activity")
		}
	}
	e.a.applyAccountPrefs(core.Prefs{})
}

// useAccountTheme puts ia_theme ia ("" none) and the mode in place and
// waits until the palette has the theme's colours: core's own
// users.prefs answer can land after the test's and put fakeslack's
// (no theme) back, and a custom colour's fields are computed off the UI
// goroutine (theme.FieldsAsync), so it applies them again until both hold.
func (e *perfEnv) useAccountTheme(ia string, dark bool) {
	want, set := theme.ParseIATheme(ia)
	e.setDark(dark)
	e.until("the account theme's colours", 20*time.Second, func() bool {
		if e.a.acct.set != set || e.a.acct.t != want {
			e.a.applyAccountPrefs(core.Prefs{IATheme: ia})
		}
		if _, ok := theme.FieldsAsync(want, dark, true, 0, nil); !ok {
			return false
		}
		e.a.th.Refresh()
		e.frame()
		return e.a.acct.set == set && e.a.acct.t == want
	})
	e.settle(300 * time.Millisecond)
	if e.a.acct.set != set || e.a.acct.t != want { // reset again during the settle
		e.a.applyAccountPrefs(core.Prefs{IATheme: ia})
		e.a.th.Refresh()
		e.settle(300 * time.Millisecond)
	}
}

// contrastSample is the inspect tool's sampleColors over r: the most
// common colour and the most common one more than 60 from it (summed
// channel distance).
func contrastSample(img *image.RGBA, r image.Rectangle) (bg, ink string) {
	counts := map[color.RGBA]int{}
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			counts[img.RGBAAt(x, y)]++
		}
	}
	type cc struct {
		c color.RGBA
		n int
	}
	list := make([]cc, 0, len(counts))
	for c, n := range counts {
		list = append(list, cc{c, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		a, b := list[i].c, list[j].c
		return uint32(a.R)<<16|uint32(a.G)<<8|uint32(a.B) < uint32(b.R)<<16|uint32(b.G)<<8|uint32(b.B)
	})
	if len(list) == 0 {
		return "-", "-"
	}
	d := func(a, b color.RGBA) int {
		ab := func(x, y uint8) int {
			if x > y {
				return int(x - y)
			}
			return int(y - x)
		}
		return ab(a.R, b.R) + ab(a.G, b.G) + ab(a.B, b.B)
	}
	b := list[0].c
	bg, ink = fmt.Sprintf("#%02x%02x%02x", b.R, b.G, b.B), "-"
	for _, x := range list[1:] {
		if d(x.c, b) > 60 {
			ink = fmt.Sprintf("#%02x%02x%02x", x.c.R, x.c.G, x.c.B)
			break
		}
	}
	return bg, ink
}

func contrastHex(c color.NRGBA) string {
	if c.A == 0xff {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}
