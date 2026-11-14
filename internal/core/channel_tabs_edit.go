package core

// Managing a channel's tabs (files-links-2; CDN build 0e695dd18302): the
// tab bar's right-click menu (useTabSchemaToTabItems' getTabContextMenu:
// "Hide tab" on a built-in tab, "Remove tab" on the others) and the
// Manage tabs card (ManageChannelTabs Wg5J: Hide tab / Show tab, Move tab
// up / down). The fetchers are createApiMethodFetcher's, which snake-case
// the args and keep reason out of them:
//
//   - removeTab (allT): conversations.removeTab {id, channel_id}, reason
//     remove-channel-tab.
//   - addTabToChannel (hm7X), Show tab on a hidden built-in:
//     conversations.addTab {type, channel_id}, reason add-channel-tab.
//   - updateTabOrder (AIhO): conversations.moveTab {channel_id, id, after}
//     with after the tab it lands behind (left out when there is none or
//     it is a static tab: Messages); the answer's tabz is the new order.

import (
	"context"
	"encoding/json"
	"net/url"
)

// TabState is one of a channel's tabs as properties.tabs has it, hidden
// built-ins included (Tabs leaves those out).
type TabState struct {
	ID, Type, Label string
	Disabled        bool // is_disabled: a built-in tab that is hidden
}

// TabOrder reads the channel's tabs in order, hidden ones too
// (conversations.info properties.tabs; tabz when that's what comes back).
func (a *App) TabOrder(ctx context.Context, ch string) ([]TabState, error) {
	var res struct {
		Channel struct {
			Properties struct {
				Tabs []tabRaw `json:"tabs"`
				Tabz []tabRaw `json:"tabz"`
			} `json:"properties"`
		} `json:"channel"`
	}
	if err := a.C.Call(ctx, "conversations.info", url.Values{"channel": {ch}}, &res); err != nil {
		return nil, err
	}
	raw := res.Channel.Properties.Tabs
	if len(raw) == 0 {
		raw = res.Channel.Properties.Tabz
	}
	return tabStates(raw), nil
}

func tabStates(raw []tabRaw) []TabState {
	out := make([]TabState, 0, len(raw))
	for _, r := range raw {
		id := r.ID
		if id == "" {
			id = r.Type // formatDynamicTab: id ?? type
		}
		out = append(out, TabState{ID: id, Type: r.Type, Label: r.Label, Disabled: r.IsDisabled})
	}
	return out
}

// HideTab hides a built-in tab (removeTab: RemoveTab's
// conversations.removeTab {id, channel_id}); the tab bar re-reads.
func (a *App) HideTab(ctx context.Context, ch, id string) error {
	err := a.RemoveTab(ctx, ch, id)
	if err == nil {
		a.emit(Event{Type: "tabs", Channel: ch})
	}
	return err
}

// ShowTab puts a hidden built-in tab back (addTabToChannel:
// conversations.addTab {type, channel_id}).
func (a *App) ShowTab(ctx context.Context, ch, typ string) error {
	err := a.C.Call(ctx, "conversations.addTab", url.Values{"type": {typ}, "channel_id": {ch}}, nil)
	if err == nil {
		a.emit(Event{Type: "tabs", Channel: ch})
	}
	return err
}

// MoveTab is updateTabOrder's call: conversations.moveTab {channel_id,
// id, after}; after "" sends none (the tab goes first). It returns the
// answer's order (tabz).
func (a *App) MoveTab(ctx context.Context, ch, id, after string) ([]TabState, error) {
	p := url.Values{"channel_id": {ch}, "id": {id}}
	if after != "" {
		p.Set("after", after)
	}
	var res struct {
		Tabz json.RawMessage `json:"tabz"`
		Tabs json.RawMessage `json:"tabs"`
	}
	if err := a.C.Call(ctx, "conversations.moveTab", p, &res); err != nil {
		return nil, err
	}
	var raw []tabRaw
	if json.Unmarshal(res.Tabz, &raw) != nil || len(raw) == 0 {
		_ = json.Unmarshal(res.Tabs, &raw)
	}
	a.emit(Event{Type: "tabs", Channel: ch})
	return tabStates(raw), nil
}

// StaticTab is isStaticChannelTab (mwX5 PN): Messages (the channel tab)
// and the other static types never move, take no after and have no
// Remove tab.
func StaticTab(t TabState) bool { return StaticTabType(t.Type) || t.ID == "channel" }

