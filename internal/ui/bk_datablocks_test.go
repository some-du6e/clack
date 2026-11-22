package ui

// A bot's leaderboard thread (fakeslack gallery "datatable/leaderboard",
// synthetic, shaped like the Otter thread Karim reported on 2026-11-22):
// its parent's data_visualization in the channel, then the parent's chart
// and its reply's data_table in the thread pane, where Karim saw only the
// messages' fallback text. The table pages from its footer there.

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

func (e *perfEnv) channelDataBlocks(ch string) []messages.DataBlockRect {
	if c := e.a.convos[ch+"/"]; c != nil {
		if l, ok := c.list.(channelList); ok {
			return l.DataBlockRects()
		}
	}
	return nil
}

func (e *perfEnv) threadDataBlocks(ch, ts string) []messages.DataBlockRect {
	if tv := e.threadView(ch, ts); tv != nil {
		return tv.DataBlockRects()
	}
	return nil
}

func TestDataBlocksChannelAndThread(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := fakeslack.GalleryChannelID
	ts := fs.GalleryTS("datatable/leaderboard")
	if ts == "" {
		t.Fatal("no leaderboard in the gallery")
	}
	e.openChannel(ch)
	e.a.JumpToMessage(ch, ts, "")
	e.until("the chart in the channel", 30*time.Second, func() bool {
		for _, d := range e.channelDataBlocks(ch) {
			if d.TS == ts && len(d.Charts) == 1 && d.Charts[0].Viz != nil && d.Charts[0].Viz.Title == "Points leaderboard" {
				return true
			}
		}
		return false
	})

	e.a.OpenThread(ch, ts)
	e.until("the thread's reply", 30*time.Second, func() bool {
		msgs, _ := e.a.data.core.S.Thread(ch, ts)
		return len(msgs) >= 2
	})
	var tbl *messages.DataBlockRect
	var seen string
	dl := time.Now().Add(30 * time.Second)
	scrolled := time.Now()
	for {
		e.frame()
		if time.Since(scrolled) > 500*time.Millisecond {
			// the reply is under the root's tall chart: down the pane
			e.scroll(image.Pt(e.size.X-160, e.size.Y/2), 300)
			scrolled = time.Now()
		}
		charts := 0
		tbl = nil
		var sb strings.Builder
		for _, d := range e.threadDataBlocks(ch, ts) {
			d := d
			charts += len(d.Charts)
			fmt.Fprintf(&sb, "[%s root=%v charts=%d tables=%d] ", d.TS, d.TS == ts, len(d.Charts), len(d.Tables))
			if len(d.Tables) == 1 && d.TS != ts {
				tbl = &d
			}
		}
		seen = sb.String()
		if charts <= 1 && tbl != nil && len(tbl.Tables[0].Rows) == 5 {
			break
		}
		if time.Now().After(dl) {
			tv := e.threadView(ch, ts)
			t.Fatalf("the thread pane (view %v) shows %q, want the root's chart and the reply's table", tv != nil, seen)
		}
		time.Sleep(20 * time.Millisecond)
	}
	tr := tbl.Tables[0]
	if tr.Block == nil || tr.Block.Caption != "Full points leaderboard" || len(tr.Block.Body) != 110 || len(tr.Rows) != 5 {
		t.Fatalf("thread table: %+v (%d rows shown)", tr.Block, len(tr.Rows))
	}
	if p, n := tbl.View.DataTablePage(tr.Block); p != 0 || n != 22 {
		t.Fatalf("page %d of %d, want 0 of 22", p, n)
	}
	next := tr.Buttons["next"]
	if next.Empty() || !next.In(image.Rectangle{Max: e.size}) {
		// bring the footer on screen
		e.scroll(image.Pt(e.size.X-200, e.size.Y/2), 400)
		e.settle(300 * time.Millisecond)
		for _, d := range e.threadDataBlocks(ch, ts) {
			if len(d.Tables) == 1 {
				next = d.Tables[0].Buttons["next"]
			}
		}
	}
	e.click(next.Min.Add(next.Size().Div(2)))
	e.until("the table's second page", 10*time.Second, func() bool {
		p, _ := tbl.View.DataTablePage(tr.Block)
		return p == 1
	})
	if got := tbl.View.DataTableRows(tr.Block); len(got) != 5 || got[0] != "6" {
		t.Errorf("second page's rows %q", got)
	}
}
