package views

// gap-w15 (docs/GAPS-2026-11-11.md W15, docs/ACTIVITY.md "Leftovers"):
// the typing dots in the status column, the reactions under a reaction
// row, the inline subtext, the VIP sort's sticky section headers and
// Accept on a Slack Connect channel invite, against internal/fakeslack
// (Size.Views). Synthetic content only; every write is fakeslack's
// recorded one.

import (
	"context"
	"encoding/json"
	"image"
	"math"
	"net/url"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

func w15Gtx(now time.Time) layout.Context {
	return layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: now,
		Constraints: layout.Exact(image.Pt(600, 400))}
}

// w15Modes runs f in light and dark (vaFakeActivity's theme), as the
// shots need both.
func w15Modes(t *testing.T, f func(t *testing.T, dark bool)) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) { f(t, dark) })
	}
}

// w15ScrollTo puts key's row at the list's top.
func w15ScrollTo(t *testing.T, v *Activity, e *vaEnv, key string) {
	t.Helper()
	page := actPage(v)
	for i, x := range v.vis {
		if x >= 0 && x < len(page.Items) && actKey(&page.Items[x]) == key {
			v.list.Position.First, v.list.Position.Offset = i, 0
			e.frame()
			e.frame()
			return
		}
	}
	t.Fatalf("%s isn't listed", key)
}

func w15Typing(app *core.App, ch, user, thread string) {
	raw, _ := json.Marshal(map[string]any{"type": "user_typing", "channel": ch, "user": user, "thread_ts": thread})
	cj, _ := json.Marshal(ch)
	app.Inject(slack.Event{Type: "user_typing", Channel: cj, Raw: raw})
}

// rZx3 z8 and the status slot's order: someone typing in a DM or a
// channel (not in a thread) shows the dots on that conversation's DM and
// channel-post rows, a thread's typing on its thread_v2 row, ahead of the
// unread badge; your own typing and a group DM never; a message from the
// typist or 6 s end it. The dots are +SIc's opacity ramps.
func TestActivityTypingStatusLikeSlack(t *testing.T) { w15Modes(t, w15TypingStatus) }

func w15TypingStatus(t *testing.T, dark bool) {
	v, _, e, _ := vaFakeActivity(t, dark)
	app := v.h.Core()
	now := time.Now()
	dm, ch, th := actItem(t, v, "fa-dm"), actItem(t, v, "fa-channel"), actItem(t, v, "fa-thread")
	if v.typingOn(w15Gtx(now), dm) || v.typingOn(w15Gtx(now), ch) || v.typingOn(w15Gtx(now), th) {
		t.Fatal("typing before anyone types")
	}
	w15Typing(app, "D0FAKE00000", "U0FAKE00001", "")
	e.until(t, "the DM's typing", func() bool { return v.typingOn(w15Gtx(time.Now()), dm) })
	if v.typingOn(w15Gtx(time.Now()), ch) || v.typingOn(w15Gtx(time.Now()), th) {
		t.Error("the DM's typing shows on other rows")
	}
	w15ScrollTo(t, v, e, "fa-dm")
	vaShot(t, e, "activity-typing", dark)
	v.dense, v.rows, v.dirty = true, map[string]*actRow{}, true
	e.frame()
	vaShot(t, e, "activity-typing-dense", dark)
	v.dense, v.rows, v.dirty = false, map[string]*actRow{}, true
	e.frame()
	// your own typing never shows (memberIsSelf)
	w15Typing(app, fakeslack.ViewsChannel, fakeslack.SelfID, "")
	v.typing.apply(core.Event{Type: "typing", Channel: fakeslack.ViewsChannel, UserID: fakeslack.SelfID}, fakeslack.SelfID)
	for range 5 {
		e.frame()
	}
	if v.typingOn(w15Gtx(time.Now()), ch) {
		t.Error("your own typing shows")
	}
	// a thread's typing is the thread row's, not the channel's
	w15Typing(app, fakeslack.ViewsChannel, "U0FAKE00002", th.ThreadTS)
	e.until(t, "the thread's typing", func() bool { return v.typingOn(w15Gtx(time.Now()), th) })
	if v.typingOn(w15Gtx(time.Now()), ch) {
		t.Error("a thread's typing shows on the channel row")
	}
	// the record lapses after 6 s
	if v.typingOn(w15Gtx(time.Now().Add(actTypingTTL+time.Second)), dm) {
		t.Error("typing still on after 6 s")
	}
	// a message from the typist ends it
	v.typing.apply(core.Event{Type: "message", Channel: "D0FAKE00000", UserID: "U0FAKE00001"}, fakeslack.SelfID)
	if v.typingOn(w15Gtx(time.Now()), dm) {
		t.Error("typing still on after the typist's message")
	}
	// group DMs aren't in z8
	v.typing.apply(core.Event{Type: "typing", Channel: "G0FAKE", UserID: "U0FAKE00001"}, fakeslack.SelfID)
	if v.typingOn(w15Gtx(time.Now()), &core.ActivityItem{Type: "mpdm", Channel: "G0FAKE"}) {
		t.Error("typing on a group DM row")
	}
	// the dots' opacity at the start of a cycle: the first starts its
	// ramp, the second and third are .65 s and .5 s into theirs
	for i, want := range []float64{1, 1 - 0.8*0.65/0.75, 1 - 0.8*0.5/0.75} {
		if got := actTypingOpacity(0, i); math.Abs(got-want) > 1e-9 {
			t.Errorf("dot %d at 0 s: opacity %.4f, want %.4f", i, got, want)
		}
	}
	if got := actTypingOpacity(0.77, 0); got != 1 {
		t.Errorf("dot 0 between ramps: %.3f, want its resting 1", got)
	}
}

