package messages

import (
	"testing"
	"time"
)

// barHost reports a context bar over the composer (ContextBarShower).
type barHost struct {
	coreHost
	bar bool
}

func (h barHost) ContextBarShowing(string) bool { return h.bar }

// The channel list ends with BaseMessageList's bottomSpacer once the newest
// messages are loaded: 24px (space.150) between the newest message and the
// composer, 16px (space.100) while a context bar shows. Without it the
// newest message (Karim's pinned "anchored test", 2026-11-10) sat right on
// the composer's top edge.
func TestChannelListBottomSpacer(t *testing.T) {
	for _, tc := range []struct {
		bar  bool
		want float32
	}{{false, 24}, {true, 16}} {
		for _, scale := range []float32{1, 2} {
			h := barHost{coreHost: spacingHost(t, false), bar: tc.bar}
			v := NewChannelView(h, tSpacing)
			v.Preview = true
			W, H := int(900*scale), int(700*scale)
			f := scaledFrame(v, scale, W, H)
			loadList(v, f)
			ts := spacingTS
			until(t, 5*time.Second, "the rows", f, func() bool { return v.s.rows[ts.m3] != nil && itemHeight(v.s, ts.m3) > 0 })
			f()
			s := v.s
			last := s.items[len(s.items)-1]
			if last.kind != itBottom {
				t.Fatalf("bar=%v: the last item is %q (kind %d), want the bottom spacer", tc.bar, last.key, last.kind)
			}
			if got := itemHeight(s, "bottomSpacer"); got != px(tc.want, scale) {
				t.Errorf("bar=%v %vx: bottom spacer %d px, Slack %v", tc.bar, scale, got, tc.want*scale)
			}
			// anchored at the newest: the newest row ends the spacer's
			// height above the list's (= the composer's) top edge
			n := s.newestItem()
			if n < 0 || s.items[n].key != ts.m3 {
				t.Fatalf("newest item %d, want %s", n, ts.m3)
			}
			if gap := H - (s.pos[n] + s.heights[n]); gap != px(tc.want, scale) {
				t.Errorf("bar=%v %vx: %d px between the newest message and the composer, Slack %v", tc.bar, scale, gap, tc.want*scale)
			}
		}
	}
}

// Threads have no bottom spacer (the reply composer's container brings its
// own 12px margin).
func TestThreadListNoBottomSpacer(t *testing.T) {
	h := spacingHost(t, false)
	v := NewThreadView(h, tSpacing, spacingTS.root)
	f := scaledFrame(v, 1, 480, 700)
	for i := 0; i < 200 && !(v.s.snap.loaded && len(v.s.items) > 0); i++ {
		f()
		time.Sleep(5 * time.Millisecond)
	}
	f()
	for _, it := range v.s.items {
		if it.kind == itBottom {
			t.Fatal("a thread list has a bottom spacer")
		}
	}
}
