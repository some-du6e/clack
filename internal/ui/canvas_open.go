package ui

// A canvas's or a list's hidden file channel (core canvas_channel.go)
// never shows as a conversation once the file is known. Karim (α.102)
// clicked a canvas mention and got "a limbo esque channel where the
// canvas contents are messages". Slack has two rules for it (client-boot
// 548618a2169f2a90):
//
//   - RedirectFileChannel (AOkh aW, a navigation middleware over every
//     container) rewrites a direct navigation's Channel view of a file
//     channel whose file is in the store. For a canvas, toMessage becomes
//     the thread view on that message (toQuipCommentThreadFromThreadIds
//     without a comment thread: the ordinary thread pane, the message its
//     highlighted root) and toChannel the canvas (toCanvas). For a list,
//     toMessage becomes the list at the record whose comment thread the
//     message is (toListRecordByThreadTs) and toChannel the list
//     (toList). A Thread view (toThread) stays as it is. Clack's direct
//     navigations are the secondary column's (OpenMessageSecondary,
//     OpenChannelSecondary: Activity's inbox, Later), the split view's
//     (OpenSplit) and the main view's toChannel (Navigate "c/<C>"):
//     redirectInstead.
//   - jumpToMessage (wc0P: a link, a search result, a notification, the
//     Activity persistent panel's click) opens a canvas's message in the
//     canvas (isSpaceship → lFpV openDocWithThreadTs → zsuR openDoc: a
//     mention at its section, a comment's root with its comment thread
//     beside it) and a list's in the list at the record (isList), from
//     wherever it starts: jumpInstead, for Navigate's c/<C>/m|t/<ts>
//     routes and jumpToMessage.
//
// Both read the file from the store. Slack's store has it by then (the
// row's entity fetch, maybePrefetchCanvasFromNotification); when Clack's
// doesn't yet, the navigation waits for the files.info the store asked
// for (the "names" that brings the file id: canvasJumpNames), at most
// canvasWaitLimit, Clack's own stand-in. Slack doesn't wait: with no file
// its middleware mounts the conversation, as Clack does when the wait
// ends, or when the file turns out to be neither. Any navigation in
// between drops one still waiting. A jump reads what it opens from a
// message (core CanvasJump, ListRecordOf); when the store doesn't hold
// it, messages.list brings it first, as lFpV and the List view's
// "load-list-record-root-message" do.

import (
	"context"
	"image"
	"log"
	"sync"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/listview"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/views"
)

var (
	_ views.FileOpener        = (*App)(nil)
	_ views.SavedJumper       = (*App)(nil)
	_ messages.FileThreadHost = (*App)(nil)
)

// canvasJumpState is the App's navigations into file channels. UI
// goroutine only.
type canvasJumpState struct {
	// gen counts navigations: one that waited for its file or its message
	// opens only while gen is still its own
	gen     uint64
	waiting map[string]fileWait // file id -> the navigation waiting for its files.info
	// focus is a full view's section to focus once its page is made
	// (OpenCanvasAt before the route's view exists)
	focus map[string]string
	// pane is the Activity persistent detail panel's canvas view and list
	// its list view (FilePane), paneSeq the frame that last drew one.
	// paneFile is the file the pane's jump opened, paneKind its kind
	// ("canvas", "list"), paneRec the list record, paneDone whether the
	// jump resolved: a file, or "" for none (the item's conversation)
	pane     *canvasview.View
	list     View
	paneSeq  uint64
	paneFile string
	paneKind string
	paneRec  string
	paneDone bool
	// bypass is set while a navigation falls back to the conversation
	bypass bool
}

// fileWait is a navigation into a file channel waiting for its file's
// files.info.
type fileWait struct {
	gen      uint64
	ch, at   string                 // the file channel, and the message whose document_mention may name its canvas
	jump     bool                   // a jump (jumpToMessage), else a redirect (RedirectFileChannel)
	open     func(fid, kind string) // the file is a canvas ("canvas") or a list ("list")
	fallback func()                 // the conversation, when the file is neither or files.info never answers
	still    func() bool            // whether the navigation still stands (Activity's item still open); nil: no such check
	split    bool                   // it started in the split view, and ends there
}

// fileTo is what a direct navigation into a file channel opens instead
// (RedirectFileChannel): for a canvas's, for a list's.
type fileTo struct {
	canvas, list func(fileID string)
}

// canvasWaitLimit is how long a navigation waits for files.info before
// it opens the conversation after all: a transport error never brings
// the file id's "names". Slack never waits (its store has the file), so
// this is Clack's.
const canvasWaitLimit = 4 * time.Second

