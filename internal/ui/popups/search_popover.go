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
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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

func (r *srRow) selectable() bool { return r.kind != srRHeader && r.kind != srRDivider }

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
	sel     int
	hover   int
	ensure  bool
	query   string
	lastKey string

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
}

// NewQuickSwitcher opens the Ctrl+K switcher: empty query, centered at the
// top of the window; Enter on the top suggestion jumps there.
func NewQuickSwitcher(h Host) *SearchPopover {
	return NewSearchPopover(h, "", image.Rectangle{})
}

// NewSearchPopover opens the popover over the top-nav search pill (pill is
// the pill's rect in window coords; empty = centered at the top).
func NewSearchPopover(h Host, query string, pill image.Rectangle) *SearchPopover {
	p := &SearchPopover{h: h, pill: pill, sel: -1, hover: -1}
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
			if cd.Name == cv.Name {
				// members unknown to the user cache: use the handles in
				// the internal name ("mpdm-a--b-1")
				if hs := srMpdmNames(cv.Name, snap.Self.Handle); len(hs) > 0 {
					cd.Name = strings.Join(hs, ", ")
				}
			}
			cd.Members = cv.Members
			// members' real names and handles also find the group DM
			for _, m := range cv.Members {
				if m == snap.Self.ID {
					continue
				}
				if u, ok := user(m); ok {
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
	rows := p.rows[:0]
	if q == "" {
		rows = p.emptyRows(rows)
	} else {
		rows = p.typingRows(rows, q, now)
	}
	p.rows = rows
	for len(p.clicks) < len(p.rows) {
		p.clicks = append(p.clicks, widget.Clickable{})
	}
	sel := -1
	if !resetSel && p.lastKey != "" {
		for i := range p.rows {
			if p.rows[i].key() == p.lastKey {
				sel = i
				break
			}
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
	if resetSel {
		p.list.Position = layout.Position{}
	}
	p.setSel(sel)
}

func (p *SearchPopover) setSel(i int) {
	p.sel = i
	p.lastKey = ""
	if i >= 0 && i < len(p.rows) {
		p.lastKey = p.rows[i].key()
	}
}

func (p *SearchPopover) emptyRows(rows []srRow) []srRow {
	if len(p.unreads) > 0 {
		rows = append(rows, srRow{kind: srRHeader, title: "Unreads & drafts"})
		for i, id := range p.unreads {
			if i == 5 {
				break
			}
			rows = append(rows, p.candRow(p.cands[p.byConv[id]], false))
		}
		rows = append(rows, srRow{kind: srRDivider})
	}
	rows = append(rows, srRow{kind: srRHeader, title: "Recent places"})
	convs, queries := srRecents()
	n := 0
	for _, id := range convs {
		if n == 5 {
			break
		}
		if i, ok := p.byConv[id]; ok {
			rows = append(rows, p.candRow(p.cands[i], false))
			n++
			continue
		}
		if c := p.h.Core(); c != nil && p.snapOK {
			ref := c.ConvRef(id)
			if ref.Name != "" && ref.Kind != "im" && ref.Kind != "mpim" {
				rows = append(rows, srRow{kind: srRConv, id: id, title: ref.Name, icon: convIcon(ref.Kind)})
				n++
			}
		}
	}
	for _, s := range [...]struct{ title, icon, route string }{
		{"Saved", "bookmark", "later"},
		{"Threads", "threads", "threads"},
		{"Activity", "activity", "activity"},
	} {
		rows = append(rows, srRow{kind: srRPlace, id: s.route, title: s.title, icon: s.icon, boxed: true})
	}
	if len(queries) > 0 {
		rows = append(rows, srRow{kind: srRDivider}, srRow{kind: srRHeader, title: "Recent searches"})
		for i, q := range queries {
			if i == 5 {
				break
			}
			rows = append(rows, srRow{kind: srRQuery, id: q, title: q, icon: "clock"})
		}
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
		terms := srParse(q).Terms
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
			run(p.h, func(ctx context.Context) ([]core.User, error) { return c.SearchUsers(ctx, q, 8) },
				func(us []core.User, err error) {
					if p.done || seq != p.seq || err != nil {
						return
					}
					p.rUsers, p.rUQ = us, q
					p.rebuild(time.Now(), false)
				})
			run(p.h, func(ctx context.Context) ([]core.ChannelHit, error) { return c.SearchChannels(ctx, q, 8) },
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
			run(p.h, func(ctx context.Context) (core.SearchResult, error) {
				return c.SearchMessages(ctx, core.SearchOpts{Query: q, Count: 3, Sort: "timestamp"})
			}, func(res core.SearchResult, err error) {
				if p.done || seq != p.seq || err != nil {
					return
				}
				p.msgs, p.msgConvs, p.msgQ = res.Messages, res.Convs, q
				p.rebuild(time.Now(), false)
			})
		}
	}
}

// ---- actions ----

func (p *SearchPopover) open(route, conv string) {
	RecordRecentConversation(conv)
	p.Dismiss()
	p.h.Navigate(route)
}

func (p *SearchPopover) submit(q string) {
	q = strings.TrimSpace(q)
	srRecordQuery(q)
	p.Dismiss()
	p.h.Navigate("search?q=" + url.QueryEscape(q))
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
	}
}

func (p *SearchPopover) move(d int) {
	n := len(p.rows)
	if n == 0 {
		return
	}
	i := p.sel
	for k := 0; k < n; k++ {
		if i < 0 {
			i = 0
			if d < 0 {
				i = n - 1
			}
		} else {
			i = (i + d + n) % n
		}
		if p.rows[i].selectable() {
			p.setSel(i)
			p.ensure = true
			return
		}
	}
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
			p.activate(p.sel)
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
				p.onQuery(gtx)
			}
		case widget.SubmitEvent:
			p.activate(p.sel)
		}
		if p.done {
			return layout.Dimensions{}
		}
	}
	// header buttons (last frame's clicks)
	if p.clear.Clicked(gtx) {
		p.ed.SetText("")
		p.query = ""
		p.refocus = true
		p.onQuery(gtx)
	}
	if p.filter.Clicked(gtx) {
		q := strings.TrimSpace(p.query)
		srRecordQuery(q)
		p.Dismiss()
		p.h.Navigate("search?q=" + url.QueryEscape(q))
		return layout.Dimensions{}
	}
	if p.history.Clicked(gtx) {
		p.openHistory()
	}
	if p.feedback.Clicked(gtx) {
		p.h.Toast("Feedback isn't supported yet")
	}
	for i := range p.rows {
		if p.clicks[i].Clicked(gtx) {
			p.activate(i)
			if p.done {
				return layout.Dimensions{}
			}
		}
	}
	hov := -1
	for i := range p.rows {
		if p.rows[i].selectable() && p.clicks[i].Hovered() {
			hov = i
		}
	}
	if hov != p.hover {
		if hov >= 0 {
			p.setSel(hov)
		}
		p.hover = hov
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

	p.inputRow(gtx, th, cl, w)

	// list
	if p.ensure {
		p.ensure = false
		p.scrollToSel(gtx, listH)
	}
	at(gtx, image.Pt(0, inputH+padT), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, listH))
		defer clip.Rect{Max: image.Pt(w, listH)}.Push(gtx.Ops).Pop()
		return p.list.Layout(gtx, len(p.rows), func(gtx layout.Context, i int) layout.Dimensions {
			return p.row(gtx, th, cl, i, w)
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
			if p.ed.Len() == 0 {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Point{}
					return text(gtx, th, 15, theme.Regular, cl.Placeholder, "Search across people, channels, files, workflows, and more")
				})
				call.Add(gtx.Ops)
			} else if p.sel >= 0 && p.sel < len(p.rows) && gtx.Focused(&p.ed) {
				switch p.rows[p.sel].kind {
				case srRConv, srRUser, srRChan:
					qw := textW(gtx, th, 15, theme.Regular, p.query)
					hint := "— Open"
					if hw := textW(gtx, th, 15, theme.Regular, hint); qw+dp(gtx, 4)+hw <= tw {
						call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min = image.Point{}
							return text(gtx, th, 15, theme.Regular, cl.Fg3, hint)
						})
						t := op.Offset(image.Pt(qw+dp(gtx, 4), 0)).Push(gtx.Ops)
						call.Add(gtx.Ops)
						t.Pop()
					}
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

func (p *SearchPopover) openHistory() {
	_, qs := srRecents()
	if len(qs) == 0 {
		p.h.Toast("No recent searches yet")
		return
	}
	items := []MenuItem{{Header: "Recent searches"}}
	for _, q := range qs {
		items = append(items, MenuItem{Label: q, Icon: "clock", Do: func() { p.submit(q) }})
	}
	p.h.ShowOverlay(NewMenuOpts(p.h, items, MenuOpts{Anchor: p.historyRect, AlignRight: true}))
}

func (p *SearchPopover) footer(gtx layout.Context, th *theme.Theme, cl *Colors, sz image.Point) layout.Dimensions {
	ch := dp(gtx, 20)
	cy := (sz.Y - ch) / 2
	x := dp(gtx, 16)
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
	fw := textW(gtx, th, 13, theme.Regular, "Give feedback")
	at(gtx, image.Pt(sz.X-dp(gtx, 16)-fw, 0), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &p.feedback, nil, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Link, "Give feedback")
			})
		})
	})
	return layout.Dimensions{Size: sz}
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
		return p.msgRow(gtx, th, cl, i, sz)
	}
	sel := i == p.sel
	gtx.Constraints = layout.Exact(sz)
	return p.clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		if sel {
			fill(gtx, image.Rectangle{Max: sz}, cl.Selected)
		}
		box := dp(gtx, 24)
		lx, ly := dp(gtx, 16), (sz.Y-box)/2
		switch {
		case r.mpim:
			rowBg := cl.Bg
			if sel {
				rowBg = cl.Selected
			}
			at(gtx, image.Pt(lx, ly), func(gtx layout.Context) layout.Dimensions {
				return mpimTile(gtx, p.h, r.members, 24, rowBg, sel)
			})
		case r.isAvatar:
			at(gtx, image.Pt(lx, ly), func(gtx layout.Context) layout.Dimensions {
				return avatar(gtx, p.h, r.avatar, 24)
			})
			if r.presence != "" {
				d := dp(gtx, 10)
				c := image.Pt(lx+box-d/2+dp(gtx, 1), ly+box-d/2+dp(gtx, 1))
				bg := cl.Bg
				if sel {
					bg = cl.Selected
				}
				theme.Circle(gtx, c, d, bg)
				dd := dp(gtx, 7)
				at(gtx, c.Sub(image.Pt(dd/2, dd/2)), func(gtx layout.Context) layout.Dimensions {
					return presenceDot(gtx, th, r.presence == "active", 7)
				})
			}
		case r.boxed:
			rb := image.Rect(lx, ly, lx+box, ly+box)
			stroke(gtx, rb, dp(gtx, 6), maxInt(1, dp(gtx, 1)), cl.BtnBorder)
			is := dp(gtx, 16)
			at(gtx, image.Pt(lx+(box-is)/2, ly+(box-is)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, r.icon, 16, cl.Fg)
			})
		case r.icon != "":
			is := dp(gtx, 20)
			at(gtx, image.Pt(lx+(box-is)/2, ly+(box-is)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, r.icon, 20, cl.Fg2)
			})
		}
		// right side
		right := sz.X - dp(gtx, 16)
		if sel {
			right -= srEnterChip(gtx, th, cl, right, sz.Y)
			right -= dp(gtx, 20)
		}
		if r.badge > 0 {
			right -= srBadge(gtx, th, cl, r.badge, right, sz.Y)
			right -= dp(gtx, 12)
		}
		x := dp(gtx, 52)
		maxX := right
		x = p.drawTitle(gtx, th, cl, r, x, maxX, sz.Y)
		if r.alt != "" && x < maxX {
			s := " · " + r.alt
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxX-x, sz.Y)}
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg3, s)
				})
			})
		}
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}

// drawTitle draws the row's primary text (match in bold) and returns the x
// after it.
func (p *SearchPopover) drawTitle(gtx layout.Context, th *theme.Theme, cl *Colors, r *srRow, x, maxX, h int) int {
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
				return text(gtx, th, 15, pc.wt, cl.Fg, pc.s)
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

func (p *SearchPopover) msgRow(gtx layout.Context, th *theme.Theme, cl *Colors, i int, sz image.Point) layout.Dimensions {
	r := &p.rows[i]
	sel := i == p.sel
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
			return avatar(gtx, p.h, r.avatar, 36)
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
				return text(gtx, th, 15, theme.Black, cl.Fg, r.name)
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
