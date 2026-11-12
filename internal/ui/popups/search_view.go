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
	"encoding/json"
	"image"
	"image/color"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
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

// search result types: the [Messages ▾] source menu, in Slack's order
// (search-view's source list: messages, dms, files, people, channels,
// canvases), each with its module's total.
const (
	srTabMessages = iota
	srTabDMs
	srTabFiles
	srTabPeople
	srTabChannels
	srTabCanvases
	srTabN
)

var srTabNames = [...]string{"Messages", "DMs", "Files", "People", "Channels", "Canvases"}
var srTabIcons = [...]string{"slack-logo", "dms", "files", "people", "hash", "canvas"}
var srTabModules = [...]string{core.ModMessages, core.ModDMs, core.ModFiles, core.ModPeople, core.ModChannels, core.ModCanvases}

// srTabOf maps a module name to its tab (-1 unknown).
func srTabOf(module string) int {
	for i, m := range srTabModules {
		if m == module {
			return i
		}
	}
	return -1
}

// srCountShort is the source menu's count: 660, 17k, 292k, 1.2M.
func srCountShort(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 10000:
		s := strconv.FormatFloat(float64(n)/1000, 'f', 1, 64)
		return strings.TrimSuffix(s, ".0") + "k"
	case n < 1000000:
		return strconv.Itoa((n+500)/1000) + "k"
	}
	s := strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0") + "M"
}

// filter pills (search_view_filters.go maps Slack's filter ids to them)
const (
	srPType = iota // the source ([Messages ▾])
	srPFrom
	srPIn
	srPMe // With: Includes me (with:me), or the With menu on DMs
	srPLink
	srPThread
	srPWeek
	srPDate
	srPFileType
	srPOnlyMine
	srPCreator
	srPMore
	srPReset
	srPN
)

// srCard is one message result's UI state.
type srCard struct {
	name, avatar, when string
	where              srWhere
	flat, full         []srSeg
	ovText             string // a plugin's text the tokens came from (overText)
	expanded           bool
	click, more, chip  widget.Clickable
	toks               []srTok
	tokW               int
	tokExp             bool
	over               bool
	lines              int

	// Slack's ProfileHoverTrigger on the sender's avatar and name
	// (SearchResult: BaseAvatar showCard at 36px, MessageSender)
	avHov, nameHov hovercard.Trigger

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
	emptyFb  widget.Clickable // the no-results state's "Give feedback"

	started bool
	seq     int
	loading bool
	err     string
	sess    core.SearchSession // one per query: every module call shares it
	exact   bool               // More > Exact matches only (search_exact_matches_only)
	// exclArch is More > Exclude archived (channel_type exclude_archived),
	// shown with search_explore_freshness_signals on
	exclArch bool
	visible  []string // the filters the row shows as pills (srFSources first), last frame
	// tabSort is the Files, Canvases, Channels and People tabs' Sort
	// select value (Slack's: score, name, relevant, …; srTabSorts)
	tabSort [srTabN]string
	// unfiltered is the messages response's unfiltered_result_count
	// (RelaxationSuggestion's "Search without filters")
	unfiltered int
	relaxBtn   widget.Clickable

	// wo is the work object sources and results (search_workobjects.go)
	wo srWO

	// the source menu's per-tab totals for the query in countQ
	counts  [srTabN]int
	countOK [srTabN]bool
	countQ  string

	msgs  []core.Message
	cards []srCard
	convs map[string]core.ConvRef
	total int
	page  int
	pages int
	store srStoreRows // the page's rows as plugins present the store (search_store.go)

	fromSugg []core.FilterSuggestion // filter_suggestions.from (With people)

	chans  []core.ChannelHit
	people []core.User
	files  []core.File
	rows   []widget.Clickable
	btns   []widget.Clickable
	listY  int
	rowH   int

	// numbered pagination (Slack's c-pagination)
	prevBtn, nextBtn widget.Clickable
	pageBtns         [5]widget.Clickable
	pageNums         [5]int

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
	tab := srTabMessages
	if q, mod, ok := strings.Cut(query, "&module="); ok { // "search?q=…&module=files"
		query = strings.TrimSpace(q)
		if t := srTabOf(mod); t >= 0 {
			tab = t
		}
	}
	srRecordQuery(query)
	v := &SearchView{h: h, q: srParse(query), tab: tab, sortBy: DefaultSearchSort(h.Core()), menuCard: -1, sess: core.NewSearchSession()}
	for t := range v.tabSort {
		v.tabSort[t] = srDefaultTabSort(h.Core(), t)
	}
	v.list.Axis = layout.Vertical
	return v
}

// Tab is the selected source (srTabMessages…).
func (v *SearchView) Tab() int { return v.tab }

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
	v.sess = core.NewSearchSession() // a new query is a new search session
	v.search(1)
}

func (v *SearchView) setTab(t int) {
	if v.wo.sel != "" { // leaving a work object source for a standard one
		v.wo.sel = ""
		if t == v.tab {
			v.h.Invalidate()
			return
		}
	}
	if t == v.tab {
		return
	}
	v.tab = t
	v.search(1)
}

func (v *SearchView) reset() {
	v.msgs, v.cards, v.convs = nil, nil, nil
	v.total, v.page, v.pages = 0, 0, 0
	v.unfiltered = 0
	v.chans, v.people, v.files = nil, nil, nil
	v.err = ""
	v.menuCard = -1
	v.list.Position = layout.Position{}
}

// srRecentChannels is recent_channels: the recently opened conversations.
func srRecentChannels() []string {
	places, _ := srRecents()
	var out []string
	for _, id := range places {
		if !strings.HasPrefix(id, "view:") && len(out) < 20 {
			out = append(out, id)
		}
	}
	return out
}