var canvasJumps sync.Map // *App -> *canvasJumpState

func (a *App) canvasJumpsOf() *canvasJumpState {
	if v, ok := canvasJumps.Load(a); ok {
		return v.(*canvasJumpState)
	}
	v, _ := canvasJumps.LoadOrStore(a, &canvasJumpState{waiting: map[string]fileWait{}, focus: map[string]string{}})
	return v.(*canvasJumpState)
}

// canvasNavigated drops the navigations into file channels still waiting
// (any navigation).
func (a *App) canvasNavigated() { a.canvasJumpsOf().gen++ }

// OpenCanvasAt is the canvas viewer on fileID, focused on sectionID (""
// for the top; canvasview FocusSection, as openDoc's focusSection), in the
// side panel or, with full, the full view ("docs/<F…>", openDoc's
// navigation in Primary).
func (a *App) OpenCanvasAt(fileID, sectionID string, full bool) {
	if fileID == "" {
		return
	}
	if !full {
		a.OpenCanvas(fileID)
		if v, ok := a.secondary.view.(*canvasview.View); ok && v.ID() == fileID && sectionID != "" {
			v.FocusSection(sectionID)
		}
		return
	}
	r := Route("docs/" + fileID)
	a.Navigate(string(r))
	if sectionID == "" {
		return
	}
	if v, ok := a.views[r].(*canvasview.View); ok {
		v.FocusSection(sectionID)
		return
	}
	a.canvasJumpsOf().focus[fileID] = sectionID
}

// takeCanvasFocus is the section a full view of fileID opens at (once).
func (a *App) takeCanvasFocus(fileID string) string {
	st := a.canvasJumpsOf()
	sec := st.focus[fileID]
	delete(st.focus, fileID)
	return sec
}

// FileShown is views.FileOpener: the secondary column shows canvas or
// list fileID (the list, or one of its items).
func (a *App) FileShown(fileID string) bool {
	if a.secondary.kind == secNone || a.secondary.hidden || fileID == "" {
		return false
	}
	switch v := a.secondary.view.(type) {
	case *canvasview.View:
		return v.ID() == fileID
	case *listview.View:
		return v.ID() == fileID
	case *listview.Item:
		return v.ListID() == fileID
	}
	return false
}

// OpenFileJump is views.FileOpener: Activity's jump into file channel
// ch, at its thread or message ts (Slack's jumpToMessage), the canvas or
// list where says. It stands while Activity's item stays open. A file
// that is neither, or whose files.info never answers, opens the item's
// conversation instead, as the click does for any other conversation.
func (a *App) OpenFileJump(ch, thread, ts string, where views.FileWhere) {
	key := a.activityOpened()
	still := func() bool { return a.activityOpened() == key }
	fallback := func() {
		if ts != "" {
			a.OpenMessageSecondary(ch, ts)
		} else {
			a.OpenChannelSecondary(ch)
		}
	}
	if where == views.FileInPane {
		st := a.canvasJumpsOf()
		st.paneFile, st.paneKind, st.paneRec, st.paneDone = "", "", "", false
		fallback = func() {
			st.paneFile, st.paneKind, st.paneRec, st.paneDone = "", "", "", true
			a.JumpPane(ch, thread, ts)
		}
	}
	if !a.fileJumpOf(ch, thread, ts, where, fallback, still) {
		a.canvasFallback(fallback)
	}
}

// JumpToSaved is views.SavedJumper: Later's row and rail peek row on
// message ts (in thread) of file channel ch, Slack's jumpToSavedMessage
// (vh7T: a document comment root's openDocWithThreadTs, else
// jumpToMessage): the canvas, at a mention's section or with a comment's
// thread, or the list at the record, where says. A file that turns out to
// be neither opens the message as Later opens any (savedBase.open).
func (a *App) JumpToSaved(ch, thread, ts string, where views.FileWhere) bool {
	fallback := func() {
		switch {
		case thread != "":
			a.OpenThreadAt(ch, thread, ts)
		case where == views.FileSide:
			a.OpenMessageSecondary(ch, ts)
		default:
			a.Navigate("c/" + ch + "/m/" + ts)
		}
	}
	return a.fileJumpOf(ch, thread, ts, where, fallback, nil)
}

