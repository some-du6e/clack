package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
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

// typeKeys types s into whatever has key focus the way a keyboard does:
// a press, the text, a release per character.
func (e *svEnv) typeKeys(s string) {
	for _, r := range s {
		n := key.Name(strings.ToUpper(string(r)))
		if r == ' ' {
			n = key.NameSpace
		}
		e.rt.Queue(key.Event{Name: n, State: key.Press}, key.EditEvent{Text: string(r)}, key.Event{Name: n, State: key.Release})
		e.frame()
	}
}

// HaikuWarning with ChatTabs (its renderer and main half, two tabs open
// so the tab bar wires Ctrl+W through before-input-event): typing a haiku
// in the channel composer and pressing Enter opens HaikuWarning's dialog
// in document.body, like Slick on Slack. Clack cleared the composer and
// the send never settled.
func TestSlickViewHaikuWarningWithChatTabs(t *testing.T) {
	e := newSV(t)
	e.install("ChatTabs", nil)
	e.install("HaikuWarning", nil)
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

	// what the composer asks and what JS answers
	type scCall struct {
		at   time.Duration
		m    composer.SendCheck
		done chan bool
	}
	var calls []*scCall
	var callsMu sync.Mutex
	tStart := time.Now()
	e.frame() // syncHooks installs taut's check
	if composer.SendChecks == nil {
		t.Fatal("the composer has no send check with HaikuWarning on")
	}
	orig := composer.SendChecks
	composer.SendChecks = func(m composer.SendCheck, done func(bool)) {
		c := &scCall{at: time.Since(tStart), m: m, done: make(chan bool, 1)}
		callsMu.Lock()
		calls = append(calls, c)
		callsMu.Unlock()
		orig(m, func(ok bool) { c.done <- ok; done(ok) })
	}
	t.Cleanup(func() { composer.SendChecks = orig })
	probe(`const orig = __taut.sendCheck; globalThis.__scLog = []
		__taut.sendCheck = async (c) => {
			const e = { t0: Date.now(), c: JSON.stringify(c).slice(0, 400) }
			__scLog.push(e)
			try { const r = await orig(c); e.r = r; return r } catch (x) { e.err = String(x); throw x } finally { e.t1 = Date.now() }
		}`)
	defer func() {
		t.Logf("route %s; JS send checks: %s", e.a.router.Current(), probe(`return __scLog.map((e) => ({ ...e, ms: e.t1 ? e.t1 - e.t0 : 'pending' }))`))
		callsMu.Lock()
		for _, c := range calls {
			select {
			case ok := <-c.done:
				t.Logf("composer send check at %v: %q -> %v", c.at, c.m.Text, ok)
			default:
				t.Logf("composer send check at %v: %q still pending", c.at, c.m.Text)
			}
		}
		if len(calls) == 0 {
			t.Logf("the composer never ran a send check")
		}
		callsMu.Unlock()
	}()

	cv := e.a.convos[ttGen+"/"]
	if cv == nil || cv.comp == nil {
		t.Fatal("no #general composer")
	}
	cv.comp.Focus()
	e.frame()
	e.frame()
	e.typeKeys("an old silent pond a frog jumps into the pond splash silence again")
	e.frame()
	t.Logf("route before Enter: %s", e.a.router.Current())
	t0 := time.Now()
	e.rt.Queue(key.Event{Name: key.NameReturn, State: key.Press}, key.Event{Name: key.NameReturn, State: key.Release})
	ok := e.wait(12*time.Second, func() bool {
		return probe(`return !!document.querySelector('dialog.slick-haiku-dialog')`) == "true"
	})
	if !ok {
		t.Logf("body: %s", probe(`return document.body.innerHTML.slice(0, 2000)`))
		direct := probe(`return await Promise.race([__taut.sendCheck({kind:'channel',channelId:'` + ttGen + `',blocks:[]}).then(String), new Promise((r) => setTimeout(() => r('pending'), 1500))])`)
		t.Logf("a direct sendCheck now: %s", direct)
		t.Fatalf("no haiku dialog %v after Enter", time.Since(t0))
	}
	t.Logf("haiku dialog after %v", time.Since(t0))
	var open bool
	_ = json.Unmarshal([]byte(probe(`return document.querySelector('dialog.slick-haiku-dialog').open`)), &open)
	if !open {
		t.Error("the dialog isn't open")
	}
}
