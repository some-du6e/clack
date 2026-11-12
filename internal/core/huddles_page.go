package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// The Huddles page's API, as Slack's page calls it (gantry-v2 huddles-page
// chunk, Igs2 HuddlesNewUserPage): huddles.history for "Recent huddles"
// (fetchHuddleHistory / fetchAndUpsertHuddleHistory, client-boot-shared
// ZTSP; the filters' params from transformFiltersForApi, DQb+),
// huddles.get for a live huddle's card (osUg useHuddleInfo, reason
// huddle-notify-member), huddles.listBackgrounds for the cards' pictures
// (maybeFetchBackgrounds, 5BO1) and users.interactions.list / .set for
// the hero banner's dismissal (loadUserInteractions /
// recordUserInteraction, Xghl). Array arguments go out the way Slack's
// getFormData (QwSy) appends a string array: comma-joined. Slack's
// _x_reason metadata isn't sent (Clack's client sends it for no call).

// HuddleFilters are the page's past-huddles filters (the huddleHistory
// slice's filters) as transformFiltersForApi sends them.
type HuddleFilters struct {
	Users, Channels []string
	// StartTS and EndTS are start_date_ts / end_date_ts (unix seconds as
	// strings): the day after the date filter's "after" and the end of the
	// day before its "before".
	StartTS, EndTS string
	MissedOnly     bool // missed_huddles_only (type "missed")
	UsersOnly      bool // users_only ("Include only these people")
	HasNotes       bool // has_huddle_notes (Has: Meeting notes)
	HasThread      bool // has_thread_replies (Has: Thread)
	HasArtifacts   bool // has_artifacts (Has: Files & links)
}

// Set reports whether any filter value is set: Slack keeps a fetch as the
// unfiltered list (setUnfilteredHuddlesList) only when none is
// (`Object.values(args).some(v => v !== undefined && v !== false)`).
func (f *HuddleFilters) Set() bool {
	return f != nil && (len(f.Users) > 0 || len(f.Channels) > 0 || f.StartTS != "" || f.EndTS != "" ||
		f.MissedOnly || f.UsersOnly || f.HasNotes || f.HasThread || f.HasArtifacts)
}

// HuddleHistoryOpts is one huddles.history call.
type HuddleHistoryOpts struct {
	Cursor string
	// Filters nil is the page's first fetch (fetchHuddleHistory({})): just
	// the limit. Non-nil adds transformFiltersForApi's args, whose two
	// booleans always go out, false included.
	Filters *HuddleFilters
}

// huddleHistoryParams builds huddles.history's form: limit 30 (Slack's
// saved.list-sized page), the cursor when paging, then the filters.
func huddleHistoryParams(o HuddleHistoryOpts) url.Values {
	p := url.Values{"limit": {"30"}}
	if o.Cursor != "" {
		p.Set("cursor", o.Cursor)
	}
	f := o.Filters
	if f == nil {
		return p
	}
	if len(f.Users) > 0 {
		p.Set("users", strings.Join(f.Users, ","))
	}
	if len(f.Channels) > 0 {
		p.Set("channels", strings.Join(f.Channels, ","))
	}
	if f.StartTS != "" {
		p.Set("start_date_ts", f.StartTS)
	}
	if f.EndTS != "" {
		p.Set("end_date_ts", f.EndTS)
	}
	p.Set("missed_huddles_only", strconv.FormatBool(f.MissedOnly))
	p.Set("users_only", strconv.FormatBool(f.UsersOnly && len(f.Users) > 0))
	for k, on := range map[string]bool{"has_huddle_notes": f.HasNotes, "has_thread_replies": f.HasThread, "has_artifacts": f.HasArtifacts} {
		if on {
			p.Set(k, "true")
		}
	}
	return p
}