// OpenSavedInHome is views.SavedJumper: Later's menu's Open in Home on
// message ts of file channel ch, R9 toMessage with forceTab Home
// (SavedItemActionsMenu e6, a file channel's reply too), which
// RedirectFileChannel makes the thread view on the message, in the
// column of the Home tab (a reply's at its thread's root, the reply
// highlighted), or the list at the record (a List view: the main view).
func (a *App) OpenSavedInHome(ch, ts string) bool {
	return a.redirectInstead(ch, ts, fileTo{
		canvas: func(string) {
			root := ts
			if c := a.Core(); c != nil {
				if m, held := c.S.FindHeld(ch, ts); held && m.ThreadTS != "" && m.ThreadTS != ts {
					root = m.ThreadTS
				}
			}
			a.railClick("home", image.Point{}) // forceTab Home
			a.openFileThreadAt(ch, root, ts)
		},
		list: func(fid string) { a.listJump(fid, ch, "", ts, views.FileFull, nil) },
	}, func() { a.Navigate("c/" + ch + "/m/" + ts) }, nil)
}

// activityStill is a late navigation's still for the column: whether the
// Activity item open now (an inbox click's) is still the open one; a
// deselect, another item or a tab switch drops the navigation.
func (a *App) activityStill() func() bool {
	key := a.activityOpened()
	return func() bool { return a.activityOpened() == key }
}

// activityOpened is the Activity page's open item ("" for none).
func (a *App) activityOpened() string {
	if v, ok := a.views[Route("activity")].(*views.Activity); ok {
		return v.OpenedItem()
	}
	return ""
}

// PaneFile is views.FileOpener: the canvas or list the persistent detail
// panel's pane jump opened, and whether the jump resolved (a file, or ""
// for none: the pane shows the item's conversation).
func (a *App) PaneFile() (string, bool) {
	st := a.canvasJumpsOf()
	return st.paneFile, st.paneDone
}

// FilePane is views.FileOpener: file fileID in the Activity persistent
// detail panel's own pane, the canvas or the list (at its record) its
// jump opened. Slack's openDoc and the list's itemDetailsInPrimary put it
// in Primary and leave the inbox in the Sidebar.
func (a *App) FilePane(fileID string) layout.Widget {
	// what the jump opened, kept with the widget: the pane redraws it while
	// the next jump resolves (Activity.pane), when the state says that one
	st := a.canvasJumpsOf()
	list, rec := st.paneKind == "list" && st.paneFile == fileID, st.paneRec
	return func(gtx layout.Context) layout.Dimensions {
		st.paneSeq = a.FrameSeq()
		if list {
			return a.activityList(fileID, rec).Layout(gtx)
		}
		return a.activityCanvas(fileID).Layout(gtx)
	}
}

// paneCanvasLive is the Activity pane's canvas view while the pane draws
// it (nil otherwise): one the pane left goes stale, its live updates
// stopped, and must not write over newer edits.
func (a *App) paneCanvasLive() *canvasview.View {
	st := a.canvasJumpsOf()
	if st.pane == nil || st.paneKind == "list" || a.FrameSeq()-st.paneSeq > 2 {
		return nil
	}
	return st.pane
}

// activityCanvas is the Activity pane's canvas view on fileID: the one it
// has, or a new one for another canvas.
func (a *App) activityCanvas(fileID string) *canvasview.View {
	st := a.canvasJumpsOf()
	if st.pane == nil || st.pane.ID() != fileID {
		st.pane = a.newCanvasView(fileID, canvasview.Full)
	}
	return st.pane
}

// activityList is the Activity pane's list view on fileID: record rec's
// details (Slack's itemDetailsInPrimary: the record's fields and its
// comment thread), else the list itself.
func (a *App) activityList(fileID, rec string) View {
	st := a.canvasJumpsOf()
	switch v := st.list.(type) {
	case *listview.Item:
		if rec != "" && v.ListID() == fileID && v.RecordID() == rec {
			return v
		}
	case *listview.View:
		if rec == "" && v.ID() == fileID {
			return v
		}
	}
	if rec != "" {
		act := a.listActions()
		act.OpenList = a.OpenList
		st.list = listview.NewItem(a, fileID, rec, act)
	} else {
		st.list = a.newListView(fileID, listview.Full)
	}
	return st.list
}

// jumpInstead is Slack's jumpToMessage into a file channel (wc0P): for a
// canvas's file channel ch the canvas opens instead (isSpaceship; core
// CanvasJump: a mention at its section, a comment with its comment thread
// in the column), for a list's the list at the record whose comment
// thread the message is in (isList), where says. A jump that waits for
// files.info calls fallback (the conversation, guards off) if the file is
// neither, or files.info never answers. It reports whether the jump is
// the file's.
func (a *App) jumpInstead(ch, thread, ts string, where views.FileWhere, fallback func()) bool {
	return a.fileJumpOf(ch, thread, ts, where, fallback, nil)
}

