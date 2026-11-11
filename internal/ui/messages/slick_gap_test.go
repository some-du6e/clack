package messages

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
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
