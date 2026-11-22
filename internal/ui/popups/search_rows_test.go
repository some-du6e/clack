package popups

// The results page's Files, Channels and People results and the search
// modal's "Results feel off? Learn more" (search_rows.go, Slack build
// c07ab895506f's search-view_tsx and boot-shared, client-boot-styles
// cf726c4f2eaecc74). With SEARCH_SHOTS_DIR set, TestSearchRowsShots
// writes light and dark PNGs. Fake Slack only.

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

func srMoveTo(d *srDrv, x, y int) {
	d.r.Queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(float32(x), float32(y)), Source: pointer.Mouse})
	d.frame()
	d.frame()
}

func srClickAt(d *srDrv, x, y int) {
	pt := f32.Pt(float32(x), float32(y))
	d.r.Queue(pointer.Event{Kind: pointer.Move, Position: pt, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Press, Position: pt, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Release, Position: pt, Source: pointer.Mouse})
	d.frame()
	d.frame()
}

func srJoined(texts []string) string { return strings.Join(texts, "\x00") }

// The row metrics at 1x: a file card 70 tall and 8 under it, a channel
// row 75 (the last 76), a 1152px grid of four 273px columns.
func TestSearchRowsMetrics(t *testing.T) {
	gtx := srTestGtx()
	if h := srFileRowH(gtx); h != 78 {
		t.Errorf("file row %d, want 1 + 16 + 36 + 16 + 1 + 8", h)
	}
	if h, last := srChanRowH(gtx, 0, 3), srChanRowH(gtx, 2, 3); h != 75 || last != 76 {
		t.Errorf("channel rows %d / last %d, want 1 + 74.25 and its bottom border", h, last)
	}
	for _, c := range []struct{ w, cols, cell int }{{1152, 4, 273}, {1079, 4, 254}, {1059, 3, 339}, {520, 2, 250}, {519, 1, 519}, {200, 1, 200}} {
		if cols, cell := srPeopleGrid(gtx, c.w); cols != c.cols || cell != c.cell {
			t.Errorf("grid %d: %d × %d, want %d × %d (auto-fill minmax(250px, 1fr), gap 20)", c.w, cols, cell, c.cols, c.cell)
		}
	}
	u := core.User{ID: "U1", TZ: "Europe/Berlin"}
	if h := srPersonCardH(gtx, nil, u); h != 96+1+46 {
		t.Errorf("card with a local time %d", h)
	}
	u.IsAdmin = true // no host: a plain workspace, so the admin keeps its band
	if h := srPersonCardH(gtx, nil, u); h != 96+1+46+35 {
		t.Errorf("admin card %d", h)
	}
	if h := srPersonCardH(gtx, nil, core.User{ID: "U2"}); h != 96 {
		t.Errorf("card without a time zone %d", h)
	}
}

// FileEntityUserTimestamp and getActionText.
func TestSearchRowsFileText(t *testing.T) {
	e := srNewEnv(t, false)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)
	self := e.h.a.Self()
	f := core.File{ID: "F1", Title: "Plan", User: self.ID, Created: now.Add(-48 * time.Hour).Unix()}
	name := userName(self)
	if got := srFileDetail(e.h, f, false, now); got != "Shared by "+name+" on Nov 13th" {
		t.Errorf("detail %q", got)
	}
	if got := srFileDetail(e.h, f, true, now); !strings.HasPrefix(got, "Created by "+name) {
		t.Errorf("with a creator: filter %q", got)
	}
	f.Modified = now.Add(-time.Hour).Unix() // getFileTimestamp: updated first
	if got := srFileDetail(e.h, f, false, now); got != "Shared by "+name+" today" {
		t.Errorf("updated today %q", got)
	}
	parts := srFileActionParts(core.File{IsImage: true, PrettyType: "PNG"})
	if len(parts) != 3 || parts[1].Text != "PNG" || parts[1].Tag != "strong" {
		t.Errorf("image action %+v, want View <strong>PNG</strong> in Slack", parts)
	}
	for f, want := range map[*core.File]string{{IsCanvas: true}: "Open canvas", {IsList: true}: "Open list", {Filetype: "pdf"}: "Open in browser"} {
		if p := srFileActionParts(*f); len(p) != 1 || p[0].Text != want {
			t.Errorf("action %+v, want %q", p, want)
		}
	}
}

