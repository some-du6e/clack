package views

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"slices"
	"sort"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Home sidebar's Huddles page, ported from Slack's: the route's view
// HuddlesNewUserPage (gantry-v2 huddles-page chunk a29464bf75cbb4a4,
// module Igs2) under its header HuddlesPageHeader (boot-render rLwU), as
// the IA4 pages router mounts them (boot-render jbC3, e3.c.Huddles). The
// page is one virtual list of renderHuddlePageRow's rows:
//
//	HuddlePageHero              HuddlesPageNewUserHero (huddles_hero.go)
//	HuddlePageHeaderMegaphone…  megaphone notices (none: no megaphone API)
//	SuggestionsOrActiveContent  HuddlesActiveSection or the DM suggestions
//	UpcomingHuddlesSection      calendar events (external_huddles only)
//	PastHuddlesTitle            "Recent huddles" (huddle_page_enhancements off)
//	HuddlesPageFilters          FilterRow (huddles_filters.go)
//	ListSkeleton                while a filter's fetch runs
//	NoFilterResults             "No results"
//	HuddlesPageChannelContent   channel suggestions, with no history
//	RelativeTime                day groups (huddle_page_enhancements on)
//	HuddlesHistoryListItem      the rows (huddles_history.go)
//	ListFooter                  64px, the spinner while the next page loads
//
// Data comes the way Slack's page gets it (core/huddles_page.go):
// huddles.history (limit 30; the filters' args; the cursor as the list
// nears its end), huddles.get for each live huddle's card,
// huddles.listBackgrounds once, users.interactions for the hero, and the
// People browser's first page (search.modules.people) for the DM
// suggestions. Live huddles come from sh_room_* events and edge
// huddles/list. Starts and joins go through Slack's startHuddle (the
// ui root's JoinHuddleFrom with Slack's entry points, else startHuddle).

// hdNS is Slack's new I18n("huddles").
var hdNS = i18n.NS("huddles")

// huddleRoom is an active or past huddle.
type huddleRoom = core.Huddle

// rawRoom is the room object carried by sh_room_* realtime events, which
// core forwards raw (Event.Raw).
type rawRoom struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	CreatedBy          string   `json:"created_by"`
	DateStart          int64    `json:"date_start"`
	DateEnd            int64    `json:"date_end"`
	Participants       []string `json:"participants"`
	ParticipantHistory []string `json:"participant_history"`
	Channels           []string `json:"channels"`
	IsDMCall           bool     `json:"is_dm_call"`
	HasEnded           bool     `json:"has_ended"`
}

