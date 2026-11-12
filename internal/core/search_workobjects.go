package core

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/some-du6e/clack/internal/workobject"
)

// Work objects in search (work_objects_message_search_xp_v2; client-boot
// rZ6M searchWorkObjects, build 4d0c78085232): a message search also
// asks search.modules.workObjects (Is0w AV: query, reason
// "search-work-objects", client_req_id, highlight, the messages tab's
// sort and sort_dir, search_only_my_channels, query_rewrite_disabled;
// search_context desktop_work_object_tab). Its items carry
// work_object_unfurl (an entity, as message attachments do); the
// non-Tableau ones are grouped by app_id into one source per app (the
// view id NS_work_object.<app_id>, jKmc), in the order the apps first
// appear, each module the app's items.

// WorkObjectHit is one search.modules.workObjects item.
type WorkObjectHit struct {
	Entity *workobject.Entity
	// ConnectorAppID and ConnectorEnabled are the item's connector
	// (enterprise search), when it names one.
	ConnectorAppID   string
	ConnectorEnabled *bool
}

// WorkObjectSource is one app's results: its source in the results
// page's source menu.
type WorkObjectSource struct {
	AppID, AppName string
	Hits           []WorkObjectHit
	// Expanded: some hit has an expanded layout (searchWorkObjects'
	// shouldRender).
	Expanded bool
}

// WorkObjectViewID is the source's view id (createWorkObjectViewId).
func WorkObjectViewID(appID string) string { return "NS_work_object." + appID }

// SearchWorkObjects is searchWorkObjects for query, with the messages
// tab's sort (score, timestamp, oldest).
func (a *App) SearchWorkObjects(ctx context.Context, query, sort string, s SearchSession) ([]WorkObjectSource, error) {
	if s.ReqID == "" {
		s = NewSearchSession()
	}
	p := url.Values{"query": {query}, "reason": {"search-work-objects"}, "client_req_id": {s.QueryID}, "highlight": {"true"},
		"search_only_my_channels": {boolStr(a.PrefBool("search_only_my_channels", false))}, "query_rewrite_disabled": {"false"},
		"search_context": {"desktop_work_object_tab"}}
	searchSortParams(p, sort)
	var raw struct {
		Items []struct {
			Unfurl           json.RawMessage `json:"work_object_unfurl"`
			ConnectorAppID   string          `json:"connector_app_id"`
			ConnectorEnabled *bool           `json:"connector_is_enabled"`
		} `json:"items"`
	}
	if err := a.C.Call(ctx, "search.modules.workObjects", p, &raw); err != nil {
		return nil, err
	}
	var out []WorkObjectSource
	idx := map[string]int{}
	for _, it := range raw.Items {
		e := workobject.Parse(it.Unfurl)
		if e == nil || e.Type == workobject.TypeTableau || e.AppID == "" {
			continue
		}
		i, ok := idx[e.AppID]
		if !ok {
			i = len(out)
			idx[e.AppID] = i
			out = append(out, WorkObjectSource{AppID: e.AppID, AppName: e.AppName})
		}
		src := &out[i]
		src.Hits = append(src.Hits, WorkObjectHit{Entity: e, ConnectorAppID: it.ConnectorAppID, ConnectorEnabled: it.ConnectorEnabled})
		if src.AppName == "" {
			src.AppName = e.AppName
		}
		if e.Expanded() != nil {
			src.Expanded = true
		}
	}
	return out, nil
}
