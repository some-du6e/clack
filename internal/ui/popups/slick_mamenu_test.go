package popups

// The message menus' MAMenu boundary (actions_mamenu.go) with plugins in
// the shape Slick's build:plugin writes, written here, so these run
// anywhere (no SLICK_PLUGINS_DIR): Slick #473's api.menu.addMessageActions
// in the More actions menu, the message context menu and a search result's
// More actions; an old build's MAMenu patch (ChatTabs before #473); and
// rows plugin CSS lays out inline (Better Message Menu's big buttons).

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/richclip"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// maPlugin is a plugin file as Slick's build:plugin writes one, body its
// class body.
func maPlugin(id, required, body string) string {
	return `// slick-plugin {"id":"` + id + `"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class P extends import_slick.SlickPlugin {
  static id = "` + id + `";
  static pluginName = "` + id + `";
  static description = "test";
  static requiredAPIs = ` + required + `;
` + body + `
}
return P;
});
`
}

// maActs adds "Act one" (and "Act two", icon threads) to the menus of
// slMsgTS and nothing to other messages' menus.
var maActs = maPlugin("ActsSlick", `["menu"]`, `
  start() {
    globalThis.__actsPicked = [];
    this.api.menu.addMessageActions(({ channelId, ts }) => channelId === "`+slGen+`" && ts === "`+slMsgTS+`" ? [
      { id: "acts_one", name: "Act one", onSelect: () => globalThis.__actsPicked.push("one") },
      { id: "acts_two", name: "Act two", icon: "threads", onSelect: () => globalThis.__actsPicked.push("two") },
    ] : []);
  }`)

var maMsg = core.Message{Channel: slGen, TS: slMsgTS, User: slBob, Text: "a synthetic message"}

// maGtx is a frame context for activating rows.
func maGtx() layout.Context {
	return layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800)), Now: time.Now()}
}

// maWait draws m until its rows have key (or don't, want false).
func maWait(t *testing.T, m *Menu, key string, want bool, what string) {
	t.Helper()
	slUntil(t, 10*time.Second, what, slFrame(m), func() bool { return (slIndex(m.items, key) >= 0) == want })
}

func maKeys(items []MenuItem) string {
	var ks []string
	for _, it := range items {
		if it.Separator {
			ks = append(ks, "|")
			continue
		}
		ks = append(ks, it.Key)
	}
	return strings.Join(ks, " ")
}

// maIDs is the rows' Slack action ids, "|" for separators.
func maIDs(items []MenuItem) string {
	var ks []string
	for i := range items {
		if items[i].Separator {
			ks = append(ks, "|")
			continue
		}
		ks = append(ks, maRowID(&items[i]))
	}
	return strings.Join(ks, " ")
}

// maApplied draws m until its MAMenu tree arrived and was applied, then a
// few frames more.
func maApplied(t *testing.T, m *Menu, what string) {
	t.Helper()
	f := slFrame(m)
	slUntil(t, 10*time.Second, what, f, func() bool { return maFrom(m) })
	for k := 0; k < 5; k++ {
		f()
		Pump()
		slackc.Pump()
	}
}

