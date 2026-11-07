package popups

// Search results page (primary pane for route "search?q=…"). Measured from
// app.slack.com (2026-11): content inset ~107px each side on a wide pane
// (24px minimum here); title "Results for: **query**" 18px (query 900);
// filter pills row at y+56: 28 tall, radius 8, 13/700, 1px border
// rgba(94,93,96,.13), icon 16 + label + chevron, gap 8, a 1px divider
// after "In". "Sorted by **Most relevant (default)** ▾" 13px (value in
// link blue), "N results" 13px #5e5d60 under it, "Give feedback" right.
// Result cards: 1px rgba(94,93,96,.13) border, radius 12, 8px apart,
// padding 16 12 12 16, avatar 36 radius 8, name 15/900 + "# channel" or
// "Thread in # channel" 13px #5e5d60, time right 13px, body 15px on one
// line ("… Show more" when longer), term highlight rgba(255,198,0,.18),
// hover bg #f8f8f8. Hovering a card floats Slack's action bar at its
// top-right corner (search_hover.png): 104x40, white, radius 12, 1px ring
// rgba(29,28,29,.13) + 0 1px 3px rgba(0,0,0,.08), padding 4, three 32px
// buttons (radius 8, 18px #616061 icons, hover #f8f8f8) with tooltips:
// Forward message…, Open in home, More actions (search_more.png menu).
// Search results have no react / reply buttons.

