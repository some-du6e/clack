package popups

// The search popover's Slack boundaries (docs/PLUGINS-components.md §9):
// BaseSearch (the top-nav search container; ChatTabs puts its new-tab
// button after it), SearchInputBox (the input; BetterBlock re-maps its
// selection and gates Enter) and SearchAutocomplete (the suggestion list;
// BetterBlock drops blocked people, DMs and group DMs from its
// suggestions). Rows the plugin drops are hidden natively; the native's
// selection, keys and clicks keep working on the rows left.

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// srSugState tracks the suggestion lists sent to JS: gen bumps when the
// row keys change, and a list a plugin hands back is compared with the one
// of its gen, so rows that arrived since are never taken as removed.
type srSugState struct {
	gen  int
	keys []string         // selectable row keys of the current gen, in order
	sent map[int][]string // recent gens' keys
}

const srGenProp = "clackSuggestionsGen"

// suggestions are p's rows (all of them, hidden ones included) as Slack's
// omniswitcher suggestions, with the row key under "_key".
func (p *SearchPopover) suggestions() ([]any, int) {
	out := make([]any, 0, len(p.full))
	keys := make([]string, 0, len(p.full))
	sel := -1
	selKey := ""
	if p.sel >= 0 && p.sel < len(p.rows) {
		selKey = p.rows[p.sel].key()
	}
	for i := range p.full {
		r := &p.full[i]
		if !r.selectable() {
			continue
		}
		k := r.key()
		if k == selKey {
			sel = len(keys)
		}
		keys = append(keys, k)
		out = append(out, p.slickSuggestion(r, k))
	}
	if !sameStrings(keys, p.sug.keys) {
		p.sug.gen++
		p.sug.keys = keys
		if p.sug.sent == nil {
			p.sug.sent = map[int][]string{}
		}
		p.sug.sent[p.sug.gen] = keys
		delete(p.sug.sent, p.sug.gen-4)
	}
	return out, sel
}

func (p *SearchPopover) slickSuggestion(r *srRow, k string) slackc.Props {
	s := slackc.Props{"_key": k, "id": r.id, "title": r.title}
	switch r.kind {
	case srRUser:
		s["type"] = "member"
		s["member"] = slackc.Props{"id": r.id}
		if r.conv != "" {
			s["im"] = slackc.Props{"id": r.conv, "is_im": true, "user": r.id}
		}
	case srRConv:
		ch := slackc.Props{"id": r.id}
		s["type"] = "channel"
		if r.mpim {
			s["type"] = "mpim"
			ch["is_mpim"] = true
			ch["members"] = r.members
		}
		s["channel"] = ch
	case srRChan:
		s["type"] = "channel"
		s["channel"] = slackc.Props{"id": r.id}
	case srRMsg:
		s["type"] = "message"
		if r.msg != nil {
			s["id"] = r.msg.TS
			s["channel"] = slackc.Props{"id": r.msg.Channel}
			s["message"] = slackMessage(r.msg)
		}
	case srRPlace:
		s["type"] = "place"
	case srRShow, srRQuery:
		s["type"] = "query"
	}
	return s
}

// autocompleteProps are SearchAutocomplete's props.
func (p *SearchPopover) autocompleteProps() slackc.Props {
	list, sel := p.suggestions()
	return slackc.Props{
		"$key":        "search-autocomplete",
		srGenProp:     p.sug.gen,
		"suggestions": list,
		"selectedIndex": func() any {
			if sel < 0 {
				return nil
			}
			return sel
		}(),
		"onSuggestionSelected": func(v any) { p.selectSuggestion(v) },
		"onAccept":             func() { p.activate(p.sel) },
	}
}

// inputProps are SearchInputBox's props.
func (p *SearchPopover) inputProps() slackc.Props {
	list, sel := p.suggestions()
	pp := slackc.Props{
		"$key":        "search-input",
		srGenProp:     p.sug.gen,
		"value":       p.query,
		"placeholder": "Search across people, channels, files, workflows, and more",
		"suggestions": list,
		"onEnter":     func() { p.activate(p.sel) },
	}
	if sel >= 0 {
		pp["autocompleteSelectedIndex"] = sel
	}
	return pp
}

