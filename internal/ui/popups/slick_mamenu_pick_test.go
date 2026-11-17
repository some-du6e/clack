package popups

// Picking message menu rows as a user does, with plugins in the shape
// Slick's build:plugin writes (no SLICK_PLUGINS_DIR): clicks and keys go
// through Gio's router, the menu and the picks it shows (maPick) are drawn
// every frame as the App draws its overlays, and what a row puts on the
// clipboard is read back from the router. A plugin's onSelectAction
// decides each pick the way Slack's dispatchMessageAction lets it
// (`onSelectAction(…) || native(…)`): a truthy answer, a throw or a
// Promise keeps Clack's row from running; a falsy one runs it, once.

import (
	"context"
	"image"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/ui/richclip"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// maDriver draws a message menu, then the picks it showed, through Gio's
// router at 1200×800, 1 px/dp.
type maDriver struct {
	t     *testing.T
	h     *tzHost
	m     *Menu
	r     input.Router
	ops   op.Ops
	clock time.Duration
	clip  []string  // what the frames put on the clipboard, in order
	now   time.Time // the frames' time; zero: the wall clock
	scale float32   // px per dp; zero: 1
}

func newMADriver(t *testing.T, h *tzHost, m *Menu) *maDriver {
	t.Helper()
	d := &maDriver{t: t, h: h, m: m}
	// two frames: the first one's FocusCmd holds that frame's events
	d.frame()
	d.frame()
	return d
}

// frame draws the menu and, above it, each pick it showed that hasn't
// finished, as the App draws its overlays.
func (d *maDriver) frame() {
	d.ops.Reset()
	now, scale := d.now, d.scale
	if now.IsZero() {
		now = time.Now()
	}
	if scale == 0 {
		scale = 1
	}
	gtx := layout.Context{Ops: &d.ops, Source: d.r.Source(), Now: now, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(image.Pt(1200, 800))}
	slackc.Frame(gtx)
	if !d.m.Dismissed() {
		d.m.Layout(gtx)
	}
	for _, p := range maPickOverlays(d.h) {
		if !p.Dismissed() {
			g := gtx
			g.Constraints = layout.Constraints{Max: gtx.Constraints.Max}
			p.Layout(g)
		}
	}
	d.r.Frame(&d.ops)
	if _, b, ok := d.r.WriteClipboard(); ok {
		d.clip = append(d.clip, string(b))
	}
	Pump()
	slackc.Pump()
}

// until draws frames until cond.
func (d *maDriver) until(what string, cond func() bool) {
	d.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		d.frame()
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	d.t.Fatalf("timed out waiting for %s", what)
}

// settle draws frames for a while: long enough for a pick's answer, and
// what a row it ran started, to come back.
func (d *maDriver) settle() {
	for k := 0; k < 20; k++ {
		d.frame()
		time.Sleep(20 * time.Millisecond)
	}
}

// click clicks the middle of r (window px) with the primary button.
func (d *maDriver) click(r image.Rectangle) {
	d.t.Helper()
	if r.Empty() {
		d.t.Fatal("clicking an empty rectangle: the row wasn't laid out")
	}
	d.clickAt(r.Min.Add(r.Max).Div(2))
}

// clickLabel clicks row r's label: where a row a plugin drew as Slack's
// MenuItem markup has its button (no Slack stylesheet here makes it the
// row's width).
func (d *maDriver) clickLabel(r image.Rectangle) {
	d.t.Helper()
	if r.Empty() {
		d.t.Fatal("clicking an empty rectangle: the row wasn't laid out")
	}
	d.clickAt(image.Pt(r.Min.X+24+12, (r.Min.Y+r.Max.Y)/2))
}

// clickAt clicks window point c with the primary button: the press in one
// frame, the release in the next, as a user's click comes.
func (d *maDriver) clickAt(c image.Point) {
	d.t.Helper()
	p := f32.Pt(float32(c.X), float32(c.Y))
	d.clock += time.Second
	d.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: d.clock},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: d.clock + 10*time.Millisecond},
	)
	d.frame()
	d.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: d.clock + 60*time.Millisecond})
	d.frame()
	d.frame()
}

// maRun runs row it as Menu.activate would without closing the menu
// first (DoCtx, then Do), so what closes it is the row's own doing.
func maRun(it MenuItem, gtx layout.Context) {
	if it.DoCtx != nil {
		it.DoCtx(gtx)
	}
	if it.Do != nil {
		it.Do()
	}
}