// countTabs fetches every other tab's total for the source menu (Slack
// fires the modules together, one session).
func (v *SearchView) countTabs(c *core.App) {
	q := v.q.String()
	if strings.TrimSpace(q) == "" || q == v.countQ {
		return
	}
	v.countQ = q
	v.countOK = [srTabN]bool{}
	sess := v.sess
	for t := 0; t < srTabN; t++ {
		if t == v.tab {
			continue
		}
		tab, mod := t, srTabModules[t]
		words := strings.TrimSpace(v.q.Text())
		query := q
		if tab == srTabChannels || tab == srTabPeople {
			query = words
			if query == "" {
				continue
			}
		}
		run(v.h, func(ctx context.Context) (int, error) { return c.ModuleCount(ctx, mod, query, sess) },
			func(n int, err error) {
				if err != nil || v.countQ != q {
					return
				}
				v.counts[tab], v.countOK[tab] = n, true
			})
	}
}

// search loads page (1 = a fresh search) of the selected tab.
func (v *SearchView) search(page int) {
	v.seq++
	seq := v.seq
	v.reset()
	v.loading = false
	c := v.h.Core()
	if c == nil {
		return
	}
	words := strings.TrimSpace(v.q.Text())
	q := v.q.String()
	if page <= 1 {
		v.countTabs(c)
	}
	sess := v.sess
	done := func(total int) {
		v.counts[v.tab], v.countOK[v.tab] = total, true
	}
	if page <= 1 && v.tab == srTabMessages {
		v.searchWO(c, q) // searchWorkObjects rides along with a messages search
	}
	switch v.tab {
	case srTabMessages, srTabDMs:
		if strings.TrimSpace(q) == "" {
			return
		}
		o := core.SearchOpts{Query: q, Page: page, Sort: v.sortBy, ExactOnly: v.exact, ExcludeArchived: v.exclArch,
			Session: sess, RecentChannels: srRecentChannels(), Module: srTabModules[v.tab]}
		v.loading = true
		run(v.h, func(ctx context.Context) (core.SearchResult, error) { return c.SearchMessages(ctx, o) },
			func(res core.SearchResult, err error) {
				if seq != v.seq {
					return
				}
				v.loading = false
				if err != nil {
					v.err = errText(err)
					return
				}
				v.addMessages(res, page)
				if v.tab == srTabMessages {
					v.fromSugg = res.FilterFrom
					v.unfiltered = res.Unfiltered
				}
				done(res.Total)
			})
	case srTabFiles, srTabCanvases:
		if strings.TrimSpace(q) == "" {
			return
		}
		o := core.FileSearchOpts{Query: q, Canvases: v.tab == srTabCanvases, Page: page, Sort: v.tabSort[v.tab], Session: sess}
		v.loading = true
		run(v.h, func(ctx context.Context) (core.FilePage, error) { return c.SearchFiles(ctx, o) },
			func(fp core.FilePage, err error) {
				if seq != v.seq {
					return
				}
				v.loading = false
				if err != nil {
					v.err = errText(err)
					return
				}
				v.files, v.total, v.page, v.pages = fp.Files, fp.Total, max(fp.Page, 1), fp.Pages
				v.growRows(len(fp.Files))
				done(fp.Total)
			})
	case srTabChannels:
		if words == "" {
			return
		}
		v.loading = true
		sortBy := v.tabSort[srTabChannels]
		run(v.h, func(ctx context.Context) (core.ChannelPage, error) {
			return c.SearchChannelsTabSort(ctx, words, sortBy, page, sess)
		},
			func(cp core.ChannelPage, err error) {
				if seq != v.seq {
					return
				}
				v.loading = false
				if err != nil {
					v.err = errText(err)
					return
				}
				v.chans, v.total, v.page, v.pages = cp.Channels, cp.Total, max(cp.Page, 1), cp.Pages
				v.growRows(len(cp.Channels))
				done(cp.Total)
			})
	case srTabPeople:
		if words == "" {
			return
		}
		v.loading = true
		sortBy := v.tabSort[srTabPeople]
		run(v.h, func(ctx context.Context) (core.PeoplePage, error) {
			return c.SearchPeopleTabSort(ctx, words, sortBy, page, sess)
		},
			func(pp core.PeoplePage, err error) {
				if seq != v.seq {
					return
				}
				v.loading = false
				if err != nil {
					v.err = errText(err)
					return
				}
				v.people, v.total, v.page, v.pages = pp.Users, pp.Total, max(pp.Page, 1), pp.Pages
				v.growRows(len(pp.Users))
				done(pp.Total)
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
	terms := v.q.Highlights()
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
	v.mirror()
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

// srSourceRow is a source menu row: Slack's radio item with the check
// hanging left of the label (link blue when selected) and the module's
// total at the right.
func srSourceRow(th *theme.Theme, label, count string, sel bool) func(gtx layout.Context, hl bool) layout.Dimensions {
	return func(gtx layout.Context, hl bool) layout.Dimensions {
		cl := colorsOf(th)
		sz := image.Pt(gtx.Constraints.Max.X, dp(gtx, 28))
		fg, sub := cl.Fg, cl.Fg2
		if sel {
			fg = cl.Link
		}
		if hl {
			fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
			fg, sub = cl.HighText, cl.HighText
		}
		if sel {
			at(gtx, image.Pt(dp(gtx, 6), (sz.Y-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "check", 16, fg)
			})
		}
		right := sz.X - dp(gtx, 24)
		if count != "" {
			cw := textW(gtx, th, 15, theme.Regular, count)
			right -= cw
			at(gtx, image.Pt(right, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, sub, count)
				})
			})
			right -= dp(gtx, 8)
		}
		at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, right-dp(gtx, 24)), sz.Y)}
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, fg, label)
			})
		})
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	}
}

