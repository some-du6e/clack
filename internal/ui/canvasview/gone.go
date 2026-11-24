package canvasview

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// QuipDeletedState (fth7, boot-shared) is what a canvas Slack doesn't
// show draws in place of the canvas: QuipEmbedWithEntityId returns it
// before any preview logic for a file shouldShowFile rejects, in every
// mode (a channel tab, the side panel, the full view, a peek). Its strings
// are in the "files" namespace:
//
//   - deleted (a tombstone or is_deleted) with an id: one of six titles with its
//     emoji (nAOp Ft), "It’s been deleted by the owner." under it.
//     getDeletedState picks the index at random on every render of the
//     state, which is on mount and when the file object changes: Clack
//     picks it when the state first draws for a load's answer, never per
//     frame.
//   - DLP tombstoned: the lock, getDLPTombstoneMessage.
//   - a blocked canvas (isBlockedExternalCanvas): the lock, "Can’t view
//     this canvas" and a subtitle by file_access. Such a canvas is
//     externally owned (n3pm: isBlockedExternalCanvas).
//   - access denied: the lock, "Need permission to view" and its subtitle.
//   - template not visible: the lock, "You don’t have access to this
//     template."
//   - not visible: the lock, "You don’t have access to this canvas.", and
//     FileRequestAccessButton (not on an externally owned canvas: n3pm's
//     other case needs the file's team, which a stand-in doesn't have).
//   - hidden by the storage limit: eye-closed and FileStorageWarning
//     (d4jA) for "quip": "This file is hidden because it was uploaded more
//     than 90 days ago." with its upgrade link (Slack shows nothing until
//     its trial check answers; Clack takes the no-trial copy, its link
//     drawn as text).
//   - anything else (not found): help and "This canvas was not found."
//
// isDSARestricted's "Restricted" needs restriction_type, which fileGoneOf
// doesn't keep, so it isn't drawn.

// deletedStates is nAOp's Ft: a title and its emoji per index.
var deletedStates = [6][2]string{
	{"This canvas has gone fishing", "fishing_pole_and_fish"},
	{"This canvas has flown the coop", "bird"},
	{"This canvas was swept away", "broom"},
	{"This canvas is on permanent vacation", "palm_tree"},
	{"This canvas was retired", "cake"},
	{"This canvas’s work is all done", "mechanical_arm"},
}

// pickDeleted is getDeletedState; tests set it.
var pickDeleted = func() int { return rand.IntN(len(deletedStates)) }

var nsFiles = i18n.NS("files")

// goneOf is err's file state, ok false for a load that failed.
func goneOf(err error) (core.FileGone, bool) {
	var ge *core.FileGoneError
	if errors.As(err, &ge) {
		return ge.Gone, true
	}
	return core.FileGone{}, false
}

// goneState is the view's QuipDeletedState bookkeeping: the load answer
// it was drawn for, the deleted title picked for it, and the Request
// Access button.
type goneState struct {
	for_  error
	pick  int
	req   widget.Clickable
	sent  bool
	reqID string
	// title and button are what it last drew (GoneTitle); clickReq is a
	// test's click on the button
	title, button string
	clickReq      bool
}