// clickRow clicks the root card's row whose MAMenu id is id.
func (d *maDriver) clickRow(id string) {
	d.t.Helper()
	i := maIndex(d.m.items, id)
	rows := d.m.RowRects()
	if i < 0 || i >= len(rows) {
		d.t.Fatalf("no row %s to click: %s", id, maIDs(d.m.items))
	}
	d.click(rows[i])
}

// openSub clicks submenu row id and waits for its card (a message
// actions submenu's after its own MAMenu's tree, when that's patched).
func (d *maDriver) openSub(id string) *Menu {
	d.t.Helper()
	d.clickRow(id)
	d.until("the "+id+" submenu", func() bool {
		s := d.m.sub
		return s != nil && laidOut(s) && (s.ma == nil || maFrom(s) || !slackc.Patched("MAMenu"))
	})
	return d.m.sub
}

// clickSub clicks the open submenu's row whose MAMenu id is id.
func (d *maDriver) clickSub(id string) {
	d.t.Helper()
	s := d.m.sub
	if s == nil {
		d.t.Fatal("no submenu open")
	}
	i := maIndex(s.items, id)
	if i < 0 || i >= len(s.rows) {
		d.t.Fatalf("no submenu row %s: %s", id, maIDs(s.items))
	}
	d.click(s.rows[i])
}

// key presses and releases n, then draws the frames that handle it.
func (d *maDriver) key(n key.Name, mods key.Modifiers) {
	d.r.Queue(key.Event{Name: n, Modifiers: mods, State: key.Press})
	d.frame()
	d.r.Queue(key.Event{Name: n, Modifiers: mods, State: key.Release})
	d.frame()
	d.frame()
}

// answered waits for the plugin's answer to the last pick and the frames
// after it, and returns it (ok false: no pick was shown).
func (d *maDriver) answered() (maVerdict, bool) {
	d.t.Helper()
	d.until("the plugin's answer", func() bool {
		v, ok := maLastAnswer(d.h)
		return !ok || v != maWaiting
	})
	d.settle()
	return maLastAnswer(d.h)
}

// maIndex is the row whose MAMenu id is id, or -1.
func maIndex(items []MenuItem, id string) int {
	for i := range items {
		if !items[i].Separator && maRowID(&items[i]) == id {
			return i
		}
	}
	return -1
}

// laidOut reports whether m's card drew its rows.
func laidOut(m *Menu) bool {
	return len(m.items) > 0 && len(m.rows) == len(m.items) && !m.rect.Empty()
}

// maOpen drives m until the plugin's MAMenu tree (when one is patched)
// was applied and the card drew its rows.
func maOpen(t *testing.T, e *slEnv, m *Menu, what string) *maDriver {
	t.Helper()
	d := newMADriver(t, e.h, m)
	d.until(what, func() bool { return (maFrom(m) || !slackc.Patched("MAMenu")) && laidOut(m) })
	for k := 0; k < 3; k++ {
		d.frame()
	}
	return d
}

// maDecide replaces the root message menu's onSelectAction as a plugin
// that handles some actions itself does: it records every pick it's asked
// about, takes Copy link and reminders (true), throws on Save, answers Get
// notified with a Promise, and hands the rest to the menu's own (falsy).
var maDecide = maPlugin("DecideSlick", `[]`, `
  start() {
    globalThis.__decided = [];
    this.api.patchComponent("MAMenu", (Original) => (props) => {
      if (props.autoClogProps?.uiComponentName !== "message_actions_menu" || typeof props.onSelectAction !== "function") return React.createElement(Original, props);
      return React.createElement(Original, { ...props, onSelectAction: (event, action, context) => {
        globalThis.__decided.push(action?.id + "@" + context?.ts + "/" + event?.type);
        if (action?.id === "copy_link" || action?.id === "remind") return true;
        if (action?.id === "later") throw new Error("the plugin's handler failed");
        if (action?.id === "subscribe") return Promise.resolve(false);
        return props.onSelectAction(event, action, context);
      } });
    });
  }`)