// HuddleHistoryPage is one page of huddles.history: the huddles (newest
// first, as the server sends them) and the next cursor ("" at the end).
func (a *App) HuddleHistoryPage(ctx context.Context, o HuddleHistoryOpts) ([]Huddle, string, error) {
	var res struct {
		Huddles []json.RawMessage `json:"huddles"`
		Rooms   []json.RawMessage `json:"rooms"`
		Meta    struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := a.C.Call(ctx, "huddles.history", huddleHistoryParams(o), &res); err != nil {
		return nil, "", err
	}
	out := parseHuddles(append(res.Huddles, res.Rooms...), false)
	a.ensureHuddleUsers(ctx, out)
	return out, res.Meta.NextCursor, nil
}

// HuddleInfo is huddles.get {huddle_id}: the huddle object a live
// huddle's card on the page reads (its topic, thread, background and
// participants).
func (a *App) HuddleInfo(ctx context.Context, id string) (Huddle, error) {
	var res struct {
		Huddle json.RawMessage `json:"huddle"`
	}
	p := url.Values{"huddle_id": {id}}
	if err := a.C.Call(ctx, "huddles.get", p, &res); err != nil {
		return Huddle{}, err
	}
	h, ok := ParseHuddle(res.Huddle)
	if !ok {
		return Huddle{}, errors.New("huddle_not_found")
	}
	a.ensureHuddleUsers(ctx, []Huddle{h})
	return h, nil
}

// HuddleBackground is one of huddles.listBackgrounds' pictures, as
// maybeFetchBackgrounds stores it.
type HuddleBackground struct {
	ID, Name, Artist string
	URL, PreviewURL  string
	CategoryID       string
	CategoryName     string
	CategoryOrder    int
}

// HuddleBackgrounds is huddles.listBackgrounds (reason
// canvas-backgrounds-list), in the server's order.
func (a *App) HuddleBackgrounds(ctx context.Context) ([]HuddleBackground, error) {
	var res struct {
		Backgrounds []struct {
			ID            json.RawMessage `json:"id"`
			Name          string          `json:"name"`
			Artist        string          `json:"artist"`
			URL           string          `json:"url"`
			PreviewURL    string          `json:"preview_url"`
			CategoryID    json.RawMessage `json:"category_id"`
			CategoryName  string          `json:"category_name"`
			CategoryOrder int             `json:"category_order"`
		} `json:"backgrounds"`
	}
	if err := a.C.Call(ctx, "huddles.listBackgrounds", url.Values{}, &res); err != nil {
		return nil, err
	}
	out := make([]HuddleBackground, 0, len(res.Backgrounds))
	for _, b := range res.Backgrounds {
		out = append(out, HuddleBackground{ID: looseString(b.ID), Name: b.Name, Artist: b.Artist, URL: b.URL,
			PreviewURL: b.PreviewURL, CategoryID: looseString(b.CategoryID), CategoryName: b.CategoryName,
			CategoryOrder: b.CategoryOrder})
	}
	return out, nil
}

// UserInteraction is one users.interactions record.
type UserInteraction struct {
	Component, Type, Step string
	Count                 int
}

// UserInteractions is users.interactions.list for the given component
// and type pairs (loadUserInteractions): the recorded ones, keyed
// "COMPONENT-TYPE-STEP" like Slack's store (makeInteractionKey, step
// DEFAULT when unset).
func (a *App) UserInteractions(ctx context.Context, pairs ...[2]string) (map[string]UserInteraction, error) {
	comps := make([]string, 0, len(pairs))
	types := make([]string, 0, len(pairs))
	for _, p := range pairs {
		comps = append(comps, p[0])
		types = append(types, p[1])
	}
	f := url.Values{"components": {strings.Join(comps, ",")}, "types": {strings.Join(types, ",")}}
	var res struct {
		Interactions []struct {
			Component string `json:"component"`
			Type      string `json:"type"`
			Step      string `json:"step"`
			Count     int    `json:"count"`
		} `json:"interactions"`
	}
	if err := a.C.Call(ctx, "users.interactions.list", f, &res); err != nil {
		return nil, err
	}
	out := map[string]UserInteraction{}
	for _, it := range res.Interactions {
		if it.Step == "" {
			it.Step = "DEFAULT"
		}
		out[InteractionKey(it.Component, it.Type, it.Step)] = UserInteraction{Component: it.Component, Type: it.Type,
			Step: it.Step, Count: it.Count}
	}
	return out, nil
}

// InteractionKey is makeInteractionKey: component-type-step.
func InteractionKey(component, typ, step string) string {
	if step == "" {
		step = "DEFAULT"
	}
	return component + "-" + typ + "-" + step
}

// RecordUserInteraction is recordUserInteraction's users.interactions.set
// {component, type}.
func (a *App) RecordUserInteraction(ctx context.Context, component, typ string) error {
	return a.C.Call(ctx, "users.interactions.set", url.Values{"component": {component}, "type": {typ}}, nil)
}

// SortHuddlesNewest orders huddles by date_start, newest first (stable).
func SortHuddlesNewest(hs []Huddle) {
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].Start > hs[j].Start })
}
