package core

import (
	"context"
	"reflect"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A page past convertEachMin is converted on every core, each message on
// its own: the same messages, in the same order, as converting them one
// by one (#giant-wall's page of 28 walls took FromMessageIn 240 ms in a
// row). Run under -race in flake-check's history-convert.
func TestTimelineConvertsBigPagesLikeOneByOne(t *testing.T) {
	ctx := context.Background()
	sz := fakeslack.Small
	sz.Giant = 12 // 11 walls of 908k characters and a short one
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := a.C.History(ctx, fakeslack.GiantWallID, slack.HistoryOpts{Limit: 28, Inclusive: true})
	if err != nil {
		t.Fatal(err)
	}
	size := 0
	for _, m := range p.Messages {
		size += len(m.Text) + len(m.Blocks)
	}
	if size < convertEachMin {
		t.Fatalf("the page is %d bytes, under convertEachMin: this test no longer converts on every core", size)
	}
	got := a.timeline(fakeslack.GiantWallID, p.Messages)
	var want []Message
	for i := range p.Messages {
		m := &p.Messages[i]
		if m.Subtype == "thread_broadcast" || m.ThreadTS == "" || m.ThreadTS == m.TS || m.ReplyBroadcast {
			want = append(want, a.message(fakeslack.GiantWallID, m))
		}
	}
	if len(got) != len(want) || len(got) == 0 {
		t.Fatalf("%d messages converted, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("message %d (%s) converted on its own core differs from one by one", i, want[i].TS)
		}
	}
}
