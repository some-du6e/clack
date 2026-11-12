package popups

import (
	"encoding/json"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The expected values below are Slack's own: its modules m5BY
// (splitToEmails, tokenizeFilter), sdt0 (defaultSplitQuery), hXK4, itIg
// and 1Glt (deriveTokenState) from build 4d0c78085232, run in node on these
// cases (creatorEmail me@club.example).

func TestInviteSplitLikeSlack(t *testing.T) {
	for q, want := range map[string][]string{
		"a@b.co":                           {"a@b.co"},
		"a@b.co, c@d.co":                   {"a@b.co", "c@d.co"},
		"a@b.co;c@d.co|e@f.co":             {"a@b.co", "c@d.co", "e@f.co"},
		"x@y.co z@w.co":                    {"x@y.co", "z@w.co"},
		"Ann Lee <ann@x.co>, Bo <bo@y.co>": {"ann@x.co", "bo@y.co"},
		"ann (ann@x.co)":                   {"ann@x.co"},
		"mailto:ann@x.co":                  {"ann@x.co"},
		" ,, ; ":                           nil,
		"John Smith":                       {"John Smith"},
		"a@b.co\tc@d.co\nе@f.co":           {"a@b.co", "c@d.co", "е@f.co"},
		"a@b.co，c@d.co、e@f.co":             {"a@b.co", "c@d.co", "e@f.co"},
		"foo bar@x.co":                     {"foo bar@x.co"},
		"name @x.co":                       {"name", "@x.co"},
	} {
		if got := inviteSplit(q); !reflect.DeepEqual(got, want) {
			t.Errorf("split %q = %q, want %q", q, got, want)
		}
	}
}

func TestInviteTokenizeLikeSlack(t *testing.T) {
	for _, c := range []struct {
		q      string
		commit bool
		add    []string
	}{
		{"a@b.co", false, nil},
		{"a@b.co,", true, []string{"a@b.co"}},
		{"a@b.co ", true, []string{"a@b.co"}},
		{"a@b.co, c", true, []string{"a@b.co", "c"}},
		{"John", false, nil},
		{"John ", true, []string{"John"}},
		{",", true, nil},
		{"a@b.co c@d.co", false, nil},
		{"Ann <ann@x.co>", false, nil},
		{"x;", true, []string{"x"}},
		// tokenizeFilter leaves a lone space, and EmailInput's
		// onFilterQueryChange then empties the field (no addresses in it)
		{" ", true, nil},
	} {
		add, commit := inviteTokenize(c.q)
		if commit != c.commit || !reflect.DeepEqual(add, c.add) {
			t.Errorf("tokenize %q = %v %q, want %v %q", c.q, commit, add, c.commit, c.add)
		}
	}
}

func TestInviteTokenErrorsLikeSlack(t *testing.T) {
	for _, c := range []struct {
		emails, members     []string
		errors              [][]string
		invitable, blocking int
	}{
		{[]string{"a@x.co", "A@x.co ", "bad", "me@club.example", "m@x.co"}, []string{"m@x.co"},
			[][]string{nil, {invErrDuplicate}, {invErrInvalid}, {invErrSelf}, {invErrAlready}}, 1, 4},
		{[]string{"one@x.co"}, nil, [][]string{nil}, 1, 0},
		{[]string{"nope", "me@club.example"}, nil, [][]string{{invErrInvalid}, {invErrSelf}}, 0, 2},
	} {
		mem := map[string]bool{}
		for _, e := range c.members {
			mem[e] = true
		}
		st := deriveInviteTokens(c.emails, "me@club.example", func(e string) bool { return mem[e] })
		for i, tk := range st.Tokens {
			if !reflect.DeepEqual(tk.Errors, c.errors[i]) {
				t.Errorf("%q errors %q, want %q", tk.Email, tk.Errors, c.errors[i])
			}
		}
		if st.Invitable != c.invitable || st.Blocking != c.blocking || st.blocked() != (c.invitable == 0 || c.blocking > 0) {
			t.Errorf("%q: invitable %d blocking %d", c.emails, st.Invitable, st.Blocking)
		}
	}
	// 200 at a time
	var many []string
	for i := 0; i < 203; i++ {
		many = append(many, "p"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+"@x.co")
	}
	st := deriveInviteTokens(many, "", nil)
	if st.OverLimit != 3 || !st.blocked() || len(st.withinLimit()) != 200 {
		t.Errorf("over limit %d, within %d", st.OverLimit, len(st.withinLimit()))
	}
}

// invEnv is a booted app against fakeslack (Grid, a plain member) with the
// viewer's email set.
func invEnv(t *testing.T) (*tzHost, *fakeslack.Server) {
	t.Helper()
	h, fs := ccEnv(t)
	s := h.a.S
	s.Lock()
	if u := s.Users[s.Self.ID]; u != nil {
		u.Profile.Email = "me@fakeclub.example"
	}
	s.Self.Profile.Email = "me@fakeclub.example"
	s.Unlock()
	return h, fs
}

func invPrefs(t *testing.T, a *core.App, js string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	a.SetTeamPrefs(m)
}

func invLastSummary(h *tzHost) *InviteSummary {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.shown) - 1; i >= 0; i-- {
		if s, ok := h.shown[i].(*InviteSummary); ok {
			return s
		}
	}
	return nil
}

