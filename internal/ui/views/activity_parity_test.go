package views

// Activity row actions against Slack's web client (activity_slack.go cites
// the modules): the hover buttons per item, the item menu (⋮ and the row's
// context menu), clicks, and realtime deletes. Synthetic feed only.

import (
	"image"
	"net/url"
	"os"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

func actItem(t *testing.T, v *Activity, key string) *core.ActivityItem {
	t.Helper()
	page := actPage(v)
	for i := range page.Items {
		if actKey(&page.Items[i]) == key {
			return &page.Items[i]
		}
	}
	t.Fatalf("%s isn't in the feed", key)
	return nil
}

func actMenuLabels(items []popups.MenuItem) string {
	var out []string
	for _, it := range items {
		if it.Separator {
			out = append(out, "|")
			continue
		}
		s := it.Label
		if len(it.Sub) > 0 {
			var sub []string
			for _, x := range it.Sub {
				sub = append(sub, x.Label)
			}
			s += "[" + strings.Join(sub, ",") + "]"
		}
		out = append(out, s)
	}
	return strings.Join(out, ";")
}

// ActivityRowContentActions: Mark as read while unread, Mark as unread
// once read (never for reactions), Clear with a key (Restore notification
// in Cleared items), More actions with a ts.
func TestActivityHoverButtonsLikeSlack(t *testing.T) {
	v, _, _, _ := vaActivity(t, false)
	name := func(bs []actBtn) string {
		var out []string
		for _, b := range bs {
			_, l := actBtnIcon(b)
			out = append(out, l)
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		key    string
		unread bool
		want   string
	}{
		{"k-at-user", true, "Mark as read,Clear,More actions"},
		{"k-at-user", false, "Mark as unread,Clear,More actions"},
		{"k-reaction", true, "Mark as read,Clear,More actions"},
		{"k-reaction", false, "Clear,More actions"},
		{"k-thread-many", false, "Mark as unread,Clear,More actions"},
	}
	for _, c := range cases {
		if got := name(v.hoverButtons(actItem(t, v, c.key), c.unread)); got != c.want {
			t.Errorf("%s unread=%v: %s, want %s", c.key, c.unread, got, c.want)
		}
	}
	v.showCleared = true
	if got := name(v.hoverButtons(actItem(t, v, "k-at-user"), false)); got != "Mark as unread,Restore notification,More actions" {
		t.Errorf("cleared items: %s", got)
	}
	v.showCleared = false
	for _, b := range []actBtn{actBtnMarkRead, actBtnMarkUnread, actBtnClear, actBtnRestore, actBtnMore} {
		icon, _ := actBtnIcon(b)
		if !icons.HasSlack(icon) {
			t.Errorf("hover icon %q has no Clack glyph", icon)
		}
	}
}

// getActivityItemMenuTemplate's rows for the activity inbox.
func TestActivityItemMenuLikeSlack(t *testing.T) {
	v, h, _, _ := vaActivity(t, false)
	remind := "Remind me about this[In 20 minutes,In 1 hour,In 3 hours,Tomorrow,Next week]"
	copyRow := "Copy[Copy name,Copy link]"
	cases := []struct {
		key    string
		unread bool
		want   string
	}{
		{"k-at-user", true, "Mark as read;Add to-do;|;" + remind + ";Mute channel;" + copyRow + ";|;Clear all before…;|;Open in home"},
		{"k-thread-many", false, "Mark as unread;Add to-do;|;" + remind + ";Get notified about new replies;" + copyRow + ";|;Clear all before…;|;Open in home"},
		{"k-reaction", false, "Add to-do;|;" + remind + ";" + copyRow + ";|;Clear all before…;|;Open in home"},
		{"k-at-channel", true, "Mark as read;Add to-do;|;" + remind + ";" + copyRow + ";|;Clear all before…;|;Open in home"},
		{"k-reminder-plain", true, "Mark as read;|;Clear all before…"},
	}
	for _, c := range cases {
		it := actItem(t, v, c.key)
		if got := actMenuLabels(v.itemMenu(h, *it, actKey(it), c.unread)); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.key, got, c.want)
		}
	}
}

