package ui

import (
	"context"
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
	"github.com/some-du6e/clack/internal/ui/views"
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
	// the sidebar's unread rows, the Activity inbox's feed, client.dms
	sz.Unreads, sz.Views, sz.DMsTab = true, true, true
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
	// Karim's Activity: the inbox with the entry layout revisions
	on := "on"
	e.a.Core().SetExperimentGroups(map[string]*string{"activity_inbox": &on, "activity_entry_layout_revisions_2": &on})
	var cur struct {
		ia   string
		dark bool
	}
	shot := func(name string) *image.RGBA {
		e.settle(400 * time.Millisecond)
		// the theme the shot is labelled with is the one it was taken
		// under: core's own users.prefs answer can put fakeslack's back
		if want, set := theme.ParseIATheme(cur.ia); e.a.acct.set != set || e.a.acct.t != want {
			t.Logf("%s: the account theme moved off %q; applied again", name, cur.ia)
			e.useAccountTheme(cur.ia, cur.dark)
			e.settle(400 * time.Millisecond)
			if e.a.acct.set != set || e.a.acct.t != want {
				t.Fatalf("%s: shot under %+v, not %q", name, e.a.acct.t, cur.ia)
			}
		}
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
			cur.ia, cur.dark = v.ia, dark
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
			// Activity's badges: ThemeImp, or the default theme's, which
			// the views keep as their own while it's unset
			imp, impText := c.ThemeImp, c.ThemeImpText
			if imp.A == 0 {
				def := theme.CachedFields(theme.DefaultIATheme, dark, true)
				imp, impText = def["ThemeImp"], def["ThemeImpText"]
			}
			t.Logf("%s (ia_theme %s): rail (the inspect tool's sample) bg %s ink %s; palette rail ink %s, search pill %s + %s, sidebar text %s / unread %s, Activity badges %s on %s",
				pre, e.a.acct.t.JSON(), bg, ink, contrastHex(c.ChromeText), contrastHex(c.SearchBg), contrastHex(c.ChromeHoverTint),
				contrastHex(c.SidebarText), contrastHex(c.SidebarTextUnread), contrastHex(impText), contrastHex(imp))
			e.movePtr(e.listPoint())
			// Direct messages with its rows (the Unreads filter off), one
			// opened (--selected) and the pointer on another (:hover, a
			// tile only in light mode)
			e.a.Navigate("dms")
			e.settle(300 * time.Millisecond)
			if d, ok := e.a.views[Route("dms")].(*views.DMs); ok {
				d.ShowAllDMs()
				e.until("the DMs list's rows", 15*time.Second, func() bool { ids, _, ok := d.Listed(); return ok && len(ids) > 1 })
				col := e.a.sidebar.origin.X + e.context().Dp(120)
				for y := e.a.primaryOrigin.Y + e.context().Dp(110); y < e.size.Y-80 && d.Selected() == ""; y += e.context().Dp(30) {
					e.click(image.Pt(col, y))
					e.settle(150 * time.Millisecond)
				}
				if d.Selected() == "" {
					t.Logf("%s: no DM opened (the shot shows the list only)", pre)
				}
				e.movePtr(image.Pt(col, e.a.primaryOrigin.Y+e.context().Dp(260)))
			}
			shot(pre + "-dms")
			// Activity's inbox: its rows, one opened (--selected, its item
			// beside the list or in the view's own pane) and the pointer on
			// another
			e.movePtr(e.listPoint())
			e.a.Navigate("activity")
			e.settle(800 * time.Millisecond)
			act, _ := e.a.views[Route("activity")].(*views.Activity)
			opened := func() bool { return e.a.secondary.open() || act != nil && act.OpenedItem() != "" }
			x := e.a.primaryOrigin.X + e.context().Dp(200)
			for y := e.a.primaryOrigin.Y + e.context().Dp(150); y < e.size.Y-60 && !opened(); y += e.context().Dp(30) {
				e.click(image.Pt(x, y))
				e.settle(200 * time.Millisecond)
			}
			if !opened() {
				t.Logf("%s: no Activity row opened", pre)
			}
			e.movePtr(image.Pt(x, e.a.primaryOrigin.Y+e.context().Dp(320)))
			shot(pre + "-activity")
			e.a.CloseSecondary()
		}
	}
	e.a.applyAccountPrefs(core.Prefs{})
}

// useAccountTheme puts ia_theme ia ("" none) and the mode in place and
// waits until the palette has the theme's colours. It saves ia to the
// account first (core's SetPref, users.prefs.set as Preferences writes
// it): every snapshot core builds applies the account's prefs again
// (backend.apply), and fakeslack's own ia_theme, Slack's default, came
// back with the next one. A custom colour's fields are computed off the
// UI goroutine (theme.FieldsAsync), so it applies the theme until both
// hold.
func (e *perfEnv) useAccountTheme(ia string, dark bool) {
	want, set := theme.ParseIATheme(ia)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := e.a.Core().SetPref(ctx, "ia_theme", ia)
	cancel()
	if err != nil {
		e.tb.Fatalf("users.prefs.set ia_theme: %v", err)
	}
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