// rZx3 GY / SKIQ: a reaction row whose message has reactions shows them
// under the footer, without the add button; other rows don't.
func TestActivityReactionRowReactionsLikeSlack(t *testing.T) { w15Modes(t, w15ReactionRow) }

func w15ReactionRow(t *testing.T, dark bool) {
	v, _, e, _ := vaFakeActivity(t, dark)
	r := v.rowFor("fa-reaction")
	r.v, r.it = v, actItem(t, v, "fa-reaction")
	if !v.showsReactions(r) {
		t.Fatalf("the reaction row shows no reactions (message %+v)", r.msg())
	}
	mention := v.rowFor("fa-at-user")
	mention.v, mention.it = v, actItem(t, v, "fa-at-user")
	if v.showsReactions(mention) {
		t.Error("a mention row shows its message's reactions")
	}
	// the row is taller by the bar: 8px + the 24px pills - 4px
	g := w15Gtx(time.Now())
	g.Constraints = layout.Constraints{Max: image.Pt(600, 400)}
	d := v.entryExtras(g, r)
	if d.Size.Y != 28 {
		t.Errorf("entry_extras %dpx tall, want 28", d.Size.Y)
	}
	w15ScrollTo(t, v, e, "fa-reaction")
	vaShot(t, e, "activity-reaction-row", dark)
}

// /ad9 ActivityMessage: the subtext is a span inside the message, so it
// runs inline at the start of a first paragraph (the second line starts
// under it) and takes its own line before any other first block.
func TestActivitySubtextInlineLikeSlack(t *testing.T) {
	para := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: "a synthetic message"}}}}}
	got := actSubtextDoc(para, "You: ")
	if len(got.Blocks) != 1 || len(got.Blocks[0].Spans) != 2 || got.Blocks[0].Spans[0].Text != "You: " || got.Blocks[0].Spans[1].Text != "a synthetic message" {
		t.Errorf("paragraph: %+v", got.Blocks)
	}
	if len(para.Blocks[0].Spans) != 1 {
		t.Error("actSubtextDoc changed the message's own document")
	}
	list := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.List, Items: [][]richtext.Span{{{Kind: richtext.Text, Text: "one"}}}}}}
	got = actSubtextDoc(list, "Ada Example: ")
	if len(got.Blocks) != 2 || got.Blocks[0].Kind != richtext.Paragraph || got.Blocks[0].Spans[0].Text != "Ada Example: " || got.Blocks[1].Kind != richtext.List {
		t.Errorf("list: %+v", got.Blocks)
	}
	if got := actSubtextDoc(para, ""); len(got.Blocks[0].Spans) != 1 {
		t.Error("an empty subtext added a span")
	}
	w15Modes(t, w15SubtextRow)
}

// a two-line message of your own in a channel post: one rich text view
// with the subtext inside, two lines tall
func w15SubtextRow(t *testing.T, dark bool) {
	v, _, e, _ := vaFakeActivity(t, dark)
	long := strings.Repeat("a synthetic message that wraps onto a second line ", 3)
	v.data.mutate(func(p *core.ActivityPage) {
		for i := range p.Items {
			if p.Items[i].Key == "fa-channel" && p.Items[i].Msg != nil {
				m := *p.Items[i].Msg
				m.User, m.Text = fakeslack.SelfID, long
				m.Doc = richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: long}}}}}
				p.Items[i].Msg = &m
			}
		}
	})
	v.rows, v.dirty = map[string]*actRow{}, true
	e.frame()
	e.frame()
	r := v.rowFor("fa-channel")
	if r.d.subtext != "You: " {
		t.Fatalf("subtext %q", r.d.subtext)
	}
	g := w15Gtx(time.Now())
	g.Constraints = layout.Constraints{Max: image.Pt(420, 400)}
	r.v, r.cur, r.it = v, &r.d, actItem(t, v, "fa-channel")
	if d := v.actBody(g, r, 2, v.p.Sec); d.Size.Y != 44 || d.Size.X > 420 {
		t.Errorf("the body with its subtext is %v, want two 22px lines", d.Size)
	}
	w15ScrollTo(t, v, e, "fa-channel")
	vaShot(t, e, "activity-subtext-inline", dark)
}