// The Files tab: one card per file with its detail line, the action text
// under the pointer; a click opens it.
func TestSearchRowsFiles(t *testing.T) {
	e := srNewEnv(t, false)
	v, d := srView(t, e, fakeslack.SearchWord, false)
	v.setTab(srTabFiles)
	d.until(t, "files", func() bool { return !v.loading && len(v.files) > 0 })
	f := v.files[0]
	x := srJoined(d.scan())
	if !strings.Contains(x, srFileTitle(f)) || !strings.Contains(x, srFileDetail(e.h, f, false, time.Now())) {
		t.Fatalf("title %q / detail %q not drawn: %q", srFileTitle(f), srFileDetail(e.h, f, false, time.Now()), x)
	}
	px := v.padX(srTestGtx(), d.size.X)
	srMoveTo(d, px+300, v.listY+30)
	if !v.row(f.ID).Hovered() {
		t.Fatal("the card isn't hovered")
	}
	x = srJoined(d.scan())
	var act []string
	for _, p := range srFileActionParts(f) {
		act = append(act, p.Text)
	}
	for _, s := range act {
		if !strings.Contains(x, s) {
			t.Errorf("hover: action %q not drawn: %q", act, x)
		}
	}
	if strings.Contains(x, srFileDetail(e.h, f, false, time.Now())) {
		t.Error("hover: the detail line stayed")
	}
}

// srChanAt finds a Channels result that's public, joined and not
// #general.
func srChanAt(t *testing.T, e *srEnv, v *SearchView) int {
	t.Helper()
	for i, ch := range v.chans {
		if cv, ok := e.h.a.Conv(ch.ID); ch.Kind == "channel" && ch.IsMember && !ch.IsArchived && !(ok && cv.IsGeneral) {
			return i
		}
	}
	t.Fatalf("no public joined channel in %+v", v.chans)
	return -1
}

// The Channels tab: Joined and the member count in the metadata; under
// the pointer "View channel", Open in Home and Joined, which reads Leave
// over it and leaves; then Join with the "Join…" caret.
func TestSearchRowsChannels(t *testing.T) {
	e := srNewEnv(t, false)
	ch := e.fs.ChannelIDs()[1]
	word := strings.Split(e.h.a.ConvRef(ch).Name, "-")[0]
	v, d := srView(t, e, word, false)
	v.setTab(srTabChannels)
	d.until(t, "channels", func() bool { return !v.loading && len(v.chans) > 0 })
	i := srChanAt(t, e, v)
	hit := v.chans[i]
	x := srJoined(d.scan())
	members := nsSearch.A("{count, plural, =1 {# member} other {# members}}", map[string]any{"count": hit.NumMembers})
	if !strings.Contains(x, "Joined") || !strings.Contains(x, members) {
		t.Fatalf("metadata Joined / %q not drawn: %q", members, x)
	}
	if strings.Contains(x, "Open in Home") || strings.Contains(x, "View channel") {
		t.Fatalf("actions drawn without the pointer: %q", x)
	}
	gtx := srTestGtx()
	px := v.padX(gtx, d.size.X)
	cw := d.size.X - 2*px
	y0, ok := v.srChanRowY(gtx, i)
	if !ok {
		t.Fatalf("row %d isn't on screen", i)
	}
	srMoveTo(d, px+cw/2, y0+40)
	x = srJoined(d.scan())
	for _, want := range []string{"Open in Home", "Joined", "View channel"} {
		if !strings.Contains(x, want) {
			t.Errorf("hover: %q not drawn: %q", want, x)
		}
	}
	s := v.chanState(v.chans[i])
	b := v.chanButtons(gtx, e.h.Theme(), i, cw, s, "Joined")
	jx, jy := px+b.join.Min.X+b.join.Dx()/2, y0+b.join.Min.Y+b.join.Dy()/2
	srMoveTo(d, jx, jy)
	if x := srJoined(d.scan()); !strings.Contains(x, "Leave") || strings.Contains(x, "View channel") {
		t.Errorf("over the button: want Leave and no View channel: %q", x)
	}
	srClickAt(d, jx, jy)
	d.until(t, "conversations.leave", func() bool { return e.fs.CallCount("conversations.leave") == 1 && !v.chans[i].IsMember })
	e.h.mu.Lock()
	early := len(e.h.navs)
	e.h.mu.Unlock()
	if early != 0 {
		t.Errorf("Leave navigated %d times", early)
	}
	s = v.chanState(v.chans[i])
	if !s.temp {
		t.Fatalf("a public channel without the viewer isn't OtvL-eligible: %+v", s)
	}
	b = v.chanButtons(gtx, e.h.Theme(), i, cw, s, "Join")
	srMoveTo(d, px+b.caret.Min.X+b.caret.Dx()/2, y0+b.caret.Min.Y+b.caret.Dy()/2)
	if x := srJoined(d.scan()); !strings.Contains(x, "Join") {
		t.Errorf("after leaving: no Join: %q", x)
	}
	srClickAt(d, px+b.caret.Min.X+b.caret.Dx()/2, y0+b.caret.Min.Y+b.caret.Dy()/2)
	m := srMenu(e.h)
	if m == nil || strings.Join(srLabels(m.items), "|") != "# Join…|Just for today|For 48 hours|For 1 week" {
		t.Fatalf("the caret's menu %v", m)
	}
	jx, jy = px+b.join.Min.X+b.join.Dx()/2, y0+b.join.Min.Y+b.join.Dy()/2
	srClickAt(d, jx, jy)
	d.until(t, "conversations.join", func() bool { return e.fs.CallCount("conversations.join") == 1 && v.chans[i].IsMember })
	// the row and Open in Home open the channel
	srClickAt(d, px+200, y0+40)
	b = v.chanButtons(gtx, e.h.Theme(), i, cw, v.chanState(v.chans[i]), "Joined")
	srClickAt(d, px+b.open.Min.X+5, y0+b.open.Min.Y+5)
	e.h.mu.Lock()
	navs := append([]string(nil), e.h.navs...)
	e.h.mu.Unlock()
	if len(navs) != 2 || navs[0] != "c/"+hit.ID || navs[1] != "c/"+hit.ID {
		t.Errorf("row and Open in Home navigated %q", navs)
	}
}

