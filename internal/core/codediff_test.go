package core

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

const testPatch = "@@ -12,7 +12,9 @@ export function next()\n-  if (next) return next\n+  if (next && !next.startsWith('/login')) {\n+    return next\n+  }\n   return '/home'\n\\ No newline at end of file"

func TestCodeChannelParsePatch(t *testing.T) {
	ls := ParsePatch(testPatch)
	want := []PatchLine{
		{Type: PatchHunk, Content: "@@ -12,7 +12,9 @@ export function next()"},
		{Type: PatchRemoved, Content: "  if (next) return next", OldLine: 12},
		{Type: PatchAdded, Content: "  if (next && !next.startsWith('/login')) {", NewLine: 12},
		{Type: PatchAdded, Content: "    return next", NewLine: 13},
		{Type: PatchAdded, Content: "  }", NewLine: 14},
		{Type: PatchContext, Content: "  return '/home'", OldLine: 13, NewLine: 15},
	}
	if !reflect.DeepEqual(ls, want) {
		t.Fatalf("ParsePatch:\n%+v\nwant\n%+v", ls, want)
	}
	if ls[1].Key() != "L:12" || ls[2].Key() != "R:12" || ls[5].Key() != "R:15" {
		t.Errorf("line keys %s %s %s", ls[1].Key(), ls[2].Key(), ls[5].Key())
	}
	// split: the removed line pairs with the first added one
	rows := SplitRows(ls)
	wantRows := []SplitRow{{Hunk: true, Index: 0, Left: -1, Right: -1}, {Left: 1, Right: 2}, {Left: -1, Right: 3}, {Left: -1, Right: 4}, {Left: 5, Right: 5}}
	if !reflect.DeepEqual(rows, wantRows) {
		t.Errorf("SplitRows %+v", rows)
	}
}

func TestCodeChannelHideWhitespace(t *testing.T) {
	ls := ParsePatch("@@ -1,3 +1,3 @@\n-a  =  1\n-b\n+a = 1\n+c")
	got := HideWhitespace(ls)
	if len(got) != 4 || got[1].Type != PatchContext || got[1].Content != "a = 1" || got[1].OldLine != 1 || got[1].NewLine != 1 ||
		got[2].Type != PatchRemoved || got[3].Type != PatchAdded {
		t.Errorf("HideWhitespace %+v", got)
	}
}

func TestCodeChannelFileTree(t *testing.T) {
	files := []CodeDiffFile{{Filename: "src/auth/redirect.ts"}, {Filename: "README.md"}, {Filename: "src/auth/session.ts"}, {Filename: "src/app.ts"}}
	var got []string
	for _, r := range FileTree(files) {
		got = append(got, strings.Repeat(" ", r.Depth)+r.Name)
	}
	want := []string{"src", " auth", "  redirect.ts", "  session.ts", " app.ts", "README.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tree %q", got)
	}
	// compaction: a single-child folder chain is one row
	got = got[:0]
	for _, r := range FileTree([]CodeDiffFile{{Filename: "a/b/c/d.go"}, {Filename: "a/b/c/e.go"}}) {
		got = append(got, r.Name)
	}
	if !reflect.DeepEqual(got, []string{"a/b/c", "d.go", "e.go"}) {
		t.Errorf("compacted %q", got)
	}
	ord := OrderFiles([]CodeDiffFile{{Filename: "go.sum", IsGenerated: true}, {Filename: "main.go"}}, false)
	if ord[0].Filename != "main.go" {
		t.Errorf("generated files go last: %+v", ord)
	}
}

func TestCodeChannelAutoCollapsed(t *testing.T) {
	var files []CodeDiffFile
	for i := 0; i < 20; i++ {
		files = append(files, CodeDiffFile{Filename: "f" + string(rune('a'+i)), Additions: 10})
	}
	files[0].IsGenerated = true
	files[1].Additions = 600 // oversized
	c := AutoCollapsed(files, nil)
	if !c["fa"] || !c["fb"] {
		t.Error("generated and oversized files start collapsed")
	}
	open := 0
	for _, f := range files {
		if !c[f.Filename] {
			open++
		}
	}
	if open != 15 {
		t.Errorf("%d files open, want Slack's 15", open)
	}
	if c := AutoCollapsed(files, map[string]bool{"ft": true}); c["ft"] {
		t.Error("a commented file opens even past the caps")
	}
}

