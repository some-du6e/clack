package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
)

// The @channel relay plugins, unmodified, against Clack's broadcast
// permissions (broadcast.go): Taut's NativeAtChannel (composer-taut
// workflow) and Slick's bChannel (ui-slick workflow). Both only step in
// where Slack would refuse, which they learn from Slack's own selectors
// (getBroadcastKeywordsForUser, canUserAtChannelInChannelById), and
// both send by turning the message into a slash command. Synthetic
// workspace and fake API; no real relay is ever reached.

const atChannelApp = "A08FPGU02A2" // at-channel, the Hack Club relay app NativeAtChannel drives

// atChannelCommands is commands.list with at-channel's /channel and /here.
func atChannelCommands(fs *fakeSlack) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.reply = map[string]any{
		"edge/permissions/info": noBroadcastPerms,
		"commands.list": map[string]any{"ok": true, "commands": map[string]any{
			"/channel": map[string]any{"name": "/channel", "desc": "Ping the channel", "type": "app", "app": atChannelApp, "app_name": "at-channel"},
			"/here":    map[string]any{"name": "/here", "desc": "Ping who's online", "type": "app", "app": atChannelApp, "app_name": "at-channel"},
		}},
	}
}

// waitJS polls a JS expression until it is truthy.
func waitJS(t *testing.T, rt *jsrt.Runtime, what, expr string) {
	t.Helper()
	fn := fmt.Sprintf("__composerTestWait%d", time.Now().UnixNano())
	if _, err := rt.Eval(fn+".js", "globalThis."+fn+" = () => !!("+expr+")"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 8*time.Second, what, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		raw, err := rt.Call(ctx, fn)
		return err == nil && string(raw) == "true"
	})
}

