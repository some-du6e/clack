package popups

// Search against internal/fakeslack: the popover (blank slate, Recent
// places, history mode, typing, keyboard, Enter), the results page (the
// six sources and their counts, the More menu's modifiers, pagination,
// "Nothing turned up", the Files tab) and search in channel. With
// SEARCH_SHOTS_DIR set (the search workflow) TestSearchShots also writes
// light and dark PNGs.

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// srEnv is a booted core on fakeslack behind a test host.
type srEnv struct {
	h  *tzHost
	fs *fakeslack.Server
}

func srNewEnv(t *testing.T, dark bool) *srEnv {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	th := theme.New(dark)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	srResetRecents()
	t.Cleanup(srResetRecents)
	return &srEnv{h: &tzHost{a: a, th: th, im: imgcache.New(noNet, "", nil)}, fs: fs}
}

// srResetRecents forgets the in-memory MRU lists between tests.
func srResetRecents() {
	srMu.Lock()
	srRecentConvs, srRecentQs, srPlaces, srCurrent = nil, nil, map[string]srPlace{}, ""
	srMu.Unlock()
}

// srDrv draws a widget through Gio's router (keys reach focused editors).
type srDrv struct {
	r    input.Router
	ops  op.Ops
	size image.Point
	w    interface {
		Layout(gtx layout.Context) layout.Dimensions
	}
	over  func(gtx layout.Context) // drawn after w (menus)
	texts []string                 // the last scanned frame's text
	dark  bool
}

func (d *srDrv) frame() {
	d.ops.Reset()
	gtx := layout.Context{Ops: &d.ops, Source: d.r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(d.size), Now: time.Now()}
	slackc.Frame(gtx)
	bg := rgb(0xffffff)
	if d.dark {
		bg = rgb(0x1a1d21)
	}
	paint.Fill(&d.ops, bg)
	d.w.Layout(gtx)
	if d.over != nil {
		d.over(gtx)
	}
	d.r.Frame(&d.ops)
}

// scan draws one frame recording its text.
func (d *srDrv) scan() []string {
	stop := theme.StartTofuScan()
	d.frame()
	d.texts = stop().Texts
	return d.texts
}

func (d *srDrv) until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	slUntil(t, 15*time.Second, what, d.frame, cond)
}

func (d *srDrv) press(name key.Name) {
	d.r.Queue(key.Event{Name: name, State: key.Press})
	d.frame()
	d.frame()
}

func srHas(texts []string, s string) bool { return slices.Contains(texts, s) }

func srHasPrefix(texts []string, p string) bool {
	return slices.ContainsFunc(texts, func(t string) bool { return strings.HasPrefix(t, p) })
}

func srTitles(rows []srRow, k srRowKind) []string {
	var out []string
	for _, r := range rows {
		if r.kind == k {
			out = append(out, r.title)
		}
	}
	return out
}

// The blank slate is Slack's merged zero state (renderBlankSlateSuggestions):
// Unreads & drafts (ten, then "Show N more" / "Show fewer"), Recent places
// from where you went (pages too) without where you are and without the
// unreads (three when there are unreads, else five), and no Recent
// searches; the clock button switches the list to Recent searches, Slack's
// server history (search.autocomplete).
func TestSearchPopoverBlankSlate(t *testing.T) {
	e := srNewEnv(t, false)
	p := NewQuickSwitcher(e.h)
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the snapshot", func() bool { return p.snapOK })
	unread := map[string]bool{}
	for _, id := range p.unreads {
		unread[id] = true
	}
	var read []string // channels that aren't unreads
	for _, id := range e.fs.ChannelIDs() {
		if !unread[id] && e.h.a.ConvRef(id).Name != "" {
			read = append(read, id)
		}
	}
	if len(read) < 3 || len(p.unreads) == 0 {
		t.Fatalf("fixture: %d read channels, %d unreads", len(read), len(p.unreads))
	}
	RecordRecentPlace(read[0], "", "")
	RecordRecentPlace("view:later", "To-dos", "todos")
	RecordRecentPlace(p.unreads[0], "", "") // an unread: listed there, not here
	RecordRecentPlace(read[1], "", "")
	RecordRecentPlace(read[2], "", "")
	RecordRecentPlace("view:activity", "Activity", "activity") // where you are now
	p.rebuild(time.Now(), true)

	places := func() []string {
		out := []string{}
		in := false
		for _, r := range p.rows {
			if r.kind == srRHeader {
				in = r.title == "Recent places"
				continue
			}
			if in && r.selectable() {
				out = append(out, r.title)
			}
		}
		return out
	}
	n2, n1 := e.h.a.ConvRef(read[2]).Name, e.h.a.ConvRef(read[1]).Name
	if got := places(); !slices.Equal(got, []string{n2, n1, "To-dos"}) {
		t.Errorf("Recent places %q, want [%s %s To-dos] (three with unreads, newest first, no unread, the open page left out)", got, n2, n1)
	}
	for _, bad := range []string{"Saved", "Threads", "Activity", e.h.a.ConvRef(p.unreads[0]).Name} {
		if slices.Contains(places(), bad) {
			t.Errorf("Recent places has %q (not visited, the open page, or an unread)", bad)
		}
	}
	if slices.Contains(srTitles(p.rows, srRHeader), "Recent searches") {
		t.Error("the blank slate lists Recent searches (Slack shows them only in history mode)")
	}
	for _, r := range p.rows {
		if r.kind == srRPlace && r.title == "To-dos" && srDataType(&r) != "view" {
			t.Errorf("page row data-type %q", srDataType(&r))
		}
	}
	// no unreads: five places
	keep := p.unreads
	p.unreads = nil
	p.rebuild(time.Now(), true)
	if got := places(); len(got) != 5 {
		t.Errorf("without unreads %d Recent places %q, want 5", len(got), got)
	}
	p.unreads = keep

	// Unreads & drafts: ten, then Show N more
	var ids []string
	for id := range p.byConv {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	p.unreads = ids[:13]
	p.rebuild(time.Now(), true)
	if got := srTitles(p.rows, srRMore); !slices.Equal(got, []string{"Show 3 more"}) {
		t.Fatalf("more row %q", got)
	}
	n := 0
	for _, r := range p.rows {
		if r.kind == srRHeader && r.title == "Recent places" {
			break
		}
		if r.kind == srRConv || r.kind == srRUser {
			n++
		}
	}
	if n != srUnreadsShown || srUnreadsShown != 10 {
		t.Errorf("%d unreads before Show more, want 10", n)
	}
	for i := range p.rows {
		if p.rows[i].kind == srRMore {
			p.activate(i)
			break
		}
	}
	if got := srTitles(p.rows, srRMore); !slices.Equal(got, []string{"Show fewer"}) {
		t.Errorf("after Show more: %q", got)
	}

	// history mode: Slack's server history, fetched when the list opens
	p.history.Click()
	d.frame()
	d.until(t, "the history fetch", func() bool { return !p.histLoading })
	if got := srTitles(p.rows, srRNote); !slices.Equal(got, []string{"Your recent searches will show up here."}) {
		t.Errorf("empty history %q", got)
	}
	if err := e.h.a.SaveSearch(context.Background(), "zephyr notes"); err != nil { // drops the cached history
		t.Fatal(err)
	}
	d.until(t, "the saved search in history", func() bool { return slices.Contains(srTitles(p.rows, srRQuery), "zephyr notes") })
	if !slices.Contains(srTitles(p.rows, srRHeader), "Recent searches") {
		t.Errorf("history rows %+v", srTitles(p.rows, srRQuery))
	}
	if f := e.fs.SearchForms("search.autocomplete"); len(f) == 0 || f[0].Get("use_hash") != "true" || f[0].Get("limit") != "100" {
		t.Errorf("search.autocomplete %v", f)
	}
}

// desktop_search_recents_read on: Recent places come from Slack's
// visits.list and Recent searches from the recents pref; forced off they
// come from this session's history and search.autocomplete again. With
// desktop_search_recents_write on, a search run from the switcher is
// recorded (and, with read on, shows in history at once).
func TestSearchPopoverRecentsGroups(t *testing.T) {
	e := srNewEnv(t, false)
	ctx := context.Background()
	old := PageInfo // the ui root's page titles
	PageInfo = func(page string) (string, string) {
		if page == "saved" {
			return "Saved", "bookmark"
		}
		return "", ""
	}
	t.Cleanup(func() { PageInfo = old })
	a := e.h.a
	chs := e.fs.ChannelIDs()
	pref := `{"search":[{"query":"from the pref","type":"SLACK","timestamp":5},{"query":"sf","type":"SALESFORCE","timestamp":9}]}`
	if err := a.C.SetPref(ctx, "recents", pref); err != nil {
		t.Fatal(err)
	}
	e.fs.SetVisits(fakeslack.Visit{EntityID: chs[3], EntityType: "channel", LastMS: 300},
		fakeslack.Visit{EntityID: "Psaved", EntityType: "view", LastMS: 200})
	e.fs.SetSearchHistory("from search.autocomplete")
	a.SetExperimentGroup("desktop_search_recents_read", "on")
	// loaded now, whichever trigger claimed the load (the forced group's
	// runs on its own goroutine)
	_ = a.LoadRecentsPrefs(ctx)
	_ = a.LoadVisits(ctx)

	p := NewQuickSwitcher(e.h)
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the snapshot", func() bool { return p.snapOK })
	p.unreads = nil // five places, none excluded
	p.rebuild(time.Now(), true)
	var places []string
	in := false
	for _, r := range p.rows {
		if r.kind == srRHeader {
			in = r.title == "Recent places"
			continue
		}
		if in && r.selectable() {
			places = append(places, r.title)
		}
	}
	if want := []string{a.ConvRef(chs[3]).Name, "Saved"}; !slices.Equal(places, want) {
		t.Errorf("Recent places from visits.list %q, want %q", places, want)
	}
	p.history.Click()
	d.frame()
	if got := srTitles(p.rows, srRQuery); !slices.Equal(got, []string{"from the pref"}) {
		t.Errorf("history from the recents pref %q (no Salesforce rows)", got)
	}

	a.SetExperimentGroup("desktop_search_recents_read", "off")
	d.frame()
	d.until(t, "search.autocomplete history", func() bool {
		return slices.Equal(srTitles(p.rows, srRQuery), []string{"from search.autocomplete"})
	})

	a.SetExperimentGroup("desktop_search_recents_read", "on")
	a.SetExperimentGroup("desktop_search_recents_write", "on")
	p.submit("fresh search")
	a.FlushRecents()
	q := NewQuickSwitcher(e.h)
	q.histMode = true
	d2 := &srDrv{size: image.Pt(1280, 800), w: q}
	d2.until(t, "the recorded search", func() bool {
		return slices.Contains(srTitles(q.rows, srRQuery), "fresh search")
	})
}

// Typing: users/channels from the edge search, Recent messages from
// search.inline (count 3, extract_len 110), files and canvases from
// search.autocomplete.files, "Show results for:" after the top hit, and
// "— Open" next to the query for a place.
func TestSearchPopoverTyping(t *testing.T) {
	e := srNewEnv(t, false)
	RecordRecentPlace(e.fs.ChannelIDs()[0], "", "") // where Clack opened: the quick messages' recent channels
	p := NewSearchPopover(e.h, fakeslack.SearchWord, image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the message and file suggestions", func() bool {
		return len(p.msgs) == 3 && len(p.files) == 1
	})
	f := e.fs.SearchForms("search.inline")
	if len(f) == 0 {
		t.Fatal("no search.inline")
	}
	last := f[len(f)-1]
	for k, v := range map[string]string{"query": fakeslack.SearchWord, "count": "3", "extract_len": "110", "from_me": "true", "with_me": "true",
		"page": "1", "recent_channels": e.fs.ChannelIDs()[0]} {
		if last.Get(k) != v {
			t.Errorf("search.inline %s = %q, want %q", k, last.Get(k), v)
		}
	}
	if last.Get("search_session_id") == "" || last.Get("max_ts") == "" {
		t.Errorf("search.inline without a session or max_ts: %v", last)
	}
	if len(e.fs.SearchForms("search.modules.messages")) != 0 {
		t.Error("the popover searched search.modules.messages (Slack's popover uses search.inline)")
	}
	if f := e.fs.SearchForms("search.autocomplete.files"); len(f) == 0 || f[0].Get("include_shares") != "true" {
		t.Errorf("search.autocomplete.files %v", f)
	}
	var file *srRow
	for i := range p.rows {
		if p.rows[i].kind == srRFile {
			file = &p.rows[i]
		}
	}
	if file == nil || file.title != fakeslack.SearchFileTitle || srDataType(file) != "file" {
		t.Fatalf("file row %+v", file)
	}
	if !slices.Contains(srTitles(p.rows, srRHeader), "Recent messages") {
		t.Error("no Recent messages header")
	}
	x := d.scan()
	if !srHas(x, "Show results for: ") || !srHas(x, fakeslack.SearchWord) {
		t.Errorf("no Show results row: %q", x)
	}
	if p.rows[0].kind == srRShow {
		// no suggestion matched: Show results is first
	} else if p.rows[1].kind != srRShow {
		t.Errorf("Show results isn't right after the top hit: %v", srTitles(p.rows, p.rows[1].kind))
	}
}