import (
	"context"
	"image"
	"image/color"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// ---- wrapped, highlighted text runs (shared with the popover) ----

// srTok is a placed word or space run.
type srTok struct {
	s    string
	hit  bool
	x, y int // y is the line index
	w    int
}

// srSplitWords splits s into word runs, space runs and "\n" tokens,
// appending to out.
func srSplitWords(s string, out []string) []string {
	start, kind := 0, -1
	for i, r := range s {
		if r == '\n' {
			if start < i {
				out = append(out, s[start:i])
			}
			out = append(out, "\n")
			start, kind = i+1, -1
			continue
		}
		k := 0
		if r == ' ' || r == '\t' || r == '\r' {
			k = 1
		}
		if kind != -1 && k != kind {
			out = append(out, s[start:i])
			start = i
		}
		kind = k
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// srWrap places segs (15px regular text) greedily in lines of width px.
// On the last allowed line it keeps reserve px free. It reports the line
// count and whether text was cut off by maxLines (0 = unlimited).
func srWrap(gtx layout.Context, th *theme.Theme, segs []srSeg, width, maxLines, reserve int) ([]srTok, int, bool) {
	if width < 1 {
		width = 1
	}
	var toks []srTok
	line, x := 0, 0
	limit := func() int {
		if maxLines > 0 && line == maxLines-1 {
			return maxInt(1, width-reserve)
		}
		return width
	}
	var words []string
	for _, sg := range segs {
		words = srSplitWords(sg.Text, words[:0])
		for _, wd := range words {
			if wd == "\n" {
				line++
				x = 0
				if maxLines > 0 && line >= maxLines {
					return toks, maxLines, true
				}
				continue
			}
			space := wd[0] == ' ' || wd[0] == '\t' || wd[0] == '\r'
			if space && x == 0 {
				continue
			}
			pw := textW(gtx, th, 15, theme.Regular, wd)
			lim := limit()
			if x+pw > lim {
				if space {
					continue
				}
				if x > 0 {
					line++
					x = 0
					if maxLines > 0 && line >= maxLines {
						return toks, maxLines, true
					}
					lim = limit()
				}
				if pw > lim {
					// a word wider than the line: clip it (ellipsized)
					toks = append(toks, srTok{s: wd, hit: sg.Hit, y: line, w: lim})
					x = lim
					if maxLines > 0 && line == maxLines-1 {
						return toks, maxLines, true
					}
					continue
				}
			}
			toks = append(toks, srTok{s: wd, hit: sg.Hit, x: x, y: line, w: pw})
			x += pw
		}
	}
	return toks, line + 1, false
}

// srWrapFit wraps segs; when they overflow maxLines it wraps again keeping
// reserve px free at the end of the last line (for "…" / "Show more").
func srWrapFit(gtx layout.Context, th *theme.Theme, segs []srSeg, width, maxLines, reserve int) ([]srTok, int, bool) {
	toks, n, over := srWrap(gtx, th, segs, width, maxLines, 0)
	if over && reserve > 0 {
		toks, n, over = srWrap(gtx, th, segs, width, maxLines, reserve)
	}
	return toks, n, over
}

// srEndX is the right edge of the text on line.
func srEndX(toks []srTok, line int) int {
	x := 0
	for i := range toks {
		if toks[i].y == line && toks[i].x+toks[i].w > x {
			x = toks[i].x + toks[i].w
		}
	}
	return x
}

// srDrawToks draws placed tokens at the origin, lines lh px apart, with
// the search highlight behind hits.
func srDrawToks(gtx layout.Context, th *theme.Theme, cl *Colors, toks []srTok, lh int, fg color.NRGBA) {
	for i := range toks {
		t := toks[i]
		y := t.y * lh
		if t.hit {
			fill(gtx, image.Rect(t.x, y+dp(gtx, 1), t.x+t.w, y+lh-dp(gtx, 1)), cl.MatchBg)
		}
		at(gtx, image.Pt(t.x, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(t.w+dp(gtx, 2), lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, fg, t.s)
			})
		})
	}
}

// ---- results view ----

// search result types (the [Messages ▾] selector)
const (
	srTabMessages = iota
	srTabFiles
	srTabChannels
	srTabPeople
)

var srTabNames = [...]string{"Messages", "Files", "Channels", "People"}
var srTabIcons = [...]string{"dms", "files", "hash", "people"}

// filter pills
const (
	srPType = iota
	srPFrom
	srPIn
	srPMe
	srPLink
	srPThread
	srPWeek
	srPMore
	srPN
)

// srCard is one message result's UI state.
type srCard struct {
	name, avatar, when string
	where              srWhere
	flat, full         []srSeg
	expanded           bool
	click, more, chip  widget.Clickable
	toks               []srTok
	tokW               int
	tokExp             bool
	over               bool
	lines              int

	// hover action bar
	bar, fwd, jump, kebab    widget.Clickable
	tipFwd, tipJump, tipMore TipState
	kebabR                   image.Rectangle // card coords, last frame
	h                        int             // laid-out height incl. the gap below
}

// srBarShadow is the action bar's 0 1px 3px rgba(0,0,0,.08) on white.
var srBarShadow = linearAlpha(rgba(0x000000, .08), rgb(0xffffff))

// SearchView is the full search results page.
type SearchView struct {
	h      Host
	q      srQuery
	tab    int
	sortBy string // score | timestamp | oldest

	origin    image.Point
	originSet bool

	list     layout.List
	pills    [srPN]widget.Clickable
	pillR    [srPN]image.Rectangle
	sortBtn  widget.Clickable
	sortR    image.Rectangle
	feedback widget.Clickable

	started bool
	seq     int
	loading bool
	err     string

	msgs  []core.Message
	cards []srCard
	convs map[string]core.ConvRef
	total int
	page  int
	pages int

	chans  []core.ChannelHit
	people []core.User
	rows   []widget.Clickable
	btns   []widget.Clickable
	listY  int
	rowH   int

	cardX    int // cards' left edge (view coords), last frame
	menuCard int // card whose More actions menu is open (-1 none)
	menuSeq  int
}

// NewSearchView builds the results page for query (as passed in the
// "search?q=" route; URL-escaped queries are unescaped).
func NewSearchView(h Host, query string) *SearchView {
	if strings.ContainsAny(query, "%+") {
		if u, err := url.QueryUnescape(query); err == nil {
			query = u
		}
	}
	query = strings.TrimSpace(query)
	srRecordQuery(query)
	v := &SearchView{h: h, q: srParse(query), sortBy: "score", menuCard: -1}
	v.list.Axis = layout.Vertical
	return v
}

// SetOrigin tells the view where its top-left corner is in window
// coordinates, so its menus and profile cards open next to the clicked
// element. Without it the view assumes Slack's layout (70, 40).
func (v *SearchView) SetOrigin(p image.Point) {
	v.origin, v.originSet = p, true
}

func (v *SearchView) win(gtx layout.Context, r image.Rectangle) image.Rectangle {
	o := v.origin
	if !v.originSet {
		o = image.Pt(dp(gtx, 70), dp(gtx, 40))
	}
	return r.Add(o)
}

// Query is the current query string (modifiers included).
func (v *SearchView) Query() string { return v.q.String() }

func (v *SearchView) setQuery(q srQuery) {
	v.q = q
	srRecordQuery(q.String())
	v.search(1)
}

func (v *SearchView) setTab(t int) {
	if t == v.tab {
		return
	}
	v.tab = t
	v.search(1)
}

func (v *SearchView) reset() {
	v.msgs, v.cards, v.convs = nil, nil, nil
	v.total, v.page, v.pages = 0, 0, 0
	v.chans, v.people = nil, nil
	v.err = ""
	v.menuCard = -1
	v.list.Position = layout.Position{}
}

// search loads page (1 = a fresh search).
func (v *SearchView) search(page int) {
	v.seq++
	seq := v.seq
	if page <= 1 {
		v.reset()
	}
	v.loading = false
	c := v.h.Core()
	if c == nil {
		return
	}
	words := strings.TrimSpace(v.q.Text())
	switch v.tab {
	case srTabMessages:
		q := v.q.String()
		if strings.TrimSpace(q) == "" {
			return
		}
		srt := v.sortBy
		v.loading = true
		run(v.h, func(ctx context.Context) (core.SearchResult, error) {
			return c.SearchMessages(ctx, core.SearchOpts{Query: q, Page: page, Count: 20, Sort: srt})
		}, func(res core.SearchResult, err error) {
			if seq != v.seq {
				return
			}
			v.loading = false
			if err != nil {
				v.err = errText(err)
				return
			}
			v.addMessages(res, page)
		})
	case srTabChannels:
		if words == "" {
			return
		}
		v.loading = true
		run(v.h, func(ctx context.Context) ([]core.ChannelHit, error) { return c.SearchChannels(ctx, words, 50) },
			func(hs []core.ChannelHit, err error) {
				if seq != v.seq {
					return
				}
				v.loading = false
				if err != nil {
					v.err = errText(err)
					return
				}
				v.chans = hs
				v.total = len(hs)
				v.growRows(len(hs))
			})
	case srTabPeople:
		if words == "" {
			return
		}
		v.loading = true
		run(v.h, func(ctx context.Context) ([]core.User, error) { return c.SearchUsers(ctx, words, 50) },
			func(us []core.User, err error) {
				if seq != v.seq {
					return
				}
				v.loading = false
				if err != nil {
					v.err = errText(err)
					return
				}
				v.people = us
				v.total = len(us)
				v.growRows(len(us))
			})
	}
}

func (v *SearchView) growRows(n int) {
	for len(v.rows) < n {
		v.rows = append(v.rows, widget.Clickable{})
	}
	for len(v.btns) < n {
		v.btns = append(v.btns, widget.Clickable{})
	}
}

func (v *SearchView) addMessages(res core.SearchResult, page int) {
	if v.convs == nil {
		v.convs = map[string]core.ConvRef{}
	}
	for k, r := range res.Convs {
		v.convs[k] = r
	}
	v.total, v.pages = res.Total, res.Pages
	v.page = res.Page
	if v.page == 0 {
		v.page = page
	}
	terms := v.q.Terms
	now := time.Now()
	for i := range res.Messages {
		m := &res.Messages[i]
		name, av := srAuthor(v.h, m)
		body := srBody(m)
		v.cards = append(v.cards, srCard{
			name: name, avatar: av, when: dayTime(tsTime(m.TS), now),
			where: srWhereOf(v.h, v.convs, m),
			flat:  srHighlight(srFlatten(body), terms),
			full:  srHighlight(strings.TrimSpace(body), terms),
		})
	}
	v.msgs = append(v.msgs, res.Messages...)
}

// ---- menus ----

func (v *SearchView) snapshot() (core.Snapshot, bool) {
	c := v.h.Core()
	if c == nil {
		return core.Snapshot{}, false
	}
	select {
	case <-c.Ready():
		return c.Snapshot(), true
	default:
		return core.Snapshot{}, false
	}
}

func (v *SearchView) openMenu(gtx layout.Context, r image.Rectangle, items []MenuItem, width float32) {
	v.h.ShowOverlay(NewMenuOpts(v.h, items, MenuOpts{Anchor: v.win(gtx, r), Width: width}))
}

func (v *SearchView) typeMenu(gtx layout.Context) {
	items := make([]MenuItem, 0, len(srTabNames))
	for i, n := range srTabNames {
		t := i
		items = append(items, MenuItem{Label: n, Icon: srTabIcons[i], Checked: v.tab == i, Do: func() { v.setTab(t) }})
	}
	v.openMenu(gtx, v.pillR[srPType], items, 220)
}

func (v *SearchView) fromMenu(gtx layout.Context) {
	cur := v.q.Get("from")
	items := []MenuItem{{Label: "Anyone", Checked: cur == "", Do: func() { v.setQuery(v.q.Set("from", "")) }}, Sep()}
	snap, ok := v.snapshot()
	if ok {
		type person struct{ id, name string }
		var ps []person
		self := snap.Self.ID
		for id, u := range snap.Users {
			if u.Deleted || id == self {
				continue
			}
			ps = append(ps, person{id, userName(u)})
		}
		sort.Slice(ps, func(i, j int) bool { return strings.ToLower(ps[i].name) < strings.ToLower(ps[j].name) })
		if len(ps) > 15 {
			ps = ps[:15]
		}
		if self != "" {
			val := "<@" + self + ">"
			items = append(items, MenuItem{Label: userName(snap.Self) + " (you)", Icon: "user",
				Checked: cur == val || strings.EqualFold(cur, "me"), Do: func() { v.setQuery(v.q.Set("from", val)) }})
		}
		for _, p := range ps {
			val := "<@" + p.id + ">"
			items = append(items, MenuItem{Label: p.name, Icon: "user", Checked: cur == val, Do: func() { v.setQuery(v.q.Set("from", val)) }})
		}
	}
	v.openMenu(gtx, v.pillR[srPFrom], items, 260)
}

func (v *SearchView) inMenu(gtx layout.Context) {
	cur := v.q.Get("in")
	items := []MenuItem{{Label: "Anywhere", Checked: cur == "", Do: func() { v.setQuery(v.q.Set("in", "")) }}, Sep()}
	snap, ok := v.snapshot()
	if ok {
		recents, _ := srRecents()
		rank := map[string]int{}
		for i, id := range recents {
			rank[id] = i + 1
		}
		var cs []core.Conv
		for _, c := range snap.Convs {
			if (c.Kind == "channel" || c.Kind == "private") && !c.IsArchived {
				cs = append(cs, c)
			}
		}
		sort.SliceStable(cs, func(i, j int) bool {
			ri, rj := rank[cs[i].ID], rank[cs[j].ID]
			if (ri > 0) != (rj > 0) {
				return ri > 0
			}
			if ri != rj {
				return ri < rj
			}
			return cs[i].Name < cs[j].Name
		})
		if len(cs) > 15 {
			cs = cs[:15]
		}
		for _, c := range cs {
			val := "<#" + c.ID + ">"
			items = append(items, MenuItem{Label: c.Name, Icon: convIcon(c.Kind), Checked: srRefID(cur) == c.ID,
				Do: func() { v.setQuery(v.q.Set("in", val)) }})
		}
	}
	v.openMenu(gtx, v.pillR[srPIn], items, 260)
}

func (v *SearchView) moreMenu(gtx layout.Context) {
	now := time.Now()
	dates := []struct {
		label string
		t     time.Time
	}{
		{"Today", now}, {"Yesterday", now.AddDate(0, 0, -1)}, {"A week ago", now.AddDate(0, 0, -7)},
		{"A month ago", now.AddDate(0, -1, 0)}, {"A year ago", now.AddDate(-1, 0, 0)},
	}
	sub := func(key string) []MenuItem {
		cur := v.q.Get(key)
		out := []MenuItem{{Label: "Any time", Checked: cur == "", Do: func() { v.setQuery(v.q.Set(key, "")) }}, Sep()}
		for _, d := range dates {
			val := srDate(d.t)
			out = append(out, MenuItem{Label: d.label, Right: val, Checked: cur == val, Do: func() { v.setQuery(v.q.Set(key, val)) }})
		}
		return out
	}
	anchor := v.win(gtx, v.pillR[srPMore])
	pick := image.Pt(anchor.Min.X, anchor.Max.Y+dp(gtx, 4))
	items := []MenuItem{
		{Label: "Before…", Icon: "calendar", Sub: sub("before")},
		{Label: "After…", Icon: "calendar", Sub: sub("after")},
		{Label: "On…", Icon: "calendar", Sub: sub("on")},
		Sep(),
		{Label: "Has reaction…", Icon: "add-reaction", Do: func() {
			v.h.OpenEmojiPicker(pick, func(name string) {
				name = strings.Trim(name, ":")
				if name != "" {
					v.setQuery(v.q.Toggle("has", ":"+name+":"))
				}
			})
		}},
		{Label: "Is pinned", Icon: "pin", Checked: v.q.Has("has", "pin"), Do: func() { v.setQuery(v.q.Toggle("has", "pin")) }},
		{Label: "Is saved", Icon: "bookmark", Checked: v.q.Has("is", "saved"), Do: func() { v.setQuery(v.q.Toggle("is", "saved")) }},
		Sep(),
		{Label: "Clear all filters", Disabled: len(v.q.Mods) == 0, Do: func() { v.setQuery(v.q.ClearMods()) }},
	}
	v.openMenu(gtx, v.pillR[srPMore], items, 260)
}

func (v *SearchView) sortMenu(gtx layout.Context) {
	opts := []struct{ label, val string }{{"Most relevant", "score"}, {"Newest", "timestamp"}, {"Oldest", "oldest"}}
	items := make([]MenuItem, 0, len(opts))
	for _, o := range opts {
		val := o.val
		items = append(items, MenuItem{Label: o.label, Checked: v.sortBy == val, Do: func() {
			if v.sortBy != val {
				v.sortBy = val
				v.search(1)
			}
		}})
	}
	v.openMenu(gtx, v.sortR, items, 220)
}

// moreActive reports modifiers that only the More menu sets.
func (v *SearchView) moreActive() bool {
	for _, m := range v.q.Mods {
		switch m.Key {
		case "from", "in":
			continue
		case "to":
			if strings.EqualFold(m.Val, "me") {
				continue
			}
		case "has":
			if strings.EqualFold(m.Val, "link") {
				continue
			}
		case "is":
			if strings.EqualFold(m.Val, "thread") {
				continue
			}
		case "after":
			if m.Val == srLast7(time.Now()) {
				continue
			}
		}
		return true
	}
	return false
}

// refName renders a from:/in: value for a pill label.
func (v *SearchView) refName(key, val string) string {
	if id := srRefID(val); id != "" {
		if key == "from" || val[1] == '@' {
			if u, ok := lookupUser(v.h, id); ok {
				return userName(u)
			}
			return id
		}
		if c := v.h.Core(); c != nil {
			if n := c.ConvRef(id).Name; n != "" {
				return "#" + n
			}
		}
		return id
	}
	if strings.EqualFold(val, "me") {
		return "you"
	}
	if key == "in" && !strings.HasPrefix(val, "#") && !strings.HasPrefix(val, "@") {
		return "#" + val
	}
	return val
}

// ---- events ----

func (v *SearchView) handle(gtx layout.Context) {
	for i := range v.pills {
		if !v.pills[i].Clicked(gtx) {
			continue
		}
		switch i {
		case srPType:
			v.typeMenu(gtx)
		case srPFrom:
			v.fromMenu(gtx)
		case srPIn:
			v.inMenu(gtx)
		case srPMe:
			v.setQuery(v.q.Toggle("to", "me"))
		case srPLink:
			v.setQuery(v.q.Toggle("has", "link"))
		case srPThread:
			v.setQuery(v.q.Toggle("is", "thread"))
		case srPWeek:
			wk := srLast7(time.Now())
			if v.q.Has("after", wk) {
				v.setQuery(v.q.Set("after", ""))
			} else {
				v.setQuery(v.q.Set("after", wk))
			}
		case srPMore:
			v.moreMenu(gtx)
		}
	}
	if v.sortBtn.Clicked(gtx) {
		v.sortMenu(gtx)
	}
	if v.feedback.Clicked(gtx) {
		v.h.Toast("Feedback isn't supported yet")
	}
	switch v.tab {
	case srTabMessages:
		for i := range v.cards {
			c := &v.cards[i]
			if c.more.Clicked(gtx) {
				c.expanded = !c.expanded
			}
			if c.chip.Clicked(gtx) {
				m := v.msgs[i]
				ts := m.ThreadTS
				if ts == "" {
					ts = m.TS
				}
				RecordRecentConversation(m.Channel)
				v.h.Navigate("c/" + m.Channel + "/t/" + ts)
			}
			// card body and "Open in home" both jump to the message
			body, jump := c.click.Clicked(gtx), c.jump.Clicked(gtx)
			if body || jump {
				RecordRecentConversation(v.msgs[i].Channel)
				v.h.Navigate(msgRoute(v.msgs[i]))
			}
			if c.fwd.Clicked(gtx) {
				v.h.ShowOverlay(NewForwardModal(v.h, v.msgs[i]))
			}
			if c.kebab.Clicked(gtx) {
				v.resultMenu(gtx, i)
			}
		}
	case srTabChannels:
		for i := range v.chans {
			if v.rows[i].Clicked(gtx) || v.btns[i].Clicked(gtx) {
				RecordRecentConversation(v.chans[i].ID)
				v.h.Navigate("c/" + v.chans[i].ID)
			}
		}
	case srTabPeople:
		for i := range v.people {
			if v.rows[i].Clicked(gtx) {
				y := v.listY + (i-v.list.Position.First)*v.rowH - v.list.Position.Offset
				pad := v.padX(gtx, gtx.Constraints.Max.X)
				r := image.Rect(pad, y, pad+dp(gtx, 52), y+v.rowH)
				w := v.win(gtx, r)
				v.h.OpenProfile(image.Pt(w.Max.X, w.Min.Y), v.people[i].ID)
			}
		}
	}
}

// ---- layout ----

func (v *SearchView) padX(gtx layout.Context, w int) int {
	p := (w - dp(gtx, 1152)) / 2
	if lo := dp(gtx, 24); p < lo {
		p = lo
	}
	if hi := dp(gtx, 107); p > hi {
		p = hi
	}
	return p
}

// Layout draws the results page filling the constraints.
func (v *SearchView) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	th := v.h.Theme()
	cl := colorsOf(th)
	sz := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	if !v.started {
		v.started = true
		v.search(1)
	}
	v.handle(gtx)
	fill(gtx, image.Rectangle{Max: sz}, cl.Bg)
	px := v.padX(gtx, sz.X)
	right := sz.X - px

	// title
	at(gtx, image.Pt(px, dp(gtx, 15)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(right-px, dp(gtx, 26))}
		return v.title(gtx, th, cl)
	})

	// filter pills
	v.pillRow(gtx, th, cl, px, dp(gtx, 56), right)

	// sort + count + feedback
	ry := dp(gtx, 96)
	lh := dp(gtx, 20)
	if v.tab == srTabMessages {
		x := px
		d := at(gtx, image.Pt(x, ry), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg2, "Sorted by ")
			})
		})
		x += d.Size.X
		label := "Most relevant (default)"
		switch v.sortBy {
		case "timestamp":
			label = "Newest"
		case "oldest":
			label = "Oldest"
		}
		tw := textW(gtx, th, 13, theme.Bold, label)
		bw := tw + dp(gtx, 4) + dp(gtx, 12)
		v.sortR = image.Rect(x, ry, x+bw, ry+lh)
		at(gtx, image.Pt(x, ry), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(bw, lh))
			return clickBox(gtx, &v.sortBtn, nil, func(gtx layout.Context) layout.Dimensions {
				vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Bold, cl.Link, label)
				})
				at(gtx, image.Pt(tw+dp(gtx, 4), (lh-dp(gtx, 12))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "chevron-down", 12, cl.Link)
				})
				return layout.Dimensions{Size: image.Pt(bw, lh)}
			})
		})
	}
	fw := textW(gtx, th, 13, theme.Regular, "Give feedback")
	at(gtx, image.Pt(right-dp(gtx, 23)-fw, ry), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &v.feedback, nil, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Link, "Give feedback")
			})
		})
	})
	cy := ry + dp(gtx, 21)
	if v.tab != srTabMessages {
		cy = ry
	}
	count := ""
	switch {
	case v.loading && v.total == 0:
		count = "Searching…"
	case v.err != "":
		count = "Search failed: " + v.err
	case v.tab == srTabFiles:
	default:
		count = plural(v.total, "result")
	}
	if count != "" {
		at(gtx, image.Pt(px, cy), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(right-px, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg3, count)
			})
		})
	}

	// results
	top := dp(gtx, 146)
	if v.tab != srTabMessages {
		top = dp(gtx, 126)
	}
	v.listY = top
	v.cardX = px
	lsz := image.Pt(sz.X, maxInt(0, sz.Y-top))
	at(gtx, image.Pt(0, top), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(lsz)
		return v.results(gtx, th, cl, px, lsz)
	})
	return layout.Dimensions{Size: sz}
}

