package ui

import (
	"image"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// measureVisibleTabs: all tabs when they fit, else those 80px clear of
// the bar's end, at least the first.
func TestTabsLastVisible(t *testing.T) {
	cases := []struct {
		ws          []int
		right, want int
	}{
		{[]int{100, 60, 60}, 400, 2},      // 16+100+4+60+4+60 = 244: all fit
		{[]int{100, 60, 60, 200}, 400, 2}, // the 200 ends at 448: overflow; 60 ends at 244 ≤ 320
		{[]int{100, 60, 60, 200}, 300, 1}, // 300-80 = 220: the second (180) fits, the third (244) doesn't
		{[]int{300, 60}, 300, 0},          // not even the first: still the first
		{nil, 300, -1},
	}
	for _, c := range cases {
		if got := tabsLastVisible(c.ws, 16, 4, c.right, tabsOverflowReserve); got != c.want {
			t.Errorf("tabsLastVisible(%v, right %d) = %d, want %d", c.ws, c.right, got, c.want)
		}
	}
}

// The whole client on the fake Slack, in a window too narrow for
// #pins-gallery's tabs: the ones that don't fit go into the More tab's
// menu (200px, radio rows with the tabs' icons, Edit tabs after a
// separator); a click on a row opens that tab and selects More. With
// channel_tab_overflow_right_click in treatment (the account's group) a
// right-click on a row opens the tab's context menu (Hide tab for a
// built-in tab) with the row still highlighted; in control it opens
// nothing. Light and dark shots (tabs-more-*.png) with PREVIEW_SHOTS_DIR.
func TestChannelTabsMoreMenu(t *testing.T) {
	sz := fakeslack.Small
	sz.PinsFolders = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.size = image.Pt(640, 760)
	if os.Getenv("PREVIEW_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	if g := e.a.Core().Experiment("channel_tab_overflow_right_click"); g != "treatment" {
		t.Fatalf("channel_tab_overflow_right_click is %q in fakeslack, want the account's treatment", g)
	}
	ch := fakeslack.PinsChannelID
	e.openChannel(ch)
	e.until("the tabs", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] })
	e.settle(300 * time.Millisecond)
	tabs := e.a.channelTabs(ch)
	hidden := e.a.TabsHidden(ch)
	if len(hidden) == 0 {
		t.Fatalf("no tab overflowed at %v: %d tabs", e.size, len(tabs))
	}
	builtin := -1
	var want []string
	for _, i := range hidden {
		want = append(want, tabs[i].label)
		if builtinTabIDs[strings.TrimPrefix(tabs[i].key, "tab:")] && builtin < 0 {
			builtin = i
		}
	}
	// the More tab: after the visible tabs in the header
	o := e.a.primaryOrigin
	msgs := e.a.clicks.get("tab." + ch + ".messages")
	mp, ok := e.scanHover(msgs, image.Pt(o.X+40, o.Y), image.Pt(o.X+40, o.Y+200))
	if !ok {
		t.Fatal("no Messages tab in the header")
	}
	more := e.a.clicks.get("tab." + ch + ".more")
	pm, ok := e.scanHover(more, mp, image.Pt(e.size.X-10, mp.Y))
	if !ok {
		t.Fatal("no More tab in the header")
	}
	open := func() {
		e.click(pm)
		e.until("the More menu", 5*time.Second, func() bool {
			m := e.topMenu()
			return m != nil && slices.Contains(m.Labels(), want[0])
		})
		e.settle(100 * time.Millisecond)
	}
	open()
	m := e.topMenu()
	got := m.Labels()
	if !slices.Equal(got[:len(want)], want) {
		t.Errorf("More menu %q, want the hidden tabs %q first", got, want)
	}
	if got[len(got)-1] != "Edit tabs" {
		t.Errorf("More menu %q doesn't end with Edit tabs", got)
	}
	if w := m.Rect().Dx(); w != 200 {
		t.Errorf("More menu %dpx wide, want 200", w)
	}
	pfShot(e, "tabs-more")

	// treatment: a right-click on a built-in tab's row opens its menu
	if builtin < 0 {
		t.Fatalf("no built-in tab among the hidden ones %q", want)
	}
	label := tabs[builtin].label
	r := m.RowRect(label)
	if r.Empty() {
		t.Fatalf("no row %q", label)
	}
	rp := image.Pt(r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2)
	e.move(f32.Pt(float32(rp.X), float32(rp.Y)))
	e.rightClickAt(rp, time.Duration(time.Now().UnixNano()))
	e.until("the tab's context menu", 5*time.Second, func() bool {
		top := e.topMenu()
		return top != nil && top != m && slices.Contains(top.Labels(), "Hide tab")
	})
	if h := m.Highlighted(); h != label {
		t.Errorf("the right-clicked row isn't highlighted under its menu (%q)", h)
	}
	pfShot(e, "tabs-more-context")
	e.key(key.NameEscape, 0)
	e.frame()
	e.key(key.NameEscape, 0)
	e.settle(200 * time.Millisecond)

	// control: the right-click opens nothing
	e.a.Core().SetExperimentGroup("channel_tab_overflow_right_click", "control")
	open()
	m = e.topMenu()
	r = m.RowRect(label)
	rp = image.Pt(r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2)
	e.move(f32.Pt(float32(rp.X), float32(rp.Y)))
	e.rightClickAt(rp, time.Duration(time.Now().UnixNano()))
	e.settle(500 * time.Millisecond)
	if top := e.topMenu(); top != m {
		t.Errorf("control: the right-click opened %q", top.Labels())
	}
	// a click opens the tab, and More is the selected tab
	e.click(rp)
	e.settle(300 * time.Millisecond)
	if cur := e.a.chTab[ch]; cur != builtin {
		t.Errorf("the row's click left tab %d open, want %d (%s)", cur, builtin, label)
	}
	e.a.Core().ClearExperimentGroup("channel_tab_overflow_right_click")
}
