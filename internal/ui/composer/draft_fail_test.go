package composer

import (
	"context"
	"image"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// draftRefusal makes fakeslack refuse drafts.create and drafts.update
// while on (Slack unreachable, as offline), counting what it refused.
type draftRefusal struct {
	on      atomic.Bool
	refused atomic.Int64
}

func refuseDrafts(fs *fakeslack.Server) *draftRefusal {
	r := &draftRefusal{}
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if r.on.Load() && (method == "drafts.create" || method == "drafts.update") {
			r.refused.Add(1)
			return map[string]any{"ok": false, "error": "fatal_error"}, true
		}
		return nil, false
	})
	return r
}

// liveDraftWith reports a fake's draft holding text.
func liveDraftWith(fs *fakeslack.Server, text string) (fakeslack.Draft, bool) {
	for _, d := range fs.LiveDrafts() {
		if strings.Contains(string(d.Blocks), text) {
			return d, true
		}
	}
	return fakeslack.Draft{}, false
}

func menuItem(items []popups.MenuItem, label string) (popups.MenuItem, bool) {
	for _, it := range items {
		if it.Label == label {
			return it, true
		}
	}
	return popups.MenuItem{}, false
}

// TestSaveAsDraftKeepsTextWhenTheSaveFails (karim-1118's review, the
// blocker): "Save as draft" with Slack unreachable, as when the hint bar
// says messages can't be sent. Slack's drafts store is local first, so
// nothing typed is ever dropped; Clack keeps the text in the composer
// until Slack has the draft, says the save failed, and saves it when
// asked again once Slack answers.
func TestSaveAsDraftKeepsTextWhenTheSaveFails(t *testing.T) {
	app, fs := fakeslackApp(t)
	old := draftSyncDelay
	draftSyncDelay = 30 * time.Millisecond
	t.Cleanup(func() { draftSyncDelay = old })
	ch := tChGen
	forget := func() { forgetDraft(ch); serverDraftIDs.Delete(draftKey(ch, "")) }
	forget()
	t.Cleanup(forget)
	app.SetExperimentGroup("draft_management_in_composer", "treatment")
	ctx := context.Background()
	if _, err := dmStash(ctx, app, ch, "an older synthetic draft"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Drafts(ctx); err != nil {
		t.Fatal(err)
	}
	refusal := refuseDrafts(fs)
	g := newFmtRigOn(t, 900, true, &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}})
	c := g.c
	until := func(what string, d time.Duration, cond func() bool) bool {
		t.Helper()
		for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			g.frame()
			if cond() {
				return true
			}
		}
		return false
	}
	g.settle()
	refusal.on.Store(true) // Slack unreachable from here
	for _, s := range strings.Split("hello there", "") {
		g.typeRune(s)
	}
	if !until("the typed text's sync", 5*time.Second, func() bool { return refusal.refused.Load() > 0 }) {
		t.Fatal("the typed text was never synced")
	}
	n := refusal.refused.Load()
	save, ok := menuItem(dmMenu(c), "Save as draft")
	if !ok || save.Disabled || save.Do == nil {
		t.Fatalf("Save as draft: %+v", save)
	}
	save.Do()
	until("the save's answer", 3*time.Second, func() bool { return refusal.refused.Load() > n && len(g.h.toasts) > 0 })
	g.settle()
	if txt := c.ed.Doc().Text(); txt != "hello there" {
		t.Fatalf("after a Save as draft Slack refused the composer holds %q; want the typed text kept (toasts %q)", txt, g.h.toasts)
	}
	if len(g.h.toasts) == 0 {
		t.Error("a Save as draft that failed said nothing")
	}
	if _, ok := liveDraftWith(fs, "hello there"); ok {
		t.Error("the refused save made a draft")
	}

	// Slack answers again: the same click keeps it as a draft of its own
	refusal.on.Store(false)
	save, _ = menuItem(dmMenu(c), "Save as draft")
	save.Do()
	if !until("the draft saved and the composer empty", 10*time.Second, func() bool {
		d, ok := liveDraftWith(fs, "hello there")
		return ok && d.IsFromComposer && c.ed.Doc().Text() == ""
	}) {
		d, _ := liveDraftWith(fs, "hello there")
		t.Fatalf("Save as draft once Slack answers: composer %q, draft %+v", c.ed.Doc().Text(), d)
	}
}