// srCheckRow is a More menu checkbox row (Slack's custom checkbox items:
// the box at the left, the label after it); on reads the live state so
// the row updates while the menu stays open.
func srCheckRow(th *theme.Theme, label string, on func() bool) func(gtx layout.Context, hl bool) layout.Dimensions {
	return srCheckIconRow(th, label, "", on)
}

// srCheckIconRow is CheckboxMenuRowComponent with a labelIcon (File type's
// rows): the 16px icon 8px before the label (checkboxLabelWithIcon). Icons
// Clack doesn't have are left out rather than drawn as placeholders.
func srCheckIconRow(th *theme.Theme, label, iconName string, on func() bool) func(gtx layout.Context, hl bool) layout.Dimensions {
	return func(gtx layout.Context, hl bool) layout.Dimensions {
		cl := colorsOf(th)
		sz := image.Pt(gtx.Constraints.Max.X, dp(gtx, 28))
		fg := cl.Fg
		if hl {
			fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
			fg = cl.HighText
		}
		at(gtx, image.Pt(dp(gtx, 24), (sz.Y-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
			return Checkbox(gtx, th, on(), false, false)
		})
		lx := dp(gtx, 48)
		if iconName != "" && icons.HasSlack(iconName) {
			at(gtx, image.Pt(lx, (sz.Y-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return icons.GetSlack(iconName)(gtx, unit.Dp(16), fg)
			})
			lx += dp(gtx, 16) + dp(gtx, 8)
		}
		at(gtx, image.Pt(lx, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, sz.X-lx-dp(gtx, 24)), sz.Y)}
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, fg, label)
			})
		})
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	}
}

// typeMenu is the [Messages ▾] source menu: each tab with its total.
func (v *SearchView) typeMenu(gtx layout.Context) {
	th := v.h.Theme()
	items := make([]MenuItem, 0, srTabN)
	for i, n := range srTabNames {
		t := i
		count := ""
		if v.countOK[i] {
			count = srCountShort(v.counts[i])
		}
		sel := v.tab == i && v.wo.sel == ""
		items = append(items, MenuItem{Key: srTabModules[i], Label: n, Checked: sel,
			Widget: srSourceRow(th, n, count, sel), Do: func() { v.setTab(t) }})
	}
	items = append(items, v.woMenuItems(th)...) // each app's work objects (search_workobjects.go)
	v.openMenu(gtx, v.pillR[srPType], items, 280)
}

func (v *SearchView) inMenu(gtx layout.Context) {
	cur := v.q.Get("in")
	items := []MenuItem{{Label: "Anywhere", Checked: cur == "", Do: func() { v.setQuery(v.q.Set("in", "")) }}, Sep()}
	for _, c := range v.inChannels() {
		val := "<#" + c.ID + ">"
		items = append(items, MenuItem{Label: c.Name, Icon: convIcon(c.Kind), Checked: srRefID(cur) == c.ID,
			Do: func() { v.setQuery(v.q.Set("in", val)) }})
	}
	v.openMenu(gtx, v.pillR[srPIn], items, 260)
}

// inChannels are the In menus' channels: the recently opened first, then
// by name, fifteen at most.
func (v *SearchView) inChannels() []core.Conv {
	snap, ok := v.snapshot()
	if !ok {
		return nil
	}
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
	return cs
}

// srDateRanges are the Date submenu's presets (search-filters: each is
// after:<day before the range> before:<day after it>; after: and before:
// are exclusive).
var srDateRanges = [...]struct {
	label     string
	from, end func(now time.Time) time.Time
}{
	{"Today", func(n time.Time) time.Time { return n.AddDate(0, 0, -1) }, func(n time.Time) time.Time { return n.AddDate(0, 0, 1) }},
	{"Yesterday", func(n time.Time) time.Time { return n.AddDate(0, 0, -2) }, func(n time.Time) time.Time { return n }},
	{"Last 7 days", func(n time.Time) time.Time { return n.AddDate(0, 0, -7) }, func(n time.Time) time.Time { return n.AddDate(0, 0, 1) }},
	{"Last 30 days", func(n time.Time) time.Time { return n.AddDate(0, 0, -30) }, func(n time.Time) time.Time { return n.AddDate(0, 0, 1) }},
	{"Last 12 months", func(n time.Time) time.Time { return n.AddDate(-1, 0, 0) }, func(n time.Time) time.Time { return n.AddDate(0, 0, 1) }},
	{"Last 3 years", func(n time.Time) time.Time { return n.AddDate(-3, 0, 0) }, func(n time.Time) time.Time { return n.AddDate(0, 0, 1) }},
}

// srWithRange sets the after:/before: pair of a Date preset ("" both clears).
func srWithRange(q srQuery, after, before string) srQuery {
	q = q.Set("on", "").Set("during", "")
	return q.Set("before", before).Set("after", after)
}

// srFileTypes are More > File type's rows, their modifiers and their
// labelIcons ($.t9).
var srFileTypes = [...]struct{ label, val, icon string }{
	{"Huddle AI Notes", "huddle_notes", "canvas"}, {"Lists", "lists", "lists"}, {"Canvases & Posts", "canvases", "canvas"},
	{"Documents", "documents", "document"}, {"Emails", "emails", "email"}, {"Images", "images", "image"},
	{"PDFs", "pdfs", "pdf-file"}, {"Presentations", "presentations", "presentation"}, {"Snippets", "snippets", "text-snippet"},
	{"Spreadsheets", "spreadsheets", "spreadsheet"}, {"Audio", "audio", "sound"}, {"Videos", "videos", "video"},
}

// srTopReactions are the Reactions submenu's quick picks.
var srTopReactions = [...]string{"thumbs-up", "slightly_smiling_face", "heart"}

