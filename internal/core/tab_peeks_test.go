package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// ChannelTabSlugPeek resolves a slug's tab from the channel's
// properties.tabz (with its data) when its peek shows; the gates and
// fetches the peeks use.
func TestChannelTabSlugPeek(t *testing.T) {
	sz := fakeslack.Small
	sz.CodeChannels, sz.Files, sz.Workflows = true, true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		ch, tab string
		want    SlugTab
		peek    bool
	}{
		{fakeslack.CodeWorkingID, "code", SlugTab{ID: "code", Type: "code", Label: "Code"}, true},
		{fakeslack.CodeWorkingID, fakeslack.CodeHTMLViewID, SlugTab{ID: fakeslack.CodeHTMLViewID, Type: "agent_view", Label: "Preview", FileID: fakeslack.CodeHTMLFileID}, true},
		{fakeslack.CodeWorkingID, "V0SUMMARY", SlugTab{ID: "V0SUMMARY", Type: "agent_view", Label: "Summary"}, true},
		{fakeslack.CodeWorkingID, "Ct" + fakeslack.CodePlanCanvasID, SlugTab{ID: "Ct" + fakeslack.CodePlanCanvasID, Type: "canvas", FileID: fakeslack.CodePlanCanvasID}, true},
		{fakeslack.CodeStaleID, "code", SlugTab{ID: "code", Type: "code"}, true}, // no tabz: MrkdwnChannelTab's Code tab
		{fakeslack.FilesChannelID, fakeslack.FilesTabID, SlugTab{ID: fakeslack.FilesTabID, Type: "files"}, true},
		{fakeslack.WorkflowsChannelID, "workflows", SlugTab{ID: "workflows", Type: "workflows"}, true},
		{fakeslack.WorkflowsChannelID, "Ct" + fakeslack.ListTodos, SlugTab{ID: "Ct" + fakeslack.ListTodos, Type: "list", FileID: fakeslack.ListTodos}, true},
		{fakeslack.WorkflowsChannelID, "Ct0FAKEWFB", SlugTab{ID: "Ct0FAKEWFB", Type: "folder"}, true},
		{fakeslack.WorkflowsChannelID, "Ct0GONE000001", SlugTab{}, false}, // not one of its tabs: the channel card
	} {
		got, ok := a.ChannelTabSlugPeek(c.ch, c.tab)
		if ok != c.peek || got != c.want {
			t.Errorf("%s/%s: %+v %v, want %+v %v", c.ch, c.tab, got, ok, c.want, c.peek)
		}
		if card := a.ChannelTabSlugCard(c.ch, c.tab); card == ok {
			t.Errorf("%s/%s: card %v and peek %v", c.ch, c.tab, card, ok)
		}
	}
	// the gates
	if !a.CodeTabPeekOn(fakeslack.CodeWorkingID) || a.CodeTabPeekOn(fakeslack.CodeEmptyID) {
		t.Error("CodeTabPeekOn: the working channel has a repo, the empty one nothing")
	}
	if g := a.AgentViewType(fakeslack.CodeWorkingID, "V0SUMMARY"); g != "block_kit" {
		t.Errorf("V0SUMMARY's view type %q", g)
	}
	if g := a.AgentViewType(fakeslack.CodeWorkingID, fakeslack.CodeHTMLViewID); g != "" {
		t.Errorf("the html view isn't one of agent_session_views: %q", g)
	}
	// the fetches carry the peeks' reasons
	ctx := context.Background()
	d, err := a.CodeTabPeekDiff(ctx, fakeslack.CodeWorkingID)
	if err != nil || d == nil || d.BaseBranch != "main" || d.HeadBranch != "fix-login-redirect" || d.TotalCommits != 3 {
		t.Fatalf("CodeTabPeekDiff: %+v %v", d, err)
	}
	if _, err := a.AgentViewPeekHTML(ctx, fakeslack.CodeWorkingID, fakeslack.CodeHTMLViewID); err != nil {
		t.Fatal(err)
	}
	if b, _, _, err := a.AgentViewPeekBlocks(ctx, fakeslack.CodeWorkingID, "V0SUMMARY"); err != nil || len(b) == 0 {
		t.Fatalf("AgentViewPeekBlocks: %s %v", b, err)
	}
	reasons := map[string]string{}
	for _, c := range fs.CodeCalls() {
		reasons[c.Method] = c.Args.Get("reason")
	}
	for _, m := range []string{"client.codeChannels.getDiff", "client.codeChannels.getView", "client.codeChannels.getBlockKitView"} {
		want := "agent_view_tab_peek"
		if m == "client.codeChannels.getDiff" {
			want = "code_tab_peek"
		}
		if reasons[m] != want {
			t.Errorf("%s reason %q, want %q", m, reasons[m], want)
		}
	}
	// the slug peek off: every resolved slug opens the card
	a.SetExperimentGroup("session_channels_slug_peek", "")
	if _, ok := a.ChannelTabSlugPeek(fakeslack.FilesChannelID, fakeslack.FilesTabID); ok {
		t.Error("session_channels_slug_peek off: a peek")
	}
}
