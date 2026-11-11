package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// Saved Activity views and the bulk writes, as Slack's web client makes
// them (gantry client-boot-render, webapp build a84662d2; the fetchers
// are client-boot-vendors' createApiMethodFetcher (WH55 r), which
// snake-cases the args and JSON-encodes objects):
//
//   - fetchActivityViews (rsjQ): activity.views → {views, prefs {sort,
//     density}}; a default view type (dms, mentions, threads) missing
//     from views is a hidden one (P0K2 Y2).
//   - createActivityViewsByApi (birT): activity.views.create {name,
//     [emoji], sort, density, filters} → {view}.
//   - updateActivityViewsByApi (+9Gp): activity.views.update {view_id,
//     sort, density, [name, emoji] | [filters]} → {view}.
//   - deleteActivityViewsByApi (r23F): activity.views.delete {view_id};
//     hideDefaultViewByApi / unhideDefaultViewByApi (dWbk):
//     activity.views.hide / unhide {view_type}.
//   - updateAllActivityViewsByApi (UykM): activity.prefs.update {sort,
//     density}.
//   - ActivityFilterBulkAction (m+ho) and useActivityItemShortcuts
//     (WbG1): activity.markRead / markUnread / archive with {reason,
//     entries}; ClearAllFilteredDialog: activity.clearAll {reason,
//     filters}.
//
// Realtime activity_views_updated {views} replaces the saved views
// (sD/F activityViewsUpdated; ActivityViewsEvent).

// ActivityViewFilters are a saved view's filters (the API's view.filters;
// RY04 formatFilterViewState reads them, birT and +9Gp write them).
type ActivityViewFilters struct {
	EntryTypes   []string
	SectionIDs   []string // channel_section_ids
	ChannelIDs   []string // channel_ids
	ArchiveOnly  bool
	UnreadOnly   bool
	PriorityOnly bool // the VIP filter
	// The Salesforce and automation filters (behind experiments Clack
	// doesn't draw): kept so a save doesn't drop them.
	OnlySalesforce, ExcludeAutomations, AutomationsOnly bool
}

type actViewFiltersRaw struct {
	EntryTypes         []string `json:"entry_types"`
	SectionIDs         []string `json:"channel_section_ids"`
	ChannelIDs         []string `json:"channel_ids"`
	ArchiveOnly        bool     `json:"archive_only"`
	UnreadOnly         bool     `json:"unread_only"`
	PriorityOnly       bool     `json:"priority_only"`
	OnlySalesforce     bool     `json:"only_salesforce_channels"`
	ExcludeAutomations bool     `json:"exclude_automations"`
	AutomationsOnly    bool     `json:"automations_only"`
}

type actViewRaw struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Emoji    string             `json:"emoji"`
	ViewType string             `json:"view_type"`
	Position json.RawMessage    `json:"position"`
	Sort     string             `json:"sort"`
	Density  string             `json:"density"`
	Filters  *actViewFiltersRaw `json:"filters"`
}

func (r *actViewRaw) view() ActivityView {
	v := ActivityView{ID: r.ID, Type: r.ViewType, Sort: r.Sort, Density: r.Density, Name: r.Name, Emoji: r.Emoji}
	if p := strings.TrimSpace(string(r.Position)); p != "" && p != "null" {
		var s string
		if json.Unmarshal(r.Position, &s) == nil {
			v.Position = s
		} else {
			v.Position = p
		}
	}
	if f := r.Filters; f != nil {
		v.Filters = ActivityViewFilters{EntryTypes: f.EntryTypes, SectionIDs: f.SectionIDs, ChannelIDs: f.ChannelIDs,
			ArchiveOnly: f.ArchiveOnly, UnreadOnly: f.UnreadOnly, PriorityOnly: f.PriorityOnly,
			OnlySalesforce: f.OnlySalesforce, ExcludeAutomations: f.ExcludeAutomations, AutomationsOnly: f.AutomationsOnly}
	}
	return v
}

// ActivityDefaultViewTypes are the default views a user can hide (P0K2
// zJ: DMs, Mentions, Threads; All can't be hidden).
var ActivityDefaultViewTypes = []string{"dms", "mentions", "threads"}

