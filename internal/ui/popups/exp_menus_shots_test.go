package popups

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestExpMenusShots draws what branch exp-menus changed, light and dark,
// for the menu-keys workflow (EXP_MENUS_SHOTS_DIR, artifact menu-keys-shots): a tooltip with a
// shortcut, the bell menu's rows with the Customized tag and badges, a
// basic select under no-auto-highlight with the placeholder row, and the
// v2 Button next to the legacy one. Synthetic content only.
func TestExpMenusShots(t *testing.T) {
	dir := os.Getenv("EXP_MENUS_SHOTS_DIR")
	if dir == "" {
		t.Skip("EXP_MENUS_SHOTS_DIR not set")
	}
	h, _, _ := emBooted(t)
	sh := theme.NewShaper()
	save := func(name string, w, hgt int, dark bool, draw func(gtx layout.Context, th *theme.Theme)) {
		t.Helper()
		th := theme.New(dark)
		th.SetShapers(sh, sh)
		h.th = th
		var ops op.Ops
		paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(w, hgt)}.Op())
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Now: time.Now(),
			Constraints: layout.Exact(image.Pt(w, hgt))}
		draw(gtx, th)
		img := pixeltest.Render(t, w, hgt, &ops)
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []struct {
		name string
		dark bool
	}{{"light", false}, {"dark", true}} {
		// a tooltip under a 36px target, with its shortcut chip
		save("tooltip-"+mode.name+".png", 520, 220, mode.dark, func(gtx layout.Context, th *theme.Theme) {
			st := &TipState{Delay: time.Millisecond}
			off := op.Offset(image.Pt(220, 40)).Push(gtx.Ops)
			rrect(gtx, image.Rect(0, 0, 72, 72), 16, colorsOf(th).BgHover)
			Tooltip(gtx, th, st, true, image.Pt(72, 72), Below, "Mark unread", "U")
			st.since = gtx.Now.Add(-time.Second)
			Tooltip(gtx, th, st, true, image.Pt(72, 72), Below, "Mark unread", "U")
			off.Pop()
		})
		// the bell menu's rows: Customized on the setting in effect, then a
		// badge on a row and on a submenu row
		save("menu-tag-badge-"+mode.name+".png", 760, 640, mode.dark, func(gtx layout.Context, th *theme.Theme) {
			items := []MenuItem{
				{Header: "Notify you about…"},
				{Label: "All new posts", Subtitle: "Messages and threads you follow", Icon: NotifyIcon("everything", false), Radio: true, Checked: true,
					Tag: &MenuTag{Label: "Customized", Style: "informative"}, Do: func() {}},
				{Label: "Just mentions", Subtitle: "@you, @channel, @here", Icon: NotifyIcon("mentions_dms", false), Radio: true, Do: func() {}},
				Sep(),
				{Label: "Message has", BadgeCount: 2, BadgeStyle: "blue", BadgeHighlighted: true, Sub: []MenuItem{{Label: "A link", Do: func() {}}}},
				{Label: "Unread messages", BadgeCount: 12, Do: func() {}},
			}
			m := NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(image.Pt(40, 20)), Width: 300})
			m.Layout(gtx)
		})
		// a basic select that opened empty, under both session toggles: the
		// placeholder row first, pseudo-active with its Enter key
		save("select-"+mode.name+".png", 520, 360, mode.dark, func(gtx layout.Context, th *theme.Theme) {
			h.a.SetExperimentGroup(ExpBasicSelectPlaceholderOption, "on")
			h.a.SetExperimentGroup(ExpNoAutoHighlightSearchMultiSelect, "on")
			defer h.a.ClearExperimentGroup(ExpBasicSelectPlaceholderOption)
			defer h.a.ClearExperimentGroup(ExpNoAutoHighlightSearchMultiSelect)
			l := NewSelectList(h, image.Rect(40, 20, 400, 56), []string{"Not what I was looking for", "Missing results", "Other"}, 1,
				SelectListOpts{Placeholder: "Select an option"}, func(int) {}).(*dtTimeList)
			l.focus = true // no focus command outside a router
			l.Layout(gtx)
		})
		// the share dialog's Forward: the v2 Button over the legacy one,
		// enabled and disabled, then a secondary and an icon-only one
		save("buttons-"+mode.name+".png", 560, 260, mode.dark, func(gtx layout.Context, th *theme.Theme) {
			var cs [8]widget.Clickable
			row := func(y int, w func(gtx layout.Context) layout.Dimensions) {
				off := op.Offset(image.Pt(24, y)).Push(gtx.Ops)
				w(gtx)
				off.Pop()
			}
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(400, 100)}
			row(16, func(gtx layout.Context) layout.Dimensions {
				return buttonV2(g, th, &cs[0], BtnPrimary, theme.ButtonV2Medium, "Forward", "", false)
			})
			row(16+48, func(gtx layout.Context) layout.Dimensions {
				return button(g, th, &cs[1], BtnPrimary, BtnMedium, "Forward", "", 80, false)
			})
			row(16+96, func(gtx layout.Context) layout.Dimensions {
				return buttonV2(g, th, &cs[2], BtnDisabled, theme.ButtonV2Medium, "Forward", "", false)
			})
			row(16+144, func(gtx layout.Context) layout.Dimensions {
				return buttonV2(g, th, &cs[3], BtnOutline, theme.ButtonV2Medium, "Save Draft", "", true)
			})
			row(16+192, func(gtx layout.Context) layout.Dimensions {
				return buttonV2(g, th, &cs[4], BtnPrimary, theme.ButtonV2Medium, "", "caret-down", false)
			})
		})
	}
	// the message menu of a reply in the thread pane: Send reply to this
	// channel after the submenus
	th := theme.New(false)
	th.SetShapers(sh, sh)
	h.th = th
	reply := core.Message{Channel: emCh, TS: "1700000000.000200", User: fakeslack.SelfID, ThreadTS: "1700000000.000100"}
	save("thread-reply-menu-light.png", 760, 760, false, func(gtx layout.Context, th *theme.Theme) {
		m := NewMessageMenuOpts(h, image.Pt(700, 20), reply, MessageMenuOpts{Where: InThread})
		m.Layout(gtx)
	})
}
