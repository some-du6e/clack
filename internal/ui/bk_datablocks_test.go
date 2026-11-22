package ui

// A bot's leaderboard thread (fakeslack gallery "datatable/leaderboard",
// synthetic, shaped like the Otter thread Karim reported on 2026-11-22):
// its parent's data_visualization in the channel, then the parent's chart
// and its reply's data_table in the thread pane, where Karim saw only the
// messages' fallback text. The table pages from its footer there.

import (
	"image"
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
	var tbl *messages.DataBlockRect
	e.until("the chart and the table in the thread", 30*time.Second, func() bool {
		charts := 0
		tbl = nil
		for _, d := range e.threadDataBlocks(ch, ts) {
			d := d
			charts += len(d.Charts)
			if len(d.Tables) == 1 && d.TS != ts {
				tbl = &d
			}
		}
		return charts == 1 && tbl != nil
	})
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