// MoveTabAfter is ManageChannelTabs' updateTabOrder for moving the tab at
// from to position to in order: the tab it lands behind is the last
// movable one before to in the new order (findLast), sent as after.
func MoveTabAfter(order []TabState, from, to int) (id, after string, ok bool) {
	if from < 0 || from >= len(order) || to < 0 || to >= len(order) || from == to {
		return "", "", false
	}
	moved := append([]TabState(nil), order...)
	t := moved[from]
	moved = append(moved[:from], moved[from+1:]...)
	moved = append(moved[:to], append([]TabState{t}, moved[to:]...)...)
	for i := to - 1; i >= 0; i-- {
		if !StaticTab(moved[i]) {
			return t.ID, moved[i].ID, true
		}
	}
	return t.ID, "", true
}

// The tab kinds (AkE0, mwX5; CDN build fa938a086569):
//
//   - isStaticChannelTab (PN): AkE0 x_'s types, the ones that never move.
//   - isKnownChannelTab (_T): an id among AkE0 fY's, the built-in tabs;
//     their menu's remove item says "Hide tab".
//   - the rename table (AkE0 i2, mwX5 cO): Rename tab / Rename folder for
//     canvases, folders, lists, Salesforce list views, external and native
//     files, AI widgets and surfaces.
//   - isEditableTab (Sc): AkE0 ot, the dynamic types (lUcP dC) but the
//     record overview and summary; isEditableSystemTab (d3): the editable
//     ones that are built in (the visibility eye in Manage tabs, else its
//     trash "Delete tab").
//   - isSavedCodeChannelArtifactTab (H$): a saved code channel pull request
//     or view (id Ct + 8 or more of [A-Z0-9]); it moves although static.
//   - useTabSchemaToTabItems' isDragDisabled: !(not static || saved code
//     artifact) || !useCanEditTabs.

var staticTabTypes = map[string]bool{
	"channel": true, "record_overview": true, "record_summary": true, "record_related_conversations": true,
	"work_object_overview": true, "slackbot_ai_conversation": true, "slackbot_ai_history": true,
	"slackbot_ai_skills": true, "slackbot_ai_tasks": true, "app_home": true, "app_about": true, "app_chat": true,
	"app_history": true, "app_memories": true, "app_tasks": true, "app_3p_agent_tasks": true,
	"app_salesforce_list_view_browser": true, "app_sessions": true, "channel_tasks": true, "import_emails": true,
	"emails": true, "create_canvas": true, "code_channel_pull_request": true, "code_channel_view": true,
	"automations": true,
}

var knownTabIDs = map[string]bool{
	"channel_canvas": true, "bookmarks": true, "channel": true, "creating_canvas": true, "files": true, "list": true,
	"pins": true, "record_opportunities": true, "record_overview": true, "record_summary": true,
	"record_related_list": true, "record_related_conversations": true, "salesforce_list_view": true,
	"app_salesforce_list_view_browser": true, "slackbot_ai_conversation": true, "slackbot_ai_history": true,
	"slackbot_ai_skills": true, "slackbot_ai_tasks": true, "app_3p_agent_tasks": true, "channel_tasks": true,
	"import_emails": true, "emails": true, "threads": true, "work_object_overview": true, "workflows": true,
	"agents": true, "app_home": true, "app_about": true, "app_chat": true, "app_history": true,
	"app_memories": true, "app_tasks": true, "native_file": true, "code": true, "agent_sessions": true,
	"channel_agent_sessions": true, "app_sessions": true, "automations": true,
}

var renameableTabTypes = map[string]bool{
	"canvas": true, "folder": true, "list": true, "salesforce_list_view": true, "external_files": true,
	"native_file": true, "ai_interactive_widget": true, "surface": true,
}

var editableTabTypes = map[string]bool{
	"list": true, "canvas": true, "files": true, "bookmarks": true, "folder": true, "agent": true, "pins": true,
	"workflows": true, "channel_canvas": true, "record_related_list": true, "salesforce_list_view": true,
	"threads": true, "external_files": true, "huddles": true, "native_file": true, "ai_interactive_widget": true,
	"code": true, "surface": true, "agent_view": true, "agent_sessions": true, "channel_agent_sessions": true,
}

var systemTabTypes = map[string]bool{
	"channel_canvas": true, "workflows": true, "files": true, "pins": true, "bookmarks": true, "threads": true,
	"code": true, "agent_sessions": true, "channel_agent_sessions": true,
}