// omni_quick_message_results (suggestionsQuickMessages): on, the typed
// words search your recent conversations (from_me, with_me) as cards and
// the places above are cut to five; an in: filter searches its channel;
// fewer than three characters, a phrase, or another filter search nothing.
// Off, only the channel you're in is searched (from_me/with_me false),
// the rows are the plain ones (text, then "name • date") and the places
// are cut to eight.
func TestSearchPopoverQuickMessages(t *testing.T) {
	e := srNewEnv(t, false)
	chs := e.fs.ChannelIDs()
	inline := func() int { return len(e.fs.SearchForms("search.inline")) }
	open := func(q string) (*SearchPopover, *srDrv) {
		p := NewSearchPopover(e.h, q, image.Rectangle{})
		d := &srDrv{size: image.Pt(1280, 900), w: p}
		d.until(t, "the switcher", func() bool { return p.snapOK })
		return p, d
	}
	settle := func(d *srDrv) {
		end := time.Now().Add(600 * time.Millisecond) // past the 250 ms debounce
		for time.Now().Before(end) {
			d.frame()
			time.Sleep(20 * time.Millisecond)
		}
	}
	navRows := func(p *SearchPopover) int {
		n := 0
		for _, r := range p.rows {
			switch r.kind {
			case srRConv, srRUser, srRChan, srRFile:
				n++
			}
		}
		return n
	}
	if g := e.h.a.Experiment("omni_quick_message_results"); g != "on" {
		t.Fatalf("the fake account has omni_quick_message_results %q, want on (as the real one)", g)
	}
	RecordRecentPlace(chs[0], "", "")
	RecordRecentPlace(chs[1], "", "")

	// on: recent conversations, cards, five places
	p, d := open(fakeslack.SearchWord)
	d.until(t, "quick messages", func() bool { return len(p.msgs) == 3 })
	f := e.fs.SearchForms("search.inline")
	last := f[len(f)-1]
	if last.Get("recent_channels") != chs[1]+","+chs[0] || last.Get("from_me") != "true" || last.Get("with_me") != "true" ||
		last.Get("channel") != "" {
		t.Errorf("on: search.inline %v", last)
	}
	for _, r := range p.rows {
		if r.kind == srRMsg && (r.plain || r.heightDp() != 76) {
			t.Errorf("on: a plain quick message row %+v", r.title)
		}
	}
	if n := navRows(p); n > 5 {
		t.Errorf("on, with quick messages: %d places, want at most 5", n)
	}

	// fewer than 3 characters, a phrase, another filter: no search
	for _, q := range []string{"ze", `"zephyr rollout"`, "from:@nobody zephyr", "@someone"} {
		n := inline()
		p, d = open(q)
		settle(d)
		if inline() != n || len(p.msgs) != 0 {
			t.Errorf("%q searched quick messages (%d calls, %d messages)", q, inline()-n, len(p.msgs))
		}
	}
	// no quick messages: up to 15 places
	p, d = open("e")
	settle(d)
	if n := navRows(p); n <= 5 || n > 15 || len(p.msgs) != 0 {
		t.Errorf("a one-letter query: %d places, %d messages (want 6 to 15 places, no messages)", n, len(p.msgs))
	}

	// in:#channel: that channel only
	name := e.h.a.ConvRef(chs[0]).Name
	p, d = open("in:#" + name + " " + fakeslack.SearchWord)
	d.until(t, "in: quick messages", func() bool { return p.msgQ != "" })
	f = e.fs.SearchForms("search.inline")
	last = f[len(f)-1]
	if last.Get("channel") != chs[0] || last.Get("with_me") != "true" || last.Get("from_me") != "false" ||
		!last.Has("recent_channels") || last.Get("query") != fakeslack.SearchWord {
		t.Errorf("in: search.inline %v", last)
	}
	for _, m := range p.msgs {
		if m.Channel != chs[0] {
			t.Errorf("in:#%s kept a hit in %s", name, m.Channel)
		}
	}

	// off: only the open channel; plain rows; eight places
	e.h.a.SetExperimentGroup("omni_quick_message_results", "off")
	RecordRecentPlace("view:activity", "Activity", "activity") // not in a channel: nothing to search
	n := inline()
	p, d = open(fakeslack.SearchWord)
	settle(d)
	if inline() != n || len(p.msgs) != 0 {
		t.Errorf("off, on a page: %d search.inline calls", inline()-n)
	}
	RecordRecentPlace(chs[2], "", "")
	p, d = open(fakeslack.SearchWord)
	d.until(t, "off quick messages", func() bool { return p.msgQ != "" })
	f = e.fs.SearchForms("search.inline")
	last = f[len(f)-1]
	if last.Get("channel") != chs[2] || last.Get("from_me") != "false" || last.Get("with_me") != "false" || last.Has("recent_channels") {
		t.Errorf("off: search.inline %v", last)
	}
	for _, r := range p.rows {
		if r.kind == srRMsg && (!r.plain || r.heightDp() != 54) {
			t.Errorf("off: a card quick message row %q", r.title)
		}
	}
	if len(p.msgs) > 0 {
		if n := navRows(p); n > 8 {
			t.Errorf("off, with quick messages: %d places, want at most 8", n)
		}
		x := d.scan()
		if !srHas(x, "\u2022") {
			t.Errorf("off: no metadata bullet in %q", x)
		}
	}
}

