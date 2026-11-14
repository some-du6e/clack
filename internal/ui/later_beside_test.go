package ui

// Work package W42 (docs/GAPS-2026-11-14.md gap 11) in the whole window,
// on internal/fakeslack's thread header fixtures and a generated channel
// (synthetic; nothing talks to Slack): a To-dos row opens its message in
// the secondary column beside the list, the route stays To-dos and the
// row is selected; a reply's thread opens at the reply and highlights it
// (jumpToThreadReply), which View to-dos from the reply's own conversation
// doesn't; the column is 55% of the region beside the list
// (isSavedListLayoutClamped). With VIEWS_SHOTS_DIR set (the views-audit
// workflow) the layouts are saved light and dark.

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/views"
)

// b2oS isSavedListLayoutClamped in secondaryWidth, at 1px per dp: beside
// the To-dos list (with eU) or the Saved page (under hq) the column is
// 55% of the region and stops where the list would keep less than 320;
// with the To-dos list in the primary the nested squish gives way at 320.
func TestSavedListColumnLikeSlack(t *testing.T) {
	gtx := layout.Context{}
	gtx.Metric.PxPerDp = 1
	nested := map[string]string{"nested_secondary_view": "on", "secondary_view_default_width": "on", "later_todos": "on"}
	cases := []struct {
		name   string
		groups map[string]string
		route  Route
		saved  bool // the has_starred_item pref
		panel  int
		manual int
		want   int
	}{
		{"To-dos: 55% of the region", nested, "later", false, 1366, 0, 751},
		// a dragged width stops where the list keeps 320
		{"To-dos: manual", nested, "later", false, 1366, 1200, 1046},
		// a narrow region: 300 at least, then the squish down to the list's 320 floor
		{"To-dos: squish", nested, "later", false, 600, 0, 280},
		// without later_todos the page isn't the LaterList (kl02 eU): 40%, floor 300
		{"To-dos: no later_todos", map[string]string{"nested_secondary_view": "on", "secondary_view_default_width": "on", "later_todos": "control"}, "later", false, 1366, 0, 546},
		// without the nested view eU is off: 30% of the region
		{"To-dos: not nested", map[string]string{"nested_secondary_view": "", "later_todos": "on"}, "later", false, 1366, 0, 410},
		// the Saved page under hq, beside a 260 sidebar
		{"Saved: hq", nested, "saved", true, 1100, 0, 605},
		{"Saved: hq manual", nested, "saved", true, 1100, 1000, 780},
		// without the pref it's a channel's 40%
		{"Saved: no hq", nested, "saved", false, 1100, 0, 440},
	}
	for _, c := range cases {
		a, ca := expNavApp(t, c.groups)
		if c.saved {
			ca.S.SetRawPrefs(map[string]json.RawMessage{"has_starred_item": json.RawMessage("true")})
		}
		a.sidebar.width = 260
		a.router.Push(c.route)
		a.secondary.width = c.manual
		if got := a.secondaryWidth(gtx, c.panel); got != c.want {
			t.Errorf("%s: width %d, want %d", c.name, got, c.want)
		}
	}
}

// laterBesideEnv is threadHeaderEnv's fixtures with a GPU window for
// VIEWS_SHOTS_DIR and the account's To-dos experiments on.
func laterBesideEnv(t *testing.T, dark bool) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("VIEWS_SHOTS_DIR") != "" {
		if e.gpu != nil {
			e.gpu.Release()
		}
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	for _, x := range []string{"later_todos", "nested_secondary_view", "secondary_view_default_width"} {
		e.a.Core().SetExperimentGroup(x, "on")
	}
	if dark {
		e.a.setDark(true)
	}
	return e
}

// laterShot saves the last frame as VIEWS_SHOTS_DIR/name-light/dark.png.
func (e *perfEnv) laterShot(name string) {
	dir := os.Getenv("VIEWS_SHOTS_DIR")
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

func TestLaterRowsOpenBesideLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) { laterRowsBeside(t, dark) })
	}
}

