package views

// The rail's Later peek: boot-shared s1p9 LaterPeekView (web client build
// f132f0e78468) in a PeekView with classNameSuffix "later":
//
//   - The header is the tab's name ("To-dos" with the to-dos experiment,
//     else "Later") and, with anything in progress, the
//     .p-peek_card__header_subtitle "{count} in progress" (todoCount, else
//     the list's length; Later's uncompletedCount).
//   - LaterPeekContent loads the list on mount (saved.list with the to-dos
//     filter: fetchSavedItems, its own instance here: the page's) and,
//     until that answers, shows the medium Spinner floating over the
//     list; with nothing listed it shows the saved_for_later empty state
//     (activeTab "saved").
//   - LaterList is .p-peek_card__later_list: the page's rows (SavedItem,
//     nckZ) square and full width, their hover actions at top 12px right
//     12px, no timestamps, and no selected tint.
//   - A row closes the peek and opens its message the way the page does,
//     in the To-dos tab (forceTabWhenNavigating: Later).

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
)

// PeekSubtitle is the header's count line ("" for none): "{count} in
// progress".
func (l *Later) PeekSubtitle() string {
	n := len(l.peekItems())
	if n == 0 {
		return ""
	}
	return nsLater.T("{count} in progress", "count", n)
}

// PeekOpened is LaterPeekContent's mount: the list starts at the top and
// reloads (its useEffect fetches while loading).
func (l *Later) PeekOpened(closePeek func()) {
	l.peekClose = closePeek
	l.plist.Position = layout.Position{}
	l.plist.Axis = layout.Vertical
	if l.feed.first.started() {
		l.reload(&l.feed)
	}
}

// peekItems is the peek's list: the to-dos still in progress.
func (l *Later) peekItems() []core.SavedItem { return l.inProgress() }

// LayoutPeek draws LaterPeekContent on the card.
func (l *Later) LayoutPeek(gtx C) D {
	l.peeking = true
	defer func() { l.peeking = false }()
	l.frame()
	l.update(gtx)
	size := gtx.Constraints.Max
	gtx.Constraints = layout.Exact(size)
	pp := peekPalOf(l.th)
	fill(gtx, image.Rectangle{Max: size}, pp.bg)
	items := l.peekItems()
	loading := l.feed.first.busy() || !l.feed.first.started()
	if _, ok, _ := l.feed.first.get(); !ok {
		loading = true
	}
	if !loading && len(items) == 0 {
		if _, _, err := l.feed.first.get(); err != nil {
			return l.errorState(gtx, err, &l.retry)
		}
		return l.savedEmpty(gtx, nsLater.T("Nothing saved yet"), nsLater.T("Save a message to keep it here."), nil, "")
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	n := len(items)
	if n > 0 {
		l.scrollList(gtx, &l.plist, n, func(gtx C, i int) D {
			return l.itemRow(gtx, &items[i], true, laterRowSt{})
		})
		if p := l.plist.Position; p.First+p.Count >= n-3 {
			l.loadMore(&l.feed)
		}
	}
	if loading { // Spinner size medium, floating: centred over the list
		d := gtx.Dp(24)
		st := op.Offset(image.Pt((size.X-d)/2, (size.Y-d)/2)).Push(gtx.Ops)
		actSpinner(gtx, d, pp.sec)
		st.Pop()
	}
	return D{Size: size}
}

// openFromPeek is a row's click on the card (SavedItem 2vyW, after
// rowClick's tombstone check): close the peek (maybeClosePeek), then
// jumpToSavedMessage in the To-dos tab (forceTabWhenNavigating Later):
// the page shows the message beside its list, in its thread when it has
// a thread_ts (laterThreadTS, later_beside.go).
func (s *savedBase) openFromPeek(it *core.SavedItem) {
	if s.peekClose != nil {
		s.peekClose()
	}
	if it.Channel == "" {
		return
	}
	if v, ok := s.h.(interface{ ViewTodo(ch, ts, thread string) }); ok { // the To-dos tab, the message beside it
		v.ViewTodo(it.Channel, it.TS, laterThreadTS(it))
		return
	}
	s.open(it)
}

// PeekCount is the rows the card lists (-1 while it loads).
func (l *Later) PeekCount() int {
	if _, ok, _ := l.feed.first.get(); !ok {
		return -1
	}
	return len(l.peekItems())
}