// U5s4 stickyItems with the VIP sort: scrolled past "VIP unreads" its
// header stays at the list's top (and the rows under it hide their hover
// actions); "Everything else" pushes it up, then takes its place.
func TestActivityStickySectionHeadersLikeSlack(t *testing.T) { w15Modes(t, w15Sticky) }

func w15Sticky(t *testing.T, dark bool) {
	v, _, e, fs := vaFakeActivity(t, dark)
	if err := v.h.Core().AddVIP(context.Background(), "U0FAKE00001"); err != nil {
		t.Fatal(err)
	}
	v.h.Core().SetExperimentGroup("activity_in_home_prototype", "on")
	v.menu = actMenuSettings
	e.frame()
	v.setSortC[1].Click() // VIP unreads on top
	e.frame()
	e.until(t, "activity.prefs.update sort", func() bool {
		return actFakeWrite(fs, "activity.prefs.update", func(f url.Values) bool { return f.Get("sort") == "vip_unreads_first" })
	})
	e.until(t, "the VIP order", func() bool { return len(v.vis) > 0 && v.vis[0] == actVisVIP })
	v.menu = actMenuNone
	elseAt := -1
	for i, x := range v.vis {
		if x == actVisElse {
			elseAt = i
		}
	}
	if elseAt < 2 {
		t.Fatalf("Everything else at %d", elseAt)
	}
	e.size = image.Pt(1000, 420) // short enough to scroll
	e.frame()
	if v.stk.on {
		t.Fatal("stuck before scrolling")
	}
	// one row into the VIP section: its header sticks at the top
	v.list.Position.First, v.list.Position.Offset = 1, 30
	e.frame()
	e.frame()
	if !v.stk.on || !v.stk.vip || v.stk.y != 0 {
		t.Fatalf("scrolled into VIP unreads: on %v vip %v y %d", v.stk.on, v.stk.vip, v.stk.y)
	}
	if first := v.list.Position.First; !v.underSticky(w15Gtx(time.Now()), first) {
		t.Errorf("row %d under the header doesn't hide its actions", first)
	}
	vaShot(t, e, "activity-sticky-vip", dark)
	// Everything else 20px from the top pushes VIP unreads up
	v.list.Position.First, v.list.Position.Offset = elseAt, -20
	e.frame()
	e.frame()
	if !v.stk.on || !v.stk.vip || v.stk.y >= 0 {
		t.Errorf("pushed: on %v vip %v y %d", v.stk.on, v.stk.vip, v.stk.y)
	}
	vaShot(t, e, "activity-sticky-push", dark)
	// past it, Everything else is the stuck one
	v.list.Position.First, v.list.Position.Offset = elseAt+1, 10
	e.frame()
	e.frame()
	if !v.stk.on || v.stk.vip {
		t.Errorf("past Everything else: on %v vip %v", v.stk.on, v.stk.vip)
	}
	// without the layout revisions nothing sticks (e5 is empty)
	v.h.Core().SetExperimentGroup("activity_entry_layout_revisions_2", "off")
	v.dirty = true
	e.frame()
	e.frame()
	if v.stk.on {
		t.Error("stuck without activity_entry_layout_revisions_2")
	}
}

func w15Overlay[T any](h *vaHost) (T, bool) {
	open := h.open()
	for i := len(open) - 1; i >= 0; i-- {
		if o, ok := open[i].(T); ok {
			return o, true
		}
	}
	var zero T
	return zero, false
}

// dcZX SlackConnectAcceptChannelInviteAction: the channel invite now has
// Accept; it checks eligibility, opens the three-pane accept modal
// (workspace, name, privacy), accepts with the name and privacy picked,
// reads the invite back (connectInvites.list) and says admins are taking a
// look. The upgrade and join_channel answers go their own ways.
func TestActivityAcceptChannelInviteLikeSlack(t *testing.T) { w15Modes(t, w15AcceptChannel) }

