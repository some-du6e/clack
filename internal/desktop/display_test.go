package desktop

import "testing"

// MOVE_WINDOW_TO_PRIMARY_SCREEN's fit and centring are Slack's rH and rG:
// the expected values are those functions' own answers, run in node from
// main.bundle.cjs 4.53.276 (win32 @806941, @807329), including JavaScript's
// Math.round on a negative half (-1456.5 is -1456).
func TestFitCenteredLikeSlack(t *testing.T) {
	for _, c := range []struct {
		w, h         float64
		work         winRect // x, y, width, height as a RECT
		x, y, fw, fh int32
	}{
		{1440, 900, rectXYWH(0, 0, 1920, 1040), 240, 70, 1440, 900},
		{2560, 1440, rectXYWH(0, 0, 1920, 1040), 36, 0, 1849, 1040},
		{3000, 1000, rectXYWH(0, 0, 1920, 1040), 0, 200, 1920, 640},
		{1440, 900, rectXYWH(-1280, 0, 1280, 984), -1280, 92, 1280, 800},
		{1001, 703, rectXYWH(-1915, -37, 1918, 1003), -1456, 113, 1001, 703},
		{800, 2000, rectXYWH(1920, 0, 2560, 1392), 2922, 0, 557, 1392},
		{668, 400, rectXYWH(100, 50, 667, 401), 100, 51, 667, 399},
	} {
		x, y, fw, fh := fitCentered(c.w, c.h, c.work)
		if x != c.x || y != c.y || fw != c.fw || fh != c.fh {
			t.Errorf("%vx%v in %+v: (%d, %d) %dx%d, want (%d, %d) %dx%d", c.w, c.h, c.work, x, y, fw, fh, c.x, c.y, c.fw, c.fh)
		}
	}
}

func rectXYWH(x, y, w, h int32) winRect { return winRect{x, y, x + w, y + h} }

// a9: priority-only mutes unless the app has priority, alarms-only always,
// off, failed and unsupported never.
func TestFocusAssistOnLikeSlack(t *testing.T) {
	for _, c := range []struct {
		mode     int
		priority bool
		want     bool
	}{
		{FocusAssistNotSupported, false, false},
		{FocusAssistFailed, false, false},
		{FocusAssistOff, false, false},
		{FocusAssistOff, true, false},
		{FocusAssistPriorityOnly, false, true},
		{FocusAssistPriorityOnly, true, false},
		{FocusAssistAlarmsOnly, false, true},
		{FocusAssistAlarmsOnly, true, true},
		{7, false, false},
	} {
		if got := focusAssistOn(c.mode, c.priority); got != c.want {
			t.Errorf("mode %d, priority %v: %v, want %v", c.mode, c.priority, got, c.want)
		}
	}
}