func TestMessageMenuAddedActions(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "ActsSlick", maActs)
	slPatched(t, "MAMenu")

	m := NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
	maWait(t, m, "acts_one", true, "Act one in the More actions menu")
	i := slIndex(m.items, "acts_one")
	// a group of their own after the menu's rows
	if i < 1 || !m.items[i-1].Separator || slIndex(m.items, "acts_two") != i+1 || i+2 != len(m.items) {
		t.Fatalf("rows %s", maKeys(m.items))
	}
	one, two := m.items[i], m.items[i+1]
	if one.Label != "Act one" || maClass(&one) != "p-message_actions_menu__acts_one" || two.Icon != "threads" {
		t.Errorf("added rows %+v %+v", one, two)
	}
	for _, k := range []string{"mark_unread", "copy_link", "organize"} {
		if slIndex(m.items, k) < 0 {
			t.Errorf("native row %s gone: %s", k, maKeys(m.items))
		}
	}
	if !strings.Contains(e.rec.raw("MAMenu"), `"name":"Act one"`) {
		t.Error("MAMenu's tree doesn't carry the action")
	}

	// picking it runs its onSelect, then onTriggerClose closes the menu
	// (the row's pick alone, without Menu closing itself first as a click
	// does)
	maRun(one, maGtx())
	slUntil(t, 10*time.Second, "Act one's onSelect", nil, func() bool { return e.jsString(t, "globalThis.__actsPicked") == "one" })
	slUntil(t, 10*time.Second, "onTriggerClose closing the menu", nil, m.Dismissed)

	// a native row goes through Slick's onSelectAction, which passes it
	// on: Mark unread marks, once
	m = NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
	d := maOpen(t, e, m, "Act one again")
	before := e.api.called("conversations.mark")
	d.clickRow("mark_unread")
	d.until("Mark unread marking the conversation", func() bool { return e.api.called("conversations.mark") > before })
	d.settle()
	if n := e.api.called("conversations.mark") - before; n != 1 {
		t.Errorf("Mark unread marked %d times, want once", n)
	}
	if v, ok := maLastAnswer(e.h); ok && v != maPassed {
		t.Errorf("Slick's onSelectAction answered %d for Mark unread", v)
	}
	if got := e.jsString(t, "globalThis.__actsPicked"); got != "one" {
		t.Errorf("picked %q after Mark unread", got)
	}

	// the right-click menu (MessageActionsContextMenu) has them too
	cm := NewMessageContextMenu(e.h, image.Pt(400, 300), maMsg, InChannel, MessageMenuOpts{})
	maWait(t, cm, "acts_one", true, "Act one in the message context menu")
	if slIndex(cm.items, "add_reaction") < 0 {
		t.Errorf("context menu rows %s", maKeys(cm.items))
	}
	// and a search result's More actions
	sm := maSearchMenu(e.h, maMsg, image.Rect(900, 200, 900, 200))
	maWait(t, sm, "acts_one", true, "Act one in a search result's More actions")

	// a provider giving nothing adds nothing
	other := core.Message{Channel: slGen, TS: "1700000001.000100", User: slAlice, Text: "first"}
	om := NewMessageMenu(e.h, image.Pt(900, 200), other)
	maApplied(t, om, "the other message's MAMenu tree")
	if slIndex(om.items, "acts_one") >= 0 || om.items[len(om.items)-1].Separator {
		t.Errorf("rows for a message without actions: %s", maKeys(om.items))
	}
	if maIDs(om.items) != maIDs(maNative(om)) {
		t.Errorf("rows %s, natively %s", maIDs(om.items), maIDs(maNative(om)))
	}

	// stopping the plugin takes its actions away, and with its provider
	// Slick's MAMenu patch: the next menu is Clack's alone, drawn in its
	// first frame with no MAMenu render
	if err := e.s.SetEnabled(e.ctx(t), "ActsSlick", false); err != nil {
		t.Fatal(err)
	}
	slUntil(t, 10*time.Second, "Slick's MAMenu patch gone", nil, func() bool { return !slackc.Patched("MAMenu") })
	roots := e.rec.count("MAMenu")
	m = NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
	slFrame(m)()
	if !laidOut(m) || slIndex(m.items, "acts_one") >= 0 || slIndex(m.items, "mark_unread") < 0 {
		t.Errorf("after the plugin stopped: drawn %v, rows %s", laidOut(m), maKeys(m.items))
	}
	if n := e.rec.count("MAMenu") - roots; n != 0 {
		t.Errorf("%d MAMenu renders after the last provider stopped", n)
	}
}