// Slack's query tokens (rawTokensFromOps): words merge into one string,
// quotes are phrases, modifier:value a filter, @x and <…> mentions.
func TestSearchQueryTokens(t *testing.T) {
	kinds := func(q string) string {
		var b strings.Builder
		for _, tk := range srQueryTokens(q) {
			b.WriteString([]string{"w", "p", "f", "m"}[tk.kind])
			if tk.kind == srQTFilter {
				b.WriteString("(" + tk.mod + "=" + tk.val + ")")
			}
			b.WriteString(" ")
		}
		return strings.TrimSpace(b.String())
	}
	for q, want := range map[string]string{
		"zephyr rollout":            "w",
		"in:#general zephyr notes":  "f(in=#general) w",
		"zephyr in: #general":       "w f(in=#general)",
		`say "the rollout" twice`:   "w p w",
		"from:<@U123> hi -in:#rand": "f(from=<@U123>) w f(-in=#rand)",
		"@karim hello":              "m w",
		"hasmy::tada: x":            "f(hasmy=:tada:) w",
		"":                          "",
	} {
		if got := kinds(q); got != want {
			t.Errorf("tokens(%q) = %q, want %q", q, got, want)
		}
	}
	if tk := srQueryTokens("zephyr   rollout  plan"); len(tk) != 1 || tk[0].text != "zephyr rollout plan" {
		t.Errorf("merged words %+v", tk)
	}
}

// Keys, as SearchAutocomplete's increment/decrementSelectedIndex: nothing
// is selected at first (the top hit is pseudo-selected, Enter opens it);
// ↓ picks the first row, then the next, skipping headers and dividers;
// past the last row nothing is selected again, and ↑ from there picks the
// last row; moving the caret clears the selection. The footer says "▲▼
// Select" until a row is picked, then "Enter Open". Enter on "Show
// results for:" opens the results page and remembers the query with
// search.save.
func TestSearchPopoverKeys(t *testing.T) {
	e := srNewEnv(t, false)
	ch := e.fs.ChannelIDs()[3]
	name := e.h.a.ConvRef(ch).Name
	p := NewSearchPopover(e.h, name, image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	// the channel and "Show results for:" (no quick messages: nothing was
	// visited, so there are no recent conversations to search)
	d.until(t, "the switcher", func() bool { return p.snapOK && len(p.rows) >= 2 && p.dirFired })
	for end := time.Now().Add(400 * time.Millisecond); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		d.frame() // the directory searches answer
	}
	if p.sel != 0 || p.picked || p.rows[0].id != ch {
		t.Fatalf("top hit %+v sel %d picked %v, want %s first, pseudo-selected", p.rows[0], p.sel, p.picked, ch)
	}
	if x := d.scan(); !srHas(x, "Select") || srHas(x, "Open") || !srHas(x, "Enter") {
		t.Errorf("footer / Enter chip before a pick: %q", x)
	}
	d.press(key.NameDownArrow)
	if p.sel != 0 || !p.picked {
		t.Fatalf("↓ from nothing: sel %d picked %v, want the first row picked", p.sel, p.picked)
	}
	if x := d.scan(); !srHas(x, "Open") || srHas(x, "Select") {
		t.Errorf("footer with a pick: %q", x)
	}
	d.press(key.NameDownArrow)
	if p.sel != 1 || p.rows[1].kind != srRShow || !p.picked {
		t.Fatalf("↓ selected %d (%v)", p.sel, p.rows[p.sel].kind)
	}
	wrapped := false
	for i := 0; i < len(p.rows)+2; i++ {
		d.press(key.NameDownArrow)
		if !p.rows[p.sel].selectable() {
			t.Fatalf("↓ landed on a %v row", p.rows[p.sel].kind)
		}
		if !p.picked {
			wrapped = true
			if p.sel != p.firstRow() {
				t.Fatalf("past the end: sel %d, want the first row (pseudo)", p.sel)
			}
			break
		}
	}
	if !wrapped {
		t.Fatal("↓ never went past the last row")
	}
	d.press(key.NameUpArrow)
	if p.sel != p.lastRow() || !p.picked {
		t.Fatalf("↑ from nothing: sel %d (last %d) picked %v", p.sel, p.lastRow(), p.picked)
	}
	// moving the caret clears the selection
	d.press(key.NameLeftArrow)
	if p.picked {
		t.Error("← kept the selection")
	}
	// nothing picked: Enter opens the top hit
	d.press(key.NameReturn)
	if !p.Dismissed() || len(e.h.navs) == 0 || e.h.navs[len(e.h.navs)-1] != "c/"+ch {
		t.Fatalf("Enter on the top hit: dismissed %v navs %q", p.Dismissed(), e.h.navs)
	}

	// Enter on Show results for:
	q := fakeslack.SearchWord + " is:thread"
	p2 := NewSearchPopover(e.h, q, image.Rectangle{})
	d2 := &srDrv{size: image.Pt(1280, 800), w: p2}
	d2.until(t, "the second switcher", func() bool { return p2.snapOK })
	for i := range p2.rows {
		if p2.rows[i].kind == srRShow {
			p2.setSel(i)
		}
	}
	d2.press(key.NameReturn)
	if got := e.h.navs[len(e.h.navs)-1]; got != "search?q=zephyr+is%3Athread" {
		t.Errorf("Show results navigated to %q", got)
	}
	slUntil(t, 5*time.Second, "search.save", nil, func() bool { return len(e.fs.SavedSearches()) > 0 })
	if s := e.fs.SavedSearches(); s[0] != q {
		t.Errorf("search.save terms %q", s)
	}
}

// srView loads a results page until its search answered.
func srView(t *testing.T, e *srEnv, q string, dark bool) (*SearchView, *srDrv) {
	t.Helper()
	v := NewSearchView(e.h, q)
	v.SetOrigin(image.Pt(0, 0))
	d := &srDrv{size: image.Pt(1280, 800), w: v, dark: dark}
	d.until(t, "the results", func() bool { return v.started && !v.loading })
	return v, d
}

// srMenu returns the last menu the host showed.
func srMenu(h *tzHost) *Menu {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.shown) - 1; i >= 0; i-- {
		if m, ok := h.shown[i].(*Menu); ok {
			return m
		}
	}
	return nil
}

func srLabels(items []MenuItem) []string {
	var out []string
	for _, it := range items {
		switch {
		case it.Header != "":
			out = append(out, "# "+it.Header)
		case it.Separator:
			out = append(out, "--")
		default:
			out = append(out, it.Label)
		}
	}
	return out
}