// ActivityViewsState is activity.views' answer: the saved views in the
// server's order, the global prefs (sort and density, which apply to
// every view with activity_global_settings on) and the default view types
// the user hid.
type ActivityViewsState struct {
	Views         []ActivityView
	Sort, Density string
	Hidden        []string
}

func actViewsState(views []actViewRaw) ActivityViewsState {
	var st ActivityViewsState
	have := map[string]bool{}
	for i := range views {
		st.Views = append(st.Views, views[i].view())
		have[views[i].ViewType] = true
	}
	for _, t := range ActivityDefaultViewTypes {
		if !have[t] {
			st.Hidden = append(st.Hidden, t)
		}
	}
	return st
}

// ActivityViewsState is fetchActivityViews: activity.views.
func (a *App) ActivityViewsState(ctx context.Context) (ActivityViewsState, error) {
	var res struct {
		Views []actViewRaw `json:"views"`
		Prefs *struct {
			Sort    string `json:"sort"`
			Density string `json:"density"`
		} `json:"prefs"`
	}
	if err := a.C.Call(ctx, "activity.views", url.Values{}, &res); err != nil {
		return ActivityViewsState{}, err
	}
	st := actViewsState(res.Views)
	if res.Prefs != nil {
		st.Sort, st.Density = res.Prefs.Sort, res.Prefs.Density
	}
	return st, nil
}

// ActivityViewsEvent reads a realtime activity_views_updated event's
// views (ok is false for an event without any: Slack ignores those).
func ActivityViewsEvent(raw json.RawMessage) (ActivityViewsState, bool) {
	var e struct {
		Views []actViewRaw `json:"views"`
	}
	if json.Unmarshal(raw, &e) != nil || len(e.Views) == 0 {
		return ActivityViewsState{}, false
	}
	return actViewsState(e.Views), true
}

// createFilters is birT's filters: only what's set, in Slack's order.
func (f ActivityViewFilters) createJSON() string {
	type raw struct {
		EntryTypes         []string `json:"entry_types,omitempty"`
		SectionIDs         []string `json:"channel_section_ids,omitempty"`
		ArchiveOnly        bool     `json:"archive_only,omitempty"`
		UnreadOnly         bool     `json:"unread_only,omitempty"`
		PriorityOnly       bool     `json:"priority_only,omitempty"`
		ChannelIDs         []string `json:"channel_ids,omitempty"`
		OnlySalesforce     bool     `json:"only_salesforce_channels,omitempty"`
		ExcludeAutomations bool     `json:"exclude_automations,omitempty"`
		AutomationsOnly    bool     `json:"automations_only,omitempty"`
	}
	b, _ := json.Marshal(raw{f.EntryTypes, f.SectionIDs, f.ArchiveOnly, f.UnreadOnly, f.PriorityOnly, f.ChannelIDs,
		f.OnlySalesforce, f.ExcludeAutomations, f.AutomationsOnly})
	return string(b)
}

// updateJSON is +9Gp's filters: every key, empty lists as null.
func (f ActivityViewFilters) updateJSON() string {
	null := func(s []string) []string {
		if len(s) == 0 {
			return nil
		}
		return s
	}
	type raw struct {
		EntryTypes         []string `json:"entry_types"`
		SectionIDs         []string `json:"channel_section_ids"`
		ChannelIDs         []string `json:"channel_ids"`
		ArchiveOnly        bool     `json:"archive_only"`
		UnreadOnly         bool     `json:"unread_only"`
		PriorityOnly       bool     `json:"priority_only"`
		OnlySalesforce     bool     `json:"only_salesforce_channels"`
		ExcludeAutomations bool     `json:"exclude_automations"`
		AutomationsOnly    bool     `json:"automations_only"`
	}
	b, _ := json.Marshal(raw{null(f.EntryTypes), null(f.SectionIDs), null(f.ChannelIDs), f.ArchiveOnly, f.UnreadOnly,
		f.PriorityOnly, f.OnlySalesforce, f.ExcludeAutomations, f.AutomationsOnly})
	return string(b)
}

