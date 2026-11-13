package core

// What the channel-tab peeks read (boot-shared q5Cv useTabPeek, web
// client build 3889f0c93d30, from the public CDN without a cookie). The
// cards are ui/tab_slug_peek.go; these are their gates and fetches.

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// CodeTabPeekOn is CodeTabPeek's gate: isCodeChannelTabEnabled (biaa.Jv,
// session_channels) and a channel with something to diff:
// properties.code_channel.repo, properties.git.repo or
// code_channel.diff_file_id.
func (a *App) CodeTabPeekOn(ch string) bool {
	if a.experiment("session_channels") != "on" {
		return false
	}
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return false
		}
	}
	var p struct {
		Code struct {
			Repo       string          `json:"repo"`
			DiffFileID json.RawMessage `json:"diff_file_id"`
		} `json:"code_channel"`
		Git struct {
			Repo string `json:"repo"`
		} `json:"git"`
	}
	if len(c.Properties) == 0 || json.Unmarshal(c.Properties, &p) != nil {
		return false
	}
	return p.Code.Repo != "" || p.Git.Repo != "" || looseString(p.Code.DiffFileID) != ""
}

// CodeTabPeekDiff is CodeTabPeek's fetch: client.codeChannels.getDiff
// {channel_id, reason: "code_tab_peek"} (rn67 zl through useFetcher).
func (a *App) CodeTabPeekDiff(ctx context.Context, ch string) (*CodeDiff, error) {
	return a.codeChannelDiff(ctx, ch, "code_tab_peek")
}

// AgentViewType is useViewType (q5Cv): the type of the
// properties.code_channel.agent_session_views entry whose view_id is the
// tab's id ("" when none).
func (a *App) AgentViewType(ch, viewID string) string {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return ""
		}
	}
	var p struct {
		Code struct {
			Views []struct {
				ID   string `json:"view_id"`
				Type string `json:"type"`
			} `json:"agent_session_views"`
		} `json:"code_channel"`
	}
	if len(c.Properties) == 0 || json.Unmarshal(c.Properties, &p) != nil {
		return ""
	}
	for _, v := range p.Code.Views {
		if v.ID == viewID {
			return v.Type
		}
	}
	return ""
}

// AgentViewPeekHTML is HtmlViewPeek's fetch: client.codeChannels.getView
// {channelId, viewId, reason: "agent_view_tab_peek"} (dUT9 jc). ok is the
// answer's presence (the peek draws nothing until it has data).
func (a *App) AgentViewPeekHTML(ctx context.Context, ch, viewID string) (*CodeHTMLView, error) {
	raw, err := a.C.Raw(ctx, "client.codeChannels.getView", url.Values{"channel_id": {ch}, "view_id": {viewID}, "reason": {"agent_view_tab_peek"}})
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

// AgentViewPeekBlocks is BlockKitViewPeek's fetch:
// client.codeChannels.getBlockKitView {channelId, viewId, reason:
// "agent_view_tab_peek"} (pLP2 oG): the blocks and the ids their actions
// go back with.
func (a *App) AgentViewPeekBlocks(ctx context.Context, ch, viewID string) (blocks json.RawMessage, serviceID, appID string, err error) {
	raw, err := a.C.Raw(ctx, "client.codeChannels.getBlockKitView", url.Values{"channel_id": {ch}, "view_id": {viewID}, "reason": {"agent_view_tab_peek"}})
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

// MarkFileReadPreview is markFileRead from a canvas preview's unmount
// (QuipEmbedWithEntityId isPreview, after 6s): files.markRead {file_id,
// timestamp (ms), reason "mark-read-preview"}, only for a file whose
// created time (unix s) is known and not after at.
func (a *App) MarkFileReadPreview(ctx context.Context, id string, created int64, at time.Time) error {
	if created == 0 || created*1000 > at.UnixMilli() {
		return nil
	}
	_, err := a.C.Raw(ctx, "files.markRead", url.Values{"file_id": {id}, "timestamp": {strconv.FormatInt(at.UnixMilli(), 10)},
		"reason": {"mark-read-preview"}})
	return err
}