func (v *SearchView) title(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	q := v.q.Text()
	if q == "" {
		q = v.q.String()
	}
	h := dp(gtx, 26)
	if q == "" {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Black, cl.Fg, "Search")
		})
	}
	d := vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 18, theme.Regular, cl.Fg, "Results for: ")
	})
	maxW := gtx.Constraints.Max.X - d.Size.X
	if maxW < 1 {
		return d
	}
	d2 := at(gtx, image.Pt(d.Size.X, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, h)}
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Black, cl.Fg, q)
		})
	})
	return layout.Dimensions{Size: image.Pt(d.Size.X+d2.Size.X, h)}
}

// pillRow lays out the filter pills starting at (x, y).
func (v *SearchView) pillRow(gtx layout.Context, th *theme.Theme, cl *Colors, x, y, right int) {
	gap := dp(gtx, 8)
	x += v.pill(gtx, th, cl, srPType, x, y, srTabIcons[v.tab], srTabNames[v.tab], true, false) + gap
	if v.tab != srTabMessages {
		for i := srPFrom; i < srPN; i++ {
			v.pillR[i] = image.Rectangle{}
		}
		return
	}
	from := "From"
	if f := v.q.Get("from"); f != "" {
		from = "From: " + v.refName("from", f)
	}
	in := "In"
	if f := v.q.Get("in"); f != "" {
		in = "In: " + v.refName("in", f)
	}
	x += v.pill(gtx, th, cl, srPFrom, x, y, "user", from, true, v.q.Get("from") != "") + gap
	x += v.pill(gtx, th, cl, srPIn, x, y, "hash", in, true, v.q.Get("in") != "")
	x += dp(gtx, 12)
	ph := dp(gtx, 28)
	fill(gtx, image.Rect(x, y+dp(gtx, 4), x+maxInt(1, dp(gtx, 1)), y+ph-dp(gtx, 4)), cl.Divider)
	x += dp(gtx, 13)
	x += v.pill(gtx, th, cl, srPMe, x, y, "user", "Includes me", false, v.q.Has("to", "me")) + gap
	x += v.pill(gtx, th, cl, srPLink, x, y, "link", "Has a link", false, v.q.Has("has", "link")) + gap
	x += v.pill(gtx, th, cl, srPThread, x, y, "threads", "Threads only", false, v.q.Has("is", "thread")) + gap
	x += v.pill(gtx, th, cl, srPWeek, x, y, "calendar", "Last 7 days", false, v.q.Has("after", srLast7(time.Now()))) + gap
	v.pill(gtx, th, cl, srPMore, x, y, "filter", "More", true, v.moreActive())
}