// TestPickDraftSavesTheTextFirst (the blocker's second path): picking a
// draft from the drafts menu while the composer holds text Slack doesn't
// have (its sync was refused) keeps that text: the pick waits for it to
// be saved, and with Slack unreachable it doesn't happen. Once Slack
// answers, the text is saved as a draft and the picked one takes the box.
func TestPickDraftSavesTheTextFirst(t *testing.T) {
	app, fs := fakeslackApp(t)
	old := draftSyncDelay
	draftSyncDelay = 30 * time.Millisecond
	t.Cleanup(func() { draftSyncDelay = old })
	ch := tChGen
	forget := func() { forgetDraft(ch); serverDraftIDs.Delete(draftKey(ch, "")) }
	forget()
	t.Cleanup(forget)
	app.SetExperimentGroup("draft_management_in_composer", "treatment")
	ctx := context.Background()
	if _, err := dmStash(ctx, app, ch, "the synthetic draft to pick"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Drafts(ctx); err != nil {
		t.Fatal(err)
	}
	refusal := refuseDrafts(fs)
	g := newFmtRigOn(t, 900, true, &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}})
	c := g.c
	until := func(d time.Duration, cond func() bool) bool {
		for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			g.frame()
			if cond() {
				return true
			}
		}
		return false
	}
	g.settle()
	refusal.on.Store(true)
	for _, s := range strings.Split("unsynced words", "") {
		g.typeRune(s)
	}
	if !until(5*time.Second, func() bool { return refusal.refused.Load() > 0 }) {
		t.Fatal("the typed text was never synced")
	}
	pick, ok := menuItem(dmMenu(c), "the synthetic draft to pick")
	if !ok || pick.Do == nil {
		t.Fatalf("no row for the draft to pick in %+v", dmMenu(c))
	}
	pick.Do()
	until(3*time.Second, func() bool { return len(g.h.toasts) > 0 || c.ed.Doc().Text() != "unsynced words" })
	g.settle()
	if txt := c.ed.Doc().Text(); txt != "unsynced words" {
		t.Fatalf("picking a draft over text Slack doesn't have left the composer holding %q; want that text kept", txt)
	}

	refusal.on.Store(false)
	pick, _ = menuItem(dmMenu(c), "the synthetic draft to pick")
	pick.Do()
	if !until(10*time.Second, func() bool { return c.ed.Doc().Text() == "the synthetic draft to pick" }) {
		t.Fatalf("the pick once Slack answers: the composer holds %q", c.ed.Doc().Text())
	}
	if _, ok := liveDraftWith(fs, "unsynced words"); !ok {
		t.Errorf("the text the pick replaced isn't a draft on Slack: %+v", fs.LiveDrafts())
	}
}

// TestPickThreadDraftTakesItsBroadcast (the review's MAJOR): a thread
// draft picked from the drafts menu brings its own "Also send to
// #channel", as Slack's selectStashedDraft puts the draft's broadcast in
// the composer: a draft saved without it unticks the box, one saved with
// it ticks it.
func TestPickThreadDraftTakesItsBroadcast(t *testing.T) {
	app, _ := fakeslackApp(t)
	ch, ts := tChGen, "1700000000.000100"
	key := draftKey(ch, ts)
	forget := func() { clearDraft(ch, ts); serverDraftIDs.Delete(key) }
	forget()
	t.Cleanup(forget)
	app.SetExperimentGroup("draft_management_in_composer", "treatment")
	ctx := context.Background()
	for _, d := range []core.DraftOpts{
		{Channel: ch, ThreadTS: ts, Text: "a synthetic reply, channel too", Broadcast: true, DateStashed: time.Now().UnixMilli()},
		{Channel: ch, ThreadTS: ts, Text: "a synthetic reply, thread only", DateStashed: time.Now().UnixMilli() + 1},
	} {
		if _, err := app.SaveDraft(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.Drafts(ctx); err != nil {
		t.Fatal(err)
	}
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := New(h, ch, ts)
	t.Cleanup(c.Close)
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(600, 400)}, Now: time.Now()}
		slackc.Frame(gtx)
		c.Layout(gtx)
	}
	settle := func(want string) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			frame()
			if c.ed.Doc().Text() == want {
				return
			}
		}
		t.Fatalf("the thread composer holds %q, want %q", c.ed.Doc().Text(), want)
	}
	frame()
	for _, tc := range []struct {
		pick      string
		before    bool
		broadcast bool
	}{
		{"a synthetic reply, thread only", true, false},
		{"a synthetic reply, channel too", false, true},
	} {
		c.broadcast = tc.before
		it, ok := menuItem(dmMenu(c), tc.pick)
		if !ok || it.Do == nil {
			t.Fatalf("no row %q in the thread's drafts menu", tc.pick)
		}
		it.Do()
		settle(tc.pick)
		if c.broadcast != tc.broadcast {
			t.Errorf("picked %q with the box %v: Also send to #channel is %v, want the draft's %v", tc.pick, tc.before, c.broadcast, tc.broadcast)
		}
	}
}
