package views

// Activity row actions against Slack's web client (activity_slack.go cites
// the modules): the hover buttons per item, the item menu (⋮ and the row's
// context menu), clicks, and realtime deletes. Synthetic feed only.

import (
	"encoding/json"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
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
	remind := "Remind me about this[In 20 minutes,In 1 hour,In 3 hours,Tomorrow,Next week,Custom…]"
	// the inbox's copy_link is the message's Copy link (the Copy ▸ name /
	// link / huddle link submenu is the DMs tab's)
	copyRow := "Copy link"
	cases := []struct {
		key    string
		unread bool
		want   string
	}{
		{"k-at-user", true, "Mark as read;Add to-do;|;" + remind + ";Mute channel;" + copyRow + ";|;Clear all before…;|;Open in home"},
		{"k-thread-many", false, "Mark as unread;Add to-do;|;" + remind + ";Get notified about new replies;" + copyRow + ";|;Clear all before…;|;Open in home"},
		{"k-reaction", false, "Add to-do;|;" + remind + ";" + copyRow + ";|;Clear all before…;|;Open in home"},
		{"k-at-channel", true, "Mark as read;Add to-do;|;" + remind + ";Don’t notify for @here and @channel;" + copyRow + ";|;Clear all before…;|;Open in home"},
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

// The Dense/Detailed choice is Slack's global Activity pref: it loads from
// activity.views (prefs.density) and the header toggle writes
// activity.prefs.update {density}.
func TestActivityDensityPersists(t *testing.T) {
	actViewsDensity = "dense"
	defer func() { actViewsDensity = "compact" }()
	v, _, e, log := vaActivity(t, false)
	e.until(t, "the saved Dense layout", func() bool { return v.dense })
	v.seg[0].Click() // Detailed
	e.frame()
	if v.dense {
		t.Fatal("still Dense after picking Detailed")
	}
	e.until(t, "activity.prefs.update", func() bool {
		return log.count("activity.prefs.update", func(f url.Values) bool { return f.Get("density") == "compact" }) == 1
	})
	v.seg[1].Click() // Dense
	e.frame()
	e.until(t, "activity.prefs.update dense", func() bool {
		return log.count("activity.prefs.update", func(f url.Values) bool { return f.Get("density") == "dense" }) == 1
	})
	if !v.dense {
		t.Error("not Dense after picking Dense")
	}
}

// Dense rows are Slack's single line: 50px each, the type icon after the
// sender, the time in a 54px column at the right.
func TestActivityDenseRowLikeSlack(t *testing.T) {
	v, _, e, _ := vaActivity(t, false)
	v.dense = true
	e.frame()
	var ops op.Ops
	for _, key := range []string{"k-at-user", "k-dm", "k-reaction", "k-list-assigned"} {
		it := actItem(t, v, key)
		gtx := actFrame(&ops, image.Pt(1100, 400))
		gtx.Constraints = layout.Constraints{Min: image.Pt(1100, 0), Max: image.Pt(1100, 400)}
		if h := v.row(gtx, it, 3).Size.Y; h != int(actDenseH) {
			t.Errorf("%s: dense row %dpx, want %d", key, h, int(actDenseH))
		}
	}
}

// actRefKeys are the rows the side-by-sides compare (unread and read, one
// per kind of footer).
var actRefKeys = []string{"k-at-user", "k-thread", "k-reaction-many", "k-dm", "k-channel", "k-invite", "k-reminder", "k-mine", "k-at-channel"}

// actRefRow is one row's text as the reference page builds it
// (.github/scripts/activity_ref.py renders it with Slack's markup and
// shipping CSS next to Clack's screenshot).
type actRefRow struct {
	Key, Name, Body, Sub, Time, Loc, LocIcon, FootIcon, TypeIcon, Emoji string
	TimeNarrow                                                          string // the Dense row's (activity_time.go)
	FootPre, FootName, FootPost, FootNameIcon                           string
	FootText, LocOn, Unread, React                                      bool
	Count                                                               int
	// the rows' marks and buttons (activity_rows.go): the read row's mark
	// icon, the inviter's team (name, icon), the invite's buttons, the
	// Detailed reminder's Mark Complete
	Mark, Team string
	TeamIcon   bool
	Invite     []string
	Complete   bool
}

// actRowKeys are the fakeslack rows the second side-by-side compares: the
// marks, the invite buttons and team icon, Mark Complete, the Slackbot
// and file footers.
var actRowKeys = []string{"fa-ext-dm-invite", "fa-ext-invite", "fa-slackbot", "fa-reminder-file", "fa-reminder", "fa-reminder-done",
	"fa-at-channel", "fa-dm-reacted", "fa-channel"}

// actRefOf is a row's text for the reference page.
func actRefOf(v *Activity, it *core.ActivityItem, key string) actRefRow {
	r := v.rowFor(actKey(it))
	d := r.d
	out := actRefRow{Key: key, Name: d.name, Body: firstNonEmpty(d.body, d.prev), Sub: d.subtext, Time: d.when, TimeNarrow: d.whenNarrow,
		Loc: d.loc, LocIcon: d.locIcon, LocOn: d.locOn, FootIcon: d.footIcon, TypeIcon: d.typeIcon, FootText: d.footText,
		FootPre: d.ft.pre, FootName: d.ft.name, FootPost: d.ft.post, FootNameIcon: d.ft.nameIcon,
		Unread: v.isUnread(it, actKey(it)), Count: actUnreadCount(it), React: d.subReact, Emoji: d.footAlt,
		Team: d.team, TeamIcon: d.teamIcon != ""}
	if !out.Unread && r.markOK {
		out.Mark = r.mark.icon
	}
	for _, a := range v.inviteActions(it) {
		out.Invite = append(out.Invite, map[string]string{core.InviteActIgnore: "Ignore", core.InviteActAcceptDM: "Accept"}[a])
	}
	out.Complete = it.Type == "saved_reminder" && (it.ReminderItem == "message" || it.ReminderItem == "reminder") && v.reminderIncomplete(it)
	if it.Type == "saved_reminder" && it.Msg != nil {
		out.Body = v.preview(it.Msg)
	}
	return out
}

// TestActivitySideBySide (ACTIVITY_SHOTS_DIR) saves Clack's rows for
// actRefKeys in Dense and Detailed, light and dark, 1100px wide, and their
// text in activity-ref-rows.json for the reference page.
func TestActivitySideBySide(t *testing.T) {
	dir := os.Getenv("ACTIVITY_SHOTS_DIR")
	if dir == "" {
		t.Skip("ACTIVITY_SHOTS_DIR not set")
	}
	const w = 1100
	var rows []actRefRow
	for _, dark := range []bool{false, true} {
		for _, dense := range []bool{false, true} {
			v, _, e, _ := vaActivity(t, dark)
			v.dense = dense
			e.frame()
			var ops op.Ops
			gtx := actFrame(&ops, image.Pt(w, 4000))
			y := 0
			for i, key := range actRefKeys {
				it := actItem(t, v, key)
				func() {
					defer op.Offset(image.Pt(0, y)).Push(gtx.Ops).Pop()
					g := gtx
					g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 400)}
					y += v.row(g, it, i+3).Size.Y
				}()
				if dark || dense {
					continue
				}
				rows = append(rows, actRefOf(v, it, key))
			}
			win, err := pixeltest.NewWindow(w, y)
			if err != nil {
				t.Fatalf("headless window: %v", err)
			}
			if err := win.Frame(&ops); err != nil {
				t.Fatalf("frame: %v", err)
			}
			img := image.NewRGBA(image.Rect(0, 0, w, y))
			if err := win.Screenshot(img); err != nil {
				t.Fatalf("screenshot: %v", err)
			}
			win.Release()
			name := "clack-" + map[bool]string{false: "detailed", true: "dense"}[dense] + "-" + map[bool]string{false: "light", true: "dark"}[dark] + ".png"
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
		}
	}
	b, _ := json.MarshalIndent(rows, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "activity-ref-rows.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	// the rows' marks and buttons, on fakeslack's feed
	rows = nil
	for _, dark := range []bool{false, true} {
		for _, dense := range []bool{false, true} {
			v, _, _, _ := vaFakeActivity(t, dark)
			if dense {
				v.dense, v.rows, v.dirty = true, map[string]*actRow{}, true
			}
			var ops op.Ops
			gtx := actFrame(&ops, image.Pt(w, 4000))
			v.Layout(gtx) // rebuild the rows (marks, descriptions) for this density
			ops.Reset()
			gtx = actFrame(&ops, image.Pt(w, 4000))
			y := 0
			for i, key := range actRowKeys {
				it := actItem(t, v, key)
				func() {
					defer op.Offset(image.Pt(0, y)).Push(gtx.Ops).Pop()
					g := gtx
					g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 400)}
					y += v.row(g, it, i+3).Size.Y
				}()
				if !dark && !dense {
					rows = append(rows, actRefOf(v, it, key))
				}
			}
			actSaveOps(t, &ops, w, y, filepath.Join(dir, "clack-rows-"+map[bool]string{false: "detailed", true: "dense"}[dense]+"-"+map[bool]string{false: "light", true: "dark"}[dark]+".png"))
		}
	}
	b, _ = json.MarshalIndent(rows, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "activity-ref-rows-actions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// actSaveOps renders ops headlessly (w x h) to path.
func actSaveOps(t *testing.T, ops *op.Ops, w, h int, path string) {
	t.Helper()
	win, err := pixeltest.NewWindow(w, h)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	defer win.Release()
	if err := win.Frame(ops); err != nil {
		t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