// pill draws one filter pill at (x, y) and returns its width.
func (v *SearchView) pill(gtx layout.Context, th *theme.Theme, cl *Colors, i, x, y int, iconName, label string, chevron, active bool) int {
	h := dp(gtx, 28)
	tw := textW(gtx, th, 13, theme.Bold, label)
	w := dp(gtx, 10) + dp(gtx, 16) + dp(gtx, 6) + tw
	if chevron {
		w += dp(gtx, 6) + dp(gtx, 12) + dp(gtx, 8)
	} else {
		w += dp(gtx, 10)
	}
	v.pillR[i] = image.Rect(x, y, x+w, y+h)
	c := &v.pills[i]
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, h))
		return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			r := image.Rect(0, 0, w, h)
			rad := dp(gtx, 8)
			fg, border, bg := cl.Fg, cl.Divider, cl.Bg
			if active {
				fg, border, bg = cl.Link, cl.Link, theme.WithAlpha(cl.Link, 0x1a)
				if c.Hovered() {
					bg = theme.WithAlpha(cl.Link, 0x2a)
				}
			} else if c.Hovered() {
				bg = cl.BgHover
			}
			rrect(gtx, r, rad, border)
			rrect(gtx, r.Inset(1), rad-1, cl.Bg)
			if bg != cl.Bg {
				rrect(gtx, r.Inset(1), rad-1, bg)
			}
			ix := dp(gtx, 10)
			at(gtx, image.Pt(ix, (h-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, iconName, 16, fg)
			})
			lx := ix + dp(gtx, 16) + dp(gtx, 6)
			at(gtx, image.Pt(lx, 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(tw+dp(gtx, 2), h)}
				return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Bold, fg, label)
				})
			})
			if chevron {
				at(gtx, image.Pt(lx+tw+dp(gtx, 6), (h-dp(gtx, 12))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "chevron-down", 12, fg)
				})
			}
			pointerCursor(gtx, r.Max)
			return layout.Dimensions{Size: r.Max}
		})
	})
	return w
}

