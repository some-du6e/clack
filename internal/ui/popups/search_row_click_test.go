package popups

// A search result held while the results change (the lost-click sweep's
// deferred relatives): Slack keys its result rows by the result, so a row
// held while more results load keeps its click, and one held while a new
// search's results land is gone with its result (no click). The search
// view's controls were slices by index: growth moved every Clickable (Gio
// routes a press by its handler's address), and a new search put another
// result under a held one, which the release then opened. The search
// popover's rows likewise (search_popover_click_test.go). Synthetic:
// fakeslack's search fixtures.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
)

func srSynthChans(prefix string, n int) []core.ChannelHit {
	out := make([]core.ChannelHit, n)
	for i := range out {
		out[i] = core.ChannelHit{ID: fmt.Sprintf("C0%s%04d", prefix, i), Name: fmt.Sprintf("synthetic-%s-%d", strings.ToLower(prefix), i), Kind: "channel", NumMembers: 3}
	}
	return out
}

func TestSearchRowClickSurvivesResultChanges(t *testing.T) {
	for _, tc := range []struct {
		name string
		land func(v *SearchView)
		open bool // the release opens the held result
	}{
		{"more results load", func(v *SearchView) {
			v.chans = append(v.chans, srSynthChans("MORE", 12)...)
			v.growRows(len(v.chans))
		}, true},
		{"a new search's results land", func(v *SearchView) {
			n := len(v.chans)
			v.chans = append(v.chans[:0], srSynthChans("NEWQ", n)...)
			v.growRows(n)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := srNewEnv(t, false)
			ch := e.fs.ChannelIDs()[1]
			word := strings.Split(e.h.a.ConvRef(ch).Name, "-")[0]
			v, d := srView(t, e, word, false)
			v.setTab(srTabChannels)
			d.until(t, "channels", func() bool { return !v.loading && len(v.chans) > 0 })
			for i := 0; i < 3; i++ {
				d.frame()
			}
			held := v.chans[0].ID
			gtx := srTestGtx()
			px := v.padX(gtx, d.size.X)
			y0, ok := v.srChanRowY(gtx, 0)
			if !ok {
				t.Fatal("the first channel isn't on screen")
			}
			p := f32.Pt(float32(px+60), float32(y0+30))
			srMoveTo(d, int(p.X), int(p.Y))
			e.h.mu.Lock()
			e.h.navs = nil
			e.h.mu.Unlock()
			at := time.Second
			d.r.Queue(pointer.Event{Kind: pointer.Press, Position: p, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse, Time: at})
			d.frame()
			tc.land(v)
			d.frame()
			d.frame()
			d.r.Queue(pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: at + 100*time.Millisecond})
			d.frame()
			d.frame()
			e.h.mu.Lock()
			navs := append([]string(nil), e.h.navs...)
			e.h.mu.Unlock()
			want := []string{}
			if tc.open {
				want = []string{"c/" + held}
			}
			if strings.Join(navs, " ") != strings.Join(want, " ") {
				t.Errorf("%s while %s was down: the release opened %v, want %v (Slack keys its result rows by the result)", tc.name, held, navs, want)
			}
		})
	}
}