func laterRowsBeside(t *testing.T, dark bool) {
	e := laterBesideEnv(t, dark)
	c := e.a.Core()
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(0)
	plain := e.fs.ChannelIDs()[0]
	ctx := context.Background()
	// the to-dos' messages, cached as Slack's store has them: a message
	// outside a thread, a reply and its thread's root
	p, err := c.History(ctx, plain, "")
	if err != nil {
		t.Fatal(err)
	}
	top := ""
	for i := len(p.Messages) - 2; i >= 0 && top == ""; i-- {
		if m := p.Messages[i]; m.ThreadTS == "" && m.Subtype == "" {
			top = m.TS
		}
	}
	if top == "" {
		t.Fatalf("no plain message in %s's history", plain)
	}
	if _, err := c.Replies(ctx, pub, root); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	todo := func(ch, ts string, age int64) map[string]any {
		return map[string]any{"item_id": ch, "item_type": "message", "ts": ts, "todo_state": "to_do", "date_created": now - age}
	}
	items := []map[string]any{todo(plain, top, 100), todo(pub, reply, 200), todo(pub, root, 300)}
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "saved.list" {
			return nil, false
		}
		if !strings.HasPrefix(f.Get("filter"), "todo") {
			return map[string]any{"ok": true, "saved_items": []any{}}, true
		}
		return map[string]any{"ok": true, "saved_items": items, "counts": map[string]any{"todo_count": len(items)}}, true
	})
	e.a.Navigate("later")
	var l *views.Later
	e.until("the to-dos", 10*time.Second, func() bool {
		l = pageView[*views.Later](e, "later")
		return l != nil && l.PeekCount() == len(items)
	})
	e.settle(300 * time.Millisecond)

	// each row, clicked in its left padding (no link or avatar there)
	kinds := map[string]secKind{top: secChannel, reply: secThread, root: secThread}
	seen := map[string]bool{}
	o := e.a.primaryOrigin
	for y := o.Y + 160; y < e.size.Y-40 && len(seen) < len(kinds); y += 20 {
		e.click(image.Pt(o.X+8, y))
		e.frame()
		e.frame()
		ch, at := e.a.SecondaryAt()
		if at == "" || seen[at] {
			continue
		}
		want, ok := kinds[at]
		if !ok {
			t.Fatalf("a row opened %s/%s", ch, at)
		}
		seen[at] = true
		s := e.a.secondary
		if r := e.a.router.Current(); r != "later" {
			t.Fatalf("%s: the row left To-dos for %q", at, r)
		}
		if s.kind != want || s.channel != map[bool]string{true: plain, false: pub}[at == top] {
			t.Fatalf("%s: the column shows %v %s/%s", at, s.kind, s.channel, s.ts)
		}
		if sch, sts := l.Selected(); sch != ch || sts != at {
			t.Errorf("%s: the selected row is %q/%q", at, sch, sts)
		}
		switch at {
		case top:
			// isSavedListLayoutClamped: 55% of the region, the list keeping the rest
			if w, x := s.drawnW, e.size.X; w < x*47/100 || w > x*58/100 {
				t.Errorf("the column is %d px of a %d px window, want 55%% of the region", w, x)
			}
			e.laterShot("later-beside-conversation")
		default:
			tv := e.threadView(pub, root)
			e.until("the thread at "+at, 5*time.Second, func() bool {
				return tv != nil && tv.JumpPending() == "" && tv.Highlighted() == at
			})
			if at == reply {
				e.laterShot("later-beside-thread")
			}
		}
	}
	if len(seen) != len(kinds) {
		t.Fatalf("the rows opened %v, want all of %v", seen, kinds)
	}

	// the conversation is the Later tab's: leaving the page closes it
	e.a.ViewTodo(plain, top, "")
	e.frame()
	if ch, at := e.a.SecondaryAt(); ch != plain || at != top || e.a.secondary.kind != secChannel {
		t.Fatalf("View to-dos on a plain message: the column shows %v %s at %s", e.a.secondary.kind, ch, at)
	}
	e.a.Navigate("c/" + pub)
	e.frame()
	if e.a.secondary.kind == secChannel {
		t.Error("the conversation opened beside To-dos stayed open in a channel")
	}

	// View to-dos on the reply from its own conversation's thread pane:
	// the thread opens at the reply without the highlight
	// (jumpToThreadReply: the navigation left the thread's channel)
	e.a.OpenThread(pub, root)
	e.settle(200 * time.Millisecond)
	e.a.ViewTodo(pub, reply, root)
	e.frame()
	tv := e.threadView(pub, root)
	if r := e.a.router.Current(); r != "later" || tv == nil {
		t.Fatalf("View to-dos went to %q (thread view %v)", r, tv != nil)
	}
	if ch, at := e.a.SecondaryAt(); ch != pub || at != reply || e.a.secondary.ts != root {
		t.Fatalf("View to-dos on the reply: the column shows %s/%s at %s", ch, e.a.secondary.ts, at)
	}
	e.until("the reply placed", 5*time.Second, func() bool { return tv.JumpPending() == "" })
	if h := tv.Highlighted(); h != "" {
		t.Errorf("View to-dos from the reply's conversation highlighted %q", h)
	}
	if sch, sts := l.Selected(); sch != pub || sts != reply {
		t.Errorf("View to-dos selected %q/%q, want the reply's to-do", sch, sts)
	}
	// from another conversation it is highlighted
	e.a.Navigate("c/" + plain)
	e.frame()
	e.a.ViewTodo(pub, reply, root)
	e.frame()
	if h := e.threadView(pub, root).Highlighted(); h != reply {
		t.Errorf("View to-dos from elsewhere highlighted %q, want the reply", h)
	}
}