// The results page: search.modules.messages pages of 20 with the
// session's other modules counted for the source menu (Messages, DMs,
// Files, People, Channels, Canvases), Slack's More menu, numbered pages
// and the filters' modifiers.
func TestSearchViewResults(t *testing.T) {
	e := srNewEnv(t, false)
	v, d := srView(t, e, fakeslack.SearchWord, false)
	if v.total != fakeslack.SearchFixtureCount || len(v.cards) != 20 || v.pages != 2 || v.page != 1 {
		t.Fatalf("page 1: total %d, %d cards, page %d of %d", v.total, len(v.cards), v.page, v.pages)
	}
	d.until(t, "every source's count", func() bool {
		for i := 0; i < srTabN; i++ {
			if !v.countOK[i] {
				return false
			}
		}
		return true
	})
	if v.counts[srTabDMs] != 1 || v.counts[srTabFiles] != 1 || v.counts[srTabMessages] != fakeslack.SearchFixtureCount {
		t.Errorf("counts %v", v.counts)
	}
	// one session for the query's modules
	sess := map[string]bool{}
	for _, m := range []string{"search.modules.messages", "search.modules.dms", "search.modules.files"} {
		for _, f := range e.fs.SearchForms(m) {
			sess[f.Get("search_session_id")] = true
		}
	}
	if len(sess) != 1 {
		t.Errorf("%d search sessions for one query, want 1", len(sess))
	}
	x := d.scan()
	for _, want := range []string{"Results for: ", fakeslack.SearchWord, "Messages", "From", "In", "Includes me", "Has a link",
		"Threads only", "Last 7 days", "More", "Sorted by ", "Most relevant (default)", "27 results", "Give feedback"} {
		if !srHas(x, want) {
			t.Errorf("%q not drawn", want)
		}
	}

	// the source menu, in Slack's order with the counts
	v.typeMenu(layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}})
	m := srMenu(e.h)
	if m == nil {
		t.Fatal("no source menu")
	}
	if got := srLabels(m.items); !slices.Equal(got, []string{"Messages", "DMs", "Files", "People", "Channels", "Canvases"}) {
		t.Errorf("source menu %q", got)
	}
	mx := (&srDrv{size: image.Pt(600, 400), w: m}).scan()
	for _, want := range []string{"27", "1"} {
		if !srHas(mx, want) {
			t.Errorf("source menu count %q not drawn: %q", want, mx)
		}
	}

	// More: Slack's rows (To-dos under later_todos, on for the account;
	// no Exclude archived without search_explore_freshness_signals)
	e.h.a.SetExperimentGroup("later_todos", "on")
	v.moreMenu(layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}})
	m = srMenu(e.h)
	want := []string{"# Saved preferences", "Exclude bot messages", "Only my channels", "--", "# Results", "Exact matches only",
		"--", "With people", "Message has", "Message is", "Date", "File type", "Reactions"}
	if got := srLabels(m.items); !slices.Equal(got, want) {
		t.Errorf("More menu %q\nwant %q", got, want)
	}
	sub := func(label string) []MenuItem {
		for _, it := range m.items {
			if it.Label == label {
				return it.Sub
			}
		}
		return nil
	}
	if got := srLabels(sub("Message is")); !slices.Equal(got, []string{"A direct message", "In a thread", "Saved", "To-dos", "Pinned", "In Activity"}) {
		t.Errorf("Message is %q", got)
	}
	if got := srLabels(sub("Date")); !slices.Equal(got, []string{"Any time", "Today", "Yesterday", "Last 7 days", "Last 30 days",
		"Last 12 months", "Last 3 years", "--", "On…", "Before…", "After…", "Range…"}) {
		t.Errorf("Date %q", got)
	}
	// Each filter starts its own search; they run concurrently and the
	// fake records them as they arrive, not as they were sent. On CI the
	// first filter's request ("zephyr is:thread") arrived after all the
	// others, so "the last recorded request" never carried every filter.
	// Hold that one back to get the same order here; only the seq check in
	// search() may decide which answer the view shows.
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "search.modules.messages" && f.Get("query") == fakeslack.SearchWord+" is:thread" {
			time.Sleep(250 * time.Millisecond)
		}
		return nil, false
	})
	for _, c := range []struct {
		menu, item, mod string
	}{
		{"Message is", "In a thread", "is:thread"}, {"Message is", "A direct message", "is:dm"}, {"Message is", "To-dos", "is:todos"},
		{"Message is", "Pinned", "has:pin"}, {"Message is", "In Activity", "in:activity"}, {"Message has", "Action", "has:action"},
		{"File type", "Videos", "type:videos"}, {"File type", "Huddle AI Notes", "type:huddle_notes"},
	} {
		for _, it := range sub(c.menu) {
			if it.Label == c.item {
				it.Do()
			}
		}
		if !strings.Contains(v.q.String(), c.mod) {
			t.Errorf("%s > %s: query %q lacks %s", c.menu, c.item, v.q.String(), c.mod)
		}
	}
	// the view settles on the last filter's search: a request carried every
	// filter, and the results shown are its (older searches' answers are
	// dropped by seq whenever they arrive)
	wantQ := v.q.String()
	sentFull := func(extra func(url.Values) bool) bool {
		for _, f := range e.fs.SearchForms("search.modules.messages") {
			if f.Get("query") == wantQ && (extra == nil || extra(f)) {
				return true
			}
		}
		return false
	}
	d.until(t, "the filtered search", func() bool { return !v.loading && sentFull(nil) })
	if !strings.HasPrefix(wantQ, fakeslack.SearchWord+" is:thread is:dm is:todos has:pin in:activity has:action type:videos type:huddle_notes") {
		t.Errorf("final query %q", wantQ)
	}
	seq := v.seq
	time.Sleep(400 * time.Millisecond) // let the held-back answer arrive
	d.frame()
	Pump()
	if v.seq != seq || v.loading || v.q.String() != wantQ {
		t.Errorf("a late answer changed the view: seq %d→%d loading %v query %q", seq, v.seq, v.loading, v.q.String())
	}
	e.fs.SetHook(nil)
	// Exclude bot messages is the search_exclude_bots pref
	for _, it := range m.items {
		if it.Label == "Exclude bot messages" {
			it.Do()
		}
	}
	slUntil(t, 10*time.Second, "the pref and the new search", d.frame, func() bool {
		return e.h.a.PrefBool("search_exclude_bots", false) && !v.loading &&
			sentFull(func(f url.Values) bool { return f.Get("search_exclude_bots") == "true" })
	})

	// Includes me is with:<@self>
	v.setQuery(srParse(fakeslack.SearchWord))
	v.pills[srPMe].Click()
	d.frame()
	if !v.q.Has("with", "<@"+fakeslack.SelfID+">") {
		t.Errorf("Includes me: %q", v.q.String())
	}
}