// acRowsUntil types text into an autocomplete for ch and returns the row
// names once cond holds for them.
func acRowsUntil(t *testing.T, app *core.App, ch, text, what string, cond func([]string) bool) []string {
	t.Helper()
	ac := newAutocomplete(&slickHost{app: app}, ch)
	t.Cleanup(ac.Close)
	var mu sync.Mutex
	var q []func()
	ac.later = func(f func()) { mu.Lock(); q = append(q, f); mu.Unlock() }
	d := acDocOf(text)
	var names []string
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		fs := q
		q = nil
		mu.Unlock()
		for _, f := range fs {
			f()
		}
		ac.Update(d, d.Len())
		names = names[:0]
		items, _ := ac.Items()
		for _, it := range items {
			names = append(names, it.Name)
		}
		if cond(names) {
			return names
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s (rows %v)", what, names)
	return nil
}

// TestTautNativeAtChannel: where Slack refuses @channel (no
// CAN_AT_CHANNEL) and at-channel's commands are installed, the @ rows
// offer @channel and @here again (never @everyone), and sending one runs
// at-channel's /channel with the message instead of posting it. In a
// thread, where at-channel doesn't relay, Slack's refusal stands.
func TestTautNativeAtChannel(t *testing.T) {
	e := startPlugin(t, "NativeAtChannel", atChannelCommands)
	rt, err := e.tt.JS()
	if err != nil {
		t.Fatal(err)
	}
	waitJS(t, rt, "at-channel's /channel in the store", `globalThis.__taut.getRawState()?.slashCommand?.commands?.['`+atChannelApp+`-/channel']`)
	// Slack's own answer, which the plugin reads, says no
	waitJS(t, rt, "getBroadcastKeywordsForUser answering []", `(() => {
		const f = globalThis.__taut.getExport((x) => typeof x === 'function' && x.name === 'getBroadcastKeywordsForUser')
		return f && f(globalThis.__taut.getRawState(), '`+tChGen+`').length === 0
	})()`)

	got := acRowsUntil(t, e.app, tChGen, "@", "@channel and @here offered by the plugin", func(n []string) bool {
		return hasName(n, "channel") && hasName(n, "here")
	})
	if hasName(got, "everyone") {
		t.Errorf("@everyone offered: %v", got)
	}

	before := len(e.slack.called("chat.command"))
	o := &outgoing{doc: broadcastDoc("channel")}
	o.text, o.blocks = o.doc.Mrkdwn(), o.doc.RichTextBlocks()
	j := &sendJob{api: coreAPI{e.app}, ch: tChGen, o: o, draftID: tChGen, view: ViewContextMessagePane}
	if err := j.run(e.ctx()); err != nil {
		t.Fatalf("send: %v", err)
	}
	cmds := e.slack.called("chat.command")
	if len(cmds) != before+1 {
		t.Fatalf("%d chat.command calls, want 1", len(cmds)-before)
	}
	c := cmds[len(cmds)-1]
	if c.Get("command") != "/channel" || c.Get("channel") != tChGen || !strings.Contains(c.Get("text"), "<!channel>") || !strings.Contains(c.Get("text"), "please") {
		t.Errorf("chat.command %v", c)
	}
	if n := len(e.slack.called("chat.postMessage")); n != 0 {
		t.Errorf("%d chat.postMessage calls, want none", n)
	}

	// a thread reply isn't relayed: Slack's check refuses it
	o = &outgoing{doc: broadcastDoc("here")}
	o.text, o.blocks = o.doc.Mrkdwn(), o.doc.RichTextBlocks()
	j = &sendJob{api: coreAPI{e.app}, ch: tChGen, ts: "1700000000.000100", o: o, draftID: tChGen + "-1700000000.000100", view: "THREADS_FLEXPANE"}
	if err := j.run(e.ctx()); !isProhibitedBroadcast(err) {
		t.Errorf("thread send: %v, want ProhibitedBroadcastKeywordInEffect", err)
	}
	if n := len(e.slack.called("chat.postMessage")); n != 0 {
		t.Errorf("thread reply posted (%d)", n)
	}
}

// TestTautNativeAtChannelStandsAside: where Slack allows @channel the
// plugin changes nothing: Slack's rows, a normal post.
func TestTautNativeAtChannelStandsAside(t *testing.T) {
	e := startPlugin(t, "NativeAtChannel", func(fs *fakeSlack) {
		atChannelCommands(fs)
		fs.mu.Lock()
		delete(fs.reply, "edge/permissions/info") // the default: allowed
		fs.mu.Unlock()
	})
	acRowsUntil(t, e.app, tChGen, "@", "Slack's own @channel @here @everyone", func(n []string) bool {
		return hasName(n, "channel") && hasName(n, "here") && hasName(n, "everyone")
	})
	posts := url.Values(e.sendDoc(broadcastDoc("channel")))
	if !strings.Contains(posts.Get("blocks"), `"range":"channel"`) {
		t.Errorf("posted blocks %s", posts.Get("blocks"))
	}
	if n := len(e.slack.called("chat.command")); n != 0 {
		t.Errorf("relayed although allowed (%d chat.command)", n)
	}
}

// bChannelCommand is commands.list with bChannel's /bchannel.
func bChannelCommand(e *slickEnv, perms map[string]any) {
	e.slack.mu.Lock()
	defer e.slack.mu.Unlock()
	e.slack.reply = map[string]any{"commands.list": map[string]any{"ok": true,
		"commands": map[string]any{"/bchannel": map[string]any{"name": "/bchannel", "desc": "Relay @channel", "type": "app"}}}}
	if perms != nil {
		e.slack.reply["edge/permissions/info"] = perms
	}
}

// TestSlickViewbChannelSend: bChannel stages the message with its relay
// (stubbed here) and sends its /bchannel command back through the
// composer's prepareAndSendMessage; Clack runs it as a command and never
// posts the @channel itself.
func TestSlickViewbChannelSend(t *testing.T) {
	var mu sync.Mutex
	var staged []map[string]any
	e := startSlick(t, "bChannel", nil, func(e *slickEnv) {
		bChannelCommand(e, noBroadcastPerms)
		e.fetch = func(req *http.Request) (string, bool) {
			if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/slick/intents") {
				return "", false
			}
			var intent map[string]any
			if req.Body != nil {
				b, _ := io.ReadAll(req.Body)
				_ = json.Unmarshal(b, &intent)
			}
			mu.Lock()
			staged = append(staged, intent)
			mu.Unlock()
			return `{"token":"` + strings.Repeat("a", 43) + `","commandText":"relay-` + strings.Repeat("a", 8) + `"}`, true
		}
	})
	e.patched("MessagePaneInput")
	rt, err := e.tt.JS()
	if err != nil {
		t.Fatal(err)
	}
	waitJS(t, rt, "/bchannel in the store", `Object.values(globalThis.__taut.getRawState()?.slashCommand?.commands || {}).some((c) => c && c.name === '/bchannel')`)
	c, frame := e.composer(tChRandom)
	e.until(8*time.Second, "the composer's send wrapped by bChannel", frame, func() bool { return c.sendJS.ID != 0 })
	// the permissions bChannel asks Slack about must be in before it decides
	if _, err := e.app.LoadBroadcastPerms(e.ctx(), tChRandom); err != nil {
		t.Fatal(err)
	}
	c.ed.SetDoc(broadcastDoc("channel"))
	c.submit()
	e.why = func() string {
		mu.Lock()
		defer mu.Unlock()
		return fmt.Sprintf("staged=%d commands=%v posts=%d", len(staged), e.slack.called("chat.command"), len(e.slack.called("chat.postMessage")))
	}
	e.until(10*time.Second, "bChannel's /bchannel run as a command", frame, func() bool {
		return len(e.slack.called("chat.command")) > 0
	})
	cmd := e.slack.called("chat.command")[0]
	if cmd.Get("command") != "/bchannel" || cmd.Get("text") != "relay-"+strings.Repeat("a", 8) || cmd.Get("channel") != tChRandom {
		t.Errorf("chat.command %v", cmd)
	}
	if n := len(e.slack.called("chat.postMessage")); n != 0 {
		t.Errorf("%d chat.postMessage calls, want none", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(staged) != 1 || staged[0]["channelId"] != tChRandom {
		t.Fatalf("staged %v", staged)
	}
	b, _ := json.Marshal(staged[0]["blocks"])
	if !strings.Contains(string(b), `"type":"broadcast"`) || !strings.Contains(string(b), `"range":"channel"`) {
		t.Errorf("staged blocks lost the @channel: %s", b)
	}
}

// TestSlickViewbChannelStandsAside: where Slack allows @channel
// (canUserAtChannelInChannelById), bChannel leaves the rows alone.
func TestSlickViewbChannelStandsAside(t *testing.T) {
	e := startSlick(t, "bChannel", nil, func(e *slickEnv) { bChannelCommand(e, nil) })
	e.patched("TextyAutocomplete")
	rt, err := e.tt.JS()
	if err != nil {
		t.Fatal(err)
	}
	waitJS(t, rt, "/bchannel in the store", `Object.values(globalThis.__taut.getRawState()?.slashCommand?.commands || {}).some((c) => c && c.name === '/bchannel')`)
	waitJS(t, rt, "canUserAtChannelInChannelById true", `(() => {
		const f = globalThis.__taut.getExport((x) => typeof x === 'function' && x.name === 'canUserAtChannelInChannelById')
		return f && f(globalThis.__taut.getRawState(), '`+tChRandom+`') === true
	})()`)
	c, frame := e.composer(tChRandom)
	edType(c.ed, "hi @")
	e.until(8*time.Second, "Slack's @channel and @here", frame, func() bool {
		n := rowNames(c)
		return c.ac.Open() && hasName(n, "channel") && hasName(n, "here")
	})
	for i := 0; i < 20; i++ {
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	if c.ac.ov.includeAll || hasName(rowNames(c), "everyone") {
		t.Errorf("bChannel relayed where Slack allows (includeAll=%v rows %v)", c.ac.ov.includeAll, rowNames(c))
	}
}