// #general: "Required" and its tooltip instead of the buttons.
func TestSearchRowsChannelRequired(t *testing.T) {
	e := srNewEnv(t, false)
	v, d := srView(t, e, "announcements", false)
	v.setTab(srTabChannels)
	d.until(t, "channels", func() bool { return !v.loading && len(v.chans) > 0 })
	s := v.chanState(v.chans[0])
	if s.inaction != "Required" || s.canLeave {
		t.Fatalf("#general: %+v", s)
	}
	gtx := srTestGtx()
	px := v.padX(gtx, d.size.X)
	cw := d.size.X - 2*px
	d.scan()
	y0, _ := v.srChanRowY(gtx, 0)
	srMoveTo(d, px+cw/2, y0+50)
	var x string
	d.until(t, "Required under the pointer", func() bool {
		x = srJoined(d.scan())
		return strings.Contains(x, "Required ")
	})
	if strings.Contains(x, "Open in Home") || !strings.Contains(x, "View channel") {
		t.Errorf("hover over #general: %q", x)
	}
}

// The People tab: a grid of profile cards; a click opens the profile.
func TestSearchRowsPeople(t *testing.T) {
	e := srNewEnv(t, false)
	v, d := srView(t, e, fakeslack.SearchPeopleWord, false)
	v.setTab(srTabPeople)
	d.until(t, "people", func() bool { return !v.loading && len(v.people) > 4 })
	x := srJoined(d.scan())
	for _, u := range v.people[:4] { // MemberProfileBaseEntity's name: the real name
		if !strings.Contains(x, pfRealName(u)) {
			t.Errorf("%q not drawn: %q", pfRealName(u), x)
		}
	}
	var opened string
	old := OpenFullProfile
	OpenFullProfile = func(id string) { opened = id }
	t.Cleanup(func() { OpenFullProfile = old })
	gtx := srTestGtx()
	px := v.padX(gtx, d.size.X)
	_, cell := srPeopleGrid(gtx, d.size.X-2*px)
	srClickAt(d, px+cell+dp(gtx, 20)+cell/2, v.listY+50) // the second card
	if opened != v.people[1].ID {
		t.Errorf("the second card opened %q, want %s", opened, v.people[1].ID)
	}
}