// The other tabs' filter rows are Slack's _p: Files From, In, File type,
// Date, Only my channels; Canvases Created By, From, In, Date; DMs With.
// More offers the tab's other filters (With people on Files and
// Canvases) and isn't drawn on DMs; Reset shows once a filter is on and
// clears it. Narrow, the pills that don't fit move into More.
func TestSearchViewTabFilters(t *testing.T) {
	e := srNewEnv(t, false)
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	for _, c := range []struct {
		q          string
		pills      []string
		more       []string // nil: no More
		notDrawn   []string
		wantModule string
	}{
		{fakeslack.SearchWord + "&module=files", []string{"Files", "From", "In", "File type", "Date", "Only my channels", "More"},
			[]string{"With people"}, []string{"Includes me", "Threads only"}, "files"},
		{fakeslack.SearchWord + "&module=canvases", []string{"Canvases", "Created By", "From", "In", "Date", "More"},
			[]string{"With people"}, []string{"File type", "Includes me"}, "canvases"},
		{fakeslack.SearchWord + "&module=dms", []string{"DMs", "With"}, nil, []string{"More", "From", "Includes me"}, "dms"},
	} {
		v, d := srView(t, e, c.q, false)
		x := d.scan()
		for _, p := range c.pills {
			if !srHas(x, p) {
				t.Errorf("%s: pill %q not drawn: %q", c.wantModule, p, x)
			}
		}
		for _, p := range c.notDrawn {
			if srHas(x, p) {
				t.Errorf("%s: %q drawn", c.wantModule, p)
			}
		}
		if c.more == nil {
			if !v.pillR[srPMore].Empty() {
				t.Errorf("%s: More drawn", c.wantModule)
			}
			continue
		}
		v.moreMenu(gtx)
		m := srMenu(e.h)
		if got := srLabels(m.items); !slices.Equal(got, c.more) {
			t.Errorf("%s: More %q, want %q", c.wantModule, got, c.more)
		}
		m.Dismiss()
	}

	// File type, Date and Only my channels on Files
	v, d := srView(t, e, fakeslack.SearchWord+"&module=files", false)
	v.pillClicked(gtx, srPFileType)
	m := srMenu(e.h)
	for _, it := range m.items {
		if it.Label == "PDFs" {
			it.Do()
		}
	}
	m.Dismiss()
	d.until(t, "the PDFs search", func() bool { return !v.loading })
	if !v.q.Has("type", "pdfs") {
		t.Errorf("File type > PDFs: %q", v.q.String())
	}
	x := d.scan()
	if !srHas(x, "PDFs") || !srHas(x, "Reset") {
		t.Errorf("the File type pill names PDFs and Reset shows: %q", x)
	}
	f := e.fs.SearchForms("search.modules.files")
	if q := f[len(f)-1].Get("query"); !strings.Contains(q, "type:pdfs") {
		t.Errorf("files query %q", q)
	}
	v.pillClicked(gtx, srPOnlyMine)
	slUntil(t, 10*time.Second, "Only my channels", d.frame, func() bool {
		f := e.fs.SearchForms("search.modules.files")
		return e.h.a.PrefBool("search_only_my_channels", false) && f[len(f)-1].Get("search_only_my_channels") == "true"
	})
	// Reset: filters and the two prefs off
	v.pillClicked(gtx, srPReset)
	slUntil(t, 10*time.Second, "Reset", d.frame, func() bool {
		return len(v.q.Mods) == 0 && !e.h.a.PrefBool("search_only_my_channels", false) && !v.loading
	})
	if x := d.scan(); srHas(x, "Reset") {
		t.Error("Reset still drawn with no filter on")
	}

	// narrow: the row keeps the pills that fit; the rest are in More
	nv := NewSearchView(e.h, fakeslack.SearchWord+"&module=files")
	nv.SetOrigin(image.Pt(0, 0))
	nd := &srDrv{size: image.Pt(560, 700), w: nv}
	nd.until(t, "the narrow results", func() bool { return nv.started && !nv.loading })
	nd.frame()
	if len(nv.visible) >= 6 || len(nv.visible) < 1 {
		t.Fatalf("narrow row shows %q", nv.visible)
	}
	nv.moreMenu(gtx)
	labels := srLabels(srMenu(e.h).items)
	hidden := srFiltersShown(srTabFiles)[len(nv.visible)-1:]
	for _, f := range hidden {
		want := map[string]string{srFFrom: "From", srFIn: "In", srFFileType: "File type", srFDate: "Date", srFOnlyMine: "Only my channels"}[f]
		if !slices.Contains(labels, want) {
			t.Errorf("overflowed %s missing from More %q", f, labels)
		}
	}
}

// The other tabs' Sort selects (search-filter-sort): Files Most relevant
// (default), Oldest, Newest, A to Z, Z to A; Canvases add Last updated;
// Channels Most relevant, Newest, Oldest, Most members, Fewest members, A
// to Z, Z to A; People Most relevant, A to Z, Z to A; each then Edit
// default. A choice is getSortParams' sort and sort_dir; with
// stick_user_sort it is kept in the tab's pref and the next search starts
// from it.
func TestSearchViewTabSorts(t *testing.T) {
	e := srNewEnv(t, false)
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	// the tab's own call (not another tab's count-only one, which Slack
	// sends newest first). The fake answers canvases (type:quip) in its
	// canvas handler, so every call is read off a hook.
	var mu sync.Mutex
	calls := map[string][]url.Values{}
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		mu.Lock()
		calls[method] = append(calls[method], f)
		mu.Unlock()
		return nil, false
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	tabCall := func(method, sctx string) url.Values {
		mu.Lock()
		defer mu.Unlock()
		f := calls[method]
		for i := len(f) - 1; i >= 0; i-- {
			if f[i].Get("search_context") == sctx && f[i].Get("count") != "0" {
				return f[i]
			}
		}
		t.Fatalf("no %s call for %s", method, sctx)
		return nil
	}
	for _, c := range []struct {
		q, method   string
		labels      []string
		pick        string
		sort, dir   string
		wantMembers bool
	}{
		{fakeslack.SearchWord + "&module=files", "search.modules.files",
			[]string{"Most relevant (default)", "Oldest", "Newest", "A to Z", "Z to A", "--", "Edit default"}, "A to Z", "name", "asc", false},
		{fakeslack.SearchWord + "&module=canvases", "search.modules.files",
			[]string{"Most relevant (default)", "Oldest", "Newest", "A to Z", "Z to A", "Last updated", "--", "Edit default"}, "Last updated", "timestamp", "desc", false},
		{"e&module=channels", "search.modules.channels",
			[]string{"Most relevant (default)", "Newest", "Oldest", "Most members", "Fewest members", "A to Z", "Z to A", "--", "Edit default"},
			"Most members", "member_count", "desc", true},
		{"e&module=people", "search.modules.people",
			[]string{"Most relevant (default)", "A to Z", "Z to A", "--", "Edit default"}, "Z to A", "name", "desc", false},
	} {
		v, d := srView(t, e, c.q, false)
		sctx := "desktop_" + srTabModules[v.tab] + "_tab"
		if first := tabCall(c.method, sctx); c.method == "search.modules.files" && (first.Get("sort") != "score" || first.Get("sort_dir") != "desc") {
			t.Errorf("%s: the tab starts %s %s, want score desc (Most relevant)", c.q, first.Get("sort"), first.Get("sort_dir"))
		}
		if x := d.scan(); !srHas(x, "Sorted by ") || !srHas(x, "Most relevant (default)") {
			t.Errorf("%s: no Sort select: %q", c.q, x)
		}
		v.sortMenu(gtx)
		m := srMenu(e.h)
		if got := srLabels(m.items); !slices.Equal(got, c.labels) {
			t.Errorf("%s: sort menu %q, want %q", c.q, got, c.labels)
		}
		for _, it := range m.items {
			if it.Label == c.pick {
				it.Do()
			}
		}
		m.Dismiss()
		d.until(t, c.pick, func() bool { return !v.loading })
		last := tabCall(c.method, sctx)
		if last.Get("sort") != c.sort || last.Get("sort_dir") != c.dir {
			t.Errorf("%s %s: sort %s %s, want %s %s", c.q, c.pick, last.Get("sort"), last.Get("sort_dir"), c.sort, c.dir)
		}
		if x := d.scan(); !srHas(x, c.pick) {
			t.Errorf("%s: the select doesn't name %q", c.q, c.pick)
		}
		if c.wantMembers {
			for i := 1; i < len(v.chans); i++ {
				if v.chans[i-1].NumMembers < v.chans[i].NumMembers {
					t.Errorf("Most members: %d before %d", v.chans[i-1].NumMembers, v.chans[i].NumMembers)
				}
			}
		}
	}

	// stick_user_sort: kept in search_file_sort, the next search starts there
	if err := e.h.a.SetPref(context.Background(), "stick_user_sort", true); err != nil {
		t.Fatal(err)
	}
	v, d := srView(t, e, fakeslack.SearchWord+"&module=files", false)
	v.sortMenu(gtx)
	m := srMenu(e.h)
	if got := srLabels(m.items); got[0] != "Most relevant" {
		t.Errorf("sticky: first option %q, want Most relevant (no default suffix)", got[0])
	}
	for _, it := range m.items {
		if it.Label == "Newest" {
			it.Do()
		}
	}
	m.Dismiss()
	slUntil(t, 10*time.Second, "search_file_sort", d.frame, func() bool { return e.h.a.PrefString("search_file_sort") == "created" })
	v2, _ := srView(t, e, fakeslack.SearchWord+"&module=files", false)
	if v2.tabSort[srTabFiles] != "created" {
		t.Errorf("the next search starts at %q, want created", v2.tabSort[srTabFiles])
	}
}