func TestInviteModalSend(t *testing.T) {
	h, fs := invEnv(t)
	if !CanInvite(h) {
		t.Fatal("a member of an open team can't invite")
	}
	m := OpenInviteModal(h, InviteFromTeamMenu)
	ccFrame(m)
	if got := m.Title(); got != "Invite people to Fake Club" {
		t.Errorf("title %q", got)
	}
	if m.send.Label != "Send" || m.CanSend() {
		t.Errorf("send %q enabled %v with no addresses", m.send.Label, m.CanSend())
	}
	if got := m.placeholder(); got != "name@fakeclub.example" {
		t.Errorf("placeholder %q", got)
	}
	m.SetText("new@x.example, bad, me@fakeclub.example,")
	if got := m.Emails(); !reflect.DeepEqual(got, []string{"new@x.example", "bad", "me@fakeclub.example"}) || m.Pending() != "" {
		t.Fatalf("tokens %q, pending %q", got, m.Pending())
	}
	st := m.Tokens()
	if st.InvalidN != 1 || st.SelfN != 1 || m.CanSend() {
		t.Errorf("errors %+v, can send %v", st.Counts, m.CanSend())
	}
	ccFrame(m)
	m.RemoveAll(invErrInvalid)
	m.RemoveAll(invErrSelf)
	m.TypeText("two@x.example")
	if m.Pending() != "two@x.example" || len(m.Emails()) != 1 {
		t.Errorf("typing without a separator: pending %q, tokens %q", m.Pending(), m.Emails())
	}
	m.Commit() // Enter
	if !reflect.DeepEqual(m.Emails(), []string{"new@x.example", "two@x.example"}) || !m.CanSend() {
		t.Fatalf("tokens %q, can send %v", m.Emails(), m.CanSend())
	}
	ccFrame(m)
	m.Send()
	ccUntil(t, m, "summary", func() bool { return invLastSummary(h) != nil })
	calls := fs.InviteCalls()
	if len(calls) != 1 || calls[0].Method != "users.admin.inviteBulk" {
		t.Fatalf("calls %+v", calls)
	}
	f := calls[0].Form
	if f.Get("campaign") != InviteFromTeamMenu || f.Get("team_id") != fakeslack.TeamID ||
		f.Get("invites") != `[{"email":"new@x.example","mode":"manual","type":"regular"},{"email":"two@x.example","mode":"manual","type":"regular"}]` {
		t.Errorf("form %v", f)
	}
	sum := invLastSummary(h)
	ccFrame(sum)
	if sum.Title() != "Sent" || !m.Dismissed() {
		t.Errorf("summary %q, form dismissed %v", sum.Title(), m.Dismissed())
	}
	if st, tip := sum.Status(0); st != "Invited as a coworker" || !strings.HasPrefix(tip, "They’re not on Slack yet") {
		t.Errorf("row status %q tip %q", st, tip)
	}
}

