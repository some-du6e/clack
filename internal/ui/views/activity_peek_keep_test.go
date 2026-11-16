package views

// The rail's Activity peek and the Activity page share one Activity. A
// peek used to clear the page (rows, data and scroll position) to refetch
// it, so clicking through after a hover showed an empty list until
// activity.feed answered, and every row was described again in one frame.
// Synthetic content only.

import (
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
)

// Opening the peek refetches the feed's first page with what the page
// shows kept (Slack's peek mounts the page, which refetches; nothing is
// cleared before the answer): the rows, the data and the page's scroll
// position stay while the fetch is out. When it answers, only the rows
// whose item changed are described again.
func TestActivityPeekKeepsThePage(t *testing.T) {
	v, _, e, fs := vaFakeActivity(t, false)
	e.frame()
	before := actPage(v)
	rows := map[string]*actRow{}
	for k, r := range v.rows {
		rows[k] = r
	}
	if len(rows) == 0 {
		t.Fatal("the page drew no rows")
	}
	pos := layout.Position{First: 3, Offset: 7}
	v.list.Position = pos

	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)
	calls := fs.CallCount("activity.feed")
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "activity.feed" {
			<-hold
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })

	v.PeekOpened(func() {}, "activity")
	if !v.data.busy() {
		t.Fatal("the peek didn't refetch the feed")
	}
	if len(v.rows) != len(rows) {
		t.Errorf("the peek left %d rows of %d", len(v.rows), len(rows))
	}
	for k, r := range rows {
		if v.rows[k] != r {
			t.Errorf("row %s was replaced", k)
		}
	}
	if v.list.Position != pos {
		t.Errorf("the page's list moved to %+v, want %+v", v.list.Position, pos)
	}
	if p := actPage(v); len(p.Items) != len(before.Items) {
		t.Errorf("the page has %d items while the fetch is out, want the %d it had", len(p.Items), len(before.Items))
	}
	if v.pk.list.Position != (layout.Position{}) {
		t.Errorf("the peek's own list starts at %+v, want the top", v.pk.list.Position)
	}

	// built before the answer: the rows as the page has them now
	built := map[string]bool{}
	for k, r := range v.rows {
		built[k] = r.built
	}
	release()
	deadline := time.Now().Add(10 * time.Second)
	for fs.CallCount("activity.feed") == calls || v.data.busy() { // no frame: settle below is the first to see the answer
		if time.Now().After(deadline) {
			t.Fatal("the refetch didn't answer")
		}
		time.Sleep(5 * time.Millisecond)
	}
	after := actPage(v)
	same := map[string]bool{}
	for i := range after.Items {
		k := actKey(&after.Items[i])
		for j := range before.Items {
			if actKey(&before.Items[j]) == k && reflect.DeepEqual(after.Items[i], before.Items[j]) &&
				before.Convs[after.Items[i].Channel] == after.Convs[after.Items[i].Channel] {
				same[k] = true
			}
		}
	}
	v.settle()
	kept := 0
	for k := range same {
		r := v.rows[k]
		if r == nil || !built[k] {
			continue
		}
		if !r.built {
			t.Errorf("row %s is described again though the answer left its item as it was", k)
			continue
		}
		kept++
	}
	if kept == 0 {
		t.Fatalf("no built row with an unchanged item to check (%d items the same of %d)", len(same), len(after.Items))
	}
}
