package core

import (
	"encoding/json"
	"strings"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Channel-tab slugs' labels and icons (richtext tabslug.go), from Slack's
// boot-shared NY16 MrkdwnChannelTab / ResolvedChannelTabSlug, 83sY
// useTabLabel, /LFd useTabIconProps and client-boot /HhV
// getChannelIconType (build 3889f0c93d30, 2026-11-13).

func init() {
	store.TabSlug = func(s *store.Store, ch, tab string) (richtext.TabSlug, bool) {
		if v, ok := appsByStore.Load(s); ok {
			return v.(*App).ChannelTabSlug(ch, tab)
		}
		return richtext.TabSlug{}, false
	}
	for name, groups := range map[string][]string{
		"resources_channel_tab": on, // biaa jL: the Files tab is "Resources" (files icon)
		"channel_tabs_threads":  on, // mwX5 U6: the Threads tab's label (none without it)
	} {
		experimentReads[name] = groups
	}
}

// slugTab is one of a channel's properties.tabz, as useTabLabel reads it.
type slugTab struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Label string `json:"label"`
	Data  struct {
		FileID           string `json:"file_id"`
		DefaultView      string `json:"lists_channel_tab_default_refine_view_id"`
		FolderBookmarkID string `json:"folder_bookmark_id"`
	} `json:"data"`
}

// findSlugTab is MrkdwnChannelTab's lookup: the first of properties.tabz
// whose id or type is id (findTabInChannelProperties), else, for "code",
// the Code tab ({id: code, type: code, label: ""}). A code channel's
// artifact tabs (getCodeChannelArtifactTabForTabId) need
// session_channels_code_tab_pr, which the account isn't in.
func findSlugTab(props json.RawMessage, id string) (slugTab, bool) {
	if id == "" {
		return slugTab{}, false
	}
	var p struct {
		Tabz []slugTab `json:"tabz"`
	}
	if len(props) > 0 && json.Unmarshal(props, &p) == nil {
		for _, t := range p.Tabz {
			if t.ID == id || t.Type == id {
				return t, true
			}
		}
	}
	if id == "code" {
		return slugTab{ID: "code", Type: "code"}, true
	}
	return slugTab{}, false
}

// ChannelTabSlug resolves a channel-tab slug (richtext.ChannelTabResolver
// through the store): false, Slack's BaseMrkdwnChannel, for an unknown
// channel, a private channel the viewer isn't in (unless it is a record
// channel) or a tab that doesn't resolve.
func (a *App) ChannelTabSlug(ch, tab string) (richtext.TabSlug, bool) {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return richtext.TabSlug{}, false
		}
	}
	private := (c.IsPrivate || c.IsGroup) && !c.IsMPIM
	if private && !a.isMemberConv(c) && recordID(c.Properties) == "" {
		return richtext.TabSlug{}, false
	}
	t, ok := findSlugTab(c.Properties, tab)
	if !ok {
		return richtext.TabSlug{}, false
	}
	return richtext.TabSlug{
		Label:  a.tabSlugLabel(t),
		Icon:   tabSlugIcon(t.Type, a.experiment("resources_channel_tab") == "on"),
		Name:   strings.TrimPrefix(a.S.ChannelName(c.ID), "#"),
		ChIcon: a.slugChannelIcon(c),
	}, true
}