// sortMenu is "Sorted by …" (search-filter-sort MessageEntity): Most
// relevant ("(default)" unless stick_user_sort), Newest, Oldest, then
// "Edit default", which opens Preferences > Advanced's search options.
// With stick_user_sort a choice is also kept in users.prefs search_sort
// (the next search starts from it, mfHU WJ).
func (v *SearchView) sortMenu(gtx layout.Context) {
	if v.tab != srTabMessages {
		v.tabSortMenu(gtx)
		return
	}
	c := v.h.Core()
	sticky := c != nil && c.PrefBool("stick_user_sort", false)
	relevant := "Most relevant (default)"
	if sticky {
		relevant = "Most relevant"
	}
	opts := []struct{ label, val string }{{relevant, "score"}, {"Newest", "timestamp"}, {"Oldest", "oldest"}}
	items := make([]MenuItem, 0, len(opts)+2)
	for _, o := range opts {
		val := o.val
		items = append(items, MenuItem{Key: LabelKey(o.label), Label: nsSearch.T(o.label), Checked: v.sortBy == val, Do: func() {
			if v.sortBy != val {
				v.sortBy = val
				v.search(1)
			}
			if sticky {
				pref := srSortPref[val]
				run(v.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SetPref(ctx, "search_sort", pref) },
					func(_ struct{}, err error) {
						if err != nil {
							v.h.Toast("Couldn't save that preference: " + errText(err))
						}
					})
			}
		}})
	}
	items = append(items, Sep(), MenuItem{Label: "Edit default", Do: func() {
		if OpenPreferencesSection != nil {
			OpenPreferencesSection("advanced")
		}
	}}.In(nsSearch))
	v.openMenu(gtx, v.sortR, items, 240)
}

// srSortPref is the search_sort value for a sort (cX/T kK).
var srSortPref = map[string]string{"score": "relevantMessages", "timestamp": "recentMessages", "oldest": "timestamp_asc"}

// DefaultSearchSort is a new search's sort: with stick_user_sort ("Last
// used:") the search_sort pref (mfHU WJ: timestamp means newest, score
// and not_set most relevant), otherwise Most relevant.
func DefaultSearchSort(c *core.App) string {
	if c == nil || !c.PrefBool("stick_user_sort", false) {
		return "score"
	}
	switch c.PrefString("search_sort") {
	case "recentMessages", "timestamp":
		return "timestamp"
	case "timestamp_asc":
		return "oldest"
	}
	return "score"
}

// moreActive reports filters that only the More menu sets (the pill
// draws active).
func (v *SearchView) moreActive() bool {
	if v.exact {
		return true
	}
	wk := srLast7(time.Now())
	self := ""
	if snap, ok := v.snapshot(); ok {
		self = "<@" + snap.Self.ID + ">"
	}
	for _, m := range v.q.Mods {
		switch m.Key {
		case "from", "in":
			if !strings.EqualFold(m.Val, "activity") {
				continue
			}
		case "with":
			if m.Val == self {
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
			if m.Val == wk {
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
		if v.pills[i].Clicked(gtx) {
			v.pillClicked(gtx, i)
		}
	}
	if v.sortBtn.Clicked(gtx) {
		v.sortMenu(gtx)
	}
	if v.relaxBtn.Clicked(gtx) && v.unfiltered > 0 {
		v.setQuery(srWithoutFilters(v.q))
	}
	if v.feedback.Clicked(gtx) || v.emptyFb.Clicked(gtx) {
		v.openFeedback()
	}
	// pagination
	if v.prevBtn.Clicked(gtx) && v.page > 1 {
		v.search(v.page - 1)
	}
	if v.nextBtn.Clicked(gtx) && v.page < v.pages {
		v.search(v.page + 1)
	}
	for i := range v.pageBtns {
		if v.pageBtns[i].Clicked(gtx) && v.pageNums[i] > 0 && v.pageNums[i] != v.page {
			v.search(v.pageNums[i])
		}
	}
	switch v.tab {
	case srTabFiles, srTabCanvases:
		for i := range v.files {
			if v.rows[i].Clicked(gtx) {
				v.openFile(v.files[i])
			}
		}
	case srTabMessages, srTabDMs:
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

// openFeedback is the results page's "Give feedback" (and the no-results
// state's): CombinedSearchFeedbackModal for the active tab's module.
func (v *SearchView) openFeedback() {
	q, sess, tab := v.q.String(), v.sess, v.tab
	sortBy, tabSort := v.sortBy, v.tabSort[v.tab]
	counts := map[string]int{}
	for t := 0; t < srTabN; t++ {
		if v.countOK[t] {
			counts[srTabModules[t]] = v.counts[t]
		}
	}
	c := v.h.Core()
	openSearchFeedback(v.h, srFeedbackOpts{feedbackType: "search", query: q, payload: func() any {
		if tab != srTabMessages {
			sortBy = tabSort
		}
		sort, dir := srActiveSort(tab, sortBy)
		return srSearchFeedbackPayload{SearchSessionID: sess.ID, QuerySessionID: sess.QueryID, Query: q,
			Module: srTabModules[tab], ActiveSort: sort, ActiveSortDir: dir, ActiveFilters: srActiveFilters(c, tab),
			ClientVersion: core.SearchFeedbackClientVersion, ModuleCounts: counts}
	}})
}

// srSearchFeedbackPayload is the results page's feedback payload
// (combinedSearchFeedbackSubmit for feedbackType "search", in Slack's key
// order). searchFeedbackDialog (search in channel) sends the same keys
// without client_version and with empty module_counts.
type srSearchFeedbackPayload struct {
	SearchSessionID string         `json:"search_session_id"`
	QuerySessionID  string         `json:"query_session_id"`
	Query           string         `json:"query"`
	Module          string         `json:"module"`
	ActiveSort      string         `json:"active_sort,omitempty"`
	ActiveSortDir   string         `json:"active_sort_dir,omitempty"`
	ActiveFilters   map[string]any `json:"active_filters"`
	ClientVersion   string         `json:"client_version,omitempty"`
	ModuleCounts    map[string]int `json:"module_counts"`
}

// srActiveSort is the module request's sort and sort_dir
// (getActiveSearchFeedbackContext reads them off the last request's args).
func srActiveSort(tab int, sortBy string) (string, string) {
	switch tab {
	case srTabMessages:
		switch sortBy {
		case "timestamp":
			return "timestamp", "desc"
		case "oldest":
			return "timestamp", "asc"
		}
		return "score", "desc"
	case srTabFiles, srTabCanvases, srTabChannels, srTabPeople:
		return core.SearchSortParams(sortBy)
	}
	return "", ""
}

// srActiveFilters is getActiveSearchFeedbackContext's active_filters: the
// module result's filters (Clack doesn't keep them) and, for messages and
// files, the exclude_bots / only_my_channels prefs.
func srActiveFilters(c *core.App, tab int) map[string]any {
	f := map[string]any{}
	if c != nil && (tab == srTabMessages || tab == srTabFiles) {
		f["exclude_bots"] = c.PrefBool("search_exclude_bots", false)
		f["only_my_channels"] = c.PrefBool("search_only_my_channels", false)
	}
	return f
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
	sorted := srTabHasSort(v.tab)
	if sorted {
		x := px
		d := at(gtx, image.Pt(x, ry), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg2, nsSearch.T("Sorted by")+" ")
			})
		})
		x += d.Size.X
		label := nsSearch.T("Most relevant (default)")
		switch v.sortBy {
		case "timestamp":
			label = nsSearch.T("Newest")
		case "oldest":
			label = nsSearch.T("Oldest")
		}
		if v.tab != srTabMessages {
			label = v.tabSortLabel()
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
	fw := textW(gtx, th, 13, theme.Regular, nsSearch.T("Give feedback"))
	at(gtx, image.Pt(right-dp(gtx, 23)-fw, ry), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &v.feedback, nil, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Link, nsSearch.T("Give feedback"))
			})
		})
	})
	cy := ry + dp(gtx, 21)
	if !sorted {
		cy = ry
	}
	count := ""
	if src := v.woSource(); src != nil {
		v.woToggle(gtx, th, cl, right-dp(gtx, 23)-fw-dp(gtx, 16)-dp(gtx, 56), ry-dp(gtx, 4))
	}
	switch {
	case v.woSource() != nil:
		count = srResultsLabel(len(v.woSource().Hits))
	case v.loading && v.total == 0:
		count = nsSearch.T("Searching…")
	case v.err != "":
		count = "Search failed: " + v.err
	default:
		count = srResultsLabel(v.total)
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
	if !sorted {
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
			return text(gtx, th, 18, theme.Black, cl.Fg, nsSearch.T("Search"))
		})
	}
	d := vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 18, theme.Regular, cl.Fg, srResultsFor())
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

