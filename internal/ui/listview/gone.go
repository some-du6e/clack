package listview

import (
	"context"
	"errors"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// BaseListErrorState (uVC3, boot-shared; "lists" namespace) is what a list
// Slack doesn't show draws in place of the list, in every mode:
//
//   - on mount, in every state, files.canUndelete {file_id} (reason
//     "canUndelete"), whose answer makes the restore link and fills
//     UndeleteFileDialog;
//   - deleted (a tombstone or is_deleted) with an id: the trash and "This
//     list was deleted.", and when can_undelete "Was that a mistake?
//     Restore this list." with "Restore this list" a link to
//     UndeleteFileDialog, whose restore reloads the list;
//   - DLP tombstoned: the trash and "Tombstoned";
//   - not visible or access denied: the lock and "You don’t have access to
//     this list.", and FileRequestAccessButton when the list isn't
//     externally owned (hc82: a blocked list is; the other case needs the
//     file's team, which a stand-in doesn't have) and lists are on (c4db
//     Il, which they are wherever Clack opens one);
//   - a blocked list (isBlockedExternalList): the lock, "Can’t view this
//     list" and, as a blocked list is externally owned, "Another team owns
//     this list. Lists from external organizations can’t be viewed by
//     members of {currentWorkspaceOrgName}." (the org's name, else "your
//     organization");
//   - anything else (not found): help and "This list was not found."
//
// The canvas's state (canvasview.QuipDeletedState) shares its geometry,
// popups.FileState.

var nsListsErr = i18n.NS("lists")

// listGone is the view's BaseListErrorState bookkeeping.
type listGone struct {
	for_     error  // the load answer the state mounted for
	asked    string // the file canUndelete was asked for
	can      bool
	file     core.DeletedFile
	restore  widget.Clickable
	req      widget.Clickable
	sent     bool
	sentFor  string
	answered bool
}

// goneOf is err's file state, ok false for a load that failed.
func goneOf(err error) (core.FileGone, bool) {
	var ge *core.FileGoneError
	if errors.As(err, &ge) {
		return ge.Gone, true
	}
	return core.FileGone{}, false
}

// layoutGone draws BaseListErrorState for g over the body.
func (v *View) layoutGone(gtx layout.Context, err error, g core.FileGone) layout.Dimensions {
	st := &v.gone
	if st.for_ != err || st.asked != v.id {
		st.for_ = err
		if st.asked != v.id {
			st.asked, st.can, st.file, st.answered = v.id, false, core.DeletedFile{}, false
			v.askCanUndelete(v.id)
		}
	}
	if st.sentFor != v.id {
		st.sentFor, st.sent = v.id, false
	}
	deleted := g.Deleted() && v.id != ""
	noAccess := g.NotVisible() || g.Denied()
	s := popups.FileState{List: true}
	switch {
	case (deleted || g.Tombstoned) && v.id != "":
		s.Icon = "trash"
	case noAccess || g.ListBlocked():
		s.Icon = "lock"
	default:
		s.Icon = "help"
	}
	switch {
	case deleted:
		s.Title = i18n.Plain(nsListsErr.RT("This list was deleted."))
		if st.can {
			parts := nsListsErr.RT("Was that a mistake? <link>Restore this list</link>.")
			s.Sub = i18n.Plain(parts)
			for _, p := range parts {
				if p.Tag == "link" {
					s.Link = p.Text
				}
			}
			s.LinkBtn = &st.restore
			if st.restore.Clicked(gtx) && st.file.ID != "" && v.act.Restore != nil {
				f := st.file
				f.FileType = "list"
				v.act.Restore(f, func() { v.load(false) }) // onSuccessfulUndelete: the list again
			}
		}
	case g.Tombstoned:
		s.Title = i18n.Plain(nsListsErr.RT("Tombstoned"))
	case noAccess:
		s.Title = nsListsErr.T("You don’t have access to this list.")
	case g.ListBlocked():
		s.Title = nsListsErr.T("Can’t view this list")
		s.Sub = i18n.Plain(nsListsErr.RT("Another team owns this list. Lists from external organizations can’t be viewed by members of {currentWorkspaceOrgName}.", "currentWorkspaceOrgName", v.orgName()))
	default:
		s.Title = i18n.Plain(nsListsErr.RT("This list was not found."))
	}
	if noAccess && !g.ListBlocked() && v.id != "" {
		s.ButtonBtn = &st.req
		if st.req.Clicked(gtx) && !st.sent {
			st.sent = true
			if ph, ok := v.h.(popups.Host); ok {
				popups.RequestFileAccess(ph, v.id, v.post)
			}
		}
		s.Button, s.ButtonOff = nsReqDone.T("Request Access"), st.sent
		if st.sent {
			s.Button = nsReqDone.T("Request Sent")
		}
	}
	if ph, ok := v.h.(popups.Host); ok {
		return s.Layout(gtx, ph)
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

var nsReqDone = i18n.NS("request_access_done")

// askCanUndelete is the state's mount effect: files.canUndelete.
func (v *View) askCanUndelete(id string) {
	a := v.h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		can, f, err := a.CanUndeleteFile(ctx, id)
		v.post(func() {
			st := &v.gone
			if st.asked != id || err != nil {
				return
			}
			st.can, st.file, st.answered = can, f, true
		})
	}()
}

// orgName is k.fu's org through q0ke: the org's name on a grid, else
// "your organization".
func (v *View) orgName() string {
	a := v.h.Core()
	if a != nil {
		if u, ok := a.S.User(a.S.SelfID()); ok && u.EnterpriseUser != nil && u.EnterpriseUser.EnterpriseName != "" {
			return u.EnterpriseUser.EnterpriseName
		}
	}
	return nsListsErr.T("your organization")
}
