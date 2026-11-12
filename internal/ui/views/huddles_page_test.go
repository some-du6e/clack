package views

// The Huddles page against Slack's code (Igs2 and its rows): synthetic
// fixtures on internal/fakeslack only, nothing starts or joins a huddle.
// HUDDLES_PAGE_SHOTS set (the huddles-audit-shots workflow) also saves
// light and dark screenshots of each state.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	hpAda      = "U0FAKE00001"
	hpGrace    = "U0FAKE00002"
	hpLinus    = "U0FAKE00003"
	hpMargaret = "U0FAKE00004"
	hpDesign   = "C0HUDDESIGN"
	hpLaunch   = "C0HUDLAUNCH"
	hpPlanning = "G0HUDPLANS1"
	hpDMAda    = "D0HUDDMADA1"
	hpGone     = "C0HUDGONE01" // a conversation conversations.info doesn't know
	hpThreadTS = "1700000000.000100"
	hpLiveRoom = "R0HUDLIVE01"
)

func hpAvatar(n int) string {
	return fmt.Sprintf("https://a.slack-edge.com/df10d/img/avatars/ava_%04d-72.png", n)
}

func hpUser(id, name, real string, ava int) slack.User {
	u := slack.User{ID: id, Name: name, RealName: real, Profile: slack.Profile{DisplayName: name, RealName: real}}
	a := hpAvatar(ava)
	u.Profile.Image24, u.Profile.Image32, u.Profile.Image48, u.Profile.Image72, u.Profile.Image192 = a, a, a, a, a
	return u
}

var hpUsers = []slack.User{
	hpUser(fakeslack.SelfID, "self", "Test Self", 1),
	hpUser(hpAda, "ada", "Ada Example", 5),
	hpUser(hpGrace, "grace", "Grace Example", 10),
	hpUser(hpLinus, "linus", "Linus Example", 15),
	hpUser(hpMargaret, "margaret", "Margaret Example", 20),
}

// hpHistory is the synthetic history: five ended huddles, newest first.
func hpHistory(now time.Time) []fakeslack.HuddleFixture {
	at := func(d time.Duration) int64 { return now.Add(-d).Unix() }
	return []fakeslack.HuddleFixture{
		{"id": "R0HUDHIST01", "date_start": at(2 * time.Hour), "date_end": at(2*time.Hour) + 25*60, "has_ended": true,
			"channels": []string{hpDesign}, "channel_type": "public", "thread_root_ts": hpThreadTS, "transcript_file_id": "F0HUDNOTES1",
			"participants": []string{}, "participant_history": []string{fakeslack.SelfID, hpAda, hpGrace}},
		{"id": "R0HUDHIST02", "date_start": at(26 * time.Hour), "date_end": at(26*time.Hour) + 65*60, "has_ended": true,
			"channels": []string{hpDMAda}, "channel_type": "im", "participants": []string{},
			"participant_history": []string{fakeslack.SelfID, hpAda}},
		{"id": "R0HUDHIST03", "name": "Launch checklist", "date_start": at(3 * 24 * time.Hour), "date_end": at(3*24*time.Hour) + 45,
			"has_ended": true, "channels": []string{hpLaunch}, "channel_type": "public", "participants": []string{},
			"participant_history": []string{fakeslack.SelfID, hpGrace, hpLinus, hpMargaret}},
		{"id": "R0HUDHIST04", "date_start": at(5 * 24 * time.Hour), "date_end": at(5*24*time.Hour) + 15*60, "has_ended": true,
			"channels": []string{hpPlanning}, "channel_type": "private", "participants": []string{},
			"participant_history": []string{hpGrace, hpLinus}, "missed_participants": map[string]any{fakeslack.SelfID: map[string]any{}}},
		{"id": "R0HUDHIST05", "date_start": at(10 * 24 * time.Hour), "date_end": at(10*24*time.Hour) + 10*60, "has_ended": true,
			"channels": []string{hpGone}, "channel_type": "private", "participants": []string{},
			"participant_history": []string{fakeslack.SelfID, hpMargaret}},
	}
}

