package popups

// gap-w24 (docs/GAPS-2026-11-12.md W24, gap 10), against Slack's CDN build
// fa938a086569: in:'s group DMs and workspaces (ltuT
// searchChannelsMembersAndWorkspaces; channel sections never show), the
// Title and Location selects' search field (4TgK / K4vW over 1Vs3
// SearchFilterSearchSelect), the messages sort's server override
// (sort_forced_to_recent, disable_sort_override), a hidden person's
// result cards (Wzwx isMsgFromHiddenSender) and the result menu's save
// and reminder rows (oMi4, ih0g). With SEARCH_SHOTS_DIR set,
// TestSearchW24Shots writes light and dark PNGs. Fake Slack only.

import (
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// srNewEnvSize is srNewEnv on a workspace of size sz.
func srNewEnvSize(t *testing.T, sz fakeslack.Size, dark bool) *srEnv {
	t.Helper()
	fs, err := fakeslack.Start(sz)
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

var srGroupDMSize = fakeslack.Size{Emoji: 50, Users: 30, Channels: 8, DMs: 2, Usergroups: 2, History: 5, GroupDMs: true}

// srKnowGroupDMUsers loads the group DMs' members, as the sidebar does
// for its rows.
func srKnowGroupDMUsers(t *testing.T, e *srEnv) {
	t.Helper()
	var ids []string
	for _, u := range e.fs.GroupDMUsers() {
		ids = append(ids, u.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e.h.a.EnsureUsers(ctx, ids)
}

func srItemIDs(items []srACItem, k srACItemKind) []string {
	var out []string
	for _, it := range items {
		if it.kind == k {
			out = append(out, it.id)
		}
	}
	return out
}

// in: offers group DMs (filterMpims: every one with no query, else each
// word matching a member) after the ranked people and channels, then the
// viewer's workspace; never a sidebar section. A pick is the filter
// formatChannelInsertData / formatWorkspaceInsertData make, sent as
// InQuerySuggestionToken's "<#G…>" / "<T…>", and those refs read back as
// the same tokens.
func TestSearchInCompleterGroupDMsAndWorkspaces(t *testing.T) {
	e := srNewEnvSize(t, srGroupDMSize, false)
	srKnowGroupDMUsers(t, e)
	rk := newSRACRanker(e.h)
	if rk == nil {
		t.Fatal("no ranker")
	}
	gdmUsers := e.fs.GroupDMUsers()
	all := rk.rankIn("", nil, nil)
	mp := srItemIDs(all, srAIMpim)
	for _, id := range []string{fakeslack.GroupDMNamedID, fakeslack.GroupDMPlainID, fakeslack.GroupDMConnectID, fakeslack.GroupDMBigID} {
		if !slices.Contains(mp, id) {
			t.Errorf("no query: group DM %s missing (%q)", id, mp)
		}
	}
	if got := srItemIDs(all, srAITeam); !slices.Equal(got, []string{fakeslack.TeamID}) {
		t.Errorf("workspaces %q, want the viewer's %s", got, fakeslack.TeamID)
	}
	// SearcherSorter: group DMs after the rest, the team in the second tier
	first := slices.IndexFunc(all, func(it srACItem) bool { return it.kind == srAIMpim })
	for i, it := range all {
		if i > first && it.kind != srAIMpim && it.kind != srAITeam {
			t.Errorf("row %d (%s %q) after the group DMs", i, it.id, it.label)
			break
		}
	}
	if all[len(all)-1].kind != srAITeam {
		t.Errorf("last row %+v, want the workspace", all[len(all)-1])
	}
	// the fifth member is only in the big group DM; two words must both match
	only := gdmUsers[3].Name
	if got := srItemIDs(rk.rankIn(only, nil, nil), srAIMpim); !slices.Equal(got, []string{fakeslack.GroupDMBigID}) {
		t.Errorf("%q: group DMs %q, want only the big one", only, got)
	}
	two := gdmUsers[0].Name + " " + gdmUsers[3].Name
	if got := srItemIDs(rk.rankIn(two, nil, nil), srAIMpim); !slices.Equal(got, []string{fakeslack.GroupDMBigID}) {
		t.Errorf("%q: group DMs %q", two, got)
	}
	// with every word matching, fewer members sort first
	if got := srItemIDs(rk.rankIn(gdmUsers[0].Name+","+gdmUsers[1].Name, nil, nil), srAIMpim); len(got) != 4 || got[3] != fakeslack.GroupDMBigID {
		t.Errorf("two members of all four: %q, want the big one last", got)
	}
	if got := srItemIDs(rk.rankIn("zzqqxx", nil, nil), srAIMpim); len(got) != 0 {
		t.Errorf("no member matches: %q", got)
	}
	snap := e.h.a.Snapshot()
	tname := snap.Team.Name
	if tname == "" {
		t.Fatal("the fake team has no name")
	}
	if got := srItemIDs(rk.rankIn(strings.ToLower(tname[:3]), nil, nil), srAITeam); len(got) != 1 {
		t.Errorf("%q: workspaces %q", tname[:3], got)
	}
	if got := srItemIDs(rk.rankIn("zzqqxx", nil, nil), srAITeam); len(got) != 0 {
		t.Errorf("zzqqxx: workspaces %q", got)
	}

	// the picks
	m := srACMatch{kind: srACFilter, typ: "in", mod: "in:"}
	var big, team srACItem
	for _, it := range all {
		switch {
		case it.id == fakeslack.GroupDMBigID:
			big = it
		case it.kind == srAITeam:
			team = it
		}
	}
	cv, _ := e.h.a.Conv(fakeslack.GroupDMBigID)
	title := convTitle(e.h, cv)
	if big.label != title || strings.HasPrefix(title, "mpdm-") {
		t.Errorf("group DM row %q, want the members' names %q", big.label, title)
	}
	if tok := srACPick(m, big); tok.label != "in:"+title || tok.req != "in:<#"+fakeslack.GroupDMBigID+">" || tok.icon != "lock-filled" {
		t.Errorf("group DM pick %+v", tok)
	}
	if tok := srACPick(m, team); tok.label != "in:"+tname || tok.req != "in:<"+fakeslack.TeamID+">" || tok.icon != "channel" {
		t.Errorf("workspace pick %+v", tok)
	}
	neg := srACMatch{kind: srACFilter, typ: "in", mod: "-in:"}
	if tok := srACPick(neg, team); tok.mod != "-in" || tok.req != "-in:<"+fakeslack.TeamID+">" {
		t.Errorf("-in: workspace pick %+v", tok)
	}
	// the display query
	if text, toks := srDisplayQuery(e.h, "in:<#"+fakeslack.GroupDMBigID+"> deploy"); text != "in:"+title+" deploy" || len(toks) != 1 || toks[0].icon != "lock-filled" {
		t.Errorf("group DM ref reads %q %+v", text, toks)
	}
	if text, toks := srDisplayQuery(e.h, "in:<"+fakeslack.TeamID+"> deploy"); text != "in:"+tname+" deploy" || len(toks) != 1 || toks[0].req != "in:<"+fakeslack.TeamID+">" {
		t.Errorf("workspace ref reads %q %+v", text, toks)
	}
	if text, toks := srDisplayQuery(e.h, "<"+fakeslack.TeamID+"> deploy"); text != "<"+fakeslack.TeamID+"> deploy" || len(toks) != 0 {
		t.Errorf("a bare <T…> became %q %+v", text, toks)
	}

	// the popover's menu draws both rows
	p := NewSearchPopover(e.h, "", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.frame()
	d.frame()
	srType(d, p, "in:"+gdmUsers[3].Name)
	d.until(t, "the in: menu", func() bool {
		return p.ac.open && slices.Contains(srItemIDs(p.ac.items, srAIMpim), fakeslack.GroupDMBigID)
	})
	if x := d.scan(); !srHas(x, title) {
		t.Errorf("the group DM row isn't drawn: %q", x)
	}
}

// SearchFilterSearchSelect for Title and Location: the placeholder, the
// chosen values, Clear all past one and five Suggestions without a
// query; with one, the facets the fuzzy matcher scores (by score) and the
// query itself, 200 ms after the last keystroke; a row toggles its value
// and the popover stays open.
func TestSearchFacetSelectLikeSlack(t *testing.T) {
	facets := []core.Facet{{Value: "Brooklyn, NY"}, {Value: "London"}, {Value: "Remote"}, {Value: "Lisbon"},
		{Value: "Berlin"}, {Value: "Boston"}, {Value: "Bogotá"}}
	if got := srFacetSearch(facets, "lon"); !slices.Equal(got, []string{"London"}) {
		t.Errorf("lon: %q", got)
	}
	if got := srFacetSearch(facets, "b"); len(got) != 4 || got[0] != "Brooklyn, NY" {
		t.Errorf("b: %q", got)
	}
	label := func(rows []srFacetRow) []string {
		var out []string
		for _, r := range rows {
			switch r.kind {
			case srFRHeading:
				out = append(out, "# "+r.value)
			case srFRClear:
				out = append(out, "x "+r.value)
			default:
				out = append(out, r.value)
			}
		}
		return out
	}
	if got := label(srFacetRows(nil, facets, "", nil)); !slices.Equal(got,
		[]string{"# Suggestions", "Brooklyn, NY", "London", "Remote", "Lisbon", "Berlin"}) {
		t.Errorf("nothing chosen: %q", got)
	}
	if got := label(srFacetRows([]string{"London", "Paris"}, facets, "", nil)); !slices.Equal(got,
		[]string{"London", "Paris", "x Clear all", "# Suggestions", "Brooklyn, NY", "Remote", "Lisbon", "Berlin"}) {
		t.Errorf("two chosen: %q", got)
	}
	if got := label(srFacetRows(nil, facets, "lon", []string{"London"})); !slices.Equal(got, []string{"London", "lon"}) {
		t.Errorf("a query: %q (want the hits, then the echo row)", got)
	}

	e := srNewEnv(t, false)
	v, d := srView(t, e, fakeslack.SearchPeopleWord, false)
	v.setTab(srTabPeople)
	d.until(t, "people", func() bool { return !v.loading && len(v.people) > 0 && len(v.facetLocs) > 0 })
	v.locationMenu(srTestGtx())
	s, ok := lastShown[*srFacetSelect](e.h)
	if !ok {
		t.Fatal("Location opened no select")
	}
	d.over = func(gtx layout.Context) { s.Layout(gtx) }
	x := d.scan()
	if !srHas(x, "Ex. New York") || !srHas(x, "Suggestions") {
		t.Errorf("Location select: %q", x)
	}
	loc := ""
	for _, r := range s.rows {
		if r.kind == srFROption {
			loc = r.value
			break
		}
	}
	if !slices.Contains(fakeslack.SearchLocations, loc) {
		t.Fatalf("first suggestion %q", loc)
	}
	// type part of it: the hits arrive after the debounce, the echo row last
	q := strings.ToLower(loc[:3])
	s.ed.SetText(q)
	d.frame()
	if s.query != q || len(s.hits) != 0 {
		t.Errorf("the search ran before its 200 ms: hits %q", s.hits)
	}
	d.until(t, "the hits", func() bool { return len(s.hits) > 0 })
	d.frame()
	if got := label(s.rows); len(got) < 2 || got[0] != loc || got[len(got)-1] != q {
		t.Errorf("%q: rows %q", q, got)
	}
	s.pick(0)
	if !slices.Equal(v.locs, []string{loc}) || s.Dismissed() {
		t.Errorf("after a pick: locs %q, dismissed %v", v.locs, s.Dismissed())
	}
	d.until(t, "one location", func() bool { return !v.loading })
	forms := e.fs.SearchForms("search.modules.people")
	if got := forms[len(forms)-1].Get("profile_location"); got != loc {
		t.Errorf("profile_location %q, want %q", got, loc)
	}
	// the echo row adds a value nobody has; a chosen row's click takes it off
	s.pick(len(s.rows) - 1)
	if !slices.Equal(v.locs, []string{loc, q}) {
		t.Errorf("echo row: locs %q", v.locs)
	}
	s.pick(0)
	if !slices.Equal(v.locs, []string{q}) {
		t.Errorf("unpicked: locs %q", v.locs)
	}
	s.Dismiss()

	// Title's placeholder
	v.titleMenu(srTestGtx())
	ts, _ := lastShown[*srFacetSelect](e.h)
	d.over = func(gtx layout.Context) { ts.Layout(gtx) }
	if x := d.scan(); !srHas(x, "Ex. Director") {
		t.Errorf("Title select: %q", x)
	}
	d.over = nil
}

// sort_forced_to_recent: the server sorted newest first, so the select
// shows Newest (with its check); picking Most relevant sends
// disable_sort_override once, and the next search doesn't.
func TestSearchSortForcedToRecentLikeSlack(t *testing.T) {
	e := srNewEnv(t, false)
	e.fs.SetSortForcedToRecent(true)
	v, d := srView(t, e, fakeslack.SearchWord, false)
	if !v.sortState.forced || v.sortBy != "score" {
		t.Fatalf("state %+v sort %q", v.sortState, v.sortBy)
	}
	if x := d.scan(); !srHas(x, "Newest") || srHas(x, "Most relevant (default)") {
		t.Errorf("forced: the select reads %q", x)
	}
	v.sortMenu(srTestGtx())
	m := srMenu(e.h)
	for _, it := range m.items {
		if it.Checked != (it.Label == "Newest") {
			t.Errorf("forced: %q checked %v", it.Label, it.Checked)
		}
	}
	for _, it := range m.items {
		if it.Label == "Most relevant (default)" {
			it.Do()
		}
	}
	m.Dismiss()
	d.until(t, "the relevant search", func() bool { return !v.loading })
	forms := e.fs.SearchForms("search.modules.messages")
	last := forms[len(forms)-1]
	if last.Get("disable_sort_override") != "true" || last.Get("sort") != "score" {
		t.Errorf("Most relevant: sort %q disable_sort_override %q", last.Get("sort"), last.Get("disable_sort_override"))
	}
	if v.sortState.forced || v.sortState.noOverride {
		t.Errorf("after the answer: %+v", v.sortState)
	}
	if x := d.scan(); !srHas(x, "Most relevant (default)") {
		t.Errorf("not forced: the select reads %q", x)
	}
	v.search(1)
	d.until(t, "the next search", func() bool { return !v.loading })
	forms = e.fs.SearchForms("search.modules.messages")
	if forms[len(forms)-1].Has("disable_sort_override") {
		t.Error("the next search still sends disable_sort_override")
	}
	if !v.sortState.forced {
		t.Error("the next answer forced the sort again, but the state didn't follow")
	}

	// search in channel: the same select state, its request too
	ch := e.fs.ChannelIDs()[0]
	sc := NewSearchInChannel(e.h, ch)
	dc := &srDrv{size: image.Pt(420, 800), w: sc}
	sc.Search(fakeslack.SearchWord)
	dc.until(t, "the in-channel search", func() bool { return !sc.loading })
	if !sc.sortSt.forced {
		t.Fatal("in channel: not forced")
	}
	if x := dc.scan(); !srHas(x, "Newest") {
		t.Errorf("in channel, forced: %q", x)
	}
	sc.sortSt.picked("score")
	sc.Search(sc.query)
	dc.until(t, "the in-channel relevant search", func() bool { return !sc.loading })
	forms = e.fs.SearchForms("search.modules.messages")
	if last := forms[len(forms)-1]; last.Get("search_context") != "search_in_channel" || last.Get("disable_sort_override") != "true" {
		t.Errorf("in channel, Most relevant: %v", last)
	}
}

// srHideFirstSender hides the first result's sender (hidden_users) and
// returns them.
func srHideFirstSender(t *testing.T, e *srEnv, msgs []core.Message) string {
	t.Helper()
	who := ""
	for _, m := range msgs {
		if m.User != "" && m.User != e.h.a.Self().ID {
			who = m.User
			break
		}
	}
	if who == "" {
		t.Fatal("no other sender in the results")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.h.a.HideUser(ctx, who, ""); err != nil {
		t.Fatal(err)
	}
	return who
}

// A hidden person's result: no header (sender, channel, time), the
// HiddenMessageBody line without Show anyway, the hidden avatar.
func TestSearchHiddenSenderCardsLikeSlack(t *testing.T) {
	e := srNewEnv(t, false)
	v, d := srView(t, e, fakeslack.SearchWord, false)
	if len(v.msgs) == 0 {
		t.Fatal("no results")
	}
	who := srHideFirstSender(t, e, v.msgs)
	x := d.scan()
	for _, want := range []string{"This message is hidden", "You’re hiding messages from this person."} {
		if !srHas(x, want) {
			t.Errorf("%q not drawn: %q", want, x)
		}
	}
	if srHas(x, "Show anyway") {
		t.Error("search drew Show anyway (renderShowMessageLink false)")
	}
	for i := range v.msgs {
		if v.msgs[i].User == who && !srHiddenSender(e.h, &v.msgs[i]) {
			t.Errorf("result %d isn't from a hidden sender", i)
		}
	}
	// search in channel
	ch := v.msgs[0].Channel
	sc := NewSearchInChannel(e.h, ch)
	dc := &srDrv{size: image.Pt(420, 800), w: sc}
	sc.Search(fakeslack.SearchWord)
	dc.until(t, "the in-channel search", func() bool { return !sc.loading })
	hid := slices.ContainsFunc(sc.msgs, func(m core.Message) bool { return m.User == who })
	if x := dc.scan(); hid != srHas(x, "This message is hidden") {
		t.Errorf("in channel (a hidden sender's result: %v): %q", hid, x)
	}
}

// The result menu's save and reminder rows follow the message's saved
// metadata as the message menu's do (oMi4, ih0g).
func TestSearchResultMenuLaterRowsLikeSlack(t *testing.T) {
	h := hcEnv(t)
	a := h.Core()
	m := mkMsg(false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows := func() (string, string) {
		items := srResultItems(h, m)
		return items[0].Label, items[1].Label
	}
	check := func(stage, save, remind string) {
		t.Helper()
		s, r := rows()
		if s != save || r != remind {
			t.Errorf("%s: %q / %q, want %q / %q", stage, s, r, save, remind)
		}
		if items := srResultItems(h, m); items[0].Icon != SaveActionFor(a, &m).Icon {
			t.Errorf("%s: save icon %q", stage, items[0].Icon)
		}
	}
	check("not saved", "Add to saved", "Add a reminder")
	if err := a.Save(ctx, m.Channel, m.TS, 0); err != nil {
		t.Fatal(err)
	}
	check("saved", "Remove from Saved", "Add a reminder")
	if err := a.MoveToTodos(ctx, m.Channel, m.TS, 0); err != nil {
		t.Fatal(err)
	}
	check("a to-do", "Move to Saved", "Edit reminder")
	// the save row's click is RunSaveAction's: "To-do moved to Saved"
	srResultItems(h, m)[0].Do()
	slUntil(t, 10*time.Second, "the toast", func() { drain() }, func() bool {
		return slices.Contains(h.toastList(), "To-do moved to Saved")
	})
}

// Light and dark PNGs: a hidden person's card, in:'s group DM and
// workspace rows, the Location select with a query.
func TestSearchW24Shots(t *testing.T) {
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
		e := srNewEnvSize(t, srGroupDMSize, dark)
		srKnowGroupDMUsers(t, e)
		v, d := srView(t, e, fakeslack.SearchWord, dark)
		srHideFirstSender(t, e, v.msgs)
		shot(d, "w24-search-hidden-sender-"+mode)

		p := NewSearchPopover(e.h, "", image.Rectangle{})
		dpo := &srDrv{size: image.Pt(1280, 800), w: p, dark: dark}
		dpo.frame()
		dpo.frame()
		srType(dpo, p, "in:")
		dpo.until(t, "the in: menu", func() bool {
			return p.ac.open && len(srItemIDs(p.ac.items, srAITeam)) > 0
		})
		// scroll to the group DMs and the workspace at the end
		p.ac.sel, p.ac.pseudo, p.ac.scroll = len(p.ac.items)-2, false, true
		shot(dpo, "w24-search-in-groupdms-"+mode)

		vp, dpe := srView(t, e, fakeslack.SearchPeopleWord, dark)
		vp.setTab(srTabPeople)
		dpe.until(t, "people", func() bool { return !vp.loading && len(vp.people) > 0 && len(vp.facetLocs) > 0 })
		vp.locs = []string{vp.facetLocs[0].Value}
		vp.search(1)
		dpe.until(t, "a location", func() bool { return !vp.loading })
		vp.locationMenu(srTestGtx())
		s, _ := lastShown[*srFacetSelect](e.h)
		dpe.over = func(gtx layout.Context) { s.Layout(gtx) }
		shot(dpe, "w24-search-location-select-"+mode)
		s.ed.SetText(strings.ToLower(vp.facetLocs[0].Value[:2]))
		dpe.until(t, "the hits", func() bool { return len(s.hits) > 0 })
		shot(dpe, "w24-search-location-query-"+mode)
		dpe.over = nil
	}
}