func actViewDensity(d string) string {
	if d == "dense" {
		return "dense"
	}
	return "compact"
}

func actViewSort(s string) string {
	if s == "" {
		return "newest"
	}
	return s
}

// CreateActivityView is createActivityViewsByApi: activity.views.create
// {name, [emoji], sort, density, filters} for v (its ID and Type unused);
// Slack answers the new view.
func (a *App) CreateActivityView(ctx context.Context, v ActivityView) (ActivityView, error) {
	p := url.Values{"name": {v.Name}, "sort": {actViewSort(v.Sort)}, "density": {actViewDensity(v.Density)},
		"filters": {v.Filters.createJSON()}, "reason": {"[Activity Views] Create views by API"}}
	if v.Emoji != "" {
		p.Set("emoji", v.Emoji)
	}
	return a.viewCall(ctx, "activity.views.create", p)
}

// RenameActivityView is "Rename view" (ActivityFilterSaveViewModal
// edit_name → updateActivityViewsByApi {name, emoji}): activity.views.update
// {view_id, sort, density, name, emoji}. name "" keeps the name (Slack
// sends it only when it changed).
func (a *App) RenameActivityView(ctx context.Context, v ActivityView, name, emoji string) (ActivityView, error) {
	p := url.Values{"view_id": {v.ID}, "sort": {actViewSort(v.Sort)}, "density": {actViewDensity(v.Density)},
		"emoji": {emoji}, "reason": {"[Activity Views] Update views by API"}}
	if name != "" {
		p.Set("name", name)
	}
	return a.viewCall(ctx, "activity.views.update", p)
}

// SaveActivityViewFilters is "Save as … ▸ Current view"
// (updateActivityViewsByApi {isAutoSave: false}): activity.views.update
// {view_id, sort, density, filters} with v's filters.
func (a *App) SaveActivityViewFilters(ctx context.Context, v ActivityView) (ActivityView, error) {
	p := url.Values{"view_id": {v.ID}, "sort": {actViewSort(v.Sort)}, "density": {actViewDensity(v.Density)},
		"filters": {v.Filters.updateJSON()}, "reason": {"[Activity Views] Update views by API"}}
	return a.viewCall(ctx, "activity.views.update", p)
}

// MoveActivityView is a tab dropped in a new place (GuYW onTabDragEnd →
// 4gYA R → updateActivityViewsByApi {nextViewId, isAutoSave}):
// activity.views.update {view_id, sort, density, next_view_id} for the
// dragged view v (its sort and density as the page shows them); next is
// the view now after it, "" when it went last.
func (a *App) MoveActivityView(ctx context.Context, v ActivityView, next string) (ActivityView, error) {
	p := url.Values{"view_id": {v.ID}, "sort": {actViewSort(v.Sort)}, "density": {actViewDensity(v.Density)},
		"next_view_id": {next}, "reason": {"[Activity Views] Update views by API"}}
	return a.viewCall(ctx, "activity.views.update", p)
}

func (a *App) viewCall(ctx context.Context, method string, p url.Values) (ActivityView, error) {
	var res struct {
		View *actViewRaw `json:"view"`
	}
	if err := a.C.Call(ctx, method, p, &res); err != nil {
		return ActivityView{}, err
	}
	if res.View == nil {
		return ActivityView{}, nil
	}
	return res.View.view(), nil
}

// DeleteActivityView is deleteActivityViewsByApi: activity.views.delete
// {view_id}.
func (a *App) DeleteActivityView(ctx context.Context, id string) error {
	return a.C.Call(ctx, "activity.views.delete", url.Values{"view_id": {id}, "reason": {"[Activity Views] Delete views by API"}}, nil)
}

// HideActivityView is "Hide view" on a default tab (hideDefaultViewByApi):
// activity.views.hide {view_type}; UnhideActivityView is the "+" menu's
// Hidden views row (activity.views.unhide).
func (a *App) HideActivityView(ctx context.Context, viewType string) error {
	return a.C.Call(ctx, "activity.views.hide", url.Values{"view_type": {viewType}, "reason": {"[Activity Views] Hide default view by API"}}, nil)
}