// OmniswitcherContent: Give feedback, or with a feedback opt-out "Results
// feel off? Learn more", which closes the search for the help article.
func TestSearchResultsFeelOff(t *testing.T) {
	for raw, want := range map[string]bool{"true": true, "false": false, `""`: false, `"0"`: true, "0": false, "1": true, "null": false, "{}": true} {
		if got := srTruthy(json.RawMessage(raw)); got != want {
			t.Errorf("srTruthy(%s) = %v", raw, got)
		}
	}
	e := srNewEnv(t, false)
	p := NewSearchPopover(e.h, "", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	x := srJoined(d.scan())
	if !strings.Contains(x, "Give feedback") || strings.Contains(x, "Results feel off?") {
		t.Fatalf("default: %q", x)
	}
	for _, k := range []string{"has_hipaa_compliance", "ml_opt_out", "search_feedback_opt_out"} {
		e.h.a.SetTeamPrefs(map[string]json.RawMessage{k: json.RawMessage("true")})
		x = srJoined(d.scan())
		if strings.Contains(x, "Give feedback") || !strings.Contains(x, "Results feel off? ") || !strings.Contains(x, "Learn more") {
			t.Errorf("%s: %q", k, x)
		}
	}
	var got string
	old := srOpenHelp
	srOpenHelp = func(_ Host, u string) { got = u }
	t.Cleanup(func() { srOpenHelp = old })
	p.learnMore.Click()
	d.frame()
	if got != srSearchHelpArticle || !p.Dismissed() {
		t.Errorf("Learn more opened %q, dismissed %v", got, p.Dismissed())
	}
}

// Light and dark PNGs of the rows, their hover states and the footer.
func TestSearchRowsShots(t *testing.T) {
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
		gtx := srTestGtx()

		vf, df := srView(t, e, fakeslack.SearchWord, dark)
		vf.setTab(srTabFiles)
		df.until(t, "files", func() bool { return !vf.loading && len(vf.files) > 0 })
		shot(df, "rows-files-"+mode)
		px := vf.padX(gtx, df.size.X)
		srMoveTo(df, px+300, vf.listY+30)
		shot(df, "rows-files-hover-"+mode)

		ch := e.fs.ChannelIDs()[1]
		word := strings.Split(e.h.a.ConvRef(ch).Name, "-")[0]
		vc, dc := srView(t, e, word, dark)
		vc.setTab(srTabChannels)
		dc.until(t, "channels", func() bool { return !vc.loading && len(vc.chans) > 0 })
		shot(dc, "rows-channels-"+mode)
		i := srChanAt(t, e, vc)
		y0, _ := vc.srChanRowY(gtx, i)
		srMoveTo(dc, px+400, y0+40)
		shot(dc, "rows-channels-hover-"+mode)
		cw := dc.size.X - 2*px
		b := vc.chanButtons(gtx, e.h.Theme(), i, cw, vc.chanState(vc.chans[i]), "Joined")
		srMoveTo(dc, px+b.join.Min.X+10, y0+b.join.Min.Y+10)
		shot(dc, "rows-channels-leave-"+mode)

		vg, dg := srView(t, e, "announcements", dark)
		vg.setTab(srTabChannels)
		dg.until(t, "#general", func() bool { return !vg.loading && len(vg.chans) > 0 })
		gy, _ := vg.srChanRowY(gtx, 0)
		srMoveTo(dg, px+300, gy+40)
		shot(dg, "rows-channels-required-"+mode)

		vp, dpp := srView(t, e, fakeslack.SearchPeopleWord, dark)
		vp.setTab(srTabPeople)
		dpp.until(t, "people", func() bool { return !vp.loading && len(vp.people) > 0 })
		shot(dpp, "rows-people-"+mode)
		srMoveTo(dpp, px+60, vp.listY+40)
		shot(dpp, "rows-people-hover-"+mode)

		e.h.a.SetTeamPrefs(map[string]json.RawMessage{"search_feedback_opt_out": json.RawMessage("true")})
		pop := NewSearchPopover(e.h, "", image.Rectangle{})
		dpo := &srDrv{size: image.Pt(1280, 800), w: pop, dark: dark}
		dpo.frame()
		shot(dpo, "rows-popover-feel-off-"+mode)
		e.h.a.SetExperimentGroup("a11y_underline_button_link", "on")
		e.h.a.S.Lock()
		e.h.a.S.Prefs.UnderlineLinks = true
		e.h.a.S.Unlock()
		shot(dpo, "rows-popover-feel-off-underline-"+mode)
	}
}
