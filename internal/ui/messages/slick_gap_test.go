package messages

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// LocalTime (like UserPronouns) patches BaseBroadcastPreamble; its children
// then flow inline through slackc, and the name-to-time gap nameLine adds
// natively must come with the Timestamp slot (live it read "clod3:55 PM").
func TestSlickViewLocalTimeKeepsTimestampGap(t *testing.T) {
	e := newSlickEnv(t, nil)
	e.install(t, "LocalTime", nil)
	patched(t, "BaseBroadcastPreamble")
	v := NewChannelView(e.h, tGen)
	v.Preview = true
	var r *row
	e.waitFor(t, "a channel row", frameFn(v), func() bool {
		for _, x := range v.s.rows {
			if x != nil && x.m.User != "" && x.rc != nil {
				r = x
				return true
			}
		}
		return false
	})
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Constraints: layout.Constraints{Max: image.Pt(800, 60)}}
	plain := r.layoutTimestamp(gtx, r.timestampProps())
	slot := r.layoutPreambleTimestamp(gtx, r.timestampProps())
	if want := plain.Size.X + dp(gtx, mt.tsGap); slot.Size.X != want || plain.Size.X == 0 {
		t.Errorf("Timestamp slot width %d, want %d (timestamp %d + gap %d)", slot.Size.X, want, plain.Size.X, dp(gtx, mt.tsGap))
	}
	kids, _ := r.preambleProps()["children"].([]any)
	if len(kids) != 2 {
		t.Fatalf("preamble children = %v", kids)
	}
	if s, ok := kids[1].(slackc.Slot); !ok || s.Name != "Timestamp" {
		t.Errorf("preamble children = %v", kids)
	}
}

// Nicknames renames people through patchSlice('members'); Slack's message
// sender reads the member from the store, so the row's name follows it.
// (Slick's names setting keeps only ids like /^[UWA][A-Z0-9]{6,}$/.)
func TestSlickViewNicknamesRenamesSender(t *testing.T) {
	const ch, uid, ts = "C0NICKS01", "U0NICKAL1", "1700000401.000100"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutUsers([]slack.User{{ID: uid, Name: "nickal", RealName: "Nick Al", Profile: slack.Profile{DisplayName: "nickal", RealName: "Nick Al"}}})
		s.PutConv(slack.Conversation{ID: ch, Name: "nicks", IsChannel: true, IsMember: true, NumMembers: 2})
		s.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: uid, Text: "synthetic hello"}}, false)
	})
	e.install(t, "Nicknames", map[string]any{"names": map[string]any{uid: "Ally Nick"}})
	v := NewChannelView(e.h, ch)
	v.Preview = true
	f := frameFn(v)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f()
		if r := v.s.rows[ts]; r != nil && r.name == "Ally Nick" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	tt := tautOf()
	mf, ok := tt.MemberView(uid)
	name := ""
	if r := v.s.rows[ts]; r != nil {
		name = r.name
	}
	t.Fatalf("row named %q, want the nickname; members patched %v, MemberView %+v %v", name, tt.SlicePatched("members"), mf, ok)
}

// UserPronouns: a person's pronouns follow the timestamp in a channel
// row's header (BaseBroadcastPreamble's children, after the Timestamp slot,
// bulleted), and grouped follow-ups get none.
func TestSlickViewUserPronouns(t *testing.T) {
	const ch, uid = "C0PRONOUN1", "U0PRONOUN1"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutUsers([]slack.User{{ID: uid, Name: "pat", RealName: "Pat Example", Profile: slack.Profile{DisplayName: "pat", RealName: "Pat Example", Pronouns: "xe/xem"}}})
		s.PutConv(slack.Conversation{ID: ch, Name: "pronouns", IsChannel: true, IsMember: true, NumMembers: 2})
		s.SetLatest(ch, []slack.Message{
			{Type: "message", TS: "1700000501.000100", User: uid, Text: "synthetic first"},
			{Type: "message", TS: "1700000502.000100", User: uid, Text: "synthetic follow-up"},
		}, false)
	})
	e.install(t, "UserPronouns", nil)
	patched(t, "BaseBroadcastPreamble")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	e.waitFor(t, "the pronouns in the row's header", frameFn(v), func() bool {
		return e.ui.anyTree("slick-pronouns--bulleted") && e.ui.anyTree("xe/xem")
	})
	n := 0
	e.ui.mu.Lock()
	for _, tr := range e.ui.trees {
		n += strings.Count(string(tr), `"xe/xem"`)
	}
	e.ui.mu.Unlock()
	if n != 1 {
		t.Errorf("the pronouns are drawn %d times, want once (the grouped follow-up has no header)", n)
	}
}

