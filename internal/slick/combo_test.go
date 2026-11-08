package slick

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Plugins that are fine alone can break each other: the acceptance suite
// loads one at a time, so these run real built plugins together.

// probeJS runs an async function body with a short deadline and reports
// how long the JS thread took to answer.
func probeJS(h *harness, body string, d time.Duration) (string, time.Duration, error) {
	rt := h.rt()
	jsMu.Lock()
	jsN++
	name := fmt.Sprintf("__slickProbe%d", jsN)
	jsMu.Unlock()
	src := fmt.Sprintf("globalThis.%s = async () => { const S = globalThis.__slick; const T = globalThis.__taut; %s }", name, body)
	t0 := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	if _, err := rt.Eval(name+".js", src); err != nil {
		return "", time.Since(t0), err
	}
	out, err := rt.Call(ctx, name)
	return string(out), time.Since(t0), err
}

// HaikuWarning's send check with ChatTabs enabled: its renderer (the tab
// bar mounted on ClientPrimaryView, its 250 ms route poll) and its main
// half with closeShortcut wired (before-input-event on the window's keys).
// On Slack the haiku opens HaikuWarning's dialog. Live, Clack never showed
// it: the shell's key forwarding for ChatTabs' window listeners took the
// composer's Enter (TestSlickViewHaikuWarningWithChatTabs in internal/ui
// covers that path); this one keeps the runtime side honest.
func TestHaikuWarningWithChatTabs(t *testing.T) {
	haikuWithChatTabs(t, pluginsDir(t))
}

func haikuWithChatTabs(t *testing.T, dir string) {
	h := newHarness(t)
	h.importFile(filepath.Join(dir, "ChatTabs.js"))
	h.importFile(filepath.Join(dir, "ChatTabs.main.js"))
	h.importFile(filepath.Join(dir, "HaikuWarning.js"))
	h.s.SetRoute(teamID, chGen)
	for _, id := range []string{"ChatTabs", "HaikuWarning"} {
		if err := h.s.SetEnabled(h.ctx(), id, true); err != nil {
			t.Fatal(err)
		}
		if info := h.plugin(id); !info.Running || info.StartError != "" {
			t.Fatalf("%s didn't start: running=%v error=%q", id, info.Running, info.StartError)
		}
	}
	time.Sleep(150 * time.Millisecond)
	h.render(7, "ClientPrimaryView", map[string]any{"$key": "main", "windowId": "main"})
	h.waitTree(t, 7, "ChatTabs' tab layout", func(nodes []any) bool { return strings.Contains(jsonOf(nodes), "slick-chat-tabs-layout") })
	h.js(`await S.callMain('ChatTabs', 'closeShortcut', [true])`)
	// keys through before-input-event, as the shell forwards them
	h.js(`for (const key of ['a', 'Enter']) { window.dispatchEvent(new KeyboardEvent('keydown', { key, cancelable: true })); window.dispatchEvent(new KeyboardEvent('keyup', { key })) }`)
	if out, d, err := probeJS(h, `
		const enc = new Response(new Blob([new TextEncoder().encode('hello stream')]).stream().pipeThrough(new CompressionStream('gzip')))
		const gz = new Uint8Array(await enc.arrayBuffer())
		return await new Response(new Blob([gz]).stream().pipeThrough(new DecompressionStream('gzip'))).text()`, 10*time.Second); err != nil || out != `"hello stream"` {
		t.Errorf("stream round trip: %s %v (%v)", out, err, d)
	} else {
		t.Logf("stream round trip ok in %v", d)
	}

	bodyKids := func() (int, time.Duration, error) {
		out, d, err := probeJS(h, `return document.body.children.length`, 2*time.Second)
		var n int
		_ = json.Unmarshal([]byte(out), &n)
		return n, d, err
	}
	n0, _, _ := bodyKids()

	// a plain message first (decodes the dictionary)
	t0 := time.Now()
	plain := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		plain <- h.tt.SendCheck(ctx, composerCtx(chGen, string(richText("just a normal message"))))
	}()
	select {
	case ok := <-plain:
		t.Logf("plain send check: %v in %v", ok, time.Since(t0))
		if !ok {
			t.Error("blocked a normal message")
		}
	case <-time.After(15 * time.Second):
		t.Errorf("plain send check still pending after 15 s")
		for i := 0; i < 3; i++ {
			n, d, err := bodyKids()
			t.Logf("  JS probe: body kids %d in %v err %v", n, d, err)
		}
	}

	t1 := time.Now()
	done := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		done <- h.tt.SendCheck(ctx, composerCtx(chGen, string(richText("an old silent pond a frog jumps into the pond splash silence again"))))
	}()
	deadline := time.Now().Add(10 * time.Second)
	opened := false
	for time.Now().Before(deadline) {
		n, d, err := bodyKids()
		if d > 500*time.Millisecond || err != nil {
			t.Logf("  JS probe at %v: body kids %d, answered in %v, err %v", time.Since(t1).Round(time.Millisecond), n, d, err)
		}
		if n > n0 {
			opened = true
			break
		}
		select {
		case ok := <-done:
			t.Errorf("the haiku send check settled without a dialog (%v) after %v", ok, time.Since(t1))
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !opened {
		t.Fatalf("no haiku dialog after %v", time.Since(t1))
	}
	t.Logf("haiku dialog opened after %v", time.Since(t1))
	var tag string
	h.jsInto(`return document.body.lastElementChild?.tagName + ':' + document.body.lastElementChild?.open`, &tag)
	if tag != "DIALOG:true" {
		t.Errorf("body's last child %q, want an open DIALOG", tag)
	}
	// Keep editing: the check settles false and the dialog goes away
	h.js(`[...document.querySelectorAll('.slick-haiku-dialog button')].find((b) => b.type === 'button').click()`)
	select {
	case ok := <-done:
		if ok {
			t.Error("Keep editing sent the haiku")
		}
	case <-time.After(5 * time.Second):
		t.Error("Keep editing didn't settle the send check")
	}
}