func TestInviteModalPartialAndFailure(t *testing.T) {
	h, fs := invEnv(t)
	m := OpenInviteModal(h, InviteFromCreateMenu)
	m.SetText("ok@x.example, already@x.example ")
	m.Send()
	ccUntil(t, m, "summary", func() bool { return invLastSummary(h) != nil })
	sum := invLastSummary(h)
	if sum.Title() != "Unable to send all" {
		t.Errorf("title %q", sum.Title())
	}
	order := sum.ordered()
	if r := sum.res[order[0]]; r.OK || r.Email != "already@x.example" {
		t.Errorf("failures first: %+v", sum.res)
	}
	if st, tip := sum.Status(order[0]); st != "Couldn’t invite" || tip != "This person is already in your workspace." {
		t.Errorf("failure %q %q", st, tip)
	}
	// a whole-call failure stays in the form with Slack's text
	fs.SetInviteError("invite_limit_reached")
	m2 := OpenInviteModal(h, InviteFromCreateMenu)
	m2.SetText("x@x.example ")
	m2.Send()
	ccUntil(t, m2, "error", func() bool { return m2.FormError() != "" })
	if got := m2.formErrorText(); !strings.HasPrefix(got, "Your workspace has reached its invitation limit") || m2.Dismissed() {
		t.Errorf("banner %q, dismissed %v", got, m2.Dismissed())
	}
	ccFrame(m2)
}

// invites_only_admins: the member's send asks an admin, with the reason
// and the note.
func TestInviteModalRequest(t *testing.T) {
	h, fs := invEnv(t)
	invPrefs(t, h.a, `{"invites_only_admins":true,"invite_requests_enabled":true}`)
	m := OpenInviteModal(h, InviteFromHuddles)
	ccFrame(m)
	if got := m.Title(); got != "Request invitations to Fake Club" || m.send.Label != "Send Request" {
		t.Errorf("title %q, button %q", got, m.send.Label)
	}
	if m.customShown() || m.customizeShown() || m.linkShown() {
		t.Error("a request has no custom message or invite link")
	}
	m.SetText("pal@x.example ")
	m.SetMessage("runs the robotics club")
	m.Send()
	ccUntil(t, m, "summary", func() bool { return invLastSummary(h) != nil })
	calls := fs.InviteCalls()
	if len(calls) != 1 || calls[0].Method != "users.inviteRequests.create" {
		t.Fatalf("calls %+v", calls)
	}
	if f := calls[0].Form; f.Get("requester_message") != "runs the robotics club" || f.Get("campaign") != InviteFromHuddles {
		t.Errorf("form %v", f)
	}
	if st, tip := invLastSummary(h).Status(0); st != "Invited as a coworker" || tip != "Your admin has to review this first." {
		t.Errorf("row %q %q", st, tip)
	}
	invPrefs(t, h.a, `{"invites_only_admins":true}`)
	if CanInvite(h) {
		t.Error("no invites and no requests: the entry points hide")
	}
}

// Guests: the channels decide single or multi channel; Send needs one.
func TestInviteModalGuest(t *testing.T) {
	h, fs := invEnv(t)
	invPrefs(t, h.a, `{"invite_requests_enabled":true}`)
	m := OpenInviteModal(h, InviteFromTeamMenu)
	if !m.guestOption() {
		t.Fatal("Guest isn't offered to a member who may request")
	}
	m.SetType(core.InviteMultiChannelGuest)
	m.SetText("guest@x.example ")
	ccFrame(m)
	if m.CanSend() {
		t.Error("a guest invite with no channel can be sent")
	}
	opts := m.inviteChannelOptions()
	if len(opts) < 2 {
		t.Fatalf("channel options %d", len(opts))
	}
	m.AddChannel(opts[0])
	if m.Type() != core.InviteSingleChannelGuest || !m.CanSend() {
		t.Errorf("one channel: %s, can send %v", m.Type(), m.CanSend())
	}
	m.ToggleMoreChannels()
	if m.Type() != core.InviteMultiChannelGuest {
		t.Errorf("Allow more channels: %s", m.Type())
	}
	m.ToggleMoreChannels()
	m.AddChannel(opts[1])
	if m.Type() != core.InviteMultiChannelGuest {
		t.Errorf("two channels: %s", m.Type())
	}
	m.SetExpiration(7)
	ccFrame(m)
	m.Send()
	ccUntil(t, m, "summary", func() bool { return invLastSummary(h) != nil })
	calls := fs.InviteCalls()
	f := calls[len(calls)-1].Form
	if calls[len(calls)-1].Method != "users.inviteRequests.create" || f.Get("restricted") != "true" ||
		f.Get("channels") != opts[0].ID+","+opts[1].ID || f.Get("expiration_ts") == "" {
		t.Errorf("guest request %s %v", calls[len(calls)-1].Method, f)
	}
}