// ChatTabs before Slick #473 found the message menu's renderer with a
// filter on its source and added "Open in new Slick tab" through its
// actions and onSelectAction: Clack's MAMenu is what it finds.
func TestMessageMenuOldPatch(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "OldTabsSlick", maPlugin("OldTabsSlick", `["redux", "elements"]`, `
  start() {
    globalThis.__oldPicked = [];
    this.api.patchComponent({
      filter: (candidate) => {
        const render = candidate?.render ?? candidate;
        return typeof render === "function" && String(render).includes("renderActionGroup") && String(render).includes("dispatchMessageAction");
      },
    }, (Original) => (props) => {
      if (props.autoClogProps?.uiComponentName !== "message_actions_menu" || !Array.isArray(props.actions)) return React.createElement(Original, props);
      return React.createElement(Original, {
        ...props,
        actions: [[{ id: "old-tabs-open-thread", name: "Open in new Slick tab", icon: "threads", "data-qa": "old-tabs-open-thread" }], ...props.actions],
        onSelectAction: (event, action, context) => {
          if (action?.id === "old-tabs-open-thread") { globalThis.__oldPicked.push(context?.ts ?? "?"); return true; }
          return props.onSelectAction?.(event, action, context);
        },
      });
    });
  }`))
	slPatched(t, "MAMenu")
	m := NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
	maWait(t, m, "old-tabs-open-thread", true, "the old build's row")
	if i := slIndex(m.items, "old-tabs-open-thread"); i != 0 || !m.items[1].Separator || m.items[0].Label != "Open in new Slick tab" {
		t.Fatalf("rows %s", maKeys(m.items))
	}
	m.activate(maGtx(), 0)
	slUntil(t, 10*time.Second, "its onSelectAction", nil, func() bool { return e.jsString(t, "globalThis.__oldPicked") == slMsgTS })
	slUntil(t, 10*time.Second, "its answer", nil, func() bool { v, ok := maLastAnswer(e.h); return !ok || v == maTaken })
	// the rest still go on to Clack's natives, once
	m = NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
	d := maOpen(t, e, m, "the old build's row again")
	before := e.api.called("conversations.mark")
	d.clickRow("mark_unread")
	d.until("Mark unread", func() bool { return e.api.called("conversations.mark") > before })
	d.settle()
	if n := e.api.called("conversations.mark") - before; n != 1 || e.jsString(t, "globalThis.__oldPicked") != slMsgTS {
		t.Errorf("Mark unread marked %d times (want once), the plugin picked %q", n, e.jsString(t, "globalThis.__oldPicked"))
	}
}

