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
	"strings"
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
	"github.com/some-du6e/clack/internal/ui/richclip"
	"github.com/some-du6e/clack/internal/ui/slackc"
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
	clip  []string // what the frames put on the clipboard, in order
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
	gtx := layout.Context{Ops: &d.ops, Source: d.r.Source(), Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800))}
	slackc.Frame(gtx)
	if !d.m.Dismissed() {
		d.m.Layout(gtx)
	}
	for _, p := range maPicks(d.h) {
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
	c := r.Min.Add(r.Max).Div(2)
	p := f32.Pt(float32(c.X), float32(c.Y))
	d.clock += time.Second
	d.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: d.clock},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: d.clock + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: d.clock + 60*time.Millisecond},
	)
	d.frame()
	d.frame()
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

// openSub clicks submenu row id and waits for its card.
func (d *maDriver) openSub(id string) *Menu {
	d.t.Helper()
	d.clickRow(id)
	d.until("the "+id+" submenu", func() bool { return d.m.sub != nil && laidOut(d.m.sub) })
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

// lastPick is the pick the menu showed last.
func (d *maDriver) lastPick() *maPick {
	d.t.Helper()
	ps := maPicks(d.h)
	if len(ps) == 0 {
		d.t.Fatal("no pick was shown")
	}
	return ps[len(ps)-1]
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
// about, takes Copy link (true), throws on Save, answers Get notified with
// a Promise, and hands the rest to the menu's own (falsy).
var maDecide = maPlugin("DecideSlick", `[]`, `
  start() {
    globalThis.__decided = [];
    this.api.patchComponent("MAMenu", (Original) => (props) => {
      if (props.autoClogProps?.uiComponentName !== "message_actions_menu" || typeof props.onSelectAction !== "function") return React.createElement(Original, props);
      return React.createElement(Original, { ...props, onSelectAction: (event, action, context) => {
        globalThis.__decided.push(action?.id + "@" + context?.ts + "/" + event?.type);
        if (action?.id === "copy_link") return true;
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
	answer := func(d *maDriver) maVerdict {
		t.Helper()
		p := d.lastPick()
		d.until("the plugin's answer", func() bool { return p.answered() != maWaiting })
		d.settle()
		return p.answered()
	}

	// Copy link: the plugin takes it, so nothing goes on the clipboard
	m, d := open("the menu")
	d.clickRow("copy_link")
	if v := answer(d); v != maTaken || decided() != "copy_link"+at {
		t.Fatalf("Copy link: answer %d, asked %q", v, decided())
	}
	if len(d.clip) != 0 || !m.Dismissed() {
		t.Errorf("Copy link the plugin took: clipboard %q, closed %v", d.clip, m.Dismissed())
	}

	// Mark unread: passed on, Clack's row runs, once
	m, d = open("the menu again")
	before := e.api.called("conversations.mark")
	d.clickRow("mark_unread")
	if v := answer(d); v != maPassed {
		t.Fatalf("Mark unread: answer %d", v)
	}
	d.until("Mark unread marking", func() bool { return e.api.called("conversations.mark") > before })
	d.settle()
	if n := e.api.called("conversations.mark") - before; n != 1 || !m.Dismissed() {
		t.Errorf("Mark unread marked %d times (want once), closed %v", n, m.Dismissed())
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
	if v := answer(d); v != maTaken || !strings.HasSuffix(decided(), ",later"+at) {
		t.Errorf("Save: answer %d, asked %q", v, decided())
	}
	if e.api.called("saved.add") != saved {
		t.Error("Save ran although the plugin's handler threw")
	}

	// Get notified: a Promise is truthy
	follows := e.api.called("subscriptions.thread.add")
	_, d = open("the menu for Get notified")
	d.clickRow("subscribe")
	if v := answer(d); v != maTaken || !strings.HasSuffix(decided(), ",subscribe"+at) {
		t.Errorf("Get notified: answer %d, asked %q", v, decided())
	}
	if e.api.called("subscriptions.thread.add") != follows {
		t.Error("Get notified ran although the plugin answered a Promise")
	}

	// Organize > Pin asks, then pins, once
	pins := e.api.called("pins.add")
	m, d = open("the menu for Organize")
	d.openSub("organize_actions_menu")
	d.clickSub("pin")
	d.until("Pin pinning", func() bool { return e.api.called("pins.add") > pins })
	d.settle()
	if n := e.api.called("pins.add") - pins; n != 1 || !strings.HasSuffix(decided(), ",pin"+at) || !m.Dismissed() {
		t.Errorf("Organize > Pin: pinned %d times (want once), asked %q, closed %v", n, decided(), m.Dismissed())
	}

	// Organize's P from the root menu is MessageActionsMenu's own key
	// handler (onMenuKeyDown): it pins without asking the plugin
	asked := decided()
	m, d = open("the menu for P")
	d.key("P", 0)
	d.until("P pinning", func() bool { return e.api.called("pins.add") > pins+1 })
	d.settle()
	if n := e.api.called("pins.add") - pins; n != 2 || decided() != asked || !m.Dismissed() {
		t.Errorf("P: pinned %d times (want 2), asked %q (was %q), closed %v", n, decided(), asked, m.Dismissed())
	}
}

// maPass hands every pick on to the menu's own onSelectAction after
// recording it, and moves Mark unread into Organize, as Better Message
// Menu does.
var maPass = maPlugin("PassSlick", `[]`, `
  start() {
    globalThis.__passed = [];
    this.api.patchComponent("MAMenu", (Original) => (props) => {
      if (props.autoClogProps?.uiComponentName !== "message_actions_menu" || !Array.isArray(props.actions)) return React.createElement(Original, props);
      const mu = props.actions.flat().find((a) => a.id === "mark_unread");
      const actions = props.actions
        .map((g) => g.filter((a) => a.id !== "mark_unread").map((a) => a.id === "organize_actions_menu" && mu ? { ...a, submenuActions: [mu, ...a.submenuActions] } : a))
        .filter((g) => g.length);
      return React.createElement(Original, { ...props, actions, onSelectAction: (event, action, context) => {
        globalThis.__passed.push(action?.id);
        return props.onSelectAction(event, action, context);
      } });
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

	// Mark unread, moved into Organize
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
	if n := e.api.called("conversations.mark") - before; n != 1 || !strings.HasSuffix(passed(), ",mark_unread") {
		t.Errorf("Mark unread from Organize: marked %d times, asked %q", n, passed())
	}

	// a reminder preset (Slack's RemindSubmenu): saved with its due date
	saved := e.api.called("saved.add")
	m = menu()
	d = open(m, "the menu for Remind")
	sub := d.openSub("remind")
	if len(sub.items) == 0 {
		t.Fatal("no reminder presets")
	}
	d.click(sub.rows[0])
	d.until("the preset saving the reminder", func() bool { return e.api.called("saved.add") > saved })
	d.settle()
	if n := e.api.called("saved.add") - saved; n != 1 {
		t.Errorf("the preset saved %d times, want once", n)
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
		d.click(m.RowRects()[i])
		d.until(what+": the old row's click", func() bool {
			return strings.Count(e.jsString(t, "globalThis.__oldRows.join(' ')"), "old_accept_delete") == n+1
		})
		if !m.Dismissed() {
			t.Errorf("%s: the menu stayed open", what)
		}
	}
	check("the old row in the More actions menu", false)
	// with a #473 plugin's actions in the same menu (Slick's MAMenu patch)
	e.loadCode(t, "ActsSlick", maActs)
	slPatched(t, "MAMenu")
	check("the old row next to Act one", true)
}