// A native row picked through a plugin's onSelectAction: the plugin's
// answer decides whether Clack's row runs, and a row passed on runs once,
// from the pick.
func TestMessageMenuSelectActionDecides(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "DecideSlick", maDecide)
	slPatched(t, "MAMenu")
	decided := func() string { return e.jsString(t, "globalThis.__decided") }
	at := "@" + slMsgTS + "/click"
	open := func(what string) (*Menu, *maDriver) {
		t.Helper()
		m := NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
		return m, maOpen(t, e, m, what)
	}
	// the plugin's answer, checked where the menu reports it (it is
	// Clack's pick overlay); what ran is checked from what it did
	answer := func(d *maDriver, want maVerdict) bool {
		t.Helper()
		v, ok := d.answered()
		return !ok || v == want
	}

	// Copy link: the plugin takes it, so nothing goes on the clipboard
	_, d := open("the menu")
	d.clickRow("copy_link")
	if !answer(d, maTaken) || decided() != "copy_link"+at {
		t.Fatalf("Copy link: not taken, or asked %q", decided())
	}
	if len(d.clip) != 0 {
		t.Errorf("Copy link the plugin took: clipboard %q", d.clip)
	}

	// Mark unread: passed on, Clack's row runs, once
	_, d = open("the menu again")
	before := e.api.called("conversations.mark")
	d.clickRow("mark_unread")
	if !answer(d, maPassed) {
		t.Fatal("Mark unread: not passed on")
	}
	d.until("Mark unread marking", func() bool { return e.api.called("conversations.mark") > before })
	d.settle()
	if n := e.api.called("conversations.mark") - before; n != 1 {
		t.Errorf("Mark unread marked %d times (want once)", n)
	}
	if got := decided(); got != "copy_link"+at+",mark_unread"+at {
		t.Errorf("asked %q", got)
	}

	// its key (U) is a pick like a click
	_, d = open("the menu for U")
	d.key("U", 0)
	d.until("U marking", func() bool { return e.api.called("conversations.mark") > before+1 })
	d.settle()
	if n := e.api.called("conversations.mark") - before; n != 2 || !strings.HasSuffix(decided(), ",mark_unread"+at+",mark_unread"+at) {
		t.Errorf("after U: marked %d times (want 2), asked %q", n, decided())
	}

	// Save: the plugin's handler throws, so Slack's own doesn't run
	saved := e.api.called("saved.add")
	_, d = open("the menu for Save")
	d.clickRow("later")
	if !answer(d, maTaken) || !strings.HasSuffix(decided(), ",later"+at) {
		t.Errorf("Save: not taken, or asked %q", decided())
	}
	if e.api.called("saved.add") != saved {
		t.Error("Save ran although the plugin's handler threw")
	}

	// Get notified: a Promise is truthy
	follows := e.api.called("subscriptions.thread.add")
	_, d = open("the menu for Get notified")
	d.clickRow("subscribe")
	if !answer(d, maTaken) || !strings.HasSuffix(decided(), ",subscribe"+at) {
		t.Errorf("Get notified: not taken, or asked %q", decided())
	}
	if e.api.called("subscriptions.thread.add") != follows {
		t.Error("Get notified ran although the plugin answered a Promise")
	}

	// a reminder preset picks through the root's onSelectAction as
	// {id: "remind"} (Slack's RemindSubmenu): the plugin takes it, so no
	// reminder is saved
	_, d = open("the menu for Remind")
	presets := d.openSub("remind")
	d.click(presets.rows[0])
	if !answer(d, maTaken) || !strings.HasSuffix(decided(), ",remind"+at) {
		t.Errorf("a reminder preset: not taken, or asked %q", decided())
	}
	if e.api.called("saved.add") != saved {
		t.Error("the preset saved a reminder although the plugin took it")
	}

	// Organize's rows are its own MAMenu's, with MessageActionsMenu's
	// onSelectAction (Slack's OrganizeMessageActionsSubmenu): a patch on
	// the root's isn't asked about them, and Pin pins, once
	pins := e.api.called("pins.add")
	asked := decided()
	_, d = open("the menu for Organize")
	d.openSub("organize_actions_menu")
	d.clickSub("pin")
	d.until("Pin pinning", func() bool { return e.api.called("pins.add") > pins })
	d.settle()
	if n := e.api.called("pins.add") - pins; n != 1 || decided() != asked {
		t.Errorf("Organize > Pin: pinned %d times (want once), asked %q (was %q)", n, decided(), asked)
	}

	// Organize's P from the root menu is MessageActionsMenu's own key
	// handler (onMenuKeyDown): it pins without asking the plugin
	_, d = open("the menu for P")
	d.key("P", 0)
	d.until("P pinning", func() bool { return e.api.called("pins.add") > pins+1 })
	d.settle()
	if n := e.api.called("pins.add") - pins; n != 2 || decided() != asked {
		t.Errorf("P: pinned %d times (want 2), asked %q (was %q)", n, decided(), asked)
	}
}

