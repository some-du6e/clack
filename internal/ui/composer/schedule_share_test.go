package composer

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The share dialog's ScheduleSendMenu (draftId "", the destination as
// channelId): the composer's rows without a member-specific one, and
// under updated_scheduled_sends_desktop each row says when that is for a
// DM's other member in another time zone (getScheduledChannelTimeZone-
// Details); a preset gives its time, Custom time calls custom.
func TestShareScheduleMenuLikeSlack(t *testing.T) {
	app, _ := syntheticApp(t, func(s *store.Store) {
		s.PutUsers([]slack.User{{ID: "UTOKYO1", Name: "kenji", TZ: "Asia/Tokyo", TZOffset: 9 * 3600,
			Profile: slack.Profile{DisplayName: "kenji"}}})
		s.PutConv(slack.Conversation{ID: "D0TOKYO01", IsIM: true, User: "UTOKYO1"})
	})
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	var picked time.Time
	customs := 0
	menu := func() []popups.MenuItem {
		ShareScheduleMenu(h, image.Rect(500, 500, 528, 536), "D0TOKYO01", func(at time.Time) { picked = at }, func() { customs++ })
		m, ok := h.last().(*popups.Menu)
		if !ok {
			t.Fatalf("no menu (%T)", h.last())
		}
		return m.Items()
	}
	labels := func(items []popups.MenuItem) []string {
		var out []string
		for _, it := range items {
			switch {
			case it.Separator:
				out = append(out, "-")
			case it.Header != "":
				out = append(out, "#"+it.Header)
			default:
				out = append(out, it.Label)
			}
		}
		return out
	}
	app.SetExperimentGroup("updated_scheduled_sends_desktop", "off")
	items := menu()
	now := time.Now()
	want := []string{"#Schedule message"}
	for _, r := range scheduleRows(now, uipref.Get().Time24, false, nil) {
		if r.opt == schedCustom {
			want = append(want, "-")
		}
		want = append(want, r.label)
	}
	if got := labels(items); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("rows %q, want %q", got, want)
	}
	for _, it := range items {
		if it.Subtitle != "" {
			t.Errorf("a time zone line without the experiment: %q", it.Subtitle)
		}
	}
	app.SetExperimentGroup("updated_scheduled_sends_desktop", "on")
	items = menu()
	seen := 0
	for _, it := range items {
		if it.Label != "" && it.Label != "Custom time" && it.Header == "" {
			if !strings.HasSuffix(it.Subtitle, " for kenji") {
				t.Errorf("%q: subtitle %q", it.Label, it.Subtitle)
			}
			seen++
		}
	}
	if seen == 0 {
		t.Fatalf("no preset rows: %q", labels(items))
	}
	for _, it := range items {
		if it.Label != "" && it.Header == "" && it.Do != nil {
			it.Do()
			break
		}
	}
	if !picked.After(time.Now()) {
		t.Errorf("a preset picked %v", picked)
	}
	items[len(items)-1].Do()
	if customs != 1 || items[len(items)-1].Label != "Custom time" {
		t.Errorf("Custom time: %d calls", customs)
	}
}