// RelaxationSuggestion: a filtered search that finds nothing, when the
// words alone find something, offers "Search without filters", which
// drops the filters and searches again.
func TestSearchViewRelaxation(t *testing.T) {
	e := srNewEnv(t, false)
	var empty string
	for _, id := range e.fs.ChannelIDs() {
		r, err := e.h.a.SearchMessages(context.Background(), core.SearchOpts{Query: fakeslack.SearchWord + " in:<#" + id + ">"})
		if err == nil && r.Total == 0 && r.Unfiltered > 0 {
			empty = id
			break
		}
	}
	if empty == "" {
		t.Fatal("no channel without the fixture word")
	}
	v, d := srView(t, e, fakeslack.SearchWord+" in:<#"+empty+">", false)
	x := d.scan()
	if !srHas(x, srNothingTitle) || !srHas(x, "Search without filters") {
		t.Fatalf("no relaxation: %q", x)
	}
	v.relaxBtn.Click()
	d.frame()
	d.until(t, "the unfiltered search", func() bool { return !v.loading && v.total > 0 })
	if v.q.String() != fakeslack.SearchWord {
		t.Errorf("after Search without filters: %q", v.q.String())
	}
	if x := d.scan(); srHas(x, "Search without filters") {
		t.Error("the button stays once results show")
	}
}

// Pages: Next asks for page 2 (7 results), Previous goes back, and the
// page chips read 1 2.
func TestSearchViewPagination(t *testing.T) {
	e := srNewEnv(t, false)
	v, d := srView(t, e, fakeslack.SearchWord, false)
	v.list.Position.First = len(v.cards) // scroll to the pager
	x := d.scan()
	if !srHas(x, "1") || !srHas(x, "2") {
		t.Errorf("page chips not drawn: %q", x)
	}
	v.nextBtn.Click()
	d.frame()
	d.until(t, "page 2", func() bool { return !v.loading && v.page == 2 })
	if len(v.cards) != fakeslack.SearchFixtureCount-20 {
		t.Errorf("page 2: %d cards", len(v.cards))
	}
	f := e.fs.SearchForms("search.modules.messages")
	if f[len(f)-1].Get("page") != "2" {
		t.Errorf("Next sent page %q", f[len(f)-1].Get("page"))
	}
	v.prevBtn.Click()
	d.frame()
	d.until(t, "page 1", func() bool { return !v.loading && v.page == 1 && len(v.cards) == 20 })
}

// No results: Slack's "Nothing turned up" state; the Files tab lists
// search.modules.files results.
func TestSearchViewEmptyAndFiles(t *testing.T) {
	e := srNewEnv(t, false)
	v, d := srView(t, e, fakeslack.SearchNothing, false)
	x := d.scan()
	if !srHas(x, srNothingTitle) || !srHasPrefix(x, "You may want to try using different keywords") {
		t.Errorf("no-results state not drawn: %q", x)
	}
	if srHas(x, "No results") {
		t.Error("the old invented empty state is still drawn")
	}
	v2, d2 := srView(t, e, fakeslack.SearchWord, false)
	v2.setTab(srTabFiles)
	d2.until(t, "the files tab", func() bool { return !v2.loading && len(v2.files) == 1 })
	if x := d2.scan(); !srHas(x, fakeslack.SearchFileTitle) {
		t.Errorf("file row not drawn: %q", x)
	}
	f := e.fs.SearchForms("search.modules.files")
	if last := f[len(f)-1]; last.Get("search_context") != "desktop_files_tab" || last.Get("count") != "20" {
		t.Errorf("files tab args %v", last)
	}
	_ = v
}

// Search in channel (⌘F): the pane's header and empty state, then
// search.modules.messages for in:<#C> with count 10 and
// search_context search_in_channel.
func TestSearchInChannel(t *testing.T) {
	e := srNewEnv(t, false)
	ch := e.fs.ChannelIDs()[0]
	v := NewSearchInChannel(e.h, ch)
	d := &srDrv{size: image.Pt(420, 800), w: v}
	x := d.scan()
	name := "#" + e.h.a.ConvRef(ch).Name
	for _, want := range []string{"Search in " + name, "Not searching in this channel?", "Go to full search (CTRL G)", "Nothing to show yet."} {
		if !srHas(x, want) {
			t.Errorf("%q not drawn: %q", want, x)
		}
	}
	v.Search(fakeslack.SearchWord)
	d.until(t, "the channel's results", func() bool { return !v.loading && len(v.msgs) > 0 })
	f := e.fs.SearchForms("search.modules.messages")
	last := f[len(f)-1]
	for k, w := range map[string]string{"query": "in:<#" + ch + "> " + fakeslack.SearchWord, "count": "10",
		"search_context": "search_in_channel", "search_exclude_bots": "false", "sort": "score"} {
		if last.Get(k) != w {
			t.Errorf("%s = %q, want %q", k, last.Get(k), w)
		}
	}
	for _, m := range v.msgs {
		if m.Channel != ch {
			t.Errorf("a result from %s", m.Channel)
		}
	}
	x = d.scan()
	if !srHas(x, `Search "zephyr" in `+name) || !srHas(x, "View full search") || !srHasPrefix(x, " results found") {
		t.Errorf("results header not drawn: %q", x)
	}
	v.fullBtn.Click()
	d.frame()
	if got := e.h.navs[len(e.h.navs)-1]; got != "search?q=in%3A%3C%23"+ch+"%3E+zephyr" {
		t.Errorf("View full search: %q", got)
	}
}

// srFeedbackModal returns the last SearchFeedbackModal the host showed.
func srFeedbackModal(h *tzHost) *SearchFeedbackModal {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.shown) - 1; i >= 0; i-- {
		if m, ok := h.shown[i].(*SearchFeedbackModal); ok {
			return m
		}
	}
	return nil
}

// Ghost text is getGhostTextForSelectedSuggestion: the rest of the top
// hit's name and " — Open", " — Search" on the Show results row, nothing
// once a space was typed.
func TestSearchPopoverGhostText(t *testing.T) {
	e := srNewEnv(t, false)
	ch := e.fs.ChannelIDs()[3]
	name := e.h.a.ConvRef(ch).Name
	if len(name) < 4 {
		t.Skipf("channel name %q too short", name)
	}
	p := NewSearchPopover(e.h, name[:3], image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the switcher", func() bool { return p.snapOK && len(p.rows) >= 2 })
	d.frame()
	top := p.rows[p.sel]
	if !strings.HasPrefix(strings.ToLower(top.title), strings.ToLower(name[:3])) {
		t.Skipf("top hit %q doesn't start with the query", top.title)
	}
	if got, want := p.ghostText(), top.title[3:]+" — Open"; got != want {
		t.Errorf("ghost %q, want %q", got, want)
	}
	if x := d.scan(); !srHas(x, top.title[3:]+" — Open") {
		t.Errorf("ghost text not drawn: %q", x)
	}
	for i := range p.rows {
		if p.rows[i].kind == srRShow {
			p.pick(i)
		}
	}
	if got := p.ghostText(); got != " — Search" {
		t.Errorf("Show results ghost %q", got)
	}
	p.query += " "
	if got := p.ghostText(); got != "" {
		t.Errorf("ghost after a space %q", got)
	}
}

// The popover's "Give feedback" is CombinedSearchFeedbackModal with
// feedbackType omniswitcher: Submit stays disabled until a category and
// text are in, then feedback.submitEnterpriseSearchFeedback goes to the
// fake (never to Slack) with the query and the payload, and the
// confirmation replaces the dialog.
func TestSearchFeedbackFromPopover(t *testing.T) {
	e := srNewEnv(t, false)
	RecordRecentPlace(e.fs.ChannelIDs()[0], "", "")
	p := NewSearchPopover(e.h, fakeslack.SearchWord, image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "suggestions", func() bool { return p.snapOK && len(p.msgs) == 3 })
	p.feedback.Click()
	d.frame()
	m := srFeedbackModal(e.h)
	if m == nil || !p.Dismissed() {
		t.Fatalf("feedback modal %v, popover dismissed %v", m, p.Dismissed())
	}
	dm := &srDrv{size: image.Pt(1280, 800), w: m}
	x := dm.scan()
	for _, want := range []string{srFeedbackTitle, srFeedbackAsk, srFeedbackPick, srFeedbackDetails, srFeedbackHint, srFeedbackContact,
		"Privacy Policy", "Cancel", "Submit", "contact", srFeedbackAddress} {
		if !srHas(x, want) {
			t.Errorf("%q not drawn", want)
		}
	}
	if m.submit.enabled() {
		t.Error("Submit enabled with nothing filled in")
	}
	m.Pick(1)
	m.SetText("the channel I wanted isn't there")
	if !m.submit.enabled() {
		t.Fatal("Submit still disabled")
	}
	m.box.Click()
	dm.frame()
	m.Submit()
	slUntil(t, 10*time.Second, "the submission", dm.frame, func() bool {
		return len(e.fs.SearchForms("feedback.submitEnterpriseSearchFeedback")) == 1 && m.Dismissed()
	})
	f := e.fs.SearchForms("feedback.submitEnterpriseSearchFeedback")[0]
	for k, v := range map[string]string{"type": "feedback", "feedback_type": "omniswitcher", "category": "missing_results",
		"text": "the channel I wanted isn't there", "contact": "true", "query": fakeslack.SearchWord, "is_test": "false"} {
		if f.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, f.Get(k), v)
		}
	}
	var pl map[string]any
	if err := json.Unmarshal([]byte(f.Get("payload")), &pl); err != nil {
		t.Fatal(err)
	}
	if pl["query"] != fakeslack.SearchWord || pl["client_version"] != "noversion" || pl["search_session_id"] == "" {
		t.Errorf("payload %v", pl)
	}
	if sg, _ := pl["suggestions"].([]any); len(sg) == 0 {
		t.Errorf("payload without suggestions: %v", pl)
	}
	// the confirmation
	var thanks *Modal
	e.h.mu.Lock()
	if mm, ok := e.h.shown[len(e.h.shown)-1].(*Modal); ok && !mm.Dismissed() {
		thanks = mm
	}
	e.h.mu.Unlock()
	if thanks == nil {
		t.Fatal("no confirmation")
	}
	if x := (&srDrv{size: image.Pt(1280, 800), w: thanks}).scan(); !srHas(x, "OK") || !srHasPrefix(x, "Slack is a tool built by people.") ||
		!srHasPrefix(x, "Thank you!") {
		t.Errorf("confirmation %q", x)
	}
}

