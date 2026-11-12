package popups

import (
	"encoding/json"
	"strconv"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// The search page's results in Slack's store, for plugins. Slack keeps a
// search's API response in state.searchResults[windowId].response
// (windowId "main", pl6F N2), each module's results under
// results[module] = {items: {[page]: rows}, pagination} (yLIV
// getResponseModule, getModuleResultsForCurrentPage), and the result rows
// (MessageListItem's result, each {messages: [msg]}) are read from there.
// So the view hands its page to the Redux mirror (Taut.SetSearchResults)
// and each card takes its result back from the slice as plugins present
// it: a patch over searchResults (Taut Censorship's patchMessageText,
// whose path is response.results.messages.items[*][*].messages[*])
// reaches the cards like it reaches Slack's. Without a plugin runtime
// nothing is marshalled; with one but no patch, the cards draw their own
// copy.

// srStoreWindow is Slack's main window id (pl6F N2).
const srStoreWindow = "main"

// srStoreRows caches the patched slice's result rows for the page.
type srStoreRows struct {
	raw  *byte // the View bytes last decoded (the cache hands back the same slice until it changes)
	n    int
	rows []slackc.Props // by card index
}

// srResultRow is result row m in Slack's shape (a message search result:
// {type: "message", messages: [msg]}).
func srResultRow(m slackc.Props) slackc.Props {
	return slackc.Props{"type": "message", "messages": []any{m}}
}

// mirror hands the current page's message results to plugins.
func (v *SearchView) mirror() {
	t := tautRT.Load()
	if t == nil || !t.Started() || v.tab != srTabMessages && v.tab != srTabDMs {
		return
	}
	rows := make([]any, len(v.msgs))
	for i := range v.msgs {
		rows[i] = srResultRow(slackMessage(&v.msgs[i]))
	}
	page := max(v.page, 1)
	mod := map[string]any{
		"items":      map[string]any{strconv.Itoa(page): rows},
		"pagination": map[string]any{"total_count": v.total, "page": page, "page_count": v.pages},
	}
	raw, err := json.Marshal(map[string]any{"results": map[string]any{srTabModules[v.tab]: mod}})
	if err != nil {
		return
	}
	t.SetSearchResults(srStoreWindow, raw)
}

// storeResult is card i's result row as plugins present the store (nil
// unpatched, or until the patched copy of this page has been computed).
func (v *SearchView) storeResult(i int) slackc.Props {
	t := tautRT.Load()
	if t == nil || !t.SlicePatched("searchResults") || i >= len(v.msgs) {
		return nil
	}
	raw, ok := t.View("searchResults", srStoreWindow)
	if !ok || len(raw) == 0 {
		return nil
	}
	st := &v.store
	if &raw[0] != st.raw || len(raw) != st.n {
		st.raw, st.n, st.rows = &raw[0], len(raw), nil
		var e struct {
			Response struct {
				Results map[string]struct {
					Items map[string][]json.RawMessage `json:"items"`
				} `json:"results"`
			} `json:"response"`
		}
		if json.Unmarshal(raw, &e) == nil {
			for _, r := range e.Response.Results[srTabModules[v.tab]].Items[strconv.Itoa(max(v.page, 1))] {
				p, _ := slackc.DecodeProps(r)
				st.rows = append(st.rows, p)
			}
		}
	}
	if i >= len(st.rows) || st.rows[i] == nil {
		return nil
	}
	// a stale copy (the last page's, a search ago) doesn't belong to this card
	r := st.rows[i]
	if l := r.List("messages"); len(l) == 0 || asProps(l[0]).StrOr("ts", "") != v.msgs[i].TS {
		return nil
	}
	return r
}
