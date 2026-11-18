package popups

// When a message menu and its submenus wait for a plugin's layout
// (maHold), and what their keys do meanwhile. Plugins are written in the
// shape Slick's build:plugin writes; the JS thread is held (maHoldJS) and
// the frames' clock stands still where a test needs a wait that only a
// tree can end.

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/key"
)

// maOrgPass patches Organize's component the way Better Message Menu does:
// its Original, as it was.
var maOrgPass = maPlugin("OrgPassSlick", `[]`, `
  start() {
    this.api.patchComponent("OrganizeMessageActionsSubmenu", (Original) => (props) => React.createElement(Original, props));
  }`)

// maOrgTake patches Organize's component to hand its MAMenu an
// onSelectAction of its own, which takes Pin and passes the rest on.
var maOrgTake = maPlugin("OrgTakeSlick", `[]`, `
  start() {
    globalThis.__orgTook = [];
    this.api.patchComponent("OrganizeMessageActionsSubmenu", (Original) => (props) => {
      if (typeof props.onSelectAction !== "function") return React.createElement(Original, props);
      return React.createElement(Original, { ...props, onSelectAction: (event, action, context) => {
        globalThis.__orgTook.push(action?.id);
        if (action?.id === "pin") return true;
        return props.onSelectAction(event, action, context);
      } });
    });
  }`)

// maOrgEmpty draws an empty box for Organize's component (its MAMenu
// never lays out) and patches MAMenu with a pass-through.
var maOrgEmpty = maPlugin("OrgEmptySlick", `[]`, `
  start() {
    this.api.patchComponent("OrganizeMessageActionsSubmenu", () => () => React.createElement("div", { className: "org-replaced" }));
    this.api.patchComponent("MAMenu", (Original) => (props) => React.createElement(Original, props));
  }`)

// With only Slick's own MAMenu patch (a message action provider, and no
// plugin patching MAMenu), Organize draws at once: that patch adds actions
// only to a menu with autoClogProps, the root, so nothing can change the
// submenu's rows and waiting a JS round trip for them only delays them.
// A pick made before its MAMenu's tree comes asks the root's
// onSelectAction, which Slick wraps with the same providers, and runs
// once when that passes it on.
func TestMessageMenuSubmenuDrawsAtOnceWithOnlyProviders(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "ActsSlick", maActs)
	slPatched(t, "MAMenu")
	d := maOpen(t, e, NewMessageMenu(e.h, image.Pt(900, 200), maMsg), "the menu")
	release := maHoldJS(t, e)
	d.now = time.Now() // the clock stands: only a tree could end a hold
	d.clickRow("organize_actions_menu")
	s := d.m.sub
	if s == nil || !laidOut(s) {
		t.Fatal("Organize waited for JS although only Slick's own patch is on MAMenu")
	}
	pins := e.api.called("pins.add")
	d.clickSub("pin")
	if _, ok := maLastAnswer(e.h); !ok {
		t.Fatal("Organize > Pin ran without asking the root's onSelectAction")
	}
	if e.api.called("pins.add") != pins {
		t.Fatal("Organize > Pin pinned before the root's onSelectAction answered")
	}
	release()
	d.now = time.Time{}
	if v, ok := d.answered(); !ok || v != maPassed {
		t.Fatalf("Organize > Pin: answer %d (shown %v), want passed", v, ok)
	}
	d.until("Pin pinning", func() bool { return e.api.called("pins.add") > pins })
	d.settle()
	if n := e.api.called("pins.add") - pins; n != 1 {
		t.Errorf("Organize > Pin pinned %d times, want 1", n)
	}
}

// A plugin's own MAMenu patch can change Organize's rows (Better Message
// Menu moves rows into it), so Organize waits for its MAMenu's tree, and
// the menu's keys with it, which then go to it; a bare Escape closes it
// at once.
func TestMessageMenuSubmenuHoldsForPluginPatch(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "ActsSlick", maActs)
	e.loadCode(t, "SubDecideSlick", maSubDecide)
	slPatched(t, "MAMenu")
	d := maOpen(t, e, NewMessageMenu(e.h, image.Pt(900, 200), maMsg), "the menu")

	release := maHoldJS(t, e)
	d.now = time.Now()
	d.clickRow("organize_actions_menu")
	s := d.m.sub
	if s == nil {
		t.Fatal("Organize didn't open")
	}
	if laidOut(s) {
		t.Fatal("Organize drew before its MAMenu's tree, which a plugin's patch can change")
	}
	d.key(key.NameDownArrow, 0) // opened by a click, as by a key: its first row selected
	if n := len(d.m.ma.queued); n != 1 {
		t.Fatalf("%d keys waiting while Organize waits for its tree, want the ↓", n)
	}
	release()
	d.now = time.Time{}
	d.until("Organize's tree, and the ↓ that waited for it", func() bool { return maFrom(s) && laidOut(s) && len(d.m.ma.queued) == 0 })

	d.key(key.NameLeftArrow, 0) // Organize closes
	release = maHoldJS(t, e)
	d.now = time.Now()
	d.clickRow("organize_actions_menu")
	if d.m.sub == nil || laidOut(d.m.sub) {
		t.Fatal("Organize, opened again, didn't wait for its tree")
	}
	d.key(key.NameEscape, 0)
	if d.m.sub != nil {
		t.Error("a bare Escape waited with Organize for its tree")
	}
	if d.m.Dismissed() {
		t.Error("a bare Escape in Organize closed the whole menu")
	}
	release()
	d.now = time.Time{}
}

