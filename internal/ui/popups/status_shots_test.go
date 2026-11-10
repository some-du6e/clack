package popups

import (
	"image"
	"image/draw"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Screenshots of the status modal (its Scheduled section; Out of office
// scheduled for tomorrow with its automatic reply) and of Availability's
// custom working days, through Gio's GPU renderer, against fakeslack.
// STATUS_SHOTS_DIR selects where they go (status-shots.yml).

const stShotScale = 2

// stShotEnv is epShotEnv's host (emoji from Slack's public CDN with
// CLACK_EMOJI_CDN) over a hookable fakeslack.
func stShotEnv(t *testing.T, dark bool) (*tzHost, *fakeslack.Server) {
	t.Helper()
	h, fs := ccEnv(t)
	e := epShotEnv(t, dark)
	h.th, h.im = e.th, e.im
	return h, fs
}

func stHookShots(fs *fakeslack.Server, days bool) {
	now := time.Now().Unix()
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		switch method {
		case "users.customStatus.list":
			return map[string]any{"ok": true, "statuses": []map[string]any{
				{"id": "S1", "emoji": ":no_entry:", "text": "Out of office", "text_canonical": "Out of office", "duration": "custom",
					"date_scheduled": now + 26*3600, "date_expire": now + 74*3600},
				{"id": "S2", "emoji": ":airplane:", "text": "Flying to the hackathon", "duration": "custom",
					"date_scheduled": now + 3*3600, "date_expire": now + 5*3600},
			}}, true
		case "users.profile.get":
			return map[string]any{"ok": true, "profile": map[string]any{}}, true
		case "users.prefs.get":
			if !days {
				return nil, false
			}
			return map[string]any{"ok": true, "prefs": map[string]any{
				"dnd_enabled": true, "dnd_days": "custom", "dnd_start_hour": "17:00", "dnd_end_hour": "09:00",
				"dnd_enabled_monday": "partial", "dnd_before_monday": "08:00", "dnd_after_monday": "16:30",
				"dnd_enabled_tuesday": "partial", "dnd_before_tuesday": "09:00", "dnd_after_tuesday": "17:00",
				"dnd_enabled_wednesday": "partial", "dnd_before_wednesday": "09:00", "dnd_after_wednesday": "17:00",
				"dnd_enabled_thursday": "partial", "dnd_before_thursday": "09:00", "dnd_after_thursday": "17:00",
				"dnd_enabled_friday": "partial", "dnd_before_friday": "09:00", "dnd_after_friday": "13:00",
				"dnd_enabled_saturday": "all_day", "dnd_enabled_sunday": "disabled", "huddles_set_status": true,
			}}, true
		}
		return nil, false
	})
}

func stRender(t *testing.T, h *tzHost, win image.Point, draw func(gtx layout.Context)) *image.RGBA {
	t.Helper()
	var ops op.Ops
	frame := func() {
		ops.Reset()
		paint.FillShape(&ops, colorsOf(h.th).Bg, clip.Rect{Max: win}.Op())
		gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: stShotScale, PxPerSp: stShotScale}, Constraints: layout.Exact(win)}
		draw(gtx)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		frame()
		Pump()
		time.Sleep(40 * time.Millisecond)
		if time.Now().After(deadline.Add(-8 * time.Second)) {
			break
		}
	}
	frame()
	return pixeltest.Render(t, win.X, win.Y, &ops)
}

func stCrop(img *image.RGBA, r image.Rectangle) *image.RGBA {
	r = r.Intersect(img.Bounds())
	out := image.NewRGBA(image.Rectangle{Max: r.Size()})
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	return out
}

func TestStatusShots(t *testing.T) {
	dir := os.Getenv("STATUS_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run("scheduled-"+mode, func(t *testing.T) {
			h, fs := stShotEnv(t, dark)
			stHookShots(fs, false)
			s := stShotStatus(t, h, false)
			win := image.Pt(1000*stShotScale, 760*stShotScale)
			img := stRender(t, h, win, func(gtx layout.Context) { s.modal.Layout(gtx) })
			epSaveShot(t, dir, "status-scheduled-"+mode, stCrop(img, s.modal.rect.Inset(-8*stShotScale)))
		})
		t.Run("ooo-later-"+mode, func(t *testing.T) {
			h, fs := stShotEnv(t, dark)
			stHookShots(fs, false)
			s := stShotStatus(t, h, true)
			win := image.Pt(1000*stShotScale, 900*stShotScale)
			img := stRender(t, h, win, func(gtx layout.Context) { s.modal.Layout(gtx) })
			epSaveShot(t, dir, "status-ooo-later-"+mode, stCrop(img, s.modal.rect.Inset(-8*stShotScale)))
		})
	}
}

// stShotStatus opens the modal over the hooked fakeslack; ooo picks Out of
// office starting tomorrow at 9:00 AM.
func stShotStatus(t *testing.T, h *tzHost, ooo bool) *actStatus {
	t.Helper()
	s := actNewStatusState(h, false)
	ccUntil(t, s.modal, "the scheduled list", func() bool { return len(s.sched) == 2 })
	if ooo {
		s.pickPreset("no_entry", "Out of office", actOOOCanonical, actDurCustom)
		start := time.Now().AddDate(0, 0, 1)
		start = time.Date(start.Year(), start.Month(), start.Day(), 9, 0, 0, 0, start.Location())
		end := start.AddDate(0, 0, 3).Add(8 * time.Hour)
		s.sDay.Day, s.sTime.Mins = dtDay(start), dtMins(start)
		s.eDay.Day, s.eTime.Mins = dtDay(end), dtMins(end)
		s.eDay.Min = s.sDay.Day
		s.rangeChanged()
	}
	return s
}

func TestAvailabilityCustomDaysShots(t *testing.T) {
	dir := os.Getenv("STATUS_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			h, fs := stShotEnv(t, dark)
			stHookShots(fs, true)
			pg := NewAvailabilityPage(h)
			ccUntil(t, avPageOverlay{pg}, "the prefs", func() bool { return pg.p.loaded })
			win := image.Pt(560*stShotScale, 1020*stShotScale)
			img := stRender(t, h, win, func(gtx layout.Context) {
				defer op.Offset(image.Pt(16*stShotScale, 0)).Push(gtx.Ops).Pop()
				gtx.Constraints = layout.Exact(image.Pt(528*stShotScale, 1000*stShotScale))
				pg.Layout(gtx)
			})
			epSaveShot(t, dir, "availability-custom-days-"+mode, img)
			if pg.p.hr.Days != "custom" || strconv.Itoa(pg.p.hr.avNoneCount()) != "1" {
				t.Errorf("prefs %+v", pg.p.hr)
			}
		})
	}
}