// roomFromEvent decodes the room of an sh_room_* event.
func roomFromEvent(raw json.RawMessage) (huddleRoom, bool) {
	var ev struct {
		Room   *rawRoom `json:"room"`
		Huddle *rawRoom `json:"huddle"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return huddleRoom{}, false
	}
	r := ev.Room
	if r == nil {
		r = ev.Huddle
	}
	if r == nil || r.ID == "" {
		return huddleRoom{}, false
	}
	return huddleRoom{ID: r.ID, Name: r.Name, CreatedBy: r.CreatedBy, Channels: r.Channels,
		Participants: r.Participants, History: r.ParticipantHistory, Start: r.DateStart, End: r.DateEnd,
		Active: r.DateEnd == 0 && !r.HasEnded && len(r.Participants) > 0, DM: r.IsDMCall}, true
}

// hdHistory is Slack's huddleHistory slice (client-boot-shared jNSy) and
// the page's loading flags.
type hdHistory struct {
	loading    bool          // isLoading: the page is its spinner until the first fetch lands
	huddles    []core.Huddle // getHuddleHistory
	cursor     string        // getHuddleHistoryCursor
	unfiltered []core.Huddle // getUnfilteredHuddlesList
	err        error         // caughtError
	filterBusy bool          // isHuddleHistoryLoadingAfterFilterSet
	inFlight   *core.HuddleFilters
	paging     bool   // isPaginating
	pageFailed string // the cursor whose page failed (Slack doesn't ask for it again)
}

// hdFilters is the slice's filters ({type, with, in, usersOnly, date, has}).
type hdFilters struct {
	typ           string   // "all" (default) | "missed"
	with          []string // member ids
	in            []string // channel ids
	usersOnly     bool
	after, before string   // the date filter (YYYY-MM-DD), "" for none
	has           []string // meeting_notes | thread | files_and_links
}

// count is calculateHuddleFilterCount: with, in, date and has that are
// set.
func (f *hdFilters) count() int {
	n := 0
	for _, on := range []bool{len(f.with) > 0, len(f.in) > 0, f.after != "" || f.before != "", len(f.has) > 0} {
		if on {
			n++
		}
	}
	return n
}

// resettable is getHasResettableHuddleHistoryFilters.
func (f *hdFilters) resettable() bool {
	return f.count() > 0 || (f.typ != "" && f.typ != "all")
}

// api is transformFiltersForApi (DQb+): the date filter's after/before
// become the start of the day after and the end of the day before.
func (f *hdFilters) api() *core.HuddleFilters {
	out := &core.HuddleFilters{Users: f.with, Channels: f.in, MissedOnly: f.typ == "missed",
		UsersOnly: len(f.with) > 0 && f.usersOnly}
	if t, err := time.ParseInLocation("2006-01-02", f.after, time.Local); err == nil {
		out.StartTS = formatInt(t.AddDate(0, 0, 1).Unix())
	}
	if t, err := time.ParseInLocation("2006-01-02", f.before, time.Local); err == nil {
		out.EndTS = formatInt(t.Add(-time.Second).Unix()) // the end of the day before
	}
	for _, h := range f.has {
		switch h {
		case "meeting_notes":
			out.HasNotes = true
		case "thread":
			out.HasThread = true
		case "files_and_links":
			out.HasArtifacts = true
		}
	}
	return out
}

func sameFilters(a, b *core.HuddleFilters) bool {
	if a == nil || b == nil {
		return a == b
	}
	return slices.Equal(a.Users, b.Users) && slices.Equal(a.Channels, b.Channels) && a.StartTS == b.StartTS &&
		a.EndTS == b.EndTS && a.MissedOnly == b.MissedOnly && a.UsersOnly == b.UsersOnly && a.HasNotes == b.HasNotes &&
		a.HasThread == b.HasThread && a.HasArtifacts == b.HasArtifacts
}

// Huddles is the Home-sidebar Huddles page (route "huddles").
type Huddles struct {
	base

	mu      sync.Mutex
	live    map[string]huddleRoom // by room id (sh_room_* events, edge huddles/list)
	info    map[string]huddleRoom // huddles.get answers by room id (useHuddleInfo)
	asked   map[string]bool       // huddles.get asked
	missing map[string]bool       // channels conversations.info doesn't know (isNonExistent)
	sub     bool

	hist    hdHistory
	filters hdFilters
	mounted bool

	bgs     []core.HuddleBackground // huddlesCanvas.backgrounds
	inter   map[string]bool         // users.interactions recorded (dismissed banners)
	interOK bool                    // loadUserInteractions finished
	heroOff bool                    // HeroBanner's showBanner, cleared by its close button

	people async[[]core.User] // the People browser's first page (DM suggestions)

	list     widget.List
	backTop  bool // showBackToTop
	topHeld  bool // handleBackToTop ran: scrolling doesn't bring the button back until the top
	moreLive bool // "Show more live huddles"
	pageW    int  // the page's width (huddlePageRef's ResizeObserver width)

	clicks map[string]*widget.Clickable
	tips   map[string]*popups.TipState
	toks   hdTokCache

	// joinFn is the onHuddleJoin a HuddleInviteWindowWithPreviewContent
	// patch handed the Join point, by room (slick.go).
	joinFn map[string]slackc.JSFunc
}

// NewHuddles builds the Huddles view (route "huddles").
func NewHuddles(h Host) *Huddles {
	v := &Huddles{base: newBase(h), live: map[string]huddleRoom{}, info: map[string]huddleRoom{},
		asked: map[string]bool{}, missing: map[string]bool{}, clicks: map[string]*widget.Clickable{},
		tips: map[string]*popups.TipState{}}
	v.list.Axis = layout.Vertical
	v.filters.typ = "all"
	return v
}

// clk is the clickable for key (created on first use).
func (v *Huddles) clk(key string) *widget.Clickable {
	c := v.clicks[key]
	if c == nil {
		c = new(widget.Clickable)
		v.clicks[key] = c
	}
	return c
}

func (v *Huddles) tip(key string) *popups.TipState {
	t := v.tips[key]
	if t == nil {
		t = &popups.TipState{}
		v.tips[key] = t
	}
	return t
}

// exp reads one of the page's experiments ("on" is the only group
// Slack's code tells apart for these).
func (v *Huddles) exp(name string) bool { return v.h.Core().Experiment(name) == "on" }

// enhancements is huddle_page_enhancements (pSxm cc).
func (v *Huddles) enhancements() bool { return v.exp("huddle_page_enhancements") }

// ---- tokens ----

// hdTokCache holds the frame's resolved design tokens.
type hdTokCache struct {
	th  *theme.Theme
	gen uint64
	m   map[string]color.NRGBA
}

// tok is the design token's colour as Slack's stylesheet defines it in
// this mode (slackc/css's captured :root values), with a Slick theme or
// user.css override on top.
func (v *Huddles) tok(name string) color.NRGBA {
	if v.toks.th != v.th || v.toks.gen != v.th.Gen || v.toks.m == nil {
		v.toks = hdTokCache{th: v.th, gen: v.th.Gen, m: map[string]color.NRGBA{}}
	}
	if c, ok := v.toks.m[name]; ok {
		return c
	}
	c, _ := css.ParseColor(css.SlackVars(v.th.Dark)["--"+name])
	theme.SetColor(v.th.Tokens, &c, "var(--"+name+")")
	v.toks.m[name] = c
	return c
}

// tokOver is tok flattened over bg (Gio blends in linear RGB).
func (v *Huddles) tokOver(name string, bg color.NRGBA) color.NRGBA { return flatten(v.tok(name), bg) }

// ---- live huddles ----

func (v *Huddles) subscribe() {
	if v.sub {
		return
	}
	v.sub = true
	ch, _ := v.h.Core().Subscribe()
	go func() {
		for ev := range ch {
			if ev.Type != "huddle" {
				continue
			}
			r, ok := roomFromEvent(ev.Raw)
			if !ok {
				continue
			}
			if len(r.Channels) == 0 && ev.Channel != "" {
				r.Channels = []string{ev.Channel}
			}
			v.mu.Lock()
			if r.Active {
				v.live[r.ID] = r
			} else {
				delete(v.live, r.ID)
				delete(v.asked, r.ID) // a new call there asks again
			}
			v.mu.Unlock()
			v.h.Core().EnsureUsers(context.Background(), r.Participants)
			v.h.Invalidate()
		}
	}()
}

// liveRooms are the live huddles, newest first.
func (v *Huddles) liveRooms() []huddleRoom {
	v.mu.Lock()
	out := make([]huddleRoom, 0, len(v.live))
	for _, r := range v.live {
		out = append(out, r)
	}
	v.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start > out[j].Start
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// pageLive is the live huddles the page shows (b0mi eE over eC over eS):
// a call with someone in it, in a conversation you're a member of (or
// the one you're invited to), not muted; the one you're in first
// (HuddlesActiveSection moves activeHuddleChannelId's call to the front).
func (v *Huddles) pageLive() []huddleRoom {
	app := v.h.Core()
	active := v.activeChannel()
	var out []huddleRoom
	for _, r := range v.liveRooms() {
		ch := roomChannel(r)
		if ch == "" || len(r.Participants) == 0 {
			continue
		}
		c, ok := app.Conv(ch)
		member := ok && !c.Preview
		if !member && ch != active {
			continue
		}
		if ok && c.Muted {
			continue
		}
		out = append(out, r)
	}
	for i, r := range out {
		if roomChannel(r) == active && i > 0 {
			out = append([]huddleRoom{r}, append(out[:i:i], out[i+1:]...)...)
			break
		}
	}
	return out
}

// huddleInfo is useHuddleInfo (osUg): the huddle object from huddles.get,
// asked once per call.
func (v *Huddles) huddleInfo(r huddleRoom) (huddleRoom, bool) {
	v.mu.Lock()
	h, ok := v.info[r.ID]
	asked := v.asked[r.ID]
	if !asked {
		v.asked[r.ID] = true
	}
	v.mu.Unlock()
	if !asked {
		app, id := v.h.Core(), r.ID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			got, err := app.HuddleInfo(ctx, id)
			if err != nil {
				return
			}
			v.mu.Lock()
			v.info[id] = got
			v.mu.Unlock()
			v.h.Invalidate()
		}()
	}
	return h, ok
}

// Hosts that know about the huddle you're in (the ui root).
type (
	huddleEntryStarter interface{ JoinHuddleFrom(ch, entry string) }
	huddleActive       interface{ ActiveHuddleChannel() string }
	huddleLeaver       interface{ LeaveHuddle() }
)

// activeChannel is activeHuddleChannelId: where the huddle you're in is.
func (v *Huddles) activeChannel() string {
	if a, ok := v.h.(huddleActive); ok {
		return a.ActiveHuddleChannel()
	}
	return ""
}

// begin is a start or join from one of the page's entry points: Slack's
// startHuddle with that entry point.
func (v *Huddles) begin(ch, entry string) {
	if ch == "" {
		return
	}
	if s, ok := v.h.(huddleEntryStarter); ok && !v.tt.ThunkPatched("startHuddle") {
		s.JoinHuddleFrom(ch, entry)
		return
	}
	startHuddle(v.h, v.tt, startHuddleArgs{ChannelID: ch})
}

// ---- loading ----

// mount is what the page does as it mounts: useHuddleHistory's fetch when
// the history is empty and no filter is set (else it shows what's
// there), maybeFetchBackgrounds without backgrounds (QxOD) and the hero's
// loadUserInteractions.
func (v *Huddles) mount() {
	if v.mounted {
		return
	}
	v.mounted = true
	app := v.h.Core()
	v.mu.Lock()
	need := len(v.hist.huddles) == 0 && !v.filters.resettable()
	v.hist.loading = need
	v.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if live, err := app.ActiveHuddles(ctx); err == nil {
			v.mu.Lock()
			for _, r := range live {
				if r.Active {
					v.live[r.ID] = r
				}
			}
			v.mu.Unlock()
		}
		if need {
			hs, next, err := app.HuddleHistoryPage(ctx, core.HuddleHistoryOpts{})
			v.mu.Lock()
			v.hist.loading = false
			if err != nil {
				v.hist.err = err
			} else {
				v.hist.huddles, v.hist.cursor, v.hist.unfiltered = hs, next, hs
			}
			v.mu.Unlock()
			v.resolveChannels(ctx, hs)
		}
		v.h.Invalidate()
	}()
	go v.loadBackgrounds(v.tt)
	go v.loadInteractions()
}

// resolveChannels asks conversations.info for the history's
// conversations the client doesn't have (Slack's store fetches unknown
// channels); one it can't see is isNonExistent: "From a private
// conversation".
func (v *Huddles) resolveChannels(ctx context.Context, hs []core.Huddle) {
	app := v.h.Core()
	seen := map[string]bool{}
	for _, h := range hs {
		ch := roomChannel(h)
		if ch == "" || seen[ch] {
			continue
		}
		seen[ch] = true
		if _, ok := app.Conv(ch); ok {
			continue
		}
		if _, ok := app.ResolveConv(ctx, ch); !ok {
			v.mu.Lock()
			v.missing[ch] = true
			v.mu.Unlock()
		}
	}
	v.h.Invalidate()
}

// loadBackgrounds is maybeFetchBackgrounds (5BO1): huddles.listBackgrounds
// once, through the thunk so a plugin that wraps it sees it.
func (v *Huddles) loadBackgrounds(tt *taut.Taut) {
	v.mu.Lock()
	have := len(v.bgs) > 0
	v.mu.Unlock()
	if have {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	app := v.h.Core()
	native := func(ctx context.Context, a huddleArgs) (any, error) {
		bs, err := app.HuddleBackgrounds(ctx)
		if err != nil {
			return nil, err
		}
		v.mu.Lock()
		v.bgs = bs
		v.mu.Unlock()
		v.h.Invalidate()
		return nil, nil
	}
	if tt.ThunkPatched("maybeFetchBackgrounds") {
		_, _ = taut.Dispatch(ctx, tt, "maybeFetchBackgrounds", huddleArgs{}, native)
		return
	}
	_, _ = native(ctx, huddleArgs{})
}

// Slack's component and type names for the hero's interactions (Zu78).
const (
	hdBannerPage     = "HUDDLE_PAGE_BANNER"
	hdBannerExternal = "EXTERNAL_HUDDLES_AWARENESS_BANNER"
	hdDismiss        = "DISMISS"
)

// loadInteractions is the hero's loadUserInteractions({interactions:
// [HuddlePageBanner, ExternalHuddlesAwarenessBanner] × Dismiss}); the
// hero stays hidden until it answers.
func (v *Huddles) loadInteractions() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	got, err := v.h.Core().UserInteractions(ctx, [2]string{hdBannerPage, hdDismiss}, [2]string{hdBannerExternal, hdDismiss})
	v.mu.Lock()
	v.interOK = true
	if err == nil {
		v.inter = map[string]bool{}
		for k, it := range got {
			v.inter[k] = it.Count >= 1
		}
	}
	v.mu.Unlock()
	v.h.Invalidate()
}

// dismissed is hasRecordedInteraction(component, Dismiss).
func (v *Huddles) dismissed(component string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.inter[core.InteractionKey(component, hdDismiss, "")]
}

// refetch is filterHuddleHistory (DQb+ x): huddles.history for the
// filters as they stand, superseding a change still in flight, with the
// list skeleton up until it lands.
func (v *Huddles) refetch() {
	args := v.filters.api()
	v.mu.Lock()
	if v.hist.inFlight != nil && sameFilters(v.hist.inFlight, args) {
		v.mu.Unlock()
		return
	}
	v.hist.inFlight = args
	v.hist.filterBusy = true
	v.mu.Unlock()
	app := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		hs, next, err := app.HuddleHistoryPage(ctx, core.HuddleHistoryOpts{Filters: args})
		v.mu.Lock()
		if v.hist.inFlight != args { // superseded
			v.mu.Unlock()
			return
		}
		v.hist.inFlight = nil
		v.hist.filterBusy = false
		if err == nil {
			v.hist.huddles, v.hist.cursor, v.hist.pageFailed = hs, next, ""
			if !args.Set() {
				v.hist.unfiltered = hs
			}
		}
		v.mu.Unlock()
		if err == nil {
			v.resolveChannels(ctx, hs)
		}
		v.h.Invalidate()
	}()
}

// paginate is the list's loadPost: the next page (fetchAndUpsertHuddleHistory
// {cursor, ...transformFiltersForApi(filters)}), kept only if the cursor
// hasn't moved meanwhile; a page that failed isn't asked for again.
func (v *Huddles) paginate() {
	v.mu.Lock()
	cur := v.hist.cursor
	if cur == "" || v.hist.paging || v.hist.pageFailed == cur || v.hist.loading || v.hist.filterBusy {
		v.mu.Unlock()
		return
	}
	v.hist.paging = true
	v.mu.Unlock()
	args := v.filters.api()
	app := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		hs, next, err := app.HuddleHistoryPage(ctx, core.HuddleHistoryOpts{Cursor: cur, Filters: args})
		v.mu.Lock()
		v.hist.paging = false
		switch {
		case err != nil:
			v.hist.pageFailed = cur
		case v.hist.cursor == cur:
			v.hist.huddles = append(v.hist.huddles, hs...)
			v.hist.cursor = next
		}
		v.mu.Unlock()
		if err == nil {
			v.resolveChannels(ctx, hs)
		}
		v.h.Invalidate()
	}()
}

// pageState is a frame's copy of what the rows read.
type pageState struct {
	loading, filterBusy, paging bool
	err                         error
	ended                       []core.Huddle // filteredHuddlesHistory: has_ended only
	unfiltered                  []core.Huddle
}

func (v *Huddles) state() pageState {
	v.mu.Lock()
	defer v.mu.Unlock()
	st := pageState{loading: v.hist.loading, filterBusy: v.hist.filterBusy, paging: v.hist.paging, err: v.hist.err,
		unfiltered: v.hist.unfiltered}
	for _, h := range v.hist.huddles {
		if h.HasEnded || (h.End > 0 && !h.Active) {
			st.ended = append(st.ended, h)
		}
	}
	return st
}

// ---- rows ----

// hpKind is a page row's type (renderHuddlePageRow's cases).
type hpKind uint8

const (
	hpHero      hpKind = iota // HuddlePageHero
	hpActive                  // SuggestionsOrActiveContent → HuddlesActiveSection
	hpDMs                     // SuggestionsOrActiveContent → HuddlesPageDmSuggestions
	hpPastTitle               // PastHuddlesTitle
	hpFilters                 // HuddlesPageFilters
	hpSkeleton                // ListSkeleton
	hpNoResults               // NoFilterResults
	hpChannels                // HuddlesPageChannelContent
	hpDay                     // RelativeTime
	hpRow                     // HuddlesHistoryListItem
	hpFooter                  // ListFooter
)

type hpItem struct {
	kind        hpKind
	h           *core.Huddle // hpRow
	first, last bool         // hpRow: isFirstItem / isLastItem; hpDay: isFirstSection
	day         int          // hpDay: 0 Today … 4 Last month and older
}

// dayBucket is the RelativeTime bucket of a huddle's date_start: today,
// yesterday, this week, last week, else older (dayjs startOf day/week,
// weeks starting Sunday).
func dayBucket(start int64, now time.Time) int {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	week := day.AddDate(0, 0, -int(day.Weekday()))
	switch {
	case start >= day.Unix():
		return 0
	case start >= day.AddDate(0, 0, -1).Unix():
		return 1
	case start >= week.Unix():
		return 2
	case start >= week.AddDate(0, 0, -7).Unix():
		return 3
	}
	return 4
}

var hdDayLabels = [...]string{"Today", "Yesterday", "This week", "Last week", "Last month and older"}

// rows is useHuddlesPageRows: the list's rows in order.
func (v *Huddles) rows(st pageState, now time.Time) []hpItem {
	enh := v.enhancements()
	resettable := v.filters.resettable()
	items := []hpItem{{kind: hpHero}}
	if v.showActive(st) {
		items = append(items, hpItem{kind: hpActive})
	} else if !st.loading {
		items = append(items, hpItem{kind: hpDMs})
	}
	if len(st.ended) > 0 || resettable || st.filterBusy {
		if !enh {
			items = append(items, hpItem{kind: hpPastTitle})
		}
		items = append(items, hpItem{kind: hpFilters})
	}
	if st.filterBusy {
		items = append(items, hpItem{kind: hpSkeleton})
	}
	switch {
	case len(st.ended) == 0 && resettable && !st.filterBusy:
		items = append(items, hpItem{kind: hpNoResults})
	case len(st.ended) == 0 && !resettable && !st.filterBusy:
		items = append(items, hpItem{kind: hpChannels})
	}
	sections := enh && v.filters.after == "" && v.filters.before == ""
	prev := -1
	for i := range st.ended {
		h := &st.ended[i]
		b := -1
		if sections {
			b = dayBucket(h.Start, now)
			if b != prev {
				items = append(items, hpItem{kind: hpDay, day: b, first: prev == -1})
			}
		}
		first, last := i == 0, i == len(st.ended)-1
		if sections {
			first = b != prev
			last = i == len(st.ended)-1 || dayBucket(st.ended[i+1].Start, now) != b
		}
		prev = b
		items = append(items, hpItem{kind: hpRow, h: h, first: first, last: last})
	}
	return append(items, hpItem{kind: hpFooter})
}

// showActive is DMSuggestionsOrLiveHuddlesContent's choice: the active
// section while you're in a huddle or once there's history, a filter,
// live huddles or hide_suggested_huddles; else (history loaded and
// empty) the DM suggestions; nothing while the history loads.
func (v *Huddles) showActive(st pageState) bool {
	if v.activeChannel() != "" {
		return true
	}
	if st.loading {
		return false
	}
	return len(st.ended) > 0 || v.filters.resettable() || st.filterBusy || len(v.pageLive()) > 0 ||
		v.exp("hide_suggested_huddles")
}

// ---- layout ----

// hdColumn is the page's centred content column (.p-huddles_page_history_hhv
// and .p-huddles-page-content: border-box, max-width 968, padding 0 20px):
// the content's left edge and width.
func hdColumn(gtx C, w int) (x, cw int) {
	box := min(w, gtx.Dp(968))
	pad := gtx.Dp(20)
	return (w-box)/2 + pad, max(0, box-2*pad)
}

// Layout draws the view: the header (HuddlesPageHeader in the view's
// header slot) over the page.
func (v *Huddles) Layout(gtx C) D {
	v.frame()
	v.subscribe()
	v.mount()
	v.update(gtx)

	size := gtx.Constraints.Max
	v.pageW = size.X
	fill(gtx, image.Rectangle{Max: size}, v.tok("dt_color-base-sec")) // .p-huddles_page
	gtx.Constraints = layout.Exact(size)
	return col(gtx,
		layout.Rigid(v.layoutHeader),
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return v.layoutPage(gtx)
		}),
	)
}

func (v *Huddles) update(gtx C) {
	if v.clk("new").Clicked(gtx) {
		newHuddle(v.h, v.tt)
	}
	if v.clk("retry").Clicked(gtx) {
		v.mu.Lock()
		v.hist.err = nil
		v.mu.Unlock()
		v.mounted = false
	}
	if v.clk("top").Clicked(gtx) {
		v.list.Position = layout.Position{}
		v.backTop, v.topHeld = false, true
	}
}

// layoutPage is the page body: the spinner until the first history
// fetch lands, else the list and the back-to-top button.
func (v *Huddles) layoutPage(gtx C) D {
	st := v.state()
	sz := gtx.Constraints.Max
	if st.loading {
		// HuddlesNewUserPage while isLoading: the page's loader
		// (.p-huddles_page_channel_content__loader, margin-top 24)
		return v.loader(gtx, sz.X, gtx.Dp(24))
	}
	if st.err != nil {
		return v.errorState(gtx, st.err, v.clk("retry"))
	}
	items := v.rows(st, gtx.Now)
	before := v.list.Position
	d := v.scrollList(gtx, &v.list, len(items), func(gtx C, i int) D { return v.item(gtx, st, items[i]) })
	pos := v.list.Position
	// onScroll: scrolled away from the top shows the button (unless it
	// just took you there); at the top it goes
	atTop := pos.First == 0 && pos.Offset <= 0
	if atTop {
		v.backTop, v.topHeld = false, false
	} else if pos != before && !v.topHeld {
		v.backTop = true
	}
	// loadPost: the list reached its footer
	if pos.First+pos.Count >= len(items) {
		v.paginate()
	}
	if v.backTop {
		v.layoutBackToTop(gtx, sz)
	}
	return d
}

// item is renderHuddlePageRow.
func (v *Huddles) item(gtx C, st pageState, it hpItem) D {
	switch it.kind {
	case hpHero:
		return v.layoutHero(gtx)
	case hpActive:
		return v.layoutActive(gtx, st)
	case hpDMs:
		return v.layoutDMSuggestions(gtx)
	case hpPastTitle:
		return v.layoutPastTitle(gtx)
	case hpFilters:
		return v.layoutFilters(gtx)
	case hpSkeleton:
		return v.layoutSkeleton(gtx)
	case hpNoResults:
		return v.layoutNoResults(gtx)
	case hpChannels:
		return v.layoutChannelSuggestions(gtx)
	case hpDay:
		return v.layoutDay(gtx, it)
	case hpRow:
		return v.layoutHistoryRow(gtx, it)
	case hpFooter:
		return v.layoutFooter(gtx, st)
	}
	return D{}
}

// layoutHeader is HuddlesPageHeader in the route's .p-view_header:
// 49px, base-pry with a 1px otl-ter border below (.p-view_header:has(
// .p-huddles_header_wrapper)), padding 0 16px 0 18px; .p-huddles_header
// is centred, at most 968 wide, padded 0 16px when the view is at most
// 1024 wide (@container view-sidebar); the title "Huddles" (18px/24px,
// 900) on its left, HuddlesPageHeaderActions on its right.
func (v *Huddles) layoutHeader(gtx C) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(49)
	bg := v.tok("dt_color-base-pry")
	fill(gtx, image.Rect(0, 0, w, h), bg)
	px1 := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(0, h-px1, w, h), v.tokOver("dt_color-otl-ter", bg))
	inner := w - gtx.Dp(18) - gtx.Dp(16)
	box := min(inner, gtx.Dp(968))
	pad := 0
	if w <= gtx.Dp(1024) {
		pad = gtx.Dp(16)
	}
	x0 := gtx.Dp(18) + (inner-box)/2 + pad
	cw := max(0, box-2*pad)
	defer opOffset(gtx, image.Pt(x0, 0)).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(cw, h-px1))
	row(g, layout.Middle,
		layout.Flexed(1, func(gtx C) D {
			return v.label(gtx, 18, wBlack, v.tok("dt_color-content-pry"), hdNS.T("Huddles"))
		}),
		layout.Rigid(func(gtx C) D { return v.newHuddleButton(gtx) }),
	)
	return D{Size: image.Pt(w, h)}
}

// newHuddleButton is HuddlesPageHeaderActions' New Huddle: a
// ViewHeaderPrimaryButton (c-button small outline: 28px, padding 0 12px
// 1px, 13px bold, 1px otl-sec border, radius 4) holding the plus icon
// (4px before the label). Its click is useStartHuddleClick: without
// standalone huddles, HuddleInviteStartModal.
func (v *Huddles) newHuddleButton(gtx C) D {
	return v.outlineButton(gtx, v.clk("new"), "plus", hdNS.T("New Huddle"), true)
}

// outlineButton is Slack's c-button--outline: small (28px, 13px, radius
// 4) or medium (36px, 15px, radius 8), padding 0 12px 1px, min-width
// 56/80, an optional 20px icon 4px before the label.
func (v *Huddles) outlineButton(gtx C, c *widget.Clickable, icon, label string, small bool) D {
	hgt, size, minW, rad := gtx.Dp(36), unit.Sp(15), gtx.Dp(80), gtx.Dp(8)
	if small {
		hgt, size, minW, rad = gtx.Dp(28), unit.Sp(13), gtx.Dp(56), gtx.Dp(4)
	}
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		tw := v.th.Measure(gtx, size, wBold, label)
		iw := 0
		if icon != "" {
			iw = gtx.Dp(20) + gtx.Dp(4)
		}
		sz := image.Pt(max(minW, tw+iw+2*gtx.Dp(12)), hgt)
		r := image.Rectangle{Max: sz}
		bg := v.tok("dt_color-base-pry")
		rrect(gtx, r, rad, bg)
		if c.Hovered() {
			rrect(gtx, r, rad, v.tokOver("dt_color-base-pry-hover", bg))
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), v.tokOver("dt_color-otl-sec", bg))
		fg := v.tok("dt_color-content-pry")
		x := (sz.X - tw - iw) / 2
		if icon != "" {
			func() {
				defer opOffset(gtx, image.Pt(x, (hgt-gtx.Dp(20))/2)).Pop()
				v.icon(gtx, icon, 20, fg)
			}()
			x += iw
		}
		func() {
			defer opOffset(gtx, image.Pt(x, 0)).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(tw, hgt-gtx.Dp(1)))
			layout.W.Layout(g, func(gtx C) D { return v.label(gtx, size, wBold, fg, label) })
		}()
		return D{Size: sz}
	})
}

// layoutBackToTop is BackToTop under its tooltip ("Scroll to the top"):
// the green square (.greenSquare__ZCbZX: 42px, radius 11, base-inv-hgl-2
// with a border in the same colour, arrow-up 20 in content-inv-pry) at
// the bottom right of the page's 968 column (.backToTopButtonContainer:
// height 95%, padding 0 20px, flex-end).
func (v *Huddles) layoutBackToTop(gtx C, sz image.Point) {
	s := gtx.Dp(42)
	box := min(sz.X, gtx.Dp(968))
	right := (sz.X-box)/2 + box - gtx.Dp(20)
	bottom := sz.Y * 95 / 100
	at := image.Pt(right-s, bottom-s)
	c := v.clk("top")
	defer opOffset(gtx, at).Pop()
	c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		bg := v.tok("dt_color-base-inv-hgl-2")
		rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(11), bg)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, "arrow-up", 20, v.tok("dt_color-content-inv-pry")) })
		return D{Size: image.Pt(s, s)}
	})
	popups.Tooltip(gtx, v.th, v.tip("top"), c.Hovered(), image.Pt(s, s), popups.Above, hdNS.T("Scroll to the top"), "")
}

// layoutPastTitle is PastHuddleSectionTitle: "Recent huddles" (15px
// bold, content-pry, margin-bottom 20) in the column.
func (v *Huddles) layoutPastTitle(gtx C) D {
	w := gtx.Constraints.Max.X
	x, cw := hdColumn(gtx, w)
	defer opOffset(gtx, image.Pt(x, 0)).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(cw, gtx.Constraints.Max.Y)}
	d := v.para(g, 15, 22, wBold, v.tok("dt_color-content-pry"), 1, hdNS.T("Recent huddles"))
	return D{Size: image.Pt(w, d.Size.Y+gtx.Dp(20))}
}

// layoutDay is HuddlesHistoryRelativeTimeItem (.sectionHeader__ueRXr:
// 15px/1.46668 bold, padding 16px 20px, the first section's top padding
// 0) in a 968 column.
func (v *Huddles) layoutDay(gtx C, it hpItem) D {
	w := gtx.Constraints.Max.X
	x, cw := hdColumn(gtx, w)
	top := gtx.Dp(16)
	if it.first {
		top = 0
	}
	defer opOffset(gtx, image.Pt(x, top)).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(cw, gtx.Constraints.Max.Y)}
	d := v.para(g, 15, 22, wBold, v.tok("dt_color-content-pry"), 1, hdNS.T(hdDayLabels[min(it.day, 4)]))
	return D{Size: image.Pt(w, top+d.Size.Y+gtx.Dp(16))}
}

// layoutNoResults is NoResults (.container__kSw1D: a centred column,
// gap 8, padding 32): "No results" (18px 900) and its hint.
func (v *Huddles) layoutNoResults(gtx C) D {
	w := gtx.Constraints.Max.X
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	return layout.UniformInset(32).Layout(g, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		fg := v.tok("dt_color-content-pry")
		return col(gtx,
			layout.Rigid(func(gtx C) D { return v.centered(gtx, 18, wBlack, fg, hdNS.T("No results")) }),
			vgap(8),
			layout.Rigid(func(gtx C) D {
				return v.centered(gtx, 15, wReg, fg, hdNS.T("You may want to try adjusting your filters."))
			}),
		)
	})
}

// layoutSkeleton is HuddlesHistoryListSkeleton under a filter's fetch:
// skeleton rows in the column.
func (v *Huddles) layoutSkeleton(gtx C) D {
	w := gtx.Constraints.Max.X
	x, cw := hdColumn(gtx, w)
	defer opOffset(gtx, image.Pt(x, 0)).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(cw, 0), Max: image.Pt(cw, gtx.Constraints.Max.Y)}
	d := v.skeleton(g, 6, 36)
	return D{Size: image.Pt(w, d.Size.Y)}
}

// layoutFooter is HuddlesHistoryListFooter: 64px with the large spinner
// while the next page loads.
func (v *Huddles) layoutFooter(gtx C, st pageState) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(64)
	if st.paging {
		v.spinnerAt(gtx, image.Pt(w/2, h/2), gtx.Dp(32))
	}
	return D{Size: image.Pt(w, h)}
}

// loader is Slack's page Loader (s6So): the spinner, centred, top px
// below the top.
func (v *Huddles) loader(gtx C, w, top int) D {
	s := gtx.Dp(32)
	v.spinnerAt(gtx, image.Pt(w/2, top+s/2), s)
	return D{Size: image.Pt(w, top+s)}
}

// spinnerAt draws Slack's spinner (a rotating arc) centred at c, d px.
func (v *Huddles) spinnerAt(gtx C, c image.Point, d int) {
	defer opOffset(gtx, image.Pt(c.X-d/2, c.Y-d/2)).Pop()
	actSpinner(gtx, d, v.tok("dt_color-content-ter"))
	gtx.Execute(op.InvalidateCmd{})
}