// baseSearchProps are BaseSearch's props.
func (p *SearchPopover) baseSearchProps() slackc.Props {
	team := ""
	if snap, ok := actSnapshot(p.h); ok {
		team = snap.Team.Name
	}
	return slackc.Props{
		"$key":                        "base-search",
		"plainTextQuery":              p.query,
		"currentRequestQuery":         p.query,
		"teamName":                    team,
		"isGovSlackWorkspace":         false,
		"showTrialStartedCelebration": false,
		"clearSearchInput":            func() { p.clearReq = true },
		"openFromClick":               func() {},
		"openAdvancedSearchModal":     func() { p.advReq = true },
	}
}

// layoutInput draws the input row through BaseSearch > SearchInputBox.
func (p *SearchPopover) layoutInput(gtx layout.Context, th *theme.Theme, cl *Colors, w, inputH int) {
	gtx.Constraints = layout.Exact(image.Pt(w, inputH))
	slackc.RenderLazy(gtx, "BaseSearch", p.baseSearchProps, func(gtx layout.Context, _ slackc.Props) layout.Dimensions {
		return slackc.RenderLazy(gtx, "SearchInputBox", p.inputProps, func(gtx layout.Context, pp slackc.Props) layout.Dimensions {
			p.inputPP = pp
			p.inputRow(gtx, th, cl, w)
			return layout.Dimensions{Size: image.Pt(w, inputH)}
		})
	})
}

// enter is Enter in the input: through a plugin's onEnter when one wraps
// it (it calls back into the native when it allows the accept).
func (p *SearchPopover) enter() {
	if f, ok := p.inputPP["onEnter"].(slackc.JSFunc); ok {
		f.Call()
		return
	}
	p.activate(p.sel)
}

// selectSuggestion is onSuggestionSelected({selectedIndex}) in the current
// suggestion list's coordinates.
func (p *SearchPopover) selectSuggestion(v any) {
	i := -1
	if m := asProps(v); m != nil {
		i = m.IntOr("selectedIndex", -1)
	}
	if i < 0 || i >= len(p.sug.keys) {
		return
	}
	k := p.sug.keys[i]
	for j := range p.rows {
		if p.rows[j].key() == k {
			p.setSel(j)
			p.ensure = true
			p.h.Invalidate()
			return
		}
	}
}

// applySuggestions takes the suggestions a plugin passed to
// SearchAutocomplete: rows it dropped are hidden.
func (p *SearchPopover) applySuggestions(pp slackc.Props) {
	var hide map[string]bool
	if pp != nil && pp.Has("suggestions") {
		sent, ok := p.sug.sent[pp.IntOr(srGenProp, -1)]
		if !ok {
			return // an answer to a list we no longer track: keep the current view
		}
		kept := make(map[string]bool, len(sent))
		for _, v := range pp.List("suggestions") {
			if s := asProps(v); s != nil {
				kept[s.StrOr("_key", "")] = true
			}
		}
		for _, k := range sent {
			if !kept[k] {
				if hide == nil {
					hide = map[string]bool{}
				}
				hide[k] = true
			}
		}
	}
	if sameSet(hide, p.hide) {
		return
	}
	p.hide = hide
	key := p.lastKey
	p.rows = p.visibleRows(p.full) // never longer than full: clicks fit
	sel := -1
	for i := range p.rows {
		if key != "" && p.rows[i].key() == key {
			sel = i
			break
		}
	}
	if sel < 0 {
		for i := range p.rows {
			if p.rows[i].selectable() {
				sel = i
				break
			}
		}
	}
	p.setSel(sel)
	p.h.Invalidate()
}

// visibleRows is rows minus the hidden ones, and minus the section headers
// and dividers left with nothing under them. rows itself when none are
// hidden.
func (p *SearchPopover) visibleRows(rows []srRow) []srRow {
	if len(p.hide) == 0 {
		return rows
	}
	out := p.vis[:0]
	for i := range rows {
		r := &rows[i]
		if r.selectable() && p.hide[r.key()] {
			continue
		}
		out = append(out, *r)
	}
	// drop headers with no rows before the next header/divider, then
	// leading, trailing and doubled dividers
	clean := out[:0]
	for i := range out {
		if out[i].kind == srRHeader {
			has := false
			for j := i + 1; j < len(out) && out[j].kind != srRHeader && out[j].kind != srRDivider; j++ {
				has = true
				break
			}
			if !has {
				continue
			}
		}
		if out[i].kind == srRDivider && (len(clean) == 0 || clean[len(clean)-1].kind == srRDivider) {
			continue
		}
		clean = append(clean, out[i])
	}
	for len(clean) > 0 && clean[len(clean)-1].kind == srRDivider {
		clean = clean[:len(clean)-1]
	}
	p.vis = clean
	return clean
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
