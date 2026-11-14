package core

// Search the way Slack's web client does it (captured from app.slack.com,
// 2026-11-08, and the CDN source's searchModules fetcher):
//
//   - the results page fires search.modules.<module> for messages, dms,
//     files, canvases (files with "type:quip"), channels and people with
//     one search_session_id / client_req_id; the inactive modules only
//     give the counts in the [Messages ▾] source menu.
//   - every module call carries the fetcher's base args (extracts,
//     highlight, max_extract_len 200, extra_message_data, no_user_profile,
//     include_files_shares, file_title_only, query_rewrite_disabled) plus
//     the module's own (getMessageSearchApiArguments and friends).
//   - "Exclude bot messages" and "Only my channels" are the user prefs
//     search_exclude_bots / search_only_my_channels, sent on every
//     messages and files search; "Exact matches only" is
//     search_exact_matches_only.
//   - the popover's "Recent messages" are search.inline (count 3,
//     extract_len 110); its file and canvas rows search.autocomplete.files;
//     a submitted search is remembered with search.save.
//   - search in channel (⌘F) is search.modules.messages for
//     "in:<#C> query", count 10, search_context search_in_channel.

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// SearchSession is one search's ids: Slack's search_session_id, the
// client_req_id every module call of one query shares, and the
// query_session_id search feedback reports (getQuerySessionId).
type SearchSession struct{ ID, ReqID, QueryID string }

// NewSearchSession starts a search session.
func NewSearchSession() SearchSession {
	return SearchSession{ID: clientMsgID(), ReqID: clientMsgID(), QueryID: clientMsgID()}
}

// SearchModule names (Slack's eG.Qt / MT).
const (
	ModMessages = "messages"
	ModDMs      = "dms"
	ModFiles    = "files"
	ModCanvases = "canvases" // search.modules.files with type:quip
	ModChannels = "channels"
	ModPeople   = "people"
)

// FilterSuggestion is a facet from filter_suggestions (the From / In
// menus' suggested people and channels, with their result counts).
type FilterSuggestion struct {
	ID      string
	Count   int
	Checked bool
}

// moduleContext is each module's search_context on the results page.
var moduleContext = map[string]string{
	ModMessages: "desktop_messages_tab", ModDMs: "desktop_dms_tab", ModFiles: "desktop_files_tab",
	ModCanvases: "desktop_canvases_tab", ModChannels: "desktop_channels_tab", ModPeople: "desktop_people_tab",
}

// moduleParams are the searchModules fetcher's base args.
func moduleParams(module, query string, page, count int, sctx string, s SearchSession) url.Values {
	if page < 1 {
		page = 1
	}
	if s.ID == "" {
		s = NewSearchSession()
	}
	return url.Values{"module": {module}, "query": {query}, "page": {strconv.Itoa(page)},
		"client_req_id": {s.ReqID}, "search_session_id": {s.ID},
		"extracts": {"1"}, "highlight": {"1"}, "max_extract_len": {"200"}, "extra_message_data": {"1"},
		"no_user_profile": {"1"}, "count": {strconv.Itoa(count)}, "file_title_only": {"false"},
		"query_rewrite_disabled": {"false"}, "include_files_shares": {"1"}, "search_context": {sctx}}
}

// SearchSortParams is getSortParams (rZ6M) for the other tabs' sort
// values (cX/T: files and canvases score, timestamp, timestamp_asc,
// created, name, name_desc; channels relevant, created, created_asc,
// member_count, member_count_asc, name, name_desc; people relevant, name,
// name_desc): its sort and sort_dir.
func SearchSortParams(v string) (sort, dir string) {
	switch v {
	case "recentMessages", "timestamp", "newestTopResults":
		return "timestamp", "desc"
	case "relevantMessages", "relevantTopResults", "score":
		return "score", "desc"
	case "timestamp_asc", "oldestTopResults":
		return "timestamp", "asc"
	case "created":
		return "created", "desc"
	case "created_asc":
		return "created", "asc"
	case "name":
		return "name", "asc"
	case "name_desc":
		return "name", "desc"
	case "member_count":
		return "member_count", "desc"
	case "member_count_asc":
		return "member_count", "asc"
	}
	return v, "desc"
}

// searchSortParams is getSortParams for the messages tab's sorts:
// relevance is score/desc, newest timestamp/desc, oldest timestamp/asc.
func searchSortParams(p url.Values, sort string) {
	switch sort {
	case "timestamp", "newest":
		p.Set("sort", "timestamp")
		p.Set("sort_dir", "desc")
	case "oldest":
		p.Set("sort", "timestamp")
		p.Set("sort_dir", "asc")
	default:
		p.Set("sort", "score")
		p.Set("sort_dir", "desc")
	}
}