func (v *SearchView) results(gtx layout.Context, th *theme.Theme, cl *Colors, px int, sz image.Point) layout.Dimensions {
	cw := sz.X - 2*px
	empty := func(title, sub string) layout.Dimensions {
		at(gtx, image.Pt(px, dp(gtx, 40)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, sz.Y)}
			d := text(gtx, th, 15, theme.Bold, cl.Fg, title)
			if sub != "" {
				at(gtx, image.Pt(0, d.Size.Y+dp(gtx, 6)), func(gtx layout.Context) layout.Dimensions {
					return para(gtx, th, 13, 18, theme.Regular, cl.Fg3, sub, 0)
				})
			}
			return d
		})
		return layout.Dimensions{Size: sz}
	}
	switch v.tab {
	case srTabFiles:
		return empty("File search isn't supported yet", "Try searching messages, channels or people instead.")
	case srTabMessages:
		if len(v.cards) == 0 {
			if v.loading || v.err != "" {
				return layout.Dimensions{Size: sz}
			}
			if strings.TrimSpace(v.q.String()) == "" {
				return empty("Search for messages", "Type a query in the search bar, or pick filters above.")
			}
			return empty("No results", "Try different keywords, or remove some filters.")
		}
		d := v.list.Layout(gtx, len(v.cards), func(gtx layout.Context, i int) layout.Dimensions {
			return at(gtx, image.Pt(px, 0), func(gtx layout.Context) layout.Dimensions {
				d := v.card(gtx, th, cl, i, cw)
				v.cards[i].h = d.Size.Y + dp(gtx, 8)
				return layout.Dimensions{Size: image.Pt(px+cw, v.cards[i].h)}
			})
		})
		if !v.loading && v.page < v.pages && v.list.Position.First+v.list.Position.Count >= len(v.cards)-3 {
			v.search(v.page + 1)
		}
		return d
	case srTabChannels:
		if len(v.chans) == 0 {
			if v.loading || v.err != "" {
				return layout.Dimensions{Size: sz}
			}
			if v.q.Text() == "" {
				return empty("Search for channels", "Type a channel name in the search bar.")
			}
			return empty("No channels found", "Try a different name.")
		}
		v.rowH = dp(gtx, 64)
		return v.list.Layout(gtx, len(v.chans), func(gtx layout.Context, i int) layout.Dimensions {
			return v.chanRow(gtx, th, cl, i, px, cw)
		})
	case srTabPeople:
		if len(v.people) == 0 {
			if v.loading || v.err != "" {
				return layout.Dimensions{Size: sz}
			}
			if v.q.Text() == "" {
				return empty("Search for people", "Type a name in the search bar.")
			}
			return empty("No people found", "Try a different name.")
		}
		v.rowH = dp(gtx, 60)
		return v.list.Layout(gtx, len(v.people), func(gtx layout.Context, i int) layout.Dimensions {
			return v.personRow(gtx, th, cl, i, px, cw)
		})
	}
	return layout.Dimensions{Size: sz}
}

