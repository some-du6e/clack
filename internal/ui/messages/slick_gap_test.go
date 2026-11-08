package messages

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
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
