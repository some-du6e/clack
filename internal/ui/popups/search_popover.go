package popups

// Search popover / quick switcher (Ctrl+K and the top-nav search pill open
// the same component in the 2026 client). Measured from app.slack.com
// (2026-11): 649 wide (pill x-8, pill width+16) at y=4 so it covers the
// pill, radius 8, popover shadow. Input row 48 tall + 1px #dddddd border:
// search icon 20 at x+20, text at x+56 15px, placeholder "Search across
// people, channels, files, workflows, and more"; right side "Clear" 13px,
// a 1px divider, filter icon (24 box) at right-68 and history icon at
// right-36. List margin-top 8; section headers 13px #5e5d60 at x+16;
// rows 40 tall, selected bg #eaeaea, 24px icon/avatar at x+16, text at
// x+52 (primary 15px with the match bold, "· secondary" 13px #5e5d60),
// unread badge pill #7c3085 and an "Enter" chip on the selected row.
// Dividers 1px rgba(29,28,29,.13) inset 16. Message cards: 1px border,
// radius 8, avatar 36, name 15/900, "# channel" 13px, time right, text 15px
// with rgba(255,198,0,.18) term highlights. Footer 45 tall: ▲▼ light key
// chips + "Select", right "Give feedback" link; offline, the "No
// connection." notice in their place (srOfflineNotice).