// An Escape pressed after keys that wait for the plugin's layout waits
// behind them, as Slack's menu, drawn at once, takes keys in order: U
// then Escape marks the message unread (U closes the menu).
func TestMessageMenuEscapeWaitsBehindQueuedKeys(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "DecideSlick", maDecide)
	slPatched(t, "MAMenu")
	marks := e.api.called("conversations.mark")
	release := maHoldJS(t, e)
	m := NewMessageMenu(e.h, image.Pt(900, 200), maMsg)
	d := &maDriver{t: t, h: e.h, m: m, now: time.Now()} // the clock stands: the hold lasts
	d.frame()
	d.frame()
	d.key("U", 0)
	d.key(key.NameEscape, 0)
	if m.Dismissed() {
		t.Fatal("Escape closed the menu ahead of the U pressed before it")
	}
	release()
	d.now = time.Time{}
	d.until("U marking unread", func() bool { return e.api.called("conversations.mark") > marks })
	if !m.Dismissed() {
		t.Error("the menu stayed open after U")
	}
}

// A submenu whose MAMenu never lays out (its component's patch draws an
// empty box; opened again within the 5 s its tree stays mounted, so not even
// for a frame) holds the menu's keys for maHold, not for as long as it's
// open.
func TestMessageMenuSubmenuHoldEnds(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "OrgEmptySlick", maOrgEmpty)
	slPatched(t, "MAMenu", "OrganizeMessageActionsSubmenu")
	d := maOpen(t, e, NewMessageMenu(e.h, image.Pt(900, 200), maMsg), "the menu")
	d.clickRow("organize_actions_menu")
	if d.m.sub == nil {
		t.Fatal("Organize didn't open")
	}
	d.settle() // its component's tree comes back
	d.key(key.NameLeftArrow, 0)
	if d.m.sub != nil {
		t.Fatal("← didn't close Organize")
	}
	d.clickRow("organize_actions_menu")
	if d.m.sub == nil {
		t.Fatal("Organize didn't open again")
	}
	d.key(key.NameDownArrow, 0)
	d.until("the ↓ Organize's wait held to run", func() bool { return len(d.m.ma.queued) == 0 })
}

// A patch on Organize's component draws its MAMenu inside its own tree,
// so the submenu's MAMenu is mounted once there, not first outside it
// while that tree is on its way (a second JS root rendering for 5 s).
func TestMessageMenuSubmenuComponentMountsMAMenuOnce(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "ActsSlick", maActs)
	e.loadCode(t, "OrgPassSlick", maOrgPass)
	slPatched(t, "MAMenu", "OrganizeMessageActionsSubmenu")
	d := maOpen(t, e, NewMessageMenu(e.h, image.Pt(900, 200), maMsg), "the menu")
	if n := e.rec.count("MAMenu"); n != 1 {
		t.Fatalf("%d MAMenu roots for the menu, want 1", n)
	}
	s := d.openSub("organize_actions_menu")
	if !maFrom(s) {
		t.Fatal("Organize's MAMenu tree didn't arrive")
	}
	d.settle()
	if n := e.rec.count("MAMenu"); n != 2 {
		t.Errorf("%d MAMenu roots with Organize open, want 2: the root's and Organize's", n)
	}
}

// A patch on Organize's component that hands its MAMenu an onSelectAction
// of its own decides Organize's picks, as Slack's
// OrganizeMessageActionsSubmenu passes its onSelectAction to its MAMenu:
// here it takes Pin, so Clack's Pin doesn't pin.
func TestMessageMenuSubmenuComponentSelectAction(t *testing.T) {
	e := slNewEnvIn(t, "")
	e.loadCode(t, "OrgTakeSlick", maOrgTake)
	slPatched(t, "OrganizeMessageActionsSubmenu")
	d := maOpen(t, e, NewMessageMenu(e.h, image.Pt(900, 200), maMsg), "the menu")
	d.clickRow("organize_actions_menu")
	d.until("Organize's component tree", func() bool {
		return len(e.rec.treesOf("OrganizeMessageActionsSubmenu")) > 0 && d.m.sub != nil && laidOut(d.m.sub)
	})
	d.settle()
	pins := e.api.called("pins.add")
	d.clickSub("pin")
	if v, ok := d.answered(); !ok || v != maTaken {
		t.Fatalf("Organize > Pin: answer %d (shown %v), want taken by the component's onSelectAction", v, ok)
	}
	if got := e.jsString(t, "globalThis.__orgTook"); got != "pin" {
		t.Errorf("the component's onSelectAction saw %q, want pin", got)
	}
	if e.api.called("pins.add") != pins {
		t.Error("Pin pinned although Organize's component took it")
	}
}
