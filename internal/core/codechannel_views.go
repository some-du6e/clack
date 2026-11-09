package core

// The views an agent publishes in a code channel and the API calls that
// fill the "Code channel details" pane, ported from Slack's web client
// (build a84662d2d135): client-code-channel-details-view (ECTe),
// code-channel-view-renderer (iuvb), the Code tab (lu3W), the agent view
// tab (vxvv) and the view helpers (V/cF, PntH, pX9p). docs/CODE-CHANNELS.md
// has the evidence.

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// CodeChannelViewsOn is the pane's gate: isCodeChannelViewsEnabled
// (session_channels) and not artifact tabs (session_channels_code_tab_pr,
// which moves the views into channel tabs). Slack's biaa.PH.
func (a *App) CodeChannelViewsOn() bool {
	return a.sessionChannelsOn() && a.experiment("session_channels_code_tab_pr") != "on"
}

// CodeChannelPRViewsOn is prViewsEnabled (biaa.jt: session_channels and
// session_channels_gh_wo): pull_request views survive getCodeChannelViews.
func (a *App) CodeChannelPRViewsOn() bool {
	return a.sessionChannelsOn() && a.experiment("session_channels_gh_wo") == "on"
}

// CodeChannelInlineDiffOn is biaa._5 (session_channels and
// session_channels_inline_diff): agent_view_added messages carry inline
// cards for the views they link.
func (a *App) CodeChannelInlineDiffOn() bool {
	return a.sessionChannelsOn() && a.experiment("session_channels_inline_diff") == "on"
}

// CodeChannelViews is getCodeChannelViews (PntH.B) with the viewer's
// experiments: the pane's tab list, the Code diff first.
func (a *App) CodeChannelViews(cc *CodeChannel) []CodeChannelView {
	if cc == nil {
		return nil
	}
	pr := a.CodeChannelPRViewsOn()
	var out []CodeChannelView
	for _, v := range cc.allViews {
		if v.Type == ViewPullRequest && (!pr || v.PRURL == "") {
			continue
		}
		out = append(out, v)
	}
	return out
}

// CodeViewLabel is labelForView (V/cF.Zm): a canvas's title, else the
// view's name, else the type's fallback.
func CodeViewLabel(v CodeChannelView, canvasTitle string) string {
	if v.Type == ViewCanvas && v.CanvasID != "" && canvasTitle != "" {
		return canvasTitle
	}
	if v.Name != "" {
		return v.Name
	}
	switch v.Type {
	case ViewDiff:
		return "Code"
	case ViewCanvas:
		return "Canvas"
	case ViewBlockKit:
		return "Details"
	case ViewPullRequest:
		return "Pull request"
	}
	return "View"
}

// CodeViewIcon is svgIconForView (V/cF.Yi): the tab's 16px icon.
func CodeViewIcon(typ string) string {
	switch typ {
	case ViewDiff:
		return "code"
	case ViewHTML:
		return "eye-open"
	case ViewCanvas:
		return "canvas-content"
	case ViewBlockKit:
		return "info"
	case ViewPullRequest:
		return "branch"
	}
	return ""
}

// CodeViewForTab is resolveCodeChannelViewIdForTab (pX9p.V): the view a
// channel-tab slug (a rich_text channel element's tab_id, or
// /archives/<C>/tabs/<id>) opens. The tab must be one of the channel's
// tabs: a Code or agent view tab is its own view; a canvas tab is a view
// when its canvas is one of plan_canvases. "" when the tab isn't a view.
func (a *App) CodeViewForTab(ch, tabID string) string {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return ""
		}
	}
	cc := codeChannelOf(c)
	if cc == nil || tabID == "" {
		return ""
	}
	tab, ok := findChannelTab(c.Properties, tabID)
	if !ok {
		return ""
	}
	switch tab.Type {
	case "code", "agent_view":
		return tab.ID
	case "canvas":
		for _, p := range cc.planCanvases {
			if p != "" && p == tab.FileID {
				return tab.ID
			}
		}
	}
	return ""
}

type channelTab struct {
	ID, Type, FileID string
}

