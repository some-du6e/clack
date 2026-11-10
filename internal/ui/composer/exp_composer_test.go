package composer

import (
	"image"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The exp-composer branch's ports (docs/EXPERIMENTS.md, Composer and
// autocomplete; docs/COMPOSER-AUDIT.md). Synthetic data and
// internal/fakeslack only.

// ovHost records the overlays the composer opens.
type ovHost struct {
	*slickHost
	overlays []popups.Overlay
	toasts   []string
}

func (h *ovHost) ShowOverlay(o popups.Overlay) { h.overlays = append(h.overlays, o) }
func (h *ovHost) Toast(s string)               { h.toasts = append(h.toasts, s) }

func (h *ovHost) last() popups.Overlay {
	if len(h.overlays) == 0 {
		return nil
	}
	return h.overlays[len(h.overlays)-1]
}

// layOverlay lays o out once in a 1200×800 headless frame.
func layOverlay(o popups.Overlay) {
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1200, 800)), Now: time.Now()}
	o.Layout(gtx)
}

func fakeslackApp(t *testing.T) (*core.App, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	app := core.New(slack.New(fs.Creds()), store.New(""))
	app.ServeOffline()
	return app, fs
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	dl := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(dl) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// autocomplete_affinity_ml_model (client-boot y+lP prepSuggestions): on,
// the sort gets userAffinityScore and channelPriority90dScore from
// search.autocomplete.offlineFeatures; off or unassigned both stay 0.
func TestAffinityFeaturesFollowExperiment(t *testing.T) {
	app, fs := fakeslackApp(t)
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "search.autocomplete.offlineFeatures" {
			return map[string]any{"ok": true,
				"user_affinity":        map[string]any{"U0AFFINIT2": 3.5},
				"channel_priority_90d": map[string]any{"C0PRIORIT2": 1.25}}, true
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	api := coreAPI{app}
	app.AutocompleteAffinity()
	waitUntil(t, "the offline features", func() bool { u, _ := app.AutocompleteAffinity(); return len(u) > 0 })

	users := []core.User{
		{ID: "U0AFFINIT1", Handle: "alexa", RealName: "Alex A"},
		{ID: "U0AFFINIT2", Handle: "alexb", RealName: "Alex B"},
	}
	rank := func() (first string, aff float64, prio float64) {
		r := rankCtxFrom(api, app.Snapshot(), "channel")
		s := memberSources{users: users, rk: r}
		items := s.rankMembers("alex", false)
		if len(items) == 0 {
			t.Fatal("no members ranked")
		}
		c := r.memberCand(users[1], s, false)
		ch := channelSources{rk: r, hits: []core.ChannelHit{{ID: "C0PRIORIT2", Name: "planning", Kind: "channel", IsMember: true}}}
		ch.rankChannels("plan")
		return items[0].ID, c.Affinity, r.priority90D["C0PRIORIT2"]
	}

	app.SetExperimentGroup("autocomplete_affinity_ml_model", "on")
	first, aff, prio := rank()
	if aff != 3.5 || prio != 1.25 {
		t.Fatalf("on: affinity %v priority %v", aff, prio)
	}
	if first != "U0AFFINIT2" {
		t.Errorf("on: %s ranks first, want the affinity member", first)
	}
	for _, g := range []string{"off", ""} {
		app.SetExperimentGroup("autocomplete_affinity_ml_model", g)
		first, aff, prio := rank()
		if aff != 0 || prio != 0 {
			t.Errorf("group %q: affinity %v priority %v", g, aff, prio)
		}
		if first != "U0AFFINIT1" {
			t.Errorf("group %q: %s ranks first, want the name order", g, first)
		}
	}
}

// ScheduleMessageDialog (z1vG): the date field's labels, the time
// select's range, the clamp, and the dialog drawn with its overlays.
func TestScheduleDialogLikeSlack(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 7, 30, 0, time.Local)
	if got := scheduleDateLabel(now, now, false); got != "Today" {
		t.Errorf("today %q", got)
	}
	if got := scheduleDateLabel(now.AddDate(0, 0, 1), now, false); got != "Tomorrow" {
		t.Errorf("tomorrow %q", got)
	}
	if got := scheduleDateLabel(now.AddDate(0, 0, 2), now, false); got != "November 12th, 2026" {
		t.Errorf("later %q", got)
	}
	if got := scheduleDateLabel(now, now, true); got != "November 10th, 2026" {
		t.Errorf("open %q", got)
	}
	// minimum 14:12 today: the 15-minute list starts at 14:15
	from, to := scheduleTimeRange(dayOf(now), now)
	if from != 14*60+12 || to != 23*60+59 {
		t.Errorf("today's range %d..%d", from, to)
	}
	from, to = scheduleTimeRange(dayOf(now.AddDate(0, 0, 120)), now)
	if from != 0 || to != 14*60+7 {
		t.Errorf("the last day's range %d..%d", from, to)
	}
	from, to = scheduleTimeRange(dayOf(now.AddDate(0, 0, 3)), now)
	if from != 0 || to != 23*60+59 {
		t.Errorf("another day's range %d..%d", from, to)
	}

	old := core.TestSystemZone
	core.TestSystemZone = "America/Chicago"
	t.Cleanup(func() { core.TestSystemZone = old })
	if got := scheduleZoneLabel(); got != "Central Time (US and Canada)" {
		t.Errorf("zone label %q", got)
	}
	core.TestSystemZone = "Mars/Olympus_Mons" // not listed: the detector gives up
	if got := scheduleZoneLabel(); got != "Pacific Time (US and Canada)" {
		t.Errorf("fallback zone label %q", got)
	}

	app, _ := syntheticApp(t, func(*store.Store) {})
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	var picked time.Time
	start := schedCustomDefault(time.Now(), nil)
	p := openSchedulePicker(h, start, nil, func(t time.Time) { picked = t })
	if h.last() != p.m {
		t.Fatal("the dialog wasn't shown")
	}
	layOverlay(p.m)
	if !p.valid() {
		t.Fatalf("the default %v isn't schedulable", p.at)
	}
	p.set(time.Now().Add(-time.Hour)) // before the minimum: clamped to it
	if !p.valid() || p.at.Before(time.Now()) {
		t.Fatalf("not clamped to the minimum: %v", p.at)
	}
	p.set(time.Now().AddDate(1, 0, 0)) // after the maximum: clamped
	if !p.valid() || p.at.After(time.Now().AddDate(0, 0, 121)) {
		t.Fatalf("not clamped to the maximum: %v", p.at)
	}
	p.at = start
	p.dateBtn.Click()
	layOverlay(p.m)
	if !overlayOpen(p.dateOv) || h.last() != p.dateOv {
		t.Fatal("the date field didn't open the calendar")
	}
	layOverlay(p.dateOv)
	p.dateOv.(interface{ Dismiss() }).Dismiss()
	p.timeBtn.Click()
	layOverlay(p.m)
	if !overlayOpen(p.timeOv) || h.last() != p.timeOv {
		t.Fatal("the time field didn't open the list")
	}
	layOverlay(p.timeOv)
	_ = picked

	// a DM with someone in another time zone: "{memberTime} for {memberName}"
	loc := time.FixedZone("X", 9*3600)
	m := &schedMemberInfo{name: "Alice", loc: loc, offset: true}
	at := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
	if got := scheduleZoneDetails(at, m, false); got != "12:00 AM for Alice" {
		t.Errorf("details %q", got)
	}
	m.sameTZ = true
	if got := scheduleZoneDetails(at, m, false); got != "" {
		t.Errorf("same zone details %q", got)
	}
}

// uploadLog records files.getUploadURL forms (fakeslack still answers).
func uploadLog(t *testing.T, fs *fakeslack.Server) func() []url.Values {
	t.Helper()
	var mu sync.Mutex
	var got []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "files.getUploadURL" {
			mu.Lock()
			got = append(got, f)
			mu.Unlock()
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	return func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return append([]url.Values(nil), got...)
	}
}

// The snippet dialog (wc+S): Ctrl+Shift+Enter moves the message into it
// (quotes straightened) and empties the composer; Create Snippet uploads
// "Untitled" with snippet_type and shares it to the conversation with
// the message; unshared it is only uploaded.
func TestSnippetDialogLikeSlack(t *testing.T) {
	app, fs := fakeslackApp(t)
	stubTyping(t)
	uploads := uploadLog(t, fs)
	ch := fs.ChannelIDs()[0]
	clearDraft(ch, "")
	t.Cleanup(func() { clearDraft(ch, "") })
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := New(h, ch, "")
	t.Cleanup(c.Close)
	d := NewDoc()
	d.Insert(0, "say “hi” to ‘everyone’", Attr{})
	c.ed.SetDoc(d)
	c.openSnippet(true)
	sd, _ := h.last().(*popups.Modal)
	if sd == nil {
		t.Fatal("no dialog")
	}
	if !c.ed.Empty() {
		t.Fatal("Ctrl+Shift+Enter left the message in the composer")
	}
	dlg := lastSnippet
	if dlg == nil || dlg.code.ed.Text() != `say "hi" to 'everyone'` || dlg.typ != "auto" || !dlg.shareOn || dlg.ch != ch {
		t.Fatalf("dialog state: %+v", dlg)
	}
	layOverlay(dlg.m)
	if dlg.goDisabled() {
		t.Fatal("Create Snippet off with content")
	}
	dlg.msg.SetText("here it is")
	if !dlg.submit() {
		t.Fatal("submit refused")
	}
	waitUntil(t, "the share", func() bool { return len(fs.Shared()) > 0 })
	ups := uploads()
	if len(ups) != 1 || ups[0].Get("filename") != "Untitled" || ups[0].Get("snippet_type") != "auto" {
		t.Fatalf("getUploadURL %v", ups)
	}
	sh := fs.Shared()[0]
	if sh.Channel != ch || sh.Comment != "here it is" {
		t.Fatalf("shared %+v", sh)
	}

	// named, typed, not shared: uploaded only
	dlg = openSnippetDialog(c, snippetSpec{text: "fmt.Println(1)", ch: ch})
	dlg.title.SetText("main.go")
	dlg.typ = "go"
	dlg.shareOn = false
	if !dlg.submit() {
		t.Fatal("unshared submit refused")
	}
	waitUntil(t, "the second upload", func() bool { return len(uploads()) == 2 })
	time.Sleep(200 * time.Millisecond)
	if u := uploads()[1]; u.Get("filename") != "main.go" || u.Get("snippet_type") != "go" {
		t.Fatalf("unshared upload %v", u)
	}
	if n := len(fs.Shared()); n != 1 {
		t.Fatalf("an unshared snippet was shared (%d shares)", n)
	}

	// isGoButtonDisabled: no content, or a title over 255 characters
	dlg = openSnippetDialog(c, snippetSpec{ch: ch})
	if !dlg.goDisabled() {
		t.Error("Create Snippet on without content")
	}
	dlg.code.ed.SetText("x")
	dlg.title.SetText(strings.Repeat("a", 256))
	if !dlg.goDisabled() {
		t.Error("Create Snippet on with a 256-character title")
	}
	// the speed bump: changed content asks first
	n := len(h.overlays)
	if dlg.closeGuard() || len(h.overlays) != n+1 {
		t.Error("closing with changed content didn't ask")
	}
	dlg = openSnippetDialog(c, snippetSpec{text: "same", ch: ch})
	if !dlg.closeGuard() {
		t.Error("closing unchanged content asked")
	}
}

// getFileTypeOptions: Auto Detect Type, Plain Text, then the types by
// label; an externally shared channel isn't offered five of them.
func TestSnippetTypeOptions(t *testing.T) {
	opts := snippetTypeOptions(false)
	if len(opts) != 2+len(snippetTypes) || opts[0].key != "auto" || opts[1].key != "text" {
		t.Fatalf("head %v (%d)", opts[:2], len(opts))
	}
	for i := 3; i < len(opts); i++ {
		if strings.ToLower(opts[i-1].label) > strings.ToLower(opts[i].label) {
			t.Fatalf("not sorted: %q before %q", opts[i-1].label, opts[i].label)
		}
	}
	ext := snippetTypeOptions(true)
	if len(ext) != len(opts)-5 {
		t.Fatalf("external: %d options, want %d", len(ext), len(opts)-5)
	}
	for _, o := range ext {
		if snippetDisallowedExt[o.key] {
			t.Errorf("external offers %s", o.key)
		}
	}
	if snippetTypeLabel("javascript") != "JavaScript" || snippetTypeLabel("auto") != "Auto Detect Type" {
		t.Error("labels")
	}
}

// The paste-limit dialog's Create a Snippet takes the refused paste and
// keeps the composer's text to put back on cancel.
func TestPasteLimitCreatesSnippet(t *testing.T) {
	app, _ := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := freshComposer(t, h)
	d := NewDoc()
	d.Insert(0, "draft", Attr{})
	c.ed.SetDoc(d)
	c.ed.paste(strings.Repeat("z", pasteMaxLength+1))
	if c.ed.RefusedPaste() == "" {
		t.Fatal("the refused paste wasn't kept")
	}
	c.pasteLimitDialog(c.ed.RefusedPaste())
	if _, ok := h.last().(*popups.Modal); !ok {
		t.Fatal("no paste-limit dialog")
	}
	c.openSnippetFromPaste(c.ed.RefusedPaste())
	dlg := lastSnippet
	if dlg.code.ed.Text() != strings.Repeat("z", pasteMaxLength+1) || c.ed.Doc().Text() != "draft" {
		t.Fatal("the paste didn't become the snippet, or the composer changed")
	}
	c.ed.SetDoc(NewDoc())
	dlg.cancel()
	if c.ed.Doc().Text() != "draft" {
		t.Fatal("cancel didn't put the composer's text back")
	}
}

// useComposerIAFormatter's rows Clack draws, in Slack's order.
func TestPlusMenuRows(t *testing.T) {
	app, _ := syntheticApp(t, func(*store.Store) {})
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	a := newAttachments(h)
	items := a.plusMenuItems(plusMenu{ch: tChGen, snippet: func() {}})
	if len(items) != 2 || items[0].Label != "Text snippet" || items[0].Icon != "text-snippet" ||
		items[1].Label != "Upload from your computer" || items[1].Icon != "laptop" {
		t.Fatalf("rows %+v", items)
	}
	for _, it := range items {
		if it.Label == "Paste image from clipboard" {
			t.Error("Slack's + menu has no Paste image from clipboard")
		}
	}
	if runtime.GOOS != "darwin" && (items[0].Hint != "Ctrl+Shift+Enter" || items[1].Hint != "Ctrl+O") {
		t.Errorf("hints %q %q", items[0].Hint, items[1].Hint)
	}
}
