package messages

// Later metadata on message rows (docs/MESSAGES-AUDIT.md "Saved and
// to-dos", "Add to-do on a to-do"), against Slack's CDN build
// d3de3a487b1c: MessageLabels (meLQ) picks the Later label for a saved
// message that isn't completed or archived, LaterLabel (wRj5) writes it,
// and the hover toolbar's to-do action (Yoyn) reads "Complete to-do" on a
// to-do. compact_shots_test.go's synthetic #release channel, fake Slack
// only.

import (
	"context"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// navHost records Navigate.
type navHost struct {
	coreHost
	mu   sync.Mutex
	navs []string
}

func (h *navHost) Navigate(r string) {
	h.mu.Lock()
	h.navs = append(h.navs, r)
	h.mu.Unlock()
}

func (h *navHost) navigated() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.navs...)
}

func TestLaterLabelLikeSlack(t *testing.T) {
	app, _, ts := shotsApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now := time.Now()
	type want struct {
		ok                      bool
		icon, text, link, route string
	}
	check := func(stage, key string, w want) {
		t.Helper()
		m := core.Message{Channel: tShots, TS: ts[key]}
		l, ok := laterLabelFor(app, &m, now)
		if ok != w.ok || l.icon != w.icon || l.text != w.text || l.link != w.link || l.route != w.route {
			t.Errorf("%s: %v %+v, want %+v", stage, ok, l, w)
		}
	}
	saved := want{ok: true, icon: "bookmark-filled", text: "Saved for later"}
	added := want{ok: true, icon: "check-circle-filled", text: "To-do added", link: "View to-dos", route: "later"}

	check("not saved", "a1", want{})
	if err := app.Save(ctx, tShots, ts["a1"], 0); err != nil {
		t.Fatal(err)
	}
	check("a bookmark", "a1", saved)
	// Add to-do: date_due is date_created, so "To-do added", no due text
	if err := app.AddTodo(ctx, tShots, ts["a2"], 0); err != nil {
		t.Fatal(err)
	}
	check("a to-do", "a2", added)
	// a to-do due later today or tomorrow: TodoDueLabel
	if err := app.AddTodo(ctx, tShots, ts["b1"], now.Add(3*time.Hour+30*time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	check("a to-do due in 3 hours", "b1", want{ok: true, icon: "check-circle-filled", text: "To-do due in 3 hours", link: "View to-dos", route: "later"})
	if err := app.AddTodo(ctx, tShots, ts["b2"], now.Add(-2*time.Hour-10*time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	check("an overdue to-do", "b2", want{ok: true, icon: "check-circle-filled", text: "To-do overdue by 2 hours", link: "View to-dos", route: "later"})
	// due in 5 days: formatDate "{ago}", lowercased
	if err := app.AddTodo(ctx, tShots, ts["c1"], now.Add(5*24*time.Hour+time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	check("a to-do due in 5 days", "c1", want{ok: true, icon: "check-circle-filled", text: "To-do due in 5 days", link: "View to-dos", route: "later"})
	// completed or archived: no label (isLater, primaryLabelType)
	if err := app.CompleteTodo(ctx, tShots, ts["c1"]); err != nil {
		t.Fatal(err)
	}
	check("completed", "c1", want{})
	archived := true
	if err := app.UpdateSaved(ctx, tShots, ts["a1"], core.SavedUpdate{Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	check("archived", "a1", want{})
	archived = false
	if err := app.UpdateSaved(ctx, tShots, ts["a1"], core.SavedUpdate{Archived: &archived}); err != nil {
		t.Fatal(err)
	}

	// the saved page (hq: has_starred_item): "{label} · {viewBookmarksLink}"
	app.S.Lock()
	app.S.Prefs.HasStarredItem = true
	app.S.Unlock()
	check("a bookmark, saved page", "a1", want{ok: true, icon: "bookmark-filled", text: "Saved for later", link: "View Saved", route: "saved"})
	check("a to-do, saved page", "a2", added)
	app.S.Lock()
	app.S.Prefs.HasStarredItem = false
	app.S.Unlock()

	// without later_todos a due date is no to-do: Qg's " • Due {toTimeAgo}"
	app.SetExperimentGroup("later_todos", "")
	check("no later_todos, due", "b1", want{ok: true, icon: "check-circle-filled", text: "Saved for later • Due in 3 hours"})
	check("no later_todos, bookmark", "a1", saved)
	app.ClearExperimentGroup("later_todos")
}

// The label as the row draws it: "To-do added · View to-dos" in
// sk_highlight and bold, the 20px label line, the #1d9bd11a tint, and the
// link opening the To-dos page; the toolbar's to-do button says
// "Complete to-do".
func TestLaterLabelDraws(t *testing.T) {
	app, _, ts := shotsApp(t)
	th := theme.New(false)
	host := &navHost{coreHost: coreHost{app: app, th: th}}
	v := NewChannelView(host, tShots)
	v.Preview = true
	f := frameFn(v)
	loadList(v, f)
	until(t, 5*time.Second, "the a1 row", f, func() bool { return itemHeight(v.s, ts["a1"]) > 0 })
	before := itemHeight(v.s, ts["a1"])
	if got, _ := toolbarFor(t, v.s, app, ts["a1"]); !strings.Contains(got, "|Add to-do|") {
		t.Errorf("toolbar before %q", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := app.AddTodo(ctx, tShots, ts["a1"], 0); err != nil {
		t.Fatal(err)
	}
	until(t, 5*time.Second, "the label", f, func() bool { return itemHeight(v.s, ts["a1"]) == before+20 })
	var rec labelRec
	var cols sync.Map
	labelHook = func(txt string, s textStyle) {
		rec.hook()(txt, s)
		cols.Store(txt, s.col)
	}
	f()
	labelHook = nil
	joined := strings.Join(rec.got, "|")
	if !strings.Contains(joined, "To-do added| · |View to-dos") {
		t.Fatalf("labels %q: want To-do added · View to-dos", rec.got)
	}
	c := colorsFor(th)
	for _, s := range []string{"To-do added", "View to-dos"} {
		if !rec.bld[s] {
			t.Errorf("%q isn't bold (.c-message_kit__labels--later_todo)", s)
		}
		if col, _ := cols.Load(s); col != any(c.Link) {
			t.Errorf("%q colour %v, want sk_highlight %v", s, col, c.Link)
		}
	}
	r := v.s.rows[ts["a1"]]
	if r == nil || r.bgDrawn != c.CustomTint {
		var got color.NRGBA
		if r != nil {
			got = r.bgDrawn
		}
		t.Errorf("row background %v, want #1d9bd11a", got)
	}
	if got, _ := toolbarFor(t, v.s, app, ts["a1"]); !strings.Contains(got, "|Complete to-do|") {
		t.Errorf("toolbar on a to-do %q", got)
	}
	r.laterLink.Click()
	f()
	f()
	if navs := host.navigated(); len(navs) != 1 || navs[0] != "later" {
		t.Errorf("View to-dos navigated %q, want later", navs)
	}
	// completing it takes the label away
	if err := app.CompleteTodo(ctx, tShots, ts["a1"]); err != nil {
		t.Fatal(err)
	}
	until(t, 5*time.Second, "no label", f, func() bool { return itemHeight(v.s, ts["a1"]) == before })
}