// maSubDecide wraps the onSelectAction of a message actions submenu's own
// MAMenu (no autoClogProps; Submenu.renderSubmenu's isSubmenu) and takes
// Pin.
var maSubDecide = maPlugin("SubDecideSlick", `[]`, `
  start() {
    globalThis.__subDecided = [];
    this.api.patchComponent("MAMenu", (Original) => (props) => {
      if (!props.isSubmenu || props.autoClogProps || typeof props.onSelectAction !== "function") return React.createElement(Original, props);
      return React.createElement(Original, { ...props, onSelectAction: (event, action, context) => {
        globalThis.__subDecided.push(action?.id + "@" + context?.ts);
        if (action?.id === "pin") return true;
        return props.onSelectAction(event, action, context);
      } });
    });
  }`)

// A patch on a message actions submenu's own MAMenu decides that
// submenu's picks, as in Slack, where Organize renders a MAMenu of its
// own: it isn't asked about the root's rows, nor about Organize's P from
// the root (MessageActionsMenu's own key handler).
func TestMessageMenuSubmenuSelectAction(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "SubDecideSlick", maSubDecide)
	slPatched(t, "MAMenu")
	decided := func() string { return e.jsString(t, "globalThis.__subDecided") }
	open := func(what string) (*Menu, *maDriver) {
		t.Helper()
		m := NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
		return m, maOpen(t, e, m, what)
	}

	// a root row: not the submenu wrapper's
	before := e.api.called("conversations.mark")
	_, d := open("the menu")
	d.clickRow("mark_unread")
	d.until("Mark unread marking", func() bool { return e.api.called("conversations.mark") > before })
	d.settle()
	if got := decided(); got != "" {
		t.Errorf("the submenu wrapper was asked about a root row: %q", got)
	}

	// Organize > Pin: asked, taken
	pins := e.api.called("pins.add")
	_, d = open("the menu for Organize")
	org := d.openSub("organize_actions_menu")
	if !maFrom(org) {
		t.Fatal("Organize's MAMenu tree didn't arrive")
	}
	d.clickSub("pin")
	if v, ok := d.answered(); ok && v != maTaken {
		t.Errorf("Organize > Pin: answer %d", v)
	}
	if got := decided(); got != "pin@"+slMsgTS {
		t.Errorf("asked %q, want pin@%s", got, slMsgTS)
	}
	if e.api.called("pins.add") != pins {
		t.Error("Pin pinned although the submenu's onSelectAction took it")
	}

	// P from the root: MessageActionsMenu's own handler, nobody asked
	_, d = open("the menu for P")
	d.key("P", 0)
	d.until("P pinning", func() bool { return e.api.called("pins.add") > pins })
	if got := decided(); got != "pin@"+slMsgTS {
		t.Errorf("P asked the submenu's onSelectAction: %q", got)
	}
}

// maPass hands every pick on to the menu's own onSelectAction after
// recording it (a submenu's as "sub:<id>"), and moves Mark unread into
// Organize as Better Message Menu does: out of the root's actions, into
// those of Organize's own MAMenu (Better Message Menu passes them in a
// context; a variable does here).
var maPass = maPlugin("PassSlick", `[]`, `
  start() {
    globalThis.__passed = [];
    let moved = null;
    this.api.patchComponent("MAMenu", (Original) => (props) => {
      if (!Array.isArray(props.actions) || typeof props.onSelectAction !== "function") return React.createElement(Original, props);
      const record = (tag) => (event, action, context) => {
        globalThis.__passed.push(tag + action?.id);
        return props.onSelectAction(event, action, context);
      };
      if (props.isSubmenu) {
        const own = props.actions.flat();
        const actions = moved && own.some((a) => a.id === "pin") ? [[moved, ...own]] : props.actions;
        return React.createElement(Original, { ...props, actions, onSelectAction: record("sub:") });
      }
      if (props.autoClogProps?.uiComponentName !== "message_actions_menu") return React.createElement(Original, props);
      moved = props.actions.flat().find((a) => a.id === "mark_unread") ?? null;
      const actions = props.actions.map((g) => g.filter((a) => a.id !== "mark_unread")).filter((g) => g.length);
      return React.createElement(Original, { ...props, actions, onSelectAction: record("") });
    });
  }`)