import (
	"context"
	"image"
	"image/color"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// srRowKind is a quick-switcher list row type.
type srRowKind uint8

const (
	srRHeader  srRowKind = iota // section heading
	srRDivider                  // section divider
	srRConv                     // sidebar conversation
	srRUser                     // person (DM)
	srRChan                     // directory channel
	srRPlace                    // shortcut route (Saved, Threads, Activity)
	srRShow                     // "Show results for: q"
	srRQuery                    // recent search
	srRMsg                      // message card
	srRFile                     // file or canvas
	srRMore                     // "Show N more" / "Show fewer" (unreads)
	srRNote                     // a disabled line ("Your recent searches will show up here.")
)

// srRow is one row of the popover list.
type srRow struct {
	kind     srRowKind
	id       string // conv / user / channel id, route, or query
	conv     string // srRUser: DM id when one exists
	title    string
	alt      string // "· secondary"
	icon     string
	avatar   string
	b0, b1   int // bold byte range in title
	boldAll  bool
	badge    int
	presence string
	boxed    bool // icon in a bordered tile
	isAvatar bool
	mpim     bool     // group DM: draws the member tile
	members  []string // group DM members (tile)

	// message cards
	msg   *core.Message
	where srWhere
	name  string
	when  string
	segs  []srSeg
	toks  []srTok
	tokW  int
	// plain is the quick message row omni_quick_message_results off draws
	// (the text first, then "name • date"); else the card.
	plain bool
}

func (r *srRow) selectable() bool {
	return r.kind != srRHeader && r.kind != srRDivider && r.kind != srRNote
}

func (r *srRow) key() string {
	if !r.selectable() {
		return ""
	}
	return string(rune('a'+r.kind)) + ":" + r.id
}

// heights in dp
func (r *srRow) heightDp() float32 {
	switch r.kind {
	case srRHeader:
		return 36
	case srRDivider:
		return 17
	case srRMsg:
		if r.plain {
			return 54 // --multiline: padding 4, two 1.5 lines
		}
		return 76
	case srRMore:
		return 32
	}
	return 40
}

// SearchPopover is the unified search popover / quick switcher overlay.
type SearchPopover struct {
	base
	h       Host
	pill    image.Rectangle
	ed      widget.Editor
	scrim   scrim
	block   blocker
	list    layout.List
	focus   bool
	refocus bool
	rect    image.Rectangle

	clear, filter, history, feedback widget.Clickable
	historyRect                      image.Rectangle

	rows    []srRow
	clicks  []widget.Clickable
	sel     int  // the row Enter opens: the picked row, else the first one
	picked  bool // a row is selected (Slack's selectedIndex isn't null)
	hover   int
	ensure  bool
	query   string
	lastKey string
	listHov gesture.Hover // the list (onMouseLeave resets the selection)
	inList  bool

	// local data (snapshot taken once when core is ready)
	snapOK  bool
	cands   []srCand
	byConv  map[string]int // conv id -> cands index
	dmOf    map[string]string
	unreads []string // conv ids, mentions first
	counts  map[string]core.Count

	// async (stale results are dropped by sequence number)
	seq                int
	changed            time.Time
	dirFired, msgFired bool
	rUsers             []core.User
	rChans             []core.ChannelHit
	rUQ, rCQ           string // queries the remote users / channels answered
	msgs               []core.Message
	msgConvs           map[string]core.ConvRef
	msgQ               string
	files              []core.File // search.autocomplete.files
	fileQ              string
	fileByID           map[string]core.File

	histMode    bool // the clock button: the list shows Recent searches
	unreadsOpen bool // "Show N more" expanded the unreads
	sess        core.SearchSession
	recVer      uint64 // core.RecentsVersion the rows were built with
	recRead     string // desktop_search_recents_read's group the rows were built with
	histLoading bool   // Recent searches are being fetched (search.autocomplete)

	// Slick boundaries (slick_search.go)
	full     []srRow         // every row; rows are full minus plugin-hidden ones
	vis      []srRow         // backing array for filtered rows
	hide     map[string]bool // row keys a plugin removed from the suggestions
	sug      srSugState
	inputPP  slackc.Props // SearchInputBox props a plugin passed (onEnter)
	clearReq bool         // BaseSearch.clearSearchInput
	advReq   bool         // BaseSearch.openAdvancedSearchModal

	// the box's completers and tokens (search_filter_ac.go,
	// search_filter_menu.go)
	ac      srACState
	tokens  []srToken
	edX     int  // the editor's x in the card
	acArmed bool // the user edited the box: Texty's completers follow its text

	// the quick filter row (search_quick_filters.go), the date pickers
	// (search_date_picker.go), the tokens' pills (search_token_pills.go)
	// and the picked row's toolbar (search_toolbar.go)
	qf      srQFState
	dpk     srDatePick
	pills   srPillState
	tb      srTBState
	listOrg image.Point // the list's window origin, last layout

	// setText: while uqOn the box shows the text of the suggestion ↑ / ↓
	// picked, and uq is the user's query the suggestions are for (with
	// uqArmed, its acArmed). See srSetText.
	uq      srBox
	uqOn    bool
	uqArmed bool
}

// NewQuickSwitcher opens the Ctrl+K switcher: empty query, centered at the
// top of the window; Enter on the top suggestion jumps there.
func NewQuickSwitcher(h Host) *SearchPopover {
	return NewSearchPopover(h, "", image.Rectangle{})
}

// NewSearchPopover opens the popover over the top-nav search pill (pill is
// the pill's rect in window coords; empty = centered at the top).
func NewSearchPopover(h Host, query string, pill image.Rectangle) *SearchPopover {
	p := &SearchPopover{h: h, pill: pill, sel: -1, hover: -1, sess: core.NewSearchSession()}
	p.ed.SingleLine = true
	p.ed.Submit = true
	p.list.Axis = layout.Vertical
	if query != "" {
		// the display query: refs as their tokens
		text, toks := srDisplayQuery(h, query)
		p.ed.SetText(text)
		n := len([]rune(text))
		p.ed.SetCaret(n, n)
		p.query, p.tokens = text, toks
	}
	p.changed = time.Now()
	p.rebuild(time.Now(), true)
	return p
}

// ---- data ----

func (p *SearchPopover) loadSnap(now time.Time) {
	if p.snapOK {
		return
	}
	c := p.h.Core()
	if c == nil {
		return
	}
	select {
	case <-c.Ready():
	default:
		return
	}
	p.snapOK = true
	snap := c.Snapshot()
	recents, _ := srRecents()
	rec := make(map[string]bool, len(recents))
	for _, id := range recents {
		rec[id] = true
	}
	p.counts = snap.Counts
	p.byConv = make(map[string]int, len(snap.Convs))
	p.dmOf = map[string]string{}
	p.cands = p.cands[:0]
	inIM := map[string]bool{}
	user := func(id string) (core.User, bool) {
		if u, ok := snap.Users[id]; ok {
			return u, true
		}
		return lookupUser(p.h, id)
	}
	for _, cv := range snap.Convs {
		if cv.IsArchived || cv.File { // a canvas's or list's conversation isn't a channel to switch to (I3y6 isValidViewableChannel)
			continue
		}
		cnt := snap.Counts[cv.ID]
		boost := 0
		switch {
		case cnt.Mentions > 0:
			boost += 4
		case cnt.Unread && !cnt.Muted:
			boost += 2
		}
		if rec[cv.ID] {
			boost += 3
		}
		if cv.Muted {
			boost--
		}
		cd := srCand{Kind: srKConv, ID: cv.ID, Name: cv.Name, ConvK: cv.Kind, Boost: boost}
		switch cv.Kind {
		case "im":
			u, ok := user(cv.User)
			if !ok || u.Deleted {
				continue
			}
			cd = srCand{Kind: srKUser, ID: cv.User, Conv: cv.ID, Name: userName(u), Alt: u.RealName, Handle: u.Handle,
				ConvK: "im", Avatar: u.Avatar, Boost: boost}
			inIM[cv.User] = true
			p.dmOf[cv.User] = cv.ID
		case "mpim":
			cd.Name = convTitle(p.h, cv)
			named := false
			if a := p.h.Core(); a != nil {
				_, named = a.MpdmCustomName(cv.ID)
			}
			if cd.Name == cv.Name && !named {
				// members unknown to the user cache: use the handles in
				// the internal name ("mpdm-a--b-1")
				if hs := srMpdmNames(cv.Name, snap.Self.Handle); len(hs) > 0 {
					cd.Name = strings.Join(hs, ", ")
				}
			}
			cd.Members = cv.Members
			// members' real names and handles also find the group DM; a
			// named one (filterMpims: its custom name first, then its
			// members) by their display names too
			for _, m := range cv.Members {
				if m == snap.Self.ID {
					continue
				}
				if u, ok := user(m); ok {
					if named && userName(u) != "" {
						cd.Keys = append(cd.Keys, userName(u))
					}
					if u.RealName != "" && u.RealName != userName(u) {
						cd.Keys = append(cd.Keys, u.RealName)
					}
					if u.Handle != "" {
						cd.Keys = append(cd.Keys, u.Handle)
					}
				}
			}
		}
		if cd.Name == "" {
			continue
		}
		p.byConv[cv.ID] = len(p.cands)
		p.cands = append(p.cands, cd)
	}
	for id, u := range snap.Users {
		if inIM[id] || u.Deleted {
			continue
		}
		p.cands = append(p.cands, srCand{Kind: srKUser, ID: id, Name: userName(u), Alt: u.RealName, Handle: u.Handle, Avatar: u.Avatar})
	}
	// unreads: mentions first, then newest activity.
	p.unreads = p.unreads[:0]
	for id, cnt := range snap.Counts {
		if _, ok := p.byConv[id]; !ok {
			continue
		}
		if cnt.Mentions > 0 || (cnt.Unread && !cnt.Muted) {
			p.unreads = append(p.unreads, id)
		}
	}
	sort.Slice(p.unreads, func(i, j int) bool {
		a, b := snap.Counts[p.unreads[i]], snap.Counts[p.unreads[j]]
		if (a.Mentions > 0) != (b.Mentions > 0) {
			return a.Mentions > 0
		}
		if a.Latest != b.Latest {
			return a.Latest > b.Latest
		}
		return p.unreads[i] < p.unreads[j]
	})
	p.rebuild(now, true)
}

// candRow turns a candidate into a list row.
func (p *SearchPopover) candRow(c srCand, typing bool) srRow {
	r := srRow{id: c.ID, title: c.Name, b0: c.Bold0, b1: c.Bold1}
	switch c.Kind {
	case srKUser:
		r.kind, r.conv, r.avatar, r.isAvatar = srRUser, c.Conv, c.Avatar, true
		r.alt = c.Alt
		if r.alt == "" {
			r.alt = c.Handle
		}
		r.boldAll = typing
		r.presence = "away" // Slack draws the hollow ring until presence is known
		if u, ok := lookupUser(p.h, c.ID); ok {
			if u.Presence != "" {
				r.presence = u.Presence
			}
			if u.IsBot {
				r.presence = ""
			}
			if r.avatar == "" {
				r.avatar = u.Avatar
			}
		}
	case srKChan:
		r.kind, r.icon = srRChan, convIcon(c.ConvK)
		r.alt = c.Note
	case srKFile:
		r.kind, r.icon, r.boxed = srRFile, "files", true
		if c.ConvK == "canvas" {
			r.icon = "canvas"
		}
	default:
		r.kind = srRConv
		if c.ConvK == "mpim" {
			r.mpim, r.members = true, c.Members
		} else {
			r.icon = convIcon(c.ConvK)
		}
	}
	if !typing {
		r.b0, r.b1 = 0, 0
	}
	conv := c.ID
	if c.Kind == srKUser {
		conv = c.Conv
	}
	if cnt, ok := p.counts[conv]; ok && conv != "" {
		r.badge = cnt.Mentions
	}
	return r
}

// rebuild recomputes the rows for the current query and results.
func (p *SearchPopover) rebuild(now time.Time, resetSel bool) {
	q := strings.TrimSpace(p.userQuery())
	rows := p.full[:0]
	if q == "" {
		rows = p.emptyRows(rows)
	} else {
		rows = p.typingRows(rows, q, now)
	}
	p.full = rows
	p.rows = p.visibleRows(rows)
	for len(p.clicks) < len(p.rows) {
		p.clicks = append(p.clicks, widget.Clickable{})
	}
	// a picked row stays picked while it's still listed; otherwise the
	// first row is the pseudo-selected one, whatever arrived above it
	sel := -1
	if !resetSel && p.picked && p.lastKey != "" {
		for i := range p.rows {
			if p.rows[i].key() == p.lastKey {
				sel = i
				break
			}
		}
	}
	if sel < 0 {
		sel = p.firstRow()
		p.picked = false
	}
	if resetSel {
		p.list.Position = layout.Position{}
	}
	p.setSel(sel)
}

// firstRow is the first selectable row (Slack's pseudo-selected
// suggestion 0), -1 when there is none.
func (p *SearchPopover) firstRow() int {
	for i := range p.rows {
		if p.rows[i].selectable() {
			return i
		}
	}
	return -1
}

// lastRow is the last selectable row.
func (p *SearchPopover) lastRow() int {
	for i := len(p.rows) - 1; i >= 0; i-- {
		if p.rows[i].selectable() {
			return i
		}
	}
	return -1
}

// pick selects row i (a keyboard or mouse selection).
func (p *SearchPopover) pick(i int) {
	p.setSel(i)
	p.picked = true
}

// unpick is selectedIndex null: nothing selected, Enter opens the first
// row (pseudo-selected).
func (p *SearchPopover) unpick() {
	p.picked = false
	p.setSel(p.firstRow())
}

// userQuery is the query the suggestions are for: the box's text, or
// the user's while the box shows a picked suggestion's (setText).
func (p *SearchPopover) userQuery() string {
	if p.uqOn {
		return p.uq.text
	}
	return p.query
}

// srSetText is the omniswitcher's onSuggestionSelected with setText (lj3A
// a$, which ↑ / ↓ call: SearchAutocomplete's increment/
// decrementSelectedIndex; the pointer's onMouseMove and the caret keys
// pass setText false and leave the box alone). A picked query completion
// (getQueryReplacementOps: a recent search, client-boot y+lP's
// queryHistory, is one) puts its suggestion_replacement_ops in the box
// as a display query (isQueryUserGenerated false: no new search, the
// suggestions stay the user's); any other row, or none (restoreUserQuery,
// past either end), puts the user's query back, as the searchy mode
// does. "Show results for:" is queryUser (tGcK's query-echo): its
// replacement is the user's query.
func (p *SearchPopover) srSetText() {
	if p.picked && p.sel >= 0 && p.sel < len(p.rows) && p.rows[p.sel].kind == srRQuery {
		if !p.uqOn {
			p.uq, p.uqArmed, p.uqOn = p.box(), p.acArmed, true
		}
		text, toks := srDisplayQuery(p.h, p.rows[p.sel].id)
		p.showBox(srBox{text: text, toks: toks})
		p.acArmed = false // Texty completes what the user types
		return
	}
	p.restoreUserQuery()
}

// restoreUserQuery is restoreUserQuery (lj3A aN): the user's query back
// in the box.
func (p *SearchPopover) restoreUserQuery() {
	if !p.uqOn {
		return
	}
	b, armed := p.uq, p.uqArmed
	p.uq, p.uqOn, p.uqArmed = srBox{}, false, false
	p.showBox(b)
	p.acArmed = armed
}

// showBox puts b in the box, the caret at its end, without a new search.
func (p *SearchPopover) showBox(b srBox) {
	if b.text != p.ed.Text() {
		p.ed.SetText(b.text)
	}
	n := len([]rune(b.text))
	p.ed.SetCaret(n, n)
	p.query, p.tokens = b.text, b.toks
}

func (p *SearchPopover) setSel(i int) {
	p.sel = i
	p.lastKey = ""
	if i >= 0 && i < len(p.rows) {
		p.lastKey = p.rows[i].key()
	}
}

// srUnreadsShown is how many unreads show before "Show N more" (the
// zero state's unreads.slice(0, 10), with "Show {count} more" for the
// rest).
const srUnreadsShown = 10

// emptyRows is Slack's blank slate (the omniswitcher's merged zero state,
// rendered by renderBlankSlateSuggestions): "Unreads & drafts" (ten, then
// Show N more / Show fewer), a divider, then "Recent places": three when
// there are unreads, else five, without the one you're on and without the
// conversations already listed as unreads. The clock button switches the
// list to Recent searches.
func (p *SearchPopover) emptyRows(rows []srRow) []srRow {
	if p.histMode {
		return p.historyRows(rows)
	}
	unreads := p.unreads
	if len(unreads) > 24 {
		unreads = unreads[:24] // suggestionsForBlankSlateUnreads' cap
	}
	if len(unreads) > 0 {
		rows = append(rows, srRow{kind: srRHeader, title: nsSearch.T("Unreads & drafts")})
		for i, id := range unreads {
			if i == srUnreadsShown && !p.unreadsOpen {
				break
			}
			rows = append(rows, p.candRow(p.cands[p.byConv[id]], false))
		}
		if more := len(unreads) - srUnreadsShown; more > 0 {
			t := nsSearch.T("Show {count} more", "count", more)
			if p.unreadsOpen {
				t = nsSearch.T("Show fewer")
			}
			rows = append(rows, srRow{kind: srRMore, id: "unreads-toggle", title: t})
		}
	}
	// excludeIds: the unreads' conversations (and a DM's person)
	seen := map[string]bool{}
	for _, id := range p.unreads {
		seen[id] = true
		if i, ok := p.byConv[id]; ok {
			seen[p.cands[i].ID] = true
		}
	}
	limit := 5
	if len(unreads) > 0 {
		limit = 3
	}
	recent := p.recentPlaces(seen, limit)
	if len(recent) > 0 {
		if len(unreads) > 0 {
			rows = append(rows, srRow{kind: srRDivider})
		}
		rows = append(rows, srRow{kind: srRHeader, title: nsSearch.T("Recent places")})
		rows = append(rows, recent...)
	}
	return rows
}

// historyRows is history mode's Recent searches
// (suggestionsBlankSlateQueryHistory, ten): Slack's server history
// (core.RecentSearches: the recents pref with desktop_search_recents_read
// on, else search.autocomplete), fetched once and redrawn when it lands.
// Slackbot conversations are left out: Clack has no Slackbot to reopen
// them in.
func (p *SearchPopover) historyRows(rows []srRow) []srRow {
	var queries []string
	if c := p.h.Core(); c != nil {
		items, ok := c.CachedRecentSearches(10)
		if !ok && !p.histLoading {
			p.histLoading = true
			run(p.h, func(ctx context.Context) ([]core.QueryHistoryItem, error) { return c.RecentSearches(ctx, 10) },
				func(_ []core.QueryHistoryItem, _ error) {
					p.histLoading = false
					if !p.done && p.histMode {
						p.rebuild(time.Now(), false)
					}
				})
		}
		for _, it := range items {
			if !it.Slackbot && !p.tb.gone[it.Query] {
				queries = append(queries, it.Query)
			}
		}
	}
	if len(queries) == 0 {
		return append(rows, srRow{kind: srRNote, title: nsSearch.T("Your recent searches will show up here.")})
	}
	rows = append(rows, srRow{kind: srRHeader, title: nsSearch.T("Recent searches")})
	for _, q := range queries {
		rows = append(rows, srRow{kind: srRQuery, id: q, title: SearchDisplayText(p.h, q), icon: "clock"})
	}
	return rows
}

// PageInfo, set by the ui root, is a page route's title and icon (the
// rows Recent places shows for pages Slack's visits.list returns).
var PageInfo func(page string) (title, icon string)

// srPageRow is a Recent places page row ("" title: Clack doesn't have it).
func srPageRow(page string) (srRow, bool) {
	if pl, ok := srPlaceOf("view:" + page); ok && pl.title != "" {
		return srRow{kind: srRPlace, id: page, title: nsPage.T(pl.title), icon: pl.icon, boxed: true}, true
	}
	if PageInfo != nil {
		if t, ic := PageInfo(page); t != "" {
			return srRow{kind: srRPlace, id: page, title: nsPage.T(t), icon: ic, boxed: true}, true
		}
	}
	return srRow{}, false
}

// recentPlaces is the zero state's Recent places: with
// desktop_search_recents_read on, navigationEventsToSuggestions over
// Slack's visits (visits.list, else the recents pref, with this session's
// navigation merged in); else historyEntriesToSuggestions over this
// session's history. seen holds the ids already listed (the unreads).
func (p *SearchPopover) recentPlaces(seen map[string]bool, limit int) []srRow {
	c := p.h.Core()
	cur := srCurrentPlace()
	var out []srRow
	conv := func(id string) {
		if i, ok := p.byConv[id]; ok {
			cd := p.cands[i]
			if seen[id] || seen[cd.ID] {
				return
			}
			seen[id], seen[cd.ID] = true, true
			out = append(out, p.candRow(cd, false))
			return
		}
		if c != nil && p.snapOK && !seen[id] {
			ref := c.ConvRef(id)
			if ref.Name != "" && ref.Kind != "im" && ref.Kind != "mpim" {
				seen[id] = true
				out = append(out, srRow{kind: srRConv, id: id, title: ref.Name, icon: convIcon(ref.Kind)})
			}
		}
	}
	if c != nil {
		if evs, ok := c.RecentPlacesEvents(); ok {
			curPage := core.SlackPageIDs[strings.TrimPrefix(cur, "view:")]
			for _, ev := range evs {
				if len(out) >= limit {
					break
				}
				switch ev.ObjectType {
				case core.NavChannel, core.NavDM:
					if ev.ID != cur {
						conv(ev.ID)
					}
				case core.NavPage:
					// canvases and lists (FILE) show only when their file is
					// loaded in Slack; Clack's popover has no file store, and
					// record channels don't exist on Hack Club
					page := core.SlackPageRoute(ev.ID)
					if page == "" || ev.ID == curPage || seen["view:"+page] {
						continue
					}
					if r, ok := srPageRow(page); ok {
						seen["view:"+page] = true
						out = append(out, r)
					}
				}
			}
			return out
		}
	}
	places, _ := srRecents()
	for _, id := range places {
		if len(out) >= limit {
			break
		}
		if id == cur {
			continue
		}
		if page, ok := strings.CutPrefix(id, "view:"); ok {
			if r, ok := srPageRow(page); ok && !seen[id] {
				seen[id] = true
				out = append(out, r)
			}
			continue
		}
		conv(id)
	}
	return out
}

// srExactGroup is search_autocomplete_promote_exact_name_matches' group
// ("" when not assigned).
func (p *SearchPopover) srExactGroup() string {
	if c := p.h.Core(); c != nil {
		return c.Experiment("search_autocomplete_promote_exact_name_matches")
	}
	return ""
}

// srExactFactsFor is the exact name rules' predicates for the query q:
// isUser (a person), isEligibleExactMatch (an active human of the org,
// not a bot, app, Slackbot, deleted or external member, whose real or
// display name normalized is the normalized query) and in context (a
// member of the open conversation).
func (p *SearchPopover) srExactFactsFor(q string) func(srCand) srExactFacts {
	nq := acrank.Normalize(acrank.FormatQuery(q))
	in := map[string]bool{}
	if c := p.h.Core(); c != nil {
		if cv, ok := c.Conv(srCurrentPlace()); ok {
			for _, m := range cv.Members {
				in[m] = true
			}
		}
	}
	// a name can only normalize to nq if its length is near nq's (the
	// pool is every cached person, scanned per keystroke)
	near := func(n string) bool { return len(n) >= len(nq)/2 && len(n) <= 2*len(nq)+4 }
	return func(cd srCand) srExactFacts {
		if cd.Kind != srKUser || cd.ID == "" || (cd.ID[0] != 'U' && cd.ID[0] != 'W') {
			return srExactFacts{}
		}
		f := srExactFacts{user: true}
		if nq == "" || (!near(cd.Name) && !near(cd.Alt) && !near(cd.Handle)) {
			return f
		}
		u, ok := lookupUser(p.h, cd.ID)
		if !ok {
			return f
		}
		human := !u.Deleted && !u.IsBot && !u.AppUser && !u.Workflow && !u.External && u.ID != "USLACKBOT"
		exact := acrank.Normalize(u.RealName) == nq || (u.DisplayName != "" && acrank.Normalize(u.DisplayName) == nq)
		f.eligible = human && exact
		f.inContext = f.eligible && in[cd.ID]
		return f
	}
}

// srQuickOn is omni_quick_message_results "on".
func (p *SearchPopover) srQuickOn() bool {
	c := p.h.Core()
	return c != nil && c.Experiment("omni_quick_message_results") == "on"
}

func (p *SearchPopover) typingRows(rows []srRow, q string, now time.Time) []srRow {
	// reorderSuggestionsForActiveQuery: with quick messages the places are
	// cut to 5 (omni_quick_message_results on) or 8, else to 15
	total := 15
	if len(p.msgs) > 0 {
		total = 8
		if p.srQuickOn() {
			total = 5
		}
	}
	// the Searcher matches formatQuery's text: a leading @, # or / dropped
	rq := q
	if t := acrank.SearcherQuery(q); t != "" {
		rq = t
	}
	all := srRank(p.cands, rq, 0, false)
	local := all
	if len(local) > total {
		local = local[:total]
	}
	var remote []srCand
	if len(p.rUsers) > 0 || len(p.rChans) > 0 {
		rc := make([]srCand, 0, len(p.rUsers))
		for _, u := range p.rUsers {
			if u.Deleted {
				continue
			}
			rc = append(rc, srCand{Kind: srKUser, ID: u.ID, Conv: p.dmOf[u.ID], Name: userName(u), Alt: u.RealName,
				Handle: u.Handle, Avatar: u.Avatar, Remote: true})
		}
		// server hits that don't match locally are kept only when they
		// answer the current query (stale ones must match to stay).
		remote = srRank(rc, rq, 8, p.rUQ == q)
		rc = make([]srCand, 0, len(p.rChans))
		for _, ch := range p.rChans {
			if ch.IsArchived {
				continue
			}
			note := ""
			if !ch.IsMember && ch.NumMembers > 0 {
				note = plural(ch.NumMembers, "member")
			}
			rc = append(rc, srCand{Kind: srKChan, ID: ch.ID, Name: ch.Name, ConvK: ch.Kind, Note: note, Remote: true})
		}
		remote = append(remote, srRank(rc, rq, 8, p.rCQ == q)...)
	}
	if len(p.files) > 0 {
		fc := make([]srCand, 0, len(p.files))
		for _, f := range p.files {
			title := f.Title
			if title == "" {
				title = f.Name
			}
			k := "file"
			if f.IsCanvas {
				k = "canvas"
			}
			fc = append(fc, srCand{Kind: srKFile, ID: f.ID, Name: title, ConvK: k, Remote: true})
		}
		remote = append(remote, srRank(fc, q, 4, p.fileQ == q)...)
	}
	if len(remote) > 0 {
		sort.SliceStable(remote, func(i, j int) bool { return remote[i].Tier > remote[j].Tier })
	}
	sug := srMerge(local, remote, total, 2) // Slack: top hit, "Show results", then the rest
	if g := p.srExactGroup(); g != "" {
		// search_autocomplete_promote_exact_name_matches: exact name
		// matches kept past the cut, then lifted among the people
		facts := p.srExactFactsFor(rq)
		pool := append(append([]srCand(nil), all...), remote...)
		sug = srApplyExactNameRules(srRetainExactNames(sug, pool, g, total, facts), g, facts)
	}
	show := srRow{kind: srRShow, id: q, title: q, icon: "search"}
	if len(sug) == 0 {
		rows = append(rows, show)
	}
	for i, c := range sug {
		rows = append(rows, p.candRow(c, true))
		if i == 0 {
			rows = append(rows, show)
		}
	}
	if len(p.msgs) > 0 {
		rows = append(rows, srRow{kind: srRDivider}, srRow{kind: srRHeader, title: nsSearch.T("Recent messages")})
		terms := srParse(q).Highlights()
		on := p.srQuickOn()
		for i := range p.msgs {
			if i == 3 {
				break
			}
			m := &p.msgs[i]
			name, av := srAuthor(p.h, m)
			r := srRow{kind: srRMsg, id: m.Channel + "/" + m.TS, msg: m, name: name, avatar: av,
				when: srShortDay(tsTime(m.TS), now), segs: srHighlight(srFlatten(srBody(m)), terms), plain: !on}
			if on {
				r.where = p.srQuickWhere(m)
			}
			rows = append(rows, r)
		}
	}
	return rows
}

// srQuickWhere is the quick message card's location (SuggestionQuickMessage
// with omni_quick_message_results on): a DM reads "Direct Message with
// {name}" ("Direct Message with you" in your own; an app's DM message only
// names the app in a thread), else the conversation.
func (p *SearchPopover) srQuickWhere(m *core.Message) srWhere {
	w := srWhereOf(p.h, p.msgConvs, m)
	ref, ok := p.msgConvs[m.Channel]
	if c := p.h.Core(); !ok && c != nil {
		ref = c.ConvRef(m.Channel)
	}
	if ref.Kind != "im" {
		return w
	}
	self := ""
	if c := p.h.Core(); c != nil {
		self = c.Self().ID
	}
	switch {
	case ref.User == self && self != "":
		w.name = nsSearch.T("Direct Message with you")
	case m.BotID != "":
		if w.pre == "" {
			w.name = "" // no thread: just the sender
		} else {
			w.name = " " + w.name
		}
	default:
		w.name = nsSearch.T("Direct Message with {name}", "name", w.name)
	}
	return w
}

// srQuickArgs is suggestionsQuickMessages' search.inline call for the
// typed text, ok=false when Slack shows no quick messages: the text must
// be words only (one STRING token), or words and one in: filter, and the
// words at least 3 characters. With omni_quick_message_results on the
// search covers your recent conversations (from_me, with_me), an in:
// filter its conversation; off, only the channel you're in.
func (p *SearchPopover) srQuickArgs(text string) (core.InlineOpts, bool) {
	toks := srQueryTokens(text)
	o := core.InlineOpts{Count: 3}
	on := p.srQuickOn()
	switch {
	case len(toks) == 2 && (toks[0].kind == srQTWord) != (toks[1].kind == srQTWord) &&
		(toks[0].mod == "in" || toks[1].mod == "in"):
		w, f := toks[0], toks[1]
		if f.kind == srQTWord {
			w, f = f, w
		}
		o.Query = w.text
		if ch := p.srInChannel(f.val); ch != "" {
			if on {
				o.RecentChannels, o.WithMe = []string{}, true // its recent threads (none kept)
			}
			o.Channel = ch
		} else if u := p.srInMember(f.val); u != "" {
			if on {
				o.RecentChannels, o.WithMe = []string{}, true
			}
			o.User = u
		}
	case len(toks) == 1 && toks[0].kind == srQTWord:
		if on {
			o.RecentChannels = srRecentChannels() // recent channels; Clack keeps no thread history
			if o.RecentChannels == nil {
				o.RecentChannels = []string{}
			}
			o.FromMe, o.WithMe = true, true
		} else if cur := srCurrentPlace(); srLooksLikeChannelID(cur) {
			o.Channel = cur
		}
		o.Query = toks[0].text
	default:
		return o, false
	}
	if len([]rune(o.Query)) < 3 || (o.Channel == "" && len(o.RecentChannels) == 0 && o.User == "") {
		return o, false
	}
	return o, true
}

// srLooksLikeChannelID is looksLikeChannelId.
func srLooksLikeChannelID(id string) bool {
	if len(id) < 9 || !strings.ContainsRune("CGDZ", rune(id[0])) {
		return false
	}
	for _, r := range id[1:] {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// srInChannel resolves an in: value to a conversation (InQuerySuggestionToken:
// <#C…|name>, #name or name).
func (p *SearchPopover) srInChannel(v string) string {
	if id := srRefID(v); id != "" && v[1] == '#' {
		return id
	}
	name := strings.TrimPrefix(v, "#")
	for _, cd := range p.cands {
		if cd.Kind == srKConv && (cd.ConvK == "channel" || cd.ConvK == "private") && strings.EqualFold(cd.Name, name) {
			return cd.ID
		}
	}
	return ""
}

// srInMember resolves an in: value to a person (<@U…> or @handle).
func (p *SearchPopover) srInMember(v string) string {
	if id := srRefID(v); id != "" && v[1] == '@' {
		return id
	}
	h, ok := strings.CutPrefix(v, "@")
	if !ok {
		return ""
	}
	for _, cd := range p.cands {
		if cd.Kind == srKUser && (strings.EqualFold(cd.Handle, h) || strings.EqualFold(cd.Name, h)) {
			return cd.ID
		}
	}
	return ""
}

// ---- async ----

func (p *SearchPopover) onQuery(gtx layout.Context) { p.queryChanged(gtx.Now) }

// queryChanged restarts the searches for the box's new text.
func (p *SearchPopover) queryChanged(now time.Time) {
	// the user changed the box: what it shows is their query now
	p.uq, p.uqOn, p.uqArmed = srBox{}, false, false
	p.seq++
	p.changed = now
	p.dirFired, p.msgFired = false, false
	q := strings.TrimSpace(p.query)
	if q == "" {
		p.rUsers, p.rChans, p.rUQ, p.rCQ = nil, nil, "", ""
		p.msgs, p.msgConvs, p.msgQ = nil, nil, ""
	} else if _, ok := p.srQuickArgs(q); !ok {
		p.msgs, p.msgConvs, p.msgQ = nil, nil, "" // eZ = []: none for this text
	} else if p.msgQ != "" && !strings.HasPrefix(strings.ToLower(q), strings.ToLower(p.msgQ)) {
		p.msgs, p.msgConvs, p.msgQ = nil, nil, ""
	}
	if q == "" || (p.fileQ != "" && !strings.HasPrefix(strings.ToLower(q), strings.ToLower(p.fileQ))) {
		p.files, p.fileQ = nil, ""
	}
	p.rebuild(now, true)
}

func (p *SearchPopover) tick(gtx layout.Context) {
	q := strings.TrimSpace(p.userQuery())
	c := p.h.Core()
	if q == "" || c == nil {
		return
	}
	seq := p.seq
	if !p.dirFired {
		due := p.changed.Add(150 * time.Millisecond)
		if gtx.Now.Before(due) {
			gtx.Execute(op.InvalidateCmd{At: due})
		} else {
			p.dirFired = true
			topU, topC := srTop(p)
			sq := q
			if t := acrank.SearcherQuery(q); t != "" {
				sq = t
			}
			run(p.h, func(ctx context.Context) ([]core.User, error) { return c.OmniSearchUsers(ctx, sq, topU) },
				func(us []core.User, err error) {
					if p.done || seq != p.seq || err != nil {
						return
					}
					p.rUsers, p.rUQ = us, q
					p.rebuild(time.Now(), false)
				})
			run(p.h, func(ctx context.Context) ([]core.ChannelHit, error) { return c.OmniSearchChannels(ctx, sq, topC) },
				func(hs []core.ChannelHit, err error) {
					if p.done || seq != p.seq || err != nil {
						return
					}
					p.rChans, p.rCQ = hs, q
					p.rebuild(time.Now(), false)
				})
		}
	}
	if !p.msgFired {
		due := p.changed.Add(250 * time.Millisecond)
		if gtx.Now.Before(due) {
			gtx.Execute(op.InvalidateCmd{At: due})
		} else {
			p.msgFired = true
			if o, ok := p.srQuickArgs(q); ok {
				o.Session = p.sess
				on := p.srQuickOn()
				dm := p.dmOf[o.User]
				run(p.h, func(ctx context.Context) (core.SearchResult, error) {
					res, err := c.SearchInline(ctx, o)
					if err == nil {
						res.Messages = tautSearchInline(ctx, p.h, q, res.Messages) // free without plugins
					}
					return res, err
				}, func(res core.SearchResult, err error) {
					if p.done || seq != p.seq || err != nil {
						return
					}
					if on && (o.Channel != "" || o.User != "") {
						// the hits in the in: conversation only
						keep := res.Messages[:0:0]
						for _, m := range res.Messages {
							if (o.Channel != "" && m.Channel == o.Channel) || (o.User != "" && dm != "" && m.Channel == dm) {
								keep = append(keep, m)
							}
						}
						res.Messages = keep
					}
					p.msgs, p.msgConvs, p.msgQ = res.Messages, res.Convs, q
					p.rebuild(time.Now(), false)
				})
			}
			run(p.h, func(ctx context.Context) ([]core.File, error) { return c.AutocompleteFiles(ctx, q) },
				func(fs []core.File, err error) {
					if p.done || seq != p.seq || err != nil {
						return
					}
					p.files, p.fileQ = fs, q
					p.fileByID = map[string]core.File{}
					for _, f := range fs {
						p.fileByID[f.ID] = f
					}
					p.rebuild(time.Now(), false)
				})
		}
	}
}

// srTop is top_users / top_channels: the people and channels of your
// recent conversations, most recent first (Slack's frecency list).
func srTop(p *SearchPopover) (users, chans []string) {
	places, _ := srRecents()
	for _, id := range places {
		if strings.HasPrefix(id, "view:") {
			continue
		}
		if i, ok := p.byConv[id]; ok {
			c := p.cands[i]
			if c.Kind == srKUser {
				users = append(users, c.ID)
				continue
			}
			if c.ConvK == "channel" || c.ConvK == "private" {
				chans = append(chans, c.ID)
			}
		}
	}
	return users, chans
}

// ---- actions ----

func (p *SearchPopover) open(route, conv string) {
	pickedConversation()
	RecordRecentConversation(conv)
	p.Dismiss()
	p.h.Navigate(route)
}

func (p *SearchPopover) submit(q string) {
	q = strings.TrimSpace(q)
	srRecordQuery(q)
	if c := p.h.Core(); c != nil && q != "" {
		c.RecordRecentSearch(q, core.RecentSearchSlack) // maybeRecordRecentSearchEvent
		run(p.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SaveSearch(ctx, q) },
			func(struct{}, error) {})
	}
	p.Dismiss()
	p.h.Navigate("search?q=" + url.QueryEscape(q))
}

// openFeedback is the footer's "Give feedback" (Footer's feedbackLinkProps:
// CombinedSearchFeedbackModal with feedbackType "omniswitcher", the query
// and the suggestions on screen). The popover closes under it.
func (p *SearchPopover) openFeedback() {
	q, sess := strings.TrimSpace(p.userQuery()), p.sess
	sugg := srFeedbackSuggestions(p.rows)
	p.Dismiss()
	openSearchFeedback(p.h, srFeedbackOpts{feedbackType: "omniswitcher", query: q, payload: func() any {
		return srOmniFeedbackPayload{SearchSessionID: sess.ID, Query: q, ClientVersion: core.SearchFeedbackClientVersion,
			Suggestions: sugg}
	}})
}

// srOmniFeedbackPayload is combinedSearchFeedbackSubmit's omniswitcher
// payload, in Slack's key order. Slack's suggestions are its full
// suggestion objects (cleanSuggestions drops modelScore, frecencyScore,
// type and featureVector); Clack sends what its rows hold: the id, the
// name and Slack's is_type_* flag.
type srOmniFeedbackPayload struct {
	SearchSessionID string           `json:"search_session_id"`
	Query           string           `json:"query"`
	ClientVersion   string           `json:"client_version"`
	Suggestions     []map[string]any `json:"suggestions"`
}

// srFeedbackSuggestions are the rows as feedback suggestions.
func srFeedbackSuggestions(rows []srRow) []map[string]any {
	out := []map[string]any{}
	for i := range rows {
		r := &rows[i]
		var flag string
		switch r.kind {
		case srRConv:
			flag = "is_type_channel"
			if r.mpim {
				flag = "is_type_mpim"
			}
		case srRChan:
			flag = "is_type_channel"
		case srRUser:
			flag = "is_type_user"
		case srRPlace:
			flag = "is_type_view"
		case srRShow:
			flag = "is_type_query"
		case srRQuery:
			flag = "is_type_query_history"
		case srRMsg:
			flag = "is_type_quick_message"
		case srRFile:
			flag = "is_type_file"
		default:
			continue
		}
		out = append(out, map[string]any{"id": r.id, "name": r.title, flag: true})
	}
	return out
}

func (p *SearchPopover) activate(i int) {
	if i < 0 || i >= len(p.rows) || !p.rows[i].selectable() {
		return
	}
	r := p.rows[i]
	switch r.kind {
	case srRConv, srRChan:
		p.open("c/"+r.id, r.id)
	case srRUser:
		if r.conv != "" {
			p.open("c/"+r.conv, r.conv)
			return
		}
		c := p.h.Core()
		if c == nil {
			return
		}
		uid := r.id
		h := p.h
		pickedConversation()
		p.Dismiss()
		run(h, func(ctx context.Context) (core.Conv, error) { return c.OpenDM(ctx, uid) }, func(cv core.Conv, err error) {
			if err != nil {
				h.Toast("Couldn't open that conversation: " + errText(err))
				return
			}
			RecordRecentConversation(cv.ID)
			h.Navigate("c/" + cv.ID)
		})
	case srRPlace:
		p.Dismiss()
		p.h.Navigate(r.id)
	case srRShow:
		// the query echo's own replacement: the user's query
		p.restoreUserQuery()
		p.submit(p.reqQuery(p.query))
	case srRQuery:
		p.submit(r.id)
	case srRMsg:
		RecordRecentConversation(r.msg.Channel)
		p.Dismiss()
		p.h.Navigate(msgRoute(*r.msg))
	case srRFile:
		f := p.fileByID[r.id]
		p.Dismiss()
		srOpenFile(p.h, f)
	case srRMore:
		p.unreadsOpen = !p.unreadsOpen
		p.rebuild(time.Now(), false)
	}
}

// move is SearchAutocomplete's increment/decrementSelectedIndex: from
// nothing selected, Down picks the first row and Up the last; past either
// end the selection goes back to none (restoreUserQuery).
func (p *SearchPopover) move(d int) {
	first, last := p.firstRow(), p.lastRow()
	if first < 0 {
		return
	}
	switch {
	case !p.picked && d > 0:
		p.pick(first)
	case !p.picked:
		p.pick(last)
	case d > 0 && p.sel >= last, d < 0 && p.sel <= first:
		p.unpick()
	default:
		i := p.sel
		for i += d; i >= 0 && i < len(p.rows) && !p.rows[i].selectable(); i += d {
		}
		if i < 0 || i >= len(p.rows) {
			p.unpick()
		} else {
			p.pick(i)
		}
	}
	p.ensure = true
	p.srSetText()
}

func (p *SearchPopover) keys(gtx layout.Context) {
	fs := []event.Filter{
		key.Filter{Focus: &p.ed, Name: key.NameUpArrow},
		key.Filter{Focus: &p.ed, Name: key.NameDownArrow},
		key.Filter{Focus: &p.ed, Name: key.NameReturn},
		key.Filter{Focus: &p.ed, Name: key.NameEnter},
		key.Filter{Focus: &p.ed, Name: key.NameEscape},
	}
	if p.ac.open {
		fs = append(fs, key.Filter{Focus: &p.ed, Name: key.NameTab, Optional: key.ModShift})
	} else if len(p.tbFor()) > 0 {
		fs = append(fs, key.Filter{Focus: &p.ed, Name: key.NameTab}) // Tab: the toolbar
	}
	for {
		e, ok := gtx.Event(fs...)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		if p.ac.open {
			// the completer's menu takes the keys (TextyAutocomplete)
			switch ke.Name {
			case key.NameUpArrow:
				p.acMove(-1)
			case key.NameDownArrow:
				p.acMove(1)
			case key.NameTab:
				if ke.Modifiers.Contain(key.ModShift) {
					p.acMove(-1)
				} else {
					p.acAccept(p.ac.sel)
				}
			case key.NameReturn, key.NameEnter:
				p.acAccept(p.ac.sel)
			case key.NameEscape:
				p.acDismiss()
			}
			p.h.Invalidate()
			continue
		}
		if p.dpShown() {
			// the date picker's surface: ↓ / ↑ move into its grid,
			// Escape closes it (Popover's onClose)
			switch ke.Name {
			case key.NameUpArrow, key.NameDownArrow:
				if ke.Modifiers == 0 && p.dpFocusGrid(gtx) {
					continue
				}
			case key.NameEscape:
				p.dpDismiss()
				p.h.Invalidate()
				continue
			}
		}
		switch ke.Name {
		case key.NameUpArrow:
			p.move(-1)
		case key.NameDownArrow:
			p.move(1)
		case key.NameReturn, key.NameEnter:
			p.enter()
		case key.NameTab:
			p.tbFocus(gtx)
		case key.NameEscape:
			if isTop(p) {
				p.Dismiss()
			}
		}
		if p.done {
			return
		}
	}
}

// ---- layout ----

// Layout draws the popover.
func (p *SearchPopover) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if p.done {
		return layout.Dimensions{}
	}
	stacked(gtx, p)
	th := p.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	p.loadSnap(gtx.Now)
	if c := p.h.Core(); c != nil && strings.TrimSpace(p.userQuery()) == "" {
		// the history and Recent places follow the recents' arrival and
		// the read experiment's group (an Experiments plugin flips it live)
		v, g := c.RecentsVersion(), c.Experiment("desktop_search_recents_read")
		if v != p.recVer || g != p.recRead {
			p.recVer, p.recRead = v, g
			p.rebuild(gtx.Now, false)
		}
	}
	if !p.focus || p.refocus {
		gtx.Execute(key.FocusCmd{Tag: &p.ed})
		p.focus, p.refocus = true, false
	}
	p.keys(gtx)
	if p.done {
		return layout.Dimensions{}
	}
	for {
		e, ok := p.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			if t := p.ed.Text(); t != p.query {
				p.tokens = srShiftTokens(p.tokens, p.query, t)
				p.query, p.acArmed = t, true
				if strings.TrimSpace(t) != "" {
					p.histMode = false
				}
				p.onQuery(gtx)
			}
		case widget.SubmitEvent:
			p.enter()
		case widget.SelectEvent:
			// ArrowLeft/Right, Home, End, ⌘A move the caret: Slack's
			// onKeydown resets the selection (selectedIndex null)
			if p.picked {
				p.unpick()
			}
		}
		if p.done {
			return layout.Dimensions{}
		}
	}
	// header buttons (last frame's clicks)
	if p.clear.Clicked(gtx) || p.clearReq {
		p.clearReq = false
		p.ed.SetText("")
		p.query, p.tokens = "", nil
		p.refocus = true
		p.onQuery(gtx)
	}
	if p.filter.Clicked(gtx) {
		p.qfToggleRow() // QuickFilterMenu's onToggleFilters
	}
	if p.advReq {
		q := strings.TrimSpace(p.reqQuery(p.query))
		srRecordQuery(q)
		p.Dismiss()
		p.h.Navigate("search?q=" + url.QueryEscape(q))
		return layout.Dimensions{}
	}
	if p.history.Clicked(gtx) {
		p.histMode = !p.histMode
		p.refocus = true
		p.rebuild(gtx.Now, true)
	}
	if p.feedback.Clicked(gtx) {
		p.openFeedback()
		return layout.Dimensions{}
	}
	p.acUpdate()
	p.dpPointer(gtx)
	p.dpUpdate()
	p.dpKeys(gtx)
	p.tbKeys(gtx)
	if p.done {
		return layout.Dimensions{}
	}
	p.qfAutoShow()
	tbHit := p.tbClicks(gtx) // first: a toolbar button's click reaches its row too
	for i := range p.rows {
		if p.clicks[i].Clicked(gtx) {
			if tbHit && p.picked && i == p.sel {
				continue // the button took it
			}
			p.activate(i)
			if p.done {
				return layout.Dimensions{}
			}
		}
	}
	// onMouseMove picks the row under the pointer; leaving the list
	// (onMouseLeaveList) goes back to nothing selected.
	hov := -1
	for i := range p.rows {
		if p.rows[i].selectable() && p.clicks[i].Hovered() {
			hov = i
		}
	}
	if hov != p.hover {
		if hov >= 0 {
			p.pick(hov)
		}
		p.hover = hov
	}
	if in := p.listHov.Update(gtx.Source); in != p.inList {
		p.inList = in
		if !in && p.picked {
			p.unpick()
		}
	}
	p.tick(gtx)
	if p.scrim.Layout(gtx) {
		p.Dismiss()
		return layout.Dimensions{}
	}

	// geometry
	var x, y, w int
	if !p.pill.Empty() {
		x, y, w = p.pill.Min.X-dp(gtx, 8), p.pill.Min.Y-dp(gtx, 16), p.pill.Dx()+dp(gtx, 16)
	} else {
		w = minInt(dp(gtx, 649), win.X-dp(gtx, 32))
		x, y = (win.X-w)/2, dp(gtx, 4)
	}
	if y < dp(gtx, 4) {
		y = dp(gtx, 4)
	}
	if w > win.X-dp(gtx, 8) {
		w = win.X - dp(gtx, 8)
	}
	if x+w > win.X-dp(gtx, 4) {
		x = win.X - dp(gtx, 4) - w
	}
	if x < dp(gtx, 4) {
		x = dp(gtx, 4)
	}
	// the input row, the quick filter row while it shows, the 1px line
	inputH := dp(gtx, 48) + p.qfRowH(gtx) + 1
	padT, padB := dp(gtx, 8), dp(gtx, 8)
	footH := p.footerH(gtx, th, w)
	content := 0
	for i := range p.rows {
		content += dp(gtx, p.rows[i].heightDp())
	}
	h := inputH + padT + content + padB + footH
	if maxH := win.Y - y - dp(gtx, 8); h > maxH {
		h = maxH
	}
	acOpen := p.ac.open || p.dpShown()
	if acOpen {
		// isTextyAutocompleteOpen or a date picker's onOpenChange: the
		// suggestions and the footer hide
		h = inputH
	}
	listH := h - inputH - padT - padB - footH
	if listH < 0 {
		listH = 0
	}
	p.rect = image.Rect(x, y, x+w, y+h)
	rad := dp(gtx, 8)
	card(gtx, cl, p.rect, rad)
	p.block.Layout(gtx, p.rect)
	p.layoutCard(gtx, th, cl, w, h, inputH, padT, footH, listH, rad, acOpen)
	p.tbRunPending(gtx)
	if p.done {
		return layout.Dimensions{}
	}
	if p.ac.open {
		p.acLayout(gtx, th, cl, win, inputH)
	}
	if p.dpShown() {
		p.dpLayout(gtx, inputH)
	}
	return layout.Dimensions{Size: win}
}