// StaticTabType is isStaticChannelTab by type.
func StaticTabType(typ string) bool { return staticTabTypes[typ] }

// KnownTabID is isKnownChannelTabId: a built-in tab's id.
func KnownTabID(id string) bool { return knownTabIDs[id] }

// RenameableTab is the rename table (cO) for a tab type.
func RenameableTab(typ string) bool { return renameableTabTypes[typ] }

// EditableTab is isEditableTab (Sc) by type.
func EditableTab(typ string) bool { return editableTabTypes[typ] }

// EditableSystemTab is isEditableSystemTab (d3) by type.
func EditableSystemTab(typ string) bool { return systemTabTypes[typ] && editableTabTypes[typ] }

// SavedCodeArtifactTab is isSavedCodeChannelArtifactTab (H$).
func SavedCodeArtifactTab(id, typ string) bool {
	if typ != "code_channel_pull_request" && typ != "code_channel_view" || len(id) < 10 || id[:2] != "Ct" {
		return false
	}
	for _, r := range id[2:] {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// DraggableTab is a tab the bar lets the viewer drag (with useCanEditTabs):
// not static, or a saved code artifact.
func DraggableTab(id, typ string) bool {
	return !StaticTabType(typ) && id != "channel" || SavedCodeArtifactTab(id, typ)
}

// MoveTabByIndex is updateTabOrderByIndex (E/Yw tE) for the tab bar's
// drop: bar is the bar's tabs (Messages first), the tab at src dropped at
// dst (calculateDropIndex's answer). The tab it lands behind is the last
// one at or before dst-1 (dst when it moves right) but itself; updateTabOrder
// sends it as after unless it is static (Messages). ok is false for no
// move.
func MoveTabByIndex(bar []TabState, src, dst int) (id, after string, ok bool) {
	if src == dst || src < 0 || src >= len(bar) || dst < 0 || dst > len(bar) {
		return "", "", false
	}
	l := dst
	if src > dst {
		l = dst - 1
	}
	u := -1
	for i := min(l, len(bar)-1); i >= 0; i-- {
		if i != src {
			u = i
			break
		}
	}
	if u >= 0 && (!StaticTab(bar[u]) || SavedCodeArtifactTab(bar[u].ID, bar[u].Type)) {
		after = bar[u].ID
	}
	return bar[src].ID, after, true
}

// RenameTab is updateTabLabel (Fx4h) from the Rename tab modal:
// conversations.updateTab {id, channel_id, label}, reason updateTabLabel.
// An empty label resets only a canvas tab (to its file's name). The tab
// bar re-reads.
func (a *App) RenameTab(ctx context.Context, ch, id, label string) error {
	err := a.RenameFolderTab(ctx, ch, id, label, "updateTabLabel")
	a.emit(Event{Type: "tabs", Channel: ch})
	return err
}

// RemoveChannelTab is removeTab (allT): conversations.removeTab {id,
// channel_id, keep_file_share} (keep_file_share only from
// RemoveFileTabConfirmationModal's Keep), reason remove-channel-tab. The
// tab bar re-reads.
func (a *App) RemoveChannelTab(ctx context.Context, ch, id string, keepFileShare bool) error {
	p := url.Values{"id": {id}, "channel_id": {ch}, "reason": {"remove-channel-tab"}}
	if keepFileShare {
		p.Set("keep_file_share", "true")
	}
	err := a.C.Call(ctx, "conversations.removeTab", p, nil)
	a.emit(Event{Type: "tabs", Channel: ch})
	return err
}

// FileTabRemove is what maybeOpenRemoveFileTabConfirmationModal (OH3f S)
// decided for a canvas or list tab.
type FileTabRemove int

const (
	FileTabRemoveNow       FileTabRemove = iota // removeTab at once
	FileTabRemoveKnowledge                      // RemoveKnowledgeCanvasTabConfirmationModal
	FileTabRemoveAsk                            // RemoveFileTabConfirmationModal
)

// FileTabRemoval is maybeOpenRemoveFileTabConfirmationModal's check for
// removing ch's tab of file fileID: the channel's knowledge canvas
// (isKnowledgeCanvasFile: properties.channel_knowledge_canvas.file_id, or
// the file's knowledge_canvas_channel_id) while it is enabled asks first;
// a file that isn't a canvas or list goes at once; else files.hasShares
// {file_id, channel_id, exclude_channel_tab_shares} (reason
// remove_file_tab_confirmation_modal) and a file shared elsewhere or with
// the org (isFileSharedWithOrg: org_or_workspace_access not none) goes at
// once, one shared only here asks. list reports a list (the modal's
// title).
func (a *App) FileTabRemoval(ctx context.Context, ch, fileID string) (what FileTabRemove, list bool, err error) {
	if fileID == "" {
		return FileTabRemoveNow, false, nil
	}
	var kc struct {
		KC *struct {
			FileID    string `json:"file_id"`
			IsEnabled *bool  `json:"is_enabled"`
		} `json:"channel_knowledge_canvas"`
	}
	if c, ok := a.S.Conv(ch); ok && len(c.Properties) > 0 {
		_ = json.Unmarshal(c.Properties, &kc)
	}
	var r struct {
		File struct {
			ID                     string `json:"id"`
			Mode                   string `json:"mode"`
			Filetype               string `json:"filetype"`
			OrgOrWorkspaceAccess   string `json:"org_or_workspace_access"`
			KnowledgeCanvasChannel string `json:"knowledge_canvas_channel_id"`
		} `json:"file"`
	}
	ferr := a.C.Call(ctx, "files.info", url.Values{"file": {fileID}}, &r)
	f := r.File
	knowledge := kc.KC != nil && kc.KC.FileID == fileID || ferr == nil && f.KnowledgeCanvasChannel == ch
	if knowledge && (kc.KC == nil || kc.KC.IsEnabled == nil || *kc.KC.IsEnabled) {
		return FileTabRemoveKnowledge, false, nil
	}
	canvas := f.Mode == "quip" || f.Filetype == "quip"
	list = f.Mode == "list" || f.Filetype == "list"
	if ferr != nil || f.ID == "" || !canvas && !list {
		return FileTabRemoveNow, list, nil
	}
	var hs struct {
		HasShares bool `json:"has_shares"`
	}
	if err := a.C.Call(ctx, "files.hasShares", url.Values{"file_id": {fileID}, "channel_id": {ch},
		"exclude_channel_tab_shares": {"true"}, "reason": {"remove_file_tab_confirmation_modal"}}, &hs); err != nil {
		return FileTabRemoveNow, list, err
	}
	if hs.HasShares || f.OrgOrWorkspaceAccess != "" && f.OrgOrWorkspaceAccess != "none" {
		return FileTabRemoveNow, list, nil
	}
	return FileTabRemoveAsk, list, nil
}

// DisableChannelKnowledge is RemoveKnowledgeCanvasTabConfirmationModal's
// Remove before the tab goes: ai.channelKnowledge.disable {channel_id},
// reason remove_knowledge_canvas_tab.
func (a *App) DisableChannelKnowledge(ctx context.Context, ch string) error {
	return a.C.Call(ctx, "ai.channelKnowledge.disable", url.Values{"channel_id": {ch}, "reason": {"remove_knowledge_canvas_tab"}}, nil)
}

// FolderItemCount is +Ltq Iz: the bookmarks shown in folder folderID
// ("" the channel's loose ones), folders aside; useRemoveTab asks before
// deleting a folder that has any.
func (a *App) FolderItemCount(ch, folderID string) int {
	all, _ := a.ChannelBookmarks(ch)
	n := 0
	for _, b := range all {
		if shouldShowBookmark(all, b) && b.Type != "local_folder" && b.ParentID == folderID {
			n++
		}
	}
	return n
}

// AddTabToFolder is the bar's drop of a canvas or list tab into a folder
// tab (E/Yw tf with tabId): the file's bookmark in the folder
// (mapTabToBookmarkData: type file, the tab's label or the file's title,
// its permalink), then removeTab. already reports the folder holding the
// file ("{folderName} already includes {itemName}"); name is the item's
// name for the toast.
func (a *App) AddTabToFolder(ctx context.Context, ch, tabID, label, fileID, folderID string) (name string, already bool, err error) {
	f, err := a.FileInfo(ctx, fileID)
	if err != nil {
		return "", false, err
	}
	name = firstNonEmpty(f.Title, label)
	all, _ := a.ChannelBookmarks(ch)
	for _, b := range all {
		if b.EntityID == fileID && b.ParentID == folderID && b.Type == "file" {
			return name, true, nil
		}
	}
	f.Title = firstNonEmpty(label, f.Title)
	if _, err := a.AddFolderFile(ctx, ch, folderID, f, ""); err != nil {
		return name, false, err
	}
	return name, false, a.RemoveChannelTab(ctx, ch, tabID, false)
}