// Clack's rows picked through a plugin's onSelectAction run as they do
// without one: the clipboard rows with the frame's clipboard, Edit and
// Delete on one's own message, a reminder preset, a row moved into
// Organize, and the thread pane's Mark unread, from a click or a key.
func TestMessageMenuNativeRowsThroughPlugin(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "PassSlick", maPass)
	slPatched(t, "MAMenu")
	passed := func() string { return e.jsString(t, "globalThis.__passed") }
	open := func(m *Menu, what string) *maDriver {
		t.Helper()
		return maOpen(t, e, m, what)
	}
	menu := func() *Menu { return NewMessageMenu(e.h, image.Pt(900, 200), maMsg) }
	link := permalink(e.h, slGen, slMsgTS, "")

	// Copy link, clicked and by its key
	d := open(menu(), "the menu")
	d.clickRow("copy_link")
	d.until("the link on the clipboard", func() bool { return len(d.clip) > 0 })
	if d.clip[0] != link || passed() != "copy_link" {
		t.Errorf("Copy link: clipboard %q (want %q), asked %q", d.clip, link, passed())
	}
	d = open(menu(), "the menu for L")
	d.key("L", 0)
	d.until("L putting the link on the clipboard", func() bool { return len(d.clip) > 0 })
	if d.clip[0] != link {
		t.Errorf("L: clipboard %q", d.clip)
	}
	if !richclip.Supported() { // else Copy message writes the OS clipboard itself
		d = open(menu(), "the menu for Copy message")
		d.clickRow("copy_text")
		d.until("the text on the clipboard", func() bool { return len(d.clip) > 0 })
		if d.clip[0] != "a synthetic message" {
			t.Errorf("Copy message: clipboard %q", d.clip)
		}
	}

	// Mark unread, moved into Organize's own MAMenu, asks its
	// onSelectAction and marks
	m := menu()
	d = open(m, "the menu for Organize")
	org := d.openSub("organize_actions_menu")
	if got := maIDs(org.items); got != "mark_unread pin" {
		t.Fatalf("Organize submenu %s", got)
	}
	before := e.api.called("conversations.mark")
	d.clickSub("mark_unread")
	d.until("Mark unread from Organize marking", func() bool { return e.api.called("conversations.mark") > before })
	d.settle()
	if n := e.api.called("conversations.mark") - before; n != 1 || !strings.HasSuffix(passed(), ",sub:mark_unread") {
		t.Errorf("Mark unread from Organize: marked %d times, asked %q", n, passed())
	}

	// a reminder preset (Slack's RemindSubmenu): asked as {id: "remind"}
	// through the root's onSelectAction, then saved with its due date
	saved := e.api.called("saved.add")
	m = menu()
	d = open(m, "the menu for Remind")
	sub := d.openSub("remind")
	if len(sub.items) == 0 {
		t.Fatal("no reminder presets")
	}
	picked := time.Now()
	d.click(sub.rows[0]) // In 30 mins
	d.until("the preset saving the reminder", func() bool { return e.api.called("saved.add") > saved })
	d.settle()
	if n := e.api.called("saved.add") - saved; n != 1 || !strings.HasSuffix(passed(), ",remind") {
		t.Errorf("the preset saved %d times (want once), asked %q", n, passed())
	}
	if f := e.api.forms("saved.add"); len(f) > 0 {
		due, _ := strconv.ParseInt(f[len(f)-1].Get("date_due"), 10, 64)
		if d := time.Unix(due, 0).Sub(picked); d < 29*time.Minute || d > 31*time.Minute {
			t.Errorf("the reminder is due in %v, want 30 mins", d)
		}
	}

	// one's own message: Edit message starts editing, Delete message asks first
	own := core.Message{Channel: slGen, TS: slMsgTS, User: slSelf, Text: "a synthetic message"}
	edits, deletes := 0, 0
	opts := MessageMenuOpts{OnEdit: func() { edits++ }, Delete: func(context.Context, string, string) error { deletes++; return nil }}
	d = open(NewMessageMenuOpts(e.h, image.Pt(900, 200), own, opts), "one's own message's menu")
	d.clickRow("edit_message")
	d.until("Edit message", func() bool { return edits > 0 })
	d.settle()
	if edits != 1 {
		t.Errorf("edited %d times, want once", edits)
	}
	d = open(NewMessageMenuOpts(e.h, image.Pt(900, 200), own, opts), "the menu for Delete")
	d.clickRow("delete_message")
	var confirm *Modal
	d.until("the delete confirmation", func() bool {
		var ok bool
		confirm, ok = lastShown[*Modal](e.h)
		return ok
	})
	if confirm.o.Title != "Delete message" || deletes != 0 {
		t.Errorf("Delete message: %q shown, %d deletes", confirm.o.Title, deletes)
	}

	// the thread pane's Mark unread marks the thread
	reply := core.Message{Channel: slGen, TS: "1700000003.000300", ThreadTS: slMsgTS, User: slBob, Text: "a reply"}
	marks := e.api.called("subscriptions.thread.mark")
	d = open(NewMessageContextMenu(e.h, image.Pt(400, 300), reply, InThread, MessageMenuOpts{}), "a reply's menu in the thread pane")
	d.clickRow("mark_thread_unread")
	d.until("the thread marked unread", func() bool { return e.api.called("subscriptions.thread.mark") > marks })
	d.settle()
	if n := e.api.called("subscriptions.thread.mark") - marks; n != 1 || !strings.HasSuffix(passed(), ",mark_thread_unread") {
		t.Errorf("the thread's Mark unread: %d marks, asked %q", n, passed())
	}
}