// findChannelTab is findTabInChannelProperties (mwX5.kt) over tabz, then
// tabs: the tab's id, type and data.file_id.
func findChannelTab(props json.RawMessage, id string) (channelTab, bool) {
	var p struct {
		Tabz []json.RawMessage `json:"tabz"`
		Tabs []json.RawMessage `json:"tabs"`
	}
	if json.Unmarshal(props, &p) != nil {
		return channelTab{}, false
	}
	for _, raw := range append(p.Tabz, p.Tabs...) {
		var t struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Data struct {
				FileID string `json:"file_id"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &t) == nil && t.ID == id {
			return channelTab{ID: t.ID, Type: t.Type, FileID: t.Data.FileID}, true
		}
	}
	return channelTab{}, false
}

// HasFilesTab is the details pane's "Files and links" tab condition: the
// channel's tabz has a files tab.
func HasFilesTab(props json.RawMessage) bool {
	var p struct {
		Tabz []struct {
			Type string `json:"type"`
		} `json:"tabz"`
	}
	if json.Unmarshal(props, &p) != nil {
		return false
	}
	for _, t := range p.Tabz {
		if t.Type == "files" {
			return true
		}
	}
	return false
}

// ---- client.codeChannels.getDiff ----

// CodeDiffFile is one file of a diff, as the Code tab reads it.
type CodeDiffFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename,omitempty"`
	Status           string `json:"status"` // added | removed | renamed | modified
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Patch            string `json:"patch,omitempty"`
	IsGenerated      bool   `json:"is_generated,omitempty"`
}

// CodeDiff is getDiff's (and getPullRequest's) diff.
type CodeDiff struct {
	Files        []CodeDiffFile `json:"files"`
	Truncated    bool           `json:"truncated"`
	TotalCommits int            `json:"total_commits"`
	BaseBranch   string         `json:"base_branch"`
	HeadBranch   string         `json:"head_branch"`
}

// Totals is the summary line's +additions −deletions (nil when the diff
// is truncated, as Slack hides them then).
func (d *CodeDiff) Totals() (add, del int, ok bool) {
	if d == nil || d.Truncated {
		return 0, 0, false
	}
	for _, f := range d.Files {
		add += f.Additions
		del += f.Deletions
	}
	return add, del, true
}

// CodeChannelDiff is the Code view's fetch (CodeTabView, branch mode):
// client.codeChannels.getDiff {channel_id, reason: "code_tab_view"},
// answering {diff}. Slack refetches when code_channel.diff_file_version
// changes.
func (a *App) CodeChannelDiff(ctx context.Context, ch string) (*CodeDiff, error) {
	raw, err := a.C.Raw(ctx, "client.codeChannels.getDiff", url.Values{"channel_id": {ch}, "reason": {"code_tab_view"}})
	if err != nil {
		return nil, err
	}
	var r struct {
		Diff CodeDiff `json:"diff"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return &r.Diff, nil
}

// ---- client.codeChannels.getPullRequest ----

// CodePRSender is a comment's author: a GitHub login (with its avatar)
// or a Slack member.
type CodePRSender struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	UserID    string `json:"user_id"`
	AvatarURL string `json:"avatar_url"`
}

// CodePRComment is one of getPullRequest's comments.
type CodePRComment struct {
	ID           string `json:"id"`
	CommentKind  string `json:"comment_kind"` // review_comment | review_summary | …
	ReviewState  string `json:"review_state"` // PENDING, APPROVED, …
	Timestamp    int64  `json:"timestamp"`
	ThreadNodeID string `json:"thread_node_id"`
	Comment      struct {
		Value string `json:"value"`
	} `json:"comment"`
	Sender       CodePRSender `json:"sender"`
	ReviewAnchor *struct {
		Path        string `json:"path"`
		Line        int    `json:"line"`
		Side        string `json:"side"` // LEFT | RIGHT
		SubjectType string `json:"subject_type"`
	} `json:"review_anchor"`
}

// CodePullRequest is getPullRequest's pull_request.
type CodePullRequest struct {
	Title          string `json:"title"`
	Body           string `json:"body"`
	State          string `json:"state"`           // open | draft | merged | closed
	ReviewDecision string `json:"review_decision"` // approved | changes_requested | none
	HasConflicts   bool   `json:"has_conflicts"`
}

// CodePR is the PR view's data.
type CodePR struct {
	PullRequest CodePullRequest `json:"pull_request"`
	Diff        CodeDiff        `json:"diff"`
	Comments    []CodePRComment `json:"comments"`
	Revision    string          `json:"revision"`
}

// CodeChannelPullRequest is useCodeChannelPullRequest's fetch (3WIv,
// GxtS): client.codeChannels.getPullRequest {channel_id, pr_url, reason}.
func (a *App) CodeChannelPullRequest(ctx context.Context, ch, prURL string) (*CodePR, error) {
	raw, err := a.C.Raw(ctx, "client.codeChannels.getPullRequest", url.Values{"channel_id": {ch}, "pr_url": {prURL}, "reason": {"code_tab_view"}})
	if err != nil {
		return nil, err
	}
	var r CodePR
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// AnchoredComments is anchorGitHubPrComments (lu3W): review comments on a
// line of the diff go under that line (by file, by line key); the rest
// (summaries, issue comments, comments whose line left the diff) are the
// Conversation's. Outdated, empty and unsent comments are dropped.
func AnchoredComments(comments []CodePRComment, files []CodeDiffFile) (byFile map[string]map[string][]CodePRComment, unanchored []CodePRComment) {
	byFile = map[string]map[string][]CodePRComment{}
	keys := map[string]map[string]bool{}
	for _, f := range files {
		m := map[string]bool{}
		for _, l := range ParsePatch(f.Patch) {
			if l.Type != PatchHunk {
				m[l.Key()] = true
			}
		}
		keys[f.Filename] = m
	}
	sorted := append([]CodePRComment(nil), comments...)
	for i := 1; i < len(sorted); i++ { // stable by timestamp
		for j := i; j > 0 && sorted[j].Timestamp < sorted[j-1].Timestamp; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	for _, c := range sorted {
		pending := c.ReviewState == "PENDING" && c.CommentKind == "review_comment"
		an := c.ReviewAnchor
		outdated := c.CommentKind == "review_comment" && (an == nil || an.SubjectType != "file") && (an == nil || an.Line == 0)
		if (c.Timestamp == 0 && !pending) || strings.TrimSpace(c.Comment.Value) == "" || outdated {
			continue
		}
		key := ""
		if c.CommentKind == "review_comment" && an != nil && an.Line > 0 {
			if an.Side == "LEFT" {
				key = PatchLine{Type: PatchRemoved, OldLine: an.Line}.Key()
			} else {
				key = PatchLine{Type: PatchAdded, NewLine: an.Line}.Key()
			}
		}
		if an == nil || an.Path == "" || key == "" || !keys[an.Path][key] {
			unanchored = append(unanchored, c)
			continue
		}
		m := byFile[an.Path]
		if m == nil {
			m = map[string][]CodePRComment{}
			byFile[an.Path] = m
		}
		m[key] = append(m[key], c)
	}
	return byFile, unanchored
}

// ---- client.codeChannels.getView (html views) ----

// CodeHTMLView is getView's answer for an html view. Clack never renders
// html_content (no web view; untrusted HTML stays out of the process): the
// pane shows Slack's own "not available in this environment" state.
type CodeHTMLView struct {
	ContentVersion int `json:"content_version"`
	HasContent     bool
}

// CodeChannelHTMLView is AgentViewTabView's fetch: client.codeChannels.getView
// {channel_id, view_id, reason: "agent_view_tab"}, answering {html_content,
// content_version, csp}. Only whether there is content is kept.
func (a *App) CodeChannelHTMLView(ctx context.Context, ch, viewID string) (*CodeHTMLView, error) {
	raw, err := a.C.Raw(ctx, "client.codeChannels.getView", url.Values{"channel_id": {ch}, "view_id": {viewID}, "reason": {"agent_view_tab"}})
	if err != nil {
		return nil, err
	}
	var r struct {
		HTML           string `json:"html_content"`
		ContentVersion int    `json:"content_version"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return &CodeHTMLView{ContentVersion: r.ContentVersion, HasContent: r.HTML != ""}, nil
}

// CodeChannelBlockKit is CodeChannelBlockKitView's fetch with the ids
// its actions go back with: {blocks, service_id, app_id}.
func (a *App) CodeChannelBlockKit(ctx context.Context, ch, viewID string) (blocks json.RawMessage, serviceID, appID string, err error) {
	raw, err := a.C.Raw(ctx, "client.codeChannels.getBlockKitView", url.Values{"channel_id": {ch}, "view_id": {viewID}, "reason": {"code_channel_block_kit_view"}})
	if err != nil {
		return nil, "", "", err
	}
	var r struct {
		Blocks    json.RawMessage `json:"blocks"`
		ServiceID string          `json:"service_id"`
		AppID     string          `json:"app_id"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.Blocks, r.ServiceID, r.AppID, nil
}

// CodeChannelOf is a conversation's code channel (nil for others).
func (a *App) CodeChannelOf(ch string) *CodeChannel {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return nil
		}
	}
	return codeChannelOf(c)
}

// CodeChannelHasDiff is the inline diff block's and the Code view's
// condition: code_channel.repo, git.repo or code_channel.diff_file_id.
func CodeChannelHasDiff(c *slack.Conversation) bool {
	if c == nil {
		return false
	}
	var p struct {
		CodeChannel struct {
			Repo   string `json:"repo"`
			DiffID string `json:"diff_file_id"`
		} `json:"code_channel"`
		Git struct {
			Repo string `json:"repo"`
		} `json:"git"`
	}
	if json.Unmarshal(c.Properties, &p) != nil {
		return false
	}
	return p.CodeChannel.Repo != "" || p.Git.Repo != "" || p.CodeChannel.DiffID != ""
}

// CodeViewFile is what the html view's header and its "Open in browser"
// need from files.info: the file's title and its url_private (Slack's
// file menu opens that) and url_private_download (Download).
func (a *App) CodeViewFile(ctx context.Context, id string) (title, urlPrivate, download string, err error) {
	var r struct {
		File slack.File `json:"file"`
	}
	if err := a.C.Call(ctx, "files.info", url.Values{"file": {id}, "count": {"0"}}, &r); err != nil {
		return "", "", "", err
	}
	return r.File.Title, r.File.URLPrivate, r.File.URLPrivateDownload, nil
}