// hpFake is a core.App on internal/fakeslack with the page's people and
// conversations in its store.
func hpFake(t *testing.T) (*core.App, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Synthetic Club"
	s.Unlock()
	s.PutUsers(hpUsers)
	for _, c := range []slack.Conversation{
		{ID: hpDesign, Name: "design-reviews", IsChannel: true, IsMember: true, NumMembers: 8},
		{ID: hpLaunch, Name: "launch", IsChannel: true, IsMember: true, NumMembers: 12},
		{ID: hpPlanning, Name: "planning", IsChannel: true, IsPrivate: true, IsMember: true, NumMembers: 4},
		{ID: hpDMAda, IsIM: true, User: hpAda},
	} {
		s.PutConv(c)
	}
	s.SetLatest(hpDesign, []slack.Message{{Type: "message", TS: hpThreadTS, User: hpAda, Text: "synthetic huddle message", ReplyCount: 3}}, false)
	app := core.New(slack.New(fs.Creds()), s)
	if u, ok := s.User(fakeslack.SelfID); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	return app, fs
}

// hpHost is the page's host: the views audit host plus the ui root's
// huddle hooks, recorded.
type hpHost struct {
	*vaHost
	imgs   *imgcache.Cache
	mu     sync.Mutex
	starts []string // OpenHuddleStartWith: entry|channel|member
	joins  []string // JoinHuddleFrom: channel|entry
	navs   []string
}

func (h *hpHost) Images() *imgcache.Cache { return h.imgs }
func (h *hpHost) Navigate(r string) {
	h.mu.Lock()
	h.navs = append(h.navs, r)
	h.mu.Unlock()
}
func (h *hpHost) OpenHuddleStartWith(entry, ch, member string) {
	h.mu.Lock()
	h.starts = append(h.starts, entry+"|"+ch+"|"+member)
	h.mu.Unlock()
}
func (h *hpHost) JoinHuddleFrom(ch, entry string) {
	h.mu.Lock()
	h.joins = append(h.joins, ch+"|"+entry)
	h.mu.Unlock()
}

type hpEnv struct {
	t   *testing.T
	fs  *fakeslack.Server
	app *core.App
	h   *hpHost
	v   *Huddles
	e   *vaEnv
}

// hpStart draws the page with the fixtures prep put in the fake.
func hpStart(t *testing.T, dark bool, size image.Point, prep func(app *core.App, fs *fakeslack.Server)) *hpEnv {
	t.Helper()
	app, fs := hpFake(t)
	if prep != nil {
		prep(app, fs)
	}
	h := &hpHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(dark)}}}
	if os.Getenv("HUDDLES_PAGE_SHOTS") != "" {
		h.imgs = hpShotCache()
	}
	v := NewHuddles(h)
	e := &vaEnv{size: size}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	env := &hpEnv{t: t, fs: fs, app: app, h: h, v: v, e: e}
	e.until(t, "the history", func() bool {
		st := v.state()
		v.mu.Lock()
		ok := v.interOK
		v.mu.Unlock()
		return !st.loading && ok
	})
	e.frame()
	return env
}

func hpKinds(items []hpItem) []hpKind {
	out := make([]hpKind, len(items))
	for i, it := range items {
		out[i] = it.kind
	}
	return out
}

func hpHas(items []hpItem, k hpKind) bool {
	for _, it := range items {
		if it.kind == k {
			return true
		}
	}
	return false
}

