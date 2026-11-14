package views

// A To-dos or Saved row opens its message beside the page (gap 11, work
// package W42), from Slack's client (CDN build fa938a086569; 2cd9270e786c
// agrees):
//
//   - The row (later-todo-row kmjU RowContent; BookmarksPage draws its
//     rows with the same component) runs its result's onClick
//     (useMessageRowRenderer ZrhC F): a tombstoned message opens
//     LaterRemoveModal (Kivd openLaterRemoveModal); anything else closes
//     the peek it may be in (maybeClosePeek) and dispatches
//     jumpToSavedMessage (xy6R s). The rail peek's rows (SavedItem 2vyW)
//     do the same. TodosPage's Enter (vAPb rv) and BookmarksPage's (eQN2
//     eT) go straight to jumpToSavedMessage.
//   - jumpToSavedMessage → jumpToMessage (wc0P Z) → jumpViaRoutes (X): a
//     message with a thread_ts (getMessageThreadTs, mun8: a root with
//     replies has its own) opens its thread at the saved message (jpad UX
//     toThread: replyTs and uiState.highlightTs the message's ts,
//     highlightRoot); anything else opens its conversation at the message
//     (jpad R9: startTs and highlightTs).
//   - Where it lands (getTargetContainer, client-boot L1Ms J): on the
//     Later tab with kl02 eU (nested_secondary_view and later_todos) every
//     view but the LaterList and pages goes in the Secondary and the tab
//     stays Later (qBXZ: the Primary is the LaterList, Primary to
//     Secondary). Without eU the LaterList is a Sidebar view (+zKh LJ) and
//     the message takes the Primary beside it. Either way the list stays
//     and the message shows beside it; Clack's To-dos fills the primary,
//     so beside it is the secondary column. On the Saved page (Page
//     Bookmarks in the Home tab's Primary) a conversation goes in the
//     Secondary under 6owI hq (later_todos and the has_starred_item pref)
//     and otherwise replaces the page; a thread is a secondary view
//     everywhere (+zKh aK).
//   - The row is selected (kmjU `.rowSelected`, surf-hgl-3; aria-current)
//     while the primary or secondary sibling view shows its message
//     (Wowu useSavedRowSelectedInEitherDetail over qb0V Ub
//     isSavedMessageSelectedInPrimaryView: a conversation whose startTs is
//     the message, or a thread whose replyTs is). The page is the primary
//     here, so that is the secondary column (SavedSecondary.SecondaryAt).

import "github.com/some-du6e/clack/internal/core"

// SavedSecondary is optionally implemented by the Host: the column a
// To-dos or Saved row opens its message in (the ui root's secondary
// column). Hosts without it open a thread in their thread pane
// (Host.OpenThread) and a conversation in place of the page.
type SavedSecondary interface {
	// OpenMessageSecondary shows conversation ch at message ts in the
	// column (jpad R9: startTs and highlightTs ts).
	OpenMessageSecondary(ch, ts string)
	// OpenThreadAt shows thread (its root's ts) in the column at message
	// ts (jpad UX: replyTs, uiState.highlightTs and highlightRoot).
	OpenThreadAt(ch, thread, ts string)
	// SecondaryAt is the message the column was opened at: the
	// conversation's startTs or the thread's replyTs ("" for none).
	SecondaryAt() (ch, ts string)
}

// rowClick is a row's click (ZrhC F, the peek's 2vyW): LaterRemoveModal
// for a deleted message, else the item opens (jumpToSavedMessage; the
// peek closes first).
func (s *savedBase) rowClick(it *core.SavedItem) {
	if s.openRemoveModal(*it) {
		return
	}
	if s.peeking {
		s.openFromPeek(it)
		return
	}
	s.open(it)
}

// open is jumpToSavedMessage for a message (xy6R s, then where L1Ms puts
// it), openListRecordDetails for a list record.
func (s *savedBase) open(it *core.SavedItem) {
	if it.ItemType == "list_record" {
		s.openListRecord(it) // later_records.go
		return
	}
	if it.Channel == "" || it.TS == "" {
		return
	}
	ch, ts, thread := it.Channel, it.TS, laterThreadTS(it)
	sec, ok := s.h.(SavedSecondary)
	switch {
	case thread != "" && ok:
		sec.OpenThreadAt(ch, thread, ts)
	case thread != "":
		s.h.OpenThread(ch, thread)
	case ok && s.besideList():
		sec.OpenMessageSecondary(ch, ts)
	default:
		s.h.Navigate("c/" + ch + "/m/" + ts) // R9 in the primary
	}
}

// laterThreadTS is getMessageThreadTs (mun8) of the saved message: its
// thread_ts, a root with replies' own too ("" for none, or when the
// message isn't loaded: jumpToMessage would look it up, and it opens a
// root's conversation then).
func laterThreadTS(it *core.SavedItem) string {
	if m := it.Msg; m != nil {
		return m.ThreadTS
	}
	return ""
}

// besideList reports whether a conversation opened from the page stays
// beside it (L1Ms): To-dos keeps its list in every group, Saved under hq.
func (s *savedBase) besideList() bool {
	return s.todos || savedPageOn(s.h.Core())
}

// savedPageOn is 6owI hq, isLaterTodosSavedSynthPageFeatureEnabled:
// later_todos and the has_starred_item pref.
func savedPageOn(c *core.App) bool {
	return c != nil && c.Experiment("later_todos") == "on" && c.PrefBool("has_starred_item", false)
}

// selectedKey is the row the column shows (Wowu over qb0V Ub): the saved
// message the column's conversation or thread was opened at ("" for
// none).
func (s *savedBase) selectedKey() string {
	sec, ok := s.h.(SavedSecondary)
	if !ok {
		return ""
	}
	ch, ts := sec.SecondaryAt()
	if ch == "" || ts == "" {
		return ""
	}
	for _, f := range s.feeds {
		for i := range f.items {
			if it := &f.items[i]; it.ItemType == "message" && it.Channel == ch && it.TS == ts {
				return laterKey(it)
			}
		}
	}
	return ""
}

// Selected is the saved message whose row the page draws selected (the
// one the column shows: selectedKey), "" for none.
func (s *savedBase) Selected() (ch, ts string) {
	if f, i := s.find(s.selectedKey()); f != nil {
		return f.items[i].Channel, f.items[i].TS
	}
	return "", ""
}