// maOldLogger adds its rows the way MessageLogger did before Slick #473
// (97cbb8e): MessageActionsMenu's patch puts them in a context, and Menu's
// patch, inside it, appends them to Menu's children as Slack MenuItem
// markup whose click runs, then closes the menu (onTriggerClose).
var maOldLogger = maPlugin("OldLoggerSlick", `["redux", "react"]`, `
  start() {
    globalThis.__oldRows = [];
    const Rows = React.createContext([]);
    const row = (key, label, close) => React.createElement("div", { key, className: "c-menu_item__li", role: "presentation" },
      React.createElement("button", { type: "button", role: "menuitem", className: "c-menu_item__button", "data-qa": key,
        onClick: () => { globalThis.__oldRows.push(key); close?.(); } },
        React.createElement("div", { className: "c-menu_item__label" }, label)));
    this.api.patchComponent("MessageActionsMenu", (Original) => (props) => {
      const rows = props.channelId === "`+slGen+`" && props.ts === "`+slMsgTS+`" ? [row("old_accept_delete", "Accept deletion", props.onTriggerClose)] : [];
      return React.createElement(Rows.Provider, { value: rows }, React.createElement(Original, props));
    });
    this.api.patchComponent("Menu", (Original) => (props) => {
      const rows = React.useContext(Rows);
      if (!rows.length) return React.createElement(Original, props);
      return React.createElement(Rows.Provider, { value: [] }, React.createElement(Original, props, props.children, rows));
    });
  }`)

// A plugin build from before Slick #473 adds message menu rows as Menu
// children inside MessageActionsMenu (MessageLogger's Accept deletion):
// they're drawn after Clack's rows, alone and next to a #473 plugin's
// message actions, and clicking one runs it and closes the menu.
func TestMessageMenuOldMenuRows(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "OldLoggerSlick", maOldLogger)
	slPatched(t, "MessageActionsMenu", "Menu")
	check := func(what string, acts bool) {
		t.Helper()
		m := NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
		d := newMADriver(t, e.h, m)
		d.until(what, func() bool {
			return slIndex(m.items, "plugin-row") >= 0 && (!acts || slIndex(m.items, "acts_one") >= 0) && laidOut(m)
		})
		d.settle()
		i := slIndex(m.items, "plugin-row")
		if i != len(m.items)-1 {
			t.Errorf("%s: the old row isn't after the menu's rows: %d of %s", what, i, maKeys(m.items))
		}
		for _, k := range []string{"mark_unread", "copy_link", "organize"} {
			if slIndex(m.items, k) < 0 {
				t.Errorf("%s: native row %s gone: %s", what, k, maKeys(m.items))
			}
		}
		if acts && slIndex(m.items, "acts_one") > i {
			t.Errorf("%s: the #473 actions after the old row: %s", what, maKeys(m.items))
		}
		n := strings.Count(e.jsString(t, "globalThis.__oldRows.join(' ')"), "old_accept_delete")
		d.clickLabel(m.RowRects()[i])
		d.until(what+": the old row's click", func() bool {
			return strings.Count(e.jsString(t, "globalThis.__oldRows.join(' ')"), "old_accept_delete") == n+1
		})
		// its close (MessageActionsMenu's onTriggerClose) comes back as a
		// queued Go call
		d.until(what+": onTriggerClose closing the menu", m.Dismissed)
	}
	check("the old row in the More actions menu", false)
	// with a #473 plugin's actions in the same menu (Slick's MAMenu patch)
	e.loadCode(t, "ActsSlick", maActs)
	slPatched(t, "MAMenu")
	check("the old row next to Act one", true)
}

// maLegacy is a plugin in the shape of Slick's IgnoreTime and CallTimer:
// it declares no requiredAPIs, so it gets every API (the menu one too),
// and only sets a style.
var maLegacy = `// slick-plugin {"id":"LegacySlick"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class P extends import_slick.SlickPlugin {
  static id = "LegacySlick";
  static pluginName = "LegacySlick";
  static description = "test";
  start() { this.api.setStyle(".legacy-slick { color: red; }"); }
}
return P;
});
`