// layoutCard draws the input row and, unless a completer's menu is open,
// the list and the footer, inside the card.
func (p *SearchPopover) layoutCard(gtx layout.Context, th *theme.Theme, cl *Colors, w, h, inputH, padT, footH, listH, rad int, acOpen bool) {
	cs := clipRRect(gtx, p.rect, rad)
	defer cs.Pop()
	defer op.Offset(p.rect.Min).Push(gtx.Ops).Pop()

	p.layoutInput(gtx, th, cl, w, inputH)
	if p.qfVisible() {
		p.qfLayout(gtx, th, cl, dp(gtx, 48), w)
	}
	if acOpen {
		return
	}
	p.listOrg = p.rect.Min.Add(image.Pt(0, inputH+padT))

	// list
	if p.ensure {
		p.ensure = false
		p.scrollToSel(gtx, listH)
	}
	at(gtx, image.Pt(0, inputH+padT), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, listH))
		return slackc.RenderLazy(gtx, "SearchAutocomplete", p.autocompleteProps, func(gtx layout.Context, pp slackc.Props) layout.Dimensions {
			p.applySuggestions(pp)
			defer clip.Rect{Max: image.Pt(w, listH)}.Push(gtx.Ops).Pop()
			p.listHov.Add(gtx.Ops)
			return p.list.Layout(gtx, len(p.rows), func(gtx layout.Context, i int) layout.Dimensions {
				return p.row(gtx, th, cl, i, w)
			})
		})
	})

	// footer
	fy := h - footH
	fill(gtx, image.Rect(0, fy, w, fy+1), srLineColor(th, cl))
	at(gtx, image.Pt(0, fy), func(gtx layout.Context) layout.Dimensions {
		return p.footer(gtx, th, cl, image.Pt(w, footH))
	})
}

