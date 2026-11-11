package ui

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

func notifyLabels(items []popups.MenuItem) string {
	var out []string
	for _, it := range items {
		switch {
		case it.Separator:
			out = append(out, "---")
		case it.Header != "":
			out = append(out, "["+it.Header+"]")
		default:
			out = append(out, it.Label)
		}
	}
	return strings.Join(out, " | ")
}

func notifyPick(t *testing.T, items []popups.MenuItem, label string) popups.MenuItem {
	t.Helper()
	for _, it := range items {
		if it.Label == label {
			return it
		}
		for _, s := range it.Sub {
			if s.Label == label {
				return s
			}
		}
	}
	t.Fatalf("no %q in %s", label, notifyLabels(items))
	return popups.MenuItem{}
}

// temp_mute on: the bell's More options › Temporarily mute… writes
// temp_mute_expiration_ts, the channel counts as muted until then, and
// Slack's "#x muted for 1 hour." toast's Undo writes muted false
// (unsynced). Off: no More options (Advanced options and Edit default
// preferences instead), and the expiration mutes nothing.
func TestTempMuteFollowsExperiment(t *testing.T) {
	e, fs := filterEnv(t, nil)
	a := e.a
	c := a.Core()
	ch := fakeslackMemberChannel(e)
	var mu sync.Mutex
	var writes []url.Values
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "users.prefs.setNotifications" && form.Get("global") != "true" {
			mu.Lock()
			writes = append(writes, form)
			mu.Unlock()
		}
		return nil, false
	})
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	if !c.TempMuteOn() {
		t.Fatal("temp_mute isn't on for the fake account")
	}
	items := popups.ChannelNotifyItems(a, ch)
	if got, want := notifyLabels(items), "[Notify you about…] | All new posts | Just mentions | Mute and hide | --- | More options"; got != want {
		t.Errorf("temp_mute on:\n got %s\nwant %s", got, want)
	}
	a.openNotifPrefs(ch)
	e.settle(150 * time.Millisecond)
	e.shot("bell-temp-mute-on")
	a.dismissTop()
	e.frame()
	more := notifyPick(t, items, "More options")
	if got, want := notifyLabels(more.Sub), "[Temporarily mute…] | Until tomorrow | 1 hour | --- | Edit default preferences | Advanced settings"; got != want {
		t.Errorf("More options:\n got %s\nwant %s", got, want)
	}

	start := time.Now().Unix()
	notifyPick(t, popups.ChannelNotifyItems(a, ch), "1 hour").Do()
	e.until("temp mute written", 10*time.Second, func() bool {
		exp := c.NotifyPrefs(ch).TempMuteExp
		return exp >= start+3590 && exp <= start+3700
	})
	e.until("muted by the temporary mute", 10*time.Second, func() bool {
		cv := a.model().conv(ch)
		return cv != nil && cv.Muted
	})
	name := "#" + a.model().conv(ch).Name
	e.until("toast", 5*time.Second, func() bool { return a.toast.text != "" })
	if got, want := a.toast.text, name+" muted for 1 hour."; got != want {
		t.Errorf("toast %q, want %q", got, want)
	}
	if a.toast.undo == nil {
		t.Fatal("the toast has no Undo")
	}
	if it := notifyPick(t, popups.ChannelNotifyItems(a, ch), "1 hour"); !it.Checked {
		t.Error("1 hour isn't checked while the temporary mute runs")
	}
	mu.Lock()
	n := len(writes)
	mu.Unlock()
	a.undoToast()
	e.until("undo write", 10*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(writes) > n
	})
	mu.Lock()
	w := writes[len(writes)-1]
	mu.Unlock()
	var ps []map[string]any
	_ = json.Unmarshal([]byte(w.Get("prefs")), &ps)
	if w.Get("channel_ids") != ch || w.Get("sync") != "false" || len(ps) != 1 || ps[0]["name"] != "muted" || ps[0]["value"] != "false" {
		t.Errorf("Undo wrote %v", w)
	}

	// temp_mute off: the expiration doesn't mute, and the menu has no
	// temporary mutes
	c.SetExperimentGroup(core.ExpTempMute, "off")
	e.until("unmuted without temp_mute", 10*time.Second, func() bool {
		cv := a.model().conv(ch)
		return cv != nil && !cv.Muted
	})
	items = popups.ChannelNotifyItems(a, ch)
	if got, want := notifyLabels(items), "[Notify you about…] | All new posts | Just mentions | Mute and hide | --- | Advanced options | --- | Edit default preferences"; got != want {
		t.Errorf("temp_mute off:\n got %s\nwant %s", got, want)
	}
	a.openNotifPrefs(ch)
	e.settle(150 * time.Millisecond)
	e.shot("bell-temp-mute-off")
	a.dismissTop()
	e.frame()
	adv := notifyPick(t, items, "Advanced options")
	if adv.Icon != "filter-sliders" {
		t.Errorf("Advanced options icon %q, want Slack's filters glyph", adv.Icon)
	}
	adv.Do()
	e.settle(150 * time.Millisecond)
	if _, ok := topOverlay[*popups.ChannelNotifyModal](a); !ok {
		t.Error("Advanced options opened no modal")
	}
	c.ClearExperimentGroup(core.ExpTempMute)
	e.until("muted again", 10*time.Second, func() bool {
		cv := a.model().conv(ch)
		return cv != nil && cv.Muted
	})
}
