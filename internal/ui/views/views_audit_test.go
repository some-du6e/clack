package views

// The rail list views audit (2026-11-09, docs/VIEWS-AUDIT.md): Slack's
// strings and rules for Threads, Drafts & sent and Activity, driven
// through the real views. Synthetic data only: internal/fakeslack's
// views fixtures and the synthetic activity feed. VIEWS_SHOTS_DIR set
// (the views-audit workflow) also saves light and dark screenshots.

import (
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// ---- Slack's strings and rules ----

// thrTime is Slack's message timestamp formatter (client-boot QvGr, the
// Threads view): no "N hours ago", weekdays within a week (the Threads
// view showed "Saturday at 2:46 PM" for a Saturday message on Monday).
func TestThreadsTimeLikeSlack(t *testing.T) {
	now := time.Date(2026, 11, 9, 15, 0, 0, 0, time.Local) // a Monday
	for _, c := range []struct {
		ago  time.Duration
		at   time.Time
		want string
	}{
		{ago: 30 * time.Second, want: "Just now"},
		{ago: 70 * time.Second, want: "1 minute ago"},
		{ago: 150 * time.Second, want: "3 minutes ago"},
		{ago: 44 * time.Minute, want: "44 minutes ago"},
		{ago: 50 * time.Minute, want: "1 hour ago"},
		{ago: 2 * time.Hour, want: "Today at " + clock(now.Add(-2*time.Hour))},
		{at: time.Date(2026, 11, 8, 16, 26, 0, 0, time.Local), want: "Yesterday at " + clock(time.Date(2026, 11, 8, 16, 26, 0, 0, time.Local))},
		{at: time.Date(2026, 11, 7, 14, 46, 0, 0, time.Local), want: "Saturday at " + clock(time.Date(2026, 11, 7, 14, 46, 0, 0, time.Local))},
		{at: time.Date(2026, 10, 30, 9, 5, 0, 0, time.Local), want: "Oct 30th at " + clock(time.Date(2026, 10, 30, 9, 5, 0, 0, time.Local))},
		{at: time.Date(2025, 12, 2, 9, 5, 0, 0, time.Local), want: "Dec 2nd, 2025 at " + clock(time.Date(2025, 12, 2, 9, 5, 0, 0, time.Local))},
	} {
		at := c.at
		if at.IsZero() {
			at = now.Add(-c.ago)
		}
		if got, _ := thrTime(at, now); got != c.want {
			t.Errorf("thrTime(%v before) = %q, want %q", now.Sub(at), got, c.want)
		}
	}
	if _, next := thrTime(now.Add(-150*time.Second), now); next != time.Minute {
		t.Errorf("3 minutes ago changes in %v, want 1m (at 3.5 minutes)", next)
	}
}

// Drafts & sent's times: DraftItem's timestamp ("Nov 7th"), and
// DraftDateScheduled's "Send {date_short_pretty} at {time}".
func TestDraftsTimesLikeSlack(t *testing.T) {
	now := time.Date(2026, 11, 9, 15, 0, 0, 0, time.Local)
	day := func(m time.Month, d, y int) time.Time { return time.Date(y, m, d, 9, 0, 0, 0, time.Local) }
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{day(11, 9, 2026), "today"},
		{day(11, 10, 2026), "tomorrow"},
		{day(11, 8, 2026), "yesterday"},
		{day(11, 12, 2026), "Nov 12"},
		{day(3, 1, 2027), "Mar 1"},
		{day(9, 1, 2027), "Sep 1, 2027"},
	} {
		if got := shortPrettyDate(c.t, now); got != c.want {
			t.Errorf("shortPrettyDate(%s) = %q, want %q", c.t.Format("2006-01-02"), got, c.want)
		}
	}
	if got := draftsListTime(day(11, 7, 2026), now); got != "Nov 7th" {
		t.Errorf("draft time = %q, want Nov 7th (Slack's Drafts tab, measured)", got)
	}
	if got := draftsListTime(day(12, 30, 2025), now); got != "Dec 30th, 2025" {
		t.Errorf("last year's draft time = %q", got)
	}
	if got := draftsListTime(now.Add(-time.Hour), now); got != clock(now.Add(-time.Hour)) {
		t.Errorf("today's draft time = %q", got)
	}
	tomorrow := time.Now().AddDate(0, 0, 1)
	if got := draftsSendText(tomorrow); got != "Send tomorrow at "+clock(tomorrow) {
		t.Errorf("draftsSendText = %q", got)
	}
}

