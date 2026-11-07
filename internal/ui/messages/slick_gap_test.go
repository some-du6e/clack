package messages

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

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