// The results page's "Give feedback" sends feedback_type "search" with the
// module, the session and the source menu's counts; the no-results
// state has the same link ("Not the results you expected?").
func TestSearchFeedbackFromResults(t *testing.T) {
	e := srNewEnv(t, false)
	v, d := srView(t, e, fakeslack.SearchWord, false)
	d.until(t, "counts", func() bool { return v.countOK[srTabDMs] && v.countOK[srTabFiles] })
	v.feedback.Click()
	d.frame()
	m := srFeedbackModal(e.h)
	if m == nil {
		t.Fatal("no feedback modal")
	}
	m.Pick(0)
	m.SetText("good")
	m.Submit()
	slUntil(t, 10*time.Second, "the submission", d.frame, func() bool {
		return len(e.fs.SearchForms("feedback.submitEnterpriseSearchFeedback")) == 1
	})
	f := e.fs.SearchForms("feedback.submitEnterpriseSearchFeedback")[0]
	if f.Get("feedback_type") != "search" || f.Get("category") != "helpful" || f.Get("contact") != "false" {
		t.Errorf("form %v", f)
	}
	var pl map[string]any
	if err := json.Unmarshal([]byte(f.Get("payload")), &pl); err != nil {
		t.Fatal(err)
	}
	mc, _ := pl["module_counts"].(map[string]any)
	if pl["module"] != "messages" || pl["active_sort"] != "score" || pl["active_sort_dir"] != "desc" ||
		pl["query_session_id"] == "" || mc["messages"] != float64(fakeslack.SearchFixtureCount) {
		t.Errorf("payload %v", pl)
	}
	_, dn := srView(t, e, fakeslack.SearchNothing, false)
	x := dn.scan()
	if !srHas(x, srNothingAsk) || !srHas(x, "Give feedback") || !strings.Contains(strings.Join(x, " "), "checking for typos") {
		t.Errorf("no-results state %q", x)
	}
}

// Search in channel's footer is searchFeedbackDialog: search.feedback with
// a client_token, type search and the in-channel payload, to the fake.
// Its sort select offers Most relevant, Newest, Oldest and Edit default;
// Oldest asks for timestamp asc.
func TestSearchInChannelFeedbackAndSort(t *testing.T) {
	e := srNewEnv(t, false)
	ch := e.fs.ChannelIDs()[0]
	v := NewSearchInChannel(e.h, ch)
	d := &srDrv{size: image.Pt(420, 800), w: v}
	d.frame()
	v.Search(fakeslack.SearchWord)
	d.until(t, "results", func() bool { return !v.loading && len(v.msgs) > 0 })
	x := d.scan()
	if !srHas(x, "Give feedback") || !srHas(x, "Most relevant (default)") || !srHas(x, "View full search") {
		t.Errorf("results header / footer %q", x)
	}
	v.feedback.Click()
	d.frame()
	slUntil(t, 10*time.Second, "search.feedback", d.frame, func() bool { return len(e.fs.SearchForms("search.feedback")) == 1 })
	f := e.fs.SearchForms("search.feedback")[0]
	if !strings.HasPrefix(f.Get("client_token"), "web-") || f.Get("type") != "search" {
		t.Errorf("search.feedback %v", f)
	}
	var pl map[string]any
	if err := json.Unmarshal([]byte(f.Get("payload")), &pl); err != nil || pl["module"] != "inChannel" || pl["client_version"] != nil {
		t.Errorf("payload %q: %v", f.Get("payload"), err)
	}
	v.sortBtn.Click()
	d.frame()
	m := srMenu(e.h)
	if m == nil {
		t.Fatal("no sort menu")
	}
	if got := srLabels(m.items); !slices.Equal(got, []string{"Most relevant (default)", "Newest", "Oldest", "--", "Edit default"}) {
		t.Errorf("sort menu %q", got)
	}
	m.items[2].Do()
	d.until(t, "the oldest-first search", func() bool { return !v.loading })
	forms := e.fs.SearchForms("search.modules.messages")
	if last := forms[len(forms)-1]; last.Get("sort") != "timestamp" || last.Get("sort_dir") != "asc" {
		t.Errorf("Oldest sent sort %q dir %q", last.Get("sort"), last.Get("sort_dir"))
	}
	v.Search(fakeslack.SearchNothing)
	d.until(t, "no results", func() bool { return !v.loading })
	if x := d.scan(); !srHas(x, srNothingTitle) || !srHas(x, srNothingAsk) || srHas(x, "Nothing to show yet.") {
		t.Errorf("in-channel no-results state %q", x)
	}
}

// Opening a file result: canvases in the canvas view, lists in the list
// view, nothing toasts "isn't supported".
func TestSearchOpenFile(t *testing.T) {
	e := srNewEnv(t, false)
	srOpenFile(e.h, core.File{ID: "F0CANVAS", IsCanvas: true, Title: "Plan"})
	srOpenFile(e.h, core.File{ID: "F0LIST", IsList: true, Title: "Tasks"})
	srOpenFile(e.h, core.File{ID: "F0IMG", IsImage: true, URL: "https://files.example/x.png"})
	if !slices.Equal(e.h.navs, []string{"docs/F0CANVAS", "lists/F0LIST"}) {
		t.Errorf("navs %q", e.h.navs)
	}
	for _, s := range e.h.toastList() {
		if strings.Contains(s, "supported") {
			t.Errorf("toast %q", s)
		}
	}
}

// More: Slack's icons (message, emoji), the Date row's description, File
// type's rows.
func TestSearchMoreMenuDetails(t *testing.T) {
	e := srNewEnv(t, false)
	v, _ := srView(t, e, fakeslack.SearchWord+" after:2026-11-02 before:2026-11-10", false)
	v.moreMenu(layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}})
	m := srMenu(e.h)
	icons := map[string]string{}
	sub := map[string]string{}
	for _, it := range m.items {
		icons[it.Label], sub[it.Label] = it.Icon, it.Subtitle
	}
	if icons["Message has"] != "message" || icons["Reactions"] != "emoji" {
		t.Errorf("icons %v", icons)
	}
	if !strings.HasPrefix(sub["Date"], "Nov 3") {
		t.Errorf("Date description %q", sub["Date"])
	}
}