// DeleteDraftModal's copy for each kind of draft.
func TestDraftsDeleteDialog(t *testing.T) {
	ch := draftDest{glyph: "hash", name: "views-fixtures"}
	text := func(q []richtext.Span) string {
		var b strings.Builder
		for _, s := range q {
			if s.Style&richtext.Bold != 0 {
				b.WriteString("*" + s.Text + "*")
			} else {
				b.WriteString(s.Text)
			}
		}
		return b.String()
	}
	for _, c := range []struct {
		name                string
		sched, thread, dest bool
		title, q, del       string
		saveAsDraft         bool
	}{
		{"draft", false, false, true, "Delete draft?", "Are you sure you want to delete this draft to *#views-fixtures*?", "Delete Draft", false},
		{"no destination", false, false, false, "Delete draft?", "Are you sure you want to delete this draft?", "Delete Draft", false},
		{"thread draft", false, true, true, "Delete draft?", "Are you sure you want to delete this message to a thread in *#views-fixtures*?", "Delete Draft", false},
		{"scheduled", true, false, true, "Delete scheduled message?", "Are you sure you want to delete this message to *#views-fixtures*?", "Delete", true},
		{"scheduled reply", true, true, true, "Delete scheduled message?", "Are you sure you want to delete this message to a thread in *#views-fixtures*?", "Delete", false},
	} {
		title, q, del, save := deleteDialogText(ch, c.sched, c.thread, c.dest)
		if title != c.title || text(q) != c.q || del != c.del || save != c.saveAsDraft {
			t.Errorf("%s: %q / %q / %q / save as draft %v", c.name, title, text(q), del, save)
		}
	}
}

// Activity's empty states follow getActivityEmptyStateData.
func TestActivityEmptyStates(t *testing.T) {
	v := NewActivity(&slickHost{app: actFeedApp(t), th: theme.New(false)})
	if e := v.emptyState(); e.title != "All caught up" || e.showAll || e.emoji != "white_check_mark" {
		t.Errorf("plain: %+v", e)
	}
	v.unread.Value = true
	for i := range actUnreadsEmpty {
		v.emptyPick = i
		if e := v.emptyState(); e != actUnreadsEmpty[i] || !e.showAll {
			t.Errorf("unreads pick %d: %+v", i, e)
		}
	}
	v.tabs.Sel = 2
	if e := v.emptyState(); e.title != "No unread mentions" || !e.showAll {
		t.Errorf("unread mentions: %+v", e)
	}
	v.unread.Value, v.tabs.Sel = false, 0
	v.searchOn = true
	v.query.ed.SetText("synthetic")
	if e := v.emptyState(); e.title != "Nothing found" || e.body != "Filtering from recent notifications." {
		t.Errorf("search: %+v", e)
	}
	v.searchOn = false
	v.showCleared = true
	if e := v.emptyState(); e.title != "All caught up" || !e.showAll {
		t.Errorf("cleared items: %+v", e)
	}
}

// ---- interaction through the real views ----

// vaHost is the test Host with overlays (Slack's modals) and the
// Preferences hook.
type vaHost struct {
	*slickHost
	mu       sync.Mutex
	overlays []popups.Overlay
	prefs    []string
}

func (h *vaHost) ShowOverlay(o popups.Overlay) {
	h.mu.Lock()
	h.overlays = append(h.overlays, o)
	h.mu.Unlock()
}
func (h *vaHost) OpenEmojiPicker(image.Point, func(string)) {}
func (h *vaHost) OpenPreferences(section string) {
	h.mu.Lock()
	h.prefs = append(h.prefs, section)
	h.mu.Unlock()
}

func (h *vaHost) open() []popups.Overlay {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []popups.Overlay
	for _, o := range h.overlays {
		if !o.Dismissed() {
			out = append(out, o)
		}
	}
	return out
}

// vaEnv runs frames through Gio's router.
type vaEnv struct {
	r    input.Router
	ops  op.Ops
	size image.Point
	draw func(gtx layout.Context)
}

func (e *vaEnv) frame() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Source: e.r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(e.size), Now: time.Now()}
	e.draw(gtx)
	e.r.Frame(&e.ops)
}

