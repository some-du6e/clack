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
// chips + "Select", right "Give feedback" link.

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
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
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

	// Slick boundaries (slick_search.go)
	full     []srRow         // every row; rows are full minus plugin-hidden ones
	vis      []srRow         // backing array for filtered rows
	hide     map[string]bool // row keys a plugin removed from the suggestions
	sug      srSugState
	inputPP  slackc.Props // SearchInputBox props a plugin passed (onEnter)
	clearReq bool         // BaseSearch.clearSearchInput
	advReq   bool         // BaseSearch.openAdvancedSearchModal
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
		p.ed.SetText(query)
		n := len([]rune(query))
		p.ed.SetCaret(n, n)
		p.query = query
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
		if cv.IsArchived {
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
	q := strings.TrimSpace(p.query)
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

func (p *SearchPopover) setSel(i int) {
	p.sel = i
	p.lastKey = ""
	if i >= 0 && i < len(p.rows) {
		p.lastKey = p.rows[i].key()
	}
}

// srUnreadsShown is how many unreads show before "Show N more".
const srUnreadsShown = 5

// emptyRows is Slack's blank slate (renderBlankSlateSuggestions):
// "Unreads & drafts" (with Show N more / Show fewer), a divider, then
// "Recent places": the last five places you went (conversations and
// pages), without the one you're on. The clock button switches the list
// to Recent searches.
func (p *SearchPopover) emptyRows(rows []srRow) []srRow {
	if p.histMode {
		_, queries := srRecents()
		if len(queries) == 0 {
			return append(rows, srRow{kind: srRNote, title: "Your recent searches will show up here."})
		}
		rows = append(rows, srRow{kind: srRHeader, title: "Recent searches"})
		for _, q := range queries {
			rows = append(rows, srRow{kind: srRQuery, id: q, title: q, icon: "clock"})
		}
		return rows
	}
	unreads := p.unreads
	if len(unreads) > 24 {
		unreads = unreads[:24] // suggestionsForBlankSlateUnreads' cap
	}
	if len(unreads) > 0 {
		rows = append(rows, srRow{kind: srRHeader, title: "Unreads & drafts"})
		for i, id := range unreads {
			if i == srUnreadsShown && !p.unreadsOpen {
				break
			}
			rows = append(rows, p.candRow(p.cands[p.byConv[id]], false))
		}
		if more := len(unreads) - srUnreadsShown; more > 0 {
			t := "Show " + strconv.Itoa(more) + " more"
			if p.unreadsOpen {
				t = "Show fewer"
			}
			rows = append(rows, srRow{kind: srRMore, id: "unreads-toggle", title: t})
		}
	}
	places, _ := srRecents()
	cur := srCurrentPlace()
	var recent []srRow
	for _, id := range places {
		if len(recent) == 5 {
			break
		}
		if id == cur {
			continue
		}
		if pl, ok := srPlaceOf(id); ok {
			recent = append(recent, srRow{kind: srRPlace, id: strings.TrimPrefix(id, "view:"), title: pl.title, icon: pl.icon, boxed: true})
			continue
		}
		if i, ok := p.byConv[id]; ok {
			recent = append(recent, p.candRow(p.cands[i], false))
			continue
		}
		if c := p.h.Core(); c != nil && p.snapOK {
			ref := c.ConvRef(id)
			if ref.Name != "" && ref.Kind != "im" && ref.Kind != "mpim" {
				recent = append(recent, srRow{kind: srRConv, id: id, title: ref.Name, icon: convIcon(ref.Kind)})
			}
		}
	}
	if len(recent) > 0 {
		if len(unreads) > 0 {
			rows = append(rows, srRow{kind: srRDivider})
		}
		rows = append(rows, srRow{kind: srRHeader, title: "Recent places"})
		rows = append(rows, recent...)
	}
	return rows
}

func (p *SearchPopover) typingRows(rows []srRow, q string, now time.Time) []srRow {
	local := srRank(p.cands, q, 8, false)
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
		remote = srRank(rc, q, 8, p.rUQ == q)
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
		remote = append(remote, srRank(rc, q, 8, p.rCQ == q)...)
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
	sug := srMerge(local, remote, 5, 2) // Slack: top hit, "Show results", then 4
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
		rows = append(rows, srRow{kind: srRDivider}, srRow{kind: srRHeader, title: "Recent messages"})
		terms := srParse(q).Highlights()
		for i := range p.msgs {
			if i == 3 {
				break
			}
			m := &p.msgs[i]
			name, av := srAuthor(p.h, m)
			rows = append(rows, srRow{kind: srRMsg, id: m.Channel + "/" + m.TS, msg: m, name: name, avatar: av,
				where: srWhereOf(p.h, p.msgConvs, m), when: srShortDay(tsTime(m.TS), now),
				segs: srHighlight(srFlatten(srBody(m)), terms)})
		}
	}
	return rows
}

// ---- async ----