// Screenshots for CI (SEARCH_SHOTS_DIR): the popover blank and typing,
// its quick filter row and + menu, tokens, date pickers and the picked
// row's toolbar, the results page, its source and More menus, no
// results, the Files tab and search in channel, light and dark.
func TestSearchShots(t *testing.T) {
	dir := os.Getenv("SEARCH_SHOTS_DIR")
	if dir == "" {
		t.Skip("SEARCH_SHOTS_DIR not set")
	}
	shot := func(d *srDrv, name string) {
		t.Helper()
		w, err := headless.NewWindow(d.size.X, d.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		defer w.Release()
		d.frame()
		d.frame()
		if err := w.Frame(&d.ops); err != nil {
			t.Fatalf("gpu frame: %v", err)
		}
		img := image.NewRGBA(image.Rect(0, 0, d.size.X, d.size.Y))
		if err := w.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e := srNewEnv(t, dark)
		chs := e.fs.ChannelIDs()
		RecordRecentPlace(chs[1], "", "")
		RecordRecentPlace("view:later", "To-dos", "todos")
		RecordRecentPlace(chs[2], "", "")

		p := NewQuickSwitcher(e.h)
		d := &srDrv{size: image.Pt(1280, 800), w: p, dark: dark}
		d.until(t, "the snapshot", func() bool { return p.snapOK })
		shot(d, "search-popover-blank-"+mode)

		RecordRecentPlace(chs[0], "", "") // the quick messages' recent channels
		p2 := NewSearchPopover(e.h, fakeslack.SearchWord, image.Rectangle{})
		d = &srDrv{size: image.Pt(1280, 800), w: p2, dark: dark}
		d.until(t, "suggestions", func() bool { return len(p2.msgs) == 3 && len(p2.files) == 1 })
		shot(d, "search-popover-typing-"+mode)
		d.press(key.NameDownArrow) // picks the first row: #1264a3, "Enter Open"
		shot(d, "search-popover-picked-"+mode)

		// the search-filter completer: from: and a name, then Enter takes
		// the person as a from:@name token
		pf := NewSearchPopover(e.h, "", image.Rectangle{})
		d = &srDrv{size: image.Pt(1280, 800), w: pf, dark: dark}
		d.frame()
		d.frame()
		srType(d, pf, "from:"+srTestPerson(t, e).RealName)
		d.until(t, "the people menu", func() bool { return pf.ac.open })
		shot(d, "search-popover-from-menu-"+mode)
		d.press(key.NameReturn)
		shot(d, "search-popover-from-token-"+mode)

		// the quick filter row (search-dates): the filter button on, From
		// me and Has a link on; then its + menu
		qf := NewSearchPopover(e.h, fakeslack.SearchWord, image.Rectangle{})
		d = &srDrv{size: image.Pt(1280, 800), w: qf, dark: dark}
		d.until(t, "the filters switcher", func() bool { return qf.snapOK })
		qf.qfToggleRow()
		qfs := qf.quickFilters()
		qf.qfToggle(qfs[0])
		qf.qfToggle(qfs[2])
		d.until(t, "the filtered suggestions", func() bool { return qf.dirFired })
		shot(d, "search-popover-filters-"+mode)
		qf.qfOpenMenu(qfs)
		qm := srMenu(e.h)
		d.over = func(gtx layout.Context) { qm.Layout(gtx) }
		shot(d, "search-popover-filters-menu-"+mode)
		d.over = nil

		// tokens as pills (the top bar's 1000px box): a person, a channel,
		// a date and you (the mention look); then one hovered (close icon)
		who := srTestPerson(t, e)
		tch := chs[1]
		tcn := e.h.a.ConvRef(tch).Name
		self := e.h.a.Self()
		tk := NewSearchPopover(e.h, "", image.Rect(132, 20, 1132, 46))
		d = &srDrv{size: image.Pt(1280, 800), w: tk, dark: dark}
		d.frame()
		tk.setBox(srJoin([]srPiece{srText(fakeslack.SearchWord + " "),
			srTokPiece(srToken{label: "from:@" + who.DisplayName, req: "from:<@" + who.ID + ">", icon: "user", mod: "from", entity: who.ID}), srSpace(),
			srTokPiece(srToken{label: "in:" + tcn, req: "in:<#" + tch + "|" + tcn + ">", icon: "channel", mod: "in", entity: tch}), srSpace(),
			srTokPiece(srDateToken("after", "2026-11-04", false)), srSpace(),
			srTokPiece(srToken{label: "with:" + srMentionName(self), req: "with:<@" + self.ID + ">", icon: "user", mod: "with", entity: self.ID, self: true}), srSpace(),
		}), -1)
		d.frame()
		shot(d, "search-popover-tokens-"+mode)
		tk.pills.hover = 2
		shot(d, "search-popover-tokens-hover-"+mode)

		// the date picker for a typed before:, then ↓ into its grid
		dpp := NewSearchPopover(e.h, "", image.Rectangle{})
		d = &srDrv{size: image.Pt(1280, 800), w: dpp, dark: dark}
		d.frame()
		d.frame()
		srType(d, dpp, fakeslack.SearchWord+" before:")
		d.until(t, "the date picker", func() bool { return dpp.dpk.open && !dpp.dpk.rect.Empty() })
		shot(d, "search-popover-date-picker-"+mode)
		d.press(key.NameDownArrow)
		shot(d, "search-popover-date-picker-grid-"+mode)

		// Date > Range after its first pick
		rg := NewSearchPopover(e.h, fakeslack.SearchWord, image.Rectangle{})
		d = &srDrv{size: image.Pt(1280, 800), w: rg, dark: dark}
		d.frame()
		rg.enterRange()
		d.until(t, "the range picker", func() bool { return rg.dpk.leftSet && !rg.dpk.rect.Empty() })
		rg.dpk.rcal.pick(dtDay(time.Now()).AddDate(0, 0, -5))
		shot(d, "search-popover-date-range-"+mode)

		// a picked channel row's toolbar, its first button focused (Tab)
		tbch := chs[3]
		tbp := NewSearchPopover(&srSplitHost{tzHost: e.h}, e.h.a.ConvRef(tbch).Name, image.Rectangle{})
		d = &srDrv{size: image.Pt(1280, 800), w: tbp, dark: dark}
		d.until(t, "the toolbar row", func() bool { return tbp.snapOK && len(tbp.rows) > 0 && tbp.rows[0].id == tbch })
		d.press(key.NameDownArrow)
		shot(d, "search-popover-toolbar-"+mode)
		d.press(key.NameTab)
		shot(d, "search-popover-toolbar-focus-"+mode)

		fm := openSearchFeedback(e.h, srFeedbackOpts{feedbackType: "omniswitcher", query: fakeslack.SearchWord})
		fm.Pick(1)
		fm.SetText("The channel I was looking for didn't come up.")
		shot(&srDrv{size: image.Pt(1280, 800), w: fm, dark: dark}, "search-feedback-modal-"+mode)
		fm.err = srFeedbackFailed
		shot(&srDrv{size: image.Pt(1280, 800), w: fm, dark: dark}, "search-feedback-failed-"+mode)
		shot(&srDrv{size: image.Pt(1280, 800), w: newSearchFeedbackThanks(e.h), dark: dark}, "search-feedback-thanks-"+mode)

		v, dv := srView(t, e, fakeslack.SearchWord, dark)
		dv.until(t, "counts", func() bool { return v.countOK[srTabDMs] && v.countOK[srTabFiles] })
		shot(dv, "search-results-"+mode)
		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
		v.typeMenu(gtx)
		m := srMenu(e.h)
		dv.over = func(gtx layout.Context) { m.Layout(gtx) }
		shot(dv, "search-results-sources-"+mode)
		v.moreMenu(gtx)
		m = srMenu(e.h)
		shot(dv, "search-results-more-"+mode)
		dv.over = nil
		v.list.Position.First = len(v.cards)
		shot(dv, "search-results-pages-"+mode)

		_, dn := srView(t, e, fakeslack.SearchNothing, dark)
		shot(dn, "search-results-nothing-"+mode)

		vf, df := srView(t, e, fakeslack.SearchWord, dark)
		vf.setTab(srTabFiles)
		df.until(t, "files", func() bool { return !vf.loading && len(vf.files) == 1 })
		shot(df, "search-results-files-"+mode)

		sc := NewSearchInChannel(e.h, chs[0])
		ds := &srDrv{size: image.Pt(420, 800), w: sc, dark: dark}
		shot(ds, "search-in-channel-empty-"+mode)
		sc.Search(fakeslack.SearchWord)
		ds.until(t, "in-channel results", func() bool { return !sc.loading && len(sc.msgs) > 0 })
		shot(ds, "search-in-channel-results-"+mode)
		sc.Search(fakeslack.SearchNothing)
		ds.until(t, "in-channel no results", func() bool { return !sc.loading })
		shot(ds, "search-in-channel-nothing-"+mode)
	}
}