// fileJumpOf is jumpInstead with still, whether the jump still stands
// when it opens late (nil: until the next navigation).
func (a *App) fileJumpOf(ch, thread, ts string, where views.FileWhere, fallback func(), still func() bool) bool {
	at := thread
	if at == "" {
		at = ts
	}
	return a.fileWaitFor(ch, at, func(fid, kind string) {
		if kind == "list" {
			a.listJump(fid, ch, thread, ts, where, still)
			return
		}
		a.canvasJump(fid, ch, thread, ts, where, still)
	}, fallback, still)
}

// redirectInstead is Slack's RedirectFileChannel for a direct navigation
// to file channel ch at message ts ("" for the conversation itself: a
// Channel view's toChannel, else its toMessage): to's canvas or list
// opens instead, once the file is known. fallback is the navigation
// itself, guards off, for a file that turns out to be neither. It reports
// whether the navigation is the file's. still is whether it still stands
// when it ends late (nil: until the next navigation). A canvas Slack
// Connect keeps from the viewer is no redirect (fileIsCanvas reads mode
// quip): the conversation opens.
func (a *App) redirectInstead(ch, ts string, to fileTo, fallback func(), still func() bool) bool {
	return a.fileWaitForAs(ch, ts, false, func(fid, kind string) {
		if kind == "list" {
			to.list(fid)
			return
		}
		to.canvas(fid)
	}, fallback, still)
}

// fileWaitFor runs open on ch's file once its kind is known (core
// FileChannelKind with message at): now, or when files.info answers. It
// reports false for a conversation that is no canvas's or list's file
// channel, and while a fallback runs.
func (a *App) fileWaitFor(ch, at string, open func(fid, kind string), fallback func(), still func() bool) bool {
	return a.fileWaitForAs(ch, at, true, open, fallback, still)
}

// fileWaitForAs is fileWaitFor for a jump (jumpToMessage, which opens a
// blocked canvas too: isBlockedExternalCanvas) or a redirect.
func (a *App) fileWaitForAs(ch, at string, jump bool, open func(fid, kind string), fallback func(), still func() bool) bool {
	c := a.Core()
	st := a.canvasJumpsOf()
	if c == nil || ch == "" || st.bypass {
		return false
	}
	fid, kind, wait := a.fileKind(ch, at, jump)
	switch {
	case wait:
		st.gen++
		gen := st.gen
		st.waiting[fid] = fileWait{gen: gen, ch: ch, at: at, jump: jump, open: open, fallback: fallback, still: still, split: a.pane == paneSplit}
		time.AfterFunc(canvasWaitLimit, func() { a.Post(func() { a.canvasWaitOver(fid, gen) }) })
		return true
	case kind == "":
		return false
	}
	open(fid, kind)
	return true
}

// fileKind is core's FileChannelKind as a jump or a redirect reads it: a
// redirect takes a canvas Slack Connect blocks for no canvas.
func (a *App) fileKind(ch, at string, jump bool) (fid, kind string, wait bool) {
	c := a.Core()
	fid, kind, wait = c.FileChannelKind(ch, at)
	if !jump && kind == "canvas" && c.CanvasBlocked(fid) {
		return fid, "", false
	}
	return fid, kind, wait
}

// canvasJump opens canvas fid where a jump to ch's thread or message ts
// says, from the message the store holds, else once messages.list brought
// it. Nothing opens if it can't be had, as in Slack, nor once another
// navigation or click came first.
func (a *App) canvasJump(fid, ch, thread, ts string, where views.FileWhere, still func() bool) {
	c := a.Core()
	st := a.canvasJumpsOf()
	st.gen++
	if sec, comment, known := c.CanvasJump(ch, thread, ts); known {
		a.openCanvasTarget(fid, ch, sec, comment, where)
		return
	}
	gen := st.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sec, comment, err := c.CanvasJumpFetch(ctx, ch, thread, ts)
		a.Post(func() {
			if a.canvasJumpsOf().gen != gen || a.Core() != c || still != nil && !still() {
				return // another navigation or click since, or another account
			}
			if err != nil {
				log.Printf("canvas %s: the jump's message: %v", fid, err)
				return
			}
			a.openCanvasTarget(fid, ch, sec, comment, where)
		})
	}()
}