// The account's page (huddle_page_enhancements, hide_suggested_huddles,
// external_huddles unassigned): huddles.history {limit: 30} once on
// mount, users.interactions.list for the hero's two banners,
// huddles.listBackgrounds once; then renderHuddlePageRow's rows: the
// hero, the active section, "Recent huddles", the filters, the ended
// huddles (first/last), the footer; each row's title, time and length as
// Slack writes them.
func TestHuddlesPageHistoryLikeSlack(t *testing.T) {
	now := time.Now()
	env := hpStart(t, false, image.Pt(1000, 900), func(_ *core.App, fs *fakeslack.Server) { fs.SetHuddleHistory(hpHistory(now)) })
	v := env.v
	calls := env.fs.HuddleHistoryCalls()
	if len(calls) != 1 || calls[0].Get("limit") != "30" || calls[0].Has("cursor") || calls[0].Has("missed_huddles_only") {
		t.Fatalf("mount fetch %v, want one huddles.history {limit: 30}", calls)
	}
	var list url.Values
	for _, f := range env.fs.InteractionCalls() {
		if f.Get("method") == "users.interactions.list" {
			list = f
		}
	}
	if list.Get("components") != "HUDDLE_PAGE_BANNER,EXTERNAL_HUDDLES_AWARENESS_BANNER" || list.Get("types") != "DISMISS,DISMISS" {
		t.Errorf("users.interactions.list %v", list)
	}
	env.e.until(t, "huddles.listBackgrounds", func() bool { return env.fs.HuddleBackgroundCalls() == 1 })
	st := v.state()
	if len(st.ended) != 5 {
		t.Fatalf("%d ended huddles, want 5", len(st.ended))
	}
	items := v.rows(st, now)
	want := []hpKind{hpHero, hpActive, hpPastTitle, hpFilters, hpRow, hpRow, hpRow, hpRow, hpRow, hpFooter}
	if got := hpKinds(items); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	if !items[4].first || items[4].last || !items[8].last || items[8].first {
		t.Errorf("first/last: %+v … %+v", items[4], items[8])
	}
	// titles: the channel, the DM's person, the topic, the private
	// channel, a conversation you can't see (the people)
	env.e.until(t, "the unknown conversation resolved", func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.missing[hpGone]
	})
	for i, w := range []struct {
		title     string
		isChannel bool
	}{{"design-reviews", true}, {"ada", false}, {"Launch checklist", false}, {"planning", true}, {"margaret", false}} {
		title, _, isCh := v.historyTitle(&st.ended[i])
		if title != w.title || isCh != w.isChannel {
			t.Errorf("row %d title %q (channel %v), want %q (%v)", i, title, isCh, w.title, w.isChannel)
		}
	}
	// when (toTimeAgo) and how long
	if got := v.hdDateText(st.ended[0].Start, now); got != i18n.TimeAgo(i18n.EnUS, unixTime(st.ended[0].Start), now, false, true) || got != "2 hours ago" {
		t.Errorf("date %q, want 2 hours ago", got)
	}
	for i, w := range []string{"25 minutes", "1 hour 5 minutes", "Less than a minute"} {
		if got := hdDurationText(&st.ended[i]); got != w {
			t.Errorf("row %d length %q, want %q", i, got, w)
		}
	}
	// the thread (3 replies on the huddle message) and the notes
	if a := v.artifacts(&st.ended[0]); !a.thread || a.replies != 3 || !a.notes || a.messageTS != hpThreadTS {
		t.Errorf("artifacts %+v", a)
	}
	// the missed one
	if !contains(st.ended[3].Missed, fakeslack.SelfID) {
		t.Errorf("row 3 missed %v", st.ended[3].Missed)
	}
	hpShot(t, env, "huddles-page-history", false)
}

// huddle_page_enhancements on: no "Recent huddles", the day groups
// (Today, Yesterday, …) with their own first/last rows, and dates over a
// day old as {date_long}.
func TestHuddlesPageEnhancementsGroups(t *testing.T) {
	now := time.Now()
	for _, dark := range []bool{false, true} {
		env := hpStart(t, dark, image.Pt(1000, 900), func(app *core.App, fs *fakeslack.Server) {
			app.SetExperimentGroup("huddle_page_enhancements", "on")
			fs.SetHuddleHistory(hpHistory(now))
		})
		v := env.v
		items := v.rows(v.state(), now)
		if hpHas(items, hpPastTitle) {
			t.Error("Recent huddles with huddle_page_enhancements on")
		}
		var days []int
		for _, it := range items {
			if it.kind == hpDay {
				days = append(days, it.day)
			}
		}
		if len(days) < 2 || days[0] != dayBucket(now.Add(-2*time.Hour).Unix(), now) {
			t.Errorf("day groups %v", days)
		}
		old := now.Add(-3 * 24 * time.Hour)
		if got := v.hdDateText(old.Unix(), now); got != i18n.LongDate(i18n.EnUS, unixTime(old.Unix()), now) {
			t.Errorf("an old huddle's date %q", got)
		}
		hpShot(t, env, "huddles-page-enhancements", dark)
	}
}