// pill draws one filter pill at (x, y) and returns its width.
func (v *SearchView) pill(gtx layout.Context, th *theme.Theme, cl *Colors, i, x, y int, iconName, label string, chevron, active bool) int {
	d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return Pill(gtx, th, &v.pills[i], iconName, label, chevron, active)
	})
	v.pillR[i] = image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x, y).Add(d.Size)}
	return d.Size.X
}

// srResultsLabel is "291,727 results": the results header's
// "{displayString} {count, plural, =1 {result} other {results}}" (c9v/,
// the search I18n) with the count in the locale's grouping.
func srResultsLabel(n int) string {
	if l := i18n.Locale(); l != i18n.EnUS {
		return nsSearch.T("{displayString} {count, plural, =1 {result} other {results}}", "displayString", i18n.FormatNumber(l, float64(n)), "count", n)
	}
	unit := " results"
	if n == 1 {
		unit = " result"
	}
	d := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range d {
		if i > 0 && (len(d)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String() + unit
}

// srNothingTitle and srNothingDesc are Slack's no-results state
// (GenericSearchEmptyState: "Nothing turned up"; with the feedback link
// allowed, "Not the results you expected? Give feedback." comes first, a
// line of its own). Slack's Slackbot variant, for teams with AI search,
// words it "check for typos, or adjust filters. Or try asking Slackbot:".
const (
	srNothingTitle = "Nothing turned up"
	srNothingDesc  = "You may want to try using different keywords, checking for typos, or adjusting filters."
	srNothingAsk   = "Not the results you expected? "
)

// srResultsFor is the title's "Results for:" (selectFiltersData) with the
// space before the query.
func srResultsFor() string {
	t := nsSearch.T("Results for:")
	if !strings.HasSuffix(t, " ") {
		t += " "
	}
	return t
}

// srNothing draws GenericSearchEmptyState centred in w at y: the title
// (18 Black), then the two description lines (15/22 secondary), the first
// with its "Give feedback" link (fb). It returns the height used.
func srNothing(gtx layout.Context, th *theme.Theme, cl *Colors, x, y, w int, fb *widget.Clickable) int {
	title := nsSearch.T(srNothingTitle)
	tw := textW(gtx, th, 18, theme.Black, title)
	at(gtx, image.Pt(x+(w-tw)/2, y), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 18, theme.Black, cl.Fg, title)
	})
	y += dp(gtx, 32)
	lh := dp(gtx, 22)
	ask := nsSearch.T("Not the results you expected?") + " " // FeedbackLink's (RhHp)
	feedback := nsSearch.T("Give feedback")
	aw := textW(gtx, th, 15, theme.Regular, ask)
	lw := textW(gtx, th, 15, theme.Regular, feedback)
	dw := textW(gtx, th, 15, theme.Regular, ".")
	lx := x + (w-aw-lw-dw)/2
	at(gtx, image.Pt(lx, y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, cl.Fg2, ask)
		})
	})
	at(gtx, image.Pt(lx+aw, y), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, fb, nil, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Link, feedback)
			})
		})
	})
	at(gtx, image.Pt(lx+aw+lw, y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, cl.Fg2, ".")
		})
	})
	y += lh
	sw := minInt(w, dp(gtx, 420))
	d := at(gtx, image.Pt(x+(w-sw)/2, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(sw, 1<<20)}
		return paraCentered(gtx, th, 15, 22, cl.Fg2, nsSearch.T(srNothingDesc), sw)
	})
	return y + d.Size.Y
}