// srLineColor is the input row's bottom border (#dddddd in light mode).
func srLineColor(th *theme.Theme, cl *Colors) color.NRGBA {
	if th != nil && th.Dark {
		return cl.Divider
	}
	return cl.Tab
}

func (p *SearchPopover) scrollToSel(gtx layout.Context, viewH int) {
	if p.sel < 0 || p.sel >= len(p.rows) {
		return
	}
	top := 0
	for i := 0; i < p.sel; i++ {
		top += dp(gtx, p.rows[i].heightDp())
	}
	bot := top + dp(gtx, p.rows[p.sel].heightDp())
	cur := 0
	for i := 0; i < p.list.Position.First && i < len(p.rows); i++ {
		cur += dp(gtx, p.rows[i].heightDp())
	}
	cur += p.list.Position.Offset
	switch {
	case top < cur:
		cur = top
		// keep the section header visible when moving to its first row
		if p.sel > 0 && p.rows[p.sel-1].kind == srRHeader {
			cur -= dp(gtx, p.rows[p.sel-1].heightDp())
		}
	case bot > cur+viewH:
		cur = bot - viewH
	default:
		return
	}
	if cur < 0 {
		cur = 0
	}
	p.list.Position = layout.Position{First: 0, Offset: cur}
}

func (p *SearchPopover) inputRow(gtx layout.Context, th *theme.Theme, cl *Colors, w int) {
	rh := dp(gtx, 48)
	// .c-search__input_wrapper's border-bottom, under the quick filter row
	ly := rh + p.qfRowH(gtx)
	fill(gtx, image.Rect(0, ly, w, ly+1), srLineColor(th, cl))
	at(gtx, image.Pt(dp(gtx, 20), (rh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "search", 20, cl.Fg2)
	})
	// right side, from the edge inwards
	hb := dp(gtx, 24)
	hx := w - dp(gtx, 36)
	at(gtx, image.Pt(hx, (rh-hb)/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &p.history, "clock", 24, 18, cl.Fg2, cl.BgHover, 4)
	})
	p.historyRect = image.Rect(hx, (rh-hb)/2, hx+hb, (rh+hb)/2).Add(p.rect.Min)
	p.filterButton(gtx, th, w-dp(gtx, 68), (rh-hb)/2)
	right := w - dp(gtx, 80)
	if p.ed.Len() > 0 {
		fill(gtx, image.Rect(right, (rh-dp(gtx, 20))/2, right+1, (rh+dp(gtx, 20))/2), cl.Divider)
		cw := textW(gtx, th, 13, theme.Regular, nsSearch.T("Clear"))
		cx := right - dp(gtx, 17) - cw
		at(gtx, image.Pt(cx, 0), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &p.clear, nil, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg2, nsSearch.T("Clear"))
				})
			})
		})
		right = cx
	}
	tx := dp(gtx, 56)
	tw := right - tx - dp(gtx, 12)
	if tw < 1 {
		tw = 1
	}
	at(gtx, image.Pt(tx, 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, rh)}
			// the editor first (a pill's click may change the box), drawn
			// over the placeholder and the ghost text
			edCall, edDims := record(gtx, func(gtx layout.Context) layout.Dimensions { return p.layoutEditor(gtx, th, cl) })
			if p.ed.Len() == 0 && p.ghostText() == "" {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Point{}
					return text(gtx, th, 15, theme.Regular, cl.Placeholder, nsSearch.T("Search across people, channels, files, workflows, and more"))
				})
				call.Add(gtx.Ops)
			}
			if ghost := p.ghostText(); ghost != "" && gtx.Focused(&p.ed) {
				qw := p.textEndX(gtx, th)
				if gw := textW(gtx, th, 15, theme.Regular, ghost); qw+gw <= tw {
					call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min = image.Point{}
						return text(gtx, th, 15, theme.Regular, cl.Fg3, ghost)
					})
					t := op.Offset(image.Pt(qw, 0)).Push(gtx.Ops)
					call.Add(gtx.Ops)
					t.Pop()
				}
			}
			edCall.Add(gtx.Ops)
			return edDims
		})
	})
	p.edX = tx
	r := image.Rect(tx, 0, tx+tw, rh)
	st := clip.Rect(r).Push(gtx.Ops)
	pointer.CursorText.Add(gtx.Ops)
	st.Pop()
}