// requestContext is getAdditionalSearchApiArguments' request_context.
func requestContext(activeCID string) string {
	b, _ := json.Marshal(struct {
		ActiveCID string   `json:"active_cid"`
		In        []string `json:"recent_filter_in"`
		From      []string `json:"recent_filter_from"`
	}{activeCID, []string{}, []string{}})
	return string(b)
}

func boolStr(b bool) string { return strconv.FormatBool(b) }

// messageSearchParams builds search.modules.messages (or dms) args.
func messageSearchParams(o SearchOpts, excludeBots, onlyMine bool) url.Values {
	module := ModMessages
	if o.Module == ModDMs {
		module = ModDMs
	}
	count := o.Count
	if count <= 0 {
		count = 20
		if module == ModDMs {
			count = 40
		}
	}
	query := o.Query
	sctx := moduleContext[module]
	if o.InChannel != "" {
		// SearchInChannelView: any in: is replaced by the channel's
		query = "in:<#" + o.InChannel + "> " + strings.TrimSpace(inRef.ReplaceAllString(query, ""))
		sctx = "search_in_channel"
		if o.Count <= 0 {
			count = 10
		}
	}
	if o.Context != "" {
		sctx = o.Context
	}
	p := moduleParams(module, query, o.Page, count, sctx, o.Session)
	if module == ModDMs {
		return p
	}
	if o.InChannel != "" {
		p.Set("search_exclude_bots", "false")
		searchSortParams(p, o.Sort)
		if o.DisableSortOverride {
			p.Set("disable_sort_override", "true") // O8Qt: ...o ? {disable_sort_override: !0} : {}
		}
		return p
	}
	p.Set("search_exclude_bots", boolStr(excludeBots))
	p.Set("search_only_my_channels", boolStr(onlyMine))
	p.Set("spell_correction", "FUZZY_MATCH")
	if o.DisableSortOverride {
		p.Set("disable_sort_override", "true") // getSearchParams: ...i ? {disable_sort_override: !0} : {}
	}
	p.Set("search_only_team", "")
	p.Set("facets_result_count", "5")
	p.Set("query_refinement_suggestions_version", "1")
	p.Set("recent_channels", strings.Join(o.RecentChannels, ","))
	searchSortParams(p, o.Sort)
	if o.ExactOnly {
		p.Set("search_exact_matches_only", "true")
		p.Set("query_rewrite_disabled", "false")
	}
	if o.ExcludeArchived {
		p.Set("channel_type", "exclude_archived")
	}
	p.Set("max_filter_suggestions", "10")
	p.Set("request_context", requestContext(o.ActiveCID))
	p.Set("search_tab_filter", ModMessages)
	p.Set("search_tab_sort", p.Get("sort"))
	return p
}

// inRef matches in:<#C…> / in:<@U…> modifiers.
var inRef = regexp.MustCompile(`(?i)in:<[@#][A-Z0-9]+?[^>]*?>\s*`)

// highlight markers search.modules adds with highlight=1 (U+E000/U+E001),
// raw or JSON-escaped.
var hlRaw = [][]byte{[]byte(""), []byte(""), []byte(``), []byte(``), []byte(``), []byte(``)}

func stripHighlightJSON(b []byte) []byte {
	for _, m := range hlRaw {
		if bytes.Contains(b, m) {
			b = bytes.ReplaceAll(b, m, nil)
		}
	}
	return b
}

// searchItemRaw is one search.modules.messages / dms item.
type searchItemRaw struct {
	Channel struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		IsIM      bool   `json:"is_im"`
		IsMPIM    bool   `json:"is_mpim"`
		IsPrivate bool   `json:"is_private"`
		IsGroup   bool   `json:"is_group"`
		User      string `json:"user"`
	} `json:"channel"`
	Messages []json.RawMessage `json:"messages"`
}

type filterSuggestionRaw struct {
	ID      string `json:"id"`
	Count   int    `json:"facet_count"`
	Checked bool   `json:"checked"`
}

// withoutExcluded drops the channels of search_exclude_channels (Advanced
// > "Exclude these channels from search results:", comma-separated ids)
// from the In suggestions, as parseSuggestions does; Slack's servers
// leave them out of the results.
func (a *App) withoutExcluded(in []FilterSuggestion) []FilterSuggestion {
	ex := SearchExcludedChannels(a.PrefString("search_exclude_channels"))
	if len(ex) == 0 {
		return in
	}
	out := in[:0:0]
	for _, f := range in {
		if !slices.Contains(ex, f.ID) {
			out = append(out, f)
		}
	}
	return out
}

