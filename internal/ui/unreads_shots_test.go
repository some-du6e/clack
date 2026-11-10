package ui

// The Unreads page and the channel join banner in the whole app, against
// internal/fakeslack's Unreads fixtures (Size.Unreads, synthetic data;
// nothing talks to Slack). With UNREADS_SHOTS_DIR set (the unreads-shots
// workflow) each state is saved as a light and a dark screenshot.

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/views"
)

func unreadsAppEnv(t *testing.T, prefs map[string]any) (*perfEnv, *fakeslack.Server) {
	sz := fakeslack.Small
	sz.Unreads = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if prefs != nil {
		fs.SetPrefs(prefs)
	}
	e := startEnv(t, fs, t.TempDir())
	if dir := os.Getenv("UNREADS_SHOTS_DIR"); dir != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	return e, fs
}

// unreadsShot saves the last frame as name-light/dark.png.
func (e *perfEnv) unreadsShot(name string) {
	dir := os.Getenv("UNREADS_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.settle(300 * time.Millisecond)
	mode := "light"
	if e.a.th.Dark {
		mode = "dark"
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+"-"+mode+".png"))
	if err != nil {
		e.tb.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.tb.Fatal(err)
	}
}

func (e *perfEnv) setDark(dark bool) {
	if e.a.th.Dark != dark {
		e.a.ToggleDark()
		e.settle(100 * time.Millisecond)
	}
}

// enable_unread_view true puts Slack's Unreads link first among the
// sidebar's page links (bG), bold while a counted conversation is unread;
// false (the default) leaves it out.
func TestUnreadsSidebarLink(t *testing.T) {
	links := func(e *perfEnv) []string {
		e.a.sidebar.rebuild(e.a.model())
		var out []string
		for _, r := range e.a.sidebar.rows {
			if r.kind == sbLink {
				out = append(out, r.key)
			}
		}
		return out
	}
	e, _ := unreadsAppEnv(t, nil)
	e.settle(200 * time.Millisecond)
	if l := links(e); len(l) == 0 || l[0] == "unreads" {
		t.Fatalf("links %v: Unreads shown with enable_unread_view off", l)
	}
	e2, _ := unreadsAppEnv(t, map[string]any{"enable_unread_view": true})
	e2.until("the unread conversations", 20*time.Second, func() bool { return e2.a.model().unsuppressedUnreads() })
	if l := links(e2); len(l) < 2 || l[0] != "unreads" || l[1] != "threads" {
		t.Fatalf("links %v, want unreads first", l)
	}
	for _, dark := range []bool{false, true} {
		e2.setDark(dark)
		e2.a.Navigate("unreads")
		e2.until("the Unreads page", 20*time.Second, func() bool {
			v, ok := e2.a.views[Route("unreads")].(*views.Unreads)
			return ok && len(v.Frames()) == 2
		})
		e2.unreadsShot("unreads-page")
	}
	// Esc on the page marks the topmost conversation read
	v := e2.a.views[Route("unreads")].(*views.Unreads)
	first := v.Frames()[0]
	e2.key(key.NameEscape, 0)
	e2.until("Esc's conversations.mark", 10*time.Second, func() bool { return e2.fs.Marked(first) != "" })
}

// The join banner: megaphone.notifications.list answers
// education_channel_join_banner for #new-joiners' message_input space, so
// the banner shows over its composer (not over other channels'); the pref
// hides it; the close button slides it away and records the dismissal.
func TestJoinBanner(t *testing.T) {
	e, fs := unreadsAppEnv(t, nil)
	ch := fakeslack.JoinBannerID
	c := e.a.convoFor(ch, "")
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.openChannel(ch)
		e.until("the join banner", 20*time.Second, func() bool { return c.banner != nil && c.banner.show && c.bannerH > 0 })
		e.unreadsShot("join-banner")
	}
	asked := false
	for _, call := range fs.UnreadsCalls() {
		if call.Method == "megaphone.notifications.list" && call.Form.Get("channel_id") == ch &&
			strings.Contains(call.Form.Get("event"), `"request_spaces"`) && strings.Contains(call.Form.Get("event"), `"message_input"`) {
			asked = true
		}
	}
	if !asked {
		t.Error("no megaphone.notifications.list request_spaces for the channel")
	}
	// the pref hides it
	ca := e.a.Core()
	if err := ca.SetPref(context.Background(), "disable_channel_join_banner", true); err != nil {
		t.Fatal(err)
	}
	e.until("the banner hidden by the pref", 10*time.Second, func() bool { return c.bannerH == 0 })
	if err := ca.SetPref(context.Background(), "disable_channel_join_banner", false); err != nil {
		t.Fatal(err)
	}
	e.until("the banner back", 10*time.Second, func() bool { return c.bannerH > 0 })
	// not over a channel the server didn't pick
	other := e.a.convoFor(fakeslack.UnreadsDesignID, "")
	e.openChannel(fakeslack.UnreadsDesignID)
	e.settle(400 * time.Millisecond)
	if other.bannerH != 0 {
		t.Error("a banner over #unreads-design")
	}
	impression := false
	for _, call := range fs.UnreadsCalls() {
		impression = impression || call.Method == "megaphone.setNotificationAsSeen" && call.Form.Get("action") == core.MegaphoneImpression
	}
	if !impression {
		t.Error("leaving the channel recorded no impression")
	}
	// close: slide away, record the dismissal
	e.openChannel(ch)
	e.until("the banner again", 10*time.Second, func() bool { return c.bannerH > 0 })
	c.banner.closeBtn.Click()
	e.until("the banner gone", 10*time.Second, func() bool { return c.bannerH == 0 && c.banner.gone })
	e.until("the dismissal", 10*time.Second, func() bool {
		for _, call := range fs.UnreadsCalls() {
			if call.Method == "megaphone.setNotificationAsSeen" && call.Form.Get("action") == "dismiss" &&
				call.Form.Get("notification") == "education_channel_join_banner" && call.Form.Get("channel_id") == ch {
				return true
			}
		}
		return false
	})
	e.unreadsShot("join-banner-dismissed")
}

// The pref gate: under the auto-sort rollout disable_channel_join_banner
// hides the banner; without it channel_join_experience_enabled must be on.
func TestJoinBannerPrefGate(t *testing.T) {
	_, ca, _ := fakeApp(t, map[string]any{"disable_channel_join_banner": true})
	if !joinBannerHidden(ca) {
		t.Error("disable_channel_join_banner true didn't hide it")
	}
	ca.SetExperimentGroup("suggested_sections_auto_sort_v2", "off")
	if !joinBannerHidden(ca) {
		t.Error("without the rollout and channel_join_experience_enabled, the banner showed")
	}
	_, ca2, _ := fakeApp(t, map[string]any{"channel_join_experience_enabled": true})
	ca2.SetExperimentGroup("suggested_sections_auto_sort_v2", "off")
	if joinBannerHidden(ca2) {
		t.Error("channel_join_experience_enabled true hid it")
	}
	ca2.SetExperimentGroup("suggested_sections_auto_sort_v2", "on")
	if joinBannerHidden(ca2) {
		t.Error("the rollout with disable_channel_join_banner unset hid it")
	}
}