// textEndX is where the box's text ends as the editor laid it out: with
// tokens, wider than the plain text (the pills' space, the bold values),
// a pill at the end with its right padding.
func (p *SearchPopover) textEndX(gtx layout.Context, th *theme.Theme) int {
	n := p.ed.Len()
	if len(p.tokens) == 0 || n == 0 {
		return textW(gtx, th, 15, theme.Regular, p.query)
	}
	x := 0
	for _, r := range p.ed.Regions(0, n, nil) {
		x = max(x, r.Bounds.Max.X)
	}
	for i, t := range p.tokens {
		if t.end() != n {
			continue
		}
		if b, ok := p.tokenBounds(i); ok {
			x = max(x, b.Max.X)
		}
		if t.mention {
			x += dp(gtx, 2)
		} else {
			x += dp(gtx, 4)
		}
	}
	return x
}

// layoutEditor draws the box's editor with its tokens: the pills under
// the text, then the tokens' text again in content-hgl-1 (the editor
// paints one color).
func (p *SearchPopover) layoutEditor(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	sel := theme.WithAlpha(cl.Focus, 0x55)
	p.pillPointer(gtx)
	p.ed.SetSpans(p.tokenSpans(gtx)) // the pills' space and bold values
	d := th.Editor(gtx, &p.ed, unit.Sp(15), cl.Fg, sel)
	if len(p.tokens) > 0 {
		p.drawPills(gtx, th, cl) // after the layout: this frame's regions
	}
	return d
}