// until runs frames until ok holds (10 s).
func (e *vaEnv) until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
}

// click presses and releases the primary button at p, then runs a frame.
func (e *vaEnv) click(p image.Point) {
	pos := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos},
	)
	e.frame()
	e.frame()
}

// callLog records API calls.
type callLog struct {
	mu    sync.Mutex
	calls []struct {
		method string
		form   url.Values
	}
}

func (l *callLog) rec(method string, f url.Values) {
	l.mu.Lock()
	l.calls = append(l.calls, struct {
		method string
		form   url.Values
	}{method, f})
	l.mu.Unlock()
}

func (l *callLog) count(method string, match func(url.Values) bool) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, c := range l.calls {
		if c.method == method && (match == nil || match(c.form)) {
			n++
		}
	}
	return n
}

func vaActivity(t *testing.T, dark bool) (*Activity, *vaHost, *vaEnv, *callLog) {
	log := &callLog{}
	app := actFeedAppWith(t, log.rec)
	h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(dark)}}
	v := NewActivity(h)
	e := &vaEnv{size: image.Pt(1200, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the activity feed", func() bool { return len(actPage(v).Items) >= actFeedLen })
	e.frame()
	return v, h, e, log
}

// The select group: the caret opens Slack's "Select options" menu,
// Select unreads picks exactly the unread items (triage mode), and "Mark
// selected as read" marks them in one activity.markRead {reason:
// bulk_mark_read, entries} with Slack's "N items marked as read" toast.
func TestActivitySelectOptions(t *testing.T) {
	v, h, e, log := vaActivity(t, false)
	// the caret half of the 54x32 group at (16, 87+16)
	e.click(image.Pt(16+28+12, int(actHeadH+actBarTop)+16))
	if v.menu != actMenuSelect {
		t.Fatalf("caret click: menu %d, want the select menu", v.menu)
	}
	page := actPage(v)
	unread := 0
	for _, i := range v.vis {
		if i >= 0 && v.isUnread(&page.Items[i], actKey(&page.Items[i])) {
			unread++
		}
	}
	if unread == 0 {
		t.Fatal("the synthetic feed has no unread items")
	}
	v.selC[2].Click() // Select unreads
	e.frame()
	if !v.triage() || len(v.picked) != unread || v.menu != actMenuNone {
		t.Fatalf("Select unreads: triage %v, %d picked of %d unread, menu %d", v.triage(), len(v.picked), unread, v.menu)
	}
	vaShot(t, e, "activity-select", v.th.Dark)
	v.bulkRead.Click()
	e.frame()
	if v.triage() || len(v.picked) != 0 {
		t.Error("the selection didn't end after the bulk action")
	}
	e.until(t, "one bulk activity.markRead", func() bool {
		return log.count("activity.markRead", func(f url.Values) bool {
			return f.Get("reason") == "bulk_mark_read" && strings.Count(f.Get("entries"), `"key"`) == unread
		}) == 1
	})
	want := plural(unread, "item") + " marked as read"
	found := false
	for _, s := range h.toasts() {
		found = found || s == want
	}
	if !found {
		t.Errorf("toasts %q, want %q", h.toasts(), want)
	}
	for _, i := range v.vis {
		if i >= 0 && v.isUnread(&page.Items[i], actKey(&page.Items[i])) {
			t.Errorf("%s still unread after Mark selected as read", actKey(&page.Items[i]))
		}
	}
	// the checkbox selects everything shown; again unselects
	e.click(image.Pt(16+12, int(actHeadH+actBarTop)+16))
	if len(v.picked) != v.shownCount() || v.shownCount() == 0 {
		t.Fatalf("checkbox: %d picked of %d shown", len(v.picked), v.shownCount())
	}
	e.click(image.Pt(16+12, int(actHeadH+actBarTop)+16))
	if len(v.picked) != 0 || v.triage() {
		t.Fatalf("checkbox again: %d picked, triage %v", len(v.picked), v.triage())
	}
}

// The header kebab is Slack's ActivitySettingsMenu: "DMs and group DMs"
// writes activity_dms_opt_out, "Channels set to “All new posts”" the
// global notification pref, and "See all notification settings" opens
// Preferences > Notifications.
func TestActivitySettingsMenu(t *testing.T) {
	v, h, e, log := vaActivity(t, false)
	e.click(image.Pt(e.size.X-16-14, 14+14))
	if v.menu != actMenuSettings {
		t.Fatalf("kebab click: menu %d, want settings", v.menu)
	}
	vaShot(t, e, "activity-settings", false)
	v.setC[0].Click()
	e.frame()
	e.until(t, "users.prefs.set activity_dms_opt_out", func() bool {
		return log.count("users.prefs.set", func(f url.Values) bool {
			return f.Get("name") == "activity_dms_opt_out" && f.Get("value") == "true"
		}) == 1
	})
	v.menu = actMenuSettings
	v.setC[1].Click()
	e.frame()
	e.until(t, "users.prefs.setNotifications global_channels_in_activity", func() bool {
		return log.count("users.prefs.setNotifications", func(f url.Values) bool {
			return f.Get("name") == "global_channels_in_activity" && f.Get("global") == "true"
		}) == 1
	})
	v.menu = actMenuSettings
	v.setC[3].Click()
	e.frame()
	h.mu.Lock()
	got := append([]string(nil), h.prefs...)
	h.mu.Unlock()
	if len(got) != 1 || got[0] != "notifications" {
		t.Fatalf("See all notification settings opened %q", got)
	}
	for _, s := range h.toasts() {
		if strings.Contains(s, "aren't available") {
			t.Errorf("stub toast %q", s)
		}
	}
	// the filter menu with Slack's rows, for the screenshots
	v.menu = actMenuFilter
	e.frame()
	vaShot(t, e, "activity-filters", false)
}

// vaFake is a core.App on internal/fakeslack's views fixtures.
func vaFake(t *testing.T) (*core.App, *fakeslack.Server) {
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Synthetic Club"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: fakeslack.SelfID, Name: "self", RealName: "Test Self"},
		{ID: "U0FAKE00001", Name: "ada", RealName: "Ada Example"},
		{ID: "U0FAKE00002", Name: "grace", RealName: "Grace Example"},
	})
	s.PutConv(slack.Conversation{ID: fakeslack.ViewsChannel, Name: "views-fixtures", IsChannel: true, IsMember: true, NumMembers: 3})
	app := core.New(slack.New(fs.Creds()), s)
	if u, ok := s.User(fakeslack.SelfID); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	return app, fs
}