// ChannelSelect's "Suggested:" row (GXKR): conversations.recommend on
// open, three shimmer tags until it answers, then the first three
// recommended channels not picked yet; a click adds one; none answered,
// no row.
func TestInviteModalSuggested(t *testing.T) {
	h, fs := invEnv(t)
	m := OpenInviteModal(h, InviteFromPeopleBrowser)
	if !m.RecommendedLoading() {
		t.Error("no shimmer while conversations.recommend is out")
	}
	ccUntil(t, m, "the recommended channels", func() bool { return !m.RecommendedLoading() })
	var call *fakeslack.InviteCall
	for _, c := range fs.InviteCalls() {
		if c.Method == "conversations.recommend" {
			call = &c
		}
	}
	if call == nil || call.Form.Get("query") != `[""]` || call.Form.Get("source") != "joiner-preadd-channels" ||
		call.Form.Get("channels_info") != "true" || call.Form.Get("limit") != "10" {
		t.Fatalf("conversations.recommend %+v", call)
	}
	sugg := m.Suggested()
	if len(sugg) != 3 {
		t.Fatalf("%d suggested", len(sugg))
	}
	m.AddSuggested(sugg[1])
	if ch := m.Channels(); len(ch) != 1 || ch[0].ID != sugg[1].ID {
		t.Fatalf("picked %+v", ch)
	}
	after := m.Suggested()
	if len(after) != 3 || after[0].ID != sugg[0].ID || after[1].ID != sugg[2].ID {
		t.Errorf("after the pick: %+v", after)
	}
	ccFrame(m)
	// a guest: a second channel from the row makes a multi-channel guest,
	// as a search pick does
	m.SetType(core.InviteMultiChannelGuest)
	m.AddSuggested(after[0])
	if m.Type() != core.InviteMultiChannelGuest || len(m.Channels()) != 2 {
		t.Errorf("guest with two channels: %s %d", m.Type(), len(m.Channels()))
	}
	m.Dismiss()

	fs.SetRecommendedChannels([]string{})
	e := OpenInviteModal(h, InviteFromTeamMenu)
	ccUntil(t, e, "the empty answer", func() bool { return !e.RecommendedLoading() })
	if len(e.Suggested()) != 0 {
		t.Errorf("suggested %+v", e.Suggested())
	}
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(600, 400))}
	if d := e.recommendedRow(gtx, h.th, colorsOf(h.th), 600); d.Size.Y != 0 {
		t.Errorf("an empty answer draws a %dpx row", d.Size.Y)
	}
}

// GuestExpirationSelect's Custom (value -1): the calendar button,
// "Deactivate on {date}" once a day is picked, the picker from tomorrow to
// ten years out, and getExpirationTs: the day's 11:59:59 PM in the
// viewer's zone, or today's with no day picked.
func TestInviteModalCustomExpiry(t *testing.T) {
	h, fs := invEnv(t)
	invPrefs(t, h.a, `{"invite_requests_enabled":true}`)
	m := OpenInviteModal(h, InviteFromTeamMenu)
	m.SetType(core.InviteMultiChannelGuest)
	m.SetText("guest@x.example ")
	opts := m.inviteChannelOptions()
	m.AddChannel(opts[0])
	if got := invExpLabel(InviteExpiryCustom); got != "Custom" || invExpOptions[len(invExpOptions)-1] != InviteExpiryCustom {
		t.Errorf("options %v, Custom %q", invExpOptions, got)
	}
	loc := m.viewerLoc()
	now := time.Date(2026, 11, 12, 15, 4, 0, 0, loc)
	m.SetExpiration(InviteExpiryCustom)
	if got, want := m.ExpiresTs(now), time.Date(2026, 11, 12, 23, 59, 59, 0, loc).Unix(); got != want {
		t.Errorf("Custom with no day: %d, want today's end %d", got, want)
	}
	if m.CustomExpirationText() != "" {
		t.Errorf("text before a pick %q", m.CustomExpirationText())
	}
	ccFrame(m)
	m.openCalendar(image.Rect(0, 500, 600, 536), 315)
	cal, ok := m.calOv.(*dtCalendar)
	if !ok {
		t.Fatalf("calendar %T", m.calOv)
	}
	tl := time.Now().In(loc)
	if want := time.Date(tl.Year(), tl.Month(), tl.Day()+1, 0, 0, 0, 0, time.Local); !cal.min.Equal(want) || cal.max.Year() != time.Now().Year()+10 {
		t.Errorf("picker range %v to %v", cal.min, cal.max)
	}
	if cal.anchor.Min.Y != 500-315 {
		t.Errorf("picker anchor %v (offsetY -315)", cal.anchor)
	}
	day := cal.min.AddDate(0, 0, 7) // a pickable day a week after tomorrow
	end := time.Date(day.Year(), day.Month(), day.Day(), 23, 59, 59, 0, loc).Unix()
	cal.pick(day)
	if got, want := m.CustomExpirationText(), "Deactivate on "+day.Format("January 2, 2006"); got != want {
		t.Errorf("button text %q, want %q (moment LL)", got, want)
	}
	if got := m.ExpiresTs(now); got != end {
		t.Errorf("Custom on %v: %d, want %d", day, got, end)
	}
	ccFrame(m)
	m.Send()
	ccUntil(t, m, "summary", func() bool { return invLastSummary(h) != nil })
	calls := fs.InviteCalls()
	if f := calls[len(calls)-1].Form; f.Get("expiration_ts") != strconv.FormatInt(end, 10) {
		t.Errorf("expiration_ts %q", f.Get("expiration_ts"))
	}
	// a type change resets the expiration
	m2 := OpenInviteModal(h, InviteFromTeamMenu)
	m2.SetType(core.InviteMultiChannelGuest)
	m2.SetCustomExpiration(day)
	m2.SetType(core.InviteMember)
	m2.SetType(core.InviteMultiChannelGuest)
	if m2.ExpiresTs(now) != 0 || m2.CustomExpirationText() != "" {
		t.Errorf("after a type change: %d %q", m2.ExpiresTs(now), m2.CustomExpirationText())
	}
}