// ghostText is SearchInputBox's getGhostTextForSelectedSuggestion: the
// rest of the selected (else the first) suggestion's name when it starts
// with the query, then " — Open"; " — Search" for a recent search or the
// "Show results for" row. With an empty box only a picked row has one
// ("<name> — Open", "<query> — Search"). Typing a space hides it
// (hasTypedSpace).
func (p *SearchPopover) ghostText() string {
	if p.sel < 0 || p.sel >= len(p.rows) || strings.HasSuffix(p.query, " ") {
		return ""
	}
	r := &p.rows[p.sel]
	nav := false
	switch r.kind {
	case srRConv, srRUser, srRChan, srRFile, srRPlace:
		nav = true
	}
	if p.query == "" {
		switch {
		case !p.picked:
			return ""
		case r.kind == srRQuery:
			return r.title + nsSearch.T(" — Search")
		case nav && r.title != "":
			return r.title + nsSearch.T(" — Open")
		}
		return ""
	}
	switch {
	case r.kind == srRQuery, r.kind == srRShow:
		return nsSearch.T(" — Search")
	case !nav:
		return ""
	}
	name, q := []rune(r.title), []rune(p.query)
	if len(q) > len(name) || !strings.EqualFold(string(name[:len(q)]), string(q)) {
		return ""
	}
	return string(name[len(q):]) + nsSearch.T(" — Open")
}

// srClientOnline is isClientOnline (core/online.go) for the footer; signed
// out counts as online. Tests replace it.
var srClientOnline = func(h Host) bool {
	c := h.Core()
	return c == nil || c.ClientOnline()
}

// srOfflineText is the footer's offline notice, rt'd in the search I18n
// (lj3A Footer's eC) with the cloud-offline icon at {iconOffline}.
const srOfflineText = "{iconOffline} <strong>No connection.</strong> Search is not available while offline, and navigating to conversations may be limited."

// footerH is the footer's height: 45 (padding 12 16, the 20px key chips,
// the 1px border-top), or with the offline notice its lines in the
// modal's 16px line-height (.c-search_modal) plus the padding and border.
func (p *SearchPopover) footerH(gtx layout.Context, th *theme.Theme, w int) int {
	if srClientOnline(p.h) {
		return dp(gtx, 45)
	}
	_, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		return srOfflineNotice(gtx, th, color.NRGBA{}, srOfflineW(gtx, w))
	})
	return d.Size.Y + dp(gtx, 24) + 1
}

// srOfflineW is the notice's wrap width: .offlineNotice__a6x3P grows to
// the footer's content box (padding 16 a side) less its padding-right 48.
func srOfflineW(gtx layout.Context, w int) int {
	return max(1, w-dp(gtx, 32)-dp(gtx, 48))
}

// srOfflineNotice lays out the notice as one wrapping paragraph (pre-wrap,
// word by word), 13px in the footer's content-sec, the <strong> part
// bold. The icon is the legacy font Icon (type cloud-offline, size
// inherit, c-icon--inline: a 1em box, the glyph U+E0A0 at the text's 13px
// in the line's middle); its ink spans the whole em, which Clack's
// cloud-offline centrelines reach at 14.4.
func srOfflineNotice(gtx layout.Context, th *theme.Theme, col color.NRGBA, w int) layout.Dimensions {
	lh := dp(gtx, 16)
	x, y := 0, 0
	space := textW(gtx, th, 13, theme.Regular, " ")
	place := func(ww int) {
		if x > 0 && x+ww > w {
			x, y = 0, y+lh
		}
	}
	for _, pt := range nsSearch.RT(srOfflineText, "iconOffline", "\uFFFC") {
		if pt.Arg == "iconOffline" {
			iw := dp(gtx, 13)
			place(iw)
			// the glyph's ink: x 0 to 1em, y 1.3 to 12.7 of the em box,
			// which sits 1.5 into the 16px line
			at(gtx, image.Pt(x-dp(gtx, 0.7), y+dp(gtx, 0.85)), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "cloud-offline", 14.4, col)
			})
			x += iw
			continue
		}
		wt := theme.Regular
		if pt.Tag == "strong" || pt.Tag == "b" {
			wt = theme.Bold
		}
		for _, wd := range strings.SplitAfter(pt.Text, " ") {
			if wd == "" {
				continue
			}
			for _, word := range i18n.LineChunks(strings.TrimRight(wd, " ")) {
				ww := textW(gtx, th, 13, wt, word)
				place(ww)
				at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 13, wt, col, word)
					})
				})
				x += ww
			}
			if strings.HasSuffix(wd, " ") {
				x += space
			}
		}
	}
	return layout.Dimensions{Size: image.Pt(w, y+lh)}
}

func (p *SearchPopover) footer(gtx layout.Context, th *theme.Theme, cl *Colors, sz image.Point) layout.Dimensions {
	if !srClientOnline(p.h) {
		// Footer with shouldShowOfflineNotice: no keyboard hints, and
		// useShouldShowFooterContent (isClientOnline, no megaphone)
		// hides the feedback link
		at(gtx, image.Pt(dp(gtx, 16), 1+dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions { // under the 1px border
			return srOfflineNotice(gtx, th, cl.Fg2, srOfflineW(gtx, sz.X))
		})
		return layout.Dimensions{Size: sz}
	}
	ch := dp(gtx, 20)
	cy := (sz.Y - ch) / 2
	x := dp(gtx, 16)
	if p.picked {
		// a row is selected: [Enter] Open, [Tab] Actions (the row's
		// toolbar); keyboardGroup gap 6, keyboardContainer gap 10. The
		// Enter chip is the prefs I18n's (ev.t("Enter", {ns: "prefs"}):
		// "Intro" in es-ES); the Tab chip is a literal "Tab".
		group := func(chip, hint string) {
			ew := textW(gtx, th, 13, theme.Regular, chip) + dp(gtx, 12)
			rrect(gtx, image.Rect(x, cy, x+ew, cy+ch), dp(gtx, 4), cl.KbdLight)
			at(gtx, image.Pt(x+dp(gtx, 6), cy), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg, chip)
				})
			})
			x += ew + dp(gtx, 6)
			d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg2, hint)
				})
			})
			x += d.Size.X + dp(gtx, 10)
		}
		group(nsPrefs.T("Enter"), nsSearch.T("Open"))
		group("Tab", nsSearch.T("Actions"))
		p.feedbackLink(gtx, th, cl, sz)
		return layout.Dimensions{Size: sz}
	}
	for _, name := range [...]string{"caret-up", "caret-down"} {
		rrect(gtx, image.Rect(x, cy, x+ch, cy+ch), dp(gtx, 4), cl.KbdLight)
		is := dp(gtx, 12)
		n := name
		at(gtx, image.Pt(x+(ch-is)/2, cy+(ch-is)/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, n, 12, cl.Fg)
		})
		x += ch + dp(gtx, 5)
	}
	at(gtx, image.Pt(x+dp(gtx, 1), 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Regular, cl.Fg2, nsSearch.T("Select"))
		})
	})
	p.feedbackLink(gtx, th, cl, sz)
	return layout.Dimensions{Size: sz}
}

// feedbackLink is the footer's FeedbackLink (experimentOmniPolish: just
// "Give feedback").
func (p *SearchPopover) feedbackLink(gtx layout.Context, th *theme.Theme, cl *Colors, sz image.Point) {
	fw := textW(gtx, th, 13, theme.Regular, nsSearch.T("Give feedback"))
	at(gtx, image.Pt(sz.X-dp(gtx, 16)-fw, 0), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &p.feedback, nil, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Link, nsSearch.T("Give feedback"))
			})
		})
	})
}

func (p *SearchPopover) row(gtx layout.Context, th *theme.Theme, cl *Colors, i, w int) layout.Dimensions {
	r := &p.rows[i]
	sz := image.Pt(w, dp(gtx, r.heightDp()))
	gtx.Constraints.Min = image.Point{}
	switch r.kind {
	case srRHeader:
		at(gtx, image.Pt(dp(gtx, 16), 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 32), sz.Y)}
			return vcenter(gtx, dp(gtx, 32), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg3, r.title)
			})
		})
		return layout.Dimensions{Size: sz}
	case srRDivider:
		y := sz.Y / 2
		fill(gtx, image.Rect(dp(gtx, 16), y, w-dp(gtx, 16), y+1), cl.Ring)
		return layout.Dimensions{Size: sz}
	case srRMsg:
		return slackc.Component(gtx, "SearchResult", srMsgArgs{p: p, th: th, cl: cl, i: i, sz: sz},
			srMsgArgs.props, srMsgArgs.layout)
	case srRNote: // c-search_autocomplete__history_empty
		at(gtx, image.Pt(dp(gtx, 16), 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 32), sz.Y)}
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Fg3, r.title)
			})
		})
		return layout.Dimensions{Size: sz}
	case srRMore: // c-search_autocomplete__see_more (--selected: base-ter, underlined; :hover underlined)
		sel := i == p.sel && p.picked
		under := sel || p.clicks[i].Hovered()
		return p.clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if sel {
				fill(gtx, image.Rectangle{Max: sz}, cl.Selected)
			}
			d := at(gtx, image.Pt(dp(gtx, 52), 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 68), sz.Y)}
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Link, r.title)
				})
			})
			if under {
				uy := (sz.Y+dp(gtx, 13))/2 + dp(gtx, 1)
				fill(gtx, image.Rect(dp(gtx, 52), uy, dp(gtx, 52)+d.Size.X, uy+maxInt(1, dp(gtx, 1))), cl.Link)
			}
			pointerCursor(gtx, sz)
			return layout.Dimensions{Size: sz}
		})
	}
	sel := i == p.sel && p.picked
	var state css.State
	if sel || p.clicks[i].Hovered() {
		state = css.StateHover
	}
	if sel {
		state |= css.StateFocusVisible
	}
	rst := srSuggClass.EnterAttr(gtx, state, 0, "data-type", srDataType(r))
	defer rst.Leave()
	if rst.Hidden() {
		return layout.Dimensions{}
	}
	gtx.Constraints = layout.Exact(sz)
	return rst.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		return p.suggestion(gtx, th, cl, rst, i, sz)
	})
}

