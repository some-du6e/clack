package ui

// The code channel details pane (codechannel_pane.go, codechannel_diff.go,
// codechannel_html.go): how it opens (channel-tab slugs and links), its
// tabs, and each view over fakeslack's fixtures (synthetic data, nothing
// talks to Slack). With CODE_SHOTS_DIR set (the code-channels workflow)
// each view is saved as a PNG, light and dark.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

func TestCodeChannelPaneLinks(t *testing.T) {
	for in, want := range map[string][2]string{
		"/archives/C1/tabs/code":                     {"C1", "code"},
		"https://acme.slack.com/archives/C1/tabs/V2": {"C1", "V2"},
		"slack://channel?team=T1&id=C1&tab=V3":       {"C1", "V3"},
		"https://example.com/archives/C1/tabs/x":     {"", ""},
		"/archives/C1/p1700000000000100":             {"", ""},
		"slack://channel?team=T1&id=C1":              {"", ""},
	} {
		ch, tab := channelTabOfLink(in)
		if [2]string{ch, tab} != want {
			t.Errorf("channelTabOfLink(%q) = %q %q, want %v", in, ch, tab, want)
		}
	}
}

func TestCodeChannelPane(t *testing.T) {
	e, f := codeShell(t)
	a := e.a
	w := fakeslack.CodeWorkingID
	a.Navigate("c/" + w)
	for i := 0; i < 3; i++ {
		e.frame()
	}
	// the agent_view_added message's channel-tab slugs are links to the tabs
	var urls []string
	for _, m := range f.History[w] {
		if m.Subtype != "agent_view_added" {
			continue
		}
		mm := m
		for _, b := range richtext.FromMessage(e.ca.S, &mm).Blocks {
			for _, sp := range b.Spans {
				if sp.Kind == richtext.Channel {
					urls = append(urls, sp.URL)
				}
			}
		}
	}
	if want := []string{"/archives/" + w + "/tabs/code", "/archives/" + w + "/tabs/" + fakeslack.CodeHTMLViewID}; !reflect.DeepEqual(urls, want) {
		t.Fatalf("slug links %q, want %q", urls, want)
	}
	// a slug to a view opens the details pane on it
	if !a.OpenChannelTabLink(urls[1]) {
		t.Fatal("a channel-tab link wasn't handled")
	}
	e.frame()
	p, ok := a.secondary.view.(*codePane)
	if !ok || a.secondary.kind != secCode || p.cur != fakeslack.CodeHTMLViewID {
		t.Fatalf("pane %T kind %d cur %q", a.secondary.view, a.secondary.kind, p.cur)
	}
	var ids, labels []string
	for _, tb := range p.tabs() {
		ids, labels = append(ids, tb.id), append(labels, tb.label)
	}
	if want := []string{"code", "V0SUMMARY", fakeslack.CodeHTMLViewID, "Ct" + fakeslack.CodePlanCanvasID}; !reflect.DeepEqual(ids, want) {
		t.Errorf("tabs %q, want %q (PR views are off)", ids, want)
	}
	if want := []string{"Code", "Summary", "View", "Canvas"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("labels %q, want %q", labels, want)
	}
	// the Code slug selects the diff in the open pane
	a.OpenChannelTabLink(urls[0])
	e.frame()
	if p.cur != "code" {
		t.Errorf("cur %q after the Code slug", p.cur)
	}
	// offline the diff can't load: Slack's error state
	for i := 0; i < 20; i++ {
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	if v, ok := p.bodies["code"].(*codeDiffView); !ok || !v.done || v.err == nil {
		t.Errorf("offline diff body %+v", p.bodies["code"])
	}
	// "Expand view" gives the pane the whole panel; close resets it
	a.secondary.expanded = true
	e.frame()
	a.secondary.expanded = false
	// a tab that isn't a view (or with artifact tabs on) opens the channel
	a.CloseSecondary()
	if a.OpenChannelTabLink("/archives/" + w + "/tabs/Ctfiles"); a.secondary.kind == secCode {
		t.Error("a non-view tab opened the pane")
	}
	a.data.core.SetExperimentGroup("session_channels_code_tab_pr", "on")
	if a.OpenChannelTabLink(urls[1]); a.secondary.kind == secCode {
		t.Error("artifact tabs on: the view belongs in a channel tab, not the pane")
	}
	a.data.core.SetExperimentGroup("session_channels_code_tab_pr", "off")
	if a.router.Current().Channel() != w {
		t.Errorf("route %q", a.router.Current())
	}
}

// Every view type over fakeslack: the diff (unified, collapsed files,
// "Show N more lines"), the PR (Conversation, Files Changed split with the
// file tree), Block Kit, the canvas and the html view.
func TestCodeChannelPaneFakeslack(t *testing.T) {
	sz := fakeslack.Small
	sz.CodeChannels = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	dir := os.Getenv("CODE_SHOTS_DIR")
	if dir != "" && e.gpu == nil {
		w, err := headless.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	mode := "light"
	shot := func(name string) {
		e.settle(400 * time.Millisecond)
		if dir == "" || e.gpu == nil {
			return
		}
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		out, err := os.Create(filepath.Join(dir, name+"-"+mode+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		if err := png.Encode(out, img); err != nil {
			t.Fatal(err)
		}
	}
	w := fakeslack.CodeWorkingID
	e.until("code channel", 20*time.Second, func() bool { return e.a.data.core.CodeChannelOf(w) != nil })
	e.openChannel(w)
	e.settle(300 * time.Millisecond)
	pane := func() *codePane {
		p, _ := e.a.secondary.view.(*codePane)
		return p
	}
	open := func(view string) {
		e.a.OpenCodeChannelView(w, view)
		e.until("pane on "+view, 10*time.Second, func() bool { p := pane(); return p != nil && p.cur == view && p.bodies[view] != nil })
	}
	diff := func(view string) *codeDiffView {
		v, _ := pane().bodies[view].(*codeDiffView)
		return v
	}
	for _, dark := range []bool{false, true} {
		mode = map[bool]string{false: "light", true: "dark"}[dark]
		e.a.setDark(dark)
		e.a.secondary.expanded = false

		open("code")
		e.until("diff", 15*time.Second, func() bool { v := diff("code"); return v != nil && v.done })
		v := diff("code")
		if v.err != nil || v.diff == nil || len(v.diff.Files) != 5 {
			t.Fatalf("diff %+v err %v", v.diff, v.err)
		}
		if !v.collapsed["package-lock.json"] || v.collapsed["src/auth/redirect.ts"] {
			t.Errorf("auto-collapse %v: generated files start collapsed", v.collapsed)
		}
		var more int
		for _, it := range v.items(v.files()) {
			if it.kind == diMore {
				more = it.n
			}
		}
		if more != 31 { // 131 lines (the hunk header too), 100 shown
			t.Errorf("Show N more lines: %d", more)
		}
		shot("code-pane-diff")

		open("V0SUMMARY")
		e.until("block kit", 10*time.Second, func() bool {
			b, ok := pane().bodies["V0SUMMARY"].(*codeBlockKitView)
			return ok && b.done && b.surf != nil
		})
		if b := pane().bodies["V0SUMMARY"].(*codeBlockKitView); b.err != nil || b.sid != fakeslack.CodeAgentBotID {
			t.Errorf("block kit %v %q", b.err, b.sid)
		}
		shot("code-pane-blockkit")

		open(fakeslack.CodeHTMLViewID)
		e.until("html view", 10*time.Second, func() bool {
			h, ok := pane().bodies[fakeslack.CodeHTMLViewID].(*codeHTMLView)
			return ok && h.done && h.urlPriv != ""
		})
		shot("code-pane-html")

		canvasID := "Ct" + fakeslack.CodePlanCanvasID
		open(canvasID)
		e.until("canvas", 15*time.Second, func() bool {
			c, ok := pane().bodies[canvasID].(*codeCanvasBody)
			return ok && c.v.Doc() != nil
		})
		e.until("canvas tab title", 5*time.Second, func() bool {
			for _, tb := range pane().tabs() {
				if tb.id == canvasID {
					return tb.label == "Plan: fix the login redirect"
				}
			}
			return false
		})
		shot("code-pane-canvas")

		// pull_request views: session_channels_gh_wo
		e.a.data.core.SetExperimentGroup("session_channels_gh_wo", "on")
		open("V0PR")
		e.until("pull request", 15*time.Second, func() bool { v := diff("V0PR"); return v != nil && v.done })
		pr := diff("V0PR")
		if pr.err != nil || pr.pr == nil || pr.pr.PullRequest.State != "open" {
			t.Fatalf("pr %+v %v", pr.pr, pr.err)
		}
		if len(pr.byFile["src/auth/redirect.ts"]["R:16"]) != 1 || len(pr.unanchored) != 2 {
			t.Errorf("comments: anchored %v, unanchored %d", pr.byFile, len(pr.unanchored))
		}
		shot("code-pane-pr-conversation")
		pr.subtab, pr.split = "files", true
		e.a.secondary.expanded = true // the file tree wants a 720px container
		e.settle(200 * time.Millisecond)
		shot("code-pane-pr-split")
		pr.split = false
		shot("code-pane-pr-unified")
		e.a.data.core.SetExperimentGroup("session_channels_gh_wo", "off")
		pr.subtab, pr.split = "conversation", false
		e.a.CloseSecondary()
		e.a.secondary.expanded = false
		e.settle(100 * time.Millisecond)
	}
	var sawDiff bool
	for _, c := range fs.CodeCalls() {
		if c.Method == "client.codeChannels.getDiff" {
			sawDiff = true
			if c.Args.Get("channel_id") != w || c.Args.Get("reason") != "code_tab_view" {
				t.Errorf("getDiff args %v", c.Args)
			}
		}
	}
	if !sawDiff {
		t.Error("no getDiff call")
	}
	_ = core.ViewDiff
}