// The Threads view: the unread thread first; once it's read the read
// thread below gets Slack's "You’re up to date" divider; the mark goes
// out as subscriptions.thread.mark.
func TestThreadsUpToDateDivider(t *testing.T) {
	for _, dark := range []bool{false, true} {
		app, fs := vaFake(t)
		v := NewThreads(&slickHost{app: app, th: theme.New(dark)})
		e := &vaEnv{size: image.Pt(1000, 1100)}
		e.draw = func(gtx layout.Context) { v.Layout(gtx) }
		e.until(t, "the followed threads", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 2 })
		p, _, _ := v.data.get()
		if p.items[0].Root.TS != fakeslack.ViewsThreadUnread || v.unread(&p.items[0]) != 1 {
			t.Fatalf("first thread %s with %d unread", p.items[0].Root.TS, v.unread(&p.items[0]))
		}
		if v.firstRead(p.items) != -1 {
			t.Error("divider shown while a thread above is unread")
		}
		vaShot(t, e, "threads-unread", dark)
		v.markRead(&p.items[0])
		e.frame()
		if got := v.firstRead(p.items); got != 1 {
			t.Fatalf("divider at %d after reading the first thread, want 1", got)
		}
		e.until(t, "subscriptions.thread.mark", func() bool { return len(fs.ThreadMarks()) == 1 })
		if m := fs.ThreadMarks()[0]; m.Channel != fakeslack.ViewsChannel || m.ThreadTS != fakeslack.ViewsThreadUnread || m.TS != "1762600200.000100" {
			t.Errorf("mark %+v", m)
		}
		e.frame()
		vaShot(t, e, "threads-up-to-date", dark)
	}
}