// card draws message result i, width w, at the origin: Slack's
// MessageListItem{result: {messages}} around SearchResult{…message}.
func (v *SearchView) card(gtx layout.Context, th *theme.Theme, cl *Colors, i, w int) layout.Dimensions {
	return slackc.Component(gtx, "MessageListItem", srCardArgs{v: v, th: th, cl: cl, i: i, w: w},
		srCardArgs.listItemProps, srCardArgs.layoutListItem)
}

// srCardArgs carry a result card through its boundaries.
type srCardArgs struct {
	v    *SearchView
	th   *theme.Theme
	cl   *Colors
	i, w int
	over slackc.Props // the message MessageListItem's plugin passed (ShowRealUser)
}

func (a srCardArgs) listItemProps() slackc.Props {
	m := &a.v.msgs[a.i]
	return slackc.Props{
		"$key":    m.Channel + "/" + m.TS,
		"index":   a.i,
		"variant": "search",
		"result":  slackc.Props{"type": "message", "messages": []any{slackMessage(m)}},
	}
}

func (a srCardArgs) layoutListItem(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if r := p.Map("result"); r != nil {
		if l := r.List("messages"); len(l) > 0 {
			a.over = asProps(l[0])
		}
	}
	return slackc.Component(gtx, "SearchResult", a, srCardArgs.resultProps, srCardArgs.layoutResult)
}

func (a srCardArgs) resultProps() slackc.Props {
	m := &a.v.msgs[a.i]
	return srResultProps(a.v.h, m, a.over, a.v.cards[a.i].avatar, 36)
}

func (a srCardArgs) layoutResult(gtx layout.Context, p slackc.Props) layout.Dimensions {
	v := a.v
	m := &v.msgs[a.i]
	c := &v.cards[a.i]
	name, av := c.name, c.avatar
	src := p
	if src == nil {
		src = a.over
	}
	if n, u, ok := srAuthorOver(v.h, m, src); ok {
		name, av = n, u
	}
	return v.cardNative(gtx, a.th, a.cl, a.i, a.w, name, av, p)
}

// cardNative is SearchResult's native: the card with the sender's name
// and avatar URL; rp holds the plugin's SearchResult props (nil
// unpatched), whose avatar slot it lays out.
func (v *SearchView) cardNative(gtx layout.Context, th *theme.Theme, cl *Colors, i, w int, name, av string, rp slackc.Props) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	c := &v.cards[i]
	m := &v.msgs[i]
	padL, padR, padT, padB := dp(gtx, 16), dp(gtx, 32), dp(gtx, 15), dp(gtx, 12)
	av := dp(gtx, 36)
	tx := padL + av + dp(gtx, 9)
	bw := w - tx - padR
	if bw < 1 {
		bw = 1
	}
	lh := dp(gtx, 22)
	if c.tokW != bw || c.tokExp != c.expanded {
		c.tokW, c.tokExp = bw, c.expanded
		if c.expanded {
			c.toks, c.lines, c.over = srWrapFit(gtx, th, c.full, bw, 0, 0)
		} else {
			more := textW(gtx, th, 13, theme.Regular, " … Show more") + dp(gtx, 4)
			c.toks, c.lines, c.over = srWrapFit(gtx, th, c.flat, bw, 1, more)
		}
	}
	lines := maxInt(1, c.lines)
	nameH := dp(gtx, 20)
	bodyY := padT + nameH
	h := bodyY + lines*lh
	if c.expanded {
		h += dp(gtx, 20) // "Show less"
	}
	if m.ReplyCount > 0 {
		h += dp(gtx, 9) + dp(gtx, 22)
	}
	h += padB
	sz := image.Pt(w, h)
	r := image.Rectangle{Max: sz}
	rad := dp(gtx, 12)
	hovered := c.click.Hovered() || c.more.Hovered() || c.chip.Hovered() || c.bar.Hovered() ||
		c.fwd.Hovered() || c.jump.Hovered() || c.kebab.Hovered() || v.menuCard == i
	bg := cl.Bg
	if hovered {
		bg = cl.BgHover
	}
	rrect(gtx, r, rad, cl.Divider)
	rrect(gtx, r.Inset(1), rad-1, bg)
	// whole-card click target below the content (siblings, not nested, so
	// "Show more" and the reply chip don't also open the message)
	{
		g := gtx
		g.Constraints = layout.Exact(sz)
		c.click.Layout(g, func(gtx layout.Context) layout.Dimensions {
			pointerCursor(gtx, sz)
			return layout.Dimensions{Size: sz}
		})
	}
	at(gtx, image.Pt(padL, padT+dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
		return srAvatar(gtx, v.h, m, av, 36, rp)
	})
	right := w - padR
	if c.when != "" {
		ww := textW(gtx, th, 13, theme.Regular, c.when)
		at(gtx, image.Pt(right-ww, padT), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, nameH, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg3, c.when)
			})
		})
		right -= ww + dp(gtx, 12)
	}
	d := at(gtx, image.Pt(tx, padT), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, right-tx), nameH)}
		return vcenter(gtx, nameH, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Black, cl.Fg, name)
		})
	})
	srDrawWhere(gtx, v.h, c.where, tx+d.Size.X+dp(gtx, 4), padT, right, nameH, bg)
	at(gtx, image.Pt(tx, bodyY), func(gtx layout.Context) layout.Dimensions {
		srDrawToks(gtx, th, cl, c.toks, lh, cl.Fg)
		return layout.Dimensions{}
	})
	y := bodyY + lines*lh
	switch {
	case !c.expanded && c.over:
		x := tx + srEndX(c.toks, 0) + dp(gtx, 4)
		at(gtx, image.Pt(x, bodyY), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &c.more, nil, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg3, "… Show more")
				})
			})
		})
	case c.expanded:
		at(gtx, image.Pt(tx, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &c.more, nil, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, dp(gtx, 20), func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg3, "Show less")
				})
			})
		})
		y += dp(gtx, 20)
	}
	if m.ReplyCount > 0 {
		y += dp(gtx, 9)
		at(gtx, image.Pt(tx, y), func(gtx layout.Context) layout.Dimensions {
			return v.replyChip(gtx, th, cl, &c.chip, m.ReplyCount)
		})
	}
	if hovered {
		v.hoverBar(gtx, th, cl, c, w)
	} else {
		c.tipFwd, c.tipJump, c.tipMore = TipState{}, TipState{}, TipState{}
	}
	return layout.Dimensions{Size: sz}
}