// layoutGone draws QuipDeletedState for g over the body.
func (v *View) layoutGone(gtx layout.Context, err error, g core.FileGone) layout.Dimensions {
	st := &v.gone
	if st.for_ != err {
		st.for_, st.pick = err, pickDeleted()
	}
	if st.reqID != v.id {
		st.reqID, st.sent = v.id, false
	}
	s := popups.FileState{}
	org := v.teamName()
	deleted := g.Deleted() && v.id != ""
	// getIconForFile
	switch {
	case deleted:
		d := deletedStates[st.pick]
		if a := v.h.Core(); a != nil {
			if u, _, ok := a.Emoji(d[1]); ok {
				s.Emoji = u
			}
		}
		if s.Emoji == "" {
			s.Icon = "trash"
		}
	case g.NotVisible() || g.Denied() || g.CanvasBlocked() || g.TemplateHidden() || g.Tombstoned:
		s.Icon = "lock"
	case g.Limit():
		s.Icon = "eye-closed"
	default:
		s.Icon = "help"
	}
	// getTextForFile
	switch {
	case g.Limit(): // FileStorageWarning for "quip", once Slack knows the viewer can't trial
		s.Sub = i18n.Plain(nsFiles.RT("This file is hidden because it was uploaded more than {limitInDays} days ago. <a>Upgrade to a paid plan</a> to view.", "limitInDays", 90))
	case g.Deleted():
		if deleted {
			s.Title, s.Sub = nsFiles.T(deletedStates[st.pick][0]), nsFiles.T("It’s been deleted by the owner.")
		}
	case g.Tombstoned:
		s.Title = g.DLPMessage()
	case g.CanvasBlocked():
		s.Title = nsFiles.T("Can’t view this canvas")
		other := i18n.Plain(nsFiles.RT("Another team owns this canvas. Canvases from this organization can’t be viewed by members of {currentWorkspaceOrgName}.", "currentWorkspaceOrgName", org))
		switch {
		case g.FileAccess == "canvas_globally_disabled":
			s.Sub = nsFiles.T("Canvas has been turned off by your admins.")
		case g.FileAccess == "canvas_disabled_user_team": // a blocked canvas is externally owned (n3pm)
			s.Sub = i18n.Plain(nsFiles.RT("Another team owns this canvas. Canvases from external organizations can’t be viewed by members of {currentWorkspaceOrgName}.", "currentWorkspaceOrgName", org))
		case g.FileAccess == "slack_connect_team_blocked":
			s.Sub = i18n.Plain(nsFiles.RT("This feature is still being rolled out so your organization may not be able to view this canvas yet."))
		case g.FileAccess == "slack_connect_canvas_sharing_blocked", g.FileAccess == "canvas_disabled_file_team", g.Mode == "canvas_slack_connect_blocked":
			s.Sub = other
		default:
			s.Sub = nsFiles.T("This feature is in development and not yet available to your organization.")
		}
	case g.Denied():
		s.Title = nsFiles.T("Need permission to view")
		s.Sub = nsFiles.T("Another team owns this canvas. It’s only been shared with specific people in your organization.")
	case g.TemplateHidden():
		s.Title = nsFiles.T("You don’t have access to this template.")
	case g.NotVisible():
		s.Title = nsFiles.T("You don’t have access to this canvas.")
	default:
		s.Title = i18n.Plain(nsFiles.RT("This canvas was not found."))
	}
	if g.NotVisible() && !g.CanvasBlocked() && v.id != "" {
		s.ButtonBtn = &st.req
		clicked := st.req.Clicked(gtx) || st.clickReq
		st.clickReq = false
		if clicked && !st.sent {
			st.sent = true
			v.requestFileAccess(v.id)
		}
		s.Button, s.ButtonOff = nsRequestDone.T("Request Access"), st.sent
		if st.sent {
			s.Button = nsRequestDone.T("Request Sent")
		}
	}
	st.title, st.button = s.Title, s.Button
	return s.Layout(gtx, v.h.Theme(), v.h.Images())
}

var nsRequestDone = i18n.NS("request_access_done")

// teamName is q0ke's name for getTeamId: the workspace's (the org's when
// the viewer has no workspace).
func (v *View) teamName() string {
	a := v.h.Core()
	if a == nil {
		return ""
	}
	if n, _, ok := a.Team(a.CurrentTeamID(), 20); ok && n != "" {
		return n
	}
	a.S.RLock()
	defer a.S.RUnlock()
	return strings.TrimSpace(a.S.TeamName)
}

// Gone is the state the view draws for its file, ok false while it shows
// the canvas (or loads, or a load failed).
func (v *View) Gone() (core.FileGone, bool) {
	v.mu.Lock()
	d, err := v.doc, v.err
	v.mu.Unlock()
	if d != nil || err == nil {
		return core.FileGone{}, false
	}
	return goneOf(err)
}

// GoneTitle is the deleted state's title and the Request Access button's
// label as last drawn (tests).
func (v *View) GoneTitle() (title, button string) { return v.gone.title, v.gone.button }

// requestFileAccess is FileRequestAccessButton's click: popups.RequestFileAccess
// and its dialogs, or (a host without dialogs) the request alone.
func (v *View) requestFileAccess(id string) {
	if ph, ok := v.h.(popups.Host); ok {
		popups.RequestFileAccess(ph, id, v.post)
		return
	}
	if a := v.h.Core(); a != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = a.RequestFileAccess(ctx, id, "DirectRequestAccessModal", "access")
		}()
	}
}

// GoneRequestAccess clicks Request Access (tests: the harness has no
// pointer).
func (v *View) GoneRequestAccess() { v.gone.clickReq = true }
