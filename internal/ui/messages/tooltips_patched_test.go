package messages

import (
	"encoding/json"
	"image"
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
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// tipEnv is a channel view over a router, with the pointer moved through
// it and the strings its frames draw (theme.DrawHook).
type tipEnv struct {
	t     *testing.T
	tt    *taut.Taut
	u     *pluginUI
	app   *core.App
	v     *ChannelView
	r     input.Router
	ops   op.Ops
	now   time.Time
	ts    time.Duration
	mu    sync.Mutex
	drawn []string
	pid   pointer.ID // the mouse's pointer id (Windows': 1)
}

func newTipEnv(t *testing.T, setup func(app *core.App)) *tipEnv {
	t.Helper()
	return newTipEnvRows(t, setup, 3)
}

// newTipEnvRows is newTipEnv waiting for rows rows.
func newTipEnvRows(t *testing.T, setup func(app *core.App), rows int) *tipEnv {
	t.Helper()
	app, _, tt, u, h := pluginEnv(t)
	if setup != nil {
		setup(app)
	}
	e := &tipEnv{t: t, tt: tt, u: u, app: app, now: time.Now()}
	e.v = NewChannelView(h, tGen)
	e.v.Preview = true
	theme.DrawHook = func(s string) {
		e.mu.Lock()
		e.drawn = append(e.drawn, s)
		e.mu.Unlock()
	}
	t.Cleanup(func() { theme.DrawHook = nil })
	until(t, 5*time.Second, "rows", e.frame, func() bool { return len(e.v.s.rows) >= rows })
	return e
}

// frame lays a frame out at the wall clock's time: the list times its
// reaction and status tips by time.Now (layoutList), the rest by gtx.Now.
func (e *tipEnv) frame() {
	e.ops.Reset()
	e.now = time.Now()
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 700)), Now: e.now, Source: e.r.Source()}
	e.v.Layout(gtx)
	e.r.Frame(&e.ops)
}

// move puts the pointer at p and runs a frame.
func (e *tipEnv) move(p image.Point) {
	e.ts += 30 * time.Millisecond
	e.r.Queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(float32(p.X), float32(p.Y)), Source: pointer.Mouse, PointerID: e.pid, Time: e.ts})
	e.frame()
}

// rest runs frames for d of wall-clock time with the pointer still at p.
func (e *tipEnv) rest(p image.Point, d time.Duration) {
	for start := time.Now(); time.Since(start) < d; {
		e.move(p)
		time.Sleep(15 * time.Millisecond)
	}
	e.move(p)
}

