package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

// Taut plugins, unmodified (built by the composer-taut workflow with Taut's
// own build:user-plugin), driven through the composer's real call sites:
// typing, the send path (prepareAndSendMessage → sendMessage → the fake
// chat.postMessage), pasting and live markup in the editor. Synthetic state
// only. Skipped unless TAUT_PLUGINS_DIR is set.

type pluginEnv struct {
	t     *testing.T
	app   *core.App
	slack *fakeSlack
	tt    *taut.Taut
	id    string

	mu    sync.Mutex
	trees map[uint64][]byte
}

// fetchStubs answer plugin fetches by URL prefix; everything else is a 404.
var fetchStubs = map[string]string{
	"https://raw.githubusercontent.com/ClearURLs/Rules/": `{"providers":{"globalRules":{"urlPattern":".*","rules":["utm_[a-z]+"],"rawRules":[],"exceptions":[]}}}`,
}

func startPlugin(t *testing.T, name string) *pluginEnv {
	t.Helper()
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the composer-taut workflow builds Taut's plugins and runs this")
	}
	code, err := os.ReadFile(filepath.Join(dir, name+".js"))
	if err != nil {
		t.Fatal(err)
	}
	app, fs := syntheticApp(t)
	e := &pluginEnv{t: t, app: app, slack: fs, trees: map[uint64][]byte{}}
	tdir := t.TempDir()
	tt, err := taut.New(taut.Options{
		App: app, Dir: tdir, CredsPath: tdir + "/creds.json", CacheDir: t.TempDir(), NoAutoStart: true,
		UI: taut.UIHooks{Deliver: func(root uint64, tree []byte) {
			e.mu.Lock()
			e.trees[root] = append([]byte(nil), tree...)
			e.mu.Unlock()
		}},
		Fetch: func(ctx context.Context, req *http.Request) (*http.Response, error) {
			u := req.URL.String()
			if strings.HasPrefix(u, fs.srv.URL) {
				return http.DefaultClient.Do(req.WithContext(ctx))
			}
			for p, body := range fetchStubs {
				if strings.HasPrefix(u, p) {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				}
			}
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
		},
		Log: func(level, msg string) {
			if level == "error" || level == "warn" {
				t.Logf("js %s: %s", level, msg)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	e.tt = tt
	ctx := e.ctx()
	id, err := tt.Import(ctx, code, "")
	if err != nil {
		t.Fatalf("import %s: %v", name, err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	e.id = id
	withTaut(t, tt)
	t.Cleanup(func() {
		// back to the native path for later tests
		SetTaut(nil)
		syncHooks()
	})
	return e
}

func (e *pluginEnv) ctx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	e.t.Cleanup(cancel)
	return ctx
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// sendDoc sends d from a channel composer the way submit does and returns
// the chat.postMessage form.
func (e *pluginEnv) sendDoc(d *Doc) map[string][]string {
	e.t.Helper()
	before := len(e.slack.called("chat.postMessage"))
	o := &outgoing{doc: d, text: d.Mrkdwn(), blocks: d.RichTextBlocks()}
	j := &sendJob{api: coreAPI{e.app}, ch: tChGen, o: o, draftID: tChGen, view: ViewContextMessagePane}
	if err := j.run(e.ctx()); err != nil {
		e.t.Fatalf("send: %v", err)
	}
	posts := e.slack.called("chat.postMessage")
	if len(posts) != before+1 {
		e.t.Fatalf("%d chat.postMessage calls, want 1", len(posts)-before)
	}
	return posts[len(posts)-1]
}

// TestTautSilentTyping: after its composer button silences the channel,
// the composer's typing reports stop (currentUserStartedTyping).
func TestTautSilentTyping(t *testing.T) {
	e := startPlugin(t, "SilentTyping")
	n := stubTyping(t)
	api := coreAPI{e.app}
	if !currentTaut().ThunkPatched(thTyping) {
		t.Fatal("currentUserStartedTyping not patched")
	}
	sendTyping(api, tChGen, "")
	if *n != 1 {
		t.Fatalf("typing before silencing: %d natives, want 1", *n)
	}
	// The composer renders the plugin's button through slackc; render it
	// the same way and click it.
	props, _ := json.Marshal(map[string]any{"placement": "end", "context": map[string]any{"kind": "channel", "channelId": tChGen}})
	e.tt.Bridge().Render(9, "TautComposerButtons", props)
	var fn float64
	waitFor(t, 5*time.Second, "the SilentTyping button", func() bool {
		e.mu.Lock()
		tree := e.trees[9]
		e.mu.Unlock()
		var nodes []any
		if json.Unmarshal(tree, &nodes) != nil {
			return false
		}
		btn := findNode(nodes, "ComposerButton")
		if btn == nil {
			return false
		}
		p, _ := btn["p"].(map[string]any)
		click, _ := p["onClick"].(map[string]any)
		fn, _ = click["$fn"].(float64)
		return fn != 0
	})
	e.tt.Bridge().Call(int64(fn), []byte("[]"), nil)
	waitFor(t, 3*time.Second, "typing silenced", func() bool {
		before := *n
		sendTyping(api, tChGen, "")
		return *n == before
	})
	// Other channels still report typing.
	before := *n
	sendTyping(api, "C0OTHER", "")
	if *n != before+1 {
		t.Error("typing in another channel was silenced too")
	}
}

func findNode(nodes []any, name string) map[string]any {
	for _, x := range nodes {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		if m["t"] == "@" && m["n"] == name {
			return m
		}
		if kids, ok := m["c"].([]any); ok {
			if f := findNode(kids, name); f != nil {
				return f
			}
		}
	}
	return nil
}

// TestTautInvisibleForward: a send whose blocks start with a Slack message
// link goes out with the link hidden (onMessageSendBlocks).
func TestTautInvisibleForward(t *testing.T) {
	e := startPlugin(t, "InvisibleForward")
	in := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"link","url":"https://hackclub.slack.com/archives/C1/p1700000000000100"},{"type":"text","text":" look at this"}]}]}]`)
	d, err := DocFromBlocksJSON(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, "send transforms registered", func() bool { return currentTaut().HasSendTransforms() })
	form := e.sendDoc(d)
	blocks := form["blocks"]
	if len(blocks) != 1 {
		t.Fatalf("no blocks posted: %v", form)
	}
	if !strings.Contains(blocks[0], "⁠") && !strings.Contains(blocks[0], `⁠`) {
		t.Errorf("link not hidden in the sent blocks: %s", blocks[0])
	}
	if !strings.Contains(blocks[0], `"look at this"`) {
		t.Errorf("text not trimmed in the sent blocks: %s", blocks[0])
	}
}

// TestTautClearURLs: a tracking URL pasted into the composer is cleaned
// (addLinkTransform), and the message carries the clean URL.
func TestTautClearURLs(t *testing.T) {
	e := startPlugin(t, "ClearURLs")
	const dirty, clean = "https://example.com/page?utm_source=news&id=7", "https://example.com/page?id=7"
	var ed *Editor
	waitFor(t, 5*time.Second, "rules loaded", func() bool {
		syncHooks() // what every composer frame does
		if LinkTransforms == nil {
			return false
		}
		ed = NewEditor()
		ed.paste(dirty)
		return ed.Doc().Text() != dirty
	})
	if got := ed.Doc().Text(); got != clean {
		t.Fatalf("pasted %q, want %q", got, clean)
	}
	form := e.sendDoc(ed.Doc())
	if b := strings.Join(form["blocks"], ""); !strings.Contains(b, clean) || strings.Contains(b, "utm_source") {
		t.Errorf("sent blocks %s", b)
	}
	// The link dialog's save goes through the same transform.
	linkSource = "link-dialog"
	got := transformLink(dirty)
	linkSource = "paste"
	if got != clean {
		t.Errorf("link dialog URL %q", got)
	}
}

// TestTautRealMarkdown: typing **hi** applies bold live (patchInlineMarkup
// replaces Slack's *bold*), and the send carries bold "hi".
func TestTautRealMarkdown(t *testing.T) {
	e := startPlugin(t, "RealMarkdown")
	waitFor(t, 3*time.Second, "inline markup registered", func() bool {
		syncHooks()
		return InlineMarkup != nil
	})
	ed := NewEditor()
	edType(ed, "say **hi** now")
	d := ed.Doc()
	if got := d.Text(); got != "say hi now" {
		t.Fatalf("text %q, want the markers removed", got)
	}
	if !d.HasStyle(4, 6, Bold) || d.HasStyle(0, 3, Bold) {
		t.Fatalf("styles: bold on hi only (%v)", edStyles(d))
	}
	// Slack's *bold* shortcut now means italic, as in Markdown.
	ed2 := NewEditor()
	edType(ed2, "a *b* c")
	if d2 := ed2.Doc(); d2.Text() != "a b c" || d2.HasStyle(2, 3, Bold) || !d2.HasStyle(2, 3, Italic) {
		t.Errorf("*b* with RealMarkdown: %q %v, want italic b", d2.Text(), edStyles(d2))
	}
	form := e.sendDoc(d)
	b := strings.Join(form["blocks"], "")
	if !strings.Contains(b, `"text":"hi","style":{"bold":true}`) {
		t.Errorf("sent blocks %s", b)
	}
}

// TestTautPluginsOffNativeAgain: disabling a plugin returns every call
// site to the native path (no hooks, no patched thunks).
func TestTautPluginsOffNativeAgain(t *testing.T) {
	e := startPlugin(t, "SilentTyping")
	if err := e.tt.SetEnabled(e.ctx(), e.id, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, "unpatched", func() bool { return !currentTaut().ThunkPatched(thTyping) })
	syncHooks()
	if SendChecks != nil || LinkTransforms != nil || InlineMarkup != nil {
		t.Error("hooks left installed")
	}
}

// TestTautAnonymizeFilenames: a file attached to the composer (upload at
// attach: addAndUploadPendingFile) and one shared on send (uploadFiles)
// reach Slack under a random name with the extension kept.
func TestTautAnonymizeFilenames(t *testing.T) {
	e := startPlugin(t, "AnonymizeFilenames")
	api := coreAPI{e.app}
	ctx := e.ctx()
	waitFor(t, 3*time.Second, "upload thunks patched", func() bool {
		return currentTaut().ThunkPatched(thPendingFile) && currentTaut().ThunkPatched(thUploadFiles)
	})
	check := func(what string, n int) {
		t.Helper()
		calls := e.slack.called("files.getUploadURLExternal")
		if len(calls) != n {
			t.Fatalf("%s: %d upload slots, want %d", what, len(calls), n)
		}
		name := calls[n-1].Get("filename")
		if name == "" || strings.Contains(name, "vacation") || !strings.HasSuffix(name, ".png") {
			t.Errorf("%s: uploaded as %q, want a random name ending .png", what, name)
		}
	}
	up, err := prepareFile(ctx, api, "vacation-at-home.png", []byte("\x89PNG synthetic"), nil)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if up.ID != "F0TEST" {
		t.Errorf("prepared %+v", up)
	}
	check("attach", 1)
	err = sendFiles(ctx, api, tChGen, "", "", nil, nil, []core.UploadFile{{Name: "vacation-at-home.png", Data: []byte("\x89PNG synthetic")}})
	if err != nil {
		t.Fatalf("share: %v", err)
	}
	check("send", 2)
}
