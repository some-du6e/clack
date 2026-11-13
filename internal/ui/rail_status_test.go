package ui

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
)

// cdnEmoji makes the client load standard emoji from Slack's public static
// CDN (CLACK_EMOJI_CDN, no cookie) and everything else from the fake.
func (e *focusEnv) cdnEmoji() {
	if os.Getenv("CLACK_EMOJI_CDN") == "" {
		return
	}
	fs := e.fs
	im := imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		if !strings.HasPrefix(u, "https://a.slack-edge.com/production-standard-emoji-assets/") {
			return fs.Fetch(ctx, u)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("%s: %s", u, res.Status)
		}
		return io.ReadAll(res.Body)
	}, "", nil)
	im.SetInvalidate(func() {})
	e.a.images = im
	e.a.th.Images = im
}

// realtime connects the client's socket to the fake (dnd_updated).
func (e *focusEnv) realtime(t *testing.T) {
	c := e.a.data.core
	evs, stop := c.Subscribe()
	defer stop()
	c.SetRealtime(true)
	dl := time.After(15 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "connection" && ev.Connected {
				return
			}
		case <-dl:
			t.Fatal("no realtime connection")
		}
	}
}

// Karim, 2026-11-12: "its also missing the animation for the little
// status icon to show up". Turning focus mode on, Slack's server sets the
// automatic "In focus mode" status (dnd_profile_status, read with dnd.info
// after dnd_updated); the control strip's user button then shows the
// telescope in a tab over the avatar, which slides up over .4s
// (p-ia__nav__user__status_icon) while the buttons above move up 24px; off,
// it slides back down (--exiting, .4s) and goes.
func TestRailStatusTab(t *testing.T) {
	e := newFocusEnv(t, func(fs *fakeslack.Server) { fs.ClearSelfStatus() }) // a status emoji wins over the automatic one
	a := e.a
	e.cdnEmoji()
	c := a.Core()
	c.SetExperimentGroup("availability_prefs_section", "on")
	a.motionOverride = -1
	e.realtime(t)
	e.rest(600 * time.Millisecond) // a cleared status's exit
	tab := a.railStatus()
	if tab.on || tab.exiting {
		t.Fatal("a status tab with no status")
	}
	trig0 := a.fm().trig

	a.toggleFocusMode()
	e.until("the telescope tab", 10*time.Second, func() bool { return tab.on })
	if tab.name != "telescope" {
		t.Errorf("tab emoji %q, want telescope (eYYm k8: focus mode's automatic status)", tab.name)
	}
	if !a.Core().DNDActive(context.Background()) {
		t.Error("fakeslack didn't pause notifications")
	}
	at := tab.at
	// the keyframes at ease(x): the buttons above move up 24·e
	for _, ms := range []int{0, 100, 200, 300, 400, 600} {
		e.frameAt(at.Add(time.Duration(ms) * time.Millisecond))
		x := math.Min(1, float64(ms)/400)
		want := trig0.Min.Y - int(24*cssEase(x)+.5)
		if got := a.fm().trig.Min.Y; got != want {
			t.Errorf("enter %d ms: the focus button at y %d, want %d", ms, got, want)
		}
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e.setDark(dark)
		e.waitEmoji(tab)
		for _, ms := range []int{100, 200, 400} {
			e.frameAt(at.Add(time.Duration(ms) * time.Millisecond))
			e.frameAt(at.Add(time.Duration(ms) * time.Millisecond))
			e.shotTo(t, os.Getenv("STATUS_SHOTS_DIR"), fmt.Sprintf("rail-status-enter-%03dms-%s", ms, mode))
		}
	}
	e.setDark(false)
	e.frame()

	// off: the tab slides back down and goes after .4s
	a.toggleFocusMode()
	e.until("the exit", 10*time.Second, func() bool { return tab.exiting })
	if tab.name != "telescope" {
		t.Errorf("exiting tab lost its emoji: %q", tab.name)
	}
	out := tab.at
	e.frameAt(out.Add(200 * time.Millisecond))
	if want := trig0.Min.Y - int(24*(1-cssEase(.5))+.5); a.fm().trig.Min.Y != want {
		t.Errorf("exit 200 ms: the focus button at y %d, want %d", a.fm().trig.Min.Y, want)
	}
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.frameAt(out.Add(200 * time.Millisecond))
		e.frameAt(out.Add(200 * time.Millisecond))
		e.shotTo(t, os.Getenv("STATUS_SHOTS_DIR"), "rail-status-exit-200ms-"+map[bool]string{false: "light", true: "dark"}[dark])
	}
	e.frameAt(out.Add(401 * time.Millisecond))
	if tab.on || tab.exiting {
		t.Error("the tab outlived its exit")
	}
	if a.fm().trig != trig0 {
		t.Errorf("the strip didn't come back: %v, was %v", a.fm().trig, trig0)
	}
	e.setDark(false)

	// reduced motion: in and out at once
	a.motionOverride = 1
	a.toggleFocusMode()
	e.until("the tab (reduced motion)", 10*time.Second, func() bool { return tab.on })
	e.frameAt(tab.at)
	if want := trig0.Min.Y - 24; a.fm().trig.Min.Y != want {
		t.Errorf("reduced motion: the focus button at y %d, want %d at once", a.fm().trig.Min.Y, want)
	}
	a.toggleFocusMode()
	e.until("the tab gone (reduced motion)", 10*time.Second, func() bool { return !tab.on })
	e.frame()
	if tab.exiting || a.fm().trig != trig0 {
		t.Errorf("reduced motion: exiting %v, button %v", tab.exiting, a.fm().trig)
	}
}

// waitEmoji runs frames until the tab's emoji image is in (CDN shots).
func (e *focusEnv) waitEmoji(tab *railStatusTab) {
	if os.Getenv("STATUS_SHOTS_DIR") == "" || e.gpu == nil {
		return
	}
	url := tab.url
	if url == "" {
		url, _, _ = e.a.Core().Emoji(tab.name)
	}
	dl := time.Now().Add(10 * time.Second)
	for time.Now().Before(dl) {
		if _, ok := e.a.images.Get(url, 32); ok {
			return
		}
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	e.tb.Logf("the %s emoji (%s) didn't load", tab.name, url)
}

// The fake plays the server's part: a focus_mode_expiration_ts write
// snoozes notifications with the focus mode profile status and says so
// with dnd_updated; 0 ends it. Nothing calls dnd.setSnooze.
func TestFocusModeServerDND(t *testing.T) {
	var snoozes int
	e := newFocusEnv(t, nil)
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "dnd.setSnooze" || method == "dnd.endSnooze" {
			snoozes++
		}
		return nil, false
	})
	a := e.a
	a.toggleFocusMode()
	e.until("the snooze", 5*time.Second, func() bool {
		d := e.fs.FocusModeDND()
		return d["snooze_enabled"] == true && d["dnd_profile_status"] != nil
	})
	a.toggleFocusMode()
	e.until("the snooze ended", 5*time.Second, func() bool {
		d := e.fs.FocusModeDND()
		return d["snooze_enabled"] == false && d["dnd_profile_status"] == nil
	})
	if snoozes != 0 {
		t.Errorf("focus mode called dnd.setSnooze / endSnooze %d times (Slack writes only the pref)", snoozes)
	}
}