// suggestion draws a non-message row (Slack's
// .c-search_autocomplete__suggestion_item; icon and title are its
// __suggestion_item_left, which StreamerMode blurs).
//
// A picked row is .c-search_autocomplete__suggestion_item--selected:
// #1264a3 with white text and icons, no badge. Nothing picked, the first
// row is --pseudo-selected while the input has the focus: base-ter and the
// Enter key (suggestion_item_shortcut, 16px from the content).
func (p *SearchPopover) suggestion(gtx layout.Context, th *theme.Theme, cl *Colors, rst slackc.Styled, i int, sz image.Point) layout.Dimensions {
	r := &p.rows[i]
	picked := i == p.sel && p.picked
	pseudo := i == p.sel && !p.picked && gtx.Focused(&p.ed)
	return p.clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		switch {
		case picked:
			fill(gtx, image.Rectangle{Max: sz}, rst.Background(cl.Highlight))
		case pseudo:
			fill(gtx, image.Rectangle{Max: sz}, rst.Background(cl.Selected))
		case rst.Has("background-color"):
			fill(gtx, image.Rectangle{Max: sz}, rst.Background(cl.Bg))
		}
		// right side; a picked row's toolbar ends its left part
		right := sz.X - dp(gtx, 16)
		if picked {
			right = p.tbRight(gtx, sz.X)
		}
		if pseudo {
			right -= srEnterChip(gtx, th, cl, right, sz.Y)
			right -= dp(gtx, 16)
		}
		if r.badge > 0 && !picked {
			right -= srBadge(gtx, th, cl, r.badge, right, sz.Y)
			right -= dp(gtx, 12)
		}
		lst := srSuggLeftClass.Enter(gtx, 0, 0)
		if !lst.Hidden() {
			lst.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
				p.suggestionLeft(gtx, th, cl, r, picked, pseudo, sz, right)
				return layout.Dimensions{Size: image.Pt(right, sz.Y)}
			})
		}
		lst.Leave()
		pointerCursor(gtx, sz)
		if picked {
			p.tbLayout(gtx, th, sz)
		}
		return layout.Dimensions{Size: sz}
	})
}

// suggestionLeft draws the row's icon, title and secondary text left of
// right.
func (p *SearchPopover) suggestionLeft(gtx layout.Context, th *theme.Theme, cl *Colors, r *srRow, picked, pseudo bool, sz image.Point, right int) {
	box := dp(gtx, 24)
	lx, ly := dp(gtx, 16), (sz.Y-box)/2
	rowBg := cl.Bg
	fg, fg2, fg3 := cl.Fg, cl.Fg2, cl.Fg3
	switch {
	case picked:
		rowBg = cl.Highlight
		fg, fg2, fg3 = cl.HighText, cl.HighText, cl.HighText
	case pseudo:
		rowBg = cl.Selected
	}
	switch {
	case r.mpim:
		at(gtx, image.Pt(lx, ly), func(gtx layout.Context) layout.Dimensions {
			return mpimTile(gtx, p.h, r.members, 24, rowBg, picked || pseudo)
		})
	case r.isAvatar:
		at(gtx, image.Pt(lx, ly), func(gtx layout.Context) layout.Dimensions {
			return avatar(gtx, p.h, r.avatar, 24)
		})
		if r.presence != "" {
			d := dp(gtx, 10)
			c := image.Pt(lx+box-d/2+dp(gtx, 1), ly+box-d/2+dp(gtx, 1))
			theme.Circle(gtx, c, d, rowBg)
			dd := dp(gtx, 7)
			at(gtx, c.Sub(image.Pt(dd/2, dd/2)), func(gtx layout.Context) layout.Dimensions {
				if picked && r.presence != "active" {
					// the --selected row whitens a non-active presence ring
					stroke(gtx, image.Rect(0, 0, dd, dd), dd/2, border1(gtx), fg)
					return layout.Dimensions{Size: image.Pt(dd, dd)}
				}
				return presenceDot(gtx, th, r.presence == "active", 7)
			})
		}
	case r.boxed:
		rb := image.Rect(lx, ly, lx+box, ly+box)
		stroke(gtx, rb, dp(gtx, 6), border1(gtx), cl.BtnBorder)
		is := dp(gtx, 16)
		at(gtx, image.Pt(lx+(box-is)/2, ly+(box-is)/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, r.icon, 16, fg)
		})
	case r.icon != "":
		is := dp(gtx, 20)
		at(gtx, image.Pt(lx+(box-is)/2, ly+(box-is)/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, r.icon, 20, fg2)
		})
	}
	x := dp(gtx, 52)
	maxX := right
	x = p.drawTitle(gtx, th, fg, r, x, maxX, sz.Y)
	if r.alt != "" && x < maxX {
		s := " · " + r.alt
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxX-x, sz.Y)}
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, fg3, s)
			})
		})
	}
}

// drawTitle draws the row's primary text (match in bold) and returns the x
// after it.
func (p *SearchPopover) drawTitle(gtx layout.Context, th *theme.Theme, fg color.NRGBA, r *srRow, x, maxX, h int) int {
	type piece struct {
		s  string
		wt font.Weight
	}
	var ps [3]piece
	n := 0
	switch {
	case r.kind == srRShow:
		ps[0] = piece{srShowResultsFor(), theme.Regular}
		ps[1] = piece{r.title, theme.Bold}
		n = 2
	case r.boldAll:
		ps[0] = piece{r.title, theme.Bold}
		n = 1
	case r.b1 > r.b0 && r.b1 <= len(r.title):
		if r.b0 > 0 {
			ps[n] = piece{r.title[:r.b0], theme.Regular}
			n++
		}
		ps[n] = piece{r.title[r.b0:r.b1], theme.Bold}
		n++
		if r.b1 < len(r.title) {
			ps[n] = piece{r.title[r.b1:], theme.Regular}
			n++
		}
	default:
		ps[0] = piece{r.title, theme.Regular}
		n = 1
	}
	for k := 0; k < n && x < maxX; k++ {
		pc := ps[k]
		d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxX-x, h)}
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, pc.wt, fg, pc.s)
			})
		})
		x += d.Size.X
	}
	return x
}

// srEnterChip draws the "Enter" hint ending at right; returns its width.
// Its label is the prefs I18n's, as the row's KeyboardKey has it
// (t("Enter", {ns: "prefs"}): "Intro" in es-ES).
func srEnterChip(gtx layout.Context, th *theme.Theme, cl *Colors, right, h int) int {
	tw := textW(gtx, th, 13, theme.Regular, nsPrefs.T("Enter"))
	w, ch := tw+dp(gtx, 12), dp(gtx, 20)
	r := image.Rect(right-w, (h-ch)/2, right, (h+ch)/2)
	rrect(gtx, r, dp(gtx, 4), cl.Ring)
	rrect(gtx, r.Inset(border1(gtx)), dp(gtx, 4)-border1(gtx), cl.BgAlt)
	at(gtx, image.Pt(r.Min.X+dp(gtx, 6), r.Min.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Regular, cl.Fg, nsPrefs.T("Enter"))
		})
	})
	return w
}

// srBadge draws a mention badge pill ending at right; returns its width.
func srBadge(gtx layout.Context, th *theme.Theme, cl *Colors, n, right, h int) int {
	s := strconv.Itoa(n)
	tw := textW(gtx, th, 12, theme.Bold, s)
	bh := dp(gtx, 18)
	w := maxInt(bh, tw+dp(gtx, 16))
	r := image.Rect(right-w, (h-bh)/2, right, (h+bh)/2)
	rrect(gtx, r, bh/2, cl.Badge)
	at(gtx, image.Pt(r.Min.X+(w-tw)/2, r.Min.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 12, theme.Bold, cl.BadgeText, s)
		})
	})
	return w
}

func (p *SearchPopover) msgRow(gtx layout.Context, th *theme.Theme, cl *Colors, i int, sz image.Point, name, av string, rp slackc.Props) layout.Dimensions {
	r := &p.rows[i]
	if r.plain {
		return p.msgRowPlain(gtx, th, cl, i, sz, name, av, rp)
	}
	// the card (--quick_message_item--card: 1px otl-ter, radius 12,
	// padding 12/14/12/12) turns --selected when picked (#1264a3, white
	// text); the pseudo-selected row is base-ter around and inside it
	sel := i == p.sel && p.picked
	pseudo := i == p.sel && !p.picked && gtx.Focused(&p.ed)
	gtx.Constraints = layout.Exact(sz)
	return p.clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		cr := image.Rect(dp(gtx, 16), dp(gtx, 6), sz.X-dp(gtx, 16), sz.Y-dp(gtx, 6))
		rad := dp(gtx, 12)
		bg, fg, fg3 := cl.Bg, cl.Fg, cl.Fg3
		switch {
		case sel:
			bg, fg, fg3 = cl.Highlight, cl.HighText, cl.HighText
		case pseudo:
			fill(gtx, image.Rectangle{Max: sz}, cl.Selected)
			bg = cl.Selected
		}
		rrect(gtx, cr, rad, cl.Ring)
		rrect(gtx, cr.Inset(border1(gtx)), rad-border1(gtx), bg)
		ax := cr.Min.X + dp(gtx, 13)
		at(gtx, image.Pt(ax, cr.Min.Y+(cr.Dy()-dp(gtx, 36))/2), func(gtx layout.Context) layout.Dimensions {
			return srAvatar(gtx, p.h, r.msg, av, 36, rp)
		})
		tx := ax + dp(gtx, 36) + dp(gtx, 11)
		right := cr.Max.X - dp(gtx, 16)
		lh := dp(gtx, 20)
		ly := cr.Min.Y + dp(gtx, 11)
		if r.when != "" {
			ww := textW(gtx, th, 13, theme.Regular, r.when)
			at(gtx, image.Pt(right-ww, ly), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, fg3, r.when)
				})
			})
			right -= ww + dp(gtx, 12)
		}
		x := tx
		d := at(gtx, image.Pt(x, ly), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, right-x), lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Black, fg, name)
			})
		})
		x += d.Size.X + dp(gtx, 6)
		srDrawWhereFg(gtx, p.h, r.where, x, ly, right, lh, bg, fg3)
		bw := cr.Max.X - dp(gtx, 16) - tx
		if r.tokW != bw {
			ell := textW(gtx, th, 15, theme.Regular, " …")
			toks, _, over := srWrapFit(gtx, th, r.segs, bw, 1, ell)
			if over {
				toks = append(toks, srTok{s: " …", x: srEndX(toks, 0), w: ell})
			}
			r.toks, r.tokW = toks, bw
		}
		at(gtx, image.Pt(tx, ly+lh), func(gtx layout.Context) layout.Dimensions {
			srDrawToks(gtx, th, cl, r.toks, dp(gtx, 22), fg)
			return layout.Dimensions{}
		})
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}

