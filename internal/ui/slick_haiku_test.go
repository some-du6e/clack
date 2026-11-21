package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/ui/composer"
)

// svJS evaluates an async function body in the shared runtime with a
// short deadline and reports how long the JS thread took to answer.
func (e *svEnv) svJS(rt *jsrt.Runtime, body string, d time.Duration) (string, time.Duration, error) {
	svJSN++
	name := fmt.Sprintf("__svProbe%d", svJSN)
	t0 := time.Now()
	if _, err := rt.Eval(name+".js", "globalThis."+name+" = async () => { "+body+" }"); err != nil {
		return "", time.Since(t0), err
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	out, err := rt.Call(ctx, name)
	return string(out), time.Since(t0), err
}

var svJSN int

// HaikuWarning with ChatTabs (its renderer and main half, two tabs open
// so the tab bar wires Ctrl+W through before-input-event): typing a haiku
// in the channel composer and pressing Enter opens HaikuWarning's dialog
// in document.body, like Slick on Slack. Clack cleared the composer and
// the send never settled.
func TestSlickViewHaikuWarningWithChatTabs(t *testing.T) {
	t.Run("alone", func(t *testing.T) { haikuThroughComposer(t, 0) })
	t.Run("ChatTabs-one-tab", func(t *testing.T) { haikuThroughComposer(t, 1) })
	t.Run("ChatTabs-two-tabs", func(t *testing.T) { haikuThroughComposer(t, 2) })
}

func haikuThroughComposer(t *testing.T, tabs int) {
	e := newSV(t)
	if tabs > 0 {
		e.install("ChatTabs", nil)
		e.until(8*time.Second, "the tab bar", func() bool { return e.tree(`slick-chat-tabs`, `data-tab-count`) != "" })
	}
	e.install("HaikuWarning", nil)
	if tabs > 1 {
		chatTabsTwoTabs(t, e)
	}
	haikuSend(t, e, tabs > 0)
}

func chatTabsTwoTabs(t *testing.T, e *svEnv) {
	e.until(8*time.Second, "the tab bar", func() bool { return e.tree(`slick-chat-tabs`, `data-tab-count`) != "" })
	// a second tab, so the tab bar asks the main half for Ctrl+W
	e.until(8*time.Second, "ChatTabs' getTemplate on #random's trigger", func() bool {
		return e.tree(`"$key":"ct:`+svRandom+`"`, `"getTemplate":{"$fn"`) != "" && e.a.sbMenu.gts[svRandom].ID != 0
	})
	e.rightClick(svRandom)
	e.until(5*time.Second, "#random's context menu", func() bool { return e.lastMenu(svRandom) != nil })
	if open, ok := menuItem(e.lastMenu(svRandom), "slick-chat-tabs-open"); ok {
		open.Do()
	}
	e.until(8*time.Second, "two tabs", func() bool { return e.tree("slick-chat-tabs__tab", `data-tab-count":2`) != "" })
	e.dismissOverlays()
	e.click(ttGen)
	e.until(5*time.Second, "#general again", func() bool { return string(e.a.router.Current()) == "c/"+ttGen })
}

func haikuSend(t *testing.T, e *svEnv, logKeys bool) {
	rt, err := e.a.Taut().JS()
	if err != nil {
		t.Fatal(err)
	}
	probe := func(body string) string {
		out, d, err := e.svJS(rt, body, 3*time.Second)
		if err != nil || d > time.Second {
			t.Logf("  JS probe answered in %v: %s err=%v", d, out, err)
		}
		return out
	}
	t.Logf("window key listeners: keydown=%v keyup=%v", e.a.Taut().Bridge().WantsEvent("keydown"), e.a.Taut().Bridge().WantsEvent("keyup"))

	// what JS answers: the composer's send runs the checks in one JS call
	// with its thunk (taut's CheckThenDispatch), through __taut.sendCheck
	e.frame() // syncHooks installs taut's check
	if composer.SendChecks == nil {
		t.Fatal("the composer has no send check with HaikuWarning on")
	}
	probe(`const orig = __taut.sendCheck; globalThis.__scLog = []
		__taut.sendCheck = async (c) => {
			const e = { t0: Date.now(), c: JSON.stringify(c).slice(0, 400) }
			__scLog.push(e)
			try { const r = await orig(c); e.r = r; return r } catch (x) { e.err = String(x); throw x } finally { e.t1 = Date.now() }
		}`)
	defer func() {
		t.Logf("route %s; JS send checks: %s", e.a.router.Current(), probe(`return __scLog.map((e) => ({ ...e, ms: e.t1 ? e.t1 - e.t0 : 'pending' }))`))
	}()

	if logKeys {
		probe(`globalThis.__keys = []; window.addEventListener('keydown', (e) => __keys.push(e.key + '@' + (e.target?.localName ?? '?')), true)`)
		defer func() { t.Logf("keydowns JS saw: %s", probe(`return __keys.join(' ')`)) }()
	}
	cv := e.a.convos[ttGen+"/"]
	if cv == nil || cv.comp == nil {
		t.Fatal("no #general composer")
	}
	cb, ok := cv.comp.(composerBox)
	if !ok {
		t.Fatalf("the composer is a %T", cv.comp)
	}
	const haiku = "an old silent pond a frog jumps into the pond splash silence again"
	cb.InsertText(haiku)
	for i := 0; i < 4; i++ {
		e.frame()
	}
	t.Logf("route before Enter: %s", e.a.router.Current())
	t0 := time.Now()
	e.rt.Queue(key.Event{Name: key.NameReturn, State: key.Press}, key.Event{Name: key.NameReturn, State: key.Release})
	opened := e.wait(12*time.Second, func() bool {
		return probe(`return !!document.querySelector('dialog.slick-haiku-dialog')`) == "true"
	})
	if !opened {
		t.Logf("body: %s", probe(`return document.body.innerHTML.slice(0, 2000)`))
		direct := probe(`return await Promise.race([__taut.sendCheck({kind:'channel',channelId:'` + ttGen + `',blocks:[]}).then(String), new Promise((r) => setTimeout(() => r('pending'), 1500))])`)
		t.Logf("a direct sendCheck now: %s", direct)
		t.Fatalf("no haiku dialog %v after Enter", time.Since(t0))
	}
	t.Logf("haiku dialog after %v", time.Since(t0))
	var st struct {
		Open, Focused bool
	}
	_ = json.Unmarshal([]byte(probe(`const d = document.querySelector('dialog.slick-haiku-dialog')
		return { open: d.open, focused: d.contains(document.activeElement) }`)), &st)
	if !st.Open || !st.Focused {
		t.Errorf("the dialog: %+v; want open with its Keep editing button focused", st)
	}
	if n := probe(`return __scLog.length`); n != "1" {
		t.Fatalf("the send ran %s send checks, want 1", n)
	}
	// the page under it is modal: Escape goes to the dialog (cancel, close),
	// HaikuWarning settles false and the message goes back to the composer
	for i := 0; i < 3; i++ {
		e.frame()
	}
	e.rt.Queue(key.Event{Name: key.NameEscape, State: key.Press}, key.Event{Name: key.NameEscape, State: key.Release})
	closed := e.wait(5*time.Second, func() bool {
		return probe(`return !document.querySelector('dialog.slick-haiku-dialog')`) == "true"
	})
	if !closed {
		t.Fatal("Escape didn't close the dialog")
	}
	// no frames from here: the held-back haiku goes back into the composer
	// at its next layout and becomes #general's draft, which the next
	// subtest's composer would start with
	settled := false
	for dl := time.Now().Add(5 * time.Second); !settled && time.Now().Before(dl); time.Sleep(20 * time.Millisecond) {
		settled = probe(`return !!__scLog[0]?.t1`) == "true"
	}
	if !settled {
		t.Error("the send check didn't settle after Escape")
	} else if r := probe(`return __scLog[0].r ?? null`); r != "false" {
		t.Errorf("the send check settled %s after Escape (Keep editing), want false", r)
	}
	// and the haiku stayed unsent: a send puts its pending copy in at once
	time.Sleep(200 * time.Millisecond)
	if tl := e.ca.S.Timeline(ttGen); tl != nil {
		for _, m := range tl.Msgs {
			if m.Text == haiku {
				t.Errorf("the haiku went out after Keep editing (ts %s)", m.TS)
			}
		}
	}
}
