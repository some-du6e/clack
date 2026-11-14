package popups

// gap-w6 (docs/GAPS-2026-11-11.md W6), against Slack's CDN build
// d3de3a487b1c: search in channel's header (the DM's avatar, "in" and the
// channel icon), loading skeleton and empty-state illustration; the
// results page's Slack logo on the source pill, the Channels and People
// tabs' pills and More's counts.

import (
	"image"
	"slices"
	"strings"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/icons"
)

func srTestGtx() layout.Context {
	return layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
}

// srDMWith is a 1:1 DM in the snapshot and its person.
func srDMWith(t *testing.T, e *srEnv) (string, core.User) {
	t.Helper()
	for _, c := range e.h.a.Snapshot().Convs {
		if c.Kind != "im" {
			continue
		}
		ref := e.h.a.ConvRef(c.ID)
		if u, ok := lookupUser(e.h, ref.User); ok && ref.User != e.h.a.Self().ID {
			return c.ID, u
		}
	}
	t.Fatal("no DM in the snapshot")
	return "", core.User{}
}

// SearchInChannelView's header: "Search", then "in" and the channel's
// name without its # (the hash icon drawn instead); a DM's shows the
// person's 16px avatar and name; with keywords, '"kw"' between them.
func TestSearchInChannelHeader(t *testing.T) {
	e := srNewEnv(t, false)
	ch := e.fs.ChannelIDs()[0]
	v := NewSearchInChannel(e.h, ch)
	d := &srDrv{size: image.Pt(420, 800), w: v}
	x := d.scan()
	name := e.h.a.ConvRef(ch).Name
	for _, want := range []string{"Search", "in", name} {
		if !srHas(x, want) {
			t.Errorf("channel header: %q not drawn: %q", want, x)
		}
	}
	if srHas(x, "Search in #"+name) || srHas(x, "#"+name) {
		t.Errorf("the header still writes the # (%q)", x)
	}
	v.Search(fakeslack.SearchWord)
	d.until(t, "the results", func() bool { return !v.loading })
	if x := d.scan(); !srHas(x, `"`+fakeslack.SearchWord+`"`) || !srHas(x, "Search") {
		t.Errorf("keywords header %q", x)
	}

	dm, who := srDMWith(t, e)
	v = NewSearchInChannel(e.h, dm)
	d = &srDrv{size: image.Pt(420, 800), w: v}
	x = d.scan()
	if !srHas(x, "in") || !srHas(x, userName(who)) {
		t.Errorf("DM header: %q", x)
	}
	if who.Avatar == "" {
		t.Fatalf("%s has no avatar", userName(who))
	}
	// the test host's fetcher fails every image: a requested avatar fails
	d.until(t, "the DM header's avatar request", func() bool { return e.h.im.Failed(who.Avatar, 16) })
}

// listLoadingFragment: while the search runs the pane draws the skeleton
// (no text), its shimmer started; with animations off it stays still.
func TestSearchInChannelSkeleton(t *testing.T) {
	e := srNewEnv(t, false)
	v := NewSearchInChannel(e.h, e.fs.ChannelIDs()[0])
	d := &srDrv{size: image.Pt(420, 800), w: v}
	d.frame()
	v.loading, v.searched = true, true
	x := d.scan()
	for _, s := range x {
		if strings.Contains(s, "Nothing to show") || strings.Contains(s, "results found") {
			t.Errorf("text %q under the skeleton", s)
		}
	}
	if v.skelStart.IsZero() {
		t.Error("the shimmer didn't start")
	}
	// the random bars are one of MissingText's three widths, stable per bar
	for i := 0; i < 7; i++ {
		for b := 0; b < 6; b++ {
			w := srcSkelRandom(i, b)
			if !slices.Contains(srcSkelWidths[:], w) || w != srcSkelRandom(i, b) {
				t.Fatalf("bar %d/%d width %v", i, b, w)
			}
		}
	}
	v.loading = false
}

// The "empty-state" row: the illustration (rasterised from
// empty-search-light-b73db25.svg, 159×140 and 318×280), "Nothing to show
// yet." and the description with "from: @name" bold.
func TestSearchInChannelEmptyState(t *testing.T) {
	for _, px := range []float32{1, 2} {
		img, ok := srcEmptyImage(px)
		want := image.Pt(159, 140).Mul(int(px))
		if !ok || img.Size() != want {
			t.Errorf("illustration at %vx: ok %v, %v, want %v", px, ok, img.Size(), want)
		}
	}
	e := srNewEnv(t, false)
	v := NewSearchInChannel(e.h, e.fs.ChannelIDs()[0])
	d := &srDrv{size: image.Pt(420, 800), w: v}
	x := d.scan()
	for _, want := range []string{"Nothing to show yet.", "Type", "from:", "@name", "looking", "for."} {
		if !srHas(x, want) {
			t.Errorf("%q not drawn: %q", want, x)
		}
	}
	parts := nsSearch.RT("Type a keyword or <strong>from: @name</strong> to find that message you’re looking for.")
	bold := ""
	for _, p := range parts {
		if p.Tag == "strong" {
			bold += p.Text
		}
	}
	if bold != "from: @name" {
		t.Errorf("the description's bold run %q", bold)
	}
}