// drew reports whether a frame since the last clear drew a string with sub.
func (e *tipEnv) drew(sub string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.drawn {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func (e *tipEnv) clear() {
	e.mu.Lock()
	e.drawn = e.drawn[:0]
	e.mu.Unlock()
}

// row is the row of the message at ts and its item's top in the list.
func (e *tipEnv) row(ts string) (*row, int) {
	for i := e.v.s.visible[0]; i < e.v.s.visible[1] && i < len(e.v.s.items); i++ {
		if it := e.v.s.items[i]; it.kind == itMsg {
			if r := e.v.s.rows[it.key]; r != nil && r.m.TS == ts {
				return r, e.v.s.pos[i]
			}
		}
	}
	return nil, 0
}

// pillCentre is the tada pill's centre (the native layout's), in the list.
func (e *tipEnv) pillCentre() image.Point {
	e.t.Helper()
	var at image.Point
	until(e.t, 5*time.Second, "the tada pill", e.frame, func() bool {
		r, top := e.row(tsTada)
		if r == nil || len(r.reactRects) == 0 || r.reactRects[0].Empty() {
			return false
		}
		rc := r.reactRects[0].Add(image.Pt(0, top))
		at = rc.Min.Add(rc.Size().Div(2))
		return true
	})
	return at
}

// reactionTipAt hovers p for longer than ReactionTip's delay and returns
// the tooltip the list holds then ("" for none).
func (e *tipEnv) reactionTipAt(p image.Point) string {
	e.move(image.Pt(5, 5))
	e.rest(image.Pt(5, 5), 100*time.Millisecond)
	e.rest(p, reactionTipDelay+200*time.Millisecond)
	if e.v.s.tip == tipPending || e.v.s.tipRect.Empty() {
		return ""
	}
	return e.v.s.tip
}

// A plugin patching ReactionBar (Karim, α.137: "hovering over this shit
// doesnt show the names") renders the bar's children itself: the pills'
// ReactionTip must still show, as it does natively.
func TestPluginPatchedReactionBarTip(t *testing.T) {
	e := newTipEnv(t, nil)
	pill := e.pillCentre()
	if tip := e.reactionTipAt(pill); !strings.Contains(tip, "reacted with") {
		t.Fatalf("control: hovering the native tada pill at %v showed %q, want its ReactionTip", pill, tip)
	}
	install(t, e.tt, []byte(`(() => {
const TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;
class PassBar extends TautPlugin {
  static id = 'PassBar'
  static pluginName = 'Pass Bar'
  static description = 'renders ReactionBar through itself'
  static authors = ['clack']
  static defaultConfig = { enabled: true }
  start() {
    this.api.patchComponent('ReactionBar', (O) => (p) => React.createElement(O, { ...p, className: (p.className || '') + ' x-passbar' }))
  }
}
return PassBar
})()`))
	patched(t, "ReactionBar")
	until(t, 8*time.Second, "the patched bar's tree", e.frame, func() bool { return e.u.anyTree("x-passbar") })
	for k := 0; k < 5; k++ {
		e.frame()
	}
	tip := e.reactionTipAt(pill)
	if !strings.Contains(tip, "reacted with") || !strings.Contains(tip, "alice") {
		t.Errorf("with ReactionBar patched, hovering the tada pill at %v showed %q, want \"alice and you reacted with :tada:\"", pill, tip)
	}
}

// ReactionTip's names are getPrefCompliantMemberNameById's: the real name
// under display_real_names, "Name hidden" for a hidden member.
func TestReactionTipNamesFollowPrefs(t *testing.T) {
	app, _, _, _, h := pluginEnv(t)
	re := core.Reaction{Name: "tada", Count: 2, Users: []string{"UALICE", tSelf}}
	if got := tipPlain(reactionTip(h, &re, tSelf)); got != "alice and you reacted with :tada:" {
		t.Errorf("display names: %q", got)
	}
	// display_real_names, as the sender line reads it (uipref)
	t.Cleanup(uipref.Reset)
	app.S.Lock()
	app.S.Prefs.DisplayRealNames = true
	prefs := app.S.Prefs
	app.S.Unlock()
	uipref.Set(prefs)
	if got := tipPlain(reactionTip(h, &re, tSelf)); got != "Alice Example and you reacted with :tada:" {
		t.Errorf("display_real_names: %q, want the real name", got)
	}
	// a real name of ordinary spaces around it reads trimmed; a lone
	// U+200B (Karim's) is no real name to the sender line (core.User's
	// RealName drops it), so the tip names the display name as the line
	// does, where Slack's getMemberRealName would print the invisible one
	zwsp := string(rune(0x200b))
	app.S.PutUsers([]slack.User{
		{ID: "UPAD", Name: "pad", Profile: slack.Profile{DisplayName: "pad", RealName: "  Pad Example "}},
		{ID: "UZW", Name: "zw", Profile: slack.Profile{DisplayName: "karim", RealName: zwsp}},
	})
	pad := core.Reaction{Name: "tada", Count: 1, Users: []string{"UPAD"}}
	if got := tipPlain(reactionTip(h, &pad, tSelf)); got != "Pad Example reacted with :tada:" {
		t.Errorf("a real name with spaces around it: %q", got)
	}
	zw := core.Reaction{Name: "tada", Count: 2, Users: []string{"UZW", tSelf}}
	if got := tipPlain(reactionTip(h, &zw, tSelf)); got != "karim and you reacted with :tada:" {
		t.Errorf("a U+200B real name (%q): %q, want the display name, as the sender line", zwsp, got)
	}
	app.S.Lock()
	if app.S.RawPrefs == nil {
		app.S.RawPrefs = map[string]json.RawMessage{}
	}
	app.S.RawPrefs["hidden_users"] = json.RawMessage(`"UALICE"`)
	app.S.Unlock()
	if got := tipPlain(reactionTip(h, &re, tSelf)); got != "Name hidden and you reacted with :tada:" {
		t.Errorf("hidden_users: %q, want \"Name hidden\"", got)
	}
}

// tipPlain is a ReactionTip's text without its subtitle marks.
func tipPlain(s string) string {
	return strings.NewReplacer(tipSubOn, "", tipSubOff, "").Replace(s)
}

// scanLine moves the pointer along the top line of the row at ts until
// hit holds, returning where.
func (e *tipEnv) scanLine(ts string, hit func(r *row) bool) (image.Point, bool) {
	r, top := e.row(ts)
	if r == nil {
		return image.Point{}, false
	}
	for _, dy := range []int{10, 14, 18} {
		for x := 40; x < 600; x += 3 {
			p := image.Pt(x, top+dy)
			e.move(p)
			if r, _ := e.row(ts); r != nil && hit(r) {
				return p, true
			}
		}
	}
	return image.Point{}, false
}

// statusUsers gives alice a custom status that ends in an hour and bob
// one that already ended.
func statusUsers(app *core.App) {
	exp := time.Now().Add(time.Hour).Unix()
	app.S.PutUsers([]slack.User{
		{ID: "UALICE", Name: "alice", RealName: "Alice Example", Profile: slack.Profile{DisplayName: "alice",
			StatusEmoji: ":palm_tree:", StatusText: "Out &amp; about", StatusExpiration: exp,
			Image24: "https://avatars.invalid/alice-48.png", Image48: "https://avatars.invalid/alice-48.png"}},
		{ID: "UBOB", Name: "bob", RealName: "Bob Example", Profile: slack.Profile{DisplayName: "bob",
			StatusEmoji: ":palm_tree:", StatusText: "Gone fishing", StatusExpiration: time.Now().Add(-time.Hour).Unix()}},
	})
}

// The status emoji by a sender's name has BaseCustomStatus' tooltip:
// the status text (unescaped) and "Until …" after 150 ms; an expired
// status isn't drawn at all.
func TestSenderStatusTooltip(t *testing.T) {
	e := newTipEnv(t, statusUsers)
	if r, _ := e.row(tsFair); r == nil || rowStatusURL(r) == "" {
		t.Fatalf("alice's row has no status emoji (statusURL %q)", func() string {
			if r == nil {
				return "no row"
			}
			return rowStatusURL(r)
		}())
	}
	p, ok := e.scanLine(tsFair, rowStatusHov)
	if !ok {
		t.Fatal("no point on alice's name line hovers her status emoji")
	}
	e.rest(p, statusTipDelayT+100*time.Millisecond)
	on, text, sub, rect := statusTipState(e.v.s)
	if !on || text != "Out & about" || !strings.HasPrefix(sub, "Until ") {
		t.Errorf("status tooltip: on %v, %q / %q; want \"Out & about\" and \"Until …\"", on, text, sub)
	}
	if rect.Empty() || !p.In(rect.Inset(-1)) {
		t.Errorf("status tooltip anchored at %v, the pointer at %v", rect, p)
	}
	if !e.drew("about") { // flowTip draws word by word
		t.Error("the status tooltip's text was never drawn")
	}
	// bob's status ended an hour ago: no emoji, nothing to hover
	if _, ok := e.scanLine(tsTada, func(r *row) bool { return rowStatusHov(r) || rowStatusDrawn(r) }); ok {
		t.Error("bob's expired status is still drawn")
	}
}

// A message's time has Timestamp's tooltip: the full date with seconds,
// after 300 ms.
func TestTimestampTooltip(t *testing.T) {
	e := newTipEnv(t, nil)
	p, ok := e.scanLine(tsFair, rowTimeHov)
	if !ok {
		t.Fatal("no point on alice's name line hovers the time")
	}
	want := fullTimestampFor(tsFair, e.now)
	e.clear()
	e.rest(p, timestampTipDelayT/3)
	if e.drew(want) {
		t.Errorf("the time's tooltip showed before its %v delay", timestampTipDelayT)
	}
	e.rest(p, timestampTipDelayT)
	if !e.drew(want) {
		t.Errorf("hovering the time didn't draw %q", want)
	}
}

// ReactionAddButton has its "Add reaction…" tooltip, with the bar
// patched or not.
func TestPluginReactionAddButtonTooltip(t *testing.T) {
	e := newTipEnv(t, nil)
	var add image.Point
	until(t, 5*time.Second, "the add button", e.frame, func() bool {
		r, top := e.row(tsTada)
		if r == nil || r.addRect.Empty() {
			return false
		}
		rc := r.addRect.Add(image.Pt(0, top))
		add = rc.Min.Add(rc.Size().Div(2))
		return true
	})
	check := func(what string) {
		t.Helper()
		e.move(image.Pt(5, 5))
		e.rest(image.Pt(5, 5), 100*time.Millisecond)
		e.clear()
		e.rest(add, reactionAddTipDelayT+100*time.Millisecond)
		if !e.drew(reactionAddTipT) {
			t.Errorf("%s: hovering the add-reaction button at %v drew no %q tooltip", what, add, reactionAddTipT)
		}
	}
	check("native")
	install(t, e.tt, []byte(`(() => {
const TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;
class PassBar extends TautPlugin {
  static id = 'PassBar'
  static pluginName = 'Pass Bar'
  static description = 'renders ReactionBar through itself'
  static authors = ['clack']
  static defaultConfig = { enabled: true }
  start() {
    this.api.patchComponent('ReactionBar', (O) => (p) => React.createElement(O, { ...p, className: (p.className || '') + ' x-passbar' }))
  }
}
return PassBar
})()`))
	patched(t, "ReactionBar")
	until(t, 8*time.Second, "the patched bar's tree", e.frame, func() bool { return e.u.anyTree("x-passbar") })
	check("ReactionBar patched")
}

// Grouped messages: alice's two (the second a grouped row, its time in
// the gutter while it's hovered) and bob's.
const (
	tsG1 = "1700000501.000100"
	tsG2 = "1700000561.000100"
	tsG3 = "1700000621.000100"
)

func groupedMessages(app *core.App) {
	app.S.SetLatest(tGen, []slack.Message{
		{Type: "message", TS: tsG1, User: "UALICE", Text: "first of a group"},
		{Type: "message", TS: tsG2, User: "UALICE", Text: "grouped follow-up"},
		{Type: "message", TS: tsG3, User: "UBOB", Text: "the next message"},
	}, false)
}

// item is the list item of the message at ts.
func (e *tipEnv) item(ts string) (r *row, top, height int, header bool) {
	for i := e.v.s.visible[0]; i < e.v.s.visible[1] && i < len(e.v.s.items); i++ {
		if it := e.v.s.items[i]; it.kind == itMsg {
			if r := e.v.s.rows[it.key]; r != nil && r.m.TS == ts {
				return r, e.v.s.pos[i], e.v.s.heights[i], it.header
			}
		}
	}
	return nil, 0, 0, false
}

// The gutter time's tooltip ends when the pointer leaves the time
// straight into the next row, in a frame that doesn't draw the time (its
// row isn't hovered then). The Leave is never read, so the trigger hears
// only its handler's reset Cancel, of pointer 0, when the row is hovered
// again; with Windows' mouse (pointer 1) the tip came up at once over the
// message and stayed (the patched-tooltips review).
func TestGutterTimeTipEndsOnWindowsMouse(t *testing.T) {
	e := newTipEnv(t, groupedMessages)
	e.pid = 1
	var r2 *row
	var top2, h2, top3, h3 int
	until(t, 5*time.Second, "the grouped rows", e.frame, func() bool {
		var hd2 bool
		r2, top2, h2, hd2 = e.item(tsG2)
		r3, t3, hh3, _ := e.item(tsG3)
		top3, h3 = t3, hh3
		return r2 != nil && r3 != nil && !hd2 && h2 > 0 && h3 > 0
	})
	onRow2 := image.Pt(300, top2+h2/2)
	e.rest(onRow2, 50*time.Millisecond)
	var at image.Point
	found := false
	for y := top2 + 2; y < top2+h2 && !found; y += 3 {
		for x := 2; x < 80 && !found; x += 3 {
			e.move(image.Pt(x, y))
			if rowCompactHov(r2) {
				at, found = image.Pt(x, y), true
			}
		}
	}
	if !found {
		t.Fatal("control: no point in the grouped row's gutter hovers its time")
	}
	e.rest(at, timestampTipDelayT+100*time.Millisecond)
	if !rowCompactTipShowing(r2, e.now) {
		t.Fatal("control: resting on the gutter time doesn't show its tooltip")
	}
	// one frame: off the time into the next row
	e.move(image.Pt(300, top3+h3/2))
	if r, _, _, _ := e.item(tsG2); r != r2 {
		t.Fatal("the grouped row was made again; the test needs the same row")
	}
	// back on the grouped row, away from its time
	e.rest(onRow2, 100*time.Millisecond)
	if rowCompactHov(r2) || rowCompactTipShowing(r2, e.now) {
		t.Errorf("the pointer on the row's text (%v), away from its gutter time: the time is still hovered (%v) and its tooltip up (%v)",
			onRow2, rowCompactHov(r2), rowCompactTipShowing(r2, e.now))
	}
}

const tsMulti = "1700000701.000100"

func twoReactions(app *core.App) {
	app.S.SetLatest(tGen, []slack.Message{
		{Type: "message", TS: tsMulti, User: "UALICE", Text: "two reactions", Reactions: []slack.Reaction{
			{Name: "tada", Count: 2, Users: []string{"UALICE", tSelf}},
			{Name: "eyes", Count: 1, Users: []string{"UBOB"}},
		}},
	}, false)
}

// A plugin's ReactionBar that wraps every slot in one element: each
// pill's ReactionTip sits on that pill, not at the row's corner (one box
// for several pills names none of them).
func TestPluginWrappedPillsTipOnTheirPill(t *testing.T) {
	e := newTipEnvRows(t, twoReactions, 1)
	install(t, e.tt, []byte(`(() => {
const TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;
class WrapPills extends TautPlugin {
  static id = 'WrapPills'
  static pluginName = 'Wrap Pills'
  static description = 'wraps the bar children in one span'
  static authors = ['clack']
  static defaultConfig = { enabled: true }
  start() {
    this.api.patchComponent('ReactionBar', (O) => (p) => React.createElement(O, { ...p, children: React.createElement('span', { className: 'x-wrapped-pills' }, p.children) }))
  }
}
return WrapPills
})()`))
	patched(t, "ReactionBar")
	until(t, 8*time.Second, "the wrapped pills", e.frame, func() bool { return e.u.anyTree("x-wrapped-pills") })
	var r *row
	var top, height int
	until(t, 5*time.Second, "the row", e.frame, func() bool {
		r, top, height, _ = e.item(tsMulti)
		return r != nil && len(r.groups) == 2
	})
	hit := map[string]bool{}
	for y := top + 2; y < top+height; y += 3 {
		for x := 40; x < 400; x += 3 {
			p := image.Pt(x, y)
			e.move(p)
			if e.v.s.tip == "" {
				continue
			}
			name := e.v.s.tipKey[strings.IndexByte(e.v.s.tipKey, 0)+1:]
			box := e.v.s.tipRect.Add(image.Pt(0, top))
			if !p.In(box.Inset(-1)) {
				t.Fatalf("the pointer at %v on the %s pill: its tip anchored at %v", p, name, box)
			}
			hit[name] = true
		}
	}
	if !hit["tada"] || !hit["eyes"] {
		t.Errorf("hovered pills %v; want both tada and eyes", hit)
	}
}