func (p *SearchPopover) onQuery(gtx layout.Context) {
	p.seq++
	p.changed = gtx.Now
	p.dirFired, p.msgFired = false, false
	q := strings.TrimSpace(p.query)
	if q == "" {
		p.rUsers, p.rChans, p.rUQ, p.rCQ = nil, nil, "", ""
		p.msgs, p.msgConvs, p.msgQ = nil, nil, ""
	} else if p.msgQ != "" && !strings.HasPrefix(strings.ToLower(q), strings.ToLower(p.msgQ)) {
		p.msgs, p.msgConvs, p.msgQ = nil, nil, ""
	}
	if q == "" || (p.fileQ != "" && !strings.HasPrefix(strings.ToLower(q), strings.ToLower(p.fileQ))) {
		p.files, p.fileQ = nil, ""
	}
	p.rebuild(gtx.Now, true)
}

func (p *SearchPopover) tick(gtx layout.Context) {
	q := strings.TrimSpace(p.query)
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
			run(p.h, func(ctx context.Context) ([]core.User, error) { return c.OmniSearchUsers(ctx, q, topU) },
				func(us []core.User, err error) {
					if p.done || seq != p.seq || err != nil {
						return
					}
					p.rUsers, p.rUQ = us, q
					p.rebuild(time.Now(), false)
				})
			run(p.h, func(ctx context.Context) ([]core.ChannelHit, error) { return c.OmniSearchChannels(ctx, q, topC) },
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
			sess := p.sess
			recent := srRecentChannels()
			run(p.h, func(ctx context.Context) (core.SearchResult, error) {
				res, err := c.SearchInline(ctx, core.InlineOpts{Query: q, RecentChannels: recent, Session: sess})
				if err == nil {
					res.Messages = tautSearchInline(ctx, p.h, q, res.Messages) // free without plugins
				}
				return res, err
			}, func(res core.SearchResult, err error) {
				if p.done || seq != p.seq || err != nil {
					return
				}
				p.msgs, p.msgConvs, p.msgQ = res.Messages, res.Convs, q
				p.rebuild(time.Now(), false)
			})
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
	q, sess := strings.TrimSpace(p.query), p.sess
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
		p.submit(p.query)
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
}

func (p *SearchPopover) keys(gtx layout.Context) {
	for {
		e, ok := gtx.Event(
			key.Filter{Focus: &p.ed, Name: key.NameUpArrow},
			key.Filter{Focus: &p.ed, Name: key.NameDownArrow},
			key.Filter{Focus: &p.ed, Name: key.NameReturn},
			key.Filter{Focus: &p.ed, Name: key.NameEnter},
			key.Filter{Focus: &p.ed, Name: key.NameEscape},
		)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameUpArrow:
			p.move(-1)
		case key.NameDownArrow:
			p.move(1)
		case key.NameReturn, key.NameEnter:
			p.enter()
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
				p.query = t
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
		p.query = ""
		p.refocus = true
		p.onQuery(gtx)
	}
	if p.filter.Clicked(gtx) || p.advReq {
		q := strings.TrimSpace(p.query)
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
	for i := range p.rows {
		if p.clicks[i].Clicked(gtx) {
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
	inputH := dp(gtx, 48) + 1
	padT, padB := dp(gtx, 8), dp(gtx, 8)
	footH := dp(gtx, 45)
	content := 0
	for i := range p.rows {
		content += dp(gtx, p.rows[i].heightDp())
	}
	h := inputH + padT + content + padB + footH
	if maxH := win.Y - y - dp(gtx, 8); h > maxH {
		h = maxH
	}
	listH := h - inputH - padT - padB - footH
	if listH < 0 {
		listH = 0
	}
	p.rect = image.Rect(x, y, x+w, y+h)
	rad := dp(gtx, 8)
	card(gtx, cl, p.rect, rad)
	p.block.Layout(gtx, p.rect)
	cs := clipRRect(gtx, p.rect, rad)
	defer cs.Pop()
	defer op.Offset(p.rect.Min).Push(gtx.Ops).Pop()

	p.layoutInput(gtx, th, cl, w, inputH)

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
	return layout.Dimensions{Size: win}
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
	fill(gtx, image.Rect(0, rh, w, rh+1), srLineColor(th, cl))
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
	at(gtx, image.Pt(w-dp(gtx, 68), (rh-hb)/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &p.filter, "filter", 24, 18, cl.Fg2, cl.BgHover, 4)
	})
	right := w - dp(gtx, 80)
	if p.ed.Len() > 0 {
		fill(gtx, image.Rect(right, (rh-dp(gtx, 20))/2, right+1, (rh+dp(gtx, 20))/2), cl.Divider)
		cw := textW(gtx, th, 13, theme.Regular, "Clear")
		cx := right - dp(gtx, 17) - cw
		at(gtx, image.Pt(cx, 0), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &p.clear, nil, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg2, "Clear")
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
			if p.ed.Len() == 0 && p.ghostText() == "" {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Point{}
					return text(gtx, th, 15, theme.Regular, cl.Placeholder, "Search across people, channels, files, workflows, and more")
				})
				call.Add(gtx.Ops)
			}
			if ghost := p.ghostText(); ghost != "" && gtx.Focused(&p.ed) {
				qw := textW(gtx, th, 15, theme.Regular, p.query)
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
			return th.Editor(gtx, &p.ed, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
	})
	r := image.Rect(tx, 0, tx+tw, rh)
	st := clip.Rect(r).Push(gtx.Ops)
	pointer.CursorText.Add(gtx.Ops)
	st.Pop()
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
			return r.title + " — Search"
		case nav && r.title != "":
			return r.title + " — Open"
		}
		return ""
	}
	switch {
	case r.kind == srRQuery, r.kind == srRShow:
		return " — Search"
	case !nav:
		return ""
	}
	name, q := []rune(r.title), []rune(p.query)
	if len(q) > len(name) || !strings.EqualFold(string(name[:len(q)]), string(q)) {
		return ""
	}
	return string(name[len(q):]) + " — Open"
}