// SearchFilterSourcesDisplay: Slack's four-colour logo before every Slack
// source; a work object source keeps its app's icon.
func TestSearchSourcePillLogo(t *testing.T) {
	e := srNewEnv(t, false)
	v, _ := srView(t, e, fakeslack.SearchWord, false)
	for tab := 0; tab < srTabN; tab++ {
		v.tab = tab
		if s := v.pillSpec(srFSources); s.icon != srSlackMark || s.label != srTabNames[tab] {
			t.Errorf("tab %d source pill %+v", tab, s)
		}
	}
	// the logo is the icons package's slack-logo-mark art (its eight
	// paths and four colours are held to Slack's file there)
	if !icons.HasArt(srSlackMark) {
		t.Fatalf("no %q art", srSlackMark)
	}
	gtx := srTestGtx()
	if d := icons.Art(gtx, srSlackMark, 15); d.Size != image.Pt(15, 15) {
		t.Errorf("logo size %v", d.Size)
	}
}

// getMoreFiltersCount: the More button reads "More (N)" and turns active
// for the filters it offers; Includes me alone and Last 7 days don't
// count; its submenus carry blue badges kept on the highlighted row.
func TestSearchMoreCounts(t *testing.T) {
	e := srNewEnv(t, false)
	who := srTestPerson(t, e)
	self := "<@" + e.h.a.Self().ID + ">"
	v, d := srView(t, e, fakeslack.SearchWord+" with:"+self, false)
	allowed := srMoreAllowed(srTabMessages, srFiltersShown(srTabMessages), false)
	if n := v.moreState().count(allowed); n != 0 {
		t.Errorf("Includes me alone counts %d", n)
	}
	if x := d.scan(); !srHas(x, "More") || srHasPrefix(x, "More (") {
		t.Errorf("More label %q", x)
	}
	v.setQuery(srParse(fakeslack.SearchWord + " with:<@" + who.ID + "> has:pin is:saved type:pdfs type:images"))
	d.until(t, "the filtered results", func() bool { return !v.loading })
	// With (1: someone other than you), Pinned, Saved, two file types
	if n := v.moreState().count(allowed); n != 5 {
		t.Errorf("More count %d, want 5", n)
	}
	if x := d.scan(); !srHas(x, "More (5)") {
		t.Errorf("More (5) not drawn: %q", x)
	}
	v.moreMenu(srTestGtx())
	m := srMenu(e.h)
	badges := map[string]int{}
	for _, it := range m.items {
		if it.BadgeCount > 0 {
			if it.BadgeStyle != "blue" || !it.BadgeHighlighted {
				t.Errorf("%s badge style %q highlighted %v", it.Label, it.BadgeStyle, it.BadgeHighlighted)
			}
			badges[it.Label] = it.BadgeCount
		}
	}
	if badges["With people"] != 1 || badges["Message is"] != 2 || badges["File type"] != 2 || badges["From"] != 0 {
		t.Errorf("badges %v", badges)
	}
	if srMoreLabel(0) != "More" || srMoreLabel(12) != "More (12)" {
		t.Errorf("labels %q %q", srMoreLabel(0), srMoreLabel(12))
	}
}

// The Channels tab: All channels, Any channel type and Workspaces pills
// (no More: it has nothing left to offer); Channel type sends channel_type
// (Public as private_exclude), All channels the two prefs.
func TestSearchChannelsPills(t *testing.T) {
	e := srNewEnv(t, false)
	ch := e.fs.ChannelIDs()[1]
	word := strings.Split(e.h.a.ConvRef(ch).Name, "-")[0]
	v, d := srView(t, e, word, false)
	v.setTab(srTabChannels)
	d.until(t, "channels", func() bool { return !v.loading && len(v.chans) > 0 })
	x := d.scan()
	for _, want := range []string{"Channels", "All channels", "Any channel type", "Workspaces"} {
		if !srHas(x, want) {
			t.Errorf("%q not drawn: %q", want, x)
		}
	}
	if srHasPrefix(x, "More") {
		t.Errorf("the Channels tab draws More: %q", x)
	}
	gtx := srTestGtx()
	v.channelTypeMenu(gtx)
	m := srMenu(e.h)
	if got := srLabels(m.items); !slices.Equal(got, []string{"Any channel type", "Public", "Private", "Archived", "External"}) {
		t.Fatalf("Channel type menu %q", got)
	}
	m.items[1].Do()
	d.until(t, "public channels", func() bool { return !v.loading })
	forms := e.fs.SearchForms("search.modules.channels")
	if got := forms[len(forms)-1].Get("channel_type"); got != "private_exclude" {
		t.Errorf("Public sent channel_type %q", got)
	}
	for _, c := range v.chans {
		if c.Kind == "private" {
			t.Errorf("Public listed private %s", c.Name)
		}
	}
	if x := d.scan(); !srHas(x, "Public") || !srHas(x, "Reset") {
		t.Errorf("after Public: %q", x)
	}
	v.allChannelsMenu(gtx)
	m = srMenu(e.h)
	if got := srLabels(m.items); !slices.Equal(got, []string{"All channels", "Channels you’ve joined", "Other channels"}) {
		t.Fatalf("All channels menu %q", got)
	}
	m.items[1].Do()
	d.until(t, "Channels you've joined", func() bool {
		f := e.fs.SearchForms("search.modules.channels")
		return e.h.a.PrefBool("search_only_my_channels", false) && f[len(f)-1].Get("search_only_my_channels") == "true"
	})
	if x := d.scan(); !srHas(x, "Channels you’ve joined") {
		t.Errorf("the pill after Channels you've joined: %q", x)
	}
	v.resetFilters()
	d.until(t, "reset", func() bool {
		return !e.h.a.PrefBool("search_only_my_channels", false) && v.chanType == "" && !v.loading
	})
}