// Each filter runs filterHuddleHistory: huddles.history with
// transformFiltersForApi's args (missed_huddles_only and users_only
// always, false included; users and channels comma-joined); a filter
// that leaves nothing shows NoResults; Reset clears them.
func TestHuddlesPageFiltersLikeSlack(t *testing.T) {
	now := time.Now()
	env := hpStart(t, false, image.Pt(1000, 900), func(_ *core.App, fs *fakeslack.Server) { fs.SetHuddleHistory(hpHistory(now)) })
	v := env.v
	// step runs one filter change and waits for its huddles.history
	step := func(n int, what string, do func()) url.Values {
		t.Helper()
		do()
		env.e.until(t, what, func() bool { return len(env.fs.HuddleHistoryCalls()) >= n && !v.state().filterBusy })
		return env.fs.HuddleHistoryCalls()[n-1]
	}
	if f := step(2, "missed", func() { v.setType("missed") }); f.Get("missed_huddles_only") != "true" || f.Get("users_only") != "false" || f.Has("users") {
		t.Errorf("missed: %v", f)
	}
	if st := v.state(); len(st.ended) != 1 || st.ended[0].ID != "R0HUDHIST04" {
		t.Errorf("missed rows %+v", st.ended)
	}
	hpShot(t, env, "huddles-page-missed", false)
	step(3, "all", func() { v.setType("all") })
	if f := step(4, "with", func() { v.toggleWith(hpGrace) }); f.Get("users") != hpGrace || f.Get("missed_huddles_only") != "false" {
		t.Errorf("with: %v", f)
	}
	if f := step(5, "include only", func() { v.filters.usersOnly = true; v.refetch() }); f.Get("users_only") != "true" {
		t.Errorf("include only: %v", f)
	}
	step(6, "reset", v.resetFilters)
	if f := step(7, "in", func() { v.toggleInChannel(hpLaunch) }); f.Get("channels") != hpLaunch || f.Has("users") || f.Get("users_only") != "false" {
		t.Errorf("in: %v", f)
	}
	if st := v.state(); len(st.ended) != 1 || st.ended[0].ID != "R0HUDHIST03" {
		t.Errorf("in #launch: %+v", st.ended)
	}
	// a conversation with no huddles: NoResults
	step(8, "no results", func() { v.filters.in = []string{"C0NOHUDDLES"}; v.refetch() })
	if items := v.rows(v.state(), now); !hpHas(items, hpNoResults) || hpHas(items, hpChannels) || hpHas(items, hpRow) {
		t.Errorf("rows with no results: %v", hpKinds(items))
	}
	hpShot(t, env, "huddles-page-no-results", false)
	step(9, "reset again", v.resetFilters)
	if n := len(v.state().ended); n != 5 {
		t.Errorf("%d rows after Reset filters, want 5", n)
	}
}

// The list's end asks for the next page: fetchAndUpsertHuddleHistory
// {cursor, ...the filters' args}, appended.
func TestHuddlesPagePaginates(t *testing.T) {
	now := time.Now()
	env := hpStart(t, false, image.Pt(1000, 900), func(_ *core.App, fs *fakeslack.Server) {
		var hs []fakeslack.HuddleFixture
		for i := 0; i < 35; i++ {
			start := now.Add(-time.Duration(i+1) * time.Hour).Unix()
			hs = append(hs, fakeslack.HuddleFixture{"id": fmt.Sprintf("R0HUDPAGE%02d", i), "date_start": start, "date_end": start + 600,
				"has_ended": true, "channels": []string{hpDesign}, "channel_type": "public", "participants": []string{},
				"participant_history": []string{fakeslack.SelfID, hpAda}})
		}
		fs.SetHuddleHistory(hs)
	})
	v := env.v
	if n := len(v.state().ended); n != 30 {
		t.Fatalf("first page %d huddles, want 30", n)
	}
	v.paginate()
	env.e.until(t, "the second page", func() bool { return len(v.state().ended) == 35 })
	cs := env.fs.HuddleHistoryCalls()
	f := cs[len(cs)-1]
	if f.Get("cursor") != "off:30" || f.Get("missed_huddles_only") != "false" || f.Get("users_only") != "false" {
		t.Errorf("page 2 %v", f)
	}
	v.mu.Lock()
	cur := v.hist.cursor
	v.mu.Unlock()
	if cur != "" {
		t.Errorf("cursor %q after the last page", cur)
	}
}