// Slick's own MAMenu patch comes with the first message action provider
// and goes with the last. Without one, nothing can change a message menu,
// so it is Clack's alone: it draws its card in the click's frame and sends
// JS no MAMenu render, with a plugin running that declares every API (the
// menu one included), and after the only menu plugin stopped.
func TestMessageMenuNativeWithoutProviders(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "LegacySlick", maLegacy)
	native := func(what string) {
		t.Helper()
		if slackc.Patched("MAMenu") {
			t.Fatalf("%s: MAMenu is patched (%v)", what, slackc.PatchedNames())
		}
		roots := e.rec.count("MAMenu")
		m := NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
		f := slFrame(m)
		f()
		if !laidOut(m) {
			t.Errorf("%s: the card wasn't drawn in the menu's first frame", what)
		}
		for k := 0; k < 5; k++ {
			f()
			Pump()
			slackc.Pump()
		}
		if n := e.rec.count("MAMenu") - roots; n != 0 {
			t.Errorf("%s: %d MAMenu renders went to JS", what, n)
		}
		if maIDs(m.items) != maIDs(maNative(m)) {
			t.Errorf("%s: rows %s, natively %s", what, maIDs(m.items), maIDs(maNative(m)))
		}
	}
	native("a plugin declaring every API")

	// a provider brings Slick's patch, and the last provider's stop takes it
	e.loadCode(t, "ActsSlick", maActs)
	slPatched(t, "MAMenu")
	if err := e.s.SetEnabled(e.ctx(t), "ActsSlick", false); err != nil {
		t.Fatal(err)
	}
	slUntil(t, 10*time.Second, "Slick's MAMenu patch gone with its provider", nil, func() bool { return !slackc.Patched("MAMenu") })
	native("after the only menu plugin stopped")
}

// maHide lays the root menu out without Mark unread, as Better Message
// Menu can.
var maHide = maPlugin("HideSlick", `[]`, `
  start() {
    this.api.patchComponent("MAMenu", (Original) => (props) => {
      if (props.autoClogProps?.uiComponentName !== "message_actions_menu" || !Array.isArray(props.actions)) return React.createElement(Original, props);
      return React.createElement(Original, { ...props, actions: props.actions.map((g) => g.filter((a) => a.id !== "mark_unread")).filter((g) => g.length) });
    });
  }`)

// maHoldJS blocks the plugins' JS thread until release is called.
func maHoldJS(t *testing.T, e *slEnv) (release func()) {
	t.Helper()
	rt, err := e.tt.JS()
	if err != nil {
		t.Fatal(err)
	}
	held, done := make(chan struct{}), make(chan struct{})
	go func() {
		_ = rt.Do(func(*jsrt.Ctx) error {
			close(held)
			<-done
			return nil
		})
	}()
	<-held
	var once sync.Once
	release = func() { once.Do(func() { close(done) }) }
	t.Cleanup(release)
	return release
}

// While a message menu waits for the plugin's layout (maHold), its keys
// wait too, then go to the rows the plugin laid out, as Slack's menu,
// drawn patched at once, has them: U does nothing in a menu without Mark
// unread. When the layout doesn't come in time, the card draws Clack's
// rows and they take the key.
func TestMessageMenuKeysWaitForPluginLayout(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "HideSlick", maHide)
	slPatched(t, "MAMenu")
	t0 := time.Now()
	marks := e.api.called("conversations.mark")

	release := maHoldJS(t, e)
	m := NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
	d := &maDriver{t: t, h: e.h, m: m, now: t0} // the clock stands: the hold lasts
	d.frame()
	d.frame()
	if laidOut(m) {
		t.Fatal("the card drew before the plugin's layout")
	}
	d.key("U", 0)
	if m.Dismissed() || e.api.called("conversations.mark") != marks {
		t.Fatal("U ran Clack's Mark unread while the card waited")
	}
	release()
	d.until("the plugin's layout", func() bool { return maFrom(m) && laidOut(m) })
	d.settle()
	if slIndex(m.items, "mark_unread") >= 0 {
		t.Fatalf("rows %s: the plugin hides Mark unread", maKeys(m.items))
	}
	if e.api.called("conversations.mark") != marks || m.Dismissed() {
		t.Errorf("U pressed during the hold marked the message unread (closed %v); the plugin's menu has no Mark unread", m.Dismissed())
	}

	// a layout that doesn't come within maHold: Clack's rows, U marks
	release = maHoldJS(t, e)
	m = NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
	d = &maDriver{t: t, h: e.h, m: m, now: t0}
	d.frame()
	d.frame()
	d.key("U", 0)
	d.now = t0.Add(maHold + time.Millisecond)
	d.frame()
	d.frame()
	release()
	d.until("U marking once the hold ran out", func() bool { return e.api.called("conversations.mark") > marks })
}