func TestCodeChannelHighlight(t *testing.T) {
	toks := HighlightLine("typescript", `  if (next && !next.startsWith('/login')) { // guard`)
	kinds := map[string]int{}
	for _, tk := range toks {
		kinds[strings.TrimSpace(tk.Text)] = tk.Kind
	}
	if kinds["startsWith"] != TokEntity || kinds["'/login'"] != TokString || kinds["// guard"] != TokComment || kinds["if"] != TokPlain {
		t.Errorf("tokens %+v", toks)
	}
	if HighlightLine(CodeLang("notes.txt"), "plain") != nil {
		t.Error("unknown extensions aren't highlighted")
	}
	if CodeLang("src/a.TSX") != "tsx" || CodeLang("Dockerfile") != "dockerfile" || CodeLang("x.py") != "python" {
		t.Error("CodeLang")
	}
	var joined string
	for _, tk := range HighlightLine("go", `	return fmt.Sprintf("%d\n", 42)`) {
		joined += tk.Text
	}
	if joined != "\treturn fmt.Sprintf(\"%d\\n\", 42)" {
		t.Errorf("tokens must cover the line: %q", joined)
	}
}

func TestCodeChannelViewsAndTabs(t *testing.T) {
	a, _ := testApp(t)
	props := `{"record_channel":{"record_id":"AC:1:2:CCODE1","record_type":"agent_channel"},"agent_session":{"status":"active"},
	"code_channel":{"repo":"acme/api","diff_file_id":"F1","diff_file_version":3,
	 "agent_session_views":[{"view_id":"V1","type":"block_kit","name":"Summary"},{"view_id":"V2","type":"pull_request","pr_url":"https://github.com/acme/api/pull/1"}],
	 "agent_views":[{"view_id":"VH","file_id":"FH"}],"plan_canvases":[{"canvas_id":"FCAN"}]},
	"tabz":[{"id":"code","type":"code"},{"id":"VH","type":"agent_view"},{"id":"Ct1","type":"canvas","data":{"file_id":"FCAN"}},{"id":"Ct2","type":"canvas","data":{"file_id":"FOTHER"}},{"id":"files","type":"files"}]}`
	a.S.PutConv(slack.Conversation{ID: "CCODE1", Name: "fix", IsChannel: true, IsMember: true, Properties: json.RawMessage(props)})
	cc := a.CodeChannelOf("CCODE1")
	ids := func(vs []CodeChannelView) (out []string) {
		for _, v := range vs {
			out = append(out, v.ViewID)
		}
		return
	}
	if got := ids(a.CodeChannelViews(cc)); !reflect.DeepEqual(got, []string{"code", "V1", "VH", "Ct1"}) { // the plan canvas takes its tab's id
		t.Errorf("views %v (PR views are off by default)", got)
	}
	a.SetExperimentGroup("session_channels_gh_wo", "on")
	if got := ids(a.CodeChannelViews(cc)); !reflect.DeepEqual(got, []string{"code", "V1", "V2", "VH", "Ct1"}) {
		t.Errorf("views with session_channels_gh_wo %v", got)
	}
	if !a.CodeChannelViewsOn() {
		t.Error("the pane is on with session_channels")
	}
	a.SetExperimentGroup("session_channels_code_tab_pr", "on")
	if a.CodeChannelViewsOn() {
		t.Error("artifact tabs replace the pane")
	}
	for tab, want := range map[string]string{"code": "code", "VH": "VH", "Ct1": "Ct1", "Ct2": "", "missing": "", "files": ""} {
		if got := a.CodeViewForTab("CCODE1", tab); got != want {
			t.Errorf("CodeViewForTab(%q) = %q, want %q", tab, got, want)
		}
	}
	if !HasFilesTab(json.RawMessage(props)) {
		t.Error("files tab")
	}
	for v, want := range map[CodeChannelView]string{{Type: ViewDiff}: "Code", {Type: ViewBlockKit}: "Details", {Type: ViewHTML}: "View",
		{Type: ViewCanvas, CanvasID: "F"}: "Plan", {Type: ViewPullRequest, Name: "#1"}: "#1"} {
		title := ""
		if v.Type == ViewCanvas {
			title = "Plan"
		}
		if got := CodeViewLabel(v, title); got != want {
			t.Errorf("label %+v = %q", v, got)
		}
	}
	if cc.DiffVersion != 3 || CodeViewIcon(ViewHTML) != "eye-open" {
		t.Error("diff version / icon")
	}
}