func (v *SearchView) results(gtx layout.Context, th *theme.Theme, cl *Colors, px int, sz image.Point) layout.Dimensions {
	if v.woSource() != nil { // WorkObjectResults (search_workobjects.go)
		return v.woResults(gtx, th, cl, px, sz)
	}
	cw := sz.X - 2*px
	n := 0
	switch v.tab {
	case srTabMessages, srTabDMs:
		n = len(v.cards)
	case srTabFiles, srTabCanvases:
		n = len(v.files)
	case srTabChannels:
		n = len(v.chans)
	case srTabPeople:
		n = len(v.people)
	}
	if n == 0 {
		if v.loading || v.err != "" {
			return layout.Dimensions{Size: sz}
		}
		if strings.TrimSpace(v.q.String()) == "" {
			return layout.Dimensions{Size: sz}
		}
		y := srNothing(gtx, th, cl, px, dp(gtx, 72), cw, &v.emptyFb)
		if v.tab == srTabMessages && v.unfiltered > 0 {
			// RelaxationSuggestion: container__LKaiN (column, centred,
			// min-height 84, margin-top 16) holding the outline medium
			// "Search without filters"
			call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
				return Button(gtx, th, &v.relaxBtn, BtnOutline, BtnMedium, nsSearch.T("Search without filters"), "", 0)
			})
			t := op.Offset(image.Pt(px+(cw-d.Size.X)/2, y+dp(gtx, 16))).Push(gtx.Ops)
			call.Add(gtx.Ops)
			t.Pop()
		}
		return layout.Dimensions{Size: sz}
	}
	pager := v.pages > 1
	items := n
	if pager {
		items++
	}
	switch v.tab {
	case srTabChannels:
		v.rowH = dp(gtx, 64)
	case srTabPeople:
		v.rowH = dp(gtx, 60)
	case srTabFiles, srTabCanvases:
		v.rowH = dp(gtx, 64)
	}
	return v.list.Layout(gtx, items, func(gtx layout.Context, i int) layout.Dimensions {
		if i == n {
			return v.pagination(gtx, th, cl, sz.X)
		}
		switch v.tab {
		case srTabChannels:
			return v.chanRow(gtx, th, cl, i, px, cw)
		case srTabPeople:
			return v.personRow(gtx, th, cl, i, px, cw)
		case srTabFiles, srTabCanvases:
			return v.fileRow(gtx, th, cl, i, px, cw)
		}
		return at(gtx, image.Pt(px, 0), func(gtx layout.Context) layout.Dimensions {
			d := v.card(gtx, th, cl, i, cw)
			v.cards[i].h = d.Size.Y + dp(gtx, 8)
			return layout.Dimensions{Size: image.Pt(px+cw, v.cards[i].h)}
		})
	})
}

// paraCentered draws text wrapped in w with each line centered.
func paraCentered(gtx layout.Context, th *theme.Theme, size, lh float32, c color.NRGBA, s string, w int) layout.Dimensions {
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, wd := range words {
		try := wd
		if cur != "" {
			try = cur + " " + wd
		}
		if cur != "" && textW(gtx, th, size, theme.Regular, try) > w {
			lines = append(lines, cur)
			cur = wd
			continue
		}
		cur = try
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	y := 0
	for _, ln := range lines {
		lw := textW(gtx, th, size, theme.Regular, ln)
		l := ln
		at(gtx, image.Pt((w-lw)/2, y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, dp(gtx, lh), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, size, theme.Regular, c, l)
			})
		})
		y += dp(gtx, lh)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// srPageRange is getCurrentPageRange: up to five pages around page.
func srPageRange(page, pages int) []int {
	if pages <= 0 {
		return nil
	}
	start := max(1, min(page-2, pages-4))
	var out []int
	for p := start; p <= pages && len(out) < 5; p++ {
		out = append(out, p)
	}
	return out
}

