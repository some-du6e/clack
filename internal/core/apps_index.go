package core

// The apps browser's reads (gantry-v2 build d3de3a487b1c, 2026-11-12):
//
//   - fetchDefaultApps (boot-shared exHJ Q): apps.index.list {limit: 50,
//     filter: the selected filter (0, none), team_id on an org-level
//     install}; the answer's categories and total_apps_installed.
//   - searchApps (boot-shared 5Rjf): apps.index.search {limit: 10 (48 in
//     the paged app launcher), query, filter}; searchAppsIfNeeded picks
//     the list for an empty query.
//   - receiveResults (P6jq) flattens the categories with getResultRows
//     (5EdJ l): a "category" row then an "app" row per app, for every
//     category with apps; the "Ai01" category's next_cursor pages it.
//   - an app can be added to the channel (client-boot AfRX vS) when it is
//     installed, has a bot user, the bot user is known and the channel
//     membership check (getChannelMembershipInfoForUsers: edge
//     channels/membership, ChannelMembership) says it isn't a member;
//     not in a DM or group DM unless it is an app user.

import (
	"context"
	"net/url"
	"strconv"
)

// AppIndexEntry is one app of apps.index.list / apps.index.search.
type AppIndexEntry struct {
	AppID       string            `json:"app_id"`
	Name        string            `json:"name"`
	Description string            `json:"short_description"`
	Icons       map[string]string `json:"icons"`
	BotUserID   string            `json:"bot_user_id"`
	BotID       string            `json:"bot_id"`
	Installed   bool              `json:"is_installed"`
	Workflow    bool              `json:"is_workflow_app"`
}

// Icon is getAppIcon (i+lO A) for a size: image_<size>, else the nearest
// one the index sent.
func (e AppIndexEntry) Icon(size int) string {
	if s := e.Icons["image_"+strconv.Itoa(size)]; s != "" {
		return s
	}
	for _, n := range []int{72, 48, 64, 36, 96, 128, 192, 512, 32, 24} {
		if s := e.Icons["image_"+strconv.Itoa(n)]; s != "" {
			return s
		}
	}
	return ""
}

// AppCategory is one category of the answer.
type AppCategory struct {
	ID          string          `json:"id"`
	DisplayName string          `json:"display_name"`
	Apps        []AppIndexEntry `json:"apps"`
	NextCursor  string          `json:"next_cursor"`
}

// AppsIndex is the answer.
type AppsIndex struct {
	Categories     []AppCategory `json:"categories"`
	TotalInstalled int           `json:"total_apps_installed"`
}

// AppsBrowserRow is one row of getResultRows.
type AppsBrowserRow struct {
	Kind     string // category | app | loading
	Category AppCategory
	App      AppIndexEntry
}

// AppsResultRows is getResultRows: each category with apps, its apps
// under it, and a loading row when the Ai01 category pages on.
func AppsResultRows(cats []AppCategory) []AppsBrowserRow {
	var out []AppsBrowserRow
	cursor := ""
	for _, c := range cats {
		if c.ID == "Ai01" && c.NextCursor != "" {
			cursor = c.NextCursor
		}
		if len(c.Apps) == 0 {
			continue // filter-empty states need a selected filter; Slack's browser has none
		}
		out = append(out, AppsBrowserRow{Kind: "category", Category: c})
		for _, a := range c.Apps {
			out = append(out, AppsBrowserRow{Kind: "app", Category: c, App: a})
		}
	}
	if cursor != "" {
		out = append(out, AppsBrowserRow{Kind: "loading"})
	}
	return out
}

// AppsIndexList is fetchDefaultApps' call.
func (a *App) AppsIndexList(ctx context.Context) (AppsIndex, error) {
	var r AppsIndex
	err := a.C.Call(ctx, "apps.index.list", url.Values{"limit": {"50"}, "filter": {"0"}}, &r)
	return r, err
}

// AppsIndexSearch is searchApps' call.
func (a *App) AppsIndexSearch(ctx context.Context, query string) (AppsIndex, error) {
	var r AppsIndex
	err := a.C.Call(ctx, "apps.index.search", url.Values{"limit": {"10"}, "query": {query}, "filter": {"0"}}, &r)
	return r, err
}

// AppsAddable is AfRX vS for every app of rows in ch: installed, with a
// bot user that channels/membership says isn't a member. A failed check
// leaves every app unknown, so none can be added (the row offers View).
func (a *App) AppsAddable(ctx context.Context, ch string, rows []AppsBrowserRow) map[string]bool {
	dm := false
	if k := a.kindOf(ch); k == "im" || k == "mpim" {
		dm = true
	}
	var bots []string
	for _, r := range rows {
		if r.Kind != "app" || !r.App.Installed || r.App.BotUserID == "" {
			continue
		}
		if dm { // in a DM or group DM only an app user can be added (Umd7 $S)
			if u, ok := a.User(r.App.BotUserID); !ok || !u.AppUser {
				continue
			}
		}
		bots = append(bots, r.App.BotUserID)
	}
	out := map[string]bool{}
	if len(bots) == 0 || ch == "" {
		return out
	}
	m, err := a.ChannelMembership(ctx, ch, bots)
	if err != nil {
		return out
	}
	for _, b := range bots {
		if in, known := m[b]; known && !in {
			out[b] = true
		}
	}
	return out
}