// hoverBar draws the floating action bar at the top-right of a card of
// width w. It is laid out after the card's content and click target, so
// its clicks never reach the card (which would open the message).
func (v *SearchView) hoverBar(gtx layout.Context, th *theme.Theme, cl *Colors, c *srCard, w int) {
	bw, bh := dp(gtx, 104), dp(gtx, 40)
	r := image.Rect(w-bw, 0, w, bh)
	rad := dp(gtx, 12)
	sh := srBarShadow
	if th != nil && th.Dark {
		sh = cl.Shadow
	}
	softShadow(gtx, r, rad, dp(gtx, 1), dp(gtx, 3), sh)
	rrect(gtx, r, rad, cl.Ring)
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)
	// the bar's own area swallows presses between the buttons
	at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(r.Size())
		return c.bar.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: r.Size()}
		})
	})
	pad, btn := dp(gtx, 4), dp(gtx, 32)
	x := r.Min.X + pad
	c.kebabR = image.Rect(x+2*btn, r.Min.Y+pad, x+3*btn, r.Min.Y+pad+btn)
	for _, b := range [...]struct {
		click       *widget.Clickable
		tip         *TipState
		icon, label string
	}{
		{&c.fwd, &c.tipFwd, "forward-msg", "Forward message…"},
		{&c.jump, &c.tipJump, "external", "Open in home"},
		{&c.kebab, &c.tipMore, "kebab", "More actions"},
	} {
		at(gtx, image.Pt(x, r.Min.Y+pad), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			d := iconButton(gtx, b.click, b.icon, 32, 18, cl.Muted, cl.BgHover, 8)
			Tooltip(gtx, th, b.tip, b.click.Hovered(), d.Size, Above, b.label, "")
			return d
		})
		x += btn
	}
}

// cardY is card i's top in view coordinates (from last frame's layout).
func (v *SearchView) cardY(i int) (int, bool) {
	first := v.list.Position.First
	if i < first || i >= len(v.cards) {
		return 0, false
	}
	y := v.listY - v.list.Position.Offset
	for k := first; k < i; k++ {
		y += v.cards[k].h
	}
	return y, true
}

// resultMenu opens the More actions menu of result i next to its kebab:
// first row level with the button, clamped into the window (Slack lets it
// overlap the button rather than flip to the far side); near the bottom it
// opens upwards with its last row level with the button.
func (v *SearchView) resultMenu(gtx layout.Context, i int) {
	y, ok := v.cardY(i)
	if !ok {
		return
	}
	k := v.win(gtx, v.cards[i].kebabR.Add(image.Pt(v.cardX, y)))
	pad := dp(gtx, 12)
	// place() reads Max.Y for "below" and Min.Y for "above".
	a := image.Rectangle{Min: image.Pt(k.Max.X, k.Max.Y+pad), Max: image.Pt(k.Max.X, k.Min.Y-pad)}
	v.menuSeq++
	seq := v.menuSeq
	v.menuCard = i
	v.h.ShowOverlay(NewMenuOpts(v.h, srResultItems(v.h, v.msgs[i]), MenuOpts{Anchor: a, OnClose: func() {
		if v.menuSeq == seq {
			v.menuCard = -1
		}
	}}))
}

// srResultItems is a search result's More actions menu (search_more.png):
// Add to saved | Add a reminder ▸ | reply notifications | sep | Copy link |
// Copy message | sep | Connect to apps ▸.
func srResultItems(h Host, m core.Message) []MenuItem {
	ch, ts := m.Channel, m.TS
	root := m.ThreadTS
	if root == "" {
		root = ts
	}
	self := actSelfID(h)
	link := m.Permalink
	if link == "" {
		link = permalink(h, ch, ts, m.ThreadTS)
	}
	remind := func(kind int) func() {
		return func() {
			due := actReminderDue(time.Now(), kind).Unix()
			actDo(h, "Reminder set", func(ctx context.Context, a *core.App) error { return a.Save(ctx, ch, ts, due) })
		}
	}
	// authors and repliers follow a thread by default
	follow := MenuItem{Label: "Get notified about new replies", Icon: "bell", Do: func() {
		actDo(h, "You'll be notified about new replies", func(ctx context.Context, a *core.App) error {
			return a.SubscribeThread(ctx, ch, root, true)
		})
	}}
	if self != "" && (m.User == self || slices.Contains(m.ReplyUsers, self)) {
		follow = MenuItem{Label: "Turn off notifications for replies", Icon: "bell-off", Do: func() {
			actDo(h, "You won't be notified about new replies", func(ctx context.Context, a *core.App) error {
				return a.SubscribeThread(ctx, ch, root, false)
			})
		}}
	}
	return []MenuItem{
		{Label: "Add to saved", Icon: "bookmark-add", Hint: "Ctrl+Shift+S", Do: func() {
			actDo(h, "Saved for later", func(ctx context.Context, a *core.App) error { return a.Save(ctx, ch, ts, 0) })
		}},
		{Label: "Add a reminder", Icon: "remind", Sub: []MenuItem{
			{Label: "In 30 mins", Do: remind(actRem30m)},
			{Label: "In 1 hour", Do: remind(actRem1h)},
			{Label: "In 3 hours", Do: remind(actRem3h)},
			{Label: "Tomorrow at 9:00 AM", Do: remind(actRemTomorrow)},
			{Label: "Monday at 9:00 AM", Do: remind(actRemMonday)},
		}},
		follow,
		Sep(),
		{Label: "Copy link", Icon: "link", Hint: "L", DoCtx: func(gtx layout.Context) {
			copyText(gtx, link)
			h.Toast("Link copied")
		}},
		{Label: "Copy message", Icon: "text-format", Hint: "Ctrl+C", DoCtx: func(gtx layout.Context) {
			copyText(gtx, actMsgText(m))
		}},
		Sep(),
		{Label: "Connect to apps", Sub: []MenuItem{{Label: "No apps", Disabled: true}}},
	}
}