// pagination is Slack's Pagination under the results: round caret
// buttons around up to five page chips (20dp tall, radius 4; the current
// page white on the highlight blue, the others secondary text), with
// 40dp of margin above and below (margin_top_100 / margin_bottom_100).
func (v *SearchView) pagination(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	pages := srPageRange(v.page, v.pages)
	for i := range v.pageNums {
		v.pageNums[i] = 0
	}
	btn := dp(gtx, 28)
	chipH := dp(gtx, 20)
	gap := dp(gtx, 4)
	widths := make([]int, len(pages))
	total := 2*btn + 2*dp(gtx, 8)
	for i, p := range pages {
		widths[i] = maxInt(dp(gtx, 20), textW(gtx, th, 13, theme.Bold, strconv.Itoa(p))+dp(gtx, 12))
		total += widths[i]
		if i > 0 {
			total += gap
		}
	}
	h := dp(gtx, 40)*2 + btn
	x := (w - total) / 2
	y := dp(gtx, 40)
	caret := func(c *widget.Clickable, name string, disabled bool) {
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			fg := cl.Fg
			if disabled {
				fg = cl.Subtle
			}
			return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
				r := image.Rect(0, 0, btn, btn)
				if c.Hovered() && !disabled {
					rrect(gtx, r, btn/2, cl.BgHover)
				}
				stroke(gtx, r, btn/2, maxInt(1, dp(gtx, 1)), cl.BtnBorder)
				is := dp(gtx, 16)
				at(gtx, image.Pt((btn-is)/2, (btn-is)/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, name, 16, fg)
				})
				return layout.Dimensions{Size: r.Max}
			})
		})
		x += btn + dp(gtx, 8)
	}
	caret(&v.prevBtn, "caret-left", v.page <= 1)
	for i, p := range pages {
		cur := p == v.page
		v.pageNums[i] = p
		cw := widths[i]
		label := strconv.Itoa(p)
		c := &v.pageBtns[i]
		at(gtx, image.Pt(x, y+(btn-chipH)/2), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
				r := image.Rect(0, 0, cw, chipH)
				fg := cl.Fg2
				switch {
				case cur:
					rrect(gtx, r, dp(gtx, 4), cl.Highlight)
					fg = cl.HighText
				case c.Hovered():
					rrect(gtx, r, dp(gtx, 4), cl.BgHover)
					fg = cl.Fg
				}
				tw := textW(gtx, th, 13, theme.Bold, label)
				at(gtx, image.Pt((cw-tw)/2, 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, chipH, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 13, theme.Bold, fg, label)
					})
				})
				return layout.Dimensions{Size: r.Max}
			})
		})
		x += cw + gap
	}
	x += dp(gtx, 8) - gap
	caret(&v.nextBtn, "caret-right", v.page >= v.pages)
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// fileRow is a Files / Canvases result: the type tile, the title, and
// "Name · date" under it.
func (v *SearchView) fileRow(gtx layout.Context, th *theme.Theme, cl *Colors, i, px, cw int) layout.Dimensions {
	f := v.files[i]
	h := v.rowH
	return at(gtx, image.Pt(px, 0), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &v.rows[i], nil, func(gtx layout.Context) layout.Dimensions {
			r := image.Rect(0, 0, cw, h-dp(gtx, 8))
			bg := cl.Bg
			if v.rows[i].Hovered() {
				bg = cl.BgHover
			}
			rrect(gtx, r, dp(gtx, 12), bg)
			stroke(gtx, r, dp(gtx, 12), maxInt(1, dp(gtx, 1)), cl.Divider)
			tile := dp(gtx, 36)
			ty := (r.Dy() - tile) / 2
			ic := "files"
			switch {
			case f.IsCanvas:
				ic = "canvas"
			case f.IsList:
				ic = "list-tab"
			case f.IsImage:
				ic = "image"
			}
			rrect(gtx, image.Rect(dp(gtx, 12), ty, dp(gtx, 12)+tile, ty+tile), dp(gtx, 8), cl.BgHover)
			at(gtx, image.Pt(dp(gtx, 12)+(tile-dp(gtx, 20))/2, ty+(tile-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, ic, 20, cl.Fg2)
			})
			tx := dp(gtx, 12) + tile + dp(gtx, 12)
			tw := maxInt(1, cw-tx-dp(gtx, 12))
			title := f.Title
			if title == "" {
				title = f.Name
			}
			at(gtx, image.Pt(tx, ty-dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 22))}
				return text(gtx, th, 15, theme.Black, cl.Fg, title)
			})
			sub := ""
			if u, ok := lookupUser(v.h, f.User); ok {
				sub = userName(u)
			}
			if f.Created > 0 {
				when := srShortDay(time.Unix(f.Created, 0), time.Now())
				if sub != "" {
					sub += " · " + when
				} else {
					sub = when
				}
			}
			at(gtx, image.Pt(tx, ty+dp(gtx, 20)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 18))}
				return text(gtx, th, 13, theme.Regular, cl.Fg2, sub)
			})
			return layout.Dimensions{Size: image.Pt(cw, h)}
		})
	})
}

// openFile opens a file result like the Files browser does.
func (v *SearchView) openFile(f core.File) { srOpenFile(v.h, f) }