// A patch reorders, renames and hides the menu's actions, moves rows
// into Organize (out of the root's actions, into those of Organize's own
// MAMenu, which Better Message Menu passes in a context), and gives two a
// class its CSS lays out side by side, as Better Message Menu does; the
// menu draws exactly that.
func TestMessageMenuReorganized(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "TilesSlick", maPlugin("TilesSlick", `["menu"]`, `
  start() {
    this.api.setStyle(".p-message_actions_menu .c-menu_item__li:has(.tile-big) { display: inline-flex; width: calc(100% / 2); box-sizing: border-box; vertical-align: top; }" +
      " .p-message_actions_menu .c-menu_item__button.tile-big { flex-direction: column; align-items: center; gap: 4px; padding: 10px 6px; }" +
      " .p-message_actions_menu .tile-big .c-menu_item__shortcut { display: none; }");
    let moved = [];
    this.api.patchComponent("MAMenu", (Original) => (props) => {
      if (props.isSubmenu && Array.isArray(props.actions) && props.actions.flat().some((a) => a.id === "pin"))
        return React.createElement(Original, { ...props, actions: [[...moved, ...props.actions.flat()]] });
      if (props.autoClogProps?.uiComponentName !== "message_actions_menu" || !Array.isArray(props.actions)) return React.createElement(Original, props);
      const flat = props.actions.flat();
      const take = (id) => flat.find((a) => a.id === id);
      const big = ["copy_link", "copy_text"].map(take).filter(Boolean).map((a) => ({ ...a, name: a.id === "copy_text" ? "Copy" : a.name, className: "tile-big" }));
      moved = ["later", "mark_unread"].map(take).filter(Boolean);
      const main = flat.filter((a) => !["copy_link", "copy_text", "later", "mark_unread", "remind"].includes(a.id));
      return React.createElement(Original, { ...props, actions: [big, main], slickMessageActionsHandled: true });
    });
  }`))
	slPatched(t, "MAMenu")
	m := NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
	maApplied(t, m, "the reorganised menu")
	got := maIDs(m.items)
	if !strings.HasPrefix(got, "copy_link copy_text | subscribe organize_actions_menu connect_to_apps_actions_menu") {
		t.Fatalf("rows %s (natively %s)", got, maIDs(maNative(m)))
	}
	if strings.Contains(got, "mark_unread") || strings.Contains(got, "remind") || strings.Contains(got, "later") {
		t.Errorf("moved or hidden rows still at the top level: %s", got)
	}
	// Organize's own MAMenu has the moved rows
	{
		d := newMADriver(t, e.h, m)
		d.until("the card", func() bool { return laidOut(m) })
		org := d.openSub("organize_actions_menu")
		if sub := maIDs(org.items); sub != "later mark_unread pin" {
			t.Errorf("Organize submenu %s", sub)
		}
		if i := maIndex(org.items, "mark_unread"); i < 0 || org.items[i].Label != "Mark unread" || (org.items[i].Do == nil && org.items[i].DoCtx == nil) {
			t.Errorf("Mark unread in Organize: %s", maIDs(org.items))
		}
		m.closeSub()
	}
	if c := m.items[1]; c.Label != "Copy" || !strings.Contains(maClass(&c), "tile-big") || !strings.Contains(maClass(&c), "p-message_actions_menu__copy_text") {
		t.Errorf("renamed row %+v", c)
	}
	// the two big rows side by side, half the card each, icon over label
	f := slFrame(m)
	for k := 0; k < 3; k++ {
		f()
	}
	rows := m.RowRects()
	a, b := rows[0], rows[1]
	if a.Min.Y != b.Min.Y || a.Max.X != b.Min.X || a.Dx() != 150 || b.Dx() != 150 {
		t.Errorf("big rows %v %v: want side by side, 150px each", a, b)
	}
	// padding 10 + the icon's 28px line + 4 + the label's 28px line + 10
	if a.Dy() != 80 {
		t.Errorf("big row %dpx tall, want 80", a.Dy())
	}
	if sep := rows[2]; sep.Min.Y != a.Max.Y || sep.Min.X != rows[3].Min.X || sep.Dx() != 300 {
		t.Errorf("separator %v under the big rows %v", sep, a)
	}

	// picking the tiles through the window's input runs them (the patch
	// passed onSelectAction on: Clack's rows run as they are): Copy link
	// puts the message's link on the clipboard, Copy its text
	open := func(what string) (*Menu, *maDriver) {
		t.Helper()
		m := NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
		return m, maOpen(t, e, m, what)
	}
	m, d := open("the reorganised menu, through the router")
	if r := m.RowRects(); r[0].Dx() != 150 || r[1].Min.X != r[0].Max.X {
		t.Fatalf("tiles %v %v", r[0], r[1])
	}
	d.click(m.RowRects()[0])
	d.until("Copy link's link on the clipboard", func() bool { return len(d.clip) > 0 })
	if link := permalink(e.h, slGen, slMsgTS, ""); d.clip[0] != link {
		t.Errorf("the Copy link tile: clipboard %q (want %q)", d.clip, link)
	}
	if !richclip.Supported() { // else Copy message writes the OS clipboard itself
		m, d = open("the menu for the Copy tile")
		d.click(m.RowRects()[1])
		d.until("Copy's text on the clipboard", func() bool { return len(d.clip) > 0 })
		if d.clip[0] != "a synthetic message" {
			t.Errorf("the Copy tile: clipboard %q", d.clip)
		}
	}
	// Mark unread, moved into Organize, marks
	m, d = open("the menu for Organize")
	d.openSub("organize_actions_menu")
	before := e.api.called("conversations.mark")
	d.clickSub("mark_unread")
	d.until("Mark unread from Organize", func() bool { return e.api.called("conversations.mark") > before })
	d.settle()
	if n := e.api.called("conversations.mark") - before; n != 1 {
		t.Errorf("Mark unread from Organize marked %d times (want once)", n)
	}
	// and Organize's own Pin, by its key from the root menu
	pins := e.api.called("pins.add")
	m, d = open("the menu for P")
	d.key("P", 0)
	d.until("P pinning", func() bool { return e.api.called("pins.add") > pins })
	if !m.Dismissed() {
		t.Error("P left the menu open")
	}
}