// No followed threads: Slack's ThreadsEmptyState.
func TestThreadsEmptyState(t *testing.T) {
	for _, dark := range []bool{false, true} {
		app, fs := vaFake(t)
		fs.SetHook(func(method string, _ url.Values) (any, bool) {
			if method == "subscriptions.thread.getView" {
				return map[string]any{"ok": true, "threads": []any{}, "has_more": false}, true
			}
			return nil, false
		})
		v := NewThreads(&slickHost{app: app, th: theme.New(dark)})
		e := &vaEnv{size: image.Pt(1000, 700)}
		e.draw = func(gtx layout.Context) { v.Layout(gtx) }
		e.until(t, "the empty threads view", func() bool { _, ok, _ := v.data.get(); return ok })
		e.frame()
		vaShot(t, e, "threads-empty", dark)
	}
}

// Drafts & sent's Scheduled tab: a scheduled message's ⋮ menu holds
// "Cancel schedule and save to drafts" (drafts.update date_scheduled 0:
// it moves to Drafts) and "Delete message", which opens Slack's "Delete
// scheduled message?" dialog with Save as Draft / Cancel / Delete.
func TestDraftsScheduledActions(t *testing.T) {
	for _, dark := range []bool{false, true} {
		app, fs := vaFake(t)
		h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(dark)}}
		d := NewDrafts(h)
		e := &vaEnv{size: image.Pt(1000, 700)}
		e.draw = func(gtx layout.Context) {
			d.Layout(gtx)
			for _, o := range h.open() {
				o.Layout(gtx)
			}
		}
		e.until(t, "drafts", func() bool { return len(d.drafts) == 1 && len(d.scheduled) == 2 })
		vaShot(t, e, "drafts", dark)
		d.tabs.Sel, d.dirty = draftsTabScheduled, true
		e.frame()
		var sched core.Draft
		for _, x := range d.scheduled {
			if x.ThreadTS == "" {
				sched = x
			}
		}
		r := d.rowState("d:" + sched.ID)
		r.more.Click()
		e.frame()
		if !d.moreOpen || d.moreID != sched.ID {
			t.Fatalf("⋮: open %v id %q, want %q", d.moreOpen, d.moreID, sched.ID)
		}
		e.frame()
		vaShot(t, e, "drafts-scheduled-menu", dark)
		d.moreItems[1].Click() // Delete message
		e.frame()
		if n := len(h.open()); n != 1 {
			t.Fatalf("Delete message opened %d dialogs", n)
		}
		e.frame()
		vaShot(t, e, "drafts-delete-scheduled", dark)
		h.open()[0].(interface{ Dismiss() }).Dismiss()
		r.more.Click()
		e.frame()
		d.moreItems[0].Click() // Cancel schedule and save to drafts
		e.frame()
		e.until(t, "the message back in Drafts", func() bool {
			for _, x := range fs.LiveDrafts() {
				if x.ID == sched.ID {
					return x.DateScheduled == 0
				}
			}
			return false
		})
		e.until(t, "the Drafts tab to list it", func() bool { return len(d.drafts) == 2 && len(d.scheduled) == 1 })
		for _, s := range h.toasts() {
			if s == "Draft deleted" || s == "Scheduled message deleted" {
				t.Errorf("invented toast %q", s)
			}
		}
	}
}

// ---- screenshots ----

// vaShot saves the env's current frame as <name>-light/dark.png when
// VIEWS_SHOTS_DIR is set.
func vaShot(t *testing.T, e *vaEnv, name string, dark bool) {
	t.Helper()
	dir := os.Getenv("VIEWS_SHOTS_DIR")
	if dir == "" {
		return
	}
	mode := "light"
	if dark {
		mode = "dark"
	}
	win, err := pixeltest.NewWindow(e.size.X, e.size.Y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	defer win.Release()
	for range 3 { // images and rows settle
		e.frame()
	}
	if err := win.Frame(&e.ops); err != nil {
		t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+"-"+mode+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("saved %s", f.Name())
}

// TestViewsAuditShotsDark repeats the Activity screenshots in dark mode.
func TestViewsAuditShotsDark(t *testing.T) {
	if os.Getenv("VIEWS_SHOTS_DIR") == "" {
		t.Skip("VIEWS_SHOTS_DIR not set")
	}
	v, _, e, _ := vaActivity(t, true)
	v.pick(func(_ *core.ActivityItem, unread bool) bool { return unread }, true)
	e.frame()
	vaShot(t, e, "activity-select", true)
	v.endSelect()
	v.menu = actMenuSettings
	e.frame()
	vaShot(t, e, "activity-settings", true)
	v.menu = actMenuFilter
	e.frame()
	vaShot(t, e, "activity-filters", true)
}