// SearchExcludedChannels splits search_exclude_channels into ids.
func SearchExcludedChannels(v string) []string {
	var out []string
	for _, id := range strings.Split(v, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func filterSuggestions(rs []filterSuggestionRaw) []FilterSuggestion {
	out := make([]FilterSuggestion, 0, len(rs))
	for _, r := range rs {
		out = append(out, FilterSuggestion{ID: r.ID, Count: r.Count, Checked: r.Checked})
	}
	return out
}

// SearchMessages runs a message (or DM) search the way Slack's results
// page does: search.modules.messages with getMessageSearchApiArguments.
func (a *App) SearchMessages(ctx context.Context, o SearchOpts) (SearchResult, error) {
	t0 := time.Now()
	module := ModMessages
	if o.Module == ModDMs {
		module = ModDMs
	}
	p := messageSearchParams(o, a.PrefBool("search_exclude_bots", false), a.PrefBool("search_only_my_channels", false))
	if module == ModMessages {
		// the messages module's loading (search_suggest.go)
		a.ssugg.loading.Add(1)
		defer a.ssugg.loading.Add(-1)
	}
	var raw json.RawMessage
	if err := a.C.Call(ctx, "search.modules."+module, p, &raw); err != nil {
		return SearchResult{}, err
	}
	var res struct {
		Pagination searchPagination `json:"pagination"`
		Items      []searchItemRaw  `json:"items"`
		Filters    struct {
			In   []filterSuggestionRaw `json:"in"`
			From []filterSuggestionRaw `json:"from"`
		} `json:"filter_suggestions"`
		Unfiltered         int                        `json:"unfiltered_result_count"`
		UnfilteredFilter   map[string]json.RawMessage `json:"unfiltered_result_filter"`
		SortForcedToRecent json.RawMessage            `json:"sort_forced_to_recent"`
	}
	if err := json.Unmarshal(stripHighlightJSON(raw), &res); err != nil {
		return SearchResult{}, err
	}
	out := SearchResult{Total: res.Pagination.Total, Page: res.Pagination.Page, Pages: res.Pagination.PageCount,
		FilterIn: a.withoutExcluded(filterSuggestions(res.Filters.In)), FilterFrom: filterSuggestions(res.Filters.From),
		// !0 === a.sort_forced_to_recent (only a JSON true), for messages only
		SortForcedToRecent: module == ModMessages && strings.TrimSpace(string(res.SortForcedToRecent)) == "true"}
	if res.Unfiltered > 0 && res.UnfilteredFilter != nil { // the relaxation selector's count && filter
		out.Unfiltered, out.UnfilteredFilter = res.Unfiltered, res.UnfilteredFilter
	}
	if o.FetchSuggestions && module == ModMessages && o.InChannel == "" {
		a.noteSuggestions(p.Get("query"), out) // parseSuggestions (search_suggest.go)
	}
	if out.Page == 0 {
		out.Page, _ = strconv.Atoi(p.Get("page"))
	}
	var msgs []slack.Message
	type hit struct {
		ch   string
		m    slack.Message
		link string
	}
	var hits []hit
	chans := map[string]bool{}
	for _, it := range res.Items {
		ch := it.Channel.ID
		if ch == "" {
			continue
		}
		chans[ch] = true
		if it.Channel.Name != "" && !it.Channel.IsIM && !it.Channel.IsMPIM {
			a.S.PutName(ch, it.Channel.Name) // names of channels we are not in (metadata)
		}
		for _, mr := range it.Messages {
			var m slack.Message
			if json.Unmarshal(mr, &m) != nil || m.TS == "" {
				continue
			}
			var aux struct {
				Permalink string `json:"permalink"`
			}
			_ = json.Unmarshal(mr, &aux)
			msgs = append(msgs, m)
			hits = append(hits, hit{ch, m, aux.Permalink})
		}
	}
	// the results' people are asked for as their rows draw (lazy_users.go)
	for i := range hits {
		v := a.message(hits[i].ch, &hits[i].m)
		v.Permalink = hits[i].link
		out.Messages = append(out.Messages, v)
	}
	out.Convs = a.convRefs(chans)
	log.Printf("search %s: %d of %d in %s", module, len(out.Messages), out.Total, time.Since(t0).Round(time.Millisecond))
	return out, nil
}

// FileSearchOpts is a Files or Canvases tab search.
type FileSearchOpts struct {
	Query    string
	Canvases bool // the Canvases tab ("type:quip query")
	Page     int
	Count    int // 20; 0 asks for the count only (CountOnly)
	Sort     string
	Session  SearchSession
	// CountOnly sends count 0: the source menu's count for an inactive tab.
	CountOnly bool
}

func fileSearchParams(o FileSearchOpts, excludeBots, onlyMine bool) url.Values {
	count := o.Count
	if count <= 0 {
		count = 20
	}
	if o.CountOnly {
		count = 0
	}
	q, sctx := o.Query, moduleContext[ModFiles]
	if o.Canvases {
		q, sctx = "type:quip "+o.Query, moduleContext[ModCanvases]
	}
	p := moduleParams(ModFiles, q, o.Page, count, sctx, o.Session)
	p.Set("search_exclude_bots", boolStr(excludeBots))
	p.Set("search_only_my_channels", boolStr(onlyMine))
	p.Set("search_only_team", "")
	p.Set("facets_result_count", "5")
	// getAdditionalFileSearchApiArguments: a count-only call is forced to
	// the newest first; the tab's own sort is the Sort select's (kwsV r:
	// Most relevant, score, unless chosen)
	sortBy, dir := "timestamp", "desc"
	if !o.CountOnly {
		v := o.Sort
		if v == "" {
			v = "score"
		}
		sortBy, dir = SearchSortParams(v)
	}
	p.Set("sort", sortBy)
	p.Set("sort_dir", dir)
	p.Set("max_filter_suggestions", "10")
	p.Set("request_context", requestContext(""))
	p.Set("search_tab_filter", ModMessages)
	p.Set("search_tab_sort", "score")
	return p
}

// SearchFiles is the results page's Files or Canvases tab
// (search.modules.files).
func (a *App) SearchFiles(ctx context.Context, o FileSearchOpts) (FilePage, error) {
	p := fileSearchParams(o, a.PrefBool("search_exclude_bots", false), a.PrefBool("search_only_my_channels", false))
	var raw json.RawMessage
	if err := a.C.Call(ctx, "search.modules.files", p, &raw); err != nil {
		return FilePage{}, err
	}
	var res struct {
		Pagination searchPagination `json:"pagination"`
		Items      []browseFileRaw  `json:"items"`
	}
	if err := json.Unmarshal(stripHighlightJSON(raw), &res); err != nil {
		return FilePage{}, err
	}
	out := FilePage{Files: a.filePage(ctx, res.Items), Page: res.Pagination.Page, Pages: res.Pagination.PageCount,
		Total: res.Pagination.Total}
	if out.Page == 0 {
		out.Page, _ = strconv.Atoi(p.Get("page"))
	}
	return out, nil
}

// channelSearchParams is getChannelSearchApiArguments for the results
// page's Channels tab (Most relevant).
func channelSearchParams(query string, page int, s SearchSession, onlyMine bool) url.Values {
	return channelSearchParamsSort(query, "relevant", page, s, onlyMine)
}

// channelSearchParamsSort is channelSearchParams with the Sort select's
// value (SearchSortParams).
func channelSearchParamsSort(query, sortBy string, page int, s SearchSession, onlyMine bool) url.Values {
	return channelTabParams(ChannelTabOpts{Query: query, Sort: sortBy, Page: page, Session: s}, onlyMine, false)
}

// ChannelTabOpts is a Channels tab search: its query, Sort select value
// and the Channel type pill (the All channels pill is the
// search_only_my_channels and search_hide_my_channels prefs).
type ChannelTabOpts struct {
	Query, Sort string
	Page        int
	Session     SearchSession
	// ChannelType is SearchFilterChannelTypes' value (cX/T xV's keys):
	// "" or all, public, private, archived, shared.
	ChannelType string
}

// SearchChannelType is channel_type for a Channel type value
// (getChannelSearchApiArguments: cX/T xV maps public to private_exclude;
// External is external_shared).
func SearchChannelType(t string) string {
	switch t {
	case "public":
		return "private_exclude"
	case "private", "archived":
		return t
	case "shared":
		return "external_shared"
	}
	return "all"
}

// channelTabParams is getChannelSearchApiArguments (client-boot rZ6M):
// search_only_my_channels and exclude_my_channels (+!!search_hide_my_channels)
// are the All channels pill's prefs, channel_type the Channel type pill's.
func channelTabParams(o ChannelTabOpts, onlyMine, hideMine bool) url.Values {
	p := moduleParams(ModChannels, o.Query, o.Page, 20, moduleContext[ModChannels], o.Session)
	p.Set("search_only_my_channels", boolStr(onlyMine))
	p.Set("channel_type", SearchChannelType(o.ChannelType))
	p.Set("external_shared_team_ids", "")
	exclude := "0"
	if hideMine {
		exclude = "1"
	}
	p.Set("exclude_my_channels", exclude)
	p.Set("request_context", requestContext(""))
	p.Set("spell_correction", "FUZZY_MATCH")
	sortBy := o.Sort
	if sortBy == "" {
		sortBy = "relevant"
	}
	sb, dir := SearchSortParams(sortBy)
	p.Set("sort", sb)
	p.Set("sort_dir", dir)
	p.Set("search_only_team", "")
	return p
}

// SearchChannelsTab is the results page's Channels tab
// (search.modules.channels), Most relevant.
func (a *App) SearchChannelsTab(ctx context.Context, query string, page int, s SearchSession) (ChannelPage, error) {
	return a.SearchChannelsTabSort(ctx, query, "relevant", page, s)
}

// SearchChannelsTabSort is SearchChannelsTab with a Sort select value
// (relevant, created, created_asc, member_count, member_count_asc, name,
// name_desc).
func (a *App) SearchChannelsTabSort(ctx context.Context, query, sortBy string, page int, s SearchSession) (ChannelPage, error) {
	return a.SearchChannelsTabOpts(ctx, ChannelTabOpts{Query: query, Sort: sortBy, Page: page, Session: s})
}

// SearchChannelsTabOpts is the Channels tab with its filters: the
// search_only_my_channels and search_hide_my_channels prefs (All
// channels) and o's Channel type.
func (a *App) SearchChannelsTabOpts(ctx context.Context, o ChannelTabOpts) (ChannelPage, error) {
	page := o.Page
	p := channelTabParams(o, a.PrefBool("search_only_my_channels", false), a.PrefBool("search_hide_my_channels", false))
	var raw json.RawMessage
	if err := a.C.Call(ctx, "search.modules.channels", p, &raw); err != nil {
		return ChannelPage{}, err
	}
	var res struct {
		Items      []channelItemRaw `json:"items"`
		Pagination searchPagination `json:"pagination"`
	}
	if err := json.Unmarshal(stripHighlightJSON(raw), &res); err != nil {
		return ChannelPage{}, err
	}
	hs := make([]slack.ChannelHit, 0, len(res.Items))
	for i := range res.Items {
		hs = append(hs, res.Items[i].hit())
	}
	out := ChannelPage{Channels: a.channelHits(hs), Total: res.Pagination.Total, Pages: res.Pagination.PageCount,
		Page: res.Pagination.Page}
	if out.Page == 0 {
		out.Page = max(page, 1)
	}
	return out, nil
}

// peopleSearchParams is the results page's People tab args (Most
// relevant).
func peopleSearchParams(query string, page int, s SearchSession) url.Values {
	return peopleSearchParamsSort(query, "relevant", page, s)
}

// peopleSearchParamsSort is peopleSearchParams with the Sort select's
// value (relevant, name, name_desc).
func peopleSearchParamsSort(query, sortBy string, page int, s SearchSession) url.Values {
	return peopleTabParams(PeopleTabOpts{Query: query, Sort: sortBy, Page: page, Session: s}, false)
}

// PeopleTabOpts is a People tab search: its query, Sort select value and
// filters (the Title and Location pills, More's Account type and
// Celebration).
type PeopleTabOpts struct {
	Query, Sort string
	Page        int
	Session     SearchSession
	// Titles and Locations are profile_title and profile_location.
	Titles, Locations []string
	// AccountType is a cX/T Dx key (owners, admins, full, guests,
	// deactivated, profile_only); "" or all sends no account_type.
	AccountType string
	// Celebrations is profile_celebration (new_hire, anniversary).
	Celebrations []string
}

// SearchAccountTypes is cX/T Dx: account_type's value for each Account
// type row.
var SearchAccountTypes = map[string]string{
	"all": "0,1,2,3,4,5,6,7,8,9,10", "owners": "0,1", "admins": "0,1,2", "full": "0,1,2,3,7",
	"guests": "4,5,8", "deactivated": "6", "profile_only": "9", "frontline": "10",
}

// peopleTabParams is getPeopleSearchApiArguments (client-boot rZ6M): the
// profile_* filters (the people browser's XjMX joins each list with
// commas), hide_deactivated_users from the search_hide_deactivated_users
// pref, account_type unless Any account type.
func peopleTabParams(o PeopleTabOpts, hideDeactivated bool) url.Values {
	p := moduleParams(ModPeople, o.Query, o.Page, 20, moduleContext[ModPeople], o.Session)
	for _, k := range []string{"tz", "external_shared_team_ids", "search_only_team"} {
		p.Set(k, "")
	}
	p.Set("profile_location", strings.Join(o.Locations, ","))
	p.Set("profile_title", strings.Join(o.Titles, ","))
	p.Set("profile_division", "")
	p.Set("profile_department", "")
	p.Set("profile_celebration", strings.Join(o.Celebrations, ","))
	p.Set("custom_fields", "{}")
	hide := "0"
	if hideDeactivated {
		hide = "1"
	}
	p.Set("hide_deactivated_users", hide)
	p.Set("request_context", requestContext(""))
	p.Set("spell_correction", "FUZZY_MATCH")
	sortBy := o.Sort
	if sortBy == "" {
		sortBy = "relevant"
	}
	sb, dir := SearchSortParams(sortBy)
	p.Set("sort", sb)
	p.Set("sort_dir", dir)
	if at, ok := SearchAccountTypes[o.AccountType]; ok && o.AccountType != "all" {
		p.Set("account_type", at)
	}
	return p
}

// SearchPeopleTab is the results page's People tab
// (search.modules.people), Most relevant.
func (a *App) SearchPeopleTab(ctx context.Context, query string, page int, s SearchSession) (PeoplePage, error) {
	return a.SearchPeopleTabSort(ctx, query, "relevant", page, s)
}

// SearchPeopleTabSort is SearchPeopleTab with a Sort select value.
func (a *App) SearchPeopleTabSort(ctx context.Context, query, sortBy string, page int, s SearchSession) (PeoplePage, error) {
	return a.SearchPeopleTabOpts(ctx, PeopleTabOpts{Query: query, Sort: sortBy, Page: page, Session: s})
}

// SearchPeopleTabOpts is the People tab with its filters. The page's
// Titles and Locations are the response's filter_suggestions, which
// Slack keeps as searchSharedFacets (ujfX hydrateFacetSuggestions: the
// locations by facet_count, most first) for the Title and Location pills.
func (a *App) SearchPeopleTabOpts(ctx context.Context, o PeopleTabOpts) (PeoplePage, error) {
	page := o.Page
	p := peopleTabParams(o, a.PrefBool("search_hide_deactivated_users", false))
	var raw json.RawMessage
	if err := a.C.Call(ctx, "search.modules.people", p, &raw); err != nil {
		return PeoplePage{}, err
	}
	var res struct {
		Items       []slack.User     `json:"items"`
		Pagination  searchPagination `json:"pagination"`
		Suggestions struct {
			Title    []facetRaw `json:"title"`
			Location []facetRaw `json:"location"`
		} `json:"filter_suggestions"`
	}
	if err := json.Unmarshal(stripHighlightJSON(raw), &res); err != nil {
		return PeoplePage{}, err
	}
	a.S.PutUsers(res.Items)
	out := PeoplePage{Users: make([]User, 0, len(res.Items)), Total: res.Pagination.Total, Page: res.Pagination.Page,
		Pages: res.Pagination.PageCount, Titles: facets(res.Suggestions.Title), Locations: facets(res.Suggestions.Location)}
	sort.SliceStable(out.Locations, func(i, j int) bool { return out.Locations[i].Count > out.Locations[j].Count })
	if out.Page == 0 {
		out.Page = max(page, 1)
	}
	ids := make([]string, 0, len(res.Items))
	for i := range res.Items {
		out.Users = append(out.Users, a.userOf(&res.Items[i]))
		ids = append(ids, res.Items[i].ID)
	}
	a.SubscribePresence(ids)
	return out, nil
}

// SearchCounts are the source menu's per-tab totals.
type SearchCounts map[string]int

// ModuleCount runs one inactive tab's search for its total (Slack fires
// them alongside the active tab: files and canvases with count 0, dms 40,
// channels and people 20).
func (a *App) ModuleCount(ctx context.Context, module, query string, s SearchSession) (int, error) {
	switch module {
	case ModMessages, ModDMs:
		r, err := a.SearchMessages(ctx, SearchOpts{Query: query, Module: module, Session: s})
		return r.Total, err
	case ModFiles, ModCanvases:
		r, err := a.SearchFiles(ctx, FileSearchOpts{Query: query, Canvases: module == ModCanvases, CountOnly: true, Session: s})
		return r.Total, err
	case ModChannels:
		r, err := a.SearchChannelsTab(ctx, query, 1, s)
		return r.Total, err
	case ModPeople:
		r, err := a.SearchPeopleTab(ctx, query, 1, s)
		return r.Total, err
	}
	return 0, nil
}

// InlineOpts is a search.inline call (the popover's Recent messages,
// Slack's searchInline thunk as suggestionsQuickMessages calls it).
type InlineOpts struct {
	Query string
	Count int // 3
	// Channel / User limit the search to a conversation (an in: filter,
	// or the open channel with omni_quick_message_results off).
	Channel, User string
	// RecentChannels is recent_channels (nil: not sent).
	RecentChannels []string
	FromMe, WithMe bool
	Session        SearchSession
	Now            time.Time
}

func inlineParams(o InlineOpts) url.Values {
	if o.Count <= 0 {
		o.Count = 3
	}
	if o.Session.ID == "" {
		o.Session = NewSearchSession()
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	p := url.Values{"search_session_id": {o.Session.ID}, "client_req_id": {o.Session.ReqID},
		"max_ts": {strconv.FormatInt(now.Unix(), 10)}, "count": {strconv.Itoa(o.Count)}, "page": {"1"},
		"query": {o.Query}, "thread_replies": {""}, "extract_len": {"110"},
		"from_me": {boolStr(o.FromMe)}, "with_me": {boolStr(o.WithMe)}}
	if o.Channel != "" {
		p.Set("channel", o.Channel)
	}
	if o.User != "" {
		p.Set("user", o.User)
	}
	if o.RecentChannels != nil {
		p.Set("recent_channels", strings.Join(o.RecentChannels, ","))
	}
	return p
}

// SearchInline is the popover's "Recent messages" (search.inline): each
// hit's text is its extract, with "…" where Slack cut it.
func (a *App) SearchInline(ctx context.Context, o InlineOpts) (SearchResult, error) {
	var raw json.RawMessage
	if err := a.C.Call(ctx, "search.inline", inlineParams(o), &raw); err != nil {
		return SearchResult{}, err
	}
	var res struct {
		Pagination searchPagination  `json:"pagination"`
		Items      []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(stripHighlightJSON(raw), &res); err != nil {
		return SearchResult{}, err
	}
	out := SearchResult{Total: res.Pagination.Total, Page: 1, Pages: res.Pagination.PageCount}
	var users []string
	chans := map[string]bool{}
	for _, ir := range res.Items {
		var it struct {
			TS        string `json:"ts"`
			User      string `json:"user"`
			Channel   string `json:"channel_id"`
			Permalink string `json:"permalink"`
			Extracts  []struct {
				Text      string `json:"text"`
				TruncHead bool   `json:"truncated_head"`
				TruncTail bool   `json:"truncated_tail"`
			} `json:"extracts"`
		}
		if json.Unmarshal(ir, &it) != nil || it.TS == "" || it.Channel == "" {
			continue
		}
		var m slack.Message // bot_id / bot_profile / username when Slack sends them
		_ = json.Unmarshal(ir, &m)
		text := ""
		if len(it.Extracts) > 0 {
			e := it.Extracts[0]
			text = e.Text
			if e.TruncHead {
				text = "…" + text
			}
			if e.TruncTail {
				text += "…"
			}
		}
		m.TS, m.User, m.Text, m.Blocks = it.TS, it.User, text, nil
		v := a.message(it.Channel, &m)
		v.Permalink = it.Permalink
		out.Messages = append(out.Messages, v)
		users = append(users, it.User)
		chans[it.Channel] = true
	}
	a.EnsureUsers(ctx, users)
	out.Convs = a.convRefs(chans)
	return out, nil
}

// AutocompleteFiles is the popover's file and canvas suggestions
// (search.autocomplete.files).
func (a *App) AutocompleteFiles(ctx context.Context, query string) ([]File, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	var raw json.RawMessage
	p := url.Values{"query": {query}, "include_shares": {"true"}}
	if err := a.C.Call(ctx, "search.autocomplete.files", p, &raw); err != nil {
		return nil, err
	}
	var res struct {
		Files []browseFileRaw `json:"files"`
	}
	if err := json.Unmarshal(stripHighlightJSON(raw), &res); err != nil {
		return nil, err
	}
	out := make([]File, 0, len(res.Files))
	for i := range res.Files {
		out = append(out, res.Files[i].file())
	}
	return out, nil
}

// SaveSearch remembers a submitted query the way Slack does (searchSave:
// search.save {terms, type}; the switcher's type is its searchy mode,
// "default"). The search history cache is dropped after it, as Slack's
// QueryHistoryCache.flushAll does.
func (a *App) SaveSearch(ctx context.Context, terms string) error {
	terms = strings.TrimSpace(terms)
	if terms == "" {
		return nil
	}
	defer a.flushQueryHistory()
	return a.C.Call(ctx, "search.save", url.Values{"terms": {terms}, "type": {"default"}}, nil)
}

// OmniSearchUsers is the popover's people lookup: edge users/search with
// the omniswitcher's args (count 30, search_email, top_users = the
// viewer's frecent people).
func (a *App) OmniSearchUsers(ctx context.Context, query string, top []string) ([]User, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	us, err := a.C.SearchUsersWith(ctx, query, slack.UserSearchOpts{Count: 30, SearchEmail: true, TopUsers: top,
		DefaultWorkspace: a.C.Creds.Team})
	if err != nil {
		return nil, err
	}
	a.S.PutUsers(us)
	out := make([]User, 0, len(us))
	ids := make([]string, 0, len(us))
	for i := range us {
		if us[i].Deleted {
			continue
		}
		out = append(out, a.userOf(&us[i]))
		ids = append(ids, us[i].ID)
	}
	a.SubscribePresence(ids)
	return out, nil
}

// OmniSearchChannels is the popover's channel lookup: edge
// channels/search with count 30, filter xws and top_channels.
func (a *App) OmniSearchChannels(ctx context.Context, query string, top []string) ([]ChannelHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	hs, err := a.C.SearchChannelsWith(ctx, query, slack.ChannelSearchOpts{Count: 30, Filter: "xws", TopChannels: top,
		DefaultWorkspace: a.C.Creds.Team})
	if err != nil {
		return nil, err
	}
	return a.channelHits(hs), nil
}

// ---- search feedback ----
//
// Slack's three "Give feedback" links (CDN build 407aff01646b, 2026-11-10):
//
//   - the search popover's footer (Footer, boot-shared) and the results
//     page (CombinedViewFeedbackLink, search-view; also
//     GenericSearchEmptyState's "Not the results you expected?") open
//     CombinedSearchFeedbackModal (q9NO). Its Submit is
//     combinedSearchFeedbackSubmit, which calls
//     feedback.submitEnterpriseSearchFeedback through a generated fetcher
//     (camelCase args snake-cased): type "feedback", feedback_type
//     "omniswitcher" or "search", text, contact, category, query, the
//     JSON payload and is_test.
//   - search in channel's footer dispatches searchFeedbackDialog: the
//     searchFeedbackApi fetcher calls search.feedback with a client_token
//     (generateAndEnqueue), type "search" and the JSON payload; Slack opens
//     its dialog for that token over the socket.

// SearchFeedbackClientVersion is the payload's client_version: Slack's
// getVersion is the web build's data-version-ts, or "noversion" when the
// page has none. Clack isn't a web build, so it sends Slack's fallback.
const SearchFeedbackClientVersion = "noversion"

// SearchFeedback is one CombinedSearchFeedbackModal submission.
type SearchFeedback struct {
	FeedbackType string // "omniswitcher" (the popover) or "search" (the results page)
	Category     string // the select's value (helpful, missing_results, …)
	Text         string
	Contact      bool // "I’m okay with being contacted by Slack about my feedback"
	Query        string
	Payload      any // JSON-encoded into payload
}

// SubmitSearchFeedback sends a CombinedSearchFeedbackModal submission
// (feedback.submitEnterpriseSearchFeedback). is_test is Slack's internal
// test flag (only Slack employees see that checkbox), so it is false.
func (a *App) SubmitSearchFeedback(ctx context.Context, f SearchFeedback) error {
	payload, err := json.Marshal(f.Payload)
	if err != nil {
		return err
	}
	p := url.Values{"type": {"feedback"}, "feedback_type": {f.FeedbackType}, "text": {f.Text},
		"contact": {boolStr(f.Contact)}, "category": {f.Category}, "query": {f.Query},
		"payload": {string(payload)}, "is_test": {"false"}}
	return a.C.Call(ctx, "feedback.submitEnterpriseSearchFeedback", p, nil)
}

// SearchFeedbackDialog is searchFeedbackDialog (search in channel's "Give
// feedback"): search.feedback {client_token, type "search", payload}.
// Slack answers by opening its feedback dialog for the client_token on the
// socket, which Interactive shows like any app dialog.
func (a *App) SearchFeedbackDialog(ctx context.Context, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	p := url.Values{"client_token": {a.Interactive().ClientToken()}, "type": {"search"}, "payload": {string(b)}}
	return a.C.Call(ctx, "search.feedback", p, nil)
}

// SearchInThread is Slack's searchInThread thunk (find in thread,
// ThreadSearchBar): search.inline with find_in_thread for query in the
// thread at ch/threadTS, 100 matches a page through next_cursor (at most
// 500 pages), and the matches' timestamps newest first. The first page's
// error is returned; a later page's ends the list with what came before
// (search_in_thread_pagination_failed).
func (a *App) SearchInThread(ctx context.Context, ch, threadTS, query string) ([]string, error) {
	var out []string
	cursor := ""
	for page := 0; page < 500 && ctx.Err() == nil; page++ {
		p := url.Values{"query": {query}, "channel": {ch}, "thread_ts": {threadTS}, "find_in_thread": {"true"}, "count": {"100"}}
		if cursor != "" {
			p.Set("cursor", cursor)
		}
		var res struct {
			Items []struct {
				TS string `json:"ts"`
			} `json:"items"`
			NextCursor string `json:"next_cursor"`
		}
		if err := a.C.Call(ctx, "search.inline", p, &res); err != nil {
			if page == 0 || ctx.Err() != nil {
				return nil, err
			}
			break
		}
		for _, it := range res.Items {
			if it.TS != "" {
				out = append(out, it.TS)
			}
		}
		if cursor = res.NextCursor; cursor == "" {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return tsFloat(out[i]) > tsFloat(out[j]) })
	return out, nil
}