func w15AcceptChannel(t *testing.T, dark bool) {
	v, h, e, fs := vaFakeActivity(t, dark)
	ch := actItem(t, v, "fa-ext-invite")
	if got := v.inviteActions(ch); len(got) != 2 || got[0] != core.InviteActIgnore || got[1] != core.InviteActAcceptChannel {
		t.Fatalf("channel invite buttons %v", got)
	}
	w15ScrollTo(t, v, e, "fa-ext-invite")
	vaShot(t, e, "activity-channel-invite-row", dark)
	v.inviteClicked(ch, core.InviteActAcceptChannel)
	e.until(t, "the eligibility check", func() bool {
		return actFakeWrite(fs, "conversations.sharedInviteAcceptanceEligibility", func(f url.Values) bool {
			return f.Get("sig") == fakeslack.ActivityInviteChannelSig
		})
	})
	var m *popups.SCAcceptModal
	e.until(t, "the accept modal", func() bool { m, _ = w15Overlay[*popups.SCAcceptModal](h); return m != nil })
	if m.Step() != "workspace" {
		t.Fatalf("first pane %s", m.Step())
	}
	vaShot(t, e, "activity-accept-workspace", dark)
	m.Next()
	e.frame()
	if m.Step() != "name" || m.Name() != "synthetic-partners" {
		t.Fatalf("name pane %s %q", m.Step(), m.Name())
	}
	e.until(t, "the name checked", m.CanNext)
	vaShot(t, e, "activity-accept-name", dark)
	m.SetName("Partners Shared")
	if m.Name() != "partners-shared" {
		t.Errorf("normalized name %q", m.Name())
	}
	e.until(t, "the new name checked", m.CanNext)
	m.Next()
	e.frame()
	if m.Step() != "privacy" {
		t.Fatalf("privacy pane %s", m.Step())
	}
	m.SetPrivate(true)
	e.frame()
	vaShot(t, e, "activity-accept-privacy", dark)
	m.Accept()
	e.until(t, "conversations.acceptSharedInvite", func() bool {
		return actFakeWrite(fs, "conversations.acceptSharedInvite", func(f url.Values) bool {
			return f.Get("invite_id") == fakeslack.ActivityInviteChannel && f.Get("channel_name") == "partners-shared" && f.Get("is_private") == "true"
		})
	})
	e.until(t, "connectInvites.list", func() bool { return actFakeWrite(fs, "connectInvites.list", nil) })
	e.until(t, "the welcome modal", func() bool { _, ok := w15Overlay[*popups.Modal](h); return ok && m.Dismissed() })
	title, body := popups.SCWelcomeText(popups.SCWelcome{ChannelName: "partners-shared", InvitingTeam: "Partner Co", InvitingName: "Pat Partner",
		InvitingTeamID: fakeslack.ExternalTeamID, PendingTeams: []string{fakeslack.TeamID, fakeslack.ExternalTeamID}})
	if title != "All set — admins are taking a look" || !strings.Contains(body, "admins from your organization and Partner Co need to approve it before you’re added to #partners-shared") {
		t.Errorf("welcome %q / %q", title, body)
	}
	vaShot(t, e, "activity-accept-welcome", dark)
	if wm, ok := w15Overlay[*popups.Modal](h); ok {
		wm.Dismiss()
	}
	if title, body := popups.SCWelcomeText(popups.SCWelcome{Connected: true, ChannelName: "partners-shared", InvitingTeam: "Partner Co", InvitingName: "Pat Partner"}); title != "Welcome to the channel!" ||
		body != "#partners-shared is where you’ll work with Pat Partner and others from Partner Co." {
		t.Errorf("connected welcome %q / %q", title, body)
	}

	// an upgrade first: SlackConnectUpgradeModal
	fs.SetChannelInviteEligibility(map[string]any{"is_eligible": true, "requirements": map[string]any{"upgrade": true}})
	n := len(h.open())
	v.inviteClicked(ch, core.InviteActAcceptChannel)
	e.until(t, "the upgrade modal", func() bool { _, ok := w15Overlay[*popups.Modal](h); return ok && len(h.open()) > n })
	vaShot(t, e, "activity-accept-upgrade", dark)
	if um, ok := w15Overlay[*popups.Modal](h); ok {
		um.Dismiss()
	}
	// the org is already in the channel: joinConnectedShared
	fs.SetChannelInviteEligibility(map[string]any{"is_eligible": false, "alternative_actions": map[string]any{"join_channel": map[string]any{}}})
	v.inviteClicked(ch, core.InviteActAcceptChannel)
	e.until(t, "conversations.joinConnectedShared", func() bool { return actFakeWrite(fs, "conversations.joinConnectedShared", nil) })
	// a required privacy disables the other option
	fs.SetChannelInviteEligibility(map[string]any{"is_eligible": true, "requirements": map[string]any{"channel_privacy": map[string]any{"privacy": "private"}}})
	v.inviteClicked(ch, core.InviteActAcceptChannel)
	var m2 *popups.SCAcceptModal
	e.until(t, "the second accept modal", func() bool { m2, _ = w15Overlay[*popups.SCAcceptModal](h); return m2 != nil && m2 != m })
	if m2.SetPrivate(false); !m2.Private() {
		t.Error("public picked when the invite requires private")
	}
}