// UserPronouns' style, against Slack's: the plugin's .slick-pronouns is
// 12px, font-weight 400, rgba(var(--sk_foreground_max_solid)), a "•"
// with 4px each side before it, exactly the native timestamp's text
// (.c-timestamp: 12px, sk_foreground_max_solid) and nothing like the
// sender's 15px Black primary foreground. In the channel row
// (BaseBroadcastPreamble) and the thread pane's header
// (ThreadSenderAndTimestampGeneric), in both modes, and inked like the
// timestamp: slackc painted the light pronouns at full alpha where the
// timestamp's label fades, so in dark mode they read heavier than it.
func TestSlickViewUserPronounsStyle(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) { pronounsStyle(t, dark) })
	}
}

func pronounsStyle(t *testing.T, dark bool) {
	const ch, uid, tsRoot, tsReply = "C0PRONOUN2", "U0PRONOUN2", "1700000601.000100", "1700000602.000100"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutUsers([]slack.User{{ID: uid, Name: "pat", RealName: "Pat Example", Profile: slack.Profile{DisplayName: "pat", RealName: "Pat Example", Pronouns: "xe/xem"}}})
		s.PutConv(slack.Conversation{ID: ch, Name: "pronouns-style", IsChannel: true, IsMember: true, NumMembers: 2})
		s.SetLatest(ch, []slack.Message{
			{Type: "message", TS: tsRoot, User: uid, Text: "synthetic root", ThreadTS: tsRoot, ReplyCount: 1},
		}, false)
		s.SetThread(ch, tsRoot, []slack.Message{
			{Type: "message", TS: tsRoot, User: uid, Text: "synthetic root", ThreadTS: tsRoot, ReplyCount: 1},
			{Type: "message", TS: tsReply, User: "UALICE", Text: "synthetic reply", ThreadTS: tsRoot},
		})
	})
	e.h.th = theme.New(dark)
	slackc.Init(slackc.Env{Theme: e.h.th, Core: e.app, Invalidate: func() {}})
	t.Cleanup(func() { slackc.Init(slackc.Env{}) })
	e.install(t, "UserPronouns", nil)
	patched(t, "BaseBroadcastPreamble", "ThreadSenderAndTimestampGeneric")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	tv := NewThreadView(e.h, ch, tsRoot)
	fc, ft := frameFn(v), frameFn(tv)
	both := func() { fc(); ft() }
	styles := func(comp string) []slackc.ClassStyle {
		var out []slackc.ClassStyle
		for _, cs := range slackc.ClassStyles("slick-pronouns") {
			if cs.Component == comp {
				out = append(out, cs)
			}
		}
		return out
	}
	e.waitFor(t, "the pronouns in the channel row and the thread header", both, func() bool {
		return len(styles("BaseBroadcastPreamble")) > 0 && len(styles("ThreadSenderAndTimestampGeneric")) > 0
	})

	c := lightColors
	want := color.NRGBA{R: 97, G: 96, B: 97, A: 0xff} // rgba(var(--sk_foreground_max_solid)), light
	if dark {
		c = darkColors
		want = color.NRGBA{R: 171, G: 171, B: 173, A: 0xff}
	}
	if c.Text2 != want {
		t.Fatalf("the timestamp's colour %v, want sk_foreground_max_solid %v", c.Text2, want)
	}
	for _, comp := range []string{"BaseBroadcastPreamble", "ThreadSenderAndTimestampGeneric"} {
		for _, cs := range styles(comp) {
			t.Logf("%s: %+v", comp, cs)
			if cs.Color != want {
				t.Errorf("%s: pronouns' colour %v, want Slack's sk_foreground_max_solid %v (the timestamp's)", comp, cs.Color, want)
			}
			if cs.Color == c.Text {
				t.Errorf("%s: pronouns in the sender's colour %v", comp, cs.Color)
			}
			if cs.FontSize != 12 {
				t.Errorf("%s: pronouns at %vpx, want 12px (the timestamp's, not the name's 15px)", comp, cs.FontSize)
			}
			if cs.FontWeight != 400 {
				t.Errorf("%s: pronouns at weight %d, want 400 (the name is 900)", comp, cs.FontWeight)
			}
			if cs.Opacity != 1 {
				t.Errorf("%s: pronouns at opacity %v, want 1", comp, cs.Opacity)
			}
			// inked as the timestamp's label is, at 1x and on a Retina 2x
			for _, dp := range []float32{1, 2} {
				if got, ts := slackc.TextInk(cs.Color, cs.FontWeight, dp, false), theme.LabelInk(c.Text2, dp, theme.Regular, false); got != ts {
					t.Errorf("%s at %vx: pronouns inked %v, the timestamp %v", comp, dp, got, ts)
				}
			}
		}
	}
}

// CopyReacted: its copy button follows the add-reaction button
// (ReactionAddButton, a child of ReactionBar's tree, so the bar's
// reactions reach it through context).
func TestSlickViewCopyReacted(t *testing.T) {
	e := newSlickEnv(t, nil)
	e.install(t, "CopyReacted", nil)
	patched(t, "ReactionBar", "ReactionAddButton")
	v := NewChannelView(e.h, tGen)
	v.Preview = true
	e.waitFor(t, "the copy button by the add-reaction button", frameFn(v), func() bool {
		return e.ui.anyTree("slick_copy_reacted")
	})
}