func (v *SearchView) replyChip(gtx layout.Context, th *theme.Theme, cl *Colors, c *widget.Clickable, n int) layout.Dimensions {
	s := strconv.Itoa(n)
	tw := textW(gtx, th, 12, theme.Regular, s)
	h := dp(gtx, 22)
	w := dp(gtx, 8) + dp(gtx, 14) + dp(gtx, 4) + tw + dp(gtx, 8)
	return clickBox(gtx, c, func(gtx layout.Context, sz image.Point, hovered bool) {
		r := image.Rectangle{Max: sz}
		rrect(gtx, r, h/2, cl.BtnBorder)
		bg := cl.Bg
		if hovered {
			bg = cl.BgHover
		}
		rrect(gtx, r.Inset(1), h/2-1, bg)
	}, func(gtx layout.Context) layout.Dimensions {
		at(gtx, image.Pt(dp(gtx, 8), (h-dp(gtx, 14))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "threads", 14, cl.Fg2)
		})
		at(gtx, image.Pt(dp(gtx, 26), 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 12, theme.Regular, cl.Fg2, s)
			})
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
}

func (v *SearchView) chanRow(gtx layout.Context, th *theme.Theme, cl *Colors, i, px, cw int) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	ch := &v.chans[i]
	h := v.rowH
	sz := image.Pt(cw, h)
	at(gtx, image.Pt(px, 0), func(gtx layout.Context) layout.Dimensions {
		if v.rows[i].Hovered() || v.btns[i].Hovered() {
			rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 8), cl.BgHover)
		}
		g := gtx
		g.Constraints = layout.Exact(sz)
		v.rows[i].Layout(g, func(gtx layout.Context) layout.Dimensions {
			pointerCursor(gtx, sz)
			return layout.Dimensions{Size: sz}
		})
		box := dp(gtx, 36)
		x := dp(gtx, 12)
		rrect(gtx, image.Rect(x, (h-box)/2, x+box, (h+box)/2), dp(gtx, 8), cl.Disabled)
		at(gtx, image.Pt(x+(box-dp(gtx, 20))/2, (h-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, convIcon(ch.Kind), 20, cl.Fg2)
		})
		tx := x + box + dp(gtx, 12)
		label := "View"
		if !ch.IsMember {
			label = "Join"
		}
		bw := textW(gtx, th, 13, theme.Bold, label) + dp(gtx, 24)
		bx := cw - dp(gtx, 12) - bw
		at(gtx, image.Pt(bx, (h-dp(gtx, 28))/2), func(gtx layout.Context) layout.Dimensions {
			return Button(gtx, th, &v.btns[i], BtnOutline, BtnSmall, label, "", 0)
		})
		maxW := maxInt(1, bx-dp(gtx, 12)-tx)
		at(gtx, image.Pt(tx, dp(gtx, 11)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, dp(gtx, 22))}
			return vcenter(gtx, dp(gtx, 22), func(gtx layout.Context) layout.Dimensions {
				name := ch.Name
				if ch.IsArchived {
					name += " (archived)"
				}
				return text(gtx, th, 15, theme.Bold, cl.Fg, name)
			})
		})
		sub := plural(ch.NumMembers, "member")
		if ch.IsMember {
			sub = "Joined · " + sub
		}
		if p := strings.TrimSpace(ch.Purpose); p != "" {
			sub += " · " + strings.Join(strings.Fields(p), " ")
		}
		at(gtx, image.Pt(tx, dp(gtx, 33)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, dp(gtx, 20))}
			return vcenter(gtx, dp(gtx, 20), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg3, sub)
			})
		})
		return layout.Dimensions{Size: sz}
	})
	return layout.Dimensions{Size: image.Pt(px+cw, h)}
}

func (v *SearchView) personRow(gtx layout.Context, th *theme.Theme, cl *Colors, i, px, cw int) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	u := &v.people[i]
	h := v.rowH
	sz := image.Pt(cw, h)
	at(gtx, image.Pt(px, 0), func(gtx layout.Context) layout.Dimensions {
		g := gtx
		g.Constraints = layout.Exact(sz)
		return clickBox(g, &v.rows[i], func(gtx layout.Context, sz image.Point, hovered bool) {
			if hovered {
				rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 8), cl.BgHover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			x := dp(gtx, 12)
			av := dp(gtx, 36)
			at(gtx, image.Pt(x, (h-av)/2), func(gtx layout.Context) layout.Dimensions {
				return avatar(gtx, v.h, u.Avatar, 36)
			})
			tx := x + av + dp(gtx, 12)
			maxW := maxInt(1, cw-tx-dp(gtx, 12))
			d := at(gtx, image.Pt(tx, dp(gtx, 9)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxW-dp(gtx, 16), dp(gtx, 22))}
				return vcenter(gtx, dp(gtx, 22), func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Bold, cl.Fg, userName(*u))
				})
			})
			if u.Presence != "" {
				at(gtx, image.Pt(tx+d.Size.X+dp(gtx, 6), dp(gtx, 9)+(dp(gtx, 22)-dp(gtx, 9))/2), func(gtx layout.Context) layout.Dimensions {
					return presenceDot(gtx, th, u.Presence == "active", 9)
				})
			}
			sub := u.Title
			if u.RealName != "" && u.RealName != userName(*u) {
				if sub != "" {
					sub = u.RealName + " · " + sub
				} else {
					sub = u.RealName
				}
			}
			if sub != "" {
				at(gtx, image.Pt(tx, dp(gtx, 31)), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, dp(gtx, 20))}
					return vcenter(gtx, dp(gtx, 20), func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 13, theme.Regular, cl.Fg3, sub)
					})
				})
			}
			return layout.Dimensions{Size: sz}
		})
	})
	return layout.Dimensions{Size: image.Pt(px+cw, h)}
}