// srOpenFile opens a file or canvas result the way Slack's results do: a
// canvas in the canvas view (openCanvas; Clack's "docs/<id>"), a list in
// the list view ("lists/<id>"), anything else through previewFile, whose
// media viewer is Clack's lightbox for images. Slack shows other files in
// its file details flexpane, which Clack doesn't have: they open like a
// message's file card does, at their permalink.
func srOpenFile(h Host, f core.File) {
	title := f.Title
	if title == "" {
		title = f.Name
	}
	switch {
	case f.IsCanvas && f.ID != "":
		h.Navigate("docs/" + f.ID)
	case f.IsList && f.ID != "":
		h.Navigate("lists/" + f.ID)
	case f.IsImage && f.URL != "":
		h.OpenLightbox(richtext.Image{URL: f.URL, Alt: title, W: f.ThumbW, H: f.ThumbH, Title: title, Proxy: true}, core.Message{})
	case f.Permalink != "":
		lbOpen(h, f.Permalink)
	case f.URL != "":
		lbOpen(h, f.URL)
	}
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

// listItemProps are MessageListItem's: its result is the store's row as
// plugins present it (search_store.go), else the card's own.
func (a srCardArgs) listItemProps() slackc.Props {
	m := &a.v.msgs[a.i]
	r := a.v.storeResult(a.i)
	if r == nil {
		r = srResultRow(slackMessage(m))
	}
	return slackc.Props{
		"$key":    m.Channel + "/" + m.TS,
		"index":   a.i,
		"variant": "search",
		"result":  r,
	}
}

func (a srCardArgs) layoutListItem(gtx layout.Context, p slackc.Props) layout.Dimensions {
	r := p.Map("result")
	if r == nil {
		// unpatched MessageListItem: the row still comes from the store,
		// where a slice patch may have changed it
		r = a.v.storeResult(a.i)
	}
	if r != nil {
		if l := r.List("messages"); len(l) > 0 {
			a.over = asProps(l[0])
		}
	}
	a.v.overText(a.i, a.over)
	return slackc.Component(gtx, "SearchResult", a, srCardArgs.resultProps, srCardArgs.layoutResult)
}

// overText makes the card show a plugin's copy of the result's text
// (MessageListItem's result.messages: Slick Censorship's masked words), as
// Slack renders the card from it; the card's own text again when it's
// gone. Parsed once per change.
func (v *SearchView) overText(i int, over slackc.Props) {
	c := &v.cards[i]
	m := &v.msgs[i]
	t, _ := over.Str("text")
	if t == m.Text {
		t = ""
	}
	if t == c.ovText {
		return
	}
	c.ovText = t
	body := srBody(m)
	if t != "" {
		mm := *m
		mm.Text, mm.Doc = t, richtext.Doc{}
		if app := v.h.Core(); app != nil && app.S != nil {
			raw, _ := json.Marshal(over["blocks"])
			if string(raw) == "null" {
				raw = nil
			}
			mm.Doc = richtext.FromMessage(app.S, &slack.Message{Type: "message", TS: m.TS, User: m.User, Text: t, Blocks: raw})
		}
		body = srBody(&mm)
	}
	terms := v.q.Highlights()
	c.flat, c.full = srHighlight(srFlatten(body), terms), srHighlight(strings.TrimSpace(body), terms)
	c.tokW = -1 // wrap again
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
func (v *SearchView) cardNative(gtx layout.Context, th *theme.Theme, cl *Colors, i, w int, name, avURL string, rp slackc.Props) layout.Dimensions {
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
			more := textW(gtx, th, 13, theme.Regular, " … "+nsSearch.T("Show more")) + dp(gtx, 4)
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
		d := srAvatar(gtx, v.h, m, avURL, 36, rp)
		c.avHov.LayoutBot(gtx, m.User, srHoverBot(m), d.Size)
		return d
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
		d := vcenter(gtx, nameH, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Black, cl.Fg, name)
		})
		c.nameHov.LayoutBot(gtx, m.User, srHoverBot(m), d.Size)
		return d
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
					return text(gtx, th, 13, theme.Regular, cl.Fg3, "… "+nsSearch.T("Show more"))
				})
			})
		})
	case c.expanded:
		at(gtx, image.Pt(tx, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &c.more, nil, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, dp(gtx, 20), func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg3, nsSearch.T("Show less"))
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
		{&c.fwd, &c.tipFwd, "forward-msg", nsMessage.T("Forward message…")},
		{&c.jump, &c.tipJump, "external", nsSearch.T("Open in home")},
		{&c.kebab, &c.tipMore, "kebab", nsSearch.T("More actions")},
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
	}}.In(nsMessage)
	if self != "" && (m.User == self || slices.Contains(m.ReplyUsers, self)) {
		follow = MenuItem{Label: "Turn off notifications for replies", Icon: "bell-off", Do: func() {
			actDo(h, "You won't be notified about new replies", func(ctx context.Context, a *core.App) error {
				return a.SubscribeThread(ctx, ch, root, false)
			})
		}}.In(nsMessage)
	}
	rems := actReminderItems(remind, nil)
	return []MenuItem{
		MenuItem{Label: "Add to saved", Icon: "bookmark-add", Hint: shortcut.Format("Mod+Shift+S"), Keys: KeySaveTodos, Do: func() {
			actDo(h, "Saved for later", func(ctx context.Context, a *core.App) error { return a.Save(ctx, ch, ts, 0) })
		}}.In(nsSaved),
		MenuItem{Label: "Add a reminder", Icon: "remind", Hint: nsMessage.T("M"), Keys: KeyRemind, Sub: rems[:len(rems)-1]}.In(nsMessage), // no Custom… here
		follow,
		Sep(),
		MenuItem{Label: "Copy link", Icon: "link", Hint: "L", Keys: KeyCopyLink, DoCtx: func(gtx layout.Context) {
			copyText(gtx, link)
			h.Toast("Link copied")
		}}.In(nsMessage),
		MenuItem{Label: "Copy message", Icon: "text-format", Hint: shortcut.Format("Mod+C"), Keys: KeyCopyText, DoCtx: func(gtx layout.Context) {
			copyText(gtx, actMsgText(m))
		}}.In(nsMessage),
		Sep(),
		MenuItem{Label: "Connect to apps", Sub: []MenuItem{{Label: "No apps", Disabled: true}}}.In(nsMessage),
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
		label := nsSearch.T("View")
		if !ch.IsMember {
			label = nsSearch.T("Join")
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
					name += " " + nsSearch.T("(archived)") // ChannelName's
				}
				return text(gtx, th, 15, theme.Bold, cl.Fg, name)
			})
		})
		sub := plural(ch.NumMembers, "member")
		if i18n.Locale() != i18n.EnUS {
			sub = nsSearch.T("{count, plural, =1 {# member} other {# members}}", "count", ch.NumMembers) // BrowsePageChannelSubtext
		}
		if ch.IsMember {
			sub = nsSearch.T("Joined") + " · " + sub
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

// srHoverBot is the bot a result's sender opens AppHoverCard for when the
// message has no user.
func srHoverBot(m *core.Message) string {
	if m.User != "" {
		return ""
	}
	if m.BotID != "" {
		return m.BotID
	}
	if m.BotProfile != nil {
		return m.BotProfile.ID
	}
	return ""
}