// The People tab: Title and Location pills fed by the response's facets
// (Suggestions: five at most, the chosen ones first, Clear all past one);
// More offers Account type (account_type) and Celebration.
func TestSearchPeoplePills(t *testing.T) {
	e := srNewEnv(t, false)
	v, d := srView(t, e, fakeslack.SearchPeopleWord, false)
	v.setTab(srTabPeople)
	d.until(t, "people", func() bool { return !v.loading && len(v.people) > 0 && len(v.facetLocs) > 0 })
	x := d.scan()
	for _, want := range []string{"Title", "Location"} {
		if !srHas(x, want) {
			t.Errorf("%q not drawn: %q", want, x)
		}
	}
	gtx := srTestGtx()
	// the Location select (srFacetSelect, gap-w24): its rows
	facetSel := func() (*srFacetSelect, []srFacetRow) {
		t.Helper()
		v.locationMenu(gtx)
		s, ok := lastShown[*srFacetSelect](e.h)
		if !ok {
			t.Fatal("Location opened no select")
		}
		return s, srFacetRows(s.chosen(), s.facets(), "", nil)
	}
	sel, rows := facetSel()
	if len(rows) < 2 || rows[0].kind != srFRHeading || rows[0].value != "Suggestions" {
		t.Fatalf("Location menu %+v", rows)
	}
	loc := rows[1].value
	if !slices.Contains(fakeslack.SearchLocations, loc) {
		t.Errorf("suggested location %q", loc)
	}
	all := v.total
	sel.rows = rows
	sel.pick(1)
	sel.Dismiss()
	d.until(t, "one location", func() bool { return !v.loading })
	forms := e.fs.SearchForms("search.modules.people")
	if got := forms[len(forms)-1].Get("profile_location"); got != loc {
		t.Errorf("profile_location %q, want %q", got, loc)
	}
	if v.total >= all {
		t.Errorf("%s: %d people of %d", loc, v.total, all)
	}
	if x := d.scan(); !srHas(x, "Location: "+loc) {
		t.Errorf("the pill after %s: %q", loc, x)
	}
	sel, rows = facetSel()
	if rows[0].kind != srFROption || rows[0].value != loc || !slices.Contains(sel.chosen(), loc) {
		t.Errorf("the chosen location isn't first: %+v", rows)
	}
	// a second location: "Multiple locations" and Clear all
	sel.rows = rows
	for i, r := range rows {
		if r.kind == srFROption && r.value != loc && slices.Contains(fakeslack.SearchLocations, r.value) {
			sel.pick(i)
			break
		}
	}
	sel.Dismiss()
	d.until(t, "two locations", func() bool { return !v.loading && len(v.locs) == 2 })
	if x := d.scan(); !srHas(x, "Multiple locations") {
		t.Errorf("two locations: %q", x)
	}
	sel, rows = facetSel()
	if !slices.ContainsFunc(rows, func(r srFacetRow) bool { return r.kind == srFRClear && r.value == "Clear all" }) {
		t.Errorf("no Clear all: %+v", rows)
	}
	sel.Dismiss()
	// More: Account type, then Celebration
	v.moreMenu(gtx)
	m := srMenu(e.h)
	if got := srLabels(m.items); !slices.Equal(got, []string{"Account type", "Celebration"}) {
		t.Fatalf("People More %q", got)
	}
	var guests MenuItem
	for _, it := range m.items[0].Sub {
		if it.Key == "guests" {
			guests = it
		}
	}
	guests.Do()
	d.until(t, "guests", func() bool { return !v.loading })
	forms = e.fs.SearchForms("search.modules.people")
	if got := forms[len(forms)-1].Get("account_type"); got != core.SearchAccountTypes["guests"] {
		t.Errorf("account_type %q", got)
	}
	if x := d.scan(); !srHas(x, "More (1)") {
		t.Errorf("More (1) not drawn: %q", x)
	}
}