// A row click selects the item, opens it in the preview pane and marks it
// read (activity.markRead with its key); Mark as unread on the hover bar
// writes activity.markUnread; Clear writes activity.archive.
func TestActivityClicksMarkRead(t *testing.T) {
	v, _, e, log := vaActivity(t, false)
	it := actItem(t, v, "k-at-user")
	key := actKey(it)
	if !v.isUnread(it, key) {
		t.Fatal("k-at-user starts read")
	}
	v.rowFor(key).click.Click()
	e.frame()
	if !v.selOK || v.selKey != key {
		t.Fatalf("click: selected %q (%v), want %s", v.selKey, v.selOK, key)
	}
	if v.isUnread(it, key) {
		t.Error("still unread after the click")
	}
	e.until(t, "activity.markRead", func() bool {
		return log.count("activity.markRead", func(f url.Values) bool { return f.Get("key") == key }) == 1
	})
	v.rowFor(key).btns[actBtnMarkUnread].Click()
	e.frame()
	e.until(t, "activity.markUnread", func() bool {
		return log.count("activity.markUnread", func(f url.Values) bool { return strings.Contains(f.Get("entries"), key) }) == 1
	})
	if !v.isUnread(it, key) {
		t.Error("Mark as unread didn't take")
	}
	v.rowFor(key).btns[actBtnClear].Click()
	e.frame()
	e.until(t, "activity.archive", func() bool {
		return log.count("activity.archive", func(f url.Values) bool { return f.Get("key") == key }) == 1
	})
}

// A right-click on a row opens the item menu at the pointer (Slack's
// ActivityItem context menu, origin "context_menu").
func TestActivityRowContextMenu(t *testing.T) {
	v, h, e, _ := vaActivity(t, false)
	// the first row sits under the header, the filter bar and the first
	// day divider (the synthetic feed is from 2023)
	p := f32.Pt(400, float32(actHeadH+actBarTop+actBarH+16+50+40))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: p},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p},
	)
	e.frame()
	e.frame()
	found := false
	for _, o := range h.open() {
		if _, ok := o.(*popups.Menu); ok {
			found = true
		}
	}
	if !found {
		t.Fatalf("no menu after a right-click (%d overlays)", len(h.open()))
	}
	if v.selOK {
		t.Error("the right-click also selected the row")
	}
}

// activity_deleted (handleActivityRTMEvent) drops the entry.
func TestActivityRealtimeDelete(t *testing.T) {
	v, _, e, _ := vaActivity(t, false)
	n := len(actPage(v).Items)
	v.notes.mu.Lock()
	v.notes.gone = append(v.notes.gone, "k-keyword")
	v.notes.mu.Unlock()
	e.frame()
	if got := len(actPage(v).Items); got != n-1 {
		t.Fatalf("%d items after activity_deleted, want %d", got, n-1)
	}
	for _, it := range actPage(v).Items {
		if it.Key == "k-keyword" {
			t.Fatal("k-keyword still listed")
		}
	}
}

// TestActivityParityShots saves the hover bar, the item menu (with the
// Remind submenu's parent highlighted) and the "Clear all before this?"
// dialog, light and dark (VIEWS_SHOTS_DIR, the activity-shots workflow).
func TestActivityParityShots(t *testing.T) {
	if os.Getenv("VIEWS_SHOTS_DIR") == "" {
		t.Skip("VIEWS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		v, h, e, _ := vaActivity(t, dark)
		e.draw = func(gtx layout.Context) {
			v.Layout(gtx)
			for _, o := range h.open() {
				o.Layout(gtx)
			}
		}
		// hover the first row: its action bar
		p := f32.Pt(400, float32(actHeadH+actBarTop+actBarH+16+50+40))
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
		e.frame()
		vaShot(t, e, "activity-hover", dark)
		// the item menu of an unread mention
		it := actItem(t, v, "k-at-user")
		v.actMenuOpen(image.Pt(700, 200), *it, actKey(it), true)
		e.frame()
		vaShot(t, e, "activity-item-menu", dark)
		// Clear all before… opens Slack's dialog
		var clearRow *popups.MenuItem
		items := v.itemMenu(h, *it, actKey(it), true)
		for i := range items {
			if items[i].Key == "clear_all_before" {
				clearRow = &items[i]
			}
		}
		if clearRow == nil {
			t.Fatal("no Clear all before… row")
		}
		for _, o := range h.open() {
			if d, ok := o.(interface{ Dismiss() }); ok {
				d.Dismiss()
			}
		}
		clearRow.Do()
		e.frame()
		vaShot(t, e, "activity-clear-before", dark)
	}
}