// tabSlugLabel is useTabLabel for a slug (not in the tab bar, so
// useIsInChannelTabsList is false).
func (a *App) tabSlugLabel(t slugTab) string {
	file := t.Type == "canvas" || t.Type == "list"
	var ref richtext.CanvasRef
	known := false
	if file && t.Data.FileID != "" {
		ref, known = a.S.CanvasRef(t.Data.FileID) // files.info, asked for when missing
	}
	if file && t.Label != "" {
		// a file the viewer can't see (isNotVisible, isExternalAccessDenied,
		// isBlockedExternalCanvas/List) is the file's name
		if known && (ref.Status == "hidden" || ref.Status == "locked") && ref.Title != "" {
			return ref.Title
		}
		return t.Label
	}
	if file && known && ref.Title != "" {
		if t.Type == "list" && a.experiment("list_channel_tab_show_view_name_fe") == "on" {
			if v := ref.Views[t.Data.DefaultView]; v != "" {
				return ref.Title + ": " + v
			}
		}
		return ref.Title
	}
	if t.Type == "folder" && t.Data.FolderBookmarkID == "" && t.Label == "" {
		return "Bookmarks"
	}
	if t.Type == "channel_canvas" { // AddCanvas
		return "Add canvas"
	}
	switch t.ID {
	case "bookmarks", "pins":
		return "Pins"
	case "channel":
		return "Messages"
	case "files":
		if a.experiment("resources_channel_tab") == "on" {
			return "Resources"
		}
		return "Files & links"
	case "automations":
		return "Automations"
	case "record_summary":
		return "Summary"
	case "work_object_overview":
		return "Record details"
	case "workflows":
		return "Workflows"
	case "threads": // ThreadsTabLabel: nothing without channel_tabs_threads
		if a.experiment("channel_tabs_threads") == "on" {
			return "Threads"
		}
		return ""
	case "slackbot_ai_skills":
		return "Skills"
	case "slackbot_ai_tasks", "app_3p_agent_tasks", "channel_tasks":
		return "Tasks"
	case "import_emails":
		return "Import Emails"
	case "emails":
		return "Emails"
	case "code", "agent_sessions":
		return "Code"
	case "channel_agent_sessions":
		return "Agent Sessions"
	case "app_sessions":
		return "Sessions"
	}
	if t.Label != "" {
		return t.Label
	}
	return "Untitled"
}

// tabSlugIcons is useTabIconProps (unfilled) by tab type.
var tabSlugIcons = map[string]string{
	"bookmarks": "pin", "pins": "pin", "canvas": "canvas-content", "channel_canvas": "canvas-content",
	"channel": "message", "create_canvas": "add-channel-canvas", "external_files": "file", "native_file": "file",
	"folder": "folder", "list": "lists", "record_overview": "sf-record", "workflows": "bolt",
	"salesforce_list_view": "sf-record-list", "app_salesforce_list_view_browser": "sf-record-list", "record_related_list": "sf-record-list",
	"threads": "threads", "record_related_conversations": "threads", "agent": "bot",
	"slackbot_ai_conversation": "message", "slackbot_ai_history": "clock", "slackbot_ai_skills": "ai-skills",
	"slackbot_ai_tasks": "ai-workflow", "app_3p_agent_tasks": "ai-workflow", "channel_tasks": "ai-workflow",
	"import_emails": "email-down", "emails": "email", "app_home": "home", "app_about": "info", "app_chat": "message",
	"app_history": "clock", "app_memories": "bookmark", "app_tasks": "check-circle", "code": "code", "agent_sessions": "code",
	"app_sessions": "ai-agents", "channel_agent_sessions": "ai-agents", "surface": "gauge", "agent_view": "eye-open",
	"code_channel_pull_request": "pr-open", "code_channel_view": "info", "automations": "ai-workflow",
}

// tabSlugIcon is useTabIconProps' icon name: the Files tab is "files"
// under resources_channel_tab, else "canvas-browser"; record_summary's
// summary icon and unknown types have none here.
func tabSlugIcon(typ string, resources bool) string {
	if typ == "files" {
		if resources {
			return "files"
		}
		return "canvas-browser"
	}
	return tabSlugIcons[typ]
}

// slugChannelIcon is getChannelIconType(channel, {showCodeIcon}) when it
// isn't the plain channel icon ("" then, and the name reads "#name"): an
// archived channel's archive, a file channel's canvas / lists / file, a
// code channel's code (with session_channels), a private channel's lock.
// (A huddle record channel's headphones, getRecordChannelOverrideIconProps,
// isn't ported.)
func (a *App) slugChannelIcon(c *slack.Conversation) string {
	switch {
	case c.IsArchived:
		return "archive"
	case IsFileChannel(c): // EK7S jT: the file's kind
		switch a.sidebarFileOf(FileOfChannel(c.ID)).typ {
		case "canvas":
			return "canvas"
		case "list":
			return "lists"
		}
		return "file"
	case codeChannelOf(c) != nil && a.sessionChannelsOn():
		return "code"
	case (c.IsPrivate || c.IsGroup) && !c.IsMPIM:
		return "lock"
	}
	return ""
}