func TestCodeChannelAnchoredComments(t *testing.T) {
	files := []CodeDiffFile{{Filename: "src/a.ts", Patch: testPatch}}
	mk := func(id, kind string, ts int64, path string, line int, side string) CodePRComment {
		c := CodePRComment{ID: id, CommentKind: kind, Timestamp: ts}
		c.Comment.Value = "looks good"
		if path != "" {
			c.ReviewAnchor = &struct {
				Path        string `json:"path"`
				Line        int    `json:"line"`
				Side        string `json:"side"`
				SubjectType string `json:"subject_type"`
			}{Path: path, Line: line, Side: side}
		}
		return c
	}
	cs := []CodePRComment{
		mk("c2", "review_comment", 20, "src/a.ts", 13, "RIGHT"),
		mk("c1", "review_comment", 10, "src/a.ts", 12, "LEFT"),
		mk("c3", "review_comment", 30, "src/a.ts", 99, "RIGHT"), // off the diff: Conversation
		mk("c4", "review_summary", 40, "", 0, ""),
		mk("c5", "review_comment", 50, "src/a.ts", 0, ""), // outdated: dropped
	}
	by, un := AnchoredComments(cs, files)
	if len(by["src/a.ts"]["L:12"]) != 1 || len(by["src/a.ts"]["R:13"]) != 1 {
		t.Errorf("anchored %+v", by)
	}
	if len(un) != 2 || un[0].ID != "c3" || un[1].ID != "c4" {
		t.Errorf("unanchored %+v", un)
	}
}

func TestCodeChannelViewCalls(t *testing.T) {
	a, f := testApp(t)
	f.reply["client.codeChannels.getDiff"] = `{"ok":true,"diff":{"files":[{"filename":"a.go","status":"modified","additions":2,"deletions":1,"patch":"@@ -1 +1 @@"}],"truncated":false,"total_commits":2,"base_branch":"main","head_branch":"fix"}}`
	f.reply["client.codeChannels.getPullRequest"] = `{"ok":true,"pull_request":{"title":"Fix","state":"open","review_decision":"approved"},"diff":{"files":[]},"comments":[],"revision":"abc"}`
	f.reply["client.codeChannels.getView"] = `{"ok":true,"html_content":"<p>x</p>","content_version":2,"csp":{"resource_domains":[]}}`
	f.reply["client.codeChannels.getBlockKitView"] = `{"ok":true,"blocks":[],"service_id":"B1","app_id":"A1"}`
	ctx := context.Background()
	d, err := a.CodeChannelDiff(ctx, "CCODE1")
	if err != nil || len(d.Files) != 1 || d.BaseBranch != "main" || d.TotalCommits != 2 {
		t.Fatalf("diff %+v %v", d, err)
	}
	if v := f.last("client.codeChannels.getDiff"); v.Get("channel_id") != "CCODE1" || v.Get("reason") != "code_tab_view" {
		t.Errorf("getDiff args %v", v)
	}
	if add, del, ok := d.Totals(); !ok || add != 2 || del != 1 {
		t.Errorf("totals %d %d %v", add, del, ok)
	}
	pr, err := a.CodeChannelPullRequest(ctx, "CCODE1", "https://github.com/acme/api/pull/1")
	if err != nil || pr.PullRequest.State != "open" || pr.Revision != "abc" {
		t.Fatalf("pr %+v %v", pr, err)
	}
	if v := f.last("client.codeChannels.getPullRequest"); v.Get("pr_url") != "https://github.com/acme/api/pull/1" || v.Get("channel_id") != "CCODE1" {
		t.Errorf("getPullRequest args %v", v)
	}
	h, err := a.CodeChannelHTMLView(ctx, "CCODE1", "VH")
	if err != nil || !h.HasContent || h.ContentVersion != 2 {
		t.Fatalf("html %+v %v", h, err)
	}
	if v := f.last("client.codeChannels.getView"); v.Get("view_id") != "VH" || v.Get("reason") != "agent_view_tab" {
		t.Errorf("getView args %v", v)
	}
	_, sid, aid, err := a.CodeChannelBlockKit(ctx, "CCODE1", "V1")
	if err != nil || sid != "B1" || aid != "A1" {
		t.Errorf("block kit %q %q %v", sid, aid, err)
	}
	if v := f.last("client.codeChannels.getBlockKitView"); v.Get("reason") != "code_channel_block_kit_view" {
		t.Errorf("getBlockKitView args %v", v)
	}
}