func (p *SearchPopover) footer(gtx layout.Context, th *theme.Theme, cl *Colors, sz image.Point) layout.Dimensions {
	ch := dp(gtx, 20)
	cy := (sz.Y - ch) / 2
	x := dp(gtx, 16)
	if p.picked {
		// a row is selected: [Enter] Open (Slack adds [Tab] Actions, the
		// row's action toolbar, which Clack doesn't have)
		ew := textW(gtx, th, 13, theme.Regular, "Enter") + dp(gtx, 12)
		rrect(gtx, image.Rect(x, cy, x+ew, cy+ch), dp(gtx, 4), cl.KbdLight)
		at(gtx, image.Pt(x+dp(gtx, 6), cy), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg, "Enter")
			})
		})
		x += ew + dp(gtx, 6)
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg2, "Open")
			})
		})
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
			return text(gtx, th, 13, theme.Regular, cl.Fg2, "Select")
		})
	})
	p.feedbackLink(gtx, th, cl, sz)
	return layout.Dimensions{Size: sz}
}

// feedbackLink is the footer's FeedbackLink (experimentOmniPolish: just
// "Give feedback").
func (p *SearchPopover) feedbackLink(gtx layout.Context, th *theme.Theme, cl *Colors, sz image.Point) {
	fw := textW(gtx, th, 13, theme.Regular, "Give feedback")
	at(gtx, image.Pt(sz.X-dp(gtx, 16)-fw, 0), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &p.feedback, nil, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Link, "Give feedback")
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
		// right side
		right := sz.X - dp(gtx, 16)
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
					stroke(gtx, image.Rect(0, 0, dd, dd), dd/2, maxInt(1, dp(gtx, 1)), fg)
					return layout.Dimensions{Size: image.Pt(dd, dd)}
				}
				return presenceDot(gtx, th, r.presence == "active", 7)
			})
		}
	case r.boxed:
		rb := image.Rect(lx, ly, lx+box, ly+box)
		stroke(gtx, rb, dp(gtx, 6), maxInt(1, dp(gtx, 1)), cl.BtnBorder)
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
		ps[0] = piece{"Show results for: ", theme.Regular}
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
func srEnterChip(gtx layout.Context, th *theme.Theme, cl *Colors, right, h int) int {
	tw := textW(gtx, th, 13, theme.Regular, "Enter")
	w, ch := tw+dp(gtx, 12), dp(gtx, 20)
	r := image.Rect(right-w, (h-ch)/2, right, (h+ch)/2)
	rrect(gtx, r, dp(gtx, 4), cl.Ring)
	rrect(gtx, r.Inset(1), dp(gtx, 4)-1, cl.BgAlt)
	at(gtx, image.Pt(r.Min.X+dp(gtx, 6), r.Min.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Regular, cl.Fg, "Enter")
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
	sel := i == p.sel && p.picked
	gtx.Constraints = layout.Exact(sz)
	return p.clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		cr := image.Rect(dp(gtx, 16), dp(gtx, 6), sz.X-dp(gtx, 16), sz.Y-dp(gtx, 6))
		rad := dp(gtx, 8)
		bg := cl.Bg
		if sel {
			bg = cl.Selected
		} else if p.clicks[i].Hovered() {
			bg = cl.BgHover
		}
		rrect(gtx, cr, rad, cl.Ring)
		rrect(gtx, cr.Inset(1), rad-1, bg)
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
					return text(gtx, th, 13, theme.Regular, cl.Fg3, r.when)
				})
			})
			right -= ww + dp(gtx, 12)
		}
		x := tx
		d := at(gtx, image.Pt(x, ly), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, right-x), lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Black, cl.Fg, name)
			})
		})
		x += d.Size.X + dp(gtx, 6)
		srDrawWhere(gtx, p.h, r.where, x, ly, right, lh, bg)
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
			srDrawToks(gtx, th, cl, r.toks, dp(gtx, 22), cl.Fg)
			return layout.Dimensions{}
		})
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
			w.name = "Direct message"
		}
	case "mpim":
		w.name = "Group message"
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
		if w.name == "Group message" {
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
		w.pre = "Thread in "
	}
	return w
}

// srDrawWhere draws a location label at (x, y) in a band of height h,
// clipped at right. bg is the background behind it (facepile rings).
func srDrawWhere(gtx layout.Context, hst Host, w srWhere, x, y, right, h int, bg color.NRGBA) {
	th := hst.Theme()
	cl := colorsOf(th)
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
