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

// StaticTab is isStaticChannelTab for the tabs Clack draws: Messages (the
// channel tab) never moves and takes no after.
func StaticTab(t TabState) bool { return t.Type == "channel" || t.ID == "channel" }

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

// CanEditTabsChecked is useCanEditTabs (mwX5 Ln) with the reads
// CanEditTabs (channel_pins.go) skips, for a menu or card about to edit: a
// conversation the viewer is in and not archived; with
// properties.who_can_manage_tabs "channel_managers" an admin or owner who
// isn't a guest, the channel's managers or its creator; on Enterprise Grid
// with no setting the MANAGE_CHANNEL_TABS grant (permissions/info); else
// yes. It may call the API (managers, the grant).
func (a *App) CanEditTabsChecked(ctx context.Context, ch string) bool {
	c, ok := a.Conv(ch)
	if !ok || c.IsArchived || !c.Member || c.Kind == "im" {
		return false
	}
	var props ChannelProps
	if sc, ok := a.S.Conv(ch); ok {
		props = ParseChannelProps(sc.Properties)
	}
	self := a.S.SelfID()
	if props.WhoCanManageTabs == "channel_managers" {
		if u, ok := a.S.User(self); ok && (u.IsAdmin || u.IsOwner) && !u.IsRestricted && !u.IsUltraRestricted {
			return true
		}
		if c.Creator == self {
			return true
		}
		ms, err := a.Managers(ctx, ch)
		if err != nil {
			return false
		}
		for _, m := range ms {
			if m == self {
				return true
			}
		}
		return false
	}
	if a.C.Creds.EnterpriseID != "" && props.WhoCanManageTabs == "" {
		// getPermissionForChannel: the permission store's fetchPermissions
		// (edge permissions/info) for MANAGE_CHANNEL_TABS
		body := map[string]any{"user_id": self, "permissions": []string{PermManageChannelTabs}, "channel_id": ch}
		var r struct {
			Allowed []string `json:"allowed"`
		}
		if err := a.C.Edge(ctx, "permissions/info", body, &r); err != nil {
			return false
		}
		for _, p := range r.Allowed {
			if p == PermManageChannelTabs {
				return true
			}
		}
		return false
	}
	return true
}