// The hero hides once HUDDLE_PAGE_BANNER (or the external one) has a
// Dismiss; its close button records one (users.interactions.set).
func TestHuddlesPageHeroDismiss(t *testing.T) {
	env := hpStart(t, false, image.Pt(1000, 900), nil)
	v := env.v
	if v.heroOff || v.dismissed(hdBannerPage) {
		t.Fatal("hero hidden before a dismissal")
	}
	hpShot(t, env, "huddles-page-empty", false)
	// the close button: 16px from the hero's top right, under the 49px header
	env.e.click(image.Pt(1000-16-18, 49+1+16+18))
	if !v.heroOff {
		t.Fatal("the close button didn't hide the hero")
	}
	env.e.until(t, "users.interactions.set", func() bool {
		for _, f := range env.fs.InteractionCalls() {
			if f.Get("method") == "users.interactions.set" && f.Get("component") == hdBannerPage && f.Get("type") == hdDismiss {
				return true
			}
		}
		return false
	})
	// dismissed elsewhere: no hero at all
	env2 := hpStart(t, false, image.Pt(1000, 900), func(_ *core.App, fs *fakeslack.Server) { fs.SetInteraction(hdBannerExternal, hdDismiss, 1) })
	if !env2.v.dismissed(hdBannerExternal) {
		t.Fatal("the external banner's Dismiss wasn't loaded")
	}
}

// With no history: the DM suggestions (the People browser's first page
// without you, three cards) and the channel suggestions; a card opens
// HuddleInviteStartModal with the person picked. hide_suggested_huddles
// on shows the (empty) active section instead.
func TestHuddlesPageSuggestionsLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		env := hpStart(t, dark, image.Pt(1100, 900), func(_ *core.App, fs *fakeslack.Server) { fs.SetPeopleBrowser(hpUsers) })
		v := env.v
		items := v.rows(v.state(), time.Now())
		if !hpHas(items, hpDMs) || !hpHas(items, hpChannels) || hpHas(items, hpActive) || hpHas(items, hpFilters) {
			t.Fatalf("rows %v", hpKinds(items))
		}
		env.e.until(t, "the people browser", func() bool { _, ok := v.peopleSuggestions(); return ok })
		us, _ := v.peopleSuggestions()
		if len(us) != len(hpUsers) {
			t.Fatalf("people %d", len(us))
		}
		if got := v.channelSuggestions(); len(got) != 3 {
			t.Errorf("channel suggestions %d, want 3", len(got))
		}
		hpShot(t, env, "huddles-page-suggestions", dark)
	}
	env := hpStart(t, false, image.Pt(1100, 900), func(app *core.App, fs *fakeslack.Server) {
		app.SetExperimentGroup("hide_suggested_huddles", "on")
	})
	if items := env.v.rows(env.v.state(), time.Now()); hpHas(items, hpDMs) || !hpHas(items, hpActive) {
		t.Errorf("hide_suggested_huddles on: %v", hpKinds(items))
	}
}

// A live huddle in a channel you're in: huddles.get once for its card,
// which shows with its background's preview; muted channels' huddles
// don't show. The card's timer is ZvaB's duration.
func TestHuddlesPageLiveCardsLikeSlack(t *testing.T) {
	now := time.Now()
	for _, dark := range []bool{false, true} {
		env := hpStart(t, dark, image.Pt(1000, 900), func(_ *core.App, fs *fakeslack.Server) {
			fs.SetHuddleHistory(hpHistory(now))
			fs.SetHuddleInfo(hpLiveRoom, fakeslack.HuddleFixture{"id": hpLiveRoom, "date_start": now.Add(-95 * time.Second).Unix(),
				"channels": []string{hpLaunch}, "participants": []string{hpGrace, hpLinus}, "background_id": "STAR_GAZING",
				"participant_history": []string{hpGrace, hpLinus}, "thread_root_ts": hpThreadTS})
		})
		v := env.v
		raw, _ := json.Marshal(map[string]any{"type": "sh_room_join", "user": hpGrace, "room": map[string]any{
			"id": hpLiveRoom, "date_start": now.Add(-95 * time.Second).Unix(), "participants": []string{hpGrace, hpLinus},
			"channels": []string{hpLaunch}}})
		env.app.Inject(slack.Event{Type: "sh_room_join", Channel: json.RawMessage(strconv.Quote(hpLaunch)), Raw: raw})
		env.e.until(t, "the live huddle's info", func() bool {
			v.mu.Lock()
			defer v.mu.Unlock()
			_, ok := v.info[hpLiveRoom]
			return ok
		})
		if got := env.fs.HuddleGetCalls(); len(got) != 1 || got[0] != hpLiveRoom {
			t.Errorf("huddles.get %v", got)
		}
		if live := v.pageLive(); len(live) != 1 {
			t.Fatalf("live huddles %d", len(live))
		}
		env.e.until(t, "the backgrounds", func() bool { return v.backgroundURL("STAR_GAZING", true) != "" })
		hpShot(t, env, "huddles-page-live", dark)
	}
	for secs, want := range map[int64]string{0: "0 sec", 95: "1 min 35 sec", 3600: "1 hour", 3725: "1 hour 2 min 5 sec", 90061: "1 day 1 hour 1 min 1 sec"} {
		if got := hdDuration(secs); got != want {
			t.Errorf("hdDuration(%d) = %q, want %q", secs, got, want)
		}
	}
}