// Off Grid the footer has "Copy invite link": the viewer's link, minted on
// the first copy.
func TestInviteModalLink(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	cr := fs.Creds()
	cr.EnterpriseID, cr.EnterpriseToken = "", ""
	st := store.New(t.TempDir())
	st.Self = slack.User{ID: fakeslack.SelfID}
	st.PutUsers([]slack.User{st.Self})
	a := core.New(slack.New(cr), st)
	t.Cleanup(a.Shutdown)
	h := &tzHost{a: a, th: theme.New(false), trees: map[uint64][]byte{}}
	m := OpenInviteModal(h, InviteFromTeamMenu)
	if !m.linkShown() {
		t.Fatal("no invite link off Grid")
	}
	ccUntil(t, m, "link listed", func() bool { return !m.linkBusy })
	if m.haveLink || m.linkTipText() != "Invite your coworkers with this link" {
		t.Errorf("before a copy: %v %q", m.haveLink, m.linkTipText())
	}
	m.CopyLink()
	ccUntil(t, m, "link minted", func() bool { return !m.linkBusy })
	if !m.LinkCopied() || !strings.HasPrefix(m.linkTipText(), "Link copied - expires in 30 days") || m.copyNext != "" {
		t.Errorf("after the copy: %v %q (pending copy %q)", m.LinkCopied(), m.linkTipText(), m.copyNext)
	}
	calls := fs.InviteCalls()
	if len(calls) != 2 || calls[0].Method != "users.sharedInvites.list" || calls[1].Method != "users.admin.createSharedInvite" {
		t.Errorf("calls %+v", calls)
	}
	m.SetType(core.InviteMultiChannelGuest)
	if m.linkShown() {
		t.Error("the link shows for a guest invite")
	}
}