// openCanvasTarget opens canvas fid at section sec where says, with
// comment thread comment (its root's ts) of file channel ch beside it,
// unless the column shows that thread already.
func (a *App) openCanvasTarget(fid, ch, sec, comment string, where views.FileWhere) {
	switch where {
	case views.FileInPane:
		st := a.canvasJumpsOf()
		if v := a.activityCanvas(fid); sec != "" {
			v.FocusSection(sec)
		}
		st.paneFile, st.paneKind, st.paneRec, st.paneDone = fid, "canvas", "", true
		a.Invalidate()
	case views.FileFull:
		a.OpenCanvasAt(fid, sec, true)
	case views.FileInSplit:
		// a jump from the split view stays there: a comment's thread on its
		// back stack (toQuipCommentThread), else the canvas in the side
		// panel, Clack's split view holding conversations only
		if comment != "" {
			a.inSplitIf(true, func() { a.OpenSplit("c/" + ch + "/t/" + comment) })
			return
		}
		a.OpenCanvasAt(fid, sec, false)
		return
	default:
		a.OpenCanvasAt(fid, sec, false)
	}
	if comment != "" && !(a.secondary.open() && a.secondary.kind == secThread && a.secondary.channel == ch && a.secondary.ts == comment) {
		a.OpenCommentThread(fid, ch, comment)
	}
}

// listJump opens list fid where a jump to ch's thread or message ts says
// (wc0P isList), at the record whose comment thread the message is in:
// the held root's slack_list (core ListRecordOf), else the root
// messages.list brings. The list opens without a record when the root
// can't be had or names none; nothing opens once another navigation or
// click came first.
func (a *App) listJump(fid, ch, thread, ts string, where views.FileWhere, still func() bool) {
	c := a.Core()
	st := a.canvasJumpsOf()
	st.gen++
	if rec := c.ListRecordOf(ch, thread, ts); rec != "" || thread == "" && ts == "" {
		a.openListTarget(fid, rec, where)
		return
	}
	gen := st.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rec, err := c.ListRecordFetch(ctx, ch, thread, ts)
		a.Post(func() {
			if a.canvasJumpsOf().gen != gen || a.Core() != c || still != nil && !still() {
				return // another navigation or click since, or another account
			}
			if err != nil {
				log.Printf("list %s: the jump's message: %v", fid, err)
			}
			a.openListTarget(fid, rec, where)
		})
	}()
}

// openListTarget opens list fid where says, at record rec ("" for the
// list itself): the full view with the record's item pane beside it
// (Slack's record details; Clack's item pane is the side panel's), the
// side panel's list or item, or the Activity pane's.
func (a *App) openListTarget(fid, rec string, where views.FileWhere) {
	switch where {
	case views.FileInPane:
		st := a.canvasJumpsOf()
		a.activityList(fid, rec)
		st.paneFile, st.paneKind, st.paneRec, st.paneDone = fid, "list", rec, true
		a.Invalidate()
		return
	case views.FileFull:
		a.OpenListFull(fid)
	default:
		if rec == "" {
			a.OpenList(fid)
		}
	}
	if rec != "" {
		a.OpenListItem(fid, rec)
	}
}

// openFileThread is RedirectFileChannel's toQuipCommentThreadFromThreadIds
// without a comment thread, in the secondary column: the thread pane on
// message ts of canvas file channel ch, the message its highlighted root
// (threadTs, highlightTs).
func (a *App) openFileThread(ch, ts string) { a.openFileThreadAt(ch, ts, ts) }

// openFileThreadAt is openFileThread for a message ts in thread root's
// thread: the thread pane on the root, ts highlighted.
func (a *App) openFileThreadAt(ch, root, ts string) {
	a.OpenThread(ch, root)
	s := &a.secondary
	if s.kind != secThread || s.channel != ch || s.ts != root || s.hidden {
		return
	}
	s.at = ts
	if j, ok := a.convoFor(ch, root).list.(interface{ JumpTo(ts string, flash bool) }); ok {
		j.JumpTo(ts, true)
	}
	a.Invalidate()
}

// openSplitFileThread is openFileThread in the split view.
func (a *App) openSplitFileThread(ch, ts string) {
	a.OpenSplit("c/" + ch + "/t/" + ts)
	if a.splitTop() != Route("c/"+ch+"/t/"+ts) {
		return
	}
	a.inPane(paneSplit, func() {
		if j, ok := a.convoFor(ch, ts).list.(interface{ JumpTo(ts string, flash bool) }); ok {
			j.JumpTo(ts, true)
		}
	})
	a.Invalidate()
}