// SuggestedHuddles: last week's huddles by conversation, most first; no
// group DMs, none with a live huddle, only ones you were in.
func TestSuggestedHuddlesLikeSlack(t *testing.T) {
	app, _ := hpFake(t)
	v := NewHuddles(&slickHost{app: app, th: theme.New(false)})
	v.frame()
	now := time.Now()
	day := func(d int) int64 { return now.Add(-time.Duration(d) * 24 * time.Hour).Unix() }
	me := fakeslack.SelfID
	hs := []core.Huddle{
		{ID: "a1", Start: day(1), Channels: []string{hpLaunch}, ChannelType: "public", History: []string{me, hpAda}},
		{ID: "b1", Start: day(1), Channels: []string{hpDesign}, ChannelType: "public", History: []string{me}},
		{ID: "a2", Start: day(2), Channels: []string{hpLaunch}, ChannelType: "public", History: []string{me}},
		{ID: "m1", Start: day(2), Channels: []string{"G0MPIM0001"}, ChannelType: "mpim", History: []string{me}},
		{ID: "x1", Start: day(2), Channels: []string{hpPlanning}, ChannelType: "private", History: []string{hpAda}},
		{ID: "o1", Start: day(30), Channels: []string{hpDMAda}, ChannelType: "im", History: []string{me}},
	}
	got := v.suggestedHuddles(hs, map[string]bool{}, now)
	var ids []string
	for _, s := range got {
		ids = append(ids, s.h.ID+":"+strconv.Itoa(s.n))
	}
	if strings.Join(ids, ",") != "a1:2,b1:1" {
		t.Errorf("suggestions %v, want a1:2,b1:1", ids)
	}
	if got := v.suggestedHuddles(hs, map[string]bool{hpLaunch: true}, now); len(got) != 1 || got[0].h.ID != "b1" {
		t.Errorf("with a live huddle in #launch: %+v", got)
	}
}

// ---- screenshots ----

// hpShotCache serves Slack's public CDN pictures (no cookie) and
// synthetic squares for anything else.
func hpShotCache() *imgcache.Cache {
	cl := &http.Client{Timeout: 8 * time.Second}
	return imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		if !strings.HasPrefix(u, "https://a.slack-edge.com/") {
			img := image.NewRGBA(image.Rect(0, 0, 64, 64))
			for i := range img.Pix {
				img.Pix[i] = 0xaa
			}
			var b bytes.Buffer
			_ = png.Encode(&b, img)
			return b.Bytes(), nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := cl.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	}, "", nil)
}

// hpShot saves the page (HUDDLES_PAGE_SHOTS), after letting its
// pictures load.
func hpShot(t *testing.T, env *hpEnv, name string, dark bool) {
	t.Helper()
	if os.Getenv("HUDDLES_PAGE_SHOTS") == "" {
		return
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		env.e.frame()
		if env.h.imgs == nil || env.h.imgs.Pending() == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	env.e.frame()
	vaShotTo(t, env.e, "HUDDLES_PAGE_SHOTS", name, dark)
}