func (a *App) UnhideActivityView(ctx context.Context, viewType string) error {
	return a.C.Call(ctx, "activity.views.unhide", url.Values{"view_type": {viewType}, "reason": {"[Activity Views] Unhide default view by API"}}, nil)
}

// SetActivityPrefs is updateAllActivityViewsByApi: activity.prefs.update
// with the sort ("newest", "vip_unreads_first") and/or the density
// ("compact", "dense") that changed; "" leaves one out.
func (a *App) SetActivityPrefs(ctx context.Context, sort, density string) error {
	p := url.Values{"reason": {"[Activity Views] Update global activity prefs by API"}}
	if sort != "" {
		p.Set("sort", sort)
	}
	if density != "" {
		p.Set("density", density)
	}
	return a.C.Call(ctx, "activity.prefs.update", p, nil)
}

// ---- bulk ----

type actBulkEntry struct {
	Type   string `json:"type"`
	Key    string `json:"key"`
	TS     string `json:"ts,omitempty"`
	FeedTS string `json:"feed_ts"`
}

func actBulkEntries(items []ActivityItem, ts bool) string {
	es := make([]actBulkEntry, 0, len(items))
	for _, it := range items {
		e := actBulkEntry{Type: it.Type, Key: it.Key, FeedTS: it.FeedTS}
		if ts {
			e.TS = it.FeedTS // the formatted item's ts is its feed_ts (/29I)
		}
		es = append(es, e)
	}
	b, _ := json.Marshal(es)
	return string(b)
}

// MarkActivityReadBulk is "Mark selected as read" and Alt+U
// (ActivityFilterBulkAction, useActivityItemShortcuts): activity.markRead
// {reason: bulk_mark_read, entries: [{type, key, feed_ts}]}.
func (a *App) MarkActivityReadBulk(ctx context.Context, items ...ActivityItem) error {
	return a.C.Call(ctx, "activity.markRead", url.Values{"reason": {"bulk_mark_read"}, "entries": {actBulkEntries(items, false)}}, nil)
}

// MarkActivityUnreadBulk is "Mark selected as unread": activity.markUnread
// {reason: bulk_mark_unread, entries: [{type, key, ts, feed_ts}]}.
func (a *App) MarkActivityUnreadBulk(ctx context.Context, items ...ActivityItem) error {
	return a.C.Call(ctx, "activity.markUnread", url.Values{"reason": {"bulk_mark_unread"}, "entries": {actBulkEntries(items, true)}}, nil)
}

// ClearActivityBulk is "Clear selected" and Alt+C: activity.archive
// {reason: bulk_archive, entries: [{type, key, feed_ts}]}.
func (a *App) ClearActivityBulk(ctx context.Context, items ...ActivityItem) error {
	return a.C.Call(ctx, "activity.archive", url.Values{"reason": {"bulk_archive"}, "entries": {actBulkEntries(items, false)}}, nil)
}

// ActivityClearFilters are ClearAllFilteredDialog's filters (FtTX oe with
// readState reads_and_unreads, then PK without reminders in Activity).
type ActivityClearFilters struct {
	EntryTypes   []string `json:"entry_types,omitempty"`
	ChannelIDs   []string `json:"channel_ids,omitempty"`
	SectionIDs   []string `json:"channel_section_ids,omitempty"`
	UnreadOnly   bool     `json:"unread_only,omitempty"`
	PriorityOnly bool     `json:"priority_only,omitempty"`
	ArchiveOnly  bool     `json:"archive_only,omitempty"`
	ReadState    string   `json:"read_state,omitempty"`
}

// ClearAllActivity is "Clear everything" once everything matching the
// filters is selected (ClearAllFilteredDialog's Yes): activity.clearAll
// {reason: server-side-select-all-clear, filters}.
func (a *App) ClearAllActivity(ctx context.Context, f ActivityClearFilters) error {
	b, _ := json.Marshal(f)
	return a.C.Call(ctx, "activity.clearAll", url.Values{"reason": {"server-side-select-all-clear"}, "filters": {string(b)}}, nil)
}