// canvasFallback runs a navigation's fallback with the file guards off.
func (a *App) canvasFallback(f func()) {
	if f == nil {
		return
	}
	st := a.canvasJumpsOf()
	st.bypass = true
	defer func() { st.bypass = false }()
	f()
}

// inSplitIf runs f in the split view when split (a navigation that
// started there ends late), its back stack keeping the view the
// navigation started from. A split view closed since drops the
// navigation: it never reopens one, nor lands outside it.
func (a *App) inSplitIf(split bool, f func()) {
	switch {
	case !split:
		f()
	case a.splitOpen():
		a.inPane(paneSplit, f)
	}
}

// jumpWhere is where a jump from the container being drawn opens a file
// (getTargetContainer): the split view's stand-ins from the split view,
// else the main view.
func (a *App) jumpWhere() views.FileWhere {
	if a.pane == paneSplit {
		return views.FileInSplit
	}
	return views.FileFull
}

// canvasWaitOver ends a navigation that waited canvasWaitLimit for its
// files.info: the conversation opens after all, unless another navigation
// or click came first.
func (a *App) canvasWaitOver(fid string, gen uint64) {
	st := a.canvasJumpsOf()
	w, ok := st.waiting[fid]
	if !ok || w.gen != gen {
		return
	}
	delete(st.waiting, fid)
	if st.gen == gen && (w.still == nil || w.still()) {
		a.inSplitIf(w.split, func() { a.canvasFallback(w.fallback) })
	}
}

// canvasJumpNames resumes the navigations that waited for their file's
// files.info ("names" with the file id): the canvas or list opens, or,
// when the file is neither, the conversation asked for.
func (a *App) canvasJumpNames(ids []string) {
	st := a.canvasJumpsOf()
	if len(st.waiting) == 0 || a.Core() == nil {
		return
	}
	for _, id := range ids {
		w, ok := st.waiting[id]
		if !ok {
			continue
		}
		delete(st.waiting, id)
		if w.gen != st.gen || w.still != nil && !w.still() {
			continue // another navigation or click since
		}
		switch fid, kind, wait := a.fileKind(w.ch, w.at, w.jump); {
		case wait:
			st.waiting[id] = w // still unknown: the next "names", or the wait's end
		case kind != "":
			a.inSplitIf(w.split, func() { w.open(fid, kind) })
		default:
			a.inSplitIf(w.split, func() { a.canvasFallback(w.fallback) })
		}
	}
}

// ThreadCanvas is messages.FileThreadHost: canvas file channel ch's
// canvas as Slack's thread header draws it (SmallFileEntity with
// useLeadingEmojiAsIcon, tab_emoji_icon.go's rules): the canvas-content
// icon, or its title's leading emoji's image, and its title without that
// emoji; a deleted canvas's icon is the trash can, one the viewer can't
// see the closed eye.
func (a *App) ThreadCanvas(ch string) (icon, emoji string, title []richtext.Span, ok bool) {
	c := a.Core()
	if c == nil || !c.IsFileChannelID(ch) {
		return "", "", nil, false
	}
	fid := core.FileOfChannel(ch)
	ref, known := c.S.KnownCanvasRef(fid)
	if !known || ref.Kind != "canvas" {
		return "", "", nil, false
	}
	t := core.Tab{Type: "canvas", FileID: fid, FileTitle: ref.Title, TitleSpans: richtext.TitleBlocks(c.S, ref.Title, ref.TitleBlocks)}
	label := ref.Title
	if len(t.TitleSpans) > 0 {
		label = richtext.PlainSpans(t.TitleSpans)
	}
	_, emoji, label, title = docTab(t, label, tabSpans(t, label, c.Emoji), false, c.Emoji)
	if title == nil && label != "" { // a title without emoji is its text (tabLabelSpans: nil)
		title = []richtext.Span{{Kind: richtext.Text, Text: label}}
	}
	switch ref.Status {
	case "deleted":
		return "trash", "", title, true
	case "hidden":
		return "eye-closed", "", title, true
	}
	return "canvas-content", emoji, title, true
}

// PrimaryCanvas is messages.FileThreadHost: the primary view is a canvas,
// its full view or the Activity persistent detail panel's pane drawing
// one (openDoc's Primary).
func (a *App) PrimaryCanvas() bool {
	r := a.router.Current()
	return canvasRouteID(r) != "" || r.Page() == "activity" && a.paneCanvasLive() != nil
}
