package popups

// The search popover's row buttons follow their rows (the lost-click
// sweep's deferred relatives): a row's button is its row's while results
// arrive above it or a new query's rows replace the list, so a held row
// keeps its press (its Clickable stays put) and never becomes another
// row's. They were a slice by index.

import "testing"

func TestSearchPopoverRowButtonsFollowRows(t *testing.T) {
	p := &SearchPopover{}
	row := func(id string) srRow { return srRow{kind: srRMsg, id: id} }
	p.rows = []srRow{row("C1/1.1"), row("C1/1.2")}
	spLanded(p)
	held := spClick(p, 0)
	// a result arrives above the held one
	p.rows = []srRow{row("C1/1.0"), row("C1/1.1"), row("C1/1.2")}
	spLanded(p)
	if spClick(p, 1) != held {
		t.Error("the held row's button changed when a row arrived above it")
	}
	if spClick(p, 0) == held {
		t.Error("the new row took the held row's button")
	}
	// a new query's rows: the held row is gone, its button with it
	p.rows = []srRow{row("C2/2.1"), row("C2/2.2")}
	spLanded(p)
	if spClick(p, 0) == held || spClick(p, 1) == held {
		t.Error("a new query's row took the held row's button")
	}
}