// TestInviteShots draws the modal light and dark for the create-channel
// workflow (INVITE_SHOTS_DIR, artifact create-channel-shots): the member
// form with error tokens, a request with the admin-approval note, a guest
// with channels, the form off Grid with "Copy invite link", and the
// summary. Synthetic content only.
func TestInviteShots(t *testing.T) {
	dir := os.Getenv("INVITE_SHOTS_DIR")
	if dir == "" {
		t.Skip("INVITE_SHOTS_DIR not set")
	}
	h, fsInv := invEnv(t)
	sh := theme.NewShaper()
	const w, hgt = 900, 960
	save := func(name string, dark bool, o Overlay) {
		t.Helper()
		th := theme.New(dark)
		th.SetShapers(sh, sh)
		h.th = th
		var ops op.Ops
		for i := 0; i < 3; i++ { // settle: focus, posted results
			ops.Reset()
			Pump()
			paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(w, hgt)}.Op())
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Now(), Constraints: layout.Exact(image.Pt(w, hgt))}
			o.Layout(gtx)
		}
		img := pixeltest.Render(t, w, hgt, &ops)
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []struct {
		name string
		dark bool
	}{{"light", false}, {"dark", true}} {
		invPrefs(t, h.a, `{}`)
		m := OpenInviteModal(h, InviteFromTeamMenu)
		m.SetText("ada@example.com, grace@example.com, not-an-email, ada@example.com, ")
		m.TypeText("linus@exam")
		ccUntil(t, m, "the suggestions", func() bool { return !m.RecommendedLoading() })
		save("invite-member-"+mode.name+".png", mode.dark, m)
		m.Dismiss()

		// the Suggested: row while it loads (the call held back) and with
		// one suggestion picked
		hold := make(chan struct{})
		fsInv.SetHook(func(method string, _ url.Values) (any, bool) {
			if method == "conversations.recommend" {
				<-hold
			}
			return nil, false
		})
		l := OpenInviteModal(h, InviteFromPeopleBrowser)
		l.SetText("ada@example.com ")
		save("invite-suggested-loading-"+mode.name+".png", mode.dark, l)
		close(hold)
		fsInv.SetHook(nil)
		ccUntil(t, l, "the suggestions", func() bool { return !l.RecommendedLoading() })
		if sg := l.Suggested(); len(sg) > 0 {
			l.AddSuggested(sg[0])
		}
		save("invite-suggested-"+mode.name+".png", mode.dark, l)
		l.Dismiss()

		invPrefs(t, h.a, `{"invites_only_admins":true,"invite_requests_enabled":true}`)
		r := OpenInviteModal(h, InviteFromHuddles)
		r.SetText("pal@example.com ")
		r.SetMessage("Runs the robotics club")
		save("invite-request-"+mode.name+".png", mode.dark, r)

		g := OpenInviteModal(h, InviteFromTeamMenu)
		g.SetType(core.InviteMultiChannelGuest)
		g.SetText("guest@partner.example ")
		if opts := g.inviteChannelOptions(); len(opts) > 0 {
			g.AddChannel(opts[0])
		}
		ccUntil(t, g, "the suggestions", func() bool { return !g.RecommendedLoading() })
		save("invite-guest-"+mode.name+".png", mode.dark, g)
		// Custom: the calendar button with its date
		g.SetCustomExpiration(time.Date(2026, 11, 20, 0, 0, 0, 0, time.Local))
		save("invite-guest-custom-"+mode.name+".png", mode.dark, g)
		g.Dismiss()
		r.Dismiss()

		invPrefs(t, h.a, `{}`)
		s := openInviteSummary(h, InviteFromTeamMenu, "Fake Club", []core.InviteResult{
			{Email: "already@example.com", Error: "already_in_team", Type: core.InviteMember},
			{Email: "ada@example.com", OK: true, Type: core.InviteMember},
			{Email: "grace@example.com", OK: true, Type: core.InviteMember},
		}, false, nil)
		save("invite-summary-"+mode.name+".png", mode.dark, s)
		s.Dismiss()
	}
	// off Grid: the footer's Copy invite link (a fakeslack without the org)
	fs2, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs2.Close() })
	cr := fs2.Creds()
	cr.EnterpriseID, cr.EnterpriseToken = "", ""
	st := store.New(t.TempDir())
	st.Self = slack.User{ID: fakeslack.SelfID, Profile: slack.Profile{Email: "me@fakeclub.example"}}
	st.PutUsers([]slack.User{st.Self})
	st.Lock()
	st.TeamName = "Fake Club"
	st.Unlock()
	off := &tzHost{a: core.New(slack.New(cr), st), th: theme.New(false), trees: map[uint64][]byte{}}
	t.Cleanup(off.a.Shutdown)
	h = off
	for _, mode := range []struct {
		name string
		dark bool
	}{{"light", false}, {"dark", true}} {
		m := OpenInviteModal(off, InviteFromTeamMenu)
		m.SetText("ada@example.com ")
		ccUntil(t, m, "link listed", func() bool { return !m.linkBusy })
		save("invite-link-"+mode.name+".png", mode.dark, m)
		m.Dismiss()
	}
}