// A pick the plugin answers after maStale doesn't run its row: behind a
// busy JS thread, Copy link would land on the clipboard long after the
// click, over whatever the user copied since. A prompt answer still runs
// it.
func TestMessageMenuStalePickDoesNotRun(t *testing.T) {
	prev := maStale
	maStale = 150 * time.Millisecond
	t.Cleanup(func() { maStale = prev })
	e := slNewEnvIn(t, "")
	e.loadCode(t, "PassSlick", maPass)
	slPatched(t, "MAMenu")

	d := maOpen(t, e, NewMessageMenu(e.h, image.Pt(900, 200), maMsg), "the menu")
	release := maHoldJS(t, e)
	d.clickRow("copy_link")
	time.Sleep(3 * maStale)
	release()
	if v, ok := d.answered(); ok && v != maPassed {
		t.Fatalf("answer %d, want passed", v)
	}
	d.settle()
	if len(d.clip) != 0 {
		t.Errorf("a pick answered %v late copied %q", 3*maStale, d.clip)
	}

	d = maOpen(t, e, NewMessageMenu(e.h, image.Pt(900, 200), maMsg), "the menu again")
	d.clickRow("copy_link")
	d.until("a prompt pick's link on the clipboard", func() bool { return len(d.clip) > 0 })
}

// A pick no runtime answers (none, or it closed first) passes: no plugin
// is there to take it, so Clack's row runs, once. A handler that threw
// (or was interrupted) took it.
func TestMAPickVerdicts(t *testing.T) {
	h := &tzHost{th: theme.New(false)}
	ran := 0
	row := func() *MenuItem { return &MenuItem{Do: func() { ran++ }} }
	pick := func(truthy, threw bool, err error) *maPick {
		p := &maPick{h: h, id: "mark_unread", nat: row(), at: time.Now()}
		p.answer(truthy, threw, err)
		p.Layout(maGtx())
		p.Layout(maGtx())
		return p
	}
	if p := pick(false, false, slackc.ErrNoRuntime); ran != 1 || !p.Dismissed() {
		t.Errorf("no runtime: the row ran %d times, done %v", ran, p.Dismissed())
	}
	if p := pick(false, false, jsrt.ErrClosed); ran != 2 || !p.Dismissed() {
		t.Errorf("a closed runtime: the row ran %d times, done %v", ran-1, p.Dismissed())
	}
	if p := pick(false, true, nil); ran != 2 || !p.Dismissed() {
		t.Errorf("a handler that threw: the row ran, done %v", p.Dismissed())
	}
	if p := pick(true, false, nil); ran != 2 || !p.Dismissed() {
		t.Errorf("a truthy answer: the row ran, done %v", p.Dismissed())
	}
}

// MAMenu's targetBounds is Slack's DOMRect of the trigger in CSS px: the
// ⋮ button's window rect over the window's px per dp (2 here), on MAMenu
// and in the context a pick hands onSelectAction.
func TestMessageMenuTargetBoundsCSSPx(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "PassSlick", maPass) // MAMenu renders through JS
	e.loadCode(t, "ActsSlick", maActs)
	slPatched(t, "MAMenu")
	m := NewMessageMenuOpts(e.h, image.Pt(900, 200), maMsg, MessageMenuOpts{Trigger: image.Rect(880, 180, 908, 208)})
	d := &maDriver{t: t, h: e.h, m: m, scale: 2}
	d.frame()
	d.frame()
	d.until("the MAMenu tree", func() bool { return maFrom(m) && laidOut(m) })
	const want = `"targetBounds":{"bottom":104,"height":14,"left":440,"right":454,"top":90,"width":14,"x":440,"y":90}`
	if raw := e.rec.raw("MAMenu"); !strings.Contains(raw, want) {
		t.Errorf("MAMenu's targetBounds, want %s in %s", want, raw)
	}
	if c := m.maContext()["targetBounds"]; !reflect.DeepEqual(c, rectProps(image.Rect(880, 180, 908, 208), 2)) {
		t.Errorf("a pick's context targetBounds %v", c)
	}
}