// msgRowPlain is SuggestionQuickMessage with omni_quick_message_results
// off: a --multiline suggestion item (padding 4px 16px, the 36px avatar
// 12px before the text) with the extract on top (15px, one line, …
// where Slack cut it) and the metadata under it (13px at 70%: the
// sender, "• " or "⟳ Reply in a thread • ", the date). The picked row is
// --selected (#1264a3, white).
func (p *SearchPopover) msgRowPlain(gtx layout.Context, th *theme.Theme, cl *Colors, i int, sz image.Point, name, av string, rp slackc.Props) layout.Dimensions {
	r := &p.rows[i]
	picked := i == p.sel && p.picked
	pseudo := i == p.sel && !p.picked && gtx.Focused(&p.ed)
	gtx.Constraints = layout.Exact(sz)
	return p.clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		fg, meta := cl.Fg, cl.Fg2
		switch {
		case picked:
			fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
			fg, meta = cl.HighText, cl.HighText
		case pseudo:
			fill(gtx, image.Rectangle{Max: sz}, cl.Selected)
		}
		ax := dp(gtx, 16)
		at(gtx, image.Pt(ax, (sz.Y-dp(gtx, 36))/2), func(gtx layout.Context) layout.Dimensions {
			return srAvatar(gtx, p.h, r.msg, av, 36, rp)
		})
		tx := ax + dp(gtx, 36) + dp(gtx, 12)
		right := sz.X - dp(gtx, 16)
		lh := dp(gtx, 22)
		y1 := dp(gtx, 2) // padding 4, the top line's margin -2
		bw := maxInt(1, right-tx)
		if r.tokW != bw {
			ell := textW(gtx, th, 15, theme.Regular, "…")
			toks, _, over := srWrapFit(gtx, th, r.segs, bw, 1, ell)
			if over {
				toks = append(toks, srTok{s: "…", x: srEndX(toks, 0), w: ell})
			}
			r.toks, r.tokW = toks, bw
		}
		at(gtx, image.Pt(tx, y1), func(gtx layout.Context) layout.Dimensions {
			srDrawToks(gtx, th, cl, r.toks, lh, fg)
			return layout.Dimensions{}
		})
		y2 := y1 + lh + dp(gtx, 2)
		x := tx
		put := func(s string) {
			if x >= right || s == "" {
				return
			}
			d := at(gtx, image.Pt(x, y2), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(right-x, lh)}
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, meta, s)
				})
			})
			x += d.Size.X
		}
		bullet := func() {
			x += dp(gtx, 6)
			put("\u2022")
			x += dp(gtx, 6)
		}
		put(name)
		if r.msg != nil && r.msg.ThreadTS != "" && r.msg.ThreadTS != r.msg.TS {
			x += dp(gtx, 6)
			if is := dp(gtx, 13); x+is < right {
				at(gtx, image.Pt(x, y2+(lh-is)/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "threads", 13, meta)
				})
				x += is
			}
			put(" " + nsSearch.T("Reply in a thread"))
		}
		bullet()
		put(r.when)
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}

// ---- Slack boundaries (SearchResult, ConnectedBaseAvatar, classes) ----

var (
	srSuggClass     = slackc.Class("c-search_autocomplete__suggestion_item", "role", "option")
	srSuggLeftClass = slackc.Class("c-search_autocomplete__suggestion_item_left")
)

// srDataType is a suggestion row's data-type attribute.
func srDataType(r *srRow) string {
	switch {
	case r.mpim:
		return "mpim"
	case r.kind == srRConv && r.isAvatar:
		return "im"
	case r.kind == srRConv, r.kind == srRChan:
		return "channel"
	case r.kind == srRUser:
		return "member"
	case r.kind == srRPlace:
		return "view"
	case r.kind == srRQuery:
		return "recent_search"
	case r.kind == srRShow:
		return "queryUser" // app.slack.com's "Show results for:" row
	case r.kind == srRFile && r.icon == "canvas":
		return "quip"
	case r.kind == srRFile:
		return "file"
	}
	return ""
}

// srMsgArgs carry a popover message card through SearchResult.
type srMsgArgs struct {
	p  *SearchPopover
	th *theme.Theme
	cl *Colors
	i  int
	sz image.Point
}

func (a srMsgArgs) props() slackc.Props {
	r := &a.p.rows[a.i]
	return srResultProps(a.p.h, r.msg, nil, r.avatar, 36)
}

func (a srMsgArgs) layout(gtx layout.Context, p slackc.Props) layout.Dimensions {
	r := &a.p.rows[a.i]
	name, av := r.name, r.avatar
	if n, u, ok := srAuthorOver(a.p.h, r.msg, p); ok {
		name, av = n, u
	}
	return a.p.msgRow(gtx, a.th, a.cl, a.i, a.sz, name, av, p)
}

// srResultProps are SearchResult's props: the message's fields at the top
// level (ShowSendingBot treats the whole props object as the message),
// channel {id}, and the avatar as a ConnectedBaseAvatar slot. over is a
// rewritten message from MessageListItem (nil: m's own fields); av the
// avatar URL drawn when the slot's userId is unchanged.
func srResultProps(h Host, m *core.Message, over slackc.Props, av string, size float32) slackc.Props {
	src := over
	if src == nil {
		src = slackMessage(m)
	}
	if _, u, ok := srAuthorOver(h, m, src); ok {
		av = u
	}
	p := make(slackc.Props, len(src)+4)
	for k, v := range src {
		p[k] = v
	}
	p["$key"] = m.Channel + "/" + m.TS
	p["channel"] = slackc.Props{"id": m.Channel}
	p["avatar"] = slackc.Child("ConnectedBaseAvatar", srAvatarProps(m, src, size), func(gtx layout.Context, ap slackc.Props) layout.Dimensions {
		return srAvatarNative(gtx, h, m, av, size, ap)
	})
	return p
}

// srAuthorOver resolves the sender when a plugin rewrote the message's
// user or bot (ShowRealUser shows a relay bot's real poster); ok is false
// when src leaves them as they are.
func srAuthorOver(h Host, m *core.Message, src slackc.Props) (name, av string, ok bool) {
	if src == nil || m == nil {
		return "", "", false
	}
	user, bot := src.StrOr("user", ""), src.StrOr("bot_id", "")
	if user == m.User && bot == m.BotID {
		return "", "", false
	}
	mm := core.Message{User: user, BotID: bot}
	if bot != "" && bot == m.BotID {
		mm.Name, mm.Avatar = m.Name, m.Avatar
	}
	name, av = srAuthor(h, &mm)
	return name, av, true
}

func srAvatarProps(m *core.Message, src slackc.Props, size float32) slackc.Props {
	p := slackc.Props{
		"size":          size,
		"className":     "c-search_message__avatar",
		"isInteractive": true,
		"messageTs":     m.TS,
	}
	if u := src.StrOr("user", ""); u != "" {
		p["userId"] = u
	}
	if b := src.StrOr("bot_id", ""); b != "" {
		p["botId"] = b
	}
	return p
}

// srAvatar draws a search result's avatar: the slot a patched SearchResult
// handed back, else ConnectedBaseAvatar (when patched) or the image.
func srAvatar(gtx layout.Context, h Host, m *core.Message, url string, size float32, rp slackc.Props) layout.Dimensions {
	if e, ok := rp.Elem("avatar"); ok && len(e.Nodes) > 0 {
		return slackc.LayoutElement(gtx, e)
	}
	if m == nil || !slackc.Patched("ConnectedBaseAvatar") {
		return avatar(gtx, h, url, size)
	}
	return slackc.Render(gtx, "ConnectedBaseAvatar", srAvatarProps(m, slackMessage(m), size), func(gtx layout.Context, ap slackc.Props) layout.Dimensions {
		return srAvatarNative(gtx, h, m, url, size, ap)
	})
}

// srAvatarNative honours a userId override.
func srAvatarNative(gtx layout.Context, h Host, m *core.Message, url string, size float32, ap slackc.Props) layout.Dimensions {
	if u := ap.StrOr("userId", ""); u != "" && m != nil && u != m.User {
		if uu, ok := lookupUser(h, u); ok && uu.Avatar != "" {
			url = uu.Avatar
		}
	}
	if s, ok := ap.Num("size"); ok && s > 0 {
		size = float32(s)
	}
	return avatar(gtx, h, url, size)
}

// ---- shared helpers (popover and results view) ----

// srBody is a message's plain text.
func srBody(m *core.Message) string {
	if s := richtext.PlainText(m.Doc); strings.TrimSpace(s) != "" {
		return s
	}
	return m.Text
}

// srAuthor returns a message's display name and avatar.
func srAuthor(h Host, m *core.Message) (string, string) {
	name, av := m.Name, m.Avatar
	if u, ok := lookupUser(h, m.User); ok {
		if name == "" {
			name = userName(u)
		}
		if av == "" {
			av = u.Avatar
		}
	}
	if name == "" {
		name = m.User
		if name == "" {
			name = m.BotID
		}
	}
	return name, av
}

// srWhere is a result's location label: "Thread in" + icon + name. Group
// DMs show a facepile of the other members before their names.
type srWhere struct {
	pre, icon, name string
	members         []string
}

func srWhereOf(h Host, refs map[string]core.ConvRef, m *core.Message) srWhere {
	ref, ok := refs[m.Channel]
	if !ok && h.Core() != nil {
		ref = h.Core().ConvRef(m.Channel)
	}
	var w srWhere
	switch ref.Kind {
	case "im":
		if u, ok := lookupUser(h, ref.User); ok {
			w.name = userName(u)
		} else {
			w.name = nsSearch.T("Direct message")
		}
	case "mpim":
		group := nsSidebar.T("Group message")
		w.name = group
		raw := ref.Name
		if a := h.Core(); a != nil {
			if c, ok := a.Conv(m.Channel); ok {
				w.members = mpimOthers(h, c.Members)
				if t := convTitle(h, c); t != c.Name {
					w.name = t
				}
				if raw == "" {
					raw = c.Name
				}
			}
		}
		if w.name == group {
			self := ""
			if a := h.Core(); a != nil {
				self = a.Self().Handle
			}
			if hs := srMpdmNames(raw, self); len(hs) > 0 {
				w.name = strings.Join(hs, ", ")
			}
		}
	default:
		w.icon = convIcon(ref.Kind)
		w.name = ref.Name
		if w.name == "" {
			w.name = m.Channel
		}
	}
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		w.pre = nsSearch.T("Thread in ")
	}
	return w
}

// srDrawWhere draws a location label at (x, y) in a band of height h,
// clipped at right. bg is the background behind it (facepile rings).
func srDrawWhere(gtx layout.Context, hst Host, w srWhere, x, y, right, h int, bg color.NRGBA) {
	srDrawWhereFg(gtx, hst, w, x, y, right, h, bg, colorsOf(hst.Theme()).Fg3)
}

// srDrawWhereFg is srDrawWhere in fg (a picked card's white).
func srDrawWhereFg(gtx layout.Context, hst Host, w srWhere, x, y, right, h int, bg, fg color.NRGBA) {
	th := hst.Theme()
	cl := colorsOf(th)
	cl.Fg3 = fg
	if w.pre != "" && x < right {
		d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(right-x, h)}
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg3, w.pre)
			})
		})
		x += d.Size.X
	}
	if w.icon != "" && x+dp(gtx, 13) < right {
		is := dp(gtx, 12)
		at(gtx, image.Pt(x, y+(h-is)/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, w.icon, 12, cl.Fg3)
		})
		x += is + dp(gtx, 2)
	}
	if len(w.members) > 0 && x+dp(gtx, 20) < right {
		fs := float32(16)
		d := at(gtx, image.Pt(x+dp(gtx, 2), y+(h-dp(gtx, fs))/2), func(gtx layout.Context) layout.Dimensions {
			return facepile(gtx, hst, w.members, fs, 3, bg)
		})
		x += dp(gtx, 2) + d.Size.X + dp(gtx, 6)
	}
	if x < right {
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(right-x, h)}
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg3, w.name)
			})
		})
	}
}

// srShowResultsFor is the "Show results for:" row's lead-in (the
// typeahead's, rs63), with the space before the bold query.
func srShowResultsFor() string {
	t := nsSearch.T("Show results for:")
	if !strings.HasSuffix(t, " ") {
		t += " "
	}
	return t
}
